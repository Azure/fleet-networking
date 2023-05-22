/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package hubconfig

import (
	"encoding/base64"
	"fmt"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/client-go/rest"
)

func TestPrepareHubConfig(t *testing.T) {
	var (
		fakeHubhubServerURLEnvVal       = "fake-hub-server-url"
		fakeConfigtokenConfigPathEnvVal = "testdata/fake-config-path" //nolint:gosec
		fakeCerhubCA                    = []byte("fake-certificate-authority")
		fakeCerhubCAEnvVal              = base64.StdEncoding.EncodeToString(fakeCerhubCA)
	)

	testCases := []struct {
		name                 string
		environmentVariables map[string]string
		tlsClientInsecure    bool
		validate             func(t *testing.T, config *rest.Config, err error)
	}{
		{
			name:                 "environment variable `HUB_SERVER_URL` is not present",
			environmentVariables: map[string]string{tokenConfigPathEnvKey: fakeConfigtokenConfigPathEnvVal, hubCAEnvKey: fakeCerhubCAEnvVal},
			tlsClientInsecure:    false,
			validate: func(t *testing.T, config *rest.Config, err error) {
				if err == nil {
					t.Errorf("expect return error if HUB_SERVER_URL not present")
				}
			},
		},
		{
			name:                 "environment variable `CONFIG_PATH` is not present",
			environmentVariables: map[string]string{hubServerURLEnvKey: fakeHubhubServerURLEnvVal, hubCAEnvKey: fakeCerhubCAEnvVal},
			tlsClientInsecure:    false,
			validate: func(t *testing.T, config *rest.Config, err error) {
				if err == nil {
					t.Errorf("expect return error if CONFIG_PATH not present")
				}
			},
		},
		{
			name:                 "environment variable `HUB_CERTIFICATE_AUTHORITY` is not present when tlsClientInsecure is false",
			environmentVariables: map[string]string{hubServerURLEnvKey: fakeHubhubServerURLEnvVal, tokenConfigPathEnvKey: fakeConfigtokenConfigPathEnvVal},
			tlsClientInsecure:    false,
			validate: func(t *testing.T, config *rest.Config, err error) {
				if err != nil {
					t.Errorf("not expect error but actually get error %s", err)
				}
				wantConfig := &rest.Config{
					BearerTokenFile: fakeConfigtokenConfigPathEnvVal,
					Host:            fakeHubhubServerURLEnvVal,
					TLSClientConfig: rest.TLSClientConfig{
						Insecure: false,
					},
				}
				if !cmp.Equal(config, wantConfig) {
					t.Errorf("got hub config %+v, want %+v", config, wantConfig)
				}
			},
		},
		{
			name:                 "environment variable `HUB_CERTIFICATE_AUTHORITY` is not present when tlsClientInsecure is true",
			environmentVariables: map[string]string{hubServerURLEnvKey: fakeHubhubServerURLEnvVal, tokenConfigPathEnvKey: fakeConfigtokenConfigPathEnvVal},
			tlsClientInsecure:    true,
			validate: func(t *testing.T, config *rest.Config, err error) {
				if err != nil {
					t.Errorf("not expect error but actually get error %s", err)
				}
				wantConfig := &rest.Config{
					BearerTokenFile: fakeConfigtokenConfigPathEnvVal,
					Host:            fakeHubhubServerURLEnvVal,
					TLSClientConfig: rest.TLSClientConfig{
						Insecure: true,
					},
				}
				if !cmp.Equal(config, wantConfig) {
					t.Errorf("got hub config %+v, want %+v", config, wantConfig)
				}
			},
		},
		{
			name:                 "hub configuration preparation is done when all requirements meet",
			environmentVariables: map[string]string{hubServerURLEnvKey: fakeHubhubServerURLEnvVal, tokenConfigPathEnvKey: fakeConfigtokenConfigPathEnvVal, hubCAEnvKey: fakeCerhubCAEnvVal},
			tlsClientInsecure:    false,
			validate: func(t *testing.T, config *rest.Config, err error) {
				if err != nil {
					t.Errorf("not expect error but actually get error %s", err)
				}
				wantConfig := &rest.Config{
					BearerTokenFile: fakeConfigtokenConfigPathEnvVal,
					Host:            fakeHubhubServerURLEnvVal,
					TLSClientConfig: rest.TLSClientConfig{
						Insecure: false,
						CAData:   fakeCerhubCA,
					},
				}
				if !cmp.Equal(config, wantConfig) {
					t.Errorf("got hub config %+v, want %+v", config, wantConfig)
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			for envKey, envVal := range tc.environmentVariables {
				t.Setenv(envKey, envVal)
			}

			hubConfig, err := PrepareHubConfig(tc.tlsClientInsecure)
			tc.validate(t, hubConfig, err)
		})
	}
}

func TestFetchMemberClusterNamespace(t *testing.T) {
	memberCluster := "cluster-a"
	testCases := []struct {
		name     string
		envKey   string
		envValue string
		want     string
		wantErr  bool
	}{
		{
			name:     "environment variable is present",
			envKey:   "MEMBER_CLUSTER_NAME",
			envValue: memberCluster,
			want:     fmt.Sprintf(hubNamespaceNameFormat, memberCluster),
			wantErr:  false,
		},
		{
			name:    "environment variable is not present",
			envKey:  "MEMBER_CLUSTER_NAME",
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if len(tc.envValue) == 0 {
				os.Unsetenv(tc.envKey)
			} else {
				os.Setenv(tc.envKey, tc.envValue)
			}
			got, err := FetchMemberClusterNamespace()
			if (err != nil) != tc.wantErr {
				t.Fatalf("FetchMemberClusterNamespace() got err %v, want err %v", err, tc.wantErr)
			}

			if tc.wantErr {
				return
			}

			if got != tc.want {
				t.Errorf("FetchMemberClusterNamespace() = %v, want %v", got, tc.want)
			}
		})
	}
}
