/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package gateway reconciles Fleet's private HTTP Gateway desired state.
package gateway

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/annotations"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewayresolver"
	"go.goms.io/fleet-networking/pkg/providers/azure/frontdoor"
)

// Reconciler maintains the supported private HTTP Gateway graph and observed status.
type Reconciler struct {
	Client          client.Client
	APIReader       client.Reader
	ProfileCleanup  ProfileCleaner
	OriginObserver  OriginObserver
	GraphStager     DisabledGraphStager
	HTTPProvisioner HTTPPreviewProvisioner
	Recorder        record.EventRecorder
}

// DisabledGraphStager stages/prunes disabled resources or suspends an owned endpoint.
// Production uses the full HTTP provisioner, never repeated disabled-only staging.
type DisabledGraphStager interface {
	StageDisabledGraph(context.Context, frontdoor.GatewayOwner, gatewaymodel.GlobalGateway) (frontdoor.DisabledGraphStagingResult, error)
	SuspendEndpoint(context.Context, frontdoor.GatewayOwner) (frontdoor.EndpointSuspensionResult, error)
}

// HTTPPreviewProvisioner maintains the complete owned private HTTP graph.
type HTTPPreviewProvisioner interface {
	ReconcileHTTPPreview(context.Context, frontdoor.GatewayOwner, gatewaymodel.GlobalGateway) (frontdoor.HTTPPreviewProvisioningResult, error)
	SuspendEndpoint(context.Context, frontdoor.GatewayOwner) (frontdoor.EndpointSuspensionResult, error)
}

// OriginObserver reads only the expected private origins under an owned profile.
type OriginObserver interface {
	Observe(context.Context, frontdoor.GatewayOwner, gatewaymodel.GlobalGateway) (frontdoor.PrivateOriginsObservation, error)
}

// ProfileCleaner reports observed deletion of this Gateway's owned AFD profile.
type ProfileCleaner interface {
	DeleteProfile(context.Context, frontdoor.GatewayOwner) (bool, error)
}

// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=patch
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways/finalizers,verbs=update
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways/status,verbs=get;patch;update
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile refreshes generation-aware Gateway and listener status.
func (r *Reconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	var current gatewayv1.Gateway
	if err := r.Client.Get(ctx, request.NamespacedName, &current); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if current.DeletionTimestamp != nil {
		// Ownership cleanup must not depend on a still-valid class or resolvable backends.
		return r.finalize(ctx, &current)
	}
	observation, observationErr := r.observeOrigins(ctx, &current)
	staging, stagingErr := r.stageGraph(ctx, &current)
	var evaluationErr error
	var observedGateway *gatewayv1.Gateway
	var observedHTTPGateway *gatewayv1.Gateway
	// This callback can repeat; keep external mutations outside status conflict retries.
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		evaluationErr = nil
		observedGateway = nil
		observedHTTPGateway = nil
		var gateway gatewayv1.Gateway
		if err := r.Client.Get(ctx, request.NamespacedName, &gateway); err != nil {
			return client.IgnoreNotFound(err)
		}
		if gateway.DeletionTimestamp != nil {
			return nil
		}
		var class gatewayv1.GatewayClass
		if err := r.Client.Get(ctx, client.ObjectKey{Name: string(gateway.Spec.GatewayClassName)}, &class); err != nil {
			return client.IgnoreNotFound(err)
		}
		if class.Spec.ControllerName != gatewayv1.GatewayController(objectmeta.GatewayAPIControllerName) {
			return nil
		}
		before := gateway.DeepCopy()
		model, err := r.resolve(ctx, &gateway, &class)
		if err != nil {
			if r.HTTPProvisioner == nil && (staging == nil || staging.denial == nil) {
				return err
			}
			// Do not publish partial attachment counts, but still clear stale readiness.
			gateway.Status = *before.Status.DeepCopy()
			evaluationErr = err
			if r.HTTPProvisioner != nil {
				setHTTPPreviewStatus(&gateway, false, "AFD desired state could not be resolved: "+err.Error())
			}
		}
		observationApplied := applyOriginObservation(&gateway, model, observation)
		applyGraphStaging(&gateway, model, staging)
		httpApplied, httpErr := r.applyHTTPPreview(ctx, &gateway, model, staging)
		evaluationErr = errors.Join(evaluationErr, httpErr)
		evaluationErr = errors.Join(evaluationErr, r.applyReferenceWithdrawal(ctx, &gateway, staging))
		preserveProgrammedTransitions(&gateway, before)
		if equality.Semantic.DeepEqual(before.Status, gateway.Status) {
			return nil
		}
		if err := r.Client.Status().Patch(ctx, &gateway, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
			return err
		}
		pending := meta.FindStatusCondition(gateway.Status.Conditions, string(gatewayv1.GatewayConditionProgrammed))
		previous := meta.FindStatusCondition(before.Status.Conditions, string(gatewayv1.GatewayConditionProgrammed))
		if observationApplied && pending != nil && pending.Message == originObservationMessage(observation) &&
			!equality.Semantic.DeepEqual(previous, pending) {
			observedGateway = gateway.DeepCopy()
		}
		if httpApplied && (!equality.Semantic.DeepEqual(previous, pending) ||
			!equality.Semantic.DeepEqual(before.Status.Addresses, gateway.Status.Addresses)) {
			observedHTTPGateway = gateway.DeepCopy()
		}
		return nil
	})
	if err == nil && observedGateway != nil {
		// Events follow persisted current status, never a failed/overridden retry candidate.
		r.recordOriginObservationEvent(observedGateway, observation)
	}
	if err == nil && observedHTTPGateway != nil {
		r.recordHTTPPreviewEvent(observedHTTPGateway, staging.http)
	}
	if err := errors.Join(err, observationErr, stagingErr, evaluationErr); err != nil {
		return ctrl.Result{}, err
	}
	return progressRequeue(observation != nil || staging != nil), nil
}

func (r *Reconciler) resolve(ctx context.Context, gateway *gatewayv1.Gateway, class *gatewayv1.GatewayClass) (*gatewaymodel.GlobalGateway, error) {
	return resolveGateway(ctx, r.Client, gateway, class, true)
}

