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

// OriginGroupResourceIDFormat mirrors the ARM ID an AFD origin group carries
// in the real Azure response. The FrontDoorBackend reconciler copies this ID
// into FrontDoorBackendStatus.OriginGroupResourceID; leaving it empty would
// mask bugs in the status-population path.
const OriginGroupResourceIDFormat = "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Cdn/profiles/%s/originGroups/%s"

// originGroupKey scopes state by (profile, group) so a single fake client
// can host origin groups belonging to sibling profiles simultaneously.
type originGroupKey struct {
	profile, group string
}

type originGroupStore struct {
	mu     sync.Mutex
	groups map[originGroupKey]armcdn.AFDOriginGroup
}

func newOriginGroupStore() *originGroupStore {
	return &originGroupStore{groups: map[originGroupKey]armcdn.AFDOriginGroup{}}
}

// OriginGroupFake bundles the client with a handle to the underlying store so
// tests can peek/mutate state directly (mirrors WAFPolicyFake shape).
type OriginGroupFake struct {
	Client *armcdn.AFDOriginGroupsClient
	store  *originGroupStore
}

// NewOriginGroupFake returns an armcdn.AFDOriginGroupsClient backed by an
// in-memory fake with its own independent state.
func NewOriginGroupFake() (*OriginGroupFake, error) {
	store := newOriginGroupStore()
	srv := fake.AFDOriginGroupsServer{
		Get:         store.get,
		BeginCreate: store.beginCreate,
		BeginDelete: store.beginDelete,
	}
	factory, err := armcdn.NewClientFactory(DefaultSubscriptionID, &azcorefake.TokenCredential{},
		&arm.ClientOptions{
			ClientOptions: azcore.ClientOptions{
				Transport: fake.NewAFDOriginGroupsServerTransport(&srv),
			},
		})
	if err != nil {
		return nil, err
	}
	return &OriginGroupFake{Client: factory.NewAFDOriginGroupsClient(), store: store}, nil
}

// Has reports whether an OriginGroup with the given (profile, name) exists.
// Tests use this to assert deletion + creation without touching Azure ARM
// semantics directly.
func (f *OriginGroupFake) Has(profileName, groupName string) bool {
	f.store.mu.Lock()
	defer f.store.mu.Unlock()
	_, ok := f.store.groups[originGroupKey{profileName, groupName}]
	return ok
}

func (s *originGroupStore) get(_ context.Context, resourceGroupName string, profileName string, groupName string, _ *armcdn.AFDOriginGroupsClientGetOptions) (resp azcorefake.Responder[armcdn.AFDOriginGroupsClientGetResponse], errResp azcorefake.ErrorResponder) {
	if resourceGroupName != DefaultResourceGroupName {
		errResp.SetResponseError(http.StatusForbidden, "AuthorizationFailed")
		return resp, errResp
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	og, ok := s.groups[originGroupKey{profileName, groupName}]
	if !ok {
		errResp.SetResponseError(http.StatusNotFound, "NotFound")
		return resp, errResp
	}
	resp.SetResponse(http.StatusOK, armcdn.AFDOriginGroupsClientGetResponse{AFDOriginGroup: og}, nil)
	return resp, errResp
}

func (s *originGroupStore) beginCreate(_ context.Context, resourceGroupName string, profileName string, groupName string, parameters armcdn.AFDOriginGroup, _ *armcdn.AFDOriginGroupsClientBeginCreateOptions) (resp azcorefake.PollerResponder[armcdn.AFDOriginGroupsClientCreateResponse], errResp azcorefake.ErrorResponder) {
	if resourceGroupName != DefaultResourceGroupName {
		errResp.SetResponseError(http.StatusForbidden, "AuthorizationFailed")
		return resp, errResp
	}
	created := parameters
	created.Name = ptr.To(groupName)
	created.ID = ptr.To(fmt.Sprintf(OriginGroupResourceIDFormat, DefaultSubscriptionID, DefaultResourceGroupName, profileName, groupName))

	s.mu.Lock()
	s.groups[originGroupKey{profileName, groupName}] = created
	s.mu.Unlock()

	resp.SetTerminalResponse(http.StatusOK, armcdn.AFDOriginGroupsClientCreateResponse{AFDOriginGroup: created}, nil)
	return resp, errResp
}

func (s *originGroupStore) beginDelete(_ context.Context, resourceGroupName string, profileName string, groupName string, _ *armcdn.AFDOriginGroupsClientBeginDeleteOptions) (resp azcorefake.PollerResponder[armcdn.AFDOriginGroupsClientDeleteResponse], errResp azcorefake.ErrorResponder) {
	if resourceGroupName != DefaultResourceGroupName {
		errResp.SetResponseError(http.StatusForbidden, "AuthorizationFailed")
		return resp, errResp
	}
	s.mu.Lock()
	delete(s.groups, originGroupKey{profileName, groupName})
	s.mu.Unlock()

	resp.SetTerminalResponse(http.StatusOK, armcdn.AFDOriginGroupsClientDeleteResponse{}, nil)
	return resp, errResp
}
