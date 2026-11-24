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

}

func TestMissingSharedRunConfiguration(t *testing.T) {
	for _, tt := range []struct {
		name string
		key  string
		call func()
	}{
		{"registry", "REGISTRY", func() { appImage() }},
		{"run prefix", "E2E_RESOURCE_PREFIX", func() { UniqueTestNamespace() }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.key, "")
			defer func() {
				if recover() == nil {
					t.Fatal("missing run configuration must fail explicitly")
				}
			}()
			tt.call()
		})
	}
}
