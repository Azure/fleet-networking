/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package serviceexport

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayv1alpha3 "sigs.k8s.io/gateway-api/apis/v1alpha3"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	fleetnetv1beta1 "go.goms.io/fleet-networking/api/v1beta1"
	"go.goms.io/fleet-networking/pkg/common/origintls"
)

func memberTLSPolicy() *gatewayv1alpha3.BackendTLSPolicy {
	return &gatewayv1alpha3.BackendTLSPolicy{
		ObjectMeta: metav1.ObjectMeta{Namespace: "store", Name: "api-tls", UID: "policy-uid", Generation: 1,
			Annotations: map[string]string{origintls.ExportAnnotation: "true"}},
		Spec: gatewayv1alpha3.BackendTLSPolicySpec{
			TargetRefs: []gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName{{
				LocalPolicyTargetReference: gatewayv1alpha2.LocalPolicyTargetReference{Group: "", Kind: "Service", Name: "api"},
			}},
			Validation: gatewayv1alpha3.BackendTLSPolicyValidation{Hostname: "origin.example.com",
				WellKnownCACertificates: ptr.To(gatewayv1alpha3.WellKnownCACertificatesSystem)},
		},
	}
}

func memberTLSClient(t *testing.T, objects ...client.Object) client.WithWatch {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, install := range []func(*runtime.Scheme) error{corev1.AddToScheme, gatewayv1alpha3.Install, fleetnetv1alpha1.AddToScheme, fleetnetv1beta1.AddToScheme} {
		if err := install(scheme); err != nil {
			t.Fatal(err)
		}
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func TestResolveOriginTLSOptInAndSinglePolicy(t *testing.T) {
	tests := []struct {
		name   string
		change func(*gatewayv1alpha3.BackendTLSPolicy) []client.Object
		reason string
	}{
		{"valid", nil, originTLSReadyReason},
		{"unmarked belongs to another implementation", func(p *gatewayv1alpha3.BackendTLSPolicy) []client.Object {
			delete(p.Annotations, origintls.ExportAnnotation)
			return nil
		}, "PolicyNotExported"},
		{"exact annotation value", func(p *gatewayv1alpha3.BackendTLSPolicy) []client.Object {
			p.Annotations[origintls.ExportAnnotation] = "True"
			return nil
		}, "PolicyNotExported"},
		{"unmarked duplicate counts", func(p *gatewayv1alpha3.BackendTLSPolicy) []client.Object {
			other := p.DeepCopy()
			other.Name = "istio"
			other.Annotations = nil
			return []client.Object{other}
		}, "DuplicatePolicies"},
		{"identical duplicate counts", func(p *gatewayv1alpha3.BackendTLSPolicy) []client.Object {
			other := p.DeepCopy()
			other.Name = "duplicate"
			return []client.Object{other}
		}, "DuplicatePolicies"},
		{"unrelated policy ignored", func(p *gatewayv1alpha3.BackendTLSPolicy) []client.Object {
			other := p.DeepCopy()
			other.Name = "unrelated-policy"
			other.Spec.TargetRefs[0].Name = "unrelated-service"
			return []client.Object{other}
		}, originTLSReadyReason},
		{"wildcard rejected", func(p *gatewayv1alpha3.BackendTLSPolicy) []client.Object {
			p.Spec.Validation.Hostname = "*.example.com"
			return nil
		}, "UnsupportedPolicy"},
		{"IP rejected", func(p *gatewayv1alpha3.BackendTLSPolicy) []client.Object {
			p.Spec.Validation.Hostname = "10.0.0.1"
			return nil
		}, "UnsupportedPolicy"},
		{"custom CA rejected", func(p *gatewayv1alpha3.BackendTLSPolicy) []client.Object {
			p.Spec.Validation.CACertificateRefs = []gatewayv1.LocalObjectReference{{Kind: "ConfigMap", Name: "ca"}}
			return nil
		}, "UnsupportedPolicy"},
		{"section rejected", func(p *gatewayv1alpha3.BackendTLSPolicy) []client.Object {
			p.Spec.TargetRefs[0].SectionName = ptr.To(gatewayv1.SectionName("https"))
			return nil
		}, "UnsupportedPolicy"},
		{"multiple targets rejected", func(p *gatewayv1alpha3.BackendTLSPolicy) []client.Object {
			p.Spec.TargetRefs = append(p.Spec.TargetRefs, p.Spec.TargetRefs[0])
			return nil
		}, "UnsupportedPolicy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := memberTLSPolicy()
			objects := []client.Object{}
			if tt.change != nil {
				objects = tt.change(policy)
			}
			objects = append(objects, policy)
			c := memberTLSClient(t, objects...)
			if err := c.Get(context.Background(), client.ObjectKeyFromObject(policy), policy); err != nil {
				t.Fatal(err)
			}
			before := policy.DeepCopy()
			service, _, _ := privateOriginFixture()
			r := Reconciler{MemberAPIReader: c, Recorder: events.NewFakeRecorder(10)}
			got, err := r.resolveOriginTLS(context.Background(), service)
			if err != nil || got.Reason != tt.reason || got.Ready != (tt.reason == originTLSReadyReason) {
				t.Fatalf("intent=%+v error=%v want reason=%s", got, err, tt.reason)
			}
			var after gatewayv1alpha3.BackendTLSPolicy
			if err := c.Get(context.Background(), client.ObjectKeyFromObject(policy), &after); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before.Spec, after.Spec) || !reflect.DeepEqual(before.Status, after.Status) || !reflect.DeepEqual(before.Annotations, after.Annotations) {
				t.Fatal("member policy was mutated")
			}
			if got.Ready && (got.Hostname != "origin.example.com" || got.Trust != "System" || got.ServiceUID != string(service.UID) || got.Policy.ResourceVersion == "") {
				t.Fatalf("incomplete non-secret provenance: %+v", got)
			}
		})
	}
}

