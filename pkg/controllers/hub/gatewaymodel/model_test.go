/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gatewaymodel

import (
	"reflect"
	"strings"
	"testing"
)

func TestNormalize_SortsNestedResources(t *testing.T) {
	model := GlobalGateway{
		Namespace: "store",
		Name:      "global",
		Listeners: []Listener{
			{Name: "https", Port: 443},
			{Name: "http", Port: 80},
		},
		Routes: []Route{
			{
				Namespace: "store",
				Name:      "z-route",
				Backends: []Backend{
					{
						Namespace: "store",
						Name:      "z-api",
						Port:      8080,
						Origins: []Origin{
							{Cluster: "west", Weight: 20},
							{Cluster: "east", Weight: 10},
						},
					},
					{Namespace: "store", Name: "a-api", Port: 8080},
				},
			},
			{Namespace: "store", Name: "a-route"},
		},
	}

	got, err := Normalize(model)
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}

	if got.Listeners[0].Name != "http" || got.Listeners[1].Name != "https" {
		t.Errorf("listener order = %v, want http then https", got.Listeners)
	}
	if got.Routes[0].Name != "a-route" || got.Routes[1].Name != "z-route" {
		t.Errorf("route order = %v, want a-route then z-route", got.Routes)
	}
	if got.Routes[1].Backends[0].Name != "a-api" || got.Routes[1].Backends[1].Name != "z-api" {
		t.Errorf("backend order = %v, want a-api then z-api", got.Routes[1].Backends)
	}
	origins := got.Routes[1].Backends[1].Origins
	if origins[0].Cluster != "east" || origins[1].Cluster != "west" {
		t.Errorf("origin order = %v, want east then west", origins)
	}

	if reflect.DeepEqual(model, got) {
		t.Error("Normalize() returned a model equal to unsorted input")
	}
	if model.Listeners[0].Name != "https" {
		t.Error("Normalize() mutated the input model")
	}
}

func TestNormalize_RejectsInvalidModels(t *testing.T) {
	tests := []struct {
		name    string
		model   GlobalGateway
		wantErr string
	}{
		{
			name: "missing Gateway identity",
			model: GlobalGateway{
				Listeners: []Listener{{Name: "http", Port: 80}},
			},
			wantErr: "Gateway identity",
		},
		{
			name: "duplicate listener",
			model: GlobalGateway{
				Namespace: "store",
				Name:      "global",
				Listeners: []Listener{
					{Name: "http", Port: 80},
					{Name: "http", Port: 8080},
				},
			},
			wantErr: "duplicate listener",
		},
		{
			name: "duplicate route",
			model: GlobalGateway{
				Namespace: "store",
				Name:      "global",
				Routes: []Route{
					{Namespace: "store", Name: "api"},
					{Namespace: "store", Name: "api"},
				},
			},
			wantErr: "duplicate route",
		},
		{
			name: "duplicate backend",
			model: GlobalGateway{
				Namespace: "store",
				Name:      "global",
				Routes: []Route{{
					Namespace: "store",
					Name:      "api",
					Backends: []Backend{
						{Namespace: "store", Name: "backend", Port: 8080},
						{Namespace: "store", Name: "backend", Port: 8080},
					},
				}},
			},
			wantErr: "duplicate backend",
		},
		{
			name: "invalid backend port",
			model: GlobalGateway{
				Namespace: "store",
				Name:      "global",
				Routes: []Route{{
					Namespace: "store",
					Name:      "api",
					Backends: []Backend{{
						Namespace: "store",
						Name:      "backend",
						Port:      70000,
					}},
				}},
			},
			wantErr: "port",
		},
		{
			name: "invalid route weight",
			model: GlobalGateway{
				Namespace: "store",
				Name:      "global",
				Routes: []Route{{
					Namespace: "store",
					Name:      "api",
					Backends: []Backend{{
						Namespace:   "store",
						Name:        "backend",
						Port:        8080,
						RouteWeight: 1000001,
					}},
				}},
			},
			wantErr: "route weight",
		},
		{
			name: "duplicate origin cluster",
			model: GlobalGateway{
				Namespace: "store",
				Name:      "global",
				Routes: []Route{{
					Namespace: "store",
					Name:      "api",
					Backends: []Backend{{
						Namespace: "store",
						Name:      "backend",
						Port:      8080,
						Origins: []Origin{
							{Cluster: "east", Weight: 1},
							{Cluster: "east", Weight: 2},
						},
					}},
				}},
			},
			wantErr: "duplicate origin",
		},
		{
			name: "invalid origin weight",
			model: GlobalGateway{
				Namespace: "store",
				Name:      "global",
				Routes: []Route{{
					Namespace: "store",
					Name:      "api",
					Backends: []Backend{{
						Namespace: "store",
						Name:      "backend",
						Port:      8080,
						Origins: []Origin{
							{Cluster: "east", Weight: 1001},
						},
					}},
				}},
			},
			wantErr: "weight",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Normalize(tt.model)
			if err == nil {
				t.Fatalf("Normalize() error = nil, want containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Normalize() error = %q, want containing %q", err, tt.wantErr)
			}
		})
	}
}
