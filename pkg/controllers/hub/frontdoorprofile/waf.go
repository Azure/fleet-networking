/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package frontdoorprofile

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/frontdoor/armfrontdoor"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/azureerrors"
)

// WAF-enforcement path for the FrontDoorProfile reconciler.
//
// Scope of enforcement (matches docs/first-party/002-afd-implementation-plan.md §11):
//   - spec.wafPolicy nil AND ComplianceMode=None → no WAF, no SecurityPolicy
//     attach, and any prior fleet-managed SecurityPolicy is cleaned up (drift
//     removal). Programmed=True.
//   - spec.wafPolicy set → resolve the referenced WAF policy; upsert a
//     SecurityPolicy of type WebApplicationFirewall on the profile that binds
//     the policy to the default endpoint (path "/*"). Programmed=True.
//   - ComplianceMode=SFI-NS253 → additionally require the referenced policy's
//     PolicySettings.Mode == "Prevention". A "Detection"-mode policy is
//     rejected with Reason=WAFPolicyNotInPreventionMode so an SFI-audited
//     tenant cannot ship a WAF policy that only observes traffic.
//
// The spec-level requirement of "WAFPolicy is set when ComplianceMode is
// SFI-NS253" is enforced by a top-level CEL rule on the CRD (see
// api/v1alpha1/frontdoorprofile_types.go), so this file assumes an SFI
// profile always has spec.wafPolicy != nil and does not re-check.
//
// POC gaps (see breadcrumb 2026-07-20 Addendum 2 / Commit 7 series):
//   - Cross-subscription WAF references are rejected with
//     Reason=WAFPolicyNotFound + a diagnostic message. Supporting them
//     requires a per-subscription armfrontdoor.PoliciesClient which the
//     Clients bundle does not construct today; the design is captured in the
//     proposal but not yet implemented.
//   - Additional AFD custom domains (once FrontDoorCustomDomain lands under
//     a full binding controller) are NOT added to the SecurityPolicy
//     Associations here — only the default *.azurefd.net endpoint is bound.
//     Custom-domain binding is Phase 4 work.
//   - The SecurityPolicy Association's PatternsToMatch is fixed to "/*".
//     Path-level exclusions are not yet exposed on the CR.

// wafPolicyResourceIDRegex parses a WAF-policy ARM ID into (sub, rg, name).
// Kept in sync with the CRD-level Pattern validator on
// FrontDoorWAFPolicyRef.ResourceID so a value that passes CRD admission also
// parses here. Anchored to avoid partial matches on odd inputs.
var wafPolicyResourceIDRegex = regexp.MustCompile(
	`^/subscriptions/([^/]+)/resourceGroups/([^/]+)/providers/Microsoft\.Network/frontdoorwebapplicationfirewallpolicies/([^/]+)$`,
)

// fleetSecurityPolicyName returns the deterministic name of the AFD
// SecurityPolicy that the profile reconciler owns for a given FrontDoorProfile.
// Uses the CR UID (same convention as AzureProfileName / AzureEndpointName)
// so the underlying Azure name survives CR renames and never collides across
// CRs. The "fleet-waf-" prefix makes the resource identifiable as
// fleet-managed when browsing the AFD portal.
func fleetSecurityPolicyName(profile *fleetnetv1alpha1.FrontDoorProfile) string {
	return fmt.Sprintf("fleet-waf-%s", profile.UID)
}

// wafResolveResult captures the parsed pieces of the WAF-policy ARM ID plus
// the resolved policy for downstream consumers (validation + attach). Kept
// as a struct rather than multiple return values because callers commonly
// need everything together.
type wafResolveResult struct {
	subscriptionID string
	resourceGroup  string
	policyName     string
	policyID       string // canonical ARM ID (same as spec.wafPolicy.resourceID).
	policy         armfrontdoor.WebApplicationFirewallPolicy
}

