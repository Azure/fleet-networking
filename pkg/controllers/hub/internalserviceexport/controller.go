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

	// fields name used to filter resources
	exportedServiceFieldNamespacedName = "spec.serviceReference.namespacedName"

	conditionReasonNoConflictFound = "NoConflictFound"
	conditionReasonConflictFound   = "ConflictFound"
)

// Reconciler reconciles a InternalServiceExport object.
type Reconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=internalserviceexports,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=internalserviceexports/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=internalserviceexports/finalizers,verbs=update
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceimports,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceimports/status,verbs=get;update;patch

// Reconcile creates/updates/deletes ServiceImport by watching internalServiceExport objects and handles the service spec.
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
		klog.ErrorS(err, "Failed to get serviceImport", "serviceImport", serviceImportKRef)
		// NotFound could happen when this serviceExport is the last one and a failure happens after we delete
		// serviceImport before removing the finalizer.
		if errors.IsNotFound(err) {
			return r.removeFinalizer(ctx, internalServiceExport)
		}
		return ctrl.Result{}, err
	}
	desiredServiceImportStatus := r.calculateDesiredServiceImportStatus(internalServiceExport, serviceImport)

	// If there is no more clusters exported service with the same spec, we need to pick the new service spec from the
	// exported services.
	change, err := r.resolveExportedServiceSpec(ctx, serviceImport, desiredServiceImportStatus)
	if err != nil {
		return ctrl.Result{}, err
	}

	// update the service import status before updating the internalServiceExport
	// If failures happen after updating the service import status, other newly created serviceExport with the same
	// spec will still be treated as the valid one. There could be some latency to update the serviceExport status.
	// It should be eventually reflected on the serviceExport status after retries.
	serviceImportKObj := klog.KObj(serviceImport)
	klog.V(2).InfoS("Updating the serviceImport status", "internalServiceExport", internalServiceExportKObj, "serviceImport", serviceImportKObj, "oldStatus", serviceImport.Status, "status", desiredServiceImportStatus)
	serviceImport.Status = *desiredServiceImportStatus
	if err := r.updateServiceImport(ctx, serviceImport); err != nil {
		return ctrl.Result{}, err
	}

	// If resolved spec has been changed, we need to update the ObservedGeneration of the conflicted objects, which is
	// useful for the troubleshooting.
	for i := range change.conflict {
		if err := r.updateInternalServiceExportStatus(ctx, change.conflict[i], true); err != nil {
			return ctrl.Result{}, err
		}
	}
	for i := range change.noConflict {
		if err := r.updateInternalServiceExportStatus(ctx, change.noConflict[i], false); err != nil {
			return ctrl.Result{}, err
		}
	}
	return r.removeFinalizer(ctx, internalServiceExport)
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

// calculateDesiredServiceImportStatus will create a new desired serviceImportStatus by removing the internalServiceExport
// cluster.
// This function won't change the original serviceImport.
func (*Reconciler) calculateDesiredServiceImportStatus(internalServiceExport *fleetnetv1alpha1.InternalServiceExport, serviceImport *fleetnetv1alpha1.ServiceImport) *fleetnetv1alpha1.ServiceImportStatus {
	desiredServiceImportStatus := serviceImport.Status.DeepCopy()
	var updatedClusters []fleetnetv1alpha1.ClusterStatus
	for _, c := range serviceImport.Status.Clusters {
		if c.Cluster != internalServiceExport.Spec.ServiceReference.ClusterID {
			updatedClusters = append(updatedClusters, c)
		}
	}
	desiredServiceImportStatus.Clusters = updatedClusters
	return desiredServiceImportStatus
}

