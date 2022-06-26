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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	fleetnetworkingapi "go.goms.io/fleet-networking/api/v1alpha1"
)

// ServiceExportReconciler reconciles the export of a Service.
type SvcExportReconciler struct {
	memberClusterID string
	memberClient    client.Client
	hubClient       client.Client
	hubNS           string
}

//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceexports,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceexports/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceexports/finalizers,verbs=update
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=internalserviceexports,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete

// Reconcile exports a Service.
func (r *SvcExportReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	reqName := req.NamespacedName
	startTime := time.Now()
	klog.InfoS("reconciliation starts", "req", req)
	defer func() {
		timeSpent := time.Since(startTime).Seconds()
		klog.InfoS(fmt.Sprintf("reconciliation ends (%.2f)", timeSpent), "svc", reqName)
	}()

	// Retrieve the ServiceExport object.
	var svcExport fleetnetworkingapi.ServiceExport
	if err := r.memberClient.Get(ctx, req.NamespacedName, &svcExport); err != nil {
		klog.ErrorS(err, "failed to get service export", "svc", reqName)
		// Skip the reconciliation if the ServiceExport does not exist; this should only happen when a ServiceExport
		// is deleted before the corresponding Service is exported to the fleet (and a cleanup finalizer is added),
		// which requires no action to take on this controller's end.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Check if the ServiceExport has been deleted and needs cleanup (unexporting Service).
	// A ServiceExport needs cleanup when it has the ServiceExport cleanup finalizer added; the absence of this
	// finalizer guarantees that the corresponding Service has never been exported to the fleet.
	if isSvcExportCleanupNeeded(&svcExport) {
		klog.InfoS("svc export is deleted; unexport the svc", "svc", reqName)
		res, err := r.unexportSvc(ctx, &svcExport)
		if err != nil {
			klog.ErrorS(err, "failed to unexport the svc", "svc", reqName)
		}
		return res, err
	}

	// Check if the Service to export exists.
	var svc corev1.Service
	err := r.memberClient.Get(ctx, req.NamespacedName, &svc)
	switch {
	// The Service to export does not exist or has been deleted.
	case errors.IsNotFound(err) || isSvcDeleted(&svc):
		// Unexport the Service if the ServiceExport has the cleanup finalizer added.
		klog.InfoS("svc is deleted; unexport the svc", "svc", reqName)
		if controllerutil.ContainsFinalizer(&svcExport, svcExportCleanupFinalizer) {
			if _, err = r.unexportSvc(ctx, &svcExport); err != nil {
				klog.ErrorS(err, "failed to unexport the svc", "svc", reqName)
				return ctrl.Result{}, err
			}
		}
		// Mark the ServiceExport as invalid.
		klog.InfoS("mark svc export as invalid (svc not found)", "svc", reqName)
		err := r.markSvcExportAsInvalidSvcNotFound(ctx, &svcExport)
		if err != nil {
			klog.ErrorS(err, "failed to mark svc export as invalid (svc not found)", "svc", reqName)
		}
		return ctrl.Result{}, err
	// An unexpected error occurs when retrieving the Service.
	case err != nil:
		klog.ErrorS(err, "failed to get the svc", "svc", reqName)
		return ctrl.Result{}, err
	}

	// Check if the Service is eligible for export.
	if !isSvcEligibleForExport(&svc) {
		// Unexport ineligible Service if the ServiceExport has the cleanup finalizer added.
		if controllerutil.ContainsFinalizer(&svcExport, svcExportCleanupFinalizer) {
			klog.InfoS("svc is ineligible; unexport the svc", "svc", reqName)
			if _, err = r.unexportSvc(ctx, &svcExport); err != nil {
				klog.ErrorS(err, "failed to unexport the svc", "svc", reqName)
				return ctrl.Result{}, err
			}
		}
		// Mark the ServiceExport as invalid.
		klog.InfoS("mark svc export as invalid (svc ineligible)", "svc", reqName)
		err := r.markSvcExportAsInvalidSvcIneligible(ctx, &svcExport)
		if err != nil {
			klog.ErrorS(err, "failed to mark svc export as ivalid (svc ineligible)", "svc", reqName)
		}
		return ctrl.Result{}, err
	}

	// Add the cleanup finalizer; this must happen before the Service is actually exported.
	if !controllerutil.ContainsFinalizer(&svcExport, svcExportCleanupFinalizer) {
		klog.InfoS("add cleanup finalizer to svc export", "svc", reqName)
		err = r.addSvcExportCleanupFinalizer(ctx, &svcExport)
		if err != nil {
			klog.ErrorS(err, "failed to add cleanup finalizer to svc export", "svc", reqName)
			return ctrl.Result{}, err
		}
	}

	// Mark the ServiceExport as valid + pending conflict resolution.
	klog.InfoS("mark svc export as valid", "svc", reqName)
	err = r.markSvcExportAsValid(ctx, &svcExport)
	if err != nil {
		klog.ErrorS(err, "failed to mark svc export as valid", "svc", reqName)
		return ctrl.Result{}, err
	}

	// Export the Service or update the exported Service.

	// Create or update the InternalServiceExport object.
	internalSvcExport := fleetnetworkingapi.InternalServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: r.hubNS,
			Name:      formatInternalSvcExportName(&svcExport),
		},
	}
	internalSvcExportKey := client.ObjectKey{Namespace: r.hubNS, Name: formatInternalSvcExportName(&svcExport)}
	err = r.hubClient.Get(ctx, internalSvcExportKey, &internalSvcExport)
	updateInternalSvcExport(r.memberClusterID, &svc, &internalSvcExport)
	if errors.IsNotFound(err) {
		// Export the Service
		klog.InfoS("export the svc", "svc", reqName)
		err = r.hubClient.Create(ctx, &internalSvcExport)
	} else if err == nil {
		// Update the exported Service
		klog.InfoS("update the exported svc", "svc", reqName)
		err = r.hubClient.Update(ctx, &internalSvcExport)
	}
	if err != nil {
		klog.ErrorS(err, "failed to export the svc or update the exported svc", "svc", reqName)
	}
	return ctrl.Result{}, err
}

