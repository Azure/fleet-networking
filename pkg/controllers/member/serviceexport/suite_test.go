/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package serviceexport

import (
	"context"
	"flag"
	"path/filepath"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
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

	hubNS := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: hubNSForMember,
		},
	}
	Expect(hubClient.Create(ctx, &hubNS)).Should(Succeed())
}

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "ServiceExport Controller Suite")
}

var _ = BeforeSuite(func() {
	By("Setup klog")
	var err error
	fs := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(fs)
	Expect(fs.Parse([]string{"--v", "5", "-add_dir_header", "true"})).Should(Succeed())

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
	ctrlMgr, err := ctrl.NewManager(memberCfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	Expect(err).NotTo(HaveOccurred())

	publicIPAddressListResponse := []*armnetwork.PublicIPAddress{
		{
			Properties: &armnetwork.PublicIPAddressPropertiesFormat{
				DNSSettings: &armnetwork.PublicIPAddressDNSSettings{
					DomainNameLabel: ptr.To("dnsLabel"),
				},
				IPAddress: ptr.To(testIngressIP),
			},
			ID: ptr.To(testPublicIPResourceID),
		},
		{
			Properties: &armnetwork.PublicIPAddressPropertiesFormat{
				IPAddress: ptr.To("1.2.5.6"),
			},
		},
	}

	err = (&Reconciler{
		MemberClusterID:             memberClusterID,
		MemberClient:                memberClient,
		HubClient:                   hubClient,
		HubNamespace:                hubNSForMember,
		Recorder:                    ctrlMgr.GetEventRecorderFor(ControllerName),
		AzurePublicIPAddressClient:  &fakePublicIPAddressClient{ListResponse: publicIPAddressListResponse},
		ResourceGroupName:           validResourceGroup,
		EnableTrafficManagerFeature: true,
	}).SetupWithManager(ctrlMgr)
	Expect(err).NotTo(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err := ctrlMgr.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to start manager")
	}()
})

var _ = AfterSuite(func() {
	defer klog.Flush()
	cancel()

	By("tearing down the test environment")
	Expect(memberTestEnv.Stop()).Should(Succeed())
	Expect(hubTestEnv.Stop()).Should(Succeed())
})
