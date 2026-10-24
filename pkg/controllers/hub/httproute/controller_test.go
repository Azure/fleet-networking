/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package httproute

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewayresolver"
)

const missingName = "missing"

func routeObjects() (*gatewayv1.GatewayClass, *gatewayv1.Gateway, *gatewayv1.HTTPRoute, *fleetnetv1alpha1.ServiceImport) {
	class := &gatewayv1.GatewayClass{ObjectMeta: metav1.ObjectMeta{Name: "afd", Generation: 1},
		Spec:   gatewayv1.GatewayClassSpec{ControllerName: gatewayv1.GatewayController(objectmeta.GatewayAPIControllerName)},
		Status: gatewayv1.GatewayClassStatus{Conditions: []metav1.Condition{{Type: "Accepted", Status: metav1.ConditionTrue, ObservedGeneration: 1}}}}
	gateway := &gatewayv1.Gateway{ObjectMeta: metav1.ObjectMeta{Namespace: "store", Name: "global", Generation: 1},
		Spec:   gatewayv1.GatewaySpec{GatewayClassName: "afd", Listeners: []gatewayv1.Listener{{Name: "http", Protocol: gatewayv1.HTTPProtocolType, Port: 80}}},
		Status: gatewayv1.GatewayStatus{Conditions: []metav1.Condition{{Type: "Accepted", Status: metav1.ConditionTrue, ObservedGeneration: 1}}}}
	route := &gatewayv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Namespace: "store", Name: "api", Generation: 2},
		Spec: gatewayv1.HTTPRouteSpec{CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gatewayv1.ParentReference{{Name: "global"}}},
			Rules: []gatewayv1.HTTPRouteRule{{BackendRefs: []gatewayv1.HTTPBackendRef{{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{
				Group: ptr.To(gatewayv1.Group(fleetnetv1alpha1.GroupVersion.Group)), Kind: ptr.To(gatewayv1.Kind("ServiceImport")), Name: "api", Port: ptr.To(gatewayv1.PortNumber(80)),
			}}}}}}}}
	service := &fleetnetv1alpha1.ServiceImport{ObjectMeta: metav1.ObjectMeta{Namespace: "store", Name: "api"},
		Status: fleetnetv1alpha1.ServiceImportStatus{Type: fleetnetv1alpha1.ClusterSetIP, Ports: []fleetnetv1alpha1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}}}}
	return class, gateway, route, service
}

func routeClient(t *testing.T, objects ...client.Object) client.WithWatch {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, install := range []func(*runtime.Scheme) error{corev1.AddToScheme, gatewayv1.Install, gatewayv1beta1.Install, fleetnetv1alpha1.AddToScheme} {
		if err := install(scheme); err != nil {
			t.Fatal(err)
		}
	}
	return fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&gatewayv1.HTTPRoute{}, &gatewayv1.Gateway{}, &gatewayv1.GatewayClass{}, &fleetnetv1alpha1.ServiceImport{}).
		WithObjects(append(objects, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "store"}})...).
		WithIndex(&fleetnetv1alpha1.InternalServiceExport{}, gatewayresolver.ExportServiceField, func(o client.Object) []string {
			return []string{o.(*fleetnetv1alpha1.InternalServiceExport).Spec.ServiceReference.NamespacedName}
		}).Build()
}

