/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package trafficmanagerbackend features the TrafficManagerBackend controller to reconcile TrafficManagerBackend CRs.
package trafficmanagerbackend

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/trafficmanager/armtrafficmanager"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/pkg/utils/controller"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	fleetnetv1beta1 "go.goms.io/fleet-networking/api/v1beta1"
	"go.goms.io/fleet-networking/pkg/common/azureerrors"
	"go.goms.io/fleet-networking/pkg/common/defaulter"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/controllers/hub/trafficmanagerprofile"
)

const (
	trafficManagerBackendProfileFieldKey = ".spec.profile.name"
	trafficManagerBackendBackendFieldKey = ".spec.backend.name"
	// fields name used to filter resources
	exportedServiceFieldNamespacedName = ".spec.serviceReference.namespacedName"

	// AzureResourceEndpointNamePrefix is the prefix format of the Azure Traffic Manager Endpoint created by the fleet controller.
	// The naming convention of a Traffic Manager Endpoint is fleet-{TrafficManagerBackendUUID}#.
	// Using the UUID of the backend here in case to support cross namespace TrafficManagerBackend in the future.
	AzureResourceEndpointNamePrefix = "fleet-%s#"

	// AzureResourceEndpointNameFormat is the name format of the Azure Traffic Manager Endpoint created by the fleet controller.
	// The naming convention of a Traffic Manager Endpoint is {AzureResourceEndpointNamePrefix}{ServiceImportName}#{ClusterName}.
	// which is fleet-{TrafficManagerBackendUUID}#{ServiceImportName}#{ClusterName}.
	// ServiceImportName will be the same as the Service name, which is up to 63 characters (RFC 1035).
	// https://github.com/kubernetes/kubernetes/pull/29523
	// The cluster name length should be restricted to <= 63 characters.
	// The endpoint name must contain no more than 260 characters, excluding the following characters "< > * % $ : \ ? + /".
	AzureResourceEndpointNameFormat = "%s%s#%s"
)

var (
	// create the func as a variable so that the integration test can use a customized function.
	generateAzureTrafficManagerProfileNameFunc = func(profile *fleetnetv1beta1.TrafficManagerProfile) string {
		return trafficmanagerprofile.GenerateAzureTrafficManagerProfileName(profile)
	}
	generateAzureTrafficManagerEndpointNamePrefixFunc = func(backend *fleetnetv1beta1.TrafficManagerBackend) string {
		return fmt.Sprintf(AzureResourceEndpointNamePrefix, backend.UID)
	}
)

// Reconciler reconciles a trafficManagerBackend object.
type Reconciler struct {
	client.Client

	ProfilesClient    *armtrafficmanager.ProfilesClient
	EndpointsClient   *armtrafficmanager.EndpointsClient
	ResourceGroupName string // default resource group name to create azure traffic manager resources
}

//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=trafficmanagerbackends,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=trafficmanagerbackends/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=trafficmanagerbackends/finalizers,verbs=get;update
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=trafficmanagerprofiles,verbs=get;list;watch
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceimports,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile triggers a single reconcile round.
func (r *Reconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	name := req.NamespacedName
	backendKRef := klog.KRef(name.Namespace, name.Name)

	startTime := time.Now()
	klog.V(2).InfoS("Reconciliation starts", "trafficManagerBackend", backendKRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Reconciliation ends", "trafficManagerBackend", backendKRef, "latency", latency)
	}()

	backend := &fleetnetv1beta1.TrafficManagerBackend{}
	if err := r.Client.Get(ctx, name, backend); err != nil {
		if apierrors.IsNotFound(err) {
			klog.V(4).InfoS("Ignoring NotFound trafficManagerBackend", "trafficManagerBackend", backendKRef)
			return ctrl.Result{}, nil
		}
		klog.ErrorS(err, "Failed to get trafficManagerBackend", "trafficManagerBackend", backendKRef)
		return ctrl.Result{}, controller.NewAPIServerError(true, err)
	}

	if !backend.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.handleDelete(ctx, backend)
	}

	// register finalizer
	if !controllerutil.ContainsFinalizer(backend, objectmeta.TrafficManagerBackendFinalizer) {
		controllerutil.AddFinalizer(backend, objectmeta.TrafficManagerBackendFinalizer)
		if err := r.Update(ctx, backend); err != nil {
			klog.ErrorS(err, "Failed to add finalizer to trafficManagerBackend", "trafficManagerBackend", backend)
			return ctrl.Result{}, controller.NewUpdateIgnoreConflictError(err)
		}
	}
	// TODO: replace the following with defaulter wehbook
	defaulter.SetDefaultsTrafficManagerBackend(backend)
	return r.handleUpdate(ctx, backend)
}

func (r *Reconciler) handleDelete(ctx context.Context, backend *fleetnetv1beta1.TrafficManagerBackend) (ctrl.Result, error) {
	backendKObj := klog.KObj(backend)
	// The backend is being deleted
	if !controllerutil.ContainsFinalizer(backend, objectmeta.TrafficManagerBackendFinalizer) {
		klog.V(4).InfoS("TrafficManagerBackend is being deleted", "trafficManagerBackend", backendKObj)
		return ctrl.Result{}, nil
	}

	if err := r.deleteAzureTrafficManagerEndpoints(ctx, backend); err != nil {
		klog.ErrorS(err, "Failed to delete Azure Traffic Manager endpoints", "trafficManagerBackend", backendKObj)
		return ctrl.Result{}, err
	}

	controllerutil.RemoveFinalizer(backend, objectmeta.TrafficManagerBackendFinalizer)
	if err := r.Client.Update(ctx, backend); err != nil {
		klog.ErrorS(err, "Failed to remove trafficManagerBackend finalizer", "trafficManagerBackend", backendKObj)
		return ctrl.Result{}, controller.NewUpdateIgnoreConflictError(err)
	}
	klog.V(2).InfoS("Removed trafficManagerBackend finalizer", "trafficManagerBackend", backendKObj)
	return ctrl.Result{}, nil
}

