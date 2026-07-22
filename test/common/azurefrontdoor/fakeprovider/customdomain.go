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

// CustomDomainResourceIDFormat is the ARM ID format for an AFD custom domain.
const CustomDomainResourceIDFormat = "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Cdn/profiles/%s/customDomains/%s"

// FakeValidationToken is the DNS validation token returned on every create.
// It's a synthetic value — real AFD-issued tokens are longer opaque strings —
// but is sufficient for asserting that
// FrontDoorCustomDomainStatus.DNSValidationToken is populated from
// AFDDomainProperties.ValidationProperties.
const FakeValidationToken = "fleet-fake-dns-validation-token" //nolint:gosec // G101: synthetic value used by the fake AFD server to populate AFDDomainProperties.ValidationProperties.ValidationToken; not a real credential.

// customDomainKey scopes state by (profile, domain) pair.
type customDomainKey struct {
	profile, domain string
}

type customDomainStore struct {
	mu      sync.Mutex
	domains map[customDomainKey]armcdn.AFDDomain
}

func newCustomDomainStore() *customDomainStore {
	return &customDomainStore{domains: map[customDomainKey]armcdn.AFDDomain{}}
}

// NewCustomDomainClient returns an armcdn.AFDCustomDomainsClient backed by an
// in-memory fake with its own independent state.
//
// Design note: this fake returns Approved validation state IMMEDIATELY on
// create. Real AFD returns Pending and only transitions to Approved after the
// customer publishes the DNS TXT record and AFD's asynchronous validator
// re-checks it. Testing the multi-step transition would require either
// non-terminal responses on the fake or explicit state-mutation hooks; the
// minimal scaffold here trades that fidelity for coverage of the happy-path
// status-population code in reflectAzureStateToStatus. A future test can
// override the beginCreate handler via a functional option to return Pending
// for one Get then Approved for the next, if pending-state coverage is
// needed.
func NewCustomDomainClient() (*armcdn.AFDCustomDomainsClient, error) {
	store := newCustomDomainStore()

	srv := fake.AFDCustomDomainsServer{
		Get:         store.get,
		BeginCreate: store.beginCreate,
		BeginDelete: store.beginDelete,
	}
	factory, err := armcdn.NewClientFactory(DefaultSubscriptionID, &azcorefake.TokenCredential{},
		&arm.ClientOptions{
			ClientOptions: azcore.ClientOptions{
				Transport: fake.NewAFDCustomDomainsServerTransport(&srv),
			},
		})
	if err != nil {
		return nil, err
	}
	return factory.NewAFDCustomDomainsClient(), nil
}

func (s *customDomainStore) get(_ context.Context, resourceGroupName string, profileName string, customDomainName string, _ *armcdn.AFDCustomDomainsClientGetOptions) (resp azcorefake.Responder[armcdn.AFDCustomDomainsClientGetResponse], errResp azcorefake.ErrorResponder) {
	if resourceGroupName != DefaultResourceGroupName {
		errResp.SetResponseError(http.StatusForbidden, "AuthorizationFailed")
		return resp, errResp
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	d, ok := s.domains[customDomainKey{profileName, customDomainName}]
	if !ok {
		errResp.SetResponseError(http.StatusNotFound, "NotFound")
		return resp, errResp
	}
	resp.SetResponse(http.StatusOK, armcdn.AFDCustomDomainsClientGetResponse{AFDDomain: d}, nil)
	return resp, errResp
}

func (s *customDomainStore) beginCreate(_ context.Context, resourceGroupName string, profileName string, customDomainName string, parameters armcdn.AFDDomain, _ *armcdn.AFDCustomDomainsClientBeginCreateOptions) (resp azcorefake.PollerResponder[armcdn.AFDCustomDomainsClientCreateResponse], errResp azcorefake.ErrorResponder) {
	if resourceGroupName != DefaultResourceGroupName {
		errResp.SetResponseError(http.StatusForbidden, "AuthorizationFailed")
		return resp, errResp
	}
	created := parameters
	created.Name = ptr.To(customDomainName)
	created.ID = ptr.To(fmt.Sprintf(CustomDomainResourceIDFormat, DefaultSubscriptionID, DefaultResourceGroupName, profileName, customDomainName))
	if created.Properties == nil {
		created.Properties = &armcdn.AFDDomainProperties{}
	}
	// Force Approved so the reconciler's happy-path Programmed=True branch is
	// exercised without needing a multi-step DNS-validation dance.
	approved := armcdn.DomainValidationStateApproved
	created.Properties.DomainValidationState = &approved
	created.Properties.ValidationProperties = &armcdn.DomainValidationProperties{
		ValidationToken: ptr.To(FakeValidationToken),
	}

	s.mu.Lock()
	s.domains[customDomainKey{profileName, customDomainName}] = created
	s.mu.Unlock()

	resp.SetTerminalResponse(http.StatusOK, armcdn.AFDCustomDomainsClientCreateResponse{AFDDomain: created}, nil)
	return resp, errResp
}

func (s *customDomainStore) beginDelete(_ context.Context, resourceGroupName string, profileName string, customDomainName string, _ *armcdn.AFDCustomDomainsClientBeginDeleteOptions) (resp azcorefake.PollerResponder[armcdn.AFDCustomDomainsClientDeleteResponse], errResp azcorefake.ErrorResponder) {
	if resourceGroupName != DefaultResourceGroupName {
		errResp.SetResponseError(http.StatusForbidden, "AuthorizationFailed")
		return resp, errResp
	}
	s.mu.Lock()
	delete(s.domains, customDomainKey{profileName, customDomainName})
	s.mu.Unlock()

	resp.SetTerminalResponse(http.StatusOK, armcdn.AFDCustomDomainsClientDeleteResponse{}, nil)
	return resp, errResp
}
