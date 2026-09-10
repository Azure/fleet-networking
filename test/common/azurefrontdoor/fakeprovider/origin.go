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

// OriginResourceIDFormat is the ARM ID format for a single Azure Front Door
// origin under a (profile, originGroup). The FrontDoorBackend reconciler
// copies this into FrontDoorOriginStatus.ResourceID.
const OriginResourceIDFormat = "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Cdn/profiles/%s/originGroups/%s/origins/%s"

type originKey struct {
	profile, group, origin string
}

type originStore struct {
	mu      sync.Mutex
	origins map[originKey]armcdn.AFDOrigin
}

func newOriginStore() *originStore {
	return &originStore{origins: map[originKey]armcdn.AFDOrigin{}}
}

// OriginFake bundles the client with a handle to the underlying store so
// tests can peek/mutate state directly.
type OriginFake struct {
	Client *armcdn.AFDOriginsClient
	store  *originStore
}

// NewOriginFake returns an armcdn.AFDOriginsClient backed by an in-memory
// fake with its own independent state.
func NewOriginFake() (*OriginFake, error) {
	store := newOriginStore()
	srv := fake.AFDOriginsServer{
		Get:         store.get,
		BeginCreate: store.beginCreate,
		BeginDelete: store.beginDelete,
	}
	factory, err := armcdn.NewClientFactory(DefaultSubscriptionID, &azcorefake.TokenCredential{},
		&arm.ClientOptions{
			ClientOptions: azcore.ClientOptions{
				Transport: fake.NewAFDOriginsServerTransport(&srv),
			},
		})
	if err != nil {
		return nil, err
	}
	return &OriginFake{Client: factory.NewAFDOriginsClient(), store: store}, nil
}

// Count returns the number of origins currently programmed under a given
// (profile, originGroup). Tests use this to assert per-cluster origin
// creation without pinning specific names.
func (f *OriginFake) Count(profileName, groupName string) int {
	f.store.mu.Lock()
	defer f.store.mu.Unlock()
	var n int
	for k := range f.store.origins {
		if k.profile == profileName && k.group == groupName {
			n++
		}
	}
	return n
}

// Get returns a snapshot of the origin with the given key, or false when it
// is not present. Tests may assert on Weight / SharedPrivateLinkResource /
// HostName.
func (f *OriginFake) Get(profileName, groupName, originName string) (armcdn.AFDOrigin, bool) {
	f.store.mu.Lock()
	defer f.store.mu.Unlock()
	o, ok := f.store.origins[originKey{profileName, groupName, originName}]
	return o, ok
}

func (s *originStore) get(_ context.Context, resourceGroupName string, profileName string, groupName string, originName string, _ *armcdn.AFDOriginsClientGetOptions) (resp azcorefake.Responder[armcdn.AFDOriginsClientGetResponse], errResp azcorefake.ErrorResponder) {
	if resourceGroupName != DefaultResourceGroupName {
		errResp.SetResponseError(http.StatusForbidden, "AuthorizationFailed")
		return resp, errResp
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.origins[originKey{profileName, groupName, originName}]
	if !ok {
		errResp.SetResponseError(http.StatusNotFound, "NotFound")
		return resp, errResp
	}
	resp.SetResponse(http.StatusOK, armcdn.AFDOriginsClientGetResponse{AFDOrigin: o}, nil)
	return resp, errResp
}

func (s *originStore) beginCreate(_ context.Context, resourceGroupName string, profileName string, groupName string, originName string, parameters armcdn.AFDOrigin, _ *armcdn.AFDOriginsClientBeginCreateOptions) (resp azcorefake.PollerResponder[armcdn.AFDOriginsClientCreateResponse], errResp azcorefake.ErrorResponder) {
	if resourceGroupName != DefaultResourceGroupName {
		errResp.SetResponseError(http.StatusForbidden, "AuthorizationFailed")
		return resp, errResp
	}
	created := parameters
	created.Name = ptr.To(originName)
	created.ID = ptr.To(fmt.Sprintf(OriginResourceIDFormat, DefaultSubscriptionID, DefaultResourceGroupName, profileName, groupName, originName))

	s.mu.Lock()
	s.origins[originKey{profileName, groupName, originName}] = created
	s.mu.Unlock()

	resp.SetTerminalResponse(http.StatusOK, armcdn.AFDOriginsClientCreateResponse{AFDOrigin: created}, nil)
	return resp, errResp
}

func (s *originStore) beginDelete(_ context.Context, resourceGroupName string, profileName string, groupName string, originName string, _ *armcdn.AFDOriginsClientBeginDeleteOptions) (resp azcorefake.PollerResponder[armcdn.AFDOriginsClientDeleteResponse], errResp azcorefake.ErrorResponder) {
	if resourceGroupName != DefaultResourceGroupName {
		errResp.SetResponseError(http.StatusForbidden, "AuthorizationFailed")
		return resp, errResp
	}
	s.mu.Lock()
	delete(s.origins, originKey{profileName, groupName, originName})
	s.mu.Unlock()

	resp.SetTerminalResponse(http.StatusOK, armcdn.AFDOriginsClientDeleteResponse{}, nil)
	return resp, errResp
}
