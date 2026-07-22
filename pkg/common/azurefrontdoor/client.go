/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package azurefrontdoor provides Azure client construction for the Front Door
// (AFD) controllers.
//
// Authentication (breadcrumb D6):
//   - Uses Azure AD Workload Identity (federated token) via
//     azidentity.NewWorkloadIdentityCredential. This is deliberately chosen over
//     managed-identity-with-mounted-azure.json (the ATM controller's approach)
//     because Workload Identity is the AKS-supported path forward for new
//     controllers, integrates cleanly with the projected ServiceAccount token
//     mounted by the azure-workload-identity mutating webhook, and does not
//     require the controller pod to read a cloud provider config file.
//   - Reads AZURE_TENANT_ID, AZURE_CLIENT_ID, AZURE_FEDERATED_TOKEN_FILE,
//     AZURE_SUBSCRIPTION_ID from the environment. The first three are set
//     automatically by the workload-identity webhook when the pod's
//     ServiceAccount is annotated appropriately; AZURE_SUBSCRIPTION_ID is
//     supplied via chart values.
//
// Scope (breadcrumb D7):
//   - Intentionally does not share code with pkg/common/azuretrafficmanager.
//     The two controllers have distinct SDKs (armcdn vs. armtrafficmanager),
//     distinct identities per Proposal 001 §7, and distinct release timelines;
//     a shared abstraction would couple them without simplifying anything.
//
// Identity-sharing note (POC bridge, Proposal 001 §7 gap):
//   - This package's Config maps 1:1 to the AFD-scoped Workload-Identity
//     federated subject. Proposal 001 §7 requires that this subject be
//     DISTINCT from the ATM controller's subject so ATM-only tenants do not
//     inherit AFD write permissions. Because a Kubernetes pod projects
//     exactly one WI federated token, achieving that isolation requires the
//     AFD controllers to run in a separate pod. The current POC hosts them
//     inside cmd/hub-net-controller-manager under --enable-frontdoor-feature
//     as a temporary bridge, which shares one subject across both controllers.
//     Migrating to a sibling binary (cmd/hub-afd-controller-manager) + sibling
//     chart (charts/hub-afd-controller-manager) is a hard GA prerequisite;
//     see docs/first-party/003-pre-implementation-checklist.md §2.4.
package azurefrontdoor

import (
	"errors"
	"fmt"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	azcloud "github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"

	// armcdn is the Azure SDK for Front Door Standard/Premium. Pinned to v2
	// (2024-02-01 API) so tests can use the SDK-provided armcdn/v2/fake
	// package — v1.x does not ship a fake subpackage. The imported name
	// stays `armcdn` (no explicit alias needed) so call sites are unchanged
	// across the version bump.
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"

	// armfrontdoor is the classic Front Door SDK; we do NOT use its classic
	// FrontDoors/FrontendEndpoints APIs (superseded by armcdn's AFD types),
	// but its PoliciesClient is the ONLY SDK-supported way to read/write
	// Microsoft.Network/frontdoorwebapplicationfirewallpolicies — the WAF
	// policy type that FrontDoorProfile.spec.wafPolicy references. Pinned to
	// v1.4.0 because it ships an armfrontdoor/fake package (PoliciesServer)
	// mirroring the armcdn/v2/fake pattern; earlier v1.x releases don't.
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/frontdoor/armfrontdoor"
)

// Environment variable names read by LoadConfigFromEnv. AZURE_CLIENT_ID,
// AZURE_TENANT_ID, and AZURE_FEDERATED_TOKEN_FILE are populated automatically
// on AKS clusters with the azure-workload-identity mutating webhook enabled.
const (
	EnvAzureClientID           = "AZURE_CLIENT_ID"
	EnvAzureTenantID           = "AZURE_TENANT_ID"
	EnvAzureFederatedTokenFile = "AZURE_FEDERATED_TOKEN_FILE"

	// EnvAzureSubscriptionID identifies the subscription that owns the AFD
	// profiles this controller manages. Not part of Workload Identity itself;
	// supplied via chart values in production.
	EnvAzureSubscriptionID = "AZURE_SUBSCRIPTION_ID"
)

// Config holds the resolved identity + target parameters for the AFD clients.
type Config struct {
	TenantID           string
	ClientID           string
	FederatedTokenFile string
	SubscriptionID     string
	// Cloud selects the Azure cloud (AzurePublic, AzureGovernment, AzureChina).
	// Zero value = AzurePublic.
	Cloud azcloud.Configuration
}

