/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package framework

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

func TestSharedRunWorkloadNames(t *testing.T) {
	t.Setenv("E2E_SHARED_RESOURCE_GROUP", "true")
	t.Setenv("E2E_RESOURCE_PREFIX", "fne0123456789abcdef")
	t.Setenv("REGISTRY", "fne0123456789abcdefacr.azurecr.io")
	if got := appImage(); got != "fne0123456789abcdefacr.azurecr.io/app" {
		t.Fatalf("app image uses the wrong run registry: %s", got)
	}
	namespace := UniqueTestNamespace()
	if !strings.HasPrefix(namespace, "fne0123456789abcdef-") {
		t.Fatalf("namespace does not identify this run: %s", namespace)
	}
	workload := WorkloadManager{
		namespace: namespace,
		service:   corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "hello-world-service"}},
	}
	label := workload.BuildServiceDNSLabelName(&Cluster{name: "member-1"})
	if problems := validation.IsDNS1123Label(label); len(problems) != 0 {
		t.Fatalf("shared run DNS label %q is invalid: %v", label, problems)
	}

	t.Setenv("E2E_SHARED_RESOURCE_GROUP", "false")
	t.Setenv("REGISTRY", "")
	t.Setenv("AZURE_RESOURCE_GROUP", "fleet-legacy-123")
	if got := appImage(); got != "fleetlegacy123.azurecr.io/app" {
		t.Fatalf("legacy registry naming changed: %s", got)
	}
	if got := UniqueTestNamespace(); !strings.HasPrefix(got, "my-ns-") {
		t.Fatalf("legacy namespace naming changed: %s", got)
	}
}
