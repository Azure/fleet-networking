/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	apicorev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// supportedProtocol is the type alias for supported Protocol string values; the alias is defined specifically for the
// purpose of enforcing correct enums on the Protocol field of ServicePort struct.
// +kubebuilder:validation:Enum="TCP";"UDP";"SCTP"
type supportedProtocol string

// ServicePort specifies a list of ports a Service exposes.
type ServicePort struct {
	// The name of the exported port in this Service.
	// +optional
	Name string `json:"name,omitempty"`
	// The IP protocol for this exported port; its value must be one of TCP, UDP, or SCTP and it defaults to TCP.
	// +kubebuilder:default:="TCP"
	// +optional
	Protocol supportedProtocol `json:"protocol,omitempty"`
	// The application protocol for this port; this field follows standard Kubernetes label syntax.
	// Un-prefixed names are reserved for IANA standard service names (as per RFC-6335 and
	// http://www.iana.org/assignments/service-names).
	// Non-standard protocols should use prefixed names such as example.com/protocol.
	// +optional
	AppProtocol string `json:"appProtocol,omitempty"`
	// The exported port.
	// +kubebuilder:validation:Minimum:=0
	// +kubebuilder:validation:Maximum:=65535
	// +kubebuilder:validation:Required
	Port int32 `json:"port"`
	// The number or name of the target port.
	// +kubebuilder:validation:Required
	TargetPort intstr.IntOrString `json:"targetPort"`
}

// supportedIPFamily is the type alias for supported IP family string values; the alias is defined specifically for the
// purpose of enforcing correct enums on the IPFamilies field of InternalServiceExportSpec struct. Note that at this
// stage only IPv4 addresses are accepted.
// +kubebuilder:validation:Enum="IPv4"
type supportedIPFamily string

// InternalServiceExportSpec specifies the spec of an exported Service.
type InternalServiceExportSpec struct {
	// The ID of the cluster where the Service is exported.
	// +kubebuilder:validation:Required
	OriginClusterID string `json:"originClusterId"`
	// The type of the exposed Service; only ClusterIP, NodePort, and LoadBalancer typed Services can be exported.
	// +kubebuilder:validation:Required
	Type apicorev1.ServiceType `json:"type"`
	// A list of ports exposed by the exported Service.
	// +listType=atomic
	// +kubebuilder:validation:Required
	Ports []ServicePort `json:"ports"`
	// A list of IP addresses where the Service accepts traffic, e.g. external load balancer IPs.
	// +listType=atomic
	// +optional
	ExternalIPs []string `json:"externalIps,omitempty"`
	// A list of IP families assigned to the exported Service; at this stage only IPv4 is supported.
	// +listType=atomic
	// +kubebuilder:default:={"IPv4"}
	// +kubebuilder:validation:UniqueItems:=true
	// +optional
	IPFamilies []supportedIPFamily `json:"ipFamilies,omitempty"`
	// The session affinity setting for the Service; accepted values are ClientIP or None.
	// +kubebuilder:validation:Enum="ClientIP";"None"
	// +optional
	SessionAffinity apicorev1.ServiceAffinity `json:"sessionAffinity"`
	// The configuration of session affinity.
	// +optional
	SessionAffinityConfig *apicorev1.SessionAffinityConfig `json:"sessionAffinityConfig"`
}

//+kubebuilder:object:root=true

// InternalServiceExport is a data transport type that member clusters in the fleet use to upload the spec of
// exported Service to the hub cluster.
type InternalServiceExport struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// +kubebuilder:validation:Required
	Spec InternalServiceExportSpec `json:"spec,omitempty"`
}

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
