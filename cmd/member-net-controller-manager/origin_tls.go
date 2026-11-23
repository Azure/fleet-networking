/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func validateOriginTLSOptions(enabled, privateOrigins bool) error {
	if enabled && !privateOrigins {
		return errors.New("enable-origin-tls requires enable-private-link-origin-discovery")
	}
	return nil
}

// Readers must bypass the managers' caches: these checks run before cache startup.
func validateOriginTLSCRDs(ctx context.Context, policyReader, exportReader client.Reader) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	checks := []struct {
		reader  client.Reader
		name    string
		version string
		field   []string
	}{
		{policyReader, "backendtlspolicies.gateway.networking.k8s.io", "v1alpha3", nil},
		{exportReader, "internalserviceexports.networking.fleet.azure.com", "v1alpha1",
			[]string{"schema", "openAPIV3Schema", "properties", "spec", "properties", "originTLS"}},
	}
	for _, check := range checks {
		crd := &unstructured.Unstructured{}
		crd.SetGroupVersionKind(schema.GroupVersionKind{
			Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition",
		})
		if err := check.reader.Get(ctx, client.ObjectKey{Name: check.name}, crd); err != nil {
			return fmt.Errorf("read required origin TLS CRD %s: %w", check.name, err)
		}
		versions, _, err := unstructured.NestedSlice(crd.Object, "spec", "versions")
		if err != nil {
			return fmt.Errorf("read versions of origin TLS CRD %s: %w", check.name, err)
		}
		ready := false
		for _, raw := range versions {
			version, ok := raw.(map[string]interface{})
			if !ok || version["name"] != check.version || version["served"] != true {
				continue
			}
			ready = true
			if len(check.field) > 0 {
				_, ready, err = unstructured.NestedMap(version, check.field...)
				if err != nil {
					return fmt.Errorf("read originTLS schema in %s: %w", check.name, err)
				}
			}
			break
		}
		if !ready {
			return fmt.Errorf("origin TLS requires %s serving %s with the required schema; install upgraded CRDs before enabling", check.name, check.version)
		}
	}
	return nil
}
