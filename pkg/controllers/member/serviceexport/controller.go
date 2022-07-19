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
	condition "go.goms.io/fleet-networking/pkg/common/condition"
)

const (
	svcExportValidCondReason                 = "ServiceIsValid"
	svcExportInvalidNotFoundCondReason       = "ServiceNotFound"
	svcExportInvalidIneligibleCondReason     = "ServiceIneligible"
	svcExportPendingConflictResolutionReason = "ServicePendingConflictResolution"

	// svcExportCleanupFinalizer is the finalizer ServiceExport controllers adds to mark that
	// a ServiceExport can only be deleted after its corresponding Service has been unexported from the hub cluster.
	svcExportCleanupFinalizer = "networking.fleet.azure.com/svc-export-cleanup"
)

// Reconciler reconciles the export of a Service.
type Reconciler struct {
	memberClusterID string
	memberClient    client.Client
	hubClient       client.Client
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
	klog.V(2).InfoS("Reconciliation starts", "service", svcRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Reconciliation ends", "service", svcRef, "latency", latency)
	}()

	// Retrieve the ServiceExport object.
	var svcExport fleetnetv1alpha1.ServiceExport
	if err := r.memberClient.Get(ctx, req.NamespacedName, &svcExport); err != nil {
		klog.ErrorS(err, "Failed to get service export", "service", svcRef)
		// Skip the reconciliation if the ServiceExport does not exist; this should only happen when a ServiceExport
		// is deleted before the corresponding Service is exported to the fleet (and a cleanup finalizer is added),
		// which requires no action to take on this controller's end.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Check if the ServiceExport has been deleted and needs cleanup (unexporting Service).
	// A ServiceExport needs cleanup when it has the ServiceExport cleanup finalizer added; the absence of this
	// finalizer guarantees that the corresponding Service has never been exported to the fleet, thus no action
	// is needed.
	if svcExport.DeletionTimestamp != nil {
		if controllerutil.ContainsFinalizer(&svcExport, svcExportCleanupFinalizer) {
			klog.V(2).InfoS("Service export is deleted; unexport the service", "service", svcRef)
			res, err := r.unexportService(ctx, &svcExport)
			if err != nil {
				klog.ErrorS(err, "Failed to unexport the service", "service", svcRef)
			}
			return res, err
		}
		return ctrl.Result{}, nil
	}

	// Check if the Service to export exists.
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: req.Namespace,
			Name:      req.Name,
		},
	}
	err := r.memberClient.Get(ctx, req.NamespacedName, &svc)
	switch {
	// The Service to export does not exist or has been deleted.
	case errors.IsNotFound(err) || svc.DeletionTimestamp != nil:
		// Unexport the Service if the ServiceExport has the cleanup finalizer added.
		klog.V(2).InfoS("Service is deleted; unexport the service", "service", svcRef)
		if controllerutil.ContainsFinalizer(&svcExport, svcExportCleanupFinalizer) {
			if _, err = r.unexportService(ctx, &svcExport); err != nil {
				klog.ErrorS(err, "Failed to unexport the service", "service", svcRef)
				return ctrl.Result{}, err
			}
		}
		// Mark the ServiceExport as invalid.
		klog.V(2).InfoS("Mark service export as invalid (service not found)", "service", svcRef)
		if err := r.markServiceExportAsInvalidNotFound(ctx, &svcExport); err != nil {
			klog.ErrorS(err, "Failed to mark service export as invalid (service not found)", "service", svcRef)
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	// An unexpected error occurs when retrieving the Service.
	case err != nil:
		klog.ErrorS(err, "Failed to get the service", "service", svcRef)
		return ctrl.Result{}, err
	}

	// Check if the Service is eligible for export.
	if !isServiceEligibleForExport(&svc) {
		// Unexport ineligible Service if the ServiceExport has the cleanup finalizer added.
		if controllerutil.ContainsFinalizer(&svcExport, svcExportCleanupFinalizer) {
			klog.V(2).InfoS("Service is ineligible; unexport the service", "service", svcRef)
			if _, err = r.unexportService(ctx, &svcExport); err != nil {
				klog.ErrorS(err, "Failed to unexport the service", "service", svcRef)
				return ctrl.Result{}, err
			}
		}
		// Mark the ServiceExport as invalid.
		klog.V(2).InfoS("Mark service export as invalid (service ineligible)", "service", svcRef)
		err := r.markServiceExportAsInvalidSvcIneligible(ctx, &svcExport)
		if err != nil {
			klog.ErrorS(err, "Failed to mark service export as ivalid (service ineligible)", "service", svcRef)
		}
		return ctrl.Result{}, err
	}

	// Add the cleanup finalizer to the ServiceExport; this must happen before the Service is actually exported.
	if !controllerutil.ContainsFinalizer(&svcExport, svcExportCleanupFinalizer) {
		klog.V(2).InfoS("Add cleanup finalizer to service export", "service", svcRef)
		if err := r.addServiceExportCleanupFinalizer(ctx, &svcExport); err != nil {
			klog.ErrorS(err, "Failed to add cleanup finalizer to svc export", "service", svcRef)
			return ctrl.Result{}, err
		}
	}

	// Mark the ServiceExport as valid.
	klog.V(2).InfoS("Mark service export as valid", "service", svcRef)
	if err := r.markServiceExportAsValid(ctx, &svcExport); err != nil {
		klog.ErrorS(err, "Failed to mark service export as valid", "service", svcRef)
		return ctrl.Result{}, err
	}

	// Export the Service or update the exported Service.

	// Create or update the InternalServiceExport object.
	internalSvcExport := fleetnetv1alpha1.InternalServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: r.hubNamespace,
			Name:      formatInternalServiceExportName(&svcExport),
		},
	}
	svcExportPorts := extractServicePorts(&svc)
	klog.V(2).InfoS("Export the service or update the exported service",
		"service", svcExport,
		"internalServiceExport", klog.KObj(&internalSvcExport))
	createOrUpdateOp, err := controllerutil.CreateOrUpdate(ctx, r.hubClient, &internalSvcExport, func() error {
		if internalSvcExport.CreationTimestamp.IsZero() {
			// Set the ServiceReference only when the InternalServiceExport is created; most of the fields in
			// an ExportedObjectReference should be immutable.
			internalSvcExport.Spec.ServiceReference = fleetnetv1alpha1.FromMetaObjects(r.memberClusterID, svc.TypeMeta, svc.ObjectMeta)
		}

		internalSvcExport.Spec.Ports = svcExportPorts
		internalSvcExport.Spec.ServiceReference.UpdateFromMetaObject(svc.ObjectMeta)
		return nil
	})
	if err != nil {
		klog.ErrorS(err, "Failed to create/update InternalServiceExport",
			"internalServiceExport", klog.KObj(&internalSvcExport),
			"service", svcRef,
			"op", createOrUpdateOp)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, err
}

