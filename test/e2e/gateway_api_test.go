//go:build e2e

/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/annotations"
	"go.goms.io/fleet-networking/test/e2e/framework"
)

const gatewayControllerName = gatewayv1.GatewayController("networking.fleet.azure.com/afd")

// This suite validates the live API contract only; AFD reconciliation is not implemented yet.
var _ = Describe("Gateway API contract", Ordered, func() {
	var (
		backendNamespace string
		gatewayClassName string
		gatewayName      = "afd-gateway"
	)

	createWithCleanup := func(obj client.Object) {
		GinkgoHelper()
		Expect(hubCluster.Client().Create(ctx, obj)).To(Succeed())
		key := client.ObjectKeyFromObject(obj)
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(hubCluster.Client().Delete(ctx, obj))).To(Succeed())
			Eventually(func() bool {
				probe := obj.DeepCopyObject().(client.Object)
				return errors.IsNotFound(hubCluster.Client().Get(ctx, key, probe))
			}, framework.PollTimeout, framework.PollInterval).Should(BeTrue(), "Failed to delete %T %s", obj, key)
		})
	}

	createExportWithCleanup := func(export *fleetnetv1alpha1.InternalServiceExport) {
		GinkgoHelper()
		Expect(hubCluster.Client().Create(ctx, export)).To(Succeed())
		exportKey := client.ObjectKeyFromObject(export)
		serviceImportKey := types.NamespacedName{
			Namespace: export.Spec.ServiceReference.Namespace,
			Name:      export.Spec.ServiceReference.Name,
		}
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(hubCluster.Client().Delete(ctx, export))).To(Succeed())
			Eventually(func() bool {
				probe := &fleetnetv1alpha1.InternalServiceExport{}
				return errors.IsNotFound(hubCluster.Client().Get(ctx, exportKey, probe))
			}, framework.PollTimeout, framework.PollInterval).Should(BeTrue(), "Failed to delete InternalServiceExport %s", exportKey)
			Eventually(func() bool {
				probe := &fleetnetv1alpha1.ServiceImport{}
				return errors.IsNotFound(hubCluster.Client().Get(ctx, serviceImportKey, probe))
			}, framework.PollTimeout, framework.PollInterval).Should(BeTrue(), "Failed to delete derived ServiceImport %s", serviceImportKey)
		})
	}

	waitForServiceImport := func(key types.NamespacedName) *fleetnetv1alpha1.ServiceImport {
		GinkgoHelper()
		serviceImport := &fleetnetv1alpha1.ServiceImport{}
		Eventually(func() bool {
			if err := hubCluster.Client().Get(ctx, key, serviceImport); err != nil {
				return false
			}
			return len(serviceImport.Status.Clusters) != 0
		}, framework.PollTimeout, framework.PollInterval).Should(BeTrue(), "Failed to get resolved ServiceImport %s", key)
		return serviceImport
	}

	annotateServiceImport := func(key types.NamespacedName, desired map[string]string) {
		GinkgoHelper()
		Eventually(func() error {
			serviceImport := &fleetnetv1alpha1.ServiceImport{}
			if err := hubCluster.Client().Get(ctx, key, serviceImport); err != nil {
				return err
			}
			if serviceImport.Annotations == nil {
				serviceImport.Annotations = make(map[string]string, len(desired))
			}
			for annotation, value := range desired {
				serviceImport.Annotations[annotation] = value
			}
			return hubCluster.Client().Update(ctx, serviceImport)
		}, framework.PollTimeout, framework.PollInterval).Should(Succeed(), "Failed to annotate ServiceImport %s", key)
	}

	BeforeAll(func() {
		backendNamespace = framework.UniqueTestNamespace()
		gatewayClassName = "afd-" + backendNamespace
		createWithCleanup(framework.Namespace(backendNamespace))
	})

	It("round-trips Gateway API resources with Fleet ServiceImport backends", func() {
		gatewayClass := framework.GatewayClass(gatewayClassName, gatewayControllerName)
		createWithCleanup(gatewayClass)

		gatewayAnnotations := map[string]string{
			annotations.AFDSKUAnnotation: string(annotations.SKUPremium),
		}
		gateway := framework.Gateway(testNamespace, gatewayName, gatewayClassName, "store.example.com", gatewayAnnotations)
		createWithCleanup(gateway)

		serviceImportAnnotations := map[string]string{
			annotations.AFDOriginConnectivityAnnotation: string(annotations.ConnectivityPrivateLink),
			annotations.AFDHealthProbePathAnnotation:    "/healthz",
			annotations.AFDOriginHostHeaderAnnotation:   "store.internal.example.com",
		}
		sameNamespaceExport := framework.InternalServiceExport(
			testNamespace, "store-same-export", testNamespace, "store-same", memberClusterNames[0], 8080,
		)
		createExportWithCleanup(sameNamespaceExport)
		sameNamespaceServiceImportKey := types.NamespacedName{Namespace: testNamespace, Name: "store-same"}
		sameNamespaceServiceImport := waitForServiceImport(sameNamespaceServiceImportKey)
		annotateServiceImport(sameNamespaceServiceImportKey, serviceImportAnnotations)

		crossNamespaceExport := framework.InternalServiceExport(
			backendNamespace, "store-cross-export", backendNamespace, "store-cross", memberClusterNames[1], 8080,
		)
		createExportWithCleanup(crossNamespaceExport)
		crossNamespaceServiceImportKey := types.NamespacedName{Namespace: backendNamespace, Name: "store-cross"}
		crossNamespaceServiceImport := waitForServiceImport(crossNamespaceServiceImportKey)
		annotateServiceImport(crossNamespaceServiceImportKey, serviceImportAnnotations)

		sameNamespaceRoute := framework.HTTPRouteToFleetServiceImport(
			testNamespace, "store-same", gatewayName, "", sameNamespaceServiceImport.Name, 8080, 70,
		)
		createWithCleanup(sameNamespaceRoute)

		referenceGrant := framework.ReferenceGrantForFleetServiceImport(
			backendNamespace, "allow-store-route", testNamespace, crossNamespaceServiceImport.Name,
		)
		createWithCleanup(referenceGrant)
		crossNamespaceRoute := framework.HTTPRouteToFleetServiceImport(
			testNamespace, "store-cross", gatewayName, backendNamespace, crossNamespaceServiceImport.Name, 8080, 30,
		)
		createWithCleanup(crossNamespaceRoute)

		By("validating the GatewayClass and Gateway contract")
		var gotGatewayClass gatewayv1.GatewayClass
		Expect(hubCluster.Client().Get(ctx, types.NamespacedName{Name: gatewayClassName}, &gotGatewayClass)).To(Succeed())
		Expect(gotGatewayClass.Spec.ControllerName).To(Equal(gatewayControllerName))

		var gotGateway gatewayv1.Gateway
		Expect(hubCluster.Client().Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: gatewayName}, &gotGateway)).To(Succeed())
		Expect(gotGateway.Spec.GatewayClassName).To(Equal(gatewayv1.ObjectName(gatewayClassName)))
		Expect(gotGateway.Annotations).To(HaveKeyWithValue(annotations.AFDSKUAnnotation, string(annotations.SKUPremium)))
		Expect(gotGateway.Spec.Listeners).To(HaveLen(1))
		Expect(gotGateway.Spec.Listeners[0].Protocol).To(Equal(gatewayv1.HTTPProtocolType))

		By("validating the same-namespace Fleet ServiceImport backend reference")
		var gotSameNamespaceRoute gatewayv1.HTTPRoute
		Expect(hubCluster.Client().Get(
			ctx,
			types.NamespacedName{Namespace: sameNamespaceRoute.Namespace, Name: sameNamespaceRoute.Name},
			&gotSameNamespaceRoute,
		)).To(Succeed())
		assertFleetServiceImportBackend(gotSameNamespaceRoute, gatewayName, "", sameNamespaceServiceImport.Name, 8080, 70)

		By("validating the cross-namespace backend and ReferenceGrant")
		var gotReferenceGrant gatewayv1beta1.ReferenceGrant
		Expect(hubCluster.Client().Get(
			ctx,
			types.NamespacedName{Namespace: referenceGrant.Namespace, Name: referenceGrant.Name},
			&gotReferenceGrant,
		)).To(Succeed())
		Expect(gotReferenceGrant.Spec.From).To(Equal(referenceGrant.Spec.From))
		Expect(gotReferenceGrant.Spec.To).To(Equal(referenceGrant.Spec.To))

		var gotCrossNamespaceRoute gatewayv1.HTTPRoute
		Expect(hubCluster.Client().Get(
			ctx,
			types.NamespacedName{Namespace: crossNamespaceRoute.Namespace, Name: crossNamespaceRoute.Name},
			&gotCrossNamespaceRoute,
		)).To(Succeed())
		assertFleetServiceImportBackend(gotCrossNamespaceRoute, gatewayName, backendNamespace, crossNamespaceServiceImport.Name, 8080, 30)

		By("validating the Fleet ServiceImport API version and annotations")
		var serviceImportCRD apiextensionsv1.CustomResourceDefinition
		Expect(hubCluster.Client().Get(
			ctx,
			types.NamespacedName{Name: "serviceimports.networking.fleet.azure.com"},
			&serviceImportCRD,
		)).To(Succeed())
		Expect(serviceImportCRD.Spec.Group).To(Equal(fleetnetv1alpha1.GroupVersion.Group))
		Expect(serviceImportCRD.Spec.Versions).To(ContainElement(And(
			HaveField("Name", fleetnetv1alpha1.GroupVersion.Version),
			HaveField("Served", true),
			HaveField("Storage", true),
		)))

		var gotServiceImport fleetnetv1alpha1.ServiceImport
		Expect(hubCluster.Client().Get(ctx, crossNamespaceServiceImportKey, &gotServiceImport)).To(Succeed())
		for annotation, value := range serviceImportAnnotations {
			Expect(gotServiceImport.Annotations).To(HaveKeyWithValue(annotation, value))
		}
	})
})

