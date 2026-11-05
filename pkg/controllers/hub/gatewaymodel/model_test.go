/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gatewaymodel

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func privateModelOrigin(cluster string, weight int64) Origin {
	return Origin{
		Cluster: cluster, Weight: weight, ServiceUID: "service-" + cluster, Endpoint: "10.0.0.10",
		PrivateLinkResourceID: "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Network/privateLinkServices/" + cluster,
		PrivateLinkLocation:   "eastus",
	}
}

func TestNormalize_SortsNestedResources(t *testing.T) {
	model := ruleModel()
	model.Listeners = append([]Listener{{Name: "z-listener", Protocol: "HTTP", Port: 80}}, model.Listeners...)
	model.Routes[0].Name = "z-route"
	model.Routes[0].Rules[0].Backends[0].Origins = []Origin{privateModelOrigin("west", 20), privateModelOrigin("east", 10)}
	second := ruleModel().Routes[0]
	second.Name = "a-route"
	model.Routes = append(model.Routes, second)
	got, err := Normalize(model)
	if err != nil {
		t.Fatal(err)
	}
	if got.Listeners[0].Name != "http" || got.Routes[0].Name != "a-route" || got.Routes[1].Name != "z-route" {
		t.Fatalf("non-deterministic resource order: %#v", got)
	}
	origins := got.Routes[1].Rules[0].Backends[0].Origins
	if origins[0].Cluster != "east" || origins[1].Cluster != "west" {
		t.Fatalf("origin order = %v", origins)
	}
	if reflect.DeepEqual(model, got) || model.Listeners[0].Name != "z-listener" ||
		model.Routes[0].Rules[0].Backends[0].Origins[0].Cluster != "west" {
		t.Fatal("Normalize did not copy and order input")
	}
}

func TestNormalizeCreationTimeBeforeName(t *testing.T) {
	model := ruleModel()
	older := ruleModel().Routes[0]
	older.Name = "z-older"
	older.CreationTimestamp = time.Unix(1, 0).UTC()
	model.Routes = append(model.Routes, older)
	got, err := Normalize(model)
	if err != nil || got.Routes[0].Name != "z-older" {
		t.Fatalf("oldest route tie-break metadata lost: %#v, %v", got.Routes, err)
	}
}

func TestNormalize_RejectsInvalidModels(t *testing.T) {
	tests := []struct {
		name    string
		change  func(*GlobalGateway)
		wantErr string
	}{
		{"missing Gateway identity", func(m *GlobalGateway) { m.Namespace = "" }, "Gateway identity"},
		{"duplicate listener", func(m *GlobalGateway) { m.Listeners = append(m.Listeners, m.Listeners[0]) }, "duplicate listener"},
		{"missing listener identity", func(m *GlobalGateway) { m.Listeners[0].Name = "" }, "listener name"},
		{"duplicate route", func(m *GlobalGateway) { m.Routes = append(m.Routes, m.Routes[0]) }, "duplicate route"},
		{"missing route identity", func(m *GlobalGateway) { m.Routes[0].Name = "" }, "route identity"},
		{"duplicate backend", func(m *GlobalGateway) {
			m.Routes[0].Rules[0].Backends = append(m.Routes[0].Rules[0].Backends, m.Routes[0].Rules[0].Backends[0])
		}, "duplicate backend"},
		{"missing backend identity", func(m *GlobalGateway) { m.Routes[0].Rules[0].Backends[0].Name = "" }, "backend identity"},
		{"invalid backend port", func(m *GlobalGateway) { m.Routes[0].Rules[0].Backends[0].Port = 70000 }, "port"},
		{"invalid route weight", func(m *GlobalGateway) { m.Routes[0].Rules[0].Backends[0].RouteWeight = 1000001 }, "weight"},
		{"zero route weight is not approximated", func(m *GlobalGateway) { m.Routes[0].Rules[0].Backends[0].RouteWeight = 0 }, "weight"},
		{"duplicate origin", func(m *GlobalGateway) {
			backend := &m.Routes[0].Rules[0].Backends[0]
			backend.Origins = append(backend.Origins, backend.Origins[0])
		}, "duplicate origin"},
		{"invalid origin weight", func(m *GlobalGateway) { m.Routes[0].Rules[0].Backends[0].Origins[0].Weight = 1001 }, "weight"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := ruleModel()
			tt.change(&model)
			if _, err := Normalize(model); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestOriginValidatePrivateContract(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Origin)
	}{
		{"missing cluster", func(o *Origin) { o.Cluster = "" }},
		{"missing source UID", func(o *Origin) { o.ServiceUID = "" }},
		{"negative weight", func(o *Origin) { o.Weight = -1 }},
		{"excessive weight", func(o *Origin) { o.Weight = 1001 }},
		{"hostname endpoint", func(o *Origin) { o.Endpoint = "example.com" }},
		{"loopback endpoint", func(o *Origin) { o.Endpoint = "127.0.0.1" }},
		{"IPv6 endpoint", func(o *Origin) { o.Endpoint = "fd00::1" }},
		{"missing PLS ID", func(o *Origin) { o.PrivateLinkResourceID = "" }},
		{"wrong Azure resource type", func(o *Origin) {
			o.PrivateLinkResourceID = strings.Replace(o.PrivateLinkResourceID, "privateLinkServices", "publicIPAddresses", 1)
		}},
		{"missing location", func(o *Origin) { o.PrivateLinkLocation = " " }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origin := privateModelOrigin("east", 1)
			tt.change(&origin)
			if err := origin.Validate(); err == nil {
				t.Fatalf("invalid private origin accepted: %#v", origin)
			}
			model := ruleModel()
			model.Routes[0].Rules[0].Backends[0].Origins = []Origin{origin}
			if _, err := Normalize(model); err == nil {
				t.Fatal("Normalize bypassed private-origin validation")
			}
		})
	}
}
