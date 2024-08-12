package azure

import fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"

type ServiceProvider struct {
}

func (s *ServiceProvider) IsExternalLoadBalancerService(service *fleetnetv1alpha1.InternalServiceExportSpec) bool {
	return true
}

func (s *ServiceProvider) ExtractExternalIP(service *fleetnetv1alpha1.InternalServiceExportSpec) string {
	return ""
}
