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
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// setUpResources help set up resources in the test environment.
func setUpResources() {
	mc := clusterv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:       memberClusterName,
			Finalizers: []string{"test-member-cluster-cleanup-finalizer"},
		},
		Spec: clusterv1beta1.MemberClusterSpec{
			Identity: rbacv1.Subject{
				Name:      "test-subject",
				Kind:      "ServiceAccount",
				Namespace: "fleet-system",
				APIGroup:  "",
			},
		},
	}
	Expect(hubClient.Create(ctx, &mc)).Should(Succeed())

	// Create the fleet member namespace.
	memberNS := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: fleetMemberNS,
		},
	}
	Expect(hubClient.Create(ctx, &memberNS)).Should(Succeed())

	// Create the EndpointSliceImport.
	esi := &fleetnetv1alpha1.EndpointSliceImport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  fleetMemberNS,
			Name:       endpointSliceImportName,
			Finalizers: []string{"networking.fleet.azure.com/endpointsliceimport-cleanup"},
		},
		Spec: fleetnetv1alpha1.EndpointSliceExportSpec{
			AddressType: discoveryv1.AddressTypeIPv4,
			Endpoints: []fleetnetv1alpha1.Endpoint{
				{
					Addresses: []string{"1.2.3.4"},
				},
			},
			EndpointSliceReference: fleetnetv1alpha1.ExportedObjectReference{
				ClusterID:       memberClusterName,
				Kind:            "EndpointSlice",
				Namespace:       fleetMemberNS,
				Name:            "test-endpoint-slice",
				ResourceVersion: "0",
				Generation:      1,
				UID:             "00000000-0000-0000-0000-000000000000",
				ExportedSince:   metav1.NewTime(time.Now().Round(time.Second)),
			},
			OwnerServiceReference: fleetnetv1alpha1.OwnerServiceReference{
				Namespace: "work",
				Name:      "test-service",
			},
		},
	}
	Expect(hubClient.Create(ctx, esi)).Should(Succeed())
}

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

	// Set up resources; this happens at last as the client used is prepared by the controller manager.
	setUpResources()
})

var _ = AfterSuite(func() {
	defer klog.Flush()
	cancel()

	By("tearing down the test environment")
	Expect(hubTestEnv.Stop()).Should(Succeed())
})
