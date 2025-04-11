/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package validator

import (
	"context"
	"fmt"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetnetv1beta1 "go.goms.io/fleet-networking/api/v1beta1"
)

var (
	cmpTrafficManagerBackendOptions = cmp.Options{
		commonCmpOptions,
		cmpopts.IgnoreFields(fleetnetv1beta1.TrafficManagerBackend{}, "TypeMeta"),
		cmpopts.SortSlices(func(s1, s2 fleetnetv1beta1.TrafficManagerEndpointStatus) bool {
			return s1.From.Cluster < s2.From.Cluster
		}),
		cmpConditionOptions,
	}

	cmpTrafficManagerBackendStatusByIgnoringEndpointName = cmp.Options{
		cmpConditionOptions,
		// Here we don't validate the endpoint name and resource id to be decoupled from the implementation.
		// It will be validated separately by comparing the values with the ones in the Azure traffic manager profile.
		cmpopts.IgnoreFields(fleetnetv1beta1.TrafficManagerEndpointStatus{}, "Name", "ResourceID"), // ignore the generated endpoint name
		cmpopts.SortSlices(func(s1, s2 fleetnetv1beta1.TrafficManagerEndpointStatus) bool {
			return s1.From.Cluster < s2.From.Cluster
		}),
	}
)

// ValidateTrafficManagerBackendIfAcceptedAndIgnoringEndpointName validates the trafficManagerBackend object if it is accepted
// while ignoring the generated endpoint name.
func ValidateTrafficManagerBackendIfAcceptedAndIgnoringEndpointName(ctx context.Context, k8sClient client.Client, backendName types.NamespacedName, isAccepted bool, wantEndpoints []fleetnetv1beta1.TrafficManagerEndpointStatus, timeout time.Duration) fleetnetv1beta1.TrafficManagerBackendStatus {
	var gotStatus fleetnetv1beta1.TrafficManagerBackendStatus
	gomega.Eventually(func() error {
		backend := &fleetnetv1beta1.TrafficManagerBackend{}
		if err := k8sClient.Get(ctx, backendName, backend); err != nil {
			return err
		}
		var wantStatus fleetnetv1beta1.TrafficManagerBackendStatus
		if !isAccepted {
			wantStatus = fleetnetv1beta1.TrafficManagerBackendStatus{
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionFalse,
						Type:               string(fleetnetv1beta1.TrafficManagerBackendConditionAccepted),
						Reason:             string(fleetnetv1beta1.TrafficManagerBackendReasonInvalid),
						ObservedGeneration: backend.Generation,
					},
				},
				Endpoints: wantEndpoints,
			}
		} else {
			wantStatus = fleetnetv1beta1.TrafficManagerBackendStatus{
				Conditions: []metav1.Condition{
					{
						Status:             metav1.ConditionTrue,
						Type:               string(fleetnetv1beta1.TrafficManagerBackendConditionAccepted),
						Reason:             string(fleetnetv1beta1.TrafficManagerBackendReasonAccepted),
						ObservedGeneration: backend.Generation,
					},
				},
				Endpoints: wantEndpoints,
			}
		}
		gotStatus = backend.Status
		if diff := cmp.Diff(
			gotStatus,
			wantStatus,
			cmpTrafficManagerBackendStatusByIgnoringEndpointName,
		); diff != "" {
			return fmt.Errorf("trafficManagerBackend status diff (-got, +want): \n%s, got %+v", diff, gotStatus)
		}
		return nil
	}, timeout, interval).Should(gomega.Succeed(), "Get() trafficManagerBackend status mismatch")
	return gotStatus
}

// ValidateTrafficManagerBackendStatusAndIgnoringEndpointNameConsistently validates the trafficManagerBackend status consistently
// while ignoring the generated endpoint name.
func ValidateTrafficManagerBackendStatusAndIgnoringEndpointNameConsistently(ctx context.Context, k8sClient client.Client, backendName types.NamespacedName, want fleetnetv1beta1.TrafficManagerBackendStatus) {
	key := types.NamespacedName{Name: backendName.Name, Namespace: backendName.Namespace}
	backend := &fleetnetv1beta1.TrafficManagerBackend{}
	gomega.Consistently(func() error {
		if err := k8sClient.Get(ctx, key, backend); err != nil {
			return err
		}
		if diff := cmp.Diff(
			backend.Status,
			want,
			cmpTrafficManagerBackendStatusByIgnoringEndpointName,
		); diff != "" {
			return fmt.Errorf("trafficManagerBackend status diff (-got, +want): \n%s, got %+v", diff, backend.Status)
		}
		return nil
	}, duration, interval).Should(gomega.Succeed(), "Get() trafficManagerBackend status mismatch")
}

// ValidateTrafficManagerBackend validates the trafficManagerBackend object.
func ValidateTrafficManagerBackend(ctx context.Context, k8sClient client.Client, want *fleetnetv1beta1.TrafficManagerBackend, timeout time.Duration) {
	key := types.NamespacedName{Name: want.Name, Namespace: want.Namespace}
	backend := &fleetnetv1beta1.TrafficManagerBackend{}
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
func ValidateTrafficManagerBackendConsistently(ctx context.Context, k8sClient client.Client, want *fleetnetv1beta1.TrafficManagerBackend) {
	key := types.NamespacedName{Name: want.Name, Namespace: want.Namespace}
	backend := &fleetnetv1beta1.TrafficManagerBackend{}
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
func IsTrafficManagerBackendDeleted(ctx context.Context, k8sClient client.Client, name types.NamespacedName, timeout time.Duration) {
	gomega.Eventually(func() error {
		backend := &fleetnetv1beta1.TrafficManagerBackend{}
		if err := k8sClient.Get(ctx, name, backend); !errors.IsNotFound(err) {
			return fmt.Errorf("trafficManagerBackend %s still exists or an unexpected error occurred: %w", name, err)
		}
		return nil
	}, timeout, interval).Should(gomega.Succeed(), "Failed to remove trafficManagerBackend %s ", name)
}
