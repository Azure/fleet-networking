/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package serviceexport

import (
	"fmt"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
)

const (
	MemberClusterID  = "bravelion"
	svcPortName      = "port1"
	svcPort          = 80
	targetPort       = 8080
	externalNameAddr = "example.com"

	eventuallyTimeout    = time.Second * 10
	eventuallyInterval   = time.Millisecond * 250
	consistentlyDuration = time.Millisecond * 1000
	consistentlyInterval = time.Millisecond * 50
)

// clusterIPService returns a Service of ClusterIP type.
func clusterIPService() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      svcName,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port:       svcPort,
					TargetPort: intstr.FromInt(targetPort),
				},
			},
		},
	}
}

// headlessService returns a headless Service.
func headlessService() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      svcName,
		},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: "None",
			Selector: map[string]string{
				"app": "redis",
			},
		},
	}
}

// externalNameService returns a Service of ExternalName type.
func externalNameService() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      svcName,
		},
		Spec: corev1.ServiceSpec{
			Type:         corev1.ServiceTypeExternalName,
			ExternalName: externalNameAddr,
		},
	}
}

func notYetFulfilledServiceExport() *fleetnetv1alpha1.ServiceExport {
	return &fleetnetv1alpha1.ServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      svcName,
		},
	}
}

var svcOrSvcExportKey = types.NamespacedName{
	Namespace: memberUserNS,
	Name:      svcName,
}
var internalSvcExportKey = types.NamespacedName{
	Namespace: hubNSForMember,
	Name:      fmt.Sprintf("%s-%s", memberUserNS, svcName),
}

var ignoredRefFields = cmpopts.IgnoreFields(fleetnetv1alpha1.ExportedObjectReference{}, "ResourceVersion")

