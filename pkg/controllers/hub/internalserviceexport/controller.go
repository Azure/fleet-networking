/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package internalserviceexport features the InternalServiceExport controller for exporting services from member to
// the fleet.
package internalserviceexport

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/condition"
)

const (
	// internalServiceExportFinalizer is the finalizer InternalServiceExport controllers adds to mark that a
	// InternalServiceExport can only be deleted after both ServiceImport label and ServiceExport conflict resolution
	// result have been updated.
	internalServiceExportFinalizer = "networking.fleet.azure.com/internal-svc-export-cleanup"

	conditionReasonNoConflictFound = "NoConflictFound"
	conditionReasonConflictFound   = "ConflictFound"
)

// Reconciler reconciles a InternalServiceExport object.
type Reconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// ServiceImportSpecProcessTime is the wait time for the controller to requeue the request and to wait for the
	// ServiceImport controller to resolve the service Spec.
	ServiceImportSpecProcessTime time.Duration
}

//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=internalserviceexports,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=internalserviceexports/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=internalserviceexports/finalizers,verbs=update
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceimports,verbs=get;list;watch;create;update;patch
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceimports/status,verbs=get;update;patch

// Reconcile creates/updates ServiceImport by watching internalServiceExport objects.
// To simplify the design and implementation in the first phase, the serviceExport will be marked as conflicted if its
// service spec does not match with serviceImport.
// We may support KEP1645 Constraints and Conflict Resolution in the future.
// https://github.com/kubernetes/enhancements/tree/master/keps/sig-multicluster/1645-multi-cluster-services-api#constraints-and-conflict-resolution
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	name := req.NamespacedName
	internalServiceExport := fleetnetv1alpha1.InternalServiceExport{}
	internalServiceExportKRef := klog.KRef(name.Namespace, name.Name)
	if err := r.Client.Get(ctx, name, &internalServiceExport); err != nil {
		klog.ErrorS(err, "Failed to get internalServiceExport", "internalServiceExport", internalServiceExportKRef)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if internalServiceExport.ObjectMeta.DeletionTimestamp != nil {
		return r.handleDelete(ctx, &internalServiceExport)
	}

	// register finalizer
	if !controllerutil.ContainsFinalizer(&internalServiceExport, internalServiceExportFinalizer) {
		controllerutil.AddFinalizer(&internalServiceExport, internalServiceExportFinalizer)
		if err := r.Update(ctx, &internalServiceExport); err != nil {
			klog.ErrorS(err, "Failed to add internalServiceExport finalizer", "internalServiceExport", internalServiceExportKRef)
			return ctrl.Result{}, err
		}
	}
	// handle update
	return r.handleUpdate(ctx, &internalServiceExport)
}

func (r *Reconciler) handleDelete(ctx context.Context, internalServiceExport *fleetnetv1alpha1.InternalServiceExport) (ctrl.Result, error) {
	// the internalServiceExport is being deleted
	if !controllerutil.ContainsFinalizer(internalServiceExport, internalServiceExportFinalizer) {
		return ctrl.Result{}, nil
	}

	internalServiceExportKObj := klog.KObj(internalServiceExport)
	klog.V(2).InfoS("Removing internalServiceExport", "internalServiceExport", internalServiceExportKObj)

	// get serviceImport
	serviceImport := &fleetnetv1alpha1.ServiceImport{}
	serviceImportName := types.NamespacedName{Namespace: internalServiceExport.Spec.ServiceReference.Namespace, Name: internalServiceExport.Spec.ServiceReference.Name}
	serviceImportKRef := klog.KRef(serviceImportName.Namespace, serviceImportName.Name)

	if err := r.Client.Get(ctx, serviceImportName, serviceImport); err != nil {
		klog.ErrorS(err, "Failed to get serviceImport", "serviceImport", serviceImportKRef, "internalServiceExport", internalServiceExportKObj)
		if !errors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		return r.removeFinalizer(ctx, internalServiceExport)
	}
	oldStatus := serviceImport.Status.DeepCopy()
	removeClusterFromServiceImportStatus(serviceImport, internalServiceExport.Spec.ServiceReference.ClusterID)
	if err := r.updateServiceImportStatus(ctx, serviceImport, oldStatus); err != nil {
		return ctrl.Result{}, err
	}
	return r.removeFinalizer(ctx, internalServiceExport)
}

