/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package frontdoor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	"k8s.io/utils/ptr"

	"go.goms.io/fleet-networking/pkg/common/azureerrors"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
)

// HTTPPreviewProvisioningState describes observed graph progress, not HTTP proof.
type HTTPPreviewProvisioningState string

// Activation requires separate route and endpoint observations. Programmed
// additionally requires deployed convergence of the complete enabled graph.
const (
	HTTPPreviewStaging          HTTPPreviewProvisioningState = "Staging"
	HTTPPreviewPending          HTTPPreviewProvisioningState = "Pending"
	HTTPPreviewRouteEnabling    HTTPPreviewProvisioningState = "RouteEnabling"
	HTTPPreviewEndpointEnabling HTTPPreviewProvisioningState = "EndpointEnabling"
	HTTPPreviewSuspending       HTTPPreviewProvisioningState = "Suspending"
	HTTPPreviewProgrammed       HTTPPreviewProvisioningState = "Programmed"
)

// HTTPPreviewProvisioningResult publishes a hostname only for a verified enabled
// graph. Programmed is ARM convergence, not measured application health.
type HTTPPreviewProvisioningResult struct {
	State            HTTPPreviewProvisioningState
	EndpointHostName string
	Staging          DisabledGraphStagingResult
	Suspension       EndpointSuspensionResult
	Diagnostic       string
}

// graphMismatchError identifies affirmative configuration/approval evidence.
// SDK, cancellation and ambiguous inventory errors must never authorize suspension.
type graphMismatchError struct{ message string }

func (e *graphMismatchError) Error() string { return e.message }

func graphMismatch(format string, args ...interface{}) error {
	return &graphMismatchError{message: fmt.Sprintf(format, args...)}
}

// ReconcileHTTPPreview maintains the gated private HTTP graph with at most one
// write and one 30-second budget, without LRO polling. Callers must first persist
// cleanup ownership/finalizers and revalidate current authorization. The enabled
// manager injects this lifecycle; live platform/traffic proof remains separate.
// Sequential ARM reads do not exclude concurrent writers; the owned profile must
// be exclusively controller-managed.
func (p *RouteProvisioner) ReconcileHTTPPreview(ctx context.Context, owner GatewayOwner,
	model gatewaymodel.GlobalGateway) (HTTPPreviewProvisioningResult, error) {
	if err := ctx.Err(); err != nil {
		return HTTPPreviewProvisioningResult{}, err
	}
	if err := p.validateGraphClients(); err != nil {
		return HTTPPreviewProvisioningResult{}, err
	}
	graph, err := BuildHTTPPreviewRequests(p.backends.roots.scope, owner, model)
	if err != nil {
		return HTTPPreviewProvisioningResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, endpoint, found, err := p.backends.observeRoots(ctx, owner, graph.Roots)
	if err != nil {
		return HTTPPreviewProvisioningResult{}, err
	}
	if !found {
		return p.stageHTTPPreview(ctx, owner, model)
	}
	endpointState := ptr.Deref(endpoint.Properties.EnabledState, "")
	converged, err := endpointReadyForHTTP(endpoint)
	if err != nil {
		return HTTPPreviewProvisioningResult{}, err
	}
	if !converged {
		return HTTPPreviewProvisioningResult{State: HTTPPreviewPending, Diagnostic: endpointProgress(endpoint)}, nil
	}
	scope, roots := p.backends.roots.scope, graph.Roots
	route, err := p.routes.Get(ctx, scope.ResourceGroup, roots.ProfileName, roots.EndpointName, graph.RouteName, nil)
	if ctx.Err() != nil {
		return HTTPPreviewProvisioningResult{}, ctx.Err()
	}
	if azureerrors.IsNotFound(err) {
		if endpointState == armcdn.EnabledStateEnabled {
			return p.suspendHTTPPreview(ctx, owner, "owned HTTP route is absent")
		}
		return p.stageHTTPPreview(ctx, owner, model)
	}
	if err != nil {
		return HTTPPreviewProvisioningResult{}, fmt.Errorf("observe HTTP activation route: %w", err)
	}
	if err := verifyRouteIdentity(graph, route.Route); err != nil {
		return HTTPPreviewProvisioningResult{}, err
	}
	routeState := ptr.Deref(route.Properties.EnabledState, "")
	if routeState != armcdn.EnabledStateEnabled && routeState != armcdn.EnabledStateDisabled {
		return HTTPPreviewProvisioningResult{}, fmt.Errorf("HTTP route has unknown enabled state %q", routeState)
	}
	if endpointState == armcdn.EnabledStateEnabled && routeState == armcdn.EnabledStateDisabled {
		return p.suspendHTTPPreview(ctx, owner, "owned HTTP route is disabled")
	}
	if endpointState == armcdn.EnabledStateDisabled && routeState == armcdn.EnabledStateDisabled {
		return p.activateStagedHTTPRoute(ctx, owner, model, graph)
	}
	_, err = p.observeHTTPInventory(ctx, owner, model, graph, endpointState, armcdn.EnabledStateEnabled)
	if err != nil {
		var mismatch *graphMismatchError
		if !errors.As(err, &mismatch) {
			return HTTPPreviewProvisioningResult{}, err
		}
		if endpointState == armcdn.EnabledStateEnabled {
			return p.suspendHTTPPreview(ctx, owner, err.Error())
		}
		staged, stageErr := p.stageHTTPPreview(ctx, owner, model)
		if stageErr != nil {
			return HTTPPreviewProvisioningResult{}, fmt.Errorf("restage HTTP graph after %s: %w", err.Error(), stageErr)
		}
		staged.Diagnostic = err.Error()
		return staged, nil
	}
	if endpointState == armcdn.EnabledStateDisabled {
		if err := ctx.Err(); err != nil {
			return HTTPPreviewProvisioningResult{}, err
		}
		parameters, err := endpointActivationParameters(endpoint)
		if err != nil {
			return HTTPPreviewProvisioningResult{}, err
		}
		if _, err := p.backends.roots.endpoints.BeginUpdate(ctx, scope.ResourceGroup, roots.ProfileName, roots.EndpointName, parameters, nil); err != nil {
			return HTTPPreviewProvisioningResult{}, fmt.Errorf("enable verified private HTTP endpoint: %w", err)
		}
		return httpPreviewProgress(ctx, HTTPPreviewEndpointEnabling)
	}
	if err := ctx.Err(); err != nil {
		return HTTPPreviewProvisioningResult{}, err
	}
	return HTTPPreviewProvisioningResult{State: HTTPPreviewProgrammed,
		EndpointHostName: *endpoint.Properties.HostName}, nil
}

// VerifyEnabledHTTPPreview reads the complete owned enabled graph without
// provisioning, suspension or approval writes. Its hostname proves ARM
// configuration convergence only; callers must independently prove HTTP delivery.
func (p *RouteProvisioner) VerifyEnabledHTTPPreview(ctx context.Context, owner GatewayOwner,
	model gatewaymodel.GlobalGateway) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := p.validateGraphClients(); err != nil {
		return "", err
	}
	graph, err := BuildHTTPPreviewRequests(p.backends.roots.scope, owner, model)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, endpoint, found, err := p.backends.observeRoots(ctx, owner, graph.Roots)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("owned AFD roots are not yet available for enabled-graph verification")
	}
	ready, err := endpointConverged(endpoint.Properties, armcdn.EnabledStateEnabled)
	if err != nil {
		return "", err
	}
	if !ready {
		return "", fmt.Errorf("owned AFD endpoint is not enabled and converged")
	}
	if _, err := p.observeHTTPInventory(ctx, owner, model, graph, armcdn.EnabledStateEnabled, armcdn.EnabledStateEnabled); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return *endpoint.Properties.HostName, nil
}

