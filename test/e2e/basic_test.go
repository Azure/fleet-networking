/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("test", func() {
	var ctx context.Context

	Context("member cluster is setup", func() {
		BeforeEach(func() {
			ctx = context.Background()
		})

		It("should return running member-net-controller-manager", func() {
			Eventually(func() bool {
				podList := &corev1.PodList{}
				listOpts := client.ListOptions{
					LabelSelector: labels.SelectorFromSet(labels.Set{
						"app.kubernetes.io/name": "member-net-controller-manager",
					}),
					Namespace: fleetSystemNamespace,
				}
				err := memberClusters[0].kubeClient.List(ctx, podList, &listOpts)
				if err != nil || len(podList.Items) == 0 {
					return false
				}
				for _, pod := range podList.Items {
					if pod.Status.Phase != corev1.PodRunning {
						return false
					}
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})

		It("should return running mcs-controller-manager", func() {
			Eventually(func() bool {
				podList := &corev1.PodList{}
				listOpts := client.ListOptions{
					LabelSelector: labels.SelectorFromSet(labels.Set{
						"app.kubernetes.io/name": "mcs-controller-manager",
					}),
					Namespace: fleetSystemNamespace,
				}
				err := memberClusters[0].kubeClient.List(ctx, podList, &listOpts)
				if err != nil || len(podList.Items) == 0 {
					return false
				}
				for _, pod := range podList.Items {
					if pod.Status.Phase != corev1.PodRunning {
						return false
					}
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("hub cluster is setup", func() {
		It("should return running hub-net-controller-manager", func() {
			Eventually(func() bool {
				podList := &corev1.PodList{}
				listOpts := client.ListOptions{
					LabelSelector: labels.SelectorFromSet(labels.Set{
						"app.kubernetes.io/name": "hub-net-controller-manager",
					}),
					Namespace: fleetSystemNamespace,
				}
				err := hubCluster.kubeClient.List(ctx, podList, &listOpts)
				if err != nil || len(podList.Items) == 0 {
					return false
				}
				for _, pod := range podList.Items {
					if pod.Status.Phase != corev1.PodRunning {
						return false
					}
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})
})
