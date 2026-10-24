/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package gatewaymodel defines the provider-neutral desired state produced from
// Gateway API and Fleet ServiceImport resources.
package gatewaymodel

import (
	"fmt"
	"net/netip"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
)

const maxServiceExportWeight = 1000
const maxHTTPRouteBackendWeight = 1000000
const pathTypePrefix = "PathPrefix"
const defaultHTTPPreviewHealthProbePath = "/"

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
	Namespace         string
	Name              string
	UID               string
	CreationTimestamp time.Time
	Attachments       []RouteAttachment
	Rules             []HTTPRule
}

// RouteAttachment contains the effective hostnames for one Gateway listener.
// Empty Hostnames means unconstrained, not an empty intersection.
type RouteAttachment struct {
	Listener  string
	Hostnames []string
}

// HTTPRule preserves one HTTPRoute rule. Rule order breaks otherwise equal
// matches and must not be changed during normalization or provider lowering.
type HTTPRule struct {
	Matches  []HTTPMatch
	Filters  []HTTPFilter
	Backends []Backend
}

// HTTPMatch contains the portable HTTP match fields supported by the initial provider.
type HTTPMatch struct {
	PathType string
	Path     string
	Method   string
}

// Validate checks the path-only HTTP-preview match subset.
func (match HTTPMatch) Validate() error {
	if (match.PathType != "Exact" && match.PathType != pathTypePrefix) || match.Method != "" ||
		!strings.HasPrefix(match.Path, "/") || strings.ContainsAny(match.Path, "?#") || strings.Contains(match.Path, "//") {
		return fmt.Errorf("HTTP preview requires Exact/PathPrefix paths without method matching")
	}
	return nil
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
	RouteWeight int32
	HealthProbe HealthProbe
	// An empty override preserves the incoming request Host. Probe Host behavior
	// must be verified independently before enabling the provider.
	OriginHostHeader string
	Origins          []Origin
}

// HealthProbe is platform-owned; its port is the backend's selected service port.
type HealthProbe struct {
	Path            string
	Method          string
	Protocol        string
	IntervalSeconds int32
}

// HTTPPreviewHealthProbe returns the fixed, approved initial preview policy.
// A future ConfigMap could supply platform policy, not customer ServiceImport annotations.
func HTTPPreviewHealthProbe() HealthProbe {
	// Resolve any future platform ConfigMap override here, not on ServiceImport.
	probePath := defaultHTTPPreviewHealthProbePath
	return HealthProbe{Path: probePath, Method: "GET", Protocol: "HTTP", IntervalSeconds: 30}
}

// Origin represents one member-cluster endpoint behind a ServiceImport.
type Origin struct {
	Cluster  string
	Endpoint string
	// Weight splits traffic between member clusters behind one ServiceImport
	// and follows the existing Fleet ServiceExport range [0, 1000].
	Weight                int64
	ServiceUID            string
	PrivateLinkResourceID string
	PrivateLinkLocation   string
}

// Validate rejects incomplete or non-private origin identities.
func (origin Origin) Validate() error {
	if origin.Cluster == "" || origin.ServiceUID == "" {
		return fmt.Errorf("origin requires cluster and source Service UID")
	}
	if origin.Weight < 0 || origin.Weight > maxServiceExportWeight {
		return fmt.Errorf("origin %q weight %d is outside [0, %d]", origin.Cluster, origin.Weight, maxServiceExportWeight)
	}
	// Internal provenance comes from discovery; RFC1918 membership is not proof of it.
	address, err := netip.ParseAddr(origin.Endpoint)
	if err != nil || !address.Is4() || !address.IsGlobalUnicast() {
		return fmt.Errorf("origin %q requires a verified IPv4 internal frontend address", origin.Cluster)
	}
	resourceID, err := arm.ParseResourceID(origin.PrivateLinkResourceID)
	if err != nil || !strings.EqualFold(resourceID.ResourceType.String(), "Microsoft.Network/privateLinkServices") ||
		resourceID.SubscriptionID == "" || resourceID.ResourceGroupName == "" || strings.TrimSpace(origin.PrivateLinkLocation) == "" {
		return fmt.Errorf("origin %q requires a PLS resource ID and location", origin.Cluster)
	}
	return nil
}

