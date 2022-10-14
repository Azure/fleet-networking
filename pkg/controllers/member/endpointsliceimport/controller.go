/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package endpointsliceimport features the EndpointSliceImport controller for importing
// EndpointSlices from hub cluster into a member cluster.
package endpointsliceimport

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/metrics"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
)

const (
	// controllerID helps identify that imported EndpointSlices are managed by this controller.
	controllerID                        = "endpointsliceimport-controller.networking.fleet.azure.com"
	endpointSliceImportCleanupFinalizer = "networking.fleet.azure.com/endpointsliceimport-cleanup"

	mcsServiceImportRefFieldKey = ".spec.serviceImport.name"

	endpointSliceImportRetryInterval = time.Second * 2
)

var (
	// endpointSliceExportImportDuration is a Prometheus histogram metric bundle that measures the time it takes for
	// Fleet networking controllers to export an EndpointSlice from one cluster in the fleet, distribute it, and
	// finally import the EndpointSlice into its destination cluster in the fleet. The stopwatch starts when an
	// EndpointSlice is ready for export (as an EndpointSliceExport in the hub cluster), and stops when the
	// EndpointSlice is successfully imported (from an EndpointSlicdeImport in the hub cluster).
	//
	// Note that this measurement does not cover the time spent for Fleet networking controllers to pick EndpointSlice
	// spec changes, as at this moment there is no way in Kubernetes API for controllers to reliably track it. Nor
	// does it cover the time needed for load balancer to actually expose endpoints in the EndpointSlice for traffic.
	endpointSliceExportImportDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metrics.MetricsNamespace,
			Subsystem: metrics.MetricsSubsystem,
			Name:      "endpointslice_export_import_duration_milliseconds",
			Help:      "The duration of an endpointslice export",
			Buckets:   metrics.ExportDurationMillisecondsBuckets,
		},
		[]string{
			// The ID of the origin cluster, which exports the Service and the EndpointSlice.
			"originClusterID",
			// The ID of the destination cluster, which imports the Service and the EndpointSlice.
			"destinationClusterID",
		},
	)
)

func init() {
	// Register endpointSliceExportImportDuration (endpointslice_export_import_duration_milliseconds) metric
	// with the controller runtime global metrics registry.
	ctrlmetrics.Registry.MustRegister(endpointSliceExportImportDuration)
}

// Reconciler reconciles an EndpointSliceImport.
type Reconciler struct {
	MemberClusterID string
	MemberClient    client.Client
	HubClient       client.Client
	// The namespace reserved for fleet resources in the member cluster.
	FleetSystemNamespace string
}

//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=endpointsliceimports,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=discovery.k8s.io,resources=endpointslices,verbs=get;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=multiclusterservices,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=services,verbs=get;list

