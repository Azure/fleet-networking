/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package internalserviceimport

import (
	"encoding/json"
	"time"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
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
			ServiceImportReference: fleetnetv1alpha1.FromMetaObjects(clusterIDForMemberA,
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
				objectmeta.ServiceImportAnnotationServiceInUseBy: fulfilledSvcInUseByAnnotationString(),
			},
			Finalizers: []string{svcImportCleanupFinalizer},
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

var _ = Describe("internalserviceimport controller", Ordered, func() {
	Context("new internalserviceimport (serviceimport does not exist)", FlakeAttempts(3), func() {
		var internalSvcImport *fleetnetv1alpha1.InternalServiceImport

		BeforeEach(func() {
			internalSvcImport = unfulfilledInternalServiceImport()
			Expect(hubClient.Create(ctx, internalSvcImport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, internalSvcImport)).Should(Succeed())
			// Confirm that InternalServiceImport is deleted; this helps make the test less flaky.
			Eventually(func() bool {
				internalSvcImport := &fleetnetv1alpha1.InternalServiceImport{}
				if err := hubClient.Get(ctx, internalSvcImportAKey, internalSvcImport); err != nil && errors.IsNotFound(err) {
					return true
				}
				return false
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})

		It("should not fulfill internalserviceimport", func() {
			Consistently(func() bool {
				internalSvcImport := &fleetnetv1alpha1.InternalServiceImport{}
				if err := hubClient.Get(ctx, internalSvcImportAKey, internalSvcImport); err != nil {
					return false
				}

				return cmp.Equal(internalSvcImport.Status, fleetnetv1alpha1.ServiceImportStatus{})
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())
		})
	})

	Context("fulfilled internalserviceimport (serviceimport does not exist)", FlakeAttempts(3), func() {
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
			// Confirm that InternalServiceImport is deleted; this helps make the test less flaky.
			Eventually(func() bool {
				internalSvcImport := &fleetnetv1alpha1.InternalServiceImport{}
				if err := hubClient.Get(ctx, internalSvcImportAKey, internalSvcImport); err != nil && errors.IsNotFound(err) {
					return true
				}
				return false
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})

		It("should clear internalserviceimport", func() {
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

	Context("unfulfilled internalserviceimport that has claimed a service import + deleted internalserviceimport", FlakeAttempts(3), func() {
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
			internalSvcImport.Finalizers = []string{internalSvcImportCleanupFinalizer}
			Expect(hubClient.Create(ctx, internalSvcImport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, svcImport)).Should(Succeed())
			// Confirm that ServiceImport is deleted; this helps make the test less flaky.
			Eventually(func() bool {
				svcImport := &fleetnetv1alpha1.ServiceImport{}
				if err := hubClient.Get(ctx, svcImportKey, svcImport); err != nil && errors.IsNotFound(err) {
					return true
				}
				return false
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})

		It("should fulfill internalserviceimport + should withdraw service import when internalserviceimport is deleted", func() {
			// Check if InternalServiceImport is fulfilled.
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

			// Delete InternalServiceImport.
			Expect(hubClient.Delete(ctx, internalSvcImport)).Should(Succeed())

			// Check if the service import is withdrawn.
			Eventually(func() bool {
				svcImport := &fleetnetv1alpha1.ServiceImport{}
				if err := hubClient.Get(ctx, svcImportKey, svcImport); err != nil {
					return false
				}

				if _, ok := svcImport.Annotations[objectmeta.ServiceImportAnnotationServiceInUseBy]; ok {
					return false
				}

				return len(svcImport.Finalizers) == 0
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// Check if InternalServiceImport cleanup finalizer has been removed (and the object is deleted).
			Eventually(func() bool {
				internalSvcImport := &fleetnetv1alpha1.InternalServiceImport{}
				if err := hubClient.Get(ctx, internalSvcImportAKey, internalSvcImport); err != nil && errors.IsNotFound(err) {
					return true
				}
				return false
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("new internalserviceimport", FlakeAttempts(3), func() {
		var internalSvcImport *fleetnetv1alpha1.InternalServiceImport
		var svcImport *fleetnetv1alpha1.ServiceImport

		fulfilledInternalSvcImport := unfulfilledInternalServiceImport()
		fulfillInternalServiceImport(fulfilledInternalSvcImport)
		expectedInternalSvcImportStatus := fulfilledInternalSvcImport.Status

		expectedSvcInUseAnnotationData := fulfilledSvcInUseByAnnotationString()

		BeforeEach(func() {
			svcImport = unfulfilledAndRequestedServiceImport()
			svcImport.Annotations = nil
			svcImport.Finalizers = []string{}
			Expect(hubClient.Create(ctx, svcImport)).Should(Succeed())
			fulfillServiceImport(svcImport)
			Expect(hubClient.Status().Update(ctx, svcImport))

			internalSvcImport = unfulfilledInternalServiceImport()
			Expect(hubClient.Create(ctx, internalSvcImport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, internalSvcImport)).Should(Succeed())
			// Confirm that InternalServiceImport is deleted; this helps make the test less flaky.
			Eventually(func() bool {
				internalSvcImport := &fleetnetv1alpha1.InternalServiceImport{}
				if err := hubClient.Get(ctx, internalSvcImportAKey, internalSvcImport); err != nil && errors.IsNotFound(err) {
					return true
				}
				return false
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Expect(hubClient.Delete(ctx, svcImport)).Should(Succeed())
			// Confirm that ServiceImport is deleted; this helps make the test less flaky.
			Eventually(func() bool {
				svcImport := &fleetnetv1alpha1.ServiceImport{}
				if err := hubClient.Get(ctx, svcImportKey, svcImport); err != nil && errors.IsNotFound(err) {
					return true
				}
				return false
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})

		It("should fulfill internalserviceimport + should claim serviceimport", func() {
			// Check if InternalServiceImport is fulfilled.
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

			// Check if ServiceImport is claimed.
			Eventually(func() bool {
				svcImport := &fleetnetv1alpha1.ServiceImport{}
				if err := hubClient.Get(ctx, svcImportKey, svcImport); err != nil {
					return false
				}

				data, ok := svcImport.Annotations[objectmeta.ServiceImportAnnotationServiceInUseBy]
				if !ok || !cmp.Equal(data, expectedSvcInUseAnnotationData) {
					return false
				}

				return cmp.Equal(svcImport.Finalizers, []string{svcImportCleanupFinalizer})
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("new internalserviceimport (service already imported by another cluster)", FlakeAttempts(3), func() {
		var internalSvcImport *fleetnetv1alpha1.InternalServiceImport
		var svcImport *fleetnetv1alpha1.ServiceImport

		expectedSvcInUseAnnotationData := fulfilledSvcInUseByAnnotationString()

		BeforeEach(func() {
			svcImport = unfulfilledAndRequestedServiceImport()
			Expect(hubClient.Create(ctx, svcImport)).Should(Succeed())
			fulfillServiceImport(svcImport)
			Expect(hubClient.Status().Update(ctx, svcImport))

			internalSvcImport = unfulfilledInternalServiceImport()
			internalSvcImport.Namespace = hubNSForMemberB
			internalSvcImport.Finalizers = []string{internalSvcImportCleanupFinalizer}
			Expect(hubClient.Create(ctx, internalSvcImport)).Should(Succeed())
			fulfillInternalServiceImport(internalSvcImport)
			Expect(hubClient.Status().Update(ctx, internalSvcImport))
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, internalSvcImport)).Should(Succeed())
			// Confirm that InternalServiceImport is deleted; this helps make the test less flaky.
			Eventually(func() bool {
				internalSvcImport := &fleetnetv1alpha1.InternalServiceImport{}
				if err := hubClient.Get(ctx, internalSvcImportAKey, internalSvcImport); err != nil && errors.IsNotFound(err) {
					return true
				}
				return false
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Expect(hubClient.Get(ctx, svcImportKey, svcImport)).Should(Succeed())
			svcImport.Finalizers = []string{}
			Expect(hubClient.Update(ctx, svcImport)).Should(Succeed())
			Expect(hubClient.Delete(ctx, svcImport)).Should(Succeed())
			// Confirm that ServiceImport is deleted; this helps make the test less flaky.
			Eventually(func() bool {
				svcImport := &fleetnetv1alpha1.ServiceImport{}
				if err := hubClient.Get(ctx, svcImportKey, svcImport); err != nil && errors.IsNotFound(err) {
					return true
				}
				return false
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})

		It("should ignore the import + should clear internalserviceimport status", func() {
			// Check if ServiceInUseBy information on ServiceImport has not changed.
			Consistently(func() bool {
				svcImport := &fleetnetv1alpha1.ServiceImport{}
				if err := hubClient.Get(ctx, svcImportKey, svcImport); err != nil {
					return false
				}

				return cmp.Equal(svcImport.Annotations[objectmeta.ServiceImportAnnotationServiceInUseBy], expectedSvcInUseAnnotationData)
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())

			// Check if InternalServiceImport is cleared.
			Eventually(func() bool {
				internalSvcImport := &fleetnetv1alpha1.InternalServiceImport{}
				if err := hubClient.Get(ctx, internalSvcImportBKey, internalSvcImport); err != nil {
					return false
				}

				if len(internalSvcImport.Finalizers) != 0 {
					return false
				}

				return cmp.Equal(internalSvcImport.Status, fleetnetv1alpha1.ServiceImportStatus{})
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("serviceimport is created (with pre-existing internalserviceimports)", FlakeAttempts(3), func() {
		var internalSvcImportB *fleetnetv1alpha1.InternalServiceImport
		var internalSvcImportC *fleetnetv1alpha1.InternalServiceImport
		var svcImport *fleetnetv1alpha1.ServiceImport

		fulfilledInternalSvcImport := unfulfilledInternalServiceImport()
		fulfillInternalServiceImport(fulfilledInternalSvcImport)
		expectedInternalSvcImportStatus := fulfilledInternalSvcImport.Status

		BeforeEach(func() {
			internalSvcImportB = unfulfilledInternalServiceImport()
			internalSvcImportB.Namespace = hubNSForMemberB
			Expect(hubClient.Create(ctx, internalSvcImportB)).Should(Succeed())

			internalSvcImportC = unfulfilledInternalServiceImport()
			internalSvcImportC.Namespace = hubNSForMemberC
			Expect(hubClient.Create(ctx, internalSvcImportC)).Should(Succeed())

			svcImport = unfulfilledAndRequestedServiceImport()
			svcImport.Annotations = nil
			svcImport.Finalizers = nil
			Expect(hubClient.Create(ctx, svcImport)).Should(Succeed())
			fulfillServiceImport(svcImport)
			Expect(hubClient.Status().Update(ctx, svcImport))
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, internalSvcImportB)).Should(Succeed())
			// Confirm that InternalServiceImport is deleted; this helps make the test less flaky.
			Eventually(func() bool {
				internalSvcImport := &fleetnetv1alpha1.InternalServiceImport{}
				if err := hubClient.Get(ctx, internalSvcImportBKey, internalSvcImport); err != nil && errors.IsNotFound(err) {
					return true
				}
				return false
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Expect(hubClient.Delete(ctx, internalSvcImportC)).Should(Succeed())
			// Confirm that InternalServiceImport is deleted; this helps make the test less flaky.
			Eventually(func() bool {
				internalSvcImport := &fleetnetv1alpha1.InternalServiceImport{}
				if err := hubClient.Get(ctx, internalSvcImportCKey, internalSvcImport); err != nil && errors.IsNotFound(err) {
					return true
				}
				return false
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Expect(hubClient.Delete(ctx, svcImport)).Should(Succeed())
			// Confirm that ServiceImport is deleted; this helps make the test less flaky.
			Eventually(func() bool {
				svcImport := &fleetnetv1alpha1.ServiceImport{}
				if err := hubClient.Get(ctx, svcImportKey, svcImport); err != nil && errors.IsNotFound(err) {
					return true
				}
				return false
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})

		It("should fulfill one of the internalserviceimports + should claim serviceimport", func() {
			var claimer fleetnetv1alpha1.ClusterNamespace
			// ServiceImport should be claimed by exactly one InternalServiceImport.
			Eventually(func() bool {
				svcImport := &fleetnetv1alpha1.ServiceImport{}
				if err := hubClient.Get(ctx, svcImportKey, svcImport); err != nil {
					return false
				}

				data, ok := svcImport.Annotations[objectmeta.ServiceImportAnnotationServiceInUseBy]
				if !ok {
					return false
				}

				svcInUseBy := &fleetnetv1alpha1.ServiceInUseBy{}
				if err := json.Unmarshal([]byte(data), svcInUseBy); err != nil {
					return false
				}

				if len(svcInUseBy.MemberClusters) != 1 {
					return false
				}

				for k := range svcInUseBy.MemberClusters {
					claimer = k
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Consistently(func() bool {
				svcImport := &fleetnetv1alpha1.ServiceImport{}
				if err := hubClient.Get(ctx, svcImportKey, svcImport); err != nil {
					return false
				}

				data, ok := svcImport.Annotations[objectmeta.ServiceImportAnnotationServiceInUseBy]
				if !ok {
					return false
				}

				svcInUseBy := &fleetnetv1alpha1.ServiceInUseBy{}
				if err := json.Unmarshal([]byte(data), svcInUseBy); err != nil {
					return false
				}

				if len(svcInUseBy.MemberClusters) != 1 {
					return false
				}

				_, ok = svcInUseBy.MemberClusters[claimer]
				return ok
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())

			// Exactly one InternalServiceImport should be fulfilled.
			Consistently(func() bool {
				internalSvcImportB := &fleetnetv1alpha1.InternalServiceImport{}
				if err := hubClient.Get(ctx, internalSvcImportBKey, internalSvcImportB); err != nil {
					return false
				}

				internalSvcImportC := &fleetnetv1alpha1.InternalServiceImport{}
				if err := hubClient.Get(ctx, internalSvcImportCKey, internalSvcImportC); err != nil {
					return false
				}

				var claimerInternalSvcImport *fleetnetv1alpha1.InternalServiceImport
				var idleInternalSvcImport *fleetnetv1alpha1.InternalServiceImport
				if string(claimer) == internalSvcImportB.Namespace {
					claimerInternalSvcImport = internalSvcImportB
					idleInternalSvcImport = internalSvcImportC
				} else {
					claimerInternalSvcImport = internalSvcImportC
					idleInternalSvcImport = internalSvcImportB
				}

				if !cmp.Equal(claimerInternalSvcImport.Finalizers, []string{internalSvcImportCleanupFinalizer}) {
					return false
				}

				if !cmp.Equal(claimerInternalSvcImport.Status, expectedInternalSvcImportStatus) {
					return false
				}

				if len(idleInternalSvcImport.Finalizers) != 0 {
					return false
				}

				return cmp.Equal(idleInternalSvcImport.Status, fleetnetv1alpha1.ServiceImportStatus{})
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())
		})
	})

	// Note that with current semantics (a service can only be imported once across the fleet) this is a test
	// case that should not happen in normal operations.
	Context("deleted internalserviceimport (with other remaining internalserviceimport having claimed the service", FlakeAttempts(3), func() {
		var internalSvcImportA *fleetnetv1alpha1.InternalServiceImport
		var internalSvcImportB *fleetnetv1alpha1.InternalServiceImport
		var svcImport *fleetnetv1alpha1.ServiceImport

		fulfilledInternalSvcImport := unfulfilledInternalServiceImport()
		fulfillInternalServiceImport(fulfilledInternalSvcImport)
		expectedInternalSvcImportStatus := fulfilledInternalSvcImport.Status

		expectedSvcInUseAnnotationData := fulfilledSvcInUseByAnnotationString()

		BeforeEach(func() {
			svcImport = unfulfilledAndRequestedServiceImport()
			svcInUseBy := &fleetnetv1alpha1.ServiceInUseBy{
				MemberClusters: map[fleetnetv1alpha1.ClusterNamespace]fleetnetv1alpha1.ClusterID{
					hubNSForMemberA: clusterIDForMemberA,
					hubNSForMemberB: clusterIDForMemberB,
				},
			}
			svcInUseByData, _ := json.Marshal(svcInUseBy)
			svcImport.Annotations[objectmeta.ServiceImportAnnotationServiceInUseBy] = string(svcInUseByData)
			Expect(hubClient.Create(ctx, svcImport)).Should(Succeed())
			fulfillServiceImport(svcImport)
			Expect(hubClient.Status().Update(ctx, svcImport)).Should(Succeed())

			internalSvcImportA = unfulfilledInternalServiceImport()
			internalSvcImportA.Finalizers = []string{internalSvcImportCleanupFinalizer}
			Expect(hubClient.Create(ctx, internalSvcImportA)).Should(Succeed())

			internalSvcImportB = unfulfilledInternalServiceImport()
			internalSvcImportB.Namespace = hubNSForMemberB
			internalSvcImportB.Finalizers = []string{internalSvcImportCleanupFinalizer}
			Expect(hubClient.Create(ctx, internalSvcImportB)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, internalSvcImportA)).Should(Succeed())
			// Confirm that InternalServiceImport is deleted; this helps make the test less flaky.
			Eventually(func() bool {
				internalSvcImport := &fleetnetv1alpha1.InternalServiceImport{}
				if err := hubClient.Get(ctx, internalSvcImportAKey, internalSvcImport); err != nil && errors.IsNotFound(err) {
					return true
				}
				return false
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Expect(hubClient.Delete(ctx, svcImport)).Should(Succeed())
			// Confirm that ServiceImport is deleted; this helps make the test less flaky.
			Eventually(func() bool {
				svcImport := &fleetnetv1alpha1.ServiceImport{}
				if err := hubClient.Get(ctx, svcImportKey, svcImport); err != nil && errors.IsNotFound(err) {
					return true
				}
				return false
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})

		It("should withdraw service import without affecting other internalserviceimports", func() {
			// Confirm that both InternalServiceImports are fulfilled.
			Eventually(func() bool {
				internalSvcImportA := &fleetnetv1alpha1.InternalServiceImport{}
				if err := hubClient.Get(ctx, internalSvcImportAKey, internalSvcImportA); err != nil {
					return false
				}

				if !cmp.Equal(internalSvcImportA.Finalizers, []string{internalSvcImportCleanupFinalizer}) {
					return false
				}

				return cmp.Equal(internalSvcImportA.Status, expectedInternalSvcImportStatus)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Eventually(func() bool {
				internalSvcImportB := &fleetnetv1alpha1.InternalServiceImport{}
				if err := hubClient.Get(ctx, internalSvcImportBKey, internalSvcImportB); err != nil {
					return false
				}

				if !cmp.Equal(internalSvcImportB.Finalizers, []string{internalSvcImportCleanupFinalizer}) {
					return false
				}

				return cmp.Equal(internalSvcImportB.Status, expectedInternalSvcImportStatus)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// Delete one of the InternalServiceImport.
			Expect(hubClient.Delete(ctx, internalSvcImportB)).Should(Succeed())

			// Confirm that InternalServiceImport is deleted and service import has been withdrawn.
			Eventually(func() bool {
				internalSvcImportB := &fleetnetv1alpha1.InternalServiceImport{}
				if err := hubClient.Get(ctx, internalSvcImportBKey, internalSvcImportB); err != nil && errors.IsNotFound(err) {
					return true
				}
				return false
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Consistently(func() bool {
				svcImport := &fleetnetv1alpha1.ServiceImport{}
				if err := hubClient.Get(ctx, svcImportKey, svcImport); err != nil {
					return false
				}

				data, ok := svcImport.Annotations[objectmeta.ServiceImportAnnotationServiceInUseBy]
				if !ok || data != expectedSvcInUseAnnotationData {
					return false
				}

				internalSvcImportA := &fleetnetv1alpha1.InternalServiceImport{}
				if err := hubClient.Get(ctx, internalSvcImportAKey, internalSvcImportA); err != nil {
					return false
				}

				if !cmp.Equal(internalSvcImportA.Finalizers, []string{internalSvcImportCleanupFinalizer}) {
					return false
				}

				return cmp.Equal(internalSvcImportA.Status, expectedInternalSvcImportStatus)
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())
		})
	})

	Context("deleted internalserviceimport (with backup internalserviceimport)", FlakeAttempts(3), func() {
		var internalSvcImportA *fleetnetv1alpha1.InternalServiceImport
		var internalSvcImportB *fleetnetv1alpha1.InternalServiceImport
		var svcImport *fleetnetv1alpha1.ServiceImport

		fulfilledInternalSvcImport := unfulfilledInternalServiceImport()
		fulfillInternalServiceImport(fulfilledInternalSvcImport)
		expectedInternalSvcImportStatus := fulfilledInternalSvcImport.Status

		expectedSvcInUseAnnotationData := fulfilledSvcInUseByAnnotationString()

		BeforeEach(func() {
			svcImport = unfulfilledAndRequestedServiceImport()
			svcImport.Annotations = nil
			Expect(hubClient.Create(ctx, svcImport)).Should(Succeed())
			fulfillServiceImport(svcImport)
			Expect(hubClient.Status().Update(ctx, svcImport)).Should(Succeed())

			internalSvcImportB = unfulfilledInternalServiceImport()
			internalSvcImportB.Namespace = hubNSForMemberB
			Expect(hubClient.Create(ctx, internalSvcImportB)).Should(Succeed())

			internalSvcImportA = unfulfilledInternalServiceImport()
			Expect(hubClient.Create(ctx, internalSvcImportA)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, internalSvcImportA)).Should(Succeed())
			// Confirm that InternalServiceImport is deleted; this helps make the test less flaky.
			Eventually(func() bool {
				internalSvcImport := &fleetnetv1alpha1.InternalServiceImport{}
				if err := hubClient.Get(ctx, internalSvcImportAKey, internalSvcImport); err != nil && errors.IsNotFound(err) {
					return true
				}
				return false
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Expect(hubClient.Delete(ctx, svcImport)).Should(Succeed())
			// Confirm that ServiceImport is deleted; this helps make the test less flaky.
			Eventually(func() bool {
				svcImport := &fleetnetv1alpha1.ServiceImport{}
				if err := hubClient.Get(ctx, svcImportKey, svcImport); err != nil && errors.IsNotFound(err) {
					return true
				}
				return false
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})

		It("backup internalserviceimport should take over", func() {
			// Confirm that only one InternalServiceImport has been fulfilled.
			Eventually(func() bool {
				internalSvcImportB := &fleetnetv1alpha1.InternalServiceImport{}
				if err := hubClient.Get(ctx, internalSvcImportBKey, internalSvcImportB); err != nil {
					return false
				}

				if !cmp.Equal(internalSvcImportB.Finalizers, []string{internalSvcImportCleanupFinalizer}) {
					return false
				}

				return cmp.Equal(internalSvcImportB.Status, expectedInternalSvcImportStatus)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Eventually(func() bool {
				internalSvcImportA := &fleetnetv1alpha1.InternalServiceImport{}
				if err := hubClient.Get(ctx, internalSvcImportAKey, internalSvcImportA); err != nil {
					return false
				}

				if len(internalSvcImportA.Finalizers) != 0 {
					return false
				}

				return cmp.Equal(internalSvcImportA.Status, fleetnetv1alpha1.ServiceImportStatus{})
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Eventually(func() bool {
				svcImport := &fleetnetv1alpha1.ServiceImport{}
				if err := hubClient.Get(ctx, svcImportKey, svcImport); err != nil {
					return false
				}

				data, ok := svcImport.Annotations[objectmeta.ServiceImportAnnotationServiceInUseBy]
				if !ok {
					return false
				}

				svcInUseBy := &fleetnetv1alpha1.ServiceInUseBy{}
				if err := json.Unmarshal([]byte(data), svcInUseBy); err != nil {
					return false
				}

				if len(svcInUseBy.MemberClusters) != 1 {
					return false
				}

				if _, ok := svcInUseBy.MemberClusters[hubNSForMemberB]; !ok {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// Delete the claimer InternalServiceImport.
			Expect(hubClient.Delete(ctx, internalSvcImportB)).Should(Succeed())
			Eventually(func() bool {
				internalSvcImport := &fleetnetv1alpha1.InternalServiceImport{}
				if err := hubClient.Get(ctx, internalSvcImportBKey, internalSvcImport); err != nil && errors.IsNotFound(err) {
					return true
				}
				return false
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// Confirm that the other (backup) InternalServiceImport has taken over.
			Eventually(func() bool {
				internalSvcImportA := &fleetnetv1alpha1.InternalServiceImport{}
				if err := hubClient.Get(ctx, internalSvcImportAKey, internalSvcImportA); err != nil {
					return false
				}

				if !cmp.Equal(internalSvcImportA.Finalizers, []string{internalSvcImportCleanupFinalizer}) {
					return false
				}

				return cmp.Equal(internalSvcImportA.Status, expectedInternalSvcImportStatus)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Eventually(func() bool {
				svcImport := &fleetnetv1alpha1.ServiceImport{}
				if err := hubClient.Get(ctx, svcImportKey, svcImport); err != nil {
					return false
				}

				data, ok := svcImport.Annotations[objectmeta.ServiceImportAnnotationServiceInUseBy]
				if !ok || data != expectedSvcInUseAnnotationData {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("serviceimport status updated", FlakeAttempts(3), func() {
		var internalSvcImport *fleetnetv1alpha1.InternalServiceImport
		var svcImport *fleetnetv1alpha1.ServiceImport

		fulfilledInternalSvcImport := unfulfilledInternalServiceImport()
		fulfillInternalServiceImport(fulfilledInternalSvcImport)
		expectedInternalSvcImportStatus := fulfilledInternalSvcImport.Status

		newPort := int32(8080)
		newTargetPort := intstr.FromInt(8080)

		BeforeEach(func() {
			svcImport = unfulfilledAndRequestedServiceImport()
			svcImport.Annotations = nil
			Expect(hubClient.Create(ctx, svcImport)).Should(Succeed())
			fulfillServiceImport(svcImport)
			Expect(hubClient.Status().Update(ctx, svcImport)).Should(Succeed())

			internalSvcImport = unfulfilledInternalServiceImport()
			Expect(hubClient.Create(ctx, internalSvcImport))
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, internalSvcImport)).Should(Succeed())
			// Confirm that InternalServiceImport is deleted; this helps make the test less flaky.
			Eventually(func() bool {
				internalSvcImport := &fleetnetv1alpha1.InternalServiceImport{}
				if err := hubClient.Get(ctx, internalSvcImportAKey, internalSvcImport); err != nil && errors.IsNotFound(err) {
					return true
				}
				return false
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Expect(hubClient.Delete(ctx, svcImport)).Should(Succeed())
			// Confirm that ServiceImport is deleted; this helps make the test less flaky.
			Eventually(func() bool {
				svcImport := &fleetnetv1alpha1.ServiceImport{}
				if err := hubClient.Get(ctx, svcImportKey, svcImport); err != nil && errors.IsNotFound(err) {
					return true
				}
				return false
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})

		It("should update internalserviceimport", func() {
			// Confirm that InternalServiceImport has been fulfilled.
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

			// Update ServiceImport.
			Expect(hubClient.Get(ctx, svcImportKey, svcImport)).Should(Succeed())
			updatedSvcImportStatus := fleetnetv1alpha1.ServiceImportStatus{
				Type: fleetnetv1alpha1.ClusterSetIP,
				Ports: []fleetnetv1alpha1.ServicePort{
					{
						Port:       newPort,
						TargetPort: newTargetPort,
					},
				},
				Clusters: []fleetnetv1alpha1.ClusterStatus{
					{
						Cluster: clusterIDForMemberC,
					},
				},
			}
			svcImport.Status = updatedSvcImportStatus
			Expect(hubClient.Status().Update(ctx, svcImport)).Should(Succeed())

			// Check if InternalServiceImport has picked up the status update.
			expectedUpdatedInternalSvcImportStatus := fleetnetv1alpha1.ServiceImportStatus{
				Type: fleetnetv1alpha1.ClusterSetIP,
				Ports: []fleetnetv1alpha1.ServicePort{
					{
						Protocol:   httpPortProtocol,
						Port:       newPort,
						TargetPort: newTargetPort,
					},
				},
				Clusters: []fleetnetv1alpha1.ClusterStatus{
					{
						Cluster: clusterIDForMemberC,
					},
				},
			}
			Eventually(func() bool {
				internalSvcImport := &fleetnetv1alpha1.InternalServiceImport{}
				if err := hubClient.Get(ctx, internalSvcImportAKey, internalSvcImport); err != nil {
					return false
				}

				if !cmp.Equal(internalSvcImport.Finalizers, []string{internalSvcImportCleanupFinalizer}) {
					return false
				}

				return cmp.Equal(internalSvcImport.Status, expectedUpdatedInternalSvcImportStatus)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("fulfilled internalserviceimport (serviceimport is being processed)", FlakeAttempts(3), func() {
		var internalSvcImport *fleetnetv1alpha1.InternalServiceImport
		var svcImport *fleetnetv1alpha1.ServiceImport

		fulfilledInternalSvcImport := unfulfilledInternalServiceImport()
		fulfillInternalServiceImport(fulfilledInternalSvcImport)
		expectedInternalSvcImportStatus := fulfilledInternalSvcImport.Status

		BeforeEach(func() {
			svcImport = unfulfilledAndRequestedServiceImport()
			Expect(hubClient.Create(ctx, svcImport)).Should(Succeed())

			internalSvcImport = unfulfilledInternalServiceImport()
			internalSvcImport.Finalizers = []string{internalSvcImportCleanupFinalizer}
			Expect(hubClient.Create(ctx, internalSvcImport)).Should(Succeed())

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
			// Confirm that InternalServiceImport is deleted; this helps make the test less flaky.
			Eventually(func() bool {
				internalSvcImport := &fleetnetv1alpha1.InternalServiceImport{}
				if err := hubClient.Get(ctx, internalSvcImportAKey, internalSvcImport); err != nil && errors.IsNotFound(err) {
					return true
				}
				return false
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})

		It("should requeue until serviceimport is processed + should clear internalserviceimport when serviceimport is deleted", func() {
			// Check if no action is taken on InternalServiceImport when ServiceImport is being processed.
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

			// Process (delete) ServiceImport.
			Expect(hubClient.Delete(ctx, svcImport)).Should(Succeed())

			// Check if InternalServiceImport is cleared.
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

			// Check if ServiceImport cleanup finalizer has been removed (and the object is deleted).
			Eventually(func() bool {
				svcImport := &fleetnetv1alpha1.ServiceImport{}
				if err := hubClient.Get(ctx, svcImportKey, svcImport); err != nil && errors.IsNotFound(err) {
					return true
				}
				return false
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})
})
