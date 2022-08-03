/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package endpointslice features the EndpointSlice controller for exporting an EndpointSlice from a member cluster
// to its fleet.
package endpointslice

import (
	"context"
	"fmt"
	"time"

	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/uniquename"
)

const (
	endpointSliceUniqueNameAnnotation = "networking.fleet.azure.com/fleet-unique-name"
)

// skipOrUnexportEndpointSliceOp describes the op the controller should take on an EndpointSlice, specifically
// whether to skip reconciling an EndpointSlice, and whether to unexport an EndpointSlice.
type skipOrUnexportEndpointSliceOp int

const (
	// shouldSkipEndpointSliceOp notes that an EndpointSlice should be skipped for reconciliation.
	shouldSkipEndpointSliceOp skipOrUnexportEndpointSliceOp = 0
	// shouldUnexportEndpointSliceOp notes that an EndpointSlice should be unexported.
	shouldUnexportEndpointSliceOp skipOrUnexportEndpointSliceOp = 1
	// noSkipOrUnexportNeededOp notes that an EndpointSlice should not be skipped or unexported.
	continueReconcileOp skipOrUnexportEndpointSliceOp = 2
)

// Reconciler reconciles the export of an EndpointSlice.
type Reconciler struct {
	// The ID of the member cluster.
	MemberClusterID string
	MemberClient    client.Client
	HubClient       client.Client
	// The namespace reserved for the current member cluster in the hub cluster.
	HubNamespace string
}

//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=endpointsliceexports,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=discovery.k8s.io,resources=endpointslices,verbs=get;list;watch

