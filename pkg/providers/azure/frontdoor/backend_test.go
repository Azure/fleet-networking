/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package frontdoor

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
)

const (
	changedValue   = "changed"
	otherName      = "other"
	replacementUID = "replacement-uid"
	testAPIVersion = "2024-02-01"
)

func backendModel() gatewaymodel.GlobalGateway {
	return gatewaymodel.GlobalGateway{
		Namespace: "store", Name: "global", UID: "gateway-uid",
		Listeners: []gatewaymodel.Listener{{Name: "http", Protocol: "HTTP", Port: 80}},
		Routes: []gatewaymodel.Route{{
			Namespace: "store", Name: "api", UID: "route-uid",
			Attachments: []gatewaymodel.RouteAttachment{{Listener: "http"}},
			Rules: []gatewaymodel.HTTPRule{{Backends: []gatewaymodel.Backend{{
				Namespace: "store", Name: "api", Port: 8080, RouteWeight: 1,
				HealthProbe: gatewaymodel.HTTPPreviewHealthProbe(),
				Origins: []gatewaymodel.Origin{
					{Cluster: "west", Endpoint: "10.0.0.10", ServiceUID: "service-west", Weight: 100,
						PrivateLinkResourceID: "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg/providers/Microsoft.Network/privateLinkServices/west", PrivateLinkLocation: "eastus"},
					{Cluster: "east", Endpoint: "10.0.0.20", ServiceUID: "service-east", Weight: 200,
						PrivateLinkResourceID: "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg/providers/Microsoft.Network/privateLinkServices/east", PrivateLinkLocation: "eastus"},
				},
			}}}},
		}},
	}
}

func appendBackend(model *gatewaymodel.GlobalGateway) *gatewaymodel.Backend {
	source := model.Routes[0].Rules[0].Backends[0]
	source.Origins = append([]gatewaymodel.Origin(nil), source.Origins...)
	model.Routes[0].Rules = append(model.Routes[0].Rules, gatewaymodel.HTTPRule{Backends: []gatewaymodel.Backend{source}})
	return &model.Routes[0].Rules[len(model.Routes[0].Rules)-1].Backends[0]
}

func TestPlanBackendsPrivateResourceShape(t *testing.T) {
	model := backendModel()
	before := backendModel()
	got, err := PlanBackends(model)
	if err != nil {
		t.Fatal(err)
	}
	legalName := regexp.MustCompile(`^[a-z][a-z0-9-]{0,45}$`)
	if !legalName.MatchString(got.ProfileName) || !legalName.MatchString(got.EndpointName) || got.ProfileName == got.EndpointName {
		t.Fatalf("invalid root names: %#v", got)
	}
	if got.Capacity != (gatewaymodel.BackendCapacity{ServiceImports: 1, OriginGroups: 1, Origins: 2}) ||
		len(got.OriginGroups) != 1 || len(got.PrivateLinks) != 2 {
		t.Fatalf("incorrect inventory: %#v", got)
	}
	group := got.OriginGroups[0]
	if group.Backend != (BackendKey{Namespace: "store", Name: "api", Port: 8080}) ||
		group.HealthProbe != gatewaymodel.HTTPPreviewHealthProbe() || !legalName.MatchString(group.Name) || len(group.Origins) != 2 {
		t.Fatalf("incorrect origin group: %#v", group)
	}
	assertPrivateOrigins(t, model, group, legalName)
	for _, target := range got.PrivateLinks {
		if target.HTTPPort != 8080 || len(target.Origins) != 1 || target.Origins[0].OriginGroup != group.Name ||
			target.Target.ResourceID == "" || target.Target.Location != "eastus" {
			t.Fatalf("incorrect shared-target references: %#v", target)
		}
		found := false
		for _, origin := range group.Origins {
			if origin.Name == target.Origins[0].Origin && origin.PrivateLink == target.Target {
				found = true
			}
		}
		if !found {
			t.Fatalf("shared target does not reference its origin: %#v", target)
		}
	}
	if !reflect.DeepEqual(before, model) {
		t.Fatal("source model changed")
	}
	got.OriginGroups[0].Origins[0].HostName = changedValue
	got.PrivateLinks[0].Origins[0].OriginGroup = changedValue
	again, err := PlanBackends(model)
	if err != nil || again.OriginGroups[0].Origins[0].HostName == changedValue || again.PrivateLinks[0].Origins[0].OriginGroup == changedValue {
		t.Fatalf("result aliases source or another invocation: %v", err)
	}
}

