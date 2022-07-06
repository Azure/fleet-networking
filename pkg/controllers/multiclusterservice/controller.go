// Package multiclusterservice features the mcs controller to multiclusterservice CRD.
// The controller could be installed in either hub cluster or member clusters.
package multiclusterservice

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/condition"
)

const (
	// multiClusterService label
	multiClusterServiceFinalizer          = "networking.fleet.azure.com/service-resources-cleanup"
	multiClusterServiceLabelService       = "networking.fleet.azure.com/derived-service"
	multiClusterServiceLabelServiceImport = "networking.fleet.azure.com/service-import"

	// service label
	serviceLabelMCSName      = "networking.fleet.azure.com/multi-cluster-service-name"
	serviceLabelMCSNamespace = "networking.fleet.azure.com/multi-cluster-service-namespace"

	conditionReasonUnknownServiceImport = "UnknownServiceImport"
	conditionReasonFoundServiceImport   = "FoundServiceImport"
)

// Reconciler reconciles a MultiClusterService object.
type Reconciler struct {
	client.Client
	Scheme               *runtime.Scheme
	FleetSystemNamespace string // reserved fleet namespace
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
	mcsKRef := klog.KRef(name.Namespace, name.Name)
	if err := r.Client.Get(ctx, name, &mcs); err != nil {
		klog.ErrorS(err, "Failed to get mcs", "multiClusterService", mcsKRef)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if mcs.ObjectMeta.DeletionTimestamp != nil {
		return r.handleDelete(ctx, &mcs)
	}

	// register finalizer
	if !controllerutil.ContainsFinalizer(&mcs, multiClusterServiceFinalizer) {
		controllerutil.AddFinalizer(&mcs, multiClusterServiceFinalizer)
		if err := r.Update(ctx, &mcs); err != nil {
			klog.ErrorS(err, "Failed to add mcs finalizer", "multiClusterService", mcsKRef)
			return ctrl.Result{}, err
		}
	}
	// handle update
	return r.handleUpdate(ctx, &mcs)
}

func (r *Reconciler) handleDelete(ctx context.Context, mcs *fleetnetv1alpha1.MultiClusterService) (ctrl.Result, error) {
	mcsKObj := klog.KObj(mcs)
	// The mcs is being deleted
	if !controllerutil.ContainsFinalizer(mcs, multiClusterServiceFinalizer) {
		return ctrl.Result{}, nil
	}

	klog.V(2).InfoS("Removing mcs", "multiClusterService", mcsKObj)

	// delete derived service in the fleet-system namespace
	serviceName := r.derivedServiceFromLabel(mcs)
	if err := r.deleteDerivedService(ctx, serviceName); err != nil {
		klog.ErrorS(err, "Failed to remove derived service of mcs", "multiClusterService", mcsKObj)
		if !errors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}
	// delete service import in the same namesapce as the multi-cluster service
	serviceImportName := r.serviceImportFromLabel(mcs)
	if err := r.deleteServiceImport(ctx, serviceImportName); err != nil {
		klog.ErrorS(err, "Failed to remove service import of mcs", "multiClusterService", mcsKObj)
		if !errors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}

	controllerutil.RemoveFinalizer(mcs, multiClusterServiceFinalizer)
	if err := r.Client.Update(ctx, mcs); err != nil {
		klog.ErrorS(err, "Failed to remove mcs finalizer", "multiClusterService", mcsKObj)
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
		return &types.NamespacedName{Namespace: r.FleetSystemNamespace, Name: val}
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
	mcsKObj := klog.KObj(mcs)
	currentServiceImportName := r.serviceImportFromLabel(mcs)
	desiredServiceImportName := types.NamespacedName{Namespace: mcs.Namespace, Name: mcs.Spec.ServiceImport.Name}
	if currentServiceImportName != nil && currentServiceImportName.Name != desiredServiceImportName.Name {
		if err := r.deleteServiceImport(ctx, currentServiceImportName); err != nil {
			klog.ErrorS(err, "Failed to remove service import of mcs", "multiClusterService", mcsKObj, "serviceImport", klog.KRef(currentServiceImportName.Namespace, currentServiceImportName.Name))
			if !errors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
		}
	}
	// update mcs service import label first to prevent the controller abort before we create the resource
	if err := r.updateMultiClusterLabel(ctx, mcs, multiClusterServiceLabelServiceImport, desiredServiceImportName.Name); err != nil {
		return ctrl.Result{}, err
	}
	serviceImport := &fleetnetv1alpha1.ServiceImport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: desiredServiceImportName.Namespace,
			Name:      desiredServiceImportName.Name,
		},
	}
	// CreateOrUpdate will
	// 1) Create a serviceImport if not exists.
	// OR 2) Update a serviceImport if the desired state does not match with current state.
	// OR 3) Get a serviceImport when ServiceImport status change triggers the MCS reconcile.
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, serviceImport, func() error {
		return r.ensureServiceImport(serviceImport, mcs)
	}); err != nil {
		klog.ErrorS(err, "Failed to create or update service import of mcs", "multiClusterService", mcsKObj, "serviceImport", klog.KObj(serviceImport))
		return ctrl.Result{}, err
	}

	if len(serviceImport.Status.Clusters) == 0 {
		// Since there is no services exported in the clusters, delete derived service if exists.
		// When service import is still in the processing state and there is no derived service attached to the MCS,
		// it will do nothing.
		return ctrl.Result{}, r.handleInvalidServiceImport(ctx, mcs, serviceImport)
	}
	serviceName := r.derivedServiceFromLabel(mcs)
	if serviceName == nil {
		serviceName = r.generateDerivedServiceName(mcs)
	}
	// update mcs service label first to prevent the controller abort before we create the resource
	if err := r.updateMultiClusterLabel(ctx, mcs, multiClusterServiceLabelService, serviceName.Name); err != nil {
		return ctrl.Result{}, err
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: serviceName.Namespace,
			Name:      serviceName.Name,
		},
	}
	// CreateOrUpdate will
	// 1) Create a service if not exists.
	// OR 2) Update a service if the desired state does not match with current state.
	// OR 3) Get a service when Service status change triggers the MCS reconcile.
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, service, func() error {
		return r.ensureDerivedService(mcs, serviceImport, service)
	}); err != nil {
		klog.ErrorS(err, "Failed to create or update derived service of mcs", "multiClusterService", mcsKObj, "service", klog.KObj(service))
		return ctrl.Result{}, err
	}
	if err := r.updateMultiClusterServiceStatus(ctx, mcs, serviceImport, service); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *Reconciler) ensureServiceImport(serviceImport *fleetnetv1alpha1.ServiceImport, mcs *fleetnetv1alpha1.MultiClusterService) error {
	return controllerutil.SetControllerReference(mcs, serviceImport, r.Scheme)
}

