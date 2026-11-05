/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gateway

import (
	"context"
	"errors"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
	"go.goms.io/fleet-networking/pkg/providers/azure/frontdoor"
)

const (
	ownershipForeignFinalizer = "foreign.example/cleanup"
	ownershipHubUID           = "trusted-hub"
	ownershipGatewayResource  = "gateways"
	ownershipExportNamespace  = "fleet-member-member-0"
	ownershipAccessLabel      = "test-ingress"
)

type ownershipReader struct {
	client.Reader
	t        *testing.T
	afterGet func(client.Object)
}

func (r ownershipReader) Get(ctx context.Context, key client.ObjectKey, object client.Object, opts ...client.GetOption) error {
	if err := r.Reader.Get(ctx, key, object, opts...); err != nil {
		return err
	}
	if r.afterGet != nil {
		r.afterGet(object)
	}
	return nil
}

func (r ownershipReader) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	options := &client.ListOptions{}
	options.ApplyOptions(opts)
	if options.FieldSelector != nil && !options.FieldSelector.Empty() {
		r.t.Fatalf("uncached ownership resolution sent a cache-only field selector: %s", options.FieldSelector)
	}
	return r.Reader.List(ctx, list, opts...)
}

func ownershipModel(t *testing.T, c client.Client, gateway *gatewayv1.Gateway) gatewaymodel.GlobalGateway {
	t.Helper()
	var class gatewayv1.GatewayClass
	if err := c.Get(context.Background(), client.ObjectKey{Name: string(gateway.Spec.GatewayClassName)}, &class); err != nil {
		t.Fatal(err)
	}
	model, err := (&Reconciler{Client: c}).resolve(context.Background(), gateway.DeepCopy(), &class)
	if err != nil || model == nil || len(model.Routes) == 0 {
		t.Fatalf("invalid ownership fixture: %#v, %v", model, err)
	}
	return *model
}

func TestPrepareProvisioningPersistsObservedOwnership(t *testing.T) {
	c, gateway, route, _ := gatewayFixture(t, 1)
	addObservationHub(t, c)
	gateway.Finalizers = []string{ownershipForeignFinalizer}
	gateway.Annotations = map[string]string{"foreign.example/config": "preserve"}
	if err := c.Update(context.Background(), gateway); err != nil {
		t.Fatal(err)
	}
	model := ownershipModel(t, c, gateway)
	unrelated := route.DeepCopy()
	unrelated.ResourceVersion, unrelated.UID = "", "unrelated"
	unrelated.Name, unrelated.Namespace = "other", "namespace-not-present"
	unrelated.Spec.ParentRefs = []gatewayv1.ParentReference{{Name: "different"}}
	if err := c.Create(context.Background(), unrelated); err != nil {
		t.Fatal(err)
	}
	patches := 0
	writesOnly := interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
		Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
			t.Fatal("ownership preparation used cached Get")
			return nil
		},
		List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
			t.Fatal("ownership preparation used cached List")
			return nil
		},
		Patch: func(ctx context.Context, cl client.WithWatch, object client.Object, patch client.Patch, opts ...client.PatchOption) error {
			patches++
			return cl.Patch(ctx, object, patch, opts...)
		},
	})
	cleaner := &fakeProfileCleaner{}
	r := &Reconciler{Client: writesOnly, APIReader: ownershipReader{Reader: c, t: t}, ProfileCleanup: cleaner}
	want := frontdoor.GatewayOwner{HubUID: ownershipHubUID, Namespace: gateway.Namespace, Name: gateway.Name, UID: string(gateway.UID)}
	for i := 0; i < 2; i++ {
		got, err := r.prepareProvisioning(context.Background(), gateway, model)
		if err != nil || got != want {
			t.Fatalf("ownership was not persisted/observed: %#v, %v", got, err)
		}
	}
	var current gatewayv1.Gateway
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(gateway), &current); err != nil {
		t.Fatal(err)
	}
	if patches != 1 || len(current.Finalizers) != 2 || !controllerutil.ContainsFinalizer(&current, objectmeta.GatewayAFDCleanupFinalizer) ||
		current.Annotations["foreign.example/config"] != "preserve" || len(cleaner.owners) != 0 ||
		!reflect.DeepEqual(current.Status, gateway.Status) {
		t.Fatalf("ownership preparation lost metadata, changed status or invoked Azure: %#v, patches=%d", current, patches)
	}
}

func TestPrepareProvisioningConflictAndPatchFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		c, gateway, _, _ := gatewayFixture(t, 1)
		addObservationHub(t, c)
		model := ownershipModel(t, c, gateway)
		conflict := &cleanupConflictClient{Client: c}
		r := &Reconciler{Client: conflict, APIReader: ownershipReader{Reader: c, t: t}, ProfileCleanup: &fakeProfileCleaner{}}
		if fail {
			r.Client = interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
				Patch: func(context.Context, client.WithWatch, client.Object, client.Patch, ...client.PatchOption) error {
					return apierrors.NewForbidden(schema.GroupResource{Group: gatewayv1.GroupName, Resource: ownershipGatewayResource}, gateway.Name, errors.New("denied"))
				},
			})
		}
		got, err := r.prepareProvisioning(context.Background(), gateway, model)
		if fail {
			if !apierrors.IsForbidden(err) || got != (frontdoor.GatewayOwner{}) {
				t.Fatalf("failed persistence returned ownership: %#v, %v", got, err)
			}
			continue
		}
		var current gatewayv1.Gateway
		if err := c.Get(context.Background(), client.ObjectKeyFromObject(gateway), &current); err != nil {
			t.Fatal(err)
		}
		if err != nil || got.UID != string(gateway.UID) || conflict.patches != 2 ||
			!controllerutil.ContainsFinalizer(&current, "concurrent.example/cleanup") {
			t.Fatalf("conflict did not preserve current metadata: %#v, %v, patches=%d", got, err, conflict.patches)
		}
	}
}

func TestPrepareProvisioningRejectsChangedIntent(t *testing.T) {
	tests := []struct {
		name   string
		change func(client.Client, *gatewayv1.Gateway, *gatewayv1.HTTPRoute) error
	}{
		{"Gateway generation", func(c client.Client, g *gatewayv1.Gateway, _ *gatewayv1.HTTPRoute) error {
			g = g.DeepCopy()
			g.Generation++
			return c.Update(context.Background(), g)
		}},
		{"Gateway deleting", func(c client.Client, g *gatewayv1.Gateway, _ *gatewayv1.HTTPRoute) error {
			g = g.DeepCopy()
			g.Finalizers = []string{ownershipForeignFinalizer}
			if err := c.Update(context.Background(), g); err != nil {
				return err
			}
			return c.Delete(context.Background(), g)
		}},
		{"route withdrawn", func(c client.Client, _ *gatewayv1.Gateway, route *gatewayv1.HTTPRoute) error {
			route.Spec.ParentRefs = nil
			return c.Update(context.Background(), route)
		}},
		{"new route not in cached model", func(c client.Client, _ *gatewayv1.Gateway, route *gatewayv1.HTTPRoute) error {
			route = route.DeepCopy()
			route.Name, route.UID, route.ResourceVersion = "new-route", "new-route", ""
			return c.Create(context.Background(), route)
		}},
		{"backend changed without Gateway generation", func(c client.Client, _ *gatewayv1.Gateway, route *gatewayv1.HTTPRoute) error {
			var export fleetnetv1alpha1.InternalServiceExport
			key := client.ObjectKey{Namespace: ownershipExportNamespace, Name: route.Namespace + "-" + string(route.Spec.Rules[0].BackendRefs[0].Name)}
			if err := c.Get(context.Background(), key, &export); err != nil {
				return err
			}
			export.Spec.PrivateLinkOrigin.InternalIP = "10.0.0.99"
			return c.Update(context.Background(), &export)
		}},
		{"backend unavailable", func(c client.Client, _ *gatewayv1.Gateway, route *gatewayv1.HTTPRoute) error {
			return c.Delete(context.Background(), &fleetnetv1alpha1.ServiceImport{ObjectMeta: metav1.ObjectMeta{
				Namespace: route.Namespace, Name: string(route.Spec.Rules[0].BackendRefs[0].Name),
			}})
		}},
		{"GatewayClass ownership changes", func(c client.Client, gateway *gatewayv1.Gateway, _ *gatewayv1.HTTPRoute) error {
			var class gatewayv1.GatewayClass
			if err := c.Get(context.Background(), client.ObjectKey{Name: string(gateway.Spec.GatewayClassName)}, &class); err != nil {
				return err
			}
			class.Spec.ControllerName = "foreign.example/controller"
			return c.Update(context.Background(), &class)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, gateway, route, _ := gatewayFixture(t, 1)
			addObservationHub(t, c)
			model := ownershipModel(t, c, gateway)
			if err := tt.change(c, gateway, route); err != nil {
				t.Fatal(err)
			}
			r := &Reconciler{Client: c, APIReader: ownershipReader{Reader: c, t: t}, ProfileCleanup: &fakeProfileCleaner{}}
			got, err := r.prepareProvisioning(context.Background(), gateway, model)
			if err == nil || got != (frontdoor.GatewayOwner{}) {
				t.Fatalf("changed intent returned prepared ownership: %#v, %v", got, err)
			}
			var current gatewayv1.Gateway
			if err := c.Get(context.Background(), client.ObjectKeyFromObject(gateway), &current); err != nil {
				t.Fatal(err)
			}
			if controllerutil.ContainsFinalizer(&current, objectmeta.GatewayAFDCleanupFinalizer) {
				t.Fatal("changed intent acquired cleanup ownership")
			}
		})
	}
}

