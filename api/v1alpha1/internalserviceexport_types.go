/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// InternalServiceExportSpec specifies the spec of an exported Service; at this stage only the ports of an
// exported Service are sync'd.
type InternalServiceExportSpec struct {
	// A list of ports exposed by the exported Service.
	// +listType=atomic
	Ports []ServicePort `json:"ports"`
	// The reference to the source Service.
	// +kubebuilder:validation:Required
	ServiceReference ExportedObjectReference `json:"serviceReference"`
	// Type is the type of the Service in each cluster.
	Type corev1.ServiceType `json:"type,omitempty"`
	// IsInternalLoadBalancer determines if the Service is an internal load balancer type.
	IsInternalLoadBalancer bool `json:"isInternalLoadBalancer,omitempty"`
	// IsDNSLabelConfigured determines if the Service has a DNS label configured.
	IsDNSLabelConfigured bool `json:"isDNSLabelConfigured,omitempty"`
	// ExternalIPResourceID is the Azure Resource URI of external IP. This is only applicable for Load Balancer type Services.
	ExternalIPResourceID *string `json:"externalIPResourceID,omitempty"`
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
