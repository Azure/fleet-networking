/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package endpointslice

import (
	"fmt"

	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/util/uuid"

	fleetnetworkingapi "go.goms.io/fleet-networking/api/v1alpha1"
)

// isEndpointSliceExportable returns if an EndpointSlice is exportable; at this moment only IPv4 addresses are
// supported.
func isEndpointSliceExportable(endpointSlice *discoveryv1.EndpointSlice) bool {
	return endpointSlice.AddressType == discoveryv1.AddressTypeIPv4
}

// isEndpointSliceCleanupNeeded returns if an EndpointSlice needs cleanup.
func isEndpointSliceCleanupNeeded(endpointSlice *discoveryv1.EndpointSlice) bool {
	_, hasUniqueNameLabel := endpointSlice.Labels[endpointSliceUniqueNameLabel]
	return hasUniqueNameLabel && endpointSlice.DeletionTimestamp != nil
}

// formatFleetUniqueName formats a unique name for an EndpointSlice.
func formatFleetUniqueName(clusterID string, endpointSlice *discoveryv1.EndpointSlice) string {
	if len(clusterID)+len(endpointSlice.Namespace)+len(endpointSlice.Name) < 253 {
		return fmt.Sprintf("%s-%s-%s", clusterID, endpointSlice.Namespace, endpointSlice.Name)
	}

	nameLimit := 253 - len(clusterID) - len(endpointSlice.Namespace) - 9
	return fmt.Sprintf("%s-%s-%s-%s",
		clusterID,
		endpointSlice.Namespace,
		endpointSlice.Name[:nameLimit],
		uuid.NewUUID()[:5],
	)
}

// extractEndpointsFromEndpointSlice extracts endpoints from an EndpointSlice.
func extractEndpointsFromEndpointSlice(endpointSlice *discoveryv1.EndpointSlice) []fleetnetworkingapi.Endpoint {
	extractedEndpoints := []fleetnetworkingapi.Endpoint{}
	for _, endpoint := range endpointSlice.Endpoints {
		// Only ready endpoints can be exported; EndpointSlice API dictates that consumers should interpret
		// unknown ready state, represented by a nil value, as true ready state.
		// TO-DO (chenyu1): In newer API versions the EndpointConditions API (V1) introduces a serving state, which
		// allows a backend to serve traffic even if it is already terminating (EndpointSliceTerminationCondition
		// feature gate).
		if endpoint.Conditions.Ready == nil || *(endpoint.Conditions.Ready) {
			extractedEndpoints = append(extractedEndpoints, fleetnetworkingapi.Endpoint{
				Addresses: endpoint.Addresses,
			})
		}
	}
	return extractedEndpoints
}
