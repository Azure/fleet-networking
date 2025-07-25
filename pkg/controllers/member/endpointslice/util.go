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
	fleetnetv1beta1 "go.goms.io/fleet-networking/api/v1beta1"
)

// isEndpointSlicePermanentlyUnexportable returns if an EndpointSlice is permanently unexportable.
func isEndpointSlicePermanentlyUnexportable(endpointSlice *discoveryv1.EndpointSlice) bool {
	// At this moment only IPv4 endpointslices can be exported; note that AddressType is an immutable field.
	return endpointSlice.AddressType != discoveryv1.AddressTypeIPv4
}

// isServiceExportValidWithNoConflict returns if a ServiceExport
// * is valid; and
// * is in no conflict with other service exports; and
// * has not been deleted
func isServiceExportValidWithNoConflict(svcExport *fleetnetv1beta1.ServiceExport) bool {
	validCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1beta1.ServiceExportValid))
	conflictCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1beta1.ServiceExportConflict))
	isValid := (validCond != nil && validCond.Status == metav1.ConditionTrue)
	hasNoConflict := (conflictCond != nil && conflictCond.Status == metav1.ConditionFalse)
	return (isValid && hasNoConflict && svcExport.DeletionTimestamp == nil)
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

// extractEndpointsFromEndpointSlice extracts endpoints from an EndpointSlice.
func extractEndpointsFromEndpointSlice(endpointSlice *discoveryv1.EndpointSlice) []fleetnetv1alpha1.Endpoint {
	extractedEndpoints := []fleetnetv1alpha1.Endpoint{}
	for _, endpoint := range endpointSlice.Endpoints {
		// Only ready endpoints can be exported; EndpointSlice API dictates that consumers should interpret
		// unknown ready state, represented by a nil value, as true ready state.
		// TO-DO (chenyu1): In newer API versions the EndpointConditions API (V1) introduces a serving state, which
		// allows a backend to serve traffic even if it is already terminating (EndpointSliceTerminationCondition
		// feature gate).
		if endpoint.Conditions.Ready == nil || *(endpoint.Conditions.Ready) {
			extractedEndpoints = append(extractedEndpoints, fleetnetv1alpha1.Endpoint{
				Addresses: endpoint.Addresses,
			})
		}
	}
	return extractedEndpoints
}
