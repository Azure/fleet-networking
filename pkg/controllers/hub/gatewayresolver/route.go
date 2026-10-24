/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gatewayresolver

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
)

// AttachmentError explains why a parent cannot accept an HTTPRoute.
type AttachmentError struct {
	Reason  gatewayv1.RouteConditionReason
	Message string
}

func (e *AttachmentError) Error() string { return e.Message }

// ListenerError identifies unsupported listener configuration for status.
type ListenerError struct {
	Reason  gatewayv1.ListenerConditionReason
	Message string
}

func (e *ListenerError) Error() string { return e.Message }

// GatewayParentKey resolves default Gateway parent group, kind and namespace.
func GatewayParentKey(namespace string, parent gatewayv1.ParentReference) (types.NamespacedName, bool) {
	if ptr.Deref(parent.Group, gatewayv1.Group(gatewayv1.GroupName)) != gatewayv1.GroupName ||
		ptr.Deref(parent.Kind, "Gateway") != "Gateway" || parent.Name == "" {
		return types.NamespacedName{}, false
	}
	return types.NamespacedName{Namespace: string(ptr.Deref(parent.Namespace, gatewayv1.Namespace(namespace))), Name: string(parent.Name)}, true
}

// ValidateListener checks the initial private HTTP-preview listener subset.
func ValidateListener(listener gatewayv1.Listener) error {
	if listener.Protocol != gatewayv1.HTTPProtocolType || listener.Port != 80 {
		return &ListenerError{Reason: gatewayv1.ListenerReasonUnsupportedProtocol, Message: fmt.Sprintf("listener %q requires HTTP on port 80", listener.Name)}
	}
	if listener.TLS != nil {
		return &ListenerError{Reason: gatewayv1.ListenerReasonInvalidCertificateRef, Message: "TLS configuration is unsupported in the HTTP preview"}
	}
	if _, _, err := gatewaymodel.IntersectHostnames(string(ptr.Deref(listener.Hostname, "")), nil); err != nil {
		return &ListenerError{Reason: gatewayv1.ListenerReasonInvalid, Message: err.Error()}
	}
	if listener.AllowedRoutes == nil {
		return nil
	}
	for _, kind := range listener.AllowedRoutes.Kinds {
		if ptr.Deref(kind.Group, gatewayv1.Group(gatewayv1.GroupName)) != gatewayv1.GroupName || kind.Kind != "HTTPRoute" {
			return &ListenerError{Reason: gatewayv1.ListenerReasonInvalidRouteKinds, Message: fmt.Sprintf("listener %q supports only gateway.networking.k8s.io/HTTPRoute", listener.Name)}
		}
	}
	namespaces := listener.AllowedRoutes.Namespaces
	if namespaces == nil {
		return nil
	}
	switch ptr.Deref(namespaces.From, gatewayv1.NamespacesFromSame) {
	case gatewayv1.NamespacesFromSame, gatewayv1.NamespacesFromAll:
		return nil
	case gatewayv1.NamespacesFromSelector:
		if namespaces.Selector == nil {
			return &ListenerError{Reason: gatewayv1.ListenerReasonInvalid, Message: fmt.Sprintf("listener %q namespace selector is required", listener.Name)}
		}
		_, err := metav1.LabelSelectorAsSelector(namespaces.Selector)
		if err != nil {
			return &ListenerError{Reason: gatewayv1.ListenerReasonInvalid, Message: err.Error()}
		}
		return nil
	default:
		return &ListenerError{Reason: gatewayv1.ListenerReasonInvalid, Message: fmt.Sprintf("listener %q has unsupported namespace policy", listener.Name)}
	}
}

