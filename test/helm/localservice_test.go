//go:build helm

/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package helm

import (
	"slices"
	"testing"
)

func TestLocalServiceChartOptIn(t *testing.T) {
	got := renderChart(t, helmSetFlag, enabledConfiguration+",afd.localServiceOrigins.enabled=true,afd.localServiceOrigins.resourceGroup=hub-node-rg")
	args := got.deployment.Spec.Template.Spec.Containers[0].Args
	for _, want := range []string{"--enable-local-service-origins=true", "--local-service-resource-group=hub-node-rg"} {
		if !slices.Contains(args, want) {
			t.Fatalf("missing local companion argument %s", want)
		}
	}
	for _, resource := range []string{"services", "endpointslices"} {
		found := false
		for _, rule := range got.clusterRole.Rules {
			if slices.Contains(rule.Resources, resource) && slices.Contains(rule.Verbs, "create") &&
				slices.Contains(rule.Verbs, "delete") && slices.Contains(rule.Verbs, "watch") {
				found = true
			}
		}
		if !found {
			t.Fatalf("local companion lifecycle RBAC missing for %s", resource)
		}
	}
	defaults := renderChart(t, helmSetFlag, enabledConfiguration)
	for _, rule := range defaults.clusterRole.Rules {
		if slices.Contains(rule.Resources, "services") || slices.Contains(rule.Resources, "endpointslices") {
			t.Fatal("local companion privileges must not be granted without opt-in")
		}
	}
}