func resolveGateway(ctx context.Context, reader client.Reader, gateway *gatewayv1.Gateway,
	class *gatewayv1.GatewayClass, indexed bool) (*gatewaymodel.GlobalGateway, error) {
	var candidate *gatewaymodel.GlobalGateway
	accepted := metav1.Condition{Type: string(gatewayv1.GatewayConditionAccepted), Status: metav1.ConditionTrue,
		Reason: string(gatewayv1.GatewayReasonAccepted), Message: "Private HTTP Gateway configuration accepted", ObservedGeneration: gateway.Generation}
	switch {
	case !gatewayClassAccepted(class):
		accepted.Status, accepted.Reason, accepted.Message = metav1.ConditionFalse, string(gatewayv1.GatewayReasonInvalidParameters), "GatewayClass is not currently accepted"
	case len(gateway.Spec.Addresses) != 0:
		accepted.Status, accepted.Reason, accepted.Message = metav1.ConditionFalse, string(gatewayv1.GatewayReasonUnsupportedAddress), "User-supplied Gateway addresses are unsupported"
	case gateway.Spec.Infrastructure != nil || gateway.Spec.BackendTLS != nil:
		accepted.Status, accepted.Reason, accepted.Message = metav1.ConditionFalse, string(gatewayv1.GatewayReasonInvalidParameters), "Infrastructure and backend TLS settings are unsupported"
	}
	config, configErr := annotations.ParseGatewayConfig(gateway.Annotations)
	if configErr != nil {
		accepted.Status, accepted.Reason, accepted.Message = metav1.ConditionFalse, string(gatewayv1.GatewayReasonInvalidParameters), configErr.Error()
	}
	model := gatewaymodel.GlobalGateway{Namespace: gateway.Namespace, Name: gateway.Name, UID: string(gateway.UID)}
	if configErr == nil {
		model.WAFPolicyID = config.WAFPolicyID
	}
	resolveListeners(gateway, &model, &accepted)
	pending := "Awaiting observed AFD private HTTP graph convergence"
	if accepted.Status == metav1.ConditionTrue {
		var routes gatewayv1.HTTPRouteList
		var options []client.ListOption
		if indexed {
			options = append(options, client.MatchingFields{gatewayresolver.HTTPRouteGatewayField: gateway.Namespace + "/" + gateway.Name})
		}
		if err := reader.List(ctx, &routes, options...); err != nil {
			return nil, err
		}
		complete := true
		denied := false
		for i := range routes.Items {
			route := &routes.Items[i]
			if route.DeletionTimestamp != nil || gatewayresolver.ValidateHTTPRoute(route) != nil ||
				!referencesGateway(route, client.ObjectKeyFromObject(gateway)) {
				continue
			}
			var namespace corev1.Namespace
			if err := reader.Get(ctx, client.ObjectKey{Name: route.Namespace}, &namespace); err != nil {
				return nil, err
			}
			attachments, err := resolveRouteAttachments(gateway, route, namespace.Labels)
			if err != nil {
				return nil, err
			}
			if len(attachments) == 0 {
				continue
			}
			resolveRoute := gatewayresolver.ResolveHTTPRoute
			if !indexed {
				resolveRoute = gatewayresolver.ResolveHTTPRouteUnindexed
			}
			resolved, err := resolveRoute(ctx, reader, route, attachments)
			if err != nil {
				var resolutionErr *gatewayresolver.ResolutionError
				if !errors.As(err, &resolutionErr) {
					return nil, err
				}
				if resolutionErr.Reason == string(gatewayv1.RouteReasonRefNotPermitted) {
					denied = true
				} else {
					complete = false
				}
				pending = resolutionErr.Message
				continue
			}
			model.Routes = append(model.Routes, resolved)
		}
		if complete && (!denied || len(model.Routes) == 0) {
			// Explicit denials can prove empty intent, never a safe resolvable subset.
			normalized, err := gatewaymodel.Normalize(model)
			if err != nil {
				return nil, fmt.Errorf("normalize Gateway desired state: %w", err)
			}
			if err := preflightHTTPPreview(normalized); err != nil {
				pending = err.Error()
			} else {
				candidate = &normalized
			}
		}
	} else {
		pending = accepted.Message
	}
	// Accepted intent is not evidence of Azure provisioning or Private Link approval.
	gateway.Status.Addresses = nil
	meta.SetStatusCondition(&gateway.Status.Conditions, accepted)
	meta.SetStatusCondition(&gateway.Status.Conditions, metav1.Condition{Type: string(gatewayv1.GatewayConditionProgrammed),
		Status: metav1.ConditionFalse, Reason: string(gatewayv1.GatewayReasonPending), Message: pending, ObservedGeneration: gateway.Generation})
	return candidate, nil
}

func gatewayClassAccepted(class *gatewayv1.GatewayClass) bool {
	acceptance := meta.FindStatusCondition(class.Status.Conditions, string(gatewayv1.GatewayClassConditionStatusAccepted))
	return class.DeletionTimestamp == nil && class.Spec.ParametersRef == nil && acceptance != nil &&
		acceptance.Status == metav1.ConditionTrue && acceptance.ObservedGeneration == class.Generation
}

func preflightHTTPPreview(model gatewaymodel.GlobalGateway) error {
	if _, err := frontdoor.BuildBackendRequests(model); err != nil {
		return err
	}
	return frontdoor.PreflightHTTPPreviewRouting(model)
}

