/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package frontdoor

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
)

func requestProperties(t *testing.T, value any) map[string]json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil || envelope.Properties == nil {
		t.Fatalf("missing SDK properties: %s, %v", data, err)
	}
	return envelope.Properties
}

func requireJSON(t *testing.T, properties map[string]json.RawMessage, key string, want any) {
	t.Helper()
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if string(properties[key]) != string(data) {
		t.Fatalf("%s = %s, want %s", key, properties[key], data)
	}
}

func TestBuildBackendRequestsSerializesPrivateHTTP(t *testing.T) {
	model := backendModel()
	plan, err := PlanBackends(model)
	if err != nil {
		t.Fatal(err)
	}
	got, err := BuildBackendRequests(model)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProfileName != plan.ProfileName || len(got.OriginGroups) != len(plan.OriginGroups) {
		t.Fatalf("SDK inventory differs from preflight: %#v", got)
	}
	for i, group := range got.OriginGroups {
		wantGroup := plan.OriginGroups[i]
		if group.Name != wantGroup.Name || len(group.Origins) != len(wantGroup.Origins) {
			t.Fatalf("SDK group differs from inventory: %#v", group)
		}
		properties := requestProperties(t, group.Parameters)
		requireJSON(t, properties, "sessionAffinityState", "Disabled")
		requireJSON(t, properties, "healthProbeSettings", map[string]any{
			"probeIntervalInSeconds": 30, "probePath": "/", "probeProtocol": "Http", "probeRequestType": "GET",
		})
		for j, origin := range group.Origins {
			want := wantGroup.Origins[j]
			if origin.Name != want.Name {
				t.Fatalf("SDK origin lost stable identity: %#v", origin)
			}
			properties := requestProperties(t, origin.Parameters)
			requireJSON(t, properties, "hostName", want.HostName)
			requireJSON(t, properties, "httpPort", want.HTTPPort)
			requireJSON(t, properties, "priority", 1)
			requireJSON(t, properties, "weight", want.Weight)
			requireJSON(t, properties, "originHostHeader", "")
			requireJSON(t, properties, "enforceCertificateNameCheck", true)
			requireJSON(t, properties, "enabledState", "Enabled")
			for _, field := range []string{"azureOrigin", "httpsPort", "provisioningState", "deploymentStatus"} {
				if _, found := properties[field]; found {
					t.Fatalf("unexpected request field %q: %s", field, properties[field])
				}
			}
			var shared map[string]json.RawMessage
			if err := json.Unmarshal(properties["sharedPrivateLinkResource"], &shared); err != nil {
				t.Fatal(err)
			}
			requireJSON(t, shared, "privateLink", map[string]string{"id": want.PrivateLink.ResourceID})
			requireJSON(t, shared, "privateLinkLocation", want.PrivateLink.Location)
			// Custom PLS uses no target group ID; status comes from Azure, never desired intent.
			for _, field := range []string{"groupId", "status"} {
				if _, found := shared[field]; found {
					t.Fatalf("unexpected private link request field %q: %s", field, shared[field])
				}
			}
			var message string
			if err := json.Unmarshal(shared["requestMessage"], &message); err != nil || !strings.Contains(message, plan.ProfileName) {
				t.Fatalf("approval request has no profile context: %q, %v", message, err)
			}
		}
	}
}

func TestBuildBackendRequestsRejectsUnsafeIntent(t *testing.T) {
	tests := []struct {
		name   string
		change func(*gatewaymodel.GlobalGateway)
	}{
		{"missing Gateway UID", func(m *gatewaymodel.GlobalGateway) { m.UID = "" }},
		{"no private target", func(m *gatewaymodel.GlobalGateway) {
			m.Routes[0].Rules[0].Backends[0].Origins[0].PrivateLinkResourceID = ""
		}},
		{"shared target different ports", func(m *gatewaymodel.GlobalGateway) { appendBackend(m).Port++ }},
		{"capacity overflow", func(m *gatewaymodel.GlobalGateway) { *m = capacityModel(101, 1) }},
		{"unimplemented WAF protection", func(m *gatewaymodel.GlobalGateway) {
			m.WAFPolicyID = "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Network/frontdoorWebApplicationFirewallPolicies/waf"
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := backendModel()
			tt.change(&model)
			got, err := BuildBackendRequests(model)
			if err == nil || !reflect.DeepEqual(got, BackendRequests{}) {
				t.Fatalf("unsafe input returned SDK requests: %#v, %v", got, err)
			}
			if tt.name == "shared target different ports" {
				var conflict *PrivateLinkPortConflictError
				if !errors.As(err, &conflict) {
					t.Fatalf("lost typed preflight error: %v", err)
				}
			}
		})
	}
}

func TestBuildBackendRequestsDoesNotAliasInputOrOtherCalls(t *testing.T) {
	model, before := backendModel(), backendModel()
	first, err := BuildBackendRequests(model)
	if err != nil {
		t.Fatal(err)
	}

	*first.OriginGroups[0].Parameters.Properties.HealthProbeSettings.ProbePath = "/changed"
	*first.OriginGroups[0].Origins[0].Parameters.Properties.SharedPrivateLinkResource.PrivateLink.ID = changedValue
	second, err := BuildBackendRequests(model)
	if err != nil || !reflect.DeepEqual(model, before) ||
		*second.OriginGroups[0].Parameters.Properties.HealthProbeSettings.ProbePath != "/" ||
		*second.OriginGroups[0].Origins[0].Parameters.Properties.SharedPrivateLinkResource.PrivateLink.ID == changedValue {
		t.Fatalf("SDK requests alias input or another invocation: %v", err)
	}
}

func TestBuildBackendRequestsWeightBoundaries(t *testing.T) {
	for _, weight := range []int64{-1, 0, 1, 1000, 1001, 1 << 62} {
		t.Run(fmt.Sprintf("weight=%d", weight), func(t *testing.T) {
			model := backendModel()
			for i := range model.Routes[0].Rules[0].Backends[0].Origins {
				model.Routes[0].Rules[0].Backends[0].Origins[i].Weight = weight
			}
			requests, err := BuildBackendRequests(model)
			if weight < 1 || weight > 1000 {
				if err == nil || !reflect.DeepEqual(requests, BackendRequests{}) {
					t.Fatalf("invalid weight produced requests: %#v, %v", requests, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, origin := range requests.OriginGroups[0].Origins {
				if int64(*origin.Parameters.Properties.Weight) != weight {
					t.Fatalf("SDK weight conversion changed %d", weight)
				}
			}
		})
	}
}
