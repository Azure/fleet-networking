/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gatewayclass

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewayresolver"
)

func TestReconcileGatewayClass(t *testing.T) {
	tests := []struct {
		name       string
		controller gatewayv1.GatewayController
		parameters bool
		accepted   metav1.ConditionStatus
	}{
		{"recognized", gatewayv1.GatewayController(objectmeta.GatewayAPIControllerName), false, metav1.ConditionTrue},
		{"foreign", "other.example/controller", false, ""},
		{"unsupported parameters", gatewayv1.GatewayController(objectmeta.GatewayAPIControllerName), true, metav1.ConditionFalse},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := gatewayv1.Install(scheme); err != nil {
				t.Fatal(err)
			}
			class := &gatewayv1.GatewayClass{ObjectMeta: metav1.ObjectMeta{Name: "afd", Generation: 3},
				Spec: gatewayv1.GatewayClassSpec{ControllerName: tt.controller}}
			if tt.parameters {
				class.Spec.ParametersRef = &gatewayv1.ParametersReference{Group: "example.com", Kind: "Config", Name: "config"}
			}
			gateway := &gatewayv1.Gateway{ObjectMeta: metav1.ObjectMeta{Namespace: "store", Name: "global"},
				Spec: gatewayv1.GatewaySpec{GatewayClassName: "afd"}}
			c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(class).WithObjects(class, gateway).
				WithIndex(&gatewayv1.Gateway{}, gatewayresolver.GatewayClassField, func(o client.Object) []string {
					return []string{string(o.(*gatewayv1.Gateway).Spec.GatewayClassName)}
				}).Build()
			r := &Reconciler{Client: c}
			request := ctrl.Request{NamespacedName: types.NamespacedName{Name: class.Name}}
			if _, err := r.Reconcile(context.Background(), request); err != nil {
				t.Fatal(err)
			}
			got := &gatewayv1.GatewayClass{}
			if err := c.Get(context.Background(), request.NamespacedName, got); err != nil {
				t.Fatal(err)
			}
			condition := meta.FindStatusCondition(got.Status.Conditions, string(gatewayv1.GatewayClassConditionStatusAccepted))
			if tt.accepted == "" {
				if condition != nil || len(got.Finalizers) != 0 {
					t.Fatal("foreign class was modified")
				}
				return
			}
			if condition == nil || condition.Status != tt.accepted || condition.ObservedGeneration != 3 ||
				!controllerutil.ContainsFinalizer(got, gatewayv1.GatewayClassFinalizerGatewaysExist) || len(got.Status.SupportedFeatures) != 0 {
				t.Fatalf("incorrect class state: %#v", got)
			}
			version := got.ResourceVersion
			if _, err := r.Reconcile(context.Background(), request); err != nil {
				t.Fatal(err)
			}
			if err := c.Get(context.Background(), request.NamespacedName, got); err != nil || got.ResourceVersion != version {
				t.Fatalf("stable reconciliation wrote status: %v", err)
			}
			if err := c.Delete(context.Background(), gateway); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Reconcile(context.Background(), request); err != nil {
				t.Fatal(err)
			}
			if err := c.Get(context.Background(), request.NamespacedName, got); err != nil ||
				controllerutil.ContainsFinalizer(got, gatewayv1.GatewayClassFinalizerGatewaysExist) {
				t.Fatalf("unused class finalizer retained: %v", err)
			}
		})
	}
}
