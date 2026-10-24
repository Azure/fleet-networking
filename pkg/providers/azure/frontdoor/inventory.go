/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package frontdoor

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	"k8s.io/utils/ptr"

	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
)

// resourceInventory tracks full ARM identities, never names alone.
type resourceInventory map[string]bool

func newResourceInventory(ids ...string) resourceInventory {
	inventory := make(resourceInventory, len(ids))
	for _, id := range ids {
		inventory[strings.ToLower(id)] = false
	}
	return inventory
}

func (inventory resourceInventory) observe(id *string) error {
	if id == nil {
		return fmt.Errorf("AFD inventory resource lacks an ARM identity")
	}
	key := strings.ToLower(*id)
	seen, expected := inventory[key]
	if !expected {
		return graphMismatch("unexpected AFD inventory resource %q; automatic cleanup of this resource is not implemented", *id)
	}
	if seen {
		return fmt.Errorf("duplicate AFD inventory resource %q", *id)
	}
	inventory[key] = true
	return nil
}

func (inventory resourceInventory) complete() error {
	for id, seen := range inventory {
		if !seen {
			return graphMismatch("expected AFD resource %q is missing from complete inventory", id)
		}
	}
	return nil
}

func requireExplicitInventoryPage(collection string, explicit bool) error {
	// Continuation links do not prove an omitted/null page was empty.
	if !explicit {
		return fmt.Errorf("AFD %s inventory lacks an explicit resource array; absence is unconfirmed", collection)
	}
	return nil
}

func (p *RouteProvisioner) observeSupportedInventory(ctx context.Context, owner GatewayOwner,
	model gatewaymodel.GlobalGateway, graph HTTPPreviewRequests) ([]armcdn.AFDOrigin, error) {
	return p.observeHTTPInventory(ctx, owner, model, graph, armcdn.EnabledStateDisabled, armcdn.EnabledStateDisabled)
}

func (p *RouteProvisioner) observeHTTPInventory(ctx context.Context, owner GatewayOwner,
	model gatewaymodel.GlobalGateway, graph HTTPPreviewRequests,
	endpointState, routeState armcdn.EnabledState) ([]armcdn.AFDOrigin, error) {
	profile, endpoint, found, err := p.backends.observeRoots(ctx, owner, graph.Roots)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("owned AFD roots unavailable before inventory verification")
	}
	converged, err := endpointConverged(endpoint.Properties, endpointState)
	if err != nil {
		return nil, err
	}
	if !converged {
		return nil, fmt.Errorf("owned AFD endpoint changed before inventory verification")
	}
	// Later collection properties must not contradict earlier convergence.
	// Sequential reads still cannot exclude subsequent external changes.
	for _, verify := range []func(context.Context, HTTPPreviewRequests) error{
		func(ctx context.Context, graph HTTPPreviewRequests) error {
			return p.verifyEndpointInventory(ctx, graph, endpointState)
		},
		func(ctx context.Context, graph HTTPPreviewRequests) error {
			return p.verifyRouteInventory(ctx, graph, routeState)
		},
		p.verifyGroupInventory,
	} {
		if err := verify(ctx, graph); err != nil {
			return nil, err
		}
	}
	stale, err := p.observeOriginInventory(ctx, owner, model, profile, graph,
		endpointState == armcdn.EnabledStateEnabled || routeState == armcdn.EnabledStateEnabled)
	if err != nil {
		return nil, err
	}
	if err := p.verifyUnsupportedInventory(ctx, graph); err != nil {
		return nil, err
	}
	return stale, ctx.Err()
}

func (p *RouteProvisioner) verifyEndpointInventory(ctx context.Context, graph HTTPPreviewRequests, state armcdn.EnabledState) error {
	scope, roots := p.backends.roots.scope, graph.Roots
	inventory := newResourceInventory(roots.ProfileID + "/afdEndpoints/" + roots.EndpointName)
	pager := p.backends.roots.endpoints.NewListByProfilePager(scope.ResourceGroup, roots.ProfileName, nil)
	for pager.More() {
		if err := ctx.Err(); err != nil {
			return err
		}
		page, err := pager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("list AFD endpoint inventory: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := requireExplicitInventoryPage("endpoints", page.Value != nil); err != nil {
			return err
		}
		for _, endpoint := range page.Value {
			if endpoint == nil {
				return fmt.Errorf("AFD endpoint inventory contains a nil resource")
			}
			if err := inventory.observe(endpoint.ID); err != nil {
				return err
			}
			if err := verifyEndpointIdentity(roots, *endpoint); err != nil {
				return err
			}
			converged, err := endpointConverged(endpoint.Properties, state)
			if err != nil {
				return fmt.Errorf("listed AFD endpoint is not converged: %w", err)
			}
			if !converged {
				return fmt.Errorf("listed AFD endpoint changed during inventory verification")
			}
		}
	}
	return inventory.complete()
}

