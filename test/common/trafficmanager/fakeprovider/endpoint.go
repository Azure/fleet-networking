/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package fakeprovider

import (
	"context"
	"net/http"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	azcorefake "github.com/Azure/azure-sdk-for-go/sdk/azcore/fake"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/trafficmanager/armtrafficmanager"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/trafficmanager/armtrafficmanager/fake"
)

// NewEndpointsClient creates a client which talks to a fake endpoint server.
func NewEndpointsClient(subscriptionID string) (*armtrafficmanager.EndpointsClient, error) {
	fakeServer := fake.EndpointsServer{
		Delete: EndpointDelete,
	}
	clientFactory, err := armtrafficmanager.NewClientFactory(subscriptionID, &azcorefake.TokenCredential{},
		&arm.ClientOptions{
			ClientOptions: azcore.ClientOptions{
				Transport: fake.NewEndpointsServerTransport(&fakeServer),
			},
		})
	if err != nil {
		return nil, err
	}
	return clientFactory.NewEndpointsClient(), nil
}

// EndpointDelete returns the http status code based on the profileName and endpointName.
func EndpointDelete(_ context.Context, resourceGroupName string, profileName string, endpointType armtrafficmanager.EndpointType, endpointName string, _ *armtrafficmanager.EndpointsClientDeleteOptions) (resp azcorefake.Responder[armtrafficmanager.EndpointsClientDeleteResponse], errResp azcorefake.ErrorResponder) {
	if resourceGroupName != DefaultResourceGroupName {
		errResp.SetResponseError(http.StatusNotFound, "ResourceGroupNotFound")
		return resp, errResp
	}
	if strings.HasPrefix(profileName, ValidProfileName) && endpointType == armtrafficmanager.EndpointTypeAzureEndpoints && strings.HasPrefix(endpointName, ValidBackendName+"#") {
		endpointResp := armtrafficmanager.EndpointsClientDeleteResponse{}
		resp.SetResponse(http.StatusOK, endpointResp, nil)
	} else {
		if endpointType != armtrafficmanager.EndpointTypeAzureEndpoints {
			// controller should not send other endpoint types.
			errResp.SetResponseError(http.StatusBadRequest, "InvalidEndpointType")
		} else {
			errResp.SetResponseError(http.StatusNotFound, "NotFound")
		}
	}
	return resp, errResp
}
