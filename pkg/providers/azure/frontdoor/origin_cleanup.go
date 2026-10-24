/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package frontdoor

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/netip"
	"sort"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	"k8s.io/utils/ptr"

	"go.goms.io/fleet-networking/pkg/common/azureerrors"
)

func verifyStaleOrigin(roots RootRequests, group OriginGroupRequest, origin armcdn.AFDOrigin) error {
	// The verified parent grants authority; generated name shape only narrows it.
	name := ptr.Deref(origin.Name, "")
	suffix, fleetName := strings.CutPrefix(strings.ToLower(name), "fleet-origin-")
	hash, err := hex.DecodeString(suffix)
	if !fleetName || err != nil || len(hash) != 16 {
		return fmt.Errorf("extra AFD origin %q is not eligible for bounded Fleet cleanup", name)
	}
	ref := OriginReference{OriginGroup: group.Name, Origin: name}
	if err := verifyOriginIdentity(roots.ProfileID+"/originGroups/"+group.Name+"/origins/"+name, ref, origin); err != nil {
		return err
	}
	properties := origin.Properties
	if properties == nil || properties.SharedPrivateLinkResource == nil || properties.SharedPrivateLinkResource.PrivateLink == nil {
		return fmt.Errorf("extra AFD origin %q lacks verified private configuration", name)
	}
	link := properties.SharedPrivateLinkResource
	id, err := arm.ParseResourceID(ptr.Deref(link.PrivateLink.ID, ""))
	location := ptr.Deref(link.PrivateLinkLocation, "")
	if err != nil || !strings.EqualFold(id.ResourceType.String(), "Microsoft.Network/privateLinkServices") ||
		id.SubscriptionID == "" || id.ResourceGroupName == "" || location == "" || strings.TrimSpace(location) != location {
		return fmt.Errorf("extra AFD origin %q lacks a valid PLS identity/location", name)
	}
	address, err := netip.ParseAddr(ptr.Deref(properties.HostName, ""))
	if err != nil || !address.Is4() || !address.IsPrivate() ||
		ptr.Deref(properties.Weight, 0) < 1 || ptr.Deref(properties.Weight, 0) > 1000 {
		return fmt.Errorf("extra AFD origin %q lacks a supported private endpoint/weight", name)
	}
	desired := staleOriginConfiguration(origin)
	desired.HTTPPort = *group.Origins[0].Parameters.Properties.HTTPPort
	desired.Priority = 1
	desired.OriginHostHeader = ""
	desired.EnforceCertificateNameCheck = true
	if !matchesPrivateOrigin(desired, properties) {
		return fmt.Errorf("extra AFD origin %q differs from the supported private Fleet configuration", name)
	}
	// Removing an owned stale binding does not require its connectivity to be healthy.
	return verifyStaleOriginState(properties)
}

func staleOriginConfiguration(origin armcdn.AFDOrigin) Origin {
	properties := origin.Properties
	link := properties.SharedPrivateLinkResource
	return Origin{
		HostName: ptr.Deref(properties.HostName, ""), HTTPPort: ptr.Deref(properties.HTTPPort, 0),
		Weight: int64(ptr.Deref(properties.Weight, 0)), Priority: ptr.Deref(properties.Priority, 0),
		OriginHostHeader: ptr.Deref(properties.OriginHostHeader, ""), EnforceCertificateNameCheck: ptr.Deref(properties.EnforceCertificateNameCheck, false),
		PrivateLink: PrivateLinkTarget{ResourceID: ptr.Deref(link.PrivateLink.ID, ""), Location: ptr.Deref(link.PrivateLinkLocation, "")},
	}
}

func verifyStaleOriginState(properties *armcdn.AFDOriginProperties) error {
	switch ptr.Deref(properties.ProvisioningState, "") {
	case armcdn.AfdProvisioningStateSucceeded, armcdn.AfdProvisioningStateFailed,
		armcdn.AfdProvisioningStateCreating, armcdn.AfdProvisioningStateUpdating, armcdn.AfdProvisioningStateDeleting:
	default:
		return fmt.Errorf("stale AFD origin has unknown provisioning state")
	}
	switch ptr.Deref(properties.DeploymentStatus, "") {
	case armcdn.DeploymentStatusSucceeded, armcdn.DeploymentStatusFailed, armcdn.DeploymentStatusInProgress, armcdn.DeploymentStatusNotStarted:
	default:
		return fmt.Errorf("stale AFD origin has unknown deployment state")
	}
	switch ptr.Deref(properties.SharedPrivateLinkResource.Status, "") {
	case armcdn.SharedPrivateLinkResourceStatusApproved, armcdn.SharedPrivateLinkResourceStatusPending,
		armcdn.SharedPrivateLinkResourceStatusRejected, armcdn.SharedPrivateLinkResourceStatusDisconnected, armcdn.SharedPrivateLinkResourceStatusTimeout:
		return nil
	default:
		return fmt.Errorf("stale AFD origin has unknown Private Link approval state")
	}
}

func staleOriginBusy(origin armcdn.AFDOrigin) bool {
	switch ptr.Deref(origin.Properties.ProvisioningState, "") {
	case armcdn.AfdProvisioningStateCreating, armcdn.AfdProvisioningStateUpdating, armcdn.AfdProvisioningStateDeleting:
		return true
	default:
		return false
	}
}

func (p *RouteProvisioner) pruneStaleOrigin(ctx context.Context, owner GatewayOwner, graph HTTPPreviewRequests,
	stale []armcdn.AFDOrigin) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	sort.Slice(stale, func(i, j int) bool { return strings.ToLower(*stale[i].ID) < strings.ToLower(*stale[j].ID) })
	selected := stale[0]
	// Do not race another ARM operation, including a deletion already in flight.
	if staleOriginBusy(selected) {
		return ctx.Err()
	}
	root, group := p.backends.roots, graph.Backends.OriginGroups[0]
	actual, err := p.backends.origins.Get(ctx, root.scope.ResourceGroup, graph.Roots.ProfileName, group.Name, *selected.Name, nil)
	if err := ctx.Err(); err != nil {
		return err
	}
	if azureerrors.IsNotFound(err) {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("reobserve stale AFD origin before cleanup: %w", err)
	}
	if err := verifyOriginIdentity(*selected.ID, OriginReference{OriginGroup: group.Name, Origin: *selected.Name}, actual.AFDOrigin); err != nil {
		return err
	}
	if err := verifyStaleOrigin(graph.Roots, group, actual.AFDOrigin); err != nil {
		return err
	}
	if !matchesPrivateOrigin(staleOriginConfiguration(selected), actual.Properties) {
		return fmt.Errorf("stale AFD origin configuration changed before cleanup")
	}
	if staleOriginBusy(actual.AFDOrigin) {
		return ctx.Err()
	}
	// DELETE exposes no If-Match option; fresh reads still cannot exclude external writers.
	_, disabled, err := p.backends.observeDisabledRoots(ctx, owner, graph.Roots)
	if err != nil {
		return err
	}
	if !disabled {
		return fmt.Errorf("owned disabled AFD roots changed before stale-origin cleanup")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := p.backends.origins.BeginDelete(ctx, root.scope.ResourceGroup, graph.Roots.ProfileName, group.Name, *selected.Name, nil); err != nil &&
		!azureerrors.IsNotFound(err) {
		return fmt.Errorf("begin owned stale AFD origin deletion: %w", err)
	}
	return ctx.Err()
}
