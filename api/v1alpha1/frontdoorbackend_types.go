/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	FrontDoorBackendKind = "FrontDoorBackend"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,categories={fleet-networking},shortName=fdb
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=`.spec.profile.name`,name="Profile",type=string
// +kubebuilder:printcolumn:JSONPath=`.spec.backend.name`,name="Backend",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=='Accepted')].status`,name="Is-Accepted",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date

// FrontDoorBackend is the AFD-side counterpart to TrafficManagerBackend: it
// binds a fleet-scoped ServiceImport (specifically, its L7-FrontDoor exports
// carrying PrivateLinkServiceResourceID — see InternalServiceExportSpec) to a
// FrontDoorProfile, producing an Azure Front Door OriginGroup with one Origin
// per qualifying member-cluster export.
//
// The shape intentionally mirrors TrafficManagerBackend 1:1 so operators
// authoring both surfaces have a single mental model:
//
//   - Spec.Profile  → parent FrontDoorProfile (same namespace)
//   - Spec.Backend  → ServiceImport (same namespace)
//   - Spec.Weight   → aggregate traffic weight, distributed across
//     per-export weights the same way as ATM (see the
//     TrafficManagerBackendSpec.Weight formula).
//
// Design references:
//   - docs/first-party/001-afd-global-load-balancing.md §4 (backend model)
//   - docs/first-party/002-afd-implementation-plan.md §6 (reconciler)
//
// +kubebuilder:validation:XValidation:rule="size(self.metadata.name) < 64",message="metadata.name max length is 63"
type FrontDoorBackend struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The desired state of FrontDoorBackend.
	Spec FrontDoorBackendSpec `json:"spec"`

	// The observed status of FrontDoorBackend.
	// +optional
	Status FrontDoorBackendStatus `json:"status,omitempty"`
}

// FrontDoorBackendSpec describes which FrontDoorProfile a ServiceImport is
// attached to, and how much aggregate traffic it should receive. Profile and
// Backend are immutable — changing them would require recreating the
// underlying OriginGroup, which loses live-traffic guarantees.
type FrontDoorBackendSpec struct {
	// Which FrontDoorProfile the backend should be attached to.
	// +required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.profile is immutable"
	Profile FrontDoorProfileRef `json:"profile"`

	// The reference to a backend.
	// +required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.backend is immutable"
	Backend FrontDoorBackendRef `json:"backend"`

	// The aggregate weight of origins behind the serviceImport under the
	// generated Front Door OriginGroup. Possible values are from 0 to 1000.
	// Semantics match TrafficManagerBackendSpec.Weight: the value is
	// distributed across each cluster's exports proportionally to the
	// per-export weight surfaced on the ServiceExport. If weight is set to
	// 0, all origins behind the serviceImport will be removed from the
	// OriginGroup (effectively draining traffic).
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	// +kubebuilder:default=1
	Weight *int64 `json:"weight,omitempty"`
}

// FrontDoorProfileRef is a reference to a FrontDoorProfile in the same
// namespace as the FrontDoorBackend. Modeled as a struct (not a bare string)
// to leave room for cross-namespace / cross-tenant refs later without a
// breaking API change.
type FrontDoorProfileRef struct {
	// Name is the name of the referenced FrontDoorProfile.
	// +required
	Name string `json:"name"`
}

// FrontDoorBackendRef is the reference to a backend. Currently only
// ServiceImport is supported; the struct wrapper anticipates additional
// backend types (e.g. direct PrivateLinkServiceResourceID refs) without a
// breaking change.
type FrontDoorBackendRef struct {
	// Name is the reference to the ServiceImport in the same namespace as
	// the FrontDoorBackend object.
	// +required
	Name string `json:"name"`
}

