/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gatewayresolver

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

const (
	// GatewayClassField indexes Gateways by their GatewayClass.
	GatewayClassField = ".spec.gatewayClassName"
	// HTTPRouteGatewayField indexes routes by namespace/name of Gateway parents.
	HTTPRouteGatewayField = ".spec.parentRefs.gateway"
	// HTTPRouteServiceImportField indexes routes by Fleet ServiceImport references.
	HTTPRouteServiceImportField = ".spec.rules.backendRefs.serviceImport"
	// HTTPRouteBackendNamespaceField indexes target namespaces for grant changes.
	HTTPRouteBackendNamespaceField = ".spec.rules.backendRefs.serviceImportNamespace"
	// ExportServiceField matches the existing hub source-service index convention.
	ExportServiceField = ".spec.serviceReference.namespacedName"
)

// SetupIndexes registers dependency indexes on the separate Gateway manager.
func SetupIndexes(ctx context.Context, indexer client.FieldIndexer) error {
	indexes := []struct {
		object  client.Object
		field   string
		extract client.IndexerFunc
	}{
		{&gatewayv1.Gateway{}, GatewayClassField, gatewayClassIndex},
		{&gatewayv1.HTTPRoute{}, HTTPRouteGatewayField, routeGatewayIndex},
		{&gatewayv1.HTTPRoute{}, HTTPRouteServiceImportField, routeServiceImportIndex},
		{&gatewayv1.HTTPRoute{}, HTTPRouteBackendNamespaceField, routeBackendNamespaceIndex},
		{&fleetnetv1alpha1.InternalServiceExport{}, ExportServiceField, exportServiceIndex},
	}
	for _, index := range indexes {
		if err := indexer.IndexField(ctx, index.object, index.field, index.extract); err != nil {
			klog.ErrorS(err, "Failed to create Gateway dependency index", "field", index.field)
			return err
		}
	}
	return nil
}

func gatewayClassIndex(object client.Object) []string {
	return []string{string(object.(*gatewayv1.Gateway).Spec.GatewayClassName)}
}

func routeGatewayIndex(object client.Object) []string {
	route := object.(*gatewayv1.HTTPRoute)
	keys := make(map[string]struct{})
	for _, parent := range route.Spec.ParentRefs {
		if key, recognized := GatewayParentKey(route.Namespace, parent); recognized {
			keys[key.Namespace+"/"+key.Name] = struct{}{}
		}
	}
	return sortedIndexKeys(keys)
}

func routeServiceImportIndex(object client.Object) []string {
	route := object.(*gatewayv1.HTTPRoute)
	keys := make(map[string]struct{})
	for _, rule := range route.Spec.Rules {
		for _, backend := range rule.BackendRefs {
			if ptr.Deref(backend.Group, "") != gatewayv1.Group(fleetnetv1alpha1.GroupVersion.Group) ||
				ptr.Deref(backend.Kind, "Service") != serviceImportKind {
				continue
			}
			namespace := string(ptr.Deref(backend.Namespace, gatewayv1.Namespace(route.Namespace)))
			keys[namespace+"/"+string(backend.Name)] = struct{}{}
		}
	}
	return sortedIndexKeys(keys)
}

func exportServiceIndex(object client.Object) []string {
	return []string{object.(*fleetnetv1alpha1.InternalServiceExport).Spec.ServiceReference.NamespacedName}
}

func routeBackendNamespaceIndex(object client.Object) []string {
	keys := make(map[string]struct{})
	for _, backend := range routeServiceImportIndex(object) {
		namespace, _, _ := strings.Cut(backend, "/")
		keys[namespace] = struct{}{}
	}
	return sortedIndexKeys(keys)
}

// DependentRoutes finds routes affected by a Gateway, backend, grant or Namespace.
// Grant lookup intentionally includes all names so wildcard grants and removals
// invalidate existing references without relying on the new grant contents.
func DependentRoutes(ctx context.Context, reader client.Reader, object client.Object) ([]gatewayv1.HTTPRoute, error) {
	var options []client.ListOption
	switch dependency := object.(type) {
	case *gatewayv1.Gateway:
		options = append(options, client.MatchingFields{HTTPRouteGatewayField: dependency.Namespace + "/" + dependency.Name})
	case *fleetnetv1alpha1.ServiceImport:
		options = append(options, client.MatchingFields{HTTPRouteServiceImportField: dependency.Namespace + "/" + dependency.Name})
	case *fleetnetv1alpha1.InternalServiceExport:
		options = append(options, client.MatchingFields{HTTPRouteServiceImportField: dependency.Spec.ServiceReference.NamespacedName})
	case *gatewayv1beta1.ReferenceGrant:
		options = append(options, client.MatchingFields{HTTPRouteBackendNamespaceField: dependency.Namespace})
	case *corev1.Namespace:
		options = append(options, client.InNamespace(dependency.Name))
	default:
		return nil, fmt.Errorf("unsupported HTTPRoute dependency %T", object)
	}
	var routes gatewayv1.HTTPRouteList
	if err := reader.List(ctx, &routes, options...); err != nil {
		return nil, err
	}
	return routes.Items, nil
}

func sortedIndexKeys(keys map[string]struct{}) []string {
	if len(keys) == 0 {
		return nil
	}
	result := make([]string, 0, len(keys))
	for key := range keys {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}
