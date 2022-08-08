/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package hubconfig

import (
	"encoding/base64"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/client-go/rest"
)

func TestPrepareHubConfig(t *testing.T) {
	var (
		fakeHubhubServerURLEnvVal       = "fake-hub-server-url"
		fakeConfigtokenConfigPathEnvVal = "fake-config-path" //nolint:gosec
		fakeCerhubCAEnvVal              = base64.StdEncoding.EncodeToString([]byte("fake-certificate-authority"))
	)

	cleanupFunc := func() {
		os.Unsetenv(hubServerURLEnvKey)
		os.Unsetenv(tokenConfigPathEnvKey)
		os.Unsetenv(hubCAEnvKey)
		os.Remove(fakeConfigtokenConfigPathEnvVal)
	}

	defer cleanupFunc()

	testCases := []struct {
		name                 string
		environmentVariables map[string]string
		tlsClientInsecure    bool
		wantErr              bool
	}{
		{
			name:                 "environment variable `HUB_SERVER_URL` is not present",
			environmentVariables: map[string]string{tokenConfigPathEnvKey: fakeConfigtokenConfigPathEnvVal, hubCAEnvKey: fakeCerhubCAEnvVal},
			tlsClientInsecure:    false,
			wantErr:              true,
		},
		{
			name:                 "environment variable `CONFIG_PATH` is not present",
			environmentVariables: map[string]string{hubServerURLEnvKey: fakeHubhubServerURLEnvVal, hubCAEnvKey: fakeCerhubCAEnvVal},
			tlsClientInsecure:    false,
			wantErr:              true,
		},
		{
			name:                 "environment variable `HUB_CERTIFICATE_AUTHORITY` is not present when tlsClientInsecure is false",
			environmentVariables: map[string]string{hubServerURLEnvKey: fakeHubhubServerURLEnvVal, tokenConfigPathEnvKey: fakeConfigtokenConfigPathEnvVal},
			tlsClientInsecure:    false,
			wantErr:              true,
		},
		{
			name:                 "environment variable `HUB_CERTIFICATE_AUTHORITY` is not present when tlsClientInsecure is true",
			environmentVariables: map[string]string{hubServerURLEnvKey: fakeHubhubServerURLEnvVal, tokenConfigPathEnvKey: fakeConfigtokenConfigPathEnvVal},
			tlsClientInsecure:    true,
			wantErr:              false,
		},
		{
			name:                 "hub configuration preparation is done when all requirements meet",
			environmentVariables: map[string]string{hubServerURLEnvKey: fakeHubhubServerURLEnvVal, tokenConfigPathEnvKey: fakeConfigtokenConfigPathEnvVal, hubCAEnvKey: fakeCerhubCAEnvVal},
			tlsClientInsecure:    false,
			wantErr:              false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// remove all environment variables related to this test
			cleanupFunc()

			for envKey, envVal := range tc.environmentVariables {
				os.Setenv(envKey, envVal)
				if envKey == tokenConfigPathEnvKey {
					if _, err := os.Create(fakeConfigtokenConfigPathEnvVal); err != nil {
						t.Errorf("failed to create file %s, err: %s", fakeConfigtokenConfigPathEnvVal, err.Error())
					}
				}
			}

			hubConfig, err := PrepareHubConfig(tc.tlsClientInsecure)
			if (err != nil) != tc.wantErr {
				t.Fatalf("PrepareHubConfig() error = %v, wantErr %t", err, tc.wantErr)
			}

			if tc.wantErr {
				return
			}

			if tc.tlsClientInsecure {
				expectedHubConfig := &rest.Config{
					BearerTokenFile: fakeConfigtokenConfigPathEnvVal,
					Host:            fakeHubhubServerURLEnvVal,
					TLSClientConfig: rest.TLSClientConfig{
						Insecure: tc.tlsClientInsecure,
					},
				}
				if !cmp.Equal(*hubConfig, *expectedHubConfig) {
					t.Errorf("PrepareHubConfig() got hub config: %v, want: %v", expectedHubConfig, hubConfig)
				}
			}

			if !tc.tlsClientInsecure {
				decodedClusterCaCertificate, err := base64.StdEncoding.DecodeString(fakeCerhubCAEnvVal)
				if err != nil {
					t.Fatalf("failed to base-encode hub CA, error: %s", err.Error())
				}
				expectedHubConfig := &rest.Config{
					BearerTokenFile: fakeConfigtokenConfigPathEnvVal,
					Host:            fakeHubhubServerURLEnvVal,
					TLSClientConfig: rest.TLSClientConfig{
						Insecure: tc.tlsClientInsecure,
						CAData:   decodedClusterCaCertificate,
					},
				}

				if !cmp.Equal(hubConfig, expectedHubConfig) {
					t.Errorf("PrepareHubConfig() got hub config: %v, want: %v", expectedHubConfig, hubConfig)
				}
			}
		})
	}
}
