/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package fakeprovider provides a fake azure implementation of traffic manager resources.
package fakeprovider

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	azcorefake "github.com/Azure/azure-sdk-for-go/sdk/azcore/fake"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/trafficmanager/armtrafficmanager"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/trafficmanager/armtrafficmanager/fake"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	"go.goms.io/fleet-networking/pkg/common/objectmeta"
)

const (
	DefaultSubscriptionID    = "default-subscription-id"
	DefaultResourceGroupName = "default-resource-group-name"

	ProfileResourceIDFormat = "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/trafficManagerProfiles/%s"

	ValidProfileName                         = "valid-profile"
	ValidProfileWithEndpointsName            = "valid-profile-with-endpoints"
	ValidProfileWithNilPropertiesName        = "valid-profile-with-empty-properties"
	ValidProfileWithFailToDeleteEndpointName = "valid-profile-with-fail-to-delete-endpoint"
	ConflictErrProfileName                   = "conflict-err-profile"
	InternalServerErrProfileName             = "internal-server-err-profile"
	ThrottledErrProfileName                  = "throttled-err-profile"
	RequestTimeoutProfileName                = "request-timeout-profile"

	ValidBackendName                           = "valid-backend"
	ServiceImportName                          = "test-import"
	ClusterName                                = "member-1"
	CreateBadRequestErrEndpointClusterName     = "create-bad-request-endpoint-cluster"
	CreateInternalServerErrEndpointClusterName = "create-internal-err-endpoint-cluster"
	CreateForbiddenErrEndpointClusterName      = "create-forbidden-endpoint-cluster"

	ProfileDNSNameFormat                  = "%s.trafficmanager.net"
	azureTrafficManagerEndpointTypePrefix = "Microsoft.Network/trafficManagerProfiles/"

	ProfileNamespace = "profile-ns" // so that the atm profile is predictable
)

var (
	ValidEndpointName                   = fmt.Sprintf("%s#%s#%s", ValidBackendName, ServiceImportName, ClusterName)
	NotFoundErrEndpointName             = fmt.Sprintf("%s#%s#%s", ValidBackendName, ServiceImportName, "not-found")
	FailToDeleteEndpointName            = fmt.Sprintf("%s#%s#%s", ValidBackendName, ServiceImportName, "fail-to-delete")
	CreateBadRequestErrEndpointName     = fmt.Sprintf("%s#%s#%s", ValidBackendName, ServiceImportName, CreateBadRequestErrEndpointClusterName)
	CreateInternalServerErrEndpointName = fmt.Sprintf("%s#%s#%s", ValidBackendName, ServiceImportName, CreateInternalServerErrEndpointClusterName)
	CreateForbiddenErrEndpointName      = fmt.Sprintf("%s#%s#%s", ValidBackendName, ServiceImportName, CreateForbiddenErrEndpointClusterName)
)

// NewProfileClient creates a client which talks to a fake profile server.
func NewProfileClient() (*armtrafficmanager.ProfilesClient, error) {
	fakeServer := fake.ProfilesServer{
		CreateOrUpdate: ProfileCreateOrUpdate,
		Delete:         ProfileDelete,
		Get:            ProfileGet,
	}
	clientFactory, err := armtrafficmanager.NewClientFactory(DefaultSubscriptionID, &azcorefake.TokenCredential{},
		&arm.ClientOptions{
			ClientOptions: azcore.ClientOptions{
				Transport: fake.NewProfilesServerTransport(&fakeServer),
			},
		})
	if err != nil {
		return nil, err
	}
	return clientFactory.NewProfilesClient(), nil
}

