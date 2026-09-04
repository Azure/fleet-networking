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

// SecurityPolicyResourceIDFormat is the ARM ID format for an AFD SecurityPolicy
// child resource. The reconciler does not read this back today (it only
// asserts existence), but it is populated on the stored fake so tests that
// want to assert the fully qualified ID have a stable format to compare
// against.
const SecurityPolicyResourceIDFormat = "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Cdn/profiles/%s/securityPolicies/%s"

// securityPolicyKey scopes state by (profile, name) so multiple concurrent
// profiles in the same fake do not collide. Cross-RG collisions cannot happen
// because the AFD SecurityPolicy resource is always a child of a profile that
// itself lives in one RG.
type securityPolicyKey struct {
	profile, name string
}

type securityPolicyStore struct {
	mu       sync.Mutex
	policies map[securityPolicyKey]armcdn.SecurityPolicy
}

// SecurityPolicyFake wraps the fake SecurityPoliciesClient together with an
// inspection handle so specs can assert on what the reconciler stored.
// Unlike the WAF policy fake, this store is written entirely by the
// reconciler (via BeginCreate/BeginDelete) — specs only READ from it via
// GetStored. Seeding is not exposed because there is no scenario where the
// reconciler should observe a SecurityPolicy it did not itself write.
type SecurityPolicyFake struct {
	Client *armcdn.SecurityPoliciesClient
	store  *securityPolicyStore
}

// NewSecurityPolicyFake returns a fresh SecurityPolicyFake with an empty
// store. Specs typically construct one per suite (BeforeSuite) and reset by
// deleting the FrontDoorProfile CR in AfterEach — the reconciler cleans up
// via its finalizer, which in turn calls Delete on this fake, so state does
// not leak between specs.
func NewSecurityPolicyFake() (*SecurityPolicyFake, error) {
	store := &securityPolicyStore{policies: map[securityPolicyKey]armcdn.SecurityPolicy{}}
	srv := fake.SecurityPoliciesServer{
		Get:         store.get,
		BeginCreate: store.beginCreate,
		BeginDelete: store.beginDelete,
	}
	factory, err := armcdn.NewClientFactory(DefaultSubscriptionID, &azcorefake.TokenCredential{},
		&arm.ClientOptions{
			ClientOptions: azcore.ClientOptions{
				Transport: fake.NewSecurityPoliciesServerTransport(&srv),
			},
		})
	if err != nil {
		return nil, err
	}
	return &SecurityPolicyFake{Client: factory.NewSecurityPoliciesClient(), store: store}, nil
}

// GetStored returns a snapshot of what the reconciler last wrote for the
// (profile, name) pair. Second return is false if nothing was ever written
// (or it was deleted). Intended for spec assertions such as "WafPolicy.ID
// matches the expected ARM ID" or "Associations contains the endpoint".
func (s *SecurityPolicyFake) GetStored(profile, name string) (armcdn.SecurityPolicy, bool) {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	p, ok := s.store.policies[securityPolicyKey{profile, name}]
	return p, ok
}

func (s *securityPolicyStore) get(_ context.Context, resourceGroupName string, profileName string, securityPolicyName string, _ *armcdn.SecurityPoliciesClientGetOptions) (resp azcorefake.Responder[armcdn.SecurityPoliciesClientGetResponse], errResp azcorefake.ErrorResponder) {
	if resourceGroupName != DefaultResourceGroupName {
		errResp.SetResponseError(http.StatusForbidden, "AuthorizationFailed")
		return resp, errResp
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.policies[securityPolicyKey{profileName, securityPolicyName}]
	if !ok {
		errResp.SetResponseError(http.StatusNotFound, "NotFound")
		return resp, errResp
	}
	resp.SetResponse(http.StatusOK, armcdn.SecurityPoliciesClientGetResponse{SecurityPolicy: p}, nil)
	return resp, errResp
}

func (s *securityPolicyStore) beginCreate(_ context.Context, resourceGroupName string, profileName string, securityPolicyName string, securityPolicy armcdn.SecurityPolicy, _ *armcdn.SecurityPoliciesClientBeginCreateOptions) (resp azcorefake.PollerResponder[armcdn.SecurityPoliciesClientCreateResponse], errResp azcorefake.ErrorResponder) {
	if resourceGroupName != DefaultResourceGroupName {
		errResp.SetResponseError(http.StatusForbidden, "AuthorizationFailed")
		return resp, errResp
	}
	created := securityPolicy
	created.Name = ptr.To(securityPolicyName)
	created.ID = ptr.To(fmt.Sprintf(SecurityPolicyResourceIDFormat, DefaultSubscriptionID, DefaultResourceGroupName, profileName, securityPolicyName))

	s.mu.Lock()
	// BeginCreate is treated as upsert here — matches SDK behavior where a
	// repeated create with the same name overwrites the existing resource
	// (AFD SecurityPolicy has no separate Update endpoint).
	s.policies[securityPolicyKey{profileName, securityPolicyName}] = created
	s.mu.Unlock()

	resp.SetTerminalResponse(http.StatusOK, armcdn.SecurityPoliciesClientCreateResponse{SecurityPolicy: created}, nil)
	return resp, errResp
}

func (s *securityPolicyStore) beginDelete(_ context.Context, resourceGroupName string, profileName string, securityPolicyName string, _ *armcdn.SecurityPoliciesClientBeginDeleteOptions) (resp azcorefake.PollerResponder[armcdn.SecurityPoliciesClientDeleteResponse], errResp azcorefake.ErrorResponder) {
	if resourceGroupName != DefaultResourceGroupName {
		errResp.SetResponseError(http.StatusForbidden, "AuthorizationFailed")
		return resp, errResp
	}
	s.mu.Lock()
	_, ok := s.policies[securityPolicyKey{profileName, securityPolicyName}]
	delete(s.policies, securityPolicyKey{profileName, securityPolicyName})
	s.mu.Unlock()

	if !ok {
		// Real Azure returns 404 for delete of a non-existent SecurityPolicy;
		// the reconciler's drift-cleanup path treats 404 as "already gone"
		// so this is the correct fake shape.
		errResp.SetResponseError(http.StatusNotFound, "NotFound")
		return resp, errResp
	}
	resp.SetTerminalResponse(http.StatusOK, armcdn.SecurityPoliciesClientDeleteResponse{}, nil)
	return resp, errResp
}
