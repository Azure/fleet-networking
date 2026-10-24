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
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
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

const httpPreviewHostname = "observed.azurefd.net"

type fakeHTTPProvisioner struct {
	fakeGraphStager
	progress frontdoor.HTTPPreviewProvisioningResult
}

func (f *fakeHTTPProvisioner) ReconcileHTTPPreview(_ context.Context, owner frontdoor.GatewayOwner,
	model gatewaymodel.GlobalGateway) (frontdoor.HTTPPreviewProvisioningResult, error) {
	f.calls++
	f.owner, f.model = owner, model
	if f.during != nil {
		f.during()
	}
	return f.progress, f.err
}

func httpControllerFixture(t *testing.T) (client.Client, *gatewayv1.Gateway, *gatewayv1.HTTPRoute, *Reconciler, *fakeHTTPProvisioner) {
	t.Helper()
	c, gateway, route, _ := gatewayFixture(t, 1)
	addObservationHub(t, c)
	provider := &fakeHTTPProvisioner{progress: frontdoor.HTTPPreviewProvisioningResult{
		State: frontdoor.HTTPPreviewProgrammed, EndpointHostName: httpPreviewHostname,
	}}
	provider.suspension.State = frontdoor.EndpointSuspended
	r := &Reconciler{Client: c, APIReader: ownershipReader{Reader: c, t: t},
		ProfileCleanup: &fakeProfileCleaner{}, HTTPProvisioner: provider}
	return c, gateway, route, r, provider
}

func assertHTTPGatewayReadiness(t *testing.T, c client.Client, gateway *gatewayv1.Gateway, ready bool) {
	t.Helper()
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(gateway), gateway); err != nil {
		t.Fatal(err)
	}
	status, reason := metav1.ConditionFalse, string(gatewayv1.GatewayReasonPending)
	if ready {
		status, reason = metav1.ConditionTrue, string(gatewayv1.GatewayReasonProgrammed)
	}
	conditions := make([][]metav1.Condition, 0, 1+len(gateway.Status.Listeners))
	conditions = append(conditions, gateway.Status.Conditions)
	for _, listener := range gateway.Status.Listeners {
		conditions = append(conditions, listener.Conditions)
	}
	for _, list := range conditions {
		condition := meta.FindStatusCondition(list, string(gatewayv1.GatewayConditionProgrammed))
		if condition == nil || condition.Status != status || condition.Reason != reason ||
			condition.ObservedGeneration != gateway.Generation {
			t.Fatalf("incorrect current HTTP readiness: %#v", gateway.Status)
		}
	}
	if !ready {
		if len(gateway.Status.Addresses) != 0 {
			t.Fatalf("unverified HTTP graph published an address: %#v", gateway.Status)
		}
		return
	}
	if len(gateway.Status.Addresses) != 1 || gateway.Status.Addresses[0].Type == nil ||
		*gateway.Status.Addresses[0].Type != gatewayv1.HostnameAddressType || gateway.Status.Addresses[0].Value != httpPreviewHostname {
		t.Fatalf("ready graph did not publish exactly the observed hostname: %#v", gateway.Status)
	}
}

