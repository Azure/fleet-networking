/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Binary hub-afd-controller-manager runs the Azure Front Door reconcilers
// (FrontDoorProfile, FrontDoorCustomDomain) against a Fleet hub cluster.
//
// This binary is INTENTIONALLY SEPARATE from cmd/hub-net-controller-manager
// (the ATM/MCS binary). Proposal 001 §7 (SFI-NS253) requires the AFD
// controller to run under its own Workload-Identity federated subject so
// that ATM-only tenants do not inherit AFD write permissions on the shared
// Azure subscription. A Kubernetes pod projects exactly ONE Workload-Identity
// token, so the identity split is only enforceable at the Pod boundary —
// i.e. by having two independent binaries in two independent Deployments
// backed by two independent ServiceAccounts. See:
//   - docs/first-party/001-afd-global-load-balancing.md §7
//   - docs/first-party/003-pre-implementation-checklist.md §2.4
//
// Consequently this binary:
//   - Loads only the AFD CRDs (FrontDoorProfile, FrontDoorCustomDomain);
//     the ATM CRDs are intentionally not registered here.
//   - Loads its credential from environment-projected Workload Identity
//     (AZURE_TENANT_ID / AZURE_CLIENT_ID / AZURE_FEDERATED_TOKEN_FILE /
//     AZURE_SUBSCRIPTION_ID) — NOT from the shared azure.json cloud config
//     used by the ATM binary.
//   - Uses a distinct leader-election ID, metrics port, and probe port so
//     that a single hub can safely run both binaries side by side without
//     lease collision or port collision.
//
// The chart wiring (charts/hub-afd-controller-manager) lands in a follow-up
// commit; this commit only introduces the binary and removes the AFD block
// from cmd/hub-net-controller-manager. Until that chart lands, this binary
// is not deployable via helm — that is intentional: the POC installation
// path is being closed BEFORE the new one opens so the shared-subject
// deployment topology cannot regress silently.
package main

import (
	"flag"
	"os"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/rand"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/discovery"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"go.goms.io/fleet/pkg/utils"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/azurefrontdoor"
	"go.goms.io/fleet-networking/pkg/controllers/hub/frontdoorcustomdomain"
	"go.goms.io/fleet-networking/pkg/controllers/hub/frontdoorprofile"
)

var (
	scheme = runtime.NewScheme()

	// Distinct defaults from cmd/hub-net-controller-manager so both binaries
	// can be co-scheduled on the same node/pod network without port collision.
	// If you change the defaults here, keep charts/hub-afd-controller-manager
	// (containerPort + probe port + Service ports) in sync.
	metricsAddr = flag.String("metrics-bind-address", ":8082", "The address the metric endpoint binds to.")
	probeAddr   = flag.String("health-probe-bind-address", ":8083", "The address the probe endpoint binds to.")

	enableLeaderElection = flag.Bool("leader-elect", true,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	leaderElectionNamespace = flag.String("leader-election-namespace", "fleet-system", "The namespace in which the leader election resource will be created.")
)

// frontDoorFeatureRequiredGVKs is the AFD-specific CRD set this binary
// depends on. Startup fails fast if any is missing so operators discover
// misconfiguration at deployment time, not at first reconcile. Silently
// starting an idle pod would be strictly worse: healthz would report
// green while nothing is being reconciled.
var frontDoorFeatureRequiredGVKs = []schema.GroupVersionKind{
	fleetnetv1alpha1.GroupVersion.WithKind(fleetnetv1alpha1.FrontDoorProfileKind),
	fleetnetv1alpha1.GroupVersion.WithKind(fleetnetv1alpha1.FrontDoorCustomDomainKind),
}

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	// Only the v1alpha1 group is registered: FrontDoorProfile and
	// FrontDoorCustomDomain both live there. Deliberately no v1beta1 /
	// clusterv1beta1 registration — this binary must not reconcile any
	// ATM / MemberCluster types even if their CRDs happen to be installed
	// on the same cluster.
	utilruntime.Must(fleetnetv1alpha1.AddToScheme(scheme))
	klog.InitFlags(nil)
}

