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
)

const (
	memberClusterID  = "bravelion"
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

var (
	svcOrSvcExportKey = types.NamespacedName{
		Namespace: memberUserNS,
		Name:      svcName,
	}
	internalSvcExportKey = types.NamespacedName{
		Namespace: hubNSForMember,
		Name:      fmt.Sprintf("%s-%s", memberUserNS, svcName),
	}

	ignoredRefFields = cmpopts.IgnoreFields(fleetnetv1alpha1.ExportedObjectReference{}, "ResourceVersion")
)

var (
	// serviceExportIsAbsentActual runs with Eventually and Consistently assertion to make sure that
	// the ServiceExport referred by svcOrSvcExportKey no longer exists.
	serviceExportIsAbsentActual = func() error {
		svcExport := fleetnetv1alpha1.ServiceExport{}
		if err := memberClient.Get(ctx, svcOrSvcExportKey, &svcExport); !errors.IsNotFound(err) {
			return fmt.Errorf("serviceExport Get(%+v), got %w, want not found", svcOrSvcExportKey, err)
		}
		return nil
	}
	// serviceIsAbsentActual runs with Eventually and Consistently assertion to make sure that the Service
	// referred by svcOrSvcExportKey no longer exists.
	serviceIsAbsentActual = func() error {
		svc := corev1.Service{}
		if err := memberClient.Get(ctx, svcOrSvcExportKey, &svc); !errors.IsNotFound(err) {
			return fmt.Errorf("service Get(%+v), got %w, want not found", svcOrSvcExportKey, err)
		}
		return nil
	}
	// serviceIsNotExportedActual runs with Eventually and Consistently assertion to make sure that no
	// Service has been exported.
	serviceIsNotExportedActual = func() error {
		internalSvcExportList := &fleetnetv1alpha1.InternalServiceExportList{}
		listOption := &client.ListOptions{Namespace: hubNSForMember}
		if err := hubClient.List(ctx, internalSvcExportList, listOption); err != nil {
			return fmt.Errorf("endpointSliceExport List(), got %w, want no error", err)
		}

		if len(internalSvcExportList.Items) > 0 {
			return fmt.Errorf("endpointSliceExportList length, got %d, want %d", len(internalSvcExportList.Items), 0)
		}
		return nil
	}
	// serviceIsInvalidForExportNotFoundActual runs with Eventually and Consistently assertion to make sure that
	// the ServiceExport referred by svcOrSvcExportKey has been marked as invalid due to not being able to find
	// the corresponding Serivce.
	serviceIsInvalidForExportNotFoundActual = func() error {
		svcExport := &fleetnetv1alpha1.ServiceExport{}
		if err := memberClient.Get(ctx, svcOrSvcExportKey, svcExport); err != nil {
			return fmt.Errorf("serviceExport Get(%+v), got %w, want no error", svcOrSvcExportKey, err)
		}

		if len(svcExport.Finalizers) != 0 {
			return fmt.Errorf("serviceExport finalizers, got %v, want empty list", svcExport.Finalizers)
		}

		expectedCond := serviceExportInvalidNotFoundCondition(memberUserNS, svcName)
		validCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportValid))
		if diff := cmp.Diff(validCond, &expectedCond, ignoredCondFields); diff != "" {
			return fmt.Errorf("serviceExportValid condition (-got, +want): %s", diff)
		}
		return nil
	}
	// serviceIsInvalidForExportIneligibleActual runs with Eventually and Consistently assertion to make sure that
	// the ServiceExport referred by svcOrSvcExportKey has been marked as invalid due to the corresponding being
	// of an unsupported type.
	serviceIsInvalidForExportIneligibleActual = func() error {
		svcExport := &fleetnetv1alpha1.ServiceExport{}
		if err := memberClient.Get(ctx, svcOrSvcExportKey, svcExport); err != nil {
			return fmt.Errorf("serviceExport Get(%+v), got %w, want no error", svcOrSvcExportKey, err)
		}

		if len(svcExport.Finalizers) != 0 {
			return fmt.Errorf("serviceExport finalizers, got %v, want empty list", svcExport.Finalizers)
		}

		svc := &corev1.Service{}
		if err := memberClient.Get(ctx, svcOrSvcExportKey, svc); err != nil {
			return fmt.Errorf("service Get(%+v), got %w, want no error", svcOrSvcExportKey, err)
		}
		expectedCond := serviceExportInvalidIneligibleCondition(memberUserNS, svcName, svc.Generation)
		validCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportValid))
		if diff := cmp.Diff(validCond, &expectedCond, ignoredCondFields); diff != "" {
			return fmt.Errorf("serviceExportValid condition (-got, +want): %s", diff)
		}
		return nil
	}
	// serviceIsExportedFromMemberActual runs with Eventually and Consistently assertion to make sure that
	// the Service referred by svcOrSvcExportKey has been exported from the member cluster, i.e. it has
	// the cleanup finalizer and has been marked as valid for export.
	serviceIsExportedFromMemberActual = func() error {
		svc := &corev1.Service{}
		if err := memberClient.Get(ctx, svcOrSvcExportKey, svc); err != nil {
			return fmt.Errorf("service Get(%+v), got %w, want no error", svcOrSvcExportKey, err)
		}
		svcExport := &fleetnetv1alpha1.ServiceExport{}
		if err := memberClient.Get(ctx, svcOrSvcExportKey, svcExport); err != nil {
			return fmt.Errorf("serviceExport Get(%+v), got %w, want no error", svcOrSvcExportKey, err)
		}

		if !cmp.Equal(svcExport.Finalizers, []string{svcExportCleanupFinalizer}) {
			return fmt.Errorf("serviceExport finalizers, got %v, want %v", svcExport.Finalizers, []string{svcExportCleanupFinalizer})
		}

		expectedValidCond := serviceExportValidCondition(memberUserNS, svcName, svc.Generation)
		validCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportValid))
		if diff := cmp.Diff(validCond, &expectedValidCond, ignoredCondFields); diff != "" {
			return fmt.Errorf("serviceExportValid condition (-got, +want): %s", diff)
		}

		expectedConflictCond := serviceExportPendingConflictResolutionCondition(memberUserNS, svcName, svc.Generation)
		conflictCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportConflict))
		if diff := cmp.Diff(conflictCond, &expectedConflictCond, ignoredCondFields); diff != "" {
			return fmt.Errorf("serviceExportConflict condition (-got, +want): %s", diff)
		}
		return nil
	}
	// serviceIsExportedToHubActual runs with Eventually and Consistently assertion to make sure that
	// the Service referred by svcOrSvcExportKey has been exported to the hub cluster, i.e. a corresponding
	// internalServiceExport has been created.
	serviceIsExportedToHubActual = func() error {
		internalSvcExport := &fleetnetv1alpha1.InternalServiceExport{}
		if err := hubClient.Get(ctx, internalSvcExportKey, internalSvcExport); err != nil {
			return fmt.Errorf("internalServiceExport Get(%+v), got %w, want no error", internalSvcExportKey, err)
		}

		svc := &corev1.Service{}
		if err := memberClient.Get(ctx, svcOrSvcExportKey, svc); err != nil {
			return fmt.Errorf("service Get(%+v), got %w, want no error", svcOrSvcExportKey, err)
		}
		svcExport := &fleetnetv1alpha1.ServiceExport{}
		if err := memberClient.Get(ctx, svcOrSvcExportKey, svcExport); err != nil {
			return fmt.Errorf("serviceExport Get(%+v), got %w, want no error", svcOrSvcExportKey, err)
		}
		expectedExportedSince := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportValid)).LastTransitionTime
		expectedInternalSvcExportSpec := fleetnetv1alpha1.InternalServiceExportSpec{
			Ports: []fleetnetv1alpha1.ServicePort{
				{
					Protocol:   corev1.ProtocolTCP,
					Port:       svcPort,
					TargetPort: intstr.FromInt(targetPort),
				},
			},
			ServiceReference: fleetnetv1alpha1.FromMetaObjects(
				memberClusterID,
				svc.TypeMeta,
				svc.ObjectMeta,
				expectedExportedSince,
			),
		}
		if diff := cmp.Diff(internalSvcExport.Spec, expectedInternalSvcExportSpec, ignoredRefFields); diff != "" {
			return fmt.Errorf("internalServiceExport spec (-got, +want): %s", diff)
		}
		return nil
	}
)

