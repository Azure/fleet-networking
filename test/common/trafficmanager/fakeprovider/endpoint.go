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
	"k8s.io/utils/ptr"
)

const (
	ValidPublicIPResourceID = "valid-public-ip-resource-id"
	ValidEndpointTarget     = "valid-endpoint-target"

	Weight = int64(50)
)

var (
	// returnEndpointForbiddenErr is to control whether to return forbidden error or not.
	// Note: it's not thread safe.
	returnEndpointForbiddenErr bool
)

// NewEndpointsClient creates a client which talks to a fake endpoint server.
func NewEndpointsClient() (*armtrafficmanager.EndpointsClient, error) {
	fakeServer := fake.EndpointsServer{
		Delete:         EndpointDelete,
		CreateOrUpdate: EndpointCreateOrUpdate,
	}
	clientFactory, err := armtrafficmanager.NewClientFactory(DefaultSubscriptionID, &azcorefake.TokenCredential{},
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

// DisableEndpointForbiddenErr disables the throttled error for endpoint with CreateForbbidenErrEndpointClusterName.
func DisableEndpointForbiddenErr() {
	returnEndpointForbiddenErr = false
}

// EnableEndpointForbiddenErr enables the throttled error for endpoint with CreateForbbidenErrEndpointClusterName.
func EnableEndpointForbiddenErr() {
	returnEndpointForbiddenErr = true
}

// EndpointDelete returns the http status code based on the profileName and endpointName.
func EndpointDelete(_ context.Context, resourceGroupName string, profileName string, endpointType armtrafficmanager.EndpointType, endpointName string, _ *armtrafficmanager.EndpointsClientDeleteOptions) (resp azcorefake.Responder[armtrafficmanager.EndpointsClientDeleteResponse], errResp azcorefake.ErrorResponder) {
	if resourceGroupName != DefaultResourceGroupName {
		errResp.SetResponseError(http.StatusNotFound, "ResourceGroupNotFound")
		return resp, errResp
	}
	if strings.HasPrefix(profileName, ValidProfileName) && endpointType == armtrafficmanager.EndpointTypeAzureEndpoints && strings.HasPrefix(strings.ToLower(endpointName), ValidBackendName+"#") {
		if endpointName == NotFoundErrEndpointName {
			errResp.SetResponseError(http.StatusNotFound, "NotFound")
			return resp, errResp
		} else if endpointName == FailToDeleteEndpointName {
			errResp.SetResponseError(http.StatusInternalServerError, "InternalServerError")
			return resp, errResp
		}
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

func EndpointCreateOrUpdate(_ context.Context, resourceGroupName string, profileName string, endpointType armtrafficmanager.EndpointType, endpointName string, endpoint armtrafficmanager.Endpoint, _ *armtrafficmanager.EndpointsClientCreateOrUpdateOptions) (resp azcorefake.Responder[armtrafficmanager.EndpointsClientCreateOrUpdateResponse], errResp azcorefake.ErrorResponder) {
	if resourceGroupName != DefaultResourceGroupName {
		errResp.SetResponseError(http.StatusNotFound, "ResourceGroupNotFound")
		return resp, errResp
	}
	if strings.HasPrefix(profileName, ValidProfileName) && endpointType == armtrafficmanager.EndpointTypeAzureEndpoints && strings.HasPrefix(strings.ToLower(endpointName), ValidBackendName+"#") {
		if endpointName == CreateBadRequestErrEndpointName {
			errResp.SetResponseError(http.StatusBadRequest, "BadRequest")
			return resp, errResp
		} else if endpointName == CreateInternalServerErrEndpointName {
			errResp.SetResponseError(http.StatusInternalServerError, "InternalServerError")
			return resp, errResp
		} else if endpointName == CreateForbiddenErrEndpointName {
			if returnEndpointForbiddenErr {
				errResp.SetResponseError(http.StatusForbidden, "Forbidden")
				return resp, errResp
			}
		}
		endpointResp := armtrafficmanager.EndpointsClientCreateOrUpdateResponse{
			Endpoint: armtrafficmanager.Endpoint{
				Name: ptr.To(endpointName),
				Properties: &armtrafficmanager.EndpointProperties{
					TargetResourceID: ptr.To(ValidPublicIPResourceID),
					Weight:           endpoint.Properties.Weight,
					Target:           ptr.To(ValidEndpointTarget),
				},
				Type: ptr.To(string(azureTrafficManagerEndpointTypePrefix + armtrafficmanager.EndpointTypeAzureEndpoints)),
			},
		}
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
