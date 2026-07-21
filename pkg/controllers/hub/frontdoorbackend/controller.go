/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package frontdoorbackend features the FrontDoorBackend controller (POC)
// that reconciles FrontDoorBackend CRs to Azure Front Door OriginGroups +
// Origins.
//
// Scope note (POC — mirrors the frontdoorprofile package's scope disclaimer):
//   - Happy-path reconcile only. Detailed error classification, drift
//     detection, and metrics are deferred past POC.
//   - AFD/ATM coexistence guard (Conflict reason) lands in a follow-up
//     commit; this file establishes the reconciler skeleton plus the
//     Accepted / Invalid / Pending paths.
//
// Data model:
//   - Each FrontDoorBackend produces ONE OriginGroup named fleet-<UID>
//     under the parent FrontDoorProfile.
//   - Each qualifying InternalServiceExport (ExportMode=L7-FrontDoor +
//     PrivateLinkServiceResourceID populated) becomes ONE Origin under
//     that OriginGroup, wired via SharedPrivateLinkResource to the
//     exported PLS.
//   - Per-origin weight = ceil(backend.Spec.Weight * export.Spec.Weight /
//     sum(export.Spec.Weight)), matching TrafficManagerBackend semantics
//     exactly so operators moving between the two surfaces get the same
//     traffic distribution given the same inputs.
package frontdoorbackend

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/azureerrors"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/controllers/hub/frontdoorprofile"
)

const (
	// ControllerName is the name of the FrontDoorBackend controller.
	ControllerName = "frontdoorbackend-controller"

	// AzureResourceOriginGroupNameFormat is the name of the Azure Front
	// Door OriginGroup created for a FrontDoorBackend. Uses the CR UID so
	// the underlying Azure name is stable across CR renames and
	// deterministic per Kubernetes object — same convention as
	// frontdoorprofile.AzureResourceProfileNameFormat.
	AzureResourceOriginGroupNameFormat = "fleet-%s"

	// AzureResourceOriginNameFormat is the name of an Azure Front Door
	// Origin under the OriginGroup: fleet-<backendUID>-<clusterID>. The
	// clusterID suffix keeps origins deterministic and lets us delete
	// stale entries by prefix-scanning when a member cluster's export
	// disappears.
	AzureResourceOriginNameFormat = "fleet-%s-%s"

	// requeueOnPending is how often we requeue while waiting for the
	// parent FrontDoorProfile to reach Programmed, or while a
	// still-in-flight ServiceImport catches up with cluster status. Kept
	// deliberately generous — SetupWithManager (Commit 10d) adds explicit
	// secondary watches so most transitions are picked up without needing
	// this fallback.
	requeueOnPending = 30 * time.Second

	eventReasonAzureAPIError = "AzureAPIError"
	eventReasonProgrammed    = "Programmed"
	eventReasonDeleted       = "Deleted"
	eventReasonInvalid       = "Invalid"
)

// Reconciler reconciles a FrontDoorBackend object.
type Reconciler struct {
	client.Client

	// OriginGroupsClient CRUDs Microsoft.Cdn/profiles/*/originGroups.
	OriginGroupsClient *armcdn.AFDOriginGroupsClient
	// OriginsClient CRUDs Microsoft.Cdn/profiles/*/originGroups/*/origins.
	OriginsClient *armcdn.AFDOriginsClient

	Recorder record.EventRecorder
}

// AzureOriginGroupName returns the Azure OriginGroup name for a given
// FrontDoorBackend CR. Kept exported so the FrontDoorProfile deletion path
// (and any future FrontDoorRoute reconciler) can reference it without
// importing this file's private naming rules.
func AzureOriginGroupName(backend *fleetnetv1alpha1.FrontDoorBackend) string {
	return fmt.Sprintf(AzureResourceOriginGroupNameFormat, backend.UID)
}

// AzureOriginName returns the Azure Origin name for a given backend + member
// cluster.
func AzureOriginName(backend *fleetnetv1alpha1.FrontDoorBackend, clusterID string) string {
	return fmt.Sprintf(AzureResourceOriginNameFormat, backend.UID, clusterID)
}

