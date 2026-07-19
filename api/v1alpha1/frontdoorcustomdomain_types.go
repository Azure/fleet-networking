/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	FrontDoorCustomDomainKind = "FrontDoorCustomDomain"
)

// FrontDoorTLSMode selects how the TLS certificate for a custom domain is sourced.
type FrontDoorTLSMode string

const (
	// FrontDoorTLSModeManaged uses an AFD-managed certificate that is issued
	// and renewed automatically by Azure.
	FrontDoorTLSModeManaged FrontDoorTLSMode = "Managed"

	// FrontDoorTLSModeBYOC binds a customer-supplied certificate stored in
	// Azure Key Vault. Requires KeyVaultCertificate to be set.
	// NOTE (POC): reserved. The controller currently only implements the
	// Managed path; BYOC reconciliation is deferred (see breadcrumb D3).
	FrontDoorTLSModeBYOC FrontDoorTLSMode = "BYOC"
)

// FrontDoorDomainValidationState is the current state of DNS-based ownership
// validation of a custom domain, mirroring the AFD resource provider states.
type FrontDoorDomainValidationState string

const (
	FrontDoorDomainValidationStatePending       FrontDoorDomainValidationState = "Pending"
	FrontDoorDomainValidationStateApproved      FrontDoorDomainValidationState = "Approved"
	FrontDoorDomainValidationStateRejected      FrontDoorDomainValidationState = "Rejected"
	FrontDoorDomainValidationStateTimedOut      FrontDoorDomainValidationState = "TimedOut"
	FrontDoorDomainValidationStateInternalError FrontDoorDomainValidationState = "InternalError"
	FrontDoorDomainValidationStateSubmitting    FrontDoorDomainValidationState = "Submitting"
	FrontDoorDomainValidationStateRefreshing    FrontDoorDomainValidationState = "RefreshingValidationToken"
	FrontDoorDomainValidationStateUnknown       FrontDoorDomainValidationState = "Unknown"
)

// FrontDoorProfileReference references a FrontDoorProfile by name in the same
// namespace as the FrontDoorCustomDomain.
type FrontDoorProfileReference struct {
	// Name of the target FrontDoorProfile. Must exist in the same namespace as
	// this FrontDoorCustomDomain (per breadcrumb D4: same-namespace only).
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name"`
}

// FrontDoorKeyVaultCertificate references a certificate stored in an Azure
// Key Vault. Used only when TLS.Mode is "BYOC".
type FrontDoorKeyVaultCertificate struct {
	// VaultURI is the base URI of the Key Vault, e.g. https://myvault.vault.azure.net.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^https://[a-zA-Z0-9-]+\.vault\.azure\.net/?$`
	VaultURI string `json:"vaultURI"`

	// CertificateName is the name of the certificate in the Key Vault.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=127
	CertificateName string `json:"certificateName"`

	// Version optionally pins a specific certificate version. When omitted,
	// the controller tracks the latest version and re-binds on rotation.
	// +optional
	Version *string `json:"version,omitempty"`
}

