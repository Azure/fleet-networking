//go:build e2e

/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v4"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
	"go.goms.io/fleet-networking/pkg/providers/azure/frontdoor"
)

type privateHTTPApprovalServices interface {
	Get(context.Context, string, string, *armnetwork.PrivateLinkServicesClientGetOptions) (armnetwork.PrivateLinkServicesClientGetResponse, error)
	UpdatePrivateEndpointConnection(context.Context, string, string, string, armnetwork.PrivateEndpointConnection,
		*armnetwork.PrivateLinkServicesClientUpdatePrivateEndpointConnectionOptions) (armnetwork.PrivateLinkServicesClientUpdatePrivateEndpointConnectionResponse, error)
}

type privateHTTPApprovalRequest struct {
	group, service, connection, id, target, description string
	pending                                             bool
}

// FIXME: Remove this opt-in E2E-only approval helper when the AFD/PLS approval
// integration is available. Only the test caller, never a controller, approves.
func approvePrivateHTTPRequests(ctx context.Context, services privateHTTPApprovalServices,
	scope frontdoor.ResourceScope, roots frontdoor.RootRequests, namespace string, plans []frontdoor.PrivateLinkPlan,
	checkpoint []privateHTTPApprovalRequest) error {
	switch os.Getenv("ENABLE_AFD_PRIVATE_LINK_AUTO_APPROVAL") {
	case "", privateHTTPDisabled:
		return nil
	case privateHTTPEnabled:
	default:
		return fmt.Errorf("ENABLE_AFD_PRIVATE_LINK_AUTO_APPROVAL must be true or false")
	}
	if os.Getenv("ENABLE_AFD_PRIVATE_HTTP_PROOF") != privateHTTPEnabled ||
		services == nil || roots.ProfileName == "" || namespace == "" || len(plans) == 0 {
		return fmt.Errorf("automatic approval requires the enabled proof and owned correlated targets")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	requests, err := collectPrivateHTTPApprovalRequests(ctx, services, scope, roots, namespace, plans)
	if err != nil {
		return err
	}
	if err := privateHTTPApprovalIdentities(checkpoint, requests); err != nil {
		return err
	}
	// Validate every target and pinned request before the first approval write.
	for _, request := range requests {
		if !request.pending {
			continue
		}
		parameters := armnetwork.PrivateEndpointConnection{Properties: &armnetwork.PrivateEndpointConnectionProperties{
			PrivateLinkServiceConnectionState: &armnetwork.PrivateLinkServiceConnectionState{
				Status: ptr.To(privateHTTPLinkApproved), Description: ptr.To(request.description),
			},
		}}
		if _, err := services.UpdatePrivateEndpointConnection(ctx, request.group, request.service, request.connection, parameters, nil); err != nil {
			return fmt.Errorf("approve exact test connection %s: %w", request.id, err)
		}
	}
	verified, err := collectPrivateHTTPApprovalRequests(ctx, services, scope, roots, namespace, plans)
	if err != nil {
		return fmt.Errorf("verify approved test connections: %w", err)
	}
	if err := privateHTTPApprovalIdentities(checkpoint, verified); err != nil {
		return err
	}
	return errors.Join(privateHTTPApprovalsComplete(verified), ctx.Err())
}

func collectPrivateHTTPApprovalRequests(ctx context.Context, services privateHTTPApprovalServices,
	scope frontdoor.ResourceScope, roots frontdoor.RootRequests, namespace string,
	plans []frontdoor.PrivateLinkPlan) ([]privateHTTPApprovalRequest, error) {
	if services == nil || roots.ProfileName == "" || namespace == "" || len(plans) == 0 {
		return nil, fmt.Errorf("PLS request observation requires the owned profile and exact proof targets")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	description := "Fleet AFD private origin for " + roots.ProfileName
	requests := make([]privateHTTPApprovalRequest, 0, len(plans))
	targets := make(map[string]bool)
	var issues []error
	for _, plan := range plans {
		if err := ctx.Err(); err != nil {
			issues = append(issues, err)
			break
		}
		target, err := arm.ParseResourceID(plan.Target.ResourceID)
		if err != nil {
			issues = append(issues, fmt.Errorf("parse approval target %s: %w", plan.Target.ResourceID, err))
			continue
		}
		key := strings.ToLower(plan.Target.ResourceID)
		if !strings.EqualFold(target.SubscriptionID, scope.SubscriptionID) ||
			!strings.EqualFold(target.ResourceType.String(), "Microsoft.Network/privateLinkServices") ||
			target.Name != namespace || targets[key] {
			issues = append(issues, fmt.Errorf("refusing duplicate or non-proof approval target %q", plan.Target.ResourceID))
			continue
		}
		targets[key] = true
		pls, err := services.Get(ctx, target.ResourceGroupName, target.Name, nil)
		if err != nil {
			issues = append(issues, fmt.Errorf("read approval PLS %s: %w", plan.Target.ResourceID, err))
			continue
		}
		request, err := privateHTTPApprovalForPLS(target, plan.Target, pls.PrivateLinkService, description)
		if err != nil {
			issues = append(issues, err)
			continue
		}
		requests = append(requests, request)
	}
	if err := ctx.Err(); err != nil {
		issues = append(issues, err)
	}
	return requests, errors.Join(issues...)
}

func privateHTTPApprovalIdentities(checkpoint, current []privateHTTPApprovalRequest) error {
	if len(checkpoint) == 0 || len(checkpoint) != len(current) {
		return fmt.Errorf("exact PLS request checkpoint is missing or incomplete")
	}
	identities := make(map[string]string, len(checkpoint))
	for _, request := range checkpoint {
		identities[strings.ToLower(request.target)] = strings.ToLower(request.id)
	}
	for _, request := range current {
		key := strings.ToLower(request.target)
		// A replacement request is not authorized by a matching description alone.
		if request.id == "" || identities[key] != strings.ToLower(request.id) {
			return fmt.Errorf("PLS request identity changed since checkpoint: %s", request.target)
		}
		delete(identities, key)
	}
	if len(identities) != 0 {
		return fmt.Errorf("PLS request checkpoint targets were not all observed")
	}
	return nil
}

func privateHTTPApprovalProgress(requests []privateHTTPApprovalRequest) string {
	states := make([]string, 0, len(requests))
	for _, request := range requests {
		state := privateHTTPLinkApproved
		if request.pending {
			state = privateHTTPLinkPending
		}
		states = append(states, fmt.Sprintf("%s state=%s", request.id, state))
	}
	return strings.Join(states, "; ")
}

func privateHTTPApprovalsComplete(requests []privateHTTPApprovalRequest) error {
	approved := len(requests) > 0
	for _, request := range requests {
		approved = approved && !request.pending
	}
	if !approved {
		return fmt.Errorf("waiting for all exact PLS approvals: %s", privateHTTPApprovalProgress(requests))
	}
	return nil
}

func privateHTTPApprovalForPLS(target *arm.ResourceID, desired frontdoor.PrivateLinkTarget,
	pls armnetwork.PrivateLinkService, description string) (privateHTTPApprovalRequest, error) {
	if !strings.EqualFold(ptr.Deref(pls.ID, ""), desired.ResourceID) ||
		!strings.EqualFold(ptr.Deref(pls.Location, ""), desired.Location) ||
		pls.Properties == nil || ptr.Deref(pls.Properties.ProvisioningState, "") != armnetwork.ProvisioningStateSucceeded {
		return privateHTTPApprovalRequest{}, fmt.Errorf("approval PLS identity/location/provisioning does not match %s", desired.ResourceID)
	}
	var request privateHTTPApprovalRequest
	matches := 0
	for _, connection := range pls.Properties.PrivateEndpointConnections {
		if connection == nil || connection.Properties == nil ||
			connection.Properties.PrivateLinkServiceConnectionState == nil {
			continue
		}
		state := connection.Properties.PrivateLinkServiceConnectionState
		if ptr.Deref(state.Description, "") != description {
			continue
		}
		matches++
		name := ptr.Deref(connection.Name, "")
		id := desired.ResourceID + "/privateEndpointConnections/" + name
		if name == "" || strings.ContainsAny(name, "/\\") ||
			!strings.EqualFold(ptr.Deref(connection.ID, ""), id) ||
			(ptr.Deref(state.Status, "") != privateHTTPLinkPending && ptr.Deref(state.Status, "") != privateHTTPLinkApproved) {
			return privateHTTPApprovalRequest{}, fmt.Errorf("refusing unverified/non-approvable connection on %s", desired.ResourceID)
		}
		request = privateHTTPApprovalRequest{
			group: target.ResourceGroupName, service: target.Name, connection: name,
			id: id, target: desired.ResourceID, description: description, pending: *state.Status == privateHTTPLinkPending,
		}
	}
	if matches != 1 {
		return privateHTTPApprovalRequest{}, fmt.Errorf("expected one owned-profile request on %s, observed %d", desired.ResourceID, matches)
	}
	return request, nil
}

func validatePrivateHTTPFrontend(service *corev1.Service, origin gatewaymodel.Origin,
	pls armnetwork.PrivateLinkService, frontend armnetwork.FrontendIPConfiguration) error {
	if err := origin.Validate(); err != nil {
		return err
	}
	address, err := netip.ParseAddr(origin.Endpoint)
	if err != nil || !address.IsPrivate() || string(service.UID) != origin.ServiceUID ||
		service.DeletionTimestamp != nil ||
		service.Spec.Type != corev1.ServiceTypeLoadBalancer ||
		service.Annotations[objectmeta.ServiceAnnotationAzureLoadBalancerInternal] != privateHTTPEnabled ||
		len(service.Status.LoadBalancer.Ingress) != 1 || service.Status.LoadBalancer.Ingress[0].IP != origin.Endpoint ||
		service.Status.LoadBalancer.Ingress[0].Hostname != "" {
		return fmt.Errorf("member Service does not establish the exact private source/frontend")
	}
	if !strings.EqualFold(ptr.Deref(pls.ID, ""), origin.PrivateLinkResourceID) ||
		!strings.EqualFold(ptr.Deref(pls.Location, ""), origin.PrivateLinkLocation) || pls.Properties == nil ||
		ptr.Deref(pls.Properties.ProvisioningState, "") != armnetwork.ProvisioningStateSucceeded {
		return fmt.Errorf("observed PLS does not establish the discovered resource/location")
	}
	properties := frontend.Properties
	if frontend.ID == nil || properties == nil || properties.Subnet == nil || ptr.Deref(properties.Subnet.ID, "") == "" ||
		properties.PublicIPAddress != nil || properties.PublicIPPrefix != nil ||
		ptr.Deref(properties.PrivateIPAddress, "") != origin.Endpoint ||
		ptr.Deref(properties.ProvisioningState, "") != armnetwork.ProvisioningStateSucceeded {
		return fmt.Errorf("Azure frontend is not a converged subnet-bound private-only frontend")
	}
	matches := 0
	for _, reference := range pls.Properties.LoadBalancerFrontendIPConfigurations {
		if reference != nil && strings.EqualFold(ptr.Deref(reference.ID, ""), *frontend.ID) {
			matches++
		}
	}
	if matches != 1 {
		return fmt.Errorf("PLS does not uniquely reference the verified private frontend")
	}
	return nil
}

func verifyPrivateHTTPFrontend(ctx context.Context, credential azcore.TokenCredential,
	service *corev1.Service, origin gatewaymodel.Origin) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	id, err := arm.ParseResourceID(origin.PrivateLinkResourceID)
	if err != nil {
		return err
	}
	services, err := armnetwork.NewPrivateLinkServicesClient(id.SubscriptionID, credential, nil)
	if err != nil {
		return err
	}
	pls, err := services.Get(ctx, id.ResourceGroupName, id.Name, nil)
	if err != nil {
		return err
	}
	if pls.Properties == nil {
		return fmt.Errorf("PLS has no observed frontend bindings")
	}
	matches := 0
	for _, reference := range pls.Properties.LoadBalancerFrontendIPConfigurations {
		if reference == nil || reference.ID == nil {
			return fmt.Errorf("PLS frontend reference has no ARM identity")
		}
		frontendID, err := arm.ParseResourceID(*reference.ID)
		if err != nil {
			return err
		}
		if frontendID.Parent == nil ||
			!strings.EqualFold(frontendID.ResourceType.String(), "Microsoft.Network/loadBalancers/frontendIPConfigurations") {
			return fmt.Errorf("PLS references an unexpected frontend resource type")
		}
		frontends, err := armnetwork.NewLoadBalancerFrontendIPConfigurationsClient(frontendID.SubscriptionID, credential, nil)
		if err != nil {
			return err
		}
		actual, err := frontends.Get(ctx, frontendID.ResourceGroupName, frontendID.Parent.Name, frontendID.Name, nil)
		if err != nil {
			return err
		}
		if !strings.EqualFold(ptr.Deref(actual.ID, ""), *reference.ID) {
			return fmt.Errorf("observed frontend contradicts its requested ARM identity")
		}
		if actual.Properties != nil && ptr.Deref(actual.Properties.PrivateIPAddress, "") == origin.Endpoint {
			if err := validatePrivateHTTPFrontend(service, origin, pls.PrivateLinkService, actual.FrontendIPConfiguration); err != nil {
				return err
			}
			matches++
		}
	}
	if matches != 1 {
		return fmt.Errorf("expected one PLS-bound Azure private frontend, observed %d", matches)
	}
	return ctx.Err()
}
