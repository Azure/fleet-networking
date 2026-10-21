/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package azurefrontdoor

import (
	"strings"
	"testing"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		wantErr     bool
		wantMissing []string
	}{
		{
			name: "all fields set",
			config: Config{
				TenantID:           "tenant",
				ClientID:           "client",
				FederatedTokenFile: "/var/run/secrets/tokens/azure",
				SubscriptionID:     "sub",
			},
			wantErr: false,
		},
		{
			name:        "all fields empty",
			config:      Config{},
			wantErr:     true,
			wantMissing: []string{EnvAzureTenantID, EnvAzureClientID, EnvAzureFederatedTokenFile, EnvAzureSubscriptionID},
		},
		{
			name: "missing subscription",
			config: Config{
				TenantID:           "tenant",
				ClientID:           "client",
				FederatedTokenFile: "/tok",
			},
			wantErr:     true,
			wantMissing: []string{EnvAzureSubscriptionID},
		},
		{
			name: "missing token file",
			config: Config{
				TenantID:       "tenant",
				ClientID:       "client",
				SubscriptionID: "sub",
			},
			wantErr:     true,
			wantMissing: []string{EnvAzureFederatedTokenFile},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.config.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() error = %v, wantErr = %v", err, tc.wantErr)
			}
			if err == nil {
				return
			}
			for _, m := range tc.wantMissing {
				if !strings.Contains(err.Error(), m) {
					t.Errorf("Validate() error %q missing expected variable %q", err.Error(), m)
				}
			}
		})
	}
}

func TestNewCredentialNilConfig(t *testing.T) {
	if _, err := NewCredential(nil); err == nil {
		t.Fatal("expected error for nil config, got nil")
	}
}

func TestNewClientsValidation(t *testing.T) {
	if _, err := NewClients(nil, "sub", nil); err == nil {
		t.Error("expected error for nil credential, got nil")
	}
}
