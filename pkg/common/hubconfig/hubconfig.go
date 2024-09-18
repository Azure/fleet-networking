/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// package hubconfig provides common functionalities for hub configuration.
package hubconfig

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/textproto"
	"os"
	"strings"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"

	"go.goms.io/fleet-networking/pkg/common/env"
	"go.goms.io/fleet-networking/pkg/common/httpclient"
)

const (
	// Environment variable keys for hub config
	hubServerURLEnvKey    = "HUB_SERVER_URL"
	tokenConfigPathEnvKey = "CONFIG_PATH" //nolint:gosec
	hubCAEnvKey           = "HUB_CERTIFICATE_AUTHORITY"
	hubKubeHeaderEnvKey   = "HUB_KUBE_HEADER"

	// Naming pattern of member cluster namespace in hub cluster, should be the same as envValue as defined in
	// https://github.com/Azure/fleet/blob/main/pkg/utils/common.go
	HubNamespaceNameFormat = "fleet-member-%s"
)

// PrepareHubConfig return the config holding attributes for a Kubernetes client to request hub cluster.
// Called must make sure all required environment variables are well set.
func PrepareHubConfig(tlsClientInsecure bool) (*rest.Config, error) {
	hubURL, err := env.Lookup(hubServerURLEnvKey)
	if err != nil {
		klog.ErrorS(err, "Hub cluster endpoint URL cannot be empty")
		return nil, err
	}

	tokenFilePath, err := env.Lookup(tokenConfigPathEnvKey)
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
		var caData []byte
		hubCA, err := env.Lookup(hubCAEnvKey)
		if err == nil {
			caData, err = base64.StdEncoding.DecodeString(hubCA)
			if err != nil {
				klog.ErrorS(err, "Cannot decode hub cluster certificate authority data")
				return nil, err
			}
		}
		hubConfig = &rest.Config{
			BearerTokenFile: tokenFilePath,
			Host:            hubURL,
			TLSClientConfig: rest.TLSClientConfig{
				Insecure: tlsClientInsecure,
				CAData:   caData,
			},
		}
	}

	// Sometime the hub cluster need additional http header for authentication or authorization.
	// the "HUB_KUBE_HEADER" to allow sending custom header to hub's API Server for authentication and authorization.
	if header, err := env.Lookup(hubKubeHeaderEnvKey); err == nil {
		r := textproto.NewReader(bufio.NewReader(strings.NewReader(header)))
		h, err := r.ReadMIMEHeader()
		if err != nil && !errors.Is(err, io.EOF) {
			klog.ErrorS(err, "failed to parse HUB_KUBE_HEADER %q", header)
			return nil, err
		}
		hubConfig.WrapTransport = func(rt http.RoundTripper) http.RoundTripper {
			return httpclient.NewCustomHeadersRoundTripper(http.Header(h), rt)
		}
	}
	return hubConfig, nil
}

// FetchMemberClusterNamespace gets the assigned namespace for the member cluster in the hub.
func FetchMemberClusterNamespace() (string, error) {
	mcName, err := env.LookupMemberClusterName()
	if err != nil {
		klog.ErrorS(err, "Member cluster name cannot be empty")
		return "", err
	}
	return fmt.Sprintf(HubNamespaceNameFormat, mcName), nil
}
