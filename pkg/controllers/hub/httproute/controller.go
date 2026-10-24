/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package httproute reconciles HTTPRoute attachment and private backend references.
package httproute

import (
	"context"
	"errors"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewayresolver"
)

// Reconciler owns only the AFD controller's HTTPRoute parent statuses.
type Reconciler struct {
	Client client.Client
}

// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes;referencegrants,verbs=get;list;watch
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes/status,verbs=get;patch;update
// +kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceimports;internalserviceexports,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch

// Reconcile replaces owned parent status while preserving other controllers.
func (r *Reconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var route gatewayv1.HTTPRoute
		if err := r.Client.Get(ctx, request.NamespacedName, &route); err != nil {
			return client.IgnoreNotFound(err)
		}
		if route.DeletionTimestamp != nil {
			return nil
		}
		before := route.DeepCopy()
		// Rebuild our entries to drop withdrawn parents without touching other controllers.
		route.Status.Parents = nil
		for _, parent := range before.Status.Parents {
			if parent.ControllerName != gatewayv1.GatewayController(objectmeta.GatewayAPIControllerName) {
				route.Status.Parents = append(route.Status.Parents, parent)
			}
		}
		for _, parent := range route.Spec.ParentRefs {
			key, recognized := gatewayresolver.GatewayParentKey(route.Namespace, parent)
			if !recognized {
				continue
			}
			var gateway gatewayv1.Gateway
			if err := r.Client.Get(ctx, key, &gateway); err != nil {
				if apierrors.IsNotFound(err) {
					continue
				}
				return err
			}
			var class gatewayv1.GatewayClass
			if err := r.Client.Get(ctx, client.ObjectKey{Name: string(gateway.Spec.GatewayClassName)}, &class); err != nil {
				if apierrors.IsNotFound(err) {
					continue
				}
				return err
			}
			if class.Spec.ControllerName != gatewayv1.GatewayController(objectmeta.GatewayAPIControllerName) {
				continue
			}
			status := gatewayv1.RouteParentStatus{ParentRef: parent, ControllerName: gatewayv1.GatewayController(objectmeta.GatewayAPIControllerName)}
			for _, previous := range before.Status.Parents {
				if previous.ControllerName == status.ControllerName && equality.Semantic.DeepEqual(previous.ParentRef, parent) {
					status.Conditions = append([]metav1.Condition(nil), previous.Conditions...)
					break
				}
			}
			if err := r.resolve(ctx, &route, &gateway, &class, &status); err != nil {
				return err
			}
			route.Status.Parents = append(route.Status.Parents, status)
		}
		if equality.Semantic.DeepEqual(before.Status, route.Status) {
			return nil
		}
		return r.Client.Status().Patch(ctx, &route, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
	})
	return ctrl.Result{}, err
}

func (r *Reconciler) resolve(ctx context.Context, route *gatewayv1.HTTPRoute, gateway *gatewayv1.Gateway,
	class *gatewayv1.GatewayClass, status *gatewayv1.RouteParentStatus) error {
	accepted := metav1.Condition{Type: string(gatewayv1.RouteConditionAccepted), Status: metav1.ConditionTrue,
		Reason: string(gatewayv1.RouteReasonAccepted), Message: "HTTPRoute attaches to the selected HTTP listeners", ObservedGeneration: route.Generation}
	routeErr := gatewayresolver.ValidateHTTPRoute(route)
	if routeErr != nil {
		accepted.Status, accepted.Reason, accepted.Message = metav1.ConditionFalse, string(gatewayv1.RouteReasonUnsupportedValue), routeErr.Error()
	} else {
		var namespace corev1.Namespace
		if err := r.Client.Get(ctx, client.ObjectKey{Name: route.Namespace}, &namespace); err != nil {
			return err
		}
		_, err := gatewayresolver.ResolveAttachments(gateway, route, status.ParentRef, namespace.Labels)
		if err != nil {
			var attachmentErr *gatewayresolver.AttachmentError
			if !errors.As(err, &attachmentErr) {
				return err
			}
			accepted.Status, accepted.Reason, accepted.Message = metav1.ConditionFalse, string(attachmentErr.Reason), attachmentErr.Message
		}
		gatewayAcceptance := meta.FindStatusCondition(gateway.Status.Conditions, string(gatewayv1.GatewayConditionAccepted))
		classAcceptance := meta.FindStatusCondition(class.Status.Conditions, string(gatewayv1.GatewayClassConditionStatusAccepted))
		if accepted.Status == metav1.ConditionTrue && (class.DeletionTimestamp != nil || class.Spec.ParametersRef != nil ||
			classAcceptance == nil || classAcceptance.Status != metav1.ConditionTrue || classAcceptance.ObservedGeneration != class.Generation ||
			gatewayAcceptance == nil || gatewayAcceptance.Status != metav1.ConditionTrue || gatewayAcceptance.ObservedGeneration != gateway.Generation) {
			accepted.Status, accepted.Reason, accepted.Message = metav1.ConditionFalse, string(gatewayv1.RouteReasonPending), "Gateway or GatewayClass is not currently accepted"
		}
	}
	// Reference validity is independent of supported routing intent and origin readiness.
	resolved, err := r.resolveReferences(ctx, route)
	if err != nil {
		return err
	}
	meta.SetStatusCondition(&status.Conditions, accepted)
	meta.SetStatusCondition(&status.Conditions, resolved)
	return nil
}

