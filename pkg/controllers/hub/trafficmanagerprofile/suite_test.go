/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package trafficmanagerprofile

import (
	"context"
	"flag"
	"os"
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

	fleetnetv1beta1 "go.goms.io/fleet-networking/api/v1beta1"
	"go.goms.io/fleet-networking/test/common/trafficmanager/fakeprovider"
)

var (
	cfg       *rest.Config
	mgr       manager.Manager
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc
)

var (
	testNamespace = fakeprovider.ProfileNamespace
)

var (
	originalGenerateAzureTrafficManagerProfileNameFunc = generateAzureTrafficManagerProfileNameFunc
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "TrafficManagerProfile Controller Suite")
}

var _ = BeforeSuite(func() {
	// Check if KUBEBUILDER_ASSETS environment variable is set
	kubebuildAssets := os.Getenv("KUBEBUILDER_ASSETS")
	if kubebuildAssets == "" {
		// Skip all tests if KUBEBUILDER_ASSETS is not set
		Skip("Skipping integration tests because KUBEBUILDER_ASSETS environment variable is not set")
	}

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

	err = fleetnetv1beta1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme
	By("construct the k8s client")
	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	By("starting the controller manager")
	klog.InitFlags(flag.CommandLine)
	flag.Parse()

	mgr, err = ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	Expect(err).NotTo(HaveOccurred())

	profileClient, err := fakeprovider.NewProfileClient()
	Expect(err).Should(Succeed(), "failed to create the fake profile client")

	generateAzureTrafficManagerProfileNameFunc = func(profile *fleetnetv1beta1.TrafficManagerProfile) string {
		return profile.Name
	}

	err = (&Reconciler{
		Client:         mgr.GetClient(),
		ProfilesClient: profileClient,
		Recorder:       mgr.GetEventRecorderFor(ControllerName),
	}).SetupWithManager(mgr)
	Expect(err).ToNot(HaveOccurred())

	ctx, cancel = context.WithCancel(context.TODO())

	By("Create profile namespace")
	ns := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNamespace,
		},
	}
	Expect(k8sClient.Create(ctx, &ns)).Should(Succeed())

	go func() {
		defer GinkgoRecover()
		err = mgr.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()
})

var _ = AfterSuite(func() {
	defer klog.Flush()

	// Only attempt to clean up if we actually set up the test environment
	if testEnv != nil && k8sClient != nil {
		By("delete profile namespace")
		ns := corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		}
		// Best effort deletion - don't fail if this doesn't work
		_ = k8sClient.Delete(ctx, &ns)
	}

	if cancel != nil {
		cancel()
	}

	By("tearing down the test environment")
	if testEnv != nil {
		err := testEnv.Stop()
		Expect(err).NotTo(HaveOccurred())
	}

	generateAzureTrafficManagerProfileNameFunc = originalGenerateAzureTrafficManagerProfileNameFunc
})
