/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Endpoint includes all exported addresses from a logical backend.
type Endpoint struct {
	// Addresses of the Endpoint.
	// Addresses should be interpreted per its owner EndpointSliceExport's addressType field. This field contains
	// at least one address and at maximum 100; for more information about this constraint,
	// see https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.19/#endpoint-v1beta1-discovery-k8s-io.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems:1
	// +kubebuilder:validation:MaxItems:100
	// +kubebuilder:validation:UniqueItems:=true
	Addresses []string `json:"addresses"`
}

// EndpointSliceExportSpec specifies the spec of an exported EndpointSlice.
type EndpointSliceExportSpec struct {
	// The type of addresses carried by this EndpointSliceExport.
	// At this stage only IPv4 addresses are supported.
	// +kubebuilder:validation:Enum:="IPv4"
	// +kubebuilder:default:="IPv4"
	AddressType discoveryv1.AddressType `json:"addressType"`
	// A list of unique endpoints in the exported EndpointSlice.
	// +kubebuilder:validation:Required
	// +listType=atomic
	Endpoints []Endpoint `json:"endpoints"`
	// The list of ports exported by each endpoint in this EndpointSliceExport. Each port must have a unique name.
	// When the field is empty, it indicates that there are no defined ports. When a port is defined with a nil
	// port value, it indicates that all ports are exported. Each slice may include a maximum of 100 ports.
	// +optional
	// +listType=atomic
	Ports []discoveryv1.EndpointPort `json:"ports"`
	// The reference to the source EndpointSlice.
	// +kubebuilder:validation:Required
	EndpointSliceReference ExportedObjectReference `json:"endpointSliceReference"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,categories={fleet-networking}
// +kubebuilder:subresource:status

// EndpointSliceExport is a data transport type that member clusters in the fleet use to upload the spec of an
// EndpointSlice to the hub cluster.
type EndpointSliceExport struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// +kubebuilder:validation:Required
	Spec EndpointSliceExportSpec `json:"spec"`
}

// +kubebuilder:object:root=true

// EndpointSliceExportList contains a list of EndpointSliceExports.
type EndpointSliceExportList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	// +listType=set
	Items []EndpointSliceExport `json:"items"`
}

func init() {
	SchemeBuilder.Register(&EndpointSliceExport{}, &EndpointSliceExportList{})
}
