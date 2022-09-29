/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package internalserviceexport

import (
	"fmt"
	"time"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
)

const (
	svcPort = 80

	eventuallyTimeout  = time.Second * 10
	eventuallyInterval = time.Millisecond * 250
)

var (
	// internalServiceExportIsAbsentActual runs with Eventually and Consistently assertion to make sure that
	// the InternalServiceExport referred by internalSvcExportKey no longer exists.
	internalServiceExportIsAbsentActual = func() error {
		internalSvcExport := &fleetnetv1alpha1.InternalServiceExport{}
		if err := hubClient.Get(ctx, internalSvcExportKey, internalSvcExport); !errors.IsNotFound(err) {
			return fmt.Errorf("internalServiceExport Get(%+v), got %w, want not found", internalSvcExportKey, err)
		}
		return nil
	}
	// serviceExportIsAbsentActual runs with Eventually and Consistently assertion to make sure that
	// the ServiceExport referred by svcExportKey no longer exists.
	serviceExportIsAbsentActual = func() error {
		svcExport := &fleetnetv1alpha1.ServiceExport{}
		if err := memberClient.Get(ctx, svcExportKey, svcExport); !errors.IsNotFound(err) {
			return fmt.Errorf("serviceExport Get(%+v), got %w, want not found", svcExportKey, err)
		}
		return nil
	}
	// internalServiceExportHasLastObservedGenerationAnnotatedActual runs with Eventually and Consistently assertion
	// to make sure that a last observed annotation has been added to the InternalServiceExport referred by
	// internalSvcExportKey when a metric data point is observed.
	internalServiceExportHasLastObservedGenerationAnnotatedActual = func() error {
		internalSvcExport := &fleetnetv1alpha1.InternalServiceExport{}
		if err := hubClient.Get(ctx, internalSvcExportKey, internalSvcExport); err != nil {
			return fmt.Errorf("internalServiceExport Get(%+v), got %w, want no error", internalSvcExportKey, err)
		}

		lastObservedGeneration, ok := internalSvcExport.Annotations[objectmeta.MetricsAnnotationLastObservedGeneration]
		if !ok || lastObservedGeneration != fmt.Sprintf("%d", internalSvcExport.Spec.ServiceReference.Generation) {
			return fmt.Errorf("lastObservedGeneration, got %s, want %d", lastObservedGeneration, internalSvcExport.Spec.ServiceReference.Generation)
		}
		return nil
	}
)

// unfulfilledInternalServiceExport returns an unfulfilled InternalServiceExport object.
func unfulfilledInternalServiceExport() *fleetnetv1alpha1.InternalServiceExport {
	return &fleetnetv1alpha1.InternalServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: hubNSForMember,
			Name:      internalSvcExportName,
		},
		Spec: fleetnetv1alpha1.InternalServiceExportSpec{
			Ports: []fleetnetv1alpha1.ServicePort{
				{
					Port: svcPort,
				},
			},
			ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
				ClusterID:       memberClusterID,
				Kind:            "Service",
				Namespace:       memberUserNS,
				Name:            svcName,
				ResourceVersion: "0",
				Generation:      1,
				UID:             "00000000-0000-0000-0000-000000000000",
				ExportedSince:   metav1.NewTime(time.Now().Round(time.Second)),
			},
		},
	}
}

// unfulfilledServiceExport returns an unfulfilled ServiceExport object.
func unfulfilledServiceExport() *fleetnetv1alpha1.ServiceExport {
	return &fleetnetv1alpha1.ServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      svcName,
		},
	}
}

