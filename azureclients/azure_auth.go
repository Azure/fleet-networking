// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package azureclients

import (
	"context"
	"fmt"

	"github.com/Azure/go-autorest/autorest/azure"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/cloud-provider-azure/pkg/auth"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

// AzureConfig defines the Azure config options.
type AzureConfig struct {
	auth.AzureAuthConfig

	GlobalVIPLocation               string `json:"globalVIPLocation,omitempty" yaml:"globalVIPLocation,omitempty"`
	GlobalLoadBalancerName          string `json:"globalLoadBalancerName,omitempty" yaml:"globalLoadBalancerName,omitempty"`
	GlobalLoadBalancerResourceGroup string `json:"globalLoadBalancerResourceGroup,omitempty" yaml:"globalLoadBalancerResourceGroup,omitempty"`
}

// GetAzureConfigFromSecret fetches Azure cloud config from given secret.
func GetAzureConfigFromSecret(kubeClient client.Client, namespace, name string) (*AzureConfig, *azure.Environment, error) {
	var secret corev1.Secret
	if err := kubeClient.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: name}, &secret); err != nil {
		return nil, nil, fmt.Errorf("failed to get secret %s: %w", name, err)
	}
	cloudConfigData, ok := secret.Data["cloud-config"]
	if !ok {
		return nil, nil, fmt.Errorf("cloud-config is not set in the secret (%s)", name)
	}

	var config AzureConfig
	var env azure.Environment
	err := yaml.Unmarshal(cloudConfigData, &config)
	if err != nil {
		return nil, nil, err
	}

	if config.Cloud == "" {
		env = azure.PublicCloud
	} else {
		env, err = azure.EnvironmentFromName(config.Cloud)
		if err != nil {
			return nil, nil, err
		}
	}
	return &config, &env, nil
}
