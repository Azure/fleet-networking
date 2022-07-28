/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"os"
	"sync"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	//+kubebuilder:scaffold:imports

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/controllers/member/endpointslice"
	"go.goms.io/fleet-networking/pkg/controllers/member/endpointsliceexport"
	"go.goms.io/fleet-networking/pkg/controllers/member/internalserviceexport"
	"go.goms.io/fleet-networking/pkg/controllers/member/internalserviceimport"
	"go.goms.io/fleet-networking/pkg/controllers/member/serviceexport"
	"go.goms.io/fleet-networking/pkg/controllers/member/serviceimport"
)

const (
	// Environment variable keys
	memberClusterNameEnvKey = "MEMBER_CLUSTER_NAME"
	hubServerURLEnvKey      = "HUB_SERVER_URL"
	tokenConfigPathEnvKey   = "CONFIG_PATH" //nolint:gosec
	hubCAEnvKey             = "HUB_CERTIFICATE_AUTHORITY"

	// Naming pattern of member cluster namespace in hub cluster, should be the same as value as defined in
	// https://github.com/Azure/fleet/blob/main/pkg/utils/common.go
	hubNamespaceNameFormat = "fleet-member-%s"
)

var (
	scheme               = runtime.NewScheme()
	tlsClientInsecure    = flag.Bool("tls-insecure", false, "Enable TLSClientConfig.Insecure property. Enabling this will make the connection inSecure (should be 'true' for testing purpose only.)")
	hubProbeAddr         = flag.String("hub-health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	hubMetricsAddr       = flag.String("hub-metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	metricsAddr          = flag.String("metrics-bind-address", ":8090", "The address the metric endpoint binds to.")
	probeAddr            = flag.String("health-probe-bind-address", ":8082", "The address the probe endpoint binds to.")
	enableLeaderElection = flag.Bool("leader-elect", true, "Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(fleetnetv1alpha1.AddToScheme(scheme))
	klog.InitFlags(nil)

	//+kubebuilder:scaffold:scheme
}

func main() {
	flag.Parse()
	defer klog.Flush()

	flag.VisitAll(func(f *flag.Flag) {
		klog.InfoS("flag:", "name", f.Name, "value", f.Value)
	})
	hubConfig, hubOptions, err := prepareHubParameters()
	if err != nil {
		os.Exit(1)
	}

	memberConfig, memberOptions, err := prepareMemberParameters()
	if err != nil {
		os.Exit(1)
	}

	if err := startControllerManagers(hubConfig, memberConfig, hubOptions, memberOptions); err != nil {
		os.Exit(1)
	}
}

func prepareHubParameters() (*rest.Config, *ctrl.Options, error) {
	hubURL, err := envOrError(hubServerURLEnvKey)
	if err != nil {
		klog.ErrorS(err, "Hub server api cannot be empty")
		return nil, nil, err
	}

	tokenFilePath, err := envOrError(tokenConfigPathEnvKey)
	if err != nil {
		klog.ErrorS(err, "Hub token file path cannot be empty")
		return nil, nil, err
	}

	// Retry on obtaining token file as it is created asynchronously by token-refesher container
	if err := retry.OnError(retry.DefaultRetry, func(e error) bool {
		return true
	}, func() error {
		// Stat returns file info. It will return an error if there is no file.
		_, err := os.Stat(tokenFilePath)
		return err
	}); err != nil {
		klog.ErrorS(err, "Cannot retrieve token file from the path %s", tokenFilePath)
		return nil, nil, err
	}
	var hubConfig *rest.Config
	if *tlsClientInsecure {
		hubConfig = &rest.Config{
			BearerTokenFile: tokenFilePath,
			Host:            hubURL,
			TLSClientConfig: rest.TLSClientConfig{
				Insecure: *tlsClientInsecure,
			},
		}
	} else {
		hubCA, err := envOrError(hubCAEnvKey)
		if err != nil {
			klog.ErrorS(err, "Hub certificate authority cannot be empty")
		}
		decodedClusterCaCertificate, err := base64.StdEncoding.DecodeString(hubCA)
		if err != nil {
			klog.ErrorS(err, "Cannot decode hub cluster certificate authority data")
			return nil, nil, err
		}
		hubConfig = &rest.Config{
			BearerTokenFile: tokenFilePath,
			Host:            hubURL,
			TLSClientConfig: rest.TLSClientConfig{
				Insecure: *tlsClientInsecure,
				CAData:   decodedClusterCaCertificate,
			},
		}
	}

	mcHubNamespace, err := getMemberClusterHubNamespaceName()
	if err != nil {
		klog.ErrorS(err, "Failed to get member cluster hub namespace")
		return nil, nil, err
	}

	hubOptions := &ctrl.Options{
		Scheme:                  scheme,
		MetricsBindAddress:      *hubMetricsAddr,
		Port:                    9443,
		HealthProbeBindAddress:  *hubProbeAddr,
		LeaderElection:          *enableLeaderElection,
		LeaderElectionID:        "2bf2b407.hub.networking.fleet.azure.com",
		LeaderElectionNamespace: mcHubNamespace, // This requires we have access to resource "leases" in API group "coordination.k8s.io" under namespace $mcHubNamespace
		Namespace:               mcHubNamespace, // Restricts the manager's cache to watch objects in the member hub namespace.
	}
	return hubConfig, hubOptions, nil
}

func prepareMemberParameters() (*rest.Config, *ctrl.Options, error) {
	memberOpts := &ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     *metricsAddr,
		Port:                   8443,
		HealthProbeBindAddress: *probeAddr,
		LeaderElection:         *enableLeaderElection,
		LeaderElectionID:       "2bf2b407.member.networking.fleet.azure.com",
	}
	return ctrl.GetConfigOrDie(), memberOpts, nil
}

func startControllerManagers(hubConfig, memberConfig *rest.Config, hubOptions, memberOptions *ctrl.Options) error {
	if hubConfig == nil || memberConfig == nil || hubOptions == nil || memberOptions == nil {
		return errors.New("config or options cannot be nil")
	}

	// Setup hub controller manager.
	hubMgr, err := ctrl.NewManager(hubConfig, *hubOptions)
	if err != nil {
		klog.ErrorS(err, "Unable to start hub manager")
		return err
	}
	if err := hubMgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		klog.ErrorS(err, "Unable to set up health check for hub manager")
		return err
	}
	if err := hubMgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		klog.ErrorS(err, "Unable to set up ready check for hub manager")
		return err
	}

	// Setup member controller manager.
	memberMgr, err := ctrl.NewManager(memberConfig, *memberOptions)
	if err != nil {
		klog.ErrorS(err, "Unable to start member manager")
		return err
	}
	if err := memberMgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		klog.ErrorS(err, "Unable to set up health check for member manager")
		return err
	}
	if err := memberMgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		klog.ErrorS(err, "Unable to set up ready check for member manager")
		return err
	}

	klog.V(2).InfoS("Setup controllers with controller manager")
	if err := setupControllersWithManager(hubMgr, memberMgr); err != nil {
		klog.ErrorS(err, "Unable to setup controllers with manager")
		return err
	}

	var startErr error
	// All managers should stop if any of them is dead.
	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		klog.V(2).InfoS("Starting hub manager")
		defer klog.V(2).InfoS("Shutting down hub manager")
		if err := hubMgr.Start(ctx); err != nil {
			klog.ErrorS(err, "Failed to starting hub manager")
		} else {
			startErr = err
		}
		cancel()
	}()
	wg.Add(1)
	go func() {
		klog.V(2).InfoS("Starting member manager")
		defer klog.V(3).InfoS("Shutting down member manager")
		if err = memberMgr.Start(ctx); err != nil {
			klog.ErrorS(err, "Failed to starting member manager")
		} else {
			startErr = err
		}
		cancel()
	}()

	wg.Wait()
	if startErr != nil {
		return startErr
	}
	return nil
}

