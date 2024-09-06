package membercluster

import (
	"context"
	"flag"
	"go/build"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/textlogger"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
)

const (
	memberClusterName = "member-1"
	fleetMemberNS     = "fleet-member-member-1"

	endpointSliceImportName = "test-endpoint-slice-import"
)

var (
	hubTestEnv *envtest.Environment
	hubClient  client.Client
	ctx        context.Context
	cancel     context.CancelFunc
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "MemberCluster Controller (Hub) Suite")
}

var _ = BeforeSuite(func() {
	By("Setup klog")
	fs := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(fs)
	Expect(fs.Parse([]string{"--v", "5", "-add_dir_header", "true"})).Should(Succeed())

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrap the test environment")
	// Start the cluster.
	hubTestEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "..", "config", "crd", "bases"),
			filepath.Join(build.Default.GOPATH, "pkg", "mod", "go.goms.io", "fleet@v0.10.5", "config", "crd", "bases", "cluster.kubernetes-fleet.io_memberclusters.yaml"),
		},
		ErrorIfCRDPathMissing: true,
	}
	hubCfg, err := hubTestEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(hubCfg).NotTo(BeNil())

	// Add custom APIs to the runtime scheme.
	Expect(fleetnetv1alpha1.AddToScheme(scheme.Scheme)).Should(Succeed())
	Expect(clusterv1beta1.AddToScheme(scheme.Scheme)).Should(Succeed())

	// Start up the EndpointSliceExport controller.
	klog.InitFlags(flag.CommandLine)
	flag.Parse()
	hubCtrlMgr, err := ctrl.NewManager(hubCfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
		Logger: textlogger.NewLogger(textlogger.NewConfig(textlogger.Verbosity(4))),
	})
	Expect(err).NotTo(HaveOccurred())

	// Set up the client.
	// The client must be one with cache (i.e. configured by the controller manager) so as to make use
	// of the cache indexes.
	hubClient = hubCtrlMgr.GetClient()
	Expect(hubClient).NotTo(BeNil())

	err = (&Reconciler{
		Client:              hubClient,
		ForceDeleteWaitTime: 1 * time.Minute,
	}).SetupWithManager(hubCtrlMgr)
	Expect(err).NotTo(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = hubCtrlMgr.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to start manager for hub controllers")
	}()
})

var _ = AfterSuite(func() {
	defer klog.Flush()
	cancel()

	By("tearing down the test environment")
	Expect(hubTestEnv.Stop()).Should(Succeed())
})
