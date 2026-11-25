/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gateway

import (
	"context"
	"fmt"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha3 "sigs.k8s.io/gateway-api/apis/v1alpha3"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/origintls"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewayresolver"
)

func tlsControllerFixture(t *testing.T, members int) (client.Client, *gatewayv1.Gateway, *gatewayv1.HTTPRoute, *Reconciler, *fakeHTTPProvisioner) {
	t.Helper()
	ctx := context.Background()
	c, gateway, route, service := gatewayFixture(t, members)
	addObservationHub(t, c)
	gateway.Spec.Listeners = []gatewayv1.Listener{{Name: "https", Protocol: gatewayv1.HTTPSProtocolType, Port: 443,
		TLS: &gatewayv1.GatewayTLSConfig{Mode: ptr.To(gatewayv1.TLSModeTerminate), Options: map[gatewayv1.AnnotationKey]gatewayv1.AnnotationValue{
			gatewayv1.AnnotationKey(origintls.DefaultCertificateOption): "true",
		}}}}
	if err := c.Update(ctx, gateway); err != nil {
		t.Fatal(err)
	}
	route.Spec.ParentRefs = []gatewayv1.ParentReference{{Name: gatewayv1.ObjectName(gateway.Name)}}
	if err := c.Update(ctx, route); err != nil {
		t.Fatal(err)
	}
	service.UID = "service-import-uid"
	if err := c.Update(ctx, service); err != nil {
		t.Fatal(err)
	}
	service.Status.Ports[0].AppProtocol = ptr.To("https")
	if err := c.Status().Update(ctx, service); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < members; i++ {
		var export fleetnetv1alpha1.InternalServiceExport
		if err := c.Get(ctx, client.ObjectKey{Namespace: fmt.Sprintf("fleet-member-member-%d", i), Name: "store-api"}, &export); err != nil {
			t.Fatal(err)
		}
		export.Spec.Ports = service.Status.Ports
		export.Spec.OriginTLS = &fleetnetv1alpha1.OriginTLS{Ready: true, Reason: "Ready", ObservedAt: metav1.Now(),
			ServiceUID: string(export.Spec.ServiceReference.UID), Hostname: "origin.example.com", Trust: "System",
			Policy: &fleetnetv1alpha1.OriginTLSPolicyReference{Namespace: "store", Name: "api-tls", UID: fmt.Sprintf("policy-%d", i), Generation: 1, ResourceVersion: "observed", ExportToFleet: "true"},
		}
		if err := c.Update(ctx, &export); err != nil {
			t.Fatal(err)
		}
	}
	provider := &fakeHTTPProvisioner{}
	provider.progress.State, provider.progress.EndpointHostName = "Programmed", httpPreviewHostname
	provider.suspension.State = "Suspended"
	return c, gateway, route, &Reconciler{Client: c, APIReader: c, EnableOriginTLS: true,
		ProfileCleanup: &fakeProfileCleaner{}, HTTPProvisioner: provider}, provider
}

