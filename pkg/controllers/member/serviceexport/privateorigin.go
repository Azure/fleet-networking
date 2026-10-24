/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package serviceexport

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v4"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
)

// OriginLoadBalancerClient exposes only the Azure read needed for origin discovery.
type OriginLoadBalancerClient interface {
	List(context.Context, string) ([]*armnetwork.LoadBalancer, error)
}

// OriginPrivateLinkServiceClient exposes only the Azure read needed for origin discovery.
type OriginPrivateLinkServiceClient interface {
	List(context.Context, string) ([]*armnetwork.PrivateLinkService, error)
}

const (
	privateOriginRediscoveryInterval = 2 * time.Minute
	internalLoadBalancerEnabled      = "true"
	azureDiscoveryFailedReason       = "AzureDiscoveryFailed"
)

func (r *Reconciler) discoverPrivateLinkOrigin(ctx context.Context, service *corev1.Service) (*fleetnetv1alpha1.PrivateLinkOrigin, error) {
	origin := &fleetnetv1alpha1.PrivateLinkOrigin{Reason: "NotInternalLoadBalancer"}
	if service.Spec.Type != corev1.ServiceTypeLoadBalancer ||
		service.Annotations[objectmeta.ServiceAnnotationAzureLoadBalancerInternal] != internalLoadBalancerEnabled {
		return origin, nil
	}
	origin.Reason = "WaitingForLoadBalancer"
	if len(service.Status.LoadBalancer.Ingress) == 0 {
		return origin, nil
	}
	origin.Reason = "UnsupportedIngress"
	if len(service.Status.LoadBalancer.Ingress) != 1 {
		return origin, nil
	}
	// Match the observed frontend, not a requested address or a guessed DNS target.
	address, err := netip.ParseAddr(service.Status.LoadBalancer.Ingress[0].IP)
	if err != nil || !address.Is4() || !address.IsGlobalUnicast() {
		return origin, nil
	}
	if r.AzureLoadBalancerClient == nil || r.AzurePrivateLinkServiceClient == nil || strings.TrimSpace(r.ResourceGroupName) == "" {
		origin.Reason = azureDiscoveryFailedReason
		return origin, fmt.Errorf("private origin discovery requires Azure read clients and a default resource group")
	}

	// Both Azure lists share one budget, rather than each receiving a fresh timeout.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	lbGroup := strings.TrimSpace(service.Annotations[objectmeta.ServiceAnnotationLoadBalancerResourceGroup])
	if lbGroup == "" {
		lbGroup = r.ResourceGroupName
	}
	loadBalancers, err := r.AzureLoadBalancerClient.List(ctx, lbGroup)
	if err != nil {
		origin.Reason = azureDiscoveryFailedReason
		return origin, fmt.Errorf("list load balancers in resource group %q: %w", lbGroup, err)
	}
	frontend, reason := findInternalFrontend(loadBalancers, address)
	if frontend == nil {
		origin.Reason = reason
		return origin, nil
	}
	origin.InternalIP = address.String()

	plsGroup := strings.TrimSpace(service.Annotations[objectmeta.ServiceAnnotationPLSResourceGroup])
	if plsGroup == "" {
		plsGroup = r.ResourceGroupName
	}
	privateLinks, err := r.AzurePrivateLinkServiceClient.List(ctx, plsGroup)
	if err != nil {
		origin.Reason = azureDiscoveryFailedReason
		return origin, fmt.Errorf("list private link services in resource group %q: %w", plsGroup, err)
	}
	// A matching name is only a filter; the PLS must reference the observed frontend.
	name := strings.TrimSpace(service.Annotations[objectmeta.ServiceAnnotationPLSName])
	privateLink, reason := findPrivateLinkService(privateLinks, name, *frontend.ID)
	if privateLink == nil {
		origin.Reason = reason
		return origin, nil
	}
	origin.Reason = "InvalidPrivateLinkService"
	plsID, err := arm.ParseResourceID(ptr.Deref(privateLink.ID, ""))
	if err != nil || !strings.EqualFold(plsID.ResourceType.String(), "Microsoft.Network/privateLinkServices") ||
		!strings.EqualFold(plsID.ResourceGroupName, plsGroup) || ptr.Deref(privateLink.Location, "") == "" {
		return origin, nil
	}
	origin.ResourceID = *privateLink.ID
	origin.Location = *privateLink.Location
	origin.Reason = "PrivateLinkServiceNotReady"
	if ptr.Deref(privateLink.Properties.ProvisioningState, "") != armnetwork.ProvisioningStateSucceeded {
		return origin, nil
	}
	origin.Ready = true
	origin.Reason = "Ready"
	return origin, nil
}

func findInternalFrontend(loadBalancers []*armnetwork.LoadBalancer, address netip.Addr) (*armnetwork.FrontendIPConfiguration, string) {
	var frontend *armnetwork.FrontendIPConfiguration
	var loadBalancerReady bool
	for _, lb := range loadBalancers {
		if lb == nil || lb.Properties == nil {
			continue
		}
		for _, candidate := range lb.Properties.FrontendIPConfigurations {
			if candidate == nil || candidate.Properties == nil {
				continue
			}
			properties := candidate.Properties
			candidateIP, parseErr := netip.ParseAddr(ptr.Deref(properties.PrivateIPAddress, ""))
			if parseErr != nil || candidateIP != address || properties.Subnet == nil ||
				ptr.Deref(properties.Subnet.ID, "") == "" || properties.PublicIPAddress != nil || properties.PublicIPPrefix != nil {
				continue
			}
			if frontend != nil {
				// Private IPs can overlap across networks; never choose an arbitrary match.
				return nil, "AmbiguousLoadBalancer"
			}
			frontend = candidate
			loadBalancerReady = ptr.Deref(lb.Properties.ProvisioningState, "") == armnetwork.ProvisioningStateSucceeded
		}
	}
	if frontend == nil {
		return nil, "LoadBalancerNotFound"
	}
	frontendID, err := arm.ParseResourceID(ptr.Deref(frontend.ID, ""))
	if err != nil || !strings.EqualFold(frontendID.ResourceType.String(), "Microsoft.Network/loadBalancers/frontendIPConfigurations") ||
		!loadBalancerReady || ptr.Deref(frontend.Properties.ProvisioningState, "") != armnetwork.ProvisioningStateSucceeded {
		return nil, "LoadBalancerNotReady"
	}
	return frontend, ""
}

func findPrivateLinkService(privateLinks []*armnetwork.PrivateLinkService, name, frontendID string) (*armnetwork.PrivateLinkService, string) {
	var privateLink *armnetwork.PrivateLinkService
	for _, candidate := range privateLinks {
		if candidate == nil || candidate.Properties == nil || (name != "" && !strings.EqualFold(name, ptr.Deref(candidate.Name, ""))) {
			continue
		}
		for _, reference := range candidate.Properties.LoadBalancerFrontendIPConfigurations {
			if reference == nil || !strings.EqualFold(ptr.Deref(reference.ID, ""), frontendID) {
				continue
			}
			if privateLink != nil {
				return nil, "AmbiguousPrivateLinkService"
			}
			privateLink = candidate
			break
		}
	}
	if privateLink == nil {
		return nil, "PrivateLinkServiceNotFound"
	}
	return privateLink, ""
}
