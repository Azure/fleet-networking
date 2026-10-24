/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package main

import (
	"fmt"

	"sigs.k8s.io/cloud-provider-azure/pkg/azclient"
	"sigs.k8s.io/cloud-provider-azure/pkg/azclient/policy/ratelimit"

	"go.goms.io/fleet/pkg/utils/cloudconfig/azure"

	"go.goms.io/fleet-networking/pkg/controllers/hub/gateway"
	"go.goms.io/fleet-networking/pkg/providers/azure/frontdoor"
)

type afdProviders struct {
	cleanup     gateway.ProfileCleaner
	observer    gateway.OriginObserver
	provisioner gateway.HTTPPreviewProvisioner
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
	factoryConfig := &azclient.ClientFactoryConfig{
		CloudProviderBackoff: true,
		SubscriptionID:       cloudConfig.SubscriptionID,
	}
	options, err := azclient.GetDefaultResourceClientOption(&cloudConfig.ARMClientConfig, factoryConfig)
	if err != nil {
		return afdProviders{}, fmt.Errorf("get default Azure client options: %w", err)
	}
	if rateLimitPolicy := ratelimit.NewRateLimitPolicy(cloudConfig.Config); rateLimitPolicy != nil {
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
	return afdProviders{cleanup: cleanup, observer: observer, provisioner: provisioner}, nil
}
