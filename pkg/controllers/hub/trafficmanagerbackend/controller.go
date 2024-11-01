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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

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

func (r *Reconciler) handleUpdate(_ context.Context, _ *fleetnetv1alpha1.TrafficManagerBackend) (ctrl.Result, error) {
	return ctrl.Result{}, nil
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
