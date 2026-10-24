/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gateway

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
	"go.goms.io/fleet-networking/pkg/providers/azure/frontdoor"
)

type fakeOriginObserver struct {
	calls  int
	result frontdoor.PrivateOriginsObservation
	err    error
	during func()
	owner  frontdoor.GatewayOwner
	model  gatewaymodel.GlobalGateway
}

func (f *fakeOriginObserver) Observe(_ context.Context, owner frontdoor.GatewayOwner,
	model gatewaymodel.GlobalGateway) (frontdoor.PrivateOriginsObservation, error) {
	f.calls++
	f.owner, f.model = owner, model
	if f.during != nil {
		f.during()
	}
	return f.result, f.err
}

func addObservationHub(t *testing.T, c client.Client) {
	t.Helper()
	if err := c.Create(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: metav1.NamespaceSystem, UID: "trusted-hub",
	}}); err != nil {
		t.Fatal(err)
	}
}

func TestGatewayObservesAsynchronousApproval(t *testing.T) {
	c, gateway, _, _ := gatewayFixture(t, 1)
	addObservationHub(t, c)
	observer := &fakeOriginObserver{result: frontdoor.PrivateOriginsObservation{
		PendingApprovals: []frontdoor.PrivateLinkPlan{{Target: frontdoor.PrivateLinkTarget{ResourceID: "expected-pls", Location: "eastus"}}},
	}}
	recorder := record.NewFakeRecorder(10)
	r := &Reconciler{Client: c, APIReader: c, OriginObserver: observer, Recorder: recorder}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}
	for _, ready := range []bool{false, true} {
		if ready {
			observer.result = frontdoor.PrivateOriginsObservation{OriginsConverged: true}
		}
		result, err := r.Reconcile(context.Background(), request)
		if err != nil || result.RequeueAfter < 30*time.Second || result.RequeueAfter > 36*time.Second {
			t.Fatalf("observation did not schedule a bounded recheck: %#v, %v", result, err)
		}
		if err := c.Get(context.Background(), request.NamespacedName, gateway); err != nil {
			t.Fatal(err)
		}
		pending := meta.FindStatusCondition(gateway.Status.Conditions, "Programmed")
		if pending == nil || pending.Status != metav1.ConditionFalse ||
			strings.Contains(pending.Message, "approval pending") == ready ||
			len(gateway.Finalizers) != 0 || len(gateway.Status.Addresses) != 0 {
			t.Fatalf("origin observation overstated readiness: %#v", gateway)
		}
		want := approvalEventPrefix
		if ready {
			want = "Normal AFDPrivateOriginsConverged"
		}
		assertObservationEvent(t, recorder, want)
	}
	if observer.calls != 2 || observer.owner.HubUID != "trusted-hub" || observer.owner.UID != string(gateway.UID) ||
		len(observer.model.Routes) != 1 {
		t.Fatalf("incorrect observation identity/intent: %#v", observer)
	}
}