func (r *Reconciler) deleteAzureTrafficManagerEndpoints(ctx context.Context, backend *fleetnetv1beta1.TrafficManagerBackend) error {
	backendKObj := klog.KObj(backend)
	profile := &fleetnetv1beta1.TrafficManagerProfile{}
	if err := r.Client.Get(ctx, types.NamespacedName{Name: backend.Spec.Profile.Name, Namespace: backend.Namespace}, profile); err != nil {
		if apierrors.IsNotFound(err) {
			klog.V(2).InfoS("NotFound trafficManagerProfile and Azure resources should be deleted ", "trafficManagerBackend", backendKObj, "trafficManagerProfile", backend.Spec.Profile.Name)
			return nil
		}
		klog.ErrorS(err, "Failed to get trafficManagerProfile", "trafficManagerBackend", backendKObj, "trafficManagerProfile", backend.Spec.Profile.Name)
		return controller.NewAPIServerError(true, err)
	}

	profileKObj := klog.KObj(profile)
	atmProfileName := generateAzureTrafficManagerProfileNameFunc(profile)
	getRes, getErr := r.ProfilesClient.Get(ctx, r.ResourceGroupName, atmProfileName, nil)
	if getErr != nil {
		if !azureerrors.IsNotFound(getErr) {
			klog.ErrorS(getErr, "Failed to get the Traffic Manager profile", "trafficManagerBackend", backendKObj, "trafficManagerProfile", profileKObj, "atmProfileName", atmProfileName)
			return getErr
		}
		klog.V(2).InfoS("Azure Traffic Manager profile does not exist", "trafficManagerBackend", backendKObj, "trafficManagerProfile", profileKObj, "atmProfileName", atmProfileName)
		return nil // skip handling endpoints deletion
	}
	return r.cleanupEndpoints(ctx, backend, &getRes.Profile)
}

func (r *Reconciler) cleanupEndpoints(ctx context.Context, backend *fleetnetv1beta1.TrafficManagerBackend, atmProfile *armtrafficmanager.Profile) error {
	backendKObj := klog.KObj(backend)
	if atmProfile.Properties == nil {
		klog.V(2).InfoS("Azure Traffic Manager profile has nil properties and skipping handling endpoints deletion", "trafficManagerBackend", backendKObj, "atmProfileName", atmProfile.Name)
		return nil
	}

	klog.V(2).InfoS("Deleting Azure Traffic Manager endpoints", "trafficManagerBackend", backendKObj, "trafficManagerProfile", backend.Spec.Profile.Name)
	atmProfileName := *atmProfile.Name
	errs, cctx := errgroup.WithContext(ctx)
	for i := range atmProfile.Properties.Endpoints {
		endpoint := atmProfile.Properties.Endpoints[i]
		if endpoint.Name == nil {
			err := controller.NewUnexpectedBehaviorError(errors.New("azure Traffic Manager endpoint name is nil"))
			klog.ErrorS(err, "Invalid Traffic Manager endpoint", "atmEndpoint", endpoint)
			continue
		}
		// Traffic manager endpoint name is case-insensitive.
		if !isEndpointOwnedByBackend(backend, *endpoint.Name) {
			continue // skipping deleting the endpoints which are not created by this backend
		}
		errs.Go(func() error {
			if _, err := r.EndpointsClient.Delete(cctx, r.ResourceGroupName, atmProfileName, armtrafficmanager.EndpointTypeAzureEndpoints, *endpoint.Name, nil); err != nil {
				if azureerrors.IsNotFound(err) {
					klog.V(2).InfoS("Ignoring NotFound Azure Traffic Manager endpoint", "trafficManagerBackend", backendKObj, "atmProfileName", atmProfileName, "atmEndpoint", *endpoint.Name)
					return nil
				}
				klog.ErrorS(err, "Failed to delete the endpoint", "trafficManagerBackend", backendKObj, "atmProfileName", atmProfileName, "atmEndpoint", *endpoint.Name)
				return err
			}
			klog.V(2).InfoS("Deleted Azure Traffic Manager endpoint", "trafficManagerBackend", backendKObj, "atmProfileName", atmProfileName, "atmEndpoint", *endpoint.Name)
			return nil
		})
	}
	return errs.Wait()
}

func isEndpointOwnedByBackend(backend *fleetnetv1beta1.TrafficManagerBackend, endpoint string) bool {
	return strings.HasPrefix(endpoint, generateAzureTrafficManagerEndpointNamePrefixFunc(backend))
}

