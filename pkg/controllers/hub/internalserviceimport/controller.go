/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package internalserviceimport features the InternalServiceImport controller for importing an exported
// service into a member cluster.
package internalserviceimport

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
)

const (
	internalSvcImportCleanupFinalizer = "networking.fleet.azure.com/internalsvcimport-cleanup"
	svcImportCleanupFinalizer         = "networking.fleet.azure.com/serviceimport-cleanup"

	internalSvcImportSvcRefNamespacedNameFieldKey = ".spec.serviceImportReference.namespacedName"

	internalSvcImportRetryInterval = time.Second * 2
)

// Reconciler reconciles an InternalServiceImport object.
type Reconciler struct {
	HubClient client.Client
}

//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=internalserviceimports,verbs=get;list;watch
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceimports,verbs=get;list
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceimports/status,verbs=get;update;patch

// Reconcile checks if a member cluster can import a Service from the hub cluster and fulfills the import.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	internalSvcImportRef := klog.KRef(req.Namespace, req.Name)
	startTime := time.Now()
	klog.V(2).InfoS("Reconciliation starts", "internalServiceImport", internalSvcImportRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Reconciliation ends", "internalServiceImport", internalSvcImportRef, "latency", latency)
	}()

	// Retrieve the InternalServiceImport object.
	internalSvcImport := &fleetnetv1alpha1.InternalServiceImport{}
	if err := r.HubClient.Get(ctx, req.NamespacedName, internalSvcImport); err != nil {
		klog.ErrorS(err, "Failed to get internalserviceimport", "internalServiceImport", internalSvcImportRef)
		// Skip the reconciliation if the InternalServiceImport does not exist.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Check if the ServiceImport exists.
	svcNS := internalSvcImport.Spec.ServiceImportReference.Namespace
	svcName := internalSvcImport.Spec.ServiceImportReference.Name
	svcImportKey := types.NamespacedName{Namespace: svcNS, Name: svcName}
	svcImportRef := klog.KRef(svcNS, svcName)

	klog.V(2).InfoS("Check if the Service can be imported", "serviceImport", svcImportRef, "internalServiceImport", internalSvcImportRef)
	svcImport := &fleetnetv1alpha1.ServiceImport{}
	err := r.HubClient.Get(ctx, svcImportKey, svcImport)
	switch {
	case err != nil && errors.IsNotFound(err):
		// The ServiceImport does not exist, and the Service will not be imported. If a Service spec has been
		// added to InternalServiceImport status, it will be cleared; the InternalServiceImport cleanup finalizer will
		// be removed as well (if applicable).
		klog.V(2).InfoS("ServiceImport does not exist; spec of imported Service (if any) will be cleared",
			"serviceImport", svcImportRef,
			"internalServiceImport", internalSvcImportRef)
		return r.clearInternalServiceImportStatus(ctx, internalSvcImport)
	case err != nil:
		// An unexpected error occurred.
		klog.ErrorS(err, "Failed to get ServiceImport", "serviceImport", svcImportRef, "internalServiceImport", internalSvcImportRef)
		return ctrl.Result{}, err
	case svcImport.DeletionTimestamp == nil && len(svcImport.Status.Clusters) == 0:
		// The ServiceImport is being processed; requeue the InternalServiceImport for later processing.
		klog.V(2).InfoS("ServiceImport is being processed; requeue for later processing",
			"serviceImport", svcImportRef,
			"internalServiceImport", internalSvcImportRef)
		return ctrl.Result{RequeueAfter: internalSvcImportRetryInterval}, nil
	}

	// Withdraw Service import request if the InternalServiceImport has been marked for deletion, or if the
	// ServceImport has been marked for deletion.
	if internalSvcImport.DeletionTimestamp != nil || svcImport.DeletionTimestamp != nil {
		if controllerutil.ContainsFinalizer(internalSvcImport, internalSvcImportCleanupFinalizer) {
			klog.V(2).InfoS("InternalServiceImport is deleted; withdraw the Service import request",
				"internalServiceImport", internalSvcImportRef)
			return r.withdrawServiceImport(ctx, svcImport, internalSvcImport)
		}
		// The absence of the InternalServiceImport cleanup finalizer guarantees that no attempt has been made to
		// import the Service to the member cluster, and as a result, no action is needed on this controller's end.
		return ctrl.Result{}, nil
	}

	// The cluster namespace and ID of the member cluster which attempts to import the Service.
	clusterNamespace := fleetnetv1alpha1.ClusterNamespace(internalSvcImport.Namespace)
	clusterID := fleetnetv1alpha1.ClusterID(internalSvcImport.Spec.ServiceImportReference.ClusterID)

	// Find out which member clusters have imported the Service.
	svcInUseBy := extractServiceInUseByInfoFromServiceImport(svcImport)
	if len(svcInUseBy.MemberClusters) > 0 {
		if _, ok := svcInUseBy.MemberClusters[clusterNamespace]; ok {
			// The current member cluster has already imported Service; fulfill the import (i.e. update the Service
			// spec kept in InternalServiceImport status).
			klog.V(2).InfoS("The member cluster has imported the Service; will sync the imported Service spec",
				"serviceImport", svcImportRef,
				"internalServiceImport", internalSvcImportRef)
			if err := r.fulfillInternalServiceImport(ctx, svcImport, internalSvcImport); err != nil {
				klog.ErrorS(err, "Failed to fulfill service import by updating InternalServiceImport status",
					"serviceImport", svcImportRef,
					"internalServiceImport", internalSvcImportRef)
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
		// Another member cluster has already imported the Service; at this moment, it is required that one Service
		// can only be imported exactly once across the whole fleet, and as a result, attempt to import the Service
		// by the current member cluster will be aborted.
		klog.V(2).InfoS("A member cluster has already imported the Service",
			"internalServiceImport", internalSvcImportRef,
			"serviceInUseBy", svcInUseBy)
		return r.clearInternalServiceImportStatus(ctx, internalSvcImport)
	}

	klog.V(2).InfoS("The Service can be imported; will sync the Service spec",
		"serviceImport", svcImportRef,
		"internalServiceImport", internalSvcImportRef)
	// Add cleanup finalizer to InternalServiceImport. This must happen before an attempt to import a Service
	// is fulfilled.
	if err := r.addInternalServiceImportCleanupFinalizer(ctx, internalSvcImport); err != nil {
		klog.ErrorS(err, "Failed to add cleanup finalizer to InternalServiceImport", "internalServiceImport", internalSvcImportRef)
		return ctrl.Result{}, err
	}

	// Update the ServiceInUseBy annotation, which claims the Service for the current member cluster to import.
	svcInUseBy.MemberClusters[clusterNamespace] = clusterID
	if err := r.annotateServiceImportWithServiceInUseByInfo(ctx, svcImport, svcInUseBy); err != nil {
		klog.ErrorS(err, "Failed to annotate ServiceImport with ServiceInUseBy info",
			"serviceImport", svcImportRef,
			"serviceInUseBy", svcInUseBy)
		return ctrl.Result{}, err
	}

	// Fulfill the import (i.e. update the Service spec kept in InternalServiceImport status).
	if err := r.fulfillInternalServiceImport(ctx, svcImport, internalSvcImport); err != nil {
		klog.ErrorS(err, "Failed to fulfill service import by updating InternalServiceImport status",
			"serviceImport", svcImportRef,
			"internalServiceImport", internalSvcImportRef)
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the InternalServiceImport controller with a controller manager.
func (r *Reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	// Set up an index for efficient InternalServiceImport lookup.
	internalSvcImportIndexerFunc := func(o client.Object) []string {
		internalSvcImport, ok := o.(*fleetnetv1alpha1.InternalServiceImport)
		if !ok {
			return []string{}
		}
		return []string{internalSvcImport.Spec.ServiceImportReference.NamespacedName}
	}
	if err := mgr.GetFieldIndexer().IndexField(ctx,
		&fleetnetv1alpha1.InternalServiceImport{},
		internalSvcImportSvcRefNamespacedNameFieldKey,
		internalSvcImportIndexerFunc,
	); err != nil {
		klog.ErrorS(err, "Failed to set up InternalServiceImport index")
		return err
	}

	// Enqueue InternalServiceImports for processing when a ServiceImport changes.
	eventHandlers := handler.EnqueueRequestsFromMapFunc(func(o client.Object) []reconcile.Request {
		svcImport, ok := o.(*fleetnetv1alpha1.ServiceImport)
		if !ok {
			return []reconcile.Request{}
		}

		internalSvcImportList := &fleetnetv1alpha1.InternalServiceImportList{}
		fieldMatcher := client.MatchingFields{
			internalSvcImportSvcRefNamespacedNameFieldKey: fmt.Sprintf("%s/%s", svcImport.Namespace, svcImport.Name),
		}
		if err := r.HubClient.List(ctx, internalSvcImportList, fieldMatcher); err != nil {
			klog.ErrorS(err, "Failed to list InternalServiceImports for an ServiceImport", "serviceImport", klog.KObj(svcImport))
			return []reconcile.Request{}
		}

		reqs := make([]reconcile.Request, 0, len(internalSvcImportList.Items))
		for _, internalSvcImport := range internalSvcImportList.Items {
			reqs = append(reqs, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: internalSvcImport.Namespace,
					Name:      internalSvcImport.Name,
				},
			})
		}
		return reqs
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetnetv1alpha1.InternalServiceImport{}).
		Watches(&source.Kind{Type: &fleetnetv1alpha1.ServiceImport{}}, eventHandlers).
		Complete(r)
}

// withdrawServiceImport withdraws the request to import a Service to a member cluster.
func (r *Reconciler) withdrawServiceImport(ctx context.Context,
	svcImport *fleetnetv1alpha1.ServiceImport,
	internalSvcImport *fleetnetv1alpha1.InternalServiceImport) (ctrl.Result, error) {
	// The cluster namespace of the member cluster which imports the Service.
	clusterNamespace := fleetnetv1alpha1.ClusterNamespace(internalSvcImport.Namespace)

	// Update the annotated ServiceInUseBy information.
	svcInUseBy := extractServiceInUseByInfoFromServiceImport(svcImport)
	if _, ok := svcInUseBy.MemberClusters[clusterNamespace]; ok {
		delete(svcInUseBy.MemberClusters, clusterNamespace)
		switch {
		case len(svcInUseBy.MemberClusters) > 0:
			// There are still member clusters importing the Service after the withdrawal; the ServiceInUseBy
			// annotation will be updated. Note that with current semantics (one import only across the fleet)
			// this branch will not run.
			if err := r.annotateServiceImportWithServiceInUseByInfo(ctx, svcImport, svcInUseBy); err != nil {
				klog.ErrorS(err, "Failed to annotate ServiceImport with ServiceInUseBy info",
					"serviceImport", klog.KObj(svcImport),
					"serviceInUseBy", svcInUseBy)
				return ctrl.Result{}, err
			}
		case len(svcInUseBy.MemberClusters) == 0:
			// No member cluster imports the Service after the withdrawal; the ServiceInUseBy annotation (and
			// the cleanup finalizer) on ServiceImport will be cleared.
			if err := r.clearServiceInUseByInfoFromServiceImport(ctx, svcImport); err != nil {
				klog.ErrorS(err, "Failed to clear ServiceImport ServiceInUseBy annotation",
					"serviceImport", klog.KObj(svcImport),
					"serviceInUseBy", svcInUseBy)
				return ctrl.Result{}, err
			}
		}
	}
	// A rare occurrence as it is, it could happen that the InternalServiceImport has the cleanup finalizer,
	// yet the import is not annotated on the ServiceImport. This is usually caused by data corruption, or
	// direct ServiceInUseBy annotation manipulation by the user; and in this case the controller will skip
	// the updating.

	// Remove the cleanup finalizer.
	if err := r.removeInternalServiceImportCleanupFinalizer(ctx, internalSvcImport); err != nil {
		klog.ErrorS(err, "Failed to remove cleanup finalizer from InternalServiceImport", "internalServiceImport", klog.KObj(internalSvcImport))
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// clearInternalServiceImportStatus clears the status (Service spec) from an InternalServiceImport; if the
// InternalServiceImport has a cleanup finalizer added, it will be removed as well.
func (r *Reconciler) clearInternalServiceImportStatus(ctx context.Context, internalSvcImport *fleetnetv1alpha1.InternalServiceImport) (ctrl.Result, error) {
	// Remove the cleanup finalizer from InternalServiceImport (if applicable).
	if err := r.removeInternalServiceImportCleanupFinalizer(ctx, internalSvcImport); err != nil {
		klog.ErrorS(err, "Failed to remove cleanup finalizer from InternalServiceImport", "internalServiceImport", klog.KObj(internalSvcImport))
		return ctrl.Result{}, err
	}

	clearedInternalSvcImportStatus := fleetnetv1alpha1.ServiceImportStatus{}
	if reflect.DeepEqual(internalSvcImport.Status, clearedInternalSvcImportStatus) {
		// The state has stablized; skip the clearing.
		return ctrl.Result{}, nil
	}
	internalSvcImport.Status = clearedInternalSvcImportStatus
	if err := r.HubClient.Status().Update(ctx, internalSvcImport); err != nil {
		klog.ErrorS(err, "Failed to clear InternalServiceImport status", "internalServiceImport", klog.KObj(internalSvcImport))
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// removeInternalServiceImportCleanupFinalizer removes the cleanup finalizer from an InternalServiceImport.
func (r *Reconciler) removeInternalServiceImportCleanupFinalizer(ctx context.Context, internalSvcImport *fleetnetv1alpha1.InternalServiceImport) error {
	if controllerutil.ContainsFinalizer(internalSvcImport, internalSvcImportCleanupFinalizer) {
		controllerutil.RemoveFinalizer(internalSvcImport, internalSvcImportCleanupFinalizer)
		return r.HubClient.Update(ctx, internalSvcImport)
	}
	return nil
}

// addInternalServiceImportCleanupFinalizer adds the cleanup finalizer to an InternalServiceImport.
func (r *Reconciler) addInternalServiceImportCleanupFinalizer(ctx context.Context, internalSvcImport *fleetnetv1alpha1.InternalServiceImport) error {
	if !controllerutil.ContainsFinalizer(internalSvcImport, internalSvcImportCleanupFinalizer) {
		controllerutil.AddFinalizer(internalSvcImport, internalSvcImportCleanupFinalizer)
		return r.HubClient.Update(ctx, internalSvcImport)
	}
	return nil
}

// annotateServiceImportWithServiceInUseByInfo annotates ServiceInUseBy information on a ServiceImport.
func (r *Reconciler) annotateServiceImportWithServiceInUseByInfo(ctx context.Context,
	svcImport *fleetnetv1alpha1.ServiceImport,
	svcInUseBy *fleetnetv1alpha1.ServiceInUseBy) error {
	if svcImport.Annotations == nil {
		// Initialize the annoation map if no annotations have been added before.
		svcImport.Annotations = map[string]string{}
	}

	data, err := json.Marshal(svcInUseBy)
	if err != nil {
		return err
	}

	svcImport.Annotations[objectmeta.ServiceInUseByAnnotationKey] = string(data)
	controllerutil.AddFinalizer(svcImport, svcImportCleanupFinalizer)
	return r.HubClient.Status().Update(ctx, svcImport)
}

// clearServiceInUseByInfoFromServiceImport clears the ServiceInUseBy annotation from a ServiceImport, and its
// cleanup finalizer.
func (r *Reconciler) clearServiceInUseByInfoFromServiceImport(ctx context.Context, svcImport *fleetnetv1alpha1.ServiceImport) error {
	delete(svcImport.Annotations, objectmeta.ServiceInUseByAnnotationKey)
	controllerutil.RemoveFinalizer(svcImport, svcImportCleanupFinalizer)
	return r.HubClient.Update(ctx, svcImport)
}

// fulfillInternalServiceImport fulfills an import of a Service by syncing the Service spec to the status of an
// InternalServiceImport.
func (r *Reconciler) fulfillInternalServiceImport(ctx context.Context,
	svcImport *fleetnetv1alpha1.ServiceImport,
	internalSvcImport *fleetnetv1alpha1.InternalServiceImport) error {
	updatedInternalSvcImportStatus := svcImport.Status.DeepCopy()
	if reflect.DeepEqual(internalSvcImport.Status, updatedInternalSvcImportStatus) {
		// The state has stablized; skip the fulfillment.
		return nil
	}
	internalSvcImport.Status = *updatedInternalSvcImportStatus

	return r.HubClient.Status().Update(ctx, internalSvcImport)
}

// extractServiceInUseByInfoFromServiceImport extracts ServiceInUseBy information from annotations on a ServiceImport.
func extractServiceInUseByInfoFromServiceImport(svcImport *fleetnetv1alpha1.ServiceImport) *fleetnetv1alpha1.ServiceInUseBy {
	data, ok := svcImport.ObjectMeta.Annotations[objectmeta.ServiceInUseByAnnotationKey]
	if !ok {
		// The ServiceInUseBy annotation is absent on ServiceImport.
		return &fleetnetv1alpha1.ServiceInUseBy{
			MemberClusters: map[fleetnetv1alpha1.ClusterNamespace]fleetnetv1alpha1.ClusterID{},
		}
	}

	svcInUseBy := &fleetnetv1alpha1.ServiceInUseBy{}
	if err := json.Unmarshal([]byte(data), svcInUseBy); err != nil {
		// The data cannot be unmarshalled; normally this should never happen, unless data corruption occurs,
		// or the annotations are manipulated directly by the user. The controller will have to overwrite
		// the data to fix the problem.
		//
		// Note that this situation, should it ever happens, can lead to an inconsistent state where one or more
		// member clusters believe that it has successfully imported a Service, yet the import itself is not
		// recognized by the fleet networking control plane (more specifically, the import is not documented
		// as ServiceImport annotations). Resync can eventually address this inconsistency, but it may take a long
		// while for the system to recover.
		klog.ErrorS(err, "Failed to unmarshal ServiceInUseBy data", "serviceImport", klog.KObj(svcImport), "data", data)
		return &fleetnetv1alpha1.ServiceInUseBy{
			MemberClusters: map[fleetnetv1alpha1.ClusterNamespace]fleetnetv1alpha1.ClusterID{},
		}
	}
	return svcInUseBy
}