func assertPrivateOrigins(t *testing.T, model gatewaymodel.GlobalGateway, group OriginGroup, legalName *regexp.Regexp) {
	t.Helper()
	for _, origin := range group.Origins {
		var member gatewaymodel.Origin
		for _, source := range model.Routes[0].Rules[0].Backends[0].Origins {
			if source.Cluster == origin.Cluster {
				member = source
			}
		}
		if !legalName.MatchString(origin.Name) || origin.HTTPPort != 8080 || origin.Priority != 1 ||
			origin.OriginHostHeader != "" || !origin.EnforceCertificateNameCheck || origin.PrivateLink.ResourceID == "" ||
			origin.PrivateLink.Location != "eastus" || origin.HostName != member.Endpoint ||
			origin.ServiceUID != member.ServiceUID || origin.Weight != member.Weight || member.Cluster == "" {
			t.Fatalf("lost private HTTP policy: %#v", origin)
		}
	}
}

func TestPlanBackendsDeduplicatesCanonicalIntent(t *testing.T) {
	model := backendModel()
	want, err := PlanBackends(model)
	if err != nil {
		t.Fatal(err)
	}
	duplicate := appendBackend(&model)
	duplicate.RouteWeight = 1000
	for i := range duplicate.Origins {
		duplicate.Origins[i].PrivateLinkResourceID = strings.ToUpper(duplicate.Origins[i].PrivateLinkResourceID)
		duplicate.Origins[i].PrivateLinkLocation = " EASTUS "
	}
	duplicate.Origins[0], duplicate.Origins[1] = duplicate.Origins[1], duplicate.Origins[0]
	other := model.Routes[0]
	other.Name, other.UID = otherName, "other-route"
	model.Routes = append(model.Routes, other)
	got, err := PlanBackends(model)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("equivalent repeated backend intent changed inventory:\n%#v\nwant %#v\nerror %v", got, want, err)
	}
}

func TestPlanBackendsSharedPrivateLinkPorts(t *testing.T) {
	tests := []struct {
		name     string
		change   func(*gatewaymodel.Backend)
		conflict bool
		targets  int
	}{
		{"same backend and port", func(*gatewaymodel.Backend) {}, false, 2},
		{"different service same port", func(b *gatewaymodel.Backend) { b.Name = otherName }, false, 2},
		{"same service different port", func(b *gatewaymodel.Backend) { b.Port++ }, true, 0},
		{"different services different ports", func(b *gatewaymodel.Backend) { b.Name = otherName; b.Port++ }, true, 0},
		{"resource casing cannot bypass port restriction", func(b *gatewaymodel.Backend) {
			b.Port++
			for i := range b.Origins {
				b.Origins[i].PrivateLinkResourceID = strings.ToUpper(b.Origins[i].PrivateLinkResourceID)
				b.Origins[i].PrivateLinkLocation = "EASTUS"
			}
		}, true, 0},
		{"different target resources", func(b *gatewaymodel.Backend) {
			b.Port++
			for i := range b.Origins {
				b.Origins[i].PrivateLinkResourceID += "-other"
			}
		}, false, 4},
		{"different private link regions", func(b *gatewaymodel.Backend) {
			b.Port++
			for i := range b.Origins {
				b.Origins[i].PrivateLinkLocation = "westeurope"
			}
		}, false, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := backendModel()
			tt.change(appendBackend(&model))
			got, err := PlanBackends(model)
			var conflict *PrivateLinkPortConflictError
			if tt.conflict {
				if !errors.As(err, &conflict) || conflict.FirstPort == conflict.SecondPort ||
					conflict.Target.ResourceID == "" || !reflect.DeepEqual(got, BackendPlan{}) {
					t.Fatalf("unsafe or partial plan accepted: %#v, %v", got, err)
				}
				model.Routes[0].Rules[0], model.Routes[0].Rules[1] = model.Routes[0].Rules[1], model.Routes[0].Rules[0]
				_, reversedErr := PlanBackends(model)
				if reversedErr == nil || reversedErr.Error() != err.Error() {
					t.Fatalf("conflict depends on rule order: %v versus %v", err, reversedErr)
				}
				return
			}
			if err != nil || len(got.PrivateLinks) != tt.targets {
				t.Fatalf("shared private links = %#v, error %v", got.PrivateLinks, err)
			}
			if tt.name == "different service same port" {
				for _, target := range got.PrivateLinks {
					if len(target.Origins) != 2 {
						t.Fatalf("shared approval lost referring origins: %#v", target)
					}
				}
			}
		})
	}
}

