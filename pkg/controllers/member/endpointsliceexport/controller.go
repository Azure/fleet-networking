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
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
)

const (
	endpointSliceExportRetryInterval = time.Minute * 5
)

type Reconciler struct {
	MemberClient client.Client
	HubClient    client.Client
}

//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=endpointsliceexports,verbs=get;list;watch;delete
//+kubebuilder:rbac:groups=discovery.k8s.io,resources=endpointslices,verbs=get;list;watch

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
	if err := r.HubClient.Get(ctx, req.NamespacedName, endpointSliceExport); err != nil {
		// Skip the reconciliation if the EndpointSliceExport does not exist; this should only happen when an
		// EndpointSliceExport is deleted before the controller gets a chance to reconcile it;
		// this requires no action to take on this controller's end.
		if errors.IsNotFound(err) {
			klog.V(4).InfoS("Ignoring NotFound endpointSliceExport", "endpointSliceExport", endpointSliceExportRef)
			return ctrl.Result{}, nil
		}
		klog.ErrorS(err, "Failed to get endpointSliceExport", "endpointSliceExport", endpointSliceExportRef)
		return ctrl.Result{}, err
	}

	// Check if the EndpointSliceExport refers to an existing EndpointSlice.
	endpointSlice := &discoveryv1.EndpointSlice{}
	endpointSliceKey := types.NamespacedName{
		Namespace: endpointSliceExport.Spec.EndpointSliceReference.Namespace,
		Name:      endpointSliceExport.Spec.EndpointSliceReference.Name,
	}
	endpointSliceRef := klog.KRef(endpointSliceKey.Namespace, endpointSliceKey.Name)
	err := r.MemberClient.Get(ctx, endpointSliceKey, endpointSlice)
	switch {
	case errors.IsNotFound(err):
		// The matching EndpointSlice is not found; the EndpointSliceExport should be deleted.
		klog.V(2).InfoS("Referred endpointSlice is not found; delete the endpointSliceExport",
			"endpointSliceExport", endpointSliceExportRef,
			"endpointSlice", endpointSliceRef,
		)
		return r.deleteEndpointSliceExport(ctx, endpointSliceExport)
	case err != nil:
		// An unexpected error has occurred.
		klog.ErrorS(err, "Failed to get endpointSlice",
			"endpointSliceExport", endpointSliceExportRef,
			"endpointSlice", endpointSliceRef)
		return ctrl.Result{}, err
	}

	// Check if the EndpointSliceExport is linked the referred EndpointSlice by the assigned unique name for export.
	// This helps guard against some corner cases, e.g.
	// * A user tampers with the unique name for export assigned to an EndpointSlice,
	//   which leads to the same EndpointSlice being exported for multiple times with different names.
	// * An EndpointSlice is deleted and immediately re-created with the same name, and the EndpointSlice
	//   controller fails to unexport the EndpointSlice in time when it is deleted.
	if !isEndpointSliceExportLinkedWithEndpointSlice(endpointSliceExport, endpointSlice) {
		return r.deleteEndpointSliceExport(ctx, endpointSliceExport)
	}

	// Periodically re-scan EndpointSliceExports; this help addresses corner cases where an EndpointSlice
	// is deleted without the EndpointSlice controller getting a chance to withdraw it from the hub cluster.
	return ctrl.Result{RequeueAfter: endpointSliceExportRetryInterval}, nil
}

// SetupWithManager builds a controller with Reconciler and sets it up with a controller manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// The EndpointSliceExport controller watches over EndpointSliceExport objects.
		// TO-DO (chenyu1): use predicates to filter out some events.
		For(&fleetnetv1alpha1.EndpointSliceExport{}).
		Complete(r)
}

// deleteEndpointSliceExport deletes an EndpointSliceExport from the hub cluster.
func (r *Reconciler) deleteEndpointSliceExport(ctx context.Context, endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport) (ctrl.Result, error) {
	if err := r.HubClient.Delete(ctx, endpointSliceExport); err != nil && !errors.IsNotFound(err) {
		klog.ErrorS(err, "Failed to delete endpoint slice export", "endpointSliceExport", klog.KObj(endpointSliceExport))
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// isEndpointSliceExportLinkedWithEndpointSlice returns if an EndpointSliceExport's name matches with the
// unique name for export assigned to an exported EndpointSlice.
func isEndpointSliceExportLinkedWithEndpointSlice(endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport, endpointSlice *discoveryv1.EndpointSlice) bool {
	uniqueName, ok := endpointSlice.Annotations[objectmeta.ExportedObjectAnnotationUniqueName]
	if !ok || uniqueName != endpointSliceExport.Name {
		return false
	}
	return true
}
