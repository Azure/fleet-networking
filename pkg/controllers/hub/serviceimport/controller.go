/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package serviceimport features the serviceimport controller to resolve the service spec when exporting multi-cluster
// services.
package serviceimport

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/apiretry"
	"go.goms.io/fleet-networking/pkg/common/condition"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
)

const (
	// fields name used to filter resources
	exportedServiceFieldNamespacedName = ".spec.serviceReference.namespacedName"
)

// Reconciler reconciles a ServiceImport object.
type Reconciler struct {
	client.Client
}

// statusChange stores the internalServiceExports list whose status needs to be updated.
type statusChange struct {
	conflict   []*fleetnetv1alpha1.InternalServiceExport
	noConflict []*fleetnetv1alpha1.InternalServiceExport
}

//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceimports,verbs=get;list;watch;update;patch;delete
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceimports/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceimports/finalizers,verbs=update
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=internalserviceexports,verbs=get;watch;list
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=internalserviceexports/status,verbs=get;update;patch

// Reconcile resolves the service spec when the serviceImport status is empty and updates the status of internalServiceExports.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	serviceImportKRef := klog.KRef(req.Namespace, req.Name)
	startTime := time.Now()
	klog.V(2).InfoS("Reconciliation starts", "serviceImport", serviceImportKRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Reconciliation ends", "serviceImport", serviceImportKRef, "latency", latency)
	}()
	serviceImport := fleetnetv1alpha1.ServiceImport{}
	if err := r.Client.Get(ctx, req.NamespacedName, &serviceImport); err != nil {
		klog.ErrorS(err, "Failed to get serviceImport", "serviceImport", serviceImportKRef)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	// If the spec has already present, no need to resolve the service spec.
	if len(serviceImport.Status.Clusters) != 0 {
		return ctrl.Result{}, nil
	}

	internalServiceExportList := &fleetnetv1alpha1.InternalServiceExportList{}
	namespaceName := types.NamespacedName{Namespace: serviceImport.Namespace, Name: serviceImport.Name}
	listOpts := client.MatchingFields{
		exportedServiceFieldNamespacedName: namespaceName.String(),
	}
	if err := r.Client.List(ctx, internalServiceExportList, &listOpts); err != nil {
		klog.ErrorS(err, "Failed to list internalServiceExports used by the serviceImport", "serviceImport", serviceImportKRef)
		return ctrl.Result{}, err
	}
	if len(internalServiceExportList.Items) == 0 {
		klog.V(2).InfoS("No internalServiceExport found and deleting serviceImport", "serviceImport", serviceImportKRef)
		return r.deleteServiceImport(ctx, &serviceImport)
	}
	change := statusChange{
		conflict:   []*fleetnetv1alpha1.InternalServiceExport{},
		noConflict: []*fleetnetv1alpha1.InternalServiceExport{},
	}

	var resolvedPortsSpec *[]fleetnetv1alpha1.ServicePort
	for i := range internalServiceExportList.Items {
		v := internalServiceExportList.Items[i]
		if v.DeletionTimestamp != nil { // skip if the resource is in the deleting state
			continue
		}
		// skip if the resource is just added which has not been handled by the internalServiceExport controller yet
		if !controllerutil.ContainsFinalizer(&v, objectmeta.InternalServiceExportFinalizer) {
			klog.V(3).InfoS("Skipping the internalServiceExport because of missing finalizer", "serviceImport", serviceImportKRef, "internalServiceExport", klog.KObj(&v))
			continue
		}

		if resolvedPortsSpec == nil {
			// pick the first internalServiceExport spec
			resolvedPortsSpec = &v.Spec.Ports
		}
		// TODO: ideally we should ignore the order when comparing the serviceImports; port and protocol are the key.
		if !equality.Semantic.DeepEqual(*resolvedPortsSpec, v.Spec.Ports) {
			change.conflict = append(change.conflict, &v)
			continue
		}
		change.noConflict = append(change.noConflict, &v)
	}

	if resolvedPortsSpec == nil {
		// All of internalServicesExports are in the deleting state or waiting for the internalserviceexport controller to process it.
		// We could safely delete the serviceImport if exists.
		// When the internalserviceexport controller starts processing the object, it will create the serviceImport at
		// that time.
		klog.V(2).InfoS("No valid internalServiceExport found and deleting serviceImport", "serviceImport", serviceImportKRef)
		return r.deleteServiceImport(ctx, &serviceImport)
	}

	// To reduce reconcile failure, we'll keep retry until it succeeds.
	clusters := make([]fleetnetv1alpha1.ClusterStatus, 0, len(change.noConflict))
	for _, v := range change.noConflict {
		klog.V(3).InfoS("Marking internalServiceExport status as nonConflict", "serviceImport", serviceImportKRef, "internalServiceExport", klog.KObj(v))
		if err := r.updateInternalServiceExportWithRetry(ctx, v, false); err != nil {
			if apierrors.IsNotFound(err) { // ignore deleted internalServiceExport
				continue
			}
			return ctrl.Result{}, err
		}
		clusters = append(clusters, fleetnetv1alpha1.ClusterStatus{Cluster: v.Spec.ServiceReference.ClusterID})
	}
	if len(clusters) == 0 {
		// At that time, all of internalServiceExports has been deleted.
		// need to redo the Reconcile to pick new ports spec
		klog.V(2).InfoS("Requeue the request to resolve the spec", "serviceImport", serviceImportKRef)
		return ctrl.Result{Requeue: true}, nil
	}
	for _, v := range change.conflict {
		klog.V(3).InfoS("Marking internalServiceExport status as Conflict", "serviceImport", serviceImportKRef, "internalServiceExport", klog.KObj(v))
		if err := r.updateInternalServiceExportWithRetry(ctx, v, true); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
	}
	serviceImport.Status = fleetnetv1alpha1.ServiceImportStatus{
		Ports:    *resolvedPortsSpec,
		Clusters: clusters,
		Type:     fleetnetv1alpha1.ClusterSetIP, // may support headless in the future
	}
	updateFunc := func() error {
		return r.Status().Update(ctx, &serviceImport)
	}
	klog.V(2).InfoS("Updating the serviceImport status", "serviceImport", serviceImportKRef, "status", serviceImport.Status)
	if err := apiretry.Do(updateFunc); err != nil {
		klog.ErrorS(err, "Failed to update serviceImport status with retry", "serviceImport", serviceImportKRef, "status", serviceImport.Status)
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *Reconciler) updateInternalServiceExportWithRetry(ctx context.Context, internalServiceExport *fleetnetv1alpha1.InternalServiceExport, conflict bool) error {
	desiredCond := condition.UnconflictedServiceExportConflictCondition(*internalServiceExport)
	if conflict {
		desiredCond = condition.ConflictedServiceExportConflictCondition(*internalServiceExport)
	}
	currentCond := meta.FindStatusCondition(internalServiceExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportConflict))
	if condition.EqualCondition(currentCond, &desiredCond) {
		return nil
	}
	exportKObj := klog.KObj(internalServiceExport)
	oldStatus := internalServiceExport.Status.DeepCopy()
	meta.SetStatusCondition(&internalServiceExport.Status.Conditions, desiredCond)

	updateFunc := func() error {
		return r.Client.Status().Update(ctx, internalServiceExport)
	}
	if err := apiretry.Do(updateFunc); err != nil {
		klog.ErrorS(err, "Failed to update internalServiceExport status with retry", "internalServiceExport", exportKObj, "status", internalServiceExport.Status, "oldStatus", oldStatus)
		return err
	}
	return nil
}

func (r *Reconciler) deleteServiceImport(ctx context.Context, serviceImport *fleetnetv1alpha1.ServiceImport) (ctrl.Result, error) {
	serviceImportKObj := klog.KObj(serviceImport)
	if err := r.Client.Delete(ctx, serviceImport); err != nil {
		klog.ErrorS(err, "Failed to delete serviceImport", "serviceImport", serviceImportKObj)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	klog.V(2).InfoS("There are no internalServiceExports and serviceImport has been deleted", "serviceImport", serviceImportKObj)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	// add index to quickly query internalServiceExport list by service
	extractFunc := func(o client.Object) []string {
		name := o.(*fleetnetv1alpha1.InternalServiceExport).Spec.ServiceReference.NamespacedName
		return []string{name}
	}
	if err := mgr.GetFieldIndexer().IndexField(ctx, &fleetnetv1alpha1.InternalServiceExport{}, exportedServiceFieldNamespacedName, extractFunc); err != nil {
		klog.ErrorS(err, "Failed to create index", "field", exportedServiceFieldNamespacedName)
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetnetv1alpha1.ServiceImport{}).
		Complete(r)
}