//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=frontdoorbackends,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=frontdoorbackends/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=frontdoorbackends/finalizers,verbs=get;update
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=frontdoorprofiles,verbs=get;list;watch
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceimports,verbs=get;list;watch
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=internalserviceexports,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile drives one reconciliation for a FrontDoorBackend. Mirrors the
// frontdoorprofile reconciler's structure so future readers can pattern-match
// between the two.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	name := req.NamespacedName
	backendKRef := klog.KRef(name.Namespace, name.Name)

	startTime := time.Now()
	klog.V(2).InfoS("Reconciliation starts", "frontDoorBackend", backendKRef)
	defer func() {
		klog.V(2).InfoS("Reconciliation ends", "frontDoorBackend", backendKRef, "latencyMs", time.Since(startTime).Milliseconds())
	}()

	backend := &fleetnetv1alpha1.FrontDoorBackend{}
	if err := r.Client.Get(ctx, name, backend); err != nil {
		if apierrors.IsNotFound(err) {
			klog.V(2).InfoS("Ignoring NotFound frontDoorBackend", "frontDoorBackend", backendKRef)
			return ctrl.Result{}, nil
		}
		klog.ErrorS(err, "Failed to get frontDoorBackend", "frontDoorBackend", backendKRef)
		return ctrl.Result{}, err
	}

	if !backend.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.handleDelete(ctx, backend)
	}
	return r.handleUpdate(ctx, backend)
}

