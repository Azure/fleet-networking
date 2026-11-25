/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gatewayresolver

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha3 "sigs.k8s.io/gateway-api/apis/v1alpha3"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/origintls"
)

func tlsBackendFixture() backendFixture {
	f := newBackendFixture()
	f.service.UID = "service-import-uid"
	f.service.Status.Ports[0].AppProtocol = ptr.To("https")
	for i := range f.exports {
		export := &f.exports[i]
		export.Spec.Ports = append([]fleetnetv1alpha1.ServicePort(nil), f.service.Status.Ports...)
		export.Spec.OriginTLS = &fleetnetv1alpha1.OriginTLS{Ready: true, Reason: "Ready", ObservedAt: metav1.Now(),
			ServiceUID: string(export.Spec.ServiceReference.UID), Hostname: "origin.example.com", Trust: "System",
			Policy: &fleetnetv1alpha1.OriginTLSPolicyReference{Namespace: "store", Name: "api-tls", UID: "policy-" + export.Spec.ServiceReference.ClusterID,
				Generation: 1, ResourceVersion: "opaque", ExportToFleet: "true"},
		}
	}
	return f
}

func TestTLSBackendEligibilityAndAgreement(t *testing.T) {
	const conflictingHostname = "different.example.com"
	tests := []struct {
		name    string
		change  func(*backendFixture)
		origins int
		reason  string
	}{
		{"agree", nil, 2, ""},
		{"one missing", func(f *backendFixture) { f.exports[0].Spec.OriginTLS = nil }, 1, ""},
		{"one invalid", func(f *backendFixture) {
			f.exports[0].Spec.OriginTLS.Ready = false
			f.exports[0].Spec.OriginTLS.Reason = "DuplicatePolicies"
		}, 1, ""},
		{"invalid empty reason cannot authorize", func(f *backendFixture) {
			f.exports[0].Spec.OriginTLS.Ready = false
			f.exports[0].Spec.OriginTLS.Reason = ""
		}, 1, ""},
		{"expired", func(f *backendFixture) {
			f.exports[0].Spec.OriginTLS.ObservedAt = metav1.NewTime(time.Now().Add(-origintls.MaxAge - time.Second))
		}, 1, ""},
		{"future timestamp", func(f *backendFixture) {
			f.exports[0].Spec.OriginTLS.ObservedAt = metav1.NewTime(time.Now().Add(2 * time.Minute))
		}, 1, ""},
		{"source replay", func(f *backendFixture) { f.exports[0].Spec.OriginTLS.ServiceUID = "recreated" }, 1, ""},
		{"member policy namespace replay", func(f *backendFixture) { f.exports[0].Spec.OriginTLS.Policy.Namespace = "other" }, 1, ""},
		{"opt-out observation", func(f *backendFixture) { f.exports[0].Spec.OriginTLS.Policy.ExportToFleet = "false" }, 1, ""},
		{"private discovery unavailable", func(f *backendFixture) { f.exports[0].Spec.PrivateLinkOrigin.Ready = false }, 1, ""},
		{"valid identities conflict", func(f *backendFixture) { f.exports[0].Spec.OriginTLS.Hostname = conflictingHostname }, 0, "ConflictingTLSIdentity"},
		{"unready origin cannot hide valid identity conflict", func(f *backendFixture) {
			f.exports[0].Spec.OriginTLS.Hostname = conflictingHostname
			f.exports[0].Spec.PrivateLinkOrigin.Ready = false
		}, 0, "ConflictingTLSIdentity"},
		{"zero weight cannot hide valid identity conflict", func(f *backendFixture) {
			f.exports[0].Spec.OriginTLS.Hostname = conflictingHostname
			f.exports[0].Spec.Weight = ptr.To[int64](0)
		}, 0, "ConflictingTLSIdentity"},
		{"none eligible", func(f *backendFixture) {
			for i := range f.exports {
				f.exports[i].Spec.OriginTLS = nil
			}
		}, 0, "BackendUnavailable"},
		{"missing inventory", func(f *backendFixture) { f.exports = f.exports[:1] }, 0, "IncompleteInventory"},
		{"unaggregated source", func(f *backendFixture) { f.service.Status.Clusters = f.service.Status.Clusters[:1] }, 0, "IncompleteInventory"},
		{"foreign source namespace", func(f *backendFixture) { f.exports[0].Namespace = "not-a-member" }, 0, "InvalidExport"},
		{"explicit HTTPS protocol required", func(f *backendFixture) { f.service.Status.Ports[0].AppProtocol = nil }, 0, "UnsupportedProtocol"},
		{"port set mismatch", func(f *backendFixture) { f.exports[0].Spec.Ports[0].TargetPort.IntVal++ }, 0, "InvalidExport"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := tlsBackendFixture()
			if tt.change != nil {
				tt.change(&f)
			}
			got, err := Resolve("store", f.ref, f.service, f.exports, true)
			if tt.reason != "" {
				var resolution *ResolutionError
				if !errors.As(err, &resolution) || resolution.Reason != tt.reason || len(got.Origins) != 0 {
					t.Fatalf("backend=%+v error=%v want %s", got, err, tt.reason)
				}
				return
			}
			if err != nil || len(got.Origins) != tt.origins || got.TLSHostname != "origin.example.com" || got.TLSProvenance == "" || got.HealthProbe.Protocol != "HTTPS" {
				t.Fatalf("backend=%+v error=%v", got, err)
			}
			if tt.origins == 1 && len(got.ExcludedOrigins) != 1 {
				t.Fatalf("member exclusion hidden: %+v", got)
			}
		})
	}
	f := tlsBackendFixture()
	if _, err := Resolve("store", f.ref, f.service, f.exports); err == nil {
		t.Fatal("default HTTP resolution silently accepted TLS-only Service")
	}
}

