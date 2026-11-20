/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package localservice

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewayresolver"
)

type assigningClient struct {
	client.Client
	creates int
}

func (c *assigningClient) Create(ctx context.Context, object client.Object, options ...client.CreateOption) error {
	c.creates++
	if object.GetUID() == "" {
		object.SetUID(types.UID(fmt.Sprintf("generated-%d", c.creates)))
	}
	return c.Client.Create(ctx, object, options...)
}

type fixedDiscovery struct{ calls int }

type discoveryFunc func(context.Context, *corev1.Service) (*fleetnetv1alpha1.PrivateLinkOrigin, error)

func (f discoveryFunc) Discover(ctx context.Context, service *corev1.Service) (*fleetnetv1alpha1.PrivateLinkOrigin, error) {
	return f(ctx, service)
}

func (d *fixedDiscovery) Discover(_ context.Context, _ *corev1.Service) (*fleetnetv1alpha1.PrivateLinkOrigin, error) {
	d.calls++
	return &fleetnetv1alpha1.PrivateLinkOrigin{Ready: true, InternalIP: "10.0.0.4", Location: "eastus",
		ResourceID: "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/nodes/providers/Microsoft.Network/privateLinkServices/origin"}, nil
}

type changeReader struct {
	client.Reader
	onGet  func(context.Context, client.ObjectKey, client.Object, error)
	onList func(context.Context, client.ObjectList, error)
}

func (r *changeReader) Get(ctx context.Context, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
	err := r.Reader.Get(ctx, key, object, options...)
	if r.onGet != nil {
		r.onGet(ctx, key, object, err)
	}
	return err
}

func (r *changeReader) List(ctx context.Context, list client.ObjectList, options ...client.ListOption) error {
	err := r.Reader.List(ctx, list, options...)
	if r.onList != nil {
		r.onList(ctx, list, err)
	}
	return err
}

func fixture(t *testing.T) (*Reconciler, *corev1.Service, *gatewayv1.HTTPRoute) {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, register := range []func(*runtime.Scheme) error{corev1.AddToScheme, discoveryv1.AddToScheme, gatewayv1.Install, fleetnetv1alpha1.AddToScheme} {
		if err := register(scheme); err != nil {
			t.Fatal(err)
		}
	}
	source := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "app", Name: "web", UID: "web-uid"},
		Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, ClusterIP: "10.96.0.1", Selector: map[string]string{"app": "web"},
			Ports: []corev1.ServicePort{{Name: "http", Port: 8080, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromString("container-port")}}}}
	conditions := []metav1.Condition{{Type: "Accepted", Status: metav1.ConditionTrue, ObservedGeneration: 1}}
	class := &gatewayv1.GatewayClass{ObjectMeta: metav1.ObjectMeta{Name: "afd", Generation: 1},
		Spec:   gatewayv1.GatewayClassSpec{ControllerName: gatewayv1.GatewayController(objectmeta.GatewayAPIControllerName)},
		Status: gatewayv1.GatewayClassStatus{Conditions: conditions}}
	gateway := &gatewayv1.Gateway{ObjectMeta: metav1.ObjectMeta{Namespace: "app", Name: "edge", Generation: 1},
		Spec:   gatewayv1.GatewaySpec{GatewayClassName: "afd", Listeners: []gatewayv1.Listener{{Name: "http", Protocol: gatewayv1.HTTPProtocolType, Port: 80}}},
		Status: gatewayv1.GatewayStatus{Conditions: conditions}}
	route := &gatewayv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Namespace: "app", Name: "root"},
		Spec: gatewayv1.HTTPRouteSpec{CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gatewayv1.ParentReference{{Name: "edge"}}},
			Rules: []gatewayv1.HTTPRouteRule{{BackendRefs: []gatewayv1.HTTPBackendRef{{BackendRef: gatewayv1.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{Name: "web", Port: ptr.To(gatewayv1.PortNumber(8080))}}}}}}}}
	slice := &discoveryv1.EndpointSlice{ObjectMeta: metav1.ObjectMeta{Namespace: "app", Name: "manual", UID: "slice-uid",
		Labels: map[string]string{discoveryv1.LabelServiceName: "web"}}, AddressType: discoveryv1.AddressTypeIPv4,
		Ports: []discoveryv1.EndpointPort{
			{Name: ptr.To("container-port"), Port: ptr.To(int32(7000)), Protocol: ptr.To(corev1.ProtocolTCP)},
			{Name: ptr.To("http"), Port: ptr.To(int32(9090)), Protocol: ptr.To(corev1.ProtocolTCP)}},
		Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.2.0.7"}, TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: "foreign", Name: "pod"}},
			{Addresses: []string{"10.2.0.8"}, Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(false)}},
			{Addresses: []string{"10.2.0.9"}, Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true), Terminating: ptr.To(true)}}}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&corev1.Service{}, &gatewayv1.Gateway{}, &gatewayv1.GatewayClass{}).
		WithObjects(source, route, class, gateway, slice, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "app"}}).Build()
	return &Reconciler{Client: &assigningClient{Client: c}, APIReader: c, Discovery: &fixedDiscovery{}}, source, route
}