func TestTLSControllerProjectionSharedReferencesAndRevocation(t *testing.T) {
	ctx := context.Background()
	c, gateway, route, r, provider := tlsControllerFixture(t, 2)
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}
	for i := 0; i < 2; i++ {
		if _, err := r.Reconcile(ctx, request); err != nil {
			t.Fatal(err)
		}
	}
	assertHTTPGatewayReadiness(t, c, gateway, true)
	var policy gatewayv1alpha3.BackendTLSPolicy
	policyKey := client.ObjectKey{Namespace: "store", Name: gatewayresolver.TLSProjectionName("api")}
	if err := c.Get(ctx, policyKey, &policy); err != nil {
		t.Fatal(err)
	}
	if len(policy.OwnerReferences) != 1 || policy.OwnerReferences[0].Kind != "ServiceImport" || len(policy.Status.Ancestors) != 1 ||
		policy.Status.Ancestors[0].AncestorRef.Name != gatewayv1.ObjectName(gateway.Name) {
		t.Fatalf("hub policy ownership/status is not target/real Gateway based: %+v", policy)
	}
	shared := gateway.DeepCopy()
	shared.Name, shared.UID, shared.ResourceVersion, shared.Finalizers = "shared", "shared-uid", "", nil
	if err := c.Create(ctx, shared); err != nil {
		t.Fatal(err)
	}
	route.Spec.ParentRefs = append(route.Spec.ParentRefs, gatewayv1.ParentReference{Name: "shared"})
	if err := c.Update(ctx, route); err != nil {
		t.Fatal(err)
	}
	if err := r.reconcileTLSProjections(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, policyKey, &policy); err != nil {
		t.Fatal(err)
	}
	if len(policy.Status.Ancestors) != 2 {
		t.Fatalf("shared Gateway ancestry missing: %+v", policy.Status)
	}
	route.Spec.ParentRefs = route.Spec.ParentRefs[1:]
	if err := c.Update(ctx, route); err != nil {
		t.Fatal(err)
	}
	if err := r.reconcileTLSProjections(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, policyKey, &policy); err != nil || len(policy.Status.Ancestors) != 1 || policy.Status.Ancestors[0].AncestorRef.Name != "shared" {
		t.Fatalf("withdrawing first ref broke shared policy: %+v %v", policy.Status, err)
	}
	if err := c.Delete(ctx, route); err != nil {
		t.Fatal(err)
	}
	if err := r.reconcileTLSProjections(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, policyKey, &policy); !apierrors.IsNotFound(err) {
		t.Fatalf("last ref retained projection: %v", err)
	}
	if provider.calls == 0 {
		t.Fatal("secure graph was never provisioned")
	}
}

func TestTLSControllerLocalExclusionAndGlobalConflict(t *testing.T) {
	ctx := context.Background()
	c, gateway, _, r, provider := tlsControllerFixture(t, 2)
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}
	if _, err := r.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	var export fleetnetv1alpha1.InternalServiceExport
	key := client.ObjectKey{Namespace: "fleet-member-member-0", Name: "store-api"}
	if err := c.Get(ctx, key, &export); err != nil {
		t.Fatal(err)
	}
	export.Spec.OriginTLS.Ready, export.Spec.OriginTLS.Reason = false, "PolicyNotExported"
	if err := c.Update(ctx, &export); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	assertHTTPGatewayReadiness(t, c, gateway, true)
	backend := provider.model.Routes[0].Rules[0].Backends[0]
	if len(backend.Origins) != 1 || len(backend.ExcludedOrigins) != 1 {
		t.Fatalf("local failure not isolated: %+v", backend)
	}
	if !strings.Contains(meta.FindStatusCondition(gateway.Status.Conditions, "Programmed").Message, "reduced capacity") {
		t.Fatal("degradation hidden")
	}
	export.Spec.OriginTLS.Ready, export.Spec.OriginTLS.Reason, export.Spec.OriginTLS.Hostname = true, "Ready", "conflict.example.com"
	if err := c.Update(ctx, &export); err != nil {
		t.Fatal(err)
	}
	calls := provider.calls
	if _, err := r.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	assertHTTPGatewayReadiness(t, c, gateway, false)
	if provider.calls != calls || provider.withdrawals == 0 {
		t.Fatal("conflicting valid identity was programmed instead of withdrawing")
	}
}

func TestTLSControllerNeverAdoptsForeignProjection(t *testing.T) {
	ctx := context.Background()
	c, gateway, _, r, provider := tlsControllerFixture(t, 1)
	foreign := &gatewayv1alpha3.BackendTLSPolicy{ObjectMeta: metav1.ObjectMeta{Namespace: "store", Name: gatewayresolver.TLSProjectionName("api")}}
	if err := c.Create(ctx, foreign); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}); err == nil {
		t.Fatal("foreign policy collision hidden")
	}
	if provider.calls != 0 {
		t.Fatal("foreign projection authorized traffic")
	}
	var current gatewayv1alpha3.BackendTLSPolicy
	if err := c.Get(ctx, client.ObjectKeyFromObject(foreign), &current); err != nil {
		t.Fatal(err)
	}
	if len(current.OwnerReferences) != 0 || len(current.Spec.TargetRefs) != 0 {
		t.Fatal("foreign policy was mutated")
	}
}
