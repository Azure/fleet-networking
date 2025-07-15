/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package trafficmanagerbackend

import (
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/trafficmanager/armtrafficmanager"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	fleetnetv1beta1 "go.goms.io/fleet-networking/api/v1beta1"
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
			name: "type case insensitive",
			current: armtrafficmanager.Endpoint{
				Type: ptr.To(string("azureEndpoints")),
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

func TestShouldHandleServiceImportUpateEvent(t *testing.T) {
	tests := []struct {
		name string
		old  *fleetnetv1alpha1.ServiceImport
		new  *fleetnetv1alpha1.ServiceImport
		want bool
	}{
		{
			name: "clusters are the same",
			old: &fleetnetv1alpha1.ServiceImport{
				Status: fleetnetv1alpha1.ServiceImportStatus{
					Clusters: []fleetnetv1alpha1.ClusterStatus{
						{Cluster: "cluster1"},
						{Cluster: "cluster2"},
					},
				},
			},
			new: &fleetnetv1alpha1.ServiceImport{
				Status: fleetnetv1alpha1.ServiceImportStatus{
					Clusters: []fleetnetv1alpha1.ClusterStatus{
						{Cluster: "cluster1"},
						{Cluster: "cluster2"},
					},
				},
			},
			want: false,
		},
		{
			name: "clusters are different",
			old: &fleetnetv1alpha1.ServiceImport{
				Status: fleetnetv1alpha1.ServiceImportStatus{
					Clusters: []fleetnetv1alpha1.ClusterStatus{
						{Cluster: "cluster1"},
					},
				},
			},
			new: &fleetnetv1alpha1.ServiceImport{
				Status: fleetnetv1alpha1.ServiceImportStatus{
					Clusters: []fleetnetv1alpha1.ClusterStatus{
						{Cluster: "cluster1"},
						{Cluster: "cluster2"},
					},
				},
			},
			want: true,
		},
		{
			name: "empty clusters in both",
			old: &fleetnetv1alpha1.ServiceImport{
				Status: fleetnetv1alpha1.ServiceImportStatus{
					Clusters: []fleetnetv1alpha1.ClusterStatus{},
				},
			},
			new: &fleetnetv1alpha1.ServiceImport{
				Status: fleetnetv1alpha1.ServiceImportStatus{
					Clusters: []fleetnetv1alpha1.ClusterStatus{},
				},
			},
			want: false,
		},
		{
			name: "nil clusters in old, empty in new",
			old: &fleetnetv1alpha1.ServiceImport{
				Status: fleetnetv1alpha1.ServiceImportStatus{
					Clusters: nil,
				},
			},
			new: &fleetnetv1alpha1.ServiceImport{
				Status: fleetnetv1alpha1.ServiceImportStatus{
					Clusters: []fleetnetv1alpha1.ClusterStatus{},
				},
			},
			want: false,
		},
		{
			name: "same clusters but different order",
			old: &fleetnetv1alpha1.ServiceImport{
				Status: fleetnetv1alpha1.ServiceImportStatus{
					Clusters: []fleetnetv1alpha1.ClusterStatus{
						{Cluster: "cluster1"},
						{Cluster: "cluster2"},
					},
				},
			},
			new: &fleetnetv1alpha1.ServiceImport{
				Status: fleetnetv1alpha1.ServiceImportStatus{
					Clusters: []fleetnetv1alpha1.ClusterStatus{
						{Cluster: "cluster2"},
						{Cluster: "cluster1"},
					},
				},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldHandleServiceImportUpateEvent(tt.old, tt.new); got != tt.want {
				t.Errorf("shouldHandleServiceImportUpateEvent() = %v, want %v", got, tt.want)
			}
		})
	}
}
func TestShouldHandleTrafficManagerProfileUpdateEvent(t *testing.T) {
	tests := []struct {
		name string
		old  *fleetnetv1beta1.TrafficManagerProfile
		new  *fleetnetv1beta1.TrafficManagerProfile
		want bool
	}{
		{
			name: "both profiles have no conditions",
			old: &fleetnetv1beta1.TrafficManagerProfile{
				Status: fleetnetv1beta1.TrafficManagerProfileStatus{
					Conditions: []metav1.Condition{},
				},
			},
			new: &fleetnetv1beta1.TrafficManagerProfile{
				Status: fleetnetv1beta1.TrafficManagerProfileStatus{
					Conditions: []metav1.Condition{},
				},
			},
			want: false,
		},
		{
			name: "old profile has no programmed condition, new profile has programmed condition",
			old: &fleetnetv1beta1.TrafficManagerProfile{
				Status: fleetnetv1beta1.TrafficManagerProfileStatus{
					Conditions: []metav1.Condition{},
				},
			},
			new: &fleetnetv1beta1.TrafficManagerProfile{
				Status: fleetnetv1beta1.TrafficManagerProfileStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(fleetnetv1beta1.TrafficManagerProfileConditionProgrammed),
							Status: metav1.ConditionTrue,
							Reason: "Programmed",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "old profile has programmed condition, new profile has no programmed condition",
			old: &fleetnetv1beta1.TrafficManagerProfile{
				Status: fleetnetv1beta1.TrafficManagerProfileStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(fleetnetv1beta1.TrafficManagerProfileConditionProgrammed),
							Status: metav1.ConditionTrue,
							Reason: "Programmed",
						},
					},
				},
			},
			new: &fleetnetv1beta1.TrafficManagerProfile{
				Status: fleetnetv1beta1.TrafficManagerProfileStatus{
					Conditions: []metav1.Condition{},
				},
			},
			want: true,
		},
		{
			name: "same programmed condition status and reason",
			old: &fleetnetv1beta1.TrafficManagerProfile{
				Status: fleetnetv1beta1.TrafficManagerProfileStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(fleetnetv1beta1.TrafficManagerProfileConditionProgrammed),
							Status: metav1.ConditionTrue,
							Reason: "Programmed",
						},
					},
				},
			},
			new: &fleetnetv1beta1.TrafficManagerProfile{
				Status: fleetnetv1beta1.TrafficManagerProfileStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(fleetnetv1beta1.TrafficManagerProfileConditionProgrammed),
							Status: metav1.ConditionTrue,
							Reason: "Programmed",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "different programmed condition status",
			old: &fleetnetv1beta1.TrafficManagerProfile{
				Status: fleetnetv1beta1.TrafficManagerProfileStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(fleetnetv1beta1.TrafficManagerProfileConditionProgrammed),
							Status: metav1.ConditionFalse,
							Reason: "Invalid",
						},
					},
				},
			},
			new: &fleetnetv1beta1.TrafficManagerProfile{
				Status: fleetnetv1beta1.TrafficManagerProfileStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(fleetnetv1beta1.TrafficManagerProfileConditionProgrammed),
							Status: metav1.ConditionTrue,
							Reason: "Programmed",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "same status but different reason - should not requeue",
			old: &fleetnetv1beta1.TrafficManagerProfile{
				Status: fleetnetv1beta1.TrafficManagerProfileStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(fleetnetv1beta1.TrafficManagerProfileConditionProgrammed),
							Status: metav1.ConditionTrue,
							Reason: "Programmed",
						},
					},
				},
			},
			new: &fleetnetv1beta1.TrafficManagerProfile{
				Status: fleetnetv1beta1.TrafficManagerProfileStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(fleetnetv1beta1.TrafficManagerProfileConditionProgrammed),
							Status: metav1.ConditionTrue,
							Reason: "Updated",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "different condition types - should not requeue",
			old: &fleetnetv1beta1.TrafficManagerProfile{
				Status: fleetnetv1beta1.TrafficManagerProfileStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "SomeOtherCondition",
							Status: metav1.ConditionTrue,
							Reason: "Ready",
						},
					},
				},
			},
			new: &fleetnetv1beta1.TrafficManagerProfile{
				Status: fleetnetv1beta1.TrafficManagerProfileStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "AnotherCondition",
							Status: metav1.ConditionTrue,
							Reason: "Ready",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "condition status changes from unknown to true",
			old: &fleetnetv1beta1.TrafficManagerProfile{
				Status: fleetnetv1beta1.TrafficManagerProfileStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(fleetnetv1beta1.TrafficManagerProfileConditionProgrammed),
							Status: metav1.ConditionUnknown,
							Reason: "Pending",
						},
					},
				},
			},
			new: &fleetnetv1beta1.TrafficManagerProfile{
				Status: fleetnetv1beta1.TrafficManagerProfileStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(fleetnetv1beta1.TrafficManagerProfileConditionProgrammed),
							Status: metav1.ConditionTrue,
							Reason: "Programmed",
						},
					},
				},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldHandleTrafficManagerProfileUpdateEvent(tt.old, tt.new); got != tt.want {
				t.Errorf("shouldHandleTrafficManagerProfileUpdateEvent() = %v, want %v", got, tt.want)
			}
		})
	}
}
func TestShouldHandleInternalServiceExportUpdateEvent(t *testing.T) {
	tests := []struct {
		name string
		old  *fleetnetv1alpha1.InternalServiceExport
		new  *fleetnetv1alpha1.InternalServiceExport
		want bool
	}{
		{
			name: "specs are the same",
			old: &fleetnetv1alpha1.InternalServiceExport{
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Type:                   corev1.ServiceTypeLoadBalancer,
					PublicIPResourceID:     ptr.To("resource-id-1"),
					IsDNSLabelConfigured:   true,
					IsInternalLoadBalancer: false,
					Weight:                 ptr.To(int64(100)),
					ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
						ClusterID:       "cluster-1",
						Kind:            "Service",
						Namespace:       "default",
						Name:            "service-1",
						NamespacedName:  "default/service-1",
						ResourceVersion: "123",
						Generation:      1,
						UID:             "uid-1",
					},
				},
			},
			new: &fleetnetv1alpha1.InternalServiceExport{
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Type:                   corev1.ServiceTypeLoadBalancer,
					PublicIPResourceID:     ptr.To("resource-id-1"),
					IsDNSLabelConfigured:   true,
					IsInternalLoadBalancer: false,
					Weight:                 ptr.To(int64(100)),
					ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
						ClusterID:       "cluster-1",
						Kind:            "Service",
						Namespace:       "default",
						Name:            "service-1",
						NamespacedName:  "default/service-1",
						ResourceVersion: "123",
						Generation:      1,
						UID:             "uid-1",
					},
				},
			},
			want: false,
		},
		{
			name: "service type changed",
			old: &fleetnetv1alpha1.InternalServiceExport{
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Type:                   corev1.ServiceTypeClusterIP,
					IsDNSLabelConfigured:   true,
					IsInternalLoadBalancer: false,
				},
			},
			new: &fleetnetv1alpha1.InternalServiceExport{
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Type:                   corev1.ServiceTypeLoadBalancer,
					IsDNSLabelConfigured:   true,
					IsInternalLoadBalancer: false,
				},
			},
			want: true,
		},
		{
			name: "public IP resource ID changed",
			old: &fleetnetv1alpha1.InternalServiceExport{
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Type:                   corev1.ServiceTypeLoadBalancer,
					PublicIPResourceID:     ptr.To("resource-id-1"),
					IsDNSLabelConfigured:   true,
					IsInternalLoadBalancer: false,
				},
			},
			new: &fleetnetv1alpha1.InternalServiceExport{
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Type:                   corev1.ServiceTypeLoadBalancer,
					PublicIPResourceID:     ptr.To("resource-id-2"),
					IsDNSLabelConfigured:   true,
					IsInternalLoadBalancer: false,
				},
			},
			want: true,
		},
		{
			name: "DNS label configuration changed",
			old: &fleetnetv1alpha1.InternalServiceExport{
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Type:                   corev1.ServiceTypeLoadBalancer,
					PublicIPResourceID:     ptr.To("resource-id-1"),
					IsDNSLabelConfigured:   false,
					IsInternalLoadBalancer: false,
				},
			},
			new: &fleetnetv1alpha1.InternalServiceExport{
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Type:                   corev1.ServiceTypeLoadBalancer,
					PublicIPResourceID:     ptr.To("resource-id-1"),
					IsDNSLabelConfigured:   true,
					IsInternalLoadBalancer: false,
				},
			},
			want: true,
		},
		{
			name: "internal load balancer flag changed",
			old: &fleetnetv1alpha1.InternalServiceExport{
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Type:                   corev1.ServiceTypeLoadBalancer,
					PublicIPResourceID:     ptr.To("resource-id-1"),
					IsDNSLabelConfigured:   true,
					IsInternalLoadBalancer: false,
				},
			},
			new: &fleetnetv1alpha1.InternalServiceExport{
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Type:                   corev1.ServiceTypeLoadBalancer,
					PublicIPResourceID:     ptr.To("resource-id-1"),
					IsDNSLabelConfigured:   true,
					IsInternalLoadBalancer: true,
				},
			},
			want: true,
		},
		{
			name: "weight changed",
			old: &fleetnetv1alpha1.InternalServiceExport{
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Type:                   corev1.ServiceTypeLoadBalancer,
					PublicIPResourceID:     ptr.To("resource-id-1"),
					IsDNSLabelConfigured:   true,
					IsInternalLoadBalancer: false,
					Weight:                 ptr.To(int64(100)),
				},
			},
			new: &fleetnetv1alpha1.InternalServiceExport{
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Type:                   corev1.ServiceTypeLoadBalancer,
					PublicIPResourceID:     ptr.To("resource-id-1"),
					IsDNSLabelConfigured:   true,
					IsInternalLoadBalancer: false,
					Weight:                 ptr.To(int64(200)),
				},
			},
			want: true,
		},
		{
			name: "weight changed from nil to value",
			old: &fleetnetv1alpha1.InternalServiceExport{
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Type:                   corev1.ServiceTypeLoadBalancer,
					PublicIPResourceID:     ptr.To("resource-id-1"),
					IsDNSLabelConfigured:   true,
					IsInternalLoadBalancer: false,
					Weight:                 nil,
				},
			},
			new: &fleetnetv1alpha1.InternalServiceExport{
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Type:                   corev1.ServiceTypeLoadBalancer,
					PublicIPResourceID:     ptr.To("resource-id-1"),
					IsDNSLabelConfigured:   true,
					IsInternalLoadBalancer: false,
					Weight:                 ptr.To(int64(100)),
				},
			},
			want: true,
		},
		{
			name: "public IP resource ID changed from nil to value",
			old: &fleetnetv1alpha1.InternalServiceExport{
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Type:                   corev1.ServiceTypeLoadBalancer,
					PublicIPResourceID:     nil,
					IsDNSLabelConfigured:   true,
					IsInternalLoadBalancer: false,
				},
			},
			new: &fleetnetv1alpha1.InternalServiceExport{
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Type:                   corev1.ServiceTypeLoadBalancer,
					PublicIPResourceID:     ptr.To("resource-id-1"),
					IsDNSLabelConfigured:   true,
					IsInternalLoadBalancer: false,
				},
			},
			want: true,
		},
		{
			name: "public IP resource ID changed from value to nil",
			old: &fleetnetv1alpha1.InternalServiceExport{
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Type:                   corev1.ServiceTypeLoadBalancer,
					PublicIPResourceID:     ptr.To("resource-id-1"),
					IsDNSLabelConfigured:   true,
					IsInternalLoadBalancer: false,
				},
			},
			new: &fleetnetv1alpha1.InternalServiceExport{
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Type:                   corev1.ServiceTypeLoadBalancer,
					PublicIPResourceID:     nil,
					IsDNSLabelConfigured:   true,
					IsInternalLoadBalancer: false,
				},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldHandleInternalServiceExportUpdateEvent(tt.old, tt.new); got != tt.want {
				t.Errorf("shouldHandleInternalServiceExportUpdateEvent() = %v, want %v", got, tt.want)
			}
		})
	}
}
