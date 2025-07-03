/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package trafficmanagerbackend

import (
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/trafficmanager/armtrafficmanager"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

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
					PublicIPResourceID:     ptr.To("abc"),
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
					PublicIPResourceID:   ptr.To("abc"),
					IsDNSLabelConfigured: false,
				},
			},
			wantErr: true,
		},
		{
			name: "load balancer type with public ip but public ip is not ready",
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

func TestEqualAzureTrafficManagerEndpoint(t *testing.T) {
	tests := []struct {
		name    string
		current armtrafficmanager.Endpoint
		want    bool
	}{
		{
			name: "endpoints are equal though current has other properties",
			current: armtrafficmanager.Endpoint{
				Type: ptr.To(string(armtrafficmanager.EndpointTypeAzureEndpoints)),
				Properties: &armtrafficmanager.EndpointProperties{
					TargetResourceID: ptr.To("RESourceID"),
					EndpointStatus:   ptr.To(armtrafficmanager.EndpointStatusEnabled),
					Weight:           ptr.To(int64(100)),
					Priority:         ptr.To(int64(1)),
					EndpointLocation: ptr.To("location"),
				},
			},
			want: true,
		},
		{
			name:    "type is nil",
			current: armtrafficmanager.Endpoint{},
		},
		{
			name: "type is different",
			current: armtrafficmanager.Endpoint{
				Type: ptr.To(string(armtrafficmanager.EndpointTypeNestedEndpoints)),
			},
		},
		{
			name: "type is case insensitive",
			current: armtrafficmanager.Endpoint{
				Type: ptr.To("azureEndpoints"),
			},
		},
		{
			name: "Properties is nil",
			current: armtrafficmanager.Endpoint{
				Type: ptr.To(string(armtrafficmanager.EndpointTypeAzureEndpoints)),
			},
		},
		{
			name: "Properties.TargetResourceID is nil",
			current: armtrafficmanager.Endpoint{
				Type:       ptr.To(string(armtrafficmanager.EndpointTypeAzureEndpoints)),
				Properties: &armtrafficmanager.EndpointProperties{},
			},
		},
		{
			name: "Properties.Weight is nil",
			current: armtrafficmanager.Endpoint{
				Type: ptr.To(string(armtrafficmanager.EndpointTypeAzureEndpoints)),
				Properties: &armtrafficmanager.EndpointProperties{
					TargetResourceID: ptr.To("resourceID"),
				},
			},
		},
		{
			name: "Properties.EndpointStatus is nil",
			current: armtrafficmanager.Endpoint{
				Type: ptr.To(string(armtrafficmanager.EndpointTypeAzureEndpoints)),
				Properties: &armtrafficmanager.EndpointProperties{
					TargetResourceID: ptr.To("resourceID"),
					Weight:           ptr.To(int64(100)),
				},
			},
		},
		{
			name: "Properties.TargetResourceID is different",
			current: armtrafficmanager.Endpoint{
				Type: ptr.To(string(armtrafficmanager.EndpointTypeAzureEndpoints)),
				Properties: &armtrafficmanager.EndpointProperties{
					TargetResourceID: ptr.To("invalid-resourceID"),
					EndpointStatus:   ptr.To(armtrafficmanager.EndpointStatusEnabled),
					Weight:           ptr.To(int64(100)),
				},
			},
		},
		{
			name: "Properties.Weight is different",
			current: armtrafficmanager.Endpoint{
				Type: ptr.To(string(armtrafficmanager.EndpointTypeAzureEndpoints)),
				Properties: &armtrafficmanager.EndpointProperties{
					TargetResourceID: ptr.To("invalid-resourceID"),
					EndpointStatus:   ptr.To(armtrafficmanager.EndpointStatusEnabled),
					Weight:           ptr.To(int64(10)),
				},
			},
		},
		{
			name: "Properties.EndpointStatus is different",
			current: armtrafficmanager.Endpoint{
				Type: ptr.To(string(armtrafficmanager.EndpointTypeAzureEndpoints)),
				Properties: &armtrafficmanager.EndpointProperties{
					TargetResourceID: ptr.To("resourceID"),
					EndpointStatus:   ptr.To(armtrafficmanager.EndpointStatusDisabled),
					Weight:           ptr.To(int64(100)),
				},
			},
		},
	}
	desired := armtrafficmanager.Endpoint{
		Type: ptr.To(string(armtrafficmanager.EndpointTypeAzureEndpoints)),
		Properties: &armtrafficmanager.EndpointProperties{
			TargetResourceID: ptr.To("resourceID"),
			EndpointStatus:   ptr.To(armtrafficmanager.EndpointStatusEnabled),
			Weight:           ptr.To(int64(100)),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := equalAzureTrafficManagerEndpoint(tt.current, desired); got != tt.want {
				t.Errorf("equalAzureTrafficManagerEndpoint() = %v, want %v", got, tt.want)
			}
		})
	}
}
