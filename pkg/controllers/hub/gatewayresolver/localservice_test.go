/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gatewayresolver

import (
	"context"
	"testing"

	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
)

type localResolverProbe struct {
	calls int
}

func (r *localResolverProbe) ResolveLocalService(_ context.Context, namespace string, ref gatewayv1.HTTPBackendRef) (gatewaymodel.Backend, error) {
	r.calls++
	return gatewaymodel.Backend{Namespace: namespace, Name: string(ref.Name), Port: int32(*ref.Port)}, nil
}

func TestLocalServiceResolutionRequiresExplicitProvider(t *testing.T) {
	reader := fake.NewClientBuilder().Build()
	ref := gatewayv1.HTTPBackendRef{BackendRef: gatewayv1.BackendRef{
		BackendObjectReference: gatewayv1.BackendObjectReference{Name: "api", Port: ptr.To(gatewayv1.PortNumber(80))},
	}}
	if _, err := ResolveBackend(context.Background(), reader, "store", ref); err == nil {
		t.Fatal("local Service resolution unexpectedly enabled without a provider")
	}
	probe := &localResolverProbe{}
	result, err := ResolveBackend(context.Background(), WithLocalServices(reader, probe), "store", ref)
	if err != nil || probe.calls != 1 || result.Kind != "Service" || result.Name != "api" || result.Port != 80 {
		t.Fatalf("local Service dispatch: result=%+v calls=%d err=%v", result, probe.calls, err)
	}
	ref.Group = ptr.To(gatewayv1.Group("unrelated.example"))
	if _, err := ResolveBackend(context.Background(), WithLocalServices(reader, probe), "store", ref); err == nil || probe.calls != 1 {
		t.Fatal("foreign group dispatched to local Service provider")
	}
}