func (p *RouteProvisioner) stageHTTPPreview(ctx context.Context, owner GatewayOwner,
	model gatewaymodel.GlobalGateway) (HTTPPreviewProvisioningResult, error) {
	staged, err := p.StageDisabledGraph(ctx, owner, model)
	if err != nil {
		return HTTPPreviewProvisioningResult{}, err
	}
	diagnostic := fmt.Sprintf("roots=%s", staged.Roots.State)
	if staged.Roots.Diagnostic != "" {
		diagnostic += "; " + staged.Roots.Diagnostic
	}
	if staged.Route.State != "" {
		diagnostic += fmt.Sprintf("; route=%s", staged.Route.State)
	}
	if staged.Route.Backends.State != "" {
		diagnostic += fmt.Sprintf("; backends=%s", staged.Route.Backends.State)
	}
	return HTTPPreviewProvisioningResult{State: HTTPPreviewStaging, Staging: staged, Diagnostic: diagnostic}, nil
}

func (p *RouteProvisioner) activateStagedHTTPRoute(ctx context.Context, owner GatewayOwner,
	model gatewaymodel.GlobalGateway, graph HTTPPreviewRequests) (HTTPPreviewProvisioningResult, error) {
	staged, err := p.stageHTTPPreview(ctx, owner, model)
	if err != nil || !staged.Staging.InventoryVerified {
		return staged, err
	}
	if err := ctx.Err(); err != nil {
		return HTTPPreviewProvisioningResult{}, err
	}
	scope, roots := p.backends.roots.scope, graph.Roots
	parameters := armcdn.RouteUpdateParameters{Properties: &armcdn.RouteUpdatePropertiesParameters{
		EnabledState: ptr.To(armcdn.EnabledStateEnabled),
	}}
	if _, err := p.routes.BeginUpdate(ctx, scope.ResourceGroup, roots.ProfileName, roots.EndpointName, graph.RouteName, parameters, nil); err != nil {
		return HTTPPreviewProvisioningResult{}, fmt.Errorf("enable verified private HTTP route: %w", err)
	}
	return httpPreviewProgress(ctx, HTTPPreviewRouteEnabling)
}

func (p *RouteProvisioner) suspendHTTPPreview(ctx context.Context, owner GatewayOwner, diagnostic string) (HTTPPreviewProvisioningResult, error) {
	suspended, err := p.SuspendEndpoint(ctx, owner)
	if err != nil {
		return HTTPPreviewProvisioningResult{}, err
	}
	return HTTPPreviewProvisioningResult{State: HTTPPreviewSuspending, Suspension: suspended, Diagnostic: diagnostic}, nil
}

func httpPreviewProgress(ctx context.Context, state HTTPPreviewProvisioningState) (HTTPPreviewProvisioningResult, error) {
	if err := ctx.Err(); err != nil {
		return HTTPPreviewProvisioningResult{}, err
	}
	return HTTPPreviewProvisioningResult{State: state}, nil
}