func (r *Reconciler) handleUpdate(ctx context.Context, backend *fleetnetv1beta1.TrafficManagerBackend) (ctrl.Result, error) {
	backendKObj := klog.KObj(backend)
	profile, err := r.validateTrafficManagerProfile(ctx, backend)
	if err != nil || profile == nil {
		// We don't need to requeue the invalid Profile (err == nil and profile == nil) because when the profile becomes
		// valid, the controller will be re-triggered again.
		// The controller will retry when err is not nil.
		return ctrl.Result{}, err
	}
	profileKObj := klog.KObj(profile)
	klog.V(2).InfoS("Found the valid trafficManagerProfile", "trafficManagerBackend", backendKObj, "trafficManagerProfile", profileKObj)

	atmProfile, err := r.validateAzureTrafficManagerProfile(ctx, backend, profile)
	if err != nil || atmProfile == nil {
		// We don't need to requeue the invalid Azure Traffic Manager profile (err == nil and atmProfile == nil) as when
		// the profile becomes valid, the controller will be re-triggered again.
		// The controller will retry when err is not nil.
		return ctrl.Result{}, err
	}
	klog.V(2).InfoS("Found the valid Azure Traffic Manager Profile", "trafficManagerBackend", backendKObj, "trafficManagerProfile", profileKObj, "atmProfileName", atmProfile.Name)

	serviceImport, err := r.validateServiceImportAndCleanupEndpointsIfInvalid(ctx, backend, atmProfile)
	if err != nil || serviceImport == nil {
		// We don't need to requeue the invalid serviceImport (err == nil and serviceImport == nil) as when the serviceImport
		// becomes valid, the controller will be re-triggered again.
		// The controller will retry when err is not nil.
		return ctrl.Result{}, err
	}

	klog.V(2).InfoS("Found the serviceImport", "trafficManagerBackend", backendKObj, "serviceImport", klog.KObj(serviceImport), "clusters", serviceImport.Status.Clusters)

	if *backend.Spec.Weight == 0 {
		klog.V(2).InfoS("Weight is 0, deleting all the endpoints", "trafficManagerBackend", backendKObj)
		if err := r.cleanupEndpoints(ctx, backend, atmProfile); err != nil {
			return ctrl.Result{}, err
		}
		setTrueCondition(backend, nil)
		return ctrl.Result{}, r.updateTrafficManagerBackendStatus(ctx, backend)
	}

	desiredEndpointsMaps, invalidServicesMaps, err := r.validateExportedServiceForServiceImport(ctx, backend, serviceImport)
	if err != nil || (desiredEndpointsMaps == nil && invalidServicesMaps == nil) {
		// We don't need to requeue not found internalServiceExport(err == nil and desiredEndpointsMaps == nil && invalidServicesMaps == nil)
		// as when the serviceImport is updated, the controller will be re-triggered again.
		// The controller will retry when err is not nil.
		return ctrl.Result{}, err
	}
	klog.V(2).InfoS("Found the exported services behind the serviceImport", "trafficManagerBackend", backendKObj, "serviceImport", klog.KObj(serviceImport), "numberOfDesiredEndpoints", len(desiredEndpointsMaps), "numberOfInvalidServices", len(invalidServicesMaps))

	acceptedEndpoints, badEndpointsErr, err := r.updateTrafficManagerEndpointsAndUpdateStatusIfUnknown(ctx, backend, atmProfile, desiredEndpointsMaps)
	if err != nil {
		return ctrl.Result{}, err
	}
	if len(invalidServicesMaps) == 0 && len(badEndpointsErr) == 0 {
		setTrueCondition(backend, acceptedEndpoints)
	} else {
		var invalidEndpointErrMessage string
		if len(badEndpointsErr) > 0 {
			invalidEndpointErrMessage = fmt.Sprintf("%v endpoint(s) failed to be created/updated in the Azure Traffic Manager, for example, %v; ", len(badEndpointsErr), badEndpointsErr[0])
		}
		if len(invalidServicesMaps) > 0 {
			for clusterID, invalidServiceErr := range invalidServicesMaps {
				invalidEndpointErrMessage = invalidEndpointErrMessage + fmt.Sprintf("%v service(s) exported from clusters cannot be exposed as the Azure Traffic Manager, for example, service exported from %v is invalid: %v", len(invalidServicesMaps), clusterID, invalidServiceErr)
				// Here we only populate the message with the first invalid exported service.
				// Note, the loop of the invalidServicesMaps is not deterministic.
				break
			}
		}
		setFalseCondition(backend, acceptedEndpoints, invalidEndpointErrMessage)
	}
	klog.V(2).InfoS("Updated Traffic Manager endpoints for the serviceImport and updating the condition", "trafficManagerBackend", backendKObj, "status", backend.Status)
	return ctrl.Result{}, r.updateTrafficManagerBackendStatus(ctx, backend)
}

// validateTrafficManagerProfile returns not nil profile when the profile is valid.
func (r *Reconciler) validateTrafficManagerProfile(ctx context.Context, backend *fleetnetv1beta1.TrafficManagerBackend) (*fleetnetv1beta1.TrafficManagerProfile, error) {
	backendKObj := klog.KObj(backend)
	var cond metav1.Condition
	profile := &fleetnetv1beta1.TrafficManagerProfile{}
	if getProfileErr := r.Client.Get(ctx, types.NamespacedName{Name: backend.Spec.Profile.Name, Namespace: backend.Namespace}, profile); getProfileErr != nil {
		if apierrors.IsNotFound(getProfileErr) {
			klog.V(2).InfoS("NotFound trafficManagerProfile", "trafficManagerBackend", backendKObj, "trafficManagerProfile", backend.Spec.Profile.Name)
			setFalseCondition(backend, nil, fmt.Sprintf("TrafficManagerProfile %q is not found", backend.Spec.Profile.Name))
			return nil, r.updateTrafficManagerBackendStatus(ctx, backend)
		}
		klog.ErrorS(getProfileErr, "Failed to get trafficManagerProfile", "trafficManagerBackend", backendKObj, "trafficManagerProfile", backend.Spec.Profile.Name)
		setUnknownCondition(backend, fmt.Sprintf("Failed to get the trafficManagerProfile %q: %v", backend.Spec.Profile.Name, getProfileErr))
		if err := r.updateTrafficManagerBackendStatus(ctx, backend); err != nil {
			return nil, err
		}
		return nil, getProfileErr // need to return the error to requeue the request
	}
	programmedCondition := meta.FindStatusCondition(profile.Status.Conditions, string(fleetnetv1beta1.TrafficManagerProfileConditionProgrammed))
	if condition.IsConditionStatusTrue(programmedCondition, profile.GetGeneration()) {
		return profile, nil // return directly if the trafficManagerProfile is programmed
	} else if condition.IsConditionStatusFalse(programmedCondition, profile.GetGeneration()) {
		setFalseCondition(backend, nil, fmt.Sprintf("Invalid trafficManagerProfile %q: %v", backend.Spec.Profile.Name, programmedCondition.Message))
	} else {
		setUnknownCondition(backend, fmt.Sprintf("In the processing of trafficManagerProfile %q", backend.Spec.Profile.Name))
	}
	klog.V(2).InfoS("Profile has not been accepted and updating the status", "trafficManagerBackend", backendKObj, "condition", cond)
	return nil, r.updateTrafficManagerBackendStatus(ctx, backend)
}