func (r *Reconciler) resolveReferences(ctx context.Context, route *gatewayv1.HTTPRoute) (metav1.Condition, error) {
	resolved := metav1.Condition{Type: string(gatewayv1.RouteConditionResolvedRefs), Status: metav1.ConditionTrue,
		Reason: string(gatewayv1.RouteReasonResolvedRefs), Message: "Backend references are valid; Azure provisioning is not implemented", ObservedGeneration: route.Generation}
	for _, rule := range route.Spec.Rules {
		for _, filter := range rule.Filters {
			if filter.ExtensionRef != nil || filter.RequestMirror != nil {
				resolved.Status, resolved.Reason, resolved.Message = metav1.ConditionFalse, string(gatewayv1.RouteReasonInvalidKind), "Filter references are unsupported in the HTTP preview"
				return resolved, nil
			}
		}
		for _, backend := range rule.BackendRefs {
			_, err := gatewayresolver.ResolveBackend(ctx, r.Client, route.Namespace, backend)
			if err == nil {
				continue
			}
			var resolutionErr *gatewayresolver.ResolutionError
			if !errors.As(err, &resolutionErr) {
				return metav1.Condition{}, err
			}
			resolved.Message = resolutionErr.Message
			if !resolutionErr.ReferenceValid {
				resolved.Status, resolved.Reason = metav1.ConditionFalse, resolutionErr.Reason
				if resolutionErr.Reason == "PortNotFound" {
					resolved.Reason = string(gatewayv1.RouteReasonBackendNotFound)
				}
				return resolved, nil
			}
		}
	}
	return resolved, nil
}

// SetupWithManager watches all dependencies that can authorize or revoke intent.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).For(&gatewayv1.HTTPRoute{}).
		Watches(&gatewayv1.Gateway{}, handler.EnqueueRequestsFromMapFunc(r.routesForDependency)).
		Watches(&fleetnetv1alpha1.ServiceImport{}, handler.EnqueueRequestsFromMapFunc(r.routesForDependency)).
		Watches(&fleetnetv1alpha1.InternalServiceExport{}, handler.EnqueueRequestsFromMapFunc(r.routesForDependency)).
		Watches(&gatewayv1beta1.ReferenceGrant{}, handler.EnqueueRequestsFromMapFunc(r.routesForDependency)).
		Watches(&corev1.Namespace{}, handler.EnqueueRequestsFromMapFunc(r.routesForDependency)).Complete(r)
}

func (r *Reconciler) routesForDependency(ctx context.Context, object client.Object) []reconcile.Request {
	routes, err := gatewayresolver.DependentRoutes(ctx, r.Client, object)
	if err != nil {
		klog.ErrorS(err, "Failed to list HTTPRoutes for dependency", "dependency", klog.KObj(object))
		return nil
	}
	requests := make([]reconcile.Request, 0, len(routes))
	for i := range routes {
		requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&routes[i])})
	}
	return requests
}
