// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package azureclients

import (
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/azure"
	"sigs.k8s.io/cloud-provider-azure/pkg/auth"
	azclients "sigs.k8s.io/cloud-provider-azure/pkg/azureclients"
	"sigs.k8s.io/cloud-provider-azure/pkg/azureclients/loadbalancerclient"
	"sigs.k8s.io/cloud-provider-azure/pkg/azureclients/publicipclient"
	"sigs.k8s.io/cloud-provider-azure/pkg/retry"
)

// NewLoadBalancerClient creates a new LoadBalancer client.
func NewLoadBalancerClient(config *auth.AzureAuthConfig, env *azure.Environment) (loadbalancerclient.Interface, error) {
	servicePrincipalToken, err := auth.GetServicePrincipalToken(config, env, env.ServiceManagementEndpoint)
	if err != nil {
		return nil, err
	}

	clientConfig := &azclients.ClientConfig{
		CloudName:               config.Cloud,
		SubscriptionID:          config.SubscriptionID,
		ResourceManagerEndpoint: env.ResourceManagerEndpoint,
		Backoff:                 &retry.Backoff{Steps: 1},
		RateLimitConfig:         &azclients.RateLimitConfig{},
		Authorizer:              autorest.NewBearerAuthorizer(servicePrincipalToken),
	}
	return loadbalancerclient.New(clientConfig), nil
}

// NewPublicIPClient creates a new PublicIP client.
func NewPublicIPClient(config *auth.AzureAuthConfig, env *azure.Environment) (publicipclient.Interface, error) {
	servicePrincipalToken, err := auth.GetServicePrincipalToken(config, env, env.ServiceManagementEndpoint)
	if err != nil {
		return nil, err
	}

	clientConfig := &azclients.ClientConfig{
		CloudName:               config.Cloud,
		SubscriptionID:          config.SubscriptionID,
		ResourceManagerEndpoint: env.ResourceManagerEndpoint,
		Backoff:                 &retry.Backoff{Steps: 1},
		RateLimitConfig:         &azclients.RateLimitConfig{},
		Authorizer:              autorest.NewBearerAuthorizer(servicePrincipalToken),
	}
	return publicipclient.New(clientConfig), nil
}
