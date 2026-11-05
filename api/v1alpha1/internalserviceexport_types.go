/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// InternalServiceExportSpec specifies the spec of an exported Service; at this stage only the ports of an
// exported Service are sync'd.
type InternalServiceExportSpec struct {
	// A list of ports exposed by the exported Service.
	// +listType=atomic
	Ports []ServicePort `json:"ports"`
	// The reference to the source Service.
	// +kubebuilder:validation:Required
	ServiceReference ExportedObjectReference `json:"serviceReference"`
	// Type is the type of the Service in each cluster.
	Type corev1.ServiceType `json:"type,omitempty"`
	// IsDNSLabelConfigured determines if the Service has a DNS label configured.
	// A valid DNS label should be configured when the public IP address of the Service is configured as an Azure Traffic
	// Manager endpoint.
	// Reference link:
	// * https://cloud-provider-azure.sigs.k8s.io/topics/loadbalancer/
	// * https://learn.microsoft.com/en-us/azure/traffic-manager/traffic-manager-endpoint-types#azure-endpoints
	IsDNSLabelConfigured bool `json:"isDNSLabelConfigured,omitempty"`
	// IsInternalLoadBalancer determines if the Service is an internal load balancer type.
	IsInternalLoadBalancer bool `json:"isInternalLoadBalancer,omitempty"`
	// PublicIPResourceID is the Azure Resource URI of public IP. This is only applicable for Load Balancer type Services.
	PublicIPResourceID *string `json:"publicIPResourceID,omitempty"`
	// Weight is the weight of the ServiceExport.
	// If unspecified, weight defaults to 1.
	// The value is from serviceExport "networking.fleet.azure.com/weight" annotation and should be in the range [0, 1000].
	Weight *int64 `json:"weight,omitempty"`

	// ExportMode selects which fleet-networking control plane on the hub
	// consumes this export:
	//   - "L4-TrafficManager" (default): the TrafficManagerBackend
	//     reconciler picks it up and programs an Azure Traffic Manager
	//     endpoint (today's behaviour).
	//   - "L7-FrontDoor": the FrontDoorBackend reconciler (Phase 4) picks
	//     it up and programs an Azure Front Door origin backed by the
	//     Private Link Service referenced in PrivateLinkServiceResourceID.
	//
	// Sourced from the member ServiceExport's
	// "networking.fleet.azure.com/export-mode" annotation (see
	// objectmeta.ExtractExportModeFromServiceExport). The field is
	// populated by the member serviceexport reconciler; hub controllers
	// treat it as read-only. Absent value is equivalent to
	// "L4-TrafficManager" so existing v1alpha1 objects (which pre-date
	// this field) continue to route through ATM.
	//
	// +optional
	// +kubebuilder:validation:Enum=L4-TrafficManager;L7-FrontDoor
	ExportMode string `json:"exportMode,omitempty"`

	// PrivateLinkServiceResourceID is the Azure Resource URI of the
	// Private Link Service (PLS) provisioned by cloud-provider-azure for
	// the exported Service's internal load balancer. Populated only when
	// ExportMode == "L7-FrontDoor" AND the Service carries the
	// "service.beta.kubernetes.io/azure-pls-*" annotations that trigger
	// PLS creation. The hub FrontDoorBackend reconciler wires this ID as
	// an AFD private-link origin so global traffic can reach the ILB
	// without traversing a public IP.
	//
	// Format: /subscriptions/{sub}/resourceGroups/{rg}/providers/
	//         Microsoft.Network/privateLinkServices/{name}
	//
	// +optional
	PrivateLinkServiceResourceID *string `json:"privateLinkServiceResourceID,omitempty"`
}

// InternalServiceExportStatus contains the current status of an InternalServiceExport.
type InternalServiceExportStatus struct {
	// +optional
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,categories={fleet-networking},shortName=internalsvcexport
// +kubebuilder:subresource:status

// InternalServiceExport is a data transport type that member clusters in the fleet use to upload the spec of
// exported Service to the hub cluster.
type InternalServiceExport struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// +optional
	Spec InternalServiceExportSpec `json:"spec,omitempty"`
	// +optional
	Status InternalServiceExportStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// InternalServiceExportList contains a list of InternalServiceExports.
type InternalServiceExportList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	// +listType=set
	Items []InternalServiceExport `json:"items"`
}

func init() {
	SchemeBuilder.Register(&InternalServiceExport{}, &InternalServiceExportList{})
}
