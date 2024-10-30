/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package serviceexport features the ServiceExport controller for exporting a Service from a member cluster to
// its fleet.
package serviceexport

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v4"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"sigs.k8s.io/cloud-provider-azure/pkg/azclient/publicipaddressclient"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	"go.goms.io/fleet/pkg/utils/controller"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/condition"
	"go.goms.io/fleet-networking/pkg/common/metrics"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
)

const (
	svcExportValidCondReason                 = "ServiceIsValid"
	svcExportInvalidNotFoundCondReason       = "ServiceNotFound"
	svcExportInvalidIneligibleCondReason     = "ServiceIneligible"
	svcExportPendingConflictResolutionReason = "ServicePendingConflictResolution"

	// svcExportCleanupFinalizer is the finalizer ServiceExport controllers adds to mark that
	// a ServiceExport can only be deleted after its corresponding Service has been unexported from the hub cluster.
	svcExportCleanupFinalizer = "networking.fleet.azure.com/svc-export-cleanup"

	// ControllerName is the name of the Reconciler.
	ControllerName = "serviceexport-controller"
)

// Reconciler reconciles the export of a Service.
type Reconciler struct {
	MemberClusterID string
	MemberClient    client.Client
	HubClient       client.Client
	// The namespace reserved for the current member cluster in the hub cluster.
	HubNamespace string
	Recorder     record.EventRecorder

	ResourceGroupName          string // default resource group name to create public IP address
	AzurePublicIPAddressClient publicipaddressclient.Interface

	EnableTrafficManagerFeature bool
}

