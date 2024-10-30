/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package validator

import (
	"context"
	"fmt"

	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
)

// IsTrafficManagerBackendFinalizerAdded validates whether the backend is created with the finalizer or not.
func IsTrafficManagerBackendFinalizerAdded(ctx context.Context, k8sClient client.Client, name types.NamespacedName) {
	gomega.Eventually(func() error {
		backend := &fleetnetv1alpha1.TrafficManagerBackend{}
		if err := k8sClient.Get(ctx, name, backend); err != nil {
			return fmt.Errorf("failed to get trafficManagerBackend %s: %w", name, err)
		}
		if !controllerutil.ContainsFinalizer(backend, objectmeta.TrafficManagerBackendFinalizer) {
			return fmt.Errorf("trafficManagerBackend %s finalizer not added", name)
		}
		return nil
	}, timeout, interval).Should(gomega.Succeed(), "Failed to add finalizer to trafficManagerBackend %s", name)
}

// IsTrafficManagerBackendDeleted validates whether the backend is deleted or not.
func IsTrafficManagerBackendDeleted(ctx context.Context, k8sClient client.Client, name types.NamespacedName) {
	gomega.Eventually(func() error {
		backend := &fleetnetv1alpha1.TrafficManagerBackend{}
		if err := k8sClient.Get(ctx, name, backend); !errors.IsNotFound(err) {
			return fmt.Errorf("trafficManagerBackend %s still exists or an unexpected error occurred: %w", name, err)
		}
		return nil
	}, timeout, interval).Should(gomega.Succeed(), "Failed to remove trafficManagerBackend %s ", name)
}