// ensureWAFEnforcement is the single entry point invoked from handleUpdate
// after the profile + default endpoint are in place. It returns a bool
// indicating whether Programmed=True can proceed:
//   - true, nil  → WAF state is settled (either attached correctly, or
//     absent and any prior attach has been cleaned up); caller may set
//     Programmed=True.
//   - false, nil → a non-Programmed condition has already been written
//     (WAFPolicyNotFound / WAFPolicyNotInPreventionMode / an Azure error
//     surfaced via reportAzureError); caller should NOT overwrite it and
//     should return early. The reconcile will be re-driven when the WAF
//     policy is created/updated (spec change on the profile CR) or on the
//     next resync tick.
//   - false, err → a transient Azure error occurred; caller should return
//     the error so the workqueue retries with backoff.
//
// endpointARMID must be the fully qualified ARM ID of the default AFD
// endpoint (needed for the SecurityPolicy Association). It is passed in
// rather than re-derived because handleUpdate already Get/Created the
// endpoint and has the value in hand.
func (r *Reconciler) ensureWAFEnforcement(ctx context.Context, profile *fleetnetv1alpha1.FrontDoorProfile, endpointARMID string) (bool, error) {
	profileKObj := klog.KObj(profile)
	azProfileName := AzureProfileName(profile)
	secPolicyName := fleetSecurityPolicyName(profile)

	// Fast path: no WAF requested. Best-effort drift removal so a user who
	// clears spec.wafPolicy does not leave an orphaned SecurityPolicy on
	// the AFD profile pointing at a WAF policy the CR no longer references.
	if profile.Spec.WAFPolicy == nil {
		return r.ensureNoSecurityPolicy(ctx, profile, secPolicyName)
	}

	res, ok, err := r.resolveWAFPolicy(ctx, profile)
	if err != nil {
		// Azure error already surfaced via reportAzureError; the caller must
		// return err so the workqueue retries. Programmed=True must not be
		// set in this reconcile.
		return false, err
	}
	if !ok {
		// Terminal client-side rejection (NotFound / bad ID / cross-sub).
		// Condition was written inside resolveWAFPolicy.
		return false, nil
	}

	// SFI-NS253 tightening: prevention mode is mandatory. Detection or a
	// nil-Mode policy would let malicious traffic through while only being
	// logged, violating the "block by default" posture SFI requires.
	if profile.Spec.ComplianceMode == fleetnetv1alpha1.FrontDoorProfileComplianceModeSFINS253 {
		mode := ""
		if res.policy.Properties != nil && res.policy.Properties.PolicySettings != nil && res.policy.Properties.PolicySettings.Mode != nil {
			mode = string(*res.policy.Properties.PolicySettings.Mode)
		}
		if mode != string(armfrontdoor.PolicyModePrevention) {
			r.setWAFCondition(ctx, profile,
				fleetnetv1alpha1.FrontDoorProfileReasonWAFPolicyNotInPreventionMode,
				fmt.Sprintf("SFI-NS253 requires WAF policy %s to be in Prevention mode; found %q", res.policyID, mode))
			r.Recorder.Eventf(profile, corev1.EventTypeWarning, string(fleetnetv1alpha1.FrontDoorProfileReasonWAFPolicyNotInPreventionMode),
				"WAF policy %s is not in Prevention mode (SFI-NS253)", res.policyID)
			return false, nil
		}
	}

	// Upsert the SecurityPolicy binding the WAF policy to the default
	// endpoint. armcdn's BeginCreate is an upsert (no separate Update
	// endpoint exists on the SecurityPolicy resource), so calling it on
	// every reconcile is safe and drives Azure to the desired state even
	// if a user edits the SecurityPolicy out-of-band.
	desired := desiredSecurityPolicy(res.policyID, endpointARMID)
	poller, err := r.SecurityPoliciesClient.BeginCreate(ctx, profile.Spec.ResourceGroup, azProfileName, secPolicyName, desired, nil)
	if err != nil {
		_, retErr := r.reportAzureError(ctx, profile, "begin create security policy", err)
		return false, retErr
	}
	if _, err := poller.PollUntilDone(ctx, nil); err != nil {
		_, retErr := r.reportAzureError(ctx, profile, "create security policy", err)
		return false, retErr
	}
	klog.V(2).InfoS("Ensured WAF SecurityPolicy attach",
		"frontDoorProfile", profileKObj, "securityPolicy", secPolicyName, "wafPolicy", res.policyID)
	return true, nil
}

