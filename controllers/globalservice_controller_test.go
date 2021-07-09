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

var _ = Describe("GlobalServiceReconciler", func() {
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

			// delete GlobalService's finalizer after each test.
			var globalService networkingv1alpha1.GlobalService
			if err = k8sClient.Get(context.TODO(), types.NamespacedName{
				Name:      globalSvcName,
				Namespace: namespace,
			}, &globalService); err == nil {
				globalService.ObjectMeta.Finalizers = RemoveItemFromSlice(globalService.Finalizers, FinalizerName)
				err := k8sClient.Update(context.TODO(), &globalService)
				Expect(client.IgnoreNotFound(err)).To(BeNil())
			}

			// delete GlobalService after each test.
			err = k8sClient.Delete(context.TODO(), &v1alpha1.GlobalService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      globalSvcName,
					Namespace: namespace,
				},
			})
			Expect(client.IgnoreNotFound(err)).To(BeNil())

		})

		It("Should succeed when the GlobalService doesn't exist", func() {
			r := &GlobalServiceReconciler{
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
			err := k8sClient.Create(context.TODO(), &v1alpha1.GlobalService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      globalSvcName,
					Namespace: namespace,
				},
				Spec: v1alpha1.GlobalServiceSpec{
					Ports: []v1alpha1.GlobalServicePort{
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

			r := &GlobalServiceReconciler{
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

			err = k8sClient.Create(context.TODO(), &v1alpha1.GlobalService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      globalSvcName,
					Namespace: namespace,
				},
				Spec: v1alpha1.GlobalServiceSpec{
					Ports: []v1alpha1.GlobalServicePort{
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

			r := &GlobalServiceReconciler{
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