// ProfileGet returns the http status code based on the profileName.
func ProfileGet(_ context.Context, resourceGroupName string, profileName string, _ *armtrafficmanager.ProfilesClientGetOptions) (resp azcorefake.Responder[armtrafficmanager.ProfilesClientGetResponse], errResp azcorefake.ErrorResponder) {
	if resourceGroupName != DefaultResourceGroupName {
		errResp.SetResponseError(http.StatusForbidden, "AuthorizationFailed")
		return resp, errResp
	}
	switch profileName {
	case ValidProfileName, ValidProfileWithEndpointsName, ValidProfileWithFailToDeleteEndpointName:
		namespacedName := types.NamespacedName{Name: profileName, Namespace: ProfileNamespace}
		profileResp := armtrafficmanager.ProfilesClientGetResponse{
			Profile: armtrafficmanager.Profile{
				Name:     ptr.To(profileName),
				Location: ptr.To("global"),
				Properties: &armtrafficmanager.ProfileProperties{
					DNSConfig: &armtrafficmanager.DNSConfig{
						Fqdn:         ptr.To(fmt.Sprintf(ProfileDNSNameFormat, profileName)),
						RelativeName: ptr.To(profileName),
						TTL:          ptr.To(int64(60)),
					},
					Endpoints: []*armtrafficmanager.Endpoint{},
					MonitorConfig: &armtrafficmanager.MonitorConfig{
						IntervalInSeconds:         ptr.To(int64(10)),
						Path:                      ptr.To("/healthz"),
						Port:                      ptr.To(int64(8080)),
						Protocol:                  ptr.To(armtrafficmanager.MonitorProtocolHTTP),
						TimeoutInSeconds:          ptr.To(int64(9)),
						ToleratedNumberOfFailures: ptr.To(int64(4)),
					},
					ProfileStatus:               ptr.To(armtrafficmanager.ProfileStatusEnabled),
					TrafficRoutingMethod:        ptr.To(armtrafficmanager.TrafficRoutingMethodWeighted),
					TrafficViewEnrollmentStatus: ptr.To(armtrafficmanager.TrafficViewEnrollmentStatusDisabled),
				},
				Tags: map[string]*string{
					objectmeta.AzureTrafficManagerProfileTagKey: ptr.To(namespacedName.String()),
				},
			}}
		if profileName == ValidProfileWithEndpointsName {
			profileResp.Profile.Properties.Endpoints = []*armtrafficmanager.Endpoint{
				{
					Name: ptr.To(strings.ToUpper(ValidEndpointName)), // test case-insensitive
					Properties: &armtrafficmanager.EndpointProperties{
						TargetResourceID: ptr.To(ValidPublicIPResourceID),
						Weight:           ptr.To(Weight),
					},
					Type: ptr.To(string(azureTrafficManagerEndpointTypePrefix + armtrafficmanager.EndpointTypeAzureEndpoints)),
				},
				{
					Name: ptr.To("other-endpoint"),
				},
				{
					// used to test not-found endpoint
					Name: ptr.To(NotFoundErrEndpointName),
				},
			}
		} else if profileName == ValidProfileWithFailToDeleteEndpointName {
			profileResp.Profile.Properties.Endpoints = []*armtrafficmanager.Endpoint{
				{
					Name: ptr.To(FailToDeleteEndpointName),
				},
			}
		}
		profileResp.Profile.ID = ptr.To(fmt.Sprintf(ProfileResourceIDFormat, DefaultSubscriptionID, DefaultResourceGroupName, profileName))
		resp.SetResponse(http.StatusOK, profileResp, nil)
	case ValidProfileWithNilPropertiesName:
		profileResp := armtrafficmanager.ProfilesClientGetResponse{
			Profile: armtrafficmanager.Profile{
				Name:     ptr.To(profileName),
				Location: ptr.To("global"),
			}}
		profileResp.Profile.ID = ptr.To(fmt.Sprintf(ProfileResourceIDFormat, DefaultSubscriptionID, DefaultResourceGroupName, profileName))
		resp.SetResponse(http.StatusOK, profileResp, nil)
	case RequestTimeoutProfileName:
		errResp.SetResponseError(http.StatusRequestTimeout, "RequestTimeoutError")
	default:
		errResp.SetResponseError(http.StatusNotFound, "NotFoundError")
	}
	return resp, errResp
}

