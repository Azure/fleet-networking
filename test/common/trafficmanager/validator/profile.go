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

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

const (
	timeout  = time.Second * 10
	interval = time.Millisecond * 250
	// duration used by consistently
	duration = time.Second * 30
)

var (
	commonCmpOptions = cmp.Options{
		cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion", "UID", "CreationTimestamp", "ManagedFields", "Generation"),
		cmpopts.IgnoreFields(metav1.OwnerReference{}, "UID"),
		cmpopts.IgnoreFields(metav1.Condition{}, "Message", "LastTransitionTime", "ObservedGeneration"),
		cmpopts.SortSlices(func(c1, c2 metav1.Condition) bool {
			return c1.Type < c2.Type
		}),
	}
	cmpTrafficManagerProfileOptions = cmp.Options{
		commonCmpOptions,
		cmpopts.IgnoreFields(fleetnetv1alpha1.TrafficManagerProfile{}, "TypeMeta"),
	}
)

// ValidateTrafficManagerProfile validates the trafficManagerProfile object.
func ValidateTrafficManagerProfile(ctx context.Context, k8sClient client.Client, want *fleetnetv1alpha1.TrafficManagerProfile) {
	key := types.NamespacedName{Name: want.Name, Namespace: want.Namespace}
	profile := &fleetnetv1alpha1.TrafficManagerProfile{}
	gomega.Eventually(func() error {
		if err := k8sClient.Get(ctx, key, profile); err != nil {
			return err
		}
		if diff := cmp.Diff(want, profile, cmpTrafficManagerProfileOptions); diff != "" {
			return fmt.Errorf("trafficManagerProfile mismatch (-want, +got) :\n%s", diff)
		}
		return nil
	}, timeout, interval).Should(gomega.Succeed(), "Get() trafficManagerProfile mismatch")
}

// IsTrafficManagerProfileDeleted validates whether the profile is deleted or not.
func IsTrafficManagerProfileDeleted(ctx context.Context, k8sClient client.Client, name types.NamespacedName) {
	gomega.Eventually(func() error {
		profile := &fleetnetv1alpha1.TrafficManagerProfile{}
		if err := k8sClient.Get(ctx, name, profile); !errors.IsNotFound(err) {
			return fmt.Errorf("trafficManagerProfile %s still exists or an unexpected error occurred: %w", name, err)
		}
		return nil
	}, timeout, interval).Should(gomega.Succeed(), "Failed to remove trafficManagerProfile %s ", name)
}
