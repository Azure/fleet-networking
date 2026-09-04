/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package fakeprovider provides a fake Azure implementation of the Front Door
// (armcdn/v2) sub-clients used by the AFD controllers. The package is
// deliberately small: it only exposes the surface (Get, BeginCreate,
// BeginDelete) actually invoked by the reconcilers today. Additional operations
// (Update, ListByResourceGroup, etc.) should be added on demand when
// controllers grow to use them.
//
// The fakes are STATEFUL: create-then-get returns the previously-created
// resource, and delete-then-get returns NotFound. This mirrors real Azure
// semantics closely enough that reconcilers exercising the "get before create"
// idempotency pattern behave the same in envtest as in production.
//
// Error injection uses magic resource names (see the *Err* constants below),
// matching the convention in test/common/trafficmanager/fakeprovider.
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

const (
	// DefaultSubscriptionID is the subscription used when constructing the fake
	// client factory. Tests requesting a different subscription will receive a
	// Forbidden response (see the guard in each handler).
	DefaultSubscriptionID = "default-subscription-id"
	// DefaultResourceGroupName is the resource group all valid fake resources
	// live in. Any other RG name yields Forbidden.
	DefaultResourceGroupName = "default-resource-group-name"
	// ProfileNamespace is the Kubernetes namespace used by suite_test.go setups
	// for FrontDoorProfile / FrontDoorCustomDomain CRs. Matches the ATM
	// convention (test/common/trafficmanager/fakeprovider.ProfileNamespace) so
	// integration tests read symmetrically.
	ProfileNamespace = "afd-profile-ns"

	// ProfileResourceIDFormat is the ARM ID format for an AFD profile.
	// Kept as a package-level constant so tests can assert on
	// FrontDoorProfileStatus.ResourceID without duplicating the layout.
	ProfileResourceIDFormat = "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Cdn/profiles/%s"

	// Magic profile names that trigger canned error paths in the fake. These
	// exercise the reconciler's error-classification branches without needing
	// an out-of-band control channel.
	ConflictErrProfileName       = "conflict-err-profile"
	InternalServerErrProfileName = "internal-server-err-profile"
)

// profileState is the in-memory record backing the fake. Only the fields the
// reconciler reads back via Get / status update are tracked; anything else can
// be added when a reconciler starts to consume it.
type profileState struct {
	profile armcdn.Profile
}

// profileStore is the shared state across all fake ProfilesServer handlers for
// a single NewProfileClient call. A fresh store per client keeps tests isolated
// (BeforeSuite constructs one client, so all specs in a suite share state — the
// same isolation model ATM uses).
type profileStore struct {
	mu       sync.Mutex
	profiles map[string]*profileState
}

func newProfileStore() *profileStore {
	return &profileStore{profiles: map[string]*profileState{}}
}

// NewProfileClient creates an armcdn.ProfilesClient backed by an in-memory
// fake. Each call returns an INDEPENDENT client with its own state store, so
// tests do not need to reset state between runs.
func NewProfileClient() (*armcdn.ProfilesClient, error) {
	store := newProfileStore()

	srv := fake.ProfilesServer{
		Get:         store.get,
		BeginCreate: store.beginCreate,
		BeginDelete: store.beginDelete,
	}
	factory, err := armcdn.NewClientFactory(DefaultSubscriptionID, &azcorefake.TokenCredential{},
		&arm.ClientOptions{
			ClientOptions: azcore.ClientOptions{
				Transport: fake.NewProfilesServerTransport(&srv),
			},
		})
	if err != nil {
		return nil, err
	}
	return factory.NewProfilesClient(), nil
}

func (s *profileStore) get(_ context.Context, resourceGroupName string, profileName string, _ *armcdn.ProfilesClientGetOptions) (resp azcorefake.Responder[armcdn.ProfilesClientGetResponse], errResp azcorefake.ErrorResponder) {
	if resourceGroupName != DefaultResourceGroupName {
		errResp.SetResponseError(http.StatusForbidden, "AuthorizationFailed")
		return resp, errResp
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	st, ok := s.profiles[profileName]
	if !ok {
		// AFD's real "profile does not exist" response — the reconciler's
		// azureerrors.IsNotFound branch depends on this shape.
		errResp.SetResponseError(http.StatusNotFound, "NotFound")
		return resp, errResp
	}
	resp.SetResponse(http.StatusOK, armcdn.ProfilesClientGetResponse{Profile: st.profile}, nil)
	return resp, errResp
}

func (s *profileStore) beginCreate(_ context.Context, resourceGroupName string, profileName string, parameters armcdn.Profile, _ *armcdn.ProfilesClientBeginCreateOptions) (resp azcorefake.PollerResponder[armcdn.ProfilesClientCreateResponse], errResp azcorefake.ErrorResponder) {
	if resourceGroupName != DefaultResourceGroupName {
		errResp.SetResponseError(http.StatusForbidden, "AuthorizationFailed")
		return resp, errResp
	}
	// Error-injection branches: return immediately, do NOT store state.
	switch profileName {
	case ConflictErrProfileName:
		resp.SetTerminalError(http.StatusConflict, "Conflict")
		return resp, errResp
	case InternalServerErrProfileName:
		resp.SetTerminalError(http.StatusInternalServerError, "InternalServerError")
		return resp, errResp
	}

	// Happy path: copy the payload, patch in the ARM ID (which real AFD
	// assigns), and remember it so subsequent Get calls succeed.
	created := parameters
	created.Name = ptr.To(profileName)
	created.ID = ptr.To(fmt.Sprintf(ProfileResourceIDFormat, DefaultSubscriptionID, DefaultResourceGroupName, profileName))
	if created.Properties == nil {
		created.Properties = &armcdn.ProfileProperties{}
	}

	s.mu.Lock()
	s.profiles[profileName] = &profileState{profile: created}
	s.mu.Unlock()

	resp.SetTerminalResponse(http.StatusOK, armcdn.ProfilesClientCreateResponse{Profile: created}, nil)
	return resp, errResp
}

func (s *profileStore) beginDelete(_ context.Context, resourceGroupName string, profileName string, _ *armcdn.ProfilesClientBeginDeleteOptions) (resp azcorefake.PollerResponder[armcdn.ProfilesClientDeleteResponse], errResp azcorefake.ErrorResponder) {
	if resourceGroupName != DefaultResourceGroupName {
		errResp.SetResponseError(http.StatusForbidden, "AuthorizationFailed")
		return resp, errResp
	}
	s.mu.Lock()
	_, ok := s.profiles[profileName]
	delete(s.profiles, profileName)
	s.mu.Unlock()

	if !ok {
		// Reconciler treats 404 on delete as "already gone" (idempotent
		// cleanup). Returning it here validates that path.
		errResp.SetResponseError(http.StatusNotFound, "NotFound")
		return resp, errResp
	}
	resp.SetTerminalResponse(http.StatusOK, armcdn.ProfilesClientDeleteResponse{}, nil)
	return resp, errResp
}
