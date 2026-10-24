/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package frontdoor

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	"k8s.io/utils/ptr"

	"go.goms.io/fleet-networking/pkg/common/azureerrors"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
)

// BackendProvisioningState describes only desired backend staging, not ingress readiness.
type BackendProvisioningState string

// Backend staging requires observed disabled roots and subsequent write observations.
// Configured permits initial route construction, never deployed-origin readiness.
const (
	BackendRootsPending       BackendProvisioningState = "RootsPending"
	BackendGroupPending       BackendProvisioningState = "GroupPending"
	BackendOriginsPending     BackendProvisioningState = "OriginsPending"
	PrivateBackendsConverged  BackendProvisioningState = "PrivateBackendsConverged"
	PrivateBackendsConfigured BackendProvisioningState = "PrivateBackendsConfigured"
)

// BackendProvisioningResult does not establish route readiness or stale-resource removal.
// Origins is populated only after reading every expected origin without a submitted write.
type BackendProvisioningResult struct {
	State   BackendProvisioningState
	Origins PrivateOriginsObservation
}

// BackendProvisioner stages expected private backends only under observed disabled roots.
// It is not wired into the manager and does not authorize production writes.
type BackendProvisioner struct {
	roots   *RootProvisioner
	groups  *armcdn.AFDOriginGroupsClient
	origins *armcdn.AFDOriginsClient
}

// NewBackendProvisioner constructs scope-bound SDK clients without Azure requests.
func NewBackendProvisioner(scope ResourceScope, credential azcore.TokenCredential, options *arm.ClientOptions) (*BackendProvisioner, error) {
	roots, err := NewRootProvisioner(scope, credential, options)
	if err != nil {
		return nil, err
	}
	groups, err := armcdn.NewAFDOriginGroupsClient(scope.SubscriptionID, credential, options)
	if err != nil {
		return nil, fmt.Errorf("initialize AFD origin groups client: %w", err)
	}
	origins, err := armcdn.NewAFDOriginsClient(scope.SubscriptionID, credential, options)
	if err != nil {
		return nil, fmt.Errorf("initialize AFD provisioning origins client: %w", err)
	}
	return &BackendProvisioner{roots: roots, groups: groups, origins: origins}, nil
}

