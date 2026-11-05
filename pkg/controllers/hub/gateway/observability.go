/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gateway

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/klog/v2"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"go.goms.io/fleet-networking/pkg/common/azureerrors"
	"go.goms.io/fleet-networking/pkg/common/metrics"
	"go.goms.io/fleet-networking/pkg/providers/azure/frontdoor"
)

const (
	originObservationSuccess      = "success"
	originObservationError        = "error"
	originObservationThrottled    = "throttled"
	profileCleanupPending         = "pending"
	profileCleanupAbsent          = "absent"
	afdOperationSuccess           = "success"
	afdOperationOwnershipConflict = "ownership_conflict"
	afdOperationThrottled         = "throttled"
	afdOperationError             = "error"
	graphStageDisabled            = "stage_disabled_graph"
	graphSuspendEmpty             = "suspend_empty"
	graphSuspendDenied            = "suspend_denied"
	graphReconcileHTTP            = "reconcile_http"
	graphSuspendInvalid           = "suspend_invalid"
	afdRequestTimedOut            = "request timed out"
)

func logGatewayOperationFailure(ctx context.Context, operation string, err error) {
	if err != nil {
		klog.FromContext(ctx).Error(nil, "Gateway operation failed", "operation", operation,
			"outcome", afdOperationOutcome(err), "cause", afdFailureCause(err))
	}
}

func logGatewayReconciliation(ctx context.Context, duration, requeueAfter time.Duration, err error) {
	logger := klog.FromContext(ctx)
	if err != nil {
		logger.Error(nil, "Gateway reconciliation failed", "duration", duration,
			"outcome", afdOperationOutcome(err), "cause", afdFailureCause(err))
		return
	}
	logger.V(2).Info("Gateway reconciliation completed", "duration", duration, "requeueAfter", requeueAfter)
}

func logGatewayObservation(ctx context.Context, observation *gatewayObservation, err error) {
	if err != nil {
		logGatewayOperationFailure(ctx, "observe_origins", err)
		return
	}
	if observation == nil {
		return
	}
	result := observation.result
	klog.FromContext(ctx).V(2).Info("Observed AFD private origins", "origins", len(result.Origins),
		"profileMissing", result.ProfileMissing, "originsConfigured", result.OriginsConfigured,
		"originsConverged", result.OriginsConverged, "pendingApprovals", len(result.PendingApprovals))
}

func logGatewayStaging(ctx context.Context, staging *gatewayStaging, err error) {
	if err != nil {
		logGatewayOperationFailure(ctx, "reconcile_graph", err)
		return
	}
	if staging == nil {
		return
	}
	logger := klog.FromContext(ctx)
	if staging.suspension != nil {
		logger.V(2).Info("AFD withdrawal progress", "state", staging.suspension.State,
			"referenceDenied", staging.denial != nil, "invalidIntent", staging.invalid)
		return
	}
	result := staging.result
	message := "AFD disabled graph progress"
	if staging.http != nil {
		result = staging.http.Staging
		message = "AFD private HTTP graph progress"
		logger = logger.WithValues("state", staging.http.State, "diagnostic", staging.http.Diagnostic)
	}
	logger.V(2).Info(message, "stagingRoots", result.Roots.State, "stagingRoute", result.Route.State,
		"stagingBackends", result.Route.Backends.State, "stagingInventoryVerified", result.InventoryVerified,
		"staleOriginsPending", result.StaleOriginsPending,
		"stagingPendingApprovals", len(result.Route.Backends.Origins.PendingApprovals))
}

func logPersistedGatewayStatus(ctx context.Context, gateway *gatewayv1.Gateway, err error) {
	if err != nil {
		logGatewayOperationFailure(ctx, "persist_status", err)
		return
	}
	if gateway == nil {
		return
	}
	logger := klog.FromContext(ctx)
	if condition := meta.FindStatusCondition(gateway.Status.Conditions, string(gatewayv1.GatewayConditionProgrammed)); condition != nil {
		logger = logger.WithValues("programmed", condition.Status, "reason", condition.Reason,
			"observedGeneration", condition.ObservedGeneration)
	}
	logger.V(2).Info("Persisted Gateway status", "addressCount", len(gateway.Status.Addresses))
}

