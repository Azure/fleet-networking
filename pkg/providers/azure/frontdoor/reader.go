/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package frontdoor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"

	"go.goms.io/fleet-networking/pkg/common/azureerrors"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
)

// OriginObserver reads only an owned profile and its expected private origins.
// It does not create resources, approve connections or schedule controller polls.
type OriginObserver struct {
	scope    ResourceScope
	profiles *armcdn.ProfilesClient
	origins  *armcdn.AFDOriginsClient
}

// NewOriginObserver binds the read-only SDK clients and ownership checks to one
// scope. Construction makes no Azure requests.
func NewOriginObserver(scope ResourceScope, credential azcore.TokenCredential, options *arm.ClientOptions) (*OriginObserver, error) {
	profiles, err := armcdn.NewProfilesClient(scope.SubscriptionID, credential, options)
	if err != nil {
		return nil, fmt.Errorf("initialize AFD observation profiles client: %w", err)
	}
	origins, err := armcdn.NewAFDOriginsClient(scope.SubscriptionID, credential, options)
	if err != nil {
		return nil, fmt.Errorf("initialize AFD origins client: %w", err)
	}
	return &OriginObserver{scope: scope, profiles: profiles, origins: origins}, nil
}

// Observe reads a fresh, bounded SDK snapshot and evaluates only expected origins.
// All pages must succeed; API/identity failures return zero observation, not
// partial readiness. These sequential reads are not a transactional ARM snapshot.
func (o *OriginObserver) Observe(ctx context.Context, owner GatewayOwner,
	model gatewaymodel.GlobalGateway) (PrivateOriginsObservation, error) {
	if err := ctx.Err(); err != nil {
		return PrivateOriginsObservation{}, err
	}
	if err := validateModelOwner(owner, model); err != nil {
		return PrivateOriginsObservation{}, err
	}
	roots, err := BuildRootRequests(o.scope, owner)
	if err != nil {
		return PrivateOriginsObservation{}, err
	}
	plan, err := PlanBackends(model)
	if err != nil {
		return PrivateOriginsObservation{}, err
	}
	if o.profiles == nil || o.origins == nil {
		return PrivateOriginsObservation{}, fmt.Errorf("AFD observation clients are not initialized")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	profile, err := o.profiles.Get(ctx, o.scope.ResourceGroup, roots.ProfileName, nil)
	if azureerrors.IsNotFound(err) {
		return PrivateOriginsObservation{ProfileMissing: true}, nil
	}
	if err != nil {
		return PrivateOriginsObservation{}, fmt.Errorf("observe owned AFD profile: %w", err)
	}
	if err := VerifyProfileOwnership(o.scope, owner, profile.Profile); err != nil {
		return PrivateOriginsObservation{}, err
	}
	observed := make(map[OriginReference]armcdn.AFDOrigin, plan.Capacity.Origins)
	for _, group := range plan.OriginGroups {
		origins, err := o.readOriginGroup(ctx, roots, group)
		if err != nil {
			return PrivateOriginsObservation{}, err
		}
		for ref, origin := range origins {
			observed[ref] = origin
		}
	}
	if err := ctx.Err(); err != nil {
		return PrivateOriginsObservation{}, err
	}
	return ObservePrivateOrigins(o.scope, owner, model, profile.Profile, observed)
}

func (o *OriginObserver) readOriginGroup(ctx context.Context, roots RootRequests,
	group OriginGroup) (map[OriginReference]armcdn.AFDOrigin, error) {
	expected := make(map[string]bool, len(group.Origins))
	for _, origin := range group.Origins {
		expected[origin.Name] = true
	}
	observed := make(map[OriginReference]armcdn.AFDOrigin, len(group.Origins))
	pager := o.origins.NewListByOriginGroupPager(o.scope.ResourceGroup, roots.ProfileName, group.Name, nil)
	for pager.More() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page, err := pager.NextPage(ctx)
		if azureerrors.IsNotFound(err) {
			// The group disappeared; discard even origins returned on earlier pages.
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("list AFD origin group %q: %w", group.Name, err)
		}
		for _, origin := range page.Value {
			if origin == nil || origin.Name == nil || *origin.Name == "" {
				return nil, fmt.Errorf("AFD origin group %q returned an origin without identity", group.Name)
			}
			name := strings.ToLower(*origin.Name)
			if !expected[name] {
				continue
			}
			ref := OriginReference{OriginGroup: group.Name, Origin: name}
			expectedID := roots.ProfileID + "/originGroups/" + group.Name + "/origins/" + name
			if err := verifyOriginIdentity(expectedID, ref, *origin); err != nil {
				return nil, err
			}
			if _, duplicate := observed[ref]; duplicate {
				return nil, fmt.Errorf("AFD origin group %q returned duplicate origin %q", group.Name, name)
			}
			observed[ref] = *origin
		}
	}
	return observed, nil
}