func reconcileSource(t *testing.T, r *Reconciler, source *corev1.Service) ctrl.Result {
	t.Helper()
	result, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(source)})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func provision(t *testing.T, r *Reconciler, source *corev1.Service) *corev1.Service {
	t.Helper()
	reconcileSource(t, r, source)
	reconcileSource(t, r, source)
	var companion corev1.Service
	if err := r.APIReader.Get(context.Background(), client.ObjectKey{Namespace: source.Namespace,
		Name: CompanionName(source, source.Spec.Ports[0])}, &companion); err != nil {
		t.Fatal(err)
	}
	return &companion
}

func TestCompanionPreservesSourceAndMapsNamedEndpointPort(t *testing.T) {
	for _, headless := range []bool{false, true} {
		t.Run(fmt.Sprintf("headless=%v", headless), func(t *testing.T) {
			r, source, route := fixture(t)
			if headless {
				var current corev1.Service
				if err := r.APIReader.Get(context.Background(), client.ObjectKeyFromObject(source), &current); err != nil {
					t.Fatal(err)
				}
				current.Spec.ClusterIP = corev1.ClusterIPNone
				current.Spec.Selector = nil
				if err := r.Client.Update(context.Background(), &current); err != nil {
					t.Fatal(err)
				}
				source = &current
			}
			var before corev1.Service
			if err := r.APIReader.Get(context.Background(), client.ObjectKeyFromObject(source), &before); err != nil {
				t.Fatal(err)
			}
			companion := provision(t, r, source)
			var after corev1.Service
			if err := r.APIReader.Get(context.Background(), client.ObjectKeyFromObject(source), &after); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("source Service changed")
			}
			if companion.Spec.Selector != nil || companion.Spec.Type != corev1.ServiceTypeLoadBalancer ||
				companion.Annotations[plsCreate] != "true" || !owner(companion, "Service", source.Name, source.UID) {
				t.Fatalf("invalid companion: %+v", companion)
			}
			var slices discoveryv1.EndpointSliceList
			if err := r.APIReader.List(context.Background(), &slices, client.MatchingLabels{discoveryv1.LabelServiceName: companion.Name}); err != nil {
				t.Fatal(err)
			}
			if len(slices.Items) != 1 {
				t.Fatalf("mirror count=%d", len(slices.Items))
			}
			mirror := slices.Items[0]
			if *mirror.Ports[0].Port != 9090 || len(mirror.Endpoints) != 1 || mirror.Endpoints[0].Addresses[0] != "10.2.0.7" ||
				mirror.Endpoints[0].TargetRef != nil || !ptr.Deref(mirror.Endpoints[0].Conditions.Ready, false) {
				t.Fatalf("wrong mirrored port/endpoints: %+v", mirror)
			}
			backend, err := r.ResolveLocalService(context.Background(), source.Namespace, route.Spec.Rules[0].BackendRefs[0])
			if err != nil {
				t.Fatal(err)
			}
			if backend.Kind != "Service" || backend.Name != source.Name || backend.Port != 8080 || backend.Origins[0].ServiceUID != string(source.UID) ||
				backend.Origins[0].Cluster != "local" {
				t.Fatalf("wrong backend: %+v", backend)
			}
			creates := r.Client.(*assigningClient).creates
			if _, err := r.ResolveLocalService(context.Background(), source.Namespace, route.Spec.Rules[0].BackendRefs[0]); err != nil {
				t.Fatal(err)
			}
			if r.Client.(*assigningClient).creates != creates {
				t.Fatal("resolution performed writes")
			}
		})
	}
}

