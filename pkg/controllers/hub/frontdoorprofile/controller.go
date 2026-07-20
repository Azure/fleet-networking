/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package frontdoorprofile features the FrontDoorProfile controller (POC) that reconciles
// FrontDoorProfile CRs to Azure Front Door profiles + their default AFD endpoint.
//
// Scope note (POC, see breadcrumb 2026-07-18 and Addendum 2 of 2026-07-20-1108):
//   - Happy-path reconcile only. Full error-classification, metrics, and conflict handling
//     are deferred past POC.
//   - No FrontDoorBackend controller exists yet, so this reconciler does not program
//     originGroups, origins, routes, or securityPolicies. A programmed profile is reachable
//     at its *.azurefd.net endpoint but has no backends. Backend reconciliation is Phase 4.
//   - The Sku enum accepts both Standard_AzureFrontDoor and Premium_AzureFrontDoor. SFI-NS253
//     workloads must use Premium (Private Link is Premium-only); a Phase-4 CRD tightening
//     removes Standard from the enum. See docs/first-party/002-afd-implementation-plan.md §9.
//   - Additional Spec fields (WAFPolicy, ComplianceMode, HealthProbe,
//     OriginResponseTimeoutSeconds) are documented in the proposals but not yet
//     implemented; they land alongside the FrontDoorBackend work in Phase 4.
//
// Identity note (SFI-NS253):
//   - Proposal 001 §7 requires a separate Azure identity for the AFD controller so
//     ATM-only tenants do not inherit AFD write permissions. Because a Kubernetes pod
//     projects exactly one Workload-Identity federated token, satisfying §7 requires
//     the AFD controllers to run in a separate pod (sibling binary
//     cmd/hub-afd-controller-manager + sibling chart charts/hub-afd-controller-manager).
//   - The current POC hosts this reconciler INSIDE cmd/hub-net-controller-manager under
//     --enable-frontdoor-feature, so it shares one WI subject with ATM. The sibling
//     binary+chart split is a hard GA prerequisite (docs/first-party/003 §2.4).
package frontdoorprofile

import (
	"context"
	"fmt"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/azureerrors"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
)

const (
	// ControllerName is the name of the FrontDoorProfile controller.
	ControllerName = "frontdoorprofile-controller"

	// AzureResourceProfileNameFormat is the name of the Azure Front Door profile created by
	// the controller. Uses the CR UID so the underlying Azure name is stable across CR renames
	// and deterministic per Kubernetes object.
	AzureResourceProfileNameFormat = "fleet-%s"

	// DefaultEndpointNameFormat is the name of the default AFD endpoint created under the
	// profile. Kept short (Azure enforces 46-char cap on endpoint names).
	DefaultEndpointNameFormat = "fleet-%s"

	// AzureFrontDoorProfileLocation is the location AFD profiles must be created in; AFD is a
	// global service and rejects any other value.
	AzureFrontDoorProfileLocation = "Global"

	// requeueOnPending is how often we requeue while an async AFD provisioning is in flight.
	requeueOnPending = 30 * time.Second

	eventReasonAzureAPIError = "AzureAPIError"
	eventReasonProgrammed    = "Programmed"
	eventReasonDeleted       = "Deleted"
)

// Reconciler reconciles a FrontDoorProfile object.
type Reconciler struct {
	client.Client

	// ProfilesClient is the armcdn Profiles client used to CRUD AFD profiles.
	ProfilesClient *armcdn.ProfilesClient
	// EndpointsClient is the armcdn AFD endpoints client used to CRUD the default endpoint.
	EndpointsClient *armcdn.AFDEndpointsClient

	Recorder record.EventRecorder
}

// AzureProfileName returns the Azure Front Door profile name the controller manages for a
// given FrontDoorProfile CR.
func AzureProfileName(profile *fleetnetv1alpha1.FrontDoorProfile) string {
	return fmt.Sprintf(AzureResourceProfileNameFormat, profile.UID)
}

// AzureEndpointName returns the name of the default AFD endpoint under a profile.
func AzureEndpointName(profile *fleetnetv1alpha1.FrontDoorProfile) string {
	return fmt.Sprintf(DefaultEndpointNameFormat, profile.UID)
}

