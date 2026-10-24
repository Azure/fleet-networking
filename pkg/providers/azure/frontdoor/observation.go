/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package frontdoor

import (
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	"k8s.io/utils/ptr"

	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
)

// PrivateOriginState describes observed origin configuration, not backend health.
type PrivateOriginState string

// PrivateOriginMissing and the other states distinguish observation and approval
// failures from matching, approved and successfully deployed origin configuration.
const (
	PrivateOriginMissing         PrivateOriginState = "Missing"
	PrivateOriginUnknown         PrivateOriginState = "Unknown"
	PrivateOriginDrifted         PrivateOriginState = "Drifted"
	PrivateOriginApprovalPending PrivateOriginState = "ApprovalPending"
	PrivateOriginProvisioning    PrivateOriginState = "Provisioning"
	PrivateOriginRejected        PrivateOriginState = "Rejected"
	PrivateOriginDisconnected    PrivateOriginState = "Disconnected"
	PrivateOriginTimedOut        PrivateOriginState = "TimedOut"
	PrivateOriginFailed          PrivateOriginState = "Failed"
	PrivateOriginReady           PrivateOriginState = "Ready"
)

// PrivateOriginObservation correlates one expected origin with its observed state.
type PrivateOriginObservation struct {
	Reference OriginReference
	Target    PrivateLinkTarget
	State     PrivateOriginState
}

// PrivateOriginsObservation summarizes only expected private origins.
// OriginsConverged does not establish profile/group/route/domain readiness,
// origin health or traffic delivery, and cannot alone set Gateway Programmed.
type PrivateOriginsObservation struct {
	// ProfileMissing is set only when the SDK observer receives a root GET 404.
	ProfileMissing   bool
	Origins          []PrivateOriginObservation
	PendingApprovals []PrivateLinkPlan
	OriginsConverged bool
}

// ObservePrivateOrigins evaluates a caller-supplied SDK snapshot without Azure
// calls or input mutation. The profile must prove ownership, and each expected
// origin must prove its full child ARM identity. Missing/unknown state fails
// closed; invalid intent or contradictory identity returns zero observation.
func ObservePrivateOrigins(scope ResourceScope, owner GatewayOwner, model gatewaymodel.GlobalGateway,
	profile armcdn.Profile, observed map[OriginReference]armcdn.AFDOrigin) (PrivateOriginsObservation, error) {
	if err := validateModelOwner(owner, model); err != nil {
		return PrivateOriginsObservation{}, err
	}
	if err := VerifyProfileOwnership(scope, owner, profile); err != nil {
		return PrivateOriginsObservation{}, err
	}
	plan, err := PlanBackends(model)
	if err != nil {
		return PrivateOriginsObservation{}, err
	}
	result := PrivateOriginsObservation{
		Origins: make([]PrivateOriginObservation, 0, plan.Capacity.Origins),
	}
	pending := make(map[PrivateLinkTarget]bool)
	allReady := true
	for _, group := range plan.OriginGroups {
		for _, origin := range group.Origins {
			ref := OriginReference{OriginGroup: group.Name, Origin: origin.Name}
			state := PrivateOriginMissing
			if actual, found := observed[ref]; found {
				expectedID := *profile.ID + "/originGroups/" + group.Name + "/origins/" + origin.Name
				if err := verifyOriginIdentity(expectedID, ref, actual); err != nil {
					return PrivateOriginsObservation{}, err
				}
				state = observePrivateOrigin(origin, actual.Properties)
			}
			result.Origins = append(result.Origins, PrivateOriginObservation{
				Reference: ref, Target: origin.PrivateLink, State: state,
			})
			if state != PrivateOriginReady {
				allReady = false
			}
			if state == PrivateOriginApprovalPending {
				pending[origin.PrivateLink] = true
			}
		}
	}
	for _, link := range plan.PrivateLinks {
		if pending[link.Target] {
			// One manual-approval diagnostic per shared target, not per origin/group.
			result.PendingApprovals = append(result.PendingApprovals, link)
		}
	}
	result.OriginsConverged = len(result.Origins) > 0 && allReady
	return result, nil
}

func validateModelOwner(owner GatewayOwner, model gatewaymodel.GlobalGateway) error {
	if model.UID != owner.UID || model.Namespace != owner.Namespace || model.Name != owner.Name {
		return fmt.Errorf("AFD intent does not match the Gateway owner identity")
	}
	return nil
}

