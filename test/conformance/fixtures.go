/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package conformance

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// HTTPFixtures selects explicitly named prerequisites from the pinned upstream manifests.
// Keys are Kind/Name; ambiguous names across namespaces are rejected.
func HTTPFixtures(manifests fs.FS, gatewayClass string, keys []string) ([]unstructured.Unstructured, error) {
	selected := make(map[string]bool, len(keys))
	for _, key := range keys {
		if selected[key] {
			return nil, fmt.Errorf("duplicate requested fixture %s", key)
		}
		selected[key] = true
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("select at least one upstream base fixture")
	}
	data, err := fs.ReadFile(manifests, "base/manifests.yaml")
	if err != nil {
		return nil, err
	}
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	var resources []unstructured.Unstructured
	found := make(map[string]bool)
	for {
		var resource unstructured.Unstructured
		err := decoder.Decode(&resource)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("decode upstream HTTP fixtures: %w", err)
		}
		key := resource.GetKind() + "/" + resource.GetName()
		if !selected[key] {
			continue
		}
		if resource.GetKind() == "Gateway" {
			if err := unstructured.SetNestedField(resource.Object, gatewayClass, "spec", "gatewayClassName"); err != nil {
				return nil, err
			}
		}
		if found[key] {
			return nil, fmt.Errorf("duplicate upstream prerequisite %s", key)
		}
		found[key] = true
		resources = append(resources, resource)
	}
	for _, key := range keys {
		if !found[key] {
			return nil, fmt.Errorf("upstream base prerequisite %s was not found", key)
		}
	}
	return resources, nil
}
