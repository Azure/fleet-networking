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

const exposedClusterNotFound = "exposedClusterNotFound"

// Reconciler reconciles a MultiClusterService object.
type Reconciler struct {
	hubClient client.Client
	Scheme    *runtime.Scheme
}

//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceimports,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=internalserviceimports,verbs=get;list;watch;create;update;patch;delete

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

	exposedCluster := utils.GetExposedClusterName(internalServiceImport.GetLabels())
	if len(exposedCluster) == 0 {
		// TODO(mainred): InternalServiceImport in current design is to indicate a cluster will be exposed to provision
		// a load balancer for N-S multi-networking traffic, and in case the indicator cannot be found in this
		// InternalServiceImport, we need a logic to make sure this resource to be re-created, or updated to carry the
		// exposed cluster info.
		err := fmt.Errorf(exposedClusterNotFound)
		klog.ErrorS(err, "Failed to get exposed cluster from internal serviceimport", "InternalServiceImport", klog.KObj(internalServiceImport))
		return ctrl.Result{}, err
	}

	// update service import in target namespace in hub cluster to carry the exposed cluster info.
	targetNamespace := utils.GetTargetNamespace(internalServiceImport.GetLabels())
	serviceImport := &fleetnetv1alpha1.ServiceImport{
		ObjectMeta: metav1.ObjectMeta{
			Name:      internalServiceImport.Name,
			Namespace: targetNamespace,
		},
	}
	serviceImport.SetLabels(map[string]string{consts.LabelExposedClusterName: exposedCluster})
	if _, err := controllerutil.CreateOrPatch(ctx, r.hubClient, serviceImport, func() error {
		return nil
	}); err != nil {
		klog.ErrorS(err, "Failed to create or update InternalServiceImport from ServiceImport", "InternalServiceImport", klog.KObj(internalServiceImport), "ServiceImport", klog.KObj(serviceImport))
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetnetv1alpha1.InternalServiceImport{}).
		Complete(r)
}
