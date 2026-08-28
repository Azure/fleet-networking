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
	AzureFrontDoorGatewayPolicyKind = "AzureFrontDoorGatewayPolicy"
)

// AzureFrontDoorProfileMode identifies how Fleet obtains the AFD profile.
// +kubebuilder:validation:Enum=Managed
type AzureFrontDoorProfileMode string

const (
	AzureFrontDoorProfileModeManaged AzureFrontDoorProfileMode = "Managed"
)

// AzureFrontDoorProfileSKU is the Azure Front Door Standard or Premium SKU.
// +kubebuilder:validation:Enum=Standard_AzureFrontDoor;Premium_AzureFrontDoor
type AzureFrontDoorProfileSKU string

const (
	AzureFrontDoorProfileSKUStandard AzureFrontDoorProfileSKU = "Standard_AzureFrontDoor"
	AzureFrontDoorProfileSKUPremium  AzureFrontDoorProfileSKU = "Premium_AzureFrontDoor"
)

// AzureFrontDoorGatewayPolicySpec configures the AFD profile used by one Gateway.
type AzureFrontDoorGatewayPolicySpec struct {
	// TargetRef identifies a Gateway in the policy namespace.
	// +required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.targetRef is immutable"
	TargetRef gatewayv1.LocalObjectReference `json:"targetRef"`

	// Profile configures the controller-managed AFD profile.
	// +required
	Profile AzureFrontDoorProfileSpec `json:"profile"`

	// WAF references the security-owned WAF policy associated with AFD routes.
	// +required
	WAF AzureFrontDoorWAFSpec `json:"waf"`

	// Diagnostics configures an existing Azure diagnostic destination.
	// +optional
	Diagnostics *AzureFrontDoorDiagnosticsSpec `json:"diagnostics,omitempty"`
}

// AzureFrontDoorProfileSpec configures a controller-managed AFD profile.
type AzureFrontDoorProfileSpec struct {
	// Mode is Managed in the initial API.
	// +kubebuilder:default=Managed
	Mode AzureFrontDoorProfileMode `json:"mode,omitempty"`

	// SKU is the AFD profile SKU.
	// +required
	SKU AzureFrontDoorProfileSKU `json:"sku"`

	// ResourceGroup is the Azure resource group containing the profile.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=90
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.profile.resourceGroup is immutable"
	ResourceGroup string `json:"resourceGroup"`

	// Name is the Azure AFD profile name.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=260
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.profile.name is immutable"
	Name string `json:"name"`
}

// AzureFrontDoorWAFSpec references an existing WAF policy.
type AzureFrontDoorWAFSpec struct {
	// Required must remain true for internet-facing AFD Gateways.
	// +kubebuilder:default=true
	Required *bool `json:"required,omitempty"`

	// PolicyResourceID is the complete Azure resource ID of an existing WAF policy.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	PolicyResourceID string `json:"policyResourceID"`
}

// AzureFrontDoorDiagnosticsSpec references an existing Azure diagnostic destination.
type AzureFrontDoorDiagnosticsSpec struct {
	// Enabled controls diagnostic settings for resources managed by this policy.
	// +kubebuilder:default=true
	Enabled *bool `json:"enabled,omitempty"`

	// DestinationResourceID is the complete Azure resource ID of the destination.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	DestinationResourceID string `json:"destinationResourceID"`
}

// AzureFrontDoorGatewayPolicyStatus describes the resolved Gateway and policy state.
type AzureFrontDoorGatewayPolicyStatus struct {
	AzureFrontDoorResourceStatus `json:",inline"`

	// Gateway records the resolved target identity.
	// +optional
	Gateway *AzureFrontDoorResolvedReference `json:"gateway,omitempty"`

	// ProfileResourceID is populated only after a later Azure-writing controller creates the profile.
	// +optional
	ProfileResourceID string `json:"profileResourceID,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,categories={fleet-networking},shortName=afdgp
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=`.spec.targetRef.name`,name="Gateway",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=='Accepted')].status`,name="Accepted",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date
// +kubebuilder:validation:XValidation:rule="self.spec.targetRef.group == 'gateway.networking.k8s.io' && self.spec.targetRef.kind == 'Gateway'",message="spec.targetRef must reference a Gateway"
// +kubebuilder:validation:XValidation:rule="self.spec.waf.required == true",message="spec.waf.required must be true"

// AzureFrontDoorGatewayPolicy configures the AFD infrastructure used by a Gateway.
type AzureFrontDoorGatewayPolicy struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec AzureFrontDoorGatewayPolicySpec `json:"spec"`

	// +optional
	Status AzureFrontDoorGatewayPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AzureFrontDoorGatewayPolicyList contains AzureFrontDoorGatewayPolicy objects.
type AzureFrontDoorGatewayPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	// +listType=set
	Items []AzureFrontDoorGatewayPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AzureFrontDoorGatewayPolicy{}, &AzureFrontDoorGatewayPolicyList{})
}
