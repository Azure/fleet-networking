/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package frontdoor

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	"k8s.io/utils/ptr"

	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
)

const (
	graphExactPathType    = "Exact"
	graphHostname         = "api.example.com"
	graphWildcardHostname = "*.example.com"
)

func TestBuildHTTPPreviewRequests(t *testing.T) {
	scope, owner := ownershipFixture()
	model := backendModel()
	got, err := BuildHTTPPreviewRequests(scope, owner, model)
	if err != nil {
		t.Fatal(err)
	}
	wantCapacity := HTTPPreviewCapacity{Profiles: 1, Endpoints: 1, OriginGroups: 1, Origins: 2, Routes: 1}
	if got.Capacity != wantCapacity || len(got.Backends.OriginGroups) != 1 ||
		got.Backends.ProfileName != got.Roots.ProfileName || got.RouteName == "" {
		t.Fatalf("invalid complete request graph: %#v", got)
	}
	if ptr.Deref(got.Roots.Endpoint.Properties.EnabledState, "") != armcdn.EnabledStateDisabled ||
		ptr.Deref(got.Route.Properties.EnabledState, "") != armcdn.EnabledStateDisabled {
		t.Fatal("initial request graph exposes traffic")
	}
	data, err := json.Marshal(got.Route)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]interface{}
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{
		"properties": map[string]interface{}{
			"enabledState": "Disabled", "forwardingProtocol": "HttpOnly",
			"httpsRedirect": "Disabled", "linkToDefaultDomain": "Enabled",
			"supportedProtocols": []interface{}{"Http"},
			"patternsToMatch":    []interface{}{"/*"},
			"originGroup": map[string]interface{}{
				"id": got.Roots.ProfileID + "/originGroups/" + got.Backends.OriginGroups[0].Name,
			},
		},
	}
	if !reflect.DeepEqual(wire, want) {
		t.Fatalf("wire route contains unexpected forwarding/caching/rewrite/domain settings: %s", data)
	}
	// All member origins remain private and the selected Service port is unchanged.
	for _, origin := range got.Backends.OriginGroups[0].Origins {
		properties := origin.Parameters.Properties
		if properties.SharedPrivateLinkResource == nil || properties.AzureOrigin != nil ||
			ptr.Deref(properties.HTTPPort, 0) != 8080 || ptr.Deref(properties.OriginHostHeader, changedValue) != "" {
			t.Fatalf("graph weakened backend policy: %#v", properties)
		}
	}
	*got.Route.Properties.OriginGroup.ID = changedValue
	*got.Route.Properties.PatternsToMatch[0] = changedValue
	*got.Backends.OriginGroups[0].Origins[0].Parameters.Properties.HostName = changedValue
	again, err := BuildHTTPPreviewRequests(scope, owner, model)
	if err != nil || !reflect.DeepEqual(model, backendModel()) ||
		strings.Contains(*again.Route.Properties.OriginGroup.ID, changedValue) ||
		*again.Route.Properties.PatternsToMatch[0] != httpPreviewPattern ||
		*again.Backends.OriginGroups[0].Origins[0].Parameters.Properties.HostName == changedValue {
		t.Fatalf("graph aliases input or another invocation: %#v, %v", again, err)
	}
}

func TestHTTPPreviewRoutingGate(t *testing.T) {
	tests := []struct {
		name   string
		change func(*gatewaymodel.GlobalGateway)
	}{
		{"no listener", func(m *gatewaymodel.GlobalGateway) { m.Listeners = nil }},
		{"multiple listeners", func(m *gatewaymodel.GlobalGateway) {
			m.Listeners = append(m.Listeners, gatewaymodel.Listener{Name: otherName, Protocol: "HTTP", Port: 80})
		}},
		{"HTTPS listener", func(m *gatewaymodel.GlobalGateway) { m.Listeners[0].Protocol = "HTTPS" }},
		{"nonstandard edge port", func(m *gatewaymodel.GlobalGateway) { m.Listeners[0].Port = 8080 }},
		{"exact listener hostname", func(m *gatewaymodel.GlobalGateway) {
			m.Listeners[0].Hostname = graphHostname
			m.Routes[0].Attachments[0].Hostnames = []string{graphHostname}
		}},
		{"wildcard listener hostname", func(m *gatewaymodel.GlobalGateway) {
			m.Listeners[0].Hostname = graphWildcardHostname
			m.Routes[0].Attachments[0].Hostnames = []string{graphWildcardHostname}
		}},
		{"exact route hostname", func(m *gatewaymodel.GlobalGateway) {
			m.Routes[0].Attachments[0].Hostnames = []string{graphHostname}
		}},
		{"wildcard route hostname", func(m *gatewaymodel.GlobalGateway) {
			m.Routes[0].Attachments[0].Hostnames = []string{graphWildcardHostname}
		}},
		{"Exact root is not a catch-all", func(m *gatewaymodel.GlobalGateway) {
			m.Routes[0].Rules[0].Matches = []gatewaymodel.HTTPMatch{{PathType: graphExactPathType, Path: "/"}}
		}},
		{"non-root prefix", func(m *gatewaymodel.GlobalGateway) {
			m.Routes[0].Rules[0].Matches = []gatewaymodel.HTTPMatch{{PathType: httpPreviewPathPrefix, Path: "/api"}}
		}},
		{"mixed-case exact path", func(m *gatewaymodel.GlobalGateway) {
			m.Routes[0].Rules[0].Matches = []gatewaymodel.HTTPMatch{{PathType: graphExactPathType, Path: "/Api"}}
		}},
		{"root and more-specific match", func(m *gatewaymodel.GlobalGateway) {
			m.Routes[0].Rules[0].Matches = []gatewaymodel.HTTPMatch{{PathType: httpPreviewPathPrefix, Path: "/"}, {PathType: graphExactPathType, Path: "/api"}}
		}},
		{"second logical backend", func(m *gatewaymodel.GlobalGateway) { appendBackend(m).Name = otherName }},
		{"second backend port", func(m *gatewaymodel.GlobalGateway) { appendBackend(m).Port++ }},
		{"request filter", func(m *gatewaymodel.GlobalGateway) {
			m.Routes[0].Rules[0].Filters = []gatewaymodel.HTTPFilter{{Type: "RequestRedirect"}}
		}},
		{"WAF intent", func(m *gatewaymodel.GlobalGateway) { m.WAFPolicyID = "declared-protection" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scope, owner := ownershipFixture()
			model := backendModel()
			tt.change(&model)
			if err := PreflightHTTPPreviewRouting(model); err == nil {
				t.Fatal("unsupported routing passed execution preflight")
			}
			got, err := BuildHTTPPreviewRequests(scope, owner, model)
			if err == nil || !reflect.DeepEqual(got, HTTPPreviewRequests{}) {
				t.Fatalf("unsupported intent returned a partial/wider graph: %#v, %v", got, err)
			}
		})
	}
}

