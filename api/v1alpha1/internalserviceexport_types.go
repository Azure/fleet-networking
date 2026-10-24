/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// InternalServiceExportSpec specifies the member Service information transported to the hub.
type InternalServiceExportSpec struct {
	// A list of ports exposed by the exported Service.
	// +listType=atomic
	Ports []ServicePort `json:"ports"`
	// The reference to the source Service.
	// +kubebuilder:validation:Required
	ServiceReference ExportedObjectReference `json:"serviceReference"`
	// Type is the type of the Service in each cluster.
	Type corev1.ServiceType `json:"type,omitempty"`
	// IsDNSLabelConfigured determines if the Service has a DNS label configured.
	// A valid DNS label should be configured when the public IP address of the Service is configured as an Azure Traffic
	// Manager endpoint.
	// Reference link:
	// * https://cloud-provider-azure.sigs.k8s.io/topics/loadbalancer/
	// * https://learn.microsoft.com/en-us/azure/traffic-manager/traffic-manager-endpoint-types#azure-endpoints
	IsDNSLabelConfigured bool `json:"isDNSLabelConfigured,omitempty"`
	// IsInternalLoadBalancer determines if the Service is an internal load balancer type.
	IsInternalLoadBalancer bool `json:"isInternalLoadBalancer,omitempty"`
	// PublicIPResourceID is the Azure Resource URI of public IP. This is only applicable for Load Balancer type Services.
	PublicIPResourceID *string `json:"publicIPResourceID,omitempty"`
	// Weight is the weight of the ServiceExport.
	// If unspecified, weight defaults to 1.
	// The value is from serviceExport "networking.fleet.azure.com/weight" annotation and should be in the range [0, 1000].
	Weight *int64 `json:"weight,omitempty"`
	// PrivateLinkOrigin is optional member-discovered private ingress metadata.
	// Absence means unknown, including exports from older members.
	// +optional
	PrivateLinkOrigin *PrivateLinkOrigin `json:"privateLinkOrigin,omitempty"`
}

// PrivateLinkOrigin describes the private endpoint behind an exported member Service.
// Ready reports discovery/provisioning only, not approval of any AFD connection.
type PrivateLinkOrigin struct {
	// ResourceID identifies the PLS bound to the Service's internal LB frontend.
	// +optional
	ResourceID string `json:"resourceID,omitempty"`
	// Location is the Azure region of the PLS.
	// +optional
	Location string `json:"location,omitempty"`
	// InternalIP is the Service's verified internal LB frontend address.
	// +optional
	InternalIP string `json:"internalIP,omitempty"`
	// Ready requires a uniquely identified, provisioned internal frontend and PLS.
	Ready bool `json:"ready"`
	// Reason explains discovery readiness without carrying raw Azure error responses.
	Reason string `json:"reason"`
}

// InternalServiceExportStatus contains the current status of an InternalServiceExport.
type InternalServiceExportStatus struct {
	// +optional
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,categories={fleet-networking},shortName=internalsvcexport
// +kubebuilder:subresource:status

// InternalServiceExport is a data transport type that member clusters in the fleet use to upload the spec of
// exported Service to the hub cluster.
type InternalServiceExport struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// +optional
	Spec InternalServiceExportSpec `json:"spec,omitempty"`
	// +optional
	Status InternalServiceExportStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// InternalServiceExportList contains a list of InternalServiceExports.
type InternalServiceExportList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	// +listType=set
	Items []InternalServiceExport `json:"items"`
}

func init() {
	SchemeBuilder.Register(&InternalServiceExport{}, &InternalServiceExportList{})
}
