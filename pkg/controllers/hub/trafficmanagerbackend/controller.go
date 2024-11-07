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
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/trafficmanager/armtrafficmanager"
	"golang.org/x/sync/errgroup"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/pkg/utils/controller"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/azureerrors"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/controllers/hub/trafficmanagerprofile"
)

const (
	trafficManagerBackendProfileFieldKey = ".spec.profile.name"
	trafficManagerBackendBackendFieldKey = ".spec.backend.name"

	// AzureResourceEndpointNamePrefix is the prefix format of the Azure Traffic Manager Endpoint created by the fleet controller.
	// The naming convention of a Traffic Manager Endpoint is fleet-{TrafficManagerBackendUUID}#.
	// Using the UUID of the backend here in case to support cross namespace TrafficManagerBackend in the future.
	AzureResourceEndpointNamePrefix = "fleet-%s#"

	// AzureResourceEndpointNameFormat is the name format of the Azure Traffic Manager Endpoint created by the fleet controller.
	// The naming convention of a Traffic Manager Endpoint is fleet-{TrafficManagerBackendUUID}#{ServiceImportName}#{ClusterName}.
	// All the object name length should be restricted to <= 63 characters.
	// The endpoint name must contain no more than 260 characters, excluding the following characters "< > * % $ : \ ? + /".
	AzureResourceEndpointNameFormat = AzureResourceEndpointNamePrefix + "%s#%s"
)

