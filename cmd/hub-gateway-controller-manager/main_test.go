/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"go.goms.io/fleet/pkg/utils/cloudconfig/azure"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
	"go.goms.io/fleet-networking/pkg/providers/azure/frontdoor"
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

func TestFeatureFlagDefaults(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{name: "enable-afd", want: "false"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			option := flag.Lookup(tt.name)
			if option == nil || option.DefValue != tt.want {
				t.Errorf("flag %q = %v, want default %q", tt.name, option, tt.want)
			}
		})
	}
}

func TestProductionDependencies_AreConfigured(t *testing.T) {
	got := productionDependencies()

	if got.getConfig == nil || got.newManager == nil || got.signalHandler == nil || got.loadAFDConfig == nil ||
		got.newScheme == nil || got.setupControllers == nil || got.newAFDProviders == nil {
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
		name                string
		options             managerOptions
		schemeError         error
		loadConfigError     error
		newManagerError     error
		healthError         error
		readyError          error
		startError          error
		setupError          error
		cleanupInitError    error
		nilCleanup          bool
		nilObserver         bool
		nilProvisioner      bool
		wantError           string
		wantConfigLoads     int
		wantManagerStarts   int
		wantRegistrations   int
		wantInitializations int
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
			name:                "controller registration failure stops startup",
			options:             managerOptions{enableAFD: true},
			setupError:          errors.New("registration failed"),
			wantError:           "register Gateway controllers",
			wantConfigLoads:     1,
			wantRegistrations:   1,
			wantInitializations: 1,
		},
		{
			name: "enabled AFD validates configuration and starts manager",
			options: managerOptions{
				enableAFD:        true,
				cloudConfigFile:  "provider.json",
				afdResourceGroup: "afd-rg",
			},
			wantConfigLoads:     1,
			wantManagerStarts:   1,
			wantRegistrations:   1,
			wantInitializations: 1,
		},
		{
			name:                "Azure provider initialization failure stops startup",
			options:             managerOptions{enableAFD: true},
			cleanupInitError:    errors.New("auth initialization failed"),
			wantError:           "initialize Azure Front Door providers",
			wantConfigLoads:     1,
			wantInitializations: 1,
		},
		{
			name:                "missing cleanup provider stops startup",
			options:             managerOptions{enableAFD: true},
			nilCleanup:          true,
			wantError:           "cleanup provider is not initialized",
			wantConfigLoads:     1,
			wantInitializations: 1,
		},
		{
			name:                "missing origin observer stops startup",
			options:             managerOptions{enableAFD: true},
			nilObserver:         true,
			wantError:           "origin observer is not initialized",
			wantConfigLoads:     1,
			wantInitializations: 1,
		},
		{
			name:                "missing HTTP provisioner stops startup",
			options:             managerOptions{enableAFD: true},
			nilProvisioner:      true,
			wantError:           "HTTP provisioner is not initialized",
			wantConfigLoads:     1,
			wantInitializations: 1,
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
			name:                "cache readiness registration failure stops enabled startup",
			options:             managerOptions{enableAFD: true},
			readyError:          errors.New("cache readiness failed"),
			wantError:           "register Gateway cache readiness",
			wantConfigLoads:     1,
			wantRegistrations:   1,
			wantInitializations: 1,
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
				healthError:   tt.healthError,
				readyError:    tt.readyError,
				startError:    tt.startError,
				informerCache: newReadinessTestCache(),
			}
			configLoads := 0
			registrations := 0
			initializations := 0
			loadedConfig := &azure.CloudConfig{SubscriptionID: "00000000-0000-0000-0000-000000000000"}
			cleanup := &managerProfileCleaner{}
			deps := dependencies{
				getConfig: func() *rest.Config {
					return &rest.Config{Host: "https://hub.example.com"}
				},
				newManager: func(_ *rest.Config, options ctrl.Options) (ctrl.Manager, error) {
					if options.Scheme == nil {
						t.Error("manager options Scheme = nil, want registered scheme")
					}
					if options.Metrics.BindAddress != tt.options.metricsAddress {
						t.Errorf("metrics address = %q, want %q", options.Metrics.BindAddress, tt.options.metricsAddress)
					}
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
					return loadedConfig, nil
				},
				newAFDProviders: func(config *azure.CloudConfig, group string) (afdProviders, error) {
					initializations++
					if config != loadedConfig || group != tt.options.afdResourceGroup {
						t.Error("provider did not receive the loaded configuration and configured scope")
					}
					if tt.cleanupInitError != nil {
						return afdProviders{}, tt.cleanupInitError
					}
					providers := afdProviders{cleanup: cleanup, observer: cleanup, provisioner: cleanup}
					if tt.nilCleanup {
						providers.cleanup = nil
					}
					if tt.nilObserver {
						providers.observer = nil
					}
					if tt.nilProvisioner {
						providers.provisioner = nil
					}
					return providers, nil
				},
				newScheme: func() (*runtime.Scheme, error) {
					if tt.schemeError != nil {
						return nil, tt.schemeError
					}
					return newScheme()
				},
				setupControllers: func(ctx context.Context, got ctrl.Manager, providers afdProviders) error {
					if got != manager {
						t.Error("registered controllers on a different manager")
					}
					if providers.cleanup != cleanup || providers.observer != cleanup || providers.provisioner != cleanup {
						t.Error("registered different AFD providers")
					}
					registrations++
					if tt.setupError != nil {
						return tt.setupError
					}
					return registerGatewayCacheReadiness(ctx, got)
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
			if registrations != tt.wantRegistrations {
				t.Errorf("controller registrations = %d, want %d", registrations, tt.wantRegistrations)
			}
			if initializations != tt.wantInitializations || cleanup.calls != 0 {
				t.Errorf("provider initializations=%d, cleanup calls=%d; want %d/0", initializations, cleanup.calls, tt.wantInitializations)
			}
			if !tt.options.enableAFD && manager.cacheRequests != 0 {
				t.Errorf("disabled mode requested cache %d times", manager.cacheRequests)
			}
			if manager.starts != 0 {
				assertManagerStartupProbes(t, manager, tt.options.enableAFD)
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
	// Managed-identity and SDK constructors are lazy; initialization must not request a token or call ARM.
	providers, err := initAzureProviders(got, "afd-rg")
	if err != nil || providers.cleanup == nil || providers.observer == nil || providers.provisioner == nil {
		t.Fatalf("initialize providers from the loaded cloud configuration: %v", err)
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

func TestSetupControllers_RegistersWithoutContactingAPIServer(t *testing.T) {
	mgr := newReadinessTestManager(t)
	provider := &managerProfileCleaner{}
	if err := setupControllers(context.Background(), mgr, afdProviders{cleanup: provider, observer: provider, provisioner: provider}); err != nil {
		t.Fatalf("register runtime controllers: %v", err)
	}
	if mgr.check == nil || mgr.checkName != gatewayCacheReadinessCheck {
		t.Fatal("enabled setup did not register Gateway cache readiness")
	}
	if mgr.recorderName != "gateway" {
		t.Fatalf("Gateway event recorder = %q, want gateway", mgr.recorderName)
	}
	if provider.calls != 0 {
		t.Fatalf("controller/cache initialization made %d Azure calls", provider.calls)
	}
}

func assertManagerStartupProbes(t *testing.T, manager *fakeControllerManager, enabled bool) {
	t.Helper()
	const healthName, readinessName = "healthz", "readyz"
	if len(manager.healthChecks) != 1 || manager.healthChecks[healthName] == nil || manager.readyChecks[readinessName] == nil {
		t.Fatalf("missing startup probes: health=%v, ready=%v", manager.healthChecks, manager.readyChecks)
	}
	if err := manager.healthChecks[healthName](nil); err != nil {
		t.Errorf("liveness failed while caches were unsynced: %v", err)
	}
	if err := manager.readyChecks[readinessName](nil); err != nil {
		t.Errorf("baseline readiness ping failed: %v", err)
	}
	if !enabled {
		if len(manager.readyChecks) != 1 {
			t.Error("disabled mode registered additional readiness checks")
		}
		return
	}
	check := manager.readyChecks[gatewayCacheReadinessCheck]
	if len(manager.readyChecks) != 2 || check == nil {
		t.Fatal("enabled mode did not register cache readiness")
	}
	if err := check(nil); err == nil {
		t.Error("enabled mode reported ready before cache synchronization")
	}
}

func newTestGatewayManager(t *testing.T) ctrl.Manager {
	t.Helper()
	scheme, err := newScheme()
	if err != nil {
		t.Fatal(err)
	}
	mapper := meta.NewDefaultRESTMapper(nil)
	for _, gvk := range []schema.GroupVersionKind{
		gatewayv1.SchemeGroupVersion.WithKind("GatewayClass"),
		gatewayv1.SchemeGroupVersion.WithKind("Gateway"),
		gatewayv1.SchemeGroupVersion.WithKind("HTTPRoute"),
		gatewayv1beta1.SchemeGroupVersion.WithKind("ReferenceGrant"),
		fleetnetv1alpha1.GroupVersion.WithKind("ServiceImport"),
		fleetnetv1alpha1.GroupVersion.WithKind("InternalServiceExport"),
		{Version: "v1", Kind: "Namespace"},
	} {
		scope := meta.RESTScopeNamespace
		if gvk.Kind == "GatewayClass" || gvk.Kind == "Namespace" {
			scope = meta.RESTScopeRoot
		}
		mapper.Add(gvk, scope)
	}
	mgr, err := ctrl.NewManager(&rest.Config{Host: "https://127.0.0.1"}, ctrl.Options{
		Scheme: scheme, MapperProvider: func(*rest.Config, *http.Client) (meta.RESTMapper, error) { return mapper, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	return mgr
}

type fakeControllerManager struct {
	ctrl.Manager
	healthError   error
	readyError    error
	startError    error
	starts        int
	healthChecks  map[string]healthz.Checker
	readyChecks   map[string]healthz.Checker
	informerCache cache.Cache
	cacheRequests int
}

func (f *fakeControllerManager) GetCache() cache.Cache {
	f.cacheRequests++
	return f.informerCache
}

func (f *fakeControllerManager) AddHealthzCheck(name string, check healthz.Checker) error {
	if f.healthError != nil {
		return f.healthError
	}
	if f.healthChecks == nil {
		f.healthChecks = make(map[string]healthz.Checker)
	}
	f.healthChecks[name] = check
	return nil
}

func (f *fakeControllerManager) AddReadyzCheck(name string, check healthz.Checker) error {
	if f.readyError != nil {
		return f.readyError
	}
	if f.readyChecks == nil {
		f.readyChecks = make(map[string]healthz.Checker)
	}
	f.readyChecks[name] = check
	return nil
}

func (f *fakeControllerManager) Start(_ context.Context) error {
	f.starts++
	return f.startError
}

type managerProfileCleaner struct {
	calls int
}

func (f *managerProfileCleaner) DeleteProfile(context.Context, frontdoor.GatewayOwner) (bool, error) {
	f.calls++
	return false, errors.New("startup must not call Azure cleanup")
}

func (f *managerProfileCleaner) Observe(context.Context, frontdoor.GatewayOwner,
	gatewaymodel.GlobalGateway) (frontdoor.PrivateOriginsObservation, error) {
	f.calls++
	return frontdoor.PrivateOriginsObservation{}, errors.New("startup must not observe Azure origins")
}

func (f *managerProfileCleaner) ReconcileHTTPPreview(context.Context, frontdoor.GatewayOwner,
	gatewaymodel.GlobalGateway) (frontdoor.HTTPPreviewProvisioningResult, error) {
	f.calls++
	return frontdoor.HTTPPreviewProvisioningResult{}, errors.New("startup must not provision Azure resources")
}

func (f *managerProfileCleaner) SuspendEndpoint(context.Context, frontdoor.GatewayOwner) (frontdoor.EndpointSuspensionResult, error) {
	f.calls++
	return frontdoor.EndpointSuspensionResult{}, errors.New("startup must not suspend Azure resources")
}

func TestCleanupInitializationRejectsMissingDependencies(t *testing.T) {
	tests := []struct {
		name string
		run  func() error
	}{
		{"missing cloud configuration", func() error {
			_, err := initAzureProviders(nil, "afd-rg")
			return err
		}},
		{"missing cleanup provider", func() error {
			return setupControllers(context.Background(), nil, afdProviders{})
		}},
		{"missing observer", func() error {
			return setupControllers(context.Background(), nil, afdProviders{cleanup: &managerProfileCleaner{}})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); err == nil {
				t.Fatal("missing dependency did not stop initialization")
			}
		})
	}
}
