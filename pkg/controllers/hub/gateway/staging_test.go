/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gateway

import (
	"context"
	"errors"
	"strconv"
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

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
	"go.goms.io/fleet-networking/pkg/providers/azure/frontdoor"
)

type fakeGraphStager struct {
	calls       int
	owner       frontdoor.GatewayOwner
	model       gatewaymodel.GlobalGateway
	result      frontdoor.DisabledGraphStagingResult
	err         error
	during      func()
	suspensions int
	suspension  frontdoor.EndpointSuspensionResult
}

func (f *fakeGraphStager) StageDisabledGraph(_ context.Context, owner frontdoor.GatewayOwner,
	model gatewaymodel.GlobalGateway) (frontdoor.DisabledGraphStagingResult, error) {
	f.calls++
	f.owner, f.model = owner, model
	if f.during != nil {
		f.during()
	}
	return f.result, f.err
}

func (f *fakeGraphStager) SuspendEndpoint(_ context.Context, owner frontdoor.GatewayOwner) (frontdoor.EndpointSuspensionResult, error) {
	f.suspensions++
	f.owner = owner
	if f.during != nil {
		f.during()
	}
	return f.suspension, f.err
}

func TestGatewayStagesOnlyAfterObservedOwnership(t *testing.T) {
	for _, state := range []frontdoor.RootProvisioningState{frontdoor.ProfileProvisioningPending, frontdoor.DisabledRootsObserved} {
		t.Run(string(state), func(t *testing.T) {
			c, gateway, _, _ := gatewayFixture(t, 1)
			addObservationHub(t, c)
			conflict := &cleanupConflictClient{Client: c}
			stager := &fakeGraphStager{result: frontdoor.DisabledGraphStagingResult{
				Roots:             frontdoor.RootProvisioningResult{State: state, EndpointHostName: "staged.azurefd.net"},
				Route:             frontdoor.RouteProvisioningResult{State: frontdoor.DisabledRouteObserved},
				InventoryVerified: state == frontdoor.DisabledRootsObserved,
			}}
			stager.during = func() {
				var observed gatewayv1.Gateway
				if err := c.Get(context.Background(), client.ObjectKeyFromObject(gateway), &observed); err != nil {
					t.Fatal(err)
				}
				if !controllerutil.ContainsFinalizer(&observed, objectmeta.GatewayAFDCleanupFinalizer) {
					t.Fatal("staging began without persisted cleanup ownership")
				}
			}
			cleaner := &fakeProfileCleaner{}
			r := &Reconciler{Client: conflict, APIReader: ownershipReader{Reader: c, t: t}, ProfileCleanup: cleaner, GraphStager: stager}
			request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}
			for i := 0; i < 2; i++ {
				result, err := r.Reconcile(context.Background(), request)
				if err != nil || result.RequeueAfter < 30*time.Second || result.RequeueAfter > 36*time.Second {
					t.Fatalf("staging did not schedule bounded observation: %#v, %v", result, err)
				}
			}
			if err := c.Get(context.Background(), request.NamespacedName, gateway); err != nil {
				t.Fatal(err)
			}
			pending := meta.FindStatusCondition(gateway.Status.Conditions, "Programmed")
			if pending == nil || pending.Status != metav1.ConditionFalse || !strings.Contains(pending.Message, "disabled graph staging") ||
				!strings.Contains(pending.Message, "inventoryVerified="+strconv.FormatBool(stager.result.InventoryVerified)) ||
				len(gateway.Status.Addresses) != 0 || stager.calls != 2 || conflict.patches != 2 || len(cleaner.owners) != 0 ||
				stager.owner.HubUID != ownershipHubUID || stager.owner.UID != string(gateway.UID) || len(stager.model.Routes) != 1 ||
				!controllerutil.ContainsFinalizer(gateway, "concurrent.example/cleanup") {
				t.Fatalf("staging lost ownership or overstated readiness: %#v, stager=%#v, patches=%d", gateway, stager, conflict.patches)
			}
		})
	}
}

