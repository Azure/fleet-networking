/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package internalserviceimport features the InternalServiceImport controller for reporting back the status from the
// fleet to a member cluster.
package internalserviceimport

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

// NewReconciler returns a reconciler for the InternalServiceImport.
func NewReconciler(memberClient, hubClient client.Client) *Reconciler {
	return &Reconciler{
		memberClient: memberClient,
		hubClient:    hubClient,
	}
}

// Reconciler reconciles a InternalServiceImport object.
type Reconciler struct {
	memberClient client.Client
	hubClient    client.Client
}

//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=internalserviceimports,verbs=get;list;watch;delete
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceimports,verbs=get;list
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceimports/status,verbs=get;update;patch

// Reconcile reports back ServiceImport status from the fleet to a member cluster.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	internalSvcImportKRef := klog.KRef(req.Namespace, req.Name)
	startTime := time.Now()
	klog.V(2).InfoS("Reconciliation starts", "internalServiceImport", internalSvcImportKRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Reconciliation ends", "internalServiceImport", internalSvcImportKRef, "latency", latency)
	}()

	// Retrieve the InternalServiceImport object.
	var internalSvcImport fleetnetv1alpha1.InternalServiceImport
	if err := r.hubClient.Get(ctx, req.NamespacedName, &internalSvcImport); err != nil {
		klog.ErrorS(err, "Failed to get internal svc import", "internalServiceImport", internalSvcImportKRef)
		// Skip the reconciliation if the InternalServiceImport does not exist.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	// Check if the service import exists in the member cluster.
	var serviceImport fleetnetv1alpha1.ServiceImport
	svcImportName := types.NamespacedName{Namespace: internalSvcImport.Spec.ServiceImportReference.Namespace, Name: internalSvcImport.Spec.ServiceImportReference.Name}
	svcImportKRef := klog.KRef(svcImportName.Namespace, svcImportName.Name)
	err := r.memberClient.Get(ctx, svcImportName, &serviceImport)
	switch {
	case errors.IsNotFound(err):
		// Normally this situation will never happen as the ServiceImport controller guarantees, using the cleanup
		// finalizer, that a InternalServiceImport should be deleted. In some corner cases,
		// however, e.g. the user chooses to remove the finalizer explicitly, a InternalServiceImport can be left over
		// in the hub cluster, and it is up to this controller to remove it.
		klog.V(2).InfoS("serviceImport does not exist; deleting the internalServiceImport",
			"serviceImport", svcImportKRef,
			"internalServiceImport", internalSvcImportKRef,
		)
		if err := r.hubClient.Delete(ctx, &internalSvcImport); err != nil {
			klog.ErrorS(err, "Failed to delete internalServiceImport", "internalServiceImport", internalSvcImportKRef)
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	case err != nil:
		// An unexpected error occurs.
		klog.ErrorS(err, "Failed to get serviceImport", "serviceImport", svcImportKRef)
		return ctrl.Result{}, err
	}

	// no status change
	if equality.Semantic.DeepEqual(internalSvcImport.Status, serviceImport.Status) {
		return ctrl.Result{}, nil
	}

	// report back import status
	klog.V(2).InfoS("Report back service import status from fleet", "internalServiceImport", internalSvcImportKRef)
	oldStatus := serviceImport.Status.DeepCopy()
	serviceImport.Status = internalSvcImport.Status

	klog.V(2).InfoS("Updating the service import status", "serviceImport", svcImportKRef, "status", serviceImport.Status, "oldStatus", oldStatus)
	if err := r.memberClient.Status().Update(ctx, &serviceImport); err != nil {
		klog.ErrorS(err, "Failed to update service import status", "serviceImport", svcImportKRef, "status", serviceImport.Status, "oldStatus", oldStatus)
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetnetv1alpha1.InternalServiceImport{}).
		Complete(r)
}
