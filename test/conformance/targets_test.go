/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package conformance

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestOriginTargetsMultipleGatewaysAndBackends(t *testing.T) {
	route := gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Namespace: "backend", Name: "route"},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gatewayv1.ParentReference{
				{Name: "local"},
				{Name: "remote", Namespace: ptr.To(gatewayv1.Namespace("infra"))},
			}},
			Rules: []gatewayv1.HTTPRouteRule{{BackendRefs: []gatewayv1.HTTPBackendRef{
				{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "one", Port: ptr.To(gatewayv1.PortNumber(80))}}},
				{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "two", Port: ptr.To(gatewayv1.PortNumber(8080))}}},
				{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "zero"}, Weight: ptr.To(int32(0))}},
			}}},
		},
	}
	got, err := OriginTargets([]gatewayv1.HTTPRoute{route})
	if err != nil || len(got) != 4 {
		t.Fatalf("targets=%+v err=%v", got, err)
	}
	if got[0].Gateway.Namespace != "backend" || got[2].Gateway.Namespace != "infra" ||
		got[1].Reference.Name != "two" || *got[1].Reference.Port != 8080 {
		t.Fatalf("reference scope/ports were lost: %+v", got)
	}
	route.Spec.Rules[0].BackendRefs[0].Port = nil
	if _, err := OriginTargets([]gatewayv1.HTTPRoute{route}); err == nil {
		t.Fatal("missing backend port must fail fixture planning")
	}
}