// Reconcile imports an EndpointSlice from hub cluster.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	endpointSliceImportRef := klog.KRef(req.Namespace, req.Name)
	startTime := time.Now()
	klog.V(2).InfoS("Reconciliation starts", "endpointSliceImport", endpointSliceImportRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Reconciliation ends", "endpointSliceImport", endpointSliceImportRef, "latency", latency)
	}()

	// Retrieve the EndpointSliceImport.
	endpointSliceImport := &fleetnetv1alpha1.EndpointSliceImport{}
	if err := r.HubClient.Get(ctx, req.NamespacedName, endpointSliceImport); err != nil {
		klog.ErrorS(err, "Failed to get endpoint slice import", "endpointSliceImport", endpointSliceImportRef)
		// Skip the reconciliation if the EndpointSliceImport does not exist; this should only happen when an
		// EndpointSliceImport is deleted before the controller gets a chance to reconcile it, which
		// requires no action to take on this controller's end.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Check if the EndpointSliceImport has been deleted and needs cleanup (unimport EndpointSlice).
	// An EndpointSliceImport needs cleanup when it has the EndpointSliceImport cleanup finalizer added;
	// the absence of this finalizer guarantees that the EndpointSliceImport has never been imported.
	endpointSliceRef := klog.KRef(r.FleetSystemNamespace, req.Name)
	if endpointSliceImport.DeletionTimestamp != nil {
		klog.V(2).InfoS("EndpointSliceImport is deleted; unimport EndpointSlice",
			"endpointSliceImport", endpointSliceImportRef,
			"endpointSlice", endpointSliceRef)
		if err := r.unimportEndpointSlice(ctx, endpointSliceImport); err != nil {
			klog.ErrorS(err, "Failed to unimport EndpointSlice",
				"endpointSliceImport", endpointSliceImportRef,
				"endpointSlice", endpointSliceRef)
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Import the EndpointSlice, or update an imported EndpointSlice.

	// Inquire the corresponding MCS to find out which Service the imported EndpointSlice should associate with.

	// List all MCSes that attempt to import the Service owning the EndpointSlice.
	multiClusterSvcList := &fleetnetv1alpha1.MultiClusterServiceList{}
	ownerSvcNS := endpointSliceImport.Spec.OwnerServiceReference.Namespace
	ownerSvcName := endpointSliceImport.Spec.OwnerServiceReference.Name
	err := r.MemberClient.List(ctx,
		multiClusterSvcList,
		client.InNamespace(ownerSvcNS),
		client.MatchingFields{mcsServiceImportRefFieldKey: ownerSvcName})
	switch {
	case err != nil:
		// An unexpected error occurs.
		klog.ErrorS(err, "Failed to list MCS",
			"serviceImport", klog.KRef(ownerSvcNS, ownerSvcName),
			"endpointSliceImport", endpointSliceImportRef)
		return ctrl.Result{}, err
	case len(multiClusterSvcList.Items) == 0:
		// No matching MCS is found; typically this will never happen as the hub cluster will only distribute
		// EndpointSlices to member clusters that have requested them with an MCS. It could be that the controller
		// sees an in-between state where a Service is imported and then immediately unimported, and the hub cluster
		// does not get to retract distributed EndpointSlices in time. In this case the controller will skip
		// importing the EndpointSlice.
		klog.V(2).InfoS("No matching MCS is found; EndpointSlice will not be imported",
			"serviceImport", klog.KRef(ownerSvcNS, ownerSvcName),
			"endpointSliceImport", endpointSliceImportRef)
		return ctrl.Result{}, nil
	}

	// Scan all matching MCSes: inspect each MCS for the derived Service label; if one is present, it signals
	// that the MCS has successfully imported the Service owning the imported EndpointSlice, which the controller
	// will use for EndpointSlice association.
	// At this moment, the scan uses a first-match logic, as it is guaranteed that if multiple MCSes, either from
	// one member cluster or from multiple clusters from the fleet, attempt to import the same Service, it is
	// guaranteed that only one will succeed.
	derivedSvcName := scanForDerivedServiceName(multiClusterSvcList)

	// Verify if the found derived Service label points to a Service that the controller can associate the
	// EndpointSlice with. In most cases this check will always pass as the hub cluster will only distribute
	// EndpointSlices to member clusters that have requested them with an MCS; still, there are some corner
	// cases that the controller must guard against, such as:
	// * user has tampered with the derived Service label; or
	// * the controller sees an in-between state where a Service is imported and then immediately unimported,
	//   and the hub cluster does not get to retract distributed EndpointSlices in time; or
	// * a connectivity issue has kept the member cluster out of sync with the hub cluster, with the member cluster
	//   not knowing that a Service has been successfully claimed by itself; or
	// * the controller for processing MCSes lags, and has not created the derived Service in time.
	isValid, err := r.isDerivedServiceValid(ctx, derivedSvcName)
	switch {
	case err != nil:
		klog.ErrorS(err, "Failed to check if derived Service is valid",
			"derivedServiceName", derivedSvcName,
			"endpointSliceImport", endpointSliceImportRef)
		return ctrl.Result{}, err
	case !isValid:
		// Retry importing the EndpointSlice at a later time if no valid derived Service can be found.
		klog.V(2).InfoS("No valid derived Service; will retry importing EndpointSlice later",
			"derivedServiceName", derivedSvcName,
			"endpointSliceImport", endpointSliceImportRef)
		return ctrl.Result{RequeueAfter: endpointSliceImportRetryInterval}, nil
	}

	// Special note:
	// There exists a corner case where an MCS that imports a specific Service have multiple derived Services created;
	// this is usually the result of direct label manipulation on the user's end. Ideally, this controller should watch
	// for changes on MCS resources and (re)associate imported EndpointSlices to the latest dervied Service in use;
	// however, unfortunately, with the current implementation of controller-runtime package, it is not possible
	// for the controller to watch for resources on two different directions (member cluster and hub cluster),
	// and as a result, an imported EndpointSlice could be bound to a derived Service that is no longer in use.
	// Periodic resyncs can help address this issue, but it may take a quite long while before the situation is
	// corrected, should this corner case happens.

	// Add the cleanup finalizer (if one has not been added earlier); this must happen before
	// the EndpointSlice is imported.
	klog.V(2).InfoS("Add cleanup finalizer to EndpointSliceImport", "endpointSliceImport", endpointSliceImportRef)
	if err := r.addEndpointSliceImportCleanupFinalizer(ctx, endpointSliceImport); err != nil {
		klog.ErrorS(err, "Failed to add cleanup finalizer to EndpointSliceImport", "endpointSliceImport", endpointSliceImportRef)
		return ctrl.Result{}, err
	}

	// Associate the EndpointSlice with the Service.
	klog.V(2).InfoS("Import the EndpointSlice", "endpointSlice", endpointSliceRef)
	endpointSlice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: r.FleetSystemNamespace,
			Name:      endpointSliceImport.Name,
		},
	}
	if op, err := controllerutil.CreateOrUpdate(ctx, r.MemberClient, endpointSlice, func() error {
		formatEndpointSliceFromImport(endpointSlice, derivedSvcName, endpointSliceImport)
		return nil
	}); err != nil {
		klog.ErrorS(err, "Failed to create/update EndpointSlice",
			"endpointSlice", endpointSliceRef,
			"op", op,
			"endpointSliceImport", endpointSliceImportRef)
		return ctrl.Result{}, err
	}

	// Observe a data point for the EndpointSliceExportImportDuration metric.
	if err := r.observeMetrics(ctx, endpointSliceImport, time.Now()); err != nil {
		klog.ErrorS(err, "Failed to observe metrics", "endpointSliceImport", endpointSliceImportRef)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager builds a controller with Reconciler and sets it up with a controller manager.
func (r *Reconciler) SetupWithManager(ctx context.Context, memberCtrlMgr, hubCtrlMgr ctrl.Manager) error {
	// Set up an index for efficient MCS lookup **on the controller manager for member cluster controllers**.
	indexerFunc := func(o client.Object) []string {
		multiClusterSvc, ok := o.(*fleetnetv1alpha1.MultiClusterService)
		if !ok {
			return []string{}
		}
		return []string{multiClusterSvc.Spec.ServiceImport.Name}
	}
	if err := memberCtrlMgr.GetFieldIndexer().IndexField(ctx,
		&fleetnetv1alpha1.MultiClusterService{},
		mcsServiceImportRefFieldKey,
		indexerFunc,
	); err != nil {
		return err
	}

	// The controller itself is managed by the controller manager for hub cluster controllers.
	return ctrl.NewControllerManagedBy(hubCtrlMgr).
		// The EndpointSliceImport controller watches over EndpointSliceImport objects.
		For(&fleetnetv1alpha1.EndpointSliceImport{}).
		Complete(r)
}

// unimportEndpointSlice unimports an EndpointSlice.
func (r *Reconciler) unimportEndpointSlice(ctx context.Context, endpointSliceImport *fleetnetv1alpha1.EndpointSliceImport) error {
	// Skip the unimporting if the cleanup finalizer is not present on the EndpointSliceImport; the absence of this
	// finalizer guarantees that the EndpointSlice has never been imported.
	if !controllerutil.ContainsFinalizer(endpointSliceImport, endpointSliceImportCleanupFinalizer) {
		return nil
	}

	// Unimport the EndpointSlice.
	endpointSlice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: r.FleetSystemNamespace,
			Name:      endpointSliceImport.Name,
		},
	}
	if err := r.MemberClient.Delete(ctx, endpointSlice); err != nil && !errors.IsNotFound(err) {
		// It is guaranteed that a finalizer is always added before an EndpointSlice is imported; in some rare
		// occasions it could happen that an EndpointSliceImport has a finalizer added yet the corresponding
		// EndpointSlice has not been imported in the member cluster. It is an expected behavior and no action
		// is needed on this controller's end.
		return err
	}

	// Remove the EndpointSliceImport cleanup finalizer.
	controllerutil.RemoveFinalizer(endpointSliceImport, endpointSliceImportCleanupFinalizer)
	return r.HubClient.Update(ctx, endpointSliceImport)
}

