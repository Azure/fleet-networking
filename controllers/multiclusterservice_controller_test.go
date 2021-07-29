// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package controllers

import (
	"context"

	"github.com/Azure/multi-cluster-networking/api/v1alpha1"
	networkingv1alpha1 "github.com/Azure/multi-cluster-networking/api/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("MultiClusterServiceReconciler", func() {
	Context("lifecycle", func() {
		var globalSvcName = "web"
		var clusterSetName = "clusterset"
		var clusterName = "cluster"

		AfterEach(func() {
			// delete ClusterSet after each test.
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

			// delete MultiClusterService's finalizer after each test.
			var multiClusterService networkingv1alpha1.MultiClusterService
			if err = k8sClient.Get(context.TODO(), types.NamespacedName{
				Name:      globalSvcName,
				Namespace: namespace,
			}, &multiClusterService); err == nil {
				multiClusterService.ObjectMeta.Finalizers = RemoveItemFromSlice(multiClusterService.Finalizers, FinalizerName)
				err := k8sClient.Update(context.TODO(), &multiClusterService)
				Expect(client.IgnoreNotFound(err)).To(BeNil())
			}

			// delete MultiClusterService after each test.
			err = k8sClient.Delete(context.TODO(), &v1alpha1.MultiClusterService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      globalSvcName,
					Namespace: namespace,
				},
			})
			Expect(client.IgnoreNotFound(err)).To(BeNil())

		})

		It("Should succeed when the MultiClusterService doesn't exist", func() {
			r := &MultiClusterServiceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}
			ctlResult, err := r.Reconcile(context.TODO(), ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      globalSvcName,
					Namespace: namespace,
				},
			})
			Expect(err).To(BeNil())
			Expect(ctlResult).To(Equal(ctrl.Result{}))
		})

		It("Should report an error when the clusterset doesn't exist", func() {
			err := k8sClient.Create(context.TODO(), &v1alpha1.MultiClusterService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      globalSvcName,
					Namespace: namespace,
				},
				Spec: v1alpha1.MultiClusterServiceSpec{
					Ports: []v1alpha1.MultiClusterServicePort{
						{
							Name:     "http",
							Port:     80,
							Protocol: "TCP",
						},
					},
					ClusterSet: clusterSetName,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			r := &MultiClusterServiceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}
			ctlResult, err := r.Reconcile(context.TODO(), ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      globalSvcName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(BeNil())
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

			err = k8sClient.Create(context.TODO(), &v1alpha1.AKSCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
				Spec: v1alpha1.AKSClusterSpec{
					KubeConfigSecret: "secret",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Create(context.TODO(), &v1alpha1.MultiClusterService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      globalSvcName,
					Namespace: namespace,
				},
				Spec: v1alpha1.MultiClusterServiceSpec{
					Ports: []v1alpha1.MultiClusterServicePort{
						{
							Name:     "http",
							Port:     80,
							Protocol: "TCP",
						},
					},
					ClusterSet: clusterSetName,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			r := &MultiClusterServiceReconciler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}
			ctlResult, err := r.Reconcile(context.TODO(), ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      clusterName,
					Namespace: namespace,
				},
			})
			Expect(err).To(BeNil())
			Expect(ctlResult).To(Equal(ctrl.Result{}))
		})
	})
})