//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceexports,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceexports/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceexports/finalizers,verbs=update
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=internalserviceexports,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile exports a Service.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	svcRef := klog.KRef(req.Namespace, req.Name)
	startTime := time.Now()
	klog.V(2).InfoS("Reconciliation starts", "service", svcRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Reconciliation ends", "service", svcRef, "latency", latency)
	}()

	// Retrieve the ServiceExport object.
	var svcExport fleetnetv1alpha1.ServiceExport
	if err := r.MemberClient.Get(ctx, req.NamespacedName, &svcExport); err != nil {
		if apierrors.IsNotFound(err) {
			// Skip the reconciliation if the ServiceExport does not exist; this happens when the controller detects
			// changes in a Service that has not been exported yet, or when a ServiceExport is deleted before the
			// corresponding Service is exported to the fleet (and a cleanup finalizer is added). Either case requires
			// no action on this controller's end.
			klog.V(4).InfoS("Service export is not found", "service", svcRef)
			return ctrl.Result{}, nil
		}
		// An error has occurred when getting the ServiceExport.
		klog.ErrorS(err, "Failed to get service export", "service", svcRef)
		return ctrl.Result{}, err
	}

	// Check if the ServiceExport has been deleted and needs cleanup (unexporting Service).
	// A ServiceExport needs cleanup when it has the ServiceExport cleanup finalizer added; the absence of this
	// finalizer guarantees that the corresponding Service has never been exported to the fleet, thus no action
	// is needed.
	if svcExport.DeletionTimestamp != nil {
		if controllerutil.ContainsFinalizer(&svcExport, svcExportCleanupFinalizer) {
			klog.V(4).InfoS("Service export is deleted; unexport the service", "service", svcRef)
			res, err := r.unexportService(ctx, &svcExport)
			if err != nil {
				klog.ErrorS(err, "Failed to unexport the service", "service", svcRef)
			}
			return res, err
		}
		return ctrl.Result{}, nil
	}

	// Check if the Service to export exists.
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: req.Namespace,
			Name:      req.Name,
		},
	}
	err := r.MemberClient.Get(ctx, req.NamespacedName, &svc)
	switch {
	// The Service to export does not exist or has been deleted.
	case apierrors.IsNotFound(err) || svc.DeletionTimestamp != nil:
		r.Recorder.Eventf(&svcExport, corev1.EventTypeWarning, "ServiceNotFound", "Service %s is not found or in the deleting state", svc.Name)

		// Unexport the Service if the ServiceExport has the cleanup finalizer added.
		klog.V(4).InfoS("Service is deleted; unexport the service", "service", svcRef)
		if controllerutil.ContainsFinalizer(&svcExport, svcExportCleanupFinalizer) {
			if _, err = r.unexportService(ctx, &svcExport); err != nil {
				klog.ErrorS(err, "Failed to unexport the service", "service", svcRef)
				return ctrl.Result{}, err
			}
		}
		// Mark the ServiceExport as invalid.
		klog.V(4).InfoS("Mark service export as invalid (service not found)", "service", svcRef)
		if err := r.markServiceExportAsInvalidNotFound(ctx, &svcExport); err != nil {
			klog.ErrorS(err, "Failed to mark service export as invalid (service not found)", "service", svcRef)
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	// An unexpected error occurs when retrieving the Service.
	case err != nil:
		klog.ErrorS(err, "Failed to get the service", "service", svcRef)
		return ctrl.Result{}, err
	}

	// Check if the Service is eligible for export.
	if !isServiceEligibleForExport(&svc) {
		r.Recorder.Eventf(&svcExport, corev1.EventTypeWarning, "ServiceNotEligible", "Service %s is not eligible for exporting and please check service spec", svc.Name)

		// Unexport ineligible Service if the ServiceExport has the cleanup finalizer added.
		if controllerutil.ContainsFinalizer(&svcExport, svcExportCleanupFinalizer) {
			klog.V(4).InfoS("Service is ineligible; unexport the service", "service", svcRef)
			if _, err = r.unexportService(ctx, &svcExport); err != nil {
				klog.ErrorS(err, "Failed to unexport the service", "service", svcRef)
				return ctrl.Result{}, err
			}
		}
		// Mark the ServiceExport as invalid.
		klog.V(4).InfoS("Mark service export as invalid (service ineligible)", "service", svcRef)
		err := r.markServiceExportAsInvalidSvcIneligible(ctx, &svcExport, &svc)
		if err != nil {
			klog.ErrorS(err, "Failed to mark service export as invalid (service ineligible)", "service", svcRef)
		}
		return ctrl.Result{}, err
	}

	// Add the cleanup finalizer to the ServiceExport; this must happen before the Service is actually exported.
	if !controllerutil.ContainsFinalizer(&svcExport, svcExportCleanupFinalizer) {
		klog.V(4).InfoS("Add cleanup finalizer to service export", "service", svcRef)
		if err := r.addServiceExportCleanupFinalizer(ctx, &svcExport); err != nil {
			klog.ErrorS(err, "Failed to add cleanup finalizer to svc export", "service", svcRef)
			return ctrl.Result{}, err
		}
	}

	// Mark the ServiceExport as valid.
	klog.V(4).InfoS("Mark service export as valid", "service", svcRef)
	if err := r.markServiceExportAsValid(ctx, &svcExport, &svc); err != nil {
		klog.ErrorS(err, "Failed to mark service export as valid", "service", svcRef)
		return ctrl.Result{}, err
	}

	// Retrieve the last seen resource version and the last seen timestamp; these two values are used for metric collection.
	// If the two values are not present or not valid, annotate ServiceExport with new values.
	//
	// Note that the two values are not tamperproof.
	exportedSince, err := r.collectAndVerifyLastSeenResourceVersionAndTimestamp(ctx, &svc, &svcExport, startTime)
	if err != nil {
		klog.Warning("Failed to annotate last seen generation and timestamp", "serviceExport", svcRef)
	}

	// Export the Service or update the exported Service.

	// Create or update the InternalServiceExport object.
	internalSvcExport := fleetnetv1alpha1.InternalServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: r.HubNamespace,
			Name:      formatInternalServiceExportName(&svcExport),
		},
	}
	svcExportPorts := extractServicePorts(&svc)
	klog.V(2).InfoS("Export the service or update the exported service",
		"service", svcExport,
		"internalServiceExport", klog.KObj(&internalSvcExport))
	createOrUpdateOp, err := controllerutil.CreateOrUpdate(ctx, r.HubClient, &internalSvcExport, func() error {
		if internalSvcExport.CreationTimestamp.IsZero() {
			// Set the ServiceReference only when the InternalServiceExport is created; most of the fields in
			// an ExportedObjectReference should be immutable.
			internalSvcExport.Spec.ServiceReference = fleetnetv1alpha1.FromMetaObjects(r.MemberClusterID,
				svc.TypeMeta, svc.ObjectMeta, metav1.NewTime(exportedSince))
		}

		// Return an error if an attempt is made to update an InternalServiceExport that references a different
		// Service from the one that is being reconciled. This usually happens when a service is deleted and
		// re-created immediately.
		if internalSvcExport.Spec.ServiceReference.UID != svc.UID {
			klog.V(4).InfoS("Failed to create/update internalServiceExport, UIDs mismatch",
				"service", svcRef,
				"internalServiceExport", klog.KObj(&internalSvcExport),
				"newUID", svc.UID,
				"oldUID", internalSvcExport.Spec.ServiceReference.UID)
			// The AlreadyExists error returned here features a different GVR source (service, rather than
			// internalServiceExport); such an error would never be yielded in the normal workflow.
			return apierrors.NewAlreadyExists(
				schema.GroupResource{Group: fleetnetv1alpha1.GroupVersion.Group, Resource: "Service"},
				fmt.Sprintf("%s/%s", svc.Namespace, svc.Name),
			)
		}

		internalSvcExport.Spec.Ports = svcExportPorts
		internalSvcExport.Spec.ServiceReference.UpdateFromMetaObject(svc.ObjectMeta, metav1.NewTime(exportedSince))

		if r.EnableTrafficManagerFeature {
			klog.V(2).InfoS("Collecting Traffic Manager related information", "service", svcRef)
			if err := r.setAzureRelatedInformation(ctx, &svc, &internalSvcExport); err != nil {
				klog.ErrorS(err, "Failed to populate the Azure information for the Traffic Manager feature", "service", svcRef)
				return err
			}
		}
		return nil
	})
	statusErr := &apierrors.StatusError{}
	ok := errors.As(err, &statusErr)
	switch {
	case apierrors.IsAlreadyExists(err) && ok && statusErr.Status().Details.Kind == "Service":
		// An export with the same key but different UID already exists; unexport the Service first, and
		// requeue a new attempt to export the Service.
		// Additional checks are performed here as two forms of AlreadyExists error can be returned in the CreateOrUpdate
		// call: it could be that an actual UID mismatch is found, however, since CreateOrUpdate is, in essence, a two-part op
		// (the function first gets the object, and then decides whether to create the object or update it according to the get
		// result), a racing condition may lead to an AlreadyExists error being yielded even if there is no UID mismatch at all.
		// This can happen, albeit quite rarely, when the system is under heavy load, and the informers cannot sync caches
		// fast enough; the out-of-date cache will return that an object does not exist when read, even though the object is
		// already present in the persistent store, and any subsequent create call would fail.
		if _, err := r.unexportService(ctx, &svcExport); err != nil {
			klog.ErrorS(err, "Failed to unexport the service", "service", svcRef)
			return ctrl.Result{}, err
		}
		// Unexporting a Service removes the cleanup finalizer from the ServiceExport, which in normal cases
		// will trigger another reconciliation loop automatically; for better clarity here the controller requests
		// the new reconciliation attempt explicitly.
		return ctrl.Result{Requeue: true}, nil
	case err != nil:
		klog.ErrorS(err, "Failed to create/update InternalServiceExport",
			"internalServiceExport", klog.KObj(&internalSvcExport),
			"service", svcRef,
			"op", createOrUpdateOp)
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *Reconciler) setAzureRelatedInformation(ctx context.Context, service *corev1.Service, export *fleetnetv1alpha1.InternalServiceExport) error {
	export.Spec.Type = service.Spec.Type
	if service.Spec.Type != corev1.ServiceTypeLoadBalancer {
		return nil
	}
	// The annotation value is case-sensitive.
	// https://github.com/kubernetes-sigs/cloud-provider-azure/blob/release-1.31/pkg/provider/azure_loadbalancer.go#L3559
	export.Spec.IsInternalLoadBalancer = service.Annotations[objectmeta.ServiceAnnotationAzureLoadBalancerInternal] == "true"
	if export.Spec.IsInternalLoadBalancer {
		// no need to populate the PublicIPResourceID and IsDNSLabelConfigured which are only applicable for external load balancer
		return nil
	}

	serviceKObj := klog.KObj(service)
	if len(service.Status.LoadBalancer.Ingress) == 0 {
		// Assuming once the service status is updated, the controller will be triggered again.
		klog.V(2).InfoS("The load balancer IP is not assigned yet", "service", serviceKObj)
		return nil
	}

	if service.Status.LoadBalancer.Ingress[0].IP == "" {
		err := errors.New("the service ingress is not nil but with empty IP")
		klog.ErrorS(controller.NewUnexpectedBehaviorError(err), "Failed to get the load balancer IP from service", "service", serviceKObj, "status", service.Status)
		return nil
	}

	pip, err := r.lookupPublicIPResourceIDByLoadBalancerIP(ctx, service)
	if err != nil {
		return err
	}
	if pip == nil {
		klog.V(2).InfoS("The public IP is in the progressing", "service", serviceKObj, "ip", service.Status.LoadBalancer.Ingress[0].IP)
		// Assuming once the service status is updated, the controller will be triggered again in instead of retrying here
		// to avoid sending Azure requests.
		return nil
	}
	export.Spec.PublicIPResourceID = pip.ID
	// Note the user can set the dns label via the Azure portal or Azure CLI without updating service.
	// This information may be stale as we don't monitor the public IP address resource.
	export.Spec.IsDNSLabelConfigured = pip.Properties != nil && pip.Properties.DNSSettings != nil && pip.Properties.DNSSettings.DomainNameLabel != nil
	return nil
}