func TestHTTPControllerLifecycleStates(t *testing.T) {
	for _, state := range []frontdoor.HTTPPreviewProvisioningState{
		frontdoor.HTTPPreviewStaging, frontdoor.HTTPPreviewPending, frontdoor.HTTPPreviewRouteEnabling,
		frontdoor.HTTPPreviewEndpointEnabling, frontdoor.HTTPPreviewSuspending, frontdoor.HTTPPreviewProgrammed,
	} {
		t.Run(string(state), func(t *testing.T) {
			c, gateway, _, r, provider := httpControllerFixture(t)
			provider.progress.State = state
			provider.during = func() {
				var owned gatewayv1.Gateway
				if err := c.Get(context.Background(), client.ObjectKeyFromObject(gateway), &owned); err != nil {
					t.Fatal(err)
				}
				if !controllerutil.ContainsFinalizer(&owned, objectmeta.GatewayAFDCleanupFinalizer) {
					t.Fatal("HTTP provisioning ran before persisted cleanup ownership")
				}
			}
			request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}
			result, err := r.Reconcile(context.Background(), request)
			if err != nil || result.RequeueAfter < 30*time.Second || result.RequeueAfter > 36*time.Second {
				t.Fatalf("HTTP progress did not schedule observation: %#v, %v", result, err)
			}
			assertHTTPGatewayReadiness(t, c, gateway, state == frontdoor.HTTPPreviewProgrammed)
			oldTransition := metav1.NewTime(time.Unix(1, 0))
			meta.FindStatusCondition(gateway.Status.Conditions, string(gatewayv1.GatewayConditionProgrammed)).LastTransitionTime = oldTransition
			for i := range gateway.Status.Listeners {
				meta.FindStatusCondition(gateway.Status.Listeners[i].Conditions, string(gatewayv1.ListenerConditionProgrammed)).LastTransitionTime = oldTransition
			}
			if err := c.Status().Update(context.Background(), gateway); err != nil {
				t.Fatal(err)
			}
			version := gateway.ResourceVersion
			if _, err := r.Reconcile(context.Background(), request); err != nil {
				t.Fatal(err)
			}
			assertHTTPGatewayReadiness(t, c, gateway, state == frontdoor.HTTPPreviewProgrammed)
			if provider.calls != 2 || provider.suspensions != 0 || provider.owner.HubUID != ownershipHubUID ||
				provider.owner.UID != string(gateway.UID) || gateway.ResourceVersion != version {
				t.Fatalf("stable maintenance replayed staging or rewrote status: provider=%#v, gateway=%#v", provider, gateway)
			}
		})
	}
}

func TestHTTPControllerFailureClearsReadiness(t *testing.T) {
	for _, scenario := range []string{"provider error", "empty hostname", "invalid hostname", "unknown state", "API resolution error"} {
		t.Run(scenario, func(t *testing.T) {
			c, gateway, _, r, provider := httpControllerFixture(t)
			request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}
			if _, err := r.Reconcile(context.Background(), request); err != nil {
				t.Fatal(err)
			}
			failure := errors.New("HTTP graph unavailable")
			switch scenario {
			case "provider error":
				provider.err = failure
			case "empty hostname":
				provider.progress.EndpointHostName = ""
			case "invalid hostname":
				provider.progress.EndpointHostName = "http://fabricated.example/"
			case "unknown state":
				provider.progress.State = "Unknown"
			case "API resolution error":
				r.Client = interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
					List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
						return failure
					},
				})
			}
			if _, err := r.Reconcile(context.Background(), request); err == nil {
				t.Fatal("failure was not surfaced")
			}
			assertHTTPGatewayReadiness(t, c, gateway, false)
			if provider.suspensions != 0 || !controllerutil.ContainsFinalizer(gateway, objectmeta.GatewayAFDCleanupFinalizer) {
				t.Fatal("ambiguous/error result authorized suspension or lost cleanup")
			}
		})
	}
}

func TestHTTPControllerProgressDiagnostic(t *testing.T) {
	for _, state := range []frontdoor.HTTPPreviewProvisioningState{frontdoor.HTTPPreviewPending, frontdoor.HTTPPreviewStaging} {
		t.Run(string(state), func(t *testing.T) {
			c, gateway, _, r, provider := httpControllerFixture(t)
			provider.progress.State = state
			provider.progress.Diagnostic = "endpoint observed: provisioning=Succeeded; deployment=NotStarted; enabled=Disabled"
			if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}); err != nil {
				t.Fatal(err)
			}
			assertHTTPGatewayReadiness(t, c, gateway, false)
			for _, conditions := range [][]metav1.Condition{gateway.Status.Conditions, gateway.Status.Listeners[0].Conditions} {
				condition := meta.FindStatusCondition(conditions, string(gatewayv1.GatewayConditionProgrammed))
				if condition == nil || !strings.Contains(condition.Message, provider.progress.Diagnostic) {
					t.Fatalf("provider diagnostic was not published: %+v", condition)
				}
			}
		})
	}
}

