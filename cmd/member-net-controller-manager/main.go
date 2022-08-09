/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	//+kubebuilder:scaffold:imports

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/env"
	"go.goms.io/fleet-networking/pkg/common/hubconfig"
	"go.goms.io/fleet-networking/pkg/controllers/member/endpointslice"
	"go.goms.io/fleet-networking/pkg/controllers/member/endpointsliceexport"
	"go.goms.io/fleet-networking/pkg/controllers/member/endpointsliceimport"
	"go.goms.io/fleet-networking/pkg/controllers/member/internalserviceexport"
	"go.goms.io/fleet-networking/pkg/controllers/member/internalserviceimport"
	"go.goms.io/fleet-networking/pkg/controllers/member/serviceexport"
	"go.goms.io/fleet-networking/pkg/controllers/member/serviceimport"
)

const (
	// Environment variable keys
	memberClusterNameEnvKey = "MEMBER_CLUSTER_NAME"

	// Naming pattern of member cluster namespace in hub cluster, should be the same as value as defined in
	// https://github.com/Azure/fleet/blob/main/pkg/utils/common.go
	hubNamespaceNameFormat = "fleet-member-%s"
)

var (
	scheme               = runtime.NewScheme()
	tlsClientInsecure    = flag.Bool("tls-insecure", false, "Enable TLSClientConfig.Insecure property. Enabling this will make the connection inSecure (should be 'true' for testing purpose only.)")
	hubMetricsAddr       = flag.String("hub-metrics-bind-address", ":8080", "The address of hub controller manager the metric endpoint binds to.")
	hubProbeAddr         = flag.String("hub-health-probe-bind-address", ":8081", "The address of hub controller manager the probe endpoint binds to.")
	metricsAddr          = flag.String("member-metrics-bind-address", ":8090", "The address of member controller manager the metric endpoint binds to.")
	probeAddr            = flag.String("member-health-probe-bind-address", ":8091", "The address of member controller manager the probe endpoint binds to.")
	enableLeaderElection = flag.Bool("leader-elect", true, "Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	fleetSystemNamespace = flag.String("fleet-system-namespace", "fleet-system", "The reserved system namespace used by fleet.")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(fleetnetv1alpha1.AddToScheme(scheme))
	klog.InitFlags(nil)

	//+kubebuilder:scaffold:scheme
}

func main() {
	flag.Parse()

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

	hubConfig, hubOptions, err := prepareHubParameters()
	if err != nil {
		exitWithErrorFunc()
	}

	memberConfig, memberOptions := prepareMemberParameters()

	// Setup hub controller manager.
	hubMgr, err := ctrl.NewManager(hubConfig, *hubOptions)
	if err != nil {
		klog.ErrorS(err, "Unable to start hub manager")
		exitWithErrorFunc()
	}
	if err := hubMgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		klog.ErrorS(err, "Unable to set up health check for hub manager")
		exitWithErrorFunc()
	}
	if err := hubMgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		klog.ErrorS(err, "Unable to set up ready check for hub manager")
		exitWithErrorFunc()
	}

	// Setup member controller manager.
	memberMgr, err := ctrl.NewManager(memberConfig, *memberOptions)
	if err != nil {
		klog.ErrorS(err, "Unable to start member manager")
		exitWithErrorFunc()
	}
	if err := memberMgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		klog.ErrorS(err, "Unable to set up health check for member manager")
		exitWithErrorFunc()
	}
	if err := memberMgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		klog.ErrorS(err, "Unable to set up ready check for member manager")
		exitWithErrorFunc()
	}

	ctx, cancel := context.WithCancel(context.Background())

	klog.V(1).InfoS("Setup controllers with controller manager")
	if err := setupControllersWithManager(ctx, hubMgr, memberMgr); err != nil {
		klog.ErrorS(err, "Unable to setup controllers with manager")
		exitWithErrorFunc()
	}

	// All managers should stop if either of them is dead or Linux SIGTERM or SIGINT signal is received
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ch
		klog.Info("Received termination, signaling shutdown ServiceExportImport controller manager")
		cancel()
	}()

	var startErrors []error
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		klog.V(1).InfoS("Starting hub manager for ServiceExportImport controller manager")
		defer func() {
			wg.Done()
			klog.V(1).InfoS("Shutting down hub manager")
			cancel()
		}()
		if err := hubMgr.Start(ctx); err != nil {
			klog.ErrorS(err, "Failed to start hub manager")
			startErrors = append(startErrors, err)
		}
	}()
	wg.Add(1)
	go func() {
		klog.V(1).InfoS("Starting member manager for ServiceExportImport controller manager")
		defer func() {
			klog.V(1).InfoS("Shutting down member manager")
			wg.Done()
			cancel()
		}()
		if err = memberMgr.Start(ctx); err != nil {
			klog.ErrorS(err, "Failed to start member manager")
			startErrors = append(startErrors, err)
		}
	}()

	wg.Wait()

	if len(startErrors) > 0 {
		exitWithErrorFunc()
	}
}

