/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gateway

import (
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
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
)

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
		r.Recorder.Event(gateway, corev1.EventTypeWarning, "AFDProfileCleanupOwnershipConflict",
			"AFD profile ownership could not be verified; cleanup finalizer retained. A platform operator must inspect ownership and controller logs; do not remove the finalizer to bypass cleanup.")
		return
	}
	r.Recorder.Event(gateway, corev1.EventTypeWarning, "AFDProfileCleanupFailed",
		"AFD profile cleanup failed; cleanup finalizer retained for retry. Inspect controller logs and Azure permissions or availability.")
}

func (r *Reconciler) recordGraphOperation(gateway *gatewayv1.Gateway, operation string, duration time.Duration, err error) {
	outcome := afdOperationOutcome(err)
	graphOperationCalls.WithLabelValues(operation, outcome).Inc()
	graphOperationDuration.WithLabelValues(operation, outcome).Observe(duration.Seconds())
	if err == nil || r.Recorder == nil {
		return
	}
	reason := "AFDDisabledGraphStagingFailed"
	message := "AFD disabled-graph staging call failed; inspect controller logs. No new ingress readiness is established."
	if operation == graphReconcileHTTP {
		reason = "AFDHTTPProvisioningFailed"
		message = "AFD private HTTP provisioning call failed; inspect controller logs. No new ingress readiness is established."
	} else if operation != graphStageDisabled {
		reason = "AFDEndpointSuspensionFailed"
		message = "AFD endpoint suspension call failed; inspect controller logs. Withdrawal enforcement is not established by this attempt."
	}
	if outcome == afdOperationOwnershipConflict {
		reason = "AFDGraphOperationOwnershipConflict"
		message = "AFD graph operation ownership could not be verified; a platform operator must inspect resource ownership and controller logs. No new readiness or withdrawal enforcement is established."
	}
	// These events describe failed calls, not persisted status or successful enforcement.
	r.Recorder.Event(gateway, corev1.EventTypeWarning, reason, message)
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
		r.Recorder.Event(gateway, corev1.EventTypeNormal, "AFDHTTPProgrammed",
			"Observed complete enabled AFD Premium private HTTP graph and published its Azure hostname; live application health is not implied.")
	} else if len(result.Staging.Route.Backends.Origins.PendingApprovals) != 0 {
		r.Recorder.Event(gateway, corev1.EventTypeNormal, "AFDPrivateLinkApprovalRequired",
			"AFD Private Link approval is asynchronous; an authorized PLS owner must approve pending shared targets. Inspect Gateway status; approval automation is not available.")
	}
}

func (r *Reconciler) recordOriginObservationEvent(gateway *gatewayv1.Gateway, observation *gatewayObservation) {
	if r.Recorder == nil {
		return
	}
	if observation.err != nil {
		r.Recorder.Event(gateway, corev1.EventTypeWarning, "AFDOriginObservationFailed",
			"AFD origin observation failed; inspect Gateway status and controller logs. No new origin readiness is established.")
		return
	}
	if observation.result.ProfileMissing {
		return
	}
	for _, origin := range observation.result.Origins {
		switch origin.State {
		case frontdoor.PrivateOriginRejected, frontdoor.PrivateOriginDisconnected, frontdoor.PrivateOriginTimedOut,
			frontdoor.PrivateOriginFailed, frontdoor.PrivateOriginDrifted:
			r.Recorder.Event(gateway, corev1.EventTypeWarning, "AFDPrivateOriginsNotReady",
				fmt.Sprintf("Observed AFD private origin state %s; inspect Gateway status. Ingress readiness is not established.", origin.State))
			return
		}
	}
	if len(observation.result.PendingApprovals) != 0 {
		r.Recorder.Event(gateway, corev1.EventTypeNormal, "AFDPrivateLinkApprovalRequired",
			"AFD Private Link approval is asynchronous; an authorized PLS owner must approve pending shared targets. Inspect Gateway status; approval automation is not available.")
	} else if observation.result.OriginsConverged {
		r.Recorder.Event(gateway, corev1.EventTypeNormal, "AFDPrivateOriginsConverged",
			"Observed private origins are approved and deployed; full Azure graph programming and ingress readiness are not established.")
	}
}
