/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package frontdoorcustomdomain features the FrontDoorCustomDomain controller (POC) that
// reconciles FrontDoorCustomDomain CRs to Azure Front Door custom domain resources.
//
// Scope note (POC, see breadcrumb 2026-07-18):
//   - Only the Managed TLS path is implemented. BYOC (Key Vault) is intentionally rejected
//     with an Invalid condition — the spec field is reserved but the reconciliation path is
//     deferred (breadcrumb D3).
//   - The DNS validation token is surfaced in .status only; Kubernetes Event emission for the
//     token is deferred (breadcrumb D5).
package frontdoorcustomdomain

import (
	"context"
	"fmt"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
	frontdoorprofilectrl "go.goms.io/fleet-networking/pkg/controllers/hub/frontdoorprofile"
)

const (
	// ControllerName is the name of the FrontDoorCustomDomain controller.
	ControllerName = "frontdoorcustomdomain-controller"

	// AzureResourceCustomDomainNameFormat names the underlying Azure custom domain resource.
	// AFD requires DNS-safe characters and forbids '.', so we use the CR UID.
	AzureResourceCustomDomainNameFormat = "fleet-%s"

	// requeueWhileValidating is how often the controller re-polls Azure for validation state
	// while the domain is Pending/Submitting.
	requeueWhileValidating = 30 * time.Second

	// requeueOnProfileNotReady is how often the controller re-polls when the parent profile is
	// not yet Programmed.
	requeueOnProfileNotReady = 10 * time.Second

	eventReasonAzureAPIError    = "AzureAPIError"
	eventReasonProgrammed       = "Programmed"
	eventReasonDeleted          = "Deleted"
	eventReasonProfileNotReady  = "ProfileNotReady"
	eventReasonAwaitingDNSAuth  = "AwaitingDNSValidation"
	eventReasonUnsupportedTLS   = "UnsupportedTLSMode"
	eventReasonValidationFailed = "ValidationFailed"
)

// Reconciler reconciles a FrontDoorCustomDomain object.
type Reconciler struct {
	client.Client

	CustomDomainsClient *armcdn.AFDCustomDomainsClient
	Recorder            record.EventRecorder
}

// AzureCustomDomainName returns the underlying Azure custom domain resource name.
func AzureCustomDomainName(d *fleetnetv1alpha1.FrontDoorCustomDomain) string {
	return fmt.Sprintf(AzureResourceCustomDomainNameFormat, d.UID)
}