// SetupWithManager builds a controller with Reconciler and sets it up with a controller manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// The ServiceExport controller watches over ServiceExport objects.
		For(&fleetnetv1alpha1.ServiceExport{}).
		// The ServiceExport controller watches over Service objects.
		Watches(&source.Kind{Type: &corev1.Service{}}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}

// unexportService unexports a Service, specifically, it deletes the corresponding InternalServiceExport from the
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
		// It is guaranteed that a finalizer is always added to a ServiceExport before the corresponding Service is
		// actually exported; in some rare occasions, e.g. the controller crashes right after it adds the finalizer
		// to the ServiceExport but before the it gets a chance to actually export the Service to the
		// hub cluster, it could happen that a ServiceExport has a finalizer present yet the corresponding Service
		// has not been exported to the hub cluster. It is an expected behavior and no action is needed on this
		// controller's end.
		return ctrl.Result{}, err
	}

	// Remove the finalizer from the ServiceExport; it must happen after the Service has been successfully unexported.
	if err := r.removeServiceExportCleanupFinalizer(ctx, svcExport); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// removeServiceExportCleanupFinalizer removes the cleanup finalizer from a ServiceExport.
func (r *Reconciler) removeServiceExportCleanupFinalizer(ctx context.Context, svcExport *fleetnetv1alpha1.ServiceExport) error {
	controllerutil.RemoveFinalizer(svcExport, svcExportCleanupFinalizer)
	return r.memberClient.Update(ctx, svcExport)
}