func main() {
	flag.Parse()
	rand.Seed(time.Now().UnixNano())

	handleExit := func() { klog.Flush() }
	exitWithError := func() {
		handleExit()
		os.Exit(1)
	}
	defer handleExit()

	flag.VisitAll(func(f *flag.Flag) {
		klog.InfoS("flag:", "name", f.Name, "value", f.Value)
	})

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	hubConfig := ctrl.GetConfigOrDie()
	mgr, err := ctrl.NewManager(hubConfig, ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: *metricsAddr,
		},
		// The webhook server is initialized even though no webhooks are
		// currently registered, so a future AFD-specific validating webhook
		// (e.g. FrontDoorBackend AFD/ATM coexistence guard from Proposal 001
		// §3.5) can be added without a chart/binary co-change.
		WebhookServer: webhook.NewServer(webhook.Options{
			Port: 9443,
		}),
		HealthProbeBindAddress:  *probeAddr,
		LeaderElection:          *enableLeaderElection,
		LeaderElectionNamespace: *leaderElectionNamespace,
		// Distinct lease name from the ATM binary
		// ("2bf2b407.hub.networking.fleet.azure.com"). Both binaries can be
		// deployed simultaneously into the same namespace without either
		// stealing the other's lease.
		LeaderElectionID: "afd.hub.networking.fleet.azure.com",
	})
	if err != nil {
		klog.ErrorS(err, "Unable to start manager")
		exitWithError()
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		klog.ErrorS(err, "Unable to set up health check")
		exitWithError()
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		klog.ErrorS(err, "Unable to set up ready check")
		exitWithError()
	}

	ctx := ctrl.SetupSignalHandler()

	// CRD gating: this binary is single-purpose, so a missing AFD CRD is a
	// hard failure. The corresponding block in hub-net-controller-manager
	// was gated by --enable-frontdoor-feature; here the feature is
	// unconditionally on and there is no flag to disable it.
	discoverClient := discovery.NewDiscoveryClientForConfigOrDie(hubConfig)
	klog.V(1).InfoS("Checking required Front Door CRDs")
	for _, gvk := range frontDoorFeatureRequiredGVKs {
		if err = utils.CheckCRDInstalled(discoverClient, gvk); err != nil {
			klog.ErrorS(err, "Unable to find the required Front Door CRD", "GVK", gvk)
			exitWithError()
		}
	}

	// Load AFD-scoped Workload Identity from the environment. The projected
	// token here MUST come from a ServiceAccount federated to an AAD app
	// that is DISTINCT from the ATM ServiceAccount's federated subject —
	// that is the SFI-NS253 §7 invariant this binary exists to enforce.
	// Enforcement itself lives outside this Go code (in the chart's
	// ServiceAccount annotations + the operator's federated-credential
	// setup); the effect is: this pod is authorized only for AFD ARM ops.
	klog.V(1).InfoS("Loading Workload Identity config and creating AFD clients")
	afdConfig, err := azurefrontdoor.LoadConfigFromEnv()
	if err != nil {
		klog.ErrorS(err, "Unable to load AFD Workload Identity config from environment")
		exitWithError()
	}
	afdCred, err := azurefrontdoor.NewCredential(afdConfig)
	if err != nil {
		klog.ErrorS(err, "Unable to create AFD Workload Identity credential")
		exitWithError()
	}
	afdClients, err := azurefrontdoor.NewClients(afdCred, afdConfig.SubscriptionID, azurefrontdoor.DefaultARMClientOptions())
	if err != nil {
		klog.ErrorS(err, "Unable to create AFD clients")
		exitWithError()
	}

	klog.V(1).InfoS("Start to setup FrontDoorProfile controller")
	if err := (&frontdoorprofile.Reconciler{
		Client:          mgr.GetClient(),
		ProfilesClient:  afdClients.Profiles,
		EndpointsClient: afdClients.AFDEndpoints,
		Recorder:        mgr.GetEventRecorderFor(frontdoorprofile.ControllerName),
	}).SetupWithManager(mgr); err != nil {
		klog.ErrorS(err, "Unable to create FrontDoorProfile controller")
		exitWithError()
	}

	klog.V(1).InfoS("Start to setup FrontDoorCustomDomain controller")
	if err := (&frontdoorcustomdomain.Reconciler{
		Client:              mgr.GetClient(),
		CustomDomainsClient: afdClients.CustomDomains,
		Recorder:            mgr.GetEventRecorderFor(frontdoorcustomdomain.ControllerName),
	}).SetupWithManager(mgr); err != nil {
		klog.ErrorS(err, "Unable to create FrontDoorCustomDomain controller")
		exitWithError()
	}

	klog.V(1).InfoS("Starting hub-afd-controller-manager")
	if err := mgr.Start(ctx); err != nil {
		klog.ErrorS(err, "Problem running manager")
		exitWithError()
	}
}