// SetupWithManager builds a controller with SvcExportReconciler and sets it up with a controller manager.
func (r *SvcExportReconciler) SetupWithManager(mgr ctrl.Manager) error {
	svcExportPredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool { return true },
		UpdateFunc: func(e event.UpdateEvent) bool {
			// Enqueue a ServiceExport for processing only when it is deleted; other changes can be ignored.
			oldObj := e.ObjectOld
			newObj := e.ObjectNew
			_, oldOk := oldObj.(*fleetnetworkingapi.ServiceExport)
			_, newOk := newObj.(*fleetnetworkingapi.ServiceExport)
			return oldOk && newOk && newObj.GetDeletionTimestamp() != nil
		},
		DeleteFunc:  func(e event.DeleteEvent) bool { return false },
		GenericFunc: func(e event.GenericEvent) bool { return true },
	}

	svcHandlerFuncs := handler.Funcs{
		CreateFunc: func(e event.CreateEvent, q workqueue.RateLimitingInterface) {
			q.Add(reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: e.Object.GetNamespace(),
					Name:      e.Object.GetName(),
				},
			})
		},
		UpdateFunc: func(e event.UpdateEvent, q workqueue.RateLimitingInterface) {
			// Enqueue a Service for processing only when its spec has changed in a significant way or it has been
			// deleted; this helps filter out some update events as many Service spec fields are not exported at all.
			oldObj := e.ObjectOld
			newObj := e.ObjectNew
			oldSvc, oldOk := oldObj.(*corev1.Service)
			newSvc, newOk := newObj.(*corev1.Service)
			if oldOk && newOk && isSvcChanged(oldSvc, newSvc) {
				q.Add(reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: newObj.GetNamespace(),
						Name:      newObj.GetName(),
					},
				})
			}
		},
		DeleteFunc: func(e event.DeleteEvent, q workqueue.RateLimitingInterface) {
			q.Add(reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: e.Object.GetNamespace(),
					Name:      e.Object.GetName(),
				},
			})
		},
		GenericFunc: func(e event.GenericEvent, q workqueue.RateLimitingInterface) {
			q.Add(reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: e.Object.GetNamespace(),
					Name:      e.Object.GetName(),
				},
			})
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		// The ServiceExport controller watches over ServiceExport objects.
		For(&fleetnetworkingapi.ServiceExport{}, builder.WithPredicates(svcExportPredicate)).
		// The ServiceExport controller watches over Service objects.
		Watches(&source.Kind{Type: &corev1.Service{}}, svcHandlerFuncs).
		Complete(r)
}

// unexportSvc unexports a Service.
func (r *SvcExportReconciler) unexportSvc(ctx context.Context, svcExport *fleetnetworkingapi.ServiceExport) (ctrl.Result, error) {
	// Get the unique name assigned when the Service is exported. it is guaranteed that Services are
	// always exported using the name format `ORIGINAL_NAMESPACE-ORIGINAL_NAME`; for example, a Service
	// from namespace `default`` with the name `store`` will be exported with the name `default-store`.
	internalSvcExportName := formatInternalSvcExportName(svcExport)

	internalSvcExport := &fleetnetworkingapi.InternalServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: r.hubNS,
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
	return r.removeSvcExportCleanupFinalizer(ctx, svcExport)
}

