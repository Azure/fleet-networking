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
	"k8s.io/utils/ptr"

	"go.goms.io/fleet-networking/pkg/common/azureerrors"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
)

// RootProvisioningState describes staging progress, not ingress readiness.
type RootProvisioningState string

// These states require fresh GET observations after any submitted operation.
const (
	ProfileProvisioningPending  RootProvisioningState = "ProfilePending"
	EndpointProvisioningPending RootProvisioningState = "EndpointPending"
	DisabledRootsObserved       RootProvisioningState = "DisabledRootsObserved"
)

// RootProvisioningResult never describes backend/route/approval convergence.
// EndpointHostName is observed from a disabled endpoint eligible for staging;
// initial bootstrap does not establish deployed-edge readiness.
type RootProvisioningResult struct {
	State            RootProvisioningState
	EndpointHostName string
	Diagnostic       string
}

// RootProvisioner stages owned Premium profiles and disabled endpoints.
// It is not wired into the manager. Callers must persist Gateway cleanup ownership
// and satisfy platform and concurrent-writer gates before enabling production use.
type RootProvisioner struct {
	scope     ResourceScope
	profiles  *armcdn.ProfilesClient
	endpoints *armcdn.AFDEndpointsClient
}

// NewRootProvisioner constructs scope-bound SDK clients without Azure requests.
func NewRootProvisioner(scope ResourceScope, credential azcore.TokenCredential, options *arm.ClientOptions) (*RootProvisioner, error) {
	profiles, err := armcdn.NewProfilesClient(scope.SubscriptionID, credential, options)
	if err != nil {
		return nil, fmt.Errorf("initialize AFD provisioning profiles client: %w", err)
	}
	endpoints, err := armcdn.NewAFDEndpointsClient(scope.SubscriptionID, credential, options)
	if err != nil {
		return nil, fmt.Errorf("initialize AFD endpoints client: %w", err)
	}
	return &RootProvisioner{scope: scope, profiles: profiles, endpoints: endpoints}, nil
}