// Reconcile exports an EndpointSlice.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	endpointSliceRef := klog.KRef(req.Namespace, req.Name)
	startTime := time.Now()
	klog.V(2).InfoS("Reconciliation starts", "endpointSlice", endpointSliceRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Reconciliation ends", "endpointSlice", endpointSliceRef, "latency", latency)
	}()

	// Retrieve the EndpointSlice object.
	var endpointSlice discoveryv1.EndpointSlice
	if err := r.MemberClient.Get(ctx, req.NamespacedName, &endpointSlice); err != nil {
		// Skip the reconciliation if the EndpointSlice does not exist; this should only happen when an EndpointSlice
		// is deleted right before the controller gets a chance to reconcile it. If the EndpointSlice has never
		// been exported to the fleet, no action is required on this controller's end; on the other hand, if the
		// EndpointSlice has been exported before, this may result in an EndpointSlice being left over on the
		// hub cluster, and it is up to another controller, EndpointSliceExport controller, to pick up the leftover
		// and clean it out.
		klog.ErrorS(err, "Failed to get endpoint slice", "endpointSlice", endpointSliceRef)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Check if the EndpointSlice should be skipped for reconciliation or unexported.
	skipOrUnexportOp, err := r.shouldSkipOrUnexportEndpointSlice(ctx, &endpointSlice)
	if err != nil {
		// An unexpected error occurs.
		klog.ErrorS(err,
			"Failed to determine whether an endpoint slice should be skipped for reconciliation or unexported",
			"endpointSlice", endpointSliceRef)
		return ctrl.Result{}, err
	}

	switch skipOrUnexportOp {
	case shouldSkipEndpointSliceOp:
		// Skip reconciling the EndpointSlice.
		klog.V(4).InfoS("Endpoint slice should be skipped for reconciliation", "endpointSlice", endpointSliceRef)
		return ctrl.Result{}, nil
	case shouldUnexportEndpointSliceOp:
		// Unexport the EndpointSlice.
		klog.V(4).InfoS("Endpoint slice should be unexported", "endpointSlice", endpointSliceRef)
		if err := r.unexportEndpointSlice(ctx, &endpointSlice); err != nil {
			klog.ErrorS(err, "Failed to unexport the endpoint slice", "endpointSlice", endpointSliceRef)
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Retrieve the unique name assigned; if none has been assigned, or the one assigned is not valid, possibly due
	// to user tampering with the annotation, assign a new unique name.
	fleetUniqueName, ok := endpointSlice.Annotations[endpointSliceUniqueNameAnnotation]
	if !ok || !isUniqueNameValid(fleetUniqueName) {
		klog.V(2).InfoS("The endpoint slice does not have a unique name assigned or the one assigned is not valid; a new one will be assigned",
			"endpointSlice", endpointSliceRef)
		var err error
		// Unique name annotation must be added before an EndpointSlice is exported.
		fleetUniqueName, err = r.assignUniqueNameAsAnnotation(ctx, &endpointSlice)
		if err != nil {
			klog.ErrorS(err, "Failed to assign unique name as an annotation", "endpointSlice", endpointSliceRef)
			return ctrl.Result{}, err
		}
	}

	// Create an EndpointSliceExport in the hub cluster if the EndpointSlice has never been exported; otherwise
	// update the corresponding EndpointSliceExport.
	extractedEndpoints := extractEndpointsFromEndpointSlice(&endpointSlice)
	endpointSliceExport := fleetnetv1alpha1.EndpointSliceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: r.HubNamespace,
			Name:      fleetUniqueName,
		},
	}
	klog.V(2).InfoS("Endpoint slice will be exported",
		"endpointSlice", endpointSliceRef,
		"endpointSliceExport", klog.KObj(&endpointSliceExport))
	createOrUpdateOp, err := controllerutil.CreateOrUpdate(ctx, r.HubClient, &endpointSliceExport, func() error {
		// Set up an EndpointSliceReference and only when an EndpointSliceExport is first created; this is because
		// most fields in EndpointSliceReference should be immutable after creation.
		if endpointSliceExport.CreationTimestamp.IsZero() {
			endpointSliceReference := fleetnetv1alpha1.FromMetaObjects(r.MemberClusterID, endpointSlice.TypeMeta, endpointSlice.ObjectMeta)
			endpointSliceExport.Spec.EndpointSliceReference = endpointSliceReference
		}

		// Return an error if an attempt is made to update an EndpointSliceExport that references a different
		// EndpointSlice from the one that is being reconciled. This usually happens when one unique name is assigned
		// to multiple EndpointSliceExports, either by chance or through direct manipulation.
		if !isEndpointSliceExportLinkedWithEndpointSlice(&endpointSliceExport, &endpointSlice) {
			return errors.NewAlreadyExists(
				schema.GroupResource{Group: fleetnetv1alpha1.GroupVersion.Group, Resource: "EndpointSliceExport"},
				fleetUniqueName,
			)
		}

		endpointSliceExport.Spec.AddressType = discoveryv1.AddressTypeIPv4
		endpointSliceExport.Spec.Endpoints = extractedEndpoints
		endpointSliceExport.Spec.Ports = endpointSlice.Ports
		endpointSliceExport.Spec.OwnerServiceReference = fleetnetv1alpha1.OwnerServiceReference{
			// The owner Service is guaranteed to reside in the same namespace as the EndpointSlice to export.
			Namespace:      endpointSlice.Namespace,
			Name:           endpointSlice.Labels[discoveryv1.LabelServiceName],
			NamespacedName: fmt.Sprintf("%s/%s", endpointSlice.Namespace, endpointSlice.Labels[discoveryv1.LabelServiceName]),
		}

		endpointSliceExport.Spec.EndpointSliceReference.UpdateFromMetaObject(endpointSlice.ObjectMeta)

		return nil
	})
	switch {
	case errors.IsAlreadyExists(err):
		// Remove the unique name annotation; a new one will be assigned in future reciliation attempts.
		klog.V(2).InfoS("The unique name assigned to the endpoint slice has been used; it will be removed", "endpointSlice", endpointSliceRef)
		delete(endpointSlice.Annotations, endpointSliceUniqueNameAnnotation)
		if err := r.MemberClient.Update(ctx, &endpointSlice); err != nil {
			klog.ErrorS(err, "Failed to remove endpointslice unique name annotation", "endpointSlice", endpointSliceRef)
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	case err != nil:
		klog.ErrorS(err,
			"Failed to create/update endpointslice export",
			"endpointSlice", endpointSliceRef,
			"endpointSliceExport", klog.KObj(&endpointSliceExport),
			"op", createOrUpdateOp)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the EndpointSlice controller with a controller manager.
func (r *Reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	// Enqueue EndpointSlices for processing when a ServiceExport changes.
	eventHandlers := handler.EnqueueRequestsFromMapFunc(func(o client.Object) []reconcile.Request {
		endpointSliceList := &discoveryv1.EndpointSliceList{}
		listOpts := client.ListOptions{
			LabelSelector: labels.SelectorFromSet(labels.Set{
				discoveryv1.LabelServiceName: o.GetName(),
			}),
			Namespace: o.GetNamespace(),
		}
		if err := r.MemberClient.List(ctx, endpointSliceList, &listOpts); err != nil {
			klog.ErrorS(err,
				"Failed to list endpoint slices in use by a service",
				"serviceExport", klog.KRef(o.GetNamespace(), o.GetName()),
			)
			return []reconcile.Request{}
		}
		reqs := []reconcile.Request{}
		for _, endpointSlice := range endpointSliceList.Items {
			reqs = append(reqs, reconcile.Request{
				NamespacedName: types.NamespacedName{Namespace: endpointSlice.Namespace, Name: endpointSlice.Name},
			})
		}
		return reqs
	})

	// EndpointSlice controller watches over EndpointSlice and ServiceExport objects.
	return ctrl.NewControllerManagedBy(mgr).
		For(&discoveryv1.EndpointSlice{}).
		Watches(&source.Kind{Type: &fleetnetv1alpha1.ServiceExport{}}, eventHandlers).
		Complete(r)
}

// shouldSkipOrUnexportEndpointSlice returns the op the controller should take on an EndpointSlice, specifically
// whether to skip reconciling an EndpointSlice, and whether to unexport an EndpointSlice.
//
// The controller can only export an EndpointSlice if
// * the EndpointSlice is in use by a Service that has been successfully exported (valid with no conflicts); and
// * the EndpointSlice has not been deleted.
//
// If an EndpointSlice has been exported before, but
// * its owner Service has not been, or is no longer, exported; or
// * the EndpointSlice itself has been deleted
// the EndpointSlice should be unexported.
//
// EndpointSlices that are
// * not exportable; or
// * not owned by a successfully exported Service
// should never be reconciled with this controller.
func (r *Reconciler) shouldSkipOrUnexportEndpointSlice(ctx context.Context,
	endpointSlice *discoveryv1.EndpointSlice) (skipOrUnexportEndpointSliceOp, error) {
	// Skip the reconciliation if the EndpointSlice is not permanently exportable.
	if isEndpointSlicePermanentlyUnexportable(endpointSlice) {
		return shouldSkipEndpointSliceOp, nil
	}

	// If the Service name label is absent, the EndpointSlice is not in use by a Service and thus cannot
	// be exported.
	svcName, hasSvcNameLabel := endpointSlice.Labels[discoveryv1.LabelServiceName]
	// It is guaranteed that if there is no unique name assigned to an EndpointSlice as an annotation, no attempt has
	// been made to export an EndpointSlice.
	_, hasUniqueNameAnnotation := endpointSlice.Annotations[endpointSliceUniqueNameAnnotation]

	if !hasSvcNameLabel {
		if !hasUniqueNameAnnotation {
			// The Service is not in use by a Service and does not have a unique name annotation (i.e. it has not been
			// exported before); it should be skipped for further processing.
			return shouldSkipEndpointSliceOp, nil
		}
		// The Service is not in use by a Service but has a unique name annotation (i.e. it might have been exported);
		// this could happen on an orphaned exported EndpointSlice, which should be unexported.
		return shouldUnexportEndpointSliceOp, nil
	}

	// Retrieve the Service Export.
	svcExport := &fleetnetv1alpha1.ServiceExport{}
	err := r.MemberClient.Get(ctx, types.NamespacedName{Namespace: endpointSlice.Namespace, Name: svcName}, svcExport)
	switch {
	case errors.IsNotFound(err) && hasUniqueNameAnnotation:
		// The Service using the EndpointSlice is not exported but the EndpointSlice has a unique name annotation
		// present (i.e. it might have been exported); the EndpointSlice should be unexported.
		return shouldUnexportEndpointSliceOp, nil
	case errors.IsNotFound(err) && !hasUniqueNameAnnotation:
		// The Service using the EndpointSlice is not exported and the EndpointSlice has no unique name annotation
		// present (i.e. it has not been exported before); the EndpointSlice should be skipped for further processing.
		return shouldSkipEndpointSliceOp, nil
	case err != nil:
		// An unexpected error has occurred.
		return continueReconcileOp, err
	}

	// Check if the ServiceExport is valid with no conflicts.
	if !isServiceExportValidWithNoConflict(svcExport) {
		if hasUniqueNameAnnotation {
			// The Service using the EndpointSlice is not valid for export or has conflicts with other exported
			// Services, but the EndpointSlice has a unique name annotation present (i.e. it might have been
			// exported before); the EndpointSlice should be unexported.
			return shouldUnexportEndpointSliceOp, nil
		}
		// The Service using the EndpointSlice is not valid for export or has conflicts with other exported
		// Services, and the EndpointSlice has no unique name annoation present (i.e. it has not been
		// exported before); the EndpointSlice should be skipped for further processing.
		return shouldSkipEndpointSliceOp, nil
	}

	if endpointSlice.DeletionTimestamp != nil {
		if hasUniqueNameAnnotation {
			// The Service using the EndpointSlice is exported with no conflicts, and the EndpointSlice has a unique
			// name annotation (i.e. it might have been exported), but it has been deleted; as a result,
			// the EndpointSlice should be unexported.
			return shouldUnexportEndpointSliceOp, nil
		}
		// The Service using the EndpointSlice is exported with no conflicts, but the EndpointSlice does not have a
		// unique name annotation (i.e. it has not been exported), and it has been deleted; as a result,
		// the EndpointSlice should be skipped.
		return shouldSkipEndpointSliceOp, nil
	}

	// The Service using the EndpointSlice is exported with no conflicts, and the EndpointSlice is not marked
	// for deletion; the EndpointSlice should be further processed.
	return continueReconcileOp, nil
}

// unexportEndpointSlice unexports an EndpointSlice by deleting its corresponding EndpointSliceExport.
func (r *Reconciler) unexportEndpointSlice(ctx context.Context, endpointSlice *discoveryv1.EndpointSlice) error {
	// Remove the EndpointSliceExport.
	if err := r.deleteEndpointSliceExportIfLinked(ctx, endpointSlice); err != nil {
		return err
	}

	// Remove the unique name annotation; this must happen after the EndpointSliceExport has been deleted.
	delete(endpointSlice.Annotations, endpointSliceUniqueNameAnnotation)
	return r.MemberClient.Update(ctx, endpointSlice)
}

// deleteEndpointSliceExportIfLinked deletes an exported EndpointSlice.
func (r *Reconciler) deleteEndpointSliceExportIfLinked(ctx context.Context, endpointSlice *discoveryv1.EndpointSlice) error {
	fleetUniqueName := endpointSlice.Annotations[endpointSliceUniqueNameAnnotation]

	// Skip the deletion if the unique name assigned as an annotation is not a valid DNS subdomain name; this
	// helps guard against user tampering with the annotation.
	if !isUniqueNameValid(fleetUniqueName) {
		klog.V(2).InfoS("The unique name annotation for exporting the EndpointSlice is not valid; unexport is skipped",
			"endpointSlice", klog.KObj(endpointSlice),
			"uniqueName", fleetUniqueName)
		return nil
	}

	endpointSliceExport := fleetnetv1alpha1.EndpointSliceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: r.HubNamespace,
			Name:      fleetUniqueName,
		},
	}
	endpointSliceExportKey := types.NamespacedName{Namespace: r.HubNamespace, Name: fleetUniqueName}
	err := r.HubClient.Get(ctx, endpointSliceExportKey, &endpointSliceExport)
	switch {
	case errors.IsNotFound(err):
		// It is guaranteed that a unique name annotation is always added before an EndpointSlice is exported; and
		// in some rare occasions it could happen that an EndpointSlice has a unique name annotation present yet has
		// not been exported to the hub cluster. It is an expected behavior and no action is needed on this controller's
		// end.
		return nil
	case err != nil:
		// An unexpected error has occurred.
		return err
	}

	if !isEndpointSliceExportLinkedWithEndpointSlice(&endpointSliceExport, endpointSlice) {
		// The EndpointSliceExport to which the unique name annotation on the EndpointSlice refers is not actually
		// linked with the EndpointSlice. This could happen if direct manipulation forces unique name annotations
		// on two different EndpointSlices to point to the same EndpointSliceExport. In this case the
		// EndpointSliceExport will not be deleted.
		return nil
	}

	if err := r.HubClient.Delete(ctx, &endpointSliceExport); err != nil && !errors.IsNotFound(err) {
		// An unexpected error has occurred.
		return err
	}
	return nil
}

// assignUniqueNameAsAnnotation assigns a new unique name as an annotation.
func (r *Reconciler) assignUniqueNameAsAnnotation(ctx context.Context, endpointSlice *discoveryv1.EndpointSlice) (string, error) {
	fleetUniqueName, err := uniquename.FleetScopedUniqueName(uniquename.DNS1123Subdomain,
		r.MemberClusterID,
		endpointSlice.Namespace,
		endpointSlice.Name)
	if err != nil {
		// Fall back to use a random lower case alphabetic string as the unique name. Normally this branch should
		// never run.
		klog.ErrorS(err, "Failed to generate a unique name; fall back to random lower case alphabetic strings",
			"endpointSlice", klog.KObj(endpointSlice))
		fleetUniqueName = uniquename.RandomLowerCaseAlphabeticString(25)
	}
	updatedEndpointSlice := endpointSlice.DeepCopy()
	// Initialize the annotations field if no annotations are present.
	if updatedEndpointSlice.Annotations == nil {
		updatedEndpointSlice.Annotations = map[string]string{}
	}
	updatedEndpointSlice.Annotations[endpointSliceUniqueNameAnnotation] = fleetUniqueName
	return fleetUniqueName, r.MemberClient.Update(ctx, updatedEndpointSlice)
}
