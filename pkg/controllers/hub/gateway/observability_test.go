/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gateway

import (
	"bytes"
	"context"
	"errors"
	"flag"
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
	"k8s.io/client-go/tools/events"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/textlogger"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/providers/azure/frontdoor"
)

func captureGatewayProgress(t *testing.T) *bytes.Buffer {
	t.Helper()
	state := klog.CaptureState()
	t.Cleanup(state.Restore)
	flags := flag.NewFlagSet("gateway-progress", flag.ContinueOnError)
	klog.InitFlags(flags)
	if err := flags.Set("v", "2"); err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	klog.SetLogger(textlogger.NewLogger(textlogger.NewConfig(textlogger.Verbosity(2), textlogger.Output(logs))))
	return logs
}

func TestGatewayReconciliationProgressLogs(t *testing.T) {
	tests := []struct {
		name        string
		providerErr bool
		patchFail   bool
		conflict    bool
		programmed  bool
	}{
		{name: "pending graph"},
		{name: "programmed graph", programmed: true},
		{name: "provider failure", providerErr: true},
		{name: "status patch failure", patchFail: true},
		{name: "status conflict", conflict: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := captureGatewayProgress(t)
			c, gateway, _, r, provider := httpControllerFixture(t)
			if !tt.programmed {
				provider.progress.State = frontdoor.HTTPPreviewPending
			}
			provider.progress.Staging.Route.Backends.Origins = pendingOriginApproval()
			if tt.providerErr {
				provider.err = errors.New(privateErrorPayload)
			}
			patches := 0
			r.Client = interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
				SubResourcePatch: func(ctx context.Context, cl client.Client, resource string, object client.Object,
					patch client.Patch, opts ...client.SubResourcePatchOption) error {
					if strings.Contains(logs.String(), "Persisted Gateway status") {
						t.Fatal("status logged as persisted before patch succeeded")
					}
					patches++
					if tt.patchFail {
						return errors.New(privateErrorPayload)
					}
					if tt.conflict && patches == 1 {
						return apierrors.NewConflict(schema.GroupResource{Resource: ownershipGatewayResource}, object.GetName(), errors.New(privateErrorPayload))
					}
					return cl.SubResource(resource).Patch(ctx, object, patch, opts...)
				},
			})
			_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)})
			if (err != nil) != (tt.providerErr || tt.patchFail) || provider.calls != 1 {
				t.Fatalf("logging changed reconciliation: calls=%d, error=%v", provider.calls, err)
			}
			output := logs.String()
			want := []string{"Reconciling Gateway", fmt.Sprintf(`gateway="%s/%s"`, gateway.Namespace, gateway.Name),
				fmt.Sprintf(`uid="%s"`, gateway.UID), fmt.Sprintf("generation=%d", gateway.Generation),
				"Resolved Gateway desired graph", "routes=1", "duration="}
			if tt.providerErr {
				want = append(want, `operation="reconcile_graph"`, `cause="unclassified operation failure"`)
			} else {
				want = append(want, "AFD private HTTP graph progress", "stagingPendingApprovals=1")
			}
			if tt.providerErr || tt.patchFail {
				want = append(want, "Gateway reconciliation failed")
			} else {
				want = append(want, "Gateway reconciliation completed")
			}
			for _, text := range want {
				if !strings.Contains(output, text) {
					t.Errorf("missing %q in logs:\n%s", text, output)
				}
			}
			wantPersisted := 1
			if tt.patchFail {
				wantPersisted = 0
			}
			if strings.Count(output, "Persisted Gateway status") != wantPersisted || strings.Contains(output, privateErrorPayload) {
				t.Fatalf("logs leaked an error payload or misreported persistence:\n%s", output)
			}
			if !tt.patchFail {
				status := `programmed="False"`
				if tt.programmed {
					status = `programmed="True"`
				}
				if !strings.Contains(output, status) {
					t.Fatalf("missing actual persisted readiness %s:\n%s", status, output)
				}
			}
			if tt.conflict && patches != 2 {
				t.Fatalf("unexpected status retry count: %d", patches)
			}
		})
	}
}

func TestGatewayObservationAndWithdrawalProgressLogs(t *testing.T) {
	logs := captureGatewayProgress(t)
	c, gateway, route, r, provider := httpControllerFixture(t)
	observer := &fakeOriginObserver{result: pendingOriginApproval()}
	r.OriginObserver = observer
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}
	if _, err := r.Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "Observed AFD private origins") || !strings.Contains(logs.String(), "pendingApprovals=1") {
		t.Fatalf("origin progress missing:\n%s", logs)
	}
	route.Spec.ParentRefs = nil
	if err := c.Update(context.Background(), route); err != nil {
		t.Fatal(err)
	}
	logs.Reset()
	if _, err := r.Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "AFD withdrawal progress") || provider.calls != 1 || provider.withdrawals != 1 || observer.calls != 1 {
		t.Fatalf("withdrawal logs missing or operations repeated: provider=%+v observer=%+v\n%s", provider, observer, logs)
	}
}

