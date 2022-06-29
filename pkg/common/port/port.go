// Package port provides ports related functions.
package port

import (
	"reflect"

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

// CompareServicePorts compares k8 service ports and ignores the order.
func CompareServicePorts(a []corev1.ServicePort, b []corev1.ServicePort) bool {
	if len(a) != len(b) {
		return false
	}
	aMap := make(map[string]corev1.ServicePort)
	for _, portA := range a {
		aMap[portA.Name] = portA
	}
	for _, portB := range b {
		portA, found := aMap[portB.Name]
		if !found {
			return false
		}
		if !reflect.DeepEqual(portA, portB) {
			return false
		}
	}
	return true
}
