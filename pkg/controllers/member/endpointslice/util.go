/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package endpointslice

import (
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

// isEndpointSlicePermanentlyUnexportable returns if an EndpointSlice is permanently unexportable.
func isEndpointSlicePermanentlyUnexportable(endpointSlice *discoveryv1.EndpointSlice) bool {
	// At this moment only IPv4 endpointslices can be exported; note that AddressType is an immutable field.
	return endpointSlice.AddressType != discoveryv1.AddressTypeIPv4
}

// isServiceExportValidWithNoConflict returns if a Service Export is valid and is in no conflict
// with other service exports.
func isServiceExportValidWithNoConflict(svcExport *fleetnetv1alpha1.ServiceExport) bool {
	validCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportValid))
	conflictCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportConflict))
	isValid := (validCond != nil && validCond.Status == metav1.ConditionTrue)
	hasNoConflict := (conflictCond != nil && conflictCond.Status == metav1.ConditionFalse)
	return (isValid && hasNoConflict)
}

// isUniqueNameValid returns if an assigned unique name is a valid DNS subdomain name.
func isUniqueNameValid(name string) bool {
	if errs := validation.IsDNS1123Subdomain(name); len(errs) != 0 {
		return false
	}
	return true
}

// IsEndpointSliceExportLinkedWithEndpointSlice returns if an EndpointSliceExport references an EndpointSlice.
func isEndpointSliceExportLinkedWithEndpointSlice(endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport,
	endpointSlice *discoveryv1.EndpointSlice) bool {
	return (endpointSliceExport.Spec.EndpointSliceReference.UID == endpointSlice.UID)
}
