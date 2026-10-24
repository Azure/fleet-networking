/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package frontdoor

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	"k8s.io/utils/ptr"

	"go.goms.io/fleet-networking/pkg/common/objectmeta"
)

func ownershipFixture() (ResourceScope, GatewayOwner) {
	return ResourceScope{SubscriptionID: "00000000-0000-0000-0000-000000000000", ResourceGroup: "ingress-rg"},
		GatewayOwner{HubUID: "hub-uid", Namespace: "store", Name: "global", UID: "gateway-uid"}
}

func TestBuildRootRequests(t *testing.T) {
	scope, owner := ownershipFixture()
	got, err := BuildRootRequests(scope, owner)
	if err != nil {
		t.Fatal(err)
	}
	backends, err := PlanBackends(backendModel())
	if err != nil {
		t.Fatal(err)
	}
	if got.ProfileName != backends.ProfileName || got.EndpointName != backends.EndpointName ||
		got.ProfileID != "/subscriptions/"+scope.SubscriptionID+"/resourceGroups/"+scope.ResourceGroup+"/providers/Microsoft.Cdn/profiles/"+got.ProfileName {
		t.Fatalf("root and backend identities differ: %#v", got)
	}
	for name, value := range map[string]any{"profile": got.Profile, "endpoint": got.Endpoint} {
		t.Run(name, func(t *testing.T) {
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(data, &body); err != nil {
				t.Fatal(err)
			}
			requireJSON(t, body, "location", "global")
			tags := map[string]string{
				"networking.fleet.azure.com.controller":        objectmeta.GatewayAPIControllerName,
				"networking.fleet.azure.com.hub-uid":           owner.HubUID,
				"networking.fleet.azure.com.gateway-namespace": owner.Namespace,
				"networking.fleet.azure.com.gateway-name":      owner.Name,
				"networking.fleet.azure.com.gateway-uid":       owner.UID,
			}
			if name == "endpoint" {
				tags[endpointActivationTag] = endpointActivationPending
			}
			requireJSON(t, body, "tags", tags)
			for _, field := range []string{"id", "name", "type", "systemData"} {
				if _, found := body[field]; found {
					t.Fatalf("read-only field %q in root request", field)
				}
			}
			if name == "profile" {
				requireJSON(t, body, "sku", map[string]string{"name": "Premium_AzureFrontDoor"})
				return
			}
			properties := requestProperties(t, value)
			requireJSON(t, properties, "enabledState", "Disabled")
			for _, field := range []string{"hostName", "profileName", "provisioningState", "deploymentStatus"} {
				if _, found := properties[field]; found {
					t.Fatalf("observed field %q in endpoint request", field)
				}
			}
		})
	}
}

func TestBuildRootRequestsRejectsIncompleteIdentity(t *testing.T) {
	tests := []struct {
		name   string
		change func(*ResourceScope, *GatewayOwner)
	}{
		{"missing subscription", func(s *ResourceScope, _ *GatewayOwner) { s.SubscriptionID = "" }},
		{"missing resource group", func(s *ResourceScope, _ *GatewayOwner) { s.ResourceGroup = "" }},
		{"subscription path injection", func(s *ResourceScope, _ *GatewayOwner) { s.SubscriptionID += "/resourceGroups/other" }},
		{"resource group path injection", func(s *ResourceScope, _ *GatewayOwner) { s.ResourceGroup += "/providers/Microsoft.Cdn/profiles/other" }},
		{"backslash in scope", func(s *ResourceScope, _ *GatewayOwner) { s.ResourceGroup += "\\" + otherName }},
		{"query in scope", func(s *ResourceScope, _ *GatewayOwner) { s.ResourceGroup += "?other" }},
		{"missing hub UID", func(_ *ResourceScope, o *GatewayOwner) { o.HubUID = "" }},
		{"missing Gateway namespace", func(_ *ResourceScope, o *GatewayOwner) { o.Namespace = "" }},
		{"missing Gateway name", func(_ *ResourceScope, o *GatewayOwner) { o.Name = "" }},
		{"missing Gateway UID", func(_ *ResourceScope, o *GatewayOwner) { o.UID = "" }},
		{"whitespace identity", func(_ *ResourceScope, o *GatewayOwner) { o.UID = " gateway-uid " }},
		{"control character in identity", func(_ *ResourceScope, o *GatewayOwner) { o.UID = "gateway\nuid" }},
		{"oversized tag", func(_ *ResourceScope, o *GatewayOwner) { o.HubUID = strings.Repeat("h", 257) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scope, owner := ownershipFixture()
			tt.change(&scope, &owner)
			got, err := BuildRootRequests(scope, owner)
			if err == nil || !reflect.DeepEqual(got, RootRequests{}) {
				t.Fatalf("invalid identity returned root requests: %#v, %v", got, err)
			}
			if err := VerifyProfileOwnership(scope, owner, armcdn.Profile{}); err == nil {
				t.Fatal("invalid expected identity passed ownership validation")
			}
		})
	}
}

