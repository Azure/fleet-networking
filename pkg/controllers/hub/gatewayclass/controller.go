/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package gatewayclass reconciles GatewayClasses owned by Fleet's AFD controller.
package gatewayclass

import (
	"context"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewayresolver"
)

// Reconciler accepts the supported GatewayClass configuration.
type Reconciler struct {
	Client client.Client
}

// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gatewayclasses,verbs=get;list;watch;patch;update
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gatewayclasses/status,verbs=get;patch;update
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gatewayclasses/finalizers,verbs=update

// Reconcile maintains acceptance and the standard in-use class finalizer.
func (r *Reconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var class gatewayv1.GatewayClass
		if err := r.Client.Get(ctx, request.NamespacedName, &class); err != nil {
			return client.IgnoreNotFound(err)
		}
		if class.Spec.ControllerName != gatewayv1.GatewayController(objectmeta.GatewayAPIControllerName) {
			return nil
		}
		var gateways gatewayv1.GatewayList
		if err := r.Client.List(ctx, &gateways, client.MatchingFields{gatewayresolver.GatewayClassField: class.Name}); err != nil {
			return err
		}
		before := class.DeepCopy()
		// This protects an in-use class only; Azure cleanup belongs to a future Gateway finalizer.
		if len(gateways.Items) != 0 && class.DeletionTimestamp == nil {
			controllerutil.AddFinalizer(&class, gatewayv1.GatewayClassFinalizerGatewaysExist)
		} else if len(gateways.Items) == 0 {
			controllerutil.RemoveFinalizer(&class, gatewayv1.GatewayClassFinalizerGatewaysExist)
		}
		if !equality.Semantic.DeepEqual(before.Finalizers, class.Finalizers) {
			if err := r.Client.Patch(ctx, &class, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
				return err
			}
			if class.DeletionTimestamp != nil {
				return nil
			}
			before = class.DeepCopy()
		}
		condition := metav1.Condition{Type: string(gatewayv1.GatewayClassConditionStatusAccepted),
			Status: metav1.ConditionTrue, Reason: string(gatewayv1.GatewayClassReasonAccepted),
			Message:            "GatewayClass configuration accepted; Azure provisioning is not implemented",
			ObservedGeneration: class.Generation}
		if class.Spec.ParametersRef != nil || class.DeletionTimestamp != nil {
			condition.Status = metav1.ConditionFalse
			condition.Reason = string(gatewayv1.GatewayClassReasonInvalidParameters)
			condition.Message = "GatewayClass parameters are unsupported or the class is being deleted"
		}
		meta.SetStatusCondition(&class.Status.Conditions, condition)
		// Accepted configuration alone does not establish Gateway API conformance.
		class.Status.SupportedFeatures = nil
		if equality.Semantic.DeepEqual(before.Status, class.Status) {
			return nil
		}
		return r.Client.Status().Patch(ctx, &class, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
	})
	return ctrl.Result{}, err
}

// SetupWithManager registers class and in-use Gateway watches.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).For(&gatewayv1.GatewayClass{}).
		Watches(&gatewayv1.Gateway{}, handler.EnqueueRequestsFromMapFunc(func(_ context.Context, object client.Object) []reconcile.Request {
			gateway, ok := object.(*gatewayv1.Gateway)
			if !ok {
				klog.ErrorS(nil, "Expected Gateway while mapping GatewayClass dependency")
				return nil
			}
			return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: string(gateway.Spec.GatewayClassName)}}}
		})).Complete(r)
}
