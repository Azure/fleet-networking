/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package trafficmanagerbackend

import (
	"context"
	"flag"
	"fmt"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log"
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

var (
	originalGenerateAzureTrafficManagerProfileNameFunc        = generateAzureTrafficManagerProfileNameFunc
	originalGenerateAzureTrafficManagerEndpointNamePrefixFunc = generateAzureTrafficManagerEndpointNamePrefixFunc

	memberClusterNames     = []string{fakeprovider.ClusterName, "member-2", "member-3", "member-4", fakeprovider.CreateBadRequestErrEndpointClusterName, fakeprovider.CreateInternalServerErrEndpointClusterName}
	internalServiceExports = []fleetnetv1alpha1.InternalServiceExport{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valid-endpoint",
				Namespace: memberClusterNames[0],
			},
			Spec: fleetnetv1alpha1.InternalServiceExportSpec{
				Ports: []fleetnetv1alpha1.ServicePort{
					{
						Name:       "portA",
						Protocol:   "TCP",
						Port:       8080,
						TargetPort: intstr.IntOrString{IntVal: 8080},
					},
				},
				ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
					ClusterID:       memberClusterNames[0],
					Kind:            "Service",
					Namespace:       testNamespace,
					Name:            serviceName,
					ResourceVersion: "0",
					Generation:      0,
					UID:             "0",
					NamespacedName:  fmt.Sprintf("%s/%s", testNamespace, serviceName),
				},
				Type:                 corev1.ServiceTypeLoadBalancer,
				IsDNSLabelConfigured: true,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "invalid-endpoint",
				Namespace: memberClusterNames[1],
			},
			Spec: fleetnetv1alpha1.InternalServiceExportSpec{
				Ports: []fleetnetv1alpha1.ServicePort{
					{
						Name:       "portA",
						Protocol:   "TCP",
						Port:       8080,
						TargetPort: intstr.IntOrString{IntVal: 8080},
					},
				},
				ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
					ClusterID:       memberClusterNames[1],
					Kind:            "Service",
					Namespace:       testNamespace,
					Name:            serviceName,
					ResourceVersion: "0",
					Generation:      0,
					UID:             "0",
					NamespacedName:  fmt.Sprintf("%s/%s", testNamespace, serviceName),
				},
				Type: corev1.ServiceTypeLoadBalancer,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "other-service",
				Namespace: memberClusterNames[2],
			},
			Spec: fleetnetv1alpha1.InternalServiceExportSpec{
				Ports: []fleetnetv1alpha1.ServicePort{
					{
						Name:       "portA",
						Protocol:   "TCP",
						Port:       8080,
						TargetPort: intstr.IntOrString{IntVal: 8080},
					},
				},
				ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
					ClusterID:       memberClusterNames[2],
					Kind:            "Service",
					Namespace:       testNamespace,
					Name:            "other-service",
					ResourceVersion: "0",
					Generation:      0,
					UID:             "0",
					NamespacedName:  fmt.Sprintf("%s/%s", testNamespace, "other-service"),
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "endpoint-not-in-the-atm-profile",
				Namespace: memberClusterNames[3],
			},
			Spec: fleetnetv1alpha1.InternalServiceExportSpec{
				Ports: []fleetnetv1alpha1.ServicePort{
					{
						Name:       "portA",
						Protocol:   "TCP",
						Port:       8080,
						TargetPort: intstr.IntOrString{IntVal: 8080},
					},
				},
				ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
					ClusterID:       memberClusterNames[3],
					Kind:            "Service",
					Namespace:       testNamespace,
					Name:            serviceName,
					ResourceVersion: "0",
					Generation:      0,
					UID:             "0",
					NamespacedName:  fmt.Sprintf("%s/%s", testNamespace, serviceName),
				},
				Type:                 corev1.ServiceTypeLoadBalancer,
				IsDNSLabelConfigured: true,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "endpoint-create-bad-request-err",
				Namespace: memberClusterNames[4],
			},
			Spec: fleetnetv1alpha1.InternalServiceExportSpec{
				Ports: []fleetnetv1alpha1.ServicePort{
					{
						Name:       "portA",
						Protocol:   "TCP",
						Port:       8080,
						TargetPort: intstr.IntOrString{IntVal: 8080},
					},
				},
				ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
					ClusterID:       memberClusterNames[4],
					Kind:            "Service",
					Namespace:       testNamespace,
					Name:            serviceName,
					ResourceVersion: "0",
					Generation:      0,
					UID:             "0",
					NamespacedName:  fmt.Sprintf("%s/%s", testNamespace, serviceName),
				},
				Type:                 corev1.ServiceTypeLoadBalancer,
				IsDNSLabelConfigured: true,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "endpoint-create-internal-server-err",
				Namespace: memberClusterNames[5],
			},
			Spec: fleetnetv1alpha1.InternalServiceExportSpec{
				Ports: []fleetnetv1alpha1.ServicePort{
					{
						Name:       "portA",
						Protocol:   "TCP",
						Port:       8080,
						TargetPort: intstr.IntOrString{IntVal: 8080},
					},
				},
				ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
					ClusterID:       memberClusterNames[5],
					Kind:            "Service",
					Namespace:       testNamespace,
					Name:            serviceName,
					ResourceVersion: "0",
					Generation:      0,
					UID:             "0",
					NamespacedName:  fmt.Sprintf("%s/%s", testNamespace, serviceName),
				},
				Type:                 corev1.ServiceTypeLoadBalancer,
				IsDNSLabelConfigured: true,
			},
		},
	}
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "TrafficManagerBackend Controller Suite")
}

var _ = BeforeSuite(func() {
	logger := zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true))
	klog.SetLogger(logger)
	log.SetLogger(logger)

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
		return backend.Name + "#"
	}

	ctx, cancel = context.WithCancel(context.TODO())
	err = (&Reconciler{
		Client:            mgr.GetClient(),
		ProfilesClient:    profileClient,
		EndpointsClient:   endpointClient,
		ResourceGroupName: fakeprovider.DefaultResourceGroupName,
	}).SetupWithManager(ctx, mgr, false)
	Expect(err).ToNot(HaveOccurred())

	By("Create profile namespace")
	ns := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNamespace,
		},
	}
	Expect(k8sClient.Create(ctx, &ns)).Should(Succeed())

	for i, name := range memberClusterNames {
		By(fmt.Sprintf("Create member cluster system namespace %v", name))
		ns = corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
		}
		Expect(k8sClient.Create(ctx, &ns)).Should(Succeed())

		By(fmt.Sprintf("Create internalServiceExport %v", internalServiceExports[i].Name))
		Expect(k8sClient.Create(ctx, &internalServiceExports[i])).Should(Succeed(), "failed to create internalServiceExport")
	}

	go func() {
		defer GinkgoRecover()
		err = mgr.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()
})

var _ = AfterSuite(func() {
	defer klog.Flush()

	for i, name := range memberClusterNames {
		By(fmt.Sprintf("Delete internalServiceExport %v", internalServiceExports[i].Name))
		Expect(k8sClient.Delete(ctx, &internalServiceExports[i])).Should(Succeed(), "failed to delete internalServiceExport")

		By(fmt.Sprintf("Delete member cluster system namespace %v", name))
		ns := corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
		}
		Expect(k8sClient.Delete(ctx, &ns)).Should(Succeed())
	}

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
