/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package cloudconfig defines azure cloud provider configuration.
package cloudconfig

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"sigs.k8s.io/cloud-provider-azure/pkg/azclient"
)

func TestNewCloudConfigFromFile(t *testing.T) {
	tests := map[string]struct {
		filePath       string
		expectErr      bool
		expectedConfig *CloudConfig
	}{
		"file path is empty": {
			filePath:  "",
			expectErr: true,
		},
		"failed to open file": {
			filePath:  "./test/not_exist.json",
			expectErr: true,
		},
		"failed to unmarshal file": {
			filePath:  "./test/azure_config_nojson.txt",
			expectErr: true,
		},
		"failed to validate config": {
			filePath:  "./test/azure_config_invalid.json",
			expectErr: true,
		},
		"succeeded to load config": {
			filePath: "./test/azure_config_valid.json",
			expectedConfig: &CloudConfig{
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud:     "AzurePublicCloud",
					TenantID:  "00000000-0000-0000-0000-000000000000",
					UserAgent: "",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: true,
					UserAssignedIdentityID:      "11111111-1111-1111-1111-111111111111",
					AADClientID:                 "",
					AADClientSecret:             "",
				},
				Location:       "eastus",
				SubscriptionID: "00000000-0000-0000-0000-000000000000",
				ResourceGroup:  "test-rg",
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			config, err := NewCloudConfigFromFile(test.filePath)
			if got := err != nil; got != test.expectErr {
				t.Errorf("failed to run NewCloudConfigFromFile(%s): got %v, want %v", test.filePath, got, test.expectErr)
			}
			if !cmp.Equal(config, test.expectedConfig) {
				t.Errorf("NewCloudConfigFromFile(%s) = %v, want %v", test.filePath, config, test.expectedConfig)
			}
		})
	}
}

func TestSetUserAgent(t *testing.T) {
	config := &CloudConfig{}
	config.SetUserAgent("test")
	if config.UserAgent != "test" {
		t.Errorf("SetUserAgent(test) = %s, want test", config.UserAgent)
	}
}

func TestTrimSpace(t *testing.T) {
	t.Run("test spaces are trimmed", func(t *testing.T) {
		config := CloudConfig{
			ARMClientConfig: azclient.ARMClientConfig{
				Cloud:     "  test  \n",
				UserAgent: "  test  \n",
				TenantID:  "  test  \t \n",
			},
			AzureAuthConfig: azclient.AzureAuthConfig{
				UserAssignedIdentityID:      "  test  \n",
				UseManagedIdentityExtension: true,
				AADClientID:                 "\n  test  \n",
				AADClientSecret:             "  test  \n",
			},
			Location:       "  test  \n",
			SubscriptionID: "  test  \n",
			ResourceGroup:  "\r\n  test  \n",
		}

		expected := CloudConfig{
			ARMClientConfig: azclient.ARMClientConfig{
				Cloud:     "test",
				TenantID:  "test",
				UserAgent: "test",
			},
			Location:       "test",
			SubscriptionID: "test",
			ResourceGroup:  "test",
			AzureAuthConfig: azclient.AzureAuthConfig{
				UseManagedIdentityExtension: true,
				UserAssignedIdentityID:      "test",
				AADClientID:                 "test",
				AADClientSecret:             "test",
			},
		}
		config.trimSpace()
		if !cmp.Equal(config, expected) {
			t.Errorf("failed to test TrimSpace: expect config fields are trimmed, got: %v, want: %v", config, expected)
		}
	})
}

func TestDefaultAndValidate(t *testing.T) {
	tests := map[string]struct {
		config     *CloudConfig
		expectPass bool
	}{
		"Cloud empty": {
			config: &CloudConfig{
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: true,
					UserAssignedIdentityID:      "a",
				},
				Location:       "l",
				SubscriptionID: "s",
				ResourceGroup:  "v",
			},
			expectPass: false,
		},
		"Location empty": {
			config: &CloudConfig{
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "c",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: true,
					UserAssignedIdentityID:      "a",
				},
				Location:       "",
				SubscriptionID: "s",
				ResourceGroup:  "v",
			},
			expectPass: false,
		},
		"SubscriptionID empty": {
			config: &CloudConfig{
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "c",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: true,
					UserAssignedIdentityID:      "a",
				},
				Location:       "l",
				SubscriptionID: "",
				ResourceGroup:  "v",
			},
			expectPass: false,
		},
		"ResourceGroup empty": {
			config: &CloudConfig{
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "c",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: true,
					UserAssignedIdentityID:      "a",
				},
				Location:       "l",
				SubscriptionID: "s",
				ResourceGroup:  "",
			},
			expectPass: false,
		},
		"UserAssignedIdentityID not empty when UseManagedIdentityExtension is false": {
			config: &CloudConfig{
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "c",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: false,
					UserAssignedIdentityID:      "aaaa",
				},
				Location:       "l",
				SubscriptionID: "s",
				ResourceGroup:  "v",
			},
			expectPass: false,
		},
		"AADClientID empty": {
			config: &CloudConfig{
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "c",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: false,
					UserAssignedIdentityID:      "",
					AADClientID:                 "",
					AADClientSecret:             "2",
				},
				Location:       "l",
				SubscriptionID: "s",
				ResourceGroup:  "v",
			},
			expectPass: false,
		},
		"AADClientSecret empty": {
			config: &CloudConfig{
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "c",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: false,
					UserAssignedIdentityID:      "",
					AADClientID:                 "1",
					AADClientSecret:             "",
				},
				Location:       "l",
				SubscriptionID: "s",
				ResourceGroup:  "v",
			},
			expectPass: false,
		},
		"has all required properties with secret and default values": {
			config: &CloudConfig{
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "c",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: false,
					UserAssignedIdentityID:      "",
					AADClientID:                 "1",
					AADClientSecret:             "2",
				},
				Location:       "l",
				SubscriptionID: "s",
				ResourceGroup:  "v",
			},
			expectPass: true,
		},
		"has all required properties with msi and specified values": {
			config: &CloudConfig{
				ARMClientConfig: azclient.ARMClientConfig{
					Cloud: "c",
				},
				AzureAuthConfig: azclient.AzureAuthConfig{
					UseManagedIdentityExtension: true,
					UserAssignedIdentityID:      "u",
				},
				Location:       "l",
				SubscriptionID: "s",
				ResourceGroup:  "v",
			},
			expectPass: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := test.config.validate()
			if got := err == nil; got != test.expectPass {
				t.Errorf("failed to test whether validate returns error: got %v, want %v", got, test.expectPass)
			}
		})
	}
}
