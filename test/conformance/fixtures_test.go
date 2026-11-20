/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package conformance

import (
	"testing"
	"testing/fstest"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestHTTPFixturesRequireExactPrerequisites(t *testing.T) {
	manifests := fstest.MapFS{"base/manifests.yaml": {Data: []byte(`kind: Namespace
metadata: {name: gateway-conformance-infra}
---
kind: Gateway
metadata: {name: same-namespace}
spec: {gatewayClassName: placeholder}
---
kind: Service
metadata: {name: infra-backend-v1}
spec: {ports: [{port: 8080, targetPort: 3000}]}
---
kind: Deployment
metadata: {name: infra-backend-v1}
---
kind: Secret
metadata: {name: ignored-https}
`)}}
	keys := []string{"Namespace/gateway-conformance-infra", "Gateway/same-namespace",
		"Service/infra-backend-v1", "Deployment/infra-backend-v1"}
	resources, err := HTTPFixtures(manifests, "afd", keys)
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 4 {
		t.Fatalf("unexpected fixture count %d", len(resources))
	}
	for _, resource := range resources {
		if resource.GetKind() == "Gateway" {
			class, _, err := unstructured.NestedString(resource.Object, "spec", "gatewayClassName")
			if err != nil || class != "afd" {
				t.Fatalf("Gateway class placeholder not replaced: %s %v", class, err)
			}
		}
	}
	manifests["base/manifests.yaml"].Data = []byte("kind: Namespace\nmetadata: {name: gateway-conformance-infra}\n")
	if _, err := HTTPFixtures(manifests, "afd", keys); err == nil {
		t.Fatal("incomplete upstream prerequisites must not silently pass")
	}
	if _, err := HTTPFixtures(manifests, "afd", []string{keys[0], keys[0]}); err == nil {
		t.Fatal("duplicate requested fixtures must not silently pass")
	}
}
