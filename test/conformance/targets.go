/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package conformance

import (
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// OriginTarget identifies a backend in the current fixture graph, not an approval grant.
type OriginTarget struct {
	Route     types.NamespacedName
	Gateway   types.NamespacedName
	Parent    gatewayv1.ParentReference
	Reference gatewayv1.HTTPBackendRef
	RuleIndex int
}

// OriginTargets discovers every forwarding reference without assuming fixture names or ports.
func OriginTargets(routes []gatewayv1.HTTPRoute) ([]OriginTarget, error) {
	var targets []OriginTarget
	for _, route := range routes {
		if route.DeletionTimestamp != nil {
			continue
		}
		for _, parent := range route.Spec.ParentRefs {
			if ptr.Deref(parent.Group, gatewayv1.Group(gatewayv1.GroupName)) != gatewayv1.GroupName ||
				ptr.Deref(parent.Kind, "Gateway") != "Gateway" {
				continue
			}
			if parent.Name == "" {
				return nil, fmt.Errorf("fixture route %s/%s has an empty Gateway reference", route.Namespace, route.Name)
			}
			gateway := types.NamespacedName{
				Namespace: string(ptr.Deref(parent.Namespace, gatewayv1.Namespace(route.Namespace))),
				Name:      string(parent.Name),
			}
			for index, rule := range route.Spec.Rules {
				for _, ref := range rule.BackendRefs {
					if ptr.Deref(ref.Weight, 1) == 0 {
						continue
					}
					localService := ptr.Deref(ref.Group, "") == "" && ptr.Deref(ref.Kind, "Service") == "Service"
					if ref.Name == "" || (localService && ref.Port == nil) {
						return nil, fmt.Errorf("fixture route %s/%s requires a named backend and port", route.Namespace, route.Name)
					}
					targets = append(targets, OriginTarget{
						Route:   types.NamespacedName{Namespace: route.Namespace, Name: route.Name},
						Gateway: gateway, Parent: parent, Reference: ref, RuleIndex: index,
					})
				}
			}
		}
	}
	return targets, nil
}
