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

func TestValidateOriginTLSCRDs(t *testing.T) {
	tests := []struct {
		name      string
		policy    map[string]interface{}
		export    map[string]interface{}
		readError error
		wantError string
	}{
		{name: "both required served schemas", policy: map[string]interface{}{"name": "v1alpha3", "served": true}, export: originTLSExportVersion()},
		{name: "missing policy CRD", wantError: "backendtlspolicies"},
		{name: "denied CRD read", readError: errors.New("forbidden"), wantError: "forbidden"},
		{name: "wrong policy version", policy: map[string]interface{}{"name": "v1alpha2", "served": true}, wantError: "v1alpha3"},
		{name: "unserved policy version", policy: map[string]interface{}{"name": "v1alpha3", "served": false}, wantError: "v1alpha3"},
		{name: "missing internal CRD", policy: map[string]interface{}{"name": "v1alpha3", "served": true}, wantError: "internalserviceexports"},
		{name: "old internal schema", policy: map[string]interface{}{"name": "v1alpha3", "served": true},
			export: map[string]interface{}{"name": "v1alpha1", "served": true}, wantError: "required schema"},
		{name: "unserved internal schema", policy: map[string]interface{}{"name": "v1alpha3", "served": true},
			export: map[string]interface{}{"name": "v1alpha1", "served": false}, wantError: "v1alpha1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &originTLSCRDReader{err: tt.readError, objects: map[string]map[string]interface{}{
				"backendtlspolicies.gateway.networking.k8s.io":      tt.policy,
				"internalserviceexports.networking.fleet.azure.com": tt.export,
			}}
			err := validateOriginTLSCRDs(context.Background(), reader, reader)
			if tt.wantError == "" && err != nil {
				t.Fatal(err)
			}
			if tt.wantError != "" && (err == nil || !strings.Contains(err.Error(), tt.wantError)) {
				t.Fatalf("validation error = %v, want %q", err, tt.wantError)
			}
		})
	}
}

func originTLSExportVersion() map[string]interface{} {
	return map[string]interface{}{
		"name": "v1alpha1", "served": true,
		"schema": map[string]interface{}{"openAPIV3Schema": map[string]interface{}{
			"properties": map[string]interface{}{"spec": map[string]interface{}{
				"properties": map[string]interface{}{"originTLS": map[string]interface{}{"type": "object"}},
			}},
		}},
	}
}

type originTLSCRDReader struct {
	client.Reader
	objects map[string]map[string]interface{}
	err     error
	reads   int
}

func (r *originTLSCRDReader) Get(ctx context.Context, key client.ObjectKey, object client.Object, _ ...client.GetOption) error {
	r.reads++
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("startup CRD read must have a deadline")
	}
	if r.err != nil {
		return r.err
	}
	version := r.objects[key.Name]
	if version == nil {
		return errors.New("CRD not found")
	}
	crd := object.(*unstructured.Unstructured)
	crd.Object["spec"] = map[string]interface{}{"versions": []interface{}{version}}
	return nil
}