func removeClusterFromServiceImportStatus(serviceImport *fleetnetv1alpha1.ServiceImport, clusterID string) {
	var updatedClusters []fleetnetv1alpha1.ClusterStatus
	for _, c := range serviceImport.Status.Clusters {
		if c.Cluster != clusterID {
			updatedClusters = append(updatedClusters, c)
		}
	}
	if len(updatedClusters) == 0 {
		serviceImport.Status = fleetnetv1alpha1.ServiceImportStatus{}
	} else {
		serviceImport.Status.Clusters = updatedClusters
	}
}

func addClusterToServiceImportStatus(serviceImport *fleetnetv1alpha1.ServiceImport, clusterID string) {
	for _, c := range serviceImport.Status.Clusters {
		if c.Cluster == clusterID {
			return
		}
	}
	serviceImport.Status.Clusters = append(serviceImport.Status.Clusters, fleetnetv1alpha1.ClusterStatus{Cluster: clusterID})
}

func (r *Reconciler) updateServiceImportStatus(ctx context.Context, serviceImport *fleetnetv1alpha1.ServiceImport, oldStatus *fleetnetv1alpha1.ServiceImportStatus) error {
	if equality.Semantic.DeepEqual(&serviceImport.Status, oldStatus) { // no change
		return nil
	}
	serviceImportKObj := klog.KObj(serviceImport)
	klog.V(2).InfoS("Updating the serviceImport status", "serviceImport", serviceImportKObj, "oldStatus", oldStatus, "status", serviceImport.Status)

	if err := r.Client.Status().Update(ctx, serviceImport); err != nil {
		klog.ErrorS(err, "Failed to update the serviceImport status", "serviceImport", serviceImportKObj, "oldStatus", oldStatus, "status", serviceImport.Status)
		return err
	}
	return nil
}

func (r *Reconciler) removeFinalizer(ctx context.Context, internalServiceExport *fleetnetv1alpha1.InternalServiceExport) (ctrl.Result, error) {
	// remove the finalizer
	controllerutil.RemoveFinalizer(internalServiceExport, internalServiceExportFinalizer)
	if err := r.Client.Update(ctx, internalServiceExport); err != nil {
		klog.ErrorS(err, "Failed to remove internalServiceExport finalizer", "internalServiceExport", klog.KObj(internalServiceExport))
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *Reconciler) updateInternalServiceExportStatus(ctx context.Context, internalServiceExport *fleetnetv1alpha1.InternalServiceExport, conflict bool) error {
	svcName := types.NamespacedName{
		Namespace: internalServiceExport.Spec.ServiceReference.Namespace,
		Name:      internalServiceExport.Spec.ServiceReference.Name,
	}
	desiredCond := metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportConflict),
		Status:             metav1.ConditionFalse,
		Reason:             conditionReasonNoConflictFound,
		ObservedGeneration: internalServiceExport.Spec.ServiceReference.Generation, // use the generation of the original object
		Message:            fmt.Sprintf("service %s is exported without conflict", svcName),
	}
	if conflict {
		desiredCond = metav1.Condition{
			Type:               string(fleetnetv1alpha1.ServiceExportConflict),
			Status:             metav1.ConditionTrue,
			Reason:             conditionReasonConflictFound,
			ObservedGeneration: internalServiceExport.Spec.ServiceReference.Generation, // use the generation of the original object
			Message:            fmt.Sprintf("service %s is in conflict with other exported services", svcName),
		}
	}
	currentCond := meta.FindStatusCondition(internalServiceExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportConflict))
	if condition.EqualCondition(currentCond, &desiredCond) {
		return nil
	}
	exportKObj := klog.KObj(internalServiceExport)
	oldStatus := internalServiceExport.Status.DeepCopy()
	meta.SetStatusCondition(&internalServiceExport.Status.Conditions, desiredCond)

	klog.V(2).InfoS("Updating internalServiceExport status", "internalServiceExport", exportKObj, "status", internalServiceExport.Status, "oldStatus", oldStatus)
	if err := r.Status().Update(ctx, internalServiceExport); err != nil {
		klog.ErrorS(err, "Failed to update internalServiceExport status", "internalServiceExport", exportKObj, "status", internalServiceExport.Status, "oldStatus", oldStatus)
		return err
	}
	return nil
}