func TestReconcileRouteConditionsAndStatusOwnership(t *testing.T) {
	tests := []struct {
		name     string
		change   func(*gatewayv1.GatewayClass, *gatewayv1.Gateway, *gatewayv1.HTTPRoute)
		accepted metav1.ConditionStatus
		resolved metav1.ConditionStatus
		reason   string
		managed  bool
	}{
		{"private discovery pending does not invalidate refs", func(*gatewayv1.GatewayClass, *gatewayv1.Gateway, *gatewayv1.HTTPRoute) {}, metav1.ConditionTrue, metav1.ConditionTrue, "ResolvedRefs", true},
		{"missing backend", func(_ *gatewayv1.GatewayClass, _ *gatewayv1.Gateway, r *gatewayv1.HTTPRoute) {
			r.Spec.Rules[0].BackendRefs[0].Name = missingName
		}, metav1.ConditionTrue, metav1.ConditionFalse, "BackendNotFound", true},
		{"wrong backend kind", func(_ *gatewayv1.GatewayClass, _ *gatewayv1.Gateway, r *gatewayv1.HTTPRoute) {
			r.Spec.Rules[0].BackendRefs[0].Kind = ptr.To(gatewayv1.Kind("Service"))
		}, metav1.ConditionTrue, metav1.ConditionFalse, "InvalidKind", true},
		{"cross namespace requires grant", func(_ *gatewayv1.GatewayClass, _ *gatewayv1.Gateway, r *gatewayv1.HTTPRoute) {
			r.Spec.Rules[0].BackendRefs[0].Namespace = ptr.To(gatewayv1.Namespace("backends"))
		}, metav1.ConditionTrue, metav1.ConditionFalse, "RefNotPermitted", true},
		{"namespace policy denies route", func(_ *gatewayv1.GatewayClass, g *gatewayv1.Gateway, _ *gatewayv1.HTTPRoute) {
			g.Spec.Listeners[0].AllowedRoutes = &gatewayv1.AllowedRoutes{Namespaces: &gatewayv1.RouteNamespaces{
				From: ptr.To(gatewayv1.NamespacesFromSelector), Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"tenant": "allowed"}},
			}}
		}, metav1.ConditionFalse, metav1.ConditionTrue, "ResolvedRefs", true},
		{"stale Gateway acceptance", func(_ *gatewayv1.GatewayClass, g *gatewayv1.Gateway, _ *gatewayv1.HTTPRoute) { g.Generation++ }, metav1.ConditionFalse, metav1.ConditionTrue, "ResolvedRefs", true},
		{"unsupported filters", func(_ *gatewayv1.GatewayClass, _ *gatewayv1.Gateway, r *gatewayv1.HTTPRoute) {
			r.Spec.Rules[0].Filters = []gatewayv1.HTTPRouteFilter{{Type: gatewayv1.HTTPRouteFilterRequestRedirect}}
		}, metav1.ConditionFalse, metav1.ConditionTrue, "ResolvedRefs", true},
		{"unsupported filter reference", func(_ *gatewayv1.GatewayClass, _ *gatewayv1.Gateway, r *gatewayv1.HTTPRoute) {
			r.Spec.Rules[0].Filters = []gatewayv1.HTTPRouteFilter{{Type: gatewayv1.HTTPRouteFilterExtensionRef,
				ExtensionRef: &gatewayv1.LocalObjectReference{Group: "example.com", Kind: "Filter", Name: "external"}}}
		}, metav1.ConditionFalse, metav1.ConditionFalse, "InvalidKind", true},
		{"foreign class", func(c *gatewayv1.GatewayClass, _ *gatewayv1.Gateway, _ *gatewayv1.HTTPRoute) {
			c.Spec.ControllerName = "other.example/controller"
		}, "", "", "", false},
		{"missing Gateway", func(_ *gatewayv1.GatewayClass, _ *gatewayv1.Gateway, r *gatewayv1.HTTPRoute) {
			r.Spec.ParentRefs[0].Name = missingName
		}, "", "", "", false},
		{"parent removed", func(_ *gatewayv1.GatewayClass, _ *gatewayv1.Gateway, r *gatewayv1.HTTPRoute) { r.Spec.ParentRefs = nil }, "", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			class, gateway, route, service := routeObjects()
			foreign := gatewayv1.RouteParentStatus{ParentRef: gatewayv1.ParentReference{Name: "other"}, ControllerName: "other.example/controller",
				Conditions: []metav1.Condition{{Type: "Accepted", Status: metav1.ConditionTrue, Reason: "Accepted", Message: "foreign", LastTransitionTime: metav1.Now()}}}
			route.Status.Parents = []gatewayv1.RouteParentStatus{foreign, {ParentRef: route.Spec.ParentRefs[0], ControllerName: gatewayv1.GatewayController(objectmeta.GatewayAPIControllerName)}}
			tt.change(class, gateway, route)
			c := routeClient(t, class, gateway, route, service)
			r := &Reconciler{Client: c}
			request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(route)}
			if _, err := r.Reconcile(context.Background(), request); err != nil {
				t.Fatal(err)
			}
			got := &gatewayv1.HTTPRoute{}
			if err := c.Get(context.Background(), request.NamespacedName, got); err != nil {
				t.Fatal(err)
			}
			if len(got.Status.Parents) == 0 || got.Status.Parents[0].ControllerName != foreign.ControllerName ||
				got.Status.Parents[0].Conditions[0].Message != "foreign" {
				t.Fatalf("foreign status lost: %#v", got.Status)
			}
			if !tt.managed {
				if len(got.Status.Parents) != 1 {
					t.Fatalf("stale owned status retained: %#v", got.Status)
				}
				return
			}
			if len(got.Status.Parents) != 2 {
				t.Fatalf("expected owned parent status: %#v", got.Status)
			}
			conditions := got.Status.Parents[1].Conditions
			accepted, resolved := meta.FindStatusCondition(conditions, "Accepted"), meta.FindStatusCondition(conditions, "ResolvedRefs")
			if accepted == nil || accepted.Status != tt.accepted || accepted.ObservedGeneration != route.Generation ||
				resolved == nil || resolved.Status != tt.resolved || resolved.Reason != tt.reason || resolved.ObservedGeneration != route.Generation {
				t.Fatalf("incorrect parent conditions: %#v", conditions)
			}
			version := got.ResourceVersion
			if _, err := r.Reconcile(context.Background(), request); err != nil {
				t.Fatal(err)
			}
			if err := c.Get(context.Background(), request.NamespacedName, got); err != nil || got.ResourceVersion != version {
				t.Fatalf("stable reconciliation wrote status: %v", err)
			}
		})
	}
}