func TestGatewayObservationFailureAndSkips(t *testing.T) {
	tests := []struct {
		name       string
		change     func(client.Client, *gatewayv1.Gateway)
		missingHub bool
		noReader   bool
		missing    bool
		fail       bool
		wantCalls  int
		wantError  bool
	}{
		{name: "missing profile", missing: true, wantCalls: 1},
		{name: "provider failure", fail: true, wantCalls: 1, wantError: true},
		{name: "missing hub identity", missingHub: true, wantError: true},
		{name: "missing API reader", noReader: true, wantError: true},
		{name: "deleting Gateway without owned finalizer", change: func(c client.Client, g *gatewayv1.Gateway) {
			g.Finalizers = []string{"foreign.example/cleanup"}
			if err := c.Update(context.Background(), g); err != nil {
				t.Fatal(err)
			}
			if err := c.Delete(context.Background(), g); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "unsupported intent", change: func(c client.Client, g *gatewayv1.Gateway) {
			g.Annotations = map[string]string{"networking.fleet.azure.com/afd-sku": "Standard_AzureFrontDoor"}
			if err := c.Update(context.Background(), g); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "empty Gateway", change: func(c client.Client, _ *gatewayv1.Gateway) {
			if err := c.Delete(context.Background(), &gatewayv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Namespace: "store", Name: "api"}}); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "unresolved backend", change: func(c client.Client, _ *gatewayv1.Gateway) {
			if err := c.Delete(context.Background(), &fleetnetv1alpha1.ServiceImport{ObjectMeta: metav1.ObjectMeta{Namespace: "store", Name: "api"}}); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "foreign class", change: func(c client.Client, _ *gatewayv1.Gateway) {
			var class gatewayv1.GatewayClass
			if err := c.Get(context.Background(), client.ObjectKey{Name: "afd"}, &class); err != nil {
				t.Fatal(err)
			}
			class.Spec.ControllerName = "foreign.example/controller"
			if err := c.Update(context.Background(), &class); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, gateway, _, _ := gatewayFixture(t, 1)
			if !tt.missingHub {
				addObservationHub(t, c)
			}
			if tt.change != nil {
				tt.change(c, gateway)
			}
			observer := &fakeOriginObserver{result: frontdoor.PrivateOriginsObservation{ProfileMissing: tt.missing}}
			if tt.fail {
				observer.err = errors.New("ARM unavailable")
			}
			r := &Reconciler{Client: c, APIReader: c, OriginObserver: observer}
			if tt.noReader {
				r.APIReader = nil
			}
			result, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)})
			if (err != nil) != tt.wantError || observer.calls != tt.wantCalls {
				t.Fatalf("calls=%d error=%v; want %d/error=%v", observer.calls, err, tt.wantCalls, tt.wantError)
			}
			if tt.missing || tt.fail {
				if err := c.Get(context.Background(), client.ObjectKeyFromObject(gateway), gateway); err != nil {
					t.Fatal(err)
				}
				message := meta.FindStatusCondition(gateway.Status.Conditions, "Programmed").Message
				want := "profile has not been created"
				if tt.fail {
					want = "observation failed"
				}
				if !strings.Contains(message, want) {
					t.Fatalf("missing truthful diagnostic: %q", message)
				}
			}
			if !tt.wantError && tt.wantCalls == 0 && result.RequeueAfter != 0 {
				t.Fatal("skipped intent unnecessarily polls Azure")
			}
		})
	}
}

func TestGatewayObservationRevalidatesIntent(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "route withdrawn during ARM read", true: "status conflict"}[conflict], func(t *testing.T) {
			c, gateway, route, _ := gatewayFixture(t, 1)
			addObservationHub(t, c)
			observer := &fakeOriginObserver{result: frontdoor.PrivateOriginsObservation{
				PendingApprovals: []frontdoor.PrivateLinkPlan{{Target: frontdoor.PrivateLinkTarget{ResourceID: "stale-pls"}}},
			}}
			if !conflict {
				observer.during = func() {
					route.Spec.ParentRefs = nil
					if err := c.Update(context.Background(), route); err != nil {
						t.Fatal(err)
					}
				}
			}
			patches := 0
			wrapped := interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
				SubResourcePatch: func(ctx context.Context, cl client.Client, subresource string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
					patches++
					if conflict && patches == 1 {
						return apierrors.NewConflict(schema.GroupResource{Group: gatewayv1.GroupName, Resource: "gateways"}, obj.GetName(), errors.New("concurrent status"))
					}
					return cl.SubResource(subresource).Patch(ctx, obj, patch, opts...)
				},
			})
			r := &Reconciler{Client: wrapped, APIReader: c, OriginObserver: observer}
			if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}); err != nil {
				t.Fatal(err)
			}
			if observer.calls != 1 {
				t.Fatalf("status retry repeated ARM observation %d times", observer.calls)
			}
			if err := c.Get(context.Background(), client.ObjectKeyFromObject(gateway), gateway); err != nil {
				t.Fatal(err)
			}
			pending := meta.FindStatusCondition(gateway.Status.Conditions, "Programmed")
			if strings.Contains(pending.Message, "stale-pls") != conflict || pending.Status != metav1.ConditionFalse {
				t.Fatalf("stale intent diagnostic: %#v", pending)
			}
			if conflict && patches != 2 {
				t.Fatalf("did not exercise status retry: %d", patches)
			}
			if len(gateway.Finalizers) != 0 || len(gateway.Status.Addresses) != 0 {
				t.Fatal("observation changed ownership")
			}
		})
	}
}

