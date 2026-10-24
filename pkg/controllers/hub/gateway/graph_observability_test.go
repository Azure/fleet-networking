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
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"go.goms.io/fleet-networking/pkg/providers/azure/frontdoor"
)

const (
	graphCounterName       = "fleet_networking_gateway_afd_graph_operation_calls_total"
	graphDurationName      = "fleet_networking_gateway_afd_graph_operation_duration_seconds"
	testStageOperation     = "stage_disabled_graph"
	testEmptyOperation     = "suspend_empty"
	testDeniedOperation    = "suspend_denied"
	testReconcileOperation = "reconcile_http"
	testInvalidOperation   = "suspend_invalid"
	graphOwnershipEvent    = "Warning AFDGraphOperationOwnershipConflict"
	graphStagingEvent      = "Warning AFDDisabledGraphStagingFailed"
	graphSuspensionEvent   = "Warning AFDEndpointSuspensionFailed"
)

func TestGraphOperationTelemetryOutcomes(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		outcome string
	}{
		{"pending or incomplete result is a successful call", nil, afdOperationSuccess},
		{"ordinary failure", errors.New(privateErrorPayload), afdOperationError},
		{"wrapped Azure 429", fmt.Errorf("operation: %w", &azcore.ResponseError{StatusCode: http.StatusTooManyRequests}), afdOperationThrottled},
		{"wrapped ownership conflict", fmt.Errorf("operation: %w", &frontdoor.OwnershipConflictError{
			ProfileID: privateErrorPayload, Reason: privateErrorPayload,
		}), afdOperationOwnershipConflict},
	}
	for _, operation := range []string{testStageOperation, testEmptyOperation, testDeniedOperation, testReconcileOperation, testInvalidOperation} {
		for _, tt := range tests {
			t.Run(operation+"/"+tt.name, func(t *testing.T) {
				c, gateway := graphTelemetryFixture(t, operation)
				recorder := record.NewFakeRecorder(10)
				provisioner := &fakeHTTPProvisioner{progress: frontdoor.HTTPPreviewProvisioningResult{State: frontdoor.HTTPPreviewPending}}
				stager := &provisioner.fakeGraphStager
				stager.err = tt.err
				r := &Reconciler{Client: c, APIReader: c, ProfileCleanup: &fakeProfileCleaner{}, GraphStager: stager, Recorder: recorder}
				if operation == testReconcileOperation || operation == testInvalidOperation {
					r.GraphStager, r.HTTPProvisioner = nil, provisioner
				}
				before := graphMetricSamples(t, operation, tt.outcome)
				totalBefore := graphMetricSamples(t, "", "")
				for poll := 0; poll < 2; poll++ {
					_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)})
					if !errors.Is(err, tt.err) {
						t.Fatalf("provider error not preserved: got %v, want %v", err, tt.err)
					}
					want := ""
					if tt.err != nil {
						want = graphFailureEvent(operation, tt.outcome)
					}
					assertObservationEvent(t, recorder, want)
				}
				assertGraphMetricDelta(t, before, graphMetricSamples(t, operation, tt.outcome), 2)
				assertGraphMetricDelta(t, totalBefore, graphMetricSamples(t, "", ""), 2)
				if stager.calls+stager.suspensions != 2 {
					t.Fatalf("provider calls = %#v, want two logical calls", stager)
				}
				if err := c.Get(context.Background(), client.ObjectKeyFromObject(gateway), gateway); err != nil {
					t.Fatal(err)
				}
				pending := meta.FindStatusCondition(gateway.Status.Conditions, "Programmed")
				if pending == nil || pending.Status != "False" || len(gateway.Status.Addresses) != 0 {
					t.Fatalf("telemetry changed ingress readiness: %#v", gateway.Status)
				}
			})
		}
	}
}

