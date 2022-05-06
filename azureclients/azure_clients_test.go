// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package azureclients

import (
	"github.com/Azure/go-autorest/autorest/azure"
	"sigs.k8s.io/cloud-provider-azure/pkg/auth"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Azure Client", func() {

	Context("LoadBalancerClient", func() {
		It("Should report an error when the config is empty", func() {
			client, err := NewLoadBalancerClient(&auth.AzureAuthConfig{}, &azure.Environment{})
			Expect(err).NotTo(BeNil())
			Expect(client).To(BeNil())
		})

		It("Should succeed when the config is correct", func() {
			client, err := NewLoadBalancerClient(&auth.AzureAuthConfig{
				TenantID:        "tenantID",
				AADClientID:     "aadClientId",
				AADClientSecret: "aadClientSecret",
			}, &azure.PublicCloud)
			Expect(err).To(BeNil())
			Expect(client).NotTo(BeNil())
		})

	})

	Context("PublicIPClient", func() {
		It("Should report an error when the config is empty", func() {
			client, err := NewPublicIPClient(&auth.AzureAuthConfig{}, &azure.Environment{})
			Expect(err).NotTo(BeNil())
			Expect(client).To(BeNil())
		})

		It("Should succeed when the config is correct", func() {
			client, err := NewPublicIPClient(&auth.AzureAuthConfig{
				TenantID:        "tenantID",
				AADClientID:     "aadClientId",
				AADClientSecret: "aadClientSecret",
			}, &azure.PublicCloud)
			Expect(err).To(BeNil())
			Expect(client).NotTo(BeNil())
		})
	})

})
