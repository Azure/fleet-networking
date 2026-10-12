/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package gatewaymodel defines the provider-neutral desired state produced from
// Gateway API and Fleet ServiceImport resources.
package gatewaymodel

import (
	"fmt"
	"sort"
)

const maxServiceExportWeight = 1000
const maxHTTPRouteBackendWeight = 1000000

// GlobalGateway is the normalized desired state for one Gateway.
type GlobalGateway struct {
	Namespace   string
	Name        string
	UID         string
	Listeners   []Listener
	Routes      []Route
	WAFPolicyID string
}

// Listener is a normalized Gateway listener.
type Listener struct {
	Name     string
	Protocol string
	Port     int32
	Hostname string
}

// Route is a normalized HTTPRoute attached to a Gateway.
type Route struct {
	Namespace string
	Name      string
	Hostnames []string
	Matches   []HTTPMatch
	Filters   []HTTPFilter
	Backends  []Backend
}

// HTTPMatch contains the portable HTTP match fields supported by the initial provider.
type HTTPMatch struct {
	PathType string
	Path     string
	Method   string
}

// HTTPFilter contains the provider-neutral representation of one supported HTTPRoute filter.
type HTTPFilter struct {
	Type       string
	StatusCode int32
	Hostname   string
	Path       string
}

// Backend represents one Fleet ServiceImport referenced by an HTTPRoute.
type Backend struct {
	Namespace string
	Name      string
	Port      int32
	// RouteWeight splits traffic between logical ServiceImport backends and
	// follows the Gateway API HTTPBackendRef range [0, 1,000,000].
	RouteWeight     int32
	HealthProbePath string
	Origins         []Origin
}

// Origin represents one member-cluster endpoint behind a ServiceImport.
type Origin struct {
	Cluster  string
	Endpoint string
	// Weight splits traffic between member clusters behind one ServiceImport
	// and follows the existing Fleet ServiceExport range [0, 1000].
	Weight                int64
	Connectivity          string
	PrivateLinkResourceID string
	PrivateLinkLocation   string
}

// Normalize validates a model and returns a deeply copied, deterministically
// ordered representation suitable for equality checks and provider reconciliation.
func Normalize(model GlobalGateway) (GlobalGateway, error) {
	normalized := clone(model)
	if err := validate(normalized); err != nil {
		return GlobalGateway{}, err
	}

	sort.Slice(normalized.Listeners, func(i, j int) bool {
		return normalized.Listeners[i].Name < normalized.Listeners[j].Name
	})
	sort.Slice(normalized.Routes, func(i, j int) bool {
		left, right := normalized.Routes[i], normalized.Routes[j]
		if left.Namespace != right.Namespace {
			return left.Namespace < right.Namespace
		}
		return left.Name < right.Name
	})
	for routeIndex := range normalized.Routes {
		route := &normalized.Routes[routeIndex]
		sort.Strings(route.Hostnames)
		sort.Slice(route.Backends, func(i, j int) bool {
			left, right := route.Backends[i], route.Backends[j]
			if left.Namespace != right.Namespace {
				return left.Namespace < right.Namespace
			}
			if left.Name != right.Name {
				return left.Name < right.Name
			}
			return left.Port < right.Port
		})
		for backendIndex := range route.Backends {
			sort.Slice(route.Backends[backendIndex].Origins, func(i, j int) bool {
				return route.Backends[backendIndex].Origins[i].Cluster < route.Backends[backendIndex].Origins[j].Cluster
			})
		}
	}
	return normalized, nil
}

func clone(model GlobalGateway) GlobalGateway {
	result := model
	result.Listeners = append([]Listener(nil), model.Listeners...)
	result.Routes = make([]Route, len(model.Routes))
	for routeIndex := range model.Routes {
		result.Routes[routeIndex] = model.Routes[routeIndex]
		result.Routes[routeIndex].Hostnames = append([]string(nil), model.Routes[routeIndex].Hostnames...)
		result.Routes[routeIndex].Matches = append([]HTTPMatch(nil), model.Routes[routeIndex].Matches...)
		result.Routes[routeIndex].Filters = append([]HTTPFilter(nil), model.Routes[routeIndex].Filters...)
		result.Routes[routeIndex].Backends = make([]Backend, len(model.Routes[routeIndex].Backends))
		for backendIndex := range model.Routes[routeIndex].Backends {
			result.Routes[routeIndex].Backends[backendIndex] = model.Routes[routeIndex].Backends[backendIndex]
			result.Routes[routeIndex].Backends[backendIndex].Origins = append([]Origin(nil), model.Routes[routeIndex].Backends[backendIndex].Origins...)
		}
	}
	return result
}

func validate(model GlobalGateway) error {
	if model.Namespace == "" || model.Name == "" {
		return fmt.Errorf("Gateway identity requires namespace and name")
	}

	listeners := make(map[string]struct{}, len(model.Listeners))
	for _, listener := range model.Listeners {
		if listener.Name == "" {
			return fmt.Errorf("listener name must not be empty")
		}
		if _, found := listeners[listener.Name]; found {
			return fmt.Errorf("duplicate listener %q", listener.Name)
		}
		listeners[listener.Name] = struct{}{}
		if listener.Port < 1 || listener.Port > 65535 {
			return fmt.Errorf("listener %q port %d is outside [1, 65535]", listener.Name, listener.Port)
		}
	}

	routes := make(map[string]struct{}, len(model.Routes))
	for _, route := range model.Routes {
		if route.Namespace == "" || route.Name == "" {
			return fmt.Errorf("route identity requires namespace and name")
		}
		routeKey := route.Namespace + "/" + route.Name
		if _, found := routes[routeKey]; found {
			return fmt.Errorf("duplicate route %q", routeKey)
		}
		routes[routeKey] = struct{}{}

		backends := make(map[string]struct{}, len(route.Backends))
		for _, backend := range route.Backends {
			if backend.Namespace == "" || backend.Name == "" {
				return fmt.Errorf("route %q backend identity requires namespace and name", routeKey)
			}
			backendKey := fmt.Sprintf("%s/%s:%d", backend.Namespace, backend.Name, backend.Port)
			if _, found := backends[backendKey]; found {
				return fmt.Errorf("route %q has duplicate backend %q", routeKey, backendKey)
			}
			backends[backendKey] = struct{}{}
			if backend.Port < 1 || backend.Port > 65535 {
				return fmt.Errorf("route %q backend %q port %d is outside [1, 65535]", routeKey, backendKey, backend.Port)
			}
			if backend.RouteWeight < 0 || backend.RouteWeight > maxHTTPRouteBackendWeight {
				return fmt.Errorf("route %q backend %q route weight %d is outside [0, %d]", routeKey, backendKey, backend.RouteWeight, maxHTTPRouteBackendWeight)
			}

			origins := make(map[string]struct{}, len(backend.Origins))
			for _, origin := range backend.Origins {
				if origin.Cluster == "" {
					return fmt.Errorf("backend %q origin cluster must not be empty", backendKey)
				}
				if _, found := origins[origin.Cluster]; found {
					return fmt.Errorf("backend %q has duplicate origin cluster %q", backendKey, origin.Cluster)
				}
				origins[origin.Cluster] = struct{}{}
				if origin.Weight < 0 || origin.Weight > maxServiceExportWeight {
					return fmt.Errorf("backend %q origin %q weight %d is outside [0, %d]", backendKey, origin.Cluster, origin.Weight, maxServiceExportWeight)
				}
			}
		}
	}
	return nil
}
