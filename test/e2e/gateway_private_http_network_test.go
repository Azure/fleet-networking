//go:build e2e

/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v4"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
)

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
