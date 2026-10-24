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

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	"k8s.io/utils/ptr"

	"go.goms.io/fleet-networking/pkg/common/azureerrors"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
)

// WithdrawHTTPPreview retires a verified private graph when no effective backend
// remains. Callers revalidate withdrawal intent and finalizer ownership first.
// Retaining Disabled roots allows re-export without changing the public hostname.
func (p *RouteProvisioner) WithdrawHTTPPreview(ctx context.Context, owner GatewayOwner) (EndpointSuspensionResult, error) {
	if err := p.validateGraphClients(); err != nil {
		return EndpointSuspensionResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	result, err := p.SuspendEndpoint(ctx, owner)
	if err != nil || result.State == EndpointSuspensionPending {
		return result, err
	}
	roots, err := BuildRootRequests(p.backends.roots.scope, owner)
	if err != nil {
		return EndpointSuspensionResult{}, err
	}
	_, endpoint, found, err := p.backends.observeRoots(ctx, owner, roots)
	if err != nil {
		return EndpointSuspensionResult{}, err
	}
	if !found {
		return p.observeWithdrawalAbsence(ctx, owner, roots, result)
	}
	if ptr.Deref(endpoint.Properties.EnabledState, "") != armcdn.EnabledStateDisabled {
		return EndpointSuspensionResult{}, fmt.Errorf("owned endpoint changed before private graph withdrawal")
	}
	graph := HTTPPreviewRequests{Roots: roots, RouteName: resourceName("fleet-route", owner.UID)}
	bootstrap, err := bootstrapEndpoint(endpoint)
	if err != nil {
		return EndpointSuspensionResult{}, err
	}
	if err := p.verifyEndpointInventory(ctx, graph, armcdn.EnabledStateDisabled, bootstrap); err != nil {
		return EndpointSuspensionResult{}, err
	}
	route, group, stale, err := p.observeWithdrawalInventory(ctx, graph)
	if err != nil {
		return EndpointSuspensionResult{}, err
	}
	if err := p.verifyUnsupportedInventory(ctx, graph); err != nil {
		return EndpointSuspensionResult{}, err
	}
	pending := EndpointSuspensionResult{State: EndpointSuspensionPending}
	if route != nil {
		actual, err := p.routes.Get(ctx, p.backends.roots.scope.ResourceGroup, roots.ProfileName, roots.EndpointName, graph.RouteName, nil)
		if azureerrors.IsNotFound(err) {
			return pending, ctx.Err()
		}
		if err != nil {
			return EndpointSuspensionResult{}, fmt.Errorf("reobserve owned route before withdrawal: %w", err)
		}
		if err := verifyWithdrawalRoute(graph, actual.Route); err != nil {
			return EndpointSuspensionResult{}, err
		}
		if !matchesHTTPPreviewRoute(route.Properties, actual.Properties) ||
			ptr.Deref(route.Properties.EnabledState, "") != ptr.Deref(actual.Properties.EnabledState, "") {
			return EndpointSuspensionResult{}, fmt.Errorf("owned route changed before withdrawal")
		}
		_, endpoint, found, err := p.backends.observeRoots(ctx, owner, roots)
		if err != nil {
			return EndpointSuspensionResult{}, err
		}
		if !found || ptr.Deref(endpoint.Properties.EnabledState, "") != armcdn.EnabledStateDisabled {
			return EndpointSuspensionResult{}, fmt.Errorf("owned roots changed before route withdrawal")
		}
		ready, err := endpointReadyForHTTP(endpoint)
		if err != nil {
			return EndpointSuspensionResult{}, err
		}
		if !ready {
			return EndpointSuspensionResult{}, fmt.Errorf("owned roots are not configured before route withdrawal")
		}
		// Remove the association before the final origin; never delete the roots.
		if _, err := p.routes.BeginDelete(ctx, p.backends.roots.scope.ResourceGroup, roots.ProfileName, roots.EndpointName, graph.RouteName, nil); err != nil &&
			!azureerrors.IsNotFound(err) {
			return EndpointSuspensionResult{}, fmt.Errorf("delete owned withdrawn route: %w", err)
		}
		return pending, ctx.Err()
	}
	if len(stale) != 0 {
		graph.Backends.OriginGroups = []OriginGroupRequest{group}
		if err := p.pruneStaleOrigin(ctx, owner, graph, stale); err != nil {
			return EndpointSuspensionResult{}, err
		}
		return pending, ctx.Err()
	}
	return EndpointSuspensionResult{State: EndpointSuspended}, ctx.Err()
}

func (p *RouteProvisioner) observeWithdrawalAbsence(ctx context.Context, owner GatewayOwner, roots RootRequests,
	result EndpointSuspensionResult) (EndpointSuspensionResult, error) {
	if result.State == EndpointAbsent {
		// An absent endpoint alone does not establish absence of profile origins.
		profile, err := p.backends.roots.profiles.Get(ctx, p.backends.roots.scope.ResourceGroup, roots.ProfileName, nil)
		if azureerrors.IsNotFound(err) {
			return result, ctx.Err()
		}
		if err != nil {
			return EndpointSuspensionResult{}, fmt.Errorf("verify withdrawn profile absence: %w", err)
		}
		if err := VerifyProfileOwnership(p.backends.roots.scope, owner, profile.Profile); err != nil {
			return EndpointSuspensionResult{}, err
		}
	}
	return EndpointSuspensionResult{}, fmt.Errorf("owned endpoint absent; remaining profile inventory cannot be withdrawn safely")
}

func (p *RouteProvisioner) observeWithdrawalInventory(ctx context.Context, graph HTTPPreviewRequests) (*armcdn.Route, OriginGroupRequest, []armcdn.AFDOrigin, error) {
	scope, roots := p.backends.roots.scope, graph.Roots
	var route *armcdn.Route
	routes := p.routes.NewListByEndpointPager(scope.ResourceGroup, roots.ProfileName, roots.EndpointName, nil)
	for routes.More() {
		page, err := routes.NextPage(ctx)
		if err != nil {
			return nil, OriginGroupRequest{}, nil, fmt.Errorf("list withdrawn routes: %w", err)
		}
		if err := requireExplicitInventoryPage("withdrawn routes", page.Value != nil); err != nil {
			return nil, OriginGroupRequest{}, nil, err
		}
		for _, actual := range page.Value {
			if actual == nil || route != nil {
				return nil, OriginGroupRequest{}, nil, fmt.Errorf("withdrawn route inventory is not a single supported resource")
			}
			if err := verifyWithdrawalRoute(graph, *actual); err != nil {
				return nil, OriginGroupRequest{}, nil, err
			}
			route = actual
		}
	}
	var group OriginGroupRequest
	groups := p.backends.groups.NewListByProfilePager(scope.ResourceGroup, roots.ProfileName, nil)
	for groups.More() {
		page, err := groups.NextPage(ctx)
		if err != nil {
			return nil, group, nil, fmt.Errorf("list withdrawn origin groups: %w", err)
		}
		if err := requireExplicitInventoryPage("withdrawn origin groups", page.Value != nil); err != nil {
			return nil, group, nil, err
		}
		for _, actual := range page.Value {
			if actual == nil || group.Name != "" || !fleetGeneratedName(ptr.Deref(actual.Name, ""), "fleet-og-") {
				return nil, group, nil, fmt.Errorf("withdrawn origin-group inventory is not a supported Fleet resource")
			}
			group = OriginGroupRequest{Name: *actual.Name, Parameters: armcdn.AFDOriginGroup{
				Properties: privateHTTPGroupProperties(gatewaymodel.HTTPPreviewHealthProbe()),
			}}
			if err := verifyGroupIdentity(roots, group.Name, *actual); err != nil {
				return nil, group, nil, err
			}
			if !matchesOriginGroupPolicy(group.Parameters.Properties, actual.Properties) {
				return nil, group, nil, fmt.Errorf("withdrawn origin group differs from the supported private policy")
			}
			if err := verifyInventoryDeployment(actual.Properties.ProvisioningState, actual.Properties.DeploymentStatus, false); err != nil {
				return nil, group, nil, err
			}
		}
	}
	if route != nil && (group.Name == "" ||
		!strings.EqualFold(ptr.Deref(route.Properties.OriginGroup.ID, ""), roots.ProfileID+"/originGroups/"+group.Name)) {
		return nil, group, nil, fmt.Errorf("withdrawn route does not reference its exact owned origin group")
	}
	if group.Name == "" {
		return route, group, nil, ctx.Err()
	}
	inventory := newOriginInventory(roots, group)
	origins := p.backends.origins.NewListByOriginGroupPager(scope.ResourceGroup, roots.ProfileName, group.Name, nil)
	for origins.More() {
		page, err := origins.NextPage(ctx)
		if err != nil {
			return nil, group, nil, fmt.Errorf("list withdrawn origins: %w", err)
		}
		if err := requireExplicitInventoryPage("withdrawn origins", page.Value != nil); err != nil {
			return nil, group, nil, err
		}
		for _, origin := range page.Value {
			if err := inventory.observe(origin); err != nil {
				return nil, group, nil, err
			}
		}
	}
	return route, group, inventory.stale, ctx.Err()
}

func verifyWithdrawalRoute(graph HTTPPreviewRequests, route armcdn.Route) error {
	if err := verifyRouteIdentity(graph, route); err != nil {
		return err
	}
	if route.Properties.OriginGroup == nil || route.Properties.OriginGroup.ID == nil {
		return fmt.Errorf("withdrawn route lacks its private origin-group reference")
	}
	expected := &armcdn.RouteProperties{
		OriginGroup:        route.Properties.OriginGroup,
		PatternsToMatch:    []*string{ptr.To(httpPreviewPattern)},
		SupportedProtocols: []*armcdn.AFDEndpointProtocols{ptr.To(armcdn.AFDEndpointProtocolsHTTP)},
		ForwardingProtocol: ptr.To(armcdn.ForwardingProtocolHTTPOnly),
		HTTPSRedirect:      ptr.To(armcdn.HTTPSRedirectDisabled), LinkToDefaultDomain: ptr.To(armcdn.LinkToDefaultDomainEnabled),
	}
	state := ptr.Deref(route.Properties.EnabledState, "")
	if !matchesHTTPPreviewRoute(expected, route.Properties) ||
		(state != armcdn.EnabledStateEnabled && state != armcdn.EnabledStateDisabled) {
		return fmt.Errorf("withdrawn route differs from the supported private HTTP subset")
	}
	return verifyInventoryDeployment(route.Properties.ProvisioningState, route.Properties.DeploymentStatus, false)
}
