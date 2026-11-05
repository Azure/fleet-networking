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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/providers/azure/frontdoor"
)

func ownedEmptyGateway(t *testing.T) (client.Client, *gatewayv1.Gateway, *gatewayv1.HTTPRoute) {
	t.Helper()
	c, gateway, route, _ := gatewayFixture(t, 1)
	addObservationHub(t, c)
	gateway.Finalizers = []string{objectmeta.GatewayAFDCleanupFinalizer, ownershipForeignFinalizer}
	if err := c.Update(context.Background(), gateway); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(context.Background(), route); err != nil {
		t.Fatal(err)
	}
	return c, gateway, route
}

func TestGatewaySuspendsConfirmedEmptyIntent(t *testing.T) {
	for _, state := range []frontdoor.EndpointSuspensionState{frontdoor.EndpointSuspensionPending, frontdoor.EndpointSuspended, frontdoor.EndpointAbsent} {
		t.Run(string(state), func(t *testing.T) {
			c, gateway, _ := ownedEmptyGateway(t)
			gateway.Status.Addresses = []gatewayv1.GatewayStatusAddress{{Value: "previous.azurefd.net"}}
			gateway.Status.Conditions = []metav1.Condition{{Type: "Programmed", Status: metav1.ConditionTrue, ObservedGeneration: gateway.Generation}}
			if err := c.Status().Update(context.Background(), gateway); err != nil {
				t.Fatal(err)
			}
			patches := 0
			writer := interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
				Patch: func(context.Context, client.WithWatch, client.Object, client.Patch, ...client.PatchOption) error {
					t.Fatal("empty intent must not patch cleanup ownership")
					return nil
				},
				SubResourcePatch: func(ctx context.Context, cl client.Client, resource string, object client.Object,
					patch client.Patch, opts ...client.SubResourcePatchOption) error {
					patches++
					if patches == 1 {
						return apierrors.NewConflict(schema.GroupResource{Resource: ownershipGatewayResource}, object.GetName(),
							errors.New("suspension status conflict"))
					}
					return cl.SubResource(resource).Patch(ctx, object, patch, opts...)
				},
			})
			stager := &fakeGraphStager{suspension: frontdoor.EndpointSuspensionResult{State: state}}
			r := &Reconciler{Client: writer, APIReader: ownershipReader{Reader: c, t: t}, ProfileCleanup: &fakeProfileCleaner{}, GraphStager: stager}
			result, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)})
			if err != nil || result.RequeueAfter < 30*time.Second || result.RequeueAfter > 36*time.Second {
				t.Fatalf("empty withdrawal did not requeue: %#v, %v", result, err)
			}
			if err := c.Get(context.Background(), client.ObjectKeyFromObject(gateway), gateway); err != nil {
				t.Fatal(err)
			}
			pending := meta.FindStatusCondition(gateway.Status.Conditions, "Programmed")
			if stager.suspensions != 1 || stager.calls != 0 || patches != 2 ||
				stager.owner.HubUID != ownershipHubUID || stager.owner.UID != string(gateway.UID) ||
				pending == nil || pending.Status != metav1.ConditionFalse || len(gateway.Status.Addresses) != 0 ||
				!strings.Contains(pending.Message, string(state)) || len(gateway.Finalizers) != 2 ||
				!controllerutil.ContainsFinalizer(gateway, objectmeta.GatewayAFDCleanupFinalizer) {
				t.Fatalf("suspension replayed, lost ownership or claimed ingress: %#v, provider=%#v, patches=%d", gateway, stager, patches)
			}
		})
	}
}

func TestGatewaySuspensionRejectsUnconfirmedWithdrawal(t *testing.T) {
	tests := []struct {
		name      string
		wantError bool
	}{
		{name: "fresh API still has a route", wantError: true},
		{name: "owned finalizer is missing", wantError: true},
		{name: "hub changes during validation", wantError: true},
		{name: "uncached reads fail", wantError: true},
		{name: "no suspension reader", wantError: true},
		{name: "no suspension cleanup provider", wantError: true},
		{name: "never owned empty Gateway"},
		{name: "nil suspension injection"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			c, gateway, route := ownedEmptyGateway(t)
			stager := &fakeGraphStager{}
			r := &Reconciler{Client: c, APIReader: c, ProfileCleanup: &fakeProfileCleaner{}, GraphStager: stager}
			switch tt.name {
			case "fresh API still has a route":
				route.ResourceVersion = ""
				if err := c.Create(ctx, route); err != nil {
					t.Fatal(err)
				}
				r.Client = interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
					List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
						if routes, ok := list.(*gatewayv1.HTTPRouteList); ok {
							routes.Items = nil
							return nil
						}
						return cl.List(ctx, list, opts...)
					},
				})
			case "owned finalizer is missing":
				r.APIReader = ownershipReader{Reader: c, t: t, afterGet: func(object client.Object) {
					if current, ok := object.(*gatewayv1.Gateway); ok {
						current.Finalizers = nil
					}
				}}
			case "hub changes during validation":
				reads := 0
				r.APIReader = ownershipReader{Reader: c, t: t, afterGet: func(object client.Object) {
					if namespace, ok := object.(*corev1.Namespace); ok && namespace.Name == metav1.NamespaceSystem {
						reads++
						if reads > 1 {
							namespace.UID = "replacement-withdrawal-hub"
						}
					}
				}}
			case "uncached reads fail":
				r.APIReader = interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
					Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
						return errors.New("withdrawal API unavailable")
					},
				})
			case "no suspension reader":
				r.APIReader = nil
			case "no suspension cleanup provider":
				r.ProfileCleanup = nil
			case "never owned empty Gateway":
				gateway.Finalizers = []string{ownershipForeignFinalizer}
				if err := c.Update(ctx, gateway); err != nil {
					t.Fatal(err)
				}
			case "nil suspension injection":
				r.GraphStager = nil
			}
			before := graphMetricSamples(t, "", "")
			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)})
			assertGraphMetricDelta(t, before, graphMetricSamples(t, "", ""), 0)
			if (err != nil) != tt.wantError || stager.calls != 0 || stager.suspensions != 0 {
				t.Fatalf("unconfirmed withdrawal reached SDK: %v, provider=%#v; want error=%v", err, stager, tt.wantError)
			}
		})
	}
}

