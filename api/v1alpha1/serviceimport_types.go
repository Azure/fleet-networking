/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,categories={fleet-networking},shortName=svcimport
// +kubebuilder:subresource:status

// ServiceImport describes a service imported from clusters in a ClusterSet.
// +kubebuilder:validation:XValidation:rule="size(self.metadata.name) < 64",message="metadata.name max length is 63"
type ServiceImport struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// status contains information about the exported services that form
	// the multi-cluster service referenced by this ServiceImport.
	// +optional
	Status ServiceImportStatus `json:"status,omitempty"`
}

// ServiceImportType designates the type of a ServiceImport
type ServiceImportType string

const (
	// ClusterSetIP are only accessible via the ClusterSet IP.
	ClusterSetIP ServiceImportType = "ClusterSetIP"
	// Headless services allow backend pods to be addressed directly.
	Headless ServiceImportType = "Headless"
)

// ServicePort represents the port on which the service is exposed.
type ServicePort struct {
	// The name of this port within the service. This must be a DNS_LABEL.
	// All ports within a ServiceSpec must have unique names. When considering the endpoints for a Service,
	// this must match the 'name' field in the EndpointPort.
	// Optional if only one ServicePort is defined on this service.
	// +optional
	Name string `json:"name,omitempty"`

	// The IP protocol for this port. Supports "TCP", "UDP", and "SCTP".
	// Default is TCP.
	// +kubebuilder:validation:Enum:=TCP;UDP;SCTP
	Protocol corev1.Protocol `json:"protocol,omitempty"`

	// The application protocol for this port.
	// This field follows standard Kubernetes label syntax.
	// Un-prefixed names are reserved for IANA standard service names (as per
	// RFC-6335 and http://www.iana.org/assignments/service-names).
	// Non-standard protocols should use prefixed names such as
	// mycompany.com/my-custom-protocol.
	// Field can be enabled with ServiceAppProtocol feature gate.
	// +optional
	AppProtocol *string `json:"appProtocol,omitempty"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// The port that will be exposed by this service.
	Port int32 `json:"port"`

	// The port to access on the pods targeted by the service.
	// +optional
	TargetPort intstr.IntOrString `json:"targetPort,omitempty"`
}

// ToServicePort converts ServicePort to a K8 ServicePort.
func (in *ServicePort) ToServicePort() corev1.ServicePort {
	return corev1.ServicePort{
		Name:        in.Name,
		Protocol:    in.Protocol,
		AppProtocol: in.AppProtocol,
		Port:        in.Port,
		TargetPort:  in.TargetPort,
	}
}

// ServiceImportStatus describes derived state of an imported service.
type ServiceImportStatus struct {
	// ip will be used as the VIP for this service when type is ClusterSetIP.
	// +kubebuilder:validation:MaxItems:=1
	// +optional
	IPs []string `json:"ips,omitempty"`
	// type defines the type of this service.
	// Must be ClusterSetIP or Headless.
	// +kubebuilder:validation:Enum=ClusterSetIP;Headless
	// +optional
	Type ServiceImportType `json:"type,omitempty"`
	// Supports "ClientIP" and "None". Used to maintain session affinity.
	// Enable client IP based session affinity.
	// Must be ClientIP or None.
	// Defaults to None.
	// Ignored when type is Headless
	// More info: https://kubernetes.io/docs/concepts/services-networking/service/#virtual-ips-and-service-proxies
	// +optional
	SessionAffinity corev1.ServiceAffinity `json:"sessionAffinity,omitempty"`
	// sessionAffinityConfig contains session affinity configuration.
	// +optional
	SessionAffinityConfig *corev1.SessionAffinityConfig `json:"sessionAffinityConfig,omitempty"`

	// +listType=atomic
	// +optional
	Ports []ServicePort `json:"ports,omitempty"`

	// clusters is the list of exporting clusters from which this service was derived.
	// +optional
	// +patchStrategy=merge
	// +patchMergeKey=cluster
	// +listType=map
	// +listMapKey=cluster
	Clusters []ClusterStatus `json:"clusters,omitempty"`
}

// ClusterStatus contains service configuration mapped to a specific source cluster.
type ClusterStatus struct {
	// cluster is the name of the exporting cluster. Must be a valid RFC-1123 DNS label.
	Cluster string `json:"cluster"`
}

// +kubebuilder:object:root=true

// ServiceImportList contains a list of ServiceImport.
type ServiceImportList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata.
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	// List of ServiceImport.
	// +listType=set
	Items []ServiceImport `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ServiceImport{}, &ServiceImportList{})
}