func verifyOriginIdentity(expectedID string, ref OriginReference, observed armcdn.AFDOrigin) error {
	if observed.ID == nil || !strings.EqualFold(*observed.ID, expectedID) {
		return fmt.Errorf("observed AFD origin %q ARM ID does not match its expected owned parent", ref.Origin)
	}
	if observed.Name != nil && !strings.EqualFold(*observed.Name, ref.Origin) {
		return fmt.Errorf("observed AFD origin name contradicts expected identity %q", ref.Origin)
	}
	if observed.Properties != nil && observed.Properties.OriginGroupName != nil &&
		!strings.EqualFold(*observed.Properties.OriginGroupName, ref.OriginGroup) {
		return fmt.Errorf("observed AFD origin %q group contradicts expected identity %q", ref.Origin, ref.OriginGroup)
	}
	return nil
}

func observePrivateOrigin(desired Origin, properties *armcdn.AFDOriginProperties) PrivateOriginState {
	if properties == nil {
		return PrivateOriginUnknown
	}
	if !matchesPrivateOrigin(desired, properties) {
		return PrivateOriginDrifted
	}
	switch ptr.Deref(properties.SharedPrivateLinkResource.Status, "") {
	case armcdn.SharedPrivateLinkResourceStatusRejected:
		return PrivateOriginRejected
	case armcdn.SharedPrivateLinkResourceStatusDisconnected:
		return PrivateOriginDisconnected
	case armcdn.SharedPrivateLinkResourceStatusTimeout:
		return PrivateOriginTimedOut
	}
	if ptr.Deref(properties.ProvisioningState, "") == armcdn.AfdProvisioningStateFailed ||
		ptr.Deref(properties.DeploymentStatus, "") == armcdn.DeploymentStatusFailed {
		return PrivateOriginFailed
	}
	switch ptr.Deref(properties.SharedPrivateLinkResource.Status, "") {
	case armcdn.SharedPrivateLinkResourceStatusPending:
		return PrivateOriginApprovalPending
	case armcdn.SharedPrivateLinkResourceStatusApproved:
		return observedOriginDeployment(properties)
	default:
		return PrivateOriginUnknown
	}
}

func matchesPrivateOrigin(desired Origin, actual *armcdn.AFDOriginProperties) bool {
	if !matchesPrivateLinkTarget(desired.PrivateLink, actual) {
		return false
	}
	return ptr.Deref(actual.HostName, "") == desired.HostName &&
		ptr.Deref(actual.HTTPPort, 0) == desired.HTTPPort &&
		int64(ptr.Deref(actual.Weight, 0)) == desired.Weight &&
		ptr.Deref(actual.Priority, 0) == desired.Priority &&
		ptr.Deref(actual.EnabledState, "") == armcdn.EnabledStateEnabled &&
		ptr.Deref(actual.OriginHostHeader, "") == desired.OriginHostHeader &&
		ptr.Deref(actual.EnforceCertificateNameCheck, false) == desired.EnforceCertificateNameCheck
}

func matchesPrivateLinkTarget(desired PrivateLinkTarget, actual *armcdn.AFDOriginProperties) bool {
	if actual == nil {
		return false
	}
	link := actual.SharedPrivateLinkResource
	if link == nil || link.PrivateLink == nil ||
		!strings.EqualFold(ptr.Deref(link.PrivateLink.ID, ""), desired.ResourceID) ||
		!strings.EqualFold(ptr.Deref(link.PrivateLinkLocation, ""), desired.Location) ||
		ptr.Deref(link.GroupID, "") != "" || actual.AzureOrigin != nil {
		return false
	}
	return true
}

func observedOriginDeployment(properties *armcdn.AFDOriginProperties) PrivateOriginState {
	switch ptr.Deref(properties.ProvisioningState, "") {
	case armcdn.AfdProvisioningStateCreating, armcdn.AfdProvisioningStateUpdating, armcdn.AfdProvisioningStateDeleting:
		return PrivateOriginProvisioning
	case armcdn.AfdProvisioningStateSucceeded:
		switch ptr.Deref(properties.DeploymentStatus, "") {
		case armcdn.DeploymentStatusSucceeded:
			return PrivateOriginReady
		case armcdn.DeploymentStatusInProgress, armcdn.DeploymentStatusNotStarted:
			return PrivateOriginProvisioning
		}
	}
	return PrivateOriginUnknown
}
