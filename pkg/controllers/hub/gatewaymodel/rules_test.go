/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gatewaymodel

import (
	"reflect"
	"testing"
	"time"
)

func ruleModel() GlobalGateway {
	return GlobalGateway{
		Namespace: "store", Name: "global", UID: "gateway-uid",
		Listeners: []Listener{{Name: "http", Protocol: "HTTP", Port: 80, Hostname: "*.example.com"}},
		Routes: []Route{{
			Namespace: "store", Name: "api", UID: "route-uid", CreationTimestamp: time.Unix(100, 0).UTC(),
			Attachments: []RouteAttachment{{Listener: "http", Hostnames: []string{"store.example.com"}}},
			Rules: []HTTPRule{{
				Matches: []HTTPMatch{{PathType: "Exact", Path: "/api"}},
				Backends: []Backend{{
					Namespace: "store", Name: "api", Port: 8080, RouteWeight: 1,
					HealthProbe: HTTPPreviewHealthProbe(), Origins: []Origin{privateModelOrigin("east", 1)},
				}},
			}},
		}},
	}
}

func TestNormalizePreservesRulesAndCopiesAllSlices(t *testing.T) {
	model := ruleModel()
	second := model.Routes[0].Rules[0]
	second.Backends = append([]Backend(nil), second.Backends...)
	second.Backends[0].Name = "second"
	second.Matches = []HTTPMatch{{PathType: pathTypePrefix, Path: "/"}}
	model.Routes[0].Rules = append(model.Routes[0].Rules, second)
	got, err := Normalize(model)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Routes[0].Rules) != 2 || got.Routes[0].Rules[0].Backends[0].Name != "api" ||
		got.Routes[0].Rules[1].Backends[0].Name != "second" || got.Routes[0].UID != "route-uid" ||
		got.Routes[0].CreationTimestamp != model.Routes[0].CreationTimestamp {
		t.Fatalf("rule boundaries/order or precedence metadata lost: %#v", got.Routes[0])
	}
	got.Routes[0].Attachments[0].Hostnames[0] = "changed.example.com"
	got.Routes[0].Rules[0].Matches[0].Path = "/changed"
	got.Routes[0].Rules[0].Backends[0].Origins[0].Endpoint = "10.0.0.99"
	if model.Routes[0].Attachments[0].Hostnames[0] != "store.example.com" ||
		model.Routes[0].Rules[0].Matches[0].Path != "/api" ||
		model.Routes[0].Rules[0].Backends[0].Origins[0].Endpoint != "10.0.0.10" {
		t.Fatal("normalized model aliases its input")
	}
}

func TestNormalizeEquivalentRuleDefaults(t *testing.T) {
	left := ruleModel()
	left.Routes[0].Rules[0].Matches = nil
	right := ruleModel()
	right.Routes[0].Rules[0].Matches = []HTTPMatch{{PathType: pathTypePrefix, Path: "/"}}
	a, err := Normalize(left)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Normalize(right)
	if err != nil || !reflect.DeepEqual(a, b) {
		t.Fatalf("default intent differs: %#v, %#v, %v", a, b, err)
	}
}

func TestNormalizeCanonicalMatchSets(t *testing.T) {
	left := ruleModel()
	left.Routes[0].CreationTimestamp = left.Routes[0].CreationTimestamp.In(time.FixedZone("offset", 3600))
	left.Routes[0].Rules[0].Matches = []HTTPMatch{
		{Path: "/prefix/"}, {PathType: "Exact", Path: "/exact/"}, {PathType: pathTypePrefix, Path: "/prefix"},
	}
	right := ruleModel()
	right.Routes[0].Rules[0].Matches = []HTTPMatch{
		{PathType: pathTypePrefix, Path: "/prefix"}, {PathType: "Exact", Path: "/exact/"},
	}
	a, err := Normalize(left)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Normalize(right)
	if err != nil || !reflect.DeepEqual(a, b) {
		t.Fatalf("equivalent match sets differ: %#v, %#v, %v", a, b, err)
	}
	if a.Routes[0].Rules[0].Matches[0].Path != "/exact/" {
		t.Fatal("Exact path trailing slash was changed")
	}
}