// addEndpointSliceImportCleanupFinalizer adds the cleanup finalizer to an EndpointSliceImport.
func (r *Reconciler) addEndpointSliceImportCleanupFinalizer(ctx context.Context, endpointSliceImport *fleetnetv1alpha1.EndpointSliceImport) error {
	if !controllerutil.ContainsFinalizer(endpointSliceImport, endpointSliceImportCleanupFinalizer) {
		controllerutil.AddFinalizer(endpointSliceImport, endpointSliceImportCleanupFinalizer)
		return r.HubClient.Update(ctx, endpointSliceImport)
	}
	return nil
}

// isDerivedServiceValid returns if a derived Service is valid for EndpointSlice association.
func (r *Reconciler) isDerivedServiceValid(ctx context.Context, derivedSvcName string) (bool, error) {
	// Check if the given name is a valid Service name; this helps guard against user tampering the label.
	if errs := validation.IsDNS1035Label(derivedSvcName); len(errs) != 0 {
		return false, nil
	}

	// Check if the derived Service has been created and has not been marked for deletion.
	// The derived Service label is added before the actual Service is created; in some (highly unlikely) scenarios it
	// could happen that the controller sees a derived Service label yet cannot find the corresponding Service.
	derivedSvc := &corev1.Service{}
	derivedSvcKey := types.NamespacedName{Namespace: r.FleetSystemNamespace, Name: derivedSvcName}
	if err := r.MemberClient.Get(ctx, derivedSvcKey, derivedSvc); err != nil {
		return false, client.IgnoreNotFound(err)
	}
	return derivedSvc.DeletionTimestamp == nil, nil
}

