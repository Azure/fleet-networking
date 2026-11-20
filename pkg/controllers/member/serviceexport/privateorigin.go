/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package serviceexport

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/privateorigin"
)

type OriginLoadBalancerClient = privateorigin.LoadBalancerClient
type OriginPrivateLinkServiceClient = privateorigin.PrivateLinkServiceClient

const (
	privateOriginRediscoveryInterval = 2 * time.Minute
	internalLoadBalancerEnabled      = "true"
	azureDiscoveryFailedReason       = "AzureDiscoveryFailed"
)

func (r *Reconciler) discoverPrivateLinkOrigin(ctx context.Context, service *corev1.Service) (*fleetnetv1alpha1.PrivateLinkOrigin, error) {
	discovery := privateorigin.Discovery{
		LoadBalancerClient:       r.AzureLoadBalancerClient,
		PrivateLinkServiceClient: r.AzurePrivateLinkServiceClient,
		ResourceGroupName:        r.ResourceGroupName,
	}
	return discovery.Discover(ctx, service)
}
