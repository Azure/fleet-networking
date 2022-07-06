/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package endpointsliceexport

import (
	"context"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

var (
	memberTestEnv *envtest.Environment
	hubTestEnv    *envtest.Environment
	memberClient  client.Client
	hubClient     client.Client
	ctx           context.Context
	cancel        context.CancelFunc
)

// setUpResources help set up resources in the test environment.
func setUpResources() {
	// Add the namespaces
	memberNS := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: memberUserNS,
		},
	}
	Expect(memberClient.Create(ctx, &memberNS)).Should(Succeed())

	hubNS := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: hubNSForMember,
		},
	}
	Expect(hubClient.Create(ctx, &hubNS)).Should(Succeed())
}

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t, "EndpointSliceExport Controller Suite", []Reporter{printer.NewlineReporter{}})
}

var _ = BeforeSuite(func() {
	log.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

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

	// Set up clients for member and hub clusters.
	memberClient, err = client.New(memberCfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(memberClient).NotTo(BeNil())
	hubClient, err = client.New(hubCfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(hubClient).NotTo(BeNil())

	// Set up resources.
	setUpResources()

	// Start up the InternalServiceExport controller.
	ctrlMgr, err := ctrl.NewManager(hubCfg, ctrl.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())

	err = (&Reconciler{
		memberClient: memberClient,
		hubClient:    hubClient,
	}).SetupWithManager(ctrlMgr)
	Expect(err).NotTo(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err := ctrlMgr.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to start manager")
	}()
}, 60)

var _ = AfterSuite(func() {
	cancel()

	By("tearing down the test environment")
	Expect(memberTestEnv.Stop()).Should(Succeed())
	Expect(hubTestEnv.Stop()).Should(Succeed())
}, 60)