func TestHTTPControllerStatusConflictDoesNotReplayProvisioning(t *testing.T) {
	for _, changeIntent := range []bool{false, true} {
		t.Run(strconv.FormatBool(changeIntent), func(t *testing.T) {
			c, gateway, route, r, provider := httpControllerFixture(t)
			patches := 0
			r.Client = interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
				SubResourcePatch: func(ctx context.Context, cl client.Client, resource string, object client.Object,
					patch client.Patch, opts ...client.SubResourcePatchOption) error {
					patches++
					if patches == 1 {
						if changeIntent {
							route.Spec.ParentRefs = nil
							if err := c.Update(ctx, route); err != nil {
								t.Fatal(err)
							}
						}
						return apierrors.NewConflict(schema.GroupResource{Resource: ownershipGatewayResource}, object.GetName(), errors.New("retry"))
					}
					return cl.SubResource(resource).Patch(ctx, object, patch, opts...)
				},
			})
			if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}); err != nil {
				t.Fatal(err)
			}
			assertHTTPGatewayReadiness(t, c, gateway, !changeIntent)
			if patches != 2 || provider.calls != 1 {
				t.Fatalf("status conflict replayed provisioning: patches=%d calls=%d", patches, provider.calls)
			}
		})
	}
}

func TestHTTPControllerRejectsStaleReadyEvidence(t *testing.T) {
	const (
		uncachedBackendCase = "uncached backend changes"
		uncachedGrantCase   = "uncached grant revoked"
		finalizerCase       = "finalizer lost"
		hubCase             = "hub changes"
	)
	for _, scenario := range []string{"generation changed", "backend changes", uncachedBackendCase, uncachedGrantCase, finalizerCase, hubCase, "route detached", "post-operation API unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			c, gateway, route, r, provider := httpControllerFixture(t)
			if scenario == uncachedGrantCase {
				c, gateway, route, _ = referenceWithdrawalFixture(t)
				r.Client, r.APIReader = c, ownershipReader{Reader: c, t: t}
			}
			provider.during = func() {
				ctx := context.Background()
				switch scenario {
				case "generation changed":
					if err := c.Get(ctx, client.ObjectKeyFromObject(gateway), gateway); err != nil {
						t.Fatal(err)
					}
					gateway.Generation++
					if err := c.Update(ctx, gateway); err != nil {
						t.Fatal(err)
					}
				case "backend changes":
					var export fleetnetv1alpha1.InternalServiceExport
					key := client.ObjectKey{Namespace: ownershipExportNamespace, Name: route.Namespace + "-" + route.Name}
					if err := c.Get(ctx, key, &export); err != nil {
						t.Fatal(err)
					}
					export.Spec.ServiceReference.UID = "replacement-service"
					if err := c.Update(ctx, &export); err != nil {
						t.Fatal(err)
					}
				case finalizerCase, hubCase:
					r.APIReader = ownershipReader{Reader: c, t: t, afterGet: func(object client.Object) {
						switch current := object.(type) {
						case *gatewayv1.Gateway:
							if scenario == finalizerCase {
								current.Finalizers = nil
							}
						case *corev1.Namespace:
							if scenario == hubCase && current.Name == metav1.NamespaceSystem {
								current.UID = "replacement-hub"
							}
						}
					}}
				case uncachedBackendCase, uncachedGrantCase:
					r.APIReader = interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
						List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
							if err := cl.List(ctx, list, opts...); err != nil {
								return err
							}
							if exports, ok := list.(*fleetnetv1alpha1.InternalServiceExportList); ok && scenario == uncachedBackendCase {
								for i := range exports.Items {
									exports.Items[i].Spec.ServiceReference.UID = "replacement-service"
								}
							}
							if grants, ok := list.(*gatewayv1beta1.ReferenceGrantList); ok && scenario == uncachedGrantCase {
								grants.Items = nil
							}
							return nil
						},
					})
				case "route detached":
					route.Spec.ParentRefs = nil
					if err := c.Update(ctx, route); err != nil {
						t.Fatal(err)
					}
				case "post-operation API unavailable":
					r.APIReader = interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
						Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
							return errors.New("uncached API unavailable")
						},
					})
				}
			}
			_, _ = r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)})
			assertHTTPGatewayReadiness(t, c, gateway, false)
			if provider.calls != 1 {
				t.Fatalf("expected one bounded candidate call, got %d", provider.calls)
			}
		})
	}
}

