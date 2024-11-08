/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package trafficmanagerbackend

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

func TestIsValidTrafficManagerEndpoint(t *testing.T) {
	tests := []struct {
		name    string
		export  *fleetnetv1alpha1.InternalServiceExport
		wantErr bool
	}{
		{
			name: "valid endpoint",
			export: &fleetnetv1alpha1.InternalServiceExport{
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Type:                   corev1.ServiceTypeLoadBalancer,
					IsDNSLabelConfigured:   true,
					IsInternalLoadBalancer: false,
				},
			},
			wantErr: false,
		},
		{
			name: "wrong service type",
			export: &fleetnetv1alpha1.InternalServiceExport{
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Type:                   corev1.ServiceTypeClusterIP,
					IsDNSLabelConfigured:   true,
					IsInternalLoadBalancer: false,
				},
			},
			wantErr: true,
		},
		{
			name: "load balancer type with internal ip",
			export: &fleetnetv1alpha1.InternalServiceExport{
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Type:                   corev1.ServiceTypeLoadBalancer,
					IsDNSLabelConfigured:   true,
					IsInternalLoadBalancer: true,
				},
			},
			wantErr: true,
		},
		{
			name: "load balancer type with public ip but dns label not configured",
			export: &fleetnetv1alpha1.InternalServiceExport{
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Type:                 corev1.ServiceTypeLoadBalancer,
					IsDNSLabelConfigured: false,
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := isValidTrafficManagerEndpoint(tt.export)
			if got := err != nil; got != tt.wantErr {
				t.Errorf("isValidTrafficManagerEndpoint() = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
