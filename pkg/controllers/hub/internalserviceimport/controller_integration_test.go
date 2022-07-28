/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package internalserviceimport

import (
	"time"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/meta"
)

const (
	eventuallyTimeout    = time.Second * 10
	eventuallyInterval   = time.Millisecond * 250
	consistentlyDuration = time.Millisecond * 1000
	consistentlyInterval = time.Millisecond * 150
)

// unfulfilledInternalServiceImport returns an unfulfilled InternalServiceImport.
func unfulfilledInternalServiceImport() *fleetnetv1alpha1.InternalServiceImport {
	svcImportForRef := unfulfilledAndRequestedServiceImport()

	return &fleetnetv1alpha1.InternalServiceImport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: hubNSForMemberA,
			Name:      internalSvcImportName,
		},
		Spec: fleetnetv1alpha1.InternalServiceImportSpec{
			ServiceImportReference: fleetnetv1alpha1.FromMetaObjects(clusterIDForMemberB,
				svcImportForRef.TypeMeta,
				svcImportForRef.ObjectMeta,
			),
		},
	}
}

// fulfillInternalServiceImport fulfills an InternalServiceImport.
func fulfillInternalServiceImport(internalSvcImport *fleetnetv1alpha1.InternalServiceImport) {
	internalSvcImport.Status = fleetnetv1alpha1.ServiceImportStatus{
		Type: fleetnetv1alpha1.ClusterSetIP,
		Ports: []fleetnetv1alpha1.ServicePort{
			{
				Name:        httpPortName,
				Protocol:    httpPortProtocol,
				AppProtocol: &httpPortAppProtocol,
				Port:        httpPort,
			},
			{
				Name:        udpPortName,
				Protocol:    udpPortProtocol,
				AppProtocol: &udpPortAppProtocol,
				Port:        udpPort,
			},
		},
		Clusters: []fleetnetv1alpha1.ClusterStatus{
			{
				Cluster: clusterIDForMemberA,
			},
			{
				Cluster: clusterIDForMemberB,
			},
			{
				Cluster: clusterIDForMemberC,
			},
		},
	}
}

// unfulfilledAndRequestedServiceImport returns an empty ServiceImport annotated with ServiceInUseBy data.
func unfulfilledAndRequestedServiceImport() *fleetnetv1alpha1.ServiceImport {
	return &fleetnetv1alpha1.ServiceImport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      svcName,
			Annotations: map[string]string{
				meta.ServiceInUseByAnnotationKey: fulfilledSvcInUseByAnnotationString(),
			},
		},
	}
}

// fulfillServiceImport fulfills a ServiceImport by updating its status.
func fulfillServiceImport(svcImport *fleetnetv1alpha1.ServiceImport) {
	svcImport.Status = fleetnetv1alpha1.ServiceImportStatus{
		Type: fleetnetv1alpha1.ClusterSetIP,
		Ports: []fleetnetv1alpha1.ServicePort{
			{
				Name:        httpPortName,
				Protocol:    httpPortProtocol,
				AppProtocol: &httpPortAppProtocol,
				Port:        httpPort,
			},
			{
				Name:        udpPortName,
				Protocol:    udpPortProtocol,
				AppProtocol: &udpPortAppProtocol,
				Port:        udpPort,
			},
		},
		Clusters: []fleetnetv1alpha1.ClusterStatus{
			{
				Cluster: clusterIDForMemberA,
			},
			{
				Cluster: clusterIDForMemberB,
			},
			{
				Cluster: clusterIDForMemberC,
			},
		},
	}
}

