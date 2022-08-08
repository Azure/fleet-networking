/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package hubconfig

import (
	"encoding/base64"
	"os"
	"reflect"
	"testing"

	"k8s.io/client-go/rest"
)

func TestPrepareHubConfig(t *testing.T) {
	var (
		fakeHubhubServerURLEnvVal       = "fake-hub-server-url"
		fakeConfigtokenConfigPathEnvVal = "fake-config-path"
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
		raiseError           bool
		tlsClientInsecure    bool
	}{
		{
			name:                 "environment variable `HUB_SERVER_URL` is not present",
			environmentVariables: map[string]string{tokenConfigPathEnvKey: fakeConfigtokenConfigPathEnvVal, hubCAEnvKey: fakeCerhubCAEnvVal},
			raiseError:           true,
			tlsClientInsecure:    false,
		},
		{
			name:                 "environment variable `CONFIG_PATH` is not present",
			environmentVariables: map[string]string{hubServerURLEnvKey: fakeHubhubServerURLEnvVal, hubCAEnvKey: fakeCerhubCAEnvVal},
			raiseError:           true,
			tlsClientInsecure:    false,
		},
		{
			name:                 "environment variable `HUB_CERTIFICATE_AUTHORITY` is not present when tlsClientInsecure is false",
			environmentVariables: map[string]string{hubServerURLEnvKey: fakeHubhubServerURLEnvVal, tokenConfigPathEnvKey: fakeConfigtokenConfigPathEnvVal},
			raiseError:           true,
			tlsClientInsecure:    false,
		},
		{
			name:                 "environment variable `HUB_CERTIFICATE_AUTHORITY` is not present when tlsClientInsecure is true",
			environmentVariables: map[string]string{hubServerURLEnvKey: fakeHubhubServerURLEnvVal, tokenConfigPathEnvKey: fakeConfigtokenConfigPathEnvVal},
			raiseError:           false,
			tlsClientInsecure:    true,
		},
		{
			name:                 "hub configuration preparation is done when all requirements meet",
			environmentVariables: map[string]string{hubServerURLEnvKey: fakeHubhubServerURLEnvVal, tokenConfigPathEnvKey: fakeConfigtokenConfigPathEnvVal, hubCAEnvKey: fakeCerhubCAEnvVal},
			raiseError:           false,
			tlsClientInsecure:    false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// remove all environment variables related to this test
			cleanupFunc()

			for envKey, envVal := range tc.environmentVariables {
				os.Setenv(envKey, envVal)
				if envKey == tokenConfigPathEnvKey {
					os.Create(fakeConfigtokenConfigPathEnvVal)
				}
			}

			hubConfig, err := PrepareHubConfig(tc.tlsClientInsecure)
			if tc.raiseError && err == nil {
				t.Errorf("error should be raised")
			}

			if !tc.raiseError && err != nil {
				t.Errorf("error should not be raised, but got err: %s", err.Error())
			}

			if tc.raiseError {
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
				if !reflect.DeepEqual(*hubConfig, *expectedHubConfig) {
					t.Errorf("expected hub config: %s, actual: %s", expectedHubConfig, hubConfig)
				}
			}

			if !tc.tlsClientInsecure {
				decodedClusterCaCertificate, err := base64.StdEncoding.DecodeString(fakeCerhubCAEnvVal)
				if err != nil {
					t.Errorf("failed to base-encode hub CA, error: %s", err.Error())
				}
				expectedHubConfig := &rest.Config{
					BearerTokenFile: fakeConfigtokenConfigPathEnvVal,
					Host:            fakeHubhubServerURLEnvVal,
					TLSClientConfig: rest.TLSClientConfig{
						Insecure: tc.tlsClientInsecure,
						CAData:   decodedClusterCaCertificate,
					},
				}

				if !reflect.DeepEqual(hubConfig, expectedHubConfig) {
					t.Errorf("expected hub config: %s, actual: %s", expectedHubConfig, hubConfig)
				}

			}
		})
	}
}
