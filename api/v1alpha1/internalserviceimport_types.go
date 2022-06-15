/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true

// InternalServiceImport describes a service imported from clusters in a ClusterSet.
// Different from ServiceImport, InternalServiceImport is used only to represent
// the imported service from a single cluster, and will be merged to ServiceImport
// carrying all exported services from namespace sameness.
type InternalServiceImport struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// spec defines the behavior of a ServiceImport.
	// +optional
	Spec ServiceImportSpec `json:"spec,omitempty"`
	// status contains information about the exported services that form
	// the multi-cluster service referenced by this ServiceImport.
	// +optional
	Status ServiceImportStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// InternalServiceImportList represents a list of endpoint slices
type InternalServiceImportList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	// List of endpoint slices
	// +listType=set
	Items []InternalServiceImport `json:"items"`
}

func init() {
	SchemeBuilder.Register(&InternalServiceImport{}, &InternalServiceImportList{})
}