var _ = Describe("internalsvcexport controller", func() {
	Context("dangling internalsvcexport", func() {
		var internalSvcExport *fleetnetv1alpha1.InternalServiceExport

		BeforeEach(func() {
			internalSvcExport = unfulfilledInternalServiceExport()
			Expect(hubClient.Create(ctx, internalSvcExport)).Should(Succeed())
		})

		It("should remove dangling internalsvcexport", func() {
			Eventually(internalServiceExportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})
	})

	Context("conflict resolution in progress", func() {
		var svcExport *fleetnetv1alpha1.ServiceExport
		var internalSvcExport *fleetnetv1alpha1.InternalServiceExport

		BeforeEach(func() {
			svcExport = unfulfilledServiceExport()
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())

			internalSvcExport = unfulfilledInternalServiceExport()
			Expect(hubClient.Create(ctx, internalSvcExport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, internalSvcExport)).Should(Succeed())
			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())

			// Confirm that both ServiceExport and InternalServiceExport have been deleted;
			// this helps make the test less flaky.
			Eventually(internalServiceExportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(serviceExportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should not report back any conflict resolution result", func() {
			Eventually(func() error {
				if err := memberClient.Get(ctx, svcExportKey, svcExport); err != nil {
					return fmt.Errorf("serviceExport Get(%+v), got %w, want no error", svcExportKey, err)
				}

				if len(svcExport.Status.Conditions) != 0 {
					return fmt.Errorf("serviceExport conditions, got %v, want empty list", svcExport.Status.Conditions)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Eventually(func() error {
				if err := hubClient.Get(ctx, internalSvcExportKey, internalSvcExport); err != nil {
					return fmt.Errorf("internalServiceExport Get(%v), got %w, want no error", internalSvcExportKey, err)
				}

				if _, ok := internalSvcExport.Annotations[objectmeta.MetricsAnnotationLastObservedGeneration]; ok {
					return fmt.Errorf("lastObservedGeneration annotation is present")
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})
	})

	Context("no conflict detected", func() {
		var svcExport *fleetnetv1alpha1.ServiceExport
		var internalSvcExport *fleetnetv1alpha1.InternalServiceExport

		BeforeEach(func() {
			svcExport = unfulfilledServiceExport()
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())

			internalSvcExport = unfulfilledInternalServiceExport()
			Expect(hubClient.Create(ctx, internalSvcExport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, internalSvcExport)).Should(Succeed())
			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())

			// Confirm that both ServiceExport and InternalServiceExport have been deleted;
			// this helps make the test less flaky.
			Eventually(internalServiceExportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(serviceExportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should report back conflict condition (no conflict found)", func() {
			// Add a no conflict condition.
			meta.SetStatusCondition(&internalSvcExport.Status.Conditions,
				unconflictedServiceExportConflictCondition(memberUserNS, svcName))
			Expect(hubClient.Status().Update(ctx, internalSvcExport)).Should(Succeed())

			Eventually(func() error {
				if err := memberClient.Get(ctx, svcExportKey, svcExport); err != nil {
					return fmt.Errorf("serviceExport Get(%+v), got %w, want no error", svcExportKey, err)
				}

				expectedConds := []metav1.Condition{unconflictedServiceExportConflictCondition(memberUserNS, svcName)}
				if diff := cmp.Diff(svcExport.Status.Conditions, expectedConds, ignoredCondFields); diff != "" {
					return fmt.Errorf("serviceExport conditions (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Eventually(internalServiceExportHasLastObservedGenerationAnnotatedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})
	})

	Context("conflict detected", func() {
		var svcExport *fleetnetv1alpha1.ServiceExport
		var internalSvcExport *fleetnetv1alpha1.InternalServiceExport

		BeforeEach(func() {
			svcExport = unfulfilledServiceExport()
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())

			internalSvcExport = unfulfilledInternalServiceExport()
			Expect(hubClient.Create(ctx, internalSvcExport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, internalSvcExport)).Should(Succeed())
			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())

			// Confirm that both ServiceExport and InternalServiceExport have been deleted;
			// this helps make the test less flaky.
			Eventually(internalServiceExportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(serviceExportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should report back conflict condition (conflict found)", func() {
			// Add a no conflict condition
			meta.SetStatusCondition(&internalSvcExport.Status.Conditions,
				conflictedServiceExportConflictCondition(memberUserNS, svcName))
			Expect(hubClient.Status().Update(ctx, internalSvcExport)).Should(Succeed())

			Eventually(func() error {
				if err := memberClient.Get(ctx, svcExportKey, svcExport); err != nil {
					return fmt.Errorf("serviceExport Get(%+v), got %w, want no error", svcExportKey, err)
				}

				expectedConds := []metav1.Condition{conflictedServiceExportConflictCondition(memberUserNS, svcName)}
				if diff := cmp.Diff(svcExport.Status.Conditions, expectedConds, ignoredCondFields); diff != "" {
					return fmt.Errorf("serviceExport conditions (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Eventually(internalServiceExportHasLastObservedGenerationAnnotatedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})
	})
})
