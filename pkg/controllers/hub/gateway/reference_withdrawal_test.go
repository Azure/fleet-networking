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
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/providers/azure/frontdoor"
)

const (
	referenceSuspensionMessage = "endpoint suspension"
	referenceGrantResource     = "referencegrants"
)

func referenceWithdrawalFixture(t *testing.T) (client.Client, *gatewayv1.Gateway, *gatewayv1.HTTPRoute, *gatewayv1beta1.ReferenceGrant) {
	t.Helper()
	const (
		backendKind    = "ServiceImport"
		routeKind      = "HTTPRoute"
		routeNamespace = "grant-client"
	)
	ctx := context.Background()
	c, gateway, route, _ := gatewayFixture(t, 1)
	addObservationHub(t, c)
	gateway.Finalizers = []string{objectmeta.GatewayAFDCleanupFinalizer}
	gateway.Spec.Listeners[0].AllowedRoutes = &gatewayv1.AllowedRoutes{Namespaces: &gatewayv1.RouteNamespaces{
		From: ptr.To(gatewayv1.NamespacesFromAll),
	}}
	if err := c.Update(ctx, gateway); err != nil {
		t.Fatal(err)
	}
	if err := c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: routeNamespace}}); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(ctx, route); err != nil {
		t.Fatal(err)
	}
	route.Namespace, route.UID, route.ResourceVersion = routeNamespace, "grant-route", ""
	for i := range route.Spec.ParentRefs {
		route.Spec.ParentRefs[i].Namespace = ptr.To(gatewayv1.Namespace(gateway.Namespace))
	}
	route.Spec.Rules[0].BackendRefs[0].Namespace = ptr.To(gatewayv1.Namespace(gateway.Namespace))
	if err := c.Create(ctx, route); err != nil {
		t.Fatal(err)
	}
	grant := &gatewayv1beta1.ReferenceGrant{ObjectMeta: metav1.ObjectMeta{Namespace: gateway.Namespace, Name: "permit-private-api"},
		Spec: gatewayv1beta1.ReferenceGrantSpec{
			From: []gatewayv1beta1.ReferenceGrantFrom{{Group: gatewayv1.GroupName, Kind: routeKind, Namespace: gatewayv1.Namespace(route.Namespace)}},
			To: []gatewayv1beta1.ReferenceGrantTo{{Group: gatewayv1.Group(fleetnetv1alpha1.GroupVersion.Group),
				Kind: backendKind, Name: ptr.To(route.Spec.Rules[0].BackendRefs[0].Name)}},
		}}
	if err := c.Create(ctx, grant); err != nil {
		t.Fatal(err)
	}
	return c, gateway, route, grant
}

