/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Binary hub-net-controller-manager watches fleet-networking CRDs in the hub cluster to export/import multi-cluster
// services.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/trafficmanager/armtrafficmanager"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/rand"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/discovery"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/klog/v2"
	"sigs.k8s.io/cloud-provider-azure/pkg/azclient"
	"sigs.k8s.io/cloud-provider-azure/pkg/azclient/policy/ratelimit"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	//+kubebuilder:scaffold:imports
	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/cloudconfig/azure"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	fleetnetv1beta1 "go.goms.io/fleet-networking/api/v1beta1"
	"go.goms.io/fleet-networking/pkg/controllers/hub/endpointsliceexport"
	"go.goms.io/fleet-networking/pkg/controllers/hub/internalserviceexport"
	"go.goms.io/fleet-networking/pkg/controllers/hub/internalserviceimport"
	"go.goms.io/fleet-networking/pkg/controllers/hub/membercluster"
	"go.goms.io/fleet-networking/pkg/controllers/hub/serviceimport"
	"go.goms.io/fleet-networking/pkg/controllers/hub/trafficmanagerbackend"
	"go.goms.io/fleet-networking/pkg/controllers/hub/trafficmanagerprofile"
)

var (
	scheme = runtime.NewScheme()

	metricsAddr = flag.String("metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	probeAddr   = flag.String("health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")

	enableLeaderElection = flag.Bool("leader-elect", true,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	leaderElectionNamespace = flag.String("leader-election-namespace", "fleet-system", "The namespace in which the leader election resource will be created.")

	internalServiceExportRetryInterval = flag.Duration("internalserviceexport-retry-interval", 2*time.Second,
		"The wait time for the internalserviceexport controller to requeue the request and to wait for the"+
			"ServiceImport controller to resolve the service Spec")

	forceDeleteWaitTime = flag.Duration("force-delete-wait-time", 15*time.Minute, "The duration the fleet hub agent waits before trying to force delete a member cluster.")

	enableV1Beta1APIs = flag.Bool("enable-v1beta1-apis", true, "If set, the agents will watch for the v1beta1 APIs.")

	enableTrafficManagerFeature = flag.Bool("enable-traffic-manager-feature", false, "If set, the traffic manager feature will be enabled.")

	cloudConfigFile = flag.String("cloud-config", "/etc/kubernetes/provider/azure.json", "The path to the cloud config file which will be used to access the Azure resource.")
)

var (
	trafficManagerFeatureRequiredGVKs = []schema.GroupVersionKind{
		fleetnetv1beta1.GroupVersion.WithKind(fleetnetv1beta1.TrafficManagerProfileKind),
		fleetnetv1beta1.GroupVersion.WithKind(fleetnetv1beta1.TrafficManagerBackendKind),
	}
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(fleetnetv1alpha1.AddToScheme(scheme))
	utilruntime.Must(fleetnetv1beta1.AddToScheme(scheme))
	utilruntime.Must(clusterv1beta1.AddToScheme(scheme))
	klog.InitFlags(nil)
	//+kubebuilder:scaffold:scheme
}

func main() {
	flag.Parse()
	rand.Seed(time.Now().UnixNano())

	handleExitFunc := func() {
		klog.Flush()
	}

	exitWithErrorFunc := func() {
		handleExitFunc()
		os.Exit(1)
	}

	defer handleExitFunc()

	flag.VisitAll(func(f *flag.Flag) {
		klog.InfoS("flag:", "name", f.Name, "value", f.Value)
	})

	hubConfig := ctrl.GetConfigOrDie()
	mgr, err := ctrl.NewManager(hubConfig, ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: *metricsAddr,
		},
		WebhookServer: webhook.NewServer(webhook.Options{
			Port: 9443,
		}),
		HealthProbeBindAddress:  *probeAddr,
		LeaderElection:          *enableLeaderElection,
		LeaderElectionNamespace: *leaderElectionNamespace,
		LeaderElectionID:        "2bf2b407.hub.networking.fleet.azure.com",
	})
	if err != nil {
		klog.ErrorS(err, "Unable to start manager")
		exitWithErrorFunc()
	}

	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		klog.ErrorS(err, "Unable to set up health check")
		exitWithErrorFunc()
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		klog.ErrorS(err, "Unable to set up ready check")
		exitWithErrorFunc()
	}

	ctx := ctrl.SetupSignalHandler()

	klog.V(1).InfoS("Start to setup EndpointsliceExport controller")
	if err := (&endpointsliceexport.Reconciler{
		HubClient: mgr.GetClient(),
	}).SetupWithManager(ctx, mgr); err != nil {
		klog.ErrorS(err, "Unable to create EndpointsliceExport controller")
		exitWithErrorFunc()
	}

	klog.V(1).InfoS("Start to setup InternalServiceExport controller")
	if err := (&internalserviceexport.Reconciler{
		Client:        mgr.GetClient(),
		RetryInternal: *internalServiceExportRetryInterval,
	}).SetupWithManager(mgr); err != nil {
		klog.ErrorS(err, "Unable to create InternalServiceExport controller")
		exitWithErrorFunc()
	}

	klog.V(1).InfoS("Start to setup InternalServiceImport controller")
	if err := (&internalserviceimport.Reconciler{
		HubClient: mgr.GetClient(),
	}).SetupWithManager(ctx, mgr); err != nil {
		klog.ErrorS(err, "Unable to create InternalServiceImport controller")
		exitWithErrorFunc()
	}

	klog.V(1).InfoS("Start to setup ServiceImport controller")
	if err := (&serviceimport.Reconciler{
		Client:   mgr.GetClient(),
		Recorder: mgr.GetEventRecorderFor(serviceimport.ControllerName),
	}).SetupWithManager(ctx, mgr); err != nil {
		klog.ErrorS(err, "Unable to create ServiceImport controller")
		exitWithErrorFunc()
	}

	discoverClient := discovery.NewDiscoveryClientForConfigOrDie(hubConfig)
	if *enableV1Beta1APIs {
		gvk := clusterv1beta1.GroupVersion.WithKind(clusterv1beta1.MemberClusterKind)
		if utils.CheckCRDInstalled(discoverClient, gvk) == nil {
			klog.V(1).InfoS("Start to setup MemberCluster controller")
			if err := (&membercluster.Reconciler{
				Client:              mgr.GetClient(),
				Recorder:            mgr.GetEventRecorderFor(membercluster.ControllerName),
				ForceDeleteWaitTime: *forceDeleteWaitTime,
			}).SetupWithManager(mgr); err != nil {
				klog.ErrorS(err, "Unable to create MemberCluster controller")
				exitWithErrorFunc()
			}
		}
	}
	if *enableTrafficManagerFeature {
		klog.V(1).InfoS("Traffic manager feature is enabled, checking the required CRDs")
		for _, gvk := range trafficManagerFeatureRequiredGVKs {
			if err = utils.CheckCRDInstalled(discoverClient, gvk); err != nil {
				klog.ErrorS(err, "Unable to find the required CRD", "GVK", gvk)
				exitWithErrorFunc()
			}
		}

		klog.V(1).InfoS("Traffic manager feature is enabled, loading cloud config and creating azure clients", "cloudConfigFile", *cloudConfigFile)
		cloudConfig, err := azure.NewCloudConfigFromFile(*cloudConfigFile)
		if err != nil {
			klog.ErrorS(err, "Unable to load cloud config", "file name", *cloudConfigFile)
			exitWithErrorFunc()
		}
		cloudConfig.SetUserAgent("fleet-hub-net-controller-manager")
		klog.V(1).InfoS("Cloud config loaded", "cloudConfig", cloudConfig)

		profilesClient, endpointsClient, err := initAzureTrafficManagerClients(cloudConfig)
		if err != nil {
			klog.ErrorS(err, "Unable to create Azure Traffic Manager clients")
			exitWithErrorFunc()
		}
		klog.V(1).InfoS("Start to setup TrafficManagerProfile controller")
		if err := (&trafficmanagerprofile.Reconciler{
			Client:         mgr.GetClient(),
			ProfilesClient: profilesClient,
		}).SetupWithManager(mgr); err != nil {
			klog.ErrorS(err, "Unable to create TrafficManagerProfile controller")
			exitWithErrorFunc()
		}

		klog.V(1).InfoS("Start to setup TrafficManagerBackend controller")
		if err := (&trafficmanagerbackend.Reconciler{
			Client:          mgr.GetClient(),
			ProfilesClient:  profilesClient,
			EndpointsClient: endpointsClient,
			// serviceImport controller has already enabled the internalServiceExportIndexer.
			// Therefore, no need to setup it again.
		}).SetupWithManager(ctx, mgr, true); err != nil {
			klog.ErrorS(err, "Unable to create TrafficManagerProfile controller")
			exitWithErrorFunc()
		}
	}

	klog.V(1).InfoS("Starting ServiceExportImport controller manager")
	if err := mgr.Start(ctx); err != nil {
		klog.ErrorS(err, "Problem running manager")
		exitWithErrorFunc()
	}
}

