/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// package endpointslice features the EndpointSlice controller for exporting an EndpointSlice from a member cluster
// to its fleet.
package endpointslice

import (
	"context"
	"time"

	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetnetworkingapi "go.goms.io/fleet-networking/api/v1alpha1"
)

const (
	endpointSliceUniqueNameLabel = "networking.fleet.azure.com/fleet-unique-name"
)

// Reconciler reconciles the export of an EndpointSlice.
type Reconciler struct {
	memberClusterID string
	memberClient    client.Client
	hubClient       client.Client
	// The namespace reserved for the current member cluster in the hub cluster.
	hubNamespace string
}

//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=endpointsliceexports,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=endpointslices,verbs=get;list;watch

// Reconcile exports an EndpointSlice.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	endpointSliceRef := klog.KRef(req.Namespace, req.Name)
	startTime := time.Now()
	klog.V(2).InfoS("Reconciliation starts", "endpointSlice", endpointSliceRef)
	defer func() {
		latency := time.Since(startTime).Seconds()
		klog.V(2).InfoS("Reconciliation ends", "endpointSlice", endpointSliceRef, "latency", latency)
	}()

	// Retrieve the EndpointSlice object.
	var endpointSlice discoveryv1.EndpointSlice
	endpointSliceKey := types.NamespacedName{Namespace: req.Namespace, Name: req.Name}
	if err := r.memberClient.Get(ctx, endpointSliceKey, &endpointSlice); err != nil {
		// Skip the reconciliation if the EndpointSlice does not exist; this should only happen when an EndpointSlice
		// is deleted right before the controller gets a chance to reconcile it. If the EndpointSlice has never
		// been exported to the fleet, no action is required on this controller's end; on the other hand, if the
		// EndpointSlice has been exported before, this may result in an EndpointSlice being left over on the
		// hub cluster, and it is up to another controller, EndpointSliceExport controller, to pick up the leftover
		// and clean it out.
		klog.ErrorS(err, "Failed to get endpoint slice", "endpointSlice", endpointSliceRef)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Skip the reconciliation if the EndpointSlice is not exportable.
	if !isEndpointSliceExportable(&endpointSlice) {
		return ctrl.Result{}, nil
	}

	// Check if the EndpointSlice has bee deleted and needs cleanup (unexporting EndpointSlice).
	if isEndpointSliceCleanupNeeded(&endpointSlice) {
		klog.V(2).InfoS("Endpoint slice is deleted; unexport the endpoint slice", "endpointSlice", endpointSliceRef)
		if err := r.unexportEndpointSlice(ctx, &endpointSlice); err != nil {
			klog.ErrorS(err, "Failed to unexport endpoint slice", "endpointSlice", endpointSliceRef)
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Retrieve the unique name assigned (if any), or format a new one and assign it..
	fleetUniqueName, ok := endpointSlice.Labels[endpointSliceUniqueNameLabel]
	if !ok {
		var err error
		fleetUniqueName, err = r.assignUniqueNameAsLabel(ctx, &endpointSlice)
		if err != nil {
			klog.ErrorS(err, "Failed to assign unique name as a label", "endpointSlice", endpointSliceRef)
			return ctrl.Result{}, err
		}
	}

	// Create an EndpointSliceExport in the hub cluster if the EndpointSlice has never been exported; otherwise
	// update the corresponding EndpointSliceExport.
	extractedEndpoints := extractEndpointsFromEndpointSlice(&endpointSlice)
	endpointSliceExport := fleetnetworkingapi.EndpointSliceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: r.hubNamespace,
			Name:      fleetUniqueName,
		},
	}
	op, err := controllerutil.CreateOrUpdate(ctx, r.hubClient, &endpointSliceExport, func() error {
		endpointSliceExport.Spec = fleetnetworkingapi.EndpointSliceExportSpec{
			AddressType: discoveryv1.AddressTypeIPv4,
			Endpoints:   extractedEndpoints,
			Ports:       endpointSlice.Ports,
			EndpointSliceReference: fleetnetworkingapi.ExportedObjectReference{
				ClusterID:       r.memberClusterID,
				APIVersion:      endpointSlice.APIVersion,
				Kind:            endpointSlice.Kind,
				Namespace:       endpointSlice.Namespace,
				Name:            endpointSlice.Name,
				ResourceVersion: endpointSlice.ResourceVersion,
				Generation:      endpointSlice.Generation,
				UID:             endpointSlice.UID,
			},
		}

		return nil
	})
	if err != nil {
		klog.ErrorS(err,
			"Failed to create/update endpointslice export",
			"endpointSlice", endpointSliceRef,
			"endpointSliceExport", klog.KRef(r.hubNamespace, fleetUniqueName),
			"op", op)
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// unexportEndpointSlice unexports an EndpointSlice by deleting its corresponding EndpointSliceExport.
func (r *Reconciler) unexportEndpointSlice(ctx context.Context, endpointSlice *discoveryv1.EndpointSlice) error {
	fleetUniqueName := endpointSlice.Labels[endpointSliceUniqueNameLabel]
	endpointSliceExport := fleetnetworkingapi.EndpointSliceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: r.hubNamespace,
			Name:      fleetUniqueName,
		},
	}
	err := r.hubClient.Delete(ctx, &endpointSliceExport)
	// It is guaranteed that a unique name label is always added before an EndpointSlice is exported; and
	// in some rare occasions it could happen that an EndpointSlice has a unique name label present yet has
	// not been exported to the hub cluster. It is an expected behavior and no action is needed on this controller's
	// end.
	return client.IgnoreNotFound(err)
}

// assignUniqueNameAsLabel assigns a new unique name as a label.
func (r *Reconciler) assignUniqueNameAsLabel(ctx context.Context, endpointSlice *discoveryv1.EndpointSlice) (string, error) {
	fleetUniqueName := formatFleetUniqueName(r.memberClusterID, endpointSlice)
	updatedEndpointSlice := endpointSlice.DeepCopy()
	// Initialize the labels field if no labels are present.
	if updatedEndpointSlice.Labels == nil {
		updatedEndpointSlice.Labels = map[string]string{}
	}
	updatedEndpointSlice.Labels[endpointSliceUniqueNameLabel] = fleetUniqueName
	err := r.memberClient.Update(ctx, updatedEndpointSlice)
	return fleetUniqueName, err
}
