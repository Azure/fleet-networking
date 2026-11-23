/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gateway

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayv1alpha3 "sigs.k8s.io/gateway-api/apis/v1alpha3"

	"go.goms.io/fleet-networking/pkg/annotations"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/common/origintls"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewayresolver"
)

type tlsProjectionIntent struct {
	backend   gatewaymodel.Backend
	ancestors map[client.ObjectKey]bool
}

// reconcileTLSProjections uses complete current route inventory so a policy
// belongs to its ServiceImport, never to the first referencing Gateway.
func (r *Reconciler) reconcileTLSProjections(ctx context.Context) error {
	if r.APIReader == nil {
		return fmt.Errorf("TLS projection requires an uncached API reader")
	}
	var routes gatewayv1.HTTPRouteList
	if err := r.APIReader.List(ctx, &routes); err != nil {
		return fmt.Errorf("list TLS projection references: %w", err)
	}
	desired := make(map[client.ObjectKey]*tlsProjectionIntent)
	gateways := make(map[client.ObjectKey]*gatewayv1.Gateway)
	classes := make(map[string]*gatewayv1.GatewayClass)
	var failures []error
	for i := range routes.Items {
		route := &routes.Items[i]
		if route.DeletionTimestamp != nil || len(route.Spec.Hostnames) != 0 || gatewayresolver.ValidateHTTPRoute(route) != nil {
			continue
		}
		ancestors, err := r.tlsProjectionAncestors(ctx, route, gateways, classes)
		if err != nil {
			return err
		}
		if len(ancestors) == 0 {
			continue
		}
		for _, rule := range route.Spec.Rules {
			rootOnly := true
			for _, match := range rule.Matches {
				if match.Path != nil && (ptr.Deref(match.Path.Type, gatewayv1.PathMatchPathPrefix) != gatewayv1.PathMatchPathPrefix || ptr.Deref(match.Path.Value, "/") != "/") {
					rootOnly = false
				}
			}
			if !rootOnly {
				continue
			}
			backend, err := gatewayresolver.ResolveBackendUnindexed(ctx, r.APIReader, route.Namespace, rule.BackendRefs[0], true)
			if err != nil {
				var resolution *gatewayresolver.ResolutionError
				if !errors.As(err, &resolution) {
					return err
				}
				continue
			}
			key := client.ObjectKey{Namespace: backend.Namespace, Name: backend.Name}
			intent := desired[key]
			if intent == nil {
				intent = &tlsProjectionIntent{backend: backend, ancestors: make(map[client.ObjectKey]bool)}
				desired[key] = intent
			}
			if intent.backend.TLSProvenance != backend.TLSProvenance || intent.backend.TLSHostname != backend.TLSHostname {
				return fmt.Errorf("TLS backend source set changed during projection inventory")
			}
			for _, ancestor := range ancestors {
				intent.ancestors[ancestor] = true
			}
		}
	}
	// Narrow cleanup to Fleet-labelled policies, then verify target UID ownership.
	var existing gatewayv1alpha3.BackendTLSPolicyList
	if err := r.APIReader.List(ctx, &existing, client.MatchingLabels{origintls.ProjectionLabel: "true"}); err != nil {
		return err
	}
	for i := range existing.Items {
		policy := &existing.Items[i]
		if len(policy.OwnerReferences) != 1 {
			continue
		}
		owner := policy.OwnerReferences[0]
		key := client.ObjectKey{Namespace: policy.Namespace, Name: owner.Name}
		if desired[key] != nil {
			continue
		}
		backend := gatewaymodel.Backend{Namespace: policy.Namespace, Name: owner.Name, TargetUID: string(owner.UID)}
		if !gatewayresolver.OwnsTLSProjection(policy, backend) {
			continue
		}
		if err := r.Client.Delete(ctx, policy, client.Preconditions{UID: &policy.UID, ResourceVersion: &policy.ResourceVersion}); client.IgnoreNotFound(err) != nil {
			failures = append(failures, err)
		}
	}
	keys := make([]client.ObjectKey, 0, len(desired))
	for key := range desired {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
	for _, key := range keys {
		if err := r.ensureTLSProjection(ctx, desired[key]); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (r *Reconciler) tlsProjectionAncestors(ctx context.Context, route *gatewayv1.HTTPRoute,
	gateways map[client.ObjectKey]*gatewayv1.Gateway, classes map[string]*gatewayv1.GatewayClass) ([]client.ObjectKey, error) {
	ancestors := make([]client.ObjectKey, 0, len(route.Spec.ParentRefs))
	for _, parent := range route.Spec.ParentRefs {
		key, recognized := gatewayresolver.GatewayParentKey(route.Namespace, parent)
		if !recognized {
			continue
		}
		gateway, seen := gateways[key]
		if !seen {
			gateway = &gatewayv1.Gateway{}
			if err := r.APIReader.Get(ctx, key, gateway); err != nil {
				if apierrors.IsNotFound(err) {
					gateways[key] = nil
					continue
				}
				return nil, err
			}
			gateways[key] = gateway
		}
		if gateway == nil || gateway.DeletionTimestamp != nil || len(gateway.Spec.Listeners) != 1 ||
			gateway.Spec.Listeners[0].Protocol != gatewayv1.HTTPSProtocolType ||
			gatewayresolver.ValidateListener(gateway.Spec.Listeners[0], true) != nil ||
			len(gateway.Spec.Addresses) != 0 || gateway.Spec.Infrastructure != nil || gateway.Spec.BackendTLS != nil {
			continue
		}
		config, err := annotations.ParseGatewayConfig(gateway.Annotations)
		if err != nil || config.WAFPolicyID != "" {
			continue
		}
		classKey := string(gateway.Spec.GatewayClassName)
		class, seen := classes[classKey]
		if !seen {
			class = &gatewayv1.GatewayClass{}
			if err := r.APIReader.Get(ctx, client.ObjectKey{Name: classKey}, class); err != nil {
				if apierrors.IsNotFound(err) {
					classes[classKey] = nil
					continue
				}
				return nil, err
			}
			classes[classKey] = class
		}
		if class == nil || class.Spec.ControllerName != gatewayv1.GatewayController(objectmeta.GatewayAPIControllerName) || !gatewayClassAccepted(class) {
			continue
		}
		var namespace corev1.Namespace
		if err := r.APIReader.Get(ctx, client.ObjectKey{Name: route.Namespace}, &namespace); err != nil {
			return nil, err
		}
		attachments, err := gatewayresolver.ResolveAttachments(gateway, route, parent, namespace.Labels, true)
		if err != nil || len(attachments) == 0 {
			continue
		}
		ancestors = append(ancestors, key)
	}
	return ancestors, nil
}

func (r *Reconciler) ensureTLSProjection(ctx context.Context, intent *tlsProjectionIntent) error {
	expected := gatewayresolver.TLSProjection(intent.backend)
	var actual gatewayv1alpha3.BackendTLSPolicy
	err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(expected), &actual)
	if apierrors.IsNotFound(err) {
		return r.Client.Create(ctx, expected)
	}
	if err != nil {
		return err
	}
	if !gatewayresolver.OwnsTLSProjection(&actual, intent.backend) || actual.DeletionTimestamp != nil {
		return fmt.Errorf("refusing to adopt foreign or deleting hub TLS policy %s", client.ObjectKeyFromObject(&actual))
	}
	before := actual.DeepCopy()
	actual.Spec = expected.Spec
	if actual.Annotations == nil {
		actual.Annotations = make(map[string]string)
	}
	actual.Annotations[origintls.ProvenanceAnnotation] = intent.backend.TLSProvenance
	if !reflect.DeepEqual(before.Spec, actual.Spec) || !reflect.DeepEqual(before.Annotations, actual.Annotations) {
		return r.Client.Patch(ctx, &actual, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
	}
	if err := gatewayresolver.ValidateTLSProjection(ctx, r.APIReader, intent.backend); err != nil {
		return err
	}
	before = actual.DeepCopy()
	ancestors := make([]gatewayv1alpha2.PolicyAncestorStatus, 0, len(actual.Status.Ancestors)+len(intent.ancestors))
	for _, ancestor := range actual.Status.Ancestors {
		if ancestor.ControllerName != gatewayv1.GatewayController(objectmeta.GatewayAPIControllerName) {
			ancestors = append(ancestors, ancestor)
		}
	}
	keys := make([]client.ObjectKey, 0, len(intent.ancestors))
	for key := range intent.ancestors {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
	if len(ancestors)+len(keys) > 16 {
		return fmt.Errorf("hub TLS policy exceeds Gateway API ancestor capacity")
	}
	for _, key := range keys {
		status := gatewayv1alpha2.PolicyAncestorStatus{
			AncestorRef:    gatewayv1.ParentReference{Group: ptr.To(gatewayv1.Group(gatewayv1.GroupName)), Kind: ptr.To(gatewayv1.Kind("Gateway")), Namespace: ptr.To(gatewayv1.Namespace(key.Namespace)), Name: gatewayv1.ObjectName(key.Name)},
			ControllerName: gatewayv1.GatewayController(objectmeta.GatewayAPIControllerName),
		}
		for _, old := range actual.Status.Ancestors {
			if old.ControllerName == status.ControllerName && reflect.DeepEqual(old.AncestorRef, status.AncestorRef) {
				status.Conditions = old.Conditions
			}
		}
		meta.SetStatusCondition(&status.Conditions, metav1.Condition{Type: "Accepted", Status: metav1.ConditionTrue, Reason: "Accepted",
			Message: "Current member TLS intent agrees; certificate trust and traffic are verified independently", ObservedGeneration: actual.Generation})
		ancestors = append(ancestors, status)
	}
	actual.Status.Ancestors = ancestors
	if reflect.DeepEqual(before.Status, actual.Status) {
		return nil
	}
	return r.Client.Status().Patch(ctx, &actual, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
}
