package serviceimport

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	// +kubebuilder:scaffold:imports

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	memberTestEnv *envtest.Environment
	hubTestEnv    *envtest.Environment
	memberClient  client.Client
	hubClient     client.Client
	ctx           context.Context
	cancel        context.CancelFunc
)

const (
	MemberClusterID     = "fake-member-cluster-id"
	HubNamespace        = "fake-hub-namespace"
	testNamespacePrefix = "fake-test-namespace"
	timeout             = time.Second * 10
	duration            = time.Second * 10
	interval            = time.Millisecond * 250
)

func TestInterServiceImportAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "InternalServiceImport Controller Suite")
}

var _ = BeforeSuite(func() {
	klog.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")

	// Start the clusters.
	memberTestEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	memberCfg, err := memberTestEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(memberCfg).NotTo(BeNil())

	hubTestEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	hubCfg, err := hubTestEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(hubCfg).NotTo(BeNil())

	// Add custom APIs to the runtime scheme.
	Expect(fleetnetv1alpha1.AddToScheme(scheme.Scheme)).Should(Succeed())

	// Set up clients for member and hub clusters.
	memberClient, err = client.New(memberCfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(memberClient).NotTo(BeNil())
	hubClient, err = client.New(hubCfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(hubClient).NotTo(BeNil())

	By("starting the controller manager")

	mgr, err := ctrl.NewManager(memberCfg, ctrl.Options{
		Scheme:             scheme.Scheme,
		MetricsBindAddress: "0",
	})
	Expect(err).NotTo(HaveOccurred())

	err = (&Reconciler{
		MemberClusterID: MemberClusterID,
		HubNamespace:    HubNamespace,
		MemberClient:    memberClient,
		HubClient:       hubClient,
	}).SetupWithManager(mgr)
	Expect(err).ToNot(HaveOccurred())

	setupResources()

	ctx, cancel = context.WithCancel(context.TODO())
	go func() {
		defer GinkgoRecover()
		err = mgr.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()
})

func setupResources() {
	// member hub namespace is bound to
	By("Create member hub namespace")
	hubNS := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: HubNamespace,
		},
	}
	Expect(hubClient.Create(ctx, &hubNS)).Should(Succeed())
}

var _ = AfterSuite(func() {
	defer klog.Flush()
	cancel()

	By("tearing down the test environment")
	Expect(memberTestEnv.Stop()).Should(Succeed())
	Expect(hubTestEnv.Stop()).Should(Succeed())
})