//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=frontdoorprofiles,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=frontdoorprofiles/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=frontdoorprofiles/finalizers,verbs=get;update
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile drives one reconciliation for a FrontDoorProfile.
//
// Flow:
//   - Object not found → treat as deleted, no-op.
//   - DeletionTimestamp set → handleDelete (idempotent cleanup + remove finalizer).
//   - Otherwise → handleUpdate (add finalizer, create-or-update AFD profile + default
//     endpoint, refresh status).
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	name := req.NamespacedName
	profileKRef := klog.KRef(name.Namespace, name.Name)

	startTime := time.Now()
	klog.V(2).InfoS("Reconciliation starts", "frontDoorProfile", profileKRef)
	defer func() {
		klog.V(2).InfoS("Reconciliation ends", "frontDoorProfile", profileKRef, "latencyMs", time.Since(startTime).Milliseconds())
	}()

	profile := &fleetnetv1alpha1.FrontDoorProfile{}
	if err := r.Client.Get(ctx, name, profile); err != nil {
		if apierrors.IsNotFound(err) {
			klog.V(2).InfoS("Ignoring NotFound frontDoorProfile", "frontDoorProfile", profileKRef)
			return ctrl.Result{}, nil
		}
		klog.ErrorS(err, "Failed to get frontDoorProfile", "frontDoorProfile", profileKRef)
		return ctrl.Result{}, err
	}

	if !profile.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.handleDelete(ctx, profile)
	}

	return r.handleUpdate(ctx, profile)
}

