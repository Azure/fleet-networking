/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package endpointsliceexport features the EndpointSliceExport controller for cleaning up left over
// EndpointSlices on the hub cluster.
package endpointsliceexport

import (
	"context"
	"time"

	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

const (
	endpointSliceUniqueNameLabel = "networking.fleet.azure.com/fleet-unique-name"
)

type Reconciler struct {
	memberClient client.Client
	hubClient    client.Client
}

//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=endpointsliceexports,verbs=get;list;watch;delete
//+kubebuilder:rbac:groups=core,resources=endpointslices,verbs=get;list;watch

// Reconcile verifies if an EndpointSliceExport in the hub cluster matches with a exported EndpointSlice from
// the current member cluster, and will clean up EndpointSliceExports that fail to match.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	endpointSliceExportRef := klog.KRef(req.Namespace, req.Name)
	startTime := time.Now()
	klog.V(2).InfoS("Reconciliation starts", "endpointSliceExport", endpointSliceExportRef)
	defer func() {
		latency := time.Since(startTime).Seconds()
		klog.V(2).InfoS("Reconciliation ends", "endpointSliceExport", endpointSliceExportRef, "latency", latency)
	}()

	// Retrieve the EndpointSliceExport object.
	endpointSliceExport := &fleetnetv1alpha1.EndpointSliceExport{}
	if err := r.hubClient.Get(ctx, req.NamespacedName, endpointSliceExport); err != nil {
		klog.ErrorS(err, "Failed to get endpoint slice export", "endpointSliceExport", endpointSliceExportRef)
		// Skip the reconciliation if the EndpointSliceExport does not exist; this should only happen when an
		// EndpointSliceExport is deleted before the controller gets a chance to reconcile it;
		// this requires no action to take on this controller's end.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Check if the EndpointSliceExport refers to an existing EndpointSlice.
	endpointSlice := &discoveryv1.EndpointSlice{}
	endpointSliceKey := types.NamespacedName{
		Namespace: endpointSliceExport.Spec.EndpointSliceReference.Namespace,
		Name:      endpointSliceExport.Spec.EndpointSliceReference.Name,
	}
	endpointSliceRef := klog.KRef(endpointSliceKey.Namespace, endpointSliceKey.Name)
	err := r.memberClient.Get(ctx, endpointSliceKey, endpointSlice)
	switch {
	case errors.IsNotFound(err):
		// The matching EndpointSlice is not found; the EndpointSliceExport should be deleted.
		klog.V(2).InfoS("Referred endpoint slice is not found; delete the endpoint slice export",
			"endpointSliceExport", endpointSliceExportRef,
			"endpointSlice", endpointSliceRef,
		)
		return r.deleteEndpointSliceExport(ctx, endpointSliceExport)
	case err != nil:
		// An unexpected error has occurred.
		return ctrl.Result{}, err
	}

	// Check if the EndpointSliceExport is linked the referred EndpointSlice by the assigned unique name for export.
	// This helps guard against some corner cases, e.g.
	// * A user tampers with the unique name for export assigned to an EndpointSlice,
	//   which leads to the same EndpointSlice being exported for multiple times with different names.
	// * An EndpointSlice is deleted and immediately re-created with the same name, and the EndpointSlice
	//   controller fails to unexport the EndpointSlice when it is deleted in time
	if !IsEndpointSliceExportLinkedWithEndpointSlice(endpointSliceExport, endpointSlice) {
		return r.deleteEndpointSliceExport(ctx, endpointSliceExport)
	}
	return ctrl.Result{}, nil
}

func (r *Reconciler) deleteEndpointSliceExport(ctx context.Context,
	endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport) (ctrl.Result, error) {
	if err := r.hubClient.Delete(ctx, endpointSliceExport); err != nil && !errors.IsNotFound(err) {
		klog.ErrorS(err, "Failed to delete endpoint slice export", "endpointSliceExport", klog.KObj(endpointSliceExport))
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}
