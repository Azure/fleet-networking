/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// package internalserviceexport features the InternalServiceExport controller for reporting back conflict resolution
// status from the fleet to a member cluster.
package internalserviceexport

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/metrics"
)

const (
	// ControllerName is the name of the Reconciler.
	ControllerName = "internalserviceexport-controller"
)

var (
	// svcExportDuration is a Prometheus histogram metric bundle that measures that time it takes for
	// Fleet networking controllers to export a valid Service with no conflicts. That is, the
	// stopwatch starts when the ServiceExport controller marks a Service (via the ServiceExportValid
	// condition) as valid for export, and stops when the InternalServiceExport controller
	// reports back (via the ServiceExportConflict condition) from the hub cluster that no spec
	// conflict has been found with the conflict.
	//
	// Note that this measurement does not cover the time spent for Fleet networking controllers to
	// pick up Service or ServiceExport spec changes, as at this moment there is no way in Kubernetes API
	// for controllers to reliably track it.
	svcExportDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metrics.MetricsNamespace,
			Subsystem: metrics.MetricsSubsystem,
			Name:      "service_export_duration_milliseconds",
			Help:      "The duration of a service export",
			Buckets:   metrics.ExportDurationMillisecondsBuckets,
		},
		[]string{
			// The ID of the origin cluster, which exports the Service.
			"originClusterID",
		},
	)
)

func init() {
	// Register svcExportDuration (fleet_networking_service_export_duration_milliseconds) metric
	// with the controller runtime global metrics registry.
	ctrlmetrics.Registry.MustRegister(svcExportDuration)
}

// Reconciler reconciles the update of an InternalServiceExport.
type Reconciler struct {
	MemberClusterID string
	MemberClient    client.Client
	HubClient       client.Client
	Recorder        record.EventRecorder
}