func TestGatewayStaleOriginCleanupRemainsUnprogrammed(t *testing.T) {
	c, gateway, _, _ := gatewayFixture(t, 1)
	addObservationHub(t, c)
	stager := &fakeGraphStager{result: frontdoor.DisabledGraphStagingResult{
		Roots: frontdoor.RootProvisioningResult{State: frontdoor.DisabledRootsObserved},
		Route: frontdoor.RouteProvisioningResult{State: frontdoor.DisabledRouteObserved,
			Backends: frontdoor.BackendProvisioningResult{State: frontdoor.PrivateBackendsConverged}},
		StaleOriginsPending: 2,
	}}
	r := &Reconciler{Client: c, APIReader: c, ProfileCleanup: &fakeProfileCleaner{}, GraphStager: stager}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(gateway), gateway); err != nil {
		t.Fatal(err)
	}
	pending := meta.FindStatusCondition(gateway.Status.Conditions, string(gatewayv1.GatewayConditionProgrammed))
	if pending == nil || pending.Status != metav1.ConditionFalse || !strings.Contains(pending.Message, "staleOriginsPending=2") ||
		!strings.Contains(pending.Message, "inventoryVerified=false") || len(gateway.Status.Addresses) != 0 || stager.calls != 1 {
		t.Fatalf("stale-origin cleanup overstated readiness: %#v, calls=%d", gateway.Status, stager.calls)
	}
}

func TestGatewayStagingStatusConflictDoesNotReplayWrites(t *testing.T) {
	c, gateway, _, _ := gatewayFixture(t, 1)
	addObservationHub(t, c)
	patches := 0
	writer := interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
		SubResourcePatch: func(ctx context.Context, cl client.Client, subresource string, object client.Object,
			patch client.Patch, opts ...client.SubResourcePatchOption) error {
			patches++
			if patches == 1 {
				return apierrors.NewConflict(schema.GroupResource{Group: gatewayv1.GroupName, Resource: ownershipGatewayResource},
					object.GetName(), errors.New("status conflict"))
			}
			return cl.SubResource(subresource).Patch(ctx, object, patch, opts...)
		},
	})
	stager := &fakeGraphStager{result: frontdoor.DisabledGraphStagingResult{
		Roots: frontdoor.RootProvisioningResult{State: frontdoor.DisabledRootsObserved},
		Route: frontdoor.RouteProvisioningResult{State: frontdoor.RouteBackendsPending,
			Backends: frontdoor.BackendProvisioningResult{State: frontdoor.BackendOriginsPending,
				Origins: frontdoor.PrivateOriginsObservation{PendingApprovals: []frontdoor.PrivateLinkPlan{{}}}}},
	}}
	r := &Reconciler{Client: writer, APIReader: c, ProfileCleanup: &fakeProfileCleaner{}, GraphStager: stager}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(gateway), gateway); err != nil {
		t.Fatal(err)
	}
	pending := meta.FindStatusCondition(gateway.Status.Conditions, "Programmed")
	if stager.calls != 1 || patches != 2 || pending == nil ||
		!strings.Contains(pending.Message, "approval is asynchronous") || !strings.Contains(pending.Message, "PLS owner") {
		t.Fatalf("status retry repeated staging or lost manual approval guidance: calls=%d patches=%d status=%#v", stager.calls, patches, gateway.Status)
	}
}

