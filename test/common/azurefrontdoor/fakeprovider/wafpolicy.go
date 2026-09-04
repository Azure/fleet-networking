/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package fakeprovider

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	azcorefake "github.com/Azure/azure-sdk-for-go/sdk/azcore/fake"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/frontdoor/armfrontdoor"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/frontdoor/armfrontdoor/fake"
	"k8s.io/utils/ptr"
)

// WAFPolicyResourceIDFormat is the ARM ID format for an AFD WAF policy. The
// classic Front Door WAF policy type lives under Microsoft.Network (NOT under
// Microsoft.Cdn), which is why it requires a separate SDK (armfrontdoor)
// from the AFD Standard/Premium profile/endpoint APIs (armcdn/v2).
const WAFPolicyResourceIDFormat = "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/frontdoorwebapplicationfirewallpolicies/%s"

// FormatWAFPolicyResourceID builds a fully qualified WAF policy ARM ID against
// the fake's DefaultSubscriptionID. Tests should use this so the string they
// put into FrontDoorProfileSpec.WAFPolicy.ResourceID exactly matches what the
// fake will resolve — the reconciler splits the string by ARM segments and any
// drift would cause a spurious NotFound.
func FormatWAFPolicyResourceID(resourceGroup, name string) string {
	return fmt.Sprintf(WAFPolicyResourceIDFormat, DefaultSubscriptionID, resourceGroup, name)
}

// wafPolicyKey scopes state by (rg, name) so a single fake can host policies
// across multiple resource groups if a test wants to exercise cross-RG refs.
// Cross-subscription refs are NOT modeled — the fake ignores the subscription
// segment (there is only DefaultSubscriptionID) and the reconciler currently
// rejects cross-sub WAF references at parse time.
type wafPolicyKey struct {
	rg, name string
}

type wafPolicyStore struct {
	mu       sync.Mutex
	policies map[wafPolicyKey]armfrontdoor.WebApplicationFirewallPolicy
}

// WAFPolicyFake wraps the fake PoliciesClient together with a seeding handle.
// Tests seed policies via SetPolicy BEFORE the reconciler runs (i.e. before
// the FrontDoorProfile CR is created) so that the reconciler's very first
// Get sees the intended state. The reconciler itself never mutates WAF
// policies — it only reads — so the store is effectively test-owned.
type WAFPolicyFake struct {
	Client *armfrontdoor.PoliciesClient
	store  *wafPolicyStore
}

// NewWAFPolicyFake returns a fresh WAFPolicyFake with an empty policy store.
// Callers typically construct this once per suite (BeforeSuite) and share the
// same instance across specs; specs seed/clear policies as they need.
func NewWAFPolicyFake() (*WAFPolicyFake, error) {
	store := &wafPolicyStore{policies: map[wafPolicyKey]armfrontdoor.WebApplicationFirewallPolicy{}}
	srv := fake.PoliciesServer{
		Get:                 store.get,
		BeginCreateOrUpdate: store.beginCreateOrUpdate,
		BeginDelete:         store.beginDelete,
	}
	factory, err := armfrontdoor.NewClientFactory(DefaultSubscriptionID, &azcorefake.TokenCredential{},
		&arm.ClientOptions{
			ClientOptions: azcore.ClientOptions{
				Transport: fake.NewPoliciesServerTransport(&srv),
			},
		})
	if err != nil {
		return nil, err
	}
	return &WAFPolicyFake{Client: factory.NewPoliciesClient(), store: store}, nil
}

// SetPolicy seeds a WAF policy in the fake store at the given (rg, name) with
// the requested mode (Prevention/Detection). Called from spec setup so the
// reconciler's Get resolves the policy AND observes the mode the spec wants
// to exercise (e.g. Detection to trigger WAFPolicyNotInPreventionMode).
// EnabledState is set to Enabled because a Disabled policy is not a scenario
// the reconciler currently distinguishes.
func (w *WAFPolicyFake) SetPolicy(rg, name string, mode armfrontdoor.PolicyMode) {
	w.store.mu.Lock()
	defer w.store.mu.Unlock()
	w.store.policies[wafPolicyKey{rg, name}] = armfrontdoor.WebApplicationFirewallPolicy{
		Name: ptr.To(name),
		ID:   ptr.To(FormatWAFPolicyResourceID(rg, name)),
		Properties: &armfrontdoor.WebApplicationFirewallPolicyProperties{
			PolicySettings: &armfrontdoor.PolicySettings{
				Mode:         ptr.To(mode),
				EnabledState: ptr.To(armfrontdoor.PolicyEnabledStateEnabled),
			},
		},
	}
}

// DeletePolicy removes a previously seeded policy. Useful for specs that want
// to exercise the WAFPolicyNotFound path without recreating the whole fake.
func (w *WAFPolicyFake) DeletePolicy(rg, name string) {
	w.store.mu.Lock()
	defer w.store.mu.Unlock()
	delete(w.store.policies, wafPolicyKey{rg, name})
}

func (s *wafPolicyStore) get(_ context.Context, resourceGroupName string, policyName string, _ *armfrontdoor.PoliciesClientGetOptions) (resp azcorefake.Responder[armfrontdoor.PoliciesClientGetResponse], errResp azcorefake.ErrorResponder) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Case-insensitive RG lookup: real Azure treats ARM resource-group names
	// case-insensitively, and the reconciler's parser preserves the case
	// from the user-supplied ResourceID. Tests that seed with lowercase and
	// reference with mixed case (or vice versa) would otherwise flake.
	for k, v := range s.policies {
		if strings.EqualFold(k.rg, resourceGroupName) && k.name == policyName {
			resp.SetResponse(http.StatusOK, armfrontdoor.PoliciesClientGetResponse{WebApplicationFirewallPolicy: v}, nil)
			return resp, errResp
		}
	}
	errResp.SetResponseError(http.StatusNotFound, "NotFound")
	return resp, errResp
}

// beginCreateOrUpdate exists so tests that want to exercise the WAF-write
// side (e.g. a future inline-WAF creation controller) do not need a second
// fake. The reconciler itself never calls this today; it only reads.
func (s *wafPolicyStore) beginCreateOrUpdate(_ context.Context, resourceGroupName string, policyName string, parameters armfrontdoor.WebApplicationFirewallPolicy, _ *armfrontdoor.PoliciesClientBeginCreateOrUpdateOptions) (resp azcorefake.PollerResponder[armfrontdoor.PoliciesClientCreateOrUpdateResponse], errResp azcorefake.ErrorResponder) {
	stored := parameters
	stored.Name = ptr.To(policyName)
	stored.ID = ptr.To(FormatWAFPolicyResourceID(resourceGroupName, policyName))
	s.mu.Lock()
	s.policies[wafPolicyKey{resourceGroupName, policyName}] = stored
	s.mu.Unlock()
	resp.SetTerminalResponse(http.StatusOK, armfrontdoor.PoliciesClientCreateOrUpdateResponse{WebApplicationFirewallPolicy: stored}, nil)
	return resp, errResp
}

func (s *wafPolicyStore) beginDelete(_ context.Context, resourceGroupName string, policyName string, _ *armfrontdoor.PoliciesClientBeginDeleteOptions) (resp azcorefake.PollerResponder[armfrontdoor.PoliciesClientDeleteResponse], errResp azcorefake.ErrorResponder) {
	s.mu.Lock()
	delete(s.policies, wafPolicyKey{resourceGroupName, policyName})
	s.mu.Unlock()
	resp.SetTerminalResponse(http.StatusOK, armfrontdoor.PoliciesClientDeleteResponse{}, nil)
	return resp, errResp
}
