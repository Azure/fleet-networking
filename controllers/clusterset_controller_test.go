// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package controllers

import (
	"context"

	"github.com/Azure/multi-cluster-networking/api/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("ClusterSetReconciler", func() {
	Context("lifecycle", func() {
		var clusterSetName = "clusterset"
		var clusterName = "cluster"

		AfterEach(func() {
			err := k8sClient.Delete(context.TODO(), &v1alpha1.ClusterSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterSetName,
					Namespace: namespace,
				},
				Spec: v1alpha1.ClusterSetSpec{
					Clusters: []string{clusterName},
				},
			})
			Expect(client.IgnoreNotFound(err)).To(BeNil())
		})

		It("Should succeed when the ClusterSet doesn't exist", func() {
			r := &ClusterSetReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}
			ctlResult, err := r.Reconcile(context.TODO(), ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      clusterSetName,
					Namespace: namespace,
				},
			})
			Expect(err).To(BeNil())
			Expect(ctlResult).To(Equal(ctrl.Result{}))
		})

		It("Should succeed when everything is good", func() {
			err := k8sClient.Create(context.TODO(), &v1alpha1.ClusterSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterSetName,
					Namespace: namespace,
				},
				Spec: v1alpha1.ClusterSetSpec{
					Clusters: []string{clusterName},
				},
			})
			Expect(err).NotTo(HaveOccurred())

			r := &ClusterSetReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}
			ctlResult, err := r.Reconcile(context.TODO(), ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      clusterSetName,
					Namespace: namespace,
				},
			})
			Expect(err).To(BeNil())
			Expect(ctlResult).To(Equal(ctrl.Result{}))
		})
	})
})
