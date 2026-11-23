/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestValidateOriginTLSOptions(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, privateOrigins := range []bool{false, true} {
			err := validateOriginTLSOptions(enabled, privateOrigins)
			if (err != nil) != (enabled && !privateOrigins) {
				t.Fatalf("TLS=%v discovery=%v error=%v", enabled, privateOrigins, err)
			}
		}
	}
}

func TestValidateOriginTLSCRDsUsesMemberAndHubReaders(t *testing.T) {
	member := &originTLSCRDReader{name: "backendtlspolicies.gateway.networking.k8s.io", version: map[string]interface{}{
		"name": "v1alpha3", "served": true,
	}}
	hub := &originTLSCRDReader{name: "internalserviceexports.networking.fleet.azure.com", version: map[string]interface{}{
		"name": "v1alpha1", "served": true,
		"schema": map[string]interface{}{"openAPIV3Schema": map[string]interface{}{
			"properties": map[string]interface{}{"spec": map[string]interface{}{
				"properties": map[string]interface{}{"originTLS": map[string]interface{}{"type": "object"}},
			}},
		}},
	}}
	if err := validateOriginTLSCRDs(context.Background(), member, hub); err != nil {
		t.Fatal(err)
	}
	hub.version = map[string]interface{}{"name": "v1alpha1", "served": true}
	if err := validateOriginTLSCRDs(context.Background(), member, hub); err == nil {
		t.Fatal("accepted a hub schema that prunes originTLS")
	}
	member.version["served"] = false
	if err := validateOriginTLSCRDs(context.Background(), member, hub); err == nil || !strings.Contains(err.Error(), "v1alpha3") {
		t.Fatalf("unserved BackendTLSPolicy error = %v", err)
	}
}

type originTLSCRDReader struct {
	client.Reader
	name    string
	version map[string]interface{}
}

func (r *originTLSCRDReader) Get(ctx context.Context, key client.ObjectKey, object client.Object, _ ...client.GetOption) error {
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("startup CRD read must have a deadline")
	}
	if key.Name != r.name {
		return errors.New("CRD read used the wrong cluster")
	}
	crd := object.(*unstructured.Unstructured)
	crd.Object["spec"] = map[string]interface{}{"versions": []interface{}{r.version}}
	return nil
}
