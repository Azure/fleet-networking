/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package main

import (
	"flag"
	"os"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	//+kubebuilder:scaffold:imports

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/controllers/hub/internalserviceexport"
	"go.goms.io/fleet-networking/pkg/controllers/hub/serviceimport"
)

var (
	scheme = runtime.NewScheme()

	metricsAddr          = flag.String("metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	probeAddr            = flag.String("health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	enableLeaderElection = flag.Bool("leader-elect", true,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	serviceImportSpecProcessTime = flag.Duration("serviceimportspec-retry-interval", 2*time.Second, "The wait time for the controller to requeue the request and to wait for the"+
		"ServiceImport controller to resolve the service Spec")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(fleetnetv1alpha1.AddToScheme(scheme))
	klog.InitFlags(nil)
	//+kubebuilder:scaffold:scheme
}

func main() {
	defer klog.Flush()

	flag.Parse()
	flag.VisitAll(func(f *flag.Flag) {
		klog.InfoS("flag:", "name", f.Name, "value", f.Value)
	})

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     *metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: *probeAddr,
		LeaderElection:         *enableLeaderElection,
		LeaderElectionID:       "2bf2b407.hub.networking.fleet.azure.com",
	})
	if err != nil {
		klog.ErrorS(err, "Unable to start manager")
		os.Exit(1)
	}

	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		klog.ErrorS(err, "Unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		klog.ErrorS(err, "Unable to set up ready check")
		os.Exit(1)
	}

	klog.V(1).InfoS("Start to setup InternalServicEexport controller")
	if err := (&internalserviceexport.Reconciler{
		Client:                       mgr.GetClient(),
		ServiceImportSpecProcessTime: *serviceImportSpecProcessTime,
	}).SetupWithManager(mgr); err != nil {
		klog.ErrorS(err, "Unable to create mcs controller")
		os.Exit(1)
	}

	klog.V(1).InfoS("Start to setup ServiceImport controller")
	if err := (&serviceimport.Reconciler{
		Client: mgr.GetClient(),
	}).SetupWithManager(mgr); err != nil {
		klog.ErrorS(err, "Unable to create mcs controller")
		os.Exit(1)
	}

	klog.V(1).InfoS("Starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		klog.ErrorS(err, "Problem running manager")
		os.Exit(1)
	}
}
