/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gateway

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/providers/azure/frontdoor"
)

func (r *Reconciler) finalize(ctx context.Context, gateway *gatewayv1.Gateway) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(gateway, objectmeta.GatewayAFDCleanupFinalizer) {
		return ctrl.Result{}, nil
	}
	var hub corev1.Namespace
	if err := r.APIReader.Get(ctx, client.ObjectKey{Name: metav1.NamespaceSystem}, &hub); err != nil {
		return ctrl.Result{}, fmt.Errorf("read trusted hub identity for AFD cleanup: %w", err)
	}
	if hub.UID == "" {
		return ctrl.Result{}, fmt.Errorf("hub kube-system Namespace UID is required for AFD cleanup")
	}
	start := time.Now()
	deleted, err := r.ProfileCleanup.DeleteProfile(ctx, frontdoor.GatewayOwner{
		HubUID: string(hub.UID), Namespace: gateway.Namespace, Name: gateway.Name, UID: string(gateway.UID),
	})
	duration := time.Since(start)
	recordProfileCleanup(duration, deleted, err)
	if err != nil {
		logGatewayOperationFailure(ctx, "cleanup_profile", err)
		r.recordProfileCleanupFailure(gateway, err)
		return ctrl.Result{}, err
	}
	logger := klog.FromContext(ctx)
	logger.V(2).Info("AFD profile cleanup observed", "state", profileCleanupOutcome(deleted, nil), "duration", duration)
	if !deleted {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	var finalizedGateway *gatewayv1.Gateway
	// Retry metadata conflicts only; never repeat the external operation in this callback.
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		finalizedGateway = nil
		var current gatewayv1.Gateway
		if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(gateway), &current); err != nil {
			return client.IgnoreNotFound(err)
		}
		if current.UID != gateway.UID || current.DeletionTimestamp == nil ||
			!controllerutil.ContainsFinalizer(&current, objectmeta.GatewayAFDCleanupFinalizer) {
			return nil
		}
		before := current.DeepCopy()
		controllerutil.RemoveFinalizer(&current, objectmeta.GatewayAFDCleanupFinalizer)
		if err := r.Client.Patch(ctx, &current, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
			return err
		}
		finalizedGateway = current.DeepCopy()
		return nil
	})
	logGatewayOperationFailure(ctx, "persist_cleanup_finalizer", err)
	if err == nil && finalizedGateway != nil {
		// Observed Azure absence alone is not persisted Kubernetes finalization.
		logger.V(2).Info("Gateway cleanup finalizer removed")
		if r.Recorder != nil {
			r.Recorder.Eventf(finalizedGateway, nil, corev1.EventTypeNormal, "AFDProfileCleanupComplete", "CleanupAFDProfile",
				"AFD profile observed absent; this controller removed the Gateway cleanup finalizer. Absence does not prove this controller submitted a deletion.")
		}
	}
	return ctrl.Result{}, err
}
