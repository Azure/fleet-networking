/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package main

import (
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"sigs.k8s.io/cloud-provider-azure/pkg/azclient"
	"sigs.k8s.io/cloud-provider-azure/pkg/azclient/loadbalancerclient"
	"sigs.k8s.io/cloud-provider-azure/pkg/azclient/privatelinkserviceclient"

	"go.goms.io/fleet/pkg/utils/cloudconfig/azure"

	"go.goms.io/fleet-networking/pkg/common/privateorigin"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gateway"
	"go.goms.io/fleet-networking/pkg/controllers/hub/localservice"
	"go.goms.io/fleet-networking/pkg/providers/azure/frontdoor"
)

type afdProviders struct {
	cleanup       gateway.ProfileCleaner
	observer      gateway.OriginObserver
	provisioner   gateway.HTTPPreviewProvisioner
	localServices *localservice.Reconciler
	credential    azcore.TokenCredential
	clientOptions *arm.ClientOptions
	subscription  string
}

func (p afdProviders) validate() error {
	if p.cleanup == nil {
		return fmt.Errorf("AFD cleanup provider is not initialized")
	}
	if p.observer == nil {
		return fmt.Errorf("AFD origin observer is not initialized")
	}
	if p.provisioner == nil {
		return fmt.Errorf("AFD HTTP provisioner is not initialized")
	}
	return nil
}

func initAzureProviders(cloudConfig *azure.CloudConfig, resourceGroup string) (afdProviders, error) {
	if cloudConfig == nil {
		return afdProviders{}, fmt.Errorf("Azure cloud configuration is required")
	}
	authProvider, err := azclient.NewAuthProvider(&cloudConfig.ARMClientConfig, &cloudConfig.AzureAuthConfig)
	if err != nil {
		return afdProviders{}, fmt.Errorf("create Azure auth provider: %w", err)
	}
	armConfig := cloudConfig.ARMClientConfig
	armConfig.CloudProviderBackoff = true
	options, err := azclient.GetDefaultResourceClientOption(&armConfig)
	if err != nil {
		return afdProviders{}, fmt.Errorf("get default Azure client options: %w", err)
	}
	rateLimitPolicy, err := frontdoor.NewRateLimitPolicy(cloudConfig.Config)
	if err != nil {
		return afdProviders{}, fmt.Errorf("configure AFD request rate limits: %w", err)
	}
	if rateLimitPolicy != nil {
		options.ClientOptions.PerCallPolicies = append(options.ClientOptions.PerCallPolicies, rateLimitPolicy)
	}
	// Reuse hub auth, cloud/user-agent, retry and throttling policy; construction performs no ARM operation.
	scope := frontdoor.ResourceScope{
		SubscriptionID: cloudConfig.SubscriptionID, ResourceGroup: resourceGroup,
	}
	cleanup, err := frontdoor.NewProfileCleanup(scope, authProvider.GetAzIdentity(), options)
	if err != nil {
		return afdProviders{}, err
	}
	observer, err := frontdoor.NewOriginObserver(scope, authProvider.GetAzIdentity(), options)
	if err != nil {
		return afdProviders{}, err
	}
	provisioner, err := frontdoor.NewRouteProvisioner(scope, authProvider.GetAzIdentity(), options)
	if err != nil {
		return afdProviders{}, err
	}
	return afdProviders{cleanup: cleanup, observer: observer, provisioner: provisioner,
		credential: authProvider.GetAzIdentity(), clientOptions: options, subscription: cloudConfig.SubscriptionID}, nil
}

func initLocalServiceDiscovery(providers afdProviders, resourceGroup string) (*privateorigin.Discovery, error) {
	if providers.credential == nil || providers.clientOptions == nil || providers.subscription == "" {
		return nil, fmt.Errorf("local Service discovery requires initialized Azure credentials and client options")
	}
	// Reuse the controller's auth, retry, user-agent, and rate-limit configuration.
	lb, err := loadbalancerclient.New(providers.subscription, providers.credential, providers.clientOptions)
	if err != nil {
		return nil, err
	}
	pls, err := privatelinkserviceclient.New(providers.subscription, providers.credential, providers.clientOptions)
	if err != nil {
		return nil, err
	}
	return &privateorigin.Discovery{LoadBalancerClient: lb, PrivateLinkServiceClient: pls, ResourceGroupName: resourceGroup}, nil
}
