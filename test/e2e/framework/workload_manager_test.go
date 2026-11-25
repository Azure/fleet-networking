/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package framework

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/util/validation"
)

func TestAppImage(t *testing.T) {
	t.Run("run registry", func(t *testing.T) {
		t.Setenv("REGISTRY", "fne0123456789abcdefacr.azurecr.io")
		if got := appImage(); got != "fne0123456789abcdefacr.azurecr.io/app" {
			t.Fatalf("app image uses the wrong run registry: %s", got)
		}
	})
	t.Run("missing registry", func(t *testing.T) {
		t.Setenv("REGISTRY", "")
		defer func() {
			if recover() == nil {
				t.Fatal("missing registry must fail explicitly")
			}
		}()
		appImage()
	})
}

func TestUniqueTestNamespace(t *testing.T) {
	t.Run("run prefix", func(t *testing.T) {
		t.Setenv("E2E_RESOURCE_PREFIX", "fne0123456789abcdef")
		namespace := UniqueTestNamespace()
		if !strings.HasPrefix(namespace, "fne0123456789abcdef-") {
			t.Fatalf("namespace does not identify this run: %s", namespace)
		}
		if problems := validation.IsDNS1123Label(namespace); len(problems) != 0 {
			t.Fatalf("namespace %q is invalid: %v", namespace, problems)
		}
	})
	t.Run("missing prefix", func(t *testing.T) {
		t.Setenv("E2E_RESOURCE_PREFIX", "")
		defer func() {
			if recover() == nil {
				t.Fatal("missing run prefix must fail explicitly")
			}
		}()
		UniqueTestNamespace()
	})
}
