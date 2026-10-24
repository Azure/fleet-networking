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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	fleetnetv1beta1 "go.goms.io/fleet-networking/api/v1beta1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewayresolver"
)

func gatewayFixture(t *testing.T, members int) (client.Client, *gatewayv1.Gateway, *gatewayv1.HTTPRoute, *fleetnetv1alpha1.ServiceImport) {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, install := range []func(*runtime.Scheme) error{corev1.AddToScheme, gatewayv1.Install, gatewayv1beta1.Install, fleetnetv1alpha1.AddToScheme} {
		if err := install(scheme); err != nil {
			t.Fatal(err)
		}
	}
	class := &gatewayv1.GatewayClass{ObjectMeta: metav1.ObjectMeta{Name: "afd", Generation: 1},
		Spec:   gatewayv1.GatewayClassSpec{ControllerName: gatewayv1.GatewayController(objectmeta.GatewayAPIControllerName)},
		Status: gatewayv1.GatewayClassStatus{Conditions: []metav1.Condition{{Type: "Accepted", Status: metav1.ConditionTrue, ObservedGeneration: 1}}}}
	gateway := &gatewayv1.Gateway{ObjectMeta: metav1.ObjectMeta{Namespace: "store", Name: "global", UID: "gateway", Generation: 1},
		Spec: gatewayv1.GatewaySpec{GatewayClassName: "afd", Listeners: []gatewayv1.Listener{{Name: "http", Protocol: gatewayv1.HTTPProtocolType, Port: 80}}}}
	route := &gatewayv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Namespace: "store", Name: "api", UID: "route"},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gatewayv1.ParentReference{{Name: "global"}, {Name: "global", SectionName: ptr.To(gatewayv1.SectionName("http"))}}},
			Rules: []gatewayv1.HTTPRouteRule{{BackendRefs: []gatewayv1.HTTPBackendRef{{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{
				Group: ptr.To(gatewayv1.Group(fleetnetv1alpha1.GroupVersion.Group)), Kind: ptr.To(gatewayv1.Kind("ServiceImport")), Name: "api", Port: ptr.To(gatewayv1.PortNumber(80)),
			}}}}}},
		}}
	service := &fleetnetv1alpha1.ServiceImport{ObjectMeta: metav1.ObjectMeta{Namespace: "store", Name: "api"},
		Status: fleetnetv1alpha1.ServiceImportStatus{Ports: []fleetnetv1alpha1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}}}}
	objects := []client.Object{class, gateway, route, service, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "store"}}}
	for i := 0; i < members; i++ {
		cluster := fmt.Sprintf("member-%d", i)
		service.Status.Clusters = append(service.Status.Clusters, fleetnetv1alpha1.ClusterStatus{Cluster: cluster})
		objects = append(objects, &fleetnetv1alpha1.InternalServiceExport{
			ObjectMeta: metav1.ObjectMeta{Namespace: "fleet-member-" + cluster, Name: "store-api", Generation: 1},
			Spec: fleetnetv1alpha1.InternalServiceExportSpec{Ports: service.Status.Ports, Type: corev1.ServiceTypeLoadBalancer, IsInternalLoadBalancer: true,
				ServiceReference: fleetnetv1alpha1.ExportedObjectReference{Namespace: "store", Name: "api", NamespacedName: "store/api", ClusterID: cluster, UID: types.UID(cluster)},
				PrivateLinkOrigin: &fleetnetv1alpha1.PrivateLinkOrigin{
					ResourceID: "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg/providers/Microsoft.Network/privateLinkServices/" + cluster,
					Location:   "eastus", InternalIP: "10.0.0.10", Ready: true,
				}},
			Status: fleetnetv1alpha1.InternalServiceExportStatus{Conditions: []metav1.Condition{{
				Type: string(fleetnetv1beta1.ServiceExportConflict), Status: metav1.ConditionFalse, ObservedGeneration: 1,
			}}},
		})
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).
		WithStatusSubresource(&gatewayv1.Gateway{}, &gatewayv1.HTTPRoute{}, &gatewayv1.GatewayClass{}, &fleetnetv1alpha1.ServiceImport{}, &fleetnetv1alpha1.InternalServiceExport{}).
		WithIndex(&gatewayv1.Gateway{}, gatewayresolver.GatewayClassField, func(o client.Object) []string { return []string{string(o.(*gatewayv1.Gateway).Spec.GatewayClassName)} }).
		WithIndex(&gatewayv1.HTTPRoute{}, gatewayresolver.HTTPRouteGatewayField, func(o client.Object) []string {
			route := o.(*gatewayv1.HTTPRoute)
			var keys []string
			for _, parent := range route.Spec.ParentRefs {
				if key, recognized := gatewayresolver.GatewayParentKey(route.Namespace, parent); recognized {
					keys = append(keys, key.Namespace+"/"+key.Name)
				}
			}
			return keys
		}).
		WithIndex(&gatewayv1.HTTPRoute{}, gatewayresolver.HTTPRouteServiceImportField, func(client.Object) []string { return []string{"store/api"} }).
		WithIndex(&fleetnetv1alpha1.InternalServiceExport{}, gatewayresolver.ExportServiceField, func(o client.Object) []string {
			return []string{o.(*fleetnetv1alpha1.InternalServiceExport).Spec.ServiceReference.NamespacedName}
		}).Build()
	return c, gateway, route, service
}

