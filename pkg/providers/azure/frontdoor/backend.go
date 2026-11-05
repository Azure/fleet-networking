/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package frontdoor plans, stages and observes Azure Front Door resources for private Fleet ingress.
package frontdoor

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"

	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
)

// BackendKey identifies one ServiceImport port within a Gateway.
type BackendKey struct {
	Namespace string
	Name      string
	Port      int32
}

// BackendPlan is a backend-only inventory, not a complete or apply-ready AFD
// resource graph. Routes, domains, API capabilities and region support still
// require separate validation before any Azure mutation.
type BackendPlan struct {
	ProfileName  string
	EndpointName string
	Capacity     gatewaymodel.BackendCapacity
	OriginGroups []OriginGroup
	PrivateLinks []PrivateLinkPlan
}

// OriginGroup holds the private member origins for one ServiceImport port.
type OriginGroup struct {
	Name        string
	Backend     BackendKey
	HealthProbe gatewaymodel.HealthProbe
	Origins     []Origin
}

// Origin describes one private HTTP member origin, independently of ARM types.
type Origin struct {
	Name                        string
	Cluster                     string
	ServiceUID                  string
	HostName                    string
	HTTPPort                    int32
	Weight                      int64
	Priority                    int32
	OriginHostHeader            string
	EnforceCertificateNameCheck bool
	PrivateLink                 PrivateLinkTarget
}

// PrivateLinkTarget identifies a custom PLS and Private Link region within one
// profile. Custom PLS requests omit the target group ID; this is not an observed
// AFD-managed private endpoint or proof of supported Private Link region placement.
type PrivateLinkTarget struct {
	ResourceID string
	Location   string
}

// PrivateLinkPlan retains every origin referring to one shared private target.
// It describes desired references, not an observed or approved connection.
type PrivateLinkPlan struct {
	Target   PrivateLinkTarget
	HTTPPort int32
	Origins  []OriginReference
}

// OriginReference identifies an origin under its profile-scoped origin group.
type OriginReference struct {
	OriginGroup string
	Origin      string
}

// PrivateLinkPortConflictError rejects the documented AFD limitation on different
// origin ports sharing one managed private endpoint, even across origin groups.
type PrivateLinkPortConflictError struct {
	Target     PrivateLinkTarget
	FirstPort  int32
	SecondPort int32
}

func (e *PrivateLinkPortConflictError) Error() string {
	return fmt.Sprintf("private link target %q in %q uses different HTTP ports %d and %d in one AFD profile",
		e.Target.ResourceID, e.Target.Location, e.FirstPort, e.SecondPort)
}