var _ = Describe("internalserviceimport controller", func() {
	Context("new internalserviceimport (serviceimport does not exist)", func() {
		var internalSvcImport *fleetnetv1alpha1.InternalServiceImport

		BeforeEach(func() {
			internalSvcImport = unfulfilledInternalServiceImport()
			Expect(hubClient.Create(ctx, internalSvcImport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, internalSvcImport)).Should(Succeed())
		})

		It("should not fulfill the internalserviceimport", func() {
			Consistently(func() bool {
				internalSvcImport := &fleetnetv1alpha1.InternalServiceImport{}
				if err := hubClient.Get(ctx, internalSvcImportAKey, internalSvcImport); err != nil {
					return false
				}

				return cmp.Equal(internalSvcImport.Status, fleetnetv1alpha1.ServiceImportStatus{})
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())
		})
	})

	Context("fulfilled internalserviceimport (serviceimport does not exist)", func() {
		var internalSvcImport *fleetnetv1alpha1.InternalServiceImport

		BeforeEach(func() {
			internalSvcImport = unfulfilledInternalServiceImport()
			Expect(hubClient.Create(ctx, internalSvcImport)).Should(Succeed())
			internalSvcImport.Finalizers = []string{internalSvcImportCleanupFinalizer}
			Expect(hubClient.Update(ctx, internalSvcImport)).Should(Succeed())

			// Retry to solve potential conflicts caused by concurrent modifications.
			Eventually(func() bool {
				if err := hubClient.Get(ctx, internalSvcImportAKey, internalSvcImport); err != nil {
					return false
				}

				fulfillInternalServiceImport(internalSvcImport)
				if err := hubClient.Status().Update(ctx, internalSvcImport); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, internalSvcImport)).Should(Succeed())
		})

		It("should clear the internalserviceimport", func() {
			Eventually(func() bool {
				internalSvcImport := &fleetnetv1alpha1.InternalServiceImport{}
				if err := hubClient.Get(ctx, internalSvcImportAKey, internalSvcImport); err != nil {
					return false
				}

				if len(internalSvcImport.Finalizers) != 0 {
					return false
				}

				return cmp.Equal(internalSvcImport.Status, fleetnetv1alpha1.ServiceImportStatus{})
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("fulfilled internalserviceimport (serviceimport is being processed)", func() {
		var internalSvcImport *fleetnetv1alpha1.InternalServiceImport
		var svcImport *fleetnetv1alpha1.ServiceImport

		fulfilledInternalSvcImport := unfulfilledInternalServiceImport()
		fulfillInternalServiceImport(fulfilledInternalSvcImport)
		expectedInternalSvcImportStatus := fulfilledInternalSvcImport.Status

		BeforeEach(func() {
			svcImport = unfulfilledAndRequestedServiceImport()
			Expect(hubClient.Create(ctx, svcImport)).Should(Succeed())

			internalSvcImport = unfulfilledInternalServiceImport()
			Expect(hubClient.Create(ctx, internalSvcImport)).Should(Succeed())
			internalSvcImport.Finalizers = []string{internalSvcImportCleanupFinalizer}
			Expect(hubClient.Update(ctx, internalSvcImport)).Should(Succeed())

			// Retry to solve potential conflicts caused by concurrent modifications.
			Eventually(func() bool {
				if err := hubClient.Get(ctx, internalSvcImportAKey, internalSvcImport); err != nil {
					return false
				}

				fulfillInternalServiceImport(internalSvcImport)
				if err := hubClient.Status().Update(ctx, internalSvcImport); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, internalSvcImport)).Should(Succeed())
		})

		It("should requeue until the ServiceImport is processed + should clear the internalserviceimport", func() {
			// Check if no action is taken on the InternalServiceImport when the ServiceImport is being processed.
			Consistently(func() bool {
				internalSvcImport := &fleetnetv1alpha1.InternalServiceImport{}
				if err := hubClient.Get(ctx, internalSvcImportAKey, internalSvcImport); err != nil {
					return false
				}

				if !cmp.Equal(internalSvcImport.Finalizers, []string{internalSvcImportCleanupFinalizer}) {
					return false
				}

				return cmp.Equal(internalSvcImport.Status, expectedInternalSvcImportStatus)
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())

			// Process (delete) the ServiceImport.
			Expect(hubClient.Delete(ctx, svcImport)).Should(Succeed())

			// Check if the InternalServiceImport is cleared.
			Eventually(func() bool {
				internalSvcImport := &fleetnetv1alpha1.InternalServiceImport{}
				if err := hubClient.Get(ctx, internalSvcImportAKey, internalSvcImport); err != nil {
					return false
				}

				if len(internalSvcImport.Finalizers) != 0 {
					return false
				}

				return cmp.Equal(internalSvcImport.Status, fleetnetv1alpha1.ServiceImportStatus{})
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("unfulfilled internalserviceimport that has claimed a service import + deleted internalserviceimport", func() {
		var internalSvcImport *fleetnetv1alpha1.InternalServiceImport
		var svcImport *fleetnetv1alpha1.ServiceImport

		fulfilledInternalSvcImport := unfulfilledInternalServiceImport()
		fulfillInternalServiceImport(fulfilledInternalSvcImport)
		expectedInternalSvcImportStatus := fulfilledInternalSvcImport.Status

		BeforeEach(func() {
			svcImport = unfulfilledAndRequestedServiceImport()
			Expect(hubClient.Create(ctx, svcImport)).Should(Succeed())
			fulfillServiceImport(svcImport)
			Expect(hubClient.Status().Update(ctx, svcImport))

			internalSvcImport = unfulfilledInternalServiceImport()
			Expect(hubClient.Create(ctx, internalSvcImport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, svcImport)).Should(Succeed())
		})

		It("should fulfill internalserviceimport + should withdraw service import when internalserviceimport is deleted", func() {
			// Check if the InternalServiceImport is fulfilled.
			Eventually(func() bool {
				internalSvcImport := &fleetnetv1alpha1.InternalServiceImport{}
				if err := hubClient.Get(ctx, internalSvcImportAKey, internalSvcImport); err != nil {
					return false
				}

				if !cmp.Equal(internalSvcImport.Finalizers, []string{internalSvcImportCleanupFinalizer}) {
					return false
				}

				return cmp.Equal(internalSvcImport.Status, expectedInternalSvcImportStatus)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("new internalserviceimport (service already imported by another cluster)", func() {})
})