// updateServiceImport updates the serviceImport status.
func (r *Reconciler) updateServiceImport(ctx context.Context, serviceImport *fleetnetv1alpha1.ServiceImport) error {
	serviceImportKObj := klog.KObj(serviceImport)

	if len(serviceImport.Status.Clusters) != 0 {
		klog.V(2).InfoS("Updating the serviceImport status to remove cluster", "serviceImport", serviceImportKObj, "status", serviceImport.Status)
		if err := r.Client.Status().Update(ctx, serviceImport); err != nil {
			klog.ErrorS(err, "Failed to update the serviceImport status to remove cluster", "serviceImport", serviceImportKObj, "status", serviceImport.Status)
			return err
		}
		return nil
	}

	klog.V(2).InfoS("Removing the serviceImport as there is no service export", "serviceImport", serviceImportKObj)
	if err := r.Client.Delete(ctx, serviceImport); err != nil {
		klog.ErrorS(err, "Failed to remove the serviceImport", "serviceImport", serviceImportKObj)
		return client.IgnoreNotFound(err)
	}
	return nil
}

// statusChange stores the internalServiceExports list whose status needs to be updated.
type statusChange struct {
	conflict   []*fleetnetv1alpha1.InternalServiceExport
	noConflict []*fleetnetv1alpha1.InternalServiceExport
}

// resolveExportedServiceSpec picks service spec from exported services if there is no more serviceExports for the
// existing spec.
// We don't support KEP1645 Constraints and Conflict Resolution for now, which is defined.
// https://github.com/kubernetes/enhancements/tree/master/keps/sig-multicluster/1645-multi-cluster-services-api#constraints-and-conflict-resolution
// It rebuilds the desiredServiceImportStatus.
// It returns the internalServiceExports list whose status needs to be updated.
func (r *Reconciler) resolveExportedServiceSpec(ctx context.Context, serviceImport *fleetnetv1alpha1.ServiceImport, desiredServiceImportStatus *fleetnetv1alpha1.ServiceImportStatus) (statusChange, error) {
	var resolvedPortsSpec *[]fleetnetv1alpha1.ServicePort
	if len(desiredServiceImportStatus.Clusters) != 0 {
		// use the existing ports spec
		resolvedPortsSpec = &serviceImport.Status.Ports
		// We still need to figure out the conflict and noConflict list to handle one case:
		// When failures happen after reconciler updates the serviceImport and before it updates the internalServiceImport
		// status, the next reconcile has to update the status.
		// So we cannot return here.
	}
	internalServiceExportList := &fleetnetv1alpha1.InternalServiceExportList{}
	listOpts := client.MatchingFields{
		exportedServiceFieldNamespacedName: serviceImport.Name,
	}
	if err := r.Client.List(ctx, internalServiceExportList, &listOpts); err != nil {
		klog.ErrorS(err, "Failed to list internalServiceExports used by the serviceImport", "serviceImport", klog.KObj(serviceImport))
		return statusChange{}, err
	}

	change := statusChange{
		conflict:   []*fleetnetv1alpha1.InternalServiceExport{},
		noConflict: []*fleetnetv1alpha1.InternalServiceExport{},
	}
	clusters := make([]fleetnetv1alpha1.ClusterStatus, 0, len(internalServiceExportList.Items))
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
		clusters = append(clusters, fleetnetv1alpha1.ClusterStatus{Cluster: v.Spec.ServiceReference.ClusterID})
		change.noConflict = append(change.noConflict, &v)
	}

	// rebuild the desired status
	// If there is no exported service, the status will be empty.
	desiredServiceImportStatus.Clusters = clusters
	desiredServiceImportStatus.Type = fleetnetv1alpha1.ClusterSetIP // may support headless in the future
	if resolvedPortsSpec != nil {
		desiredServiceImportStatus.Ports = *resolvedPortsSpec
	}
	return change, nil
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

func (r *Reconciler) handleUpdate(_ context.Context, _ *fleetnetv1alpha1.InternalServiceExport) (ctrl.Result, error) {
	return ctrl.Result{}, nil
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
		For(&fleetnetv1alpha1.InternalServiceExport{}).
		Complete(r)
}