// handleDelete deletes the underlying Azure Front Door profile (which cascades to its
// child endpoints/custom-domains/etc.) and then removes our finalizer. Not-found errors from
// Azure are treated as success so the flow is idempotent across retries and crash recoveries.
func (r *Reconciler) handleDelete(ctx context.Context, profile *fleetnetv1alpha1.FrontDoorProfile) (ctrl.Result, error) {
	profileKObj := klog.KObj(profile)

	if !controllerutil.ContainsFinalizer(profile, objectmeta.FrontDoorProfileFinalizer) {
		// Either we already cleaned up, or we never touched Azure. Nothing to do.
		return ctrl.Result{}, nil
	}

	azProfileName := AzureProfileName(profile)
	klog.V(2).InfoS("Deleting Azure Front Door profile", "frontDoorProfile", profileKObj, "azureProfile", azProfileName)

	poller, err := r.ProfilesClient.BeginDelete(ctx, profile.Spec.ResourceGroup, azProfileName, nil)
	if err != nil {
		// A 404 from BeginDelete means the profile was already deleted (or never created) —
		// fall through to the finalizer-removal path below.
		if !azureerrors.IsNotFound(err) {
			r.Recorder.Eventf(profile, corev1.EventTypeWarning, eventReasonAzureAPIError,
				"Failed to begin delete of AFD profile %s: %v", azProfileName, err)
			klog.ErrorS(err, "Failed to begin delete of Azure Front Door profile",
				"frontDoorProfile", profileKObj, "azureProfile", azProfileName)
			return ctrl.Result{}, err
		}
	} else {
		if _, err := poller.PollUntilDone(ctx, nil); err != nil {
			// Same rationale as above: races with concurrent deletes may surface as 404.
			if !azureerrors.IsNotFound(err) {
				r.Recorder.Eventf(profile, corev1.EventTypeWarning, eventReasonAzureAPIError,
					"Failed to delete AFD profile %s: %v", azProfileName, err)
				klog.ErrorS(err, "Failed to delete Azure Front Door profile",
					"frontDoorProfile", profileKObj, "azureProfile", azProfileName)
				return ctrl.Result{}, err
			}
		}
	}

	r.Recorder.Eventf(profile, corev1.EventTypeNormal, eventReasonDeleted,
		"Deleted Azure Front Door profile %s", azProfileName)

	controllerutil.RemoveFinalizer(profile, objectmeta.FrontDoorProfileFinalizer)
	if err := r.Client.Update(ctx, profile); err != nil {
		klog.ErrorS(err, "Failed to remove frontDoorProfile finalizer", "frontDoorProfile", profileKObj)
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// handleUpdate is the reconcile path for a live (non-deleting) FrontDoorProfile. It ensures
// the AFD profile and its default endpoint exist in Azure, then reflects the observed state
// into .status. All Azure operations are get-then-create; we do not attempt to mutate an
// existing profile because SKU/Location changes are blocked by CRD validation.
func (r *Reconciler) handleUpdate(ctx context.Context, profile *fleetnetv1alpha1.FrontDoorProfile) (ctrl.Result, error) {
	profileKObj := klog.KObj(profile)
	azProfileName := AzureProfileName(profile)
	azEndpointName := AzureEndpointName(profile)

	// Register finalizer only just before we contact Azure so that a persistent 403 (e.g.
	// wrong RG or missing role assignment) does not leave the resource undeletable.
	if !controllerutil.ContainsFinalizer(profile, objectmeta.FrontDoorProfileFinalizer) {
		controllerutil.AddFinalizer(profile, objectmeta.FrontDoorProfileFinalizer)
		if err := r.Update(ctx, profile); err != nil {
			klog.ErrorS(err, "Failed to add finalizer to frontDoorProfile", "frontDoorProfile", profileKObj)
			return ctrl.Result{}, err
		}
	}

	desiredProfile := desiredAzureProfile(profile)

	// Ensure the profile exists.
	if _, getErr := r.ProfilesClient.Get(ctx, profile.Spec.ResourceGroup, azProfileName, nil); getErr != nil {
		if !azureerrors.IsNotFound(getErr) {
			return r.reportAzureError(ctx, profile, "get profile", getErr)
		}
		poller, err := r.ProfilesClient.BeginCreate(ctx, profile.Spec.ResourceGroup, azProfileName, desiredProfile, nil)
		if err != nil {
			return r.reportAzureError(ctx, profile, "begin create profile", err)
		}
		if _, err := poller.PollUntilDone(ctx, nil); err != nil {
			return r.reportAzureError(ctx, profile, "create profile", err)
		}
		klog.V(2).InfoS("Created Azure Front Door profile", "frontDoorProfile", profileKObj, "azureProfile", azProfileName)
	}

	// Ensure the default endpoint exists.
	epRes, epGetErr := r.EndpointsClient.Get(ctx, profile.Spec.ResourceGroup, azProfileName, azEndpointName, nil)
	if epGetErr != nil {
		if !azureerrors.IsNotFound(epGetErr) {
			return r.reportAzureError(ctx, profile, "get endpoint", epGetErr)
		}
		poller, err := r.EndpointsClient.BeginCreate(ctx, profile.Spec.ResourceGroup, azProfileName, azEndpointName, desiredAzureEndpoint(), nil)
		if err != nil {
			return r.reportAzureError(ctx, profile, "begin create endpoint", err)
		}
		res, err := poller.PollUntilDone(ctx, nil)
		if err != nil {
			return r.reportAzureError(ctx, profile, "create endpoint", err)
		}
		epRes.AFDEndpoint = res.AFDEndpoint
		klog.V(2).InfoS("Created Azure Front Door default endpoint",
			"frontDoorProfile", profileKObj, "azureEndpoint", azEndpointName)
	}

	// Populate status from Azure state.
	profile.Status.ResourceID = azureResourceIDForProfile(profile, azProfileName)
	if epRes.Properties != nil && epRes.Properties.HostName != nil {
		profile.Status.EndpointHostname = ptr.To(*epRes.Properties.HostName)
	}
	meta.SetStatusCondition(&profile.Status.Conditions, metav1.Condition{
		Type:               string(fleetnetv1alpha1.FrontDoorProfileConditionProgrammed),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: profile.Generation,
		Reason:             string(fleetnetv1alpha1.FrontDoorProfileReasonProgrammed),
		Message:            fmt.Sprintf("AFD profile %s programmed", azProfileName),
	})
	if err := r.Client.Status().Update(ctx, profile); err != nil {
		klog.ErrorS(err, "Failed to update frontDoorProfile status", "frontDoorProfile", profileKObj)
		return ctrl.Result{}, err
	}

	r.Recorder.Eventf(profile, corev1.EventTypeNormal, eventReasonProgrammed,
		"AFD profile %s and default endpoint %s programmed", azProfileName, azEndpointName)
	return ctrl.Result{}, nil
}

// reportAzureError writes a Pending/AzureError condition, emits an event, and returns the error
// so the controller-runtime rate-limited workqueue schedules a retry.
func (r *Reconciler) reportAzureError(ctx context.Context, profile *fleetnetv1alpha1.FrontDoorProfile, op string, azErr error) (ctrl.Result, error) {
	profileKObj := klog.KObj(profile)
	klog.ErrorS(azErr, "Azure Front Door operation failed",
		"frontDoorProfile", profileKObj, "operation", op)
	r.Recorder.Eventf(profile, corev1.EventTypeWarning, eventReasonAzureAPIError,
		"AFD %s failed: %v", op, azErr)

	status := metav1.ConditionUnknown
	reason := fleetnetv1alpha1.FrontDoorProfileReasonPending
	if azureerrors.IsClientError(azErr) && !azureerrors.IsThrottled(azErr) {
		status = metav1.ConditionFalse
		if azureerrors.IsForbidden(azErr) {
			reason = fleetnetv1alpha1.FrontDoorProfileReasonInvalid
		} else {
			reason = fleetnetv1alpha1.FrontDoorProfileReasonAzureError
		}
	}
	meta.SetStatusCondition(&profile.Status.Conditions, metav1.Condition{
		Type:               string(fleetnetv1alpha1.FrontDoorProfileConditionProgrammed),
		Status:             status,
		ObservedGeneration: profile.Generation,
		Reason:             string(reason),
		Message:            fmt.Sprintf("AFD %s: %v", op, azErr),
	})
	if err := r.Client.Status().Update(ctx, profile); err != nil {
		klog.ErrorS(err, "Failed to update frontDoorProfile status after Azure error",
			"frontDoorProfile", profileKObj)
	}
	return ctrl.Result{RequeueAfter: requeueOnPending}, azErr
}

// desiredAzureProfile builds the armcdn.Profile payload for a create. AFD requires
// Location="Global"; the SKU is copied verbatim from the CR (validated by CRD enum).
func desiredAzureProfile(profile *fleetnetv1alpha1.FrontDoorProfile) armcdn.Profile {
	return armcdn.Profile{
		Location: ptr.To(AzureFrontDoorProfileLocation),
		SKU: &armcdn.SKU{
			Name: ptr.To(armcdn.SKUName(profile.Spec.Sku)),
		},
		Properties: &armcdn.ProfileProperties{},
	}
}

// desiredAzureEndpoint builds the payload for the profile's default AFD endpoint. Enabled by
// default so the *.azurefd.net hostname starts serving traffic as soon as backends are wired
// in later phases.
func desiredAzureEndpoint() armcdn.AFDEndpoint {
	return armcdn.AFDEndpoint{
		Location: ptr.To(AzureFrontDoorProfileLocation),
		Properties: &armcdn.AFDEndpointProperties{
			EnabledState: ptr.To(armcdn.EnabledStateEnabled),
		},
	}
}

// azureResourceIDForProfile constructs the ARM resource ID for the AFD profile without needing
// an extra Azure round-trip. Subscription ID is embedded via the client at construction time
// but not accessible here, so we leave the /subscriptions/... prefix to the caller-facing
// status message and return the RG-and-below suffix. Full ID with subscription is patched in by
// the SDK response when we later call Get; for POC this is sufficient for diagnostics.
func azureResourceIDForProfile(profile *fleetnetv1alpha1.FrontDoorProfile, azProfileName string) string {
	return fmt.Sprintf("/resourceGroups/%s/providers/Microsoft.Cdn/profiles/%s",
		profile.Spec.ResourceGroup, azProfileName)
}

// SetupWithManager registers the controller with the manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named(ControllerName).
		For(&fleetnetv1alpha1.FrontDoorProfile{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}