func TestGatewayStagingGuardFailuresAndSkips(t *testing.T) {
	const (
		emptyGatewayCase = "empty Gateway"
		unsupportedCase  = "unsupported intent"
		foreignClassCase = "foreign class"
	)
	tests := []struct {
		name      string
		wantError bool
	}{
		{name: "nil injection"},
		{name: emptyGatewayCase},
		{name: unsupportedCase},
		{name: foreignClassCase},
		{name: "deleting Gateway"},
		{name: "missing reader", wantError: true},
		{name: "missing cleaner", wantError: true},
		{name: "metadata patch fails", wantError: true},
		{name: "intent changes before ownership", wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			c, gateway, route, _ := gatewayFixture(t, 1)
			addObservationHub(t, c)
			stager := &fakeGraphStager{}
			r := &Reconciler{Client: c, APIReader: c, ProfileCleanup: &fakeProfileCleaner{}, GraphStager: stager}
			switch tt.name {
			case "nil injection":
				r.GraphStager, r.APIReader, r.ProfileCleanup = nil, nil, nil
			case emptyGatewayCase:
				if err := c.Delete(ctx, route); err != nil {
					t.Fatal(err)
				}
			case unsupportedCase:
				hostname := gatewayv1.Hostname("restricted.example.net")
				gateway.Spec.Listeners[0].Hostname = &hostname
				if err := c.Update(ctx, gateway); err != nil {
					t.Fatal(err)
				}
			case foreignClassCase:
				var class gatewayv1.GatewayClass
				if err := c.Get(ctx, client.ObjectKey{Name: string(gateway.Spec.GatewayClassName)}, &class); err != nil {
					t.Fatal(err)
				}
				class.Spec.ControllerName = "other.example/staging-controller"
				if err := c.Update(ctx, &class); err != nil {
					t.Fatal(err)
				}

			case "deleting Gateway":
				gateway.Finalizers = []string{ownershipForeignFinalizer}
				if err := c.Update(ctx, gateway); err != nil {
					t.Fatal(err)
				}
				if err := c.Delete(ctx, gateway); err != nil {
					t.Fatal(err)
				}
			case "missing reader":
				r.APIReader = nil
			case "missing cleaner":
				r.ProfileCleanup = nil
			case "metadata patch fails":
				r.Client = interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
					Patch: func(context.Context, client.WithWatch, client.Object, client.Patch, ...client.PatchOption) error {
						return apierrors.NewForbidden(schema.GroupResource{Resource: ownershipGatewayResource}, gateway.Name, errors.New("denied"))
					},
				})
			case "intent changes before ownership":
				r.APIReader = ownershipReader{Reader: c, t: t, afterGet: func(object client.Object) {
					if namespace, ok := object.(*corev1.Namespace); ok && namespace.Name == metav1.NamespaceSystem {
						route.Spec.ParentRefs = nil
						if err := c.Update(ctx, route); err != nil {
							t.Fatal(err)
						}
					}
				}}
			}
			before := graphMetricSamples(t, "", "")
			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)})
			assertGraphMetricDelta(t, before, graphMetricSamples(t, "", ""), 0)
			if (err != nil) != tt.wantError || stager.calls != 0 {
				t.Fatalf("invalid ownership reached staging: calls=%d err=%v; want error=%v", stager.calls, err, tt.wantError)
			}
			if err := c.Get(ctx, client.ObjectKeyFromObject(gateway), gateway); err != nil {
				t.Fatal(err)
			}
			if controllerutil.ContainsFinalizer(gateway, objectmeta.GatewayAFDCleanupFinalizer) {
				t.Fatal("skipped/failed preparation acquired ownership")
			}
		})
	}
}

func TestGatewayStagingFailureAndChangedIntent(t *testing.T) {
	for _, changeIntent := range []bool{false, true} {
		c, gateway, route, _ := gatewayFixture(t, 1)
		addObservationHub(t, c)
		failure := errors.New("staging ARM request failed")
		stager := &fakeGraphStager{err: failure}
		if changeIntent {
			stager.err = nil
			stager.result.Roots.State = frontdoor.ProfileProvisioningPending
			stager.during = func() {
				route.Spec.ParentRefs = nil
				if err := c.Update(context.Background(), route); err != nil {
					t.Fatal(err)
				}
			}
		}
		r := &Reconciler{Client: c, APIReader: c, ProfileCleanup: &fakeProfileCleaner{}, GraphStager: stager}
		_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)})
		if (!changeIntent && !errors.Is(err, failure)) || (changeIntent && err != nil) || stager.calls != 1 {
			t.Fatalf("staging failure/changed intent mishandled: %v, calls=%d", err, stager.calls)
		}
		if err := c.Get(context.Background(), client.ObjectKeyFromObject(gateway), gateway); err != nil {
			t.Fatal(err)
		}
		pending := meta.FindStatusCondition(gateway.Status.Conditions, "Programmed")
		if pending == nil || pending.Status != metav1.ConditionFalse || len(gateway.Status.Addresses) != 0 ||
			!controllerutil.ContainsFinalizer(gateway, objectmeta.GatewayAFDCleanupFinalizer) {
			t.Fatalf("staging failure lost cleanup ownership or claimed readiness: %#v", gateway)
		}
		if changeIntent {
			if strings.Contains(pending.Message, "disabled graph staging") {
				t.Fatalf("stale staging result applied to withdrawn intent: %q", pending.Message)
			}
		} else if !strings.Contains(pending.Message, failure.Error()) {
			t.Fatalf("staging failure diagnostic lost: %q", pending.Message)
		}
	}
}

