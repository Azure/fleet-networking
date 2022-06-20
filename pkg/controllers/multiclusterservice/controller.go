// Package multiclusterservice features the mcs controller to multiclusterservice CRD.
// The controller could be installed in either hub cluster or member clusters.
package multiclusterservice

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

const (
	multiClusterServiceFinalizer    = "networking.fleet.azure.com/multi-cluster-service-finalizer"
	multiClusterServiceLabelService = "service"
)

// MultiClusterServiceReconciler reconciles a MultiClusterService object.
type MultiClusterServiceReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	SystemNamespace string
}

//+kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=multiclusterservices,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=multiclusterservices/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=multiclusterservices/finalizers,verbs=get;update
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceimport,verbs=get;list;watch;create;update;patch;delete

// Reconcile triggers a single reconcile round.
func (r *MultiClusterServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	name := req.NamespacedName
	mcs := fleetnetv1alpha1.MultiClusterService{}
	if err := r.Client.Get(ctx, name, &mcs); err != nil {
		klog.ErrorS(err, "failed to get mcs", "multiClusterService", name)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if mcs.ObjectMeta.DeletionTimestamp.IsZero() {
		// register finalizer
		if !controllerutil.ContainsFinalizer(&mcs, multiClusterServiceFinalizer) {
			controllerutil.AddFinalizer(&mcs, multiClusterServiceFinalizer)
			if err := r.Update(ctx, &mcs); err != nil {
				klog.ErrorS(err, "failed to add mcs finalizer", "multiClusterService", name)
				return ctrl.Result{}, err
			}
		}
	} else {
		return r.handleDelete(ctx, &mcs)
	}

	// handle update
	return ctrl.Result{}, nil
}

func (r *MultiClusterServiceReconciler) handleDelete(ctx context.Context, mcs *fleetnetv1alpha1.MultiClusterService) (ctrl.Result, error) {
	// The mcs is being deleted
	if controllerutil.ContainsFinalizer(mcs, multiClusterServiceFinalizer) {
		klog.InfoS("removing mcs", "namespace", mcs.Namespace, "name", mcs.Name)

		// delete derived service in the fleet-system namesapce
		if err := r.deleteDerivedService(ctx, mcs); err != nil && !errors.IsNotFound(err) {
			klog.ErrorS(err, "failed to remove derived service of mcs", "namespace", mcs.Namespace, "name", mcs.Name)
			return ctrl.Result{}, err
		}
		// delete service import in the same namesapce as the multi-cluster service
		if err := r.deleteServiceImport(ctx, mcs); err != nil && !errors.IsNotFound(err) {
			klog.ErrorS(err, "failed to remove service import of mcs", "namespace", mcs.Namespace, "name", mcs.Name)
			return ctrl.Result{}, err
		}

		controllerutil.RemoveFinalizer(mcs, multiClusterServiceFinalizer)
		if err := r.Client.Update(ctx, mcs); err != nil {
			klog.ErrorS(err, "failed to remove mcs finalizer", "namespace", mcs.Namespace, "name", mcs.Name)
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *MultiClusterServiceReconciler) deleteDerivedService(ctx context.Context, mcs *fleetnetv1alpha1.MultiClusterService) error {
	serviceName := r.derivedServiceFromLabel(mcs)
	if serviceName == nil {
		klog.Warningf("cannot find the derived service label %q from namespace %q mcs %q", multiClusterServiceLabelService, mcs.Namespace, mcs.Name)
		return nil
	}
	service := corev1.Service{}
	if err := r.Client.Get(ctx, *serviceName, &service); err != nil {
		return err
	}
	return r.Client.Delete(ctx, &service)
}

func (r *MultiClusterServiceReconciler) deleteServiceImport(ctx context.Context, mcs *fleetnetv1alpha1.MultiClusterService) error {
	serviceImportName := types.NamespacedName{Namespace: mcs.Namespace, Name: mcs.Spec.ServiceImport.Name}
	serviceImport := fleetnetv1alpha1.ServiceImport{}
	if err := r.Client.Get(ctx, serviceImportName, &serviceImport); err != nil {
		return err
	}
	return r.Client.Delete(ctx, &serviceImport)
}

// mcs-controller will record derived service name as the label to make sure the derived name is unique.
// Key is `service`.
func (r *MultiClusterServiceReconciler) derivedServiceFromLabel(mcs *fleetnetv1alpha1.MultiClusterService) *types.NamespacedName {
	if val, ok := mcs.GetLabels()[multiClusterServiceLabelService]; ok {
		return &types.NamespacedName{Namespace: r.SystemNamespace, Name: val}
	}
	return nil
}