// ensureNoSecurityPolicy removes any previously-attached fleet-managed
// SecurityPolicy. Called when spec.wafPolicy is nil so a user clearing the
// field does not leave stale WAF attachment behind. NotFound from the Get is
// the common case (no prior attach) and is not an error.
func (r *Reconciler) ensureNoSecurityPolicy(ctx context.Context, profile *fleetnetv1alpha1.FrontDoorProfile, secPolicyName string) (bool, error) {
	profileKObj := klog.KObj(profile)
	azProfileName := AzureProfileName(profile)

	if _, err := r.SecurityPoliciesClient.Get(ctx, profile.Spec.ResourceGroup, azProfileName, secPolicyName, nil); err != nil {
		if azureerrors.IsNotFound(err) {
			return true, nil // Nothing to clean up; happy path.
		}
		_, retErr := r.reportAzureError(ctx, profile, "get security policy (cleanup probe)", err)
		return false, retErr
	}

	// Existing SecurityPolicy found and spec no longer wants one → delete.
	poller, err := r.SecurityPoliciesClient.BeginDelete(ctx, profile.Spec.ResourceGroup, azProfileName, secPolicyName, nil)
	if err != nil {
		if azureerrors.IsNotFound(err) {
			return true, nil
		}
		_, retErr := r.reportAzureError(ctx, profile, "begin delete security policy (drift removal)", err)
		return false, retErr
	}
	if _, err := poller.PollUntilDone(ctx, nil); err != nil {
		if azureerrors.IsNotFound(err) {
			return true, nil
		}
		_, retErr := r.reportAzureError(ctx, profile, "delete security policy (drift removal)", err)
		return false, retErr
	}
	klog.V(2).InfoS("Removed stale WAF SecurityPolicy after spec.wafPolicy cleared",
		"frontDoorProfile", profileKObj, "securityPolicy", secPolicyName)
	return true, nil
}

// resolveWAFPolicy parses spec.wafPolicy.resourceID and Gets the referenced
// policy from Azure. Returns:
//   - (res, true, nil)   on success
//   - (_,   false, nil)  on terminal client rejection (invalid ID / cross-sub
//     / NotFound). The appropriate condition is written before returning.
//   - (_,   false, err)  on transient Azure error. The AzureError condition
//     is written via reportAzureError; caller must return err to retry.
func (r *Reconciler) resolveWAFPolicy(ctx context.Context, profile *fleetnetv1alpha1.FrontDoorProfile) (wafResolveResult, bool, error) {
	raw := profile.Spec.WAFPolicy.ResourceID
	match := wafPolicyResourceIDRegex.FindStringSubmatch(raw)
	if match == nil {
		// Should be unreachable in practice — the CRD Pattern validator
		// rejects malformed IDs at admission — but guard defensively so a
		// version-skew (older API server without the Pattern) does not
		// crash the reconciler.
		r.setWAFCondition(ctx, profile,
			fleetnetv1alpha1.FrontDoorProfileReasonWAFPolicyNotFound,
			fmt.Sprintf("spec.wafPolicy.resourceID %q is not a valid AFD WAF policy ARM ID", raw))
		return wafResolveResult{}, false, nil
	}
	sub, rg, name := match[1], match[2], match[3]

	// Cross-subscription references would need a separate armfrontdoor
	// PoliciesClient constructed against `sub`, which the current Clients
	// bundle (pkg/common/azurefrontdoor/client.go) does not build. Rather
	// than silently failing later with an opaque 403, surface the limit
	// clearly so the user knows to co-locate the WAF policy for the POC.
	if !strings.EqualFold(sub, r.subscriptionID()) {
		r.setWAFCondition(ctx, profile,
			fleetnetv1alpha1.FrontDoorProfileReasonWAFPolicyNotFound,
			fmt.Sprintf("cross-subscription WAF references are not yet supported (policy sub %s, profile sub %s)", sub, r.subscriptionID()))
		return wafResolveResult{}, false, nil
	}

	resp, err := r.WAFPoliciesClient.Get(ctx, rg, name, nil)
	if err != nil {
		if azureerrors.IsNotFound(err) {
			r.setWAFCondition(ctx, profile,
				fleetnetv1alpha1.FrontDoorProfileReasonWAFPolicyNotFound,
				fmt.Sprintf("WAF policy %s not found", raw))
			r.Recorder.Eventf(profile, corev1.EventTypeWarning, string(fleetnetv1alpha1.FrontDoorProfileReasonWAFPolicyNotFound),
				"WAF policy %s not found", raw)
			return wafResolveResult{}, false, nil
		}
		_, retErr := r.reportAzureError(ctx, profile, "get waf policy", err)
		return wafResolveResult{}, false, retErr
	}

	return wafResolveResult{
		subscriptionID: sub,
		resourceGroup:  rg,
		policyName:     name,
		policyID:       raw,
		policy:         resp.WebApplicationFirewallPolicy,
	}, true, nil
}