func TestGatewayAllDeniedReferencesSuspendWithoutTargetReads(t *testing.T) {
	const deletingGrantCase = "grant marked deleting"
	for _, scenario := range []string{"grant removed", deletingGrantCase, "target also missing"} {
		c, gateway, route, grant := referenceWithdrawalFixture(t)
		stager := &fakeGraphStager{result: frontdoor.DisabledGraphStagingResult{
			Roots: frontdoor.RootProvisioningResult{State: frontdoor.ProfileProvisioningPending},
		}, suspension: frontdoor.EndpointSuspensionResult{State: frontdoor.EndpointSuspended}}
		r := &Reconciler{Client: c, APIReader: c, ProfileCleanup: &fakeProfileCleaner{}, GraphStager: stager}
		request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}
		if _, err := r.Reconcile(context.Background(), request); err != nil {
			t.Fatal(err)
		}
		if stager.calls != 1 || stager.suspensions != 0 {
			t.Fatalf("authorized route was not staged: %#v", stager)
		}
		if scenario == deletingGrantCase {
			grant.Finalizers = []string{ownershipForeignFinalizer}
			if err := c.Update(context.Background(), grant); err != nil {
				t.Fatal(err)
			}
		}
		if err := c.Delete(context.Background(), grant); err != nil {
			t.Fatal(err)
		}
		if scenario == "target also missing" {
			if err := c.Delete(context.Background(), &fleetnetv1alpha1.ServiceImport{ObjectMeta: metav1.ObjectMeta{
				Namespace: gateway.Namespace, Name: string(route.Spec.Rules[0].BackendRefs[0].Name),
			}}); err != nil {
				t.Fatal(err)
			}
		}
		guarded := interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
			Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, object client.Object, opts ...client.GetOption) error {
				if _, target := object.(*fleetnetv1alpha1.ServiceImport); target {
					t.Fatal("denied reference read its ServiceImport")
				}
				return cl.Get(ctx, key, object, opts...)
			},
			List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, target := list.(*fleetnetv1alpha1.InternalServiceExportList); target {
					t.Fatal("denied reference read internal exports")
				}
				return cl.List(ctx, list, opts...)
			},
		})
		r.Client, r.APIReader = guarded, ownershipReader{Reader: guarded, t: t}
		result, err := r.Reconcile(context.Background(), request)
		if err != nil || result.RequeueAfter < 30*time.Second || result.RequeueAfter > 36*time.Second || stager.suspensions != 1 || stager.calls != 1 {
			t.Fatalf("all-denied graph did not withdraw: %#v, %v, provider=%#v", result, err, stager)
		}
		if err := c.Get(context.Background(), request.NamespacedName, gateway); err != nil {
			t.Fatal(err)
		}
		pending := meta.FindStatusCondition(gateway.Status.Conditions, "Programmed")
		if pending == nil || pending.Status != metav1.ConditionFalse || !strings.Contains(pending.Message, referenceSuspensionMessage) ||
			len(gateway.Status.Addresses) != 0 || !controllerutil.ContainsFinalizer(gateway, objectmeta.GatewayAFDCleanupFinalizer) {
			t.Fatalf("denial withdrawal lost cleanup ownership or truthful status: %#v", gateway)
		}
	}
}

func TestGatewayDeniedReferencesSuspendWithoutPartialGraph(t *testing.T) {
	for _, unresolved := range []bool{false, true} {
		c, gateway, route, grant := referenceWithdrawalFixture(t)
		if err := c.Delete(context.Background(), grant); err != nil {
			t.Fatal(err)
		}
		other := route.DeepCopy()
		other.Name, other.Namespace, other.UID, other.ResourceVersion = "other-authorization", gateway.Namespace, "other-authorization", ""
		other.Spec.Rules[0].BackendRefs[0].Namespace = nil
		if unresolved {
			other.Spec.Rules[0].BackendRefs[0].Name = "pending-import"
		}
		if err := c.Create(context.Background(), other); err != nil {
			t.Fatal(err)
		}
		stager := &fakeGraphStager{}
		r := &Reconciler{Client: c, APIReader: c, ProfileCleanup: &fakeProfileCleaner{}, GraphStager: stager}
		var class gatewayv1.GatewayClass
		if err := c.Get(context.Background(), client.ObjectKey{Name: string(gateway.Spec.GatewayClassName)}, &class); err != nil {
			t.Fatal(err)
		}
		model, err := r.resolve(context.Background(), gateway.DeepCopy(), &class)
		if err != nil || model != nil {
			t.Fatalf("mixed denial produced a complete/partial candidate: %#v, %v", model, err)
		}
		if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}); err != nil {
			t.Fatal(err)
		}
		if stager.calls != 0 || stager.suspensions != 1 {
			t.Fatalf("mixed graph did not suspend the whole endpoint without partial staging: %#v", stager)
		}
	}
}

func addWithdrawalPeer(t *testing.T, c client.Client, gateway *gatewayv1.Gateway, route *gatewayv1.HTTPRoute) {
	t.Helper()
	peer := route.DeepCopy()
	peer.Name, peer.Namespace, peer.UID, peer.ResourceVersion = "withdrawal-peer", gateway.Namespace, "withdrawal-peer", ""
	peer.Spec.Rules[0].BackendRefs[0].Namespace = nil
	if err := c.Create(context.Background(), peer); err != nil {
		t.Fatal(err)
	}
}