// TODO: can improve the performance by caching the public IP address resource ID.
func (r *Reconciler) lookupPublicIPResourceIDByLoadBalancerIP(ctx context.Context, service *corev1.Service) (*armnetwork.PublicIPAddress, error) {
	// The customer can specify the resource group for the public IP address in the service annotation.
	rg := strings.TrimSpace(service.Annotations[objectmeta.ServiceAnnotationLoadBalancerResourceGroup])
	if len(rg) == 0 {
		rg = r.ResourceGroupName
	}
	serviceKObj := klog.KObj(service)
	pips, err := r.AzurePublicIPAddressClient.List(ctx, rg)
	if err != nil {
		klog.ErrorS(err, "Failed to list Azure public IP addresses", "service", serviceKObj, "resourceGroup", rg)
		return nil, err
	}
	for _, pip := range pips {
		if pip.Properties != nil && pip.Properties.IPAddress != nil &&
			*pip.Properties.IPAddress == service.Status.LoadBalancer.Ingress[0].IP {
			return pip, nil
		}
	}
	klog.V(2).InfoS("The public IP address resource ID cannot be found in the public IP lists", "service", serviceKObj, "ip", service.Status.LoadBalancer.Ingress[0].IP, "resourceGroup", rg)
	return nil, nil
}