// Normalize validates a model and returns a deeply copied, deterministically
// ordered representation suitable for equality checks and provider reconciliation.
// Providers must still rank hostname/path specificity before route-age and
// rule-order tie-breaks; canonical container order is not a routing algorithm.
func Normalize(model GlobalGateway) (GlobalGateway, error) {
	normalized := clone(model)
	for i := range normalized.Routes {
		route := &normalized.Routes[i]
		route.CreationTimestamp = route.CreationTimestamp.UTC()
		for j := range route.Attachments {
			route.Attachments[j].Hostnames = canonicalHostnames(route.Attachments[j].Hostnames)
		}
		for j := range route.Rules {
			rule := &route.Rules[j]
			if len(rule.Matches) == 0 {
				rule.Matches = []HTTPMatch{{PathType: pathTypePrefix, Path: "/"}}
			}
			for k := range rule.Matches {
				match := &rule.Matches[k]
				if match.PathType == "" {
					match.PathType = pathTypePrefix
				}
				if match.Path == "" {
					match.Path = "/"
				}
				if match.PathType == pathTypePrefix && match.Path != "/" && !strings.Contains(match.Path, "//") {
					// Gateway PathPrefix ignores a trailing slash; Exact must preserve it.
					match.Path = strings.TrimSuffix(match.Path, "/")
				}
			}
		}
	}
	if err := validate(normalized); err != nil {
		return GlobalGateway{}, err
	}

	sort.Slice(normalized.Listeners, func(i, j int) bool {
		return normalized.Listeners[i].Name < normalized.Listeners[j].Name
	})
	sort.Slice(normalized.Routes, func(i, j int) bool {
		left, right := normalized.Routes[i], normalized.Routes[j]
		if !left.CreationTimestamp.Equal(right.CreationTimestamp) {
			return left.CreationTimestamp.Before(right.CreationTimestamp)
		}
		if left.Namespace != right.Namespace {
			return left.Namespace < right.Namespace
		}
		return left.Name < right.Name
	})
	for routeIndex := range normalized.Routes {
		route := &normalized.Routes[routeIndex]
		sort.Slice(route.Attachments, func(i, j int) bool {
			return route.Attachments[i].Listener < route.Attachments[j].Listener
		})
		for ruleIndex := range route.Rules {
			rule := &route.Rules[ruleIndex]
			sort.Slice(rule.Matches, func(i, j int) bool {
				left, right := rule.Matches[i], rule.Matches[j]
				if left.PathType != right.PathType {
					return left.PathType < right.PathType
				}
				return left.Path < right.Path
			})
			matches := rule.Matches[:0]
			for _, match := range rule.Matches {
				if len(matches) == 0 || matches[len(matches)-1] != match {
					matches = append(matches, match)
				}
			}
			rule.Matches = matches
			for backendIndex := range rule.Backends {
				origins := rule.Backends[backendIndex].Origins
				sort.Slice(origins, func(i, j int) bool { return origins[i].Cluster < origins[j].Cluster })
			}
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
		route := &result.Routes[routeIndex]
		source := model.Routes[routeIndex]
		route.Attachments = append([]RouteAttachment(nil), source.Attachments...)
		for i := range route.Attachments {
			route.Attachments[i].Hostnames = append([]string(nil), source.Attachments[i].Hostnames...)
		}
		route.Rules = make([]HTTPRule, len(source.Rules))
		for i := range source.Rules {
			route.Rules[i] = source.Rules[i]
			route.Rules[i].Matches = append([]HTTPMatch(nil), source.Rules[i].Matches...)
			route.Rules[i].Filters = append([]HTTPFilter(nil), source.Rules[i].Filters...)
			route.Rules[i].Backends = append([]Backend(nil), source.Rules[i].Backends...)
			for j := range source.Rules[i].Backends {
				route.Rules[i].Backends[j].Origins = append([]Origin(nil), source.Rules[i].Backends[j].Origins...)
			}
		}
	}
	return result
}

func validate(model GlobalGateway) error {
	if model.Namespace == "" || model.Name == "" {
		return fmt.Errorf("Gateway identity requires namespace and name")
	}

	listeners := make(map[string]Listener, len(model.Listeners))
	for _, listener := range model.Listeners {
		if listener.Name == "" {
			return fmt.Errorf("listener name must not be empty")
		}
		if _, found := listeners[listener.Name]; found {
			return fmt.Errorf("duplicate listener %q", listener.Name)
		}
		listeners[listener.Name] = listener
		if listener.Protocol != "HTTP" || listener.Port != 80 {
			return fmt.Errorf("listener %q requires HTTP on edge port 80 in the initial preview", listener.Name)
		}
		if err := validateHostname(listener.Hostname); err != nil {
			return fmt.Errorf("listener %q: %w", listener.Name, err)
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
		if err := validateRoute(route, routeKey, listeners); err != nil {
			return err
		}
	}
	return nil
}

func validateRoute(route Route, routeKey string, listeners map[string]Listener) error {
	if len(route.Attachments) == 0 || len(route.Rules) == 0 {
		return fmt.Errorf("route %q requires listener attachments and rules", routeKey)
	}
	attached := make(map[string]struct{}, len(route.Attachments))
	for _, attachment := range route.Attachments {
		listener, found := listeners[attachment.Listener]
		if _, duplicate := attached[attachment.Listener]; !found || duplicate {
			return fmt.Errorf("route %q has an unknown or duplicate listener attachment %q", routeKey, attachment.Listener)
		}
		attached[attachment.Listener] = struct{}{}
		hosts, matches, err := IntersectHostnames(listener.Hostname, attachment.Hostnames)
		if err != nil || !matches || !reflect.DeepEqual(hosts, attachment.Hostnames) {
			return fmt.Errorf("route %q attachment %q must preserve the listener hostname intersection", routeKey, attachment.Listener)
		}
	}
	for ruleIndex, rule := range route.Rules {
		if err := validateRule(rule, routeKey, ruleIndex); err != nil {
			return err
		}
	}
	return nil
}

func validateRule(rule HTTPRule, routeKey string, ruleIndex int) error {
	if len(rule.Filters) != 0 {
		return fmt.Errorf("route %q rule %d filters are not supported in the HTTP preview", routeKey, ruleIndex)
	}
	for _, match := range rule.Matches {
		if err := match.Validate(); err != nil {
			return fmt.Errorf("route %q rule %d: %w", routeKey, ruleIndex, err)
		}
	}
	backends := make(map[string]struct{}, len(rule.Backends))
	for _, backend := range rule.Backends {
		if backend.Namespace == "" || backend.Name == "" {
			return fmt.Errorf("route %q backend identity requires namespace and name", routeKey)
		}
		backendKey := fmt.Sprintf("%s/%s:%d", backend.Namespace, backend.Name, backend.Port)
		if _, found := backends[backendKey]; found {
			return fmt.Errorf("route %q has duplicate backend %q", routeKey, backendKey)
		}
		backends[backendKey] = struct{}{}
		if err := validateBackend(backend, backendKey, routeKey); err != nil {
			return err
		}
	}
	if len(rule.Backends) != 1 {
		return fmt.Errorf("route %q rule %d requires exactly one logical backend in the HTTP preview", routeKey, ruleIndex)
	}
	return nil
}

func validateBackend(backend Backend, backendKey, routeKey string) error {
	if backend.Port < 1 || backend.Port > 65535 {
		return fmt.Errorf("route %q backend %q port %d is outside [1, 65535]", routeKey, backendKey, backend.Port)
	}
	if backend.RouteWeight < 1 || backend.RouteWeight > maxHTTPRouteBackendWeight {
		return fmt.Errorf("route %q backend %q requires a positive route weight at most %d", routeKey, backendKey, maxHTTPRouteBackendWeight)
	}
	if backend.HealthProbe != HTTPPreviewHealthProbe() || backend.OriginHostHeader != "" {
		return fmt.Errorf("backend %q requires the fixed HTTP-preview probe and incoming Host policy", backendKey)
	}
	if len(backend.Origins) == 0 {
		return fmt.Errorf("backend %q requires private origins", backendKey)
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
		if err := origin.Validate(); err != nil {
			return fmt.Errorf("backend %q: %w", backendKey, err)
		}
		if origin.Weight == 0 {
			return fmt.Errorf("backend %q must omit zero-weight origin %q", backendKey, origin.Cluster)
		}
	}
	return nil
}
