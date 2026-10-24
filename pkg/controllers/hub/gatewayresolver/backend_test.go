/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gatewayresolver

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	fleetnetv1beta1 "go.goms.io/fleet-networking/api/v1beta1"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
)

const otherName = "other"

type backendFixture struct {
	ref     gatewayv1.HTTPBackendRef
	service *fleetnetv1alpha1.ServiceImport
	exports []fleetnetv1alpha1.InternalServiceExport
}

func newBackendFixture() backendFixture {
	service := &fleetnetv1alpha1.ServiceImport{
		ObjectMeta: metav1.ObjectMeta{Namespace: "store", Name: "api"},
		Status: fleetnetv1alpha1.ServiceImportStatus{
			Ports:    []fleetnetv1alpha1.ServicePort{{Name: "http", Protocol: corev1.ProtocolTCP, Port: 8080}},
			Clusters: []fleetnetv1alpha1.ClusterStatus{{Cluster: "west"}, {Cluster: "east"}},
		},
	}
	fixture := backendFixture{
		service: service,
		ref: gatewayv1.HTTPBackendRef{BackendRef: gatewayv1.BackendRef{
			BackendObjectReference: gatewayv1.BackendObjectReference{
				Group: ptr.To(gatewayv1.Group(fleetnetv1alpha1.GroupVersion.Group)),
				Kind:  ptr.To(gatewayv1.Kind(serviceImportKind)),
				Name:  "api", Port: ptr.To(gatewayv1.PortNumber(8080)),
			},
		}},
	}
	for _, cluster := range []string{"west", "east"} {
		fixture.exports = append(fixture.exports, fleetnetv1alpha1.InternalServiceExport{
			ObjectMeta: metav1.ObjectMeta{Namespace: "fleet-member-" + cluster, Name: "store-api", Generation: 2},
			Spec: fleetnetv1alpha1.InternalServiceExportSpec{
				Ports:                  append([]fleetnetv1alpha1.ServicePort(nil), service.Status.Ports...),
				Type:                   corev1.ServiceTypeLoadBalancer,
				IsInternalLoadBalancer: true,
				ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
					ClusterID: cluster, Namespace: "store", Name: "api", Kind: "Service", APIVersion: "v1",
					UID: types.UID("service-" + cluster), NamespacedName: "store/api",
				},
				PrivateLinkOrigin: &fleetnetv1alpha1.PrivateLinkOrigin{
					ResourceID: fmt.Sprintf("/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg/providers/Microsoft.Network/privateLinkServices/%s", cluster),
					Location:   "eastus", InternalIP: "10.0.0.10", Ready: true, Reason: "Ready",
				},
			},
			Status: fleetnetv1alpha1.InternalServiceExportStatus{Conditions: []metav1.Condition{{
				Type: string(fleetnetv1beta1.ServiceExportConflict), Status: metav1.ConditionFalse, ObservedGeneration: 2,
			}}},
		})
	}
	return fixture
}

func TestResolvePrivateBackend(t *testing.T) {
	fixture := newBackendFixture()
	beforeService := fixture.service.DeepCopy()
	beforeExport := fixture.exports[0].DeepCopy()
	got, err := Resolve("store", fixture.ref, fixture.service, fixture.exports)
	if err != nil {
		t.Fatal(err)
	}
	if got.Namespace != "store" || got.Name != "api" || got.Port != 8080 || got.RouteWeight != 1 ||
		got.HealthProbe != (gatewaymodel.HealthProbe{Path: "/", Method: "GET", Protocol: "HTTP", IntervalSeconds: 30}) ||
		got.OriginHostHeader != "" {
		t.Fatalf("unexpected backend or platform policy: %#v", got)
	}
	if len(got.Origins) != 2 || got.Origins[0].Cluster != "east" || got.Origins[1].Cluster != "west" ||
		got.Origins[0].ServiceUID != "service-east" || got.Origins[0].Endpoint != "10.0.0.10" ||
		got.Origins[0].Weight != 1 || got.Origins[0].PrivateLinkLocation != "eastus" {
		t.Fatalf("unexpected origins: %#v", got.Origins)
	}
	slices.Reverse(fixture.exports)
	slices.Reverse(fixture.service.Status.Clusters)
	reordered, err := Resolve("store", fixture.ref, fixture.service, fixture.exports)
	if err != nil || !reflect.DeepEqual(got, reordered) {
		t.Fatalf("resolution depends on input order: %#v, %v", reordered, err)
	}
	slices.Reverse(fixture.service.Status.Clusters)
	if !reflect.DeepEqual(beforeService, fixture.service) || !reflect.DeepEqual(beforeExport, &fixture.exports[1]) {
		t.Fatal("resolution mutated Kubernetes input")
	}
	got.Origins[0].Endpoint = "mutated"
	if fixture.exports[0].Spec.PrivateLinkOrigin.InternalIP != "10.0.0.10" {
		t.Fatal("resolved model aliases input")
	}
}

