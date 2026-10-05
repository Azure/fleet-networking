/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,categories={fleet-networking},shortName=mclb
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=`.status.loadBalancer.ingress[0].ip`,name="External-IP",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=='Valid')].status`,name="Is-Valid",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=='Deployed')].status`,name="Is-Deployed",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date

// MultiClusterService is the Schema for creating north-south L4 load balancer to consume services across clusters.
// +kubebuilder:validation:XValidation:rule="size(self.metadata.name) < 64",message="metadata.name max length is 63"
type MultiClusterLoadBalancer struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec defines the desired state of MultiClusterLoadBalancer
	// Spec MultiClusterLoadBalancerSpec `json:"spec,omitempty"`
	// Status represents the current status of MultiClusterLoadBalancer
	// +optional
	Status MultiClusterLoadBalancerStatus `json:"status,omitempty"`
}

// MultiClusterLoadBalancerStatus contains the current status of an export.
type MultiClusterLoadBalancerStatus struct {
	// LoadBalancer contains the current status of the load-balancer,
	// if one is present.
	// +optional
	LoadBalancer v1.LoadBalancerStatus `json:"loadBalancer,omitempty" protobuf:"bytes,1,opt,name=loadBalancer"`
	// +optional
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// LoadBalancerConditionType identifies a specific condition on a MultiClusterLoadBalancer.
type LoadBalancerConditionType string

const (
	// LoadBalancerValid means that the service referenced by this service export has been recognized as valid.
	// This will be false if the service is found to be unexportable (e.g. ExternalName, not found).
	LoadBalancerValid LoadBalancerConditionType = "Valid"
)

// +kubebuilder:object:root=true

// MultiClusterLoadBalancerList contains a list of MultiClusterLoadBalancer.
type MultiClusterLoadBalancerList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	// +listType=set
	Items []MultiClusterLoadBalancer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MultiClusterLoadBalancer{}, &MultiClusterLoadBalancerList{})
}