func TestForeignCompanionsAndSlicesAreNeverMutated(t *testing.T) {
	for _, foreignSlice := range []bool{false, true} {
		t.Run(fmt.Sprintf("slice=%v", foreignSlice), func(t *testing.T) {
			r, source, _ := fixture(t)
			var foreign client.Object
			if foreignSlice {
				reconcileSource(t, r, source)
				var companion corev1.Service
				if err := r.APIReader.Get(context.Background(), client.ObjectKey{Namespace: source.Namespace,
					Name: CompanionName(source, source.Spec.Ports[0])}, &companion); err != nil {
					t.Fatal(err)
				}
				foreign = &discoveryv1.EndpointSlice{ObjectMeta: metav1.ObjectMeta{Namespace: source.Namespace, Name: "foreign-slice",
					Labels: map[string]string{discoveryv1.LabelServiceName: companion.Name}}, AddressType: discoveryv1.AddressTypeIPv4}
			} else {
				foreign = desiredCompanion(source, source.Spec.Ports[0])
				foreign.SetOwnerReferences(nil)
			}
			if err := r.Client.Create(context.Background(), foreign); err != nil {
				t.Fatal(err)
			}
			before := foreign.DeepCopyObject().(client.Object)
			if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(source)}); err == nil {
				t.Fatal("expected ownership collision")
			}
			current := foreign.DeepCopyObject().(client.Object)
			if err := r.APIReader.Get(context.Background(), client.ObjectKeyFromObject(foreign), current); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, current) {
				t.Fatal("foreign object changed")
			}
		})
	}
}