// handleInvalidServiceImport deletes derived service and updates its label when the service import is no longer valid.
func (r *Reconciler) handleInvalidServiceImport(ctx context.Context, mcs *fleetnetv1alpha1.MultiClusterService, serviceImport *fleetnetv1alpha1.ServiceImport) error {
	// If serviceImport is invalid or in the processing state, the existing mcs load balancer status should be reset.
	if err := r.updateMultiClusterServiceStatus(ctx, mcs, serviceImport, &corev1.Service{}); err != nil {
		return err
	}

	serviceName := r.derivedServiceFromLabel(mcs)
	if serviceName == nil {
		return nil // do nothing
	}
	mcsKObj := klog.KObj(mcs)
	svcKRef := klog.KRef(serviceName.Namespace, serviceName.Name)
	if err := r.deleteDerivedService(ctx, serviceName); err != nil && !errors.IsNotFound(err) {
		klog.ErrorS(err, "Failed to remove derived service of mcs", "multiClusterService", mcsKObj, "service", svcKRef)
		return err
	}
	// update mcs label
	delete(mcs.GetLabels(), multiClusterServiceLabelService)
	if err := r.Client.Update(ctx, mcs); err != nil {
		klog.ErrorS(err, "Failed to update the derived service label of mcs", "multiClusterService", mcsKObj)
		return err
	}
	return nil
}

func (r *Reconciler) updateMultiClusterLabel(ctx context.Context, mcs *fleetnetv1alpha1.MultiClusterService, key, value string) error {
	labels := mcs.GetLabels()
	if v, ok := labels[key]; ok && v == value {
		// no need to update the mcs
		return nil
	}
	if labels == nil { // in case labels map is nil and causes the panic
		mcs.Labels = map[string]string{}
	}
	mcs.Labels[key] = value
	if err := r.Client.Update(ctx, mcs); err != nil {
		klog.ErrorS(err, "Failed to add label to mcs", "multiClusterService", klog.KObj(mcs), "key", key, "value", value)
		return err
	}
	return nil
}