var _ = Describe("serviceexport controller", func() {
	Context("export non-existent service", func() {
		var svcExport = &fleetnetv1alpha1.ServiceExport{}

		BeforeEach(func() {
			svcExport = notYetFulfilledServiceExport()
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
			// Confirm that ServiceExport has been deleted; this helps make the test less flaky.
			Eventually(serviceExportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should mark the service export as invalid + should not export service", func() {
			Eventually(serviceIsInvalidForExportNotFoundActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Consistently(serviceIsNotExportedActual, consistentlyDuration, consistentlyInterval).Should(BeNil())
		})
	})

	Context("export existing service", func() {
		var svcExport = &fleetnetv1alpha1.ServiceExport{}
		var svc = &corev1.Service{}

		BeforeEach(func() {
			svc = clusterIPService()
			Expect(memberClient.Create(ctx, svc)).Should(Succeed())

			svcExport = notYetFulfilledServiceExport()
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
			Expect(memberClient.Delete(ctx, svc)).Should(Succeed())

			// Confirm that the Service has been unexported; this helps make the tests less flaky.
			Eventually(serviceIsNotExportedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Eventually(serviceExportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(serviceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should mark the service export as valid + should export the service", func() {
			Eventually(serviceIsExportedFromMemberActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(serviceIsExportedToHubActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})
	})

	Context("export new service", func() {
		var svcExport = &fleetnetv1alpha1.ServiceExport{}
		var svc = &corev1.Service{}

		BeforeEach(func() {
			svcExport = notYetFulfilledServiceExport()
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())

			svc = clusterIPService()
			Expect(memberClient.Create(ctx, svc)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
			Expect(memberClient.Delete(ctx, svc)).Should(Succeed())

			// Confirm that the Service has been unexported; this helps make the tests less flaky.
			Eventually(serviceIsNotExportedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Eventually(serviceExportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(serviceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should mark the service export as valid + should export the service", func() {
			Eventually(serviceIsExportedFromMemberActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(serviceIsExportedToHubActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
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
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())

			svc = clusterIPService()
			Expect(memberClient.Create(ctx, svc)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
			Expect(memberClient.Delete(ctx, svc)).Should(Succeed())

			// Confirm that the Service has been unexported; this helps make the tests less flaky.
			Eventually(serviceIsNotExportedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Eventually(serviceExportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(serviceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should update the exported service", func() {
			By("confirm that the service has been exported")
			Eventually(serviceIsExportedFromMemberActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(serviceIsExportedToHubActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			By("update the service")
			Expect(memberClient.Get(ctx, svcOrSvcExportKey, svc)).Should(Succeed())
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
			Expect(memberClient.Update(ctx, svc)).Should(Succeed())

			By("confirm that the exported service has been updated")
			Eventually(func() error {
				internalSvcExport := &fleetnetv1alpha1.InternalServiceExport{}
				if err := hubClient.Get(ctx, internalSvcExportKey, internalSvcExport); err != nil {
					return fmt.Errorf("internalServiceExport Get(%+v), got %w, want no error", internalSvcExportKey, err)
				}

				if err := memberClient.Get(ctx, svcOrSvcExportKey, svcExport); err != nil {
					return fmt.Errorf("serviceExport Get(%+v), got %w, want no error", svcOrSvcExportKey, err)
				}
				expectedExportedSince := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportValid)).LastTransitionTime
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
						memberClusterID,
						svc.TypeMeta,
						svc.ObjectMeta,
						expectedExportedSince,
					),
				}
				if diff := cmp.Diff(internalSvcExport.Spec, expectedInternalSvcExportSpec, ignoredRefFields); diff != "" {
					return fmt.Errorf("internalServiceExport spec (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})
	})

	Context("unexport service", func() {
		var svcExport = &fleetnetv1alpha1.ServiceExport{}
		var svc = &corev1.Service{}

		BeforeEach(func() {
			svcExport = notYetFulfilledServiceExport()
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())

			svc = clusterIPService()
			Expect(memberClient.Create(ctx, svc)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, svc)).Should(Succeed())
			// Confirm that Service has been deleted; this helps make the test less flaky.
			Eventually(serviceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should unexport the service when service export is deleted", func() {
			By("confirm that the service has been exported")
			Eventually(serviceIsExportedFromMemberActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(serviceIsExportedToHubActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			By("delete the service export")
			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())

			By("confirm that the service has been unexported")
			Eventually(serviceIsNotExportedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})
	})

	Context("deleted exported service", func() {
		var svcExport = &fleetnetv1alpha1.ServiceExport{}
		var svc = &corev1.Service{}

		BeforeEach(func() {
			svcExport = notYetFulfilledServiceExport()
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())

			svc = clusterIPService()
			Expect(memberClient.Create(ctx, svc)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
			// Confirm that the ServiceExport has been deleted; this helps make the test less flaky.
			Eventually(serviceExportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should unexport the service when service is deleted", func() {
			By("confirm that the service has been exported")
			Eventually(serviceIsExportedFromMemberActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(serviceIsExportedToHubActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			By("delete the service")
			Expect(memberClient.Delete(ctx, svc)).Should(Succeed())
			Eventually(serviceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			By("confirm that the service has been unexported")
			Eventually(serviceIsNotExportedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(serviceIsInvalidForExportNotFoundActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})
	})

	Context("export ineligible service (headless)", func() {
		var svcExport = &fleetnetv1alpha1.ServiceExport{}
		var svc = &corev1.Service{}

		BeforeEach(func() {
			svcExport = notYetFulfilledServiceExport()
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())

			svc = headlessService()
			Expect(memberClient.Create(ctx, svc)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
			Expect(memberClient.Delete(ctx, svc)).Should(Succeed())

			// Confirm that Service + ServiceExport have been deleted; this helps make the test less flaky.
			Eventually(serviceExportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(serviceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should mark the service export as invalid (ineligible) + should not export headless service", func() {
			Eventually(serviceIsInvalidForExportIneligibleActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Consistently(serviceIsNotExportedActual, consistentlyDuration, consistentlyInterval).Should(BeNil())
		})
	})

	Context("export ineligible service (external name)", func() {
		var svcExport = &fleetnetv1alpha1.ServiceExport{}
		var svc = &corev1.Service{}

		BeforeEach(func() {
			svcExport = notYetFulfilledServiceExport()
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())

			svc = externalNameService()
			Expect(memberClient.Create(ctx, svc)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
			Expect(memberClient.Delete(ctx, svc)).Should(Succeed())

			// Confirm that Service + ServiceExport have been deleted; this helps make the test less flaky.
			Eventually(serviceExportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(serviceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should mark the service export as invalid (ineligible) + should not export external name service", func() {
			Eventually(serviceIsInvalidForExportIneligibleActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Consistently(serviceIsNotExportedActual, consistentlyDuration, consistentlyInterval).Should(BeNil())
		})
	})

	Context("unexport service that becomes ineligible for export (external name)", func() {
		var svcExport = &fleetnetv1alpha1.ServiceExport{}
		var svc = &corev1.Service{}

		BeforeEach(func() {
			svcExport = notYetFulfilledServiceExport()
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())

			svc = clusterIPService()
			Expect(memberClient.Create(ctx, svc)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
			Expect(memberClient.Delete(ctx, svc)).Should(Succeed())

			// Confirm that Service + ServiceExport have been deleted; this helps make the test less flaky.
			Eventually(serviceExportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(serviceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should mark the service export as invalid (ineligible) + should unexport the service", func() {
			By("confirm that the service has been exported")
			Eventually(serviceIsExportedFromMemberActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(serviceIsExportedToHubActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			By("update the service; set it to an external name service")
			Expect(memberClient.Get(ctx, svcOrSvcExportKey, svc)).Should(Succeed())
			svc.Spec.Type = corev1.ServiceTypeExternalName
			svc.Spec.Ports = []corev1.ServicePort{}
			svc.Spec.ExternalName = externalNameAddr
			Expect(memberClient.Update(ctx, svc)).Should(Succeed())

			By("confirm that the service has been unexported")
			Eventually(serviceIsNotExportedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(serviceIsInvalidForExportIneligibleActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})
	})

	Context("export service that becomes eligible for export", func() {
		var svcExport = &fleetnetv1alpha1.ServiceExport{}
		var svc = &corev1.Service{}

		BeforeEach(func() {
			svcExport = notYetFulfilledServiceExport()
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())

			svc = externalNameService()
			Expect(memberClient.Create(ctx, svc)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
			Expect(memberClient.Delete(ctx, svc)).Should(Succeed())

			// Confirm that the Service has been unexported; this helps make the tests less flaky.
			Eventually(serviceIsNotExportedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Confirm that Service + ServiceExport have been deleted; this helps make the test less flaky.
			Eventually(serviceExportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(serviceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should mark the service export as valid + should export the service", func() {
			By("confirm that the service has not been exported")
			Consistently(serviceIsNotExportedActual, consistentlyDuration, consistentlyInterval).Should(BeNil())
			Eventually(serviceIsInvalidForExportIneligibleActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			By("update the service; set it as a cluster IP service")
			Expect(memberClient.Get(ctx, svcOrSvcExportKey, svc)).Should(Succeed())
			svc.Spec.Type = corev1.ServiceTypeClusterIP
			svc.Spec.ExternalName = ""
			svc.Spec.Ports = []corev1.ServicePort{
				{
					Port:       svcPort,
					TargetPort: intstr.FromInt(targetPort),
				},
			}
			Expect(memberClient.Update(ctx, svc)).Should(Succeed())

			By("confirm that the service has been exported")
			Eventually(serviceIsExportedFromMemberActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(serviceIsExportedToHubActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})
	})

	Context("export a service when another service with the same name has been already exported earlier", func() {
		var (
			internalSvcExport = &fleetnetv1alpha1.InternalServiceExport{}
			svcExport         = &fleetnetv1alpha1.ServiceExport{}
			svc               = &corev1.Service{}
		)

		BeforeEach(func() {
			internalSvcExport = &fleetnetv1alpha1.InternalServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMember,
					Name:      fmt.Sprintf("%s-%s", memberUserNS, svcName),
				},
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Ports: []fleetnetv1alpha1.ServicePort{
						{
							Protocol:   corev1.ProtocolTCP,
							Port:       svcPort,
							TargetPort: intstr.FromInt(targetPort),
						},
					},
					ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
						ClusterID:       memberClusterID,
						Kind:            "Service",
						Namespace:       memberUserNS,
						Name:            svcName,
						ResourceVersion: "0",
						Generation:      0,
						UID:             "0",
						NamespacedName:  svcOrSvcExportKey.String(),
						ExportedSince:   metav1.NewTime(time.Now().Round(time.Second)),
					},
				},
			}
			Expect(hubClient.Create(ctx, internalSvcExport)).Should(Succeed())

			svcExport = notYetFulfilledServiceExport()
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())

			svc = clusterIPService()
			Expect(memberClient.Create(ctx, svc)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
			Expect(memberClient.Delete(ctx, svc)).Should(Succeed())

			// Confirm that the Service has been unexported; this helps make the tests less flaky.
			Eventually(serviceIsNotExportedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Confirm that Service + ServiceExport have been deleted; this helps make the test less flaky.
			Eventually(serviceExportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(serviceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should re-export the service", func() {
			// Confirm that the Service has been exported.
			Eventually(serviceIsExportedFromMemberActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			// Confirm that the InternalServiceExport has been re-created.
			Eventually(serviceIsExportedToHubActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})
	})
})