func TestWithdrawalDeletesMirrorsAndRetainsCCMFinalizer(t *testing.T) {
	tests := []struct {
		name     string
		withdraw func(context.Context, *Reconciler, *corev1.Service, *gatewayv1.HTTPRoute) error
	}{
		{"reference removal", func(ctx context.Context, r *Reconciler, _ *corev1.Service, route *gatewayv1.HTTPRoute) error {
			return r.Client.Delete(ctx, route)
		}},
		{"class removal", func(ctx context.Context, r *Reconciler, _ *corev1.Service, _ *gatewayv1.HTTPRoute) error {
			return r.Client.Delete(ctx, &gatewayv1.GatewayClass{ObjectMeta: metav1.ObjectMeta{Name: "afd"}})
		}},
		{"namespace policy", func(ctx context.Context, r *Reconciler, _ *corev1.Service, _ *gatewayv1.HTTPRoute) error {
			var gateway gatewayv1.Gateway
			if err := r.APIReader.Get(ctx, client.ObjectKey{Namespace: "app", Name: "edge"}, &gateway); err != nil {
				return err
			}
			gateway.Spec.Listeners[0].AllowedRoutes = &gatewayv1.AllowedRoutes{Namespaces: &gatewayv1.RouteNamespaces{From: ptr.To(gatewayv1.NamespacesFromSelector),
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"allowed": "true"}}}}
			return r.Client.Update(ctx, &gateway)
		}},
		{"source deletion", func(ctx context.Context, r *Reconciler, source *corev1.Service, _ *gatewayv1.HTTPRoute) error {
			return r.Client.Delete(ctx, source)
		}},
		{"source deleting with finalizer", func(ctx context.Context, r *Reconciler, source *corev1.Service, _ *gatewayv1.HTTPRoute) error {
			var current corev1.Service
			if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(source), &current); err != nil {
				return err
			}
			current.Finalizers = []string{"example.com/source-cleanup"}
			if err := r.Client.Update(ctx, &current); err != nil {
				return err
			}
			return r.Client.Delete(ctx, &current)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			r, source, route := fixture(t)
			companion := provision(t, r, source)
			companion.Finalizers = []string{"service.kubernetes.io/load-balancer-cleanup"}
			if err := r.Client.Update(ctx, companion); err != nil {
				t.Fatal(err)
			}
			if err := test.withdraw(ctx, r, source, route); err != nil {
				t.Fatal(err)
			}
			result := reconcileSource(t, r, source)
			if result.RequeueAfter == 0 {
				t.Fatal("CCM cleanup not tracked")
			}
			var current corev1.Service
			if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(companion), &current); err != nil {
				t.Fatal(err)
			}
			if current.DeletionTimestamp == nil || !reflect.DeepEqual(current.Finalizers, companion.Finalizers) {
				t.Fatal("CCM finalizer was bypassed")
			}
			var slices discoveryv1.EndpointSliceList
			if err := r.APIReader.List(ctx, &slices, client.MatchingLabels{discoveryv1.LabelServiceName: companion.Name}); err != nil {
				t.Fatal(err)
			}
			if len(slices.Items) != 0 {
				t.Fatal("withdrawn endpoints remain")
			}
			current.Finalizers = nil
			if err := r.Client.Update(ctx, &current); err != nil {
				t.Fatal(err)
			}
			if result := reconcileSource(t, r, source); result.RequeueAfter != 0 {
				t.Fatal("completed cleanup still pending")
			}
		})
	}
}

func TestEndpointReadinessChangesBlockResolutionBeforeMirrorUpdate(t *testing.T) {
	r, source, route := fixture(t)
	provision(t, r, source)
	ctx := context.Background()
	var slice discoveryv1.EndpointSlice
	if err := r.APIReader.Get(ctx, client.ObjectKey{Namespace: "app", Name: "manual"}, &slice); err != nil {
		t.Fatal(err)
	}
	slice.Endpoints[0].Conditions.Terminating = ptr.To(true)
	if err := r.Client.Update(ctx, &slice); err != nil {
		t.Fatal(err)
	}
	_, err := r.ResolveLocalService(ctx, source.Namespace, route.Spec.Rules[0].BackendRefs[0])
	assertResolution(t, err, "BackendUnavailable", true)
	reconcileSource(t, r, source)
	var mirrors discoveryv1.EndpointSliceList
	if err := r.APIReader.List(ctx, &mirrors, client.MatchingLabels{discoveryv1.LabelManagedBy: managedBy}); err != nil {
		t.Fatal(err)
	}
	if len(mirrors.Items) != 0 {
		t.Fatal("terminating endpoints were not removed")
	}
}