func (r *Reconciler) ensureDerivedService(mcs *fleetnetv1alpha1.MultiClusterService, serviceImport *fleetnetv1alpha1.ServiceImport, service *corev1.Service) error {
	svcPorts := make([]corev1.ServicePort, len(serviceImport.Status.Ports))
	for i, importPort := range serviceImport.Status.Ports {
		svcPorts[i] = importPort.ToServicePort()
	}
	service.Spec = corev1.ServiceSpec{
		Type:  corev1.ServiceTypeLoadBalancer,
		Ports: svcPorts,
	}
	if service.GetLabels() == nil { // in case labels map is nil and causes the panic
		service.Labels = map[string]string{}
	}

	service.Labels[serviceLabelMCSName] = mcs.Name
	service.Labels[serviceLabelMCSNamespace] = mcs.Namespace
	return nil
}

// generateDerivedServiceName appends multiclusterservice name and namespace as the derived service name since a service
// import may be exported by the multiple MCSs.
// It makes sure the service name is unique and less than 63 characters.
func (r *Reconciler) generateDerivedServiceName(mcs *fleetnetv1alpha1.MultiClusterService) *types.NamespacedName {
	// TODO make sure the service name is unique and less than 63 characters.
	return &types.NamespacedName{Namespace: r.FleetSystemNamespace, Name: fmt.Sprintf("%v-%v", mcs.Namespace, mcs.Name)}
}

// updateMultiClusterServiceStatus updates mcs condition and status based on the service import and service status.
func (r *Reconciler) updateMultiClusterServiceStatus(ctx context.Context, mcs *fleetnetv1alpha1.MultiClusterService, serviceImport *fleetnetv1alpha1.ServiceImport, service *corev1.Service) error {
	currentCond := meta.FindStatusCondition(mcs.Status.Conditions, string(fleetnetv1alpha1.MultiClusterServiceValid))
	desiredCond := &metav1.Condition{
		Type:               string(fleetnetv1alpha1.MultiClusterServiceValid),
		Status:             metav1.ConditionTrue,
		Reason:             conditionReasonFoundServiceImport,
		ObservedGeneration: mcs.GetGeneration(),
		Message:            "found valid service import",
	}
	if len(serviceImport.Status.Clusters) == 0 {
		desiredCond = &metav1.Condition{
			Type:               string(fleetnetv1alpha1.MultiClusterServiceValid),
			Status:             metav1.ConditionUnknown,
			Reason:             conditionReasonUnknownServiceImport,
			Message:            "waiting for processing service import or unable to find valid service import",
			ObservedGeneration: mcs.GetGeneration(),
		}
	}

	if equality.Semantic.DeepEqual(mcs.Status.LoadBalancer, service.Status.LoadBalancer) &&
		condition.EqualCondition(currentCond, desiredCond) {
		return nil
	}
	mcsKObj := klog.KObj(mcs)
	oldStatus := mcs.Status.DeepCopy()
	mcs.Status.LoadBalancer = service.Status.LoadBalancer
	meta.SetStatusCondition(&mcs.Status.Conditions, *desiredCond)

	klog.V(2).InfoS("Updating mcs status", "multiClusterService", mcsKObj, "status", mcs.Status, "oldStatus", oldStatus)
	if err := r.Status().Update(ctx, mcs); err != nil {
		klog.ErrorS(err, "Failed to update mcs status", "multiClusterService", mcsKObj, "status", mcs.Status, "oldStatus", oldStatus)
		return err
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetnetv1alpha1.MultiClusterService{}).
		Owns(&fleetnetv1alpha1.ServiceImport{}).
		// cannot add cross-namespace owner reference on service object
		// watch for the changes to the service object
		// This object is bound to be updated when Service in the fleet system namespace is updated. There is also a
		// filtering logic to enqueue those service event.
		Watches(
			&source.Kind{Type: &corev1.Service{}},
			handler.EnqueueRequestsFromMapFunc(r.serviceEventHandler()),
		).
		Complete(r)
}

func (r *Reconciler) serviceEventHandler() handler.MapFunc {
	return func(object client.Object) []reconcile.Request {
		namespace := object.GetLabels()[serviceLabelMCSNamespace]
		name := object.GetLabels()[serviceLabelMCSName]

		// ignore any service which is not in the fleet system namespace and does not have two labels
		if object.GetNamespace() != r.FleetSystemNamespace || namespace == "" || name == "" {
			return []reconcile.Request{}
		}
		return []reconcile.Request{
			{
				NamespacedName: types.NamespacedName{Namespace: namespace, Name: name},
			},
		}
	}
}