func TestGatewayStagingResultRequiresCurrentIntent(t *testing.T) {
	const generationCase = "Gateway generation"
	for _, change := range []string{generationCase, "private backend", "class acceptance"} {
		t.Run(change, func(t *testing.T) {
			c, gateway, route, _ := gatewayFixture(t, 1)
			addObservationHub(t, c)
			stager := &fakeGraphStager{result: frontdoor.DisabledGraphStagingResult{
				Roots: frontdoor.RootProvisioningResult{State: frontdoor.ProfileProvisioningPending},
			}}
			stager.during = func() {
				ctx := context.Background()
				switch change {
				case generationCase:
					if err := c.Get(ctx, client.ObjectKeyFromObject(gateway), gateway); err != nil {
						t.Fatal(err)
					}
					gateway.Generation++
					if err := c.Update(ctx, gateway); err != nil {
						t.Fatal(err)
					}
				case "private backend":
					var service fleetnetv1alpha1.InternalServiceExport
					key := client.ObjectKey{Namespace: ownershipExportNamespace,
						Name: route.Namespace + "-" + string(route.Spec.Rules[0].BackendRefs[0].Name)}
					if err := c.Get(ctx, key, &service); err != nil {
						t.Fatal(err)
					}
					service.Spec.PrivateLinkOrigin.InternalIP = "10.0.0.98"
					if err := c.Update(ctx, &service); err != nil {
						t.Fatal(err)
					}
				case "class acceptance":
					var class gatewayv1.GatewayClass
					if err := c.Get(ctx, client.ObjectKey{Name: string(gateway.Spec.GatewayClassName)}, &class); err != nil {
						t.Fatal(err)
					}
					class.Status.Conditions[0].Status = metav1.ConditionFalse
					if err := c.Status().Update(ctx, &class); err != nil {
						t.Fatal(err)
					}
				}
			}
			r := &Reconciler{Client: c, APIReader: c, ProfileCleanup: &fakeProfileCleaner{}, GraphStager: stager}
			if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}); err != nil {
				t.Fatal(err)
			}
			if err := c.Get(context.Background(), client.ObjectKeyFromObject(gateway), gateway); err != nil {
				t.Fatal(err)
			}
			pending := meta.FindStatusCondition(gateway.Status.Conditions, "Programmed")
			if stager.calls != 1 || pending == nil || pending.Status != metav1.ConditionFalse ||
				strings.Contains(pending.Message, "disabled graph staging") || len(gateway.Status.Addresses) != 0 {
				t.Fatalf("stale staging result applied to changed intent: %#v, calls=%d", gateway.Status, stager.calls)
			}
		})
	}
}

func TestGatewayStagingDeletionAndCombinedFailures(t *testing.T) {
	for _, deleting := range []bool{false, true} {
		c, gateway, _, _ := gatewayFixture(t, 1)
		addObservationHub(t, c)
		stageFailure, observationFailure := errors.New("stage unavailable"), errors.New("observation unavailable")
		stager := &fakeGraphStager{err: stageFailure}
		observer := &fakeOriginObserver{err: observationFailure}
		cleaner := &fakeProfileCleaner{}
		if deleting {
			gateway.Finalizers = []string{objectmeta.GatewayAFDCleanupFinalizer}
			if err := c.Update(context.Background(), gateway); err != nil {
				t.Fatal(err)
			}
			if err := c.Delete(context.Background(), gateway); err != nil {
				t.Fatal(err)
			}
		}
		r := &Reconciler{Client: c, APIReader: c, ProfileCleanup: cleaner, OriginObserver: observer, GraphStager: stager}
		_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)})
		if deleting {
			if err != nil || stager.calls != 0 || observer.calls != 0 || len(cleaner.owners) != 1 {
				t.Fatalf("staging/observation ran instead of cleanup: %v, stage=%d observer=%d cleanup=%d",
					err, stager.calls, observer.calls, len(cleaner.owners))
			}
		} else if !errors.Is(err, stageFailure) || !errors.Is(err, observationFailure) || stager.calls != 1 || observer.calls != 1 {
			t.Fatalf("simultaneous external failures were lost: %v, stage=%d observer=%d", err, stager.calls, observer.calls)
		}
	}
}