func TestNormalizeRejectsUnsupportedOrIncompleteRules(t *testing.T) {
	tests := []struct {
		name   string
		change func(*GlobalGateway)
	}{
		{"HTTPS listener", func(m *GlobalGateway) { m.Listeners[0].Protocol = "HTTPS" }},
		{"nonstandard HTTP edge port", func(m *GlobalGateway) { m.Listeners[0].Port = 8080 }},
		{"unknown attachment", func(m *GlobalGateway) { m.Routes[0].Attachments[0].Listener = "other" }},
		{"hostname outside listener", func(m *GlobalGateway) { m.Routes[0].Attachments[0].Hostnames = []string{"other.test"} }},
		{"lost listener hostname constraint", func(m *GlobalGateway) { m.Routes[0].Attachments[0].Hostnames = nil }},
		{"missing attachment", func(m *GlobalGateway) { m.Routes[0].Attachments = nil }},
		{"missing rules", func(m *GlobalGateway) { m.Routes[0].Rules = nil }},
		{"regex match", func(m *GlobalGateway) { m.Routes[0].Rules[0].Matches[0].PathType = "RegularExpression" }},
		{"method match", func(m *GlobalGateway) { m.Routes[0].Rules[0].Matches[0].Method = "GET" }},
		{"relative path", func(m *GlobalGateway) { m.Routes[0].Rules[0].Matches[0].Path = "api" }},
		{"invalid prefix is not repaired", func(m *GlobalGateway) {
			m.Routes[0].Rules[0].Matches[0] = HTTPMatch{PathType: pathTypePrefix, Path: "/api//"}
		}},
		{"filter", func(m *GlobalGateway) { m.Routes[0].Rules[0].Filters = []HTTPFilter{{Type: "RequestRedirect"}} }},
		{"multiple logical backends", func(m *GlobalGateway) {
			backend := m.Routes[0].Rules[0].Backends[0]
			backend.Name = "other"
			m.Routes[0].Rules[0].Backends = append(m.Routes[0].Rules[0].Backends, backend)
		}},
		{"missing origins", func(m *GlobalGateway) { m.Routes[0].Rules[0].Backends[0].Origins = nil }},
		{"unconfigured probe", func(m *GlobalGateway) { m.Routes[0].Rules[0].Backends[0].HealthProbe = HealthProbe{} }},
		{"host override", func(m *GlobalGateway) { m.Routes[0].Rules[0].Backends[0].OriginHostHeader = "other.example.com" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := ruleModel()
			tt.change(&model)
			if _, err := Normalize(model); err == nil {
				t.Fatal("unsupported/incomplete intent accepted")
			}
		})
	}
}

func TestIntersectHostnames(t *testing.T) {
	tests := []struct {
		name     string
		listener string
		routes   []string
		want     []string
		matches  bool
	}{
		{"unconstrained", "", nil, nil, true},
		{"listener only", "store.example.com", nil, []string{"store.example.com"}, true},
		{"route only and deduplication", "", []string{"b.example.com", "a.example.com", "b.example.com"}, []string{"a.example.com", "b.example.com"}, true},
		{"exact", "store.example.com", []string{"store.example.com"}, []string{"store.example.com"}, true},
		{"wildcard excludes apex", "*.example.com", []string{"example.com"}, nil, false},
		{"wildcard covers multiple labels", "*.example.com", []string{"a.b.example.com"}, []string{"a.b.example.com"}, true},
		{"narrow wildcard intersection", "*.example.com", []string{"*.sub.example.com"}, []string{"*.sub.example.com"}, true},
		{"route wildcard", "a.sub.example.com", []string{"*.example.com"}, []string{"a.sub.example.com"}, true},
		{"unrelated", "*.example.com", []string{"a.test"}, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, matches, err := IntersectHostnames(tt.listener, tt.routes)
			if err != nil || matches != tt.matches || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("intersection = %v, %v, %v; want %v, %v", got, matches, err, tt.want, tt.matches)
			}
		})
	}
	for _, invalid := range []string{"*", "EXAMPLE.com", "127.0.0.1", "a.*.example.com", "example.com:80"} {
		t.Run(invalid, func(t *testing.T) {
			if _, _, err := IntersectHostnames(invalid, nil); err == nil {
				t.Fatal("invalid hostname accepted")
			}
		})
	}
}
