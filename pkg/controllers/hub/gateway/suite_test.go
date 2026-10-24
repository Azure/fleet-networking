/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gateway

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewayclass"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewayresolver"
	"go.goms.io/fleet-networking/pkg/controllers/hub/httproute"
)

var (
	integrationContext      context.Context
	integrationClient       client.Client
	integrationCachedReader client.Reader
	integrationProvider     *integrationAFDProvider
	integrationHub          corev1.Namespace
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Gateway Current Runtime Integration Suite")
}

var _ = BeforeSuite(func() {
	ctrl.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	lookupContext, cancelLookup := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelLookup()
	// Use the selected dependency's real schemas, not a second vendored CRD version.
	module, err := exec.CommandContext(lookupContext, "go", "list", "-mod=readonly", "-m", "-f", "{{.Dir}}", "sigs.k8s.io/gateway-api").Output()
	Expect(err).NotTo(HaveOccurred())
	Expect(strings.TrimSpace(string(module))).NotTo(BeEmpty())
	testEnvironment := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "..", "config", "crd", "bases"),
			filepath.Join(strings.TrimSpace(string(module)), "config", "crd", "standard"),
		},
		ErrorIfCRDPathMissing: true,
	}
	DeferCleanup(func() {
		// A failed process startup has no initialized API server for Stop.
		if testEnvironment.Config != nil {
			Expect(testEnvironment.Stop()).To(Succeed())
		}
	})
	cfg, err := testEnvironment.Start()
	Expect(err).NotTo(HaveOccurred())

	scheme := runtime.NewScheme()
	for _, install := range []func(*runtime.Scheme) error{
		corev1.AddToScheme, gatewayv1.Install, gatewayv1beta1.Install, fleetnetv1alpha1.AddToScheme,
	} {
		Expect(install(scheme)).To(Succeed())
	}
	integrationClient, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())
	integrationContext = context.Background()
	key := client.ObjectKey{Name: metav1.NamespaceSystem}
	if err := integrationClient.Get(integrationContext, key, &integrationHub); apierrors.IsNotFound(err) {
		integrationHub.Name = metav1.NamespaceSystem
		Expect(integrationClient.Create(integrationContext, &integrationHub)).To(Succeed())
	} else {
		Expect(err).NotTo(HaveOccurred())
	}
	Expect(integrationHub.UID).NotTo(BeEmpty())

	manager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme, Metrics: metricsserver.Options{BindAddress: "0"}, HealthProbeBindAddress: "0",
	})
	Expect(err).NotTo(HaveOccurred())
	integrationCachedReader = manager.GetClient()
	ctx, cancel := context.WithCancel(context.Background())
	Expect(gatewayresolver.SetupIndexes(ctx, manager.GetFieldIndexer())).To(Succeed())
	integrationProvider = &integrationAFDProvider{}
	Expect((&gatewayclass.Reconciler{Client: manager.GetClient()}).SetupWithManager(manager)).To(Succeed())
	Expect((&Reconciler{
		Client: manager.GetClient(), APIReader: manager.GetAPIReader(),
		OriginObserver: integrationProvider, ProfileCleanup: integrationProvider,
		Recorder: manager.GetEventRecorderFor("gateway"),
	}).SetupWithManager(manager)).To(Succeed())
	Expect((&httproute.Reconciler{Client: manager.GetClient()}).SetupWithManager(manager)).To(Succeed())
	done := make(chan error, 1)
	DeferCleanup(func() {
		cancel()
		Eventually(done, 15*time.Second).Should(Receive(Succeed()))
	})
	go func() {
		done <- manager.Start(ctx)
	}()
	syncContext, cancelSync := context.WithTimeout(ctx, 30*time.Second)
	defer cancelSync()
	Expect(manager.GetCache().WaitForCacheSync(syncContext)).To(BeTrue())
})
