/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gateway

import (
	"errors"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewayresolver"
)

func resolveListeners(gateway *gatewayv1.Gateway, model *gatewaymodel.GlobalGateway, accepted *metav1.Condition) {
	previous := gateway.Status.Listeners
	gateway.Status.Listeners = nil
	for _, listener := range gateway.Spec.Listeners {
		status := gatewayv1.ListenerStatus{Name: listener.Name}
		for _, old := range previous {
			if old.Name == listener.Name {
				status.Conditions = append([]metav1.Condition(nil), old.Conditions...)
				break
			}
		}
		listenerErr := gatewayresolver.ValidateListener(listener)
		conflicted := false
		for _, other := range gateway.Spec.Listeners {
			if other.Name != listener.Name && other.Protocol == listener.Protocol && other.Port == listener.Port &&
				ptr.Deref(other.Hostname, "") == ptr.Deref(listener.Hostname, "") {
				conflicted = true
			}
		}
		listenerAccepted := metav1.Condition{Type: string(gatewayv1.ListenerConditionAccepted), Status: metav1.ConditionTrue,
			Reason: string(gatewayv1.ListenerReasonAccepted), Message: "HTTP listener configuration accepted", ObservedGeneration: gateway.Generation}
		conflict := metav1.Condition{Type: string(gatewayv1.ListenerConditionConflicted), Status: metav1.ConditionFalse,
			Reason: string(gatewayv1.ListenerReasonNoConflicts), Message: "No listener conflicts", ObservedGeneration: gateway.Generation}
		references := metav1.Condition{Type: string(gatewayv1.ListenerConditionResolvedRefs), Status: metav1.ConditionTrue,
			Reason: string(gatewayv1.ListenerReasonResolvedRefs), Message: "HTTP listener has no external references", ObservedGeneration: gateway.Generation}
		if listener.TLS != nil {
			references.Status, references.Reason, references.Message = metav1.ConditionUnknown, string(gatewayv1.ListenerReasonPending), "TLS references are not resolved by the HTTP preview"
		}
		if listenerErr != nil {
			listenerAccepted.Status, listenerAccepted.Reason, listenerAccepted.Message = metav1.ConditionFalse, string(gatewayv1.ListenerReasonInvalid), listenerErr.Error()
			var validationErr *gatewayresolver.ListenerError
			if errors.As(listenerErr, &validationErr) {
				listenerAccepted.Reason = string(validationErr.Reason)
				if validationErr.Reason == gatewayv1.ListenerReasonInvalidRouteKinds {
					references.Status, references.Reason, references.Message = metav1.ConditionFalse, string(validationErr.Reason), validationErr.Message
				}
			}
		}
		if conflicted {
			conflict.Status, conflict.Reason, conflict.Message = metav1.ConditionTrue, string(gatewayv1.ListenerReasonHostnameConflict), "Listeners have the same protocol, port and hostname"
			listenerAccepted.Status, listenerAccepted.Reason, listenerAccepted.Message = metav1.ConditionFalse, string(gatewayv1.ListenerReasonPortUnavailable), conflict.Message
		}
		if listenerAccepted.Status == metav1.ConditionFalse {
			accepted.Status, accepted.Reason, accepted.Message = metav1.ConditionFalse, string(gatewayv1.GatewayReasonListenersNotValid), "All listeners must be valid and nonconflicting in the HTTP preview"
		} else {
			status.SupportedKinds = []gatewayv1.RouteGroupKind{{Group: ptr.To(gatewayv1.Group(gatewayv1.GroupName)), Kind: "HTTPRoute"}}
		}
		meta.SetStatusCondition(&status.Conditions, listenerAccepted)
		meta.SetStatusCondition(&status.Conditions, conflict)
		meta.SetStatusCondition(&status.Conditions, references)
		meta.SetStatusCondition(&status.Conditions, metav1.Condition{Type: string(gatewayv1.ListenerConditionProgrammed),
			Status: metav1.ConditionFalse, Reason: string(gatewayv1.ListenerReasonPending), Message: "Awaiting observed AFD private HTTP graph convergence", ObservedGeneration: gateway.Generation})
		gateway.Status.Listeners = append(gateway.Status.Listeners, status)
		model.Listeners = append(model.Listeners, gatewaymodel.Listener{Name: string(listener.Name), Protocol: string(listener.Protocol),
			Port: int32(listener.Port), Hostname: string(ptr.Deref(listener.Hostname, ""))})
	}
	if len(gateway.Spec.Listeners) == 0 {
		accepted.Status, accepted.Reason, accepted.Message = metav1.ConditionFalse, string(gatewayv1.GatewayReasonListenersNotValid), "At least one HTTP listener is required"
	}
}
