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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/internalserviceexport"
)

const (
	// fields name used to filter resources
	exportedServiceFieldNamespacedName = ".spec.serviceReference.namespacedName"
)

// Reconciler reconciles a ServiceImport object.
type Reconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// statusChange stores the internalServiceExports list whose status needs to be updated.
type statusChange struct {
	conflict   []*fleetnetv1alpha1.InternalServiceExport
	noConflict []*fleetnetv1alpha1.InternalServiceExport
}

//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceimports,verbs=get;list;watch;update;patch;delete
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceimports/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceimports/finalizers,verbs=update
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=internalserviceexports,verbs=list
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
		// All of internalServicesExports are in the deleting state.
		return r.deleteServiceImport(ctx, &serviceImport)
	}

	// To reduce reconcile failure, we'll keep retry until it succeeds.
	clusters := make([]fleetnetv1alpha1.ClusterStatus, 0, len(internalServiceExportList.Items))
	for _, v := range change.noConflict {
		klog.V(3).InfoS("Marking internalServiceExport status as nonConflict", "serviceImport", serviceImportKRef, "internalServiceExport", klog.KObj(v))
		if err := internalserviceexport.UpdateStatus(ctx, r.Client, v, false, true); err != nil {
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
		if err := internalserviceexport.UpdateStatus(ctx, r.Client, v, true, true); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
	}
	serviceImport.Status = fleetnetv1alpha1.ServiceImportStatus{
		Ports:    *resolvedPortsSpec,
		Clusters: clusters,
		Type:     fleetnetv1alpha1.ClusterSetIP, // may support headless in the future
	}
	if err := r.updateServiceImportStatusWithRetry(ctx, &serviceImport); err != nil {
		klog.ErrorS(err, "Failed to update serviceImport status with retry", "serviceImport", serviceImportKRef, "status", serviceImport.Status)
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r Reconciler) deleteServiceImport(ctx context.Context, serviceImport *fleetnetv1alpha1.ServiceImport) (ctrl.Result, error) {
	serviceImportKObj := klog.KObj(serviceImport)
	if err := r.Client.Delete(ctx, serviceImport); err != nil {
		klog.ErrorS(err, "Failed to delete serviceImport", "serviceImport", serviceImportKObj)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	klog.V(2).InfoS("There are no internalServiceExports and serviceImport has been deleted", "serviceImport", serviceImportKObj)
	return ctrl.Result{}, nil
}

func (r *Reconciler) updateServiceImportStatusWithRetry(ctx context.Context, serviceImport *fleetnetv1alpha1.ServiceImport) error {
	backOffPeriod := retry.DefaultBackoff
	backOffPeriod.Cap = time.Second * 1

	return retry.OnError(backOffPeriod,
		func(err error) bool {
			if apierrors.IsNotFound(err) || apierrors.IsInvalid(err) {
				return false
			}
			return true
		},
		func() error {
			return r.Status().Update(ctx, serviceImport)
		})
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	ctx := context.Background()
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