func TestGatewayDenialSurvivesBackendAndStatusFailures(t *testing.T) {
	for _, providerFails := range []bool{false, true} {
		for _, statusFails := range []bool{false, true} {
			c, gateway, route, grant := referenceWithdrawalFixture(t)
			addWithdrawalPeer(t, c, gateway, route)
			if err := c.Delete(context.Background(), grant); err != nil {
				t.Fatal(err)
			}
			gateway.Status.Addresses = []gatewayv1.GatewayStatusAddress{{Value: "previous.azurefd.net"}}
			gateway.Status.Conditions = []metav1.Condition{{Type: string(gatewayv1.GatewayConditionProgrammed), Status: metav1.ConditionTrue}}
			gateway.Status.Listeners = []gatewayv1.ListenerStatus{{Name: gateway.Spec.Listeners[0].Name, AttachedRoutes: 7}}
			if err := c.Status().Update(context.Background(), gateway); err != nil {
				t.Fatal(err)
			}
			readErr, providerErr, statusErr := errors.New("peer backend unavailable"), errors.New("suspension unavailable"), errors.New("status unavailable")
			patches := 0
			reader := interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
				Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, object client.Object, opts ...client.GetOption) error {
					if _, backend := object.(*fleetnetv1alpha1.ServiceImport); backend {
						return readErr
					}
					return cl.Get(ctx, key, object, opts...)
				},
				SubResourcePatch: func(ctx context.Context, cl client.Client, resource string, object client.Object,
					patch client.Patch, opts ...client.SubResourcePatchOption) error {
					patches++
					current := object.(*gatewayv1.Gateway)
					pending := meta.FindStatusCondition(current.Status.Conditions, "Programmed")
					if pending == nil || pending.Status != metav1.ConditionFalse || len(current.Status.Addresses) != 0 ||
						current.Status.Listeners[0].AttachedRoutes != 7 || !strings.Contains(pending.Message, referenceSuspensionMessage) {
						t.Fatalf("backend failure blocked truthful withdrawal status: %#v", current.Status)
					}
					if statusFails {
						return statusErr
					}
					return cl.SubResource(resource).Patch(ctx, object, patch, opts...)
				},
			})
			stager := &fakeGraphStager{suspension: frontdoor.EndpointSuspensionResult{State: frontdoor.EndpointSuspended}}
			if providerFails {
				stager.err = providerErr
			}
			r := &Reconciler{Client: reader, APIReader: ownershipReader{Reader: reader, t: t}, ProfileCleanup: &fakeProfileCleaner{}, GraphStager: stager}
			_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)})
			if !errors.Is(err, readErr) || errors.Is(err, providerErr) != providerFails || errors.Is(err, statusErr) != statusFails ||
				stager.suspensions != 1 || stager.calls != 0 || patches != 1 {
				t.Fatalf("withdrawal/error preservation failed: %v, provider=%#v, patches=%d", err, stager, patches)
			}
			if !statusFails {
				if err := c.Get(context.Background(), client.ObjectKeyFromObject(gateway), gateway); err != nil {
					t.Fatal(err)
				}
				pending := meta.FindStatusCondition(gateway.Status.Conditions, "Programmed")
				if pending == nil || pending.Status != metav1.ConditionFalse || len(gateway.Status.Addresses) != 0 {
					t.Fatalf("withdrawal status was not persisted: %#v", gateway.Status)
				}
			}
		}
	}
}