func TestInvalidIntentAndConflictingPorts(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*gatewayv1.HTTPRoute)
	}{
		{"nonroot", func(route *gatewayv1.HTTPRoute) {
			route.Spec.Rules[0].Matches = []gatewayv1.HTTPRouteMatch{{Path: &gatewayv1.HTTPPathMatch{Value: ptr.To("/private")}}}
		}},
		{"cross namespace", func(route *gatewayv1.HTTPRoute) {
			route.Spec.Rules[0].BackendRefs[0].Namespace = ptr.To(gatewayv1.Namespace("other"))
		}},
		{"invalid later rule", func(route *gatewayv1.HTTPRoute) {
			rule := *route.Spec.Rules[0].DeepCopy()
			rule.BackendRefs[0].Name = "missing"
			route.Spec.Rules = append(route.Spec.Rules, rule)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r, source, route := fixture(t)
			test.mutate(route)
			if err := r.Client.Update(context.Background(), route); err != nil {
				t.Fatal(err)
			}
			reconcileSource(t, r, source)
			var companions corev1.ServiceList
			if err := r.APIReader.List(context.Background(), &companions, client.MatchingLabels{managedLabel: managedBy}); err != nil {
				t.Fatal(err)
			}
			if len(companions.Items) != 0 {
				t.Fatal("invalid intent provisioned an origin")
			}
		})
	}
	t.Run("conflict withdraws old companion", func(t *testing.T) {
		r, source, route := fixture(t)
		companion := provision(t, r, source)
		var current corev1.Service
		if err := r.APIReader.Get(context.Background(), client.ObjectKeyFromObject(source), &current); err != nil {
			t.Fatal(err)
		}
		current.Spec.Ports = append(current.Spec.Ports, corev1.ServicePort{Name: "second", Port: 8081, Protocol: corev1.ProtocolTCP})
		if err := r.Client.Update(context.Background(), &current); err != nil {
			t.Fatal(err)
		}
		second := route.DeepCopy()
		second.Name, second.ResourceVersion, second.UID = "second", "", ""
		second.Spec.Rules[0].BackendRefs[0].Port = ptr.To(gatewayv1.PortNumber(8081))
		if err := r.Client.Create(context.Background(), second); err != nil {
			t.Fatal(err)
		}
		_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(source)})
		assertResolution(t, err, "ConflictingPorts", true)
		if err := r.APIReader.Get(context.Background(), client.ObjectKeyFromObject(companion), &corev1.Service{}); !apierrors.IsNotFound(err) {
			t.Fatalf("conflicting companion remains: %v", err)
		}
	})
}

func TestResolutionErrors(t *testing.T) {
	r, source, route := fixture(t)
	ref := route.Spec.Rules[0].BackendRefs[0]
	_, err := r.ResolveLocalService(context.Background(), "app", ref)
	assertResolution(t, err, "BackendUnavailable", true)
	missing := *ref.DeepCopy()
	missing.Name = "missing"
	_, err = r.ResolveLocalService(context.Background(), "app", missing)
	assertResolution(t, err, "BackendNotFound", false)
	ref.Port = ptr.To(gatewayv1.PortNumber(9999))
	_, err = r.ResolveLocalService(context.Background(), "app", ref)
	assertResolution(t, err, "PortNotFound", false)
	ref.Namespace = ptr.To(gatewayv1.Namespace("other"))
	_, err = r.ResolveLocalService(context.Background(), source.Namespace, ref)
	assertResolution(t, err, "RefNotPermitted", false)
}

func assertResolution(t *testing.T, err error, reason string, valid bool) {
	t.Helper()
	var resolution *gatewayresolver.ResolutionError
	if !errors.As(err, &resolution) || resolution.Reason != reason || resolution.ReferenceValid != valid {
		t.Fatalf("error=%v; want %s, referenceValid=%v", err, reason, valid)
	}
}

func TestMirrorSkipsDeletedIPv6AndMismatchedPorts(t *testing.T) {
	_, source, _ := fixture(t)
	companion := desiredCompanion(source, source.Spec.Ports[0])
	slice := &discoveryv1.EndpointSlice{ObjectMeta: metav1.ObjectMeta{Name: "endpoints"}, AddressType: discoveryv1.AddressTypeIPv4,
		Ports:     []discoveryv1.EndpointPort{{Name: ptr.To("wrong"), Port: ptr.To(int32(8080))}},
		Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.1"}}}}
	if mirroredSlice(source, source.Spec.Ports[0], companion, slice) != nil {
		t.Fatal("guessed targetPort mapping")
	}
	slice.Ports[0].Name = ptr.To("http")
	slice.AddressType = discoveryv1.AddressTypeIPv6
	if mirroredSlice(source, source.Spec.Ports[0], companion, slice) != nil {
		t.Fatal("IPv6 mirrored")
	}
	slice.AddressType = discoveryv1.AddressTypeIPv4
	slice.DeletionTimestamp = ptr.To(metav1.NewTime(time.Now()))
	if mirroredSlice(source, source.Spec.Ports[0], companion, slice) != nil {
		t.Fatal("deleting slice mirrored")
	}
	slice.DeletionTimestamp = nil
	oldSource := source.DeepCopy()
	oldSource.UID = "previous-service"
	slice.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(oldSource, corev1.SchemeGroupVersion.WithKind("Service"))}
	if mirroredSlice(source, source.Spec.Ports[0], companion, slice) != nil {
		t.Fatal("slice belonging to a deleted source UID mirrored")
	}
}

