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

// RouteProvisioningState describes staging progress, never traffic readiness.
type RouteProvisioningState string

// Route staging waits for expected private backends and fresh disabled-route observations.
const (
	RouteBackendsPending  RouteProvisioningState = "BackendsPending"
	DisabledRoutePending  RouteProvisioningState = "DisabledRoutePending"
	DisabledRouteObserved RouteProvisioningState = "DisabledRouteObserved"
)

// RouteProvisioningResult does not establish enablement or stale-resource removal.
type RouteProvisioningResult struct {
	State    RouteProvisioningState
	Backends BackendProvisioningResult
}

// RouteProvisioner stages backend dependencies and one disabled HTTP root-path route.
// It is not wired into the manager and does not authorize production writes.
type RouteProvisioner struct {
	backends *BackendProvisioner
	routes   *armcdn.RoutesClient
	domains  *armcdn.AFDCustomDomainsClient
	ruleSets *armcdn.RuleSetsClient
	policies *armcdn.SecurityPoliciesClient
	secrets  *armcdn.SecretsClient
}

// NewRouteProvisioner constructs scope-bound SDK clients without Azure requests.
func NewRouteProvisioner(scope ResourceScope, credential azcore.TokenCredential, options *arm.ClientOptions) (*RouteProvisioner, error) {
	backends, err := NewBackendProvisioner(scope, credential, options)
	if err != nil {
		return nil, err
	}
	routes, err := armcdn.NewRoutesClient(scope.SubscriptionID, credential, options)
	if err != nil {
		return nil, fmt.Errorf("initialize AFD routes client: %w", err)
	}
	domains, err := armcdn.NewAFDCustomDomainsClient(scope.SubscriptionID, credential, options)
	if err != nil {
		return nil, fmt.Errorf("initialize AFD custom-domain inventory client: %w", err)
	}
	ruleSets, err := armcdn.NewRuleSetsClient(scope.SubscriptionID, credential, options)
	if err != nil {
		return nil, fmt.Errorf("initialize AFD rule-set inventory client: %w", err)
	}
	policies, err := armcdn.NewSecurityPoliciesClient(scope.SubscriptionID, credential, options)
	if err != nil {
		return nil, fmt.Errorf("initialize AFD security-policy inventory client: %w", err)
	}
	secrets, err := armcdn.NewSecretsClient(scope.SubscriptionID, credential, options)
	if err != nil {
		return nil, fmt.Errorf("initialize AFD secret inventory client: %w", err)
	}
	return &RouteProvisioner{backends: backends, routes: routes, domains: domains, ruleSets: ruleSets, policies: policies, secrets: secrets}, nil
}

