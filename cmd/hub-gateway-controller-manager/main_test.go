/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"go.goms.io/fleet/pkg/utils/cloudconfig/azure"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

func TestNewScheme_RegistersGatewayAndFleetTypes(t *testing.T) {
	scheme, err := newScheme()
	if err != nil {
		t.Fatalf("newScheme() error = %v", err)
	}

	tests := []struct {
		name string
		gvk  schema.GroupVersionKind
	}{
		{
			name: "Gateway",
			gvk:  gatewayv1.SchemeGroupVersion.WithKind("Gateway"),
		},
		{
			name: "HTTPRoute",
			gvk:  gatewayv1.SchemeGroupVersion.WithKind("HTTPRoute"),
		},
		{
			name: "ReferenceGrant",
			gvk:  gatewayv1beta1.SchemeGroupVersion.WithKind("ReferenceGrant"),
		},
		{
			name: "Fleet ServiceImport",
			gvk:  fleetnetv1alpha1.GroupVersion.WithKind("ServiceImport"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !scheme.Recognizes(tt.gvk) {
				t.Errorf("scheme does not recognize %s", tt.gvk)
			}
		})
	}
}

func TestNewScheme_PreservesFleetServiceImportGVK(t *testing.T) {
	scheme, err := newScheme()
	if err != nil {
		t.Fatalf("newScheme() error = %v", err)
	}

	gvks, _, err := scheme.ObjectKinds(&fleetnetv1alpha1.ServiceImport{})
	if err != nil {
		t.Fatalf("scheme.ObjectKinds(ServiceImport) error = %v", err)
	}

	want := fleetnetv1alpha1.GroupVersion.WithKind("ServiceImport")
	for _, got := range gvks {
		if got == want {
			return
		}
	}
	t.Errorf("ServiceImport GVKs = %v, want to contain %s", gvks, want)
}

func TestCurrentOptions_UsesCommandLineConfiguration(t *testing.T) {
	got := currentOptions()

	if got.metricsAddress != *metricsAddr ||
		got.probeAddress != *probeAddr ||
		got.leaderElection != *enableLeaderElection ||
		got.leaderElectionNamespace != *leaderElectionNamespace ||
		got.enableAFD != *enableAFD ||
		got.cloudConfigFile != *cloudConfigFile ||
		got.afdResourceGroup != *afdResourceGroup {
		t.Errorf("currentOptions() = %#v, want values from command-line flags", got)
	}
}

func TestProductionDependencies_AreConfigured(t *testing.T) {
	got := productionDependencies()

	if got.getConfig == nil || got.newManager == nil || got.signalHandler == nil || got.loadAFDConfig == nil || got.newScheme == nil {
		t.Errorf("productionDependencies() = %#v, want all dependencies configured", got)
	}

	// Constructing a manager exercises the production adapter without starting
	// it or contacting a Kubernetes API server.
	if _, err := got.newManager(&rest.Config{Host: "https://127.0.0.1"}, ctrl.Options{}); err != nil {
		t.Fatalf("production newManager() error = %v", err)
	}
}

