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
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	"go.goms.io/fleet-networking/pkg/providers/azure/frontdoor"
)

const (
	observationCounterName  = "fleet_networking_gateway_afd_origin_observations_total"
	observationDurationName = "fleet_networking_gateway_afd_origin_observation_duration_seconds"
	privateErrorPayload     = "private-sdk-payload"
	approvalEventPrefix     = corev1.EventTypeNormal + " AFDPrivateLinkApprovalRequired"
	notReadyEventPrefix     = corev1.EventTypeWarning + " AFDPrivateOriginsNotReady"
	observationTestLocation = "eastus"
)

func TestOriginObservationTransitionEvents(t *testing.T) {
	tests := []struct {
		name   string
		result frontdoor.PrivateOriginsObservation
		err    error
		want   string
	}{
		{"approval required", pendingOriginApproval(), nil, approvalEventPrefix},
		{"origins converged", frontdoor.PrivateOriginsObservation{OriginsConverged: true}, nil, "Normal AFDPrivateOriginsConverged"},
		{"read failed", frontdoor.PrivateOriginsObservation{}, errors.New(privateErrorPayload), "Warning AFDOriginObservationFailed"},
		{"profile absent", frontdoor.PrivateOriginsObservation{ProfileMissing: true}, nil, ""},
		{"profile absent cannot claim convergence", frontdoor.PrivateOriginsObservation{ProfileMissing: true, OriginsConverged: true}, nil, ""},
		{"provisioning", originState(frontdoor.PrivateOriginProvisioning), nil, ""},
		{"unknown", originState(frontdoor.PrivateOriginUnknown), nil, ""},
		{"rejected", originState(frontdoor.PrivateOriginRejected), nil, notReadyEventPrefix},
		{"disconnected", originState(frontdoor.PrivateOriginDisconnected), nil, notReadyEventPrefix},
		{"timed out", originState(frontdoor.PrivateOriginTimedOut), nil, notReadyEventPrefix},
		{"failed", originState(frontdoor.PrivateOriginFailed), nil, notReadyEventPrefix},
		{"drifted", originState(frontdoor.PrivateOriginDrifted), nil, notReadyEventPrefix},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, gateway, _, _ := gatewayFixture(t, 1)
			addObservationHub(t, c)
			recorder := record.NewFakeRecorder(10)
			observer := &fakeOriginObserver{result: tt.result, err: tt.err}
			r := &Reconciler{Client: c, APIReader: c, OriginObserver: observer, Recorder: recorder}
			request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}
			for poll := 0; poll < 2; poll++ {
				_, err := r.Reconcile(context.Background(), request)
				if (err != nil) != (tt.err != nil) {
					t.Fatalf("reconcile error = %v, want %v", err, tt.err)
				}
				want := ""
				if poll == 0 {
					want = tt.want
				}
				assertObservationEvent(t, recorder, want)
			}
			if observer.calls != 2 {
				t.Fatalf("polls made %d observer calls, want 2", observer.calls)
			}
			if err := c.Get(context.Background(), request.NamespacedName, gateway); err != nil {
				t.Fatal(err)
			}
			condition := meta.FindStatusCondition(gateway.Status.Conditions, "Programmed")
			if condition == nil || condition.Status != "False" || len(gateway.Status.Addresses) != 0 {
				t.Fatal("observation telemetry changed ingress readiness")
			}
		})
	}
}

func TestOriginObservationTelemetryRetriesAndDiscard(t *testing.T) {
	tests := []struct {
		name      string
		conflict  bool
		patchFail bool
		stale     bool
		wantEvent bool
	}{
		{name: "successful retry", conflict: true, wantEvent: true},
		{name: "failed status patch", patchFail: true},
		{name: "stale observation", stale: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, gateway, route, _ := gatewayFixture(t, 1)
			addObservationHub(t, c)
			recorder := record.NewFakeRecorder(10)
			observer := &fakeOriginObserver{result: pendingOriginApproval()}
			if tt.stale {
				observer.during = func() {
					route.Spec.ParentRefs = nil
					if err := c.Update(context.Background(), route); err != nil {
						t.Fatal(err)
					}
				}
			}
			patches := 0
			wrapped := interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
				SubResourcePatch: func(ctx context.Context, cl client.Client, subresource string, object client.Object, patch client.Patch, options ...client.SubResourcePatchOption) error {
					assertObservationEvent(t, recorder, "")
					patches++
					if tt.patchFail {
						return errors.New("status unavailable")
					}
					if tt.conflict && patches == 1 {
						return apierrors.NewConflict(schema.GroupResource{Resource: ownershipGatewayResource}, object.GetName(), errors.New("conflict"))
					}
					return cl.SubResource(subresource).Patch(ctx, object, patch, options...)
				},
			})
			before := observationMetricSamples(t, originObservationSuccess)
			r := &Reconciler{Client: wrapped, APIReader: c, OriginObserver: observer, Recorder: recorder}
			_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)})
			if (err != nil) != tt.patchFail {
				t.Fatalf("reconcile error = %v, want failure=%v", err, tt.patchFail)
			}
			want := ""
			if tt.wantEvent {
				want = approvalEventPrefix
			}
			assertObservationEvent(t, recorder, want)
			after := observationMetricSamples(t, originObservationSuccess)
			if observer.calls != 1 || after.calls-before.calls != 1 || after.durations-before.durations != 1 {
				t.Fatalf("observer calls/samples = %d/%v -> %v", observer.calls, before, after)
			}
			if tt.conflict && patches != 2 {
				t.Fatalf("status patches = %d, want 2", patches)
			}
		})
	}
}

