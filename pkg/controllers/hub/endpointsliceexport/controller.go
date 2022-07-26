/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package endpointsliceexport features the EndpointSliceExport controller running on the hub cluster, which
// is responsible for distributing EndpointSlices exported from member clusters.
package endpointsliceexport

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/consts"
)

const (
	endpointSliceExportCleanupFinalizer = "networking.fleet.azure.com/endpointsliceexport-cleanup"

	endpointSliceImportNameFieldKey                   = ".metadata.name"
	endpointSliceExportOwnerSvcNamespacedNameFieldKey = ".spec.ownerServiceReference.namespacedName"

	endpointSliceExportRetryInterval = time.Second * 5
)

// Reconciler reconciles the distribution of EndpointSlices across the fleet.
type Reconciler struct {
	hubClient            client.Client
	fleetSystemNamespace string
}

//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=endpointsliceexports,verbs=get;list;watch;create;update;patch
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=endpointsliceimports,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceimports,verbs=get;list;watch

// Reconcile distributes an exported EndpointSlice (in the form of EndpointSliceExports) to whichever member
// cluster that has imported the EndpointSlice's owner Service.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	endpointSliceExportRef := klog.KRef(req.Namespace, req.Name)
	startTime := time.Now()
	klog.V(2).InfoS("Reconciliation starts", "endpointSliceExport", endpointSliceExportRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Reconciliation ends", "endpointSliceExport", endpointSliceExportRef, "latency", latency)
	}()

	// Retrieve the EndpointSliceExport object.
	endpointSliceExport := &fleetnetv1alpha1.EndpointSliceExport{}
	if err := r.hubClient.Get(ctx, req.NamespacedName, endpointSliceExport); err != nil {
		// Skip the reconciliation if the EndpointSliceExport does not exist; this should only happen when the
		// EndpointSliceExport does not have a finalizer set and is deleted right before the controller gets a
		// chance to reconcile it. The absence of the finalizer guarantees that the EndpointSlice has never been
		// distributed across the fleet, thus no action is needed on this controller's side.
		klog.ErrorS(err, "Failed to get EndpointSliceExport", "endpointSliceExport", endpointSliceExportRef)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Check if the EndpointSliceExport has been marked for deletion; withdraw EndpointSliceImports across
	// the fleet if the EndpointSlice has been distributed.
	if endpointSliceExport.DeletionTimestamp != nil {
		if controllerutil.ContainsFinalizer(endpointSliceExport, endpointSliceExportCleanupFinalizer) {
			// The presence of the EndpointSliceExport cleanup finalizer guarantees that an attempt has been made
			// to distribute the EndpointSlice.
			klog.V(2).InfoS("EndpointSliceExport deleted; withdraw distributed EndpointSlices", "endpointSliceExport", endpointSliceExportRef)
			if err := r.withdrawAllEndpointSliceImports(ctx, endpointSliceExport); err != nil {
				return ctrl.Result{}, err
			}
			if err := r.removeHubEndpointSliceCopyAndCleanupFinalizer(ctx, endpointSliceExport); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
	}

	// Add cleanup finalizer to the EndpointSliceExport; this must happen before EndpointSlice is distributed.
	if !controllerutil.ContainsFinalizer(endpointSliceExport, endpointSliceExportCleanupFinalizer) {
		if err := r.addEndpointSliceExportCleanupFinalizer(ctx, endpointSliceExport); err != nil {
			klog.ErrorS(err, "Failed to add cleanup finalizer to EndpointSliceExport", "endpointSliceExport", endpointSliceExportRef)
			return ctrl.Result{}, err
		}
	}

	// Keep one copy of EndpointSlice in the hub cluster for reference. This will always happen even if its owner
	// Service has not been imported by any member cluster yet.
	klog.V(2).InfoS("Create/update the local EndpointSlice copy",
		"endpointSlice", klog.KRef(r.fleetSystemNamespace, endpointSliceExport.Name),
		"endpointSliceExport", endpointSliceExportRef)
	endpointSlice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: r.fleetSystemNamespace,
			Name:      endpointSliceExport.Name,
		},
	}
	op, err := controllerutil.CreateOrUpdate(ctx, r.hubClient, endpointSlice, func() error {
		formatEndpointSliceFromExport(endpointSlice, endpointSliceExport)
		return nil
	})
	if err != nil {
		klog.ErrorS(err, "Failed to create or update EndpointSlice",
			"endpointSlice", klog.KObj(endpointSlice),
			"endpointSliceExport", endpointSliceExportRef,
			"op", op)
		return ctrl.Result{}, err
	}

	// Inquire the corresponding ServiceImport to find out which member clusters the EndpointSlice should be
	// distributed to.
	ownerSvcNS := endpointSliceExport.Spec.OwnerServiceReference.Namespace
	ownerSvc := endpointSliceExport.Spec.OwnerServiceReference.Name
	svcImportKey := types.NamespacedName{Namespace: ownerSvcNS, Name: ownerSvc}
	svcImport := &fleetnetv1alpha1.ServiceImport{}
	svcImportRef := klog.KRef(ownerSvcNS, ownerSvc)
	klog.V(2).InfoS("Inquire ServceImport to find out which member clusters have requested the EndpointSlice",
		"serviceImport", svcImportRef,
		"endpointSliceExport", endpointSliceExportRef)
	err = r.hubClient.Get(ctx, svcImportKey, svcImport)
	switch {
	case err != nil && errors.IsNotFound(err):
		// The corresponding ServiceImport does not exist; normally this will never happen as an EndpointSlice can
		// only be exported after its owner Service has been successfully exported. It could be that the controller
		// observes some in-between state, such as a Service is deleted right after being exported successfully,
		// and the system does not get to withdraw exported EndpointSlices from the Service yet. The controller
		// will requeue the EndpointSliceExport and wait until the state stablizes.
		klog.V(2).InfoS("ServiceImport does not exist", "serviceImport", svcImportRef, "endpointSliceExport", endpointSliceExportRef)
		return ctrl.Result{RequeueAfter: endpointSliceExportRetryInterval}, nil
	case err != nil:
		// An unexpected error occurs.
		klog.ErrorS(err, "Failed to get ServiceImport", "serviceImport", svcImportRef, "endpointSliceExport", endpointSliceExportRef)
		return ctrl.Result{}, err
	case len(svcImport.Status.Clusters) == 0:
		// The corresponding ServiceImport exists but it is still being processed. This is also a case that
		// should not happen in normal situations. The controller could be, once again, observing some in-between
		// state. The EndpointSliceExport will be requeued and re-processed when the state stablizes.
		klog.V(2).InfoS("ServiceImport is being processed (no accepted exports yet)",
			"serviceImport", svcImportRef,
			"endpointSliceExport", endpointSliceExportRef)
		return ctrl.Result{RequeueAfter: endpointSliceExportRetryInterval}, nil
	}

	data, ok := svcImport.ObjectMeta.Annotations[consts.ServiceInUseByAnnotationKey]
	if !ok {
		// No cluster has requested to import the EndpointSlice's owner service.
		// If the exported EndpointSlice has been distributed across the fleet before; withdraw the
		// EndpointSliceImports.
		klog.V(2).InfoS("No cluster has requested to import the Service; withdraw distributed EndpointSlices",
			"serviceImport", svcImportRef,
			"endpointSliceExport", endpointSliceExportRef)
		if err := r.withdrawAllEndpointSliceImports(ctx, endpointSliceExport); err != nil {
			return ctrl.Result{}, err
		}
		// There is no need to remove the local EndpointSlice copy in this situation.
		return ctrl.Result{}, nil
	}

	svcInUseBy := &fleetnetv1alpha1.ServiceInUseBy{}
	if err := json.Unmarshal([]byte(data), svcInUseBy); err != nil {
		klog.ErrorS(err, "Failed to unmarshal data for in-use Services from ServiceImport annotations",
			"serviceImport", svcImportRef,
			"endpointSliceExport", endpointSliceExportRef)
		// This error cannot be recovered by retrying; a reconciliation will be triggered when the ServiceInUseBy
		// data is updated.
		return ctrl.Result{}, nil
	}

	// Distribute the EndpointSlices.

	// Scan for EndpointSlices to withdraw and EndpointSlices to create or update.
	klog.V(2).InfoS("Scan for EndpointSliceImports to withdraw and to create/update",
		"serviceInUseBy", svcInUseBy,
		"endpointSliceExport", endpointSliceExport)
	endpointSliceImportsToWithdraw, endpointSlicesImportsToCreateOrUpdate, err := r.scanForEndpointSliceImports(ctx, endpointSliceExport, svcInUseBy)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Delete distributed EndpointSlices that are no longer needed.
	//
	// Note: At this moment, it is guaranteed that any Service can only be imported once across the fleet, consequently
	// len(endpointSliceImportsToDelete) is at most 1. However, this behavior is subject to change as fleet
	// networking evolves, and for future compatibility reasons, the function assumes that a Service might have been
	// imported to multiple clusters.
	for idx := range endpointSliceImportsToWithdraw {
		endpointSliceImport := endpointSliceImportsToWithdraw[idx]
		// Skip if the EndpointSliceImport has been marked for deletion.
		if endpointSliceImport.DeletionTimestamp != nil {
			continue
		}
		klog.V(4).InfoS("Withdraw endpointSlice",
			"endpointSliceImport", klog.KObj(endpointSliceImport),
			"endpointSliceExport", endpointSliceExportRef)
		// TO-DO (chenyu1): Add retry with exponential backoff logic to guard against transitory errors
		// (e.g. API server throuttling).
		if err := r.hubClient.Delete(ctx, endpointSliceImport); err != nil && !errors.IsNotFound(err) {
			klog.ErrorS(err, "Failed to withdraw EndpointSlice",
				"endpointSliceImport", klog.KObj(endpointSliceImport),
				"endpointSliceExport", endpointSliceExportRef)
			return ctrl.Result{}, err
		}
	}

	// Create or update distributed EndpointSlices.
	//
	// Note: At this moment, it is guaranteed that any Service can only be imported once across the fleet, consequently
	// len(endpointSliceImportsToCreateOrUpdate) is at most 1. However, this behavior is subject to change as fleet
	// networking evolves, and for future compatibility reasons, the function assumes that a Service might have been
	// imported to multiple clusters.
	for idx := range endpointSlicesImportsToCreateOrUpdate {
		endpointSliceImport := endpointSlicesImportsToCreateOrUpdate[idx]
		klog.V(4).InfoS("Create/update endpointSliceImport",
			"endpointSliceImport", klog.KObj(endpointSliceImport),
			"endpointSliceExport", endpointSliceExportRef)
		// TO-DO (chenyu1): Add retry with exponential backoff logic to guard against transitory errors
		// (e.g. API server throuttling).
		op, err := controllerutil.CreateOrUpdate(ctx, r.hubClient, endpointSliceImport, func() error {
			endpointSliceImport.Spec = *endpointSliceExport.Spec.DeepCopy()
			return nil
		})
		if err != nil {
			klog.ErrorS(err, "Failed to create or update EndpointSliceImport",
				"endpointSliceImport", klog.KObj(endpointSliceImport),
				"endpointSliceExport", endpointSliceExportRef,
				"op", op)
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the EndpointSliceExport controller with a controller manager.
func (r *Reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	// Set up an index for efficient EndpointSliceImport lookup.
	endpointSliceImportIndexerFunc := func(o client.Object) []string {
		endpointSliceImport, ok := o.(*fleetnetv1alpha1.EndpointSliceImport)
		if !ok {
			return []string{}
		}
		return []string{endpointSliceImport.ObjectMeta.Name}
	}
	if err := mgr.GetFieldIndexer().IndexField(ctx,
		&fleetnetv1alpha1.EndpointSliceImport{},
		endpointSliceImportNameFieldKey,
		endpointSliceImportIndexerFunc,
	); err != nil {
		return err
	}

	// Set up an index for efficient EndpointSliceExport lookup.
	endpointSliceExportIndexerFunc := func(o client.Object) []string {
		endpointSliceExport, ok := o.(*fleetnetv1alpha1.EndpointSliceExport)
		if !ok {
			return []string{}
		}
		return []string{endpointSliceExport.Spec.OwnerServiceReference.NamespacedName}
	}
	if err := mgr.GetFieldIndexer().IndexField(ctx,
		&fleetnetv1alpha1.EndpointSliceExport{},
		endpointSliceExportOwnerSvcNamespacedNameFieldKey,
		endpointSliceExportIndexerFunc,
	); err != nil {
		return err
	}

	// Enqueue EndpointSliceExports for processing when a ServiceImport changes.
	eventHandlers := handler.EnqueueRequestsFromMapFunc(func(o client.Object) []reconcile.Request {
		svcImport, ok := o.(*fleetnetv1alpha1.ServiceImport)
		if !ok {
			return []reconcile.Request{}
		}

		endpointSliceExportList := &fleetnetv1alpha1.EndpointSliceExportList{}
		fieldMatcher := client.MatchingFields{
			endpointSliceExportOwnerSvcNamespacedNameFieldKey: fmt.Sprintf("%s/%s", svcImport.Namespace, svcImport.Name),
		}
		if err := r.hubClient.List(ctx, endpointSliceExportList, fieldMatcher); err != nil {
			klog.ErrorS(err,
				"Failed to list EndpointSliceExports for an imported Service",
				"serviceImport", klog.KObj(svcImport))
			return []reconcile.Request{}
		}

		reqs := make([]reconcile.Request, 0, len(endpointSliceExportList.Items))
		for _, endpointSliceExport := range endpointSliceExportList.Items {
			reqs = append(reqs, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: endpointSliceExport.Namespace,
					Name:      endpointSliceExport.Name,
				},
			})
		}
		return reqs
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetnetv1alpha1.EndpointSliceExport{}).
		Watches(&source.Kind{Type: &fleetnetv1alpha1.ServiceImport{}}, eventHandlers).
		Complete(r)
}

// withdrawEndpointSliceImports withdraws EndpointSliceImports distributed across the fleet.
func (r *Reconciler) withdrawAllEndpointSliceImports(ctx context.Context, endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport) error {
	// List all EndpointSlices distributed as EndpointSliceImports.
	endpointSliceImportList := &fleetnetv1alpha1.EndpointSliceImportList{}
	listOpts := client.MatchingFields{
		endpointSliceImportNameFieldKey: endpointSliceExport.Name,
	}
	if err := r.hubClient.List(ctx, endpointSliceImportList, listOpts); err != nil {
		klog.ErrorS(err, "Failed to list EndpointSliceImports by a specific name",
			"endpointSliceImportName", endpointSliceExport.Name,
			"endpointSliceExport", klog.KObj(endpointSliceExport))
		return err
	}

	// Withdraw EndpointSliceImports from member clusters.
	// TO-DO (chenyu1): Add retry with exponential backoff logic to guard against transitory errors
	// (e.g. API server throuttling).
	for idx := range endpointSliceImportList.Items {
		endpointSliceImport := endpointSliceImportList.Items[idx]
		if err := r.hubClient.Delete(ctx, &endpointSliceImport); err != nil && !errors.IsNotFound(err) {
			klog.ErrorS(err, "Failed to withdraw EndpointSliceImport",
				"endpointSliceImport", klog.KObj(&endpointSliceImport),
				"endpointSliceExport", klog.KObj(endpointSliceExport))
			return err
		}
	}
	return nil
}

// removeHubEndpointSliceCopyAndCleanupFinalizer removes the EndpointSlice copy kept in the hub cluster
// and the cleanup finalizer on the EndpointSliceExport.
func (r *Reconciler) removeHubEndpointSliceCopyAndCleanupFinalizer(ctx context.Context, endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport) error {
	endpointSlice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: r.fleetSystemNamespace,
			Name:      endpointSliceExport.Name,
		},
	}
	if err := r.hubClient.Delete(ctx, endpointSlice); err != nil && !errors.IsNotFound(err) {
		klog.ErrorS(err, "Failed to remove EndpointSlice copy in the hub cluster",
			"endpointSlice", klog.KObj(endpointSlice),
			"endpointSliceExport", klog.KObj(endpointSliceExport))
		return err
	}

	// Remove the EndpointSliceExport cleanup finalizer.
	if err := r.removeEndpointSliceExportCleanupFinalizer(ctx, endpointSliceExport); err != nil {
		klog.ErrorS(err, "Failed to remove EndpointSliceImport cleanup finalizer", "endpointSliceExport", klog.KObj(endpointSliceExport))
		return err
	}
	return nil
}

