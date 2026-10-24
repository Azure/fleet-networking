/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gateway

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewayresolver"
)

const otherName = "other"

func TestReconcileGatewayControlPlaneOnly(t *testing.T) {
	tests := []struct {
		name     string
		change   func(*gatewayv1.Gateway, *gatewayv1.GatewayClass)
		accepted metav1.ConditionStatus
	}{
		{"valid", func(*gatewayv1.Gateway, *gatewayv1.GatewayClass) {}, metav1.ConditionTrue},
		{"foreign class", func(_ *gatewayv1.Gateway, c *gatewayv1.GatewayClass) {
			c.Spec.ControllerName = "other.example/controller"
		}, ""},
		{"Standard rejected", func(g *gatewayv1.Gateway, _ *gatewayv1.GatewayClass) {
			g.Annotations = map[string]string{"networking.fleet.azure.com/afd-sku": "Standard_AzureFrontDoor"}
		}, metav1.ConditionFalse},
		{"HTTPS unsupported", func(g *gatewayv1.Gateway, _ *gatewayv1.GatewayClass) {
			g.Spec.Listeners[0].Protocol = gatewayv1.HTTPSProtocolType
		}, metav1.ConditionFalse},
		{"TLS references unsupported", func(g *gatewayv1.Gateway, _ *gatewayv1.GatewayClass) {
			g.Spec.Listeners[0].Protocol = gatewayv1.HTTPSProtocolType
			g.Spec.Listeners[0].TLS = &gatewayv1.GatewayTLSConfig{CertificateRefs: []gatewayv1.SecretObjectReference{{Name: "certificate"}}}
		}, metav1.ConditionFalse},
		{"conflicting listeners", func(g *gatewayv1.Gateway, _ *gatewayv1.GatewayClass) {
			second := g.Spec.Listeners[0]
			second.Name = otherName
			g.Spec.Listeners = append(g.Spec.Listeners, second)
		}, metav1.ConditionFalse},
		{"stale class acceptance", func(_ *gatewayv1.Gateway, c *gatewayv1.GatewayClass) { c.Generation++ }, metav1.ConditionFalse},
		{"static address unsupported", func(g *gatewayv1.Gateway, _ *gatewayv1.GatewayClass) {
			g.Spec.Addresses = []gatewayv1.GatewayAddress{{Value: "10.0.0.1"}}
		}, metav1.ConditionFalse},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := gatewayv1.Install(scheme); err != nil {
				t.Fatal(err)
			}
			class := &gatewayv1.GatewayClass{ObjectMeta: metav1.ObjectMeta{Name: "afd", Generation: 1},
				Spec: gatewayv1.GatewayClassSpec{ControllerName: gatewayv1.GatewayController(objectmeta.GatewayAPIControllerName)},
				Status: gatewayv1.GatewayClassStatus{Conditions: []metav1.Condition{{
					Type: string(gatewayv1.GatewayClassConditionStatusAccepted), Status: metav1.ConditionTrue, ObservedGeneration: 1,
				}}}}
			gateway := &gatewayv1.Gateway{ObjectMeta: metav1.ObjectMeta{Namespace: "store", Name: "global", UID: "gateway", Generation: 4},
				Spec: gatewayv1.GatewaySpec{GatewayClassName: "afd", Listeners: []gatewayv1.Listener{{Name: "http", Protocol: gatewayv1.HTTPProtocolType, Port: 80}}}}
			tt.change(gateway, class)
			c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(gateway, class).WithObjects(gateway, class).
				WithIndex(&gatewayv1.HTTPRoute{}, gatewayresolver.HTTPRouteGatewayField, func(client.Object) []string { return nil }).Build()
			r := &Reconciler{Client: c}
			request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}
			if _, err := r.Reconcile(context.Background(), request); err != nil {
				t.Fatal(err)
			}
			got := &gatewayv1.Gateway{}
			if err := c.Get(context.Background(), request.NamespacedName, got); err != nil {
				t.Fatal(err)
			}
			accepted := meta.FindStatusCondition(got.Status.Conditions, string(gatewayv1.GatewayConditionAccepted))
			if tt.accepted == "" {
				if accepted != nil {
					t.Fatal("foreign Gateway status modified")
				}
				return
			}
			programmed := meta.FindStatusCondition(got.Status.Conditions, string(gatewayv1.GatewayConditionProgrammed))
			if accepted == nil || accepted.Status != tt.accepted || accepted.ObservedGeneration != 4 ||
				programmed == nil || programmed.Status != metav1.ConditionFalse || len(got.Status.Addresses) != 0 || len(got.Finalizers) != 0 {
				t.Fatalf("incorrect control-plane state: %#v", got)
			}
			if gateway.Spec.Listeners[0].TLS != nil {
				resolved := meta.FindStatusCondition(got.Status.Listeners[0].Conditions, "ResolvedRefs")
				if resolved == nil || resolved.Status != metav1.ConditionUnknown {
					t.Fatalf("unread TLS references claimed resolved: %#v", got.Status.Listeners)
				}
			}
			version := got.ResourceVersion
			if _, err := r.Reconcile(context.Background(), request); err != nil {
				t.Fatal(err)
			}
			if err := c.Get(context.Background(), request.NamespacedName, got); err != nil || got.ResourceVersion != version {
				t.Fatalf("stable reconciliation rewrote status: %v", err)
			}
		})
	}
}
