/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gatewaymodel

import (
	"fmt"
	"reflect"
	"testing"
)

func capacityModel(originsPerGroup []int) GlobalGateway {
	model := ruleModel()
	model.Routes = nil
	for i, count := range originsPerGroup {
		route := ruleModel().Routes[0]
		route.Name = fmt.Sprintf("route-%d", i)
		route.UID = fmt.Sprintf("route-uid-%d", i)
		rule := &route.Rules[0]
		rule.Matches[0].Path = fmt.Sprintf("/api-%d", i)
		rule.Backends[0].Name = fmt.Sprintf("api-%d", i)
		rule.Backends[0].Origins = nil
		for j := 0; j < count; j++ {
			rule.Backends[0].Origins = append(rule.Backends[0].Origins, privateModelOrigin(fmt.Sprintf("member-%d", j), 1))
		}
		model.Routes = append(model.Routes, route)
	}
	return model
}

func TestPreflightBackendCapacityBoundaries(t *testing.T) {
	hundred := make([]int, 100)
	for i := range hundred {
		hundred[i] = 1
	}
	tests := []struct {
		name   string
		counts []int
		want   BackendCapacity
		err    bool
	}{
		{"100 groups", hundred, BackendCapacity{ServiceImports: 100, OriginGroups: 100, Origins: 100}, false},
		{"101 groups", append(append([]int(nil), hundred...), 1), BackendCapacity{}, true},
		{"40 origins per group", []int{40}, BackendCapacity{ServiceImports: 1, OriginGroups: 1, Origins: 40}, false},
		{"41 origins per group", []int{41}, BackendCapacity{}, true},
		{"160 total origins", []int{40, 40, 40, 40}, BackendCapacity{ServiceImports: 4, OriginGroups: 4, Origins: 160}, false},
		{"161 total origins", []int{40, 40, 40, 40, 1}, BackendCapacity{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := PreflightBackendCapacity(capacityModel(tt.counts))
			if (err != nil) != tt.err || got != tt.want {
				t.Fatalf("capacity = %#v, %v; want %#v, error=%v", got, err, tt.want, tt.err)
			}
		})
	}
}

func TestPreflightBackendCapacityUsesServicePortIdentity(t *testing.T) {
	model := capacityModel([]int{2})
	second := ruleModel().Routes[0].Rules[0]
	second.Backends = append([]Backend(nil), model.Routes[0].Rules[0].Backends...)
	model.Routes[0].Rules = append(model.Routes[0].Rules, second)
	got, err := PreflightBackendCapacity(model)
	if err != nil || got != (BackendCapacity{ServiceImports: 1, OriginGroups: 1, Origins: 2}) {
		t.Fatalf("repeated references counted twice: %#v, %v", got, err)
	}
	model.Routes[0].Rules[1].Backends[0].Port = 9090
	got, err = PreflightBackendCapacity(model)
	if err != nil || got != (BackendCapacity{ServiceImports: 1, OriginGroups: 2, Origins: 4}) {
		t.Fatalf("different service ports were collapsed: %#v, %v", got, err)
	}
}

func TestPreflightBackendCapacityRejectsInconsistentOrigins(t *testing.T) {
	model := capacityModel([]int{2})
	other, err := Normalize(model)
	if err != nil {
		t.Fatal(err)
	}
	other.Routes[0].Rules[0].Backends[0].Origins[0].Weight = 99
	model.Routes[0].Rules = append(model.Routes[0].Rules, other.Routes[0].Rules[0])
	if got, err := PreflightBackendCapacity(model); err == nil || !reflect.DeepEqual(got, BackendCapacity{}) {
		t.Fatalf("inconsistent copies of a backend were accepted: %#v, %v", got, err)
	}
}
