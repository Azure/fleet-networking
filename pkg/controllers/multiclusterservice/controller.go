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
	"strconv"
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

	// multiClusterService annotation
	multiClusterServiceAnnotationInternalLoadBalancer = "networking.fleet.azure.com/azure-load-balancer-internal"

	// service annotation
	serviceAnnotationInternalLoadBalancer = "service.beta.kubernetes.io/azure-load-balancer-internal"
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
	if err := r.deleteDerivedService(ctx, serviceName, mcs); err != nil {
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

func (r *Reconciler) deleteDerivedService(ctx context.Context, serviceName *types.NamespacedName, mcs *fleetnetv1alpha1.MultiClusterService) error {
	if serviceName == nil {
		return nil
	}

	// Retrieve the service first.
	derivedSvc := &corev1.Service{}
	if err := r.Client.Get(ctx, *serviceName, derivedSvc); err != nil {
		return fmt.Errorf("failed to get derived service: %w", err)
	}

	// Note that here the controller only checks for the presence of the owner object namespace label as the two labels
	// are always set together when the derived service is created/updated.
	ownerMCSNamespace, foundOwnerNS := derivedSvc.GetLabels()[serviceLabelMCSNamespace]
	ownerMCSName := derivedSvc.GetLabels()[serviceLabelMCSName]
	if foundOwnerNS && (ownerMCSNamespace != mcs.Namespace || ownerMCSName != mcs.Name) {
		// The derived service is owned by another MCS, which signals a name collision situation. No action needs
		// to be taken on the linked derived service any more, as it is managed by a different MCS.
		klog.V(2).InfoS("The derived service is owned by another MCS, no cleanup needed",
			"multiClusterService", klog.KObj(mcs),
			"derivedService", klog.KRef(serviceName.Namespace, serviceName.Name),
			"ownerMCS", klog.KRef(ownerMCSNamespace, ownerMCSName))
		return nil
	}
	return r.Client.Delete(ctx, derivedSvc)
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
		var err error
		serviceName, err = r.uniqueDerivedServiceName(mcs)
		if err != nil {
			klog.ErrorS(err, "Failed to generate a unique derived service name for mcs", "multiClusterService", mcsKObj)
			return ctrl.Result{}, fmt.Errorf("failed to generate a unique derived service name: %w", err)
		}
		klog.V(4).InfoS("Generated derived service name", "multiClusterService", mcsKObj, "derivedService", *serviceName)
	}
	// update mcs service label first to prevent the controller abort before we create the resource
	if err := r.updateMultiClusterLabel(ctx, mcs, objectmeta.MultiClusterServiceLabelDerivedService, serviceName.Name); err != nil {
		klog.ErrorS(err, "Failed to update MCS with derived service name labels", "multiClusterService", mcsKObj, "derivedService", *serviceName)
		return ctrl.Result{}, fmt.Errorf("failed to update MCS with derived service name labels: %w", err)
	}

	// To address a name collision issue, the controller has been updated to generate derived service names differently,
	// from the format [MCS-NAMESPACE]-[MCS-NAME] to [MCS-NAMESPACE]-[MCS-NAME]-[HASH-SUFFIX].
	//
	// However, there might be existing MCS that were created before this change, which already had a derived service
	// created in the old format. To ensure that such MCS continues to function with no interruption, here the controller
	// performs an extra round of check: if the MCS has been linked with a derived service, we check if the derived service
	// has been labeled with the namespace and name of the owner MCS; should the labels exist but do not match with that of the current
	// MCS, we know that a name collision has occurred, and the MCS being reconciled will be assigned a new derived service
	// using the new name format. Otherwise the MCS will continue to use the existing derived service.
	res, err := r.verifyDerivedServiceOwnership(ctx, mcs, serviceName.Name)
	if err != nil {
		klog.ErrorS(err, "Failed to verify derived service ownership", "multiClusterService", mcsKObj, "derivedService", *serviceName)
		return ctrl.Result{}, fmt.Errorf("failed to verify derived service ownership: %w", err)
	}
	switch res {
	case derivedSvcOwnerVeriResNotFound:
		// The derived service has not been created yet; no further action to take here, as the following
		// createOrUpdate step will create the derived service.
	case derivedSvcOwnerVeriResOrphaned:
		// The derived service exists but is missing owner information; let the following createOrUpdate step to overwrite
		// it with the correct owner information. This normally wouldn't happen.
		klog.V(2).InfoS("Derived service has no owner information set", "multiClusterService", mcsKObj, "derivedService", *serviceName)
	case derivedSvcOwnerVeriResOwnedByOthers:
		// The derived service is owned by another MCS, which signals a name collision situation. Remove the current
		// derived service name label and requeue the request to let the controller generate a new derived service name
		// for the MCS being reconciled.
		klog.V(2).InfoS("A name collision has been found; correct the situation by removing the current derived service name label and requeue", "multiClusterService", mcsKObj, "derivedService", *serviceName)
		delete(mcs.GetLabels(), objectmeta.MultiClusterServiceLabelDerivedService)
		if err := r.Client.Update(ctx, mcs); err != nil {
			klog.ErrorS(err, "Failed to remove the derived service name label", "multiClusterService", mcsKObj, "derivedService", *serviceName)
			return ctrl.Result{}, fmt.Errorf("failed to remove the derived service name label: %w", err)
		}
		return ctrl.Result{Requeue: true}, nil
	case derivedSvcOwnerVeriResSelfOwned:
		// The derived service is owned by the MCS being reconciled, which is the expected case; no further action needed.
	default:
		// An unexpected result is found.
		err := fmt.Errorf("unexpected result when verifying derived service ownership: %s", res)
		klog.ErrorS(err, "", "multiClusterService", mcsKObj, "derivedService", *serviceName, "result", res)
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
	if err := r.deleteDerivedService(ctx, serviceName, mcs); err != nil && !errors.IsNotFound(err) {
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

func configureInternalLoadBalancer(mcs *fleetnetv1alpha1.MultiClusterService, service *corev1.Service) {
	isInternal, err := strconv.ParseBool(mcs.Annotations[multiClusterServiceAnnotationInternalLoadBalancer])
	if err != nil || !isInternal {
		return
	}
	if service.GetAnnotations() == nil { // in case annotation map is nil
		service.Annotations = map[string]string{}
	}
	service.Annotations[serviceAnnotationInternalLoadBalancer] = "true"
}

type derivedSvcOwnerVeriRes string

const (
	derivedSvcOwnerVeriResNotFound      derivedSvcOwnerVeriRes = "NotFound"
	derivedSvcOwnerVeriResOrphaned      derivedSvcOwnerVeriRes = "Orphaned"
	derivedSvcOwnerVeriResOwnedByOthers derivedSvcOwnerVeriRes = "OwnedByOthers"
	derivedSvcOwnerVeriResSelfOwned     derivedSvcOwnerVeriRes = "SelfOwned"
	derivedSvcOwnerVeriResUnknown       derivedSvcOwnerVeriRes = "Unknown"
)

func (r *Reconciler) verifyDerivedServiceOwnership(
	ctx context.Context, mcs *fleetnetv1alpha1.MultiClusterService, derivedServiceName string) (derivedSvcOwnerVeriRes, error) {
	derivedService := &corev1.Service{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: r.FleetSystemNamespace, Name: derivedServiceName}, derivedService); err != nil {
		if errors.IsNotFound(err) {
			return derivedSvcOwnerVeriResNotFound, nil
		}
		return derivedSvcOwnerVeriResUnknown, fmt.Errorf("failed to get derived service: %w", err)
	}

	// Note that here the controller only checks for the presence of the owner object namespace label as the two labels
	// are always set together when the derived service is created/updated.
	ownerMCSNamespace, foundOwnerNS := derivedService.GetLabels()[serviceLabelMCSNamespace]
	ownerMCSName := derivedService.GetLabels()[serviceLabelMCSName]
	switch {
	case !foundOwnerNS:
		// The owner information is missing, this normally wouldn't happen, as the derived service is created in one go
		// with the owner information set as labels. Still, the controller handles this by letting the following createOrUpdate
		// step to overwrite the derived service with the correct owner information.
		return derivedSvcOwnerVeriResOrphaned, nil
	case ownerMCSNamespace != mcs.Namespace || ownerMCSName != mcs.Name:
		// The derived service is owned by another MCS, which signals a name collision situation. Set the controller to re-gen
		// a new derived service name for the MCS being reconciled.
		return derivedSvcOwnerVeriResOwnedByOthers, nil
	default:
		// The derived service is owned by the MCS being reconciled, which is the expected case.
		return derivedSvcOwnerVeriResSelfOwned, nil
	}
}

func (r *Reconciler) ensureDerivedService(mcs *fleetnetv1alpha1.MultiClusterService, serviceImport *fleetnetv1alpha1.ServiceImport, service *corev1.Service) error {
	// Verify the owner reference; throw an error if the controller is trying to update a derived service
	// that is owned by another MCS.
	//
	// Note that here the controller only checks for the presence of the owner object namespace label as the two labels
	// are always set together when the derived service is created/updated.
	ownerMCSNamespace, foundOwnerNS := service.GetLabels()[serviceLabelMCSNamespace]
	ownerMCSName := service.GetLabels()[serviceLabelMCSName]
	if foundOwnerNS && (ownerMCSNamespace != mcs.Namespace || ownerMCSName != mcs.Name) {
		// The derived service is owned by another MCS, which signals a name collision situation. Fail the createOrUpdate
		// step now.
		return fmt.Errorf("the derived service %s/%s is owned by another MCS %s/%s (expected %s/%s); there might be a name collision",
			service.Namespace, service.Name, ownerMCSNamespace, ownerMCSName, mcs.Namespace, mcs.Name)
	}

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
	configureInternalLoadBalancer(mcs, service)
	return nil
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
			&corev1.Service{},
			handler.EnqueueRequestsFromMapFunc(r.serviceEventHandler()),
		).
		Complete(r)
}

func (r *Reconciler) serviceEventHandler() handler.MapFunc {
	return func(_ context.Context, object client.Object) []reconcile.Request {
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