func TestRun_ManagesStartupLifecycle(t *testing.T) {
	tests := []struct {
		name              string
		options           managerOptions
		schemeError       error
		loadConfigError   error
		newManagerError   error
		healthError       error
		readyError        error
		startError        error
		wantError         string
		wantConfigLoads   int
		wantManagerStarts int
	}{
		{
			name:        "scheme registration failure stops startup",
			schemeError: errors.New("scheme failed"),
			wantError:   "register controller schemes",
		},
		{
			name: "disabled AFD starts manager without loading Azure configuration",
			options: managerOptions{
				metricsAddress:          ":8082",
				probeAddress:            ":8083",
				leaderElection:          true,
				leaderElectionNamespace: "fleet-system",
			},
			wantManagerStarts: 1,
		},
		{
			name: "enabled AFD validates configuration and starts manager",
			options: managerOptions{
				enableAFD:        true,
				cloudConfigFile:  "provider.json",
				afdResourceGroup: "afd-rg",
			},
			wantConfigLoads:   1,
			wantManagerStarts: 1,
		},
		{
			name: "configuration failure stops startup",
			options: managerOptions{
				enableAFD: true,
			},
			loadConfigError: errors.New("invalid cloud config"),
			wantError:       "load Azure Front Door configuration",
			wantConfigLoads: 1,
		},
		{
			name:            "manager creation failure is returned",
			newManagerError: errors.New("manager failed"),
			wantError:       "create hub Gateway controller manager",
		},
		{
			name:        "health registration failure is returned",
			healthError: errors.New("health failed"),
			wantError:   "set up health check",
		},
		{
			name:       "readiness registration failure is returned",
			readyError: errors.New("ready failed"),
			wantError:  "set up readiness check",
		},
		{
			name:              "manager start failure is returned",
			startError:        errors.New("start failed"),
			wantError:         "start hub Gateway controller manager",
			wantManagerStarts: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := &fakeControllerManager{
				healthError: tt.healthError,
				readyError:  tt.readyError,
				startError:  tt.startError,
			}
			configLoads := 0
			var receivedOptions ctrl.Options
			deps := dependencies{
				getConfig: func() *rest.Config {
					return &rest.Config{Host: "https://hub.example.com"}
				},
				newManager: func(_ *rest.Config, options ctrl.Options) (controllerManager, error) {
					receivedOptions = options
					if tt.newManagerError != nil {
						return nil, tt.newManagerError
					}
					return manager, nil
				},
				signalHandler: func() context.Context {
					return context.Background()
				},
				loadAFDConfig: func(_, _ string) (*azure.CloudConfig, error) {
					configLoads++
					if tt.loadConfigError != nil {
						return nil, tt.loadConfigError
					}
					return &azure.CloudConfig{SubscriptionID: "00000000-0000-0000-0000-000000000000"}, nil
				},
				newScheme: func() (*runtime.Scheme, error) {
					if tt.schemeError != nil {
						return nil, tt.schemeError
					}
					return newScheme()
				},
			}

			err := run(tt.options, deps)
			if tt.wantError != "" {
				if err == nil {
					t.Fatalf("run() error = nil, want containing %q", tt.wantError)
				}
				if !strings.Contains(err.Error(), tt.wantError) {
					t.Errorf("run() error = %q, want containing %q", err, tt.wantError)
				}
			} else if err != nil {
				t.Fatalf("run() error = %v", err)
			}

			if configLoads != tt.wantConfigLoads {
				t.Errorf("Azure configuration loads = %d, want %d", configLoads, tt.wantConfigLoads)
			}
			if manager.starts != tt.wantManagerStarts {
				t.Errorf("manager starts = %d, want %d", manager.starts, tt.wantManagerStarts)
			}
			if tt.schemeError == nil && tt.newManagerError == nil && tt.loadConfigError == nil {
				if receivedOptions.Scheme == nil {
					t.Error("manager options Scheme = nil, want registered scheme")
				}
				if receivedOptions.Metrics.BindAddress != tt.options.metricsAddress {
					t.Errorf("metrics address = %q, want %q", receivedOptions.Metrics.BindAddress, tt.options.metricsAddress)
				}
			}
		})
	}
}

func TestLoadAFDConfiguration_RejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name          string
		cloudConfig   string
		resourceGroup string
		wantError     string
	}{
		{
			name:        "missing resource group",
			cloudConfig: "provider.json",
			wantError:   "afd-resource-group",
		},
		{
			name:          "missing cloud config file",
			cloudConfig:   "does-not-exist.json",
			resourceGroup: "afd-rg",
			wantError:     "does-not-exist.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadAFDConfiguration(tt.cloudConfig, tt.resourceGroup)
			if err == nil {
				t.Fatalf("loadAFDConfiguration() error = nil, want containing %q", tt.wantError)
			}
			if !strings.Contains(err.Error(), tt.wantError) {
				t.Errorf("loadAFDConfiguration() error = %q, want containing %q", err, tt.wantError)
			}
		})
	}
}

func TestLoadAFDConfiguration_LoadsValidConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "provider.json")
	const subscriptionID = "00000000-0000-0000-0000-000000000000"
	config := `{
		"cloud":"AzurePublicCloud",
		"location":"eastus",
		"subscriptionID":"` + subscriptionID + `",
		"resourceGroup":"fleet-rg",
		"useManagedIdentityExtension":true
	}`
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	got, err := loadAFDConfiguration(path, "afd-rg")
	if err != nil {
		t.Fatalf("loadAFDConfiguration() error = %v", err)
	}
	if got.SubscriptionID != subscriptionID {
		t.Errorf("SubscriptionID = %q, want %q", got.SubscriptionID, subscriptionID)
	}
}

func TestLoadAFDConfiguration_RejectsMissingSubscription(t *testing.T) {
	path := filepath.Join(t.TempDir(), "provider.json")
	config := `{
		"cloud":"AzurePublicCloud",
		"location":"eastus",
		"resourceGroup":"fleet-rg",
		"useManagedIdentityExtension":true
	}`
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	_, err := loadAFDConfiguration(path, "afd-rg")
	if err == nil {
		t.Fatal("loadAFDConfiguration() error = nil, want missing subscription error")
	}
	if !strings.Contains(err.Error(), "subscription ID") {
		t.Errorf("loadAFDConfiguration() error = %q, want missing subscription ID", err)
	}
}

type fakeControllerManager struct {
	healthError error
	readyError  error
	startError  error
	starts      int
}

func (f *fakeControllerManager) AddHealthzCheck(_ string, _ healthz.Checker) error {
	return f.healthError
}

func (f *fakeControllerManager) AddReadyzCheck(_ string, _ healthz.Checker) error {
	return f.readyError
}

func (f *fakeControllerManager) Start(_ context.Context) error {
	f.starts++
	return f.startError
}
