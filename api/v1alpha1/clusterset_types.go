// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ClusterSetSpec defines the desired state of ClusterSet
type ClusterSetSpec struct {
	Clusters []string `json:"clusters,omitempty"`
}

// ClusterSetStatus defines the observed state of ClusterSet
type ClusterSetStatus struct {
	ClusterStatuses []ClusterStatus `json:"clusterStatus,omitempty"`
}

// ClusterStatus defines the cluster status
type ClusterStatus struct {
	Name   string `json:"name,omitempty"`
	State  string `json:"state,omitempty"`
	Reason string `json:"reason,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ClusterSet is the Schema for the clustersets API
type ClusterSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterSetSpec   `json:"spec,omitempty"`
	Status ClusterSetStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ClusterSetList contains a list of ClusterSet
type ClusterSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterSet `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterSet{}, &ClusterSetList{})
}
