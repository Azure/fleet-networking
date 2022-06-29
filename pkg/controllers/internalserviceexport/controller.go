/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// package internalsvcexport features the InternalServiceExport controller for reporting back conflict resolution
// status from the fleet to a member cluster.
package internalsvcexport

import (
	"context"
	"reflect"
	"time"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Reconciler struct {
	memberClient client.Client
	hubClient    client.Client
}

//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=internalserviceexports,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=internalserviceexports/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceexports,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceexports/status,verbs=get;update;patch

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	internalSvcExportRef := klog.KRef(req.Namespace, req.Name)
	startTime := time.Now()
	klog.V(2).InfoS("Reconciliation starts", "internalServiceExport", internalSvcExportRef)
	defer func() {
		latency := time.Since(startTime).Seconds()
		klog.V(2).InfoS("Reconciliation ends", "internalServiceExport", internalSvcExportRef, "latency", latency)
	}()

	// Retrieve the InternalServiceExport object.
	var internalSvcExport fleetnetv1alpha1.InternalServiceExport
	if err := r.hubClient.Get(ctx, req.NamespacedName, &internalSvcExport); err != nil {
		klog.ErrorS(err, "Failed to get internal svc export", "internalServiceExport", internalSvcExportRef)
		// Skip the reconciliation if the InternalServiceExport does not exist.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Check if the exported Service exists.
	svcNS := internalSvcExport.Spec.ServiceReference.Namespace
	svcName := internalSvcExport.Spec.ServiceReference.Name
	svcRef := klog.KRef(svcNS, svcName)
	var svcExport fleetnetv1alpha1.ServiceExport
	err := r.memberClient.Get(ctx, types.NamespacedName{Namespace: svcNS, Name: svcName}, &svcExport)
	switch {
	case errors.IsNotFound(err):
		// The absence of ServiceExport suggests that the Service should not be, yet has been, exported. Normally
		// this situation will never happen as the ServiceExport controller guarantees, using the cleanup finalizer,
		// that a ServiceExport will only be deleted after the Service has been unexported. In some corner cases,
		// however, e.g. the user chooses to remove the finalizer explicitly, a Service can be left over in the hub
		// cluster, and it is up to this controller to remove it.
		klog.V(2).InfoS("Svc export does not exist; delete the internal svc export", "service", svcRef)
		err = r.hubClient.Delete(ctx, &internalSvcExport)
		if err != nil {
			klog.ErrorS(err, "Failed to delete internal svc export", "internalServiceExport", internalSvcExportRef)
		}
		return ctrl.Result{}, err
	case err != nil:
		// An unexpected error occurs.
		klog.ErrorS(err, "Failed to get svc export", "service", svcRef)
		return ctrl.Result{}, err
	}

	// Report back conflict resolution result.
	klog.V(2).InfoS("Report back conflict resolution result", "internalServiceExport", internalSvcExportRef)
	if err = r.reportBackConflictCondition(ctx, &svcExport, &internalSvcExport); err != nil {
		klog.ErrorS(err, "Failed to report back conflict resolution result", "service", svcRef)
	}
	return ctrl.Result{}, err
}

// SetupWithManager builds a controller with InternalSvcExportReconciler and sets it up with a
// (multi-namespaced) controller manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).For(&fleetnetv1alpha1.InternalServiceExport{}).Complete(r)
}

// reportBackConflictCond reports the ServiceExportConflict condition added to the InternalServiceExport object in the
// hub cluster back to the ServiceExport ojbect in the member cluster.
func (r *Reconciler) reportBackConflictCondition(ctx context.Context,
	svcExport *fleetnetv1alpha1.ServiceExport,
	internalSvcExport *fleetnetv1alpha1.InternalServiceExport) error {
	internalSvcExportRef := klog.KRef(internalSvcExport.Namespace, internalSvcExport.Name)
	internalSvcExportConflictCond := meta.FindStatusCondition(internalSvcExport.Status.Conditions,
		string(fleetnetv1alpha1.ServiceExportConflict))
	if internalSvcExportConflictCond == nil {
		// No conflict condition to report back; this is the expected behavior when the conflict resolution process
		// has not completed yet.
		klog.V(4).InfoS("No conflict condition to report back", "internalServiceExport", internalSvcExportRef)
		return nil
	}

	svcExportConflictCond := meta.FindStatusCondition(internalSvcExport.Status.Conditions,
		string(fleetnetv1alpha1.ServiceExportConflict))
	if reflect.DeepEqual(internalSvcExportConflictCond, svcExportConflictCond) {
		// The conflict condition has not changed and there is no need to report back; this is also an expected
		// behavior.
		klog.V(4).InfoS("No update on the conflict condition", "internalServiceExport", internalSvcExportRef)
		return nil
	}

	// Update the conditions
	meta.SetStatusCondition(&svcExport.Status.Conditions, *internalSvcExportConflictCond)
	return r.memberClient.Status().Update(ctx, svcExport)
}
