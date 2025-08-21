/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1beta1

import (
	"context"
	"go/build"
	"log"
	"os"
	"path/filepath"
	"testing"

	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/config"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	fleetnetv1beta1 "go.goms.io/fleet-networking/api/v1beta1"
)

var (
	memberTestEnv *envtest.Environment
	hubTestEnv    *envtest.Environment
	memberClient  client.Client
	hubClient     client.Client
	ctx           context.Context
	cancel        context.CancelFunc
)

var (
	mcsAgentType                 = clusterv1beta1.MultiClusterServiceAgent
	serviceExportImportAgentType = clusterv1beta1.ServiceExportImportAgent
)

func TestMain(m *testing.M) {
	// Add custom APIs to the runtime scheme.
	if err := fleetnetv1alpha1.AddToScheme(scheme.Scheme); err != nil {
		log.Fatalf("failed to add custom APIs to the runtime scheme: %v", err)
	}
	if err := fleetnetv1beta1.AddToScheme(scheme.Scheme); err != nil {
		log.Fatalf("failed to add custom APIs to the runtime scheme: %v", err)
	}
	if err := clusterv1beta1.AddToScheme(scheme.Scheme); err != nil {
		log.Fatalf("failed to add custom APIs to the runtime scheme: %v", err)
	}

	os.Exit(m.Run())
}

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "EndpointSlice Controller Suite")
}

var _ = BeforeSuite(func() {
	klog.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrap the test environment")

	// Start the clusters.
	memberTestEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	memberCfg, err := memberTestEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(memberCfg).NotTo(BeNil())

	hubTestEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "..", "..", "config", "crd", "bases"),
			// The package name must match with the version of the fleet package in use.
			filepath.Join(build.Default.GOPATH, "pkg", "mod", "go.goms.io", "fleet@v0.14.0", "config", "crd", "bases"),
		},

		ErrorIfCRDPathMissing: true,
	}
	hubCfg, err := hubTestEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(hubCfg).NotTo(BeNil())

	Expect(clusterv1beta1.AddToScheme(scheme.Scheme)).To(Succeed())

	// Set up clients for member and hub clusters.
	memberClient, err = client.New(memberCfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(memberClient).NotTo(BeNil())
	hubClient, err = client.New(hubCfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(hubClient).NotTo(BeNil())

	// Start up the controllers.
	ctrlMgr, err := ctrl.NewManager(hubCfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
		Controller: config.Controller{
			SkipNameValidation: ptr.To(true),
		},
	})
	Expect(err).NotTo(HaveOccurred())

	err = (&Reconciler{
		MemberClient:              memberClient,
		HubClient:                 hubClient,
		AgentType:                 mcsAgentType,
		EnabledNetworkingFeatures: true,
	}).SetupWithManager(ctrlMgr)
	Expect(err).NotTo(HaveOccurred())

	err = (&Reconciler{
		MemberClient:              memberClient,
		HubClient:                 hubClient,
		AgentType:                 serviceExportImportAgentType,
		EnabledNetworkingFeatures: true,
	}).SetupWithManager(ctrlMgr)
	Expect(err).NotTo(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err := ctrlMgr.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "Failed to start controller manager")
	}()
})

var _ = AfterSuite(func() {
	defer klog.Flush()
	By("stop the controller manager")
	cancel()

	By("tear down the test environment")
	Expect(memberTestEnv.Stop()).Should(Succeed())
	Expect(hubTestEnv.Stop()).Should(Succeed())
})
