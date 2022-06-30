/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package endpointslice

import (
	"fmt"

	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/util/uuid"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

// isEndpointSlicePermanentlyUnexportable returns if an EndpointSlice is permanently unexportable.
func isEndpointSlicePermanentlyUnexportable(endpointSlice *discoveryv1.EndpointSlice) bool {
	// At this moment only IPv4 endpointslices can be exported; note that AddressType is an immutable field.
	return endpointSlice.AddressType != discoveryv1.AddressTypeIPv4
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