// ResolveAttachments enforces parent selection, namespace policy and hostname
// intersection. Namespace labels must come from the route's Namespace object.
func ResolveAttachments(gateway *gatewayv1.Gateway, route *gatewayv1.HTTPRoute, parent gatewayv1.ParentReference,
	namespaceLabels map[string]string) ([]gatewaymodel.RouteAttachment, error) {
	key, recognized := GatewayParentKey(route.Namespace, parent)
	if !recognized || key != client.ObjectKeyFromObject(gateway) || gateway.DeletionTimestamp != nil {
		return nil, &AttachmentError{Reason: gatewayv1.RouteReasonNoMatchingParent, Message: "parent does not identify an available Gateway"}
	}
	var selected, allowed bool
	var result []gatewaymodel.RouteAttachment
	routeHosts := make([]string, len(route.Spec.Hostnames))
	for i, hostname := range route.Spec.Hostnames {
		routeHosts[i] = string(hostname)
	}
	for _, listener := range gateway.Spec.Listeners {
		if (parent.SectionName != nil && *parent.SectionName != listener.Name) || (parent.Port != nil && *parent.Port != listener.Port) {
			continue
		}
		selected = true
		if err := ValidateListener(listener); err != nil {
			continue
		}
		from := gatewayv1.NamespacesFromSame
		if listener.AllowedRoutes != nil && listener.AllowedRoutes.Namespaces != nil {
			from = ptr.Deref(listener.AllowedRoutes.Namespaces.From, gatewayv1.NamespacesFromSame)
		}
		switch from {
		case gatewayv1.NamespacesFromSame:
			if route.Namespace != gateway.Namespace {
				continue
			}
		case gatewayv1.NamespacesFromSelector:
			selector, err := metav1.LabelSelectorAsSelector(listener.AllowedRoutes.Namespaces.Selector)
			if err != nil {
				return nil, err
			}
			if !selector.Matches(labels.Set(namespaceLabels)) {
				continue
			}
		}
		allowed = true
		hosts, matches, err := gatewaymodel.IntersectHostnames(string(ptr.Deref(listener.Hostname, "")), routeHosts)
		if err != nil {
			return nil, &AttachmentError{Reason: gatewayv1.RouteReasonUnsupportedValue, Message: err.Error()}
		}
		if matches {
			result = append(result, gatewaymodel.RouteAttachment{Listener: string(listener.Name), Hostnames: hosts})
		}
	}
	if len(result) != 0 {
		return result, nil
	}
	reason := gatewayv1.RouteReasonNoMatchingParent
	if selected {
		reason = gatewayv1.RouteReasonNotAllowedByListeners
	}
	if allowed {
		reason = gatewayv1.RouteReasonNoMatchingListenerHostname
	}
	return nil, &AttachmentError{Reason: reason, Message: "no selected listener permits this route namespace and hostname"}
}

// ValidateHTTPRoute rejects intent that the initial HTTP preview cannot preserve.
func ValidateHTTPRoute(route *gatewayv1.HTTPRoute) error {
	if len(route.Spec.Rules) == 0 {
		return fmt.Errorf("HTTPRoute requires at least one forwarding rule")
	}
	for i, rule := range route.Spec.Rules {
		if len(rule.BackendRefs) != 1 || len(rule.Filters) != 0 || rule.Timeouts != nil || rule.Retry != nil || rule.SessionPersistence != nil {
			return fmt.Errorf("rule %d requires one backend without filters, timeouts, retries or session persistence", i)
		}
		backend := rule.BackendRefs[0]
		if ptr.Deref(backend.Weight, 1) < 1 || ptr.Deref(backend.Weight, 1) > 1000000 || len(backend.Filters) != 0 {
			return fmt.Errorf("rule %d requires a positive backend weight at most 1000000 and no backend filters", i)
		}
		for _, match := range rule.Matches {
			if len(match.Headers) != 0 || len(match.QueryParams) != 0 || match.Method != nil {
				return fmt.Errorf("rule %d header, query and method matching are not supported", i)
			}
			if err := httpMatch(match).Validate(); err != nil {
				return fmt.Errorf("rule %d: %w", i, err)
			}
		}
	}
	return nil
}

// ResolveBackend authorizes a reference before reading its target and uses the
// same source-service field index as the existing hub networking controllers.
func ResolveBackend(ctx context.Context, reader client.Reader, namespace string, ref gatewayv1.HTTPBackendRef) (gatewaymodel.Backend, error) {
	return resolveBackend(ctx, reader, namespace, ref, true)
}

func resolveBackend(ctx context.Context, reader client.Reader, namespace string, ref gatewayv1.HTTPBackendRef, indexed bool) (gatewaymodel.Backend, error) {
	if err := AuthorizeBackendReference(ctx, reader, namespace, ref); err != nil {
		return gatewaymodel.Backend{}, err
	}
	targetNamespace := string(ptr.Deref(ref.Namespace, gatewayv1.Namespace(namespace)))
	var service fleetnetv1alpha1.ServiceImport
	if err := reader.Get(ctx, types.NamespacedName{Namespace: targetNamespace, Name: string(ref.Name)}, &service); err != nil {
		if apierrors.IsNotFound(err) {
			return Resolve(namespace, ref, nil, nil)
		}
		return gatewaymodel.Backend{}, fmt.Errorf("get backend ServiceImport: %w", err)
	}
	var exports fleetnetv1alpha1.InternalServiceExportList
	var options []client.ListOption
	if indexed {
		options = append(options, client.MatchingFields{ExportServiceField: targetNamespace + "/" + string(ref.Name)})
	}
	if err := reader.List(ctx, &exports, options...); err != nil {
		return gatewaymodel.Backend{}, fmt.Errorf("list backend internal exports: %w", err)
	}
	return Resolve(namespace, ref, &service, exports.Items)
}