func TestReconcileRouteRetriesWithFreshIntentAndPreservesConcurrentStatus(t *testing.T) {
	class, gateway, route, service := routeObjects()
	base := routeClient(t, class, gateway, route, service)
	patches, conflicts := 0, 0
	c := interceptor.NewClient(base, interceptor.Funcs{SubResourcePatch: func(ctx context.Context, c client.Client,
		subresource string, object client.Object, patch client.Patch, options ...client.SubResourcePatchOption) error {
		patches++
		if patches == 1 {
			current := &gatewayv1.HTTPRoute{}
			if err := c.Get(ctx, client.ObjectKeyFromObject(route), current); err != nil {
				return err
			}
			current.Spec.Rules[0].BackendRefs[0].Name = missingName
			current.Generation++
			if err := c.Update(ctx, current); err != nil {
				return err
			}
			current.Status.Parents = []gatewayv1.RouteParentStatus{{ControllerName: "other.example/controller", ParentRef: gatewayv1.ParentReference{Name: "other"}}}
			if err := c.Status().Update(ctx, current); err != nil {
				return err
			}
		}
		err := c.SubResource(subresource).Patch(ctx, object, patch, options...)
		if apierrors.IsConflict(err) {
			conflicts++
		}
		return err
	}})
	r := &Reconciler{Client: c}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(route)}); err != nil {
		t.Fatal(err)
	}
	got := &gatewayv1.HTTPRoute{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(route), got); err != nil {
		t.Fatal(err)
	}
	if conflicts != 1 || patches != 2 || len(got.Status.Parents) != 2 || got.Status.Parents[0].ControllerName != "other.example/controller" {
		t.Fatalf("retry lost concurrent state: patches=%d conflicts=%d status=%#v", patches, conflicts, got.Status)
	}
	resolved := meta.FindStatusCondition(got.Status.Parents[1].Conditions, "ResolvedRefs")
	if resolved == nil || resolved.Status != metav1.ConditionFalse || resolved.Reason != "BackendNotFound" || resolved.ObservedGeneration != got.Generation {
		t.Fatalf("retry reused stale intent: %#v", resolved)
	}
}

func TestReconcileRouteReturnsAPIErrorWithoutChangingStatus(t *testing.T) {
	class, gateway, route, service := routeObjects()
	failure := errors.New("Namespace lookup failed")
	c := interceptor.NewClient(routeClient(t, class, gateway, route, service), interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
			if _, ok := object.(*corev1.Namespace); ok {
				return failure
			}
			return c.Get(ctx, key, object, options...)
		},
	})
	r := &Reconciler{Client: c}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(route)}); !errors.Is(err, failure) {
		t.Fatalf("API failure was hidden: %v", err)
	}
	got := &gatewayv1.HTTPRoute{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(route), got); err != nil || len(got.Status.Parents) != 0 {
		t.Fatalf("API failure produced success-shaped status: %#v, %v", got.Status, err)
	}
}

func TestReconcileRouteRevokesDeletedGrant(t *testing.T) {
	class, gateway, route, service := routeObjects()
	service.Namespace = "backends"
	route.Spec.Rules[0].BackendRefs[0].Namespace = ptr.To(gatewayv1.Namespace(service.Namespace))
	grant := &gatewayv1beta1.ReferenceGrant{ObjectMeta: metav1.ObjectMeta{Namespace: service.Namespace, Name: "allow"},
		Spec: gatewayv1beta1.ReferenceGrantSpec{
			From: []gatewayv1beta1.ReferenceGrantFrom{{Group: gatewayv1.GroupName, Kind: "HTTPRoute", Namespace: "store"}},
			To:   []gatewayv1beta1.ReferenceGrantTo{{Group: gatewayv1.Group(fleetnetv1alpha1.GroupVersion.Group), Kind: "ServiceImport"}},
		}}
	c := routeClient(t, class, gateway, route, service, grant)
	r := &Reconciler{Client: c}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(route)}
	for _, deleted := range []bool{false, true} {
		if deleted {
			if err := c.Delete(context.Background(), grant); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := r.Reconcile(context.Background(), request); err != nil {
			t.Fatal(err)
		}
		got := &gatewayv1.HTTPRoute{}
		if err := c.Get(context.Background(), request.NamespacedName, got); err != nil {
			t.Fatal(err)
		}
		resolved := meta.FindStatusCondition(got.Status.Parents[0].Conditions, "ResolvedRefs")
		if resolved == nil || (resolved.Status == metav1.ConditionTrue) == deleted || (deleted && resolved.Reason != "RefNotPermitted") {
			t.Fatalf("grant deletion=%v did not update authorization: %#v", deleted, resolved)
		}
	}
}
