/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package trafficmanagerprofile features the TrafficManagerProfile controller to reconcile TrafficManagerProfile CRs.
package trafficmanagerprofile

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/trafficmanager/armtrafficmanager"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"go.goms.io/fleet/pkg/utils/controller"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/azureerrors"
	"go.goms.io/fleet-networking/pkg/common/defaulter"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
)

const (
	// DNSRelativeNameFormat consists of "Profile-Namespace" and "Profile-Name".
	DNSRelativeNameFormat = "%s-%s"
	// AzureResourceProfileNameFormat is the name format of the Azure Traffic Manager Profile created by the fleet controller.
	AzureResourceProfileNameFormat = "fleet-%s"
)

var (
	// create the func as a variable so that the integration test can use a customized function.
	generateAzureTrafficManagerProfileNameFunc = func(profile *fleetnetv1alpha1.TrafficManagerProfile) string {
		return GenerateAzureTrafficManagerProfileName(profile)
	}
)

// GenerateAzureTrafficManagerProfileName generates the Azure Traffic Manager profile name based on the profile.
func GenerateAzureTrafficManagerProfileName(profile *fleetnetv1alpha1.TrafficManagerProfile) string {
	return fmt.Sprintf(AzureResourceProfileNameFormat, profile.UID)
}

// Reconciler reconciles a TrafficManagerProfile object.
type Reconciler struct {
	client.Client

	ProfilesClient    *armtrafficmanager.ProfilesClient
	ResourceGroupName string // default resource group name to create azure traffic manager profiles
}

//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=trafficmanagerprofiles,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=trafficmanagerprofiles/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=trafficmanagerprofiles/finalizers,verbs=get;update
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile triggers a single reconcile round.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	name := req.NamespacedName
	profileKRef := klog.KRef(name.Namespace, name.Name)

	startTime := time.Now()
	klog.V(2).InfoS("Reconciliation starts", "trafficManagerProfile", profileKRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Reconciliation ends", "trafficManagerProfile", profileKRef, "latency", latency)
	}()

	profile := &fleetnetv1alpha1.TrafficManagerProfile{}
	if err := r.Client.Get(ctx, name, profile); err != nil {
		if apierrors.IsNotFound(err) {
			klog.V(4).InfoS("Ignoring NotFound trafficManagerProfile", "trafficManagerProfile", profileKRef)
			return ctrl.Result{}, nil
		}
		klog.ErrorS(err, "Failed to get trafficManagerProfile", "trafficManagerProfile", profileKRef)
		return ctrl.Result{}, controller.NewAPIServerError(true, err)
	}

	if !profile.ObjectMeta.DeletionTimestamp.IsZero() {
		// TODO: handle the deletion when backends are still attached to the profile
		return r.handleDelete(ctx, profile)
	}

	// register finalizer
	if !controllerutil.ContainsFinalizer(profile, objectmeta.TrafficManagerProfileFinalizer) {
		controllerutil.AddFinalizer(profile, objectmeta.TrafficManagerProfileFinalizer)
		if err := r.Update(ctx, profile); err != nil {
			klog.ErrorS(err, "Failed to add finalizer to trafficManagerProfile", "trafficManagerProfile", profileKRef)
			return ctrl.Result{}, controller.NewUpdateIgnoreConflictError(err)
		}
	}

	// TODO: replace the following with defaulter wehbook
	defaulter.SetDefaultsTrafficManagerProfile(profile)
	return r.handleUpdate(ctx, profile)
}

func (r *Reconciler) handleDelete(ctx context.Context, profile *fleetnetv1alpha1.TrafficManagerProfile) (ctrl.Result, error) {
	profileKObj := klog.KObj(profile)
	// The profile is being deleted
	if !controllerutil.ContainsFinalizer(profile, objectmeta.TrafficManagerProfileFinalizer) {
		klog.V(4).InfoS("TrafficManagerProfile is being deleted", "trafficManagerProfile", profileKObj)
		return ctrl.Result{}, nil
	}

	atmProfileName := generateAzureTrafficManagerProfileNameFunc(profile)
	klog.V(2).InfoS("Deleting Azure Traffic Manager profile", "trafficManagerProfile", profileKObj, "atmProfileName", atmProfileName)
	if _, err := r.ProfilesClient.Delete(ctx, r.ResourceGroupName, atmProfileName, nil); err != nil {
		if !azureerrors.IsNotFound(err) {
			klog.ErrorS(err, "Failed to delete Azure Traffic Manager profile", "trafficManagerProfile", profileKObj, "atmProfileName", atmProfileName)
			return ctrl.Result{}, err
		}
	}
	klog.V(2).InfoS("Deleted Azure Traffic Manager profile", "trafficManagerProfile", profileKObj, "atmProfileName", atmProfileName)

	controllerutil.RemoveFinalizer(profile, objectmeta.TrafficManagerProfileFinalizer)
	if err := r.Client.Update(ctx, profile); err != nil {
		klog.ErrorS(err, "Failed to remove trafficManagerProfile finalizer", "trafficManagerProfile", profileKObj)
		return ctrl.Result{}, controller.NewUpdateIgnoreConflictError(err)
	}
	klog.V(2).InfoS("Removed trafficManagerProfile finalizer", "trafficManagerProfile", profileKObj)
	return ctrl.Result{}, nil
}