func assertFleetServiceImportBackend(
	route gatewayv1.HTTPRoute,
	gatewayName, namespace, name string,
	port gatewayv1.PortNumber,
	weight int32,
) {
	GinkgoHelper()
	Expect(route.Spec.ParentRefs).To(HaveLen(1))
	Expect(route.Spec.ParentRefs[0].Name).To(Equal(gatewayv1.ObjectName(gatewayName)))
	Expect(route.Spec.Rules).To(HaveLen(1))
	Expect(route.Spec.Rules[0].BackendRefs).To(HaveLen(1))

	backendRef := route.Spec.Rules[0].BackendRefs[0].BackendRef
	Expect(backendRef.Group).NotTo(BeNil())
	Expect(string(*backendRef.Group)).To(Equal(fleetnetv1alpha1.GroupVersion.Group))
	Expect(backendRef.Kind).NotTo(BeNil())
	Expect(string(*backendRef.Kind)).To(Equal("ServiceImport"))
	Expect(string(backendRef.Name)).To(Equal(name))
	Expect(backendRef.Port).NotTo(BeNil())
	Expect(*backendRef.Port).To(Equal(port))
	Expect(backendRef.Weight).NotTo(BeNil())
	Expect(*backendRef.Weight).To(Equal(weight))
	if namespace == "" {
		Expect(backendRef.Namespace).To(BeNil())
		return
	}
	Expect(backendRef.Namespace).NotTo(BeNil())
	Expect(string(*backendRef.Namespace)).To(Equal(namespace))
}
