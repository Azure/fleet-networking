/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package conformance

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// CleanupFixtures waits for Gateway finalizers before withdrawing backend fixtures.
// Namespaces must have been newly created by this run and must not be shared with other tests.
func CleanupFixtures(ctx context.Context, c client.Client, fixtures []unstructured.Unstructured) error {
	for _, fixture := range fixtures {
		if fixture.GetKind() != "Namespace" {
			continue
		}
		var namespace corev1.Namespace
		err := c.Get(ctx, client.ObjectKey{Name: fixture.GetName()}, &namespace)
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return err
		}
		if namespace.UID != fixture.GetUID() {
			return fmt.Errorf("refusing cleanup of replaced namespace %s", fixture.GetName())
		}
		var gateways gatewayv1.GatewayList
		if err := c.List(ctx, &gateways, client.InNamespace(namespace.Name)); err != nil {
			return err
		}
		for i := range gateways.Items {
			gateway := &gateways.Items[i]
			if err := c.Delete(ctx, gateway, client.Preconditions{UID: &gateway.UID}); err != nil && !apierrors.IsNotFound(err) {
				return err
			}
			if err := waitForDeletion(ctx, c, gateway); err != nil {
				return fmt.Errorf("Gateway %s cleanup: %w", client.ObjectKeyFromObject(gateway), err)
			}
		}
	}
	for i := len(fixtures) - 1; i >= 0; i-- {
		fixture := &fixtures[i]
		uid := fixture.GetUID()
		if err := c.Delete(ctx, fixture, client.Preconditions{UID: &uid}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		if fixture.GetKind() == "Namespace" {
			if err := waitForDeletion(ctx, c, fixture); err != nil {
				return fmt.Errorf("namespace %s cleanup: %w", fixture.GetName(), err)
			}
		}
	}
	return nil
}

func waitForDeletion(ctx context.Context, c client.Client, object client.Object) error {
	return wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
		current := object.DeepCopyObject().(client.Object)
		err := c.Get(ctx, client.ObjectKeyFromObject(object), current)
		return apierrors.IsNotFound(err), client.IgnoreNotFound(err)
	})
}