// EnsureDisabledRoute shares one deadline and submits at most one backend or route write.
// Roots must already be staged; route access requires observed expected-backend convergence.
// Only an otherwise matching route can be disabled; other routing drift fails explicitly.
// Sequential observations do not exclude external writers or establish ingress readiness.
func (p *RouteProvisioner) EnsureDisabledRoute(ctx context.Context, owner GatewayOwner,
	model gatewaymodel.GlobalGateway) (RouteProvisioningResult, error) {
	if err := ctx.Err(); err != nil {
		return RouteProvisioningResult{}, err
	}
	if p.backends == nil || p.backends.roots == nil || p.routes == nil {
		return RouteProvisioningResult{}, fmt.Errorf("AFD route provisioning clients are not initialized")
	}
	graph, err := BuildHTTPPreviewRequests(p.backends.roots.scope, owner, model)
	if err != nil {
		return RouteProvisioningResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	backends, err := p.backends.EnsurePrivateBackends(ctx, owner, model)
	if err != nil {
		return RouteProvisioningResult{}, err
	}
	if backends.State != PrivateBackendsConverged {
		return RouteProvisioningResult{State: RouteBackendsPending, Backends: backends}, nil
	}
	state, err := p.ensureDisabledRoute(ctx, graph)
	if err != nil {
		return RouteProvisioningResult{}, err
	}
	return RouteProvisioningResult{State: state, Backends: backends}, nil
}

func (p *RouteProvisioner) ensureDisabledRoute(ctx context.Context, graph HTTPPreviewRequests) (RouteProvisioningState, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	scope := p.backends.roots.scope
	roots := graph.Roots
	actual, err := p.routes.Get(ctx, scope.ResourceGroup, roots.ProfileName, roots.EndpointName, graph.RouteName, nil)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if azureerrors.IsNotFound(err) {
		if _, err := p.routes.BeginCreate(ctx, scope.ResourceGroup, roots.ProfileName, roots.EndpointName, graph.RouteName, graph.Route, nil); err != nil {
			return "", fmt.Errorf("begin disabled AFD HTTP route creation: %w", err)
		}
		return DisabledRoutePending, ctx.Err()
	}
	if err != nil {
		return "", fmt.Errorf("observe owned AFD HTTP route: %w", err)
	}
	if err := verifyRouteIdentity(graph, actual.Route); err != nil {
		return "", err
	}
	ready, err := afdProvisioningReady(actual.Properties.ProvisioningState)
	if err != nil {
		return "", err
	}
	if !ready {
		return DisabledRoutePending, nil
	}
	if !matchesHTTPPreviewRoute(graph.Route.Properties, actual.Properties) {
		// Nil PATCH fields cannot be assumed to clear cached/protected routing state.
		return "", fmt.Errorf("owned AFD route configuration differs from the gated HTTP graph; automatic routing migration is not implemented")
	}
	switch state := ptr.Deref(actual.Properties.EnabledState, ""); state {
	case armcdn.EnabledStateEnabled:
		parameters := armcdn.RouteUpdateParameters{Properties: &armcdn.RouteUpdatePropertiesParameters{
			EnabledState: ptr.To(armcdn.EnabledStateDisabled),
		}}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if _, err := p.routes.BeginUpdate(ctx, scope.ResourceGroup, roots.ProfileName, roots.EndpointName, graph.RouteName, parameters, nil); err != nil {
			return "", fmt.Errorf("disable owned AFD HTTP route for staging: %w", err)
		}
		return DisabledRoutePending, ctx.Err()
	case armcdn.EnabledStateDisabled:
		if err := validateAFDDeploymentState(actual.Properties.DeploymentStatus); err != nil {
			return "", err
		}
		if ptr.Deref(actual.Properties.DeploymentStatus, "") == armcdn.DeploymentStatusSucceeded {
			return DisabledRouteObserved, nil
		}
		return DisabledRoutePending, nil
	default:
		return "", fmt.Errorf("owned AFD route has unknown enabled state %q", state)
	}
}

func verifyRouteIdentity(graph HTTPPreviewRequests, route armcdn.Route) error {
	expectedID := graph.Roots.ProfileID + "/afdEndpoints/" + graph.Roots.EndpointName + "/routes/" + graph.RouteName
	if route.ID == nil || !strings.EqualFold(*route.ID, expectedID) ||
		(route.Name != nil && !strings.EqualFold(*route.Name, graph.RouteName)) || route.Properties == nil {
		return fmt.Errorf("observed AFD route lacks its expected owned ARM identity/properties")
	}
	if parent := route.Properties.EndpointName; parent != nil && !strings.EqualFold(*parent, graph.Roots.EndpointName) {
		return fmt.Errorf("observed AFD route contradicts its expected owned endpoint")
	}
	return nil
}

func matchesHTTPPreviewRoute(desired, actual *armcdn.RouteProperties) bool {
	return actual.OriginGroup != nil &&
		strings.EqualFold(ptr.Deref(actual.OriginGroup.ID, ""), ptr.Deref(desired.OriginGroup.ID, "")) &&
		reflect.DeepEqual(actual.PatternsToMatch, desired.PatternsToMatch) &&
		reflect.DeepEqual(actual.SupportedProtocols, desired.SupportedProtocols) &&
		ptr.Deref(actual.ForwardingProtocol, "") == ptr.Deref(desired.ForwardingProtocol, "") &&
		ptr.Deref(actual.HTTPSRedirect, "") == ptr.Deref(desired.HTTPSRedirect, "") &&
		ptr.Deref(actual.LinkToDefaultDomain, "") == ptr.Deref(desired.LinkToDefaultDomain, "") &&
		actual.CacheConfiguration == nil && ptr.Deref(actual.OriginPath, "") == "" &&
		len(actual.RuleSets) == 0 && len(actual.CustomDomains) == 0
}