// SetupWithManager builds a controller with Reconciler and sets it up with a controller manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// The ServiceExport controller watches over ServiceExport objects.
		For(&fleetnetv1alpha1.ServiceExport{}).
		// The ServiceExport controller watches over Service objects.
		Watches(&corev1.Service{}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}

// unexportService unexports a Service, specifically, it deletes the corresponding InternalServiceExport from the
// hub cluster and removes the cleanup finalizer.
func (r *Reconciler) unexportService(ctx context.Context, svcExport *fleetnetv1alpha1.ServiceExport) (ctrl.Result, error) {
	// Get the unique name assigned when the Service is exported. it is guaranteed that Services are
	// always exported using the name format `ORIGINAL_NAMESPACE-ORIGINAL_NAME`; for example, a Service
	// from namespace `default`` with the name `store`` will be exported with the name `default-store`.
	internalSvcExportName := formatInternalServiceExportName(svcExport)
	internalSvcExport := &fleetnetv1alpha1.InternalServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: r.HubNamespace,
			Name:      internalSvcExportName,
		},
	}

	// Unexport the Service.
	if err := r.HubClient.Delete(ctx, internalSvcExport); err != nil && !apierrors.IsNotFound(err) {
		// It is guaranteed that a finalizer is always added to a ServiceExport before the corresponding Service is
		// actually exported; in some rare occasions, e.g. the controller crashes right after it adds the finalizer
		// to the ServiceExport but before the it gets a chance to actually export the Service to the
		// hub cluster, it could happen that a ServiceExport has a finalizer present yet the corresponding Service
		// has not been exported to the hub cluster. It is an expected behavior and no action is needed on this
		// controller's end.
		return ctrl.Result{}, err
	}

	// Remove the finalizer from the ServiceExport; it must happen after the Service has been successfully unexported.
	if err := r.removeServiceExportCleanupFinalizer(ctx, svcExport); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// removeServiceExportCleanupFinalizer removes the cleanup finalizer from a ServiceExport.