// validateAzureTrafficManagerProfile returns not nil Azure Traffic Manager profile when the atm profile is valid.
func (r *Reconciler) validateAzureTrafficManagerProfile(ctx context.Context, backend *fleetnetv1beta1.TrafficManagerBackend, profile *fleetnetv1beta1.TrafficManagerProfile) (*armtrafficmanager.Profile, error) {
	atmProfileName := generateAzureTrafficManagerProfileNameFunc(profile)
	backendKObj := klog.KObj(backend)
	profileKObj := klog.KObj(profile)
	getRes, getErr := r.ProfilesClient.Get(ctx, r.ResourceGroupName, atmProfileName, nil)
	if getErr != nil {
		if azureerrors.IsNotFound(getErr) {
			// We've already checked the TrafficManagerProfile condition before getting Azure resource.
			// It may happen when
			// 1. customers delete the azure profile manually
			// 2. the TrafficManagerProfile info is stale.
			// For the case 1, retry won't help to recover the Azure Traffic Manager profile resource.
			// For the case 2, the controller will be re-triggered when the TrafficManagerProfile is updated.
			klog.ErrorS(getErr, "NotFound Azure Traffic Manager profile", "trafficManagerBackend", backendKObj, "trafficManagerProfile", profileKObj, "atmProfileName", atmProfileName)
			// none of the endpoints are accepted by the TrafficManager
			setFalseCondition(backend, nil, fmt.Sprintf("Azure Traffic Manager profile %q under %q is not found", atmProfileName, r.ResourceGroupName))
			return nil, r.updateTrafficManagerBackendStatus(ctx, backend)
		}
		klog.V(2).InfoS("Failed to get Azure Traffic Manager profile", "trafficManagerBackend", backendKObj, "trafficManagerProfile", profileKObj, "atmProfileName", atmProfileName)
		setUnknownCondition(backend, fmt.Sprintf("Failed to get the Azure Traffic Manager profile %q under %q: %v", atmProfileName, r.ResourceGroupName, getErr))
		if err := r.updateTrafficManagerBackendStatus(ctx, backend); err != nil {
			return nil, err
		}
		return nil, getErr // need to return the error to requeue the request
	}
	return &getRes.Profile, nil
}

// validateServiceImportAndCleanupEndpointsIfInvalid returns not nil serviceImport when the serviceImport is valid.
func (r *Reconciler) validateServiceImportAndCleanupEndpointsIfInvalid(ctx context.Context, backend *fleetnetv1beta1.TrafficManagerBackend, azureProfile *armtrafficmanager.Profile) (*fleetnetv1alpha1.ServiceImport, error) {
	backendKObj := klog.KObj(backend)
	var cond metav1.Condition
	serviceImport := &fleetnetv1alpha1.ServiceImport{}
	if getServiceImportErr := r.Client.Get(ctx, types.NamespacedName{Name: backend.Spec.Backend.Name, Namespace: backend.Namespace}, serviceImport); getServiceImportErr != nil {
		if apierrors.IsNotFound(getServiceImportErr) {
			klog.V(2).InfoS("NotFound serviceImport and starting deleting any stale endpoints", "trafficManagerBackend", backendKObj, "serviceImport", backend.Spec.Backend.Name)
			if err := r.cleanupEndpoints(ctx, backend, azureProfile); err != nil {
				klog.ErrorS(err, "Failed to delete stale endpoints for an invalid serviceImport", "trafficManagerBackend", backendKObj, "serviceImport", backend.Spec.Backend.Name)
				return nil, err
			}
			cond = metav1.Condition{
				Type:               string(fleetnetv1beta1.TrafficManagerBackendConditionAccepted),
				Status:             metav1.ConditionFalse,
				ObservedGeneration: backend.Generation,
				Reason:             string(fleetnetv1beta1.TrafficManagerBackendReasonInvalid),
				Message:            fmt.Sprintf("ServiceImport %q is not found", backend.Spec.Backend.Name),
			}
			meta.SetStatusCondition(&backend.Status.Conditions, cond)
			backend.Status.Endpoints = []fleetnetv1beta1.TrafficManagerEndpointStatus{} // none of the endpoints are accepted by the TrafficManager
			return nil, r.updateTrafficManagerBackendStatus(ctx, backend)
		}
		klog.ErrorS(getServiceImportErr, "Failed to get serviceImport", "trafficManagerBackend", backendKObj, "serviceImport", backend.Spec.Backend.Name)
		setUnknownCondition(backend, fmt.Sprintf("Failed to get the serviceImport %q: %v", backend.Spec.Profile.Name, getServiceImportErr))
		if err := r.updateTrafficManagerBackendStatus(ctx, backend); err != nil {
			return nil, err
		}
		return nil, getServiceImportErr // need to return the error to requeue the request
	}
	return serviceImport, nil
}