func (p *RouteProvisioner) verifyRouteInventory(ctx context.Context, graph HTTPPreviewRequests, state armcdn.EnabledState) error {
	scope, roots := p.backends.roots.scope, graph.Roots
	inventory := newResourceInventory(roots.ProfileID + "/afdEndpoints/" + roots.EndpointName + "/routes/" + graph.RouteName)
	pager := p.routes.NewListByEndpointPager(scope.ResourceGroup, roots.ProfileName, roots.EndpointName, nil)
	for pager.More() {
		if err := ctx.Err(); err != nil {
			return err
		}
		page, err := pager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("list AFD route inventory: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := requireExplicitInventoryPage("routes", page.Value != nil); err != nil {
			return err
		}
		for _, route := range page.Value {
			if route == nil {
				return fmt.Errorf("AFD route inventory contains a nil resource")
			}
			if err := inventory.observe(route.ID); err != nil {
				return err
			}
			if err := verifyRouteIdentity(graph, *route); err != nil {
				return err
			}
			if err := verifyInventoryDeployment(route.Properties.ProvisioningState, route.Properties.DeploymentStatus); err != nil {
				return fmt.Errorf("listed AFD route is not converged: %w", err)
			}
			enabled := ptr.Deref(route.Properties.EnabledState, "")
			if enabled != armcdn.EnabledStateEnabled && enabled != armcdn.EnabledStateDisabled {
				return fmt.Errorf("listed AFD route has unknown enabled state %q", enabled)
			}
			if enabled != state ||
				!matchesHTTPPreviewRoute(graph.Route.Properties, route.Properties) {
				return graphMismatch("listed AFD route changed during inventory verification")
			}
		}
	}
	return inventory.complete()
}

func (p *RouteProvisioner) verifyGroupInventory(ctx context.Context, graph HTTPPreviewRequests) error {
	scope, roots := p.backends.roots.scope, graph.Roots
	group := graph.Backends.OriginGroups[0]
	inventory := newResourceInventory(roots.ProfileID + "/originGroups/" + group.Name)
	pager := p.backends.groups.NewListByProfilePager(scope.ResourceGroup, roots.ProfileName, nil)
	for pager.More() {
		if err := ctx.Err(); err != nil {
			return err
		}
		page, err := pager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("list AFD origin-group inventory: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := requireExplicitInventoryPage("origin groups", page.Value != nil); err != nil {
			return err
		}
		for _, actual := range page.Value {
			if actual == nil {
				return fmt.Errorf("AFD origin-group inventory contains a nil resource")
			}
			if err := inventory.observe(actual.ID); err != nil {
				return err
			}
			if err := verifyGroupIdentity(roots, group.Name, *actual); err != nil {
				return err
			}
			if err := verifyInventoryDeployment(actual.Properties.ProvisioningState, actual.Properties.DeploymentStatus); err != nil {
				return fmt.Errorf("listed AFD origin group is not converged: %w", err)
			}
			if !matchesOriginGroupPolicy(group.Parameters.Properties, actual.Properties) {
				return graphMismatch("listed AFD origin-group policy changed after staging")
			}
		}
	}
	return inventory.complete()
}

