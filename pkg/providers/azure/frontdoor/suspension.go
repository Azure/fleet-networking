/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package frontdoor

import (
	"context"
	"fmt"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	"k8s.io/utils/ptr"

	"go.goms.io/fleet-networking/pkg/common/azureerrors"
)

// EndpointSuspensionState distinguishes a submission from observed withdrawal.
type EndpointSuspensionState string

// Suspension is observed only after disabled deployment or resource absence.
const (
	EndpointSuspensionPending EndpointSuspensionState = "SuspensionPending"
	EndpointSuspended         EndpointSuspensionState = "Suspended"
	EndpointAbsent            EndpointSuspensionState = "Absent"
)

// EndpointSuspensionResult does not prove traffic enforcement or remove resources.
type EndpointSuspensionResult struct {
	State EndpointSuspensionState
}

// SuspendEndpoint observes an owned profile/endpoint and submits at most one
// disable-only PATCH under one deadline. It never creates resources or needs a
// backend model. Callers must revalidate existing ownership and withdrawal intent.
func (p *RouteProvisioner) SuspendEndpoint(ctx context.Context, owner GatewayOwner) (EndpointSuspensionResult, error) {
	if err := ctx.Err(); err != nil {
		return EndpointSuspensionResult{}, err
	}
	if p == nil || p.backends == nil || p.backends.roots == nil ||
		p.backends.roots.profiles == nil || p.backends.roots.endpoints == nil {
		return EndpointSuspensionResult{}, fmt.Errorf("AFD root clients are required for endpoint suspension")
	}
	root := p.backends.roots
	requests, err := BuildRootRequests(root.scope, owner)
	if err != nil {
		return EndpointSuspensionResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	profile, err := root.profiles.Get(ctx, root.scope.ResourceGroup, requests.ProfileName, nil)
	if err := ctx.Err(); err != nil {
		return EndpointSuspensionResult{}, err
	}
	if azureerrors.IsNotFound(err) {
		return EndpointSuspensionResult{State: EndpointAbsent}, nil
	}
	if err != nil {
		return EndpointSuspensionResult{}, fmt.Errorf("observe AFD profile before suspension: %w", err)
	}
	// Ownership, not profile configuration readiness, authorizes child suspension.
	if err := VerifyProfileOwnership(root.scope, owner, profile.Profile); err != nil {
		return EndpointSuspensionResult{}, err
	}
	endpoint, err := root.endpoints.Get(ctx, root.scope.ResourceGroup, requests.ProfileName, requests.EndpointName, nil)
	if err := ctx.Err(); err != nil {
		return EndpointSuspensionResult{}, err
	}
	if azureerrors.IsNotFound(err) {
		return EndpointSuspensionResult{State: EndpointAbsent}, nil
	}
	if err != nil {
		return EndpointSuspensionResult{}, fmt.Errorf("observe AFD endpoint before suspension: %w", err)
	}
	if err := verifyEndpointIdentity(requests, endpoint.AFDEndpoint); err != nil {
		return EndpointSuspensionResult{}, err
	}
	ready, err := afdProvisioningReady(endpoint.Properties.ProvisioningState)
	if err != nil {
		return EndpointSuspensionResult{}, err
	}
	pending := EndpointSuspensionResult{State: EndpointSuspensionPending}
	if !ready {
		return pending, nil
	}
	switch state := ptr.Deref(endpoint.Properties.EnabledState, ""); state {
	case armcdn.EnabledStateEnabled:
		parameters := armcdn.AFDEndpointUpdateParameters{Properties: &armcdn.AFDEndpointPropertiesUpdateParameters{
			EnabledState: ptr.To(armcdn.EnabledStateDisabled),
		}}
		if err := ctx.Err(); err != nil {
			return EndpointSuspensionResult{}, err
		}
		if _, err := root.endpoints.BeginUpdate(ctx, root.scope.ResourceGroup, requests.ProfileName, requests.EndpointName, parameters, nil); err != nil {
			return EndpointSuspensionResult{}, fmt.Errorf("suspend owned AFD endpoint: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return EndpointSuspensionResult{}, err
		}
		return pending, nil
	case armcdn.EnabledStateDisabled:
		if err := validateAFDDeploymentState(endpoint.Properties.DeploymentStatus); err != nil {
			return EndpointSuspensionResult{}, err
		}
		if ptr.Deref(endpoint.Properties.DeploymentStatus, "") == armcdn.DeploymentStatusSucceeded {
			return EndpointSuspensionResult{State: EndpointSuspended}, nil
		}
		return pending, nil
	default:
		return EndpointSuspensionResult{}, fmt.Errorf("owned AFD endpoint has unknown enabled state %q", state)
	}
}
