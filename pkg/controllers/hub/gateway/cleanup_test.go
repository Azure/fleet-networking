/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gateway

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/providers/azure/frontdoor"
)

const (
	cleanupCounterName  = "fleet_networking_gateway_afd_profile_cleanup_calls_total"
	cleanupDurationName = "fleet_networking_gateway_afd_profile_cleanup_duration_seconds"
	cleanupFailedEvent  = "Warning AFDProfileCleanupFailed"
	cleanupOwnedEvent   = "Warning AFDProfileCleanupOwnershipConflict"
	cleanupDoneEvent    = "Normal AFDProfileCleanupComplete"
)

type fakeProfileCleaner struct {
	deleted bool
	err     error
	owners  []frontdoor.GatewayOwner
}

func (f *fakeProfileCleaner) DeleteProfile(_ context.Context, owner frontdoor.GatewayOwner) (bool, error) {
	f.owners = append(f.owners, owner)
	return f.deleted, f.err
}

type cleanupConflictClient struct {
	client.Client
	patches int
}

type cleanupAPIReader struct {
	client.Reader
	hub client.Reader
}

func (r cleanupAPIReader) Get(ctx context.Context, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
	if _, ok := object.(*corev1.Namespace); ok {
		return r.hub.Get(ctx, key, object, options...)
	}
	return r.Reader.Get(ctx, key, object, options...)
}

func (c *cleanupConflictClient) Patch(ctx context.Context, object client.Object, patch client.Patch, options ...client.PatchOption) error {
	c.patches++
	if c.patches == 1 {
		var current gatewayv1.Gateway
		if err := c.Client.Get(ctx, client.ObjectKeyFromObject(object), &current); err != nil {
			return err
		}
		current.Finalizers = append(current.Finalizers, "concurrent.example/cleanup")
		if err := c.Client.Update(ctx, &current); err != nil {
			return err
		}
		return apierrors.NewConflict(schema.GroupResource{Group: gatewayv1.GroupName, Resource: "gateways"}, object.GetName(), errors.New("concurrent metadata update"))
	}
	return c.Client.Patch(ctx, object, patch, options...)
}

