// Package port provides ports related functions.
package port

import (
	corev1 "k8s.io/api/core/v1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

// ToServicePort converts ServicePort of fleet networking api to a k8 ServicePort.
func ToServicePort(port fleetnetv1alpha1.ServicePort) corev1.ServicePort {
	return corev1.ServicePort{
		Name:        port.Name,
		Protocol:    port.Protocol,
		AppProtocol: port.AppProtocol,
		Port:        port.Port,
		TargetPort:  port.TargetPort,
	}
}
