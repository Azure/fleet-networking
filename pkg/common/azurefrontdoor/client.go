/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package azurefrontdoor provides Azure client construction for the Front Door
// (AFD) controllers. Per breadcrumb D6, authentication uses Azure AD Workload
// Identity (federated token) via azidentity.NewWorkloadIdentityCredential.
// Per breadcrumb D7, this package is intentionally scoped to AFD only and does
// not share code with pkg/common/azuretrafficmanager.
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
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn"
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
type Clients struct {
	Profiles      *armcdn.ProfilesClient
	AFDEndpoints  *armcdn.AFDEndpointsClient
	CustomDomains *armcdn.AFDCustomDomainsClient
}

// NewClients builds the AFD sub-clients using the given credential and target
// subscription. armOpts may be nil; callers wanting retry/telemetry tuning
// should pass a shared *arm.ClientOptions.
func NewClients(cred azcore.TokenCredential, subscriptionID string, armOpts *arm.ClientOptions) (*Clients, error) {
	if cred == nil {
		return nil, errors.New("azurefrontdoor: credential is nil")
	}
	if subscriptionID == "" {
		return nil, errors.New("azurefrontdoor: subscriptionID is empty")
	}
	factory, err := armcdn.NewClientFactory(subscriptionID, cred, armOpts)
	if err != nil {
		return nil, fmt.Errorf("azurefrontdoor: create armcdn client factory: %w", err)
	}
	return &Clients{
		Profiles:      factory.NewProfilesClient(),
		AFDEndpoints:  factory.NewAFDEndpointsClient(),
		CustomDomains: factory.NewAFDCustomDomainsClient(),
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
