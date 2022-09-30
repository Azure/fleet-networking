/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package endpointsliceexport

import (
	"encoding/json"
	"time"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
)

const (
	eventuallyTimeout    = time.Second * 10
	eventuallyInterval   = time.Millisecond * 250
	consistentlyDuration = time.Millisecond * 1000
	consistentlyInterval = time.Millisecond * 150
)

var (
	svcImportKey            = types.NamespacedName{Namespace: memberUserNS, Name: svcName}
	endpointSliceImportBKey = types.NamespacedName{Namespace: hubNSForMemberB, Name: endpointSliceExportName}
	endpointSliceImportCKey = types.NamespacedName{Namespace: hubNSForMemberC, Name: endpointSliceExportName}
)

// fulfilledSvcInUseByAnnotation returns a fulfilled ServiceInUseBy for annotation use.
func fulfilledSvcInUseByAnnotation() fleetnetv1alpha1.ServiceInUseBy {
	return fleetnetv1alpha1.ServiceInUseBy{
		MemberClusters: map[fleetnetv1alpha1.ClusterNamespace]fleetnetv1alpha1.ClusterID{
			hubNSForMemberB: clusterIDForMemberB,
			hubNSForMemberC: clusterIDForMemberC,
		},
	}
}

// unfulfilledAndRequestedServiceImport returns an empty ServiceImport annotated with ServiceInUseBy data.
func unfulfilledAndRequestedServiceImport() *fleetnetv1alpha1.ServiceImport {
	data, err := json.Marshal(fulfilledSvcInUseByAnnotation())
	if err != nil {
		panic("failed to marshal service in use annotation")
	}

	return &fleetnetv1alpha1.ServiceImport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      svcName,
			Annotations: map[string]string{
				objectmeta.ServiceImportAnnotationServiceInUseBy: string(data),
			},
		},
	}
}

