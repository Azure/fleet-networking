/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// package hubconfig provides common functionalities for hub configuration.
package hubconfig

import (
	"encoding/base64"
	"os"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"

	"go.goms.io/fleet-networking/pkg/env"
)

const (
	// Environment variable keys for hub config
	hubServerURLEnvKey    = "HUB_SERVER_URL"
	tokenConfigPathEnvKey = "CONFIG_PATH" //nolint:gosec
	hubCAEnvKey           = "HUB_CERTIFICATE_AUTHORITY"
)

// PrepareHubConfig return the config holding attributes for a Kubernetes client to request hub cluster.
// Called must make sure all required environment variables are well set.
func PrepareHubConfig(tlsClientInsecure bool) (*rest.Config, error) {
	hubURL, err := env.EnvOrError(hubServerURLEnvKey)
	if err != nil {
		klog.ErrorS(err, "Hub cluster endpoint URL cannot be empty")
		return nil, err
	}

	tokenFilePath, err := env.EnvOrError(tokenConfigPathEnvKey)
	if err != nil {
		klog.ErrorS(err, "Hub token file path cannot be empty")
		return nil, err
	}

	// Retry on obtaining token file as it is created asynchronously by token-refesh container
	if err := retry.OnError(retry.DefaultRetry, func(e error) bool {
		return true
	}, func() error {
		// Stat returns file info. It will return an error if there is no file.
		_, err := os.Stat(tokenFilePath)
		return err
	}); err != nil {
		klog.ErrorS(err, "Cannot retrieve token file from the path %s", tokenFilePath)
		return nil, err
	}
	var hubConfig *rest.Config
	if tlsClientInsecure {
		hubConfig = &rest.Config{
			BearerTokenFile: tokenFilePath,
			Host:            hubURL,
			TLSClientConfig: rest.TLSClientConfig{
				Insecure: tlsClientInsecure,
			},
		}
	} else {
		hubCA, err := env.EnvOrError(hubCAEnvKey)
		if err != nil {
			klog.ErrorS(err, "Hub certificate authority cannot be empty")
			return nil, err
		}
		decodedClusterCaCertificate, err := base64.StdEncoding.DecodeString(hubCA)
		if err != nil {
			klog.ErrorS(err, "Cannot decode hub cluster certificate authority data")
			return nil, err
		}
		hubConfig = &rest.Config{
			BearerTokenFile: tokenFilePath,
			Host:            hubURL,
			TLSClientConfig: rest.TLSClientConfig{
				Insecure: tlsClientInsecure,
				CAData:   decodedClusterCaCertificate,
			},
		}
	}

	return hubConfig, nil
}
