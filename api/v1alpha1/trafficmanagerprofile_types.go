package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,categories={fleet-networking},shortName=tmp
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=`.status.dnsName`,name="DNS-Name",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=='Programmed')].status`,name="Is-Programmed",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date

// TrafficManagerProfile is used to manage a simple Azure Traffic Manager Profile using cloud native way.
// https://learn.microsoft.com/en-us/azure/traffic-manager/traffic-manager-overview
type TrafficManagerProfile struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of TrafficManagerProfile.
	Spec TrafficManagerProfileSpec `json:"spec"`

	// The observed status of TrafficManagerProfile.
	// +optional
	Status TrafficManagerProfileStatus `json:"status,omitempty"`
}

// TrafficManagerProfileSpec defines the desired state of TrafficManagerProfile.
type TrafficManagerProfileSpec struct {
	// The endpoint monitoring settings of the Traffic Manager profile.
	// +optional
	MonitorConfig *MonitorConfig `json:"monitorConfig,omitempty"`

	// Namespaces indicates namespaces from which TrafficManagerBackend may be attached to this
	// Profile. This is restricted to the namespace of this Profile by default.
	// +optional
	// +kubebuilder:default={from: Same}
	Namespaces *BackendNamespaces `json:"namespaces,omitempty"`
}

// BackendNamespaces indicate which namespaces TrafficManagerBackend should be selected from.
type BackendNamespaces struct {
	// From indicates where TrafficManagerBackend will be selected for this Profile. Possible
	// values are:
	//
	// * All: TrafficManagerBackends in all namespaces may be used by this Profile.
	// * Selector: TrafficManagerBackends in namespaces selected by the selector may be used by
	//   this Profile.
	// * Same: Only TrafficManagerBackends in the same namespace may be used by this Profile.
	// +optional
	// +kubebuilder:default=Same
	From *FromNamespaces `json:"from,omitempty"`

	// Selector must be specified when From is set to "Selector". In that case,
	// only TrafficManagerBackends in Namespaces matching this Selector will be selected by this
	// Profile. This field is ignored for other values of "From".
	// +optional
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
}

// FromNamespaces specifies namespace from which TrafficManagerBackend may be attached to a Profile.
//
// +kubebuilder:validation:Enum=All;Selector;Same
type FromNamespaces string

const (
	// NamespacesFromAll defines TrafficManagerBackends in all namespaces may be attached to this Profile.
	NamespacesFromAll FromNamespaces = "All"
	// NamespacesFromSelector defines that only TrafficManagerBackends in namespaces selected by the selector may be
	// attached to this Profile.
	NamespacesFromSelector FromNamespaces = "Selector"
	// NamespacesFromSame defines that only TrafficManagerBackends in the same namespace as the Profile may be attached
	// to this Profile.
	NamespacesFromSame FromNamespaces = "Same"
)

// MonitorConfig defines the endpoint monitoring settings of the Traffic Manager profile.
// https://learn.microsoft.com/en-us/azure/traffic-manager/traffic-manager-monitoring
type MonitorConfig struct {
	// The monitor interval for endpoints in this profile. This is the interval at which Traffic Manager will check the health
	// of each endpoint in this profile.
	// You can specify two values here: 30 seconds (normal probing) and 10 seconds (fast probing).
	// +optional
	// +kubebuilder:default=30
	IntervalInSeconds *int64 `json:"intervalInSeconds,omitempty"`

	// The path relative to the endpoint domain name used to probe for endpoint health.
	// +optional
	// +kubebuilder:default="/"
	Path *string `json:"path,omitempty"`

	// The TCP port used to probe for endpoint health.
	// +optional
	// +kubebuilder:default=80
	Port *int64 `json:"port,omitempty"`

	// The protocol (HTTP, HTTPS or TCP) used to probe for endpoint health.
	// +kubebuilder:validation:Enum=HTTP;HTTPS;TCP
	// +kubebuilder:default=HTTP
	// +optional
	Protocol *TrafficManagerMonitorProtocol `json:"protocol,omitempty"`

	// The monitor timeout for endpoints in this profile. This is the time that Traffic Manager allows endpoints in this profile
	// to response to the health check.
	// +optional
	// * If the IntervalInSeconds is set to 30 seconds, then you can set the Timeout value between 5 and 10 seconds.
	//   If no value is specified, it uses a default value of 10 seconds.
	// * If the IntervalInSeconds is set to 10 seconds, then you can set the Timeout value between 5 and 9 seconds.
	//   If no Timeout value is specified, it uses a default value of 9 seconds.
	TimeoutInSeconds *int64 `json:"timeoutInSeconds,omitempty"`

	// The number of consecutive failed health check that Traffic Manager tolerates before declaring an endpoint in this profile
	// Degraded after the next failed health check.
	// +optional
	// +kubebuilder:default=3
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=9
	ToleratedNumberOfFailures *int64 `json:"toleratedNumberOfFailures,omitempty"`
}

// TrafficManagerMonitorProtocol defines the protocol used to probe for endpoint health.
type TrafficManagerMonitorProtocol string

const (
	MonitorProtocolHTTP  TrafficManagerMonitorProtocol = "HTTP"
	MonitorProtocolHTTPS TrafficManagerMonitorProtocol = "HTTPS"
	MonitorProtocolTCP   TrafficManagerMonitorProtocol = "TCP"
)

type TrafficManagerProfileStatus struct {
	// DNSName is the fully-qualified domain name (FQDN) of the Traffic Manager profile.
	// For example, "azuresdkfornetautoresttrafficmanager3880.tmpreview.watmtest.azure-test.net"
	// +optional
	DNSName *string `json:"dnsName,omitempty"`

	// Current service state
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// TrafficManagerProfileConditionType is a type of condition associated with a
// Traffic Manager Profile. This type should be used with the TrafficManagerProfileStatus.Conditions
// field.
type TrafficManagerProfileConditionType string

// TrafficManagerProfileConditionReason defines the set of reasons that explain why a
// particular profile condition type has been raised.
type TrafficManagerProfileConditionReason string

const (
	// ProfileConditionProgrammed condition indicates whether a profile has been generated that is assumed to be ready
	// soon in the underlying data plane. This does not indicate whether or not the configuration has been propagated
	// to the data plane.
	//
	// It is a positive-polarity summary condition, and so should always be
	// present on the resource with ObservedGeneration set.
	//
	// Possible reasons for this condition to be True are:
	//
	// * "Programmed"
	//
	// Possible reasons for this condition to be False are:
	//
	// * "Invalid"
	// * "AddressNotUsable"
	// * "Pending"
	ProfileConditionProgrammed TrafficManagerProfileConditionType = "Programmed"

	// ProfileReasonProgrammed is used with the "Programmed" condition when the condition is true.
	ProfileReasonProgrammed TrafficManagerProfileConditionReason = "Programmed"

	// ProfileReasonInvalid is used with the "Programmed" when the profile is syntactically or semantically invalid.
	ProfileReasonInvalid TrafficManagerProfileConditionReason = "Invalid"

	// ProfileReasonAddressNotUsable is used with the "Programmed" condition when the generated DNS name is not usable.
	ProfileReasonAddressNotUsable TrafficManagerProfileConditionReason = "AddressNotUsable"

	// ProfileReasonPending is used with the "Programmed" when creating or updating the profile hits an internal error
	// with more detail in the message and the controller will keep retry.
	ProfileReasonPending TrafficManagerProfileConditionReason = "Pending"
)

//+kubebuilder:object:root=true

// TrafficManagerProfileList contains a list of TrafficManagerProfile.
type TrafficManagerProfileList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	// +listType=set
	Items []TrafficManagerProfile `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TrafficManagerProfile{}, &TrafficManagerProfileList{})
}