func TestTLSProjectionCurrentOwnershipAndContents(t *testing.T) {
	f := tlsBackendFixture()
	backend, err := Resolve("store", f.ref, f.service, f.exports, true)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		change    func(*gatewayv1alpha3.BackendTLSPolicy)
		duplicate bool
	}{
		{"valid", nil, false},
		{"foreign owner", func(p *gatewayv1alpha3.BackendTLSPolicy) { p.OwnerReferences[0].UID = "recreated-target" }, false},
		{"stale provenance", func(p *gatewayv1alpha3.BackendTLSPolicy) { p.Annotations[origintls.ProvenanceAnnotation] = "old" }, false},
		{"stale hostname with Accepted true", func(p *gatewayv1alpha3.BackendTLSPolicy) { p.Spec.Validation.Hostname = "old.example.com" }, false},
		{"manual competing policy", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := TLSProjection(backend)
			if tt.change != nil {
				tt.change(policy)
			}
			scheme := runtime.NewScheme()
			if err := gatewayv1alpha3.Install(scheme); err != nil {
				t.Fatal(err)
			}
			objects := []client.Object{policy}
			if tt.duplicate {
				other := policy.DeepCopy()
				other.Name = "manual"
				other.Labels = nil
				other.OwnerReferences = nil
				objects = append(objects, other)
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
			err := ValidateTLSProjection(context.Background(), c, backend)
			if (err == nil) != (tt.name == "valid") {
				t.Fatalf("unexpected projection validation: %v", err)
			}
		})
	}
	if name := TLSProjectionName(strings.Repeat("a", 253)); len(name) > 253 {
		t.Fatalf("projection name too long: %d", len(name))
	}
	renewed := f.exports[0].Spec.OriginTLS.DeepCopy()
	renewed.ObservedAt = metav1.NewTime(time.Now().Add(time.Second))
	if err := origintls.RefreshObservation(&f.exports[0], renewed); err != nil {
		t.Fatal(err)
	}
	again, err := Resolve("store", f.ref, f.service, f.exports, true)
	if err != nil || !reflect.DeepEqual(backend, again) {
		t.Fatalf("heartbeat destabilized backend: %+v %v", again, err)
	}
}

func TestHTTPSListenerRequiresExplicitBothHopCapability(t *testing.T) {
	valid := gatewayv1.Listener{Name: "https", Protocol: gatewayv1.HTTPSProtocolType, Port: 443,
		TLS: &gatewayv1.GatewayTLSConfig{Mode: ptr.To(gatewayv1.TLSModeTerminate),
			Options: map[gatewayv1.AnnotationKey]gatewayv1.AnnotationValue{gatewayv1.AnnotationKey(origintls.DefaultCertificateOption): "true"}}}
	if err := ValidateListener(valid); err == nil {
		t.Fatal("TLS enabled by default")
	}
	for _, change := range []func(*gatewayv1.Listener){
		func(l *gatewayv1.Listener) { l.TLS = nil },
		func(l *gatewayv1.Listener) { l.Port = 80 },
		func(l *gatewayv1.Listener) { l.Hostname = ptr.To(gatewayv1.Hostname("custom.example.com")) },
		func(l *gatewayv1.Listener) {
			l.TLS.Options[gatewayv1.AnnotationKey(origintls.DefaultCertificateOption)] = "false"
		},
		func(l *gatewayv1.Listener) { l.TLS.Mode = ptr.To(gatewayv1.TLSModePassthrough) },
		func(l *gatewayv1.Listener) { l.TLS.CertificateRefs = []gatewayv1.SecretObjectReference{{Name: "cert"}} },
	} {
		listener := *valid.DeepCopy()
		change(&listener)
		if err := ValidateListener(listener, true); err == nil {
			t.Fatalf("unsupported listener accepted: %+v", listener)
		}
	}
	if err := ValidateListener(valid, true); err != nil {
		t.Fatal(err)
	}
}