func setFalseCondition(backend *fleetnetv1beta1.TrafficManagerBackend, acceptedEndpoints []fleetnetv1beta1.TrafficManagerEndpointStatus, message string) {
	cond := metav1.Condition{
		Type:               string(fleetnetv1beta1.TrafficManagerBackendConditionAccepted),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: backend.Generation,
		Reason:             string(fleetnetv1beta1.TrafficManagerBackendReasonInvalid),
		Message:            message,
	}
	if len(acceptedEndpoints) == 0 {
		backend.Status.Endpoints = []fleetnetv1beta1.TrafficManagerEndpointStatus{}
	} else {
		backend.Status.Endpoints = acceptedEndpoints
	}
	meta.SetStatusCondition(&backend.Status.Conditions, cond)
}

func setUnknownCondition(backend *fleetnetv1beta1.TrafficManagerBackend, message string) {
	cond := metav1.Condition{
		Type:               string(fleetnetv1beta1.TrafficManagerBackendConditionAccepted),
		Status:             metav1.ConditionUnknown,
		ObservedGeneration: backend.Generation,
		Reason:             string(fleetnetv1beta1.TrafficManagerBackendReasonPending),
		Message:            message,
	}
	backend.Status.Endpoints = []fleetnetv1beta1.TrafficManagerEndpointStatus{}
	meta.SetStatusCondition(&backend.Status.Conditions, cond)
}

func setTrueCondition(backend *fleetnetv1beta1.TrafficManagerBackend, acceptedEndpoints []fleetnetv1beta1.TrafficManagerEndpointStatus) {
	cond := metav1.Condition{
		Type:               string(fleetnetv1beta1.TrafficManagerBackendConditionAccepted),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: backend.Generation,
		Reason:             string(fleetnetv1beta1.TrafficManagerBackendReasonAccepted),
		Message:            fmt.Sprintf("%v service(s) exported from clusters have been accepted as Traffic Manager endpoints", len(acceptedEndpoints)),
	}
	backend.Status.Endpoints = acceptedEndpoints
	meta.SetStatusCondition(&backend.Status.Conditions, cond)
}

func (r *Reconciler) updateTrafficManagerBackendStatus(ctx context.Context, backend *fleetnetv1beta1.TrafficManagerBackend) error {
	backendKObj := klog.KObj(backend)
	if err := r.Client.Status().Update(ctx, backend); err != nil {
		klog.ErrorS(err, "Failed to update trafficManagerBackend status", "trafficManagerBackend", backendKObj)
		return controller.NewUpdateIgnoreConflictError(err)
	}
	klog.V(2).InfoS("Updated trafficManagerBackend status", "trafficManagerBackend", backendKObj, "status", backend.Status)
	return nil
}

type desiredEndpoint struct {
	Endpoint armtrafficmanager.Endpoint
	Cluster  fleetnetv1beta1.ClusterStatus
}

// validateExportedServiceForServiceImport returns two maps:
// * a map of desired endpoints for the serviceImport (key is the endpoint name).
// * a map of invalid services which cannot be exposed as the trafficManagerEndpoints (key is the cluster name).
func (r *Reconciler) validateExportedServiceForServiceImport(ctx context.Context, backend *fleetnetv1beta1.TrafficManagerBackend, serviceImport *fleetnetv1alpha1.ServiceImport) (map[string]desiredEndpoint, map[string]error, error) {
	backendKObj := klog.KObj(backend)
	serviceImportKObj := klog.KObj(serviceImport)

	if len(serviceImport.Status.Clusters) == 0 {
		klog.V(2).InfoS("No clusters found in the serviceImport", "trafficManagerBackend", backendKObj, "serviceImport", serviceImportKObj)
		// Controller will only create the serviceImport when there is a cluster exposing their services.
		// Updating the status will be in a separate call and could fail.
		setUnknownCondition(backend, "In the process of exporting the services")
		// We don't need to requeue the request and when the serviceImport status is set, the controller will be re-triggered.
		return nil, nil, r.updateTrafficManagerBackendStatus(ctx, backend)
	}

	internalServiceExportList := &fleetnetv1alpha1.InternalServiceExportList{}
	namespaceName := types.NamespacedName{Namespace: serviceImport.Namespace, Name: serviceImport.Name}
	listOpts := client.MatchingFields{
		exportedServiceFieldNamespacedName: namespaceName.String(),
	}
	if listErr := r.Client.List(ctx, internalServiceExportList, &listOpts); listErr != nil {
		klog.ErrorS(listErr, "Failed to list internalServiceExports used by the serviceImport", "trafficManagerBackend", backendKObj, "serviceImport", serviceImportKObj)
		setUnknownCondition(backend, fmt.Sprintf("Failed to list the exported service %q: %v", namespaceName, listErr))
		if err := r.updateTrafficManagerBackendStatus(ctx, backend); err != nil {
			return nil, nil, err
		}
		return nil, nil, listErr
	}
	internalServiceExportMap := make(map[string]*fleetnetv1alpha1.InternalServiceExport, len(internalServiceExportList.Items))
	for i, export := range internalServiceExportList.Items {
		internalServiceExportMap[export.Spec.ServiceReference.ClusterID] = &internalServiceExportList.Items[i]
	}

	desiredEndpoints := make(map[string]desiredEndpoint, len(serviceImport.Status.Clusters)) // key is the endpoint name
	invalidServices := make(map[string]error, len(serviceImport.Status.Clusters))            // key is cluster name
	for _, clusterStatus := range serviceImport.Status.Clusters {
		internalServiceExport, ok := internalServiceExportMap[clusterStatus.Cluster]
		if !ok {
			getErr := fmt.Errorf("failed to find the internalServiceExport for the cluster %q", clusterStatus.Cluster)
			// Usually controller should update the serviceImport status first before deleting the internalServiceImport.
			// It could happen that the current serviceImport has stale information.
			// The controller will be re-triggered when the serviceImport is updated.
			klog.ErrorS(getErr, "InternalServiceExport not found for the cluster", "trafficManagerBackend", backendKObj, "serviceImport", serviceImportKObj, "clusterID", clusterStatus.Cluster)
			setUnknownCondition(backend, fmt.Sprintf("Failed to find the exported service %q for %q: %v", namespaceName, clusterStatus.Cluster, getErr))
			return nil, nil, r.updateTrafficManagerBackendStatus(ctx, backend)
		}
		if err := isValidTrafficManagerEndpoint(internalServiceExport); err != nil {
			invalidServices[clusterStatus.Cluster] = err
			klog.V(2).InfoS("Invalid service for TrafficManager endpoint", "trafficManagerBackend", backendKObj, "serviceImport", serviceImportKObj, "clusterID", clusterStatus.Cluster, "error", err)
			continue
		}
		endpoint := generateAzureTrafficManagerEndpoint(backend, internalServiceExport)
		desiredEndpoints[*endpoint.Name] = desiredEndpoint{
			Endpoint: endpoint,
			Cluster: fleetnetv1beta1.ClusterStatus{
				Cluster: clusterStatus.Cluster,
			},
		}
	}
	desiredWeight := int(math.Ceil(float64(*backend.Spec.Weight) / float64(len(desiredEndpoints))))
	for _, dp := range desiredEndpoints {
		dp.Endpoint.Properties.Weight = ptr.To(int64(desiredWeight))
	}
	klog.V(2).InfoS("Finishing validating services", "trafficManagerBackend", backendKObj, "serviceImport", serviceImportKObj, "numberOfDesiredEndpoints", len(desiredEndpoints), "numberOfInvalidServices", len(invalidServices), "desiredWeight", desiredWeight)
	return desiredEndpoints, invalidServices, nil
}

