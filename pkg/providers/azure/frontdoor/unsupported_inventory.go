/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package frontdoor

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
)

func (p *RouteProvisioner) verifyUnsupportedInventory(ctx context.Context, graph HTTPPreviewRequests) error {
	scope, profile := p.backends.roots.scope, graph.Roots.ProfileName
	if err := verifyEmptyProfileCollection(ctx, "custom domains",
		p.domains.NewListByProfilePager(scope.ResourceGroup, profile, nil),
		func(page armcdn.AFDCustomDomainsClientListByProfileResponse) (int, bool) {
			return len(page.Value), page.Value != nil
		}); err != nil {
		return err
	}
	if err := verifyEmptyProfileCollection(ctx, "rule sets",
		p.ruleSets.NewListByProfilePager(scope.ResourceGroup, profile, nil),
		func(page armcdn.RuleSetsClientListByProfileResponse) (int, bool) {
			return len(page.Value), page.Value != nil
		}); err != nil {
		return err
	}
	if err := verifyEmptyProfileCollection(ctx, "security policies",
		p.policies.NewListByProfilePager(scope.ResourceGroup, profile, nil),
		func(page armcdn.SecurityPoliciesClientListByProfileResponse) (int, bool) {
			return len(page.Value), page.Value != nil
		}); err != nil {
		return err
	}
	return verifyEmptyProfileCollection(ctx, "secrets",
		p.secrets.NewListByProfilePager(scope.ResourceGroup, profile, nil),
		func(page armcdn.SecretsClientListByProfileResponse) (int, bool) {
			return len(page.Value), page.Value != nil
		})
}

func verifyEmptyProfileCollection[T any](ctx context.Context, collection string, pager *runtime.Pager[T],
	resourceCount func(T) (int, bool)) error {
	for pager.More() {
		if err := ctx.Err(); err != nil {
			return err
		}
		page, err := pager.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("list unsupported AFD %s inventory: %w", collection, err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		count, explicit := resourceCount(page)
		if err := requireExplicitInventoryPage(collection, explicit); err != nil {
			return err
		}
		if count != 0 {
			return graphMismatch("owned AFD profile contains unsupported %s; automatic migration or removal is not implemented", collection)
		}
	}
	return ctx.Err()
}
