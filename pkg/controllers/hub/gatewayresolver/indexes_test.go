/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gatewayresolver

import (
	"context"
	"errors"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

func indexedRoute() *gatewayv1.HTTPRoute {
	ref := newBackendFixture().ref
	ref.Namespace = ptr.To(gatewayv1.Namespace("store"))
	return &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Namespace: "routes", Name: "api"},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gatewayv1.ParentReference{{
				Name: "global", Namespace: ptr.To(gatewayv1.Namespace("gateways")),
			}}},
			Rules: []gatewayv1.HTTPRouteRule{{BackendRefs: []gatewayv1.HTTPBackendRef{ref, ref}}},
		},
	}
}

func TestRouteReferenceIndexes(t *testing.T) {
	route := indexedRoute()
	tests := []struct {
		name    string
		extract client.IndexerFunc
		change  func(*gatewayv1.HTTPRoute)
		want    []string
	}{
		{"gateway parent", routeGatewayIndex, func(*gatewayv1.HTTPRoute) {}, []string{"gateways/global"}},
		{"default parent namespace", routeGatewayIndex, func(r *gatewayv1.HTTPRoute) { r.Spec.ParentRefs[0].Namespace = nil }, []string{"routes/global"}},
		{"foreign parent group", routeGatewayIndex, func(r *gatewayv1.HTTPRoute) { r.Spec.ParentRefs[0].Group = ptr.To(gatewayv1.Group("other.example")) }, nil},
		{"foreign parent kind", routeGatewayIndex, func(r *gatewayv1.HTTPRoute) { r.Spec.ParentRefs[0].Kind = ptr.To(gatewayv1.Kind("Service")) }, nil},
		{"deduplicated backend", routeServiceImportIndex, func(*gatewayv1.HTTPRoute) {}, []string{"store/api"}},
		{"backend namespace", routeBackendNamespaceIndex, func(*gatewayv1.HTTPRoute) {}, []string{"store"}},
		{"default backend namespace", routeServiceImportIndex, func(r *gatewayv1.HTTPRoute) {
			for i := range r.Spec.Rules[0].BackendRefs {
				r.Spec.Rules[0].BackendRefs[i].Namespace = nil
			}
		}, []string{"routes/api"}},
		{"non-Fleet backends ignored", routeServiceImportIndex, func(r *gatewayv1.HTTPRoute) {
			for i := range r.Spec.Rules[0].BackendRefs {
				r.Spec.Rules[0].BackendRefs[i].Group = nil
			}
		}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			copy := route.DeepCopy()
			tt.change(copy)
			if got := tt.extract(copy); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("keys = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIndexedReferencesFollowChanges(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := gatewayv1.Install(scheme); err != nil {
		t.Fatal(err)
	}
	route := indexedRoute()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(route).
		WithIndex(&gatewayv1.HTTPRoute{}, HTTPRouteGatewayField, routeGatewayIndex).
		WithIndex(&gatewayv1.HTTPRoute{}, HTTPRouteBackendNamespaceField, routeBackendNamespaceIndex).
		WithIndex(&gatewayv1.HTTPRoute{}, HTTPRouteServiceImportField, routeServiceImportIndex).Build()
	ctx := context.Background()
	fields := map[string]string{HTTPRouteGatewayField: "gateways/global", HTTPRouteServiceImportField: "store/api", HTTPRouteBackendNamespaceField: "store"}
	for field, value := range fields {
		var routes gatewayv1.HTTPRouteList
		if err := c.List(ctx, &routes, client.MatchingFields{field: value}); err != nil || len(routes.Items) != 1 {
			t.Fatalf("indexed lookup %s: %d routes, %v", field, len(routes.Items), err)
		}
	}
	route.Spec.ParentRefs = nil
	route.Spec.Rules = nil
	if err := c.Update(ctx, route); err != nil {
		t.Fatal(err)
	}
	for field, value := range fields {
		var routes gatewayv1.HTTPRouteList
		if err := c.List(ctx, &routes, client.MatchingFields{field: value}); err != nil || len(routes.Items) != 0 {
			t.Fatalf("stale index %s retained reference: %d routes, %v", field, len(routes.Items), err)
		}
	}
}

func TestIndexesDeduplicateAcrossRulesAndParents(t *testing.T) {
	route := indexedRoute()
	route.Spec.ParentRefs = append(route.Spec.ParentRefs,
		route.Spec.ParentRefs[0],
		gatewayv1.ParentReference{Name: otherName},
	)
	other := route.Spec.Rules[0].BackendRefs[0]
	other.Name = otherName
	route.Spec.Rules = append(route.Spec.Rules, gatewayv1.HTTPRouteRule{BackendRefs: []gatewayv1.HTTPBackendRef{other}})
	if got := routeGatewayIndex(route); !reflect.DeepEqual(got, []string{"gateways/global", "routes/other"}) {
		t.Fatalf("parent keys = %v", got)
	}
	if got := routeServiceImportIndex(route); !reflect.DeepEqual(got, []string{"store/api", "store/other"}) {
		t.Fatalf("backend keys = %v", got)
	}
}

type recordingFieldIndexer struct {
	fields []string
	fail   string
	err    error
}

func (r *recordingFieldIndexer) IndexField(_ context.Context, _ client.Object, field string, _ client.IndexerFunc) error {
	r.fields = append(r.fields, field)
	if field == r.fail {
		return r.err
	}
	return nil
}

func TestSetupIndexes(t *testing.T) {
	fields := []string{GatewayClassField, HTTPRouteGatewayField, HTTPRouteServiceImportField, HTTPRouteBackendNamespaceField, ExportServiceField}
	for _, fail := range append([]string{""}, fields...) {
		t.Run("failure="+fail, func(t *testing.T) {
			expectedErr := errors.New("index setup failed")
			indexer := &recordingFieldIndexer{fail: fail, err: expectedErr}
			err := SetupIndexes(context.Background(), indexer)
			if (fail == "" && err != nil) || (fail != "" && !errors.Is(err, expectedErr)) {
				t.Fatalf("setup error = %v, fail=%s", err, fail)
			}
			count := len(fields)
			if fail != "" {
				for i, field := range fields {
					if field == fail {
						count = i + 1
					}
				}
			}
			if !reflect.DeepEqual(indexer.fields, fields[:count]) {
				t.Fatalf("setup did not stop at error: %v", indexer.fields)
			}
		})
	}
}

func TestSourceAndClassIndexes(t *testing.T) {
	gateway := &gatewayv1.Gateway{Spec: gatewayv1.GatewaySpec{GatewayClassName: "azure-fleet-afd"}}
	if got := gatewayClassIndex(gateway); !reflect.DeepEqual(got, []string{"azure-fleet-afd"}) {
		t.Fatalf("class index = %v", got)
	}
	export := &fleetnetv1alpha1.InternalServiceExport{Spec: fleetnetv1alpha1.InternalServiceExportSpec{
		ServiceReference: fleetnetv1alpha1.ExportedObjectReference{NamespacedName: "store/api"},
	}}
	if got := exportServiceIndex(export); !reflect.DeepEqual(got, []string{"store/api"}) {
		t.Fatalf("source index = %v", got)
	}
}

func TestDependentRoutes(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := gatewayv1.Install(scheme); err != nil {
		t.Fatal(err)
	}
	route := indexedRoute()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(route).
		WithIndex(&gatewayv1.HTTPRoute{}, HTTPRouteGatewayField, routeGatewayIndex).
		WithIndex(&gatewayv1.HTTPRoute{}, HTTPRouteServiceImportField, routeServiceImportIndex).
		WithIndex(&gatewayv1.HTTPRoute{}, HTTPRouteBackendNamespaceField, routeBackendNamespaceIndex).Build()
	tests := []struct {
		name   string
		object client.Object
		count  int
		fail   bool
	}{
		{"Gateway", &gatewayv1.Gateway{ObjectMeta: metav1.ObjectMeta{Namespace: "gateways", Name: "global"}}, 1, false},
		{"backend", &fleetnetv1alpha1.ServiceImport{ObjectMeta: metav1.ObjectMeta{Namespace: "store", Name: "api"}}, 1, false},
		{"export", &fleetnetv1alpha1.InternalServiceExport{Spec: fleetnetv1alpha1.InternalServiceExportSpec{ServiceReference: fleetnetv1alpha1.ExportedObjectReference{NamespacedName: "store/api"}}}, 1, false},
		{"removed wildcard grant", &gatewayv1beta1.ReferenceGrant{ObjectMeta: metav1.ObjectMeta{Namespace: "store"}}, 1, false},
		{"namespace policy", &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "routes"}}, 1, false},
		{"unrelated grant namespace", &gatewayv1beta1.ReferenceGrant{ObjectMeta: metav1.ObjectMeta{Namespace: otherName}}, 0, false},
		{"unsupported dependency", &corev1.Service{}, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DependentRoutes(context.Background(), c, tt.object)
			if (err != nil) != tt.fail || len(got) != tt.count {
				t.Fatalf("dependent routes = %d, %v; want count %d, fail=%v", len(got), err, tt.count, tt.fail)
			}
		})
	}
}