// ProfileCreateOrUpdate returns the http status code based on the profileName.
func ProfileCreateOrUpdate(_ context.Context, resourceGroupName string, profileName string, parameters armtrafficmanager.Profile, _ *armtrafficmanager.ProfilesClientCreateOrUpdateOptions) (resp azcorefake.Responder[armtrafficmanager.ProfilesClientCreateOrUpdateResponse], errResp azcorefake.ErrorResponder) {
	if resourceGroupName != DefaultResourceGroupName {
		errResp.SetResponseError(http.StatusForbidden, "AuthorizationFailed")
		return resp, errResp
	}
	switch profileName {
	case ConflictErrProfileName:
		errResp.SetResponseError(http.StatusConflict, "Conflict")
	case InternalServerErrProfileName:
		errResp.SetResponseError(http.StatusInternalServerError, "InternalServerError")
	case ThrottledErrProfileName:
		errResp.SetResponseError(http.StatusTooManyRequests, "ThrottledError")
	case ValidProfileName:
		if parameters.Properties.MonitorConfig.IntervalInSeconds != nil && *parameters.Properties.MonitorConfig.IntervalInSeconds == 10 {
			if parameters.Properties.MonitorConfig.TimeoutInSeconds != nil && *parameters.Properties.MonitorConfig.TimeoutInSeconds > 9 {
				errResp.SetResponseError(http.StatusBadRequest, "BadRequestError")
				return
			}
		}
		profileResp := armtrafficmanager.ProfilesClientCreateOrUpdateResponse{
			Profile: armtrafficmanager.Profile{
				Name:     ptr.To(profileName),
				Location: ptr.To("global"),
				Properties: &armtrafficmanager.ProfileProperties{
					DNSConfig: &armtrafficmanager.DNSConfig{
						Fqdn:         ptr.To(fmt.Sprintf(ProfileDNSNameFormat, *parameters.Properties.DNSConfig.RelativeName)),
						RelativeName: parameters.Properties.DNSConfig.RelativeName,
						TTL:          ptr.To[int64](30),
					},
					Endpoints:                   []*armtrafficmanager.Endpoint{},
					MonitorConfig:               parameters.Properties.MonitorConfig,
					ProfileStatus:               ptr.To(armtrafficmanager.ProfileStatusEnabled),
					TrafficRoutingMethod:        ptr.To(armtrafficmanager.TrafficRoutingMethodWeighted),
					TrafficViewEnrollmentStatus: ptr.To(armtrafficmanager.TrafficViewEnrollmentStatusDisabled),
				},
				ID: ptr.To(fmt.Sprintf(ProfileResourceIDFormat, DefaultSubscriptionID, DefaultResourceGroupName, profileName)),
			}}
		resp.SetResponse(http.StatusOK, profileResp, nil)
	default:
		errResp.SetResponseError(http.StatusBadRequest, "BadRequestError")
	}
	return resp, errResp
}

// ProfileDelete returns the http status code based on the profileName.
func ProfileDelete(_ context.Context, resourceGroupName string, profileName string, _ *armtrafficmanager.ProfilesClientDeleteOptions) (resp azcorefake.Responder[armtrafficmanager.ProfilesClientDeleteResponse], errResp azcorefake.ErrorResponder) {
	if resourceGroupName != DefaultResourceGroupName {
		errResp.SetResponseError(http.StatusForbidden, "AuthorizationFailed")
		return resp, errResp
	}
	switch profileName {
	case ValidProfileName:
		profileResp := armtrafficmanager.ProfilesClientDeleteResponse{}
		resp.SetResponse(http.StatusOK, profileResp, nil)
	default:
		errResp.SetResponseError(http.StatusNotFound, "NotFound")
	}
	return resp, errResp
}
