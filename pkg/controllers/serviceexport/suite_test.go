package serviceexport

import (
	"log"
	"os"
	"testing"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"k8s.io/client-go/kubernetes/scheme"
)

// TestMain bootstraps the test environment.
func TestMain(m *testing.M) {
	// Add custom APIs to the runtime scheme
	err := fleetnetv1alpha1.AddToScheme(scheme.Scheme)
	if err != nil {
		log.Fatalf("failed to add custom APIs to the runtime scheme: %v", err)
	}

	// Run the tests
	os.Exit(m.Run())
}
