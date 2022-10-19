/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package multiclusterservice features the mcs controller to reconcile multiclusterservice CRD.
// The controller could be installed in either hub cluster or member clusters.
package multiclusterservice

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/condition"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
)

const (
	// multiClusterService label
	multiClusterServiceFinalizer          = "networking.fleet.azure.com/service-resources-cleanup"
	multiClusterServiceLabelServiceImport = "networking.fleet.azure.com/service-import"

	// service label
	serviceLabelMCSName      = "networking.fleet.azure.com/multi-cluster-service-name"
	serviceLabelMCSNamespace = "networking.fleet.azure.com/multi-cluster-service-namespace"

	conditionReasonUnknownServiceImport = "UnknownServiceImport"
	conditionReasonFoundServiceImport   = "FoundServiceImport"

	mcsRetryInterval = time.Second * 5

	// ControllerName is the name of the Reconciler.
	ControllerName = "multiclusterservice-controller"
)

// Reconciler reconciles a MultiClusterService object.
type Reconciler struct {
	client.Client
	Scheme               *runtime.Scheme
	FleetSystemNamespace string // reserved fleet namespace
	Recorder             record.EventRecorder
}

//+kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=multiclusterservices,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=multiclusterservices/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=multiclusterservices/finalizers,verbs=get;update
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceimports,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile triggers a single reconcile round.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	name := req.NamespacedName
	mcs := fleetnetv1alpha1.MultiClusterService{}
	mcsKRef := klog.KRef(name.Namespace, name.Name)

	startTime := time.Now()
	klog.V(2).InfoS("Reconciliation starts", "multiClusterService", mcsKRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Reconciliation ends", "multiClusterService", mcsKRef, "latency", latency)
	}()

	if err := r.Client.Get(ctx, name, &mcs); err != nil {
		if errors.IsNotFound(err) {
			klog.V(4).InfoS("Ignoring NotFound multiClusterService", "multiClusterService", mcsKRef)
			return ctrl.Result{}, nil
		}
		klog.ErrorS(err, "Failed to get multiClusterService", "multiClusterService", mcsKRef)
		return ctrl.Result{}, err
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
		klog.V(4).InfoS("multiClusterService is being deleted", "multiClusterService", mcsKObj)
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
	// delete service import in the same namespace as the multi-cluster service
	serviceImportName := r.serviceImportFromLabel(mcs)
	if err := r.deleteServiceImport(ctx, serviceImportName); err != nil {
		klog.ErrorS(err, "Failed to remove service import of mcs", "multiClusterService", mcsKObj)
		if !errors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}
	r.Recorder.Eventf(mcs, corev1.EventTypeNormal, "UnimportedService", "Unimported service %s", serviceImportName)

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
	if val, ok := mcs.GetLabels()[objectmeta.MultiClusterServiceLabelDerivedService]; ok {
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
	if op, err := controllerutil.CreateOrUpdate(ctx, r.Client, serviceImport, func() error {
		return r.ensureServiceImport(serviceImport, mcs)
	}); err != nil {
		serviceImportKObj := klog.KObj(serviceImport)
		// If the service import is already owned by another MultiClusterService, serviceImport update or creation will fail.
		if err := r.Client.Get(ctx, desiredServiceImportName, serviceImport); err == nil && isServiceImportOwnedByOthers(mcs, serviceImport) { // check if NO error
			// reset the current serviceImport to empty as input so that internal func will update mcs status based on the serviceImport status
			// it won't change the serviceImport in the API server
			// TODO could be improved by moving into the mutate func and creating a customized error
			serviceImport.Status = fleetnetv1alpha1.ServiceImportStatus{}
			if err := r.handleInvalidServiceImport(ctx, mcs, serviceImport); err != nil {
				klog.ErrorS(err, "Failed to update status of mcs as serviceImport has been owned by other mcs", "multiClusterService", mcsKObj, "serviceImport", serviceImportKObj, "owner", serviceImport.OwnerReferences)
				return ctrl.Result{}, err
			}
			// have to requeue the request to see if the service import is deleted by owner or not
			klog.V(3).InfoS("ServiceImport has been owned by other mcs and requeue the request", "multiClusterService", mcsKObj, "serviceImport", serviceImportKObj)
			return ctrl.Result{RequeueAfter: mcsRetryInterval}, nil
		}

		klog.ErrorS(err, "Failed to create or update service import of mcs", "multiClusterService", mcsKObj, "serviceImport", serviceImportKObj, "op", op)
		return ctrl.Result{}, err
	}

	if len(serviceImport.Status.Clusters) == 0 {
		// Since there is no services exported in the clusters, delete derived service if exists.
		// When service import is still in the processing state and there is no derived service attached to the MCS,
		// it will do nothing.
		return ctrl.Result{}, r.handleInvalidServiceImport(ctx, mcs, serviceImport)
	}
	r.Recorder.Eventf(mcs, corev1.EventTypeNormal, "FoundValidService", "Found valid service %s and importing", serviceImport.Name)

	serviceName := r.derivedServiceFromLabel(mcs)
	if serviceName == nil {
		serviceName = r.generateDerivedServiceName(mcs)
		klog.V(4).InfoS("Generated derived service name", "multiClusterService", mcsKObj, "service", serviceName)
	}
	// update mcs service label first to prevent the controller abort before we create the resource
	if err := r.updateMultiClusterLabel(ctx, mcs, objectmeta.MultiClusterServiceLabelDerivedService, serviceName.Name); err != nil {
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
	if op, err := controllerutil.CreateOrUpdate(ctx, r.Client, service, func() error {
		return r.ensureDerivedService(mcs, serviceImport, service)
	}); err != nil {
		klog.ErrorS(err, "Failed to create or update derived service of mcs", "multiClusterService", mcsKObj, "service", klog.KObj(service), "op", op)
		return ctrl.Result{}, err
	}
	if err := r.updateMultiClusterServiceStatus(ctx, mcs, serviceImport, service); err != nil {
		return ctrl.Result{}, err
	}
	r.Recorder.Eventf(mcs, corev1.EventTypeNormal, "SuccessfulUpdateStatus", "Imported %s service and updated %s status", serviceImport.Name, mcs.Name)
	return ctrl.Result{}, nil
}

func isServiceImportOwnedByOthers(mcs *fleetnetv1alpha1.MultiClusterService, serviceImport *fleetnetv1alpha1.ServiceImport) bool {
	for _, owner := range serviceImport.OwnerReferences {
		if owner.APIVersion == mcs.APIVersion &&
			owner.Kind == mcs.Kind &&
			owner.Controller != nil && *owner.Controller &&
			owner.Name != mcs.Name {
			return true
		}
	}
	return false
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
	r.Recorder.Eventf(mcs, corev1.EventTypeNormal, "SuccessfulUpdateStatus", "Importing %s service and updated %s status", serviceImport.Name, mcs.Name)

	serviceName := r.derivedServiceFromLabel(mcs)
	mcsKObj := klog.KObj(mcs)
	if serviceName == nil {
		klog.V(4).InfoS("Skipping deleting derived service", "multiClusterService", mcsKObj)
		return nil // do nothing
	}
	svcKRef := klog.KRef(serviceName.Namespace, serviceName.Name)
	if err := r.deleteDerivedService(ctx, serviceName); err != nil && !errors.IsNotFound(err) {
		klog.ErrorS(err, "Failed to remove derived service of mcs", "multiClusterService", mcsKObj, "service", svcKRef)
		return err
	}
	// update mcs label
	delete(mcs.GetLabels(), objectmeta.MultiClusterServiceLabelDerivedService)
	if err := r.Client.Update(ctx, mcs); err != nil {
		klog.ErrorS(err, "Failed to update the derived service label of mcs", "multiClusterService", mcsKObj)
		return err
	}
	return nil
}

func (r *Reconciler) updateMultiClusterLabel(ctx context.Context, mcs *fleetnetv1alpha1.MultiClusterService, key, value string) error {
	labels := mcs.GetLabels()
	mcsKObj := klog.KObj(mcs)
	if v, ok := labels[key]; ok && v == value {
		// no need to update the mcs
		klog.V(4).InfoS("No need to update the mcs label", "multiClusterService", mcsKObj)
		return nil
	}
	if labels == nil { // in case labels map is nil and causes the panic
		mcs.Labels = map[string]string{}
	}
	mcs.Labels[key] = value
	if err := r.Client.Update(ctx, mcs); err != nil {
		klog.ErrorS(err, "Failed to add label to mcs", "multiClusterService", mcsKObj, "key", key, "value", value)
		return err
	}
	return nil
}

func (r *Reconciler) ensureDerivedService(mcs *fleetnetv1alpha1.MultiClusterService, serviceImport *fleetnetv1alpha1.ServiceImport, service *corev1.Service) error {
	svcPorts := make([]corev1.ServicePort, len(serviceImport.Status.Ports))
	for i, importPort := range serviceImport.Status.Ports {
		svcPorts[i] = importPort.ToServicePort()
	}
	service.Spec.Ports = svcPorts
	service.Spec.Type = corev1.ServiceTypeLoadBalancer

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
			ObservedGeneration: mcs.GetGeneration(),
			Message:            "importing service; if the condition remains for a while, please verify that service has been exported or service has been exported by other multiClusterService",
		}
	}

	mcsKObj := klog.KObj(mcs)
	if equality.Semantic.DeepEqual(mcs.Status.LoadBalancer, service.Status.LoadBalancer) &&
		condition.EqualCondition(currentCond, desiredCond) {
		klog.V(4).InfoS("Status is in the desired state and skipping updating status", "multiClusterService", mcsKObj)
		return nil
	}
	mcs.Status.LoadBalancer = service.Status.LoadBalancer
	meta.SetStatusCondition(&mcs.Status.Conditions, *desiredCond)

	klog.V(2).InfoS("Updating mcs status", "multiClusterService", mcsKObj)
	if err := r.Status().Update(ctx, mcs); err != nil {
		klog.ErrorS(err, "Failed to update mcs status", "multiClusterService", mcsKObj)
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