// isValidTrafficManagerEndpoint returns error if the service cannot be added as a TrafficManager endpoint.
func isValidTrafficManagerEndpoint(export *fleetnetv1alpha1.InternalServiceExport) error {
	if export.Spec.Type != corev1.ServiceTypeLoadBalancer {
		return fmt.Errorf("unsupported service type %q", export.Spec.Type)
	}
	if export.Spec.IsInternalLoadBalancer {
		return fmt.Errorf("internal load balancer is not supported")
	}
	if !export.Spec.IsDNSLabelConfigured {
		return fmt.Errorf("DNS label is not configured to the public IP")
	}
	return nil
}

func generateAzureTrafficManagerEndpoint(backend *fleetnetv1beta1.TrafficManagerBackend, service *fleetnetv1alpha1.InternalServiceExport) armtrafficmanager.Endpoint {
	endpointName := fmt.Sprintf(AzureResourceEndpointNameFormat, generateAzureTrafficManagerEndpointNamePrefixFunc(backend), backend.Spec.Backend.Name, service.Spec.ServiceReference.ClusterID)
	return armtrafficmanager.Endpoint{
		Name: &endpointName,
		Type: ptr.To(string("Microsoft.Network/trafficManagerProfiles/" + armtrafficmanager.EndpointTypeAzureEndpoints)),
		Properties: &armtrafficmanager.EndpointProperties{
			TargetResourceID: service.Spec.PublicIPResourceID,
			EndpointStatus:   ptr.To(armtrafficmanager.EndpointStatusEnabled),
		},
	}
}

func buildAcceptedEndpointStatus(endpoint *armtrafficmanager.Endpoint, cluster fleetnetv1beta1.ClusterStatus) fleetnetv1beta1.TrafficManagerEndpointStatus {
	return fleetnetv1beta1.TrafficManagerEndpointStatus{
		Name:   strings.ToLower(*endpoint.Name), // name is case-insensitive
		Target: endpoint.Properties.Target,
		Weight: endpoint.Properties.Weight,
		From: &fleetnetv1beta1.FromCluster{
			ClusterStatus: cluster,
		},
	}
}

// equalAzureTrafficManagerEndpoint compares only few fields of the current and desired Azure Traffic Manager endpoints
// by ignoring others.
// The desired endpoint is built by the controllers and all the required fields should not be nil.
func equalAzureTrafficManagerEndpoint(current, desired armtrafficmanager.Endpoint) bool {
	if current.Type == nil || *current.Type != *desired.Type {
		return false
	}
	if current.Properties == nil || current.Properties.TargetResourceID == nil || current.Properties.Weight == nil || current.Properties.EndpointStatus == nil {
		return false
	}
	return strings.EqualFold(*current.Properties.TargetResourceID, *desired.Properties.TargetResourceID) &&
		*current.Properties.Weight == *desired.Properties.Weight &&
		*current.Properties.EndpointStatus == *desired.Properties.EndpointStatus
}

