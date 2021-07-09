// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package azureclients

import (
	"context"

	"github.com/Azure/go-autorest/autorest/azure"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Azure Auth", func() {
	Context("GetAzureConfigFromSecret", func() {
		var secretName = "secret"

		AfterEach(func() {
			err := k8sClient.Delete(context.TODO(), &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: namespace,
				},
			})
			Expect(client.IgnoreNotFound(err)).To(BeNil())
		})

		It("Should report an error when the kubeConfigSecret doesn't exist", func() {
			config, env, err := GetAzureConfigFromSecret(k8sClient, namespace, secretName)
			Expect(err).NotTo(BeNil())
			Expect(config).To(BeNil())
			Expect(env).To(BeNil())
		})

		It("Should report an error when the kubeConfigSecret doesn't have cloud-config data", func() {
			err := k8sClient.Create(context.TODO(), &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			config, env, err := GetAzureConfigFromSecret(k8sClient, namespace, secretName)
			Expect(err).NotTo(BeNil())
			Expect(config).To(BeNil())
			Expect(env).To(BeNil())
		})

		It("Should report an error when the cloud-config data is not in correct format", func() {
			err := k8sClient.Create(context.TODO(), &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: namespace,
				},
				Data: map[string][]byte{
					"cloud-config": []byte("random-bytes"),
				},
			})
			Expect(err).NotTo(HaveOccurred())

			config, env, err := GetAzureConfigFromSecret(k8sClient, namespace, secretName)
			Expect(err).NotTo(BeNil())
			Expect(config).To(BeNil())
			Expect(env).To(BeNil())
		})

		It("Should succeed when the secret is configured correctly", func() {
			azureConfig := `{"useManagedIdentityExtension": true}`
			err := k8sClient.Create(context.TODO(), &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: namespace,
				},
				Data: map[string][]byte{
					"cloud-config": []byte(azureConfig),
				},
			})
			Expect(err).NotTo(HaveOccurred())

			config, env, err := GetAzureConfigFromSecret(k8sClient, namespace, secretName)
			Expect(err).NotTo(HaveOccurred())
			Expect(config).NotTo(BeNil())
			Expect(env).NotTo(BeNil())
			Expect(env).To(Equal(&azure.PublicCloud))
			Expect(config.UseManagedIdentityExtension).To(BeTrue())
		})
	})
})
