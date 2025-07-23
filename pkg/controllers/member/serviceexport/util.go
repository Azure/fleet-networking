/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package serviceexport

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	fleetnetv1beta1 "go.goms.io/fleet-networking/api/v1beta1"
)

// formatInternalServiceExportName returns the unique name assigned to an exported Service.
func formatInternalServiceExportName(svcExport *fleetnetv1beta1.ServiceExport) string {
	return fmt.Sprintf("%s-%s", svcExport.Namespace, svcExport.Name)
}

// isServiceEligibleForExport returns if a Service is eligible for export; at this stage, headless Services
// and Services of the ExternalName type cannot be exported.
func isServiceEligibleForExport(svc *corev1.Service) bool {
	if svc.Spec.Type == corev1.ServiceTypeExternalName || svc.Spec.ClusterIP == "None" {
		return false
	}
	return true
}

// extractServicePorts extracts ports in use from Service.
func extractServicePorts(svc *corev1.Service) []fleetnetv1alpha1.ServicePort {
	svcExportPorts := []fleetnetv1alpha1.ServicePort{}
	for _, svcPort := range svc.Spec.Ports {
		svcExportPorts = append(svcExportPorts, fleetnetv1alpha1.ServicePort{
			Name:        svcPort.Name,
			Protocol:    svcPort.Protocol,
			AppProtocol: svcPort.AppProtocol,
			Port:        svcPort.Port,
			TargetPort:  svcPort.TargetPort,
		})
	}

	return svcExportPorts
}
