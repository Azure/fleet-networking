/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// InternalServiceExportSpec specifies the spec of an exported Service; at this stage only the ports of an
// exported Service are sync'd.
type InternalServiceExportSpec struct {
	// A list of ports exposed by the exported Service.
	// +listType=atomic
	// +optional
	Ports []ServicePort `json:"ports"`
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
