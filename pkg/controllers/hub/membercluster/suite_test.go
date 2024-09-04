package membercluster

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
)

var (
	hubTestEnv *envtest.Environment
	hubClient  client.Client
	ctx        context.Context
	cancel     context.CancelFunc
)

// fulfilledSvcInUseByAnnotation returns a fulfilled ServiceInUseBy for annotation use.
func fulfilledServiceInUseByAnnotation() *fleetnetv1alpha1.ServiceInUseBy {
	return &fleetnetv1alpha1.ServiceInUseBy{
		MemberClusters: map[fleetnetv1alpha1.ClusterNamespace]fleetnetv1alpha1.ClusterID{
			"fleet-member-member-1": "0",
		},
	}
}

// fulfilledSvcInUseByAnnotationString returns marshalled ServiceInUseBy data in the string form.
func fulfilledSvcInUseByAnnotationString() string {
	data, _ := json.Marshal(fulfilledServiceInUseByAnnotation())
	return string(data)
}

// setUpResources help set up resources in the test environment.
func setUpResources() {
	// Need to apply MC CRD.
	mc := clusterv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "member-1",
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

	// Add the namespaces.
	memberNS := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "fleet-member-member-1",
		},
	}
	Expect(hubClient.Create(ctx, &memberNS)).Should(Succeed())

	isi := fleetnetv1alpha1.ServiceImport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "fleet-member-member-1",
			Name:      "app",
			Annotations: map[string]string{
				objectmeta.ServiceImportAnnotationServiceInUseBy: fulfilledSvcInUseByAnnotationString(),
			},
			Finalizers: []string{"networking.fleet.azure.com/serviceimport-cleanup"},
		},
	}
	Expect(hubClient.Create(ctx, &isi)).Should(Succeed())
}

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "MemberCluster Controller (Hub) Suite")
}

var _ = BeforeSuite(func() {
	klog.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrap the test environment")

	// Start the clusters.
	hubTestEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	hubCfg, err := hubTestEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(hubCfg).NotTo(BeNil())

	// Add custom APIs to the runtime scheme.
	Expect(fleetnetv1alpha1.AddToScheme(scheme.Scheme)).Should(Succeed())
	Expect(clusterv1beta1.AddToScheme(scheme.Scheme)).Should(Succeed())

	// Start up the EndpointSliceExport controller.
	hubCtrlMgr, err := ctrl.NewManager(hubCfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	Expect(err).NotTo(HaveOccurred())

	// Set up the client.
	// The client must be one with cache (i.e. configured by the controller manager) so as to make use
	// of the cache indexes.
	hubClient = hubCtrlMgr.GetClient()
	Expect(hubClient).NotTo(BeNil())

	err = (&Reconciler{
		Client:              hubClient,
		ForceDeleteWaitTime: 2 * time.Minute,
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