func TestGatewayDenialSurvivesOtherGrantReadFailure(t *testing.T) {
	c, gateway, route, grant := referenceWithdrawalFixture(t)
	if err := c.Delete(context.Background(), grant); err != nil {
		t.Fatal(err)
	}
	unknown := *route.Spec.Rules[0].DeepCopy()
	unknown.BackendRefs[0].Namespace = ptr.To(gatewayv1.Namespace("unavailable-grants"))
	route.Spec.Rules = append([]gatewayv1.HTTPRouteRule{unknown}, route.Spec.Rules...)
	if err := c.Update(context.Background(), route); err != nil {
		t.Fatal(err)
	}
	readErr := errors.New("other ReferenceGrants unavailable")
	reader := interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
		List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			options := &client.ListOptions{}
			options.ApplyOptions(opts)
			if _, grants := list.(*gatewayv1beta1.ReferenceGrantList); grants && options.Namespace == string(*unknown.BackendRefs[0].Namespace) {
				return readErr
			}
			return cl.List(ctx, list, opts...)
		},
	})
	stager := &fakeGraphStager{suspension: frontdoor.EndpointSuspensionResult{State: frontdoor.EndpointSuspended}}
	r := &Reconciler{Client: reader, APIReader: ownershipReader{Reader: reader, t: t}, ProfileCleanup: &fakeProfileCleaner{}, GraphStager: stager}
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)})
	if !errors.Is(err, readErr) || stager.suspensions != 1 || stager.calls != 0 {
		t.Fatalf("other grant failure blocked positive denial: %v, provider=%#v", err, stager)
	}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(gateway), gateway); err != nil {
		t.Fatal(err)
	}
	pending := meta.FindStatusCondition(gateway.Status.Conditions, "Programmed")
	if pending == nil || pending.Status != metav1.ConditionFalse || !strings.Contains(pending.Message, string(stager.suspension.State)) ||
		!strings.Contains(pending.Message, "other authorization reads failed") {
		t.Fatalf("positive denial/provider result or uncertainty was lost: %#v", gateway.Status)
	}
}

func TestGatewayDenialRevalidatesOwnershipAndRoute(t *testing.T) {
	tests := []struct {
		name   string
		change func(client.Object)
	}{
		{name: "replacement Gateway", change: func(object client.Object) {
			if g, ok := object.(*gatewayv1.Gateway); ok {
				g.UID = "replacement-withdrawal-gateway"
			}
		}},
		{name: "changed generation", change: func(object client.Object) {
			if g, ok := object.(*gatewayv1.Gateway); ok {
				g.Generation++
			}
		}},
		{name: "cleanup ownership removed", change: func(object client.Object) {
			if g, ok := object.(*gatewayv1.Gateway); ok {
				g.Finalizers = nil
			}
		}},
		{name: "Gateway deletion in progress", change: func(object client.Object) {
			if g, ok := object.(*gatewayv1.Gateway); ok {
				now := metav1.Now()
				g.DeletionTimestamp = &now
			}
		}},
		{name: "class ownership changed", change: func(object client.Object) {
			if class, ok := object.(*gatewayv1.GatewayClass); ok {
				class.Spec.ControllerName = "foreign.example/gateway"
			}
		}},
		{name: "class no longer accepted", change: func(object client.Object) {
			if class, ok := object.(*gatewayv1.GatewayClass); ok {
				class.Status.Conditions = nil
			}
		}},
		{name: "replacement route", change: func(object client.Object) {
			if route, ok := object.(*gatewayv1.HTTPRoute); ok {
				route.UID = "replacement-withdrawal-route"
			}
		}},
		{name: "route specification changed", change: func(object client.Object) {
			if route, ok := object.(*gatewayv1.HTTPRoute); ok {
				route.Spec.Rules[0].BackendRefs[0].Name = "changed-backend"
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, gateway, route, grant := referenceWithdrawalFixture(t)
			addWithdrawalPeer(t, c, gateway, route)
			if err := c.Delete(context.Background(), grant); err != nil {
				t.Fatal(err)
			}
			stager := &fakeGraphStager{}
			r := &Reconciler{Client: c, APIReader: ownershipReader{Reader: c, t: t, afterGet: tt.change},
				ProfileCleanup: &fakeProfileCleaner{}, GraphStager: stager}
			before := graphMetricSamples(t, "", "")
			_, _ = r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)})
			assertGraphMetricDelta(t, before, graphMetricSamples(t, "", ""), 0)
			if stager.suspensions != 0 || stager.calls != 0 {
				t.Fatalf("unconfirmed current denial mutated Azure: %#v", stager)
			}
		})
	}
}