// LoadConfigFromEnv reads Config values from the standard Workload Identity
// environment variables plus AZURE_SUBSCRIPTION_ID. It returns an error listing
// every missing variable so misconfiguration surfaces in a single log line.
func LoadConfigFromEnv() (*Config, error) {
	c := &Config{
		TenantID:           os.Getenv(EnvAzureTenantID),
		ClientID:           os.Getenv(EnvAzureClientID),
		FederatedTokenFile: os.Getenv(EnvAzureFederatedTokenFile),
		SubscriptionID:     os.Getenv(EnvAzureSubscriptionID),
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// Validate returns an aggregated error identifying every missing required field.
func (c *Config) Validate() error {
	var missing []string
	if c.TenantID == "" {
		missing = append(missing, EnvAzureTenantID)
	}
	if c.ClientID == "" {
		missing = append(missing, EnvAzureClientID)
	}
	if c.FederatedTokenFile == "" {
		missing = append(missing, EnvAzureFederatedTokenFile)
	}
	if c.SubscriptionID == "" {
		missing = append(missing, EnvAzureSubscriptionID)
	}
	if len(missing) > 0 {
		return fmt.Errorf("azurefrontdoor: missing required environment variables: %v", missing)
	}
	return nil
}

// NewCredential constructs a token credential using Azure AD Workload Identity.
func NewCredential(c *Config) (azcore.TokenCredential, error) {
	if c == nil {
		return nil, errors.New("azurefrontdoor: Config is nil")
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	cred, err := azidentity.NewWorkloadIdentityCredential(&azidentity.WorkloadIdentityCredentialOptions{
		ClientID:      c.ClientID,
		TenantID:      c.TenantID,
		TokenFilePath: c.FederatedTokenFile,
		ClientOptions: azcore.ClientOptions{Cloud: c.Cloud},
	})
	if err != nil {
		return nil, fmt.Errorf("azurefrontdoor: create workload identity credential: %w", err)
	}
	return cred, nil
}

// Clients bundles the AFD sub-clients used by the Phase 2 POC controllers.
// Kept small on purpose; additional clients (routes, origins, secrets) can be
// added as later phases need them.
//
// The bundle intentionally mixes two SDK modules:
//   - armcdn/v2: Microsoft.Cdn/profiles/* (the AFD profile itself, endpoints,
//     custom domains, security-policy attaches).
//   - armfrontdoor: Microsoft.Network/frontdoorwebapplicationfirewallpolicies
//     (the WAF policy resource referenced by
//     FrontDoorProfile.spec.wafPolicy.resourceID). Kept as a distinct client
//     because it lives under a different ARM resource provider and the
//     armcdn SDK deliberately does not expose it (WAF policies pre-date the
//     AFD Standard/Premium API surface).
type Clients struct {
	Profiles         *armcdn.ProfilesClient
	AFDEndpoints     *armcdn.AFDEndpointsClient
	CustomDomains    *armcdn.AFDCustomDomainsClient
	SecurityPolicies *armcdn.SecurityPoliciesClient
	// OriginGroups + Origins are the FrontDoorBackend reconciler's write
	// surface: it programs one OriginGroup per FrontDoorBackend and one
	// Origin per qualifying InternalServiceExport (see
	// docs/first-party/002-afd-implementation-plan.md §6). Both live under
	// Microsoft.Cdn/profiles/<profile>/originGroups[/<group>/origins/<name>]
	// so they share the armcdn factory with the profile itself.
	OriginGroups *armcdn.AFDOriginGroupsClient
	Origins      *armcdn.AFDOriginsClient
	// WAFPolicies reads and writes classic AFD WAF policies. Read is used
	// unconditionally by the profile reconciler to resolve
	// spec.wafPolicy.resourceID; write is currently unused by the
	// controllers (all first-party services reference a centrally-managed
	// policy — see the FrontDoorWAFPolicyRef type doc) but is exposed here
	// so a future inline-WAF creation path can use it without another
	// change to this bundle.
	WAFPolicies *armfrontdoor.PoliciesClient
}

// NewClients builds the AFD sub-clients using the given credential and target
// subscription. armOpts may be nil; callers wanting retry/telemetry tuning
// should pass a shared *arm.ClientOptions.
//
// Both underlying SDKs (armcdn/v2, armfrontdoor) share the same credential
// and arm.ClientOptions so retry/telemetry policy is applied uniformly across
// AFD and WAF calls.
func NewClients(cred azcore.TokenCredential, subscriptionID string, armOpts *arm.ClientOptions) (*Clients, error) {
	if cred == nil {
		return nil, errors.New("azurefrontdoor: credential is nil")
	}
	if subscriptionID == "" {
		return nil, errors.New("azurefrontdoor: subscriptionID is empty")
	}
	cdnFactory, err := armcdn.NewClientFactory(subscriptionID, cred, armOpts)
	if err != nil {
		return nil, fmt.Errorf("azurefrontdoor: create armcdn client factory: %w", err)
	}
	fdFactory, err := armfrontdoor.NewClientFactory(subscriptionID, cred, armOpts)
	if err != nil {
		return nil, fmt.Errorf("azurefrontdoor: create armfrontdoor client factory: %w", err)
	}
	return &Clients{
		Profiles:         cdnFactory.NewProfilesClient(),
		AFDEndpoints:     cdnFactory.NewAFDEndpointsClient(),
		CustomDomains:    cdnFactory.NewAFDCustomDomainsClient(),
		SecurityPolicies: cdnFactory.NewSecurityPoliciesClient(),
		OriginGroups:     cdnFactory.NewAFDOriginGroupsClient(),
		Origins:          cdnFactory.NewAFDOriginsClient(),
		WAFPolicies:      fdFactory.NewPoliciesClient(),
	}, nil
}

// DefaultARMClientOptions returns arm.ClientOptions suitable for controller
// use. Kept as a seam for retry/logging tuning that later phases are expected
// to add without touching call sites.
func DefaultARMClientOptions() *arm.ClientOptions {
	return &arm.ClientOptions{
		ClientOptions: azcore.ClientOptions{
			Retry: policy.RetryOptions{
				MaxRetries: 3,
			},
		},
	}
}