// AuthorizeBackendReference checks the supported kind and ReferenceGrants without
// reading the target, so unavailable backends cannot prevent policy withdrawal.
func AuthorizeBackendReference(ctx context.Context, reader client.Reader, namespace string, ref gatewayv1.HTTPBackendRef) error {
	if ptr.Deref(ref.Group, "") != gatewayv1.Group(fleetnetv1alpha1.GroupVersion.Group) || ptr.Deref(ref.Kind, "Service") != serviceImportKind {
		return newResolutionError("InvalidKind", false, "only %s/ServiceImport backends are supported", fleetnetv1alpha1.GroupVersion.Group)
	}
	targetNamespace := string(ptr.Deref(ref.Namespace, gatewayv1.Namespace(namespace)))
	if targetNamespace == namespace {
		return nil
	}
	// Listener attachment permission does not authorize cross-namespace backend reads.
	var grants gatewayv1beta1.ReferenceGrantList
	if err := reader.List(ctx, &grants, client.InNamespace(targetNamespace)); err != nil {
		return fmt.Errorf("list backend ReferenceGrants: %w", err)
	}
	if !referencePermitted(namespace, ref.Name, grants.Items) {
		return newResolutionError(string(gatewayv1.RouteReasonRefNotPermitted), false,
			"no ReferenceGrant permits HTTPRoute namespace %q to reference %s/%s", namespace, targetNamespace, ref.Name)
	}
	return nil
}

func referencePermitted(namespace string, name gatewayv1.ObjectName, grants []gatewayv1beta1.ReferenceGrant) bool {
	for _, grant := range grants {
		if grant.DeletionTimestamp != nil {
			continue
		}
		fromAllowed := false
		for _, from := range grant.Spec.From {
			if from.Group == gatewayv1.GroupName && from.Kind == "HTTPRoute" && string(from.Namespace) == namespace {
				fromAllowed = true
			}
		}
		if !fromAllowed {
			continue
		}
		for _, to := range grant.Spec.To {
			if to.Group == gatewayv1.Group(fleetnetv1alpha1.GroupVersion.Group) && to.Kind == serviceImportKind && (to.Name == nil || *to.Name == name) {
				return true
			}
		}
	}
	return false
}

// ResolveHTTPRoute builds rule-scoped private intent. An unavailable origin does
// not hide invalid references in subsequent rules or produce a partial route.
func ResolveHTTPRoute(ctx context.Context, reader client.Reader, route *gatewayv1.HTTPRoute,
	attachments []gatewaymodel.RouteAttachment) (gatewaymodel.Route, error) {
	return resolveHTTPRoute(ctx, reader, route, attachments, true)
}

// ResolveHTTPRouteUnindexed applies the same authorization and backend validation
// without cache-only export selectors. Callers requiring current API observations
// must supply an uncached reader; all exports are listed and filtered by identity.
func ResolveHTTPRouteUnindexed(ctx context.Context, reader client.Reader, route *gatewayv1.HTTPRoute,
	attachments []gatewaymodel.RouteAttachment) (gatewaymodel.Route, error) {
	return resolveHTTPRoute(ctx, reader, route, attachments, false)
}

func resolveHTTPRoute(ctx context.Context, reader client.Reader, route *gatewayv1.HTTPRoute,
	attachments []gatewaymodel.RouteAttachment, indexed bool) (gatewaymodel.Route, error) {
	if err := ValidateHTTPRoute(route); err != nil {
		return gatewaymodel.Route{}, err
	}
	result := gatewaymodel.Route{Namespace: route.Namespace, Name: route.Name, UID: string(route.UID),
		CreationTimestamp: route.CreationTimestamp.Time, Attachments: attachments}
	var unavailable error
	for _, rule := range route.Spec.Rules {
		backend, err := resolveBackend(ctx, reader, route.Namespace, rule.BackendRefs[0], indexed)
		if err != nil {
			var resolutionErr *ResolutionError
			if !errors.As(err, &resolutionErr) || !resolutionErr.ReferenceValid {
				return gatewaymodel.Route{}, err
			}
			if unavailable == nil {
				unavailable = err
			}
			continue
		}
		normalizedRule := gatewaymodel.HTTPRule{Backends: []gatewaymodel.Backend{backend}}
		for _, match := range rule.Matches {
			normalizedRule.Matches = append(normalizedRule.Matches, httpMatch(match))
		}
		result.Rules = append(result.Rules, normalizedRule)
	}
	if unavailable != nil {
		return gatewaymodel.Route{}, unavailable
	}
	return result, nil
}

func httpMatch(match gatewayv1.HTTPRouteMatch) gatewaymodel.HTTPMatch {
	result := gatewaymodel.HTTPMatch{PathType: "PathPrefix", Path: "/"}
	if match.Path != nil {
		result.PathType = string(ptr.Deref(match.Path.Type, gatewayv1.PathMatchPathPrefix))
		result.Path = ptr.Deref(match.Path.Value, "/")
	}
	return result
}