// handleDelete deletes the OriginGroup (which cascade-deletes its Origins)
// and removes the finalizer. NotFound from Azure is treated as success so
// the flow is idempotent across retries.
func (r *Reconciler) handleDelete(ctx context.Context, backend *fleetnetv1alpha1.FrontDoorBackend) (ctrl.Result, error) {
	backendKObj := klog.KObj(backend)
	if !controllerutil.ContainsFinalizer(backend, objectmeta.FrontDoorBackendFinalizer) {
		return ctrl.Result{}, nil
	}

	// We need the parent profile's resource group to address the
	// OriginGroup. If the profile CR is already gone the AFD profile is
	// likely gone too (OriginGroups cascade with it); we still attempt
	// the delete + accept NotFound so a lingering FrontDoorBackend can
	// always be finalized.
	profile, err := r.getProfile(ctx, backend)
	if err != nil && !apierrors.IsNotFound(err) {
		klog.ErrorS(err, "Failed to look up parent FrontDoorProfile during delete", "frontDoorBackend", backendKObj)
		return ctrl.Result{}, err
	}
	if profile != nil {
		azProfileName := frontdoorprofile.AzureProfileName(profile)
		azOriginGroupName := AzureOriginGroupName(backend)
		poller, err := r.OriginGroupsClient.BeginDelete(ctx, profile.Spec.ResourceGroup, azProfileName, azOriginGroupName, nil)
		if err != nil {
			if !azureerrors.IsNotFound(err) {
				return r.reportAzureError(ctx, backend, "begin delete origin group", err)
			}
		} else if _, err := poller.PollUntilDone(ctx, nil); err != nil {
			if !azureerrors.IsNotFound(err) {
				return r.reportAzureError(ctx, backend, "delete origin group", err)
			}
		}
		r.Recorder.Eventf(backend, corev1.EventTypeNormal, eventReasonDeleted,
			"AFD OriginGroup %s deleted", azOriginGroupName)
	}

	controllerutil.RemoveFinalizer(backend, objectmeta.FrontDoorBackendFinalizer)
	if err := r.Client.Update(ctx, backend); err != nil {
		klog.ErrorS(err, "Failed to remove frontDoorBackend finalizer", "frontDoorBackend", backendKObj)
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// handleUpdate is the reconcile path for a live FrontDoorBackend.
func (r *Reconciler) handleUpdate(ctx context.Context, backend *fleetnetv1alpha1.FrontDoorBackend) (ctrl.Result, error) {
	backendKObj := klog.KObj(backend)

	// 1. Resolve parent FrontDoorProfile. Missing → Invalid (user error);
	//    not-yet-programmed → Pending. Both terminate this reconcile;
	//    SetupWithManager (Commit 10d) wires a Profile watch that
	//    re-triggers us when the profile flips Programmed=True.
	profile, err := r.getProfile(ctx, backend)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return r.setInvalidAndUpdate(ctx, backend, fmt.Sprintf("FrontDoorProfile %q not found", backend.Spec.Profile.Name))
		}
		return ctrl.Result{}, err
	}
	if !isProfileProgrammed(profile) {
		return r.setPendingAndUpdate(ctx, backend, fmt.Sprintf("FrontDoorProfile %q is not yet programmed", profile.Name))
	}

	// 2. Resolve the ServiceImport. Missing → Invalid; no clusters →
	//    Pending (still waiting on member clusters to export).
	svcImport := &fleetnetv1alpha1.ServiceImport{}
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: backend.Namespace, Name: backend.Spec.Backend.Name}, svcImport); err != nil {
		if apierrors.IsNotFound(err) {
			return r.setInvalidAndUpdate(ctx, backend, fmt.Sprintf("ServiceImport %q not found", backend.Spec.Backend.Name))
		}
		return ctrl.Result{}, err
	}
	if len(svcImport.Status.Clusters) == 0 {
		return r.setPendingAndUpdate(ctx, backend, "ServiceImport has no member cluster exports yet")
	}

	// 3. List InternalServiceExports feeding this ServiceImport and keep
	//    only those that opted into the L7-FrontDoor path AND have a
	//    resolved PLS resource ID. Everything else is either the ATM
	//    surface's export or still waiting on cloud-provider-azure to
	//    finish provisioning its PLS.
	exports := &fleetnetv1alpha1.InternalServiceExportList{}
	if err := r.Client.List(ctx, exports, client.InNamespace(backend.Namespace)); err != nil {
		return ctrl.Result{}, err
	}
	eligible := filterEligibleExports(exports.Items, svcImport)
	if len(eligible) == 0 {
		return r.setPendingAndUpdate(ctx, backend,
			"No InternalServiceExport with ExportMode=L7-FrontDoor and a resolved PrivateLinkServiceResourceID yet")
	}

	// 4. Register finalizer immediately before we contact Azure so a
	//    persistent 403 (bad RG, missing role assignment) cannot leave
	//    the CR undeletable — same rule the profile + TMB reconcilers
	//    follow.
	if !controllerutil.ContainsFinalizer(backend, objectmeta.FrontDoorBackendFinalizer) {
		controllerutil.AddFinalizer(backend, objectmeta.FrontDoorBackendFinalizer)
		if err := r.Update(ctx, backend); err != nil {
			klog.ErrorS(err, "Failed to add finalizer to frontDoorBackend", "frontDoorBackend", backendKObj)
			return ctrl.Result{}, err
		}
	}

	// 5. Upsert the OriginGroup + one Origin per eligible export, then
	//    reflect what we programmed into .status.
	azProfileName := frontdoorprofile.AzureProfileName(profile)
	azOriginGroupName := AzureOriginGroupName(backend)
	ogRes, err := r.ensureOriginGroup(ctx, profile.Spec.ResourceGroup, azProfileName, azOriginGroupName)
	if err != nil {
		return r.reportAzureError(ctx, backend, "ensure origin group", err)
	}

	originStatuses, err := r.upsertOrigins(ctx, backend, profile.Spec.ResourceGroup, azProfileName, azOriginGroupName, eligible)
	if err != nil {
		return r.reportAzureError(ctx, backend, "upsert origins", err)
	}

	backend.Status.OriginGroupResourceID = derefString(ogRes.ID)
	backend.Status.Origins = originStatuses
	meta.SetStatusCondition(&backend.Status.Conditions, metav1.Condition{
		Type:               string(fleetnetv1alpha1.FrontDoorBackendConditionAccepted),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: backend.Generation,
		Reason:             string(fleetnetv1alpha1.FrontDoorBackendReasonAccepted),
		Message:            fmt.Sprintf("Programmed %d origin(s) under OriginGroup %s", len(originStatuses), azOriginGroupName),
	})
	if err := r.Client.Status().Update(ctx, backend); err != nil {
		klog.ErrorS(err, "Failed to update frontDoorBackend status", "frontDoorBackend", backendKObj)
		return ctrl.Result{}, err
	}
	r.Recorder.Eventf(backend, corev1.EventTypeNormal, eventReasonProgrammed,
		"AFD OriginGroup %s programmed with %d origin(s)", azOriginGroupName, len(originStatuses))
	return ctrl.Result{}, nil
}

