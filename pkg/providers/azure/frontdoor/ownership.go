/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package frontdoor

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	"k8s.io/utils/ptr"

	"go.goms.io/fleet-networking/pkg/common/objectmeta"
)

// ResourceScope is the platform-configured ARM scope for owned AFD profiles.
type ResourceScope struct {
	SubscriptionID string
	ResourceGroup  string
}

// GatewayOwner identifies a Gateway independently of its reusable name.
// HubUID must come from the hub's kube-system Namespace UID, not customer input.
type GatewayOwner struct {
	HubUID    string
	Namespace string
	Name      string
	UID       string
}

// RootRequests contains Premium profile and initially disabled endpoint bodies.
// These are not write authorization: a persisted finalizer and the complete,
// validated routing graph are still required before any Azure mutation.
type RootRequests struct {
	ProfileID    string
	ProfileName  string
	EndpointName string
	Profile      armcdn.Profile
	Endpoint     armcdn.AFDEndpoint
}

// OwnershipConflictError prevents adoption or deletion of an unverified profile.
type OwnershipConflictError struct {
	ProfileID string
	Reason    string
}

func (e *OwnershipConflictError) Error() string {
	return fmt.Sprintf("AFD profile %q ownership conflict: %s", e.ProfileID, e.Reason)
}

// BuildRootRequests constructs fresh owned root requests without Azure calls.
// Every invalid identity returns zero requests.
func BuildRootRequests(scope ResourceScope, owner GatewayOwner) (RootRequests, error) {
	values := []struct {
		name  string
		value string
	}{
		{"subscription ID", scope.SubscriptionID}, {"resource group", scope.ResourceGroup},
		{"hub UID", owner.HubUID}, {"Gateway namespace", owner.Namespace},
		{"Gateway name", owner.Name}, {"Gateway UID", owner.UID},
	}
	for _, field := range values {
		if field.value == "" || strings.TrimSpace(field.value) != field.value ||
			strings.ContainsFunc(field.value, unicode.IsControl) {
			return RootRequests{}, fmt.Errorf("%s must be nonempty without surrounding whitespace or control characters", field.name)
		}
	}
	for _, value := range []string{scope.SubscriptionID, scope.ResourceGroup} {
		if strings.ContainsAny(value, "/\\?#") {
			return RootRequests{}, fmt.Errorf("AFD subscription ID and resource group must be single ARM path segments")
		}
	}
	tags := ownerTags(owner)
	for key, value := range tags {
		if len(*value) > 256 {
			return RootRequests{}, fmt.Errorf("AFD owner tag %q exceeds 256 bytes", key)
		}
	}
	profileName := resourceName("fleet-afd", owner.UID)
	return RootRequests{
		ProfileID: fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Cdn/profiles/%s",
			scope.SubscriptionID, scope.ResourceGroup, profileName),
		ProfileName: profileName, EndpointName: resourceName("fleet-ep", owner.UID),
		Profile: armcdn.Profile{
			Location: ptr.To("global"), SKU: &armcdn.SKU{Name: ptr.To(armcdn.SKUNamePremiumAzureFrontDoor)},
			Tags: tags,
		},
		Endpoint: armcdn.AFDEndpoint{
			Location: ptr.To("global"), Tags: endpointTags(owner),
			// Creation must not expose bindings before the complete graph is ready.
			Properties: &armcdn.AFDEndpointProperties{EnabledState: ptr.To(armcdn.EnabledStateDisabled)},
		},
	}, nil
}

// VerifyProfileOwnership checks the observed root's ARM identity and owner tags.
// Configuration drift is deliberately excluded so owned roots remain removable.
// Child operations must use this verified parent, never tags or a name alone.
func VerifyProfileOwnership(scope ResourceScope, owner GatewayOwner, profile armcdn.Profile) error {
	expected, err := BuildRootRequests(scope, owner)
	if err != nil {
		return err
	}
	conflict := func(reason string) error {
		return &OwnershipConflictError{ProfileID: expected.ProfileID, Reason: reason}
	}
	if profile.ID == nil || !strings.EqualFold(*profile.ID, expected.ProfileID) {
		return conflict("observed ARM ID does not match the expected subscription, resource group and profile")
	}
	if profile.Name != nil && !strings.EqualFold(*profile.Name, expected.ProfileName) {
		return conflict("observed resource name contradicts the expected profile identity")
	}
	for key, value := range expected.Profile.Tags {
		matches := 0
		for actualKey, actualValue := range profile.Tags {
			// ARM tag keys are case-insensitive; owner identity values are not.
			if strings.EqualFold(actualKey, key) {
				matches++
				if actualValue == nil || *actualValue != *value {
					return conflict(fmt.Sprintf("owner tag %q is absent or belongs to another owner", key))
				}
			}
		}
		if matches != 1 {
			return conflict(fmt.Sprintf("owner tag %q is missing or ambiguous", key))
		}
	}
	return nil
}

func ownerTags(owner GatewayOwner) map[string]*string {
	// Azure tag names cannot contain '/'; match the repository's dotted prefix.
	const prefix = "networking.fleet.azure.com."
	return map[string]*string{
		prefix + "controller":        ptr.To(objectmeta.GatewayAPIControllerName),
		prefix + "hub-uid":           ptr.To(owner.HubUID),
		prefix + "gateway-namespace": ptr.To(owner.Namespace),
		prefix + "gateway-name":      ptr.To(owner.Name),
		prefix + "gateway-uid":       ptr.To(owner.UID),
	}
}

func endpointTags(owner GatewayOwner) map[string]*string {
	tags := ownerTags(owner)
	tags[endpointActivationTag] = ptr.To(endpointActivationPending)
	return tags
}