func TestGatewayDenialIgnoresIneligibleRoutes(t *testing.T) {
	tests := []struct {
		name   string
		change func(*gatewayv1.Gateway, *gatewayv1.HTTPRoute)
	}{
		{name: "unrelated parent", change: func(_ *gatewayv1.Gateway, route *gatewayv1.HTTPRoute) {
			route.Spec.ParentRefs = []gatewayv1.ParentReference{{Name: "unrelated-gateway"}}
		}},
		{name: "unsupported filter", change: func(_ *gatewayv1.Gateway, route *gatewayv1.HTTPRoute) {
			route.Spec.Rules[0].Filters = []gatewayv1.HTTPRouteFilter{{Type: gatewayv1.HTTPRouteFilterRequestRedirect}}
		}},
		{name: "namespace not allowed", change: func(gateway *gatewayv1.Gateway, _ *gatewayv1.HTTPRoute) {
			gateway.Spec.Listeners[0].AllowedRoutes = nil
		}},
		{name: "unsupported backend kind", change: func(_ *gatewayv1.Gateway, route *gatewayv1.HTTPRoute) {
			route.Spec.Rules[0].BackendRefs[0].Kind = ptr.To(gatewayv1.Kind("Service"))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, gateway, route, grant := referenceWithdrawalFixture(t)
			addWithdrawalPeer(t, c, gateway, route)
			if err := c.Delete(context.Background(), grant); err != nil {
				t.Fatal(err)
			}
			tt.change(gateway, route)
			for _, object := range []client.Object{gateway, route} {
				if err := c.Update(context.Background(), object); err != nil {
					t.Fatal(err)
				}
			}
			stager := &fakeGraphStager{}
			r := &Reconciler{Client: c, APIReader: c, ProfileCleanup: &fakeProfileCleaner{}, GraphStager: stager}
			_, _ = r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)})
			if stager.suspensions != 0 {
				t.Fatalf("ineligible route authorized suspension: %#v", stager)
			}
		})
	}
}

func TestGatewayDenialStatusConflictDoesNotReplaySuspension(t *testing.T) {
	c, gateway, route, grant := referenceWithdrawalFixture(t)
	addWithdrawalPeer(t, c, gateway, route)
	if err := c.Delete(context.Background(), grant); err != nil {
		t.Fatal(err)
	}
	patches := 0
	writer := interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
		SubResourcePatch: func(ctx context.Context, cl client.Client, resource string, object client.Object,
			patch client.Patch, opts ...client.SubResourcePatchOption) error {
			patches++
			if patches == 1 {
				return apierrors.NewConflict(schema.GroupResource{Resource: ownershipGatewayResource}, object.GetName(), errors.New("withdrawal conflict"))
			}
			return cl.SubResource(resource).Patch(ctx, object, patch, opts...)
		},
	})
	stager := &fakeGraphStager{suspension: frontdoor.EndpointSuspensionResult{State: frontdoor.EndpointSuspended}}
	r := &Reconciler{Client: writer, APIReader: ownershipReader{Reader: c, t: t}, ProfileCleanup: &fakeProfileCleaner{}, GraphStager: stager}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}); err != nil {
		t.Fatal(err)
	}
	if stager.suspensions != 1 || stager.calls != 0 || patches != 2 {
		t.Fatalf("status conflict replayed suspension: %#v, patches=%d", stager, patches)
	}
}

func TestGatewayFreshDenialOverridesCachedPermission(t *testing.T) {
	c, gateway, route, grant := referenceWithdrawalFixture(t)
	addWithdrawalPeer(t, c, gateway, route)
	if err := c.Delete(context.Background(), grant); err != nil {
		t.Fatal(err)
	}
	cache := interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
		List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if grants, ok := list.(*gatewayv1beta1.ReferenceGrantList); ok {
				grants.Items = []gatewayv1beta1.ReferenceGrant{*grant.DeepCopy()}
				return nil
			}
			return cl.List(ctx, list, opts...)
		},
	})
	stager := &fakeGraphStager{suspension: frontdoor.EndpointSuspensionResult{State: frontdoor.EndpointSuspended}}
	r := &Reconciler{Client: cache, APIReader: ownershipReader{Reader: c, t: t}, ProfileCleanup: &fakeProfileCleaner{}, GraphStager: stager}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}); err != nil {
		t.Fatal(err)
	}
	if stager.suspensions != 1 || stager.calls != 0 {
		t.Fatalf("cached permission overrode current denial: %#v", stager)
	}
}