func (p *RouteProvisioner) observeOriginInventory(ctx context.Context, owner GatewayOwner,
	model gatewaymodel.GlobalGateway, profile armcdn.Profile, graph HTTPPreviewRequests, rejectExtras bool) ([]armcdn.AFDOrigin, error) {
	scope, roots := p.backends.roots.scope, graph.Roots
	group := graph.Backends.OriginGroups[0]
	inventory := newOriginInventory(roots, group)
	inventory.rejectExtras = rejectExtras
	pager := p.backends.origins.NewListByOriginGroupPager(scope.ResourceGroup, roots.ProfileName, group.Name, nil)
	for pager.More() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list AFD origin inventory: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := requireExplicitInventoryPage("origins", page.Value != nil); err != nil {
			return nil, err
		}
		for _, origin := range page.Value {
			if err := inventory.observe(origin); err != nil {
				return nil, err
			}
		}
	}
	if err := inventory.seen.complete(); err != nil {
		return nil, err
	}
	origins, err := ObservePrivateOrigins(scope, owner, model, profile, inventory.observed)
	if err != nil {
		return nil, err
	}
	for _, origin := range origins.Origins {
		if origin.State == PrivateOriginReady {
			continue
		}
		if origin.State == PrivateOriginUnknown || origin.State == PrivateOriginProvisioning {
			return nil, fmt.Errorf("listed AFD origin %q has unconfirmed convergence (%s)", origin.Reference.Origin, origin.State)
		}
		if origin.State == PrivateOriginApprovalPending {
			return nil, graphMismatch("listed AFD origin %q changed to %s; AFD Private Link approval is asynchronous and requires the authorized PLS owner to approve the request",
				origin.Reference.Origin, origin.State)
		}
		return nil, graphMismatch("listed AFD origin %q changed to %s after staging", origin.Reference.Origin, origin.State)
	}
	if !origins.OriginsConverged {
		return nil, fmt.Errorf("listed AFD origins do not establish complete private-origin convergence")
	}
	return inventory.stale, ctx.Err()
}

type originInventory struct {
	roots        RootRequests
	group        OriginGroupRequest
	references   map[string]OriginReference
	seen         resourceInventory
	observed     map[OriginReference]armcdn.AFDOrigin
	stale        []armcdn.AFDOrigin
	rejectExtras bool
}

func newOriginInventory(roots RootRequests, group OriginGroupRequest) originInventory {
	inventory := originInventory{
		roots: roots, group: group, seen: make(resourceInventory, len(group.Origins)),
		references: make(map[string]OriginReference, len(group.Origins)),
		observed:   make(map[OriginReference]armcdn.AFDOrigin, len(group.Origins)),
	}
	for _, origin := range group.Origins {
		id := strings.ToLower(roots.ProfileID + "/originGroups/" + group.Name + "/origins/" + origin.Name)
		inventory.seen[id] = false
		inventory.references[id] = OriginReference{OriginGroup: group.Name, Origin: origin.Name}
	}
	return inventory
}

func (inventory *originInventory) observe(origin *armcdn.AFDOrigin) error {
	if origin == nil || origin.Properties == nil || origin.ID == nil {
		return fmt.Errorf("AFD origin inventory lacks a resource, properties or ARM identity")
	}
	key := strings.ToLower(*origin.ID)
	if ref, expected := inventory.references[key]; expected {
		if err := inventory.seen.observe(origin.ID); err != nil {
			return err
		}
		expectedID := inventory.roots.ProfileID + "/originGroups/" + ref.OriginGroup + "/origins/" + ref.Origin
		if err := verifyOriginIdentity(expectedID, ref, *origin); err != nil {
			return err
		}
		inventory.observed[ref] = *origin
		return nil
	}
	if inventory.rejectExtras {
		return graphMismatch("enabled AFD graph contains unexpected origin %q; disabled restaging is required", *origin.ID)
	}
	if err := verifyStaleOrigin(inventory.roots, inventory.group, *origin); err != nil {
		return err
	}
	if inventory.seen[key] {
		return fmt.Errorf("duplicate stale AFD origin %q", *origin.ID)
	}
	inventory.seen[key] = true
	inventory.stale = append(inventory.stale, *origin)
	return nil
}

func verifyInventoryDeployment(provisioning *armcdn.AfdProvisioningState, deployment *armcdn.DeploymentStatus) error {
	ready, err := afdProvisioningReady(provisioning)
	if err != nil {
		return err
	}
	if !ready {
		return fmt.Errorf("AFD provisioning changed during inventory verification")
	}
	if err := validateAFDDeploymentState(deployment); err != nil {
		return err
	}
	if ptr.Deref(deployment, "") != armcdn.DeploymentStatusSucceeded {
		return fmt.Errorf("AFD deployment changed during inventory verification")
	}
	return nil
}
