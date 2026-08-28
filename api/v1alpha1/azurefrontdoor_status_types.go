/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	AzureFrontDoorConditionAccepted       = "Accepted"
	AzureFrontDoorConditionResolvedRefs   = "ResolvedRefs"
	AzureFrontDoorConditionProgrammed     = "Programmed"
	AzureFrontDoorReasonAccepted          = "Accepted"
	AzureFrontDoorReasonConflicted        = "Conflicted"
	AzureFrontDoorReasonInvalid           = "Invalid"
	AzureFrontDoorReasonInvalidPort       = "InvalidPort"
	AzureFrontDoorReasonPending           = "Pending"
	AzureFrontDoorReasonRefNotFound       = "RefNotFound"
	AzureFrontDoorReasonUnsupportedRef    = "UnsupportedRef"
	AzureFrontDoorReasonUnsupportedConfig = "UnsupportedConfiguration"
)

// AzureFrontDoorResolvedReference records the identity used to resolve an immutable API reference.
type AzureFrontDoorResolvedReference struct {
	// Name is the referenced object's name.
	Name string `json:"name"`

	// UID distinguishes a recreated object from the object originally resolved.
	UID types.UID `json:"uid"`
}

// AzureFrontDoorResourceStatus is shared status for provider resources.
type AzureFrontDoorResourceStatus struct {
	// Conditions describe the current reconciliation state.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}
