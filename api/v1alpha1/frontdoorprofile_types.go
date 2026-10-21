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
// +kubebuilder:validation:XValidation:rule="self.spec.complianceMode != 'SFI-NS253' || has(self.spec.wafPolicy)",message="spec.wafPolicy is required when spec.complianceMode is SFI-NS253"
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

	// ComplianceMode declares the security/compliance regime this profile
	// (and its backends) must satisfy. See docs/first-party/001-afd-global-load-balancing.md
	// §2.1 for the design and docs/first-party/003-pre-implementation-checklist.md §1.2
	// for the operational requirements.
	//
	// Effects of the value:
	//   - "None" (default): no additional constraints beyond the structural
	//     ones. Suitable for dev/test tenants that don't need SFI-NS253
	//     compliance, and lets the profile be created without a WAF policy
	//     attach (WAFPolicy stays optional).
	//   - "SFI-NS253":
	//       * spec.wafPolicy is REQUIRED (enforced by the cross-field CEL
	//         rule on this Spec — see below).
	//       * The FrontDoorProfile reconciler additionally verifies that the
	//         referenced WAF policy exists and is in Prevention mode
	//         (surfaced as Programmed=False with
	//         Reason=WAFPolicyNotFound / WAFPolicyNotInPreventionMode).
	//       * Every FrontDoorBackend that references this profile must have
	//         spec.privateLink.enabled = true (enforced in the backend
	//         reconciler, surfaced as Accepted=False,
	//         Reason=SFIComplianceViolation).
	//
	// Immutable after creation: switching a live profile out of SFI-NS253
	// would silently weaken guarantees the operator relied on when
	// creating the profile — recreate the profile if the compliance regime
	// legitimately needs to change.
	// +optional
	// +kubebuilder:validation:Enum=None;SFI-NS253
	// +kubebuilder:default=None
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="complianceMode is immutable"
	ComplianceMode FrontDoorProfileComplianceMode `json:"complianceMode,omitempty"`

	// WAFPolicy attaches a Web Application Firewall policy to the profile.
	//
	// Required when ComplianceMode is "SFI-NS253"; optional otherwise (dev/test
	// tenants may run without a WAF attach). The cross-field CEL rule below
	// rejects any object that violates this at admission time so that a
	// misconfigured SFI-NS253 profile never even reaches the reconciler.
	// +optional
	WAFPolicy *FrontDoorWAFPolicyRef `json:"wafPolicy,omitempty"`
}

// FrontDoorProfileComplianceMode selects the security/compliance regime a
// FrontDoorProfile is subject to. See the ComplianceMode field on
// FrontDoorProfileSpec for the enforcement matrix.
type FrontDoorProfileComplianceMode string

const (
	// FrontDoorProfileComplianceModeNone imposes no compliance-driven
	// constraints beyond the structural ones (SKU enum, immutability, etc.).
	FrontDoorProfileComplianceModeNone FrontDoorProfileComplianceMode = "None"

	// FrontDoorProfileComplianceModeSFINS253 enables the full SFI-NS253
	// enforcement described on the ComplianceMode field of
	// FrontDoorProfileSpec: WAF required + Prevention mode + PrivateLink
	// mandatory on all referencing backends.
	FrontDoorProfileComplianceModeSFINS253 FrontDoorProfileComplianceMode = "SFI-NS253"
)

// FrontDoorWAFPolicyRef references a Front Door Web Application Firewall
// policy that should be bound to the profile's default endpoint (and, in the
// future, its custom domains via a securityPolicies AFD resource).
//
// The reference is a raw ARM resource ID rather than a Kubernetes object
// reference because AFD WAF policies are typically pre-created and centrally
// managed (e.g. by a security team) and do not have a matching CRD in this
// repository today.
//
// An inline creation path was sketched in the design proposal
// (docs/first-party/002-afd-implementation-plan.md §3.1 — the `Inline` field
// on FrontDoorWAFPolicyRef) but is deliberately deferred: nearly all
// first-party services will reference a centrally-managed WAF policy, and
// exposing inline creation would duplicate what would ultimately be its own
// CRD (WAFPolicy) with its own reconciler. When/if inline creation lands, it
// will be added as an additional optional field on this struct without a
// schema break.
type FrontDoorWAFPolicyRef struct {
	// ResourceID is the fully qualified ARM resource ID of an existing
	// Microsoft.Network/frontdoorwebapplicationfirewallpolicies resource.
	//
	// Format:
	//   /subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Network/frontdoorwebapplicationfirewallpolicies/{name}
	//
	// The policy may live in a different subscription/RG from the AFD
	// profile; cross-subscription references are supported at the Azure
	// level, subject to the AFD controller's managed identity having
	// Microsoft.Network/frontDoorWebApplicationFirewallPolicies/read on
	// the policy's scope. Cross-sub RBAC failures surface as
	// Programmed=False, Reason=WAFPolicyNotFound with the exact ID in the
	// message (per docs/first-party/002-afd-implementation-plan.md §11).
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^/subscriptions/[^/]+/resourceGroups/[^/]+/providers/Microsoft\.Network/frontdoorwebapplicationfirewallpolicies/[^/]+$`
	ResourceID string `json:"resourceID"`
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
	// False reasons: "Invalid", "AzureError", "WAFPolicyNotFound", "WAFPolicyNotInPreventionMode".
	// Unknown reasons: "Pending".
	FrontDoorProfileConditionProgrammed FrontDoorProfileConditionType = "Programmed"

	FrontDoorProfileReasonProgrammed FrontDoorProfileConditionReason = "Programmed"
	FrontDoorProfileReasonInvalid    FrontDoorProfileConditionReason = "Invalid"
	FrontDoorProfileReasonAzureError FrontDoorProfileConditionReason = "AzureError"
	FrontDoorProfileReasonPending    FrontDoorProfileConditionReason = "Pending"

	// FrontDoorProfileReasonWAFPolicyNotFound indicates that spec.wafPolicy
	// references a policy ARM resource ID that the AFD controller cannot
	// resolve (Azure returns NotFound, or the controller's identity lacks
	// the required read permission on the policy's scope — the two failure
	// modes are indistinguishable from the AFD RP's perspective, so both
	// surface as this single reason; the exact ID is included in the
	// condition message for diagnosis).
	FrontDoorProfileReasonWAFPolicyNotFound FrontDoorProfileConditionReason = "WAFPolicyNotFound"

	// FrontDoorProfileReasonWAFPolicyNotInPreventionMode indicates that
	// spec.wafPolicy resolves to a real policy but the policy's
	// PolicySettings.Mode is not "Prevention". Only enforced when
	// ComplianceMode == "SFI-NS253" (Detection mode is a valid choice for
	// tenants with ComplianceMode="None" who want alerts without blocking).
	FrontDoorProfileReasonWAFPolicyNotInPreventionMode FrontDoorProfileConditionReason = "WAFPolicyNotInPreventionMode"
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