// markServiceExportAsInvalidNotFound marks a ServiceExport as invalid.
func (r *Reconciler) markServiceExportAsInvalidNotFound(ctx context.Context, svcExport *fleetnetv1alpha1.ServiceExport) error {
	validCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportValid))
	expectedValidCond := &metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportValid),
		Status:             metav1.ConditionFalse,
		Reason:             svcExportInvalidNotFoundCondReason,
		ObservedGeneration: svcExport.Generation,
		Message:            fmt.Sprintf("service %s/%s is not found", svcExport.Namespace, svcExport.Name),
	}
	if condition.EqualCondition(validCond, expectedValidCond) {
		// A stable state has been reached; no further action is needed.
		return nil
	}

	meta.SetStatusCondition(&svcExport.Status.Conditions, *expectedValidCond)
	return r.memberClient.Status().Update(ctx, svcExport)
}

// markServiceExportAsInvalidSvcIneligible marks a ServiceExport as invalid.
func (r *Reconciler) markServiceExportAsInvalidSvcIneligible(ctx context.Context, svcExport *fleetnetv1alpha1.ServiceExport) error {
	validCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportValid))
	expectedValidCond := &metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportValid),
		Status:             metav1.ConditionFalse,
		Reason:             svcExportInvalidIneligibleCondReason,
		ObservedGeneration: svcExport.Generation,
		Message:            fmt.Sprintf("service %s/%s is not eligible for export", svcExport.Namespace, svcExport.Name),
	}
	if condition.EqualCondition(validCond, expectedValidCond) {
		// A stable state has been reached; no further action is needed.
		return nil
	}

	meta.SetStatusCondition(&svcExport.Status.Conditions, *expectedValidCond)
	return r.memberClient.Status().Update(ctx, svcExport)
}

// addServiceExportCleanupFinalizer adds the cleanup finalizer to a ServiceExport.
func (r *Reconciler) addServiceExportCleanupFinalizer(ctx context.Context, svcExport *fleetnetv1alpha1.ServiceExport) error {
	controllerutil.AddFinalizer(svcExport, svcExportCleanupFinalizer)
	return r.memberClient.Update(ctx, svcExport)
}

// markServiceExportAsValid marks a ServiceExport as valid; if no conflict condition has been added, the
// ServiceExport will be marked as pending conflict resolution as well.
func (r *Reconciler) markServiceExportAsValid(ctx context.Context, svcExport *fleetnetv1alpha1.ServiceExport) error {
	validCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportValid))
	expectedValidCond := &metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportValid),
		Status:             metav1.ConditionTrue,
		Reason:             svcExportValidCondReason,
		ObservedGeneration: svcExport.Generation,
		Message:            fmt.Sprintf("service %s/%s is valid for export", svcExport.Namespace, svcExport.Name),
	}
	conflictCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportConflict))
	if condition.EqualCondition(validCond, expectedValidCond) &&
		conflictCond != nil {
		// A stable state has been reached; no further action is needed.
		return nil
	}

	meta.SetStatusCondition(&svcExport.Status.Conditions, *expectedValidCond)
	meta.SetStatusCondition(&svcExport.Status.Conditions, metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportConflict),
		Status:             metav1.ConditionUnknown,
		ObservedGeneration: svcExport.Generation,
		Reason:             svcExportPendingConflictResolutionReason,
		Message:            fmt.Sprintf("service %s/%s is pending export conflict resolution", svcExport.Namespace, svcExport.Name),
	})
	return r.memberClient.Status().Update(ctx, svcExport)
}
