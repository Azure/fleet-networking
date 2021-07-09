// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// AKSClusterSpec defines the desired state of AKSCluster
type AKSClusterSpec struct {
	// ResourceID refers to an existing AKS cluster.
	ResourceID string `json:"resourceId,omitempty"`

	// ManagedCluster refers to a new AKS cluster. If the cluster has already been there, existing cluster would be updated per the spec.
	ManagedCluster *ManagedCluster `json:"managedCluster,omitempty"`

	// The secret name for kubeconfig which could be applied for any Kubernetes cluster running on Azure (the secret data key should be "kubeconfig").
	KubeConfigSecret string `json:"kubeConfigSecret,omitempty"`
}

// ManagedCluster defines the AKS cluster spec.
type ManagedCluster struct {
	// TODO: add managedCluster spec
}

// AKSClusterStatus defines the observed state of AKSCluster
type AKSClusterStatus struct {
	State  string `json:"state,omitempty"`
	Reason string `json:"reason,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.state`
//+kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.reason`

// AKSCluster is the Schema for the aksclusters API
type AKSCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AKSClusterSpec   `json:"spec,omitempty"`
	Status AKSClusterStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// AKSClusterList contains a list of AKSCluster
type AKSClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AKSCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AKSCluster{}, &AKSClusterList{})
}