var (
	// create the func as a variable so that the integration test can use a customized function.
	generateAzureTrafficManagerProfileNameFunc = func(profile *fleetnetv1alpha1.TrafficManagerProfile) string {
		return trafficmanagerprofile.GenerateAzureTrafficManagerProfileName(profile)
	}
	generateAzureTrafficManagerEndpointNamePrefixFunc = func(backend *fleetnetv1alpha1.TrafficManagerBackend) string {
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

	backend := &fleetnetv1alpha1.TrafficManagerBackend{}
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
	return r.handleUpdate(ctx, backend)
}

func (r *Reconciler) handleDelete(ctx context.Context, backend *fleetnetv1alpha1.TrafficManagerBackend) (ctrl.Result, error) {
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

func (r *Reconciler) deleteAzureTrafficManagerEndpoints(ctx context.Context, backend *fleetnetv1alpha1.TrafficManagerBackend) error {
	backendKObj := klog.KObj(backend)
	profile := &fleetnetv1alpha1.TrafficManagerProfile{}
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

func (r *Reconciler) cleanupEndpoints(ctx context.Context, backend *fleetnetv1alpha1.TrafficManagerBackend, atmProfile *armtrafficmanager.Profile) error {
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
			klog.ErrorS(err, "Invalid Traffic Manager endpoint", "azureEndpoint", endpoint)
			continue
		}
		// Traffic manager endpoint name is case-insensitive.
		if !isEndpointOwnedByBackend(backend, *endpoint.Name) {
			continue // skipping deleting the endpoints which are not created by this backend
		}
		errs.Go(func() error {
			if _, err := r.EndpointsClient.Delete(cctx, r.ResourceGroupName, atmProfileName, armtrafficmanager.EndpointTypeAzureEndpoints, *endpoint.Name, nil); err != nil {
				if azureerrors.IsNotFound(err) {
					klog.V(2).InfoS("Ignoring NotFound Azure Traffic Manager endpoint", "trafficManagerBackend", backendKObj, "atmProfileName", atmProfileName, "azureEndpointName", *endpoint.Name)
					return nil
				}
				klog.ErrorS(err, "Failed to delete the endpoint", "trafficManagerBackend", backendKObj, "atmProfileName", atmProfileName, "azureEndpointName", *endpoint.Name)
				return err
			}
			klog.V(2).InfoS("Deleted Azure Traffic Manager endpoint", "trafficManagerBackend", backendKObj, "atmProfileName", atmProfileName, "azureEndpointName", *endpoint.Name)
			return nil
		})
	}
	return errs.Wait()
}

func isEndpointOwnedByBackend(backend *fleetnetv1alpha1.TrafficManagerBackend, endpoint string) bool {
	return strings.HasPrefix(strings.ToLower(endpoint), generateAzureTrafficManagerEndpointNamePrefixFunc(backend))
}

func (r *Reconciler) handleUpdate(ctx context.Context, backend *fleetnetv1alpha1.TrafficManagerBackend) (ctrl.Result, error) {
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
	return ctrl.Result{}, nil
}

// validateTrafficManagerProfile returns not nil profile when the profile is valid.
func (r *Reconciler) validateTrafficManagerProfile(ctx context.Context, backend *fleetnetv1alpha1.TrafficManagerBackend) (*fleetnetv1alpha1.TrafficManagerProfile, error) {
	backendKObj := klog.KObj(backend)
	var cond metav1.Condition
	profile := &fleetnetv1alpha1.TrafficManagerProfile{}
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
	programmedCondition := meta.FindStatusCondition(profile.Status.Conditions, string(fleetnetv1alpha1.TrafficManagerProfileConditionProgrammed))
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
func (r *Reconciler) validateAzureTrafficManagerProfile(ctx context.Context, backend *fleetnetv1alpha1.TrafficManagerBackend, profile *fleetnetv1alpha1.TrafficManagerProfile) (*armtrafficmanager.Profile, error) {
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
func (r *Reconciler) validateServiceImportAndCleanupEndpointsIfInvalid(ctx context.Context, backend *fleetnetv1alpha1.TrafficManagerBackend, azureProfile *armtrafficmanager.Profile) (*fleetnetv1alpha1.ServiceImport, error) {
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
				Type:               string(fleetnetv1alpha1.TrafficManagerBackendConditionAccepted),
				Status:             metav1.ConditionFalse,
				ObservedGeneration: backend.Generation,
				Reason:             string(fleetnetv1alpha1.TrafficManagerBackendReasonInvalid),
				Message:            fmt.Sprintf("ServiceImport %q is not found", backend.Spec.Backend.Name),
			}
			meta.SetStatusCondition(&backend.Status.Conditions, cond)
			backend.Status.Endpoints = []fleetnetv1alpha1.TrafficManagerEndpointStatus{} // none of the endpoints are accepted by the TrafficManager
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

func setFalseCondition(backend *fleetnetv1alpha1.TrafficManagerBackend, acceptedEndpoints []fleetnetv1alpha1.TrafficManagerEndpointStatus, message string) {
	cond := metav1.Condition{
		Type:               string(fleetnetv1alpha1.TrafficManagerBackendConditionAccepted),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: backend.Generation,
		Reason:             string(fleetnetv1alpha1.TrafficManagerBackendReasonInvalid),
		Message:            message,
	}
	if len(acceptedEndpoints) == 0 {
		backend.Status.Endpoints = []fleetnetv1alpha1.TrafficManagerEndpointStatus{}
	} else {
		backend.Status.Endpoints = acceptedEndpoints
	}
	meta.SetStatusCondition(&backend.Status.Conditions, cond)
}

func setUnknownCondition(backend *fleetnetv1alpha1.TrafficManagerBackend, message string) {
	cond := metav1.Condition{
		Type:               string(fleetnetv1alpha1.TrafficManagerBackendConditionAccepted),
		Status:             metav1.ConditionUnknown,
		ObservedGeneration: backend.Generation,
		Reason:             string(fleetnetv1alpha1.TrafficManagerBackendReasonPending),
		Message:            message,
	}
	backend.Status.Endpoints = []fleetnetv1alpha1.TrafficManagerEndpointStatus{}
	meta.SetStatusCondition(&backend.Status.Conditions, cond)
}

func (r *Reconciler) updateTrafficManagerBackendStatus(ctx context.Context, backend *fleetnetv1alpha1.TrafficManagerBackend) error {
	backendKObj := klog.KObj(backend)
	if err := r.Client.Status().Update(ctx, backend); err != nil {
		klog.ErrorS(err, "Failed to update trafficManagerBackend status", "trafficManagerBackend", backendKObj)
		return controller.NewUpdateIgnoreConflictError(err)
	}
	klog.V(2).InfoS("Updated trafficManagerBackend status", "trafficManagerBackend", backendKObj, "status", backend.Status)
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	// set up an index for efficient trafficManagerBackend lookup
	profileIndexerFunc := func(o client.Object) []string {
		tmb, ok := o.(*fleetnetv1alpha1.TrafficManagerBackend)
		if !ok {
			return []string{}
		}
		return []string{tmb.Spec.Profile.Name}
	}
	if err := mgr.GetFieldIndexer().IndexField(ctx, &fleetnetv1alpha1.TrafficManagerBackend{}, trafficManagerBackendProfileFieldKey, profileIndexerFunc); err != nil {
		klog.ErrorS(err, "Failed to setup profile field indexer for TrafficManagerBackend")
		return err
	}

	backendIndexerFunc := func(o client.Object) []string {
		tmb, ok := o.(*fleetnetv1alpha1.TrafficManagerBackend)
		if !ok {
			return []string{}
		}
		return []string{tmb.Spec.Backend.Name}
	}
	if err := mgr.GetFieldIndexer().IndexField(ctx, &fleetnetv1alpha1.TrafficManagerBackend{}, trafficManagerBackendBackendFieldKey, backendIndexerFunc); err != nil {
		klog.ErrorS(err, "Failed to setup backend field indexer for TrafficManagerBackend")
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetnetv1alpha1.TrafficManagerBackend{}).
		Watches(
			&fleetnetv1alpha1.TrafficManagerProfile{},
			handler.EnqueueRequestsFromMapFunc(r.trafficManagerProfileEventHandler()),
		).
		Watches(
			&fleetnetv1alpha1.ServiceImport{},
			handler.EnqueueRequestsFromMapFunc(r.serviceImportEventHandler()),
		).
		Complete(r)
}

func (r *Reconciler) trafficManagerProfileEventHandler() handler.MapFunc {
	return func(ctx context.Context, object client.Object) []reconcile.Request {
		trafficManagerBackendList := &fleetnetv1alpha1.TrafficManagerBackendList{}
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
		trafficManagerBackendList := &fleetnetv1alpha1.TrafficManagerBackendList{}
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
}