func TestGatewayObservationDiscardsChangedBackends(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		t.Run(map[bool]string{false: "member configuration changed", true: "member became unavailable"}[unavailable], func(t *testing.T) {
			c, gateway, _, _ := gatewayFixture(t, 1)
			addObservationHub(t, c)
			observer := &fakeOriginObserver{
				result: frontdoor.PrivateOriginsObservation{OriginsConverged: true},
				during: func() {
					var export fleetnetv1alpha1.InternalServiceExport
					key := client.ObjectKey{Namespace: "fleet-member-member-0", Name: "store-api"}
					if err := c.Get(context.Background(), key, &export); err != nil {
						t.Fatal(err)
					}
					if unavailable {
						export.Spec.PrivateLinkOrigin.Ready = false
					} else {
						export.Spec.PrivateLinkOrigin.InternalIP = "10.0.0.99"
					}
					if err := c.Update(context.Background(), &export); err != nil {
						t.Fatal(err)
					}
				},
			}
			r := &Reconciler{Client: c, APIReader: c, OriginObserver: observer}
			if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}); err != nil {
				t.Fatal(err)
			}
			if err := c.Get(context.Background(), client.ObjectKeyFromObject(gateway), gateway); err != nil {
				t.Fatal(err)
			}
			pending := meta.FindStatusCondition(gateway.Status.Conditions, "Programmed")
			if pending.Status != metav1.ConditionFalse || strings.Contains(pending.Message, "approved and deployed") ||
				gateway.Generation != 1 || observer.calls != 1 {
				t.Fatalf("dependency change reused a stale observation: %#v", gateway)
			}
		})
	}
}

func TestGatewayObservationUsesUncachedHubIdentity(t *testing.T) {
	c, gateway, _, _ := gatewayFixture(t, 1)
	// The cached client deliberately has no kube-system Namespace.
	hub := fake.NewClientBuilder().WithScheme(c.Scheme()).WithObjects(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: metav1.NamespaceSystem, UID: "uncached-hub"}},
	).Build()
	observer := &fakeOriginObserver{result: frontdoor.PrivateOriginsObservation{ProfileMissing: true}}
	r := &Reconciler{Client: c, APIReader: cleanupAPIReader{Reader: c, hub: hub}, OriginObserver: observer}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}); err != nil {
		t.Fatal(err)
	}
	if observer.owner.HubUID != "uncached-hub" {
		t.Fatalf("observation did not use the trusted uncached identity: %#v", observer.owner)
	}
}

func TestOriginObservationIdentityGuards(t *testing.T) {
	for _, changed := range []string{"uid", "generation"} {
		t.Run(changed, func(t *testing.T) {
			gateway := &gatewayv1.Gateway{ObjectMeta: metav1.ObjectMeta{UID: "gateway", Generation: 1},
				Status: gatewayv1.GatewayStatus{Conditions: []metav1.Condition{{
					Type: "Programmed", Status: metav1.ConditionFalse, Message: "current intent",
				}}}}
			model := gatewaymodel.GlobalGateway{UID: "gateway"}
			observation := &gatewayObservation{uid: gateway.UID, generation: gateway.Generation, model: model,
				result: frontdoor.PrivateOriginsObservation{OriginsConverged: true}}
			if changed == "uid" {
				gateway.UID = "replacement"
			} else {
				gateway.Generation++
			}
			applyOriginObservation(gateway, &model, observation)
			if gateway.Status.Conditions[0].Message != "current intent" {
				t.Fatal("observation was applied to replaced or changed Gateway")
			}
		})
	}
}
