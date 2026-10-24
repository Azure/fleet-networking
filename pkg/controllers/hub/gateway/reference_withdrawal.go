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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewayresolver"
	"go.goms.io/fleet-networking/pkg/providers/azure/frontdoor"
)

type referenceDenial struct {
	hubUID string
	route  *gatewayv1.HTTPRoute
}

func (r *Reconciler) suspendDeniedReferences(ctx context.Context, gateway *gatewayv1.Gateway) (*gatewayStaging, error) {
	if !controllerutil.ContainsFinalizer(gateway, objectmeta.GatewayAFDCleanupFinalizer) {
		return nil, nil
	}
	if r.APIReader == nil || r.ProfileCleanup == nil || gateway.UID == "" {
		return nil, fmt.Errorf("uncached reader, cleanup provider and existing Gateway identity are required before reference withdrawal")
	}
	hubUID, err := r.provisioningHubUID(ctx)
	if err != nil {
		return nil, err
	}
	current, class, err := r.currentGatewayIdentity(ctx, gateway, hubUID)
	if err != nil {
		return nil, err
	}
	if !controllerutil.ContainsFinalizer(current, objectmeta.GatewayAFDCleanupFinalizer) || !gatewayClassAccepted(class) {
		return nil, errProvisioningIntentChanged
	}
	denial, scanErr := r.findDeniedReference(ctx, current, hubUID)
	if denial == nil {
		return nil, scanErr
	}
	staging := &gatewayStaging{uid: gateway.UID, generation: gateway.Generation, denial: denial,
		suspension: &frontdoor.EndpointSuspensionResult{}}
	confirmed, validationErr := r.currentReferenceDenial(ctx, gateway, denial)
	if !confirmed {
		staging.err = validationErr
		if staging.err == nil {
			staging.err = errProvisioningIntentChanged
		}
		return staging, errors.Join(scanErr, staging.err)
	}
	if err := ctx.Err(); err != nil {
		staging.err = err
		return staging, errors.Join(scanErr, validationErr, err)
	}
	owner := frontdoor.GatewayOwner{HubUID: hubUID, Namespace: current.Namespace, Name: current.Name, UID: string(current.UID)}
	// Whole-endpoint fail-closed withdrawal intentionally interrupts permitted peers.
	// Sequential revalidation narrows races; it is not an atomic policy snapshot.
	start := time.Now()
	result, err := r.suspendEndpoint(ctx, owner)
	r.recordGraphOperation(gateway, graphSuspendDenied, time.Since(start), err)
	*staging.suspension, staging.err = result, err
	return staging, errors.Join(scanErr, validationErr, err)
}

func (r *Reconciler) findDeniedReference(ctx context.Context, gateway *gatewayv1.Gateway, hubUID string) (*referenceDenial, error) {
	var routes gatewayv1.HTTPRouteList
	// The API reader cannot use the controller cache's custom parent field index.
	if err := r.APIReader.List(ctx, &routes); err != nil {
		return nil, fmt.Errorf("list HTTPRoutes before reference withdrawal: %w", err)
	}
	var readErr error
	for i := range routes.Items {
		route := &routes.Items[i]
		denied, err := r.routeReferenceDenied(ctx, gateway, route)
		readErr = errors.Join(readErr, err)
		if denied {
			return &referenceDenial{hubUID: hubUID, route: route.DeepCopy()}, readErr
		}
	}
	return nil, readErr
}

