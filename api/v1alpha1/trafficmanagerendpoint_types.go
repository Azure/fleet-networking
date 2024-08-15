package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type TrafficManagerEndpoint struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec TrafficManagerEndpointSpec `json:"spec"`
	// +optional
	Status TrafficManagerEndpointStatus `json:"status,omitempty"`
}

type TrafficManagerEndpointSpec struct {
	// +required
	// immutable
	// The profile should be created in the same namespace as the TrafficManagerEndpoint.
	Profile *string

	// The reference to the endpoint.
	// +required
	EndpointRef *TrafficManagerEndpointRef

	// +required
	Properties *TrafficManagerEndpointProperties
}

type TrafficManagerEndpointRefType string

const (
	// TrafficManagerEndpointTypeServiceImport is a group of public ip exposed by the exported service.
	// Later, we can support Service (for single cluster) and TrafficManagerProfile type.
	TrafficManagerEndpointTypeServiceImport TrafficManagerEndpointRefType = "ServiceImport"
)

type TrafficManagerEndpointRef struct {
	// Type of endpoint k8 custom resource reference. Can be "ServiceImport". Default is ServiceImport.
	// Possible value is "ServiceImport", "TrafficManagerProfile" or "Service" (for single cluster).
	// +kubebuilder:validation:Enum=ServiceImport
	// +kubebuilder:default=ServiceImport
	// +optional
	//Type TrafficManagerEndpointRefType

	// Name is the reference to the ServiceImport in the same namespace of Traffic Manager Profile if the type is "ServiceImport".
	// +required
	Name string
}

type TrafficManagerEndpointType string

const (
	// EndpointTypeAzureEndpoints are used for services hosted in Azure exposed via PublicIPAddress.
	// The publicIpAddress must have a DNS name assigned to be used in a Traffic Manager profile.
	EndpointTypeAzureEndpoints TrafficManagerEndpointType = "AzureEndpoints"
	// EndpointTypeExternalEndpoints are used for IPv4/IPv6 addresses, FQDNs, or for services hosted outside Azure.
	// These services can either be on-premises or with a different hosting provider.
	// EndpointTypeExternalEndpoints TrafficManagerEndpointType = "ExternalEndpoints"
	// EndpointTypeNestedEndpoints are used to combine Traffic Manager profiles to create more flexible traffic-routing
	// schemes to support the needs of larger, more complex deployments.
	// EndpointTypeNestedEndpoints TrafficManagerEndpointType = "NestedEndpoints"
)

type TrafficManagerEndpointProperties struct {
	// If the type is "AzureEndpoints", it means the public ip is an azure resource and must have a DNS name assigned.
	// Note: To use Traffic Manager with endpoints from other subscriptions, the controller needs to have read access to
	// the endpoint.
	// +kubebuilder:validation:Enum=AzureEndpoints
	// +kubebuilder:default=AzureEndpoints
	// +optional
	//Type TrafficManagerEndpointType

	// If Always Serve is enabled, probing for endpoint health will be disabled and endpoints will be included in the traffic
	// routing method.
	// +kubebuilder:default=false
	// +optional
	AlwaysServe bool

	// The total weight of endpoints behind the serviceImport when using the 'Weighted' traffic routing method.
	// Possible values are from 1 to 1000.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1000
	// For example, if there are two clusters exporting the service via public ip, each public ip will be configured
	// as "Weight"/2.
	Weight *int64

	// The fully-qualified DNS name or IP address of the endpoint.
	// When the endpoint reference type is ServiceImport, the controller will configure this value by looking at the
	// exported Service information.
	// +optional
	Target *string
}

type AcceptedEndpoint struct {
	// Name is a unique Azure Traffic Manager endpoint name generated by the controller.
	// +required
	Name *string

	// +optional
	Properties *TrafficManagerEndpointProperties

	// Cluster is where the endpoint is exported from.
	// +optional
	Cluster *ClusterStatus
}

type TrafficManagerEndpointStatus struct {
	// Endpoints contains a list of accepted Azure endpoints which are created or updated under the traffic manager Profile.
	// +optional
	Endpoints []AcceptedEndpoint

	// Current service state
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// TrafficManagerEndpointConditionType is a type of condition associated with a TrafficManagerEndpoint. This type
// should be used with the TrafficManagerEndpointStatus.Conditions field.
type TrafficManagerEndpointConditionType string

// TrafficManagerEndpointConditionReason defines the set of reasons that explain why a
// particular endpoints condition type has been raised.
type TrafficManagerEndpointConditionReason string

const (
	// EndpointConditionAccepted condition indicates whether endpoints have been created or updated for the profile.
	// This does not indicate whether or not the configuration has been propagated to the data plane.
	//
	// Possible reasons for this condition to be True are:
	//
	// * "Accepted"
	//
	// Possible reasons for this condition to be False are:
	//
	// * "Invalid"
	// * "Pending"
	EndpointConditionAccepted TrafficManagerEndpointConditionReason = "Accepted"

	// EndpointReasonAccepted is used with the "Accepted" condition when the condition is True.
	EndpointReasonAccepted TrafficManagerEndpointConditionReason = "Accepted"

	// EndpointReasonInvalid is used with the "Accepted" condition when one or
	// more endpoint references have an invalid or unsupported configuration
	// and cannot be configured on the Profile with more detail in the message.
	EndpointReasonInvalid TrafficManagerEndpointConditionReason = "Invalid"

	// EndpointReasonPending is used with the "Accepted" when creating or updating endpoint hits an internal error with
	// more detail in the message and the controller will keep retry.
	EndpointReasonPending TrafficManagerEndpointConditionReason = "Pending"
)