var (
	originObservations = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metrics.MetricsNamespace,
		Subsystem: metrics.MetricsSubsystem,
		Name:      "gateway_afd_origin_observations_total",
		Help:      "Completed logical AFD origin-observer calls, not individual ARM requests or readiness.",
	}, []string{"outcome"})
	originObservationDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metrics.MetricsNamespace,
		Subsystem: metrics.MetricsSubsystem,
		Name:      "gateway_afd_origin_observation_duration_seconds",
		Help:      "Duration of completed logical AFD origin-observer calls, not approval wait time.",
	}, []string{"outcome"})
	profileCleanupCalls = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metrics.MetricsNamespace,
		Subsystem: metrics.MetricsSubsystem,
		Name:      "gateway_afd_profile_cleanup_calls_total",
		Help:      "Completed logical AFD profile-cleanup calls, not ARM requests, deletes or finalizer persistence.",
	}, []string{"outcome"})
	profileCleanupDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metrics.MetricsNamespace,
		Subsystem: metrics.MetricsSubsystem,
		Name:      "gateway_afd_profile_cleanup_duration_seconds",
		Help:      "Duration of completed logical AFD profile-cleanup calls, not total deletion wait time.",
	}, []string{"outcome"})
	graphOperationCalls = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metrics.MetricsNamespace,
		Subsystem: metrics.MetricsSubsystem,
		Name:      "gateway_afd_graph_operation_calls_total",
		Help:      "Completed logical AFD graph-operation calls, not ARM requests, readiness or withdrawal enforcement.",
	}, []string{"operation", "outcome"})
	graphOperationDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metrics.MetricsNamespace,
		Subsystem: metrics.MetricsSubsystem,
		Name:      "gateway_afd_graph_operation_duration_seconds",
		Help:      "Duration of completed logical AFD graph-operation calls, not provisioning or approval wait time.",
	}, []string{"operation", "outcome"})
)

func init() {
	ctrlmetrics.Registry.MustRegister(originObservations, originObservationDuration, profileCleanupCalls, profileCleanupDuration,
		graphOperationCalls, graphOperationDuration)
}

func afdOperationOutcome(err error) string {
	var conflict *frontdoor.OwnershipConflictError
	switch {
	case errors.As(err, &conflict):
		return afdOperationOwnershipConflict
	case azureerrors.IsThrottled(err):
		return afdOperationThrottled
	case err != nil:
		return afdOperationError
	default:
		return afdOperationSuccess
	}
}

// Keep event causes bounded and independent of SDK bodies or arbitrary error text.
func afdFailureCause(err error) string {
	var conflict *frontdoor.OwnershipConflictError
	var response *azcore.ResponseError
	var network net.Error
	switch {
	case errors.As(err, &conflict):
		return "resource ownership conflict"
	case errors.Is(err, context.DeadlineExceeded), apierrors.IsTimeout(err):
		return afdRequestTimedOut
	case errors.Is(err, context.Canceled):
		return "request canceled"
	case azureerrors.IsThrottled(err), apierrors.IsTooManyRequests(err):
		return "request throttled"
	case azureerrors.IsForbidden(err), apierrors.IsForbidden(err):
		return "authorization denied"
	case apierrors.IsUnauthorized(err):
		return "authentication failed"
	case azureerrors.IsNotFound(err), apierrors.IsNotFound(err):
		return "resource not found"
	case azureerrors.IsConflict(err), apierrors.IsConflict(err):
		return "resource update conflict"
	case errors.As(err, &response):
		switch {
		case response.StatusCode == http.StatusUnauthorized:
			return "authentication failed"
		case response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusGatewayTimeout:
			return afdRequestTimedOut
		case response.StatusCode >= http.StatusInternalServerError:
			return "Azure service unavailable"
		case response.StatusCode >= http.StatusBadRequest:
			return "Azure request rejected"
		}
	case errors.As(err, &network):
		if network.Timeout() {
			return afdRequestTimedOut
		}
		return "network request failed"
	}
	return "unclassified operation failure"
}

func profileCleanupOutcome(absent bool, err error) string {
	if err != nil {
		return afdOperationOutcome(err)
	}
	if absent {
		return profileCleanupAbsent
	}
	return profileCleanupPending
}

func recordProfileCleanup(duration time.Duration, absent bool, err error) {
	outcome := profileCleanupOutcome(absent, err)
	profileCleanupCalls.WithLabelValues(outcome).Inc()
	profileCleanupDuration.WithLabelValues(outcome).Observe(duration.Seconds())
}

func (r *Reconciler) recordProfileCleanupFailure(gateway *gatewayv1.Gateway, err error) {
	if r.Recorder == nil {
		return
	}
	if afdOperationOutcome(err) == afdOperationOwnershipConflict {
		r.Recorder.Eventf(gateway, nil, corev1.EventTypeWarning, "AFDProfileCleanupOwnershipConflict", "CleanupAFDProfile",
			"AFD profile cleanup blocked: resource ownership conflict; cleanup finalizer retained. A platform operator must verify ownership; do not bypass cleanup.")
		return
	}
	r.Recorder.Eventf(gateway, nil, corev1.EventTypeWarning, "AFDProfileCleanupFailed", "CleanupAFDProfile",
		"AFD profile cleanup failed: %s; cleanup finalizer retained for retry.", afdFailureCause(err))
}