func (r *Reconciler) routeReferenceDenied(ctx context.Context, gateway *gatewayv1.Gateway, route *gatewayv1.HTTPRoute) (bool, error) {
	// Unsupported routes are ineligible, not failed authorization reads.
	eligible := route.UID != "" && route.DeletionTimestamp == nil && gatewayresolver.ValidateHTTPRoute(route) == nil &&
		referencesGateway(route, client.ObjectKeyFromObject(gateway))
	if !eligible {
		return false, nil
	}
	var namespace corev1.Namespace
	if err := r.APIReader.Get(ctx, client.ObjectKey{Name: route.Namespace}, &namespace); err != nil {
		return false, fmt.Errorf("read route namespace before reference withdrawal: %w", err)
	}
	attachments, err := resolveRouteAttachments(gateway.DeepCopy(), route, namespace.Labels)
	if err != nil || len(attachments) == 0 {
		return false, err
	}
	var readErr error
	for _, rule := range route.Spec.Rules {
		// ValidateHTTPRoute guarantees exactly one effective backend per rule.
		err := gatewayresolver.AuthorizeBackendReference(ctx, r.APIReader, route.Namespace, rule.BackendRefs[0])
		if err == nil {
			continue
		}
		var resolutionErr *gatewayresolver.ResolutionError
		if errors.As(err, &resolutionErr) {
			if resolutionErr.Reason == string(gatewayv1.RouteReasonRefNotPermitted) {
				return true, readErr
			}
		} else {
			// Unknown reads are not denial, nor may they hide a separate positive proof.
			readErr = errors.Join(readErr, err)
		}
	}
	return false, readErr
}

func (r *Reconciler) currentReferenceDenial(ctx context.Context, expected *gatewayv1.Gateway, denial *referenceDenial) (bool, error) {
	current, class, err := r.currentGatewayIdentity(ctx, expected, denial.hubUID)
	if err != nil {
		return false, err
	}
	if !controllerutil.ContainsFinalizer(current, objectmeta.GatewayAFDCleanupFinalizer) || !gatewayClassAccepted(class) {
		return false, errProvisioningIntentChanged
	}
	var route gatewayv1.HTTPRoute
	if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(denial.route), &route); err != nil {
		return false, client.IgnoreNotFound(err)
	}
	if route.UID != denial.route.UID || route.Generation != denial.route.Generation || !reflect.DeepEqual(route.Spec, denial.route.Spec) {
		return false, nil
	}
	return r.routeReferenceDenied(ctx, current, &route)
}

func (r *Reconciler) applyReferenceWithdrawal(ctx context.Context, gateway *gatewayv1.Gateway, staging *gatewayStaging) error {
	if staging == nil || staging.denial == nil || gateway.UID != staging.uid || gateway.Generation != staging.generation {
		return nil
	}
	confirmed, err := r.currentReferenceDenial(ctx, gateway, staging.denial)
	message := "Authorization changed during AFD withdrawal; current graph is not programmed"
	switch {
	case !confirmed && err != nil:
		message = "AFD withdrawal authorization could not be revalidated: " + err.Error()
	case confirmed && staging.err != nil:
		message = "AFD endpoint suspension failed: " + staging.err.Error()
	case confirmed:
		message = fmt.Sprintf("AFD endpoint suspension: %s; ReferenceGrant denies HTTPRoute %s/%s; whole-endpoint withdrawal also interrupts permitted routes; ingress is not programmed",
			staging.suspension.State, staging.denial.route.Namespace, staging.denial.route.Name)
	}
	if confirmed && err != nil {
		message += "; other authorization reads failed: " + err.Error()
	}
	gateway.Status.Addresses = nil
	meta.SetStatusCondition(&gateway.Status.Conditions, metav1.Condition{Type: string(gatewayv1.GatewayConditionProgrammed),
		Status: metav1.ConditionFalse, Reason: string(gatewayv1.GatewayReasonPending), Message: message, ObservedGeneration: gateway.Generation})
	for i := range gateway.Status.Listeners {
		meta.SetStatusCondition(&gateway.Status.Listeners[i].Conditions, metav1.Condition{Type: string(gatewayv1.ListenerConditionProgrammed),
			Status: metav1.ConditionFalse, Reason: string(gatewayv1.ListenerReasonPending), Message: message, ObservedGeneration: gateway.Generation})
	}
	return err
}
