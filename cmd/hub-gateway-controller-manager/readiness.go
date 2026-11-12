/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package main

import (
	"context"
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

const gatewayCacheReadinessCheck = "gateway-cache"

func registerGatewayCacheReadiness(ctx context.Context, mgr ctrl.Manager) error {
	objects := []client.Object{
		&gatewayv1.GatewayClass{},
		&gatewayv1.Gateway{},
		&gatewayv1.HTTPRoute{},
		&gatewayv1beta1.ReferenceGrant{},
		&fleetnetv1alpha1.ServiceImport{},
		&fleetnetv1alpha1.InternalServiceExport{},
		&corev1.Namespace{},
	}
	informers := make([]cache.Informer, 0, len(objects))
	informerCache := mgr.GetCache()
	// Create every watched informer before startup; an empty cache can sync
	// before controller sources register their required informers.
	for _, object := range objects {
		informer, err := informerCache.GetInformer(ctx, object, cache.BlockUntilSynced(false))
		if err != nil {
			return fmt.Errorf("initialize %T informer for Gateway readiness: %w", object, err)
		}
		informers = append(informers, informer)
	}
	// Local synchronization, not leadership or Azure/approval availability,
	// determines readiness; probe requests perform no remote operations.
	check := func(_ *http.Request) error {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("Gateway manager is shutting down: %w", err)
		}
		for i, informer := range informers {
			if informer.IsStopped() {
				return fmt.Errorf("%T informer has stopped", objects[i])
			}
			if !informer.HasSynced() {
				return fmt.Errorf("%T informer has not synced", objects[i])
			}
		}
		return nil
	}
	if err := mgr.AddReadyzCheck(gatewayCacheReadinessCheck, check); err != nil {
		return fmt.Errorf("register Gateway cache readiness: %w", err)
	}
	return nil
}