func (r *Reconciler) removeServiceExportCleanupFinalizer(ctx context.Context, svcExport *fleetnetv1alpha1.ServiceExport) error {
	controllerutil.RemoveFinalizer(svcExport, svcExportCleanupFinalizer)
	return r.MemberClient.Update(ctx, svcExport)
}

// markServiceExportAsInvalidNotFound marks a ServiceExport as invalid.
func (r *Reconciler) markServiceExportAsInvalidNotFound(ctx context.Context, svcExport *fleetnetv1alpha1.ServiceExport) error {
	validCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportValid))
	expectedValidCond := &metav1.Condition{
		Type:   string(fleetnetv1alpha1.ServiceExportValid),
		Status: metav1.ConditionFalse,
		// The Service is not found, therefore the observedGeneration field is ignored.
		Reason:  svcExportInvalidNotFoundCondReason,
		Message: fmt.Sprintf("service %s/%s is not found", svcExport.Namespace, svcExport.Name),
	}
	if condition.EqualCondition(validCond, expectedValidCond) {
		// A stable state has been reached; no further action is needed.
		return nil
	}

	meta.SetStatusCondition(&svcExport.Status.Conditions, *expectedValidCond)
	return r.MemberClient.Status().Update(ctx, svcExport)
}

// markServiceExportAsInvalidSvcIneligible marks a ServiceExport as invalid.
func (r *Reconciler) markServiceExportAsInvalidSvcIneligible(ctx context.Context, svcExport *fleetnetv1alpha1.ServiceExport, svc *corev1.Service) error {
	validCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportValid))
	expectedValidCond := &metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportValid),
		Status:             metav1.ConditionFalse,
		Reason:             svcExportInvalidIneligibleCondReason,
		ObservedGeneration: svc.Generation,
		Message:            fmt.Sprintf("service %s/%s is not eligible for export", svcExport.Namespace, svcExport.Name),
	}
	if condition.EqualCondition(validCond, expectedValidCond) {
		// A stable state has been reached; no further action is needed.
		return nil
	}

	meta.SetStatusCondition(&svcExport.Status.Conditions, *expectedValidCond)
	return r.MemberClient.Status().Update(ctx, svcExport)
}

// addServiceExportCleanupFinalizer adds the cleanup finalizer to a ServiceExport.
func (r *Reconciler) addServiceExportCleanupFinalizer(ctx context.Context, svcExport *fleetnetv1alpha1.ServiceExport) error {
	controllerutil.AddFinalizer(svcExport, svcExportCleanupFinalizer)
	return r.MemberClient.Update(ctx, svcExport)
}