func TestPrepareProvisioningRechecksAfterPatch(t *testing.T) {
	const (
		readFailureCase      = "uncached reread fails"
		cancelAfterPatchCase = "canceled after persistence"
	)
	for _, change := range []string{"Gateway UID", "hub UID", "route withdrawal", "lost finalizer", readFailureCase, cancelAfterPatchCase} {
		t.Run(change, func(t *testing.T) {
			c, gateway, route, _ := gatewayFixture(t, 1)
			addObservationHub(t, c)
			model := ownershipModel(t, c, gateway)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			patched := false
			readFailure := errors.New("uncached read failed")
			r := &Reconciler{ProfileCleanup: &fakeProfileCleaner{}}
			r.Client = interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
				Patch: func(ctx context.Context, cl client.WithWatch, object client.Object, patch client.Patch, opts ...client.PatchOption) error {
					if err := cl.Patch(ctx, object, patch, opts...); err != nil {
						return err
					}
					patched = true
					if change == cancelAfterPatchCase {
						cancel()
					}
					if change == "route withdrawal" {
						route.Spec.ParentRefs = nil
						return cl.Update(ctx, route)
					}
					return nil
				},
			})
			reader := interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
				Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, object client.Object, opts ...client.GetOption) error {
					if patched && change == readFailureCase {
						return readFailure
					}
					return cl.Get(ctx, key, object, opts...)
				},
			})
			r.APIReader = ownershipReader{Reader: reader, t: t, afterGet: func(object client.Object) {
				if !patched {
					return
				}
				switch current := object.(type) {
				case *gatewayv1.Gateway:
					if change == "Gateway UID" {
						current.UID = "replacement-gateway"
					}
					if change == "lost finalizer" {
						current.Finalizers = nil
					}
				case *corev1.Namespace:
					if current.Name == metav1.NamespaceSystem && change == "hub UID" {
						current.UID = "replacement-hub"
					}
				}
			}}
			got, err := r.prepareProvisioning(ctx, gateway, model)
			if err == nil || got != (frontdoor.GatewayOwner{}) || !patched {
				t.Fatalf("post-persistence change returned ownership: %#v, %v", got, err)
			}
			if change == readFailureCase && !errors.Is(err, readFailure) {
				t.Fatalf("lost uncached read failure: %v", err)
			}
			if change == cancelAfterPatchCase && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
			}
		})
	}
}

func TestPrepareProvisioningStopsRetryOnDependencyChange(t *testing.T) {
	c, gateway, route, _ := gatewayFixture(t, 1)
	addObservationHub(t, c)
	model := ownershipModel(t, c, gateway)
	patches := 0
	writer := interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
		Patch: func(ctx context.Context, cl client.WithWatch, object client.Object, _ client.Patch, _ ...client.PatchOption) error {
			patches++
			route.Spec.ParentRefs = nil
			if err := cl.Update(ctx, route); err != nil {
				return err
			}
			return apierrors.NewConflict(schema.GroupResource{Group: gatewayv1.GroupName, Resource: ownershipGatewayResource},
				object.GetName(), errors.New("intent changed during metadata conflict"))
		},
	})
	r := &Reconciler{Client: writer, APIReader: ownershipReader{Reader: c, t: t}, ProfileCleanup: &fakeProfileCleaner{}}
	got, err := r.prepareProvisioning(context.Background(), gateway, model)
	if !errors.Is(err, errProvisioningIntentChanged) || got != (frontdoor.GatewayOwner{}) || patches != 1 {
		t.Fatalf("metadata retry ignored withdrawn intent: %#v, %v, patches=%d", got, err, patches)
	}
	var current gatewayv1.Gateway
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(gateway), &current); err != nil {
		t.Fatal(err)
	}
	if controllerutil.ContainsFinalizer(&current, objectmeta.GatewayAFDCleanupFinalizer) {
		t.Fatal("withdrawn intent acquired ownership during conflict retry")
	}
}