func (r *Reconciler) handleUpdate(ctx context.Context, profile *fleetnetv1alpha1.TrafficManagerProfile) (ctrl.Result, error) {
	profileKObj := klog.KObj(profile)
	atmProfileName := generateAzureTrafficManagerProfileNameFunc(profile)
	var responseError *azcore.ResponseError
	getRes, getErr := r.ProfilesClient.Get(ctx, r.ResourceGroupName, atmProfileName, nil)
	if getErr != nil {
		if !azureerrors.IsNotFound(getErr) {
			klog.ErrorS(getErr, "Failed to get the profile", "trafficManagerProfile", profileKObj, "atmProfileName", atmProfileName)
			return ctrl.Result{}, getErr
		}
		klog.V(2).InfoS("Azure Traffic Manager profile does not exist", "trafficManagerProfile", profileKObj, "atmProfileName", atmProfileName)
	} else {
		existingSpec := convertToTrafficManagerProfileSpec(&getRes.Profile)
		if equality.Semantic.DeepEqual(existingSpec, profile.Spec) {
			// skip creating or updating the profile
			klog.V(2).InfoS("No profile update needed", "trafficManagerProfile", profileKObj, "atmProfileName", atmProfileName)
			return r.updateProfileStatus(ctx, profile, getRes.Profile, nil)
		}
	}

	res, updateErr := r.ProfilesClient.CreateOrUpdate(ctx, r.ResourceGroupName, atmProfileName, generateAzureTrafficManagerProfile(profile), nil)
	if updateErr != nil {
		if !errors.As(updateErr, &responseError) {
			klog.ErrorS(updateErr, "Failed to send the createOrUpdate request", "trafficManagerProfile", profileKObj, "atmProfileName", atmProfileName)
			return ctrl.Result{}, updateErr
		}
		klog.ErrorS(updateErr, "Failed to create or update a profile", "trafficManagerProfile", profileKObj,
			"atmProfileName", atmProfileName,
			"errorCode", responseError.ErrorCode, "statusCode", responseError.StatusCode)
	}
	klog.V(2).InfoS("Created or updated Azure Traffic Manager Profile", "trafficManagerProfile", profileKObj, "atmProfileName", atmProfileName)
	return r.updateProfileStatus(ctx, profile, res.Profile, updateErr)
}

func convertToTrafficManagerProfileSpec(profile *armtrafficmanager.Profile) fleetnetv1alpha1.TrafficManagerProfileSpec {
	if profile.Properties != nil && profile.Properties.MonitorConfig != nil {
		var protocol fleetnetv1alpha1.TrafficManagerMonitorProtocol
		if profile.Properties.MonitorConfig.Protocol != nil {
			protocol = fleetnetv1alpha1.TrafficManagerMonitorProtocol(*profile.Properties.MonitorConfig.Protocol)
		}
		return fleetnetv1alpha1.TrafficManagerProfileSpec{
			MonitorConfig: &fleetnetv1alpha1.MonitorConfig{
				IntervalInSeconds:         profile.Properties.MonitorConfig.IntervalInSeconds,
				Path:                      profile.Properties.MonitorConfig.Path,
				Port:                      profile.Properties.MonitorConfig.Port,
				Protocol:                  &protocol,
				TimeoutInSeconds:          profile.Properties.MonitorConfig.TimeoutInSeconds,
				ToleratedNumberOfFailures: profile.Properties.MonitorConfig.ToleratedNumberOfFailures,
			},
		}
	}
	return fleetnetv1alpha1.TrafficManagerProfileSpec{}
}

