/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,categories={fleet-networking}

// EndpointSliceImport is a data transport type that hub cluster uses to distribute exported EndpointSlices
// to member clusters.
type EndpointSliceImport struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// +kubebuilder:validation:Required
	Spec EndpointSliceExportSpec `json:"spec"`
}

// +kubebuilder:object:root=true

// EndpointSliceImportList contains a list of EndpointSliceExports.
type EndpointSliceImportList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	// +listType=set
	Items []EndpointSliceImport `json:"items"`
}

func init() {
	SchemeBuilder.Register(&EndpointSliceImport{}, &EndpointSliceImportList{})
}
