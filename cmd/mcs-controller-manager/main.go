/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
// Binary mcs-controller-manager features the mcs controller to multiclusterservice CRD.
// The controller could be installed in either hub cluster or member clusters.
package main

import (
	"flag"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/klog/v2"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	//+kubebuilder:scaffold:imports

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/util"
	"go.goms.io/fleet-networking/pkg/controllers/multiclusterservice"
)

var (
	scheme               = runtime.NewScheme()
	metricsAddr          = flag.String("metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	probeAddr            = flag.String("health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	enableLeaderElection = flag.Bool("leader-elect", true,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	fleetSystemNamespace = flag.String("fleet-system-namespace", "fleet-system", "The reserved system namespace used by fleet.")
)

func init() {
	klog.InitFlags(nil)

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(fleetnetv1alpha1.AddToScheme(scheme))

	//+kubebuilder:scaffold:scheme
}

func main() {
	flag.Parse()

	deferredFunc := func() {
		klog.Flush()
	}
	defer deferredFunc()

	flag.VisitAll(func(f *flag.Flag) {
		klog.InfoS("flag:", "name", f.Name, "value", f.Value)
	})

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     *metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: *probeAddr,
		LeaderElection:         *enableLeaderElection,
		LeaderElectionID:       "2bf2b407.mcs.networking.fleet.azure.com",
	})
	if err != nil {
		klog.ErrorS(err, "unable to start manager")
		os.Exit(util.BeforeProgramExitWithError(deferredFunc))
	}

	//+kubebuilder:scaffold:builder
	r := &multiclusterservice.Reconciler{
		Client:               mgr.GetClient(),
		FleetSystemNamespace: *fleetSystemNamespace,
	}
	if err := r.SetupWithManager(mgr); err != nil {
		klog.ErrorS(err, "unable to create mcs controller")
		os.Exit(util.BeforeProgramExitWithError(deferredFunc))
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		klog.ErrorS(err, "unable to set up health check")
		os.Exit(util.BeforeProgramExitWithError(deferredFunc))
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		klog.ErrorS(err, "unable to set up ready check")
		os.Exit(util.BeforeProgramExitWithError(deferredFunc))
	}

	klog.V(1).Info("starting mcs controller manager")

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		klog.ErrorS(err, "problem running manager")
		os.Exit(util.BeforeProgramExitWithError(deferredFunc))
	}
}