// scanForDerivedServiceName scans a list of MCSes and returns the first found derived Service label in the list.
func scanForDerivedServiceName(multiClusterSvcList *fleetnetv1alpha1.MultiClusterServiceList) string {
	var derivedSvcName string
	for _, multiClusterSvc := range multiClusterSvcList.Items {
		if multiClusterSvc.DeletionTimestamp != nil {
			continue
		}

		svcName, ok := multiClusterSvc.Labels[objectmeta.MultiClusterServiceLabelDerivedService]
		if ok {
			derivedSvcName = svcName
			break
		}
	}
	return derivedSvcName
}

// formatEndpointSliceFromImport formats an EndpointSlice using an EndpointSliceImport.
func formatEndpointSliceFromImport(endpointSlice *discoveryv1.EndpointSlice, derivedSvcName string, endpointSliceImport *fleetnetv1alpha1.EndpointSliceImport) {
	endpointSlice.AddressType = endpointSliceImport.Spec.AddressType
	endpointSlice.Labels = map[string]string{
		discoveryv1.LabelServiceName: derivedSvcName,
		discoveryv1.LabelManagedBy:   controllerID,
	}
	endpointSlice.Ports = endpointSliceImport.Spec.Ports

	endpoints := []discoveryv1.Endpoint{}
	for _, importedEndpoint := range endpointSliceImport.Spec.Endpoints {
		endpoints = append(endpoints, discoveryv1.Endpoint{
			Addresses: importedEndpoint.Addresses,
		})
	}
	endpointSlice.Endpoints = endpoints
}