// getProfile resolves the parent FrontDoorProfile in the same namespace as
// the backend. Returns apierrors.NotFound when the CR is missing so callers
// can distinguish "user error" from "transient API failure".
func (r *Reconciler) getProfile(ctx context.Context, backend *fleetnetv1alpha1.FrontDoorBackend) (*fleetnetv1alpha1.FrontDoorProfile, error) {
	profile := &fleetnetv1alpha1.FrontDoorProfile{}
	err := r.Client.Get(ctx, types.NamespacedName{Namespace: backend.Namespace, Name: backend.Spec.Profile.Name}, profile)
	if err != nil {
		return nil, err
	}
	return profile, nil
}

// isProfileProgrammed returns true only when the profile carries a
// Programmed=True condition observing the current generation. We check
// ObservedGeneration explicitly so a stale True from before a profile spec
// change doesn't cause us to program origins against a half-migrated
// profile.
func isProfileProgrammed(profile *fleetnetv1alpha1.FrontDoorProfile) bool {
	cond := meta.FindStatusCondition(profile.Status.Conditions, string(fleetnetv1alpha1.FrontDoorProfileConditionProgrammed))
	return cond != nil && cond.Status == metav1.ConditionTrue && cond.ObservedGeneration == profile.Generation
}

// eligibleExport carries an export we are going to program together with
// its resolved raw weight. Kept as a private carrier struct so the
// per-origin weight math is easy to unit-test in isolation.
type eligibleExport struct {
	Export *fleetnetv1alpha1.InternalServiceExport
	// EffectiveWeight starts as the raw export weight (default 1 when
	// absent) and is overwritten in-place by distributeWeights with the
	// final per-origin weight.
	EffectiveWeight int64
}

// filterEligibleExports keeps only exports that:
//   - target this ServiceImport (matched via
//     ServiceReference.Namespace+Name), AND
//   - opted into the L7-FrontDoor mode, AND
//   - already have a resolved PrivateLinkServiceResourceID.
//
// The ATM path's InternalServiceExports remain in the list but are dropped
// here. The AFD/ATM coexistence guard (Commit 10c) rejects the backend
// entirely when a competing TrafficManagerBackend already owns the same
// ServiceImport; that check will live in a separate helper for
// testability.
func filterEligibleExports(all []fleetnetv1alpha1.InternalServiceExport, svcImport *fleetnetv1alpha1.ServiceImport) []eligibleExport {
	out := make([]eligibleExport, 0, len(all))
	for i := range all {
		exp := &all[i]
		if exp.Spec.ServiceReference.Namespace != svcImport.Namespace ||
			exp.Spec.ServiceReference.Name != svcImport.Name {
			continue
		}
		if exp.Spec.ExportMode != objectmeta.ExportModeValueFrontDoor {
			continue
		}
		if exp.Spec.PrivateLinkServiceResourceID == nil || *exp.Spec.PrivateLinkServiceResourceID == "" {
			continue
		}
		// Default per-export weight to 1 to match TrafficManagerBackend
		// behaviour when the annotation is absent on the source
		// ServiceExport.
		w := int64(1)
		if exp.Spec.Weight != nil {
			w = *exp.Spec.Weight
		}
		out = append(out, eligibleExport{Export: exp, EffectiveWeight: w})
	}
	return out
}

// distributeWeights rewrites EffectiveWeight in-place using the same
// ceil(backend.Weight * export.Weight / totalExportWeight) formula as
// TrafficManagerBackend, so the two surfaces produce identical traffic
// distributions given identical inputs. See TrafficManagerBackendSpec.Weight
// godoc for the derivation.
func distributeWeights(list []eligibleExport, aggregate int64) {
	if len(list) == 0 || aggregate == 0 {
		return
	}
	var total int64
	for _, e := range list {
		total += e.EffectiveWeight
	}
	if total == 0 {
		return
	}
	for i := range list {
		w := math.Ceil(float64(aggregate*list[i].EffectiveWeight) / float64(total))
		list[i].EffectiveWeight = int64(w)
	}
}