// addSvcExportCleanupFinalizer adds the cleanup finalizer to a ServiceExport.
func (r *SvcExportReconciler) addSvcExportCleanupFinalizer(ctx context.Context, svcExport *fleetnetworkingapi.ServiceExport) error {
	controllerutil.AddFinalizer(svcExport, svcExportCleanupFinalizer)
	err := r.memberClient.Update(ctx, svcExport)
	return err
}

// removeSvcExportCleanupFinalizer removes the cleanup finalizer from a ServiceExport.
func (r *SvcExportReconciler) removeSvcExportCleanupFinalizer(ctx context.Context, svcExport *fleetnetworkingapi.ServiceExport) (ctrl.Result, error) {
	controllerutil.RemoveFinalizer(svcExport, svcExportCleanupFinalizer)
	err := r.memberClient.Update(ctx, svcExport)
	return ctrl.Result{}, err
}

// markSvcExportAsValid marks a ServiceExport as valid + pending conflict resolution.
func (r *SvcExportReconciler) markSvcExportAsValid(ctx context.Context, svcExport *fleetnetworkingapi.ServiceExport) error {
	updatedConds := []metav1.Condition{}
	for _, cond := range svcExport.Status.Conditions {
		if cond.Type != string(fleetnetworkingapi.ServiceExportValid) && cond.Type != string(fleetnetworkingapi.ServiceExportConflict) {
			updatedConds = append(updatedConds, cond)
		}
	}
	updatedConds = append(updatedConds, metav1.Condition{
		Type:               string(fleetnetworkingapi.ServiceExportValid),
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             "ServiceIsValid",
		Message:            fmt.Sprintf("service %s/%s is valid for export", svcExport.Namespace, svcExport.Name),
	})
	updatedConds = append(updatedConds, metav1.Condition{
		Type:               string(fleetnetworkingapi.ServiceExportConflict),
		Status:             metav1.ConditionUnknown,
		LastTransitionTime: metav1.Now(),
		Reason:             "PendingConflictResolution",
		Message:            fmt.Sprintf("service %s/%s is pending export conflict resolution", svcExport.Namespace, svcExport.Name),
	})
	svcExport.Status.Conditions = updatedConds
	return r.memberClient.Status().Update(ctx, svcExport)
}

// markSvcExportAsInvalidNotFound marks a ServiceExport as invalid.
func (r *SvcExportReconciler) markSvcExportAsInvalidSvcNotFound(ctx context.Context, svcExport *fleetnetworkingapi.ServiceExport) error {
	updatedConds := []metav1.Condition{}
	for _, cond := range svcExport.Status.Conditions {
		if cond.Type != string(fleetnetworkingapi.ServiceExportValid) && cond.Type != string(fleetnetworkingapi.ServiceExportConflict) {
			updatedConds = append(updatedConds, cond)
		}
	}
	updatedConds = append(updatedConds, metav1.Condition{
		Type:               string(fleetnetworkingapi.ServiceExportValid),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             "ServiceNotFound",
		Message:            fmt.Sprintf("service %s/%s is not found", svcExport.Namespace, svcExport.Name),
	})
	svcExport.Status.Conditions = updatedConds
	return r.memberClient.Status().Update(ctx, svcExport)
}

// markSvcExportAsInvalidSvcIneligible marks a ServiceExport as invalid.
func (r *SvcExportReconciler) markSvcExportAsInvalidSvcIneligible(ctx context.Context, svcExport *fleetnetworkingapi.ServiceExport) error {
	updatedConds := []metav1.Condition{}
	for _, cond := range svcExport.Status.Conditions {
		if cond.Type != string(fleetnetworkingapi.ServiceExportValid) && cond.Type != string(fleetnetworkingapi.ServiceExportConflict) {
			updatedConds = append(updatedConds, cond)
		}
	}
	updatedConds = append(updatedConds, metav1.Condition{
		Type:               string(fleetnetworkingapi.ServiceExportValid),
		Status:             metav1.ConditionStatus(corev1.ConditionFalse),
		LastTransitionTime: metav1.Now(),
		Reason:             "ServiceIneligible",
		Message:            fmt.Sprintf("service %s/%s is not eligible for export", svcExport.Namespace, svcExport.Name),
	})
	svcExport.Status.Conditions = updatedConds
	return r.memberClient.Status().Update(ctx, svcExport)
}
