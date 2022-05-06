// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// GlobalServiceSpec defines the desired state of GlobalService
type GlobalServiceSpec struct {
	// LabelSelector for the global service. Services with the same name of GlobalService would be selected if the selector is not set.
	Selector metav1.LabelSelector `json:"selector,omitempty"`
	// Ports for the global service.
	Ports []GlobalServicePort `json:"ports,omitempty"`
	// ClusterSet for the global service.
	ClusterSet string `json:"clusterSet,omitempty"`
}

// GlobalServicePort defines the spec for GlobalService port
type GlobalServicePort struct {
	Name       string `json:"name,omitempty"`
	Protocol   string `json:"protocol,omitempty"`
	Port       int    `json:"port,omitempty"`
	TargetPort int    `json:"targetPort,omitempty"`
}

// GlobalServiceStatus defines the observed state of GlobalService
type GlobalServiceStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Endpoints represents a list of endpoint for the global service.
	Endpoints []GlobalEndpoint `json:"endpoints,omitempty"`
	VIP       string           `json:"vip,omitempty"`
	State     string           `json:"state,omitempty"`
}

// GlobalEndpoint defines the endpoints for the global service.
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

// GlobalService is the Schema for the globalservices API
type GlobalService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GlobalServiceSpec   `json:"spec,omitempty"`
	Status GlobalServiceStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// GlobalServiceList contains a list of GlobalService
type GlobalServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GlobalService `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GlobalService{}, &GlobalServiceList{})
}
