/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package cloudconfig defines azure cloud provider configuration.
package cloudconfig

import (
	"fmt"
	"io"
	"os"
	"strings"

	"sigs.k8s.io/cloud-provider-azure/pkg/azclient"
	"sigs.k8s.io/yaml"
)

// CloudConfig defines the necessary configurations to access Azure resources.
type CloudConfig struct {
	azclient.ARMClientConfig `json:",inline" mapstructure:",squash"`
	azclient.AzureAuthConfig `json:",inline" mapstructure:",squash"`
	// subscription ID
	SubscriptionID string `json:"subscriptionID,omitempty" mapstructure:"subscriptionID,omitempty"`
	// azure resource location
	Location string `json:"location,omitempty" mapstructure:"location,omitempty"`
	// default resource group where the azure resources are deployed
	ResourceGroup string `json:"resourceGroup,omitempty" mapstructure:"resourceGroup,omitempty"`
}

// NewCloudConfigFromFile loads cloud config from a file given the file path.
func NewCloudConfigFromFile(filePath string) (*CloudConfig, error) {
	if filePath == "" {
		return nil, fmt.Errorf("failed to load cloud config: file path is empty")
	}

	var config CloudConfig
	configReader, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open cloud config file: %w, file path: %s", err, filePath)
	}
	defer configReader.Close()

	contents, err := io.ReadAll(configReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read cloud config file: %w, file path: %s", err, filePath)
	}

	if err := yaml.Unmarshal(contents, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cloud config: %w, file path: %s", err, filePath)
	}

	config.trimSpace()
	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("failed to validate cloud config: %w, file contents: `%s`", err, string(contents))
	}

	return &config, nil
}

// SetUserAgent sets the user agent string to access Azure resources.
func (cfg *CloudConfig) SetUserAgent(userAgent string) {
	cfg.UserAgent = userAgent
}

func (cfg *CloudConfig) validate() error {
	if cfg.Cloud == "" {
		return fmt.Errorf("cloud is empty")
	}

	if cfg.Location == "" {
		return fmt.Errorf("location is empty")
	}

	if cfg.SubscriptionID == "" {
		return fmt.Errorf("subscription ID is empty")
	}

	if cfg.ResourceGroup == "" {
		return fmt.Errorf("resource group is empty")
	}

	if !cfg.UseManagedIdentityExtension {
		if cfg.UserAssignedIdentityID != "" {
			return fmt.Errorf("useManagedIdentityExtension needs to be true when userAssignedIdentityID is provided")
		}
		if cfg.AADClientID == "" || cfg.AADClientSecret == "" {
			return fmt.Errorf("AAD client ID or AAD client secret is empty")
		}
	}

	return nil
}

func (cfg *CloudConfig) trimSpace() {
	cfg.Cloud = strings.TrimSpace(cfg.Cloud)
	cfg.TenantID = strings.TrimSpace(cfg.TenantID)
	cfg.UserAgent = strings.TrimSpace(cfg.UserAgent)
	cfg.SubscriptionID = strings.TrimSpace(cfg.SubscriptionID)
	cfg.Location = strings.TrimSpace(cfg.Location)
	cfg.ResourceGroup = strings.TrimSpace(cfg.ResourceGroup)
	cfg.UserAssignedIdentityID = strings.TrimSpace(cfg.UserAssignedIdentityID)
	cfg.AADClientID = strings.TrimSpace(cfg.AADClientID)
	cfg.AADClientSecret = strings.TrimSpace(cfg.AADClientSecret)
}