func TestHTTPControllerWithdrawalAndCleanup(t *testing.T) {
	const (
		deletedCase      = "Gateway deleted"
		grantRevokedCase = "grant revoked"
	)
	for _, scenario := range []string{"route removed", "private origin unavailable", "unsupported listener", "class rejected", grantRevokedCase, deletedCase} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			c, gateway, route, r, provider := httpControllerFixture(t)
			var revoke func()
			if scenario == grantRevokedCase {
				var grantObject client.Object
				c, gateway, route, grantObject = referenceWithdrawalFixture(t)
				r.Client, r.APIReader = c, ownershipReader{Reader: c, t: t}
				revoke = func() {
					if err := c.Delete(ctx, grantObject); err != nil {
						t.Fatal(err)
					}
				}
			}
			request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}
			if _, err := r.Reconcile(ctx, request); err != nil {
				t.Fatal(err)
			}
			assertHTTPGatewayReadiness(t, c, gateway, true)
			switch scenario {
			case "route removed":
				if err := c.Delete(ctx, route); err != nil {
					t.Fatal(err)
				}
			case "private origin unavailable":
				var export fleetnetv1alpha1.InternalServiceExport
				key := client.ObjectKey{Namespace: ownershipExportNamespace, Name: route.Namespace + "-" + route.Name}
				if err := c.Get(ctx, key, &export); err != nil {
					t.Fatal(err)
				}
				export.Spec.PrivateLinkOrigin.Ready = false
				if err := c.Update(ctx, &export); err != nil {
					t.Fatal(err)
				}
			case "unsupported listener":
				gateway.Spec.Listeners[0].Port = 81
				if err := c.Update(ctx, gateway); err != nil {
					t.Fatal(err)
				}
			case "class rejected":
				var class gatewayv1.GatewayClass
				if err := c.Get(ctx, client.ObjectKey{Name: string(gateway.Spec.GatewayClassName)}, &class); err != nil {
					t.Fatal(err)
				}
				class.Status.Conditions[0].Status = metav1.ConditionFalse
				if err := c.Status().Update(ctx, &class); err != nil {
					t.Fatal(err)
				}
			case grantRevokedCase:
				revoke()
			case deletedCase:
				if err := c.Delete(ctx, gateway); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := r.Reconcile(ctx, request); err != nil {
				t.Fatal(err)
			}
			if scenario == deletedCase {
				if cleaner, ok := r.ProfileCleanup.(*fakeProfileCleaner); !ok || len(cleaner.owners) != 1 {
					t.Fatal("deletion did not take priority over provisioning")
				}
				if provider.calls != 1 || provider.suspensions != 0 {
					t.Fatal("deletion replayed graph work")
				}
				return
			}
			assertHTTPGatewayReadiness(t, c, gateway, false)
			condition := meta.FindStatusCondition(gateway.Status.Conditions, string(gatewayv1.GatewayConditionProgrammed))
			if provider.calls != 1 || provider.suspensions != 1 ||
				!strings.Contains(condition.Message, string(frontdoor.EndpointSuspended)) ||
				!controllerutil.ContainsFinalizer(gateway, objectmeta.GatewayAFDCleanupFinalizer) {
				t.Fatalf("withdrawal did not suspend once while retaining cleanup: provider=%#v, gateway=%#v", provider, gateway)
			}
		})
	}
}

func TestHTTPControllerOwnershipGuards(t *testing.T) {
	for _, scenario := range []string{"reader dependency missing", "cleanup dependency missing", "metadata write fails", "competing stager"} {
		t.Run(scenario, func(t *testing.T) {
			c, gateway, _, r, provider := httpControllerFixture(t)
			switch scenario {
			case "reader dependency missing":
				r.APIReader = nil
			case "cleanup dependency missing":
				r.ProfileCleanup = nil
			case "metadata write fails":
				r.Client = interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
					Patch: func(context.Context, client.WithWatch, client.Object, client.Patch, ...client.PatchOption) error {
						return errors.New("ownership patch denied")
					},
				})
			case "competing stager":
				r.GraphStager = &fakeGraphStager{}
			}
			if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}); err == nil {
				t.Fatal("invalid provisioning prerequisites were not surfaced")
			}
			assertHTTPGatewayReadiness(t, c, gateway, false)
			if provider.calls != 0 || provider.suspensions != 0 ||
				controllerutil.ContainsFinalizer(gateway, objectmeta.GatewayAFDCleanupFinalizer) {
				t.Fatal("invalid prerequisites authorized provisioning or acquired ownership")
			}
		})
	}
}