func TestGatewayFinalizesOwnedProfileWithoutClass(t *testing.T) {
	tests := []struct {
		name          string
		withoutOwner  bool
		notDeleting   bool
		missingHub    bool
		emptyHubUID   bool
		missingClient bool
		missingReader bool
		conflict      bool
		deleted       bool
		cleanupError  error
		wantError     bool
		wantCalls     int
		wantFinalizer bool
		wantRetry     bool
		outcome       string
		event         string
	}{
		{name: "never owned", withoutOwner: true},
		{name: "not deleting", notDeleting: true, wantFinalizer: true},
		{name: "Azure deletion pending", wantCalls: 1, wantFinalizer: true, wantRetry: true, outcome: "pending"},
		{name: "observed absent", deleted: true, wantCalls: 1, outcome: "absent", event: cleanupDoneEvent},
		{name: "failed cleanup", cleanupError: errors.New(privateErrorPayload), wantCalls: 1, wantError: true, wantFinalizer: true,
			outcome: "error", event: cleanupFailedEvent},
		{name: "error takes precedence over absence", deleted: true, cleanupError: errors.New(privateErrorPayload),
			wantCalls: 1, wantError: true, wantFinalizer: true, outcome: "error", event: cleanupFailedEvent},
		{name: "wrapped throttling", cleanupError: fmt.Errorf("cleanup: %w", &azcore.ResponseError{StatusCode: http.StatusTooManyRequests}),
			wantCalls: 1, wantError: true, wantFinalizer: true, outcome: "throttled", event: cleanupFailedEvent},
		{name: "ownership collision", cleanupError: fmt.Errorf("cleanup: %w", &frontdoor.OwnershipConflictError{ProfileID: privateErrorPayload, Reason: privateErrorPayload}),
			wantCalls: 1, wantError: true, wantFinalizer: true, outcome: "ownership_conflict", event: cleanupOwnedEvent},
		{name: "missing hub", missingHub: true, wantError: true, wantFinalizer: true},
		{name: "missing hub UID", emptyHubUID: true, wantError: true, wantFinalizer: true},
		{name: "provider unavailable", missingClient: true, wantError: true, wantFinalizer: true},
		{name: "reader unavailable", missingReader: true, wantError: true, wantFinalizer: true},
		{name: "metadata conflict does not repeat Azure deletion", deleted: true, conflict: true, wantCalls: 1, outcome: "absent", event: cleanupDoneEvent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			scheme := runtime.NewScheme()
			if err := gatewayv1.Install(scheme); err != nil {
				t.Fatal(err)
			}
			if err := corev1.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			now := metav1.Now()
			gateway := &gatewayv1.Gateway{ObjectMeta: metav1.ObjectMeta{
				Namespace: "store", Name: "global", UID: "gateway-uid", DeletionTimestamp: &now,
				Finalizers: []string{objectmeta.GatewayAFDCleanupFinalizer, "other.example/cleanup"},
			}, Spec: gatewayv1.GatewaySpec{GatewayClassName: "class-no-longer-present"}}
			if tt.withoutOwner {
				gateway.Finalizers = gateway.Finalizers[1:]
			}
			if tt.notDeleting {
				gateway.DeletionTimestamp = nil
			}
			cachedHub := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: metav1.NamespaceSystem, UID: "stale-hub"}}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gateway, cachedHub).Build()
			readerBuilder := fake.NewClientBuilder().WithScheme(scheme)
			if !tt.missingHub {
				hub := cachedHub.DeepCopy()
				hub.UID = "hub-uid"
				if tt.emptyHubUID {
					hub.UID = ""
				}
				readerBuilder = readerBuilder.WithObjects(hub)
			}
			cleaner := &fakeProfileCleaner{deleted: tt.deleted, err: tt.cleanupError}
			recorder := record.NewFakeRecorder(10)
			r := &Reconciler{Client: c, APIReader: cleanupAPIReader{Reader: c, hub: readerBuilder.Build()}, ProfileCleanup: cleaner, Recorder: recorder}
			if tt.missingClient {
				r.ProfileCleanup = nil
			}
			if tt.missingReader {
				r.APIReader = nil
			}
			var conflictClient *cleanupConflictClient
			if tt.conflict {
				conflictClient = &cleanupConflictClient{Client: c}
				r.Client = conflictClient
			}
			key := client.ObjectKeyFromObject(gateway)
			before := cleanupMetricSamples(t, tt.outcome)
			beforeTotal := cleanupMetricSamples(t, "")
			result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
			if (err != nil) != tt.wantError || len(cleaner.owners) != tt.wantCalls {
				t.Fatalf("err=%v cleanup calls=%d; want error=%v/calls=%d", err, len(cleaner.owners), tt.wantError, tt.wantCalls)
			}
			if tt.cleanupError != nil && !errors.Is(err, tt.cleanupError) {
				t.Fatalf("cleanup error was not preserved: %v", err)
			}
			assertObservationEvent(t, recorder, tt.event)
			after := cleanupMetricSamples(t, tt.outcome)
			afterTotal := cleanupMetricSamples(t, "")
			calls := float64(tt.wantCalls)
			if after.calls-before.calls != calls || after.durations-before.durations != calls ||
				afterTotal.calls-beforeTotal.calls != calls || afterTotal.durations-beforeTotal.durations != calls {
				t.Fatalf("cleanup samples = %v -> %v (total %v -> %v), want %v", before, after, beforeTotal, afterTotal, calls)
			}
			if (result.RequeueAfter > 0) != tt.wantRetry {
				t.Fatalf("unexpected cleanup requeue: %#v", result)
			}
			if len(cleaner.owners) != 0 {
				want := frontdoor.GatewayOwner{HubUID: "hub-uid", Namespace: gateway.Namespace, Name: gateway.Name, UID: string(gateway.UID)}
				if cleaner.owners[0] != want {
					t.Fatalf("cleanup used stale or incorrect ownership: %#v", cleaner.owners[0])
				}
			}

			var current gatewayv1.Gateway
			if err := c.Get(ctx, key, &current); err != nil {
				t.Fatal(err)
			}
			assertCleanupFinalizers(t, current.Finalizers, tt.wantFinalizer, conflictClient)
			if !tt.wantFinalizer && tt.wantCalls != 0 {
				if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
					t.Fatal(err)
				}
				assertObservationEvent(t, recorder, "")
				if samples := cleanupMetricSamples(t, ""); samples != afterTotal || len(cleaner.owners) != tt.wantCalls {
					t.Fatalf("completed finalization repeated cleanup: calls=%d samples=%v -> %v", len(cleaner.owners), afterTotal, samples)
				}
			}
		})
	}
}

func assertCleanupFinalizers(t *testing.T, finalizers []string, wantOwned bool, conflictClient *cleanupConflictClient) {
	t.Helper()
	if slices.Contains(finalizers, objectmeta.GatewayAFDCleanupFinalizer) != wantOwned ||
		!slices.Contains(finalizers, "other.example/cleanup") ||
		(conflictClient != nil && (!slices.Contains(finalizers, "concurrent.example/cleanup") || conflictClient.patches != 2)) {
		t.Fatalf("cleanup lost finalizer ownership or concurrent metadata: %v", finalizers)
	}
}

