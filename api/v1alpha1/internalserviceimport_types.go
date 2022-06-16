/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true

// InternalServiceImport is used by the MCS controller to import the Service to a single cluster manually,
// while ServiceImport is logical identifiers for a Service that exists in another cluster or that stretches
// across multiple clusters and it will be automatically created in the hub cluster when exporting service.
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

// InternalServiceImportList represents a list of InternalServiceImport
type InternalServiceImportList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	// List of InternalServiceImport
	// +listType=set
	Items []InternalServiceImport `json:"items"`
}

func init() {
	SchemeBuilder.Register(&InternalServiceImport{}, &InternalServiceImportList{})
}