func (r *Reconciler) handleUpdate(ctx context.Context, internalServiceExport *fleetnetv1alpha1.InternalServiceExport) (ctrl.Result, error) {
	internalServiceExportKObj := klog.KObj(internalServiceExport)
	// get serviceImport
	serviceImport := &fleetnetv1alpha1.ServiceImport{}
	serviceImportName := types.NamespacedName{Namespace: internalServiceExport.Spec.ServiceReference.Namespace, Name: internalServiceExport.Spec.ServiceReference.Name}
	serviceImportKRef := klog.KRef(serviceImportName.Namespace, serviceImportName.Name)

	if err := r.Client.Get(ctx, serviceImportName, serviceImport); err != nil {
		if !errors.IsNotFound(err) {
			klog.ErrorS(err, "Failed to get serviceImport", "serviceImport", serviceImportKRef, "internalServiceExport", internalServiceExportKObj)
			return ctrl.Result{}, err
		}
		serviceImport = &fleetnetv1alpha1.ServiceImport{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: serviceImportName.Namespace,
				Name:      serviceImportName.Name,
			},
		}
		klog.V(2).InfoS("Creating serviceImport", "serviceImport", serviceImportKRef, "internalServiceExport", internalServiceExportKObj)
		if err := r.Client.Create(ctx, serviceImport); err != nil {
			klog.ErrorS(err, "Failed to create or update service import", "serviceImport", serviceImportKRef, "internalServiceExport", internalServiceExportKObj)
			return ctrl.Result{}, err
		}
	}

	if len(serviceImport.Status.Ports) == 0 {
		// Requeue the request and waiting for the ServiceImport controller to resolve the spec.
		klog.V(3).InfoS("Waiting for serviceImport controller to resolve the spec", "serviceImport", serviceImportKRef, "internalServiceExport", internalServiceExportKObj)
		return ctrl.Result{RequeueAfter: r.ServiceImportSpecProcessTime}, nil
	}

	oldStatus := serviceImport.Status.DeepCopy()
	clusterID := internalServiceExport.Spec.ServiceReference.ClusterID

	// To simplify the implementation, we compare the whole ports structure.
	// TODO, change to compare the ports by ignoring the order and protocol and port are the map keys.
	if !equality.Semantic.DeepEqual(serviceImport.Status.Ports, internalServiceExport.Spec.Ports) {
		removeClusterFromServiceImportStatus(serviceImport, clusterID)
		if err := r.updateServiceImportStatus(ctx, serviceImport, oldStatus); err != nil {
			return ctrl.Result{}, err
		}
		// It's possible, eg, there is only one serviceExport and its spec has been changed.
		// ServiceImport stores the old spec of this ServiceExport and later the serviceExport changes its spec.
		if len(serviceImport.Status.Ports) == 0 {
			klog.V(3).InfoS("Removed the cluster and waiting for serviceImport controller to resolve the spec", "serviceImport", serviceImportKRef, "internalServiceExport", internalServiceExportKObj)
			// Requeue the request and waiting for the ServiceImport controller to resolve the spec.
			return ctrl.Result{RequeueAfter: r.ServiceImportSpecProcessTime}, nil
		}
		return ctrl.Result{}, r.updateInternalServiceExportStatus(ctx, internalServiceExport, true)
	}

	addClusterToServiceImportStatus(serviceImport, clusterID)
	if err := r.updateServiceImportStatus(ctx, serviceImport, oldStatus); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.updateInternalServiceExportStatus(ctx, internalServiceExport, false); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetnetv1alpha1.InternalServiceExport{}).
		Complete(r)
}
