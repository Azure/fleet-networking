/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
// Package trafficmanagerclient provides a client for Azure Traffic Manager profiles.
// It wraps the Azure SDK for Go's Traffic Manager client and provides additional functionality
// such as metrics collection.
package trafficmanagerclient

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/trafficmanager/armtrafficmanager"
	"sigs.k8s.io/cloud-provider-azure/pkg/azclient/metrics"
)

// ProfilesClient is a wrapper around the Azure Traffic Manager Profiles client.
// It provides methods to interact with Traffic Manager profiles in Azure.
type ProfilesClient struct {
	*armtrafficmanager.ProfilesClient
	subscriptionID string
}

// NewProfilesClient creates an Azure traffic manager profile client.
func NewProfilesClient(client *armtrafficmanager.ProfilesClient, subscriptionID string) *ProfilesClient {
	return &ProfilesClient{
		ProfilesClient: client,
		subscriptionID: subscriptionID,
	}
}

// Get gets a Traffic Manager profile by its name and resource group.
func (client *ProfilesClient) Get(ctx context.Context, resourceGroupName, profileName string, options *armtrafficmanager.ProfilesClientGetOptions) (resp armtrafficmanager.ProfilesClientGetResponse, err error) {
	metricsCtx := metrics.BeginARMRequest(client.subscriptionID, resourceGroupName, "TrafficManagerProfile", "get")
	defer func() { metricsCtx.Observe(ctx, err) }()

	resp, err = client.ProfilesClient.Get(ctx, resourceGroupName, profileName, options)
	return resp, err
}

// CreateOrUpdate creates or updates a Traffic Manager profile with the specified parameters.
func (client *ProfilesClient) CreateOrUpdate(ctx context.Context, resourceGroupName, profileName string, parameters armtrafficmanager.Profile, options *armtrafficmanager.ProfilesClientCreateOrUpdateOptions) (resp armtrafficmanager.ProfilesClientCreateOrUpdateResponse, err error) {
	metricsCtx := metrics.BeginARMRequest(client.subscriptionID, resourceGroupName, "TrafficManagerProfile", "createOrUpdate")
	defer func() { metricsCtx.Observe(ctx, err) }()

	resp, err = client.ProfilesClient.CreateOrUpdate(ctx, resourceGroupName, profileName, parameters, options)
	return resp, err
}

// Delete deletes a Traffic Manager profile by its name and resource group.
func (client *ProfilesClient) Delete(ctx context.Context, resourceGroupName, profileName string, options *armtrafficmanager.ProfilesClientDeleteOptions) (resp armtrafficmanager.ProfilesClientDeleteResponse, err error) {
	metricsCtx := metrics.BeginARMRequest(client.subscriptionID, resourceGroupName, "TrafficManagerProfile", "delete")
	defer func() { metricsCtx.Observe(ctx, err) }()

	resp, err = client.ProfilesClient.Delete(ctx, resourceGroupName, profileName, options)
	return resp, err
}