func TestGraphOperationTelemetryRetriesAndDiscard(t *testing.T) {
	tests := []struct {
		name          string
		conflict      bool
		patchFail     bool
		stale         bool
		providerError bool
	}{
		{name: "successful status retry", conflict: true},
		{name: "failed status patch", patchFail: true},
		{name: "stale result", stale: true},
		{name: "provider and status failure", patchFail: true, providerError: true},
	}
	for _, operation := range []string{testStageOperation, testEmptyOperation, testDeniedOperation} {
		for _, tt := range tests {
			t.Run(operation+"/"+tt.name, func(t *testing.T) {
				c, gateway := graphTelemetryFixture(t, operation)
				recorder := record.NewFakeRecorder(10)
				stager := &fakeGraphStager{}
				if tt.providerError {
					stager.err = errors.New(privateErrorPayload)
				}
				if tt.stale {
					stager.during = func() {
						if err := c.Get(context.Background(), client.ObjectKeyFromObject(gateway), gateway); err != nil {
							t.Fatal(err)
						}
						gateway.Generation++
						if err := c.Update(context.Background(), gateway); err != nil {
							t.Fatal(err)
						}
					}
				}
				patches := 0
				writer := interceptor.NewClient(c.(client.WithWatch), interceptor.Funcs{
					SubResourcePatch: func(ctx context.Context, cl client.Client, resource string, object client.Object,
						patch client.Patch, options ...client.SubResourcePatchOption) error {
						patches++
						if tt.patchFail {
							return errors.New("status unavailable")
						}
						if tt.conflict && patches == 1 {
							return apierrors.NewConflict(schema.GroupResource{Resource: ownershipGatewayResource}, object.GetName(), errors.New("conflict"))
						}
						return cl.SubResource(resource).Patch(ctx, object, patch, options...)
					},
				})
				before := graphMetricSamples(t, "", "")
				r := &Reconciler{Client: writer, APIReader: c, ProfileCleanup: &fakeProfileCleaner{}, GraphStager: stager, Recorder: recorder}
				_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)})
				if (err != nil) != tt.patchFail || (tt.providerError && !errors.Is(err, stager.err)) {
					t.Fatalf("unexpected reconcile error: %v", err)
				}
				want := ""
				if tt.providerError {
					want = graphFailureEvent(operation, afdOperationError)
				}
				assertObservationEvent(t, recorder, want)
				assertGraphMetricDelta(t, before, graphMetricSamples(t, "", ""), 1)
				if stager.calls+stager.suspensions != 1 || (tt.conflict && patches != 2) {
					t.Fatalf("status retries replayed operation: provider=%#v patches=%d", stager, patches)
				}
			})
		}
	}
}

func TestGraphOperationTelemetrySkipsPrerequisites(t *testing.T) {
	const missingStagerCase = "stager"
	for _, operation := range []string{testStageOperation, testEmptyOperation, testDeniedOperation} {
		for _, missing := range []string{missingStagerCase, "reader", "cleaner"} {
			t.Run(operation+"/"+missing, func(t *testing.T) {
				c, gateway := graphTelemetryFixture(t, operation)
				recorder := record.NewFakeRecorder(10)
				stager := &fakeGraphStager{}
				r := &Reconciler{Client: c, APIReader: c, ProfileCleanup: &fakeProfileCleaner{}, GraphStager: stager, Recorder: recorder}
				switch missing {
				case missingStagerCase:
					r.GraphStager = nil
				case "reader":
					r.APIReader = nil
				case "cleaner":
					r.ProfileCleanup = nil
				}
				before := graphMetricSamples(t, "", "")
				_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)})
				if (err != nil) != (missing != missingStagerCase) {
					t.Fatalf("unexpected prerequisite error: %v", err)
				}
				assertGraphMetricDelta(t, before, graphMetricSamples(t, "", ""), 0)
				assertObservationEvent(t, recorder, "")
				if stager.calls+stager.suspensions != 0 {
					t.Fatalf("skipped prerequisite called provider: %#v", stager)
				}
			})
		}
	}
}