func getMemberClusterHubNamespaceName() (string, error) {
	mcName, err := envOrError(memberClusterNameEnvKey)
	if err != nil {
		klog.ErrorS(err, "Member cluster name cannot be empty")
		return "", err
	}
	mcHubNamespace := fmt.Sprintf(hubNamespaceNameFormat, mcName)
	return mcHubNamespace, nil
}

func setupControllersWithManager(hubMgr, memberMgr manager.Manager) error {
	klog.V(2).InfoS("Begin to setup controllers with controller manager")

	mcName, err := envOrError(memberClusterNameEnvKey)
	if err != nil {
		klog.ErrorS(err, "Member cluster name cannot be empty")
		return err
	}

	mcHubNamespace, err := getMemberClusterHubNamespaceName()
	if err != nil {
		klog.ErrorS(err, "Failed to get member cluster hub namespace")
		return err
	}

	klog.V(2).InfoS("Create endpointslice reconciler")
	ctx := context.Background()
	if err := endpointslice.NewReconciler(memberMgr.GetClient(), hubMgr.GetClient(), mcHubNamespace).SetupWithManager(ctx, hubMgr); err != nil {
		klog.ErrorS(err, "Unable to create endpointslice reconciler")
		return err
	}

	klog.V(2).InfoS("Create endpointslice reconciler")
	if err := endpointsliceexport.NewReconciler(memberMgr.GetClient(), hubMgr.GetClient()).SetupWithManager(hubMgr); err != nil {
		klog.ErrorS(err, "Unable to create endpointslice reconciler")
		return err
	}

	klog.V(2).InfoS("Create endpointslice reconciler")
	if err := endpointsliceexport.NewReconciler(memberMgr.GetClient(), hubMgr.GetClient()).SetupWithManager(hubMgr); err != nil {
		klog.ErrorS(err, "Unable to create endpointslice reconciler")
		return err
	}

	klog.V(2).InfoS("Create internalserviceexport reconciler")
	if err := internalserviceexport.NewReconciler(memberMgr.GetClient(), hubMgr.GetClient()).SetupWithManager(hubMgr); err != nil {
		klog.ErrorS(err, "Unable to create internalserviceexport reconciler")
		return err
	}

	klog.V(2).InfoS("Create internalserviceimport reconciler")
	if err := internalserviceimport.NewReconciler(memberMgr.GetClient(), hubMgr.GetClient()).SetupWithManager(hubMgr); err != nil {
		klog.ErrorS(err, "Unable to create internalserviceimport reconciler")
		return err
	}

	klog.V(2).InfoS("Create serviceexport reconciler")
	if err := serviceexport.NewReconciler(memberMgr.GetClient(), hubMgr.GetClient(), mcName, mcHubNamespace).SetupWithManager(memberMgr); err != nil {
		klog.ErrorS(err, "Unable to create serviceexport reconciler")
		return err
	}

	klog.V(2).InfoS("Create serviceimport reconciler")
	if err := serviceimport.NewReconciler(memberMgr.GetClient(), hubMgr.GetClient(), mcName, mcHubNamespace).SetupWithManager(memberMgr); err != nil {
		klog.ErrorS(err, "Unable to create serviceimport reconciler")
		return err
	}

	klog.V(2).InfoS("Succeeded to setup controllers with controller manager")
	return nil
}

func envOrError(envKey string) (string, error) {
	value, ok := os.LookupEnv(envKey)
	if !ok {
		return "", fmt.Errorf("Failed to retrieve the environment variable value from %s", envKey)
	}
	return value, nil
}
