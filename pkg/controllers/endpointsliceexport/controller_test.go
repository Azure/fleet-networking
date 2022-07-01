/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package endpointsliceexport

import (
	"log"
	"os"
	"testing"

	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/client-go/kubernetes/scheme"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

// TestMain bootstraps the test environment.
func TestMain(m *testing.M) {
	// Add custom APIs to the runtime scheme
	err := fleetnetv1alpha1.AddToScheme(scheme.Scheme)
	if err != nil {
		log.Fatalf("failed to add custom APIs to the runtime scheme: %v", err)
	}

	os.Exit(m.Run())
}

func TestIsEndpointSliceExportLinkedWithEndpointSlice(t *testing.T) {
	testCases := []struct {
		name                string
		endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport
		endpointSlice       *discoveryv1.EndpointSlice
	}{
		{},
		{},
		{},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {})
	}
}
