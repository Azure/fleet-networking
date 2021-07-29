// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// MultiClusterServiceSpec defines the desired state of MultiClusterService
type MultiClusterServiceSpec struct {
	// LabelSelector for the multicluster service. Services with the same name of MultiClusterService would be selected if the selector is not set.
	Selector metav1.LabelSelector `json:"selector,omitempty"`
	// Ports for the multicluster service.
	Ports []MultiClusterServicePort `json:"ports,omitempty"`
	// ClusterSet for the multicluster service.
	ClusterSet string `json:"clusterSet,omitempty"`
}

// MultiClusterServicePort defines the spec for MultiClusterService port
type MultiClusterServicePort struct {
	Name       string `json:"name,omitempty"`
	Protocol   string `json:"protocol,omitempty"`
	Port       int    `json:"port,omitempty"`
	TargetPort int    `json:"targetPort,omitempty"`
}

// MultiClusterServiceStatus defines the observed state of MultiClusterService
type MultiClusterServiceStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Endpoints represents a list of endpoint for the multicluster service.
	Endpoints []GlobalEndpoint `json:"endpoints,omitempty"`
	VIP       string           `json:"vip,omitempty"`
	State     string           `json:"state,omitempty"`
}

// GlobalEndpoint defines the endpoints for the multicluster service.
type GlobalEndpoint struct {
	Cluster   string   `json:"cluster,omitempty"`
	Service   string   `json:"service,omitempty"`
	IP        string   `json:"ip,omitempty"`
	Endpoints []string `json:"endpoints,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ports",type=string,JSONPath=`.spec.ports[*].port`
// +kubebuilder:printcolumn:name="VIP",type=string,JSONPath=`.status.vip`
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.state`

// MultiClusterService is the Schema for the multiClusterServices API
type MultiClusterService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MultiClusterServiceSpec   `json:"spec,omitempty"`
	Status MultiClusterServiceStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// MultiClusterServiceList contains a list of MultiClusterService
type MultiClusterServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MultiClusterService `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MultiClusterService{}, &MultiClusterServiceList{})
}
