/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package internalserviceimport

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

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

var (
	hubTestEnv *envtest.Environment
	hubClient  client.Client
	ctx        context.Context
	cancel     context.CancelFunc
)

// setUpResources help set up resources in the test environment.
func setUpResources() {
	// Add the namespaces.
	hubNSA := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: hubNSForMemberA,
		},
	}
	Expect(hubClient.Create(ctx, &hubNSA)).Should(Succeed())

	hubNSB := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: hubNSForMemberB,
		},
	}
	Expect(hubClient.Create(ctx, &hubNSB)).Should(Succeed())

	hubNSC := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: hubNSForMemberC,
		},
	}
	Expect(hubClient.Create(ctx, &hubNSC)).Should(Succeed())

	memberNSInHub := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: memberUserNS,
		},
	}
	Expect(hubClient.Create(ctx, &memberNSInHub)).Should(Succeed())
}

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "InternalServiceImport Controller (Hub) Suite")
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

	// Start up the EndpointSliceExport controller.
	hubCtrlMgr, err := ctrl.NewManager(hubCfg, ctrl.Options{
		Scheme:             scheme.Scheme,
		MetricsBindAddress: "0",
	})
	Expect(err).NotTo(HaveOccurred())

	// Set up the client.
	// The client must be one with cache (i.e. configured by the controller manager) so as to make use
	// of the cache indexes.
	hubClient = hubCtrlMgr.GetClient()
	Expect(hubClient).NotTo(BeNil())

	err = (&Reconciler{
		HubClient: hubClient,
	}).SetupWithManager(ctx, hubCtrlMgr)
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