func TestGatewayDenialRestoredBeforeOperationPreventsSuspension(t *testing.T) {
	c, gateway, route, grant := referenceWithdrawalFixture(t)
	addWithdrawalPeer(t, c, gateway, route)
	if err := c.Delete(context.Background(), grant); err != nil {
		t.Fatal(err)
	}
	stager := &fakeGraphStager{}
	restored := false
	reader := ownershipReader{Reader: c, t: t, afterGet: func(object client.Object) {
		if _, ok := object.(*gatewayv1.HTTPRoute); ok && !restored {
			restored = true
			grant.ResourceVersion = ""
			if err := c.Create(context.Background(), grant); err != nil {
				t.Fatal(err)
			}
		}
	}}
	r := &Reconciler{Client: c, APIReader: reader, ProfileCleanup: &fakeProfileCleaner{}, GraphStager: stager}
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)})
	if !errors.Is(err, errProvisioningIntentChanged) || !restored || stager.suspensions != 0 || stager.calls != 0 {
		t.Fatalf("permission was not rechecked immediately before suspension: %v, provider=%#v", err, stager)
	}
}

func TestGatewayMixedDenialRechecksStatusEvidence(t *testing.T) {
	tests := []struct {
		name        string
		changeRoute bool
		readFails   bool
	}{
		{name: "route changed during suspension", changeRoute: true},
		{name: "permission restored during suspension"},
		{name: "status proof read fails", readFails: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, gateway, route, grant := referenceWithdrawalFixture(t)
			addWithdrawalPeer(t, c, gateway, route)
			if err := c.Delete(context.Background(), grant); err != nil {
				t.Fatal(err)
			}
			afterOperation := false
			proofErr := errors.New("current grant evidence unavailable")
			reader := interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
				List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					if _, grants := list.(*gatewayv1beta1.ReferenceGrantList); grants && afterOperation && tt.readFails {
						return proofErr
					}
					return cl.List(ctx, list, opts...)
				},
			})
			stager := &fakeGraphStager{suspension: frontdoor.EndpointSuspensionResult{State: frontdoor.EndpointSuspended}, during: func() {
				afterOperation = true
				switch {
				case tt.changeRoute:
					route.Spec.Rules[0].BackendRefs[0].Name = "changed-backend"
					if err := c.Update(context.Background(), route); err != nil {
						t.Fatal(err)
					}
				case !tt.readFails:
					grant.ResourceVersion = ""
					if err := c.Create(context.Background(), grant); err != nil {
						t.Fatal(err)
					}
				}
			}}
			r := &Reconciler{Client: c, APIReader: ownershipReader{Reader: reader, t: t}, ProfileCleanup: &fakeProfileCleaner{}, GraphStager: stager}
			_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)})
			if errors.Is(err, proofErr) != tt.readFails || (err != nil && !tt.readFails) {
				t.Fatalf("current evidence failure was lost: %v", err)
			}
			if err := c.Get(context.Background(), client.ObjectKeyFromObject(gateway), gateway); err != nil {
				t.Fatal(err)
			}
			pending := meta.FindStatusCondition(gateway.Status.Conditions, "Programmed")
			if stager.suspensions != 1 || stager.calls != 0 || pending == nil || pending.Status != metav1.ConditionFalse ||
				strings.Contains(pending.Message, referenceSuspensionMessage) || len(gateway.Status.Addresses) != 0 {
				t.Fatalf("changed/unknown policy retained stale denial diagnostics: %#v, provider=%#v", gateway.Status, stager)
			}
		})
	}
}