// FrontDoorOriginStatus captures the status of a single Azure Front Door
// Origin created under the FrontDoorProfile for this backend.
type FrontDoorOriginStatus struct {
	// Name of the origin (as created inside the AFD OriginGroup).
	// +required
	Name string `json:"name"`

	// ResourceID is the fully qualified Azure resource Id of the origin.
	// Ex - /subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Cdn/profiles/{profile}/originGroups/{group}/origins/{name}
	// +optional
	ResourceID string `json:"resourceID,omitempty"`

	// PrivateLinkServiceResourceID is the PLS this origin was wired to
	// (see InternalServiceExportSpec.PrivateLinkServiceResourceID).
	// +optional
	PrivateLinkServiceResourceID *string `json:"privateLinkServiceResourceID,omitempty"`

	// Weight is the effective weight of this origin, after the aggregate
	// FrontDoorBackendSpec.Weight has been distributed across per-export
	// weights. Semantics parallel TrafficManagerEndpointStatus.Weight.
	// +optional
	Weight *int64 `json:"weight,omitempty"`

	// From is where the origin's underlying export was sourced.
	// +optional
	From *FromCluster `json:"from,omitempty"`
}

// FrontDoorBackendStatus reflects what the reconciler has actually
// programmed on the Azure Front Door profile. OriginGroupResourceID lets
// consumers (e.g. FrontDoorRoute in a later phase) key off a stable ID
// without re-deriving it from the profile + backend names.
type FrontDoorBackendStatus struct {
	// OriginGroupResourceID is the fully qualified Azure resource Id of
	// the Origin Group created for this backend. Empty while the
	// backend is still being accepted.
	// +optional
	OriginGroupResourceID string `json:"originGroupResourceID,omitempty"`

	// Origins contains a list of accepted Azure Front Door origins that
	// are created or updated under the generated OriginGroup.
	// +optional
	Origins []FrontDoorOriginStatus `json:"origins,omitempty"`

	// Current backend status.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// FrontDoorBackendConditionType is a type of condition associated with a
// FrontDoorBackendStatus. Mirrors TrafficManagerBackendConditionType so
// consumers can reuse condition-handling code across the two backends.
type FrontDoorBackendConditionType string

// FrontDoorBackendConditionReason defines the set of reasons that explain
// why a particular backend has been raised.
type FrontDoorBackendConditionReason string

const (
	// FrontDoorBackendConditionAccepted indicates whether origins have
	// been created or updated for the profile. This does not indicate
	// whether or not the configuration has been propagated to the AFD
	// data plane (POP rollout is asynchronous and observed via ARM only
	// after the fact).
	//
	// Possible reasons for this condition to be True are:
	//
	// * "Accepted"
	//
	// Possible reasons for this condition to be False are:
	//
	// * "Invalid"
	// * "Conflict" (a TrafficManagerBackend already claims the same
	//   ServiceImport — the AFD/ATM coexistence guard rejects the
	//   duplicate to avoid split-brain export ownership)
	//
	// Possible reasons for this condition to be Unknown are:
	//
	// * "Pending"
	//
	FrontDoorBackendConditionAccepted FrontDoorBackendConditionType = "Accepted"

	// FrontDoorBackendReasonAccepted is used with the "Accepted"
	// condition when the condition is True.
	FrontDoorBackendReasonAccepted FrontDoorBackendConditionReason = "Accepted"

	// FrontDoorBackendReasonInvalid is used with the "Accepted"
	// condition when one or more origin references have an invalid or
	// unsupported configuration (e.g. an export with ExportMode !=
	// L7-FrontDoor, or a missing PrivateLinkServiceResourceID).
	FrontDoorBackendReasonInvalid FrontDoorBackendConditionReason = "Invalid"

	// FrontDoorBackendReasonConflict is used with the "Accepted"
	// condition when the AFD/ATM coexistence guard rejects a backend
	// because a TrafficManagerBackend already owns the same
	// ServiceImport. See docs/first-party/002-afd-implementation-plan.md
	// §7 for the ownership rules.
	FrontDoorBackendReasonConflict FrontDoorBackendConditionReason = "Conflict"

	// FrontDoorBackendReasonPending is used with the "Accepted" condition
	// when creating or updating origins hits a transient error; the
	// controller keeps retrying.
	FrontDoorBackendReasonPending FrontDoorBackendConditionReason = "Pending"
)

// +kubebuilder:object:root=true

// FrontDoorBackendList contains a list of FrontDoorBackend.
type FrontDoorBackendList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	// +listType=set
	Items []FrontDoorBackend `json:"items"`
}

func init() {
	SchemeBuilder.Register(&FrontDoorBackend{}, &FrontDoorBackendList{})
}