func (r *Reconciler) recordGraphOperation(gateway *gatewayv1.Gateway, operation string, duration time.Duration, err error) {
	outcome := afdOperationOutcome(err)
	graphOperationCalls.WithLabelValues(operation, outcome).Inc()
	graphOperationDuration.WithLabelValues(operation, outcome).Observe(duration.Seconds())
	if err == nil || r.Recorder == nil {
		return
	}
	reason := "AFDDisabledGraphStagingFailed"
	message := "AFD disabled-graph staging failed: %s. No new ingress readiness is established."
	if operation == graphReconcileHTTP {
		reason = "AFDHTTPProvisioningFailed"
		message = "AFD private HTTP provisioning failed: %s. No new ingress readiness is established."
	} else if operation != graphStageDisabled {
		reason = "AFDEndpointSuspensionFailed"
		message = "AFD endpoint suspension failed: %s. Withdrawal enforcement is not established by this attempt."
	}
	if outcome == afdOperationOwnershipConflict {
		reason = "AFDGraphOperationOwnershipConflict"
		message = "AFD graph operation blocked: %s. A platform operator must verify ownership; no new readiness or withdrawal enforcement is established."
	}
	// These events describe failed calls, not persisted status or successful enforcement.
	r.Recorder.Eventf(gateway, nil, corev1.EventTypeWarning, reason, "ReconcileAFDGraph", message, afdFailureCause(err))
}

func recordOriginObservation(duration time.Duration, err error) {
	outcome := originObservationSuccess
	if azureerrors.IsThrottled(err) {
		outcome = originObservationThrottled
	} else if err != nil {
		outcome = originObservationError
	}
	originObservations.WithLabelValues(outcome).Inc()
	originObservationDuration.WithLabelValues(outcome).Observe(duration.Seconds())
}

func (r *Reconciler) recordHTTPPreviewEvent(gateway *gatewayv1.Gateway, result *frontdoor.HTTPPreviewProvisioningResult) {
	if r.Recorder == nil {
		return
	}
	if result.State == frontdoor.HTTPPreviewProgrammed {
		r.Recorder.Eventf(gateway, nil, corev1.EventTypeNormal, "AFDHTTPProgrammed", "ProgramAFDHTTP",
			"Observed complete enabled AFD Premium private HTTP graph and published its Azure hostname; live application health is not implied.")
	} else if len(result.Staging.Route.Backends.Origins.PendingApprovals) != 0 {
		r.Recorder.Eventf(gateway, nil, corev1.EventTypeNormal, "AFDPrivateLinkApprovalRequired", "ProgramAFDHTTP",
			"AFD Private Link approval is asynchronous; an authorized PLS owner must approve pending shared targets. Inspect Gateway status; approval automation is not available.")
	}
}

func (r *Reconciler) recordOriginObservationEvent(gateway *gatewayv1.Gateway, observation *gatewayObservation) {
	if r.Recorder == nil {
		return
	}
	if observation.err != nil {
		r.Recorder.Eventf(gateway, nil, corev1.EventTypeWarning, "AFDOriginObservationFailed", "ObserveAFDOrigins",
			"AFD origin observation failed: %s. No new origin readiness is established.", afdFailureCause(observation.err))
		return
	}
	if observation.result.ProfileMissing {
		return
	}
	for _, origin := range observation.result.Origins {
		switch origin.State {
		case frontdoor.PrivateOriginRejected, frontdoor.PrivateOriginDisconnected, frontdoor.PrivateOriginTimedOut,
			frontdoor.PrivateOriginFailed, frontdoor.PrivateOriginDrifted:
			r.Recorder.Eventf(gateway, nil, corev1.EventTypeWarning, "AFDPrivateOriginsNotReady", "ObserveAFDOrigins",
				"Observed AFD private origin state %s; inspect Gateway status. Ingress readiness is not established.", origin.State)
			return
		}
	}
	if len(observation.result.PendingApprovals) != 0 {
		r.Recorder.Eventf(gateway, nil, corev1.EventTypeNormal, "AFDPrivateLinkApprovalRequired", "ObserveAFDOrigins",
			"AFD Private Link approval is asynchronous; an authorized PLS owner must approve pending shared targets. Inspect Gateway status; approval automation is not available.")
	} else if observation.result.OriginsConverged {
		r.Recorder.Eventf(gateway, nil, corev1.EventTypeNormal, "AFDPrivateOriginsConverged", "ObserveAFDOrigins",
			"Observed private origins are approved and deployed; full Azure graph programming and ingress readiness are not established.")
	}
}