func TestGraphOperationDurationUsesSeconds(t *testing.T) {
	recorder := record.NewFakeRecorder(1)
	r := &Reconciler{Recorder: recorder}
	before := graphMetricSamples(t, testStageOperation, afdOperationSuccess)
	r.recordGraphOperation(&gatewayv1.Gateway{}, testStageOperation, 250*time.Millisecond, nil)
	after := graphMetricSamples(t, testStageOperation, afdOperationSuccess)
	assertGraphMetricDelta(t, before, after, 1)
	if math.Abs(after.seconds-before.seconds-0.25) > 1e-9 {
		t.Fatalf("duration sample = %v, want 0.25 seconds", after.seconds-before.seconds)
	}
	assertObservationEvent(t, recorder, "")
}

func graphTelemetryFixture(t *testing.T, operation string) (client.Client, *gatewayv1.Gateway) {
	t.Helper()
	switch operation {
	case testStageOperation, testReconcileOperation:
		c, gateway, _, _ := gatewayFixture(t, 1)
		addObservationHub(t, c)
		return c, gateway
	case testEmptyOperation:
		c, gateway, _ := ownedEmptyGateway(t)
		return c, gateway
	case testDeniedOperation:
		c, gateway, _, grant := referenceWithdrawalFixture(t)
		if err := c.Delete(context.Background(), grant); err != nil {
			t.Fatal(err)
		}
		return c, gateway
	case testInvalidOperation:
		c, gateway, route := ownedEmptyGateway(t)
		route.ResourceVersion = ""
		if err := c.Create(context.Background(), route); err != nil {
			t.Fatal(err)
		}
		gateway.Spec.Listeners[0].Port = 81
		if err := c.Update(context.Background(), gateway); err != nil {
			t.Fatal(err)
		}
		return c, gateway
	default:
		t.Fatalf("unsupported test operation %q", operation)
		return nil, nil
	}
}

func graphFailureEvent(operation, outcome string) string {
	if outcome == afdOperationOwnershipConflict {
		return graphOwnershipEvent
	}
	if operation == testStageOperation {
		return graphStagingEvent
	}
	if operation == testReconcileOperation {
		return "Warning AFDHTTPProvisioningFailed"
	}
	return graphSuspensionEvent
}

func assertGraphMetricDelta(t *testing.T, before, after observationSamples, calls float64) {
	t.Helper()
	if after.calls-before.calls != calls || after.durations-before.durations != calls {
		t.Fatalf("graph operation samples = %v -> %v, want %v calls/durations", before, after, calls)
	}
}

func graphMetricSamples(t *testing.T, operation, outcome string) observationSamples {
	t.Helper()
	families, err := ctrlmetrics.Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var samples observationSamples
	for _, family := range families {
		if family.GetName() != graphCounterName && family.GetName() != graphDurationName {
			continue
		}
		for _, metric := range family.Metric {
			if len(metric.Label) != 2 || metric.Label[0].GetName() != "operation" || metric.Label[1].GetName() != "outcome" {
				t.Fatalf("unbounded/unexpected graph metric labels: %v", metric.Label)
			}
			op, result := metric.Label[0].GetValue(), metric.Label[1].GetValue()
			switch op {
			case testStageOperation, testEmptyOperation, testDeniedOperation, testReconcileOperation, testInvalidOperation:
			default:
				t.Fatalf("unexpected operation label %q", op)
			}
			switch result {
			case afdOperationSuccess, afdOperationError, afdOperationThrottled, afdOperationOwnershipConflict:
			default:
				t.Fatalf("unexpected outcome label %q", result)
			}
			if (operation == "" || operation == op) && (outcome == "" || outcome == result) {
				samples.calls += metric.GetCounter().GetValue()
				samples.durations += float64(metric.GetHistogram().GetSampleCount())
				samples.seconds += metric.GetHistogram().GetSampleSum()
			}
		}
	}
	return samples
}
