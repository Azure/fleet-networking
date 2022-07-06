/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package internalserviceexport

import (
	"fmt"
	"time"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

const (
	memberClusterID = "bravelion"
	svcPort         = 80

	eventuallyTimeout  = time.Second * 10
	eventuallyInterval = time.Millisecond * 250
)

var _ = Describe("internalsvcexport controller", func() {
	Context("dangling internalsvcexport", func() {
		var danglingInternalSvcExport *fleetnetv1alpha1.InternalServiceExport

		BeforeEach(func() {
			danglingInternalSvcExport = &fleetnetv1alpha1.InternalServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMember,
					Name:      fmt.Sprintf("%s-%s", memberUserNS, svcName),
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
						Generation:      0,
						UID:             "0",
					},
				},
			}
			Expect(hubClient.Create(ctx, danglingInternalSvcExport)).Should(Succeed())
		})

		It("should remove dangling internalsvcexport", func() {
			internalSvcExportKey := types.NamespacedName{
				Namespace: hubNSForMember,
				Name:      fmt.Sprintf("%s-%s", memberUserNS, svcName),
			}
			Eventually(func() bool {
				return errors.IsNotFound(hubClient.Get(ctx, internalSvcExportKey, danglingInternalSvcExport))
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("conflict resolution in progress", func() {
		var svcExport *fleetnetv1alpha1.ServiceExport
		var internalSvcExport *fleetnetv1alpha1.InternalServiceExport

		BeforeEach(func() {
			svcExport = &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
			}
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())

			internalSvcExport = &fleetnetv1alpha1.InternalServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMember,
					Name:      fmt.Sprintf("%s-%s", memberUserNS, svcName),
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
						ResourceVersion: "1",
						Generation:      1,
						UID:             "1",
					},
				},
			}
			Expect(hubClient.Create(ctx, internalSvcExport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, internalSvcExport)).Should(Succeed())
			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
		})

		It("should not report back any conflict resolution result", func() {
			svcExportKey := types.NamespacedName{Namespace: memberUserNS, Name: svcName}
			Eventually(func() []metav1.Condition {
				Expect(memberClient.Get(ctx, svcExportKey, svcExport)).Should(Succeed())
				return svcExport.Status.Conditions
			}).Should(BeNil())
		})
	})

	Context("no conflict detected", func() {
		var unconflictedSvcExport *fleetnetv1alpha1.ServiceExport
		var unconflictedInternalSvcExport *fleetnetv1alpha1.InternalServiceExport

		BeforeEach(func() {
			unconflictedSvcExport = &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
			}
			Expect(memberClient.Create(ctx, unconflictedSvcExport)).Should(Succeed())

			unconflictedInternalSvcExport = &fleetnetv1alpha1.InternalServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMember,
					Name:      fmt.Sprintf("%s-%s", memberUserNS, svcName),
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
						ResourceVersion: "2",
						Generation:      2,
						UID:             "2",
					},
				},
			}
			Expect(hubClient.Create(ctx, unconflictedInternalSvcExport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, unconflictedInternalSvcExport)).Should(Succeed())
			Expect(memberClient.Delete(ctx, unconflictedSvcExport)).Should(Succeed())
		})

		It("should report back conflict condition (no conflict found)", func() {
			// Add a no conflict condition
			meta.SetStatusCondition(&unconflictedInternalSvcExport.Status.Conditions,
				unconflictedServiceExportConflictCondition(memberUserNS, svcName))
			Expect(hubClient.Status().Update(ctx, unconflictedInternalSvcExport)).Should(Succeed())

			unconflictedSvcExportKey := types.NamespacedName{Namespace: memberUserNS, Name: svcName}
			// TO-DO (chenyu1): newer gomega versions offer BeComparableTo function, which automatically
			// calls cmp package for diffs.
			Eventually(func() string {
				Expect(memberClient.Get(ctx, unconflictedSvcExportKey, unconflictedSvcExport)).Should(Succeed())
				return cmp.Diff(
					unconflictedSvcExport.Status.Conditions,
					[]metav1.Condition{
						unconflictedServiceExportConflictCondition(memberUserNS, svcName),
					},
					ignoredCondFields)
			}, eventuallyTimeout, eventuallyInterval).Should(BeEmpty())
		})
	})

	Context("conflict detected", func() {
		var conflictedSvcExport *fleetnetv1alpha1.ServiceExport
		var conflictedInternalSvcExport *fleetnetv1alpha1.InternalServiceExport

		BeforeEach(func() {
			conflictedSvcExport = &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
			}
			Expect(memberClient.Create(ctx, conflictedSvcExport)).Should(Succeed())

			conflictedInternalSvcExport = &fleetnetv1alpha1.InternalServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMember,
					Name:      fmt.Sprintf("%s-%s", memberUserNS, svcName),
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
						ResourceVersion: "3",
						Generation:      3,
						UID:             "3",
					},
				},
			}
			Expect(hubClient.Create(ctx, conflictedInternalSvcExport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, conflictedInternalSvcExport)).Should(Succeed())
			Expect(memberClient.Delete(ctx, conflictedSvcExport)).Should(Succeed())
		})

		It("should report back conflict condition (conflict found)", func() {
			// Add a no conflict condition
			meta.SetStatusCondition(&conflictedInternalSvcExport.Status.Conditions,
				conflictedServiceExportConflictCondition(memberUserNS, svcName))
			Expect(hubClient.Status().Update(ctx, conflictedInternalSvcExport)).Should(Succeed())

			conflictedSvcExportKey := types.NamespacedName{Namespace: memberUserNS, Name: svcName}
			// TO-DO (chenyu1): newer gomega versions offer BeComparableTo function, which automatically
			// calls cmp package for diffs.
			Eventually(func() string {
				Expect(memberClient.Get(ctx, conflictedSvcExportKey, conflictedSvcExport)).Should(Succeed())
				return cmp.Diff(
					conflictedSvcExport.Status.Conditions,
					[]metav1.Condition{
						conflictedServiceExportConflictCondition(memberUserNS, svcName),
					},
					ignoredCondFields)
			}, eventuallyTimeout, eventuallyInterval).Should(BeEmpty())
		})
	})
})