func TestResolveRejectsInvalidOrUnavailableBackends(t *testing.T) {
	tests := []struct {
		name           string
		change         func(*backendFixture)
		reason         string
		referenceValid bool
	}{
		{"core Service", func(f *backendFixture) { f.ref.Group = nil; f.ref.Kind = nil }, "InvalidKind", false},
		{"upstream MCS group", func(f *backendFixture) { f.ref.Group = ptr.To(gatewayv1.Group("multicluster.x-k8s.io")) }, "InvalidKind", false},
		{"missing ServiceImport", func(f *backendFixture) { f.service = nil }, "BackendNotFound", false},
		{"wrong ServiceImport", func(f *backendFixture) { f.service.Name = otherName }, "BackendNotFound", false},
		{"missing port", func(f *backendFixture) { f.ref.Port = nil }, "PortNotFound", false},
		{"unknown port", func(f *backendFixture) { f.ref.Port = ptr.To(gatewayv1.PortNumber(80)) }, "PortNotFound", false},
		{"invalid route weight", func(f *backendFixture) { f.ref.Weight = ptr.To(int32(-1)) }, "UnsupportedValue", false},
		{"excessive route weight", func(f *backendFixture) { f.ref.Weight = ptr.To(int32(1000001)) }, "UnsupportedValue", false},
		{"unsupported backend filter", func(f *backendFixture) {
			f.ref.Filters = []gatewayv1.HTTPRouteFilter{{Type: gatewayv1.HTTPRouteFilterRequestMirror}}
		}, "UnsupportedValue", false},
		{"UDP port", func(f *backendFixture) { f.service.Status.Ports[0].Protocol = corev1.ProtocolUDP }, "UnsupportedProtocol", false},
		{"HTTPS app protocol", func(f *backendFixture) { f.service.Status.Ports[0].AppProtocol = ptr.To("https") }, "UnsupportedProtocol", false},
		{"headless import", func(f *backendFixture) { f.service.Status.Type = fleetnetv1alpha1.Headless }, "UnsupportedTopology", true},
		{"retired AFD configuration", func(f *backendFixture) {
			f.service.Annotations = map[string]string{"networking.fleet.azure.com/afd-health-probe-path": "/healthz"}
		}, "UnsupportedConfiguration", true},
		{"no exporting clusters", func(f *backendFixture) { f.service.Status.Clusters = nil }, "BackendUnavailable", true},
		{"missing member export", func(f *backendFixture) { f.exports = f.exports[1:] }, "BackendUnavailable", true},
		{"old member without discovery", func(f *backendFixture) { f.exports[0].Spec.PrivateLinkOrigin = nil }, "PrivateOriginUnavailable", true},
		{"public metadata is never a fallback", func(f *backendFixture) {
			f.exports[0].Spec.PrivateLinkOrigin = nil
			f.exports[0].Spec.PublicIPResourceID = ptr.To("/public/ip")
			f.service.Status.IPs = []string{"203.0.113.10"}
		}, "PrivateOriginUnavailable", true},
		{"pending discovery", func(f *backendFixture) { f.exports[0].Spec.PrivateLinkOrigin.Ready = false }, "PrivateOriginUnavailable", true},
		{"public topology", func(f *backendFixture) { f.exports[0].Spec.IsInternalLoadBalancer = false }, "UnsupportedTopology", true},
		{"ClusterIP topology", func(f *backendFixture) { f.exports[0].Spec.Type = corev1.ServiceTypeClusterIP }, "UnsupportedTopology", true},
		{"source UID absent", func(f *backendFixture) { f.exports[0].Spec.ServiceReference.UID = "" }, "InvalidExport", true},
		{"source namespaced name mismatch", func(f *backendFixture) { f.exports[0].Spec.ServiceReference.NamespacedName = "other/api" }, "InvalidExport", true},
		{"source namespace absent", func(f *backendFixture) { f.exports[0].Spec.ServiceReference.Namespace = "" }, "BackendUnavailable", true},
		{"wrong member namespace", func(f *backendFixture) { f.exports[0].Namespace = "fleet-member-other" }, "InvalidExport", true},
		{"source is not a Service", func(f *backendFixture) { f.exports[0].Spec.ServiceReference.Kind = "Pod" }, "InvalidExport", true},
		{"conflicting ports", func(f *backendFixture) { f.exports[0].Spec.Ports[0].Port = 80 }, "InvalidExport", true},
		{"conflict status absent", func(f *backendFixture) { f.exports[0].Status.Conditions = nil }, "InvalidExport", true},
		{"conflicted export", func(f *backendFixture) { f.exports[0].Status.Conditions[0].Status = metav1.ConditionTrue }, "InvalidExport", true},
		{"unknown conflict state", func(f *backendFixture) { f.exports[0].Status.Conditions[0].Status = metav1.ConditionUnknown }, "InvalidExport", true},
		{"stale conflict status", func(f *backendFixture) { f.exports[0].Status.Conditions[0].ObservedGeneration-- }, "InvalidExport", true},
		{"invalid member weight", func(f *backendFixture) { f.exports[0].Spec.Weight = ptr.To(int64(1001)) }, "InvalidExport", true},
		{"no active member weight", func(f *backendFixture) {
			for i := range f.exports {
				f.exports[i].Spec.Weight = ptr.To(int64(0))
			}
		}, "BackendUnavailable", true},
		{"duplicate export", func(f *backendFixture) { f.exports = append(f.exports, *f.exports[0].DeepCopy()) }, "ConflictingExports", true},
		{"recreated source with duplicate export", func(f *backendFixture) {
			replacement := f.exports[0].DeepCopy()
			replacement.Spec.ServiceReference.UID = "new-service"
			f.exports = append(f.exports, *replacement)
		}, "ConflictingExports", true},
		{"deleting member export", func(f *backendFixture) { now := metav1.Now(); f.exports[0].DeletionTimestamp = &now }, "BackendUnavailable", true},
		{"invalid PLS identity", func(f *backendFixture) { f.exports[0].Spec.PrivateLinkOrigin.ResourceID = "/invalid" }, "InvalidExport", true},
		{"public DNS endpoint", func(f *backendFixture) { f.exports[0].Spec.PrivateLinkOrigin.InternalIP = "example.com" }, "InvalidExport", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newBackendFixture()
			tt.change(&fixture)
			got, err := Resolve("store", fixture.ref, fixture.service, fixture.exports)
			var resolutionErr *ResolutionError
			if !errors.As(err, &resolutionErr) || resolutionErr.Reason != tt.reason || resolutionErr.ReferenceValid != tt.referenceValid {
				t.Fatalf("error = %#v, want reason=%s referenceValid=%v", err, tt.reason, tt.referenceValid)
			}
			if !reflect.DeepEqual(got, gatewaymodel.Backend{}) {
				t.Fatalf("failed resolution leaked partial backend: %#v", got)
			}
		})
	}
}