func TestGatewaySuspensionFailureAndReattachment(t *testing.T) {
	for _, reattach := range []bool{false, true} {
		c, gateway, route := ownedEmptyGateway(t)
		failure := errors.New("endpoint suspension unavailable")
		stager := &fakeGraphStager{err: failure}
		if reattach {
			stager.err = nil
			stager.suspension.State = frontdoor.EndpointSuspended
			stager.during = func() {
				route.ResourceVersion = ""
				if err := c.Create(context.Background(), route); err != nil {
					t.Fatal(err)
				}
			}
		}
		r := &Reconciler{Client: c, APIReader: c, ProfileCleanup: &fakeProfileCleaner{}, GraphStager: stager}
		_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)})
		if (!reattach && !errors.Is(err, failure)) || (reattach && err != nil) || stager.suspensions != 1 || stager.calls != 0 {
			t.Fatalf("suspension error/reattachment mishandled: %v, provider=%#v", err, stager)
		}
		if err := c.Get(context.Background(), client.ObjectKeyFromObject(gateway), gateway); err != nil {
			t.Fatal(err)
		}
		pending := meta.FindStatusCondition(gateway.Status.Conditions, "Programmed")
		if pending == nil || pending.Status != metav1.ConditionFalse || len(gateway.Status.Addresses) != 0 ||
			!controllerutil.ContainsFinalizer(gateway, objectmeta.GatewayAFDCleanupFinalizer) {
			t.Fatalf("suspension lost cleanup or claimed readiness: %#v", gateway)
		}
		if reattach {
			if strings.Contains(pending.Message, "endpoint suspension") {
				t.Fatalf("stale suspension status applied to reattached route: %q", pending.Message)
			}
		} else if !strings.Contains(pending.Message, failure.Error()) {
			t.Fatalf("suspension failure was hidden: %q", pending.Message)
		}
	}
}

func TestGatewaySuspensionSkipsTransientResolutionFailure(t *testing.T) {
	c, gateway, route := ownedEmptyGateway(t)
	route.ResourceVersion = ""
	if err := c.Create(context.Background(), route); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("route listing unavailable")
	reader := interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
		List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
			return failure
		},
	})
	stager := &fakeGraphStager{}
	r := &Reconciler{Client: reader, APIReader: c, ProfileCleanup: &fakeProfileCleaner{}, GraphStager: stager}
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)})
	if !errors.Is(err, failure) || stager.suspensions != 0 || stager.calls != 0 {
		t.Fatalf("transient lookup failure was treated as withdrawal: %v, provider=%#v", err, stager)
	}
}

func TestGatewaySuspensionRejectsChangedGateway(t *testing.T) {
	const replacementCase = "replacement"
	for _, change := range []string{replacementCase, "new generation", "deleting"} {
		t.Run(change, func(t *testing.T) {
			c, gateway, _ := ownedEmptyGateway(t)
			stager := &fakeGraphStager{}
			reader := ownershipReader{Reader: c, t: t, afterGet: func(object client.Object) {
				current, ok := object.(*gatewayv1.Gateway)
				if !ok {
					return
				}
				switch change {
				case replacementCase:
					current.UID = "replacement-withdrawal-gateway"
				case "new generation":
					current.Generation++
				case "deleting":
					now := metav1.Now()
					current.DeletionTimestamp = &now
				}
			}}
			r := &Reconciler{Client: c, APIReader: reader, ProfileCleanup: &fakeProfileCleaner{}, GraphStager: stager}
			_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)})
			if !errors.Is(err, errProvisioningIntentChanged) || stager.suspensions != 0 || stager.calls != 0 {
				t.Fatalf("changed Gateway was suspended: %v, provider=%#v", err, stager)
			}
		})
	}
}