func TestGatewayPrivateDiscoveryAndRouteRemoval(t *testing.T) {
	ctx := context.Background()
	c, gateway, route, _ := gatewayFixture(t, 1)
	r := &Reconciler{Client: c}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}
	if _, err := r.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, request.NamespacedName, gateway); err != nil {
		t.Fatal(err)
	}
	if gateway.Status.Listeners[0].AttachedRoutes != 1 || !strings.Contains(meta.FindStatusCondition(gateway.Status.Conditions, "Programmed").Message, "convergence") {
		t.Fatalf("private model did not resolve or parent references were counted twice: %#v", gateway.Status)
	}
	export := &fleetnetv1alpha1.InternalServiceExport{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: "fleet-member-member-0", Name: "store-api"}, export); err != nil {
		t.Fatal(err)
	}
	export.Spec.PrivateLinkOrigin.Ready = false
	if err := c.Update(ctx, export); err != nil {
		t.Fatal(err)
	}
	if requests := r.gatewaysForDependency(ctx, export); len(requests) == 0 || requests[0] != request {
		t.Fatalf("discovery change did not enqueue parent: %#v", requests)
	}
	if _, err := r.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, request.NamespacedName, gateway); err != nil {
		t.Fatal(err)
	}
	pending := meta.FindStatusCondition(gateway.Status.Conditions, "Programmed")
	if pending.Status != metav1.ConditionFalse || !strings.Contains(pending.Message, "no ready private origin") || gateway.Status.Listeners[0].AttachedRoutes != 1 {
		t.Fatalf("private unavailability was ignored or changed attachment count: %#v", gateway.Status)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(route), route); err != nil {
		t.Fatal(err)
	}
	route.Spec.ParentRefs = nil
	if err := c.Update(ctx, route); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, request.NamespacedName, gateway); err != nil || gateway.Status.Listeners[0].AttachedRoutes != 0 {
		t.Fatalf("removed parent retained attachment: %#v, %v", gateway.Status, err)
	}
}

func TestGatewayRunsBackendCapacityPreflight(t *testing.T) {
	for _, members := range []int{40, 41} {
		t.Run(fmt.Sprint(members), func(t *testing.T) {
			c, gateway, _, _ := gatewayFixture(t, members)
			r := &Reconciler{Client: c}
			request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}
			if _, err := r.Reconcile(context.Background(), request); err != nil {
				t.Fatal(err)
			}
			if err := c.Get(context.Background(), request.NamespacedName, gateway); err != nil {
				t.Fatal(err)
			}
			programmed := meta.FindStatusCondition(gateway.Status.Conditions, "Programmed")
			if programmed == nil || programmed.Status != metav1.ConditionFalse || strings.Contains(programmed.Message, "maximum is 40") != (members == 41) {
				t.Fatalf("backend preflight result for %d members: %#v", members, programmed)
			}
		})
	}
}