func TestProfileCleanupCompletionRequiresPersistedRemoval(t *testing.T) {
	tests := []struct {
		name      string
		scenario  string
		wantError bool
	}{
		{"patch failure", "patch-failure", true},
		{"exhausted conflicts", "conflict", true},
		{"Gateway replaced", "replaced", false},
		{"Gateway disappeared", "missing", false},
		{"Gateway no longer deleting", "not-deleting", false},
		{"finalizer removed by another writer", "removed", false},
		{"conflict followed by another writer removal", "conflict-removed", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, gateway, _, _ := gatewayFixture(t, 1)
			addObservationHub(t, c)
			now := metav1.Now()
			gateway.DeletionTimestamp = &now
			gateway.Finalizers = []string{objectmeta.GatewayAFDCleanupFinalizer}
			recorder := record.NewFakeRecorder(10)
			patches := 0
			wrapped := interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
				Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
					if err := cl.Get(ctx, key, object, options...); err != nil {
						return err
					}
					current, ok := object.(*gatewayv1.Gateway)
					if !ok {
						return nil
					}
					current.DeletionTimestamp = &now
					current.Finalizers = []string{objectmeta.GatewayAFDCleanupFinalizer}
					switch tt.scenario {
					case "replaced":
						current.UID = "replacement-uid"
					case "missing":
						return apierrors.NewNotFound(schema.GroupResource{Resource: ownershipGatewayResource}, key.Name)
					case "not-deleting":
						current.DeletionTimestamp = nil
					case "removed":
						current.Finalizers = nil
					case "conflict-removed":
						if patches > 0 {
							current.Finalizers = nil
						}
					}
					return nil
				},
				Patch: func(_ context.Context, _ client.WithWatch, object client.Object, _ client.Patch, _ ...client.PatchOption) error {
					assertObservationEvent(t, recorder, "")
					patches++
					if tt.scenario == "patch-failure" {
						return errors.New("patch unavailable")
					}
					return apierrors.NewConflict(schema.GroupResource{Resource: ownershipGatewayResource}, object.GetName(), errors.New("conflict"))
				},
			})
			cleaner := &fakeProfileCleaner{deleted: true}
			r := &Reconciler{Client: wrapped, APIReader: wrapped, ProfileCleanup: cleaner, Recorder: recorder}
			before := cleanupMetricSamples(t, "absent")
			if _, err := r.finalize(context.Background(), gateway); (err != nil) != tt.wantError {
				t.Fatalf("finalization error = %v, want failure=%v", err, tt.wantError)
			}
			assertObservationEvent(t, recorder, "")
			after := cleanupMetricSamples(t, "absent")
			if len(cleaner.owners) != 1 || after.calls-before.calls != 1 || after.durations-before.durations != 1 {
				t.Fatalf("cleanup repeated or lost samples: calls=%d samples=%v -> %v", len(cleaner.owners), before, after)
			}
		})
	}
}

func TestProfileCleanupPendingPolls(t *testing.T) {
	c, gateway, _, _ := gatewayFixture(t, 1)
	addObservationHub(t, c)
	now := metav1.Now()
	gateway.DeletionTimestamp = &now
	gateway.Finalizers = []string{objectmeta.GatewayAFDCleanupFinalizer}
	recorder := record.NewFakeRecorder(10)
	cleaner := &fakeProfileCleaner{}
	r := &Reconciler{Client: c, APIReader: c, ProfileCleanup: cleaner, Recorder: recorder}
	before := cleanupMetricSamples(t, "pending")
	for i := 0; i < 2; i++ {
		result, err := r.finalize(context.Background(), gateway)
		if err != nil || result.RequeueAfter != 30*time.Second {
			t.Fatalf("pending cleanup result=%v err=%v", result, err)
		}
		assertObservationEvent(t, recorder, "")
	}
	after := cleanupMetricSamples(t, "pending")
	if len(cleaner.owners) != 2 || after.calls-before.calls != 2 || after.durations-before.durations != 2 {
		t.Fatalf("pending polls lost samples: %v -> %v", before, after)
	}
}

func TestProfileCleanupDurationUsesSeconds(t *testing.T) {
	before := cleanupMetricSamples(t, "absent")
	recordProfileCleanup(250*time.Millisecond, true, nil)
	after := cleanupMetricSamples(t, "absent")
	if after.calls-before.calls != 1 || after.durations-before.durations != 1 ||
		math.Abs(after.seconds-before.seconds-0.25) > 1e-9 {
		t.Fatalf("duration samples = %v -> %v, want one sample of 0.25 seconds", before, after)
	}
}

func cleanupMetricSamples(t *testing.T, outcome string) observationSamples {
	t.Helper()
	return operationMetricSamples(t, cleanupCounterName, cleanupDurationName, outcome,
		"pending", "absent", "ownership_conflict", "throttled", "error")
}