// removeEndpointSliceExportCleanupFinalizer removes the cleanup finalizer from an EndpointSliceExport.
func (r *Reconciler) removeEndpointSliceExportCleanupFinalizer(ctx context.Context, endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport) error {
	controllerutil.RemoveFinalizer(endpointSliceExport, endpointSliceExportCleanupFinalizer)
	return r.hubClient.Update(ctx, endpointSliceExport)
}

// addEndpointSliceExportCleanupFinalizer adds the cleanup finalizer to an EndpointSliceExport.
func (r *Reconciler) addEndpointSliceExportCleanupFinalizer(ctx context.Context, endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport) error {
	controllerutil.AddFinalizer(endpointSliceExport, endpointSliceExportCleanupFinalizer)
	return r.hubClient.Update(ctx, endpointSliceExport)
}

// scanForEndpointSliceImports lists all EndpointSliceImports across the fleet created from a specific
// EndpointSliceExport, and matches them with the set of member clusters that have requested the EndpointSlice;
// it returns
// * a list of EndpointSliceImports to withdraw (as their member clusters no longer need them); and
// * a list of EndpointSliceImports to create or update (as some member clusters have requested them).
//
// Note: At this moment, it is guaranteed that any Service can only be imported once across the fleet, consequently
// len(svcInUseBy.MemberClusters) should always be 1. However, this behavior is subject to change as fleet
// networking evolves, and for future compatibility reasons, the function assumes that a Service might have been
// imported to multiple clusters.
func (r *Reconciler) scanForEndpointSliceImports(
	ctx context.Context,
	endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport,
	svcInUseBy *fleetnetv1alpha1.ServiceInUseBy,
) (endpointSliceImportsToWithdraw, endpointSliceImportsToCreateOrUpdate []*fleetnetv1alpha1.EndpointSliceImport, err error) {
	// List all EndpointSlices distributed as EndpointSliceImports.
	endpointSliceImportList := &fleetnetv1alpha1.EndpointSliceImportList{}
	listOpts := client.MatchingFields{
		endpointSliceImportNameFieldKey: endpointSliceExport.Name,
	}
	if err := r.hubClient.List(ctx, endpointSliceImportList, listOpts); err != nil {
		klog.ErrorS(err, "Failed to list EndpointSliceImports by a specific name",
			"endpointSliceImportName", endpointSliceExport.Name,
			"endpointSliceExport", klog.KObj(endpointSliceExport))
		return endpointSliceImportsToWithdraw, endpointSliceImportsToCreateOrUpdate, err
	}

	// Match the EndpointSliceImports with the member clusters that have requested the EndpointSlice.
	for idx := range endpointSliceImportList.Items {
		endpointSliceImport := endpointSliceImportList.Items[idx]
		nsKey := fleetnetv1alpha1.ClusterNamespace(endpointSliceImport.Namespace)
		if _, ok := svcInUseBy.MemberClusters[nsKey]; ok {
			// A member cluster has requested the EndpointSlice and an EndpointSlice has been distributed to the
			// cluster; the EndpointSliceImport should be updated.
			endpointSliceImportsToCreateOrUpdate = append(endpointSliceImportsToCreateOrUpdate, &endpointSliceImport)
			delete(svcInUseBy.MemberClusters, nsKey)
		} else {
			// No member cluster has imported the EndpointSlice yet an EndpointSlice has been distributed to the cluster;
			// the EndpointSliceImport should be withdrawn.
			endpointSliceImportsToWithdraw = append(endpointSliceImportsToWithdraw, &endpointSliceImport)
		}
	}
	// A member cluster has requested the EndpointSlice but no EndpointSlice has been distributed to the cluster;
	// an EndpointSliceImport should be created.
	for ns := range svcInUseBy.MemberClusters {
		endpointSliceImport := &fleetnetv1alpha1.EndpointSliceImport{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: string(ns),
				Name:      endpointSliceExport.Name,
			},
		}
		endpointSliceImportsToCreateOrUpdate = append(endpointSliceImportsToCreateOrUpdate, endpointSliceImport)
	}
	return endpointSliceImportsToWithdraw, endpointSliceImportsToCreateOrUpdate, nil
}

// formatEndpointSliceFromExport formats an EndpointSlice using an EndpointSliceExport.
func formatEndpointSliceFromExport(endpointSlice *discoveryv1.EndpointSlice, endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport) {
	endpointSlice.AddressType = endpointSliceExport.Spec.AddressType
	endpointSlice.Ports = endpointSliceExport.Spec.Ports

	endpoints := make([]discoveryv1.Endpoint, 0, len(endpointSliceExport.Spec.Endpoints))
	for _, importedEndpoint := range endpointSliceExport.Spec.Endpoints {
		endpoints = append(endpoints, discoveryv1.Endpoint{
			Addresses: importedEndpoint.Addresses,
		})
	}
	endpointSlice.Endpoints = endpoints
}
