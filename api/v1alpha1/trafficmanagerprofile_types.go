package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type TrafficManagerProfile struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec TrafficManagerProfileSpec `json:"spec"`
	// +optional
	Status TrafficManagerProfileStatus `json:"status,omitempty"`
}

type TrafficManagerProfileSpec struct {
	// Type of routing method.
	// +kubebuilder:validation:Enum=Weighted
	// +kubebuilder:default=Weighted
	// +optional
	RoutingMethod TrafficRoutingMethod

	// The DNS settings of the Traffic Manager profile.
	// +required
	DNSConfig *DNSConfig

	// The endpoint monitoring settings of the Traffic Manager profile.
	MonitorConfig *MonitorConfig
}

type DNSConfig struct {
	// +required
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=2147483647
	// Traffic Manager allows you to configure the TTL used in Traffic Manager DNS responses to be as low as 0 seconds
	// and as high as 2,147,483,647 seconds (the maximum range compliant with RFC-1035), enabling you to choose the value
	// that best balances the needs of your application.
	TTL *int64
}

type MonitorConfig struct {
	// The monitor interval for endpoints in this profile. This is the interval at which Traffic Manager will check the health
	// of each endpoint in this profile.
	IntervalInSeconds *int64

	// The path relative to the endpoint domain name used to probe for endpoint health.
	Path *string

	// The TCP port used to probe for endpoint health.
	Port *int64

	// The protocol (HTTP, HTTPS or TCP) used to probe for endpoint health.
	Protocol *MonitorProtocol

	// The monitor timeout for endpoints in this profile. This is the time that Traffic Manager allows endpoints in this profile
	// to response to the health check.
	TimeoutInSeconds *int64

	// The number of consecutive failed health check that Traffic Manager tolerates before declaring an endpoint in this profile
	// Degraded after the next failed health check.
	ToleratedNumberOfFailures *int64
}

type MonitorProtocol string

const (
	MonitorProtocolHTTP  MonitorProtocol = "HTTP"
	MonitorProtocolHTTPS MonitorProtocol = "HTTPS"
	MonitorProtocolTCP   MonitorProtocol = "TCP"
)

type TrafficRoutingMethod string

const (
	// TrafficRoutingMethodGeographic  TrafficRoutingMethod = "Geographic"
	// TrafficRoutingMethodMultiValue  TrafficRoutingMethod = "MultiValue"
	// TrafficRoutingMethodPerformance TrafficRoutingMethod = "Performance"
	// TrafficRoutingMethodPriority    TrafficRoutingMethod = "Priority"
	// TrafficRoutingMethodSubnet      TrafficRoutingMethod = "Subnet"

	// TrafficRoutingMethodWeighted is selected when you want to distribute traffic across a set of endpoints based on
	// their weight. Set the weight the same to distribute evenly across all endpoints.
	TrafficRoutingMethodWeighted TrafficRoutingMethod = "Weighted"
)

type TrafficManagerProfileStatus struct {
	// DNSName is the fully-qualified domain name (FQDN) of the Traffic Manager profile.
	// For example, azuresdkfornetautoresttrafficmanager3880.tmpreview.watmtest.azure-test.net
	// +optional
	DNSName *string

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

	// ProfileReasonInvalid is used with the "Programmed" when the profile or endpoint is syntactically or
	// semantically invalid.
	ProfileReasonInvalid TrafficManagerProfileConditionReason = "Invalid"

	// ProfileReasonAddressNotUsable is used with the "Programmed" condition when the generated DNS name is not usable.
	ProfileReasonAddressNotUsable TrafficManagerProfileConditionReason = "AddressNotUsable"

	// ProfileReasonPending is used with the "Programmed" when creating or updating the profile hits an internal error
	// with more detail in the message and the controller will keep retry.
	ProfileReasonPending TrafficManagerProfileConditionReason = "Pending"
)
