/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package frontdoor

import (
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	"k8s.io/utils/ptr"

	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
)

const (
	httpPreviewPathPrefix = "PathPrefix"
	httpPreviewPattern    = "/*"
)

// HTTPPreviewCapacity counts generated resources, not input HTTPRoute objects.
// Rule sets, rules and custom domains are zero in the gated root-path subset.
type HTTPPreviewCapacity struct {
	Profiles      int
	Endpoints     int
	OriginGroups  int
	Origins       int
	Routes        int
	RuleSets      int
	Rules         int
	CustomDomains int
}

// HTTPPreviewRequests is the complete, initially disabled root-path resource
// graph. It is not write authorization: platform proof gates, persisted ownership
// and dependency-ordered operations remain required before Azure mutation.
type HTTPPreviewRequests struct {
	Roots     RootRequests
	Backends  BackendRequests
	RouteName string
	Route     armcdn.Route
	Capacity  HTTPPreviewCapacity
}

// PreflightHTTPPreviewRouting rejects intent outside the approved initial
// execution subset without approximating hostname or case-sensitive path policy.
// Empty intent passes withdrawal preflight but cannot construct a resource graph.
func PreflightHTTPPreviewRouting(model gatewaymodel.GlobalGateway) error {
	normalized, err := gatewaymodel.Normalize(model)
	if err != nil {
		return err
	}
	if normalized.WAFPolicyID != "" {
		return fmt.Errorf("WAF association is not implemented")
	}
	if len(normalized.Listeners) != 1 || normalized.Listeners[0].Hostname != "" {
		return fmt.Errorf("AFD execution subset requires one unrestricted HTTP/80 listener; hostname routing is not implemented")
	}
	var selected BackendKey
	for _, route := range normalized.Routes {
		for _, attachment := range route.Attachments {
			if len(attachment.Hostnames) != 0 {
				return fmt.Errorf("AFD execution subset requires unrestricted route hostnames; custom-domain routing is not implemented")
			}
		}
		for _, rule := range route.Rules {
			for _, match := range rule.Matches {
				if match.PathType != httpPreviewPathPrefix || match.Path != "/" {
					return fmt.Errorf("AFD execution subset requires only PathPrefix /; case-sensitive non-root/Exact routing is not implemented")
				}
			}
			// Normalize proves one positive logical backend per rule.
			backend := rule.Backends[0]
			key := BackendKey{Namespace: backend.Namespace, Name: backend.Name, Port: backend.Port}
			if selected.Name != "" && selected != key {
				return fmt.Errorf("AFD execution subset requires one logical ServiceImport backend port across all rules; multiple-backend routing is not implemented")
			}
			selected = key
		}
	}
	return nil
}

// BuildHTTPPreviewRequests constructs fresh private HTTP requests without Azure
// calls. Unsupported/empty intent and identity failures return zero requests.
func BuildHTTPPreviewRequests(scope ResourceScope, owner GatewayOwner,
	model gatewaymodel.GlobalGateway) (HTTPPreviewRequests, error) {
	if err := validateModelOwner(owner, model); err != nil {
		return HTTPPreviewRequests{}, err
	}
	roots, err := BuildRootRequests(scope, owner)
	if err != nil {
		return HTTPPreviewRequests{}, err
	}
	if err := PreflightHTTPPreviewRouting(model); err != nil {
		return HTTPPreviewRequests{}, err
	}
	backends, err := BuildBackendRequests(model)
	if err != nil {
		return HTTPPreviewRequests{}, err
	}
	if len(backends.OriginGroups) != 1 {
		return HTTPPreviewRequests{}, fmt.Errorf("AFD HTTP graph requires one nonempty logical backend")
	}
	group := backends.OriginGroups[0]
	return HTTPPreviewRequests{
		Roots: roots, Backends: backends, RouteName: resourceName("fleet-route", owner.UID),
		// Omit cache, origin path, rule sets and custom domains: preserve path/query/Host.
		Route: armcdn.Route{Properties: &armcdn.RouteProperties{
			EnabledState: ptr.To(armcdn.EnabledStateDisabled),
			OriginGroup: &armcdn.ResourceReference{
				ID: ptr.To(roots.ProfileID + "/originGroups/" + group.Name),
			},
			PatternsToMatch:     []*string{ptr.To(httpPreviewPattern)},
			SupportedProtocols:  []*armcdn.AFDEndpointProtocols{ptr.To(armcdn.AFDEndpointProtocolsHTTP)},
			ForwardingProtocol:  ptr.To(armcdn.ForwardingProtocolHTTPOnly),
			HTTPSRedirect:       ptr.To(armcdn.HTTPSRedirectDisabled),
			LinkToDefaultDomain: ptr.To(armcdn.LinkToDefaultDomainEnabled),
		}},
		Capacity: HTTPPreviewCapacity{
			Profiles: 1, Endpoints: 1, OriginGroups: 1, Origins: len(group.Origins), Routes: 1,
		},
	}, nil
}