func referencesGateway(route *gatewayv1.HTTPRoute, gateway client.ObjectKey) bool {
	for _, parent := range route.Spec.ParentRefs {
		if key, recognized := gatewayresolver.GatewayParentKey(route.Namespace, parent); recognized && key == gateway {
			return true
		}
	}
	return false
}

func resolveRouteAttachments(gateway *gatewayv1.Gateway, route *gatewayv1.HTTPRoute,
	namespaceLabels map[string]string) ([]gatewaymodel.RouteAttachment, error) {
	var attachments []gatewaymodel.RouteAttachment
	seen := make(map[string]bool)
	for _, parent := range route.Spec.ParentRefs {
		resolved, err := gatewayresolver.ResolveAttachments(gateway, route, parent, namespaceLabels)
		if err != nil {
			var attachmentErr *gatewayresolver.AttachmentError
			if !errors.As(err, &attachmentErr) {
				return nil, err
			}
			continue
		}
		for _, attachment := range resolved {
			// Multiple parentRefs may select the same listener; count the route once.
			if !seen[attachment.Listener] {
				seen[attachment.Listener] = true
				attachments = append(attachments, attachment)
			}
		}
	}
	for i := range gateway.Status.Listeners {
		if seen[string(gateway.Status.Listeners[i].Name)] {
			gateway.Status.Listeners[i].AttachedRoutes++
		}
	}
	return attachments, nil
}

// SetupWithManager registers Gateway, class and route dependency watches.
// Watch backends directly: readiness may change while HTTPRoute refs remain valid.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).For(&gatewayv1.Gateway{}).
		Watches(&gatewayv1.GatewayClass{}, handler.EnqueueRequestsFromMapFunc(r.gatewaysForClass)).
		Watches(&gatewayv1.HTTPRoute{}, handler.EnqueueRequestsFromMapFunc(r.gatewaysForRoute)).
		Watches(&fleetnetv1alpha1.ServiceImport{}, handler.EnqueueRequestsFromMapFunc(r.gatewaysForDependency)).
		Watches(&fleetnetv1alpha1.InternalServiceExport{}, handler.EnqueueRequestsFromMapFunc(r.gatewaysForDependency)).
		Watches(&gatewayv1beta1.ReferenceGrant{}, handler.EnqueueRequestsFromMapFunc(r.gatewaysForDependency)).
		Watches(&corev1.Namespace{}, handler.EnqueueRequestsFromMapFunc(r.gatewaysForDependency)).Complete(r)
}

func (r *Reconciler) gatewaysForClass(ctx context.Context, object client.Object) []reconcile.Request {
	var gateways gatewayv1.GatewayList
	if err := r.Client.List(ctx, &gateways, client.MatchingFields{gatewayresolver.GatewayClassField: object.GetName()}); err != nil {
		klog.ErrorS(err, "Failed to list Gateways for GatewayClass", "class", object.GetName())
		return nil
	}
	requests := make([]reconcile.Request, 0, len(gateways.Items))
	for i := range gateways.Items {
		requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&gateways.Items[i])})
	}
	return requests
}

func (r *Reconciler) gatewaysForRoute(_ context.Context, object client.Object) []reconcile.Request {
	route := object.(*gatewayv1.HTTPRoute)
	var requests []reconcile.Request
	for _, parent := range route.Spec.ParentRefs {
		if key, recognized := gatewayresolver.GatewayParentKey(route.Namespace, parent); recognized {
			requests = append(requests, reconcile.Request{NamespacedName: key})
		}
	}
	return requests
}

func (r *Reconciler) gatewaysForDependency(ctx context.Context, object client.Object) []reconcile.Request {
	routes, err := gatewayresolver.DependentRoutes(ctx, r.Client, object)
	if err != nil {
		klog.ErrorS(err, "Failed to list HTTPRoutes for Gateway dependency", "dependency", klog.KObj(object))
		return nil
	}
	var requests []reconcile.Request
	for i := range routes {
		requests = append(requests, r.gatewaysForRoute(ctx, &routes[i])...)
	}
	return requests
}
