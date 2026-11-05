/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package fakeprovider

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	azcorefake "github.com/Azure/azure-sdk-for-go/sdk/azcore/fake"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2/fake"
	"k8s.io/utils/ptr"
)

// EndpointHostnameFormat is the *.azurefd.net hostname format the fake assigns
// on create. The reconciler copies this into
// FrontDoorProfileStatus.EndpointHostname, so specs can assert against it.
const EndpointHostnameFormat = "%s.z01.azurefd.net"

// EndpointResourceIDFormat is the ARM ID format for an AFD endpoint under a
// profile. The fake populates .ID on create so the reconciler's WAF-attach
// path (which references the endpoint by ARM ID in a SecurityPolicy
// Association) sees a non-nil ID; a nil ID would silently produce an empty
// Association and mask bugs. Format matches what real AFD returns.
const EndpointResourceIDFormat = "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Cdn/profiles/%s/afdEndpoints/%s"

// endpointKey scopes state by (profile, endpoint) pair so a single fake client
// can host endpoints under multiple profiles simultaneously (some tests may
// create sibling profiles).
type endpointKey struct {
	profile, endpoint string
}

type endpointStore struct {
	mu        sync.Mutex
	endpoints map[endpointKey]armcdn.AFDEndpoint
}

func newEndpointStore() *endpointStore {
	return &endpointStore{endpoints: map[endpointKey]armcdn.AFDEndpoint{}}
}

// NewAFDEndpointClient returns an armcdn.AFDEndpointsClient backed by an
// in-memory fake with its own independent state.
func NewAFDEndpointClient() (*armcdn.AFDEndpointsClient, error) {
	store := newEndpointStore()

	srv := fake.AFDEndpointsServer{
		Get:         store.get,
		BeginCreate: store.beginCreate,
		BeginDelete: store.beginDelete,
	}
	factory, err := armcdn.NewClientFactory(DefaultSubscriptionID, &azcorefake.TokenCredential{},
		&arm.ClientOptions{
			ClientOptions: azcore.ClientOptions{
				Transport: fake.NewAFDEndpointsServerTransport(&srv),
			},
		})
	if err != nil {
		return nil, err
	}
	return factory.NewAFDEndpointsClient(), nil
}

func (s *endpointStore) get(_ context.Context, resourceGroupName string, profileName string, endpointName string, _ *armcdn.AFDEndpointsClientGetOptions) (resp azcorefake.Responder[armcdn.AFDEndpointsClientGetResponse], errResp azcorefake.ErrorResponder) {
	if resourceGroupName != DefaultResourceGroupName {
		errResp.SetResponseError(http.StatusForbidden, "AuthorizationFailed")
		return resp, errResp
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	ep, ok := s.endpoints[endpointKey{profileName, endpointName}]
	if !ok {
		errResp.SetResponseError(http.StatusNotFound, "NotFound")
		return resp, errResp
	}
	resp.SetResponse(http.StatusOK, armcdn.AFDEndpointsClientGetResponse{AFDEndpoint: ep}, nil)
	return resp, errResp
}

func (s *endpointStore) beginCreate(_ context.Context, resourceGroupName string, profileName string, endpointName string, parameters armcdn.AFDEndpoint, _ *armcdn.AFDEndpointsClientBeginCreateOptions) (resp azcorefake.PollerResponder[armcdn.AFDEndpointsClientCreateResponse], errResp azcorefake.ErrorResponder) {
	if resourceGroupName != DefaultResourceGroupName {
		errResp.SetResponseError(http.StatusForbidden, "AuthorizationFailed")
		return resp, errResp
	}
	// Assign a stable synthetic hostname. Reconciler copies HostName into
	// status.endpointHostname; a nil HostName here would leave that field
	// empty and mask bugs in the status-population path.
	created := parameters
	created.Name = ptr.To(endpointName)
	created.ID = ptr.To(fmt.Sprintf(EndpointResourceIDFormat, DefaultSubscriptionID, DefaultResourceGroupName, profileName, endpointName))
	if created.Properties == nil {
		created.Properties = &armcdn.AFDEndpointProperties{}
	}
	created.Properties.HostName = ptr.To(fmt.Sprintf(EndpointHostnameFormat, endpointName))

	s.mu.Lock()
	s.endpoints[endpointKey{profileName, endpointName}] = created
	s.mu.Unlock()

	resp.SetTerminalResponse(http.StatusOK, armcdn.AFDEndpointsClientCreateResponse{AFDEndpoint: created}, nil)
	return resp, errResp
}

func (s *endpointStore) beginDelete(_ context.Context, resourceGroupName string, profileName string, endpointName string, _ *armcdn.AFDEndpointsClientBeginDeleteOptions) (resp azcorefake.PollerResponder[armcdn.AFDEndpointsClientDeleteResponse], errResp azcorefake.ErrorResponder) {
	if resourceGroupName != DefaultResourceGroupName {
		errResp.SetResponseError(http.StatusForbidden, "AuthorizationFailed")
		return resp, errResp
	}
	s.mu.Lock()
	delete(s.endpoints, endpointKey{profileName, endpointName})
	s.mu.Unlock()

	resp.SetTerminalResponse(http.StatusOK, armcdn.AFDEndpointsClientDeleteResponse{}, nil)
	return resp, errResp
}