func TestUncachedClassWithdrawalPreventsCompanionCreation(t *testing.T) {
	r, source, _ := fixture(t)
	reader := r.APIReader
	changed := false
	r.APIReader = &changeReader{Reader: reader, onGet: func(ctx context.Context, key client.ObjectKey, _ client.Object, err error) {
		if changed || key.Name != CompanionName(source, source.Spec.Ports[0]) || !apierrors.IsNotFound(err) {
			return
		}
		changed = true
		if err := r.Client.Delete(ctx, &gatewayv1.GatewayClass{ObjectMeta: metav1.ObjectMeta{Name: "afd"}}); err != nil {
			t.Fatal(err)
		}
	}}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(source)}); err == nil {
		t.Fatal("expected stale intent guard to stop creation")
	}
	if !changed {
		t.Fatal("race was not injected")
	}
	var companion corev1.Service
	if err := reader.Get(context.Background(), client.ObjectKey{Namespace: source.Namespace,
		Name: CompanionName(source, source.Spec.Ports[0])}, &companion); !apierrors.IsNotFound(err) {
		t.Fatalf("companion provisioned after class removal: %v", err)
	}
}

func TestUncachedSourceSliceChangePreventsStaleMirroring(t *testing.T) {
	r, source, _ := fixture(t)
	reconcileSource(t, r, source)
	reader := r.APIReader
	changed := false
	r.APIReader = &changeReader{Reader: reader, onGet: func(ctx context.Context, key client.ObjectKey, object client.Object, _ error) {
		if changed || key.Name != "manual" {
			return
		}

		slice, ok := object.(*discoveryv1.EndpointSlice)
		if !ok {
			return
		}
		changed = true
		slice.Endpoints[0].Conditions.Ready = ptr.To(false)
		if err := r.Client.Update(ctx, slice); err != nil {
			t.Fatal(err)
		}
	}}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(source)}); err == nil {
		t.Fatal("expected endpoint race guard to stop stale mirroring")
	}
	var mirrors discoveryv1.EndpointSliceList
	if err := reader.List(context.Background(), &mirrors, client.MatchingLabels{discoveryv1.LabelManagedBy: managedBy}); err != nil {
		t.Fatal(err)
	}
	if len(mirrors.Items) != 0 {
		t.Fatal("stale endpoints were exposed")
	}
}

func TestResolveRejectsDiscoveryInflightChanges(t *testing.T) {
	tests := []struct {
		name   string
		change func(context.Context, *Reconciler, *corev1.Service, *corev1.Service) error
	}{
		{"endpoint draining", func(ctx context.Context, r *Reconciler, _, _ *corev1.Service) error {
			var slice discoveryv1.EndpointSlice
			if err := r.APIReader.Get(ctx, client.ObjectKey{Namespace: "app", Name: "manual"}, &slice); err != nil {
				return err
			}
			slice.Endpoints[0].Conditions.Terminating = ptr.To(true)
			return r.Client.Update(ctx, &slice)
		}},
		{"companion drift", func(ctx context.Context, r *Reconciler, _, companion *corev1.Service) error {
			companion.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyLocal
			return r.Client.Update(ctx, companion)
		}},
		{"source deletion", func(ctx context.Context, r *Reconciler, source, _ *corev1.Service) error {
			return r.Client.Delete(ctx, source)
		}},
		{"Gateway withdrawal", func(ctx context.Context, r *Reconciler, _, _ *corev1.Service) error {
			return r.Client.Delete(ctx, &gatewayv1.Gateway{ObjectMeta: metav1.ObjectMeta{Namespace: "app", Name: "edge"}})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r, source, route := fixture(t)
			provision(t, r, source)
			r.Discovery = discoveryFunc(func(ctx context.Context, companion *corev1.Service) (*fleetnetv1alpha1.PrivateLinkOrigin, error) {
				if err := test.change(ctx, r, source, companion); err != nil {
					t.Fatal(err)
				}
				return (&fixedDiscovery{}).Discover(ctx, companion)
			})
			backend, err := r.ResolveLocalService(context.Background(), source.Namespace, route.Spec.Rules[0].BackendRefs[0])
			assertResolution(t, err, "BackendUnavailable", true)
			if len(backend.Origins) != 0 {
				t.Fatal("returned stale private origin")
			}
		})
	}
}