func TestOriginObservationEventSuppressedByStaging(t *testing.T) {
	c, gateway, _, _ := gatewayFixture(t, 1)
	addObservationHub(t, c)
	recorder := record.NewFakeRecorder(10)
	stager := &fakeGraphStager{}
	r := &Reconciler{Client: c, APIReader: ownershipReader{Reader: c, t: t},
		OriginObserver: &fakeOriginObserver{result: pendingOriginApproval()},
		ProfileCleanup: &fakeProfileCleaner{}, GraphStager: stager, Recorder: recorder}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}); err != nil {
		t.Fatal(err)
	}
	if stager.calls != 1 {
		t.Fatal("test did not exercise the injected staging path")
	}
	assertObservationEvent(t, recorder, "")
}

func TestOriginObservationMetricOutcomes(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		missingHub bool
		disabled   bool
		outcome    string
		calls      float64
	}{
		{name: "success", outcome: originObservationSuccess, calls: 1},
		{name: "SDK failure", err: errors.New(privateErrorPayload), outcome: originObservationError, calls: 1},
		{name: "wrapped throttling", err: fmt.Errorf("observe: %w", &azcore.ResponseError{StatusCode: http.StatusTooManyRequests}),
			outcome: originObservationThrottled, calls: 1},
		{name: "missing trusted identity", missingHub: true, outcome: originObservationError},
		{name: "no injected observer", disabled: true, outcome: originObservationSuccess},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, gateway, _, _ := gatewayFixture(t, 1)
			if !tt.missingHub {
				addObservationHub(t, c)
			}
			observer := &fakeOriginObserver{err: tt.err}
			r := &Reconciler{Client: c, APIReader: c, OriginObserver: observer}
			if tt.disabled {
				r.OriginObserver = nil
			}
			before := observationMetricSamples(t, tt.outcome)
			beforeTotal := observationMetricSamples(t, "")
			_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)})
			if (err != nil) != (tt.err != nil || tt.missingHub) {
				t.Fatalf("unexpected reconcile error: %v", err)
			}
			after := observationMetricSamples(t, tt.outcome)
			afterTotal := observationMetricSamples(t, "")
			if float64(observer.calls) != tt.calls || after.calls-before.calls != tt.calls || after.durations-before.durations != tt.calls {
				t.Fatalf("calls/samples = %d/%v -> %v, want %v", observer.calls, before, after, tt.calls)
			}
			if afterTotal.calls-beforeTotal.calls != tt.calls || afterTotal.durations-beforeTotal.durations != tt.calls {
				t.Fatalf("unexpected additional outcome samples: %v -> %v", beforeTotal, afterTotal)
			}
		})
	}
}

func pendingOriginApproval() frontdoor.PrivateOriginsObservation {
	return frontdoor.PrivateOriginsObservation{PendingApprovals: []frontdoor.PrivateLinkPlan{{
		Target: frontdoor.PrivateLinkTarget{ResourceID: privateErrorPayload, Location: observationTestLocation},
	}}}
}

func TestOriginObservationDurationUsesSeconds(t *testing.T) {
	before := observationMetricSamples(t, originObservationSuccess)
	recordOriginObservation(250*time.Millisecond, nil)
	after := observationMetricSamples(t, originObservationSuccess)
	if after.calls-before.calls != 1 || after.durations-before.durations != 1 ||
		math.Abs(after.seconds-before.seconds-0.25) > 1e-9 {
		t.Fatalf("duration samples = %v -> %v, want one sample of 0.25 seconds", before, after)
	}
}

func originState(state frontdoor.PrivateOriginState) frontdoor.PrivateOriginsObservation {
	return frontdoor.PrivateOriginsObservation{Origins: []frontdoor.PrivateOriginObservation{{State: state}}}
}

func assertObservationEvent(t *testing.T, recorder *record.FakeRecorder, want string) {
	t.Helper()
	select {
	case event := <-recorder.Events:
		if want == "" || !strings.HasPrefix(event, want+" ") || strings.Contains(event, privateErrorPayload) {
			t.Fatalf("event = %q, want %q without SDK/resource payloads", event, want)
		}
	default:
		if want != "" {
			t.Fatalf("missing event %q", want)
		}
	}
	if len(recorder.Events) != 0 {
		t.Fatal("one observation emitted duplicate events")
	}
}

type observationSamples struct {
	calls     float64
	durations float64
	seconds   float64
}

func observationMetricSamples(t *testing.T, outcome string) observationSamples {
	t.Helper()
	return operationMetricSamples(t, observationCounterName, observationDurationName, outcome,
		originObservationSuccess, originObservationError, originObservationThrottled)
}

func operationMetricSamples(t *testing.T, counterName, durationName, outcome string, allowed ...string) observationSamples {
	t.Helper()
	families, err := ctrlmetrics.Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var samples observationSamples
	for _, family := range families {
		if family.GetName() != counterName && family.GetName() != durationName {
			continue
		}
		for _, metric := range family.Metric {
			if len(metric.Label) != 1 || metric.Label[0].GetName() != "outcome" {
				t.Fatalf("unbounded/unexpected metric labels: %v", metric.Label)
			}
			value := metric.Label[0].GetValue()
			valid := false
			for _, candidate := range allowed {
				valid = valid || value == candidate
			}
			if !valid {
				t.Fatalf("unexpected outcome label %q", value)
			}
			if outcome == "" || value == outcome {
				samples.calls += metric.GetCounter().GetValue()
				samples.durations += float64(metric.GetHistogram().GetSampleCount())
				samples.seconds += metric.GetHistogram().GetSampleSum()
			}
		}
	}
	return samples
}
