/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package trafficmanagerbackend

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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/test/common/trafficmanager/fakeprovider"
)

var (
	cfg       *rest.Config
	mgr       manager.Manager
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc
)

const (
	testNamespace = "backend-ns"
)

var (
	originalGenerateAzureTrafficManagerProfileNameFunc        = generateAzureTrafficManagerProfileNameFunc
	originalGenerateAzureTrafficManagerEndpointNamePrefixFunc = generateAzureTrafficManagerEndpointNamePrefixFunc
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "TrafficManagerBackend Controller Suite")
}

var _ = BeforeSuite(func() {
	klog.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("../../../../", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = fleetnetv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme
	By("construct the k8s client")
	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	By("starting the controller manager")
	klog.InitFlags(flag.CommandLine)
	flag.Parse()

	mgr, err = ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	Expect(err).NotTo(HaveOccurred())

	profileClient, err := fakeprovider.NewProfileClient("default-sub")
	Expect(err).Should(Succeed(), "failed to create the fake profile client")

	endpointClient, err := fakeprovider.NewEndpointsClient("default-sub")
	Expect(err).Should(Succeed(), "failed to create the fake endpoint client")

	generateAzureTrafficManagerProfileNameFunc = func(profile *fleetnetv1alpha1.TrafficManagerProfile) string {
		return profile.Name
	}
	generateAzureTrafficManagerEndpointNamePrefixFunc = func(backend *fleetnetv1alpha1.TrafficManagerBackend) string {
		return backend.Name
	}

	ctx, cancel = context.WithCancel(context.TODO())
	err = (&Reconciler{
		Client:            mgr.GetClient(),
		ProfilesClient:    profileClient,
		EndpointsClient:   endpointClient,
		ResourceGroupName: fakeprovider.DefaultResourceGroupName,
	}).SetupWithManager(ctx, mgr)
	Expect(err).ToNot(HaveOccurred())

	By("Create profile namespace")
	ns := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNamespace,
		},
	}
	Expect(k8sClient.Create(ctx, &ns)).Should(Succeed())

	go func() {
		defer GinkgoRecover()
		err = mgr.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()
})

var _ = AfterSuite(func() {
	defer klog.Flush()

	By("delete profile namespace")
	ns := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNamespace,
		},
	}
	Expect(k8sClient.Delete(ctx, &ns)).Should(Succeed())

	cancel()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())

	generateAzureTrafficManagerProfileNameFunc = originalGenerateAzureTrafficManagerProfileNameFunc
	generateAzureTrafficManagerEndpointNamePrefixFunc = originalGenerateAzureTrafficManagerEndpointNamePrefixFunc
})