// setWAFCondition writes a non-Programmed Programmed-type condition capturing
// a terminal WAF rejection (not a transient Azure error — those flow through
// reportAzureError). Uses Programmed=False so status.conditions has a single
// authoritative condition type; consumers watching for readiness look at
// Programmed only.
func (r *Reconciler) setWAFCondition(ctx context.Context, profile *fleetnetv1alpha1.FrontDoorProfile, reason fleetnetv1alpha1.FrontDoorProfileConditionReason, message string) {
	meta.SetStatusCondition(&profile.Status.Conditions, metav1.Condition{
		Type:               string(fleetnetv1alpha1.FrontDoorProfileConditionProgrammed),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: profile.Generation,
		Reason:             string(reason),
		Message:            message,
	})
	if err := r.Client.Status().Update(ctx, profile); err != nil {
		klog.ErrorS(err, "Failed to update frontDoorProfile status after WAF rejection",
			"frontDoorProfile", klog.KObj(profile), "reason", reason)
	}
}

// subscriptionID returns the ARM subscription the WAF PoliciesClient targets.
// The SDK client does not expose this publicly, so we cache it in the
// Reconciler (see SubscriptionID field). Kept as a method rather than a
// direct field access so a future indirection (e.g. multi-sub) is easy.
func (r *Reconciler) subscriptionID() string {
	return r.SubscriptionID
}

// desiredSecurityPolicy builds the armcdn.SecurityPolicy payload binding
// the resolved WAF policy to the default AFD endpoint. Path "/*" catches all
// requests; per-path exclusions are not yet exposed on the CR.
func desiredSecurityPolicy(wafPolicyID, endpointARMID string) armcdn.SecurityPolicy {
	return armcdn.SecurityPolicy{
		Properties: &armcdn.SecurityPolicyProperties{
			Parameters: &armcdn.SecurityPolicyWebApplicationFirewallParameters{
				Type: ptr.To(armcdn.SecurityPolicyTypeWebApplicationFirewall),
				WafPolicy: &armcdn.ResourceReference{
					ID: ptr.To(wafPolicyID),
				},
				Associations: []*armcdn.SecurityPolicyWebApplicationFirewallAssociation{{
					Domains: []*armcdn.ActivatedResourceReference{{
						ID: ptr.To(endpointARMID),
					}},
					PatternsToMatch: []*string{ptr.To("/*")},
				}},
			},
		},
	}
}