// EnsureDisabledRoots validates complete supported intent before any ARM request.
// It submits at most one write under a shared deadline and never polls an LRO.
// Child access requires an observed owned, Active/Succeeded Premium/global root.
// A prior GET is not an atomic exclusion of concurrent external ARM writers.
func (p *RootProvisioner) EnsureDisabledRoots(ctx context.Context, owner GatewayOwner,
	model gatewaymodel.GlobalGateway) (RootProvisioningResult, error) {
	if err := ctx.Err(); err != nil {
		return RootProvisioningResult{}, err
	}
	graph, err := BuildHTTPPreviewRequests(p.scope, owner, model)
	if err != nil {
		return RootProvisioningResult{}, err
	}
	if p.profiles == nil || p.endpoints == nil {
		return RootProvisioningResult{}, fmt.Errorf("AFD root provisioning clients are not initialized")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ready, diagnostic, err := p.ensureProfile(ctx, owner, graph.Roots)
	if err != nil {
		return RootProvisioningResult{}, err
	}
	if !ready {
		return RootProvisioningResult{State: ProfileProvisioningPending, Diagnostic: diagnostic}, nil
	}
	return p.ensureDisabledEndpoint(ctx, graph.Roots)
}

func (p *RootProvisioner) ensureProfile(ctx context.Context, owner GatewayOwner, roots RootRequests) (bool, string, error) {
	if err := ctx.Err(); err != nil {
		return false, "", err
	}
	observed, err := p.profiles.Get(ctx, p.scope.ResourceGroup, roots.ProfileName, nil)
	if err := ctx.Err(); err != nil {
		return false, "", err
	}
	if azureerrors.IsNotFound(err) {
		if _, err := p.profiles.BeginCreate(ctx, p.scope.ResourceGroup, roots.ProfileName, roots.Profile, nil); err != nil {
			return false, "", fmt.Errorf("begin owned AFD profile creation: %w", err)
		}
		// Even a synchronously completed submission requires a fresh ownership/readiness GET.
		return false, fmt.Sprintf("profile %s creation submitted; awaiting observation", roots.ProfileName), ctx.Err()
	}
	if err != nil {
		return false, "", fmt.Errorf("observe AFD profile before provisioning: %w", err)
	}
	if err := VerifyProfileOwnership(p.scope, owner, observed.Profile); err != nil {
		return false, "", err
	}
	ready, err := profileReadyForChildren(observed.Profile)
	if err != nil {
		return false, "", err
	}
	return ready, fmt.Sprintf("profile %s: provisioning=%s; resource=%s", roots.ProfileName,
		ptr.Deref(observed.Properties.ProvisioningState, ""), ptr.Deref(observed.Properties.ResourceState, "")), nil
}

func profileReadyForChildren(profile armcdn.Profile) (bool, error) {
	if profile.SKU == nil || ptr.Deref(profile.SKU.Name, "") != armcdn.SKUNamePremiumAzureFrontDoor ||
		!strings.EqualFold(ptr.Deref(profile.Location, ""), "global") {
		return false, fmt.Errorf("owned AFD profile must be Premium/global; SKU/location drift is not automatically migrated")
	}
	if profile.Properties == nil {
		return false, fmt.Errorf("owned AFD profile has no provisioning state")
	}
	switch state := ptr.Deref(profile.Properties.ProvisioningState, ""); state {
	case armcdn.ProfileProvisioningStateCreating, armcdn.ProfileProvisioningStateUpdating, armcdn.ProfileProvisioningStateDeleting:
		return false, nil
	case armcdn.ProfileProvisioningStateSucceeded:
	default:
		return false, fmt.Errorf("owned AFD profile has failed or unknown provisioning state %q", state)
	}
	switch state := ptr.Deref(profile.Properties.ResourceState, ""); state {
	case armcdn.ProfileResourceStateActive:
		return true, nil
	case armcdn.ProfileResourceStateCreating, armcdn.ProfileResourceStateDeleting:
		return false, nil
	default:
		return false, fmt.Errorf("owned AFD profile has unsupported resource state %q", state)
	}
}

func (p *RootProvisioner) ensureDisabledEndpoint(ctx context.Context, roots RootRequests) (RootProvisioningResult, error) {
	if err := ctx.Err(); err != nil {
		return RootProvisioningResult{}, err
	}
	pending := RootProvisioningResult{State: EndpointProvisioningPending,
		Diagnostic: fmt.Sprintf("endpoint %s creation submitted; awaiting observation", roots.EndpointName)}
	observed, err := p.endpoints.Get(ctx, p.scope.ResourceGroup, roots.ProfileName, roots.EndpointName, nil)
	if err := ctx.Err(); err != nil {
		return RootProvisioningResult{}, err
	}
	if azureerrors.IsNotFound(err) {
		if _, err := p.endpoints.BeginCreate(ctx, p.scope.ResourceGroup, roots.ProfileName, roots.EndpointName, roots.Endpoint, nil); err != nil {
			return RootProvisioningResult{}, fmt.Errorf("begin disabled AFD endpoint creation: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return RootProvisioningResult{}, err
		}
		return pending, nil
	}
	if err != nil {
		return RootProvisioningResult{}, fmt.Errorf("observe owned AFD endpoint: %w", err)
	}
	if err := verifyEndpointIdentity(roots, observed.AFDEndpoint); err != nil {
		return RootProvisioningResult{}, err
	}
	pending.Diagnostic = endpointProgress(observed.AFDEndpoint)
	properties := observed.Properties
	switch state := ptr.Deref(properties.ProvisioningState, ""); state {
	case armcdn.AfdProvisioningStateCreating, armcdn.AfdProvisioningStateUpdating, armcdn.AfdProvisioningStateDeleting:
		return pending, nil
	case armcdn.AfdProvisioningStateSucceeded:
	default:
		return RootProvisioningResult{}, fmt.Errorf("owned AFD endpoint has failed or unknown provisioning state %q", state)
	}
	switch state := ptr.Deref(properties.EnabledState, ""); state {
	case armcdn.EnabledStateEnabled:
		if _, err := p.endpoints.BeginUpdate(ctx, p.scope.ResourceGroup, roots.ProfileName, roots.EndpointName,
			armcdn.AFDEndpointUpdateParameters{Properties: &armcdn.AFDEndpointPropertiesUpdateParameters{
				EnabledState: ptr.To(armcdn.EnabledStateDisabled),
			}}, nil); err != nil {
			return RootProvisioningResult{}, fmt.Errorf("disable owned AFD endpoint for staging: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return RootProvisioningResult{}, err
		}
		return pending, nil
	case armcdn.EnabledStateDisabled:
		return observedDisabledEndpoint(observed.AFDEndpoint)
	default:
		return RootProvisioningResult{}, fmt.Errorf("owned AFD endpoint has unknown enabled state %q", state)
	}
}

func verifyEndpointIdentity(roots RootRequests, endpoint armcdn.AFDEndpoint) error {
	expectedID := roots.ProfileID + "/afdEndpoints/" + roots.EndpointName
	if endpoint.ID == nil || !strings.EqualFold(*endpoint.ID, expectedID) ||
		(endpoint.Name != nil && !strings.EqualFold(*endpoint.Name, roots.EndpointName)) {
		return fmt.Errorf("observed AFD endpoint does not match its expected owned ARM identity")
	}
	if !strings.EqualFold(ptr.Deref(endpoint.Location, ""), "global") || endpoint.Properties == nil {
		return fmt.Errorf("observed AFD endpoint requires global location and provisioning properties")
	}
	if parent := endpoint.Properties.ProfileName; parent != nil && !strings.EqualFold(*parent, roots.ProfileName) {
		return fmt.Errorf("observed AFD endpoint contradicts its expected owned profile")
	}
	return nil
}

func observedDisabledEndpoint(endpoint armcdn.AFDEndpoint) (RootProvisioningResult, error) {
	ready, err := endpointReadyForStaging(endpoint)
	if err != nil {
		return RootProvisioningResult{}, err
	}
	if !ready {
		return RootProvisioningResult{State: EndpointProvisioningPending, Diagnostic: endpointProgress(endpoint)}, nil
	}
	return RootProvisioningResult{State: DisabledRootsObserved, EndpointHostName: *endpoint.Properties.HostName,
		Diagnostic: endpointProgress(endpoint)}, nil
}

func endpointProgress(endpoint armcdn.AFDEndpoint) string {
	return fmt.Sprintf("endpoint %s: provisioning=%s; deployment=%s; enabled=%s",
		ptr.Deref(endpoint.Name, ""), ptr.Deref(endpoint.Properties.ProvisioningState, ""),
		ptr.Deref(endpoint.Properties.DeploymentStatus, ""), ptr.Deref(endpoint.Properties.EnabledState, ""))
}

func observedEndpointDeployment(properties *armcdn.AFDEndpointProperties) (string, bool, error) {
	switch state := ptr.Deref(properties.DeploymentStatus, ""); state {
	case armcdn.DeploymentStatusInProgress, armcdn.DeploymentStatusNotStarted:
		return "", false, nil
	case armcdn.DeploymentStatusSucceeded:
	default:
		return "", false, fmt.Errorf("owned AFD endpoint has failed or unknown deployment state %q", state)
	}
	host, err := observedEndpointHostName(properties)
	return host, err == nil, err
}

func observedEndpointHostName(properties *armcdn.AFDEndpointProperties) (string, error) {
	host := ptr.Deref(properties.HostName, "")
	if host == "" || strings.TrimSpace(host) != host {
		return "", fmt.Errorf("observed AFD endpoint has no usable Azure hostname")
	}
	return host, nil
}
