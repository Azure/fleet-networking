/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/yaml"
)

func TestInternalServiceExportPrivateOriginCompatibility(t *testing.T) {
	var old InternalServiceExport
	if err := json.Unmarshal([]byte(`{"spec":{"ports":[],"serviceReference":{}}}`), &old); err != nil {
		t.Fatal(err)
	}

	if old.Spec.PrivateLinkOrigin != nil {
		t.Fatal("old exports must leave private discovery unknown")
	}
	export := &InternalServiceExport{Spec: InternalServiceExportSpec{
		PrivateLinkOrigin: &PrivateLinkOrigin{ResourceID: "resource", Location: "eastus", InternalIP: "10.0.0.10", Ready: true, Reason: "Ready"},
	}}
	copy := export.DeepCopy()
	copy.Spec.PrivateLinkOrigin.ResourceID = "replacement"
	if export.Spec.PrivateLinkOrigin.ResourceID != "resource" {
		t.Fatal("DeepCopy aliases private origin metadata")
	}
	data, err := json.Marshal(export)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip InternalServiceExport
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if roundTrip.Spec.PrivateLinkOrigin == nil || *roundTrip.Spec.PrivateLinkOrigin != *export.Spec.PrivateLinkOrigin {
		t.Fatalf("private origin did not round-trip: %s", data)
	}
}

func TestInternalServiceExportPrivateOriginSchema(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "crd", "bases", "networking.fleet.azure.com_internalserviceexports.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var crd apiextensionsv1.CustomResourceDefinition
	if err := yaml.Unmarshal(data, &crd); err != nil {
		t.Fatal(err)
	}
	for _, version := range crd.Spec.Versions {
		spec := version.Schema.OpenAPIV3Schema.Properties["spec"]
		if slices.Contains(spec.Required, "privateLinkOrigin") {
			t.Fatal("private metadata must remain optional for older members")
		}
		origin, found := spec.Properties["privateLinkOrigin"]
		if !found || origin.Type != "object" {
			t.Fatal("internal CRD would prune private origin metadata")
		}
		for _, field := range []string{"resourceID", "location", "internalIP", "ready", "reason"} {
			if _, found := origin.Properties[field]; !found {
				t.Fatalf("internal CRD is missing %s", field)
			}
		}
		if !slices.Contains(origin.Required, "ready") || !slices.Contains(origin.Required, "reason") {
			t.Fatal("discovery readiness and reason must be explicit")
		}
	}
}