func TestVerifyProfileOwnership(t *testing.T) {
	tests := []struct {
		name   string
		change func(*armcdn.Profile)
		valid  bool
	}{
		{"owned", func(*armcdn.Profile) {}, true},
		{"missing response name", func(p *armcdn.Profile) { p.Name = nil }, true},
		{"scope case differences", func(p *armcdn.Profile) { p.ID = ptr.To(strings.ToUpper(*p.ID)) }, true},
		{"tag key case differences", func(p *armcdn.Profile) {
			p.Tags["NETWORKING.FLEET.AZURE.COM.HUB-UID"] = p.Tags["networking.fleet.azure.com.hub-uid"]
			delete(p.Tags, "networking.fleet.azure.com.hub-uid")
		}, true},
		{"unmanaged tags", func(p *armcdn.Profile) { p.Tags["cost-center"] = ptr.To("shared") }, true},
		// Drift does not relinquish ownership or prevent deletion of an owned root.
		{"owned SKU drift", func(p *armcdn.Profile) { p.SKU = nil }, true},
		{"owned Standard SKU drift", func(p *armcdn.Profile) {
			p.SKU = &armcdn.SKU{Name: ptr.To(armcdn.SKUNameStandardAzureFrontDoor)}
		}, true},
		{"missing ID", func(p *armcdn.Profile) { p.ID = nil }, false},
		{"invalid ID", func(p *armcdn.Profile) { p.ID = ptr.To("not-an-arm-id") }, false},
		{"different subscription", func(p *armcdn.Profile) {
			p.ID = ptr.To(strings.Replace(*p.ID, "00000000-0000-0000-0000-000000000000", "11111111-1111-1111-1111-111111111111", 1))
		}, false},
		{"different resource group", func(p *armcdn.Profile) { p.ID = ptr.To(strings.Replace(*p.ID, "ingress-rg", "other-rg", 1)) }, false},
		{"different root", func(p *armcdn.Profile) { p.ID = ptr.To(*p.ID + "-other") }, false},
		{"child instead of root", func(p *armcdn.Profile) { p.ID = ptr.To(*p.ID + "/afdEndpoints/child") }, false},
		{"contradictory response name", func(p *armcdn.Profile) { p.Name = ptr.To(otherName) }, false},
		{"missing tags", func(p *armcdn.Profile) { p.Tags = nil }, false},
		{"missing hub tag", func(p *armcdn.Profile) { delete(p.Tags, "networking.fleet.azure.com.hub-uid") }, false},
		{"nil hub tag", func(p *armcdn.Profile) { p.Tags["networking.fleet.azure.com.hub-uid"] = nil }, false},
		{"different hub", func(p *armcdn.Profile) { p.Tags["networking.fleet.azure.com.hub-uid"] = ptr.To("other-hub") }, false},
		{"case-different hub identity", func(p *armcdn.Profile) { p.Tags["networking.fleet.azure.com.hub-uid"] = ptr.To("HUB-UID") }, false},
		{"different controller", func(p *armcdn.Profile) { p.Tags["networking.fleet.azure.com.controller"] = ptr.To("other-controller") }, false},
		{"different namespace", func(p *armcdn.Profile) { p.Tags["networking.fleet.azure.com.gateway-namespace"] = ptr.To(otherName) }, false},
		{"different Gateway name", func(p *armcdn.Profile) { p.Tags["networking.fleet.azure.com.gateway-name"] = ptr.To(otherName) }, false},
		{"same-name Gateway replacement", func(p *armcdn.Profile) { p.Tags["networking.fleet.azure.com.gateway-uid"] = ptr.To("old-uid") }, false},
		{"ambiguous tag casing", func(p *armcdn.Profile) { p.Tags["NETWORKING.FLEET.AZURE.COM.HUB-UID"] = ptr.To("other-hub") }, false},
		{"duplicate equivalent tag casing", func(p *armcdn.Profile) { p.Tags["NETWORKING.FLEET.AZURE.COM.HUB-UID"] = ptr.To("hub-uid") }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scope, owner := ownershipFixture()
			requests, err := BuildRootRequests(scope, owner)
			if err != nil {
				t.Fatal(err)
			}
			profile := requests.Profile
			profile.ID, profile.Name = ptr.To(requests.ProfileID), ptr.To(requests.ProfileName)
			tt.change(&profile)
			before, err := json.Marshal(profile)
			if err != nil {
				t.Fatal(err)
			}
			err = VerifyProfileOwnership(scope, owner, profile)
			if (err == nil) != tt.valid {
				t.Fatalf("owned=%v, want %v: %v", err == nil, tt.valid, err)
			}
			if !tt.valid {
				var collision *OwnershipConflictError
				if !errors.As(err, &collision) || collision.ProfileID != requests.ProfileID || collision.Reason == "" {
					t.Fatalf("missing actionable typed ownership error: %v", err)
				}
			}
			after, marshalErr := json.Marshal(profile)
			if marshalErr != nil || string(before) != string(after) {
				t.Fatalf("ownership verification mutated observed Azure state: %v", marshalErr)
			}
		})
	}
}

func TestRootRequestsIdentityAndIsolation(t *testing.T) {
	scope, owner := ownershipFixture()
	first, err := BuildRootRequests(scope, owner)
	if err != nil {
		t.Fatal(err)
	}
	*first.Profile.Tags["networking.fleet.azure.com.hub-uid"] = changedValue
	second, err := BuildRootRequests(scope, owner)
	if err != nil || *second.Profile.Tags["networking.fleet.azure.com.hub-uid"] != owner.HubUID ||
		*first.Endpoint.Tags["networking.fleet.azure.com.hub-uid"] != owner.HubUID {
		t.Fatalf("root request tags alias another request: %v", err)
	}
	previous := second
	previous.Profile.ID, previous.Profile.Name = ptr.To(previous.ProfileID), ptr.To(previous.ProfileName)
	if err := VerifyProfileOwnership(scope, owner, previous.Profile); err != nil {
		t.Fatal(err)
	}
	owner.UID = replacementUID
	replacement, err := BuildRootRequests(scope, owner)
	if err != nil || replacement.ProfileName == previous.ProfileName || replacement.EndpointName == previous.EndpointName {
		t.Fatalf("same-name Gateway replacement reused Azure identity: %#v, %v", replacement, err)
	}
	if err := VerifyProfileOwnership(scope, owner, previous.Profile); err == nil {
		t.Fatal("replacement Gateway adopted the old profile")
	}
}
