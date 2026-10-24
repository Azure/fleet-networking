/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package frontdoor

import (
	"context"
	"fmt"
	"time"

	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
)

// DisabledGraphStagingResult reports disabled resources and supported inventory,
// not traffic readiness, complete stale-resource removal or production write authorization.
type DisabledGraphStagingResult struct {
	Roots RootProvisioningResult
	Route RouteProvisioningResult
	// InventoryVerified covers supported membership/convergence and empty
	// unsupported domain/rule-set/security-policy/secret collections, not readiness.
	InventoryVerified bool
	// StaleOriginsPending counts observed extras awaiting verified cleanup.
	StaleOriginsPending int
}

// StageDisabledGraph composes disabled staging and bounded stale-origin cleanup
// under one deadline with at most one write. Callers must first persist cleanup ownership.
// Platform and concurrent-writer gates still apply; the manager does not inject it.
func (p *RouteProvisioner) StageDisabledGraph(ctx context.Context, owner GatewayOwner,
	model gatewaymodel.GlobalGateway) (DisabledGraphStagingResult, error) {
	if err := ctx.Err(); err != nil {
		return DisabledGraphStagingResult{}, err
	}
	if err := p.validateGraphClients(); err != nil {
		return DisabledGraphStagingResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	graph, err := BuildHTTPPreviewRequests(p.backends.roots.scope, owner, model)
	if err != nil {
		return DisabledGraphStagingResult{}, err
	}
	roots, err := p.backends.roots.EnsureDisabledRoots(ctx, owner, model)
	if err != nil {
		return DisabledGraphStagingResult{}, err
	}
	if roots.State != DisabledRootsObserved {
		return DisabledGraphStagingResult{Roots: roots}, nil
	}
	route, err := p.EnsureDisabledRoute(ctx, owner, model)
	if err != nil {
		return DisabledGraphStagingResult{}, err
	}
	result := DisabledGraphStagingResult{Roots: roots, Route: route}
	if route.State == DisabledRouteObserved {
		// Later collection reads must prove both membership and managed convergence.
		stale, err := p.observeSupportedInventory(ctx, owner, model, graph)
		if err != nil {
			return DisabledGraphStagingResult{}, err
		}
		if len(stale) != 0 {
			// All inventory must pass before cleanup; submissions/404s are not absence.
			if err := p.pruneStaleOrigin(ctx, owner, graph, stale); err != nil {
				return DisabledGraphStagingResult{}, err
			}
			result.StaleOriginsPending = len(stale)
			return result, nil
		}
		result.InventoryVerified = true
	}
	return result, nil
}

func (p *RouteProvisioner) validateGraphClients() error {
	if p == nil || p.backends == nil || p.backends.roots == nil || p.backends.roots.profiles == nil ||
		p.backends.roots.endpoints == nil || p.backends.groups == nil || p.backends.origins == nil || p.routes == nil ||
		p.domains == nil || p.ruleSets == nil || p.policies == nil || p.secrets == nil {
		return fmt.Errorf("complete AFD graph provisioning clients are required")
	}
	return nil
}