// PlanBackends validates and inventories private HTTP backends without mutating
// input or contacting Azure. Every error returns a zero plan.
func PlanBackends(model gatewaymodel.GlobalGateway) (BackendPlan, error) {
	if strings.TrimSpace(model.UID) == "" {
		return BackendPlan{}, fmt.Errorf("Gateway UID is required for AFD resource ownership")
	}
	normalized, err := gatewaymodel.Normalize(model)
	if err != nil {
		return BackendPlan{}, err
	}
	// Canonicalize the copy before equality/deduplication so casing cannot bypass safety checks.
	for i := range normalized.Routes {
		for j := range normalized.Routes[i].Rules {
			for k := range normalized.Routes[i].Rules[j].Backends {
				backend := &normalized.Routes[i].Rules[j].Backends[k]
				for l := range backend.Origins {
					origin := &backend.Origins[l]
					resourceID, err := arm.ParseResourceID(origin.PrivateLinkResourceID)
					if err != nil {
						return BackendPlan{}, fmt.Errorf("parse origin %q PLS resource ID: %w", origin.Cluster, err)
					}
					origin.PrivateLinkResourceID = strings.ToLower(resourceID.String())
					origin.PrivateLinkLocation = strings.ToLower(strings.TrimSpace(origin.PrivateLinkLocation))
				}
			}
		}
	}
	capacity, err := gatewaymodel.PreflightBackendCapacity(normalized)
	if err != nil {
		return BackendPlan{}, err
	}
	plan := BackendPlan{
		ProfileName: resourceName("fleet-afd", normalized.UID), EndpointName: resourceName("fleet-ep", normalized.UID),
		Capacity: capacity,
	}
	groups := make(map[BackendKey]bool)
	for _, route := range normalized.Routes {
		for _, rule := range route.Rules {
			for _, backend := range rule.Backends {
				key := BackendKey{Namespace: backend.Namespace, Name: backend.Name, Port: backend.Port}
				if groups[key] {
					continue
				}
				groups[key] = true
				// One positive logical backend weight does not alter its member resource graph.
				group := OriginGroup{
					Name:    resourceName("fleet-og", normalized.UID, key.Namespace, key.Name, strconv.Itoa(int(key.Port))),
					Backend: key, HealthProbe: backend.HealthProbe,
				}
				for _, member := range backend.Origins {
					target := PrivateLinkTarget{ResourceID: member.PrivateLinkResourceID, Location: member.PrivateLinkLocation}
					group.Origins = append(group.Origins, Origin{
						// Replacements change identity; weight-only updates keep the same resource.
						Name: resourceName("fleet-origin", group.Name, member.Cluster, member.ServiceUID,
							target.ResourceID, target.Location, member.Endpoint),
						Cluster: member.Cluster, ServiceUID: member.ServiceUID, HostName: member.Endpoint,
						HTTPPort: key.Port, Weight: member.Weight, Priority: 1,
						OriginHostHeader: backend.OriginHostHeader, EnforceCertificateNameCheck: true, PrivateLink: target,
					})
				}
				plan.OriginGroups = append(plan.OriginGroups, group)
			}
		}
	}
	sort.Slice(plan.OriginGroups, func(i, j int) bool {
		left, right := plan.OriginGroups[i].Backend, plan.OriginGroups[j].Backend
		if left.Namespace != right.Namespace {
			return left.Namespace < right.Namespace
		}
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		return left.Port < right.Port
	})
	// Sharing is profile-wide: separate origin groups cannot isolate different ports.
	targets := make(map[PrivateLinkTarget]*PrivateLinkPlan)
	for _, group := range plan.OriginGroups {
		for _, origin := range group.Origins {
			target, found := targets[origin.PrivateLink]
			if !found {
				target = &PrivateLinkPlan{Target: origin.PrivateLink, HTTPPort: origin.HTTPPort}
				targets[origin.PrivateLink] = target
			} else if target.HTTPPort != origin.HTTPPort {
				return BackendPlan{}, &PrivateLinkPortConflictError{
					Target: origin.PrivateLink, FirstPort: target.HTTPPort, SecondPort: origin.HTTPPort,
				}
			}
			target.Origins = append(target.Origins, OriginReference{OriginGroup: group.Name, Origin: origin.Name})
		}
	}
	for _, target := range targets {
		sort.Slice(target.Origins, func(i, j int) bool {
			left, right := target.Origins[i], target.Origins[j]
			if left.OriginGroup != right.OriginGroup {
				return left.OriginGroup < right.OriginGroup
			}
			return left.Origin < right.Origin
		})
		plan.PrivateLinks = append(plan.PrivateLinks, *target)
	}
	sort.Slice(plan.PrivateLinks, func(i, j int) bool {
		left, right := plan.PrivateLinks[i].Target, plan.PrivateLinks[j].Target
		if left.ResourceID != right.ResourceID {
			return left.ResourceID < right.ResourceID
		}
		return left.Location < right.Location
	})
	return plan, nil
}

func resourceName(prefix string, parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		// Length prefixes prevent different identity tuples from hashing the same concatenation.
		fmt.Fprintf(hash, "%d:%s", len(part), part)
	}
	return fmt.Sprintf("%s-%x", prefix, hash.Sum(nil)[:16])
}