// updateTrafficManagerEndpointsAndUpdateStatusIfUnknown updates the Azure Traffic Manager endpoints.
// Returns the accepted endpoints and a list of bad endpoints error when it fails to create/update endpoint or not because of bad request.
func (r *Reconciler) updateTrafficManagerEndpointsAndUpdateStatusIfUnknown(ctx context.Context, backend *fleetnetv1beta1.TrafficManagerBackend, profile *armtrafficmanager.Profile, desiredEndpoints map[string]desiredEndpoint) ([]fleetnetv1beta1.TrafficManagerEndpointStatus, []error, error) {
	backendKObj := klog.KObj(backend)
	acceptedEndpoints := make([]fleetnetv1beta1.TrafficManagerEndpointStatus, 0, len(desiredEndpoints))
	for _, endpoint := range profile.Properties.Endpoints {
		if endpoint.Name == nil {
			err := controller.NewUnexpectedBehaviorError(errors.New("azure Traffic Manager endpoint name is nil"))
			klog.ErrorS(err, "Invalid Traffic Manager endpoint", "atmEndpoint", endpoint)
			continue
		}

		endpointName := strings.ToLower(*endpoint.Name) // resource name are case-insensitive
		if !isEndpointOwnedByBackend(backend, endpointName) {
			continue // skipping the endpoint which is not owned by this backend
		}

		desired, ok := desiredEndpoints[endpointName]
		if !ok {
			klog.V(2).InfoS("Deleting the Azure Traffic Manager endpoint", "trafficManagerBackend", backendKObj, "atmProfile", profile.Name, "atmEndpoint", endpointName)
			if _, deleteErr := r.EndpointsClient.Delete(ctx, r.ResourceGroupName, *profile.Name, armtrafficmanager.EndpointTypeAzureEndpoints, *endpoint.Name, nil); deleteErr != nil {
				if azureerrors.IsNotFound(deleteErr) {
					klog.V(2).InfoS("Ignoring NotFound Azure Traffic Manager endpoint", "trafficManagerBackend", backendKObj, "atmProfile", profile.Name, "atmEndpoint", endpointName)
					continue
				}
				klog.ErrorS(deleteErr, "Failed to delete the Azure Traffic Manager endpoint", "trafficManagerBackend", backendKObj, "atmProfile", profile.Name, "atmEndpoint", endpointName)
				setUnknownCondition(backend, fmt.Sprintf("Failed to cleanup the existing %q for %q: %v", endpointName, *profile.Name, deleteErr))
				if err := r.updateTrafficManagerBackendStatus(ctx, backend); err != nil {
					return nil, nil, err
				}
				return nil, nil, deleteErr
			}
			klog.V(2).InfoS("Deleted the Azure Traffic Manager endpoint", "trafficManagerBackend", backendKObj, "atmProfile", profile.Name, "atmEndpoint", endpointName)
			continue
		}
		if equalAzureTrafficManagerEndpoint(*endpoint, desired.Endpoint) {
			klog.V(2).InfoS("Skipping updating the existing Traffic Manager endpoint", "trafficManagerBackend", backendKObj, "atmProfile", profile.Name, "atmEndpoint", endpointName)
			delete(desiredEndpoints, endpointName) // no need to update the existing endpoint
			acceptedEndpoints = append(acceptedEndpoints, buildAcceptedEndpointStatus(endpoint, desired.Cluster))
			continue
		} // no need to update the endpoint if it's the same
	}
	badEndpointsError := make([]error, 0, len(desiredEndpoints))
	// The remaining endpoints in the desiredEndpoints should be created or updated.
	for _, endpoint := range desiredEndpoints {
		klog.V(2).InfoS("Creating new Traffic Manager endpoint", "trafficManagerBackend", backendKObj, "atmProfile", profile.Name, "atmEndpoint", endpoint)
		var responseError *azcore.ResponseError
		endpointName := *endpoint.Endpoint.Name
		res, updateErr := r.EndpointsClient.CreateOrUpdate(ctx, r.ResourceGroupName, *profile.Name, armtrafficmanager.EndpointTypeAzureEndpoints, endpointName, endpoint.Endpoint, nil)
		if updateErr != nil {
			if !errors.As(updateErr, &responseError) {
				klog.ErrorS(updateErr, "Failed to send the createOrUpdate request", "trafficManagerBackend", backendKObj, "atmProfile", *profile.Name, "atmEndpoint", endpointName)
				return nil, nil, updateErr
			}
			klog.ErrorS(updateErr, "Failed to create or update the Traffic Manager endpoint", "trafficManagerBackend", backendKObj, "atmProfile", *profile.Name, "atmEndpoint", endpointName)
			if azureerrors.IsClientError(updateErr) && !azureerrors.IsThrottled(updateErr) {
				// When the failure is caused by the client error, will continue to process others.
				badEndpointsError = append(badEndpointsError, updateErr)
				continue
			}
			setUnknownCondition(backend, fmt.Sprintf("Failed to create or update %q for %q: %v", *endpoint.Endpoint.Name, *profile.Name, updateErr))
			if err := r.updateTrafficManagerBackendStatus(ctx, backend); err != nil {
				return nil, nil, err
			}
			return nil, nil, updateErr
		}
		klog.V(2).InfoS("Created or updated Traffic Manager endpoint", "trafficManagerBackend", backendKObj, "atmProfile", profile.Name, "atmEndpoint", endpointName)
		acceptedEndpoints = append(acceptedEndpoints, buildAcceptedEndpointStatus(&res.Endpoint, endpoint.Cluster))
	}
	klog.V(2).InfoS("Successfully updated the Traffic Manager endpoints", "trafficManagerBackend", backendKObj, "atmProfile", profile.Name, "numberOfAcceptedEndpoints", len(acceptedEndpoints), "numberOfBadEndpoints", len(badEndpointsError))
	return acceptedEndpoints, badEndpointsError, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, disableInternalServiceExportIndexer bool) error {
	// set up an index for efficient trafficManagerBackend lookup
	profileIndexerFunc := func(o client.Object) []string {
		tmb, ok := o.(*fleetnetv1beta1.TrafficManagerBackend)
		if !ok {
			return []string{}
		}
		return []string{tmb.Spec.Profile.Name}
	}
	if err := mgr.GetFieldIndexer().IndexField(ctx, &fleetnetv1beta1.TrafficManagerBackend{}, trafficManagerBackendProfileFieldKey, profileIndexerFunc); err != nil {
		klog.ErrorS(err, "Failed to setup profile field indexer for TrafficManagerBackend")
		return err
	}

	backendIndexerFunc := func(o client.Object) []string {
		tmb, ok := o.(*fleetnetv1beta1.TrafficManagerBackend)
		if !ok {
			return []string{}
		}
		return []string{tmb.Spec.Backend.Name}
	}
	if err := mgr.GetFieldIndexer().IndexField(ctx, &fleetnetv1beta1.TrafficManagerBackend{}, trafficManagerBackendBackendFieldKey, backendIndexerFunc); err != nil {
		klog.ErrorS(err, "Failed to setup backend field indexer for TrafficManagerBackend")
		return err
	}

	// add index to quickly query internalServiceExport list by service
	if !disableInternalServiceExportIndexer {
		internalServiceExportIndexerFunc := func(o client.Object) []string {
			name, ok := o.(*fleetnetv1alpha1.InternalServiceExport)
			if !ok {
				return []string{}
			}
			return []string{name.Spec.ServiceReference.NamespacedName}
		}
		if err := mgr.GetFieldIndexer().IndexField(ctx, &fleetnetv1alpha1.InternalServiceExport{}, exportedServiceFieldNamespacedName, internalServiceExportIndexerFunc); err != nil {
			klog.ErrorS(err, "Failed to create index", "field", exportedServiceFieldNamespacedName)
			return err
		}
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetnetv1beta1.TrafficManagerBackend{}).
		Watches(
			&fleetnetv1beta1.TrafficManagerProfile{},
			handler.EnqueueRequestsFromMapFunc(r.trafficManagerProfileEventHandler()),
		).
		Watches(
			&fleetnetv1alpha1.ServiceImport{},
			handler.EnqueueRequestsFromMapFunc(r.serviceImportEventHandler()),
		).
		Watches(
			&fleetnetv1alpha1.InternalServiceExport{},
			handler.EnqueueRequestsFromMapFunc(r.internalServiceExportEventHandler()),
		).
		Complete(r)
}

