/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MultiClusterServiceSpec defines the desired state of MultiClusterService.
type MultiClusterServiceSpec struct {
	// ServiceImport is the reference to the Service with the same name exported in the member clusters.
	ServiceImport ServiceImportRef `json:"serviceImport,omitempty"`
}

// ServiceImportRef is the reference to the ServiceImport. To consume multi-cluster service, users are expected to use
// ServiceImport. When mcs controller sees the MCS definition, the ServiceImport will be created in the importing
// cluster to represent the multi-cluster service.
type ServiceImportRef struct {
	// Name is the name of the referent.
	//
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^([a-z]([-a-z0-9]*[a-z0-9])?)$`
	// +required
	Name string `json:"name"`
}

// MultiClusterServiceStatus represents the current status of a multi-cluster service.
type MultiClusterServiceStatus struct {
	// LoadBalancerStatus represents the status of a load-balancer.
	// if one is present.
	// +optional
	LoadBalancer corev1.LoadBalancerStatus `json:"loadBalancer,omitempty"`

	// Current service state
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// MultiClusterServiceConditionType identifies a specific condition.
type MultiClusterServiceConditionType string

const (
	// MultiClusterServiceValid means that the ServiceImported referenced by this
	// multi-cluster service and its configurations have been recognized as valid by a mcs-controller.
	// This will be false if the ServiceImport is not found in the hub cluster.
	MultiClusterServiceValid MultiClusterServiceConditionType = "Valid"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,categories={fleet-networking},shortName=mcs
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=`.spec.serviceImport.name`,name="Service-Import",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.loadBalancer.ingress[0].ip`,name="External-IP",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=='Valid')].status`,name="Is-Valid",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date

// MultiClusterService is the Schema for creating north-south L4 load balancer to consume services across clusters.
type MultiClusterService struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec MultiClusterServiceSpec `json:"spec"`
	// +optional
	Status MultiClusterServiceStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// MultiClusterServiceList contains a list of MultiClusterService.
type MultiClusterServiceList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	// +listType=set
	Items []MultiClusterService `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MultiClusterService{}, &MultiClusterServiceList{})
}
