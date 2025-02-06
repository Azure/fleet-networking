package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	TrafficManagerProfileKind = "TrafficManagerProfile"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,categories={fleet-networking},shortName=tmp
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=`.status.dnsName`,name="DNS-Name",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=='Programmed')].status`,name="Is-Programmed",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date

// TrafficManagerProfile is used to manage a simple Azure Traffic Manager Profile using cloud native way.
// https://learn.microsoft.com/en-us/azure/traffic-manager/traffic-manager-overview
// +kubebuilder:validation:XValidation:rule="size(self.metadata.name) < 64",message="metadata.name max length is 63"
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
// For now, only the "Weighted" traffic routing method is supported.
type TrafficManagerProfileSpec struct {
	// The name of the resource group to contain the Azure Traffic Manager resource corresponding to this profile.
	// When this profile is created, updated, or deleted, the corresponding traffic manager with the same name will be created, updated, or deleted
	// in the specified resource group.
	// Reference link: https://learn.microsoft.com/en-us/azure/azure-resource-manager/management/resource-name-rules#microsoftresources
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=90
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="resourceGroup is immutable"
	ResourceGroup string `json:"resourceGroup"`

	// The endpoint monitoring settings of the Traffic Manager profile.
	// +optional
	MonitorConfig *MonitorConfig `json:"monitorConfig,omitempty"`
}

// MonitorConfig defines the endpoint monitoring settings of the Traffic Manager profile.
// https://learn.microsoft.com/en-us/azure/traffic-manager/traffic-manager-monitoring
type MonitorConfig struct {
	// The monitor interval for endpoints in this profile. This is the interval at which Traffic Manager will check the health
	// of each endpoint in this profile.
	// You can specify two values here: 30 seconds (normal probing) and 10 seconds (fast probing).
	// +optional
	// +kubebuilder:default=30
	// +kubebuilder:validation:Enum=10;30
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
	// +optional
	// +kubebuilder:default="HTTP"
	Protocol *TrafficManagerMonitorProtocol `json:"protocol,omitempty"`

	// The monitor timeout for endpoints in this profile. This is the time that Traffic Manager allows endpoints in this profile
	// to response to the health check.
	// +optional
	// * If the IntervalInSeconds is set to 30 seconds, then you can set the Timeout value between 5 and 10 seconds.
	//   If no value is specified, it uses a default value of 10 seconds.
	// * If the IntervalInSeconds is set to 10 seconds, then you can set the Timeout value between 5 and 9 seconds.
	//   If no Timeout value is specified, it uses a default value of 9 seconds.
	// +kubebuilder:validation:Minimum=5
	// +kubebuilder:validation:Maximum=10
	TimeoutInSeconds *int64 `json:"timeoutInSeconds,omitempty"`

	// The number of consecutive failed health check that Traffic Manager tolerates before declaring an endpoint in this profile
	// Degraded after the next failed health check.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=9
	// +kubebuilder:default=3
	ToleratedNumberOfFailures *int64 `json:"toleratedNumberOfFailures,omitempty"`
}

// TrafficManagerMonitorProtocol defines the protocol used to probe for endpoint health.
type TrafficManagerMonitorProtocol string

const (
	TrafficManagerMonitorProtocolHTTP  TrafficManagerMonitorProtocol = "HTTP"
	TrafficManagerMonitorProtocolHTTPS TrafficManagerMonitorProtocol = "HTTPS"
	TrafficManagerMonitorProtocolTCP   TrafficManagerMonitorProtocol = "TCP"
)

type TrafficManagerProfileStatus struct {
	// DNSName is the fully-qualified domain name (FQDN) of the Traffic Manager profile.
	// It consists of profile name and the DNS domain name used by Azure Traffic Manager to form the fully-qualified
	// domain name (FQDN) of the profile.
	// For example, "<TrafficManagerProfileNamespace>-<TrafficManagerProfileName>.trafficmanager.net"
	// +optional
	DNSName *string `json:"dnsName,omitempty"`

	// ResourceID is the fully qualified Azure resource Id for the resource.
	// Ex - /subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Network/trafficManagerProfiles/{resourceName}
	ResourceID string `json:"resourceID,omitempty"`

	// Current profile status.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// TrafficManagerProfileConditionType is a type of condition associated with a
// Traffic Manager Profile. This type should be used within the TrafficManagerProfileStatus.Conditions field.
type TrafficManagerProfileConditionType string

// TrafficManagerProfileConditionReason defines the set of reasons that explain why a
// particular profile condition type has been raised.
type TrafficManagerProfileConditionReason string

const (
	// TrafficManagerProfileConditionProgrammed condition indicates whether a profile has been generated that is assumed to be ready
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
	// * "DNSNameNotAvailable"
	//
	// Possible reasons for this condition to be Unknown are:
	//
	// * "Pending"
	//
	TrafficManagerProfileConditionProgrammed TrafficManagerProfileConditionType = "Programmed"

	// TrafficManagerProfileReasonProgrammed is used with the "Programmed" condition when the condition is true.
	TrafficManagerProfileReasonProgrammed TrafficManagerProfileConditionReason = "Programmed"

	// TrafficManagerProfileReasonInvalid is used with the "Programmed" when the profile is syntactically or semantically invalid.
	TrafficManagerProfileReasonInvalid TrafficManagerProfileConditionReason = "Invalid"

	// TrafficManagerProfileReasonDNSNameNotAvailable is used with the "Programmed" condition when the generated DNS name is not available.
	TrafficManagerProfileReasonDNSNameNotAvailable TrafficManagerProfileConditionReason = "DNSNameNotAvailable"

	// TrafficManagerProfileReasonPending is used with the "Programmed" when creating or updating the profile hits an internal error
	// with more details in the message and the controller will keep retry.
	TrafficManagerProfileReasonPending TrafficManagerProfileConditionReason = "Pending"
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