// ensureOriginGroup creates the OriginGroup if missing; otherwise returns
// the existing resource. We do NOT currently patch OriginGroup properties
// (health probe, session affinity) — those Spec knobs are Phase 4 work.
func (r *Reconciler) ensureOriginGroup(ctx context.Context, rg, profileName, name string) (*armcdn.AFDOriginGroup, error) {
	got, err := r.OriginGroupsClient.Get(ctx, rg, profileName, name, nil)
	if err == nil {
		return &got.AFDOriginGroup, nil
	}
	if !azureerrors.IsNotFound(err) {
		return nil, err
	}
	desired := desiredAzureOriginGroup()
	poller, err := r.OriginGroupsClient.BeginCreate(ctx, rg, profileName, name, desired, nil)
	if err != nil {
		return nil, err
	}
	res, err := poller.PollUntilDone(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &res.AFDOriginGroup, nil
}

// desiredAzureOriginGroup builds a minimal OriginGroup payload. The only
// hard requirement AFD imposes is a LoadBalancingSettings block; we use the
// service-default values (see Azure Front Door docs → "Origin group") since
// the CRD does not (yet) surface health-probe knobs.
func desiredAzureOriginGroup() armcdn.AFDOriginGroup {
	return armcdn.AFDOriginGroup{
		Properties: &armcdn.AFDOriginGroupProperties{
			LoadBalancingSettings: &armcdn.LoadBalancingSettingsParameters{
				SampleSize:                      ptr.To[int32](4),
				SuccessfulSamplesRequired:       ptr.To[int32](3),
				AdditionalLatencyInMilliseconds: ptr.To[int32](50),
			},
		},
	}
}

// upsertOrigins programs one Origin per eligible export under the origin
// group. We compute distributed weights first so a single mid-loop failure
// cannot produce a mismatch between what we programmed and what we
// reported.
func (r *Reconciler) upsertOrigins(ctx context.Context, backend *fleetnetv1alpha1.FrontDoorBackend, rg, profileName, ogName string, eligible []eligibleExport) ([]fleetnetv1alpha1.FrontDoorOriginStatus, error) {
	// Distribute aggregate weight across exports up-front.
	distributed := make([]eligibleExport, len(eligible))
	copy(distributed, eligible)
	aggregate := int64(1)
	if backend.Spec.Weight != nil {
		aggregate = *backend.Spec.Weight
	}
	distributeWeights(distributed, aggregate)

	statuses := make([]fleetnetv1alpha1.FrontDoorOriginStatus, 0, len(distributed))
	for _, ee := range distributed {
		exp := ee.Export
		clusterID := exp.Spec.ServiceReference.ClusterID
		originName := AzureOriginName(backend, clusterID)
		desired := desiredAzureOrigin(*exp.Spec.PrivateLinkServiceResourceID, ee.EffectiveWeight)
		poller, err := r.OriginsClient.BeginCreate(ctx, rg, profileName, ogName, originName, desired, nil)
		if err != nil {
			return nil, err
		}
		res, err := poller.PollUntilDone(ctx, nil)
		if err != nil {
			return nil, err
		}
		statuses = append(statuses, fleetnetv1alpha1.FrontDoorOriginStatus{
			Name:                         originName,
			ResourceID:                   derefString(res.ID),
			PrivateLinkServiceResourceID: exp.Spec.PrivateLinkServiceResourceID,
			Weight:                       ptr.To(ee.EffectiveWeight),
			From: &fleetnetv1alpha1.FromCluster{
				ClusterStatus: fleetnetv1alpha1.ClusterStatus{Cluster: clusterID},
				Weight:        exp.Spec.Weight,
			},
		})
	}
	return statuses, nil
}

// desiredAzureOrigin builds an Origin backed by the given PLS. HostName is
// required by the AFD API but is ignored at runtime when
// SharedPrivateLinkResource is present (traffic is tunnelled via PLS); we
// pass the PLS ID again as a placeholder so the payload validates without
// leaking a public hostname.
func desiredAzureOrigin(plsResourceID string, weight int64) armcdn.AFDOrigin {
	return armcdn.AFDOrigin{
		Properties: &armcdn.AFDOriginProperties{
			HostName: ptr.To(plsResourceID),
			Weight:   ptr.To(int32(weight)),
			SharedPrivateLinkResource: &armcdn.SharedPrivateLinkResourceProperties{
				PrivateLink: &armcdn.ResourceReference{
					ID: ptr.To(plsResourceID),
				},
				RequestMessage: ptr.To("Fleet-networking Front Door backend"),
			},
			EnabledState: ptr.To(armcdn.EnabledStateEnabled),
		},
	}
}

// setInvalidAndUpdate writes Accepted=False,Reason=Invalid and stops
// reconciling. Used for terminal user errors (missing profile / import).
func (r *Reconciler) setInvalidAndUpdate(ctx context.Context, backend *fleetnetv1alpha1.FrontDoorBackend, msg string) (ctrl.Result, error) {
	r.Recorder.Eventf(backend, corev1.EventTypeWarning, eventReasonInvalid, "%s", msg)
	meta.SetStatusCondition(&backend.Status.Conditions, metav1.Condition{
		Type:               string(fleetnetv1alpha1.FrontDoorBackendConditionAccepted),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: backend.Generation,
		Reason:             string(fleetnetv1alpha1.FrontDoorBackendReasonInvalid),
		Message:            msg,
	})
	return ctrl.Result{}, r.Client.Status().Update(ctx, backend)
}

// setPendingAndUpdate writes Accepted=Unknown,Reason=Pending. Used while
// waiting for parent objects (Profile, ServiceImport, InternalServiceExport)
// to catch up; SetupWithManager (Commit 10d) wires the appropriate
// watches so we get re-triggered without needing an explicit requeue.
func (r *Reconciler) setPendingAndUpdate(ctx context.Context, backend *fleetnetv1alpha1.FrontDoorBackend, msg string) (ctrl.Result, error) {
	meta.SetStatusCondition(&backend.Status.Conditions, metav1.Condition{
		Type:               string(fleetnetv1alpha1.FrontDoorBackendConditionAccepted),
		Status:             metav1.ConditionUnknown,
		ObservedGeneration: backend.Generation,
		Reason:             string(fleetnetv1alpha1.FrontDoorBackendReasonPending),
		Message:            msg,
	})
	return ctrl.Result{}, r.Client.Status().Update(ctx, backend)
}

// reportAzureError writes a Pending or Invalid condition depending on the
// error class, emits an event, and requeues with backoff. Same shape as
// frontdoorprofile.reportAzureError.
func (r *Reconciler) reportAzureError(ctx context.Context, backend *fleetnetv1alpha1.FrontDoorBackend, op string, azErr error) (ctrl.Result, error) {
	backendKObj := klog.KObj(backend)
	klog.ErrorS(azErr, "Azure Front Door operation failed",
		"frontDoorBackend", backendKObj, "operation", op)
	r.Recorder.Eventf(backend, corev1.EventTypeWarning, eventReasonAzureAPIError,
		"AFD %s failed: %v", op, azErr)

	status := metav1.ConditionUnknown
	reason := fleetnetv1alpha1.FrontDoorBackendReasonPending
	if azureerrors.IsClientError(azErr) && !azureerrors.IsThrottled(azErr) {
		// 4xx that is not 429 → we won't fix this by retrying.
		status = metav1.ConditionFalse
		reason = fleetnetv1alpha1.FrontDoorBackendReasonInvalid
	}
	meta.SetStatusCondition(&backend.Status.Conditions, metav1.Condition{
		Type:               string(fleetnetv1alpha1.FrontDoorBackendConditionAccepted),
		Status:             status,
		ObservedGeneration: backend.Generation,
		Reason:             string(reason),
		Message:            fmt.Sprintf("AFD %s: %v", op, azErr),
	})
	if err := r.Client.Status().Update(ctx, backend); err != nil {
		klog.ErrorS(err, "Failed to update frontDoorBackend status after Azure error",
			"frontDoorBackend", backendKObj)
	}
	return ctrl.Result{RequeueAfter: requeueOnPending}, azErr
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// SetupWithManager wires the reconciler. For now watches only the primary
// resource; secondary watches (FrontDoorProfile status flips,
// InternalServiceExport churn) are added in Commit 10d alongside the tests
// that exercise them, so both land together and the churn is easier to
// review.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetnetv1alpha1.FrontDoorBackend{}).
		Complete(r)
}