func TestGatewayCleanupProgressLogs(t *testing.T) {
	for _, scenario := range []string{"deletion pending", "finalizer removed", "finalizer patch fails"} {
		t.Run(scenario, func(t *testing.T) {
			logs := captureGatewayProgress(t)
			c, gateway, _, r, _ := httpControllerFixture(t)
			gateway.Finalizers = []string{objectmeta.GatewayAFDCleanupFinalizer, "other.example/cleanup"}
			if err := c.Update(context.Background(), gateway); err != nil {
				t.Fatal(err)
			}
			if err := c.Delete(context.Background(), gateway); err != nil {
				t.Fatal(err)
			}
			cleaner := &fakeProfileCleaner{deleted: scenario != "deletion pending"}
			r.ProfileCleanup = cleaner
			failing := scenario == "finalizer patch fails"
			if failing {
				r.Client = interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
					Patch: func(context.Context, client.WithWatch, client.Object, client.Patch, ...client.PatchOption) error {
						return errors.New(privateErrorPayload)
					},
				})
			}
			_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)})
			if (err != nil) != failing || len(cleaner.owners) != 1 {
				t.Fatalf("logging changed cleanup: calls=%d error=%v", len(cleaner.owners), err)
			}
			output := logs.String()
			if !strings.Contains(output, "AFD profile cleanup observed") || strings.Contains(output, privateErrorPayload) {
				t.Fatalf("cleanup observation missing or error payload leaked:\n%s", output)
			}
			completed := strings.Contains(output, "Gateway cleanup finalizer removed")
			if completed != (scenario == "finalizer removed") {
				t.Fatalf("logs misreported finalizer persistence:\n%s", output)
			}
		})
	}
}

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
			recorder := events.NewFakeRecorder(10)
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
			recorder := events.NewFakeRecorder(10)
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
	recorder := events.NewFakeRecorder(10)
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

func TestAFDFailureEventCauses(t *testing.T) {
	tests := []struct {
		name, cause string
		err         error
	}{
		{"authorization", "authorization denied", &azcore.ResponseError{StatusCode: http.StatusForbidden, ErrorCode: privateErrorPayload}},
		{"authentication", "authentication failed", &azcore.ResponseError{StatusCode: http.StatusUnauthorized, ErrorCode: privateErrorPayload}},
		{"throttling", "request throttled", &azcore.ResponseError{StatusCode: http.StatusTooManyRequests, ErrorCode: privateErrorPayload}},
		{"not found", "resource not found", &azcore.ResponseError{StatusCode: http.StatusNotFound, ErrorCode: privateErrorPayload}},
		{"conflict", "resource update conflict", &azcore.ResponseError{StatusCode: http.StatusConflict, ErrorCode: privateErrorPayload}},
		{"service failure", "Azure service unavailable", &azcore.ResponseError{StatusCode: http.StatusServiceUnavailable, ErrorCode: privateErrorPayload}},
		{"timeout", "request timed out", context.DeadlineExceeded},
		{"canceled", "request canceled", context.Canceled},
		{"ownership", "resource ownership conflict", &frontdoor.OwnershipConflictError{ProfileID: privateErrorPayload, Reason: privateErrorPayload}},
		{"unknown", "unclassified operation failure", errors.New(privateErrorPayload)},
	}
	for _, tt := range tests {
		for _, source := range []string{"graph", "cleanup", "observation"} {
			t.Run(tt.name+"/"+source, func(t *testing.T) {
				_, gateway, _, _ := gatewayFixture(t, 1)
				recorder := events.NewFakeRecorder(1)
				r := &Reconciler{Recorder: recorder}
				err := fmt.Errorf("wrapped %s: %w", privateErrorPayload, tt.err)
				switch source {
				case "graph":
					r.recordGraphOperation(gateway, graphReconcileHTTP, time.Millisecond, err)
				case "cleanup":
					r.recordProfileCleanupFailure(gateway, err)
				case "observation":
					r.recordOriginObservationEvent(gateway, &gatewayObservation{err: err})
				}
				select {
				case event := <-recorder.Events:
					if !strings.Contains(event, tt.cause) || strings.Contains(event, privateErrorPayload) ||
						strings.Contains(event, "inspect controller logs") {
						t.Fatalf("event must explain a sanitized cause: %s", event)
					}
				default:
					t.Fatal("failure event was not emitted")
				}
			})
		}
	}
}

func originState(state frontdoor.PrivateOriginState) frontdoor.PrivateOriginsObservation {
	return frontdoor.PrivateOriginsObservation{Origins: []frontdoor.PrivateOriginObservation{{State: state}}}
}

func assertObservationEvent(t *testing.T, recorder *events.FakeRecorder, want string) {
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