// EnsurePrivateBackends submits at most one write under one 30-second budget.
// Complete intent is validated before ARM access; roots are observed, never mutated.
// Submitted writes require later GETs, and pending approval does not starve later origins.
// Sequential reads are not transactional; finalizer/platform/concurrent-writer gates
// remain prerequisites for production integration.
func (p *BackendProvisioner) EnsurePrivateBackends(ctx context.Context, owner GatewayOwner,
	model gatewaymodel.GlobalGateway) (BackendProvisioningResult, error) {
	if err := ctx.Err(); err != nil {
		return BackendProvisioningResult{}, err
	}
	if p.roots == nil || p.roots.profiles == nil || p.roots.endpoints == nil || p.groups == nil || p.origins == nil {
		return BackendProvisioningResult{}, fmt.Errorf("AFD backend provisioning clients are not initialized")
	}
	graph, err := BuildHTTPPreviewRequests(p.roots.scope, owner, model)
	if err != nil {
		return BackendProvisioningResult{}, err
	}
	plan, err := PlanBackends(model)
	if err != nil {
		return BackendProvisioningResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	profile, endpoint, found, err := p.observeRoots(ctx, owner, graph.Roots)
	if err != nil {
		return BackendProvisioningResult{}, err
	}
	if !found {
		return BackendProvisioningResult{State: BackendRootsPending}, nil
	}
	ready, err := endpointReadyForStaging(endpoint)
	if err != nil {
		return BackendProvisioningResult{}, err
	}
	if !ready {
		return BackendProvisioningResult{State: BackendRootsPending}, nil
	}
	group := graph.Backends.OriginGroups[0]
	observedGroup, ready, err := p.ensureGroup(ctx, graph.Roots, group)
	if err != nil {
		return BackendProvisioningResult{}, err
	}
	if !ready {
		return BackendProvisioningResult{State: BackendGroupPending}, nil
	}
	observed := make(map[OriginReference]armcdn.AFDOrigin, len(group.Origins))
	for i, origin := range group.Origins {
		actual, submitted, err := p.ensureOrigin(ctx, graph.Roots, group.Name, plan.OriginGroups[0].Origins[i], origin)
		if err != nil {
			return BackendProvisioningResult{}, err
		}
		if submitted {
			return BackendProvisioningResult{State: BackendOriginsPending}, nil
		}
		observed[OriginReference{OriginGroup: group.Name, Origin: origin.Name}] = actual
	}
	if err := ctx.Err(); err != nil {
		return BackendProvisioningResult{}, err
	}
	origins, err := ObservePrivateOrigins(p.roots.scope, owner, model, profile, observed)
	if err != nil {
		return BackendProvisioningResult{}, err
	}
	result := BackendProvisioningResult{State: BackendOriginsPending, Origins: origins}
	bootstrap, err := bootstrapEndpoint(endpoint)
	if err != nil {
		return BackendProvisioningResult{}, err
	}
	if bootstrap && deploymentReadyForBootstrap(observedGroup.Properties.DeploymentStatus) {
		configured := len(observed) > 0
		for _, origin := range observed {
			configured = configured && originApprovedAndProvisioned(origin.Properties)
		}
		if configured {
			result.State = PrivateBackendsConfigured
		}
		return result, nil
	}
	if origins.OriginsConverged && ptr.Deref(observedGroup.Properties.DeploymentStatus, "") == armcdn.DeploymentStatusSucceeded {
		result.State = PrivateBackendsConverged
	}
	return result, nil
}

func (p *BackendProvisioner) observeDisabledRoots(ctx context.Context, owner GatewayOwner,
	roots RootRequests) (armcdn.Profile, bool, error) {
	profile, endpoint, found, err := p.observeRoots(ctx, owner, roots)
	if err != nil || !found {
		return armcdn.Profile{}, false, err
	}
	disabled, err := endpointReadyForStaging(endpoint)
	return profile, disabled, err
}

func (p *BackendProvisioner) observeRoots(ctx context.Context, owner GatewayOwner,
	roots RootRequests) (armcdn.Profile, armcdn.AFDEndpoint, bool, error) {
	if err := ctx.Err(); err != nil {
		return armcdn.Profile{}, armcdn.AFDEndpoint{}, false, err
	}
	scope := p.roots.scope
	profile, err := p.roots.profiles.Get(ctx, scope.ResourceGroup, roots.ProfileName, nil)
	if err := ctx.Err(); err != nil {
		return armcdn.Profile{}, armcdn.AFDEndpoint{}, false, err
	}
	if azureerrors.IsNotFound(err) {
		return armcdn.Profile{}, armcdn.AFDEndpoint{}, false, nil
	}
	if err != nil {
		return armcdn.Profile{}, armcdn.AFDEndpoint{}, false, fmt.Errorf("observe profile before backend staging: %w", err)
	}
	if err := VerifyProfileOwnership(scope, owner, profile.Profile); err != nil {
		return armcdn.Profile{}, armcdn.AFDEndpoint{}, false, err
	}
	ready, err := profileReadyForChildren(profile.Profile)
	if err != nil || !ready {
		return armcdn.Profile{}, armcdn.AFDEndpoint{}, false, err
	}
	if err := ctx.Err(); err != nil {
		return armcdn.Profile{}, armcdn.AFDEndpoint{}, false, err
	}
	endpoint, err := p.roots.endpoints.Get(ctx, scope.ResourceGroup, roots.ProfileName, roots.EndpointName, nil)
	if err := ctx.Err(); err != nil {
		return armcdn.Profile{}, armcdn.AFDEndpoint{}, false, err
	}
	if azureerrors.IsNotFound(err) {
		return armcdn.Profile{}, armcdn.AFDEndpoint{}, false, nil
	}
	if err != nil {
		return armcdn.Profile{}, armcdn.AFDEndpoint{}, false, fmt.Errorf("observe endpoint before backend staging: %w", err)
	}
	if err := verifyEndpointIdentity(roots, endpoint.AFDEndpoint); err != nil {
		return armcdn.Profile{}, armcdn.AFDEndpoint{}, false, err
	}
	return profile.Profile, endpoint.AFDEndpoint, true, nil
}

func disabledEndpointConverged(properties *armcdn.AFDEndpointProperties) (bool, error) {
	return endpointConverged(properties, armcdn.EnabledStateDisabled)
}

func endpointConverged(properties *armcdn.AFDEndpointProperties, expected armcdn.EnabledState) (bool, error) {
	ready, err := afdProvisioningReady(properties.ProvisioningState)
	if err != nil || !ready {
		return false, err
	}
	switch state := ptr.Deref(properties.EnabledState, ""); state {
	case armcdn.EnabledStateEnabled, armcdn.EnabledStateDisabled:
	default:
		return false, fmt.Errorf("observed owned AFD endpoint has unknown enabled state %q", state)
	}
	if *properties.EnabledState != expected {
		return false, nil
	}
	_, ready, err = observedEndpointDeployment(properties)
	return ready, err
}

func (p *BackendProvisioner) ensureGroup(ctx context.Context, roots RootRequests,
	group OriginGroupRequest) (armcdn.AFDOriginGroup, bool, error) {
	if err := ctx.Err(); err != nil {
		return armcdn.AFDOriginGroup{}, false, err
	}
	scope := p.roots.scope
	actual, err := p.groups.Get(ctx, scope.ResourceGroup, roots.ProfileName, group.Name, nil)
	if err := ctx.Err(); err != nil {
		return armcdn.AFDOriginGroup{}, false, err
	}
	if azureerrors.IsNotFound(err) {
		if _, err := p.groups.BeginCreate(ctx, scope.ResourceGroup, roots.ProfileName, group.Name, group.Parameters, nil); err != nil {
			return armcdn.AFDOriginGroup{}, false, fmt.Errorf("begin private AFD origin group creation: %w", err)
		}
		return armcdn.AFDOriginGroup{}, false, ctx.Err()
	}
	if err != nil {
		return armcdn.AFDOriginGroup{}, false, fmt.Errorf("observe AFD origin group %q: %w", group.Name, err)
	}
	if err := verifyGroupIdentity(roots, group.Name, actual.AFDOriginGroup); err != nil {
		return armcdn.AFDOriginGroup{}, false, err
	}
	ready, err := afdProvisioningReady(actual.Properties.ProvisioningState)
	if err != nil || !ready {
		return armcdn.AFDOriginGroup{}, false, err
	}
	if err := validateAFDDeploymentState(actual.Properties.DeploymentStatus); err != nil {
		return armcdn.AFDOriginGroup{}, false, err
	}
	desired := group.Parameters.Properties
	if !matchesOriginGroupPolicy(desired, actual.Properties) {
		parameters := armcdn.AFDOriginGroupUpdateParameters{Properties: &armcdn.AFDOriginGroupUpdatePropertiesParameters{
			HealthProbeSettings: desired.HealthProbeSettings, SessionAffinityState: desired.SessionAffinityState,
		}}
		if err := ctx.Err(); err != nil {
			return armcdn.AFDOriginGroup{}, false, err
		}
		if _, err := p.groups.BeginUpdate(ctx, scope.ResourceGroup, roots.ProfileName, group.Name, parameters, nil); err != nil {
			return armcdn.AFDOriginGroup{}, false, fmt.Errorf("begin private AFD origin group update: %w", err)
		}
		return armcdn.AFDOriginGroup{}, false, ctx.Err()
	}
	return actual.AFDOriginGroup, true, nil
}

func matchesOriginGroupPolicy(desired, actual *armcdn.AFDOriginGroupProperties) bool {
	// Service-returned load-balancing defaults are not controller-managed policy.
	return reflect.DeepEqual(actual.HealthProbeSettings, desired.HealthProbeSettings) &&
		ptr.Deref(actual.SessionAffinityState, "") == ptr.Deref(desired.SessionAffinityState, "")
}

func verifyGroupIdentity(roots RootRequests, name string, group armcdn.AFDOriginGroup) error {
	if group.ID == nil || !strings.EqualFold(*group.ID, roots.ProfileID+"/originGroups/"+name) ||
		(group.Name != nil && !strings.EqualFold(*group.Name, name)) || group.Properties == nil {
		return fmt.Errorf("observed AFD origin group %q lacks its expected owned identity/properties", name)
	}
	if parent := group.Properties.ProfileName; parent != nil && !strings.EqualFold(*parent, roots.ProfileName) {
		return fmt.Errorf("observed AFD origin group %q contradicts its expected owned profile", name)
	}
	return nil
}

func (p *BackendProvisioner) ensureOrigin(ctx context.Context, roots RootRequests, group string,
	desired Origin, request OriginRequest) (armcdn.AFDOrigin, bool, error) {
	if err := ctx.Err(); err != nil {
		return armcdn.AFDOrigin{}, false, err
	}
	scope := p.roots.scope
	actual, err := p.origins.Get(ctx, scope.ResourceGroup, roots.ProfileName, group, request.Name, nil)
	if err := ctx.Err(); err != nil {
		return armcdn.AFDOrigin{}, false, err
	}
	if azureerrors.IsNotFound(err) {
		if _, err := p.origins.BeginCreate(ctx, scope.ResourceGroup, roots.ProfileName, group, request.Name, request.Parameters, nil); err != nil {
			return armcdn.AFDOrigin{}, false, fmt.Errorf("begin private AFD origin creation: %w", err)
		}
		return armcdn.AFDOrigin{}, true, ctx.Err()
	}
	if err != nil {
		return armcdn.AFDOrigin{}, false, fmt.Errorf("observe AFD origin %q: %w", request.Name, err)
	}
	ref := OriginReference{OriginGroup: group, Origin: request.Name}
	if err := verifyOriginIdentity(roots.ProfileID+"/originGroups/"+group+"/origins/"+request.Name, ref, actual.AFDOrigin); err != nil {
		return armcdn.AFDOrigin{}, false, err
	}
	if !matchesPrivateLinkTarget(desired.PrivateLink, actual.Properties) {
		return armcdn.AFDOrigin{}, false, fmt.Errorf("observed AFD origin %q cannot be adopted from a public or different Private Link target", request.Name)
	}
	ready, err := afdProvisioningReady(actual.Properties.ProvisioningState)
	if err != nil {
		return armcdn.AFDOrigin{}, false, err
	}
	if !ready {
		return actual.AFDOrigin, false, nil
	}
	if err := validateAFDDeploymentState(actual.Properties.DeploymentStatus); err != nil {
		return armcdn.AFDOrigin{}, false, err
	}
	// Omitting a PATCH field cannot be relied on to clear an existing override.
	if ptr.Deref(actual.Properties.OriginHostHeader, "") != "" {
		return armcdn.AFDOrigin{}, false, fmt.Errorf("observed AFD origin %q has a Host header override; automatic clearing is not supported", request.Name)
	}
	if !matchesPrivateOrigin(desired, actual.Properties) {
		properties := request.Parameters.Properties
		parameters := armcdn.AFDOriginUpdateParameters{Properties: &armcdn.AFDOriginUpdatePropertiesParameters{
			HostName: properties.HostName, HTTPPort: properties.HTTPPort, Weight: properties.Weight,
			Priority: properties.Priority, EnabledState: properties.EnabledState,
			EnforceCertificateNameCheck: properties.EnforceCertificateNameCheck, SharedPrivateLinkResource: properties.SharedPrivateLinkResource,
		}}
		if err := ctx.Err(); err != nil {
			return armcdn.AFDOrigin{}, false, err
		}
		if _, err := p.origins.BeginUpdate(ctx, scope.ResourceGroup, roots.ProfileName, group, request.Name, parameters, nil); err != nil {
			return armcdn.AFDOrigin{}, false, fmt.Errorf("begin private AFD origin update: %w", err)
		}
		return armcdn.AFDOrigin{}, true, ctx.Err()
	}
	return actual.AFDOrigin, false, nil
}

func afdProvisioningReady(state *armcdn.AfdProvisioningState) (bool, error) {
	switch state := ptr.Deref(state, ""); state {
	case armcdn.AfdProvisioningStateCreating, armcdn.AfdProvisioningStateUpdating, armcdn.AfdProvisioningStateDeleting:
		return false, nil
	case armcdn.AfdProvisioningStateSucceeded:
		return true, nil
	default:
		return false, fmt.Errorf("AFD resource has failed or unknown provisioning state %q", state)
	}
}

func validateAFDDeploymentState(state *armcdn.DeploymentStatus) error {
	switch state := ptr.Deref(state, ""); state {
	case armcdn.DeploymentStatusNotStarted, armcdn.DeploymentStatusInProgress, armcdn.DeploymentStatusSucceeded:
		return nil
	default:
		return fmt.Errorf("AFD resource has failed or unknown deployment state %q", state)
	}
}