func prepareHubParameters() (*rest.Config, *ctrl.Options, error) {
	hubConfig, err := hubconfig.PrepareHubConfig(*tlsClientInsecure)
	if err != nil {
		klog.ErrorS(err, "Failed to get hub config")
		return nil, nil, err
	}

	mcHubNamespace, err := fetchMemberClusterHubNamespaceName()
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

func prepareMemberParameters() (*rest.Config, *ctrl.Options) {
	memberOpts := &ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     *metricsAddr,
		Port:                   8443,
		HealthProbeBindAddress: *probeAddr,
		LeaderElection:         *enableLeaderElection,
		LeaderElectionID:       "2bf2b407.member.networking.fleet.azure.com",
	}
	return ctrl.GetConfigOrDie(), memberOpts
}

func fetchMemberClusterHubNamespaceName() (string, error) {
	mcName, err := env.Lookup(memberClusterNameEnvKey)
	if err != nil {
		klog.ErrorS(err, "Member cluster name cannot be empty")
		return "", err
	}
	return fmt.Sprintf(hubNamespaceNameFormat, mcName), nil
}

func setupControllersWithManager(ctx context.Context, hubMgr, memberMgr manager.Manager) error {
	klog.V(1).InfoS("Begin to setup controllers with controller manager")

	mcName, err := env.Lookup(memberClusterNameEnvKey)
	if err != nil {
		klog.ErrorS(err, "Member cluster name cannot be empty")
		return err
	}

	mcHubNamespace, err := fetchMemberClusterHubNamespaceName()
	if err != nil {
		klog.ErrorS(err, "Failed to get member cluster hub namespace")
		return err
	}

	memberClient := memberMgr.GetClient()
	hubClient := hubMgr.GetClient()

	klog.V(1).InfoS("Create endpointslice controller")
	if err := (&endpointslice.Reconciler{
		MemberClusterID: mcName,
		MemberClient:    memberClient,
		HubClient:       hubClient,
		HubNamespace:    mcHubNamespace,
	}).SetupWithManager(ctx, memberMgr); err != nil {
		klog.ErrorS(err, "Unable to create endpointslice controller")
		return err
	}

	klog.V(1).InfoS("Create endpointsliceexport controller")
	if err := (&endpointsliceexport.Reconciler{
		MemberClient: memberClient,
		HubClient:    hubClient,
	}).SetupWithManager(hubMgr); err != nil {
		klog.ErrorS(err, "Unable to create endpointsliceexport controller")
		return err
	}

	klog.V(1).InfoS("Create endpointsliceimport controller")
	if err := (&endpointsliceimport.Reconciler{
		MemberClient:         memberClient,
		HubClient:            hubClient,
		FleetSystemNamespace: *fleetSystemNamespace,
	}).SetupWithManager(ctx, memberMgr, hubMgr); err != nil {
		klog.ErrorS(err, "Unable to create endpointsliceimport controller")
		return err
	}

	klog.V(1).InfoS("Create internalserviceexport controller")
	if err := (&internalserviceexport.Reconciler{
		MemberClient: memberClient,
		HubClient:    hubClient,
	}).SetupWithManager(hubMgr); err != nil {
		klog.ErrorS(err, "Unable to create internalserviceexport controller")
		return err
	}

	klog.V(1).InfoS("Create internalserviceimport controller")
	if err := (&internalserviceimport.Reconciler{
		MemberClient: memberClient,
		HubClient:    hubClient,
	}).SetupWithManager(hubMgr); err != nil {
		klog.ErrorS(err, "Unable to create internalserviceimport controller")
		return err
	}

	klog.V(1).InfoS("Create serviceexport reconciler")
	if err := (&serviceexport.Reconciler{
		MemberClient:    memberClient,
		HubClient:       hubClient,
		MemberClusterID: mcName,
		HubNamespace:    mcHubNamespace,
	}).SetupWithManager(memberMgr); err != nil {
		klog.ErrorS(err, "Unable to create serviceexport reconciler")
		return err
	}

	klog.V(1).InfoS("Create serviceimport reconciler")
	if err := (&serviceimport.Reconciler{
		MemberClient:    memberClient,
		HubClient:       hubClient,
		MemberClusterID: mcName,
		HubNamespace:    mcHubNamespace,
	}).SetupWithManager(memberMgr); err != nil {
		klog.ErrorS(err, "Unable to create serviceimport reconciler")
		return err
	}

	klog.V(1).InfoS("Succeeded to setup controllers with controller manager")
	return nil
}
