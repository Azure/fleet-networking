/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Binary hub-net-controller-manager watches fleet-networking CRDs in the hub cluster to export/import multi-cluster
// services.
package main

import (
	"flag"
	"os"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	//+kubebuilder:scaffold:imports

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/controllers/hub/endpointsliceexport"
	"go.goms.io/fleet-networking/pkg/controllers/hub/internalserviceexport"
	"go.goms.io/fleet-networking/pkg/controllers/hub/internalserviceimport"
	"go.goms.io/fleet-networking/pkg/controllers/hub/membercluster"
	"go.goms.io/fleet-networking/pkg/controllers/hub/serviceimport"
	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
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

	forceDeleteWaitTime = flag.Duration("force-delete-wait-time", 15*time.Minute, "The duration the hub agent waits before force deleting a member cluster.")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(fleetnetv1alpha1.AddToScheme(scheme))
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

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
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

	klog.V(1).InfoS("Start to setup MemberCluster controller")
	if err := (&membercluster.Reconciler{
		Client:              mgr.GetClient(),
		Recorder:            mgr.GetEventRecorderFor(membercluster.ControllerName),
		ForceDeleteWaitTime: *forceDeleteWaitTime,
	}).SetupWithManager(mgr); err != nil {
		klog.ErrorS(err, "Unable to create MemberCluster controller")
		exitWithErrorFunc()
	}

	klog.V(1).InfoS("Starting ServiceExportImport controller manager")
	if err := mgr.Start(ctx); err != nil {
		klog.ErrorS(err, "Problem running manager")
		exitWithErrorFunc()
	}
}