func TestPlanBackendsNamesTrackResourceIdentity(t *testing.T) {
	tests := []struct {
		name          string
		change        func(*gatewaymodel.GlobalGateway)
		rootChanges   bool
		groupChanges  bool
		originChanges bool
	}{
		{"weight change", func(m *gatewaymodel.GlobalGateway) { m.Routes[0].Rules[0].Backends[0].Origins[0].Weight++ }, false, false, false},
		{"source Service replacement", func(m *gatewaymodel.GlobalGateway) {
			m.Routes[0].Rules[0].Backends[0].Origins[0].ServiceUID = "replacement"
		}, false, false, true},
		{"PLS replacement", func(m *gatewaymodel.GlobalGateway) {
			m.Routes[0].Rules[0].Backends[0].Origins[0].PrivateLinkResourceID += "-replacement"
		}, false, false, true},
		{"Private Link region change", func(m *gatewaymodel.GlobalGateway) {
			m.Routes[0].Rules[0].Backends[0].Origins[0].PrivateLinkLocation = "westeurope"
		}, false, false, true},
		{"internal frontend replacement", func(m *gatewaymodel.GlobalGateway) {
			m.Routes[0].Rules[0].Backends[0].Origins[0].Endpoint = "10.0.0.30"
		}, false, false, true},
		{"backend port change", func(m *gatewaymodel.GlobalGateway) { m.Routes[0].Rules[0].Backends[0].Port++ }, false, true, true},
		{"Gateway recreation", func(m *gatewaymodel.GlobalGateway) { m.UID = "replacement" }, true, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := backendModel()
			before, err := PlanBackends(model)
			if err != nil {
				t.Fatal(err)
			}
			tt.change(&model)
			after, err := PlanBackends(model)
			if err != nil {
				t.Fatal(err)
			}
			if (before.ProfileName != after.ProfileName) != tt.rootChanges || (before.EndpointName != after.EndpointName) != tt.rootChanges ||
				(before.OriginGroups[0].Name != after.OriginGroups[0].Name) != tt.groupChanges {
				t.Fatalf("resource identity changed incorrectly: before=%#v after=%#v", before, after)
			}
			oldName, newName := "", ""
			for _, origin := range before.OriginGroups[0].Origins {
				if origin.Cluster == "west" {
					oldName = origin.Name
				}
			}
			for _, origin := range after.OriginGroups[0].Origins {
				if origin.Cluster == "west" {
					newName = origin.Name
				}
			}
			if oldName == "" || newName == "" || (oldName != newName) != tt.originChanges {
				t.Fatalf("origin identity changed incorrectly: %q -> %q", oldName, newName)
			}
		})
	}
}

func TestPlanBackendsRejectsIncompleteOrConflictingInput(t *testing.T) {
	tests := []struct {
		name   string
		change func(*gatewaymodel.GlobalGateway)
	}{
		{"missing Gateway UID", func(m *gatewaymodel.GlobalGateway) { m.UID = "" }},
		{"blank Gateway UID", func(m *gatewaymodel.GlobalGateway) { m.UID = " \t" }},
		{"missing PLS", func(m *gatewaymodel.GlobalGateway) {
			m.Routes[0].Rules[0].Backends[0].Origins[0].PrivateLinkResourceID = ""
		}},
		{"unsupported probe override", func(m *gatewaymodel.GlobalGateway) { m.Routes[0].Rules[0].Backends[0].HealthProbe.Path = "/custom" }},
		{"unsupported Host override", func(m *gatewaymodel.GlobalGateway) {
			m.Routes[0].Rules[0].Backends[0].OriginHostHeader = "internal.example.com"
		}},
		{"zero-weight member", func(m *gatewaymodel.GlobalGateway) { m.Routes[0].Rules[0].Backends[0].Origins[0].Weight = 0 }},
		{"same backend inconsistent origins", func(m *gatewaymodel.GlobalGateway) { appendBackend(m).Origins[0].Weight++ }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := backendModel()
			tt.change(&model)
			got, err := PlanBackends(model)
			if err == nil || !reflect.DeepEqual(got, BackendPlan{}) {
				t.Fatalf("invalid input returned a partial/success plan: %#v, %v", got, err)
			}
		})
	}
}

