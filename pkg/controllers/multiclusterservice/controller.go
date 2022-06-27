// Package multiclusterservice features the mcs controller to multiclusterservice CRD.
// The controller could be installed in either hub cluster or member clusters.
package multiclusterservice

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

const (
	multiClusterServiceFinalizer          = "networking.fleet.azure.com/service-resources-cleanup"
	multiClusterServiceLabelService       = "networking.fleet.azure.com/derived-service"
	multiClusterServiceLabelServiceImport = "networking.fleet.azure.com/service-import"
)

// Reconciler reconciles a MultiClusterService object.
type Reconciler struct {
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
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	name := req.NamespacedName
	mcs := fleetnetv1alpha1.MultiClusterService{}
	if err := r.Client.Get(ctx, name, &mcs); err != nil {
		klog.ErrorS(err, "Failed to get mcs", "multiClusterService", klog.KRef(name.Namespace, name.Name))
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if mcs.ObjectMeta.DeletionTimestamp != nil {
		// register finalizer
		if !controllerutil.ContainsFinalizer(&mcs, multiClusterServiceFinalizer) {
			controllerutil.AddFinalizer(&mcs, multiClusterServiceFinalizer)
			if err := r.Update(ctx, &mcs); err != nil {
				klog.ErrorS(err, "Failed to add mcs finalizer", "multiClusterService", klog.KRef(name.Namespace, name.Name))
				return ctrl.Result{}, err
			}
		}
	} else {
		return r.handleDelete(ctx, &mcs)
	}

	// handle update
	return r.handleUpdate(ctx, &mcs)
}

func (r *Reconciler) handleDelete(ctx context.Context, mcs *fleetnetv1alpha1.MultiClusterService) (ctrl.Result, error) {
	// The mcs is being deleted
	if !controllerutil.ContainsFinalizer(mcs, multiClusterServiceFinalizer) {
		return ctrl.Result{}, nil
	}

	klog.V(1).InfoS("Removing mcs", "multiClusterService", klog.KObj(mcs))

	// delete derived service in the fleet-system namesapce
	serviceName := r.derivedServiceFromLabel(mcs)
	if err := r.deleteDerivedService(ctx, serviceName); err != nil {
		klog.ErrorS(err, "Failed to remove derived service of mcs", "multiClusterService", klog.KObj(mcs))
		if !errors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}
	// delete service import in the same namesapce as the multi-cluster service
	serviceImportName := r.serviceImportFromLabel(mcs)
	if err := r.deleteServiceImport(ctx, serviceImportName); err != nil {
		klog.ErrorS(err, "Failed to remove service import of mcs", "multiClusterService", klog.KObj(mcs))
		if !errors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}

	controllerutil.RemoveFinalizer(mcs, multiClusterServiceFinalizer)
	if err := r.Client.Update(ctx, mcs); err != nil {
		klog.ErrorS(err, "Failed to remove mcs finalizer", "multiClusterService", klog.KObj(mcs))
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *Reconciler) deleteDerivedService(ctx context.Context, serviceName *types.NamespacedName) error {
	if serviceName == nil {
		return nil
	}
	service := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: serviceName.Namespace,
			Name:      serviceName.Name,
		},
	}
	return r.Client.Delete(ctx, &service)
}

func (r *Reconciler) deleteServiceImport(ctx context.Context, serviceImportName *types.NamespacedName) error {
	if serviceImportName == nil {
		return nil
	}
	serviceImport := fleetnetv1alpha1.ServiceImport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: serviceImportName.Namespace,
			Name:      serviceImportName.Name,
		},
	}
	return r.Client.Delete(ctx, &serviceImport)
}

// mcs-controller will record derived service name as the label to make sure the derived name is unique.
func (r *Reconciler) derivedServiceFromLabel(mcs *fleetnetv1alpha1.MultiClusterService) *types.NamespacedName {
	if val, ok := mcs.GetLabels()[multiClusterServiceLabelService]; ok {
		return &types.NamespacedName{Namespace: r.SystemNamespace, Name: val}
	}
	return nil
}