func TestHTTPPreviewEquivalentBindingsDeduplicate(t *testing.T) {
	scope, owner := ownershipFixture()
	model := backendModel()
	appendBackend(&model)
	second := model.Routes[0]
	second.Name, second.UID = otherName, "second-route"
	model.Routes = append(model.Routes, second)
	got, err := BuildHTTPPreviewRequests(scope, owner, model)
	if err != nil || got.Capacity != (HTTPPreviewCapacity{Profiles: 1, Endpoints: 1, OriginGroups: 1, Origins: 2, Routes: 1}) {
		t.Fatalf("equivalent root bindings generated duplicate Azure resources: %#v, %v", got, err)
	}
	// Removing an equivalent binding does not change the Azure route or backend identity.
	model.Routes = model.Routes[1:]
	model.Routes[0].Rules = model.Routes[0].Rules[:1]
	again, err := BuildHTTPPreviewRequests(scope, owner, model)
	if err != nil || !reflect.DeepEqual(got, again) {
		t.Fatalf("equivalent binding withdrawal changed the graph: %#v, %v", again, err)
	}
	model.Routes = nil
	if err := PreflightHTTPPreviewRouting(model); err != nil {
		t.Fatalf("empty intent must remain valid for withdrawal: %v", err)
	}
	got, err = BuildHTTPPreviewRequests(scope, owner, model)
	if err == nil || !reflect.DeepEqual(got, HTTPPreviewRequests{}) {
		t.Fatalf("empty intent created an unused profile graph: %#v, %v", got, err)
	}
}

func TestHTTPPreviewRequestsOwnership(t *testing.T) {
	scope, owner := ownershipFixture()
	for _, field := range []string{"hub", "namespace", "name", "uid", "scope"} {
		t.Run(field, func(t *testing.T) {
			model := backendModel()
			targetScope, targetOwner := scope, owner
			switch field {
			case "hub":
				targetOwner.HubUID = ""
			case "namespace":
				model.Namespace = otherName
			case "name":
				model.Name = otherName
			case "uid":
				model.UID = otherName
			case "scope":
				targetScope.ResourceGroup = ""
			}
			got, err := BuildHTTPPreviewRequests(targetScope, targetOwner, model)
			if err == nil || !reflect.DeepEqual(got, HTTPPreviewRequests{}) {
				t.Fatalf("invalid identity returned graph requests: %#v, %v", got, err)
			}
		})
	}
	first, err := BuildHTTPPreviewRequests(scope, owner, backendModel())
	if err != nil {
		t.Fatal(err)
	}
	replacement := backendModel()
	replacement.UID, owner.UID = replacementUID, replacementUID
	second, err := BuildHTTPPreviewRequests(scope, owner, replacement)
	if err != nil || first.RouteName == second.RouteName || first.Roots.ProfileName == second.Roots.ProfileName {
		t.Fatalf("Gateway recreation reused old resources: %#v, %v", second, err)
	}
}

func TestHTTPPreviewGeneratedCapacityBoundaries(t *testing.T) {
	for _, count := range []int{1, 40, 41} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			scope, owner := ownershipFixture()
			model := backendModel()
			backend := &model.Routes[0].Rules[0].Backends[0]
			source := backend.Origins[0]
			backend.Origins = make([]gatewaymodel.Origin, count)
			for i := range backend.Origins {
				origin := source
				origin.Cluster = fmt.Sprintf("member-%d", i)
				origin.ServiceUID = fmt.Sprintf("source-%d", i)
				origin.Endpoint = fmt.Sprintf("10.0.0.%d", i+1)
				backend.Origins[i] = origin
			}
			got, err := BuildHTTPPreviewRequests(scope, owner, model)
			if count > 40 {
				if err == nil || !reflect.DeepEqual(got, HTTPPreviewRequests{}) {
					t.Fatalf("over-limit graph returned usable requests: %#v, %v", got, err)
				}
				return
			}
			if err != nil || got.Capacity != (HTTPPreviewCapacity{
				Profiles: 1, Endpoints: 1, OriginGroups: 1, Origins: count, Routes: 1,
			}) || len(got.Backends.OriginGroups[0].Origins) != count {
				t.Fatalf("generated capacity does not match requests: %#v, %v", got, err)
			}
		})
	}
}