func TestCompanionAndMirrorDriftAreRepaired(t *testing.T) {
	r, source, route := fixture(t)
	companion := provision(t, r, source)
	ctx := context.Background()
	companion.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyLocal
	companion.Spec.HealthCheckNodePort = 30001
	companion.Spec.AllocateLoadBalancerNodePorts = ptr.To(false)
	companion.Annotations[objectmeta.ServiceAnnotationPLSResourceGroup] = "foreign"
	if err := r.Client.Update(ctx, companion); err != nil {
		t.Fatal(err)
	}
	_, err := r.ResolveLocalService(ctx, source.Namespace, route.Spec.Rules[0].BackendRefs[0])
	assertResolution(t, err, "BackendUnavailable", true)
	reconcileSource(t, r, source)
	var current corev1.Service
	if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(companion), &current); err != nil {
		t.Fatal(err)
	}
	if !companionValid(&current, source, source.Spec.Ports[0]) {
		t.Fatal("companion topology drift not repaired")
	}
	var mirrors discoveryv1.EndpointSliceList
	if err := r.APIReader.List(ctx, &mirrors, client.MatchingLabels{discoveryv1.LabelServiceName: companion.Name}); err != nil {
		t.Fatal(err)
	}
	mirror := mirrors.Items[0]
	mirror.Labels[discoveryv1.LabelServiceName] = "wrong-service"
	if err := r.Client.Update(ctx, &mirror); err != nil {
		t.Fatal(err)
	}
	reconcileSource(t, r, source)
	if _, err := r.ResolveLocalService(ctx, source.Namespace, route.Spec.Rules[0].BackendRefs[0]); err != nil {
		t.Fatal(err)
	}
	// The API makes addressType immutable. Recreate, rather than attempting an update.
	if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(&mirror), &mirror); err != nil {
		t.Fatal(err)
	}
	mirror.AddressType = discoveryv1.AddressTypeIPv6
	if err := r.Client.Update(ctx, &mirror); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(source)}); err == nil {
		t.Fatal("expected owned slice recreation")
	}
	reconcileSource(t, r, source)
	if _, err := r.ResolveLocalService(ctx, source.Namespace, route.Spec.Rules[0].BackendRefs[0]); err != nil {
		t.Fatal(err)
	}
}

