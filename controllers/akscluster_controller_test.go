// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package controllers

import (
	"context"
	"fmt"

	"github.com/Azure/multi-cluster-networking/api/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	testKubeConfig = `apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    name: test
    certificate-authority:
    server: %s
users:
- name: test
  user:
contexts:
- name: test
  context:
    cluster: test
    user: test
current-context: test`
)

var _ = Describe("AKSClusterReconciler", func() {
	Context("lifecycle", func() {
		var secretName = "secret"
		var clusterName = "cluster"

		AfterEach(func() {
			err := k8sClient.Delete(context.TODO(), &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: namespace,
				},
			})
			Expect(client.IgnoreNotFound(err)).To(BeNil())

			err = k8sClient.Delete(context.TODO(), &v1alpha1.AKSCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
				Spec: v1alpha1.AKSClusterSpec{
					KubeConfigSecret: secretName,
				},
			})
			Expect(client.IgnoreNotFound(err)).To(BeNil())
		})

		It("Should report an error when the kubeConfigSecret doesn't exist", func() {
			r := &AKSClusterReconciler{Client: k8sClient}
			config, err := r.getKubeConfig(secretName, namespace)
			Expect(err).NotTo(BeNil())
			Expect(config).To(Equal(""))
		})

		It("Should succeed when the AKSCluster doesn't exist", func() {
			r := &AKSClusterReconciler{
				Client:          k8sClient,
				Scheme:          scheme.Scheme,
				ClusterManagers: make(map[string]*ClusterManager),
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

		It("Should report an error when the kubeConfigSecret data doesn't contain 'kubeconfig'", func() {
			err := k8sClient.Create(context.TODO(), &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Create(context.TODO(), &v1alpha1.AKSCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
				Spec: v1alpha1.AKSClusterSpec{
					KubeConfigSecret: secretName,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			r := &AKSClusterReconciler{
				Client:          k8sClient,
				Scheme:          scheme.Scheme,
				ClusterManagers: make(map[string]*ClusterManager),
			}
			ctlResult, err := r.Reconcile(context.TODO(), ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      clusterName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(BeNil())
			Expect(ctlResult).To(Equal(ctrl.Result{}))
		})

		It("Should succeed when the kubeConfigSecret is configured correctly", func() {
			err := k8sClient.Create(context.TODO(), &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: namespace,
				},
				Data: map[string][]byte{
					"kubeconfig": []byte(fmt.Sprintf(testKubeConfig, cfg.Host)),
				},
			})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Create(context.TODO(), &v1alpha1.AKSCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
				Spec: v1alpha1.AKSClusterSpec{
					KubeConfigSecret: secretName,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			r := &AKSClusterReconciler{
				Client:          k8sClient,
				Scheme:          scheme.Scheme,
				ClusterManagers: make(map[string]*ClusterManager),
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
