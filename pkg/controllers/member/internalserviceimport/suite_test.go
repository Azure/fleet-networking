package internalserviceimport

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
	"k8s.io/klog/v2/klogr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	// +kubebuilder:scaffold:imports

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

var (
	memberConfig  *rest.Config
	hubConfig     *rest.Config
	mgr           manager.Manager
	memberClient  client.Client
	hubClient     client.Client
	memberTestEnv *envtest.Environment
	hubTestEnv    *envtest.Environment
	ctx           context.Context
	cancel        context.CancelFunc
)

const (
	testNamespace      = "my-ns"
	testName           = "my-svc"
	testFleetNamespace = "member-cluster-a"
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "InternalServiceImport Controller Suite")
}

var _ = BeforeSuite(func() {
	klog.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")
	memberTestEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("../../../../", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	memberConfig, err = memberTestEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(memberConfig).NotTo(BeNil())

	hubTestEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("../../../../", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	hubConfig, err = hubTestEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(hubConfig).NotTo(BeNil())

	err = fleetnetv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme
	By("construct the member k8s client")
	memberClient, err = client.New(memberConfig, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(memberClient).NotTo(BeNil())

	By("construct the hub k8s client")
	hubClient, err = client.New(hubConfig, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(hubClient).NotTo(BeNil())

	By("Create member namespace")
	ns := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNamespace,
		},
	}
	Expect(memberClient.Create(ctx, &ns)).Should(Succeed())

	By("Create fleet namespace")
	ns = corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testFleetNamespace,
		},
	}
	Expect(hubClient.Create(ctx, &ns)).Should(Succeed())

	By("starting the controller manager")
	klog.InitFlags(flag.CommandLine)
	flag.Parse()

	mgr, err = ctrl.NewManager(hubConfig, ctrl.Options{
		Scheme:             scheme.Scheme,
		MetricsBindAddress: "0",
		Logger:             klogr.NewWithOptions(klogr.WithFormat(klogr.FormatKlog)),
		Port:               4848,
	})
	Expect(err).NotTo(HaveOccurred())

	err = (&Reconciler{
		memberClient: memberClient,
		hubClient:    hubClient,
	}).SetupWithManager(mgr)
	Expect(err).ToNot(HaveOccurred())

	ctx, cancel = context.WithCancel(context.TODO())
	go func() {
		defer GinkgoRecover()
		err = mgr.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()
})

var _ = AfterSuite(func() {
	defer klog.Flush()

	cancel()
	By("tearing down the test environment")
	Expect(memberTestEnv.Stop()).Should(Succeed())
	Expect(hubTestEnv.Stop()).Should(Succeed())
})