func TestGatewayDenialRequiresCurrentGrantEvidence(t *testing.T) {
	for _, lookupFails := range []bool{false, true} {
		c, gateway, _, _ := referenceWithdrawalFixture(t)
		cache := interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
			List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if grants, ok := list.(*gatewayv1beta1.ReferenceGrantList); ok {
					grants.Items = nil
					return nil
				}
				return cl.List(ctx, list, opts...)
			},
		})
		stager := &fakeGraphStager{}
		r := &Reconciler{Client: cache, APIReader: c, ProfileCleanup: &fakeProfileCleaner{}, GraphStager: stager}
		if lookupFails {
			r.APIReader = interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
				List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					if _, grants := list.(*gatewayv1beta1.ReferenceGrantList); grants {
						return apierrors.NewForbidden(schema.GroupResource{Resource: referenceGrantResource}, gateway.Namespace, errors.New("grant reads unavailable"))
					}
					return cl.List(ctx, list, opts...)
				},
			})
		}
		_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)})
		if err == nil || stager.calls != 0 || stager.suspensions != 0 ||
			(lookupFails && !apierrors.IsForbidden(err)) || (!lookupFails && !errors.Is(err, errProvisioningIntentChanged)) {
			t.Fatalf("stale denial/read failure caused withdrawal: %v, provider=%#v", err, stager)
		}
	}
}

func TestGatewayRestoredGrantInvalidatesSuspensionStatus(t *testing.T) {
	c, gateway, _, grant := referenceWithdrawalFixture(t)
	if err := c.Delete(context.Background(), grant); err != nil {
		t.Fatal(err)
	}
	stager := &fakeGraphStager{suspension: frontdoor.EndpointSuspensionResult{State: frontdoor.EndpointSuspended}, during: func() {
		grant.ResourceVersion = ""
		if err := c.Create(context.Background(), grant); err != nil {
			t.Fatal(err)
		}
	}}
	r := &Reconciler{Client: c, APIReader: c, ProfileCleanup: &fakeProfileCleaner{}, GraphStager: stager}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(gateway), gateway); err != nil {
		t.Fatal(err)
	}
	pending := meta.FindStatusCondition(gateway.Status.Conditions, "Programmed")
	if stager.suspensions != 1 || stager.calls != 0 || pending == nil || pending.Status != metav1.ConditionFalse ||
		strings.Contains(pending.Message, referenceSuspensionMessage) || len(gateway.Status.Addresses) != 0 {
		t.Fatalf("restored permission retained stale suspension status: %#v, provider=%#v", gateway.Status, stager)
	}
}

func TestGatewayAllDeniedReferencesDoNotAcquireOwnership(t *testing.T) {
	c, gateway, _, grant := referenceWithdrawalFixture(t)
	gateway.Finalizers = nil
	if err := c.Update(context.Background(), gateway); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(context.Background(), grant); err != nil {
		t.Fatal(err)
	}
	stager := &fakeGraphStager{}
	r := &Reconciler{Client: c, GraphStager: stager}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(gateway), gateway); err != nil {
		t.Fatal(err)
	}
	if stager.calls != 0 || stager.suspensions != 0 || len(gateway.Finalizers) != 0 {
		t.Fatalf("denied empty intent acquired ownership: %#v, provider=%#v", gateway, stager)
	}
}

func TestGatewayGrantLookupFailureIsNotDenial(t *testing.T) {
	c, gateway, _, _ := referenceWithdrawalFixture(t)
	reader := interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
		List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, grants := list.(*gatewayv1beta1.ReferenceGrantList); grants {
				return apierrors.NewForbidden(schema.GroupResource{Resource: referenceGrantResource}, gateway.Namespace, errors.New("cached grant lookup failed"))
			}
			return cl.List(ctx, list, opts...)
		},
	})
	stager := &fakeGraphStager{}
	r := &Reconciler{Client: reader, APIReader: c, ProfileCleanup: &fakeProfileCleaner{}, GraphStager: stager}
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)})
	if !apierrors.IsForbidden(err) || stager.suspensions != 0 || stager.calls != 0 {
		t.Fatalf("grant lookup failure was treated as denial: %v, provider=%#v", err, stager)
	}
}
