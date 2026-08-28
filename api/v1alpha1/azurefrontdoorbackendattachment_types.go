/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const (
	AzureFrontDoorBackendAttachmentKind = "AzureFrontDoorBackendAttachment"
)

// AzureFrontDoorConnectivityMode selects public or Private Link origins.
// +kubebuilder:validation:Enum=Public;PrivateLink
type AzureFrontDoorConnectivityMode string

const (
	AzureFrontDoorConnectivityModePublic      AzureFrontDoorConnectivityMode = "Public"
	AzureFrontDoorConnectivityModePrivateLink AzureFrontDoorConnectivityMode = "PrivateLink"
)

// AzureFrontDoorOriginProtocol is the protocol used from AFD to an origin.
// +kubebuilder:validation:Enum=HTTP;HTTPS
type AzureFrontDoorOriginProtocol string

const (
	AzureFrontDoorOriginProtocolHTTP  AzureFrontDoorOriginProtocol = "HTTP"
	AzureFrontDoorOriginProtocolHTTPS AzureFrontDoorOriginProtocol = "HTTPS"
)

// AzureFrontDoorHealthProbeMethod is an AFD health probe method.
// +kubebuilder:validation:Enum=GET;HEAD
type AzureFrontDoorHealthProbeMethod string

const (
	AzureFrontDoorHealthProbeMethodGET  AzureFrontDoorHealthProbeMethod = "GET"
	AzureFrontDoorHealthProbeMethodHEAD AzureFrontDoorHealthProbeMethod = "HEAD"
)

// AzureFrontDoorMemberFailurePolicy controls whether one invalid member blocks the backend.
// +kubebuilder:validation:Enum=All;Partial
type AzureFrontDoorMemberFailurePolicy string

const (
	AzureFrontDoorMemberFailurePolicyAll     AzureFrontDoorMemberFailurePolicy = "All"
	AzureFrontDoorMemberFailurePolicyPartial AzureFrontDoorMemberFailurePolicy = "Partial"
)

// AzureFrontDoorBackendReference identifies a ServiceImport port in the attachment namespace.
type AzureFrontDoorBackendReference struct {
	gatewayv1.LocalObjectReference `json:",inline"`

	// Port is the ServiceImport service port consumed by the Gateway.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port"`
}

// AzureFrontDoorBackendAttachmentSpec configures one Gateway consumption of one ServiceImport port.
type AzureFrontDoorBackendAttachmentSpec struct {
	// GatewayRef identifies the Gateway that consumes this backend.
	// +required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.gatewayRef is immutable"
	GatewayRef gatewayv1.LocalObjectReference `json:"gatewayRef"`

	// BackendRef identifies a ServiceImport port in the attachment namespace.
	// +required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.backendRef is immutable"
	BackendRef AzureFrontDoorBackendReference `json:"backendRef"`

	// Connectivity selects public or Private Link origins.
	// +required
	Connectivity AzureFrontDoorConnectivitySpec `json:"connectivity"`

	// Origin configures AFD-to-origin connections.
	// +required
	Origin AzureFrontDoorOriginSpec `json:"origin"`

	// HealthProbe configures AFD origin health evaluation.
	// +kubebuilder:default={"protocol":"HTTPS","method":"HEAD","path":"/healthz","intervalSeconds":30,"sampleSize":4,"successfulSamplesRequired":3}
	// +optional
	HealthProbe AzureFrontDoorHealthProbeSpec `json:"healthProbe,omitempty"`

	// Traffic configures defaults for member-cluster origins.
	// +kubebuilder:default={"defaultPriority":1,"defaultWeight":1000}
	// +optional
	Traffic AzureFrontDoorTrafficSpec `json:"traffic,omitempty"`

	// MemberFailurePolicy controls whether invalid members block all origins.
	// +kubebuilder:default=Partial
	MemberFailurePolicy AzureFrontDoorMemberFailurePolicy `json:"memberFailurePolicy,omitempty"`
}

// AzureFrontDoorConnectivitySpec selects the origin connectivity contract.
type AzureFrontDoorConnectivitySpec struct {
	// Mode selects public or Private Link origins.
	// +required
	Mode AzureFrontDoorConnectivityMode `json:"mode"`

	// PrivateLink configures Private Link origin behavior.
	// +optional
	PrivateLink *AzureFrontDoorPrivateLinkSpec `json:"privateLink,omitempty"`
}

// AzureFrontDoorPrivateLinkSpec configures the initial manual approval workflow.
type AzureFrontDoorPrivateLinkSpec struct {
	// Approval is Manual in the initial API.
	// +kubebuilder:validation:Enum=Manual
	// +kubebuilder:default=Manual
	Approval string `json:"approval,omitempty"`

	// RegionSelection determines how the managed private endpoint region is selected.
	// +kubebuilder:validation:Enum=MemberRegion;ClosestSupported
	// +kubebuilder:default=ClosestSupported
	RegionSelection string `json:"regionSelection,omitempty"`
}