//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=internalserviceexports,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=internalserviceexports/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceexports,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceexports/status,verbs=get;update;patch
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile reports back whether an export of a Service has been accepted with no conflict detected.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	internalSvcExportRef := klog.KRef(req.Namespace, req.Name)
	startTime := time.Now()
	klog.V(2).InfoS("Reconciliation starts", "internalServiceExport", internalSvcExportRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Reconciliation ends", "internalServiceExport", internalSvcExportRef, "latency", latency)
	}()

	// Retrieve the InternalServiceExport object.
	var internalSvcExport fleetnetv1alpha1.InternalServiceExport
	if err := r.HubClient.Get(ctx, req.NamespacedName, &internalSvcExport); err != nil {
		klog.ErrorS(err, "Failed to get internal svc export", "internalServiceExport", internalSvcExportRef)
		// Skip the reconciliation if the InternalServiceExport does not exist.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Check if the exported Service exists.
	svcNS := internalSvcExport.Spec.ServiceReference.Namespace
	svcName := internalSvcExport.Spec.ServiceReference.Name
	svcExportRef := klog.KRef(svcNS, svcName)
	var svcExport fleetnetv1alpha1.ServiceExport
	err := r.MemberClient.Get(ctx, types.NamespacedName{Namespace: svcNS, Name: svcName}, &svcExport)
	switch {
	case errors.IsNotFound(err):
		// The absence of ServiceExport suggests that the Service should not be, yet has been, exported. Normally
		// this situation will never happen as the ServiceExport controller guarantees, using the cleanup finalizer,
		// that a ServiceExport will only be deleted after the Service has been unexported. In some corner cases,
		// however, e.g. the user chooses to remove the finalizer explicitly, a Service can be left over in the hub
		// cluster, and it is up to this controller to remove it.
		klog.V(2).InfoS("Svc export does not exist; delete the internal svc export",
			"serviceExport", svcExportRef,
			"internalServiceExport", internalSvcExportRef,
		)
		if err := r.HubClient.Delete(ctx, &internalSvcExport); err != nil {
			klog.ErrorS(err, "Failed to delete internal svc export", "internalServiceExport", internalSvcExportRef)
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	case err != nil:
		// An unexpected error occurs.
		klog.ErrorS(err, "Failed to get svc export", "serviceExport", svcExportRef)
		return ctrl.Result{}, err
	}

	// Report back conflict resolution result.
	klog.V(4).InfoS("Report back conflict resolution result", "internalServiceExport", internalSvcExportRef)
	reported, err := r.reportBackConflictCondition(ctx, &svcExport, &internalSvcExport)
	if err != nil {
		klog.ErrorS(err, "Failed to report back conflict resolution result", "serviceExport", svcExportRef)
		return ctrl.Result{}, err
	}

	// Observe a data point for the svcExportDuration metric.
	// Note that an observation happens only when there is a conflict resolution result to report back.
	if reported {
		if err := r.observeMetrics(ctx, &internalSvcExport, time.Now()); err != nil {
			klog.ErrorS(err, "Failed to observe metrics", "internalServiceExport", internalSvcExportRef)
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager builds a controller with InternalSvcExportReconciler and sets it up with a
// (multi-namespaced) controller manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).For(&fleetnetv1alpha1.InternalServiceExport{}).Complete(r)
}

// reportBackConflictCond reports the ServiceExportConflict condition added to the InternalServiceExport object in the
// hub cluster back to the ServiceExport ojbect in the member cluster.
// It returns a bool value, reported, to signify whether a report-back has been completed.
func (r *Reconciler) reportBackConflictCondition(ctx context.Context,
	svcExport *fleetnetv1alpha1.ServiceExport,
	internalSvcExport *fleetnetv1alpha1.InternalServiceExport) (reported bool, err error) {
	internalSvcExportRef := klog.KRef(internalSvcExport.Namespace, internalSvcExport.Name)
	internalSvcExportConflictCond := meta.FindStatusCondition(internalSvcExport.Status.Conditions,
		string(fleetnetv1alpha1.ServiceExportConflict))
	if internalSvcExportConflictCond == nil {
		// No conflict condition to report back; this is the expected behavior when the conflict resolution process
		// has not completed yet.
		klog.V(4).InfoS("No conflict condition to report back", "internalServiceExport", internalSvcExportRef)
		return false, nil
	}

	svcExportConflictCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportConflict))
	if reflect.DeepEqual(internalSvcExportConflictCond, svcExportConflictCond) {
		// The conflict condition has not changed and there is no need to report back; this is also an expected
		// behavior.
		klog.V(4).InfoS("No update on the conflict condition", "internalServiceExport", internalSvcExportRef)
		// Return true here to allow following steps to run again upon retries.
		return true, nil
	}

	// Update the conditions
	if internalSvcExportConflictCond.Status == metav1.ConditionTrue {
		r.Recorder.Eventf(svcExport, corev1.EventTypeWarning, "ServiceExportConflictFound", "Service %s is in conflict with other exported services", svcExport.Name)
	}
	if internalSvcExportConflictCond.Status == metav1.ConditionFalse {
		r.Recorder.Eventf(svcExport, corev1.EventTypeNormal, "NoServiceExportConflictFound", "Service %s is exported without conflict", svcExport.Name)
	}
	meta.SetStatusCondition(&svcExport.Status.Conditions, *internalSvcExportConflictCond)
	return true, r.MemberClient.Status().Update(ctx, svcExport)
}

// Observe data points for metrics.
func (r *Reconciler) observeMetrics(ctx context.Context,
	internalSvcExport *fleetnetv1alpha1.InternalServiceExport,
	startTime time.Time) error {
	// Check if a metric data point has been observed for the current generation of the object; this helps guard
	// against repeated observation of metric data points for the same generation of an object due to no-op
	// reconciliations (e.g. resyncs, untracked changes).
	lastObservedGeneration, ok := internalSvcExport.Annotations[metrics.MetricsAnnotationLastObservedGeneration]
	currentGenerationStr := fmt.Sprintf("%d", internalSvcExport.Spec.ServiceReference.Generation)
	if ok && lastObservedGeneration == currentGenerationStr {
		// A data point has been observed for this generation; skip the observation.
		return nil
	}

	// Observe a new data point.

	// Annotate the object to track the last observed generation; this must happen before the actual observation.
	if internalSvcExport.Annotations == nil {
		// Initialize the annotation map if it is empty.
		internalSvcExport.Annotations = map[string]string{}
	}
	internalSvcExport.Annotations[metrics.MetricsAnnotationLastObservedGeneration] = currentGenerationStr
	if err := r.HubClient.Update(ctx, internalSvcExport); err != nil {
		return err
	}

	timeSpent := startTime.Sub(internalSvcExport.Spec.ServiceReference.ExportedSince.Time).Milliseconds()
	// Under some rare circumstances (such as user manipulating the timestamps; note that for this specific metric
	// clock drifts are less of an issue as all timestamps are from the same local lock), it could
	// happen that the valid timestamp of an ServiceExport appears later than its conflict resolution timestamp.
	// To avoid negative outliers affecting data analysis, this controller assigns a constant of exactly 1 second
	// when the calculated duration does not make sense.
	if timeSpent <= 0 {
		timeSpent = time.Second.Milliseconds() * 1
		klog.V(4).Info("A negative service export duration data point has been observed",
			"serviceNamespacedName", internalSvcExport.Spec.ServiceReference.NamespacedName,
			"originClusterID", internalSvcExport.Spec.ServiceReference.ClusterID)
	}
	// Similarly, to avoid large outliers skewing the stats (e.g. averages), this controller caps the data point
	// to a constant value.
	if timeSpent > int64(metrics.ExportDurationRightBound) {
		timeSpent = int64(metrics.ExportDurationRightBound)
	}
	svcExportDuration.WithLabelValues(r.MemberClusterID).Observe(float64(timeSpent))
	// TO-DO (chenyu1): Remove the metric logs when histogram metrics are supported in the backend.
	klog.V(2).InfoS("serviceExportDurationMilliseconds",
		"value", timeSpent,
		"originClusterID", r.MemberClusterID)
	return nil
}