func TestOriginTLSMetadataRevocationAndRetargeting(t *testing.T) {
	ctx := context.Background()
	policy := memberTLSPolicy()
	c := memberTLSClient(t, policy)
	r := Reconciler{MemberAPIReader: c}
	service, _, _ := privateOriginFixture()
	before, err := r.resolveOriginTLS(ctx, service)
	if err != nil || !before.Ready {
		t.Fatalf("initial intent: %+v %v", before, err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(policy), policy); err != nil {
		t.Fatal(err)
	}
	generation := policy.Generation
	delete(policy.Annotations, origintls.ExportAnnotation)
	if err := c.Update(ctx, policy); err != nil {
		t.Fatal(err)
	}
	after, err := r.resolveOriginTLS(ctx, service)
	if err != nil || after.Ready || after.Reason != "PolicyNotExported" || policy.Generation != generation {
		t.Fatalf("metadata-only opt-out survived: %+v %v", after, err)
	}
	policy.Spec.TargetRefs[0].Name = "other"
	if got := r.exportsForTLSPolicy(ctx, policy); len(got) != 1 || got[0].Name != "other" {
		t.Fatalf("retarget dependency not enqueued: %v", got)
	}
	// controller-runtime maps both old and new objects on update.
	old := memberTLSPolicy()
	if got := r.exportsForTLSPolicy(ctx, old); len(got) != 1 || got[0].Name != "api" {
		t.Fatal("old target cannot be revoked")
	}
}

func TestExportOriginTLSReadFailurePreservesBaseline(t *testing.T) {
	ctx := context.Background()
	service, lbs, plss := privateOriginFixture()
	export := &fleetnetv1beta1.ServiceExport{ObjectMeta: metav1.ObjectMeta{Namespace: service.Namespace, Name: service.Name}}
	member, hub := memberTLSClient(t, export), memberTLSClient(t)
	wantErr := errors.New("policy read unavailable")
	reader := interceptor.NewClient(member, interceptor.Funcs{List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error { return wantErr }})
	r := Reconciler{MemberClient: member, MemberAPIReader: reader, HubClient: hub,
		HubNamespace: "fleet-member-test", MemberClusterID: "test", EnableOriginTLS: true, EnablePrivateLinkOriginDiscovery: true,
		ResourceGroupName: "node-rg", AzureLoadBalancerClient: lbs, AzurePrivateLinkServiceClient: plss}
	if _, err := r.exportService(ctx, export, service, time.Now(), 1); !errors.Is(err, wantErr) {
		t.Fatalf("read error lost: %v", err)
	}
	var got fleetnetv1alpha1.InternalServiceExport
	if err := hub.Get(ctx, client.ObjectKey{Namespace: r.HubNamespace, Name: formatInternalServiceExportName(export)}, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Spec.Ports) != len(service.Spec.Ports) || got.Spec.OriginTLS == nil || got.Spec.OriginTLS.Ready || got.Spec.OriginTLS.Reason != "PolicyReadFailed" || !got.Spec.PrivateLinkOrigin.Ready {
		t.Fatalf("baseline export lost or failed TLS hidden: %+v", got.Spec)
	}
}
