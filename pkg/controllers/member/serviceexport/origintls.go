/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package serviceexport

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1alpha3 "sigs.k8s.io/gateway-api/apis/v1alpha3"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/origintls"
)

const originTLSReadyReason = "Ready"

func (r *Reconciler) resolveOriginTLS(ctx context.Context, service *corev1.Service) (*fleetnetv1alpha1.OriginTLS, error) {
	intent := &fleetnetv1alpha1.OriginTLS{ServiceUID: string(service.UID), Reason: "PolicyMissing", ObservedAt: metav1.NewTime(time.Now().UTC())}
	reader := r.MemberAPIReader
	if reader == nil {
		return nil, fmt.Errorf("origin TLS requires an uncached member API reader")
	}
	var list gatewayv1alpha3.BackendTLSPolicyList
	if err := reader.List(ctx, &list, client.InNamespace(service.Namespace)); err != nil {
		intent.Reason = "PolicyReadFailed"
		return intent, fmt.Errorf("read member TLS policies: %w", err)
	}
	var matches []*gatewayv1alpha3.BackendTLSPolicy
	for i := range list.Items {
		policy := &list.Items[i]
		for _, target := range policy.Spec.TargetRefs {
			if target.Group == "" && target.Kind == serviceKind && string(target.Name) == service.Name {
				matches = append(matches, policy)
				break
			}
		}
	}
	if len(matches) > 1 {
		intent.Reason = "DuplicatePolicies"
	}
	if len(matches) != 1 {
		for _, policy := range matches {
			r.recordTLSPolicyFailure(policy, intent.Reason)
		}
		return intent, nil
	}
	policy := matches[0]
	if policy.Annotations[origintls.ExportAnnotation] != "true" {
		intent.Reason = "PolicyNotExported"
		return intent, nil
	}
	intent.Policy = &fleetnetv1alpha1.OriginTLSPolicyReference{
		Namespace: policy.Namespace, Name: policy.Name, UID: string(policy.UID), Generation: policy.Generation,
		ResourceVersion: policy.ResourceVersion, ExportToFleet: policy.Annotations[origintls.ExportAnnotation],
	}
	intent.Reason = "UnsupportedPolicy"
	if err := origintls.Validate(policy); err != nil {
		r.recordTLSPolicyFailure(policy, intent.Reason)
		return intent, nil //nolint:nilerr // Publish invalid customer intent and its Event; only API failures need error retries.
	}
	intent.Ready, intent.Reason = true, originTLSReadyReason
	intent.Hostname, intent.Trust = string(policy.Spec.Validation.Hostname), "System"
	return intent, nil
}

func (r *Reconciler) recordTLSPolicyFailure(policy *gatewayv1alpha3.BackendTLSPolicy, reason string) {
	if r.Recorder != nil && policy.Annotations[origintls.ExportAnnotation] == "true" {
		r.Recorder.Eventf(policy, nil, corev1.EventTypeWarning, reason, "ResolveOriginTLS",
			"Fleet origin TLS intent unavailable: %s; baseline MCS export is preserved", reason)
	}
}

func (r *Reconciler) exportsForTLSPolicy(_ context.Context, object client.Object) []reconcile.Request {
	policy := object.(*gatewayv1alpha3.BackendTLSPolicy)
	var requests []reconcile.Request
	for _, target := range policy.Spec.TargetRefs {
		if target.Group == "" && target.Kind == serviceKind {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKey{Namespace: policy.Namespace, Name: string(target.Name)}})
		}
	}
	return requests
}