func TestPrepareProvisioningMissingPrerequisites(t *testing.T) {
	for _, kind := range []string{"no reader", "no cleaner", "empty model", "model owner mismatch", "hub Namespace absent", "empty hub UID", "canceled"} {
		t.Run(kind, func(t *testing.T) {
			c, gateway, _, _ := gatewayFixture(t, 1)
			if kind != "hub Namespace absent" {
				addObservationHub(t, c)
			}
			model := ownershipModel(t, c, gateway)
			r := &Reconciler{Client: c, APIReader: ownershipReader{Reader: c, t: t}, ProfileCleanup: &fakeProfileCleaner{}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch kind {
			case "no reader":
				r.APIReader = nil
			case "no cleaner":
				r.ProfileCleanup = nil
			case "empty model":
				model.Routes = nil
			case "model owner mismatch":
				model.UID = "foreign"
			case "empty hub UID":
				r.APIReader = ownershipReader{Reader: c, t: t, afterGet: func(o client.Object) {
					if namespace, ok := o.(*corev1.Namespace); ok && namespace.Name == metav1.NamespaceSystem {
						namespace.UID = ""
					}
				}}
			case "canceled":
				cancel()
			}
			got, err := r.prepareProvisioning(ctx, gateway, model)
			if err == nil || got != (frontdoor.GatewayOwner{}) {
				t.Fatalf("missing prerequisites returned ownership: %#v, %v", got, err)
			}
		})
	}
}

func TestPrepareProvisioningRechecksCrossNamespaceAuthorization(t *testing.T) {
	for _, change := range []string{"authorized", "grant revoked", "namespace policy revoked"} {
		t.Run(change, func(t *testing.T) {
			ctx := context.Background()
			c, gateway, route, _ := gatewayFixture(t, 1)
			addObservationHub(t, c)
			routeNamespace := "tenant-routes"
			namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
				Name: routeNamespace, Labels: map[string]string{ownershipAccessLabel: "allowed"},
			}}
			if err := c.Create(ctx, namespace); err != nil {
				t.Fatal(err)
			}
			gateway.Spec.Listeners[0].AllowedRoutes = &gatewayv1.AllowedRoutes{Namespaces: &gatewayv1.RouteNamespaces{
				From: ptr.To(gatewayv1.NamespacesFromSelector), Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{ownershipAccessLabel: "allowed"},
				},
			}}
			if err := c.Update(ctx, gateway); err != nil {
				t.Fatal(err)
			}
			if err := c.Delete(ctx, route); err != nil {
				t.Fatal(err)
			}
			route = route.DeepCopy()
			route.Namespace, route.ResourceVersion = routeNamespace, ""
			for i := range route.Spec.ParentRefs {
				route.Spec.ParentRefs[i].Namespace = ptr.To(gatewayv1.Namespace(gateway.Namespace))
			}
			route.Spec.Rules[0].BackendRefs[0].Namespace = ptr.To(gatewayv1.Namespace(gateway.Namespace))
			if err := c.Create(ctx, route); err != nil {
				t.Fatal(err)
			}
			grant := &gatewayv1beta1.ReferenceGrant{ObjectMeta: metav1.ObjectMeta{Namespace: gateway.Namespace, Name: "allow-route"},
				Spec: gatewayv1beta1.ReferenceGrantSpec{
					From: []gatewayv1beta1.ReferenceGrantFrom{{
						Group: gatewayv1.GroupName, Kind: "HTTPRoute", Namespace: gatewayv1.Namespace(routeNamespace),
					}},
					To: []gatewayv1beta1.ReferenceGrantTo{{
						Group: gatewayv1.Group(fleetnetv1alpha1.GroupVersion.Group), Kind: "ServiceImport",
						Name: ptr.To(route.Spec.Rules[0].BackendRefs[0].Name),
					}},
				}}
			if err := c.Create(ctx, grant); err != nil {
				t.Fatal(err)
			}
			model := ownershipModel(t, c, gateway)
			switch change {
			case "grant revoked":
				if err := c.Delete(ctx, grant); err != nil {
					t.Fatal(err)
				}
			case "namespace policy revoked":
				namespace.Labels[ownershipAccessLabel] = "denied"
				if err := c.Update(ctx, namespace); err != nil {
					t.Fatal(err)
				}
			}
			r := &Reconciler{Client: c, APIReader: ownershipReader{Reader: c, t: t}, ProfileCleanup: &fakeProfileCleaner{}}
			got, err := r.prepareProvisioning(ctx, gateway, model)
			if change == "authorized" {
				if err != nil || got.UID != string(gateway.UID) {
					t.Fatalf("valid cross-namespace authorization rejected: %#v, %v", got, err)
				}
			} else if !errors.Is(err, errProvisioningIntentChanged) || got != (frontdoor.GatewayOwner{}) {
				t.Fatalf("revoked authorization returned ownership: %#v, %v", got, err)
			}
		})
	}
}