var _ = Describe("serviceexport controller", func() {
	Context("export non-existent service", func() {
		var svcExport = &fleetnetv1alpha1.ServiceExport{}

		BeforeEach(func() {
			svcExport = notYetFulfilledServiceExport()
			Expect(MemberClient.Create(ctx, svcExport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(MemberClient.Delete(ctx, svcExport)).Should(Succeed())
		})

		It("should mark the service export as invalid + should not export service", func() {
			Eventually(func() bool {
				if err := MemberClient.Get(ctx, svcOrSvcExportKey, svcExport); err != nil {
					return false
				}

				if len(svcExport.Finalizers) != 0 {
					return false
				}

				expectedCond := serviceExportInvalidNotFoundCondition(memberUserNS, svcName)
				validCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportValid))
				return cmp.Equal(validCond, &expectedCond, ignoredCondFields)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Consistently(func() bool {
				internalSvcExportList := &fleetnetv1alpha1.InternalServiceExportList{}
				if err := HubClient.List(ctx, internalSvcExportList, &client.ListOptions{Namespace: hubNSForMember}); err != nil {
					return false
				}

				return len(internalSvcExportList.Items) == 0
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())
		})
	})

	Context("export existing service", func() {
		var svcExport = &fleetnetv1alpha1.ServiceExport{}
		var svc = &corev1.Service{}

		BeforeEach(func() {
			svc = clusterIPService()
			Expect(MemberClient.Create(ctx, svc)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(MemberClient.Delete(ctx, svcExport)).Should(Succeed())
			Expect(MemberClient.Delete(ctx, svc)).Should(Succeed())

			// Confirm that the Service has been unexported; this helps make the tests less flaky.
			Eventually(func() bool {
				internalSvcExportList := &fleetnetv1alpha1.InternalServiceExportList{}
				if err := HubClient.List(ctx, internalSvcExportList, &client.ListOptions{Namespace: hubNSForMember}); err != nil {
					return false
				}

				return len(internalSvcExportList.Items) == 0
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Eventually(func() bool {
				if err := MemberClient.Get(ctx, svcOrSvcExportKey, svcExport); err == nil || !errors.IsNotFound(err) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})

		It("should mark the service export as valid + should export the service", func() {
			svcExport = notYetFulfilledServiceExport()
			Expect(MemberClient.Create(ctx, svcExport)).Should(Succeed())

			Eventually(func() bool {
				if err := MemberClient.Get(ctx, svcOrSvcExportKey, svcExport); err != nil {
					return false
				}

				if !cmp.Equal(svcExport.Finalizers, []string{objectmeta.ServiceExportCleanupFinalizer}) {
					return false
				}

				expectedValidCond := serviceExportValidCondition(memberUserNS, svcName)
				validCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportValid))
				if !cmp.Equal(validCond, &expectedValidCond, ignoredCondFields) {
					return false
				}

				expectedConflictCond := serviceExportPendingConflictResolutionCondition(memberUserNS, svc.Name)
				conflictCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportConflict))
				return cmp.Equal(conflictCond, &expectedConflictCond, ignoredCondFields)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Eventually(func() bool {
				internalSvcExport := &fleetnetv1alpha1.InternalServiceExport{}
				if err := HubClient.Get(ctx, internalSvcExportKey, internalSvcExport); err != nil {
					return false
				}

				expectedInternalSvcExportSpec := fleetnetv1alpha1.InternalServiceExportSpec{
					Ports: []fleetnetv1alpha1.ServicePort{
						{
							Protocol:   corev1.ProtocolTCP,
							Port:       svcPort,
							TargetPort: intstr.FromInt(targetPort),
						},
					},
					ServiceReference: fleetnetv1alpha1.FromMetaObjects(
						MemberClusterID,
						svc.TypeMeta,
						svc.ObjectMeta,
					),
				}
				return cmp.Equal(internalSvcExport.Spec, expectedInternalSvcExportSpec, ignoredRefFields)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("export new service", func() {
		var svcExport = &fleetnetv1alpha1.ServiceExport{}
		var svc = &corev1.Service{}

		BeforeEach(func() {
			svcExport = notYetFulfilledServiceExport()
			Expect(MemberClient.Create(ctx, svcExport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(MemberClient.Delete(ctx, svcExport)).Should(Succeed())
			Expect(MemberClient.Delete(ctx, svc)).Should(Succeed())

			// Confirm that the Service has been unexported; this helps make the tests less flaky.
			Eventually(func() bool {
				internalSvcExportList := &fleetnetv1alpha1.InternalServiceExportList{}
				if err := HubClient.List(ctx, internalSvcExportList, &client.ListOptions{Namespace: hubNSForMember}); err != nil {
					return false
				}

				return len(internalSvcExportList.Items) == 0
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Eventually(func() bool {
				if err := MemberClient.Get(ctx, svcOrSvcExportKey, svcExport); err == nil || !errors.IsNotFound(err) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})

		It("should mark the service export as valid + should export the service", func() {
			svc = clusterIPService()
			Expect(MemberClient.Create(ctx, svc)).Should(Succeed())

			Eventually(func() bool {
				if err := MemberClient.Get(ctx, svcOrSvcExportKey, svcExport); err != nil {
					return false
				}

				if !cmp.Equal(svcExport.Finalizers, []string{objectmeta.ServiceExportCleanupFinalizer}) {
					return false
				}

				expectedValidCond := serviceExportValidCondition(memberUserNS, svcName)
				validCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportValid))
				if !cmp.Equal(validCond, &expectedValidCond, ignoredCondFields) {
					return false
				}

				expectedConflictCond := serviceExportPendingConflictResolutionCondition(memberUserNS, svc.Name)
				conflictCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportConflict))
				return cmp.Equal(conflictCond, &expectedConflictCond, ignoredCondFields)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Eventually(func() bool {
				internalSvcExport := &fleetnetv1alpha1.InternalServiceExport{}
				if err := HubClient.Get(ctx, internalSvcExportKey, internalSvcExport); err != nil {
					return false
				}

				expectedInternalSvcExportSpec := fleetnetv1alpha1.InternalServiceExportSpec{
					Ports: []fleetnetv1alpha1.ServicePort{
						{
							Protocol:   corev1.ProtocolTCP,
							Port:       svcPort,
							TargetPort: intstr.FromInt(targetPort),
						},
					},
					ServiceReference: fleetnetv1alpha1.FromMetaObjects(
						MemberClusterID,
						svc.TypeMeta,
						svc.ObjectMeta,
					),
				}
				return cmp.Equal(internalSvcExport.Spec, expectedInternalSvcExportSpec, ignoredRefFields)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("spec change in exported service", func() {
		var svcExport = &fleetnetv1alpha1.ServiceExport{}
		var svc = &corev1.Service{}
		altSvcPortName := "port2"
		altSvcPort := 81
		altTargetPort := 8081

		BeforeEach(func() {
			svcExport = notYetFulfilledServiceExport()
			Expect(MemberClient.Create(ctx, svcExport)).Should(Succeed())

			svc = clusterIPService()
			Expect(MemberClient.Create(ctx, svc)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(MemberClient.Delete(ctx, svcExport)).Should(Succeed())
			Expect(MemberClient.Delete(ctx, svc)).Should(Succeed())

			// Confirm that the Service has been unexported; this helps make the tests less flaky.
			Eventually(func() bool {
				internalSvcExportList := &fleetnetv1alpha1.InternalServiceExportList{}
				if err := HubClient.List(ctx, internalSvcExportList, &client.ListOptions{Namespace: hubNSForMember}); err != nil {
					return false
				}

				return len(internalSvcExportList.Items) == 0
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Eventually(func() bool {
				if err := MemberClient.Get(ctx, svcOrSvcExportKey, svcExport); err == nil || !errors.IsNotFound(err) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})

		It("should update the exported service", func() {
			By("confirm that the service has been exported")
			Eventually(func() bool {
				if err := MemberClient.Get(ctx, svcOrSvcExportKey, svcExport); err != nil {
					return false
				}

				if !cmp.Equal(svcExport.Finalizers, []string{objectmeta.ServiceExportCleanupFinalizer}) {
					return false
				}

				expectedValidCond := serviceExportValidCondition(memberUserNS, svcName)
				validCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportValid))
				if !cmp.Equal(validCond, &expectedValidCond, ignoredCondFields) {
					return false
				}

				expectedConflictCond := serviceExportPendingConflictResolutionCondition(memberUserNS, svc.Name)
				conflictCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportConflict))
				return cmp.Equal(conflictCond, &expectedConflictCond, ignoredCondFields)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Eventually(func() bool {
				internalSvcExport := &fleetnetv1alpha1.InternalServiceExport{}
				if err := HubClient.Get(ctx, internalSvcExportKey, internalSvcExport); err != nil {
					return false
				}

				expectedInternalSvcExportSpec := fleetnetv1alpha1.InternalServiceExportSpec{
					Ports: []fleetnetv1alpha1.ServicePort{
						{
							Protocol:   corev1.ProtocolTCP,
							Port:       svcPort,
							TargetPort: intstr.FromInt(targetPort),
						},
					},
					ServiceReference: fleetnetv1alpha1.FromMetaObjects(
						MemberClusterID,
						svc.TypeMeta,
						svc.ObjectMeta,
					),
				}
				return cmp.Equal(internalSvcExport.Spec, expectedInternalSvcExportSpec, ignoredRefFields)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			By("update the service")
			Expect(MemberClient.Get(ctx, svcOrSvcExportKey, svc)).Should(Succeed())
			svc.Spec.Ports = []corev1.ServicePort{
				{
					Name:       svcPortName,
					Port:       svcPort,
					TargetPort: intstr.FromInt(targetPort),
				},
				{
					Name:       altSvcPortName,
					Port:       int32(altSvcPort),
					TargetPort: intstr.FromInt(altTargetPort),
				},
			}
			Expect(MemberClient.Update(ctx, svc)).Should(Succeed())

			By("confirm that the exported service has been updated")
			Eventually(func() bool {
				internalSvcExport := &fleetnetv1alpha1.InternalServiceExport{}
				if err := HubClient.Get(ctx, internalSvcExportKey, internalSvcExport); err != nil {
					return false
				}

				expectedInternalSvcExportSpec := fleetnetv1alpha1.InternalServiceExportSpec{
					Ports: []fleetnetv1alpha1.ServicePort{
						{
							Name:       svcPortName,
							Protocol:   corev1.ProtocolTCP,
							Port:       svcPort,
							TargetPort: intstr.FromInt(targetPort),
						},
						{
							Name:       altSvcPortName,
							Protocol:   corev1.ProtocolTCP,
							Port:       int32(altSvcPort),
							TargetPort: intstr.FromInt(altTargetPort),
						},
					},
					ServiceReference: fleetnetv1alpha1.FromMetaObjects(
						MemberClusterID,
						svc.TypeMeta,
						svc.ObjectMeta,
					),
				}
				return cmp.Equal(internalSvcExport.Spec, expectedInternalSvcExportSpec, ignoredRefFields)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("unexport service", func() {
		var svcExport = &fleetnetv1alpha1.ServiceExport{}
		var svc = &corev1.Service{}

		BeforeEach(func() {
			svcExport = notYetFulfilledServiceExport()
			Expect(MemberClient.Create(ctx, svcExport)).Should(Succeed())

			svc = clusterIPService()
			Expect(MemberClient.Create(ctx, svc)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(MemberClient.Delete(ctx, svc)).Should(Succeed())
		})

		It("should unexport the service when service export is deleted", func() {
			By("confirm that the service has been exported")
			Eventually(func() bool {
				if err := MemberClient.Get(ctx, svcOrSvcExportKey, svcExport); err != nil {
					return false
				}

				if !cmp.Equal(svcExport.Finalizers, []string{objectmeta.ServiceExportCleanupFinalizer}) {
					return false
				}

				expectedValidCond := serviceExportValidCondition(memberUserNS, svcName)
				validCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportValid))
				if !cmp.Equal(validCond, &expectedValidCond, ignoredCondFields) {
					return false
				}

				expectedConflictCond := serviceExportPendingConflictResolutionCondition(memberUserNS, svc.Name)
				conflictCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportConflict))
				return cmp.Equal(conflictCond, &expectedConflictCond, ignoredCondFields)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Eventually(func() bool {
				internalSvcExport := &fleetnetv1alpha1.InternalServiceExport{}
				if err := HubClient.Get(ctx, internalSvcExportKey, internalSvcExport); err != nil {
					return false
				}

				expectedInternalSvcExportSpec := fleetnetv1alpha1.InternalServiceExportSpec{
					Ports: []fleetnetv1alpha1.ServicePort{
						{
							Protocol:   corev1.ProtocolTCP,
							Port:       svcPort,
							TargetPort: intstr.FromInt(targetPort),
						},
					},
					ServiceReference: fleetnetv1alpha1.FromMetaObjects(
						MemberClusterID,
						svc.TypeMeta,
						svc.ObjectMeta,
					),
				}
				return cmp.Equal(internalSvcExport.Spec, expectedInternalSvcExportSpec, ignoredRefFields)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			By("delete the service export")
			Expect(MemberClient.Delete(ctx, svcExport)).Should(Succeed())

			By("confirm that the service has been unexported")
			Eventually(func() bool {
				internalSvcExportList := &fleetnetv1alpha1.InternalServiceExportList{}
				if err := HubClient.List(ctx, internalSvcExportList, &client.ListOptions{Namespace: hubNSForMember}); err != nil {
					return false
				}

				return len(internalSvcExportList.Items) == 0
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("deleted exported service", func() {
		var svcExport = &fleetnetv1alpha1.ServiceExport{}
		var svc = &corev1.Service{}

		BeforeEach(func() {
			svcExport = notYetFulfilledServiceExport()
			Expect(MemberClient.Create(ctx, svcExport)).Should(Succeed())

			svc = clusterIPService()
			Expect(MemberClient.Create(ctx, svc)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(MemberClient.Delete(ctx, svcExport)).Should(Succeed())
		})

		It("should unexport the service when service is deleted", func() {
			By("confirm that the service has been exported")
			Eventually(func() bool {
				if err := MemberClient.Get(ctx, svcOrSvcExportKey, svcExport); err != nil {
					return false
				}

				if !cmp.Equal(svcExport.Finalizers, []string{objectmeta.ServiceExportCleanupFinalizer}) {
					return false
				}

				expectedValidCond := serviceExportValidCondition(memberUserNS, svcName)
				validCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportValid))
				if !cmp.Equal(validCond, &expectedValidCond, ignoredCondFields) {
					return false
				}

				expectedConflictCond := serviceExportPendingConflictResolutionCondition(memberUserNS, svc.Name)
				conflictCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportConflict))
				return cmp.Equal(conflictCond, &expectedConflictCond, ignoredCondFields)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Eventually(func() bool {
				internalSvcExport := &fleetnetv1alpha1.InternalServiceExport{}
				if err := HubClient.Get(ctx, internalSvcExportKey, internalSvcExport); err != nil {
					return false
				}

				expectedInternalSvcExportSpec := fleetnetv1alpha1.InternalServiceExportSpec{
					Ports: []fleetnetv1alpha1.ServicePort{
						{
							Protocol:   corev1.ProtocolTCP,
							Port:       svcPort,
							TargetPort: intstr.FromInt(targetPort),
						},
					},
					ServiceReference: fleetnetv1alpha1.FromMetaObjects(
						MemberClusterID,
						svc.TypeMeta,
						svc.ObjectMeta,
					),
				}
				return cmp.Equal(internalSvcExport.Spec, expectedInternalSvcExportSpec, ignoredRefFields)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			By("delete the service")
			Expect(MemberClient.Delete(ctx, svc)).Should(Succeed())

			By("confirm that the service has been unexported")
			Eventually(func() bool {
				internalSvcExportList := &fleetnetv1alpha1.InternalServiceExportList{}
				if err := HubClient.List(ctx, internalSvcExportList, &client.ListOptions{Namespace: hubNSForMember}); err != nil {
					return false
				}

				return len(internalSvcExportList.Items) == 0
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Eventually(func() bool {
				if err := MemberClient.Get(ctx, svcOrSvcExportKey, svc); err == nil || !errors.IsNotFound(err) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Eventually(func() bool {
				if err := MemberClient.Get(ctx, svcOrSvcExportKey, svcExport); err != nil {
					return false
				}

				if len(svcExport.Finalizers) != 0 {
					return false
				}

				expectedCond := serviceExportInvalidNotFoundCondition(memberUserNS, svcName)
				validCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportValid))
				return cmp.Equal(validCond, &expectedCond, ignoredCondFields)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("export ineligible service (headless)", func() {
		var svcExport = &fleetnetv1alpha1.ServiceExport{}
		var svc = &corev1.Service{}

		BeforeEach(func() {
			svcExport = notYetFulfilledServiceExport()
			Expect(MemberClient.Create(ctx, svcExport)).Should(Succeed())

			svc = headlessService()
			Expect(MemberClient.Create(ctx, svc)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(MemberClient.Delete(ctx, svcExport)).Should(Succeed())
			Expect(MemberClient.Delete(ctx, svc)).Should(Succeed())
		})

		It("should mark the service export as invalid (ineligible) + should not export headless service", func() {
			Eventually(func() bool {
				if err := MemberClient.Get(ctx, svcOrSvcExportKey, svcExport); err != nil {
					return false
				}

				if len(svcExport.Finalizers) != 0 {
					return false
				}

				expectedCond := serviceExportInvalidIneligibleCondition(memberUserNS, svcName)
				validCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportValid))
				return cmp.Equal(validCond, &expectedCond, ignoredCondFields)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Consistently(func() bool {
				internalSvcExportList := &fleetnetv1alpha1.InternalServiceExportList{}
				if err := HubClient.List(ctx, internalSvcExportList, &client.ListOptions{Namespace: hubNSForMember}); err != nil {
					return false
				}

				return len(internalSvcExportList.Items) == 0
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())
		})
	})

	Context("export ineligible service (external name)", func() {
		var svcExport = &fleetnetv1alpha1.ServiceExport{}
		var svc = &corev1.Service{}

		BeforeEach(func() {
			svcExport = notYetFulfilledServiceExport()
			Expect(MemberClient.Create(ctx, svcExport)).Should(Succeed())

			svc = externalNameService()
			Expect(MemberClient.Create(ctx, svc)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(MemberClient.Delete(ctx, svcExport)).Should(Succeed())
			Expect(MemberClient.Delete(ctx, svc)).Should(Succeed())
		})

		It("should mark the service export as invalid (ineligible) + should not export external name service", func() {
			Eventually(func() bool {
				if err := MemberClient.Get(ctx, svcOrSvcExportKey, svcExport); err != nil {
					return false
				}

				if len(svcExport.Finalizers) != 0 {
					return false
				}

				expectedCond := serviceExportInvalidIneligibleCondition(memberUserNS, svcName)
				validCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportValid))
				return cmp.Equal(validCond, &expectedCond, ignoredCondFields)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Consistently(func() bool {
				internalSvcExportList := &fleetnetv1alpha1.InternalServiceExportList{}
				if err := HubClient.List(ctx, internalSvcExportList, &client.ListOptions{Namespace: hubNSForMember}); err != nil {
					return false
				}

				return len(internalSvcExportList.Items) == 0
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())
		})
	})

	Context("unexport service that becomes ineligible for export (external name)", func() {
		var svcExport = &fleetnetv1alpha1.ServiceExport{}
		var svc = &corev1.Service{}

		BeforeEach(func() {
			svcExport = notYetFulfilledServiceExport()
			Expect(MemberClient.Create(ctx, svcExport)).Should(Succeed())

			svc = clusterIPService()
			Expect(MemberClient.Create(ctx, svc)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(MemberClient.Delete(ctx, svcExport)).Should(Succeed())
			Expect(MemberClient.Delete(ctx, svc)).Should(Succeed())
		})

		It("should mark the service export as invalid (ineligible) + should unexport the service", func() {
			By("confirm that the service has been exported")
			Eventually(func() bool {
				if err := MemberClient.Get(ctx, svcOrSvcExportKey, svcExport); err != nil {
					return false
				}

				if !cmp.Equal(svcExport.Finalizers, []string{objectmeta.ServiceExportCleanupFinalizer}) {
					return false
				}

				expectedValidCond := serviceExportValidCondition(memberUserNS, svcName)
				validCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportValid))
				if !cmp.Equal(validCond, &expectedValidCond, ignoredCondFields) {
					return false
				}

				expectedConflictCond := serviceExportPendingConflictResolutionCondition(memberUserNS, svc.Name)
				conflictCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportConflict))
				return cmp.Equal(conflictCond, &expectedConflictCond, ignoredCondFields)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Eventually(func() bool {
				internalSvcExport := &fleetnetv1alpha1.InternalServiceExport{}
				if err := HubClient.Get(ctx, internalSvcExportKey, internalSvcExport); err != nil {
					return false
				}

				expectedInternalSvcExportSpec := fleetnetv1alpha1.InternalServiceExportSpec{
					Ports: []fleetnetv1alpha1.ServicePort{
						{
							Protocol:   corev1.ProtocolTCP,
							Port:       svcPort,
							TargetPort: intstr.FromInt(targetPort),
						},
					},
					ServiceReference: fleetnetv1alpha1.FromMetaObjects(
						MemberClusterID,
						svc.TypeMeta,
						svc.ObjectMeta,
					),
				}
				return cmp.Equal(internalSvcExport.Spec, expectedInternalSvcExportSpec, ignoredRefFields)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			By("update the service; set it to an external name service")
			Expect(MemberClient.Get(ctx, svcOrSvcExportKey, svc)).Should(Succeed())
			svc.Spec.Type = corev1.ServiceTypeExternalName
			svc.Spec.Ports = []corev1.ServicePort{}
			svc.Spec.ExternalName = externalNameAddr
			Expect(MemberClient.Update(ctx, svc)).Should(Succeed())

			By("confirm that the service has been unexported")
			Eventually(func() bool {
				internalSvcExportList := &fleetnetv1alpha1.InternalServiceExportList{}
				if err := HubClient.List(ctx, internalSvcExportList, &client.ListOptions{Namespace: hubNSForMember}); err != nil {
					return false
				}

				return len(internalSvcExportList.Items) == 0
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Eventually(func() bool {
				if err := MemberClient.Get(ctx, svcOrSvcExportKey, svcExport); err != nil {
					return false
				}

				if len(svcExport.Finalizers) != 0 {
					return false
				}

				expectedCond := serviceExportInvalidIneligibleCondition(memberUserNS, svcName)
				validCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportValid))
				return cmp.Equal(validCond, &expectedCond, ignoredCondFields)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("export service that becomes eligible for export", func() {
		var svcExport = &fleetnetv1alpha1.ServiceExport{}
		var svc = &corev1.Service{}

		BeforeEach(func() {
			svcExport = notYetFulfilledServiceExport()
			Expect(MemberClient.Create(ctx, svcExport)).Should(Succeed())

			svc = externalNameService()
			Expect(MemberClient.Create(ctx, svc)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(MemberClient.Delete(ctx, svcExport)).Should(Succeed())
			Expect(MemberClient.Delete(ctx, svc)).Should(Succeed())

			// Confirm that the Service has been unexported; this helps make the tests less flaky.
			Eventually(func() bool {
				internalSvcExportList := &fleetnetv1alpha1.InternalServiceExportList{}
				if err := HubClient.List(ctx, internalSvcExportList, &client.ListOptions{Namespace: hubNSForMember}); err != nil {
					return false
				}

				return len(internalSvcExportList.Items) == 0
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Eventually(func() bool {
				if err := MemberClient.Get(ctx, svcOrSvcExportKey, svcExport); err == nil || !errors.IsNotFound(err) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})

		It("should mark the service export as valid + should export the service", func() {
			By("confirm that the service has not been exported")
			Consistently(func() bool {
				internalSvcExportList := &fleetnetv1alpha1.InternalServiceExportList{}
				if err := HubClient.List(ctx, internalSvcExportList, &client.ListOptions{Namespace: hubNSForMember}); err != nil {
					return false
				}

				return len(internalSvcExportList.Items) == 0
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())

			Eventually(func() bool {
				if err := MemberClient.Get(ctx, svcOrSvcExportKey, svcExport); err != nil {
					return false
				}

				if len(svcExport.Finalizers) != 0 {
					return false
				}

				expectedCond := serviceExportInvalidIneligibleCondition(memberUserNS, svcName)
				validCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportValid))
				return cmp.Equal(validCond, &expectedCond, ignoredCondFields)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			By("update the service; set it as a cluster IP service")
			Expect(MemberClient.Get(ctx, svcOrSvcExportKey, svc)).Should(Succeed())
			svc.Spec.Type = corev1.ServiceTypeClusterIP
			svc.Spec.ExternalName = ""
			svc.Spec.Ports = []corev1.ServicePort{
				{
					Port:       svcPort,
					TargetPort: intstr.FromInt(targetPort),
				},
			}
			Expect(MemberClient.Update(ctx, svc)).Should(Succeed())

			By("confirm that the service has been exported")
			Eventually(func() bool {
				if err := MemberClient.Get(ctx, svcOrSvcExportKey, svcExport); err != nil {
					return false
				}

				if !cmp.Equal(svcExport.Finalizers, []string{objectmeta.ServiceExportCleanupFinalizer}) {
					return false
				}

				expectedValidCond := serviceExportValidCondition(memberUserNS, svcName)
				validCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportValid))
				if !cmp.Equal(validCond, &expectedValidCond, ignoredCondFields) {
					return false
				}

				expectedConflictCond := serviceExportPendingConflictResolutionCondition(memberUserNS, svc.Name)
				conflictCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportConflict))
				return cmp.Equal(conflictCond, &expectedConflictCond, ignoredCondFields)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Eventually(func() bool {
				internalSvcExport := &fleetnetv1alpha1.InternalServiceExport{}
				if err := HubClient.Get(ctx, internalSvcExportKey, internalSvcExport); err != nil {
					return false
				}

				expectedInternalSvcExportSpec := fleetnetv1alpha1.InternalServiceExportSpec{
					Ports: []fleetnetv1alpha1.ServicePort{
						{
							Protocol:   corev1.ProtocolTCP,
							Port:       svcPort,
							TargetPort: intstr.FromInt(targetPort),
						},
					},
					ServiceReference: fleetnetv1alpha1.FromMetaObjects(
						MemberClusterID,
						svc.TypeMeta,
						svc.ObjectMeta,
					),
				}
				return cmp.Equal(internalSvcExport.Spec, expectedInternalSvcExportSpec, ignoredRefFields)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})
})