// fulfillSvcImport fulfills a ServiceImport by updating its status.
func fulfillSvcImport(svcImport *fleetnetv1alpha1.ServiceImport) {
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
				Name:        tcpPortName,
				Protocol:    tcpPortProtocol,
				AppProtocol: &tcpPortAppProtocol,
				Port:        tcpPort,
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

var _ = Describe("endpointsliceexport controller", func() {
	Context("deleted endpointsliceexport", func() {
		var (
			endpointSliceExport  *fleetnetv1alpha1.EndpointSliceExport
			svcImport            *fleetnetv1alpha1.ServiceImport
			endpointSliceImportB *fleetnetv1alpha1.EndpointSliceImport
			endpointSliceImportC *fleetnetv1alpha1.EndpointSliceImport
		)

		BeforeEach(func() {
			endpointSliceExport = ipv4EndpointSliceExport()
			endpointSliceExportSpec := endpointSliceExport.Spec

			endpointSliceImportB = &fleetnetv1alpha1.EndpointSliceImport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMemberB,
					Name:      endpointSliceExportName,
				},
				Spec: *endpointSliceExportSpec.DeepCopy(),
			}
			Expect(hubClient.Create(ctx, endpointSliceImportB)).Should(Succeed())

			endpointSliceImportC = &fleetnetv1alpha1.EndpointSliceImport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMemberC,
					Name:      endpointSliceExportName,
				},
				Spec: *endpointSliceExportSpec.DeepCopy(),
			}
			Expect(hubClient.Create(ctx, endpointSliceImportC)).Should(Succeed())

			svcImport = unfulfilledAndRequestedServiceImport()
			Expect(hubClient.Create(ctx, svcImport)).Should(Succeed())
			fulfillSvcImport(svcImport)
			Expect(hubClient.Status().Update(ctx, svcImport)).Should(Succeed())

			Expect(hubClient.Create(ctx, endpointSliceExport)).Should(Succeed())
			Expect(hubClient.Delete(ctx, endpointSliceExport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, svcImport)).Should(Succeed())
			// Confirm that ServiceImport is deleted; this helps make the test less flaky.
			Eventually(func() error {
				return client.IgnoreNotFound(hubClient.Get(ctx, svcImportKey, svcImport))
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should withdraw all endpointsliceimports", func() {
			// Check if all EndpointSliceImports has been withdrawn.
			Eventually(func() bool {
				if err := hubClient.Get(ctx, endpointSliceImportBKey, endpointSliceImportB); err != nil && errors.IsNotFound(err) {
					return false
				}

				if err := hubClient.Get(ctx, endpointSliceImportCKey, endpointSliceImportC); err != nil && errors.IsNotFound(err) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// Check if the cleanup finalizer has been removed.
			Eventually(func() bool {
				if err := hubClient.Get(ctx, endpointSliceExportKey, endpointSliceExport); err != nil && errors.IsNotFound(err) {
					return true
				}
				return false
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("new endpointsliceexport (no service import)", func() {
		var endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport

		BeforeEach(func() {
			endpointSliceExport = ipv4EndpointSliceExport()
			endpointSliceExport.Finalizers = []string{}
			Expect(hubClient.Create(ctx, endpointSliceExport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, endpointSliceExport)).Should(Succeed())
			// Wait until all EndpointSliceExport related resources are cleaned up; this helps make the test less flaky.
			Eventually(func() bool {
				endpointSliceImportList := &fleetnetv1alpha1.EndpointSliceImportList{}
				if err := hubClient.List(ctx, endpointSliceImportList); err != nil {
					return false
				}

				if len(endpointSliceImportList.Items) != 0 {
					return false
				}

				if err := hubClient.Get(ctx, endpointSliceExportKey, endpointSliceExport); !errors.IsNotFound(err) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})

		It("should not distribute endpointslice to member clusters", func() {
			// Check if no EndpointSlice has been distributed.
			Consistently(func() bool {
				endpointSliceImportList := &fleetnetv1alpha1.EndpointSliceImportList{}
				if err := hubClient.List(ctx, endpointSliceImportList); err != nil {
					return false
				}

				if len(endpointSliceImportList.Items) != 0 {
					return false
				}
				return true
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())

			// Check if the cleanup finalizer has been added.
			Eventually(func() bool {
				if err := hubClient.Get(ctx, endpointSliceExportKey, endpointSliceExport); err != nil {
					return false
				}

				return cmp.Equal(endpointSliceExport.Finalizers, []string{endpointSliceExportCleanupFinalizer})
			})
		})
	})

	Context("new endpointsliceexport (owner service is not imported)", func() {
		var endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport
		var svcImport *fleetnetv1alpha1.ServiceImport

		BeforeEach(func() {
			svcImport = unfulfilledAndRequestedServiceImport()
			svcImport.Annotations = map[string]string{}
			Expect(hubClient.Create(ctx, svcImport)).Should(Succeed())
			fulfillSvcImport(svcImport)
			Expect(hubClient.Status().Update(ctx, svcImport)).Should(Succeed())

			endpointSliceExport = ipv4EndpointSliceExport()
			endpointSliceExport.Finalizers = []string{}
			Expect(hubClient.Create(ctx, endpointSliceExport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, endpointSliceExport)).Should(Succeed())
			// Wait until all EndpointSliceExport related resources are cleaned up; this helps make the test less flaky.
			Eventually(func() bool {
				endpointSliceImportList := &fleetnetv1alpha1.EndpointSliceImportList{}
				if err := hubClient.List(ctx, endpointSliceImportList); err != nil {
					return false
				}

				if len(endpointSliceImportList.Items) != 0 {
					return false
				}

				if err := hubClient.Get(ctx, endpointSliceExportKey, endpointSliceExport); !errors.IsNotFound(err) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Expect(hubClient.Delete(ctx, svcImport)).Should(Succeed())
			// Confirm that ServiceImport is deleted; this helps make the test less flaky.
			Eventually(func() error {
				return client.IgnoreNotFound(hubClient.Get(ctx, svcImportKey, svcImport))
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should not distribute endpointslice to member clusters", func() {
			// Check if no EndpointSlice has been distributed.
			Consistently(func() bool {
				endpointSliceImportList := &fleetnetv1alpha1.EndpointSliceImportList{}
				if err := hubClient.List(ctx, endpointSliceImportList); err != nil {
					return false
				}

				if len(endpointSliceImportList.Items) != 0 {
					return false
				}
				return true
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())

			// Check if the cleanup finalizer has been added.
			Eventually(func() bool {
				if err := hubClient.Get(ctx, endpointSliceExportKey, endpointSliceExport); err != nil {
					return false
				}

				return cmp.Equal(endpointSliceExport.Finalizers, []string{endpointSliceExportCleanupFinalizer})
			})
		})
	})

	Context("new endpointsliceexport (empty serviceimport)", func() {
		var endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport
		var svcImport *fleetnetv1alpha1.ServiceImport

		BeforeEach(func() {
			svcImport = unfulfilledAndRequestedServiceImport()
			svcImport.Annotations = map[string]string{}
			Expect(hubClient.Create(ctx, svcImport)).Should(Succeed())
			fulfillSvcImport(svcImport)
			Expect(hubClient.Status().Update(ctx, svcImport)).Should(Succeed())

			endpointSliceExport = ipv4EndpointSliceExport()
			endpointSliceExport.Finalizers = []string{}
			Expect(hubClient.Create(ctx, endpointSliceExport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, endpointSliceExport)).Should(Succeed())
			// Wait until all EndpointSliceExport resources are cleaned up; this helps make the test less flaky.
			Eventually(func() bool {
				endpointSliceImportList := &fleetnetv1alpha1.EndpointSliceImportList{}
				if err := hubClient.List(ctx, endpointSliceImportList); err != nil {
					return false
				}

				if len(endpointSliceImportList.Items) != 0 {
					return false
				}

				if err := hubClient.Get(ctx, endpointSliceExportKey, endpointSliceExport); !errors.IsNotFound(err) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Expect(hubClient.Delete(ctx, svcImport)).Should(Succeed())
			// Confirm that ServiceImport is deleted; this helps make the test less flaky.
			Eventually(func() error {
				return client.IgnoreNotFound(hubClient.Get(ctx, svcImportKey, svcImport))
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should not distribute endpointslice to member clusters", func() {
			// Check if no EndpointSlice has been distributed.
			Consistently(func() bool {
				endpointSliceImportList := &fleetnetv1alpha1.EndpointSliceImportList{}
				if err := hubClient.List(ctx, endpointSliceImportList); err != nil {
					return false
				}

				if len(endpointSliceImportList.Items) != 0 {
					return false
				}
				return true
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())

			// Check if the cleanup finalizer has been added.
			Eventually(func() bool {
				if err := hubClient.Get(ctx, endpointSliceExportKey, endpointSliceExport); err != nil {
					return false
				}

				return cmp.Equal(endpointSliceExport.Finalizers, []string{endpointSliceExportCleanupFinalizer})
			})
		})
	})

	Context("new endpointsliceexport (bad service in use annotation)", func() {
		var endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport
		var svcImport *fleetnetv1alpha1.ServiceImport

		BeforeEach(func() {
			svcImport = unfulfilledAndRequestedServiceImport()
			svcImport.Annotations = map[string]string{
				objectmeta.ServiceImportAnnotationServiceInUseBy: "xyz",
			}
			Expect(hubClient.Create(ctx, svcImport)).Should(Succeed())
			fulfillSvcImport(svcImport)
			Expect(hubClient.Status().Update(ctx, svcImport)).Should(Succeed())

			endpointSliceExport = ipv4EndpointSliceExport()
			endpointSliceExport.Finalizers = []string{}
			Expect(hubClient.Create(ctx, endpointSliceExport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, endpointSliceExport)).Should(Succeed())
			// Wait until all EndpointSliceExport resources are cleaned up; this helps make the test less flaky.
			Eventually(func() bool {
				endpointSliceImportList := &fleetnetv1alpha1.EndpointSliceImportList{}
				if err := hubClient.List(ctx, endpointSliceImportList); err != nil {
					return false
				}

				if len(endpointSliceImportList.Items) != 0 {
					return false
				}

				if err := hubClient.Get(ctx, endpointSliceExportKey, endpointSliceExport); !errors.IsNotFound(err) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Expect(hubClient.Delete(ctx, svcImport)).Should(Succeed())
			// Confirm that ServiceImport is deleted; this helps make the test less flaky.
			Eventually(func() error {
				return client.IgnoreNotFound(hubClient.Get(ctx, svcImportKey, svcImport))
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should not distribute endpointslice to member clusters", func() {
			// Check if no EndpointSlice has been distributed.
			Consistently(func() bool {
				endpointSliceImportList := &fleetnetv1alpha1.EndpointSliceImportList{}
				if err := hubClient.List(ctx, endpointSliceImportList); err != nil {
					return false
				}

				if len(endpointSliceImportList.Items) != 0 {
					return false
				}
				return true
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())

			// Check if the cleanup finalizer has been added.
			Eventually(func() bool {
				if err := hubClient.Get(ctx, endpointSliceExportKey, endpointSliceExport); err != nil {
					return false
				}

				return cmp.Equal(endpointSliceExport.Finalizers, []string{endpointSliceExportCleanupFinalizer})
			})
		})
	})

	Context("no service in use by annotation", func() {
		var endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport
		var svcImport *fleetnetv1alpha1.ServiceImport

		BeforeEach(func() {
			svcImport = unfulfilledAndRequestedServiceImport()
			svcImport.Annotations = map[string]string{}
			Expect(hubClient.Create(ctx, svcImport)).Should(Succeed())
			fulfillSvcImport(svcImport)
			Expect(hubClient.Status().Update(ctx, svcImport)).Should(Succeed())

			endpointSliceExport = ipv4EndpointSliceExport()
			endpointSliceExport.Finalizers = []string{}
			Expect(hubClient.Create(ctx, endpointSliceExport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, endpointSliceExport)).Should(Succeed())
			// Wait until all EndpointSliceExport related resources are cleaned up; this helps make the test less flaky.
			Eventually(func() bool {
				endpointSliceImportList := &fleetnetv1alpha1.EndpointSliceImportList{}
				if err := hubClient.List(ctx, endpointSliceImportList); err != nil {
					return false
				}

				if len(endpointSliceImportList.Items) != 0 {
					return false
				}

				if err := hubClient.Get(ctx, endpointSliceExportKey, endpointSliceExport); !errors.IsNotFound(err) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Expect(hubClient.Delete(ctx, svcImport)).Should(Succeed())
			// Confirm that ServiceImport is deleted; this helps make the test less flaky.
			Eventually(func() error {
				return client.IgnoreNotFound(hubClient.Get(ctx, svcImportKey, svcImport))
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should not distribute endpointslice to member clusters", func() {
			// Check if no EndpointSlice has been distributed.
			Consistently(func() bool {
				endpointSliceImportList := &fleetnetv1alpha1.EndpointSliceImportList{}
				if err := hubClient.List(ctx, endpointSliceImportList); err != nil {
					return false
				}

				if len(endpointSliceImportList.Items) != 0 {
					return false
				}
				return true
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())

			// Check if the cleanup finalizer has been added.
			Eventually(func() bool {
				if err := hubClient.Get(ctx, endpointSliceExportKey, endpointSliceExport); err != nil {
					return false
				}

				return cmp.Equal(endpointSliceExport.Finalizers, []string{endpointSliceExportCleanupFinalizer})
			})
		})
	})

	Context("new endpointsliceexport", func() {
		var endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport
		var svcImport *fleetnetv1alpha1.ServiceImport

		BeforeEach(func() {
			svcImport = unfulfilledAndRequestedServiceImport()
			Expect(hubClient.Create(ctx, svcImport)).Should(Succeed())
			fulfillSvcImport(svcImport)
			Expect(hubClient.Status().Update(ctx, svcImport)).Should(Succeed())

			endpointSliceExport = ipv4EndpointSliceExport()
			endpointSliceExport.Finalizers = []string{}
			Expect(hubClient.Create(ctx, endpointSliceExport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, endpointSliceExport)).Should(Succeed())
			// Wait until all EndpointSliceExport resources are cleaned up; this helps make the test less flaky.
			Eventually(func() bool {
				endpointSliceImportList := &fleetnetv1alpha1.EndpointSliceImportList{}
				if err := hubClient.List(ctx, endpointSliceImportList); err != nil {
					return false
				}

				if len(endpointSliceImportList.Items) != 0 {
					return false
				}

				if err := hubClient.Get(ctx, endpointSliceExportKey, endpointSliceExport); !errors.IsNotFound(err) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Expect(hubClient.Delete(ctx, svcImport)).Should(Succeed())
			// Confirm that ServiceImport is deleted; this helps make the test less flaky.
			Eventually(func() error {
				return client.IgnoreNotFound(hubClient.Get(ctx, svcImportKey, svcImport))
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should distribute endpointslice to member clusters", func() {
			// Check if the EndpointSlice has been distributed.
			Eventually(func() bool {
				endpointSliceImportList := &fleetnetv1alpha1.EndpointSliceImportList{}
				if err := hubClient.List(ctx, endpointSliceImportList); err != nil {
					return false
				}

				if len(endpointSliceImportList.Items) != 2 {
					return false
				}

				var endpointSliceImportB *fleetnetv1alpha1.EndpointSliceImport
				var endpointSliceImportC *fleetnetv1alpha1.EndpointSliceImport

				for idx := range endpointSliceImportList.Items {
					endpointSliceImport := endpointSliceImportList.Items[idx]
					switch endpointSliceImport.Namespace {
					case hubNSForMemberB:
						endpointSliceImportB = &endpointSliceImport
					case hubNSForMemberC:
						endpointSliceImportC = &endpointSliceImport
					}
				}

				if endpointSliceImportB == nil || !cmp.Equal(endpointSliceImportB.Spec, endpointSliceExport.Spec) {
					return false
				}
				if endpointSliceImportC == nil || !cmp.Equal(endpointSliceImportC.Spec, endpointSliceExport.Spec) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// Check if the cleanup finalizer has been added.
			Eventually(func() bool {
				endpointSliceExport := &fleetnetv1alpha1.EndpointSliceExport{}
				if err := hubClient.Get(ctx, endpointSliceExportKey, endpointSliceExport); err != nil {
					return false
				}

				if !cmp.Equal(endpointSliceExport.Finalizers, []string{endpointSliceExportCleanupFinalizer}) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("updated endpointsliceexport", func() {
		var endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport
		var svcImport *fleetnetv1alpha1.ServiceImport

		newIPAddr := "3.4.5.6"
		newResourceVersion := "1"
		newGeneration := 2

		BeforeEach(func() {
			svcImport = unfulfilledAndRequestedServiceImport()
			Expect(hubClient.Create(ctx, svcImport)).Should(Succeed())
			fulfillSvcImport(svcImport)
			Expect(hubClient.Status().Update(ctx, svcImport)).Should(Succeed())

			endpointSliceExport = ipv4EndpointSliceExport()
			endpointSliceExport.Finalizers = []string{}
			Expect(hubClient.Create(ctx, endpointSliceExport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, endpointSliceExport)).Should(Succeed())
			// Wait until all EndpointSliceExport related resources are cleaned up; this helps make the test less flaky.
			Eventually(func() bool {
				endpointSliceImportList := &fleetnetv1alpha1.EndpointSliceImportList{}
				if err := hubClient.List(ctx, endpointSliceImportList); err != nil {
					return false
				}

				if len(endpointSliceImportList.Items) != 0 {
					return false
				}

				if err := hubClient.Get(ctx, endpointSliceExportKey, endpointSliceExport); !errors.IsNotFound(err) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Expect(hubClient.Delete(ctx, svcImport)).Should(Succeed())
			// Confirm that ServiceImport is deleted; this helps make the test less flaky.
			Eventually(func() error {
				return client.IgnoreNotFound(hubClient.Get(ctx, svcImportKey, svcImport))
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should update distributed endpointslices", func() {
			// Check if the EndpointSlice has been distributed.
			Eventually(func() bool {
				endpointSliceImportList := &fleetnetv1alpha1.EndpointSliceImportList{}
				if err := hubClient.List(ctx, endpointSliceImportList); err != nil {
					return false
				}

				if len(endpointSliceImportList.Items) != 2 {
					return false
				}

				var endpointSliceImportB *fleetnetv1alpha1.EndpointSliceImport
				var endpointSliceImportC *fleetnetv1alpha1.EndpointSliceImport

				for idx := range endpointSliceImportList.Items {
					endpointSliceImport := endpointSliceImportList.Items[idx]
					switch endpointSliceImport.Namespace {
					case hubNSForMemberB:
						endpointSliceImportB = &endpointSliceImport
					case hubNSForMemberC:
						endpointSliceImportC = &endpointSliceImport
					}
				}

				if endpointSliceImportB == nil || !cmp.Equal(endpointSliceImportB.Spec, endpointSliceExport.Spec) {
					return false
				}
				if endpointSliceImportC == nil || !cmp.Equal(endpointSliceImportC.Spec, endpointSliceExport.Spec) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Eventually(func() bool {
				endpointSliceExport := &fleetnetv1alpha1.EndpointSliceExport{}
				if err := hubClient.Get(ctx, endpointSliceExportKey, endpointSliceExport); err != nil {
					return false
				}

				if !cmp.Equal(endpointSliceExport.Finalizers, []string{endpointSliceExportCleanupFinalizer}) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// Update the exported EndpointSlice.
			Expect(hubClient.Get(ctx, endpointSliceExportKey, endpointSliceExport)).Should(Succeed())
			endpointSliceExport.Spec.Endpoints[0].Addresses = []string{newIPAddr}
			endpointSliceExport.Spec.EndpointSliceReference.ResourceVersion = newResourceVersion
			endpointSliceExport.Spec.EndpointSliceReference.Generation = int64(newGeneration)
			Expect(hubClient.Update(ctx, endpointSliceExport)).Should(Succeed())

			// Check if the update has been applied to EndpointSlice copies distributed to member clusters.
			Eventually(func() bool {
				endpointSliceImportList := &fleetnetv1alpha1.EndpointSliceImportList{}
				if err := hubClient.List(ctx, endpointSliceImportList); err != nil {
					return false
				}

				if len(endpointSliceImportList.Items) != 2 {
					return false
				}

				var endpointSliceImportB *fleetnetv1alpha1.EndpointSliceImport
				var endpointSliceImportC *fleetnetv1alpha1.EndpointSliceImport

				for idx := range endpointSliceImportList.Items {
					endpointSliceImport := endpointSliceImportList.Items[idx]
					switch endpointSliceImport.Namespace {
					case hubNSForMemberB:
						endpointSliceImportB = &endpointSliceImport
					case hubNSForMemberC:
						endpointSliceImportC = &endpointSliceImport
					}
				}

				if endpointSliceImportB == nil || !cmp.Equal(endpointSliceImportB.Spec, endpointSliceExport.Spec) {
					return false
				}
				if endpointSliceImportC == nil || !cmp.Equal(endpointSliceImportC.Spec, endpointSliceExport.Spec) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("service in use by info changed", func() {
		var endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport
		var svcImport *fleetnetv1alpha1.ServiceImport

		BeforeEach(func() {
			svcImport = unfulfilledAndRequestedServiceImport()
			Expect(hubClient.Create(ctx, svcImport)).Should(Succeed())
			fulfillSvcImport(svcImport)
			Expect(hubClient.Status().Update(ctx, svcImport)).Should(Succeed())

			endpointSliceExport = ipv4EndpointSliceExport()
			endpointSliceExport.Finalizers = []string{}
			Expect(hubClient.Create(ctx, endpointSliceExport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, endpointSliceExport)).Should(Succeed())
			// Wait until all EndpointSliceExport related resources are cleaned up; this helps make the test less flaky.
			Eventually(func() bool {
				endpointSliceImportList := &fleetnetv1alpha1.EndpointSliceImportList{}
				if err := hubClient.List(ctx, endpointSliceImportList); err != nil {
					return false
				}

				if len(endpointSliceImportList.Items) != 0 {
					return false
				}

				if err := hubClient.Get(ctx, endpointSliceExportKey, endpointSliceExport); !errors.IsNotFound(err) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Expect(hubClient.Delete(ctx, svcImport)).Should(Succeed())
			// Confirm that ServiceImport is deleted; this helps make the test less flaky.
			Eventually(func() error {
				return client.IgnoreNotFound(hubClient.Get(ctx, svcImportKey, svcImport))
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should re-distribute endpointslice copies (unimports + new imports)", func() {
			// Check if the EndpointSlice has been distributed.
			Eventually(func() bool {
				endpointSliceImportList := &fleetnetv1alpha1.EndpointSliceImportList{}
				if err := hubClient.List(ctx, endpointSliceImportList); err != nil {
					return false
				}

				if len(endpointSliceImportList.Items) != 2 {
					return false
				}

				var endpointSliceImportB *fleetnetv1alpha1.EndpointSliceImport
				var endpointSliceImportC *fleetnetv1alpha1.EndpointSliceImport

				for idx := range endpointSliceImportList.Items {
					endpointSliceImport := endpointSliceImportList.Items[idx]
					switch endpointSliceImport.Namespace {
					case hubNSForMemberB:
						endpointSliceImportB = &endpointSliceImport
					case hubNSForMemberC:
						endpointSliceImportC = &endpointSliceImport
					}
				}

				if endpointSliceImportB == nil || !cmp.Equal(endpointSliceImportB.Spec, endpointSliceExport.Spec) {
					return false
				}
				if endpointSliceImportC == nil || !cmp.Equal(endpointSliceImportC.Spec, endpointSliceExport.Spec) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// Check if the cleanup finalizer has been added.
			Eventually(func() bool {
				endpointSliceExport := &fleetnetv1alpha1.EndpointSliceExport{}
				if err := hubClient.Get(ctx, endpointSliceExportKey, endpointSliceExport); err != nil {
					return false
				}

				if !cmp.Equal(endpointSliceExport.Finalizers, []string{endpointSliceExportCleanupFinalizer}) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// Update ServiceInUseBy data.
			updatedSvcInUseBy := fleetnetv1alpha1.ServiceInUseBy{
				MemberClusters: map[fleetnetv1alpha1.ClusterNamespace]fleetnetv1alpha1.ClusterID{
					hubNSForMemberA: clusterIDForMemberA,
					hubNSForMemberC: clusterIDForMemberC,
				},
			}
			updatedSvcInUseByData, err := json.Marshal(updatedSvcInUseBy)
			Expect(updatedSvcInUseByData).ToNot(BeNil())
			Expect(err).To(BeNil())

			Expect(hubClient.Get(ctx, svcImportKey, svcImport)).Should(Succeed())
			svcImport.Annotations[objectmeta.ServiceImportAnnotationServiceInUseBy] = string(updatedSvcInUseByData)
			Expect(hubClient.Update(ctx, svcImport)).Should(Succeed())

			// Check if the EndpointSlice has been re-distributed.
			Eventually(func() bool {
				endpointSliceImportList := &fleetnetv1alpha1.EndpointSliceImportList{}
				if err := hubClient.List(ctx, endpointSliceImportList); err != nil {
					return false
				}

				if len(endpointSliceImportList.Items) != 2 {
					return false
				}

				var endpointSliceImportA *fleetnetv1alpha1.EndpointSliceImport
				var endpointSliceImportC *fleetnetv1alpha1.EndpointSliceImport

				for idx := range endpointSliceImportList.Items {
					endpointSliceImport := endpointSliceImportList.Items[idx]
					switch endpointSliceImport.Namespace {
					case hubNSForMemberA:
						endpointSliceImportA = &endpointSliceImport
					case hubNSForMemberC:
						endpointSliceImportC = &endpointSliceImport
					}
				}

				if endpointSliceImportA == nil || !cmp.Equal(endpointSliceImportA.Spec, endpointSliceExport.Spec) {
					return false
				}
				if endpointSliceImportC == nil || !cmp.Equal(endpointSliceImportC.Spec, endpointSliceExport.Spec) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})

		It("should re-distribute endpointslice copies (unimports)", func() {
			// Check if the EndpointSlice has been distributed.
			Eventually(func() bool {
				endpointSliceImportList := &fleetnetv1alpha1.EndpointSliceImportList{}
				if err := hubClient.List(ctx, endpointSliceImportList); err != nil {
					return false
				}

				if len(endpointSliceImportList.Items) != 2 {
					return false
				}

				var endpointSliceImportB *fleetnetv1alpha1.EndpointSliceImport
				var endpointSliceImportC *fleetnetv1alpha1.EndpointSliceImport

				for idx := range endpointSliceImportList.Items {
					endpointSliceImport := endpointSliceImportList.Items[idx]
					switch endpointSliceImport.Namespace {
					case hubNSForMemberB:
						endpointSliceImportB = &endpointSliceImport
					case hubNSForMemberC:
						endpointSliceImportC = &endpointSliceImport
					}
				}

				if endpointSliceImportB == nil || !cmp.Equal(endpointSliceImportB.Spec, endpointSliceExport.Spec) {
					return false
				}
				if endpointSliceImportC == nil || !cmp.Equal(endpointSliceImportC.Spec, endpointSliceExport.Spec) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// Check if the cleanup finalizer has been added.
			Eventually(func() bool {
				endpointSliceExport := &fleetnetv1alpha1.EndpointSliceExport{}
				if err := hubClient.Get(ctx, endpointSliceExportKey, endpointSliceExport); err != nil {
					return false
				}

				if !cmp.Equal(endpointSliceExport.Finalizers, []string{endpointSliceExportCleanupFinalizer}) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// Update ServiceInUseBy data.
			updatedSvcInUseBy := fleetnetv1alpha1.ServiceInUseBy{
				MemberClusters: map[fleetnetv1alpha1.ClusterNamespace]fleetnetv1alpha1.ClusterID{},
			}
			updatedSvcInUseByData, err := json.Marshal(updatedSvcInUseBy)
			Expect(updatedSvcInUseByData).ToNot(BeNil())
			Expect(err).To(BeNil())

			Expect(hubClient.Get(ctx, svcImportKey, svcImport)).Should(Succeed())
			svcImport.Annotations[objectmeta.ServiceImportAnnotationServiceInUseBy] = string(updatedSvcInUseByData)
			Expect(hubClient.Update(ctx, svcImport)).Should(Succeed())

			// Check if the EndpointSlice has been re-distributed.
			Eventually(func() bool {
				endpointSliceImportList := &fleetnetv1alpha1.EndpointSliceImportList{}
				if err := hubClient.List(ctx, endpointSliceImportList); err != nil {
					return false
				}

				return len(endpointSliceImportList.Items) == 0
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})

		It("should re-distribute endpointslice copies (new imports)", func() {
			// Check if the EndpointSlice has been distributed.
			Eventually(func() bool {
				endpointSliceImportList := &fleetnetv1alpha1.EndpointSliceImportList{}
				if err := hubClient.List(ctx, endpointSliceImportList); err != nil {
					return false
				}

				if len(endpointSliceImportList.Items) != 2 {
					return false
				}

				var endpointSliceImportB *fleetnetv1alpha1.EndpointSliceImport
				var endpointSliceImportC *fleetnetv1alpha1.EndpointSliceImport

				for idx := range endpointSliceImportList.Items {
					endpointSliceImport := endpointSliceImportList.Items[idx]
					switch endpointSliceImport.Namespace {
					case hubNSForMemberB:
						endpointSliceImportB = &endpointSliceImport
					case hubNSForMemberC:
						endpointSliceImportC = &endpointSliceImport
					}
				}

				if endpointSliceImportB == nil || !cmp.Equal(endpointSliceImportB.Spec, endpointSliceExport.Spec) {
					return false
				}
				if endpointSliceImportC == nil || !cmp.Equal(endpointSliceImportC.Spec, endpointSliceExport.Spec) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// Check if the cleanup finalizer has been added.
			Eventually(func() bool {
				endpointSliceExport := &fleetnetv1alpha1.EndpointSliceExport{}
				if err := hubClient.Get(ctx, endpointSliceExportKey, endpointSliceExport); err != nil {
					return false
				}

				if !cmp.Equal(endpointSliceExport.Finalizers, []string{endpointSliceExportCleanupFinalizer}) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// Update ServiceInUseBy data.
			updatedSvcInUseBy := fleetnetv1alpha1.ServiceInUseBy{
				MemberClusters: map[fleetnetv1alpha1.ClusterNamespace]fleetnetv1alpha1.ClusterID{
					hubNSForMemberA: clusterIDForMemberA,
					hubNSForMemberB: clusterIDForMemberB,
					hubNSForMemberC: clusterIDForMemberC,
				},
			}
			updatedSvcInUseByData, err := json.Marshal(updatedSvcInUseBy)
			Expect(updatedSvcInUseByData).ToNot(BeNil())
			Expect(err).To(BeNil())

			Expect(hubClient.Get(ctx, svcImportKey, svcImport)).Should(Succeed())
			svcImport.Annotations[objectmeta.ServiceImportAnnotationServiceInUseBy] = string(updatedSvcInUseByData)
			Expect(hubClient.Update(ctx, svcImport)).Should(Succeed())

			// Check if the EndpointSlice has been re-distributed.
			Eventually(func() bool {
				endpointSliceImportList := &fleetnetv1alpha1.EndpointSliceImportList{}
				if err := hubClient.List(ctx, endpointSliceImportList); err != nil {
					return false
				}

				if len(endpointSliceImportList.Items) != 3 {
					return false
				}

				var endpointSliceImportA *fleetnetv1alpha1.EndpointSliceImport
				var endpointSliceImportB *fleetnetv1alpha1.EndpointSliceImport
				var endpointSliceImportC *fleetnetv1alpha1.EndpointSliceImport

				for idx := range endpointSliceImportList.Items {
					endpointSliceImport := endpointSliceImportList.Items[idx]
					switch endpointSliceImport.Namespace {
					case hubNSForMemberA:
						endpointSliceImportA = &endpointSliceImport
					case hubNSForMemberB:
						endpointSliceImportB = &endpointSliceImport
					case hubNSForMemberC:
						endpointSliceImportC = &endpointSliceImport
					}
				}

				if endpointSliceImportA == nil || !cmp.Equal(endpointSliceImportA.Spec, endpointSliceExport.Spec) {
					return false
				}
				if endpointSliceImportB == nil || !cmp.Equal(endpointSliceImportB.Spec, endpointSliceExport.Spec) {
					return false
				}
				if endpointSliceImportC == nil || !cmp.Equal(endpointSliceImportC.Spec, endpointSliceExport.Spec) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})
})
