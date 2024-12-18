/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package validator

import (
	"context"
	"fmt"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
)

var (
	cmpTrafficManagerBackendOptions = cmp.Options{
		commonCmpOptions,
		cmpopts.IgnoreFields(fleetnetv1alpha1.TrafficManagerBackend{}, "TypeMeta"),
		cmpopts.SortSlices(func(s1, s2 fleetnetv1alpha1.TrafficManagerEndpointStatus) bool {
			return s1.Cluster.Cluster < s2.Cluster.Cluster
		}),
		cmpConditionOptions,
	}

	cmpTrafficManagerStatusByIgnoringEndpointName = cmp.Options{
		cmpConditionOptions,
		cmpopts.IgnoreFields(fleetnetv1alpha1.TrafficManagerEndpointStatus{}, "Name"), // ignore the generated endpoint name
	}
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

// ValidateTrafficManagerBackendIfAcceptedAndIgnoringEndpointName validates the trafficManagerBackend object if it is accepted
// while ignoring the generated endpoint name.
func ValidateTrafficManagerBackendIfAcceptedAndIgnoringEndpointName(ctx context.Context, k8sClient client.Client, backendName types.NamespacedName, wantEndpoints []fleetnetv1alpha1.TrafficManagerEndpointStatus) fleetnetv1alpha1.TrafficManagerBackendStatus {
	var wantStatus fleetnetv1alpha1.TrafficManagerBackendStatus
	if len(wantEndpoints) == 0 {
		wantStatus = fleetnetv1alpha1.TrafficManagerBackendStatus{
			Conditions: []metav1.Condition{
				{
					Status: metav1.ConditionFalse,
					Type:   string(fleetnetv1alpha1.TrafficManagerBackendConditionAccepted),
					Reason: string(fleetnetv1alpha1.TrafficManagerBackendReasonInvalid),
				},
			},
		}
	} else {
		wantStatus = fleetnetv1alpha1.TrafficManagerBackendStatus{
			Conditions: []metav1.Condition{
				{
					Status: metav1.ConditionTrue,
					Type:   string(fleetnetv1alpha1.TrafficManagerBackendConditionAccepted),
					Reason: string(fleetnetv1alpha1.TrafficManagerBackendReasonAccepted),
				},
			},
			Endpoints: wantEndpoints,
		}
	}

	gomega.Eventually(func() error {
		backend := &fleetnetv1alpha1.TrafficManagerBackend{}
		if err := k8sClient.Get(ctx, backendName, backend); err != nil {
			return err
		}

		if diff := cmp.Diff(
			backend.Status,
			wantStatus,
			cmpTrafficManagerStatusByIgnoringEndpointName,
		); diff != "" {
			return fmt.Errorf("trafficManagerBackend status diff (-got, +want): %s", diff)
		}
		return nil
	}, timeout, interval).Should(gomega.Succeed(), "Get() trafficManagerBackend status mismatch")
	return wantStatus
}

// ValidateTrafficManagerBackendStatusAndIgnoringEndpointNameConsistently validates the trafficManagerBackend status consistently
// while ignoring the generated endpoint name.
func ValidateTrafficManagerBackendStatusAndIgnoringEndpointNameConsistently(ctx context.Context, k8sClient client.Client, backendName types.NamespacedName, want fleetnetv1alpha1.TrafficManagerBackendStatus) {
	key := types.NamespacedName{Name: backendName.Name, Namespace: backendName.Namespace}
	backend := &fleetnetv1alpha1.TrafficManagerBackend{}
	gomega.Consistently(func() error {
		if err := k8sClient.Get(ctx, key, backend); err != nil {
			return err
		}
		if diff := cmp.Diff(
			backend.Status,
			want,
			cmpTrafficManagerStatusByIgnoringEndpointName,
		); diff != "" {
			return fmt.Errorf("trafficManagerBackend status diff (-got, +want): %s", diff)
		}
		return nil
	}, duration, interval).Should(gomega.Succeed(), "Get() trafficManagerBackend status mismatch")
}

// ValidateTrafficManagerBackend validates the trafficManagerBackend object.
func ValidateTrafficManagerBackend(ctx context.Context, k8sClient client.Client, want *fleetnetv1alpha1.TrafficManagerBackend) {
	key := types.NamespacedName{Name: want.Name, Namespace: want.Namespace}
	backend := &fleetnetv1alpha1.TrafficManagerBackend{}
	gomega.Eventually(func() error {
		if err := k8sClient.Get(ctx, key, backend); err != nil {
			return err
		}
		if diff := cmp.Diff(want, backend, cmpTrafficManagerBackendOptions); diff != "" {
			return fmt.Errorf("trafficManagerBackend mismatch (-want, +got) :\n%s", diff)
		}
		return nil
	}, timeout, interval).Should(gomega.Succeed(), "Get() trafficManagerBackend mismatch")
}

// ValidateTrafficManagerBackendConsistently validates the trafficManagerBackend object consistently.
func ValidateTrafficManagerBackendConsistently(ctx context.Context, k8sClient client.Client, want *fleetnetv1alpha1.TrafficManagerBackend) {
	key := types.NamespacedName{Name: want.Name, Namespace: want.Namespace}
	backend := &fleetnetv1alpha1.TrafficManagerBackend{}
	gomega.Consistently(func() error {
		if err := k8sClient.Get(ctx, key, backend); err != nil {
			return err
		}
		if diff := cmp.Diff(want, backend, cmpTrafficManagerBackendOptions); diff != "" {
			return fmt.Errorf("trafficManagerBackend mismatch (-want, +got) :\n%s", diff)
		}
		return nil
	}, duration, interval).Should(gomega.Succeed(), "Get() trafficManagerBackend mismatch")
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

// ValidateTrafficManagerConsistentlyExist validates whether the backend consistently exists.
func ValidateTrafficManagerConsistentlyExist(ctx context.Context, k8sClient client.Client, name types.NamespacedName) {
	gomega.Consistently(func() error {
		backend := &fleetnetv1alpha1.TrafficManagerBackend{}
		if err := k8sClient.Get(ctx, name, backend); errors.IsNotFound(err) {
			return fmt.Errorf("trafficManagerBackend %s does not exist: %w", name, err)
		}
		return nil
	}, duration, interval).Should(gomega.Succeed(), "Failed to find trafficManagerBackend %s ", name)
}