func (r *Reconciler) updateProfileStatus(ctx context.Context, profile *fleetnetv1alpha1.TrafficManagerProfile, atmProfile armtrafficmanager.Profile, updateErr error) (ctrl.Result, error) {
	profileKObj := klog.KObj(profile)
	if updateErr == nil {
		// atmProfile.Properties.DNSConfig.Fqdn should not be nil
		if atmProfile.Properties != nil && atmProfile.Properties.DNSConfig != nil {
			profile.Status.DNSName = atmProfile.Properties.DNSConfig.Fqdn
		} else {
			err := fmt.Errorf("got nil DNSConfig for Azure Traffic Manager profile")
			klog.ErrorS(controller.NewUnexpectedBehaviorError(err), "Unexpected value returned by the Azure Traffic Manager", "trafficManagerProfile", profileKObj, "atmProfileName", atmProfile.Name)
			profile.Status.DNSName = nil // reset the DNS name
		}
	} else {
		profile.Status.DNSName = nil // reset the DNS name
	}

	cond := metav1.Condition{
		Type:               string(fleetnetv1alpha1.TrafficManagerProfileConditionProgrammed),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: profile.Generation,
		Reason:             string(fleetnetv1alpha1.TrafficManagerProfileReasonProgrammed),
		Message:            "Successfully configured the Azure Traffic Manager profile",
	}
	if azureerrors.IsConflict(updateErr) {
		cond = metav1.Condition{
			Type:               string(fleetnetv1alpha1.TrafficManagerProfileConditionProgrammed),
			Status:             metav1.ConditionFalse,
			ObservedGeneration: profile.Generation,
			Reason:             string(fleetnetv1alpha1.TrafficManagerProfileReasonDNSNameNotAvailable),
			Message:            "Domain name is not available. Please choose a different profile name or namespace",
		}
	} else if azureerrors.IsClientError(updateErr) && !azureerrors.IsThrottled(updateErr) {
		cond = metav1.Condition{
			Type:               string(fleetnetv1alpha1.TrafficManagerProfileConditionProgrammed),
			Status:             metav1.ConditionFalse,
			ObservedGeneration: profile.Generation,
			Reason:             string(fleetnetv1alpha1.TrafficManagerProfileReasonInvalid),
			Message:            fmt.Sprintf("Invalid profile: %v", updateErr),
		}
	} else if updateErr != nil {
		cond = metav1.Condition{
			Type:               string(fleetnetv1alpha1.TrafficManagerProfileConditionProgrammed),
			Status:             metav1.ConditionUnknown,
			ObservedGeneration: profile.Generation,
			Reason:             string(fleetnetv1alpha1.TrafficManagerProfileReasonPending),
			Message:            fmt.Sprintf("Failed to configure profile and retyring: %v", updateErr),
		}
	}
	meta.SetStatusCondition(&profile.Status.Conditions, cond)
	if err := r.Client.Status().Update(ctx, profile); err != nil {
		klog.ErrorS(err, "Failed to update trafficManagerProfile status", "trafficManagerProfile", profileKObj)
		return ctrl.Result{}, controller.NewUpdateIgnoreConflictError(err)
	}
	klog.V(2).InfoS("Updated the trafficProfile status", "trafficManagerProfile", profileKObj, "status", profile.Status)
	return ctrl.Result{}, updateErr
}

func generateAzureTrafficManagerProfile(profile *fleetnetv1alpha1.TrafficManagerProfile) armtrafficmanager.Profile {
	mc := profile.Spec.MonitorConfig
	namespacedName := types.NamespacedName{Name: profile.Name, Namespace: profile.Namespace}
	return armtrafficmanager.Profile{
		Location: ptr.To("global"),
		Properties: &armtrafficmanager.ProfileProperties{
			DNSConfig: &armtrafficmanager.DNSConfig{
				RelativeName: ptr.To(fmt.Sprintf(DNSRelativeNameFormat, profile.Namespace, profile.Name)),
			},
			MonitorConfig: &armtrafficmanager.MonitorConfig{
				IntervalInSeconds:         mc.IntervalInSeconds,
				Path:                      mc.Path,
				Port:                      mc.Port,
				Protocol:                  ptr.To(armtrafficmanager.MonitorProtocol(*mc.Protocol)),
				TimeoutInSeconds:          mc.TimeoutInSeconds,
				ToleratedNumberOfFailures: mc.ToleratedNumberOfFailures,
			},
			ProfileStatus: ptr.To(armtrafficmanager.ProfileStatusEnabled),
			// By default, the routing method is set to Weighted.
			TrafficRoutingMethod: ptr.To(armtrafficmanager.TrafficRoutingMethodWeighted),
		},
		Tags: map[string]*string{
			objectmeta.AzureTrafficManagerProfileTagKey: ptr.To(namespacedName.String()),
		},
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetnetv1alpha1.TrafficManagerProfile{}).
		Complete(r)
}
