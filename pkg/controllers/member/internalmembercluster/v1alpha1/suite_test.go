/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package internalmembercluster

import (
	"context"
	"flag"
	"go/build"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	// +kubebuilder:scaffold:imports

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

var (
	memberCfg     *rest.Config
	hubCfg        *rest.Config
	memberTestEnv *envtest.Environment
	hubTestEnv    *envtest.Environment
	memberClient  client.Client
	hubClient     client.Client
)

const (
	memberClusterNamespace = "fleet-system-member-cluster-a"
	memberClusterName      = "member-cluster-a"
)

var (
	namespaceList = []corev1.Namespace{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "mcs-ns-1",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "mcs-ns-2",
			},
		},
	}
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "InternalMemberCluster Controller Suite")
}

var _ = BeforeSuite(func() {
	klog.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	memberTestEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("../../../../../", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	var err error
	memberCfg, err = memberTestEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(memberCfg).NotTo(BeNil())

	hubTestEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("../../../../../", "config", "crd", "bases"),
			// need to make sure the version matches the one in the go.mod
			// workaround mentioned in https://github.com/kubernetes-sigs/controller-runtime/issues/1191
			filepath.Join(build.Default.GOPATH, "pkg", "mod", "go.goms.io", "fleet@v0.10.10", "config", "crd", "bases"),
		},
		ErrorIfCRDPathMissing: true,
	}
	hubCfg, err = hubTestEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(hubCfg).NotTo(BeNil())

	Expect(fleetv1alpha1.AddToScheme(scheme.Scheme)).Should(Succeed())
	Expect(fleetnetv1alpha1.AddToScheme(scheme.Scheme)).Should(Succeed())

	//+kubebuilder:scaffold:scheme
	By("construct the k8s member client")
	memberClient, err = client.New(memberCfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(memberClient).NotTo(BeNil())

	By("construct the k8s hub client")
	hubClient, err = client.New(hubCfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(hubClient).NotTo(BeNil())

	By("create member namespace")
	ns := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: memberClusterNamespace,
		},
	}
	ctx := context.Background()
	Expect(hubClient.Create(ctx, &ns)).Should(Succeed())

	By("Creating service namespaces")
	for i := range namespaceList {
		Expect(memberClient.Create(ctx, &namespaceList[i])).Should(Succeed())
	}

	klog.InitFlags(flag.CommandLine)
	flag.Parse()
})

var _ = AfterSuite(func() {
	defer klog.Flush()
	By("tearing down the test environment")
	err := memberTestEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
	err = hubTestEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
