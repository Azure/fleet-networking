/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package trafficmanagerclient

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/trafficmanager/armtrafficmanager"
)

// ProfilesInterface defines the interface for Azure Traffic Manager Profiles operations.
type ProfilesInterface interface {
	Get(ctx context.Context, resourceGroupName, profileName string, options *armtrafficmanager.ProfilesClientGetOptions) (armtrafficmanager.ProfilesClientGetResponse, error)
	CreateOrUpdate(ctx context.Context, resourceGroupName, profileName string, parameters armtrafficmanager.Profile, options *armtrafficmanager.ProfilesClientCreateOrUpdateOptions) (armtrafficmanager.ProfilesClientCreateOrUpdateResponse, error)
	Delete(ctx context.Context, resourceGroupName, profileName string, options *armtrafficmanager.ProfilesClientDeleteOptions) (armtrafficmanager.ProfilesClientDeleteResponse, error)
}
