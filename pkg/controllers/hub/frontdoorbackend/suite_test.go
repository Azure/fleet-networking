/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package frontdoorbackend

import (
	"context"
	"flag"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	fleetnetv1beta1 "go.goms.io/fleet-networking/api/v1beta1"
	"go.goms.io/fleet-networking/test/common/azurefrontdoor/fakeprovider"
)

// Test-suite bootstrap for the FrontDoorBackend controller. Mirrors the
// frontdoorprofile suite so contributors reading either side see the same
// shape: envtest brings up etcd + kube-apiserver, controller-runtime wires
// the Reconciler with in-memory armcdn fakes, and Ginkgo specs run against
// the real cache/informer stack.

var (
	cfg             *rest.Config
	mgr             manager.Manager
	k8sClient       client.Client
	testEnv         *envtest.Environment
	ctx             context.Context
	cancel          context.CancelFunc
	originGroupFake *fakeprovider.OriginGroupFake
	originFake      *fakeprovider.OriginFake
)

var testNamespace = fakeprovider.ProfileNamespace

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "FrontDoorBackend Controller Suite")
}

var _ = BeforeSuite(func() {
	logger := zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true))
	klog.SetLogger(logger)
	log.SetLogger(logger)

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("../../../../", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	// Both API groups are needed: FrontDoorBackend + FrontDoorProfile +
	// InternalServiceExport live in v1alpha1; TrafficManagerBackend
	// (needed for the coexistence guard) is v1beta1.
	Expect(fleetnetv1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(fleetnetv1beta1.AddToScheme(scheme.Scheme)).To(Succeed())

	By("constructing the k8s client")
	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	By("starting the controller manager")
	klog.InitFlags(flag.CommandLine)
	flag.Parse()

	mgr, err = ctrl.NewManager(cfg, ctrl.Options{
		Scheme:  scheme.Scheme,
		Metrics: metricsserver.Options{BindAddress: "0"},
	})
	Expect(err).NotTo(HaveOccurred())

	originGroupFake, err = fakeprovider.NewOriginGroupFake()
	Expect(err).To(Succeed(), "failed to create fake AFD origin-groups client")

	originFake, err = fakeprovider.NewOriginFake()
	Expect(err).To(Succeed(), "failed to create fake AFD origins client")

	Expect((&Reconciler{
		Client:             mgr.GetClient(),
		OriginGroupsClient: originGroupFake.Client,
		OriginsClient:      originFake.Client,
		Recorder:           mgr.GetEventRecorderFor(ControllerName),
	}).SetupWithManager(mgr)).To(Succeed())

	By("creating the test namespace")
	ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}}
	Expect(k8sClient.Create(ctx, &ns)).To(Succeed())

	go func() {
		defer GinkgoRecover()
		Expect(mgr.Start(ctx)).To(Succeed(), "failed to run manager")
	}()
})

var _ = AfterSuite(func() {
	defer klog.Flush()

	By("deleting the test namespace")
	ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}}
	Expect(k8sClient.Delete(ctx, &ns)).To(Succeed())

	cancel()
	By("tearing down the test environment")
	Expect(testEnv.Stop()).To(Succeed())
})
