/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package internalsvcexport

import (
	"log"
	"testing"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"k8s.io/client-go/kubernetes/scheme"
)

func TestMain(m *testing.M) {
	// Bootstrap the test environment

	// Add custom APIs to the runtime scheme
	err := fleetnetv1alpha1.AddToScheme(scheme.Scheme)
	if err != nil {
		log.Fatalf("failed to add custom APIs to the runtime scheme: %v", err)
	}
}