func TestResolveWeightsAndMemberRemoval(t *testing.T) {
	tests := []struct {
		name   string
		change func(*backendFixture)
	}{
		{"removed member is ignored", func(f *backendFixture) { f.service.Status.Clusters = f.service.Status.Clusters[1:] }},
		{"zero-weight member does not need discovery", func(f *backendFixture) {
			f.exports[0].Spec.Weight = ptr.To(int64(0))
			f.exports[0].Spec.PrivateLinkOrigin = nil
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newBackendFixture()
			tt.change(&fixture)
			fixture.ref.Weight = ptr.To(int32(1000000))
			fixture.exports[1].Spec.Weight = ptr.To(int64(1000))
			got, err := Resolve("store", fixture.ref, fixture.service, fixture.exports)
			if err != nil || len(got.Origins) != 1 || got.Origins[0].Cluster != "east" ||
				got.Origins[0].Weight != 1000 || got.RouteWeight != 1000000 {
				t.Fatalf("unexpected active members/weights: %#v, %v", got, err)
			}
		})
	}
}

func TestResolveCachedSourceTypeMeta(t *testing.T) {
	fixture := newBackendFixture()
	for i := range fixture.exports {
		// Cached member Services can have empty TypeMeta.
		fixture.exports[i].Spec.ServiceReference.Kind = ""
		fixture.exports[i].Spec.ServiceReference.APIVersion = ""
	}
	if _, err := Resolve("store", fixture.ref, fixture.service, fixture.exports); err != nil {
		t.Fatal(err)
	}
}

func TestResolveCrossNamespaceReference(t *testing.T) {
	fixture := newBackendFixture()
	fixture.ref.Namespace = ptr.To(gatewayv1.Namespace("store"))
	if _, err := Resolve(otherName, fixture.ref, fixture.service, fixture.exports); err != nil {
		t.Fatal(err)
	}
}