//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=frontdoorcustomdomains,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=frontdoorcustomdomains/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=frontdoorcustomdomains/finalizers,verbs=get;update
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=frontdoorprofiles,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile drives one reconciliation for a FrontDoorCustomDomain.
//
// Flow:
//   - Object not found → no-op.
//   - DeletionTimestamp set → handleDelete (idempotent Azure delete + finalizer removal).
//   - Otherwise → handleUpdate (BYOC guard → resolve parent profile → ensure Azure resource →
//     reflect validation state into status; requeue while validation is pending).
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	name := req.NamespacedName
	kref := klog.KRef(name.Namespace, name.Name)

	startTime := time.Now()
	klog.V(2).InfoS("Reconciliation starts", "frontDoorCustomDomain", kref)
	defer func() {
		klog.V(2).InfoS("Reconciliation ends", "frontDoorCustomDomain", kref, "latencyMs", time.Since(startTime).Milliseconds())
	}()

	cd := &fleetnetv1alpha1.FrontDoorCustomDomain{}
	if err := r.Client.Get(ctx, name, cd); err != nil {
		if apierrors.IsNotFound(err) {
			klog.V(2).InfoS("Ignoring NotFound frontDoorCustomDomain", "frontDoorCustomDomain", kref)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if !cd.DeletionTimestamp.IsZero() {
		return r.handleDelete(ctx, cd)
	}
	return r.handleUpdate(ctx, cd)
}

// handleDelete cleans up the underlying Azure custom domain (if the parent profile still
// exists) and removes our finalizer. Not-found errors from Azure and Kubernetes are treated
// as success so the flow is idempotent.
func (r *Reconciler) handleDelete(ctx context.Context, cd *fleetnetv1alpha1.FrontDoorCustomDomain) (ctrl.Result, error) {
	cdKObj := klog.KObj(cd)

	if !controllerutil.ContainsFinalizer(cd, objectmeta.FrontDoorCustomDomainFinalizer) {
		return ctrl.Result{}, nil
	}

	// We need the parent profile to know the Azure profile name. It may already be gone; if so
	// there is nothing to delete on Azure's side.
	profile, err := r.getProfile(ctx, cd)
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	if profile != nil {
		azProfileName := frontdoorprofilectrl.AzureProfileName(profile)
		azDomainName := AzureCustomDomainName(cd)
		klog.V(2).InfoS("Deleting Azure Front Door custom domain",
			"frontDoorCustomDomain", cdKObj, "azureProfile", azProfileName, "azureCustomDomain", azDomainName)

		poller, dErr := r.CustomDomainsClient.BeginDelete(ctx, profile.Spec.ResourceGroup, azProfileName, azDomainName, nil)
		if dErr != nil {
			if !azureerrors.IsNotFound(dErr) {
				r.Recorder.Eventf(cd, corev1.EventTypeWarning, eventReasonAzureAPIError,
					"Failed to begin delete of AFD custom domain %s: %v", azDomainName, dErr)
				return ctrl.Result{}, dErr
			}
		} else {
			if _, pErr := poller.PollUntilDone(ctx, nil); pErr != nil {
				if !azureerrors.IsNotFound(pErr) {
					r.Recorder.Eventf(cd, corev1.EventTypeWarning, eventReasonAzureAPIError,
						"Failed to delete AFD custom domain %s: %v", azDomainName, pErr)
					return ctrl.Result{}, pErr
				}
			}
		}

		r.Recorder.Eventf(cd, corev1.EventTypeNormal, eventReasonDeleted,
			"Deleted Azure Front Door custom domain %s", azDomainName)
	}

	controllerutil.RemoveFinalizer(cd, objectmeta.FrontDoorCustomDomainFinalizer)
	if err := r.Client.Update(ctx, cd); err != nil {
		klog.ErrorS(err, "Failed to remove frontDoorCustomDomain finalizer", "frontDoorCustomDomain", cdKObj)
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// handleUpdate is the reconcile path for a live FrontDoorCustomDomain. Ordering is
// important: reject unsupported TLS modes BEFORE resolving the parent (so that a bad-spec
// object never gets a finalizer that only the AFD delete path can remove) and BEFORE
// touching Azure at all.
func (r *Reconciler) handleUpdate(ctx context.Context, cd *fleetnetv1alpha1.FrontDoorCustomDomain) (ctrl.Result, error) {
	cdKObj := klog.KObj(cd)

	// POC guard (breadcrumb D3): reject BYOC with a permanent Invalid condition.
	if cd.Spec.TLS.Mode == fleetnetv1alpha1.FrontDoorTLSModeBYOC {
		r.Recorder.Eventf(cd, corev1.EventTypeWarning, eventReasonUnsupportedTLS,
			"BYOC TLS mode is not implemented in the POC; only Managed is supported")
		return r.writeStatus(ctx, cd, metav1.Condition{
			Type:               string(fleetnetv1alpha1.FrontDoorCustomDomainConditionProgrammed),
			Status:             metav1.ConditionFalse,
			ObservedGeneration: cd.Generation,
			Reason:             string(fleetnetv1alpha1.FrontDoorCustomDomainReasonInvalid),
			Message:            "BYOC TLS mode is not implemented in the POC; only Managed is supported",
		}, ctrl.Result{})
	}

	// Resolve parent profile (same namespace; breadcrumb D4).
	profile, err := r.getProfile(ctx, cd)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return r.writeStatus(ctx, cd, metav1.Condition{
				Type:               string(fleetnetv1alpha1.FrontDoorCustomDomainConditionProgrammed),
				Status:             metav1.ConditionFalse,
				ObservedGeneration: cd.Generation,
				Reason:             string(fleetnetv1alpha1.FrontDoorCustomDomainReasonProfileNotReady),
				Message:            fmt.Sprintf("Referenced FrontDoorProfile %q not found in namespace %q", cd.Spec.ProfileRef.Name, cd.Namespace),
			}, ctrl.Result{RequeueAfter: requeueOnProfileNotReady})
		}
		return ctrl.Result{}, err
	}

	if !isProfileProgrammed(profile) {
		r.Recorder.Eventf(cd, corev1.EventTypeWarning, eventReasonProfileNotReady,
			"Waiting for FrontDoorProfile %s to become Programmed", profile.Name)
		return r.writeStatus(ctx, cd, metav1.Condition{
			Type:               string(fleetnetv1alpha1.FrontDoorCustomDomainConditionProgrammed),
			Status:             metav1.ConditionUnknown,
			ObservedGeneration: cd.Generation,
			Reason:             string(fleetnetv1alpha1.FrontDoorCustomDomainReasonProfileNotReady),
			Message:            fmt.Sprintf("Referenced FrontDoorProfile %q is not yet Programmed", profile.Name),
		}, ctrl.Result{RequeueAfter: requeueOnProfileNotReady})
	}

	// Register finalizer only just before we contact Azure (avoid stuck-delete on 403).
	if !controllerutil.ContainsFinalizer(cd, objectmeta.FrontDoorCustomDomainFinalizer) {
		controllerutil.AddFinalizer(cd, objectmeta.FrontDoorCustomDomainFinalizer)
		if err := r.Update(ctx, cd); err != nil {
			klog.ErrorS(err, "Failed to add finalizer to frontDoorCustomDomain", "frontDoorCustomDomain", cdKObj)
			return ctrl.Result{}, err
		}
	}

	azProfileName := frontdoorprofilectrl.AzureProfileName(profile)
	azDomainName := AzureCustomDomainName(cd)
	rg := profile.Spec.ResourceGroup

	// Ensure the custom domain resource exists in AFD.
	getRes, getErr := r.CustomDomainsClient.Get(ctx, rg, azProfileName, azDomainName, nil)
	if getErr != nil {
		if !azureerrors.IsNotFound(getErr) {
			return r.reportAzureError(ctx, cd, "get custom domain", getErr)
		}
		desired := desiredAzureCustomDomain(cd)
		poller, err := r.CustomDomainsClient.BeginCreate(ctx, rg, azProfileName, azDomainName, desired, nil)
		if err != nil {
			return r.reportAzureError(ctx, cd, "begin create custom domain", err)
		}
		res, err := poller.PollUntilDone(ctx, nil)
		if err != nil {
			return r.reportAzureError(ctx, cd, "create custom domain", err)
		}
		getRes.AFDDomain = res.AFDDomain
		klog.V(2).InfoS("Created Azure Front Door custom domain",
			"frontDoorCustomDomain", cdKObj, "azureCustomDomain", azDomainName)
	}

	return r.reflectAzureStateToStatus(ctx, cd, getRes.AFDDomain, azDomainName)
}

// reflectAzureStateToStatus copies validation + provisioning state from the AFD resource into
// the CR status. Returns Programmed=True only when validation is Approved.
func (r *Reconciler) reflectAzureStateToStatus(ctx context.Context, cd *fleetnetv1alpha1.FrontDoorCustomDomain, azDomain armcdn.AFDDomain, azDomainName string) (ctrl.Result, error) {
	cd.Status.ResourceID = derefString(azDomain.ID)

	if azDomain.Properties != nil {
		if azDomain.Properties.DomainValidationState != nil {
			cd.Status.ValidationState = fleetnetv1alpha1.FrontDoorDomainValidationState(*azDomain.Properties.DomainValidationState)
		}
		if azDomain.Properties.ValidationProperties != nil {
			cd.Status.DNSValidationToken = azDomain.Properties.ValidationProperties.ValidationToken
			cd.Status.DNSValidationExpiry = parseExpiry(azDomain.Properties.ValidationProperties.ExpirationDate)
		}
	}

	cond, requeue := conditionFromValidationState(cd)
	return r.writeStatus(ctx, cd, cond, ctrl.Result{RequeueAfter: requeue})
}

// conditionFromValidationState maps AFD validation state to the Programmed condition and a
// suggested requeue interval.
func conditionFromValidationState(cd *fleetnetv1alpha1.FrontDoorCustomDomain) (metav1.Condition, time.Duration) {
	base := metav1.Condition{
		Type:               string(fleetnetv1alpha1.FrontDoorCustomDomainConditionProgrammed),
		ObservedGeneration: cd.Generation,
	}
	switch cd.Status.ValidationState {
	case fleetnetv1alpha1.FrontDoorDomainValidationStateApproved:
		base.Status = metav1.ConditionTrue
		base.Reason = string(fleetnetv1alpha1.FrontDoorCustomDomainReasonProgrammed)
		base.Message = "Custom domain validated and TLS bound (Managed)"
		return base, 0
	case fleetnetv1alpha1.FrontDoorDomainValidationStateRejected,
		fleetnetv1alpha1.FrontDoorDomainValidationStateTimedOut,
		fleetnetv1alpha1.FrontDoorDomainValidationStateInternalError:
		base.Status = metav1.ConditionFalse
		base.Reason = string(fleetnetv1alpha1.FrontDoorCustomDomainReasonValidationFailed)
		base.Message = fmt.Sprintf("Domain validation state: %s", cd.Status.ValidationState)
		return base, 0
	default:
		base.Status = metav1.ConditionUnknown
		base.Reason = string(fleetnetv1alpha1.FrontDoorCustomDomainReasonAwaitingDNSValidation)
		base.Message = fmt.Sprintf("Awaiting DNS validation (current state: %s). Create a TXT record at _dnsauth.%s with the value in status.dnsValidationToken.", cd.Status.ValidationState, cd.Spec.Hostname)
		return base, requeueWhileValidating
	}
}

// writeStatus persists the condition and returns the caller-supplied ctrl.Result.
func (r *Reconciler) writeStatus(ctx context.Context, cd *fleetnetv1alpha1.FrontDoorCustomDomain, cond metav1.Condition, res ctrl.Result) (ctrl.Result, error) {
	meta.SetStatusCondition(&cd.Status.Conditions, cond)
	if err := r.Client.Status().Update(ctx, cd); err != nil {
		klog.ErrorS(err, "Failed to update frontDoorCustomDomain status", "frontDoorCustomDomain", klog.KObj(cd))
		return ctrl.Result{}, err
	}
	return res, nil
}

func (r *Reconciler) reportAzureError(ctx context.Context, cd *fleetnetv1alpha1.FrontDoorCustomDomain, op string, azErr error) (ctrl.Result, error) {
	klog.ErrorS(azErr, "Azure Front Door custom domain operation failed",
		"frontDoorCustomDomain", klog.KObj(cd), "operation", op)
	r.Recorder.Eventf(cd, corev1.EventTypeWarning, eventReasonAzureAPIError,
		"AFD custom domain %s failed: %v", op, azErr)

	status := metav1.ConditionUnknown
	reason := fleetnetv1alpha1.FrontDoorCustomDomainReasonPending
	if azureerrors.IsClientError(azErr) && !azureerrors.IsThrottled(azErr) {
		status = metav1.ConditionFalse
		if azureerrors.IsForbidden(azErr) {
			reason = fleetnetv1alpha1.FrontDoorCustomDomainReasonInvalid
		} else {
			reason = fleetnetv1alpha1.FrontDoorCustomDomainReasonAzureError
		}
	}
	cond := metav1.Condition{
		Type:               string(fleetnetv1alpha1.FrontDoorCustomDomainConditionProgrammed),
		Status:             status,
		ObservedGeneration: cd.Generation,
		Reason:             string(reason),
		Message:            fmt.Sprintf("AFD %s: %v", op, azErr),
	}
	if _, wErr := r.writeStatus(ctx, cd, cond, ctrl.Result{}); wErr != nil {
		return ctrl.Result{}, wErr
	}
	return ctrl.Result{RequeueAfter: requeueWhileValidating}, azErr
}

func (r *Reconciler) getProfile(ctx context.Context, cd *fleetnetv1alpha1.FrontDoorCustomDomain) (*fleetnetv1alpha1.FrontDoorProfile, error) {
	profile := &fleetnetv1alpha1.FrontDoorProfile{}
	err := r.Client.Get(ctx, types.NamespacedName{Namespace: cd.Namespace, Name: cd.Spec.ProfileRef.Name}, profile)
	if err != nil {
		return nil, err
	}
	return profile, nil
}

func isProfileProgrammed(profile *fleetnetv1alpha1.FrontDoorProfile) bool {
	cond := meta.FindStatusCondition(profile.Status.Conditions, string(fleetnetv1alpha1.FrontDoorProfileConditionProgrammed))
	return cond != nil && cond.Status == metav1.ConditionTrue
}

// desiredAzureCustomDomain builds the armcdn payload for a create. TLS is pinned to
// AFD-managed certificate; BYOC is filtered out earlier in handleUpdate (breadcrumb D3).
func desiredAzureCustomDomain(cd *fleetnetv1alpha1.FrontDoorCustomDomain) armcdn.AFDDomain {
	return armcdn.AFDDomain{
		Properties: &armcdn.AFDDomainProperties{
			HostName: ptr.To(cd.Spec.Hostname),
			TLSSettings: &armcdn.AFDDomainHTTPSParameters{
				CertificateType: ptr.To(armcdn.AfdCertificateTypeManagedCertificate),
			},
		},
	}
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// parseExpiry converts the Azure-supplied ExpirationDate (RFC3339) into a *metav1.Time. On
// parse failure it returns nil rather than surfacing an error, since expiry is advisory.
func parseExpiry(s *string) *metav1.Time {
	if s == nil || *s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, *s)
	if err != nil {
		return nil
	}
	mt := metav1.NewTime(t)
	return &mt
}

// SetupWithManager registers the controller with the manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named(ControllerName).
		For(&fleetnetv1alpha1.FrontDoorCustomDomain{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}