// FrontDoorTLSConfig defines the TLS configuration for a custom domain.
// +kubebuilder:validation:XValidation:rule="self.mode != 'BYOC' || has(self.keyVaultCertificate)",message="keyVaultCertificate is required when mode is BYOC"
// +kubebuilder:validation:XValidation:rule="self.mode != 'Managed' || !has(self.keyVaultCertificate)",message="keyVaultCertificate must not be set when mode is Managed"
type FrontDoorTLSConfig struct {
	// Mode selects the source of the TLS certificate.
	// +required
	// +kubebuilder:validation:Enum=Managed;BYOC
	Mode FrontDoorTLSMode `json:"mode"`

	// KeyVaultCertificate references the certificate to bind when Mode is BYOC.
	// +optional
	KeyVaultCertificate *FrontDoorKeyVaultCertificate `json:"keyVaultCertificate,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,categories={fleet-networking},shortName=afdcd
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=`.spec.hostname`,name="Hostname",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.validationState`,name="Validation",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=='Programmed')].status`,name="Is-Programmed",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date

// FrontDoorCustomDomain represents a custom domain attached to a FrontDoorProfile,
// including the DNS-based ownership validation and the TLS binding.
// https://learn.microsoft.com/en-us/azure/frontdoor/domain
// +kubebuilder:validation:XValidation:rule="size(self.metadata.name) < 64",message="metadata.name max length is 63"
type FrontDoorCustomDomain struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of FrontDoorCustomDomain.
	Spec FrontDoorCustomDomainSpec `json:"spec"`

	// The observed status of FrontDoorCustomDomain.
	// +optional
	Status FrontDoorCustomDomainStatus `json:"status,omitempty"`
}

// FrontDoorCustomDomainSpec defines the desired state of FrontDoorCustomDomain.
type FrontDoorCustomDomainSpec struct {
	// ProfileRef references the FrontDoorProfile that owns this custom domain.
	// The referenced profile must exist in the same namespace as this resource.
	// Immutable after creation.
	// +required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="profileRef is immutable"
	ProfileRef FrontDoorProfileReference `json:"profileRef"`

	// Hostname is the fully qualified custom domain name (e.g. www.contoso.com).
	// Immutable after creation.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="hostname is immutable"
	Hostname string `json:"hostname"`

	// TLS configures the certificate binding for this custom domain.
	// +required
	TLS FrontDoorTLSConfig `json:"tls"`
}

// FrontDoorCustomDomainStatus defines the observed state of FrontDoorCustomDomain.
type FrontDoorCustomDomainStatus struct {
	// ResourceID is the fully qualified Azure resource ID of the custom domain.
	// Example: /subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Cdn/profiles/{profile}/customDomains/{name}
	// +optional
	ResourceID string `json:"resourceID,omitempty"`

	// ValidationState is the current DNS ownership validation state reported by Azure.
	// +optional
	ValidationState FrontDoorDomainValidationState `json:"validationState,omitempty"`

	// DNSValidationToken is the token that must be published as a TXT record on
	// the customer's DNS zone to prove ownership. The expected TXT record name
	// is `_dnsauth.<hostname>`, and the value is this token.
	// +optional
	DNSValidationToken *string `json:"dnsValidationToken,omitempty"`

	// DNSValidationExpiry is the time at which the current validation token
	// expires. Azure issues a new token periodically; the controller refreshes
	// status before expiry.
	// +optional
	DNSValidationExpiry *metav1.Time `json:"dnsValidationExpiry,omitempty"`

	// Current custom domain status.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// FrontDoorCustomDomainConditionType is a type of condition associated with a
// FrontDoorCustomDomain.
type FrontDoorCustomDomainConditionType string

// FrontDoorCustomDomainConditionReason defines the set of reasons that explain
// why a particular condition type has been raised.
type FrontDoorCustomDomainConditionReason string

const (
	// FrontDoorCustomDomainConditionProgrammed indicates whether the custom
	// domain resource has been programmed in AFD (created + validated + bound).
	//
	// Positive-polarity summary condition; always present on the resource with
	// ObservedGeneration set.
	//
	// True reasons: "Programmed".
	// False reasons: "Invalid", "ProfileNotReady", "ValidationFailed", "TLSFailed", "AzureError".
	// Unknown reasons: "Pending", "AwaitingDNSValidation".
	FrontDoorCustomDomainConditionProgrammed FrontDoorCustomDomainConditionType = "Programmed"

	FrontDoorCustomDomainReasonProgrammed            FrontDoorCustomDomainConditionReason = "Programmed"
	FrontDoorCustomDomainReasonInvalid               FrontDoorCustomDomainConditionReason = "Invalid"
	FrontDoorCustomDomainReasonProfileNotReady       FrontDoorCustomDomainConditionReason = "ProfileNotReady"
	FrontDoorCustomDomainReasonAwaitingDNSValidation FrontDoorCustomDomainConditionReason = "AwaitingDNSValidation"
	FrontDoorCustomDomainReasonValidationFailed      FrontDoorCustomDomainConditionReason = "ValidationFailed"
	FrontDoorCustomDomainReasonTLSFailed             FrontDoorCustomDomainConditionReason = "TLSFailed"
	FrontDoorCustomDomainReasonAzureError            FrontDoorCustomDomainConditionReason = "AzureError"
	FrontDoorCustomDomainReasonPending               FrontDoorCustomDomainConditionReason = "Pending"
)

// +kubebuilder:object:root=true

// FrontDoorCustomDomainList contains a list of FrontDoorCustomDomain.
type FrontDoorCustomDomainList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	// +listType=set
	Items []FrontDoorCustomDomain `json:"items"`
}

func init() {
	SchemeBuilder.Register(&FrontDoorCustomDomain{}, &FrontDoorCustomDomainList{})
}