// Observe data points for metrics.
func (r *Reconciler) observeMetrics(ctx context.Context,
	endpointSliceImport *fleetnetv1alpha1.EndpointSliceImport,
	startTime time.Time) error {
	// Check if a metric data point has been observed for the current generation of the object; this helps guard
	// against repeated observation of metric data points for the same generation of an object due to no-op
	// reconciliations (e.g. resyncs, untracked changes).
	lastObservedGeneration, ok := endpointSliceImport.Annotations[metrics.MetricsAnnotationLastObservedGeneration]
	currentGenerationStr := fmt.Sprintf("%d", endpointSliceImport.Spec.EndpointSliceReference.Generation)
	if ok && lastObservedGeneration == currentGenerationStr {
		// A data point has been observed for this generation; skip the observation.
		return nil
	}

	// Observe a new data point.

	// Annotate the object to track the last observed generation; this must happen before the actual observation.
	if endpointSliceImport.Annotations == nil {
		// Initialize the annotation map if it is empty.
		endpointSliceImport.Annotations = map[string]string{}
	}
	endpointSliceImport.Annotations[metrics.MetricsAnnotationLastObservedGeneration] = currentGenerationStr
	if err := r.HubClient.Update(ctx, endpointSliceImport); err != nil {
		return err
	}

	timeSpent := startTime.Sub(endpointSliceImport.Spec.EndpointSliceReference.ExportedSince.Time).Milliseconds()
	// Under some rare circumstances (such as time sync not being configured properly across clusters), it could
	// happen that the export timestamp of an EndpointSlice appears later than its import timestamp. Unfortunately,
	// clock discrepancies are out of Fleet networking's control and there is not an easy way to determine how
	// much clocks drift from each other and if (when) it will recover. To avoid negative outliers affecting
	// data analysis, this controller assigns a constant of exactly 1 second when the calculated duration does
	// not make sense.
	if timeSpent <= 0 {
		timeSpent = time.Second.Milliseconds() * 1
		klog.V(4).Info("A negative endpoint export/import duration data point has been observed; time sync might be out of order",
			"serviceNamespacedName", endpointSliceImport.Spec.OwnerServiceReference.NamespacedName,
			"endpointSliceNamespacedName", endpointSliceImport.Spec.EndpointSliceReference.NamespacedName,
			"originClusterID", endpointSliceImport.Spec.EndpointSliceReference.ClusterID,
			"destinationClusterID", r.MemberClusterID)
	}
	// Similarly, to avoid large outliers skewing the stats (e.g. averages), this controller caps the data point
	// to a constant value.
	if timeSpent > int64(metrics.ExportDurationRightBound) {
		timeSpent = int64(metrics.ExportDurationRightBound)
	}
	endpointSliceExportImportDuration.
		WithLabelValues(endpointSliceImport.Spec.EndpointSliceReference.ClusterID, r.MemberClusterID).
		Observe(float64(timeSpent))
	// TO-DO (chenyu1): Remove the metric logs when histogram metrics are supported in the backend.
	klog.V(2).InfoS("endpointSliceExportImportDuration",
		"value", timeSpent,
		"originClusterID", endpointSliceImport.Spec.EndpointSliceReference.ClusterID,
		"destinationClusterID", r.MemberClusterID)
	return nil
}
