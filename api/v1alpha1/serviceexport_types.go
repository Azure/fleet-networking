/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ServiceExportConditionType identifies a specific condition on a ServiceExport.
type ServiceExportConditionType string

const (
	// ServiceExportValid means that the service referenced by this service export has been recognized as valid.
	// This will be false if the service is found to be unexportable (e.g. ExternalName, not found).
	ServiceExportValid ServiceExportConditionType = "Valid"
	// ServiceExportConflict means that there is a conflict between two exports for the same Service.
	// When "True", the condition message should contain enough information to diagnose the conflict:
	// field(s) under contention, which cluster won, and why.
	// Users should not expect detailed per-cluster information in the conflict message.
	ServiceExportConflict ServiceExportConditionType = "Conflict"
)

// ServiceExportStatus contains the current status of an export.
type ServiceExportStatus struct {
	// +optional
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,categories={fleet-networking},shortName=svcexport
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=='Valid')].status`,name="Is-Valid",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=='Conflict')].status`,name="Is-Conflicted",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date

// ServiceExport declares that the associated service should be exported to other clusters.
// The annotation "networking.fleet.azure.com/weight" specifies the proportion of requests forwarded to the cluster
// within a serviceImport.
// The actual value is the ceiling value of a number computed as weight/(sum of all weights in the serviceImport).
// If weight is set to 0, no traffic should be forwarded for this entry.
// If unspecified, weight defaults to 1.
// The value should be in the range [0, 1000].
// +kubebuilder:validation:XValidation:rule="size(self.metadata.name) < 64",message="metadata.name max length is 63"
type ServiceExport struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// +optional
	Status ServiceExportStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ServiceExportList contains a list of ServiceExport.
type ServiceExportList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	// +listType=set
	Items []ServiceExport `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ServiceExport{}, &ServiceExportList{})
}
