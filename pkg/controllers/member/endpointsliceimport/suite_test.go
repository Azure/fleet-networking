/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package endpointsliceimport

import (
	"context"
	"path/filepath"
	"testing"

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
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

var (
	memberTestEnv *envtest.Environment
	hubTestEnv    *envtest.Environment
	memberClient  client.Client
	hubClient     client.Client
	ctx           context.Context
	cancel        context.CancelFunc
	reconciler    *Reconciler
)

// setUpResources help set up resources in the test environment.
func setUpResources() {
	// Add the namespaces.
	memberNS := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: memberUserNS,
		},
	}
	Expect(memberClient.Create(ctx, &memberNS)).Should(Succeed())
	fleetNS := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: fleetSystemNS,
		},
	}
	Expect(memberClient.Create(ctx, &fleetNS)).Should(Succeed())

	hubNS := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: hubNSForMember,
		},
	}
	Expect(hubClient.Create(ctx, &hubNS)).Should(Succeed())
}

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "EndpointSliceImport Controller Suite")
}

var _ = BeforeSuite(func() {
	klog.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrap the test environment")

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

	// Start up the InternalServiceExport controller.
	memberCtrlMgr, err := ctrl.NewManager(memberCfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	Expect(err).NotTo(HaveOccurred())

	hubCtrlMgr, err := ctrl.NewManager(hubCfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	Expect(err).NotTo(HaveOccurred())

	// Set up the clients for the member and hub clusters.
	// The clients must be ones with caches (i.e. configured by the controller managers) so as to make use
	// of the cache indexes.
	memberClient = memberCtrlMgr.GetClient()
	Expect(memberClient).NotTo(BeNil())
	hubClient = hubCtrlMgr.GetClient()
	Expect(hubClient).NotTo(BeNil())

	reconciler = &Reconciler{
		MemberClient:         memberClient,
		HubClient:            hubClient,
		FleetSystemNamespace: fleetSystemNS,
	}
	err = reconciler.SetupWithManager(ctx, memberCtrlMgr, hubCtrlMgr)
	Expect(err).NotTo(HaveOccurred())
	Expect(reconciler.Join(ctx)).Should(Succeed())

	go func() {
		defer GinkgoRecover()
		err := memberCtrlMgr.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to start manager for member controllers")
	}()

	go func() {
		defer GinkgoRecover()
		err = hubCtrlMgr.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to start manager for hub controllers")
	}()

	// Set up resources; this happens at last as the clients used are prepared by the controller managers.
	setUpResources()
})

var _ = AfterSuite(func() {
	defer klog.Flush()
	Expect(reconciler.Leave(ctx)).Should(Succeed())
	cancel()

	By("tearing down the test environment")
	Expect(memberTestEnv.Stop()).Should(Succeed())
	Expect(hubTestEnv.Stop()).Should(Succeed())
})
