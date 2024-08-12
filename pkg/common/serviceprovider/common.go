package serviceprovider

import fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"

// ServiceProvider provides the functions how to extract the external ip from the exported services based on the endpoint
// type.
type ServiceProvider interface {
	// IsExternalLoadBalancerService returns whether the service is load balancer type with a public ip.
	IsExternalLoadBalancerService(service *fleetnetv1alpha1.InternalServiceExportSpec) bool

	// ExtractExternalIP returns an external ip or DNS name of the Service.
	// If the service is exposed via multiple IPs, it will only return the first one.
	ExtractExternalIP(service *fleetnetv1alpha1.InternalServiceExportSpec) string
}
