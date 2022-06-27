/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package serviceexport features the ServiceExport controller for exporting a Service from a member cluster to
// its fleet.
package serviceexport

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

// Reconciler reconciles the export of a Service.
type Reconciler struct {
	memberClient client.Client
	hubClient    client.Client
	// The namespace reserved for the current member cluster in the hub cluster.
	hubNamespace string
}

//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceexports,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceexports/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceexports/finalizers,verbs=update
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=internalserviceexports,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete

// Reconcile exports a Service.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	svcRef := klog.KRef(req.Namespace, req.Name)
	startTime := time.Now()
	klog.V(2).InfoS("reconciliation starts", "req", req)
	defer func() {
		latency := time.Since(startTime).Seconds()
		klog.V(2).InfoS("reconciliation ends", "svc", svcRef, "latency", latency)
	}()

	// Retrieve the ServiceExport object.
	var svcExport fleetnetv1alpha1.ServiceExport
	if err := r.memberClient.Get(ctx, req.NamespacedName, &svcExport); err != nil {
		klog.ErrorS(err, "failed to get service export", "svc", svcRef)
		// Skip the reconciliation if the ServiceExport does not exist; this should only happen when a ServiceExport
		// is deleted before the corresponding Service is exported to the fleet (and a cleanup finalizer is added),
		// which requires no action to take on this controller's end.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Check if the ServiceExport has been deleted and needs cleanup (unexporting Service).
	// A ServiceExport needs cleanup when it has the ServiceExport cleanup finalizer added; the absence of this
	// finalizer guarantees that the corresponding Service has never been exported to the fleet.
	if isServiceExportCleanupNeeded(&svcExport) {
		klog.V(2).InfoS("svc export is deleted; unexport the svc", "svc", svcRef)
		res, err := r.unexportService(ctx, &svcExport)
		if err != nil {
			klog.ErrorS(err, "failed to unexport the svc", "svc", svcRef)
		}
		return res, err
	}

	// Check if the Service to export exists.
	var svc corev1.Service
	err := r.memberClient.Get(ctx, req.NamespacedName, &svc)
	switch {
	// The Service to export does not exist or has been deleted.
	case errors.IsNotFound(err) || isServiceDeleted(&svc):
		// Unexport the Service if the ServiceExport has the cleanup finalizer added.
		klog.V(2).InfoS("svc is deleted; unexport the svc", "svc", svcRef)
		if controllerutil.ContainsFinalizer(&svcExport, svcExportCleanupFinalizer) {
			if _, err = r.unexportService(ctx, &svcExport); err != nil {
				klog.ErrorS(err, "failed to unexport the svc", "svc", svcRef)
				return ctrl.Result{}, err
			}
		}
		// Mark the ServiceExport as invalid.
		klog.V(2).InfoS("mark svc export as invalid (svc not found)", "svc", svcRef)
		if err := r.markServiceExportAsInvalidSvcNotFound(ctx, &svcExport); err != nil {
			klog.ErrorS(err, "failed to mark svc export as invalid (svc not found)", "svc", svcRef)
		}
		return ctrl.Result{}, err
	// An unexpected error occurs when retrieving the Service.
	case err != nil:
		klog.ErrorS(err, "failed to get the svc", "svc", svcRef)
		return ctrl.Result{}, err
	}

	// Check if the Service is eligible for export.

	// Add the cleanup finalizer; this must happen before the Service is actually exported.

	// Mark the ServiceExport as valid + pending conflict resolution.

	// Export the Service or update the exported Service.

	return ctrl.Result{}, err
}

// SetupWithManager builds a controller with SvcExportReconciler and sets it up with a controller manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// The ServiceExport controller watches over ServiceExport objects.
		// TO-DO (chenyu1): use predicates to filter out some events.
		For(&fleetnetv1alpha1.ServiceExport{}).
		// The ServiceExport controller watches over Service objects.
		// TO-DO (chenyu1): use handler funcs to filter out some events.
		Watches(&source.Kind{Type: &corev1.Service{}}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}

// unexportSvc unexports a Service, specifically, it deletes the corresponding InternalServiceExport from the
// hub cluster and removes the cleanup finalizer.
func (r *Reconciler) unexportService(ctx context.Context, svcExport *fleetnetv1alpha1.ServiceExport) (ctrl.Result, error) {
	// Get the unique name assigned when the Service is exported. it is guaranteed that Services are
	// always exported using the name format `ORIGINAL_NAMESPACE-ORIGINAL_NAME`; for example, a Service
	// from namespace `default`` with the name `store`` will be exported with the name `default-store`.
	internalSvcExportName := formatInternalServiceExportName(svcExport)

	internalSvcExport := &fleetnetv1alpha1.InternalServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: r.hubNamespace,
			Name:      internalSvcExportName,
		},
	}

	// Unexport the Service.
	if err := r.hubClient.Delete(ctx, internalSvcExport); err != nil && !errors.IsNotFound(err) {
		// It is guaranteed that a finalizer is always added before the Service is actually exported; as a result,
		// in some rare occasions it could happen that a ServiceExport has a finalizer present yet the corresponding
		// Service has not been exported to the hub cluster yet. It is an expected behavior and no action
		// is needed on this controller's end.
		return ctrl.Result{}, err
	}

	// Remove the finalizer; it must happen after the Service has successfully been unexported.
	return r.removeServiceExportCleanupFinalizer(ctx, svcExport)
}

// removeSvcExportCleanupFinalizer removes the cleanup finalizer from a ServiceExport.
func (r *Reconciler) removeServiceExportCleanupFinalizer(ctx context.Context, svcExport *fleetnetv1alpha1.ServiceExport) (ctrl.Result, error) {
	controllerutil.RemoveFinalizer(svcExport, svcExportCleanupFinalizer)
	err := r.memberClient.Update(ctx, svcExport)
	return ctrl.Result{}, err
}

// markSvcExportAsInvalidNotFound marks a ServiceExport as invalid.
func (r *Reconciler) markServiceExportAsInvalidSvcNotFound(ctx context.Context, svcExport *fleetnetv1alpha1.ServiceExport) error {
	validCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportValid))
	if validCond != nil && validCond.Status == metav1.ConditionFalse && validCond.Reason == "ServiceNotFound" {
		// A stable state has been reached; no further action is needed.
		return nil
	}

	meta.SetStatusCondition(&svcExport.Status.Conditions, metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportValid),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             "ServiceNotFound",
		Message:            fmt.Sprintf("service %s/%s is not found", svcExport.Namespace, svcExport.Name),
	})
	return r.memberClient.Status().Update(ctx, svcExport)
}
