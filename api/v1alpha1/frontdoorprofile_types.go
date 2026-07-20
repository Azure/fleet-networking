/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	FrontDoorProfileKind = "FrontDoorProfile"
)

// FrontDoorProfileSkuName defines the SKU of the Azure Front Door profile.
//
// Only Premium is supported (see docs/first-party/001-afd-global-load-balancing.md
// §2.3 and docs/first-party/003-pre-implementation-checklist.md §1.2). Private
// Link origins — the SFI-NS253 cornerstone that makes AFD viable as a
// first-party GLB surface — are Premium-only, so Standard would produce an
// installation that could not satisfy the compliance regime this feature
// exists to serve. Restricting the enum at admission time gives tenants an
// immediate, unambiguous rejection instead of a surprise runtime condition
// hours later at backend creation.
//
// Note there is no Location field on FrontDoorProfileSpec: AFD is a global
// service and the RP rejects any Location other than "Global", so the
// controller sets Location internally rather than exposing a single-valued
// CR field.
type FrontDoorProfileSkuName string

const (
	// FrontDoorProfileSkuPremium is the only supported SKU value; see
	// FrontDoorProfileSkuName for the SFI-NS253 rationale.
	FrontDoorProfileSkuPremium FrontDoorProfileSkuName = "Premium_AzureFrontDoor"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,categories={fleet-networking},shortName=afdp
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=`.status.endpointHostname`,name="Endpoint",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=='Programmed')].status`,name="Is-Programmed",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date

// FrontDoorProfile manages an Azure Front Door profile and its default endpoint
// using the cloud-native (Kubernetes) API. It is the L7 counterpart to
// TrafficManagerProfile.
// https://learn.microsoft.com/en-us/azure/frontdoor/front-door-overview
// +kubebuilder:validation:XValidation:rule="size(self.metadata.name) < 64",message="metadata.name max length is 63"
type FrontDoorProfile struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of FrontDoorProfile.
	Spec FrontDoorProfileSpec `json:"spec"`

	// The observed status of FrontDoorProfile.
	// +optional
	Status FrontDoorProfileStatus `json:"status,omitempty"`
}

// FrontDoorProfileSpec defines the desired state of FrontDoorProfile.
type FrontDoorProfileSpec struct {
	// ResourceGroup is the name of the Azure resource group in which the
	// underlying Front Door profile will be created. Immutable after creation.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=90
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="resourceGroup is immutable"
	ResourceGroup string `json:"resourceGroup"`

	// Sku selects the Front Door SKU. Premium is the only supported value
	// (see the FrontDoorProfileSkuName type comment for the SFI-NS253
	// rationale). Retained as an explicit field — even though the enum is
	// currently single-valued — so future SKUs (if AFD ever ships a
	// compliance-equivalent alternative) can be introduced additively
	// without a schema break. Immutable after creation because AFD does
	// not support in-place SKU upgrades on an existing profile.
	// +required
	// +kubebuilder:validation:Enum=Premium_AzureFrontDoor
	// +kubebuilder:default=Premium_AzureFrontDoor
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="sku is immutable"
	Sku FrontDoorProfileSkuName `json:"sku,omitempty"`
}

// FrontDoorProfileStatus defines the observed state of FrontDoorProfile.
type FrontDoorProfileStatus struct {
	// ResourceID is the fully qualified Azure resource ID of the Front Door profile.
	// Example: /subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Cdn/profiles/{name}
	// +optional
	ResourceID string `json:"resourceID,omitempty"`

	// EndpointHostname is the default *.azurefd.net hostname assigned by Azure
	// to the profile's default endpoint. Populated once the endpoint is programmed.
	// +optional
	EndpointHostname *string `json:"endpointHostname,omitempty"`

	// Current profile status.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// FrontDoorProfileConditionType is a type of condition associated with a
// FrontDoorProfile. This type should be used within the FrontDoorProfileStatus.Conditions field.
type FrontDoorProfileConditionType string

// FrontDoorProfileConditionReason defines the set of reasons that explain why
// a particular profile condition type has been raised.
type FrontDoorProfileConditionReason string

const (
	// FrontDoorProfileConditionProgrammed indicates whether the AFD profile and
	// its default endpoint have been programmed in Azure.
	//
	// Positive-polarity summary condition; always present on the resource with
	// ObservedGeneration set.
	//
	// True reasons: "Programmed".
	// False reasons: "Invalid", "AzureError".
	// Unknown reasons: "Pending".
	FrontDoorProfileConditionProgrammed FrontDoorProfileConditionType = "Programmed"

	FrontDoorProfileReasonProgrammed FrontDoorProfileConditionReason = "Programmed"
	FrontDoorProfileReasonInvalid    FrontDoorProfileConditionReason = "Invalid"
	FrontDoorProfileReasonAzureError FrontDoorProfileConditionReason = "AzureError"
	FrontDoorProfileReasonPending    FrontDoorProfileConditionReason = "Pending"
)

// +kubebuilder:object:root=true

// FrontDoorProfileList contains a list of FrontDoorProfile.
type FrontDoorProfileList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	// +listType=set
	Items []FrontDoorProfile `json:"items"`
}

func init() {
	SchemeBuilder.Register(&FrontDoorProfile{}, &FrontDoorProfileList{})
}