// AzureFrontDoorOriginSpec configures connections from AFD to every selected member origin.
type AzureFrontDoorOriginSpec struct {
	// Protocol is the origin connection protocol.
	// +kubebuilder:default=HTTPS
	Protocol AzureFrontDoorOriginProtocol `json:"protocol,omitempty"`

	// HostHeader is the HTTP Host header sent to the origin.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	HostHeader string `json:"hostHeader"`

	// CertificateSubjectNameCheck enables origin certificate name validation.
	// +kubebuilder:default=true
	CertificateSubjectNameCheck *bool `json:"certificateSubjectNameCheck,omitempty"`
}

// AzureFrontDoorHealthProbeSpec configures one origin group's health probe.
// +kubebuilder:validation:XValidation:rule="self.successfulSamplesRequired <= self.sampleSize",message="successfulSamplesRequired cannot exceed sampleSize"
type AzureFrontDoorHealthProbeSpec struct {
	// Protocol is the probe protocol.
	// +kubebuilder:default=HTTPS
	Protocol AzureFrontDoorOriginProtocol `json:"protocol,omitempty"`

	// Method is the probe method.
	// +kubebuilder:default=HEAD
	Method AzureFrontDoorHealthProbeMethod `json:"method,omitempty"`

	// Path is an absolute HTTP path.
	// +kubebuilder:default="/healthz"
	// +kubebuilder:validation:Pattern=`^/`
	// +kubebuilder:validation:MaxLength=1024
	Path string `json:"path,omitempty"`

	// IntervalSeconds is the interval between probes.
	// +kubebuilder:validation:Enum=30;60;120;180;240
	// +kubebuilder:default=30
	IntervalSeconds int32 `json:"intervalSeconds,omitempty"`

	// SampleSize is the number of recent samples used for health evaluation.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=255
	// +kubebuilder:default=4
	SampleSize int32 `json:"sampleSize,omitempty"`

	// SuccessfulSamplesRequired is the minimum successful sample count.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=255
	// +kubebuilder:default=3
	SuccessfulSamplesRequired int32 `json:"successfulSamplesRequired,omitempty"`
}

// AzureFrontDoorTrafficSpec configures member origin defaults.
type AzureFrontDoorTrafficSpec struct {
	// DefaultPriority is the origin priority.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=5
	// +kubebuilder:default=1
	DefaultPriority int32 `json:"defaultPriority,omitempty"`

	// DefaultWeight is the origin weight.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1000
	// +kubebuilder:default=1000
	DefaultWeight int32 `json:"defaultWeight,omitempty"`
}

// AzureFrontDoorBackendAttachmentStatus describes reference resolution and acceptance.
type AzureFrontDoorBackendAttachmentStatus struct {
	AzureFrontDoorResourceStatus `json:",inline"`

	// Gateway records the resolved Gateway identity.
	// +optional
	Gateway *AzureFrontDoorResolvedReference `json:"gateway,omitempty"`

	// Backend records the resolved ServiceImport identity.
	// +optional
	Backend *AzureFrontDoorResolvedReference `json:"backend,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,categories={fleet-networking},shortName=afdba
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=`.spec.gatewayRef.name`,name="Gateway",type=string
// +kubebuilder:printcolumn:JSONPath=`.spec.backendRef.name`,name="Backend",type=string
// +kubebuilder:printcolumn:JSONPath=`.spec.backendRef.port`,name="Port",type=integer
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=='Accepted')].status`,name="Accepted",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date
// +kubebuilder:validation:XValidation:rule="self.spec.gatewayRef.group == 'gateway.networking.k8s.io' && self.spec.gatewayRef.kind == 'Gateway'",message="spec.gatewayRef must reference a Gateway"
// +kubebuilder:validation:XValidation:rule="self.spec.backendRef.group == 'networking.fleet.azure.com' && self.spec.backendRef.kind == 'ServiceImport'",message="spec.backendRef must reference a ServiceImport"
// +kubebuilder:validation:XValidation:rule="self.spec.connectivity.mode == 'PrivateLink' ? has(self.spec.connectivity.privateLink) : !has(self.spec.connectivity.privateLink)",message="privateLink must be set only when connectivity mode is PrivateLink"
// +kubebuilder:validation:XValidation:rule="self.spec.connectivity.mode != 'PrivateLink' || self.spec.origin.protocol != 'HTTPS' || self.spec.origin.certificateSubjectNameCheck == true",message="Private Link HTTPS origins require certificate subject-name validation"

// AzureFrontDoorBackendAttachment binds one Gateway to one ServiceImport port.
type AzureFrontDoorBackendAttachment struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec AzureFrontDoorBackendAttachmentSpec `json:"spec"`

	// +optional
	Status AzureFrontDoorBackendAttachmentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AzureFrontDoorBackendAttachmentList contains AzureFrontDoorBackendAttachment objects.
type AzureFrontDoorBackendAttachmentList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	// +listType=set
	Items []AzureFrontDoorBackendAttachment `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AzureFrontDoorBackendAttachment{}, &AzureFrontDoorBackendAttachmentList{})
}