func TestGatewayWithdrawalCleansUpAndRestoredReferenceStopsDeletion(t *testing.T) {
	t.Run("withdrawal", func(t *testing.T) {
		r, source, _ := fixture(t)
		companion := provision(t, r, source)
		if err := r.Client.Delete(context.Background(), &gatewayv1.Gateway{ObjectMeta: metav1.ObjectMeta{Namespace: "app", Name: "edge"}}); err != nil {
			t.Fatal(err)
		}
		reconcileSource(t, r, source)
		if err := r.APIReader.Get(context.Background(), client.ObjectKeyFromObject(companion), &corev1.Service{}); !apierrors.IsNotFound(err) {
			t.Fatalf("withdrawn companion remains: %v", err)
		}
	})
	t.Run("restoration race", func(t *testing.T) {
		r, source, route := fixture(t)
		companion := provision(t, r, source)
		if err := r.Client.Delete(context.Background(), route); err != nil {
			t.Fatal(err)
		}
		restored := false
		r.APIReader = &changeReader{Reader: r.APIReader, onGet: func(ctx context.Context, _ client.ObjectKey, object client.Object, _ error) {
			if _, ok := object.(*discoveryv1.EndpointSlice); !ok || restored {
				return
			}
			restored = true
			route.UID, route.ResourceVersion = "", ""
			if err := r.Client.Create(ctx, route); err != nil {
				t.Fatal(err)
			}
		}}
		if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(source)}); err == nil {
			t.Fatal("expected restored-reference race guard")
		}
		if !restored {
			t.Fatal("restoration race not injected")
		}
		if err := r.APIReader.Get(context.Background(), client.ObjectKeyFromObject(companion), &corev1.Service{}); err != nil {
			t.Fatal("referenced companion deleted", err)
		}
		if _, err := r.ResolveLocalService(context.Background(), source.Namespace, route.Spec.Rules[0].BackendRefs[0]); err != nil {
			t.Fatal("referenced mirrors deleted", err)
		}
	})
}

func TestReadyMirrorListRaceRejectsDrainingSource(t *testing.T) {
	r, source, route := fixture(t)
	provision(t, r, source)
	discovery := r.Discovery.(*fixedDiscovery)
	changed := false
	r.APIReader = &changeReader{Reader: r.APIReader, onList: func(ctx context.Context, list client.ObjectList, _ error) {
		slices, ok := list.(*discoveryv1.EndpointSliceList)
		if !ok || changed || len(slices.Items) == 0 || slices.Items[0].Labels[discoveryv1.LabelManagedBy] != managedBy {
			return
		}
		changed = true
		var sourceSlice discoveryv1.EndpointSlice
		if err := r.Client.Get(ctx, client.ObjectKey{Namespace: source.Namespace, Name: "manual"}, &sourceSlice); err != nil {
			t.Fatal(err)
		}
		sourceSlice.Endpoints[0].Conditions.Terminating = ptr.To(true)
		if err := r.Client.Update(ctx, &sourceSlice); err != nil {
			t.Fatal(err)
		}
	}}
	_, err := r.ResolveLocalService(context.Background(), source.Namespace, route.Spec.Rules[0].BackendRefs[0])
	assertResolution(t, err, "BackendUnavailable", true)
	if !changed {
		t.Fatal("list race not injected")
	}
	if discovery.calls != 0 {
		t.Fatal("stale endpoint set reached Azure discovery")
	}
}

func TestResolveRejectsIngressChangeDuringFinalEndpointChecks(t *testing.T) {
	r, source, route := fixture(t)
	companion := provision(t, r, source)
	companion.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{{IP: "10.0.0.4"}}
	if err := r.Client.Status().Update(context.Background(), companion); err != nil {
		t.Fatal(err)
	}
	sourceLists := 0
	r.APIReader = &changeReader{Reader: r.APIReader, onList: func(ctx context.Context, list client.ObjectList, _ error) {
		slices, ok := list.(*discoveryv1.EndpointSliceList)
		if !ok || len(slices.Items) == 0 || slices.Items[0].Labels[discoveryv1.LabelServiceName] != source.Name {
			return
		}
		sourceLists++
		if sourceLists != 4 {
			return
		}
		companion.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{{IP: "10.0.0.5"}}
		if err := r.Client.Status().Update(ctx, companion); err != nil {
			t.Fatal(err)
		}
	}}
	backend, err := r.ResolveLocalService(context.Background(), source.Namespace, route.Spec.Rules[0].BackendRefs[0])
	assertResolution(t, err, "BackendUnavailable", true)
	if sourceLists != 4 {
		t.Fatal("ingress race not injected")
	}
	if len(backend.Origins) != 0 {
		t.Fatal("stale ingress was approved")
	}
}
