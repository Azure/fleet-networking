/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gateway

import (
	"context"
	"fmt"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
	"go.goms.io/fleet-networking/pkg/providers/azure/frontdoor"
)

func (r *Reconciler) suspendEmptyGateway(ctx context.Context, gateway *gatewayv1.Gateway,
	model gatewaymodel.GlobalGateway) (*gatewayStaging, error) {
	if !controllerutil.ContainsFinalizer(gateway, objectmeta.GatewayAFDCleanupFinalizer) {
		return nil, nil
	}
	staging := &gatewayStaging{uid: gateway.UID, generation: gateway.Generation, model: model,
		suspension: &frontdoor.EndpointSuspensionResult{}}
	owner, err := r.suspensionOwner(ctx, gateway, model)
	if err != nil {
		staging.err = err
		return staging, err
	}
	start := time.Now()
	result, err := r.suspendEndpoint(ctx, owner)
	r.recordGraphOperation(gateway, graphSuspendEmpty, time.Since(start), err)
	*staging.suspension, staging.err = result, err
	return staging, err
}

func (r *Reconciler) suspensionOwner(ctx context.Context, gateway *gatewayv1.Gateway,
	model gatewaymodel.GlobalGateway) (frontdoor.GatewayOwner, error) {
	if err := ctx.Err(); err != nil {
		return frontdoor.GatewayOwner{}, err
	}
	if r.APIReader == nil || r.ProfileCleanup == nil {
		return frontdoor.GatewayOwner{}, fmt.Errorf("uncached reader and AFD cleanup provider are required before endpoint suspension")
	}
	if gateway.UID == "" || len(model.Routes) != 0 {
		return frontdoor.GatewayOwner{}, fmt.Errorf("matching Gateway identity and complete empty intent are required before endpoint suspension")
	}
	hubUID, err := r.provisioningHubUID(ctx)
	if err != nil {
		return frontdoor.GatewayOwner{}, err
	}
	current, err := r.currentProvisioningIntent(ctx, gateway, model, hubUID)
	if err != nil {
		return frontdoor.GatewayOwner{}, err
	}
	if !controllerutil.ContainsFinalizer(current, objectmeta.GatewayAFDCleanupFinalizer) {
		return frontdoor.GatewayOwner{}, fmt.Errorf("existing Gateway cleanup finalizer is required before endpoint suspension")
	}
	return frontdoor.GatewayOwner{
		HubUID: hubUID, Namespace: current.Namespace, Name: current.Name, UID: string(current.UID),
	}, nil
}
