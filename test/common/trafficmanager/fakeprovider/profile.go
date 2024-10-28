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
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/trafficmanager/armtrafficmanager/fake"
	"k8s.io/utils/ptr"

	azcorefake "github.com/Azure/azure-sdk-for-go/sdk/azcore/fake"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/trafficmanager/armtrafficmanager"
)

const (
	DefaultResourceGroupName = "default-resource-group-name"

	ValidProfileName              = "valid-profile"
	ValidProfileWithEndpointsName = "valid-profile-with-endpoints"
	ConflictErrProfileName        = "conflict-err-profile"
	InternalServerErrProfileName  = "internal-server-err-profile"
	ThrottledErrProfileName       = "throttled-err-profile"

	ValidBackendName  = "valid-backend"
	ServiceImportName = "test-import"
	ClusterName       = "member-1"

	ProfileDNSNameFormat = "%s.trafficmanager.net"
)

var (
	ValidEndpointName = fmt.Sprintf("%s#%s#%s", ValidBackendName, ServiceImportName, ClusterName)
)

// NewProfileClient creates a client which talks to a fake profile server.
func NewProfileClient(subscriptionID string) (*armtrafficmanager.ProfilesClient, error) {
	fakeServer := fake.ProfilesServer{
		CreateOrUpdate: ProfileCreateOrUpdate,
		Delete:         ProfileDelete,
		Get:            ProfileGet,
	}
	clientFactory, err := armtrafficmanager.NewClientFactory(subscriptionID, &azcorefake.TokenCredential{},
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
		errResp.SetResponseError(http.StatusNotFound, "ResourceGroupNotFound")
		return resp, errResp
	}
	switch profileName {
	case ValidProfileName, ValidProfileWithEndpointsName:
		profileResp := armtrafficmanager.ProfilesClientGetResponse{
			Profile: armtrafficmanager.Profile{
				Name:     ptr.To(profileName),
				Location: ptr.To("global"),
				Properties: &armtrafficmanager.ProfileProperties{
					DNSConfig: &armtrafficmanager.DNSConfig{
						Fqdn:         ptr.To(fmt.Sprintf(ProfileDNSNameFormat, profileName)),
						RelativeName: ptr.To(profileName),
						TTL:          ptr.To[int64](30),
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
			}}
		if profileName == ValidProfileWithEndpointsName {
			profileResp.Profile.Properties.Endpoints = []*armtrafficmanager.Endpoint{
				{
					Name: ptr.To(strings.ToUpper(ValidEndpointName)), // test case-insensitive
				},
				{
					Name: ptr.To("other-endpoint"),
				},
			}
		}
		resp.SetResponse(http.StatusOK, profileResp, nil)
	default:
		errResp.SetResponseError(http.StatusNotFound, "NotFoundError")
	}
	return resp, errResp
}

// ProfileCreateOrUpdate returns the http status code based on the profileName.
func ProfileCreateOrUpdate(_ context.Context, resourceGroupName string, profileName string, parameters armtrafficmanager.Profile, _ *armtrafficmanager.ProfilesClientCreateOrUpdateOptions) (resp azcorefake.Responder[armtrafficmanager.ProfilesClientCreateOrUpdateResponse], errResp azcorefake.ErrorResponder) {
	if resourceGroupName != DefaultResourceGroupName {
		errResp.SetResponseError(http.StatusNotFound, "ResourceGroupNotFound")
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
		errResp.SetResponseError(http.StatusNotFound, "ResourceGroupNotFound")
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
