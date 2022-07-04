/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package endpointsliceexport

import (
	discoveryv1 "k8s.io/api/discovery/v1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

// IsEndpointSliceExportLinkedWithEndpointSlice returns if an EndpointSliceExport's name matches with the
// unique name for export assigned to an exported EndpointSlice.
func isEndpointSliceExportLinkedWithEndpointSlice(endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport,
	endpointSlice *discoveryv1.EndpointSlice) bool {
	uniqueName, ok := endpointSlice.Labels[endpointSliceUniqueNameLabel]
	if !ok || uniqueName != endpointSliceExport.Name {
		return false
	}
	return true
}