func TestHTTPControllerUnprogrammableWithdrawalRequiresFreshEvidence(t *testing.T) {
	for _, readFails := range []bool{false, true} {
		t.Run(strings.Join([]string{"read fails", strconv.FormatBool(readFails)}, "="), func(t *testing.T) {
			ctx := context.Background()
			c, gateway, route, r, provider := httpControllerFixture(t)
			request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}
			if _, err := r.Reconcile(ctx, request); err != nil {
				t.Fatal(err)
			}

			var export fleetnetv1alpha1.InternalServiceExport
			key := client.ObjectKey{Namespace: ownershipExportNamespace, Name: route.Namespace + "-" + route.Name}
			if err := c.Get(ctx, key, &export); err != nil {
				t.Fatal(err)
			}
			export.Spec.PrivateLinkOrigin.Ready = false
			if err := c.Update(ctx, &export); err != nil {
				t.Fatal(err)
			}
			r.APIReader = interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
				List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					if readFails {
						return errors.New("fresh API unavailable")
					}
					if err := cl.List(ctx, list, opts...); err != nil {
						return err
					}
					if exports, ok := list.(*fleetnetv1alpha1.InternalServiceExportList); ok {
						for i := range exports.Items {
							exports.Items[i].Spec.PrivateLinkOrigin.Ready = true
						}
					}
					return nil
				},
			})
			if _, err := r.Reconcile(ctx, request); err == nil {
				t.Fatal("unconfirmed withdrawal evidence was not surfaced")
			}
			assertHTTPGatewayReadiness(t, c, gateway, false)
			if provider.calls != 1 || provider.suspensions != 0 {
				t.Fatal("stale/ambiguous invalid intent authorized Azure mutation")
			}
		})
	}
}

func TestHTTPControllerEventsFollowPersistedStatus(t *testing.T) {
	for _, statusFails := range []bool{false, true} {
		t.Run(strconv.FormatBool(statusFails), func(t *testing.T) {
			c, gateway, _, r, provider := httpControllerFixture(t)
			recorder := record.NewFakeRecorder(10)
			r.Recorder = recorder
			provider.progress = frontdoor.HTTPPreviewProvisioningResult{State: frontdoor.HTTPPreviewStaging,
				Staging: frontdoor.DisabledGraphStagingResult{Route: frontdoor.RouteProvisioningResult{
					Backends: frontdoor.BackendProvisioningResult{Origins: frontdoor.PrivateOriginsObservation{
						PendingApprovals: []frontdoor.PrivateLinkPlan{{}},
					}},
				}}}
			if statusFails {
				r.Client = interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
					SubResourcePatch: func(context.Context, client.Client, string, client.Object, client.Patch, ...client.SubResourcePatchOption) error {
						return errors.New("status publication denied")
					},
				})
			}
			request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}
			_, err := r.Reconcile(context.Background(), request)
			if (err != nil) != statusFails {
				t.Fatalf("status error lost: %v", err)
			}
			if statusFails {
				assertObservationEvent(t, recorder, "")
				return
			}
			assertObservationEvent(t, recorder, "Normal AFDPrivateLinkApprovalRequired")
			if _, err := r.Reconcile(context.Background(), request); err != nil {
				t.Fatal(err)
			}
			assertObservationEvent(t, recorder, "")
			provider.progress.State = frontdoor.HTTPPreviewProgrammed
			provider.progress.EndpointHostName = httpPreviewHostname
			if _, err := r.Reconcile(context.Background(), request); err != nil {
				t.Fatal(err)
			}
			assertObservationEvent(t, recorder, "Normal AFDHTTPProgrammed")
		})
	}
}