func TestGatewayPreflightsSharedPrivateLinkPorts(t *testing.T) {
	tests := []struct {
		name     string
		port     int32
		otherPLS bool
		conflict bool
	}{
		{"shared target same port", 80, false, false},
		{"shared target different ports", 8080, false, true},
		{"different targets different ports", 8080, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			c, gateway, route, service := gatewayFixture(t, 1)
			other := service.DeepCopy()
			other.Name, other.ResourceVersion = otherName, ""
			other.Status.Ports = []fleetnetv1alpha1.ServicePort{{Port: tt.port, Protocol: corev1.ProtocolTCP}}
			if err := c.Create(ctx, other); err != nil {
				t.Fatal(err)
			}
			var source fleetnetv1alpha1.InternalServiceExport
			if err := c.Get(ctx, client.ObjectKey{Namespace: "fleet-member-member-0", Name: "store-api"}, &source); err != nil {
				t.Fatal(err)
			}
			source.Name, source.ResourceVersion = "store-other", ""
			source.Spec.Ports = other.Status.Ports
			source.Spec.ServiceReference.Name = other.Name
			source.Spec.ServiceReference.NamespacedName = "store/other"
			source.Spec.ServiceReference.UID = "other-service"
			if tt.otherPLS {
				source.Spec.PrivateLinkOrigin.ResourceID += "-other"
			}
			if err := c.Create(ctx, &source); err != nil {
				t.Fatal(err)
			}
			rule := route.Spec.Rules[0].DeepCopy()
			rule.BackendRefs[0].Name = otherName
			rule.BackendRefs[0].Port = ptr.To(gatewayv1.PortNumber(tt.port))
			route.Spec.Rules = append(route.Spec.Rules, *rule)
			if err := c.Update(ctx, route); err != nil {
				t.Fatal(err)
			}
			r := &Reconciler{Client: c}
			request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}
			if _, err := r.Reconcile(ctx, request); err != nil {
				t.Fatal(err)
			}
			if err := c.Get(ctx, request.NamespacedName, gateway); err != nil {
				t.Fatal(err)
			}
			accepted := meta.FindStatusCondition(gateway.Status.Conditions, "Accepted")
			programmed := meta.FindStatusCondition(gateway.Status.Conditions, "Programmed")
			if accepted == nil || accepted.Status != metav1.ConditionTrue || programmed == nil ||
				programmed.Status != metav1.ConditionFalse || programmed.ObservedGeneration != gateway.Generation ||
				strings.Contains(programmed.Message, "different HTTP ports") != tt.conflict ||
				len(gateway.Status.Addresses) != 0 || len(gateway.Finalizers) != 0 {
				t.Fatalf("incorrect shared-target preflight: %#v", gateway.Status)
			}
			if !tt.conflict && !strings.Contains(programmed.Message, "multiple-backend routing") {
				t.Fatalf("valid backend inventory did not resolve: %#v", programmed)
			}
			version := gateway.ResourceVersion
			if _, err := r.Reconcile(ctx, request); err != nil {
				t.Fatal(err)
			}
			if err := c.Get(ctx, request.NamespacedName, gateway); err != nil || gateway.ResourceVersion != version {
				t.Fatalf("stable preflight rewrote status: %v", err)
			}
			if tt.conflict {
				if err := c.Get(ctx, client.ObjectKeyFromObject(route), route); err != nil {
					t.Fatal(err)
				}
				route.Spec.Rules = route.Spec.Rules[:1]
				if err := c.Update(ctx, route); err != nil {
					t.Fatal(err)
				}
				if _, err := r.Reconcile(ctx, request); err != nil {
					t.Fatal(err)
				}
				if err := c.Get(ctx, request.NamespacedName, gateway); err != nil {
					t.Fatal(err)
				}
				programmed = meta.FindStatusCondition(gateway.Status.Conditions, "Programmed")
				if programmed == nil || programmed.Status != metav1.ConditionFalse ||
					!strings.Contains(programmed.Message, "convergence") {
					t.Fatalf("withdrawn conflict retained stale diagnostics: %#v", programmed)
				}
			}
		})
	}
}

func TestGatewayDoesNotIgnoreDeclaredWAFProtection(t *testing.T) {
	ctx := context.Background()
	c, gateway, _, _ := gatewayFixture(t, 1)
	gateway.Annotations = map[string]string{
		"networking.fleet.azure.com/afd-waf-policy-id": "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg/providers/Microsoft.Network/frontdoorWebApplicationFirewallPolicies/waf",
	}
	if err := c.Update(ctx, gateway); err != nil {
		t.Fatal(err)
	}
	r := &Reconciler{Client: c}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}
	if _, err := r.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, request.NamespacedName, gateway); err != nil {
		t.Fatal(err)
	}
	programmed := meta.FindStatusCondition(gateway.Status.Conditions, "Programmed")
	if programmed == nil || programmed.Status != metav1.ConditionFalse ||
		!strings.Contains(programmed.Message, "WAF association is not implemented") || len(gateway.Status.Addresses) != 0 {
		t.Fatalf("declared WAF protection was silently ignored: %#v", gateway.Status)
	}
}
