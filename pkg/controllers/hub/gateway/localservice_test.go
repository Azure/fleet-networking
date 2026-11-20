/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gateway

import (
	"context"
	"testing"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewayresolver"
)

type localBackendFixture struct{}

func (localBackendFixture) ResolveLocalService(_ context.Context, namespace string, ref gatewayv1.HTTPBackendRef) (gatewaymodel.Backend, error) {
	return gatewaymodel.Backend{
		Kind: "Service", Namespace: namespace, Name: string(ref.Name), Port: int32(*ref.Port), RouteWeight: 1,
		HealthProbe: gatewaymodel.HTTPPreviewHealthProbe(),
		Origins: []gatewaymodel.Origin{{
			Cluster: "local", ServiceUID: "local-service", Endpoint: "10.0.0.25", Weight: 1,
			PrivateLinkResourceID: "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/node/providers/Microsoft.Network/privateLinkServices/local",
			PrivateLinkLocation:   "eastus",
		}},
	}, nil
}

func TestGatewayProgramsLocalServiceOnlyWithOptInResolver(t *testing.T) {
	ctx := context.Background()
	c, gateway, route, r, provider := httpControllerFixture(t)
	ref := &route.Spec.Rules[0].BackendRefs[0]
	ref.Group, ref.Kind = nil, nil
	if err := c.Update(ctx, route); err != nil {
		t.Fatal(err)
	}

	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}
	if _, err := r.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 0 {
		t.Fatal("local Service programmed without opt-in")
	}
	r.LocalServices = localBackendFixture{}
	if _, err := r.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	assertHTTPGatewayReadiness(t, c, gateway, true)
	if provider.calls != 1 || provider.model.Routes[0].Rules[0].Backends[0].Kind != "Service" {
		t.Fatalf("local Service not passed to the private provider: %+v", provider.model)
	}
}

type delayedLocalBackend struct {
	ready   bool
	invalid bool
}

func (r *delayedLocalBackend) ResolveLocalService(ctx context.Context, namespace string, ref gatewayv1.HTTPBackendRef) (gatewaymodel.Backend, error) {
	if !r.ready {
		return gatewaymodel.Backend{}, &gatewayresolver.ResolutionError{
			Reason: "BackendUnavailable", ReferenceValid: !r.invalid, Message: "waiting for companion ILB/PLS",
		}
	}
	return (localBackendFixture{}).ResolveLocalService(ctx, namespace, ref)
}

func TestGatewayRetriesPendingLocalOriginAndProgramsAfterRecovery(t *testing.T) {
	ctx := context.Background()
	c, gateway, route, r, provider := httpControllerFixture(t)
	ref := &route.Spec.Rules[0].BackendRefs[0]
	ref.Group, ref.Kind = nil, nil
	if err := c.Update(ctx, route); err != nil {
		t.Fatal(err)
	}
	resolver := &delayedLocalBackend{}
	r.LocalServices = resolver
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}
	for i := 0; i < 2; i++ {
		result, err := r.Reconcile(ctx, request)
		if err != nil || result.RequeueAfter < 30*time.Second || result.RequeueAfter > 36*time.Second {
			t.Fatalf("stable pending local origin lost retry: %+v, %v", result, err)
		}
		if provider.calls != 0 {
			t.Fatal("AFD programmed before the local private origin was ready")
		}
	}
	resolver.ready = true
	if _, err := r.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	assertHTTPGatewayReadiness(t, c, gateway, true)
	if provider.calls != 1 {
		t.Fatalf("recovered local origin was not programmed: calls=%d", provider.calls)
	}
}

func TestGatewayDoesNotPollInvalidLocalReference(t *testing.T) {
	ctx := context.Background()
	c, gateway, route, r, _ := httpControllerFixture(t)
	route.Spec.Rules[0].BackendRefs[0].Group = nil
	route.Spec.Rules[0].BackendRefs[0].Kind = nil
	if err := c.Update(ctx, route); err != nil {
		t.Fatal(err)
	}
	r.LocalServices = &delayedLocalBackend{invalid: true}
	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)})
	if err != nil || result.RequeueAfter != 0 {
		t.Fatalf("invalid local reference should wait for corrected intent: %+v %v", result, err)
	}
}
