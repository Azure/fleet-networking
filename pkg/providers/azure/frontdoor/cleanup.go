/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package frontdoor

import (
	"context"
	"fmt"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"

	"go.goms.io/fleet-networking/pkg/common/azureerrors"
)

// ProfileCleanup deletes only verified owned profiles and their AFD children.
// It never manages member PLS/LB, external DNS or external policy resources.
type ProfileCleanup struct {
	scope    ResourceScope
	profiles *armcdn.ProfilesClient
}

// NewProfileCleanup binds the SDK client and ownership checks to the same scope.
// Client construction makes no Azure requests.
func NewProfileCleanup(scope ResourceScope, credential azcore.TokenCredential, options *arm.ClientOptions) (*ProfileCleanup, error) {
	profiles, err := armcdn.NewProfilesClient(scope.SubscriptionID, credential, options)
	if err != nil {
		return nil, fmt.Errorf("initialize AFD profiles client: %w", err)
	}
	return &ProfileCleanup{scope: scope, profiles: profiles}, nil
}

// DeleteProfile returns true only after observing that the owned root is absent.
// Callers must retain the Gateway cleanup finalizer while false or on any error.
func (p *ProfileCleanup) DeleteProfile(ctx context.Context, owner GatewayOwner) (bool, error) {
	requests, err := BuildRootRequests(p.scope, owner)
	if err != nil {
		return false, err
	}
	if p.profiles == nil {
		return false, fmt.Errorf("AFD profiles client is not initialized")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	observed, err := p.profiles.Get(ctx, p.scope.ResourceGroup, requests.ProfileName, nil)
	if azureerrors.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("observe AFD profile for cleanup: %w", err)
	}
	if err := VerifyProfileOwnership(p.scope, owner, observed.Profile); err != nil {
		return false, err
	}
	if properties := observed.Properties; properties != nil {
		if (properties.ResourceState != nil && *properties.ResourceState == armcdn.ProfileResourceStateDeleting) ||
			(properties.ProvisioningState != nil && *properties.ProvisioningState == armcdn.ProfileProvisioningStateDeleting) {
			return false, nil
		}
	}
	// Do not wait on or persist a poller. A later GET proves deletion, including after restart.
	if _, err := p.profiles.BeginDelete(ctx, p.scope.ResourceGroup, requests.ProfileName, nil); err != nil {
		if azureerrors.IsNotFound(err) {
			return true, nil
		}
		return false, fmt.Errorf("begin owned AFD profile deletion: %w", err)
	}
	return false, nil
}
