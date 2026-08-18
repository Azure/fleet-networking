/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Binary hub-gateway-controller-manager watches Gateway API and Fleet
// ServiceImport resources in the hub cluster.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"go.goms.io/fleet/pkg/utils/cloudconfig/azure"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

var (
	metricsAddr = flag.String("metrics-bind-address", ":8082", "The address the metrics endpoint binds to.")
	probeAddr   = flag.String("health-probe-bind-address", ":8083", "The address the health probe endpoint binds to.")

	enableLeaderElection    = flag.Bool("leader-elect", true, "Enable leader election for the controller manager.")
	leaderElectionNamespace = flag.String("leader-election-namespace", "fleet-system", "The namespace used for leader election.")

	enableAFD        = flag.Bool("enable-afd", false, "Enable Azure Front Door reconciliation.")
	cloudConfigFile  = flag.String("cloud-config", "/etc/kubernetes/provider/azure.json", "The Azure cloud configuration file.")
	afdResourceGroup = flag.String("afd-resource-group", "", "The resource group in which the controller manages Azure Front Door resources.")
)

func init() {
	// Register klog flags before flag.Parse so production verbosity and output
	// settings are available consistently with the existing controller binaries.
	klog.InitFlags(nil)
}

func main() {
	flag.Parse()
	defer klog.Flush()

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	if err := run(currentOptions(), productionDependencies()); err != nil {
		klog.ErrorS(err, "Problem running hub Gateway controller manager")
		os.Exit(1)
	}
}

func currentOptions() managerOptions {
	return managerOptions{
		metricsAddress:          *metricsAddr,
		probeAddress:            *probeAddr,
		leaderElection:          *enableLeaderElection,
		leaderElectionNamespace: *leaderElectionNamespace,
		enableAFD:               *enableAFD,
		cloudConfigFile:         *cloudConfigFile,
		afdResourceGroup:        *afdResourceGroup,
	}
}

type managerOptions struct {
	metricsAddress          string
	probeAddress            string
	leaderElection          bool
	leaderElectionNamespace string
	enableAFD               bool
	cloudConfigFile         string
	afdResourceGroup        string
}

type controllerManager interface {
	AddHealthzCheck(string, healthz.Checker) error
	AddReadyzCheck(string, healthz.Checker) error
	Start(context.Context) error
}

type dependencies struct {
	getConfig     func() *rest.Config
	newManager    func(*rest.Config, ctrl.Options) (controllerManager, error)
	signalHandler func() context.Context
	loadAFDConfig func(string, string) (*azure.CloudConfig, error)
	newScheme     func() (*runtime.Scheme, error)
}

func productionDependencies() dependencies {
	return dependencies{
		getConfig: ctrl.GetConfigOrDie,
		newManager: func(config *rest.Config, options ctrl.Options) (controllerManager, error) {
			return ctrl.NewManager(config, options)
		},
		signalHandler: ctrl.SetupSignalHandler,
		loadAFDConfig: loadAFDConfiguration,
		newScheme:     newScheme,
	}
}

func run(options managerOptions, deps dependencies) error {
	scheme, err := deps.newScheme()
	if err != nil {
		return fmt.Errorf("register controller schemes: %w", err)
	}

	if options.enableAFD {
		if _, err := deps.loadAFDConfig(options.cloudConfigFile, options.afdResourceGroup); err != nil {
			return fmt.Errorf("load Azure Front Door configuration: %w", err)
		}
		// The feature flag intentionally performs configuration validation only
		// until the Gateway reconcilers are introduced in the next slice.
		klog.InfoS("Azure Front Door configuration is valid; no reconcilers are registered in the foundation release")
	} else {
		klog.InfoS("Azure Front Door reconciliation is disabled")
	}

	mgr, err := deps.newManager(deps.getConfig(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: options.metricsAddress,
		},
		HealthProbeBindAddress:  options.probeAddress,
		LeaderElection:          options.leaderElection,
		LeaderElectionNamespace: options.leaderElectionNamespace,
		LeaderElectionID:        "hub-gateway-controller-manager.networking.fleet.azure.com",
	})
	if err != nil {
		return fmt.Errorf("create hub Gateway controller manager: %w", err)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("set up health check: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("set up readiness check: %w", err)
	}

	klog.InfoS("Starting hub Gateway controller manager")
	if err := mgr.Start(deps.signalHandler()); err != nil {
		return fmt.Errorf("start hub Gateway controller manager: %w", err)
	}
	return nil
}

func newScheme() (*runtime.Scheme, error) {
	scheme := runtime.NewScheme()
	installers := []func(*runtime.Scheme) error{
		clientgoscheme.AddToScheme,
		gatewayv1.Install,
		// ReferenceGrant remains v1beta1 in Gateway API v1.2.1.
		gatewayv1beta1.Install,
		fleetnetv1alpha1.AddToScheme,
	}
	for _, install := range installers {
		if err := install(scheme); err != nil {
			return nil, err
		}
	}
	return scheme, nil
}

func loadAFDConfiguration(cloudConfigPath, resourceGroup string) (*azure.CloudConfig, error) {
	if resourceGroup == "" {
		return nil, errors.New("afd-resource-group must be configured when Azure Front Door reconciliation is enabled")
	}
	cloudConfig, err := azure.NewCloudConfigFromFile(cloudConfigPath)
	if err != nil {
		return nil, err
	}
	if cloudConfig.SubscriptionID == "" {
		return nil, errors.New("Azure subscription ID must be configured")
	}
	cloudConfig.SetUserAgent("fleet-hub-gateway-controller-manager")
	return cloudConfig, nil
}
