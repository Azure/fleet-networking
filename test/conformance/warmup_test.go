//go:build conformance

/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package conformance_test

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	harness "go.goms.io/fleet-networking/test/conformance"
)

func TestWarmRequestUsesSelectedRuleAndHostnameIntersection(t *testing.T) {
	gateway := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gateway", Namespace: "infra"},
		Spec: gatewayv1.GatewaySpec{Listeners: []gatewayv1.Listener{{
			Name: "http", Protocol: gatewayv1.HTTPProtocolType, Port: 80,
			Hostname: ptr.To(gatewayv1.Hostname("matches.example.com")),
		}}},
	}
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "route", Namespace: "infra"},
		Spec: gatewayv1.HTTPRouteSpec{
			Hostnames: []gatewayv1.Hostname{"unmatched.example.com", "matches.example.com"},
			Rules: []gatewayv1.HTTPRouteRule{
				{},
				{Matches: []gatewayv1.HTTPRouteMatch{{
					Path:    &gatewayv1.HTTPPathMatch{Type: ptr.To(gatewayv1.PathMatchExact), Value: ptr.To("/selected")},
					Method:  ptr.To(gatewayv1.HTTPMethod("POST")),
					Headers: []gatewayv1.HTTPHeaderMatch{{Name: "Version", Value: "two"}},
				}}},
			},
		},
	}
	target := harness.OriginTarget{
		Gateway: types.NamespacedName{Namespace: "infra", Name: "gateway"},
		Parent:  gatewayv1.ParentReference{Name: "gateway"}, RuleIndex: 1,
	}
	request, err := warmRequest(context.Background(), "test.azurefd.net", gateway, route, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	if request.Method != "POST" || request.URL.Path != "/selected" || request.URL.Host != "test.azurefd.net:80" ||
		request.Host != "matches.example.com" || request.Header.Get("Version") != "two" {
		t.Fatalf("warmup request does not preserve selected rule/host: %+v", request)
	}
	target.RuleIndex = 5
	if _, err := warmRequest(context.Background(), "test.azurefd.net", gateway, route, target, nil); err == nil {
		t.Fatal("changed rule index must fail explicitly")
	}
}
