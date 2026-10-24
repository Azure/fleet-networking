//go:build e2e

/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"go.goms.io/fleet-networking/test/e2e/framework"
)

func assertGatewayControlPlane(class *gatewayv1.GatewayClass, gateway *gatewayv1.Gateway,
	sameNamespace, crossNamespace *gatewayv1.HTTPRoute, grant *gatewayv1beta1.ReferenceGrant) {
	GinkgoHelper()
	c := hubCluster.Client()
	By("observing accepted class configuration without advertising unproven features")
	Eventually(func() error {
		var current gatewayv1.GatewayClass
		if err := c.Get(ctx, client.ObjectKeyFromObject(class), &current); err != nil {
			return err
		}
		if err := currentGatewayCondition(current.Status.Conditions, "Accepted", metav1.ConditionTrue, "Accepted", current.Generation); err != nil {
			return err
		}
		if len(current.Status.SupportedFeatures) != 0 {
			return fmt.Errorf("configuration acceptance must not advertise unproven features")
		}
		return nil
	}, framework.PollTimeout, framework.PollInterval).Should(Succeed())

	By("observing private-only control-plane status, not fabricated Azure readiness")
	waitForGatewayControlPlane(gateway, 2)
	waitForGatewayRouteCondition(sameNamespace, gateway.Name, "Accepted", metav1.ConditionTrue, "Accepted")
	initial := waitForGatewayRouteCondition(sameNamespace, gateway.Name, "ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")
	waitForGatewayRouteCondition(crossNamespace, gateway.Name, "Accepted", metav1.ConditionTrue, "Accepted")
	authorized := waitForGatewayRouteCondition(crossNamespace, gateway.Name, "ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")

	By("reporting a missing backend at the new route generation and recovering")
	backendName := sameNamespace.Spec.Rules[0].BackendRefs[0].Name
	for _, name := range []gatewayv1.ObjectName{"missing-backend", backendName} {
		Expect(retry.RetryOnConflict(retry.DefaultRetry, func() error {
			var current gatewayv1.HTTPRoute
			if err := c.Get(ctx, client.ObjectKeyFromObject(sameNamespace), &current); err != nil {
				return err
			}
			current.Spec.Rules[0].BackendRefs[0].Name = name
			return c.Update(ctx, &current)
		})).To(Succeed())
		status, reason := metav1.ConditionTrue, "ResolvedRefs"
		if name != backendName {
			status, reason = metav1.ConditionFalse, "BackendNotFound"
		}
		current := waitForGatewayRouteCondition(sameNamespace, gateway.Name, "ResolvedRefs", status, reason)
		Expect(current.Generation).To(BeNumerically(">", initial.Generation))
		// Missing references do not invalidate the route's listener attachment.
		waitForGatewayRouteCondition(sameNamespace, gateway.Name, "Accepted", metav1.ConditionTrue, "Accepted")
	}

	By("revoking cross-namespace access from a grant deletion without editing the route")
	Expect(c.Delete(ctx, grant)).To(Succeed())
	revoked := waitForGatewayRouteCondition(crossNamespace, gateway.Name, "ResolvedRefs", metav1.ConditionFalse, "RefNotPermitted")
	Expect(revoked.Generation).To(Equal(authorized.Generation))

	By("recovering authorization from a newly created grant without editing the route")
	// A recreated grant has a new UID; discard metadata from the deleted object.
	*grant = gatewayv1beta1.ReferenceGrant{
		ObjectMeta: metav1.ObjectMeta{Namespace: grant.Namespace, Name: grant.Name},
		Spec:       grant.Spec,
	}
	Expect(c.Create(ctx, grant)).To(Succeed())
	recovered := waitForGatewayRouteCondition(crossNamespace, gateway.Name, "ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs")
	Expect(recovered.Generation).To(Equal(authorized.Generation))
	waitForGatewayControlPlane(gateway, 2)
}

func waitForGatewayRouteCondition(route *gatewayv1.HTTPRoute, gatewayName, conditionType string,
	status metav1.ConditionStatus, reason string) gatewayv1.HTTPRoute {
	GinkgoHelper()
	var current gatewayv1.HTTPRoute
	Eventually(func() error {
		if err := hubCluster.Client().Get(ctx, client.ObjectKeyFromObject(route), &current); err != nil {
			return err
		}
		for _, parent := range current.Status.Parents {
			if parent.ControllerName == gatewayControllerName && string(parent.ParentRef.Name) == gatewayName {
				return currentGatewayCondition(parent.Conditions, conditionType, status, reason, current.Generation)
			}
		}
		return fmt.Errorf("route %s has no Fleet parent status for %s", client.ObjectKeyFromObject(route), gatewayName)
	}, framework.PollTimeout, framework.PollInterval).Should(Succeed())
	return current
}

func waitForGatewayControlPlane(gateway *gatewayv1.Gateway, attached int32) {
	GinkgoHelper()
	Eventually(func() error {
		var current gatewayv1.Gateway
		if err := hubCluster.Client().Get(ctx, client.ObjectKeyFromObject(gateway), &current); err != nil {
			return err
		}
		if err := currentGatewayCondition(current.Status.Conditions, "Accepted", metav1.ConditionTrue, "Accepted", current.Generation); err != nil {
			return err
		}
		if err := currentGatewayCondition(current.Status.Conditions, "Programmed", metav1.ConditionFalse, "Pending", current.Generation); err != nil {
			return err
		}
		if len(current.Status.Addresses) != 0 || len(current.Finalizers) != 0 {
			return fmt.Errorf("unprogrammed Gateway has Azure addresses or an Azure cleanup finalizer")
		}
		if len(current.Status.Listeners) != 1 || current.Status.Listeners[0].AttachedRoutes != attached {
			return fmt.Errorf("Gateway has not observed %d attached routes", attached)
		}
		return currentGatewayCondition(current.Status.Listeners[0].Conditions, "Programmed", metav1.ConditionFalse, "Pending", current.Generation)
	}, framework.PollTimeout, framework.PollInterval).Should(Succeed())
}

func currentGatewayCondition(conditions []metav1.Condition, conditionType string,
	status metav1.ConditionStatus, reason string, generation int64) error {
	condition := meta.FindStatusCondition(conditions, conditionType)
	if condition == nil || condition.Status != status || condition.Reason != reason || condition.ObservedGeneration != generation {
		return fmt.Errorf("condition %s = %#v; want %s/%s at generation %d", conditionType, condition, status, reason, generation)
	}
	return nil
}