// markServiceExportAsValid marks a ServiceExport as valid; if no conflict condition has been added, the
// ServiceExport will be marked as pending conflict resolution as well.
func (r *Reconciler) markServiceExportAsValid(ctx context.Context, svcExport *fleetnetv1alpha1.ServiceExport, svc *corev1.Service) error {
	validCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportValid))
	expectedValidCond := &metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportValid),
		Status:             metav1.ConditionTrue,
		Reason:             svcExportValidCondReason,
		ObservedGeneration: svc.Generation,
		Message:            fmt.Sprintf("service %s/%s is valid for export", svcExport.Namespace, svcExport.Name),
	}
	conflictCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportConflict))
	if condition.EqualCondition(validCond, expectedValidCond) &&
		conflictCond != nil {
		// A stable state has been reached; no further action is needed.
		return nil
	}

	meta.SetStatusCondition(&svcExport.Status.Conditions, *expectedValidCond)
	meta.SetStatusCondition(&svcExport.Status.Conditions, metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportConflict),
		Status:             metav1.ConditionUnknown,
		ObservedGeneration: svc.Generation,
		Reason:             svcExportPendingConflictResolutionReason,
		Message:            fmt.Sprintf("service %s/%s is pending export conflict resolution", svcExport.Namespace, svcExport.Name),
	})
	r.Recorder.Eventf(svcExport, corev1.EventTypeNormal, "ValidServiceExport", "Service %s is valid for export", svcExport.Name)
	r.Recorder.Eventf(svcExport, corev1.EventTypeNormal, "PendingExportConflictResolution", "Service %s is pending export conflict resolution", svcExport.Name)
	return r.MemberClient.Status().Update(ctx, svcExport)
}

// collectAndVerifyLastSeenResourceVersionAndTime collects and verifies the last seen resource version and timestamp annotations
// on ServiceExports; it will assign new values if the annotations are not present or not valid.
func (r *Reconciler) collectAndVerifyLastSeenResourceVersionAndTimestamp(ctx context.Context,
	svc *corev1.Service, svcExport *fleetnetv1alpha1.ServiceExport, startTime time.Time) (time.Time, error) {
	// Check if the two annotations are present; assign new values if they are absent.
	lastSeenResourceVersion, lastSeenResourceVersionOk := svcExport.Annotations[metrics.MetricsAnnotationLastSeenResourceVersion]
	lastSeenTimestampData, lastSeenTimestampOk := svcExport.Annotations[metrics.MetricsAnnotationLastSeenTimestamp]
	if !lastSeenResourceVersionOk || !lastSeenTimestampOk {
		return startTime, r.annotateLastSeenResourceVersionAndTimestamp(ctx, svc, svcExport, startTime)
	}

	lastSeenTimestamp, lastSeenTimestampErr := time.Parse(metrics.MetricsLastSeenTimestampFormat, lastSeenTimestampData)
	if lastSeenTimestampErr != nil {
		return startTime, r.annotateLastSeenResourceVersionAndTimestamp(ctx, svc, svcExport, startTime)
	}
	if lastSeenResourceVersion != svc.ResourceVersion || lastSeenTimestamp.After(startTime) {
		return startTime, r.annotateLastSeenResourceVersionAndTimestamp(ctx, svc, svcExport, startTime)
	}
	return lastSeenTimestamp, nil
}

// annotateLastSeenResourceVersionAndTimestamp annotates a ServiceExport with last seen resource version and timestamp.
func (r *Reconciler) annotateLastSeenResourceVersionAndTimestamp(ctx context.Context,
	svc *corev1.Service, svcExport *fleetnetv1alpha1.ServiceExport, startTime time.Time) error {
	// Initialize the annotation map if no annoation has been added yet.
	if svcExport.Annotations == nil {
		svcExport.Annotations = map[string]string{}
	}

	svcExport.Annotations[metrics.MetricsAnnotationLastSeenResourceVersion] = svc.ResourceVersion
	svcExport.Annotations[metrics.MetricsAnnotationLastSeenTimestamp] = startTime.Format(metrics.MetricsLastSeenTimestampFormat)
	return r.MemberClient.Update(ctx, svcExport)
}