// mcs-controller will record service import name as the label when it successfully creates the service import.
func (r *Reconciler) serviceImportFromLabel(mcs *fleetnetv1alpha1.MultiClusterService) *types.NamespacedName {
	if val, ok := mcs.GetLabels()[multiClusterServiceLabelServiceImport]; ok {
		return &types.NamespacedName{Namespace: mcs.Namespace, Name: val}
	}
	return nil
}

func (r *Reconciler) handleUpdate(ctx context.Context, mcs *fleetnetv1alpha1.MultiClusterService) (ctrl.Result, error) {
	_, err := r.reconcileServiceImport(ctx, mcs)
	if err != nil {
		return ctrl.Result{}, err
	}
	// reconcileDerivedService
	// updateMultiClusterServiceStatus
	return ctrl.Result{}, nil
}

// reconcileServiceImport gets or creates the service import if not exists.
func (r *Reconciler) reconcileServiceImport(ctx context.Context, mcs *fleetnetv1alpha1.MultiClusterService) (*fleetnetv1alpha1.ServiceImport, error) {
	serviceImportName := r.serviceImportFromLabel(mcs)
	// check if mcs updates the service import. If so, need to delete the existing one and create new one.
	if serviceImportName != nil && serviceImportName.Name != mcs.Spec.ServiceImport.Name {
		if err := r.deleteServiceImport(ctx, serviceImportName); err != nil {
			klog.ErrorS(err, "Failed to remove service import of mcs", "multiClusterService", klog.KObj(mcs), "serviceImport", klog.KRef(serviceImportName.Namespace, serviceImportName.Name))
			if !errors.IsNotFound(err) {
				return nil, err
			}
		}
		serviceImportName.Name = mcs.Spec.ServiceImport.Name
	}
	if serviceImportName == nil {
		return r.createAndGetServiceImport(ctx, mcs)
	}

	serviceImport := fleetnetv1alpha1.ServiceImport{}
	if err := r.Client.Get(ctx, *serviceImportName, &serviceImport); err != nil {
		if !errors.IsNotFound(err) {
			klog.ErrorS(err, "Failed to get service import of mcs", "multiClusterService", klog.KObj(mcs), "serviceImport", klog.KRef(serviceImportName.Namespace, serviceImportName.Name))
			return nil, err
		}
		return r.createAndGetServiceImport(ctx, mcs)
	}
	return &serviceImport, nil
}

// createAndGetServiceImport creates the service import based on the mcs spec and updates the mcs label.
// Returns created service import.
func (r *Reconciler) createAndGetServiceImport(ctx context.Context, mcs *fleetnetv1alpha1.MultiClusterService) (*fleetnetv1alpha1.ServiceImport, error) {
	serviceImportName := types.NamespacedName{Namespace: mcs.Namespace, Name: mcs.Spec.ServiceImport.Name}
	ownerRef := metav1.NewControllerRef(mcs, schema.GroupVersionKind{Version: mcs.TypeMeta.APIVersion, Kind: mcs.TypeMeta.Kind})
	toCreate := fleetnetv1alpha1.ServiceImport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       serviceImportName.Namespace,
			Name:            serviceImportName.Name,
			OwnerReferences: []metav1.OwnerReference{*ownerRef},
		},
	}
	if err := r.Client.Create(ctx, &toCreate); err != nil {
		klog.ErrorS(err, "Failed to create service import of mcs", "multiClusterService", klog.KObj(mcs), "serviceImport", klog.KRef(serviceImportName.Namespace, serviceImportName.Name))
		return nil, err
	}
	// update mcs service import label
	mcs.GetLabels()[multiClusterServiceLabelServiceImport] = serviceImportName.Name
	if err := r.Client.Update(ctx, mcs); err != nil {
		klog.ErrorS(err, "Failed to update service import label of mcs", "multiClusterService", klog.KObj(mcs))
		return nil, err
	}
	serviceImport := fleetnetv1alpha1.ServiceImport{}
	if err := r.Client.Get(ctx, serviceImportName, &serviceImport); err != nil {
		klog.ErrorS(err, "Failed to get service import of mcs", "multiClusterService", klog.KObj(mcs), "serviceImport", klog.KRef(serviceImportName.Namespace, serviceImportName.Name))
		return nil, err
	}
	return &serviceImport, nil
}
