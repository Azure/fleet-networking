/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package condition provides condition related utils.
package condition

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

const (
	conditionReasonNoConflictFound = "NoConflictFound"
	conditionReasonConflictFound   = "ConflictFound"
)

// EqualCondition compares one condition with another; it ignores the LastTransitionTime and Message fields,
// and will consider the ObservedGeneration values from the two conditions a match if the current
// condition is newer.
func EqualCondition(current, desired *metav1.Condition) bool {
	if current == nil && desired == nil {
		return true
	}
	return current != nil &&
		desired != nil &&
		current.Type == desired.Type &&
		current.Status == desired.Status &&
		current.Reason == desired.Reason &&
		current.ObservedGeneration >= desired.ObservedGeneration
}

// EqualConditionIgnoreReason compares one condition with another; it ignores the Reason, LastTransitionTime, and
// Message fields, and will consider the ObservedGeneration values from the two conditions a match if the current
// condition is newer.
func EqualConditionIgnoreReason(current, desired *metav1.Condition) bool {
	if current == nil && desired == nil {
		return true
	}

	return current != nil &&
		desired != nil &&
		current.Type == desired.Type &&
		current.Status == desired.Status &&
		current.ObservedGeneration >= desired.ObservedGeneration
}

// EqualConditionWithMessage compares one condition with another; it ignores the LastTransitionTime field,
// and will consider the ObservedGeneration values from the two conditions a match if the current
// condition is newer.
func EqualConditionWithMessage(current, desired *metav1.Condition) bool {
	if current == nil && desired == nil {
		return true
	}
	return current != nil &&
		desired != nil &&
		current.Type == desired.Type &&
		current.Status == desired.Status &&
		current.Reason == desired.Reason &&
		current.Message == desired.Message &&
		current.ObservedGeneration >= desired.ObservedGeneration
}

// UnconflictedServiceExportConflictCondition returns the desired unconflicted condition.
func UnconflictedServiceExportConflictCondition(internalServiceExport fleetnetv1alpha1.InternalServiceExport) metav1.Condition {
	svcName := types.NamespacedName{
		Namespace: internalServiceExport.Spec.ServiceReference.Namespace,
		Name:      internalServiceExport.Spec.ServiceReference.Name,
	}
	return metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportConflict),
		Status:             metav1.ConditionFalse,
		Reason:             conditionReasonNoConflictFound,
		ObservedGeneration: internalServiceExport.Generation,
		Message:            fmt.Sprintf("service %s is exported without conflict", svcName),
	}
}

// ConflictedServiceExportConflictCondition returns the desired conflicted condition.
func ConflictedServiceExportConflictCondition(internalServiceExport fleetnetv1alpha1.InternalServiceExport) metav1.Condition {
	svcName := types.NamespacedName{
		Namespace: internalServiceExport.Spec.ServiceReference.Namespace,
		Name:      internalServiceExport.Spec.ServiceReference.Name,
	}
	return metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportConflict),
		Status:             metav1.ConditionTrue,
		Reason:             conditionReasonConflictFound,
		ObservedGeneration: internalServiceExport.Generation,
		Message:            fmt.Sprintf("service %s is in conflict with other exported services", svcName),
	}
}