// initAzureTrafficManagerClients initializes the Azure Traffic Manager profiles and endpoints clients.
func initAzureTrafficManagerClients(cloudConfig *azure.CloudConfig) (*armtrafficmanager.ProfilesClient, *armtrafficmanager.EndpointsClient, error) {
	authProvider, err := azclient.NewAuthProvider(&cloudConfig.ARMClientConfig, &cloudConfig.AzureAuthConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create Azure auth provider: %w", err)
	}

	factoryConfig := &azclient.ClientFactoryConfig{
		CloudProviderBackoff: true,
		SubscriptionID:       cloudConfig.SubscriptionID,
	}
	options, err := azclient.GetDefaultResourceClientOption(&cloudConfig.ARMClientConfig, factoryConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get default resource client option: %w", err)
	}

	if rateLimitPolicy := ratelimit.NewRateLimitPolicy(cloudConfig.Config); rateLimitPolicy != nil {
		options.ClientOptions.PerCallPolicies = append(options.ClientOptions.PerCallPolicies, rateLimitPolicy)
	}

	profilesClient, err := armtrafficmanager.NewProfilesClient(cloudConfig.SubscriptionID, authProvider.GetAzIdentity(), options)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create Azure trafficManager profiles client: %w", err)
	}

	endpointsClient, err := armtrafficmanager.NewEndpointsClient(cloudConfig.SubscriptionID, authProvider.GetAzIdentity(), options)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create Azure trafficManager endpoints client: %w", err)
	}
	return profilesClient, endpointsClient, nil
}
