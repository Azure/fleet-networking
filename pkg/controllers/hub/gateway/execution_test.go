/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gateway

import (
	"context"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestGatewayReportsExecutionSubsetWithoutChangingAcceptance(t *testing.T) {
	tests := []struct {
		name   string
		change func(*gatewayv1.Gateway, *gatewayv1.HTTPRoute)
	}{
		{"non-root prefix", func(_ *gatewayv1.Gateway, route *gatewayv1.HTTPRoute) {
			route.Spec.Rules[0].Matches = []gatewayv1.HTTPRouteMatch{{
				Path: &gatewayv1.HTTPPathMatch{Type: ptr.To(gatewayv1.PathMatchPathPrefix), Value: ptr.To("/api")},
			}}
		}},
		{"exact root", func(_ *gatewayv1.Gateway, route *gatewayv1.HTTPRoute) {
			route.Spec.Rules[0].Matches = []gatewayv1.HTTPRouteMatch{{
				Path: &gatewayv1.HTTPPathMatch{Type: ptr.To(gatewayv1.PathMatchExact), Value: ptr.To("/")},
			}}
		}},
		{"listener hostname", func(gateway *gatewayv1.Gateway, _ *gatewayv1.HTTPRoute) {
			gateway.Spec.Listeners[0].Hostname = ptr.To(gatewayv1.Hostname("api.example.com"))
		}},
		{"route hostname", func(_ *gatewayv1.Gateway, route *gatewayv1.HTTPRoute) {
			route.Spec.Hostnames = []gatewayv1.Hostname{"api.example.com"}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, gateway, route, _ := gatewayFixture(t, 1)
			tt.change(gateway, route)
			ctx := context.Background()
			if err := c.Update(ctx, gateway); err != nil {
				t.Fatal(err)
			}
			if err := c.Update(ctx, route); err != nil {
				t.Fatal(err)
			}
			observer := &fakeOriginObserver{}
			r := &Reconciler{Client: c, OriginObserver: observer}
			result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(gateway)})
			if err != nil {
				t.Fatal(err)
			}
			if err := c.Get(ctx, client.ObjectKeyFromObject(gateway), gateway); err != nil {
				t.Fatal(err)
			}
			accepted := meta.FindStatusCondition(gateway.Status.Conditions, "Accepted")
			programmed := meta.FindStatusCondition(gateway.Status.Conditions, "Programmed")
			if accepted == nil || accepted.Status != metav1.ConditionTrue || programmed == nil ||
				programmed.Status != metav1.ConditionFalse || !strings.Contains(programmed.Message, "execution subset") ||
				observer.calls != 0 || result.RequeueAfter != 0 || len(gateway.Finalizers) != 0 || len(gateway.Status.Addresses) != 0 {
				t.Fatalf("unsupported execution intent was observed/widened: %#v, %#v", gateway.Status, result)
			}
		})
	}
}