func (r *Reconciler) trafficManagerProfileEventHandler() handler.MapFunc {
	return func(ctx context.Context, object client.Object) []reconcile.Request {
		trafficManagerBackendList := &fleetnetv1beta1.TrafficManagerBackendList{}
		fieldMatcher := client.MatchingFields{
			trafficManagerBackendProfileFieldKey: object.GetName(),
		}
		// For now, we only support the backend and profile in the same namespace.
		if err := r.Client.List(ctx, trafficManagerBackendList, client.InNamespace(object.GetNamespace()), fieldMatcher); err != nil {
			klog.ErrorS(err,
				"Failed to list trafficManagerBackends for the profile",
				"trafficManagerProfile", klog.KObj(object))
			return []reconcile.Request{}
		}

		res := make([]reconcile.Request, 0, len(trafficManagerBackendList.Items))
		for _, backend := range trafficManagerBackendList.Items {
			res = append(res, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: backend.Namespace,
					Name:      backend.Name,
				},
			})
		}
		return res
	}
}

func (r *Reconciler) serviceImportEventHandler() handler.MapFunc {
	return func(ctx context.Context, object client.Object) []reconcile.Request {
		return r.enqueueTrafficManagerBackendByServiceImport(ctx, object)
	}
}

func (r *Reconciler) enqueueTrafficManagerBackendByServiceImport(ctx context.Context, object client.Object) []reconcile.Request {
	trafficManagerBackendList := &fleetnetv1beta1.TrafficManagerBackendList{}
	fieldMatcher := client.MatchingFields{
		trafficManagerBackendBackendFieldKey: object.GetName(),
	}
	// ServiceImport and TrafficManagerBackend should be in the same namespace.
	if err := r.Client.List(ctx, trafficManagerBackendList, client.InNamespace(object.GetNamespace()), fieldMatcher); err != nil {
		klog.ErrorS(err,
			"Failed to list trafficManagerBackends for the serviceImport",
			"serviceImport", klog.KObj(object))
		return []reconcile.Request{}
	}

	res := make([]reconcile.Request, 0, len(trafficManagerBackendList.Items))
	for _, backend := range trafficManagerBackendList.Items {
		res = append(res, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: backend.Namespace,
				Name:      backend.Name,
			},
		})
	}
	return res
}

func (r *Reconciler) internalServiceExportEventHandler() handler.MapFunc {
	return func(ctx context.Context, object client.Object) []reconcile.Request {
		internalServiceExport, ok := object.(*fleetnetv1alpha1.InternalServiceExport)
		if !ok {
			return []reconcile.Request{}
		}

		serviceImport := &fleetnetv1alpha1.ServiceImport{}
		serviceImportName := types.NamespacedName{Namespace: internalServiceExport.Spec.ServiceReference.Namespace, Name: internalServiceExport.Spec.ServiceReference.Name}
		serviceImportKRef := klog.KRef(serviceImportName.Namespace, serviceImportName.Name)
		if err := r.Client.Get(ctx, serviceImportName, serviceImport); err != nil {
			klog.ErrorS(err, "Failed to get serviceImport", "serviceImport", serviceImportKRef, "internalServiceExport", klog.KObj(internalServiceExport))
			return []reconcile.Request{}
		}
		for _, cs := range serviceImport.Status.Clusters {
			// When the cluster exposes the service, first we will check whether the cluster can be exposed or not.
			// For example, whether the service spec conflicts with other existing services.
			// If the cluster is not in the serviceImport status, there are two possibilities:
			// * the controller is still in the processing of this cluster.
			// * the cluster cannot be exposed because of the conflicted spec, which will be clearly indicated in the
			// serviceExport status.
			// For the first case, when the processing is finished, serviceImport will be updated so that this controller
			// will be triggered again.
			if cs.Cluster == internalServiceExport.Spec.ServiceReference.ClusterID {
				return r.enqueueTrafficManagerBackendByServiceImport(ctx, serviceImport)
			}
		}
		return []reconcile.Request{}
	}
}