func TestPlanBackendsCanonicalizationDoesNotMutateSource(t *testing.T) {
	model, before := backendModel(), backendModel()
	for _, source := range []*gatewaymodel.GlobalGateway{&model, &before} {
		for i := range source.Routes[0].Rules[0].Backends[0].Origins {
			origin := &source.Routes[0].Rules[0].Backends[0].Origins[i]
			origin.PrivateLinkResourceID = strings.ToUpper(origin.PrivateLinkResourceID)
			origin.PrivateLinkLocation = " EASTUS "
		}
	}
	want, err := PlanBackends(backendModel())
	if err != nil {
		t.Fatal(err)
	}
	got, err := PlanBackends(model)
	if err != nil || !reflect.DeepEqual(got, want) || !reflect.DeepEqual(model, before) {
		t.Fatalf("canonicalization changed intent or source: %#v, %v", got, err)
	}
}

func TestPlanBackendsEmptyGateway(t *testing.T) {
	model := backendModel()
	model.Routes = nil
	got, err := PlanBackends(model)
	if err != nil || got.ProfileName == "" || got.EndpointName == "" ||
		got.Capacity != (gatewaymodel.BackendCapacity{}) || len(got.OriginGroups) != 0 || len(got.PrivateLinks) != 0 {
		t.Fatalf("empty Gateway has backend resources: %#v, %v", got, err)
	}
}

func TestResourceNameSeparatesIdentityParts(t *testing.T) {
	if resourceName("fleet-og", "a", "bc") == resourceName("fleet-og", "ab", "c") ||
		resourceName("fleet-og", "a", "b") == resourceName("fleet-og", "a:b") {
		t.Fatal("ambiguous identity parts share a resource name")
	}
}

func capacityModel(groups, origins int) gatewaymodel.GlobalGateway {
	model := backendModel()
	base := model.Routes[0].Rules[0].Backends[0]
	model.Routes[0].Rules = nil
	for i := 0; i < groups; i++ {
		backend := base
		backend.Name = fmt.Sprintf("api-%d", i)
		backend.Origins = nil
		for j := 0; j < origins; j++ {
			origin := base.Origins[0]
			origin.Cluster, origin.ServiceUID = fmt.Sprintf("member-%d", j), fmt.Sprintf("service-%d-%d", i, j)
			origin.PrivateLinkResourceID += fmt.Sprintf("-%d", j)
			backend.Origins = append(backend.Origins, origin)
		}
		model.Routes[0].Rules = append(model.Routes[0].Rules, gatewaymodel.HTTPRule{Backends: []gatewaymodel.Backend{backend}})
	}
	return model
}

func TestPlanBackendsCapacityBoundaries(t *testing.T) {
	tests := []struct {
		name   string
		model  gatewaymodel.GlobalGateway
		reject bool
	}{
		{"100 groups", capacityModel(100, 1), false},
		{"101 groups", capacityModel(101, 1), true},
		{"40 origins per group", capacityModel(1, 40), false},
		{"41 origins per group", capacityModel(1, 41), true},
		{"160 total origins", capacityModel(4, 40), false},
		{"161 total origins", capacityModel(4, 40), true},
	}
	extra := appendBackend(&tests[len(tests)-1].model)
	extra.Name = "extra"
	extra.Origins = extra.Origins[:1]
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := PlanBackends(tt.model)
			if (err != nil) != tt.reject || (tt.reject && !reflect.DeepEqual(got, BackendPlan{})) {
				t.Fatalf("capacity rejection=%v: %#v, %v", tt.reject, got.Capacity, err)
			}
			if !tt.reject {
				groups := len(tt.model.Routes[0].Rules)
				origins := 0
				for _, group := range got.OriginGroups {
					origins += len(group.Origins)
				}
				if got.Capacity != (gatewaymodel.BackendCapacity{ServiceImports: groups, OriginGroups: groups, Origins: origins}) {
					t.Fatalf("capacity differs from generated backend inventory: %#v", got.Capacity)
				}
			}
		})
	}
}
