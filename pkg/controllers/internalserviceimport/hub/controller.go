/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package hub features the internalserviceimport controller deployed in hub cluster to keep serviceimport
// synced with internalserviceimport.
package hub

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/controllers/internalserviceimport/consts"
	"go.goms.io/fleet-networking/pkg/controllers/internalserviceimport/utils"
)

const (
	InternalServiceImportFinalizer = "networking.fleet.azure.com/internalserviceimport-cleanup"
	exposedClusterNotFound         = "exposedClusterNotFound"
)

// Reconciler reconciles a MultiClusterService object.
type Reconciler struct {
	hubClient client.Client
	Scheme    *runtime.Scheme
}

//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceimports,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=internalserviceimports,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=internalserviceimports/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=internalserviceimports/finalizers,verbs=get;update

// Reconcile in hub cluster updates service import with exposed cluster according the corresponding internal service import.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	internalServiceImport := &fleetnetv1alpha1.InternalServiceImport{}
	internalServiceImportRef := klog.KRef(req.Namespace, req.Name)
	err := r.hubClient.Get(ctx, req.NamespacedName, internalServiceImport)
	if errors.IsNotFound(err) {
		klog.ErrorS(err, "Could not find InternalServiceImport in member cluser", "InternalServiceImport", internalServiceImportRef)
		return reconcile.Result{}, nil
	}
	if err != nil {
		klog.ErrorS(err, "Failed to get InternalServiceImport", "InternalServiceImport", internalServiceImportRef)
		return reconcile.Result{}, err
	}

	// Update service import in target namespace in hub cluster to carry the exposed cluster info.
	targetNamespace := utils.GetTargetNamespace(internalServiceImport)
	serviceImport := &fleetnetv1alpha1.ServiceImport{
		ObjectMeta: metav1.ObjectMeta{
			Name:      internalServiceImport.Name,
			Namespace: targetNamespace,
		},
	}

	// Examine DeletionTimestamp to determine if service import is under deletion.
	if internalServiceImport.ObjectMeta.DeletionTimestamp.IsZero() {
		// Add finalizer when it's in internal service import when not being deleted
		if !controllerutil.ContainsFinalizer(internalServiceImport, InternalServiceImportFinalizer) {
			controllerutil.AddFinalizer(internalServiceImport, InternalServiceImportFinalizer)
			if err := r.hubClient.Update(ctx, internalServiceImport); err != nil {
				klog.ErrorS(err, "Failed to add serviceimport finalizer", "InternalServiceImport", internalServiceImportRef, "finalizer", InternalServiceImportFinalizer)
				return ctrl.Result{}, err
			}
		}
	} else {
		// Delete internal service import dependency when the finalizer is expected then remove the finalizer from internal service import.
		if controllerutil.ContainsFinalizer(internalServiceImport, InternalServiceImportFinalizer) {

			// Remove exposed cluster name label from service import
			// TODO(mainred): not idea how to delete a lable from a k8s resource, through API, we achieved this by setting
			// the label value to null.
			serviceImport.SetLabels(map[string]string{consts.LabelExposedClusterName: ""})
			objPatch := client.MergeFrom(serviceImport.DeepCopyObject().(client.Object))
			if err := r.hubClient.Patch(ctx, serviceImport, objPatch); err != nil {
				klog.ErrorS(err, "Failed to delete internalserviceimport as requried by serviceimport finalizer", "InternalServiceImport", klog.KObj(internalServiceImport), "ServiceImport", internalServiceImport, "finalizer", InternalServiceImportFinalizer)
				return ctrl.Result{}, err
			}

			controllerutil.RemoveFinalizer(internalServiceImport, InternalServiceImportFinalizer)
			if err := r.hubClient.Update(ctx, internalServiceImport); err != nil {
				klog.ErrorS(err, "Failed to remove serviceimport finalizer", "InternalServiceImport", internalServiceImportRef, "finalizer", InternalServiceImportFinalizer)
				return ctrl.Result{}, err
			}

			// Stop reconciliation as the item is being deleted.
			return ctrl.Result{}, nil
		}
	}

	exposedCluster := utils.GetExposedClusterName(internalServiceImport)
	if len(exposedCluster) == 0 {
		// TODO(mainred): InternalServiceImport in current design is to indicate a cluster will be exposed to provision
		// a load balancer for N-S multi-networking traffic, and in case the indicator cannot be found in this
		// InternalServiceImport, we need a logic to make sure this resource to be re-created, or updated to carry the
		// exposed cluster info.
		err := fmt.Errorf(exposedClusterNotFound)
		klog.ErrorS(err, "Failed to get exposed cluster from internal serviceimport", "InternalServiceImport", klog.KObj(internalServiceImport))
		return ctrl.Result{}, err
	}

	serviceImport.SetLabels(map[string]string{consts.LabelExposedClusterName: exposedCluster})
	if _, err := controllerutil.CreateOrPatch(ctx, r.hubClient, serviceImport, func() error {
		return nil
	}); err != nil {
		klog.ErrorS(err, "Failed to create or update ServiceImport from InternalServiceImport", "ServiceImport", klog.KObj(serviceImport), "InternalServiceImport", klog.KObj(internalServiceImport))
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetnetv1alpha1.InternalServiceImport{}).
		Complete(r)
}
