/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package endpointslice

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
)

const (
	ipv4Addr             = "1.2.3.4"
	altIPv4Addr          = "2.3.4.5"
	ipv6Addr             = "2001:db8:1::ab9:C0A8:102"
	altEndpointSliceName = "app-endpointslice-2"

	eventuallyTimeout    = time.Second * 10
	eventuallyInterval   = time.Millisecond * 250
	consistentlyDuration = time.Millisecond * 1000
	consistentlyInterval = time.Millisecond * 50
)

var (
	endpointSlicePort = int32(80)
	svcKey            = types.NamespacedName{
		Namespace: memberUserNS,
		Name:      svcName,
	}
)

var (
	// endpointSliceUniqueNameIsNotAssignedActual runs with Eventually and Consistently assertion to make sure that
	// no unique name has been assigned to an EndpointSlice.
	endpointSliceUniqueNameIsNotAssignedActual = func() error {
		endpointSlice := &discoveryv1.EndpointSlice{}
		if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
			return fmt.Errorf("endpointSlice Get(%+v), got %v, want no error", endpointSliceKey, err)
		}

		if _, ok := endpointSlice.Annotations[objectmeta.ExportedObjectAnnotationUniqueName]; ok {
			return fmt.Errorf("endpointSlice unique name annotation is present")
		}
		return nil
	}
	// endpointSliceIsNotExportedActual runs with Eventually and Consistently assertion to make sure that no
	// EndpointSlice has been exported.
	endpointSliceIsNotExportedActual = func() error {
		endpointSliceExportList := &fleetnetv1alpha1.EndpointSliceExportList{}
		listOption := &client.ListOptions{Namespace: hubNSForMember}
		if err := hubClient.List(ctx, endpointSliceExportList, listOption); err != nil {
			return fmt.Errorf("endpointSliceExport List(), got %v, want no error", err)
		}

		if len(endpointSliceExportList.Items) > 0 {
			return fmt.Errorf("endpointSliceExportList length, got %d, want %d", len(endpointSliceExportList.Items), 0)
		}
		return nil
	}

	// endpointSliceIsAbsentActual runs with Eventually and Consistently assertion to make sure that a given
	// EndpointSlice no longer exists.
	endpointSliceIsAbsentActual = func() error {
		endpointSlice := &discoveryv1.EndpointSlice{}
		if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); !errors.IsNotFound(err) {
			return fmt.Errorf("endpointSlice Get(%+v), got %v, want not found", endpointSliceKey, err)
		}
		return nil
	}

	// serviceExportIsAbsentActual runs with Eventually and Consistently assertion to make sure that a given
	// ServiceExport no longer exists.
	serviceExportIsAbsentActual = func() error {
		svcExport := &fleetnetv1alpha1.ServiceExport{}
		if err := memberClient.Get(ctx, svcKey, svcExport); !errors.IsNotFound(err) {
			return fmt.Errorf("serviceExport Get(%+v), got %v, want not found", svcKey, err)
		}
		return nil
	}
)

func managedIPv4EndpointSliceWithoutUniqueNameAnnotation() *discoveryv1.EndpointSlice {
	return &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      endpointSliceName,
			Labels: map[string]string{
				discoveryv1.LabelServiceName: svcName,
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints: []discoveryv1.Endpoint{
			{
				Addresses: []string{ipv4Addr},
			},
		},
		Ports: []discoveryv1.EndpointPort{
			{
				Port: &endpointSlicePort,
			},
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

var _ = Describe("endpointslice controller (skip endpointslice)", Serial, Ordered, func() {
	Context("IPv6 endpointSlice", func() {
		var (
			endpointSlice *discoveryv1.EndpointSlice
			svcExport     *fleetnetv1alpha1.ServiceExport
		)

		BeforeEach(func() {
			endpointSlice = &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
					Labels: map[string]string{
						discoveryv1.LabelServiceName: svcName,
					},
				},
				AddressType: discoveryv1.AddressTypeIPv6,
				Endpoints: []discoveryv1.Endpoint{
					{
						Addresses: []string{ipv6Addr},
					},
				},
				Ports: []discoveryv1.EndpointPort{
					{
						Port: &endpointSlicePort,
					},
				},
			}
			Expect(memberClient.Create(ctx, endpointSlice)).Should(Succeed())

			svcExport = notYetFulfilledServiceExport()
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())
			svcExport.Status = fleetnetv1alpha1.ServiceExportStatus{
				Conditions: []metav1.Condition{
					serviceExportValidCondition(memberUserNS, svcName),
					serviceExportNoConflictCondition(memberUserNS, svcName),
				},
			}
			Expect(memberClient.Status().Update(ctx, svcExport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, endpointSlice)).Should(Succeed())
			// Confirm that the EndpointSlice is deleted; this helps make the test less flaky.
			Eventually(endpointSliceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
			// Confirm that the ServiceExport is deleted; this helps make the test less flaky.
			Eventually(serviceExportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should not export ipv6 endpointslice", func() {
			// Wait until the state stablizes to run consistently check; this helps make the test less flaky.
			Eventually(endpointSliceUniqueNameIsNotAssignedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Consistently(endpointSliceUniqueNameIsNotAssignedActual, consistentlyDuration, consistentlyInterval).Should(BeNil())

			// Wait until the state stablized to run consistently check; this helps make the test less flaky.
			Eventually(endpointSliceIsNotExportedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Consistently(endpointSliceIsNotExportedActual, consistentlyDuration, consistentlyInterval).Should(BeNil())
		})
	})

	Context("dangling endpointslice (endpointslice with no associated service)", func() {
		var (
			endpointSlice *discoveryv1.EndpointSlice
			svcExport     *fleetnetv1alpha1.ServiceExport
		)

		BeforeEach(func() {
			endpointSlice = &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
				},
				AddressType: discoveryv1.AddressTypeIPv4,
				Endpoints: []discoveryv1.Endpoint{
					{
						Addresses: []string{ipv4Addr},
					},
				},
				Ports: []discoveryv1.EndpointPort{
					{
						Port: &endpointSlicePort,
					},
				},
			}
			Expect(memberClient.Create(ctx, endpointSlice)).Should(Succeed())

			svcExport = notYetFulfilledServiceExport()
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())
			svcExport.Status = fleetnetv1alpha1.ServiceExportStatus{
				Conditions: []metav1.Condition{
					serviceExportValidCondition(memberUserNS, svcName),
					serviceExportNoConflictCondition(memberUserNS, svcName),
				},
			}
			Expect(memberClient.Status().Update(ctx, svcExport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, endpointSlice)).Should(Succeed())
			// Confirm that the EndpointSlice is deleted; this helps make the test less flaky.
			Eventually(endpointSliceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
			// Confirm that the ServiceExport is deleted; this helps make the test less flaky.
			Eventually(serviceExportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should not export dangling endpointslice", func() {
			// Wait until the state stablizes to run consistently check; this helps make the test less flaky.
			Eventually(endpointSliceUniqueNameIsNotAssignedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Consistently(endpointSliceUniqueNameIsNotAssignedActual, consistentlyDuration, consistentlyInterval).Should(BeNil())

			// Wait until the state stablized to run consistently check; this helps make the test less flaky.
			Eventually(endpointSliceIsNotExportedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Consistently(endpointSliceIsNotExportedActual, consistentlyDuration, consistentlyInterval).Should(BeNil())
		})
	})

	Context("endpointslice associated with unexported service", func() {
		var endpointSlice *discoveryv1.EndpointSlice

		BeforeEach(func() {
			endpointSlice = managedIPv4EndpointSliceWithoutUniqueNameAnnotation()
			Expect(memberClient.Create(ctx, endpointSlice)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, endpointSlice)).Should(Succeed())
			// Confirm that the EndpointSlice is deleted; this helps make the test less flaky.
			Eventually(endpointSliceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should not export endpointslice associated with unexported service", func() {
			// Wait until the state stablizes to run consistently check; this helps make the test less flaky.
			Eventually(endpointSliceUniqueNameIsNotAssignedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Consistently(endpointSliceUniqueNameIsNotAssignedActual, consistentlyDuration, consistentlyInterval).Should(BeNil())

			// Wait until the state stablized to run consistently check; this helps make the test less flaky.
			Eventually(endpointSliceIsNotExportedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Consistently(endpointSliceIsNotExportedActual, consistentlyDuration, consistentlyInterval).Should(BeNil())
		})
	})

	Context("endpointslice associated with invalid exported service", func() {
		var (
			endpointSlice *discoveryv1.EndpointSlice
			svcExport     *fleetnetv1alpha1.ServiceExport
		)

		BeforeEach(func() {
			endpointSlice = managedIPv4EndpointSliceWithoutUniqueNameAnnotation()
			Expect(memberClient.Create(ctx, endpointSlice)).Should(Succeed())

			svcExport = notYetFulfilledServiceExport()
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())
			svcExport.Status = fleetnetv1alpha1.ServiceExportStatus{
				Conditions: []metav1.Condition{
					serviceExportInvalidNotFoundCondition(memberUserNS, svcName),
				},
			}
			Expect(memberClient.Status().Update(ctx, svcExport)).Should(Succeed())
		})

		AfterEach(func() {
			// Confirm that the EndpointSlice is deleted; this helps make the test less flaky.
			Expect(memberClient.Delete(ctx, endpointSlice)).Should(Succeed())
			// Confirm that the EndpointSlice is deleted; this helps make the test less flaky.
			Eventually(endpointSliceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
			// Confirm that the ServiceExport is deleted; this helps make the test less flaky.
			Eventually(serviceExportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should not export endpointslice associated with invalid exported service", func() {
			// Wait until the state stablizes to run consistently check; this helps make the test less flaky.
			Eventually(endpointSliceUniqueNameIsNotAssignedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Consistently(endpointSliceUniqueNameIsNotAssignedActual, consistentlyDuration, consistentlyInterval).Should(BeNil())

			// Wait until the state stablized to run consistently check; this helps make the test less flaky.
			Eventually(endpointSliceIsNotExportedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Consistently(endpointSliceIsNotExportedActual, consistentlyDuration, consistentlyInterval).Should(BeNil())
		})
	})

	Context("endpointslice associated with conflicted exported service", func() {
		var (
			endpointSlice *discoveryv1.EndpointSlice
			svcExport     *fleetnetv1alpha1.ServiceExport
		)

		BeforeEach(func() {
			endpointSlice = managedIPv4EndpointSliceWithoutUniqueNameAnnotation()
			Expect(memberClient.Create(ctx, endpointSlice)).Should(Succeed())

			svcExport = notYetFulfilledServiceExport()
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())
			svcExport.Status = fleetnetv1alpha1.ServiceExportStatus{
				Conditions: []metav1.Condition{
					serviceExportConflictedCondition(memberUserNS, svcName),
				},
			}
			Expect(memberClient.Status().Update(ctx, svcExport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, endpointSlice)).Should(Succeed())
			// Confirm that the EndpointSlice is deleted; this helps make the test less flaky.
			Eventually(endpointSliceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
			// Confirm that the ServiceExport is deleted; this helps make the test less flaky.
			Eventually(serviceExportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should not export endpointslice associated with invalid exported service", func() {
			// Wait until the state stablizes to run consistently check; this helps make the test less flaky.
			Eventually(endpointSliceUniqueNameIsNotAssignedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Consistently(endpointSliceUniqueNameIsNotAssignedActual, consistentlyDuration, consistentlyInterval).Should(BeNil())

			// Wait until the state stablized to run consistently check; this helps make the test less flaky.
			Eventually(endpointSliceIsNotExportedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Consistently(endpointSliceIsNotExportedActual, consistentlyDuration, consistentlyInterval).Should(BeNil())
		})
	})
})

var _ = Describe("endpointslice controller (unexport endpointslice)", Serial, Ordered, func() {
	endpointSliceExportTemplate := &fleetnetv1alpha1.EndpointSliceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: hubNSForMember,
			Name:      endpointSliceUniqueName,
		},
		Spec: fleetnetv1alpha1.EndpointSliceExportSpec{
			AddressType: discoveryv1.AddressTypeIPv4,
			Endpoints: []fleetnetv1alpha1.Endpoint{
				{
					Addresses: []string{ipv4Addr},
				},
			},
			Ports: []discoveryv1.EndpointPort{
				{
					Port: &endpointSlicePort,
				},
			},
			OwnerServiceReference: fleetnetv1alpha1.OwnerServiceReference{
				Namespace: memberUserNS,
				Name:      svcName,
			},
		},
	}

	Context("exported dangling endpointslice (endpointslice with no associated service)", func() {
		var (
			endpointSlice       *discoveryv1.EndpointSlice
			endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport

			startTime = time.Now()
		)

		BeforeEach(func() {
			endpointSlice = &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
					// Must add the unique name annotation later; controller may reconcile too quickly for the desired
					// test case to happen.
				},
				AddressType: discoveryv1.AddressTypeIPv4,
				Endpoints: []discoveryv1.Endpoint{
					{
						Addresses: []string{ipv4Addr},
					},
				},
				Ports: []discoveryv1.EndpointPort{
					{
						Port: &endpointSlicePort,
					},
				},
			}
			Expect(memberClient.Create(ctx, endpointSlice)).Should(Succeed())

			endpointSliceExport = endpointSliceExportTemplate.DeepCopy()
			endpointSliceExport.Spec.EndpointSliceReference = fleetnetv1alpha1.FromMetaObjects(
				memberClusterID,
				endpointSlice.TypeMeta,
				endpointSlice.ObjectMeta,
				metav1.NewTime(startTime),
			)
			Expect(hubClient.Create(ctx, endpointSliceExport)).Should(Succeed())

			// Add the unique name + last seen annotations now.
			endpointSlice.Annotations = map[string]string{
				objectmeta.ExportedObjectAnnotationUniqueName:  endpointSliceUniqueName,
				objectmeta.MetricsAnnotationLastSeenGeneration: fmt.Sprintf("%d", endpointSlice.Generation),
				objectmeta.MetricsAnnotationLastSeenTimestamp:  startTime.Format(objectmeta.MetricsLastSeenTimestampFormat),
			}
			Expect(memberClient.Update(ctx, endpointSlice)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, endpointSlice)).Should(Succeed())
			// Confirm that the EndpointSlice is deleted; this helps make the test less flaky.
			Eventually(endpointSliceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should remove exported dangling endpointslice", func() {
			Eventually(endpointSliceIsNotExportedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(endpointSliceUniqueNameIsNotAssignedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})
	})

	Context("exported endpointslice from unexported service", func() {
		var (
			endpointSlice       *discoveryv1.EndpointSlice
			endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport

			startTime = time.Now()
		)

		BeforeEach(func() {
			// Must add the unique name annotation later; controller may reconcile too quickly for the desired
			// test case to happen.
			endpointSlice = managedIPv4EndpointSliceWithoutUniqueNameAnnotation()

			Expect(memberClient.Create(ctx, endpointSlice)).Should(Succeed())

			endpointSliceExport = endpointSliceExportTemplate.DeepCopy()
			endpointSliceExport.Spec.EndpointSliceReference = fleetnetv1alpha1.FromMetaObjects(
				memberClusterID,
				endpointSlice.TypeMeta,
				endpointSlice.ObjectMeta,
				metav1.NewTime(startTime),
			)
			Expect(hubClient.Create(ctx, endpointSliceExport)).Should(Succeed())

			// Add the unique name annotation now.
			endpointSlice.Annotations = map[string]string{
				objectmeta.ExportedObjectAnnotationUniqueName:  endpointSliceUniqueName,
				objectmeta.MetricsAnnotationLastSeenGeneration: fmt.Sprintf("%d", endpointSlice.Generation),
				objectmeta.MetricsAnnotationLastSeenTimestamp:  startTime.Format(objectmeta.MetricsLastSeenTimestampFormat),
			}
			Expect(memberClient.Update(ctx, endpointSlice)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, endpointSlice)).Should(Succeed())
			// Confirm that the EndpointSlice is deleted; this helps make the test less flaky.
			Eventually(endpointSliceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should remove exported endpointslice from unexported service", func() {
			Eventually(endpointSliceIsNotExportedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(endpointSliceUniqueNameIsNotAssignedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})
	})

	Context("exported endpointslice with invalid exported service", func() {
		var (
			endpointSlice       *discoveryv1.EndpointSlice
			endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport
			svcExport           *fleetnetv1alpha1.ServiceExport

			startTime = time.Now()
		)

		BeforeEach(func() {
			// Must add the unique name annotation later; controller may reconcile too quickly for the desired
			// test case to happen.
			endpointSlice = managedIPv4EndpointSliceWithoutUniqueNameAnnotation()
			Expect(memberClient.Create(ctx, endpointSlice)).Should(Succeed())

			endpointSliceExport = endpointSliceExportTemplate.DeepCopy()
			endpointSliceExport.Spec.EndpointSliceReference = fleetnetv1alpha1.FromMetaObjects(
				memberClusterID,
				endpointSlice.TypeMeta,
				endpointSlice.ObjectMeta,
				metav1.NewTime(startTime),
			)
			Expect(hubClient.Create(ctx, endpointSliceExport)).Should(Succeed())

			svcExport = notYetFulfilledServiceExport()
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())
			meta.SetStatusCondition(&svcExport.Status.Conditions, serviceExportInvalidNotFoundCondition(memberUserNS, svcName))
			Expect(memberClient.Status().Update(ctx, svcExport)).Should(Succeed())

			// Add the unique name annotation now.
			endpointSlice.Annotations = map[string]string{
				objectmeta.ExportedObjectAnnotationUniqueName:  endpointSliceUniqueName,
				objectmeta.MetricsAnnotationLastSeenGeneration: fmt.Sprintf("%d", endpointSlice.Generation),
				objectmeta.MetricsAnnotationLastSeenTimestamp:  startTime.Format(objectmeta.MetricsLastSeenTimestampFormat),
			}
			Expect(memberClient.Update(ctx, endpointSlice)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, endpointSlice)).Should(Succeed())
			// Confirm that the EndpointSlice is deleted; this helps make the test less flaky.
			Eventually(endpointSliceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
			// Confirm that the ServiceExport is deleted; this helps make the test less flaky.
			Eventually(serviceExportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should remove exported endpointslice from invalid service", func() {
			Eventually(endpointSliceIsNotExportedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(endpointSliceUniqueNameIsNotAssignedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})
	})

	Context("exported endpointslice with conflicted exported service", func() {
		var (
			endpointSlice       *discoveryv1.EndpointSlice
			endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport
			svcExport           *fleetnetv1alpha1.ServiceExport

			startTime = time.Now()
		)

		BeforeEach(func() {
			// Must add the unique name annotation later; controller may reconcile too quickly for the desired
			// test case to happen.
			endpointSlice = managedIPv4EndpointSliceWithoutUniqueNameAnnotation()
			Expect(memberClient.Create(ctx, endpointSlice)).Should(Succeed())

			endpointSliceExport = endpointSliceExportTemplate.DeepCopy()
			endpointSliceExport.Spec.EndpointSliceReference = fleetnetv1alpha1.FromMetaObjects(
				memberClusterID,
				endpointSlice.TypeMeta,
				endpointSlice.ObjectMeta,
				metav1.NewTime(startTime),
			)
			Expect(hubClient.Create(ctx, endpointSliceExport)).Should(Succeed())

			svcExport = notYetFulfilledServiceExport()
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())
			meta.SetStatusCondition(&svcExport.Status.Conditions, serviceExportConflictedCondition(memberUserNS, svcName))
			Expect(memberClient.Status().Update(ctx, svcExport)).Should(Succeed())

			// Add the unique name annotation now.
			endpointSlice.Annotations = map[string]string{
				objectmeta.ExportedObjectAnnotationUniqueName:  endpointSliceUniqueName,
				objectmeta.MetricsAnnotationLastSeenGeneration: fmt.Sprintf("%d", endpointSlice.Generation),
				objectmeta.MetricsAnnotationLastSeenTimestamp:  startTime.Format(objectmeta.MetricsLastSeenTimestampFormat),
			}
			Expect(memberClient.Update(ctx, endpointSlice)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, endpointSlice)).Should(Succeed())
			// Confirm that the EndpointSlice is deleted; this helps make the test less flaky.
			Eventually(endpointSliceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
			// Confirm that the ServiceExport is deleted; this helps make the test less flaky.
			Eventually(serviceExportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should remove exported endpointslice from conflicted exported service", func() {
			Eventually(endpointSliceIsNotExportedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(endpointSliceUniqueNameIsNotAssignedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})
	})

	Context("exported but deleted endpointslice", func() {
		var (
			endpointSlice       *discoveryv1.EndpointSlice
			endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport
			svcExport           *fleetnetv1alpha1.ServiceExport

			startTime = time.Now()
		)

		BeforeEach(func() {
			// Must add the unique name annotation later; controller may reconcile too quickly for the desired
			// test case to happen.
			endpointSlice = managedIPv4EndpointSliceWithoutUniqueNameAnnotation()
			Expect(memberClient.Create(ctx, endpointSlice)).Should(Succeed())

			endpointSliceExport = endpointSliceExportTemplate.DeepCopy()
			endpointSliceExport.Spec.EndpointSliceReference = fleetnetv1alpha1.FromMetaObjects(
				memberClusterID,
				endpointSlice.TypeMeta,
				endpointSlice.ObjectMeta,
				metav1.NewTime(startTime),
			)
			Expect(hubClient.Create(ctx, endpointSliceExport)).Should(Succeed())

			svcExport = notYetFulfilledServiceExport()
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())
			meta.SetStatusCondition(&svcExport.Status.Conditions, serviceExportValidCondition(memberUserNS, svcName))
			meta.SetStatusCondition(&svcExport.Status.Conditions, serviceExportNoConflictCondition(memberUserNS, svcName))
			Expect(memberClient.Status().Update(ctx, svcExport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
			// Confirm that the ServiceExport is deleted; this helps make the test less flaky.
			Eventually(serviceExportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Remove the finalizer.
			Expect(memberClient.Get(ctx, endpointSliceKey, endpointSlice)).Should(Succeed())
			endpointSlice.ObjectMeta.Finalizers = []string{}
			Expect(memberClient.Update(ctx, endpointSlice)).Should(Succeed())

			Eventually(endpointSliceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should remove exported but deleted endpointslice", func() {
			// Add the unique name annotation now; a finalizer is also added to prevent premature deletion.
			Eventually(func() error {
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
					return err
				}
				endpointSlice.Annotations = map[string]string{
					objectmeta.ExportedObjectAnnotationUniqueName:  endpointSliceUniqueName,
					objectmeta.MetricsAnnotationLastSeenGeneration: fmt.Sprintf("%d", endpointSlice.Generation),
					objectmeta.MetricsAnnotationLastSeenTimestamp:  startTime.Format(objectmeta.MetricsLastSeenTimestampFormat),
				}
				endpointSlice.ObjectMeta.Finalizers = []string{"networking.fleet.azure.com/test"}
				return memberClient.Update(ctx, endpointSlice)
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

			// Set the deletion timestamp.
			Expect(memberClient.Delete(ctx, endpointSlice)).Should(Succeed())

			Eventually(endpointSliceIsNotExportedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})
	})
})

var _ = Describe("endpointslice controller (export endpointslice or update exported endpointslice)", Serial, Ordered, func() {
	Context("new endpointslice for export", func() {
		var (
			endpointSlice *discoveryv1.EndpointSlice
			svcExport     *fleetnetv1alpha1.ServiceExport

			// Apply an offset of 1 second to account for limited timing precision.
			startTime     = time.Now().Add(-time.Second * 1)
			trueStartTime time.Time
		)

		BeforeEach(func() {
			svcExport = notYetFulfilledServiceExport()
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())
			meta.SetStatusCondition(&svcExport.Status.Conditions, serviceExportValidCondition(memberUserNS, svcName))
			meta.SetStatusCondition(&svcExport.Status.Conditions, serviceExportNoConflictCondition(memberUserNS, svcName))
			Expect(memberClient.Status().Update(ctx, svcExport)).Should(Succeed())

			endpointSlice = managedIPv4EndpointSliceWithoutUniqueNameAnnotation()
			Expect(memberClient.Create(ctx, endpointSlice)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, endpointSlice)).Should(Succeed())
			// Confirm that the EndpointSlice is deleted; this helps make the test less flaky.
			Eventually(endpointSliceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
			// Confirm that the ServiceExport is deleted; this helps make the test less flaky.
			Eventually(serviceExportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Expect(hubClient.DeleteAllOf(ctx, &fleetnetv1alpha1.EndpointSliceExport{}, client.InNamespace(hubNSForMember))).Should(Succeed())
			// Confirm that all EndpointSliceExports have been deleted; this helps make the test less flaky.
			Eventually(endpointSliceIsNotExportedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should export the new endpointslice", func() {
			Eventually(func() error {
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
					return fmt.Errorf("endpointSlice Get(%+v), got %v, want no error", endpointSliceKey, err)
				}

				uniqueName, ok := endpointSlice.Annotations[objectmeta.ExportedObjectAnnotationUniqueName]
				if !ok || !strings.HasPrefix(uniqueName, fmt.Sprintf("%s-%s-%s-", memberClusterID, memberUserNS, endpointSliceName)) {
					return fmt.Errorf("endpointSlice unique name, got %s, want prefix %s", uniqueName, fmt.Sprintf("%s-%s-%s-", memberClusterID, memberUserNS, endpointSliceName))
				}

				lastSeenGenerationData, ok := endpointSlice.Annotations[objectmeta.MetricsAnnotationLastSeenGeneration]
				if !ok || lastSeenGenerationData != fmt.Sprintf("%d", endpointSlice.Generation) {
					return fmt.Errorf("lastSeenGenerationData, got %s, want %d", lastSeenGenerationData, endpointSlice.Generation)
				}

				lastSeenTimestampData, ok := endpointSlice.Annotations[objectmeta.MetricsAnnotationLastSeenTimestamp]
				if !ok {
					return fmt.Errorf("lastSeenTimestampData is absent")
				}
				lastSeenTimestamp, err := time.Parse(objectmeta.MetricsLastSeenTimestampFormat, lastSeenTimestampData)
				if err != nil {
					return fmt.Errorf("lastSeenTimestamp Parse(%s), got %v, want no error", lastSeenTimestamp, err)
				}
				trueStartTime = lastSeenTimestamp
				if lastSeenTimestamp.Before(startTime) {
					return fmt.Errorf("lastSeenTimestamp, got %v, want after %v", lastSeenTimestamp, startTime)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Eventually(func() error {
				endpointSliceExportList := &fleetnetv1alpha1.EndpointSliceExportList{}
				if err := hubClient.List(ctx, endpointSliceExportList, &client.ListOptions{Namespace: hubNSForMember}); err != nil {
					return fmt.Errorf("endpointSliceExport List(), got %v, want no error", err)
				}

				if len(endpointSliceExportList.Items) != 1 {
					return fmt.Errorf("endpointSliceExportList length, got %d, want %d", len(endpointSliceExportList.Items), 1)
				}

				endpointSliceRef := endpointSliceExportList.Items[0].Spec.EndpointSliceReference
				wantEndpointSliceRef := fleetnetv1alpha1.FromMetaObjects(
					memberClusterID,
					endpointSlice.TypeMeta,
					endpointSlice.ObjectMeta,
					metav1.NewTime(trueStartTime),
				)
				if diff := cmp.Diff(endpointSliceRef, wantEndpointSliceRef); diff != "" {
					return fmt.Errorf("endpointSliceReference diff (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})
	})

	Context("updated exported endpointslice", func() {
		var (
			endpointSlice *discoveryv1.EndpointSlice
			svcExport     *fleetnetv1alpha1.ServiceExport

			// Apply an offset of 1 second to account for limited timing precision.
			startTime     = time.Now().Add(-time.Second * 1)
			trueStartTime time.Time
		)

		BeforeEach(func() {
			svcExport = notYetFulfilledServiceExport()
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())
			meta.SetStatusCondition(&svcExport.Status.Conditions, serviceExportValidCondition(memberUserNS, svcName))
			meta.SetStatusCondition(&svcExport.Status.Conditions, serviceExportNoConflictCondition(memberUserNS, svcName))
			Expect(memberClient.Status().Update(ctx, svcExport)).Should(Succeed())

			endpointSlice = managedIPv4EndpointSliceWithoutUniqueNameAnnotation()
			Expect(memberClient.Create(ctx, endpointSlice)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, endpointSlice)).Should(Succeed())
			// Confirm that the EndpointSlice is deleted; this helps make the test less flaky.
			Eventually(endpointSliceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
			// Confirm that the ServiceExport is deleted; this helps make the test less flaky.
			Eventually(serviceExportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Expect(hubClient.DeleteAllOf(ctx, &fleetnetv1alpha1.EndpointSliceExport{}, client.InNamespace(hubNSForMember))).Should(Succeed())
			// Confirm that all EndpointSliceExports have been deleted; this helps make the test less flaky.
			Eventually(endpointSliceIsNotExportedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should update exported endpointslice", func() {
			// Verify first that the EndpointSlice has been exported.
			Eventually(func() error {
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
					return fmt.Errorf("endpointSlice Get(%+v), got %v, want no error", endpointSliceKey, err)
				}

				uniqueName, ok := endpointSlice.Annotations[objectmeta.ExportedObjectAnnotationUniqueName]
				if !ok || !strings.HasPrefix(uniqueName, fmt.Sprintf("%s-%s-%s-", memberClusterID, memberUserNS, endpointSliceName)) {
					return fmt.Errorf("endpointSlice unique name, got %s, want prefix %s", uniqueName, fmt.Sprintf("%s-%s-%s-", memberClusterID, memberUserNS, endpointSliceName))
				}

				lastSeenGenerationData, ok := endpointSlice.Annotations[objectmeta.MetricsAnnotationLastSeenGeneration]
				if !ok || lastSeenGenerationData != fmt.Sprintf("%d", endpointSlice.Generation) {
					return fmt.Errorf("lastSeenGenerationData, got %s, want %d", lastSeenGenerationData, endpointSlice.Generation)
				}

				lastSeenTimestampData, ok := endpointSlice.Annotations[objectmeta.MetricsAnnotationLastSeenTimestamp]
				if !ok {
					return fmt.Errorf("lastSeenTimestampData is absent")
				}
				lastSeenTimestamp, err := time.Parse(objectmeta.MetricsLastSeenTimestampFormat, lastSeenTimestampData)
				if err != nil {
					return fmt.Errorf("lastSeenTimestamp Parse(%s), got %v, want no error", lastSeenTimestamp, err)
				}
				trueStartTime = lastSeenTimestamp
				if lastSeenTimestamp.Before(startTime) {
					return fmt.Errorf("lastSeenTimestamp, got %v, want after %v", lastSeenTimestamp, startTime)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			var endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport
			Eventually(func() error {
				endpointSliceExportList := &fleetnetv1alpha1.EndpointSliceExportList{}
				if err := hubClient.List(ctx, endpointSliceExportList, &client.ListOptions{Namespace: hubNSForMember}); err != nil {
					return fmt.Errorf("endpointSliceExport List(), got %v, want no error", err)
				}

				if len(endpointSliceExportList.Items) != 1 {
					return fmt.Errorf("endpointSliceExportList length, got %d, want %d", len(endpointSliceExportList.Items), 1)
				}

				endpointSliceExport = &(endpointSliceExportList.Items[0])
				endpointSliceRef := endpointSliceExportList.Items[0].Spec.EndpointSliceReference
				wantEndpointSliceRef := fleetnetv1alpha1.FromMetaObjects(
					memberClusterID,
					endpointSlice.TypeMeta,
					endpointSlice.ObjectMeta,
					metav1.NewTime(trueStartTime),
				)
				if diff := cmp.Diff(endpointSliceRef, wantEndpointSliceRef); diff != "" {
					return fmt.Errorf("endpointSliceReference diff (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Update the EndpointSlice.
			endpointSlice.Endpoints = append(endpointSlice.Endpoints, discoveryv1.Endpoint{
				Addresses: []string{altIPv4Addr},
			})
			Expect(memberClient.Update(ctx, endpointSlice)).Should(Succeed())

			// Confirm that the last seen annotations have been updated.
			Eventually(func() error {
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
					return fmt.Errorf("endpointSlice Get(%+v), got %v, want no error", endpointSliceKey, err)
				}

				lastSeenGenerationData, ok := endpointSlice.Annotations[objectmeta.MetricsAnnotationLastSeenGeneration]
				if !ok || lastSeenGenerationData != fmt.Sprintf("%d", endpointSlice.Generation) {
					return fmt.Errorf("lastSeenGenerationData, got %s, want %d", lastSeenGenerationData, endpointSlice.Generation)
				}

				lastSeenTimestampData, ok := endpointSlice.Annotations[objectmeta.MetricsAnnotationLastSeenTimestamp]
				if !ok {
					return fmt.Errorf("lastSeenTimestampData is absent")
				}
				lastSeenTimestamp, err := time.Parse(objectmeta.MetricsLastSeenTimestampFormat, lastSeenTimestampData)
				if err != nil {
					return fmt.Errorf("lastSeenTimestamp Parse(), got %v, want no error", err)
				}
				// Due to timing precision limitations, the later timestamp may appear the same as the earlier one.
				if lastSeenTimestamp.Before(trueStartTime) {
					return fmt.Errorf("lastSeenTimestamp, got %v, want before %v", lastSeenTimestamp, trueStartTime)
				}
				trueStartTime = lastSeenTimestamp
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Confirm that the EndpointSliceExport has been updated.
			endpointSliceExportKey := types.NamespacedName{Namespace: hubNSForMember, Name: endpointSliceExport.Name}
			Eventually(func() error {
				if err := hubClient.Get(ctx, endpointSliceExportKey, endpointSliceExport); err != nil {
					return fmt.Errorf("endpointSliceExport Get(%+v), got %v, want no error", endpointSliceExportKey, err)
				}

				expectedEndpoints := []fleetnetv1alpha1.Endpoint{
					{
						Addresses: []string{ipv4Addr},
					},
					{
						Addresses: []string{altIPv4Addr},
					},
				}
				if diff := cmp.Diff(endpointSliceExport.Spec.Endpoints, expectedEndpoints); diff != "" {
					return fmt.Errorf("endpoints (-got, +want): %s", diff)
				}

				endpointSliceRef := endpointSliceExport.Spec.EndpointSliceReference
				wantEndpointSliceRef := fleetnetv1alpha1.FromMetaObjects(
					memberClusterID,
					endpointSlice.TypeMeta,
					endpointSlice.ObjectMeta,
					metav1.NewTime(trueStartTime),
				)
				if diff := cmp.Diff(endpointSliceRef, wantEndpointSliceRef); diff != "" {
					return fmt.Errorf("endpointSliceReference (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})
	})

	Context("exported endpointslice with tampered invalid unique name annotation", func() {
		var (
			endpointSlice *discoveryv1.EndpointSlice
			svcExport     *fleetnetv1alpha1.ServiceExport

			// Apply an offset of 1 second to account for limited timing precision.
			startTime     = time.Now().Add(-time.Second * 1)
			trueStartTime time.Time
		)

		BeforeEach(func() {
			svcExport = notYetFulfilledServiceExport()
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())
			meta.SetStatusCondition(&svcExport.Status.Conditions, serviceExportValidCondition(memberUserNS, svcName))
			meta.SetStatusCondition(&svcExport.Status.Conditions, serviceExportNoConflictCondition(memberUserNS, svcName))
			Expect(memberClient.Status().Update(ctx, svcExport)).Should(Succeed())

			endpointSlice = managedIPv4EndpointSliceWithoutUniqueNameAnnotation()
			Expect(memberClient.Create(ctx, endpointSlice)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, endpointSlice)).Should(Succeed())
			// Confirm that the EndpointSlice is deleted; this helps make the test less flaky.
			Eventually(endpointSliceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
			// Confirm that the ServiceExport is deleted; this helps make the test less flaky.
			Eventually(serviceExportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Expect(hubClient.DeleteAllOf(ctx, &fleetnetv1alpha1.EndpointSliceExport{}, client.InNamespace(hubNSForMember))).Should(Succeed())
			// Confirm that all EndpointSliceExports have been deleted; this helps make the test less flaky.
			Eventually(endpointSliceIsNotExportedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should export the endpointslice with the invalid unique name annotation again with a new assigned unique name", func() {
			// Verify first that the EndpointSlice has been exported.
			Eventually(func() error {
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
					return fmt.Errorf("endpointSlice Get(%+v), got %v, want no error", endpointSliceKey, err)
				}

				uniqueName, ok := endpointSlice.Annotations[objectmeta.ExportedObjectAnnotationUniqueName]
				if !ok || !strings.HasPrefix(uniqueName, fmt.Sprintf("%s-%s-%s-", memberClusterID, memberUserNS, endpointSliceName)) {
					return fmt.Errorf("endpointSlice unique name, got %s, want prefix %s", uniqueName, fmt.Sprintf("%s-%s-%s-", memberClusterID, memberUserNS, endpointSliceName))
				}

				lastSeenGenerationData, ok := endpointSlice.Annotations[objectmeta.MetricsAnnotationLastSeenGeneration]
				if !ok || lastSeenGenerationData != fmt.Sprintf("%d", endpointSlice.Generation) {
					return fmt.Errorf("lastSeenGenerationData, got %s, want %d", lastSeenGenerationData, endpointSlice.Generation)
				}

				lastSeenTimestampData, ok := endpointSlice.Annotations[objectmeta.MetricsAnnotationLastSeenTimestamp]
				if !ok {
					return fmt.Errorf("lastSeenTimestampData is absent")
				}
				lastSeenTimestamp, err := time.Parse(objectmeta.MetricsLastSeenTimestampFormat, lastSeenTimestampData)
				if err != nil {
					return fmt.Errorf("lastSeenTimestamp Parse(%s), got %v, want no error", lastSeenTimestamp, err)
				}
				trueStartTime = lastSeenTimestamp
				if lastSeenTimestamp.Before(startTime) {
					return fmt.Errorf("lastSeenTimestamp, got %v, want after %v", lastSeenTimestamp, startTime)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			var originalEndpointSliceExport *fleetnetv1alpha1.EndpointSliceExport
			Eventually(func() error {
				endpointSliceExportList := &fleetnetv1alpha1.EndpointSliceExportList{}
				if err := hubClient.List(ctx, endpointSliceExportList, &client.ListOptions{Namespace: hubNSForMember}); err != nil {
					return fmt.Errorf("endpointSliceExport List(), got %v, want no error", err)
				}

				if len(endpointSliceExportList.Items) != 1 {
					return fmt.Errorf("endpointSliceExport list length, got %d, want %d", len(endpointSliceExportList.Items), 1)
				}

				originalEndpointSliceExport = &endpointSliceExportList.Items[0]
				endpointSliceRef := originalEndpointSliceExport.Spec.EndpointSliceReference
				wantEndpointSliceRef := fleetnetv1alpha1.FromMetaObjects(
					memberClusterID,
					endpointSlice.TypeMeta,
					endpointSlice.ObjectMeta,
					metav1.NewTime(trueStartTime),
				)
				if diff := cmp.Diff(endpointSliceRef, wantEndpointSliceRef); diff != "" {
					return fmt.Errorf("endpointSliceReference (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Tamper with the unique name annotation.
			endpointSlice.Annotations[objectmeta.ExportedObjectAnnotationUniqueName] = "x_y" // "x_y" is not a valid DNS subdomain.
			Expect(memberClient.Update(ctx, endpointSlice)).Should(Succeed())

			// Confirm that the EndpointSlice has been exported again with a new name.
			Eventually(func() error {
				endpointSliceExportList := &fleetnetv1alpha1.EndpointSliceExportList{}
				if err := hubClient.List(ctx, endpointSliceExportList, &client.ListOptions{Namespace: hubNSForMember}); err != nil {
					return fmt.Errorf("endpointSliceExport List(), got %v, want no error", err)
				}

				if len(endpointSliceExportList.Items) != 2 {
					return fmt.Errorf("endpointSliceExport list length, got %d, want %d", len(endpointSliceExportList.Items), 2)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Expect(memberClient.Get(ctx, endpointSliceKey, endpointSlice)).Should(Succeed())
			newEndpointSliceExportName := endpointSlice.Annotations[objectmeta.ExportedObjectAnnotationUniqueName]
			Expect(strings.HasPrefix(newEndpointSliceExportName, fmt.Sprintf("%s-%s-%s-", memberClusterID, memberUserNS, endpointSliceName)))
			Expect(newEndpointSliceExportName != originalEndpointSliceExport.Name).Should(BeTrue())

			newEndpointSliceExport := &fleetnetv1alpha1.EndpointSliceExport{}
			newEndpointSliceExportKey := types.NamespacedName{
				Namespace: hubNSForMember,
				Name:      newEndpointSliceExportName,
			}
			Expect(hubClient.Get(ctx, newEndpointSliceExportKey, newEndpointSliceExport)).Should(Succeed())
			endpointSliceRef := newEndpointSliceExport.Spec.EndpointSliceReference
			wantEndpointSliceRef := fleetnetv1alpha1.FromMetaObjects(
				memberClusterID,
				endpointSlice.TypeMeta,
				endpointSlice.ObjectMeta,
				metav1.NewTime(trueStartTime),
			)
			Expect(cmp.Diff(endpointSliceRef, wantEndpointSliceRef)).Should(BeZero())
		})
	})

	Context("exported endpointslice with tampered used unique name annotation", func() {
		var (
			endpointSlice *discoveryv1.EndpointSlice
			svcExport     *fleetnetv1alpha1.ServiceExport

			// Apply an offset of 1 second to account for limited timing precision.
			startTime     = time.Now().Add(-time.Second * 1)
			trueStartTime time.Time
		)

		altEndpointSliceExport := &fleetnetv1alpha1.EndpointSliceExport{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: hubNSForMember,
				Name:      endpointSliceUniqueName,
			},
			Spec: fleetnetv1alpha1.EndpointSliceExportSpec{
				AddressType: discoveryv1.AddressTypeIPv4,
				Endpoints: []fleetnetv1alpha1.Endpoint{
					{
						Addresses: []string{altIPv4Addr},
					},
				},
				Ports: []discoveryv1.EndpointPort{
					{
						Port: &endpointSlicePort,
					},
				},
				EndpointSliceReference: fleetnetv1alpha1.ExportedObjectReference{
					ClusterID:       memberClusterID,
					Kind:            "EndpointSlice",
					Namespace:       memberUserNS,
					Name:            altEndpointSliceName,
					ResourceVersion: "0",
					Generation:      1,
					UID:             "1",
					ExportedSince:   metav1.Now(),
				},
				OwnerServiceReference: fleetnetv1alpha1.OwnerServiceReference{
					Namespace: memberUserNS,
					Name:      svcName,
				},
			},
		}

		BeforeEach(func() {
			svcExport = notYetFulfilledServiceExport()
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())
			meta.SetStatusCondition(&svcExport.Status.Conditions, serviceExportValidCondition(memberUserNS, svcName))
			meta.SetStatusCondition(&svcExport.Status.Conditions, serviceExportNoConflictCondition(memberUserNS, svcName))
			Expect(memberClient.Status().Update(ctx, svcExport)).Should(Succeed())

			endpointSlice = managedIPv4EndpointSliceWithoutUniqueNameAnnotation()
			Expect(memberClient.Create(ctx, endpointSlice)).Should(Succeed())

			Expect(hubClient.Create(ctx, altEndpointSliceExport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, endpointSlice)).Should(Succeed())
			// Confirm that the EndpointSlice is deleted; this helps make the test less flaky.
			Eventually(endpointSliceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
			// Confirm that the ServiceExport is deleted; this helps make the test less flaky.
			Eventually(serviceExportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Expect(hubClient.DeleteAllOf(ctx, &fleetnetv1alpha1.EndpointSliceExport{}, client.InNamespace(hubNSForMember))).Should(Succeed())
			// Confirm that all EndpointSliceExports have been deleted; this helps make the test less flaky.
			Eventually(endpointSliceIsNotExportedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should export the endpointslice with the used unique name annotation again with a new assigned unique name", func() {
			// Verify first that the EndpointSlice has been exported.
			Eventually(func() error {
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
					return fmt.Errorf("endpointSlice Get(%+v), got %v, want no error", endpointSliceKey, err)
				}

				uniqueName, ok := endpointSlice.Annotations[objectmeta.ExportedObjectAnnotationUniqueName]
				if !ok || !strings.HasPrefix(uniqueName, fmt.Sprintf("%s-%s-%s-", memberClusterID, memberUserNS, endpointSliceName)) {
					return fmt.Errorf("endpointSlice unique name, got %s, want prefix %s", uniqueName, fmt.Sprintf("%s-%s-%s-", memberClusterID, memberUserNS, endpointSliceName))
				}

				lastSeenGenerationData, ok := endpointSlice.Annotations[objectmeta.MetricsAnnotationLastSeenGeneration]
				if !ok || lastSeenGenerationData != fmt.Sprintf("%d", endpointSlice.Generation) {
					return fmt.Errorf("lastSeenGenerationData, got %s, want %d", lastSeenGenerationData, endpointSlice.Generation)
				}

				lastSeenTimestampData, ok := endpointSlice.Annotations[objectmeta.MetricsAnnotationLastSeenTimestamp]
				if !ok {
					return fmt.Errorf("lastSeenTimestampData is absent")
				}
				lastSeenTimestamp, err := time.Parse(objectmeta.MetricsLastSeenTimestampFormat, lastSeenTimestampData)
				if err != nil {
					return fmt.Errorf("lastSeenTimestamp Parse(%s), got %v, want no error", lastSeenTimestamp, err)
				}
				trueStartTime = lastSeenTimestamp
				if lastSeenTimestamp.Before(startTime) {
					return fmt.Errorf("lastSeenTimestamp, got %v, want after %v", lastSeenTimestamp, startTime)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			var originalEndpointSliceExport *fleetnetv1alpha1.EndpointSliceExport
			Eventually(func() error {
				endpointSliceExportList := &fleetnetv1alpha1.EndpointSliceExportList{}
				if err := hubClient.List(ctx, endpointSliceExportList, &client.ListOptions{Namespace: hubNSForMember}); err != nil {
					return fmt.Errorf("endpointSliceExport List(), got %v, want no error", err)
				}

				if len(endpointSliceExportList.Items) != 2 {
					return fmt.Errorf("endpointSliceExport list length, got %d, want %d", len(endpointSliceExportList.Items), 2)
				}

				for idx := range endpointSliceExportList.Items {
					endpointSliceExport := endpointSliceExportList.Items[idx]
					endpointSliceRef := endpointSliceExport.Spec.EndpointSliceReference
					if endpointSliceRef.Name == endpointSliceName && endpointSliceRef.UID == endpointSlice.UID {
						originalEndpointSliceExport = &endpointSliceExport
						break
					}
				}
				if originalEndpointSliceExport == nil {
					return fmt.Errorf("exported endpointslice %s is not found", endpointSlice.Name)
				}

				endpointSliceRef := originalEndpointSliceExport.Spec.EndpointSliceReference
				wantEndpointSliceRef := fleetnetv1alpha1.FromMetaObjects(
					memberClusterID,
					endpointSlice.TypeMeta,
					endpointSlice.ObjectMeta,
					metav1.NewTime(trueStartTime),
				)
				if diff := cmp.Diff(endpointSliceRef, wantEndpointSliceRef); diff != "" {
					return fmt.Errorf("endpointSliceReference (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Tamper with the unique name annotation.
			endpointSlice.Annotations[objectmeta.ExportedObjectAnnotationUniqueName] = endpointSliceUniqueName
			Expect(memberClient.Update(ctx, endpointSlice)).Should(Succeed())

			// Confirm that the EndpointSlice has been exported again with a new name.
			Eventually(func() error {
				endpointSliceExportList := &fleetnetv1alpha1.EndpointSliceExportList{}
				if err := hubClient.List(ctx, endpointSliceExportList, &client.ListOptions{Namespace: hubNSForMember}); err != nil {
					return fmt.Errorf("endpointSliceExport List(), got %v, want no error", err)
				}

				if len(endpointSliceExportList.Items) != 3 {
					return fmt.Errorf("endpointSliceExport list length, got %d, want %d", len(endpointSliceExportList.Items), 3)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Expect(memberClient.Get(ctx, endpointSliceKey, endpointSlice)).Should(Succeed())
			newEndpointSliceExportName := endpointSlice.Annotations[objectmeta.ExportedObjectAnnotationUniqueName]
			Expect(strings.HasPrefix(newEndpointSliceExportName, fmt.Sprintf("%s-%s-%s-", memberClusterID, memberUserNS, endpointSliceName)))
			Expect(newEndpointSliceExportName != originalEndpointSliceExport.Name).Should(BeTrue())

			newEndpointSliceExport := &fleetnetv1alpha1.EndpointSliceExport{}
			newEndpointSliceExportKey := types.NamespacedName{
				Namespace: hubNSForMember,
				Name:      newEndpointSliceExportName,
			}
			Expect(hubClient.Get(ctx, newEndpointSliceExportKey, newEndpointSliceExport)).Should(Succeed())
			endpointSliceRef := newEndpointSliceExport.Spec.EndpointSliceReference
			wantEndpointSliceRef := fleetnetv1alpha1.FromMetaObjects(
				memberClusterID,
				endpointSlice.TypeMeta,
				endpointSlice.ObjectMeta,
				metav1.NewTime(trueStartTime),
			)
			Expect(cmp.Diff(endpointSliceRef, wantEndpointSliceRef)).Should(BeZero())
		})
	})
})

var _ = Describe("endpointslice controller (service export status changes)", Serial, Ordered, func() {
	Context("endpointslices when service export becomes valid with no conflicts", func() {
		var (
			endpointSlice    *discoveryv1.EndpointSlice
			altEndpointSlice = &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      altEndpointSliceName,
					Labels: map[string]string{
						discoveryv1.LabelServiceName: svcName,
					},
				},
				AddressType: discoveryv1.AddressTypeIPv4,
				Endpoints: []discoveryv1.Endpoint{
					{
						Addresses: []string{altIPv4Addr},
					},
				},
				Ports: []discoveryv1.EndpointPort{
					{
						Port: &endpointSlicePort,
					},
				},
			}
			altEndpointSliceKey = types.NamespacedName{
				Namespace: memberUserNS,
				Name:      altEndpointSliceName,
			}
			svcExport *fleetnetv1alpha1.ServiceExport

			// Apply an offset of 1 second to account for limited timing precision.
			startTime = time.Now().Add(-time.Second * 1)
		)

		BeforeEach(func() {
			endpointSlice = managedIPv4EndpointSliceWithoutUniqueNameAnnotation()
			Expect(memberClient.Create(ctx, endpointSlice)).Should(Succeed())

			Expect(memberClient.Create(ctx, altEndpointSlice)).Should(Succeed())

			svcExport = notYetFulfilledServiceExport()
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())
			meta.SetStatusCondition(&svcExport.Status.Conditions, serviceExportValidCondition(memberUserNS, svcName))
			meta.SetStatusCondition(&svcExport.Status.Conditions, serviceExportNoConflictCondition(memberUserNS, svcName))
			Expect(memberClient.Status().Update(ctx, svcExport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, endpointSlice)).Should(Succeed())
			// Confirm that the EndpointSlice is deleted; this helps make the test less flaky.
			Eventually(endpointSliceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Expect(memberClient.Delete(ctx, altEndpointSlice)).Should(Succeed())
			// Confirm that the EndpointSlice is deleted; this helps make the test less flaky.
			Eventually(func() error {
				endpointSlice := &discoveryv1.EndpointSlice{}
				if err := memberClient.Get(ctx, altEndpointSliceKey, endpointSlice); !errors.IsNotFound(err) {
					return fmt.Errorf("endpointSlice Get(%+v), got %v, want not found", altEndpointSliceKey, err)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
			// Confirm that the ServiceExport is deleted; this helps make the test less flaky.
			Eventually(serviceExportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Expect(hubClient.DeleteAllOf(ctx, &fleetnetv1alpha1.EndpointSliceExport{}, client.InNamespace(hubNSForMember))).Should(Succeed())
			// Confirm that all EndpointSliceExports have been deleted; this helps make the test less flaky.
			Eventually(endpointSliceIsNotExportedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should export endpointslices when service export becomes valid", func() {
			Eventually(func() error {
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
					return fmt.Errorf("endpointSlice Get(%+v), got %v, want no error", endpointSliceKey, err)
				}

				uniqueName, ok := endpointSlice.Annotations[objectmeta.ExportedObjectAnnotationUniqueName]
				if !ok || !strings.HasPrefix(uniqueName, fmt.Sprintf("%s-%s-%s-", memberClusterID, memberUserNS, endpointSliceName)) {
					return fmt.Errorf("endpointSlice unique name, got %s, want prefix %s", uniqueName, fmt.Sprintf("%s-%s-%s-", memberClusterID, memberUserNS, endpointSliceName))
				}

				lastSeenGenerationData, ok := endpointSlice.Annotations[objectmeta.MetricsAnnotationLastSeenGeneration]
				if !ok || lastSeenGenerationData != fmt.Sprintf("%d", endpointSlice.Generation) {
					return fmt.Errorf("lastSeenGenerationData, got %s, want %d", lastSeenGenerationData, endpointSlice.Generation)
				}

				lastSeenTimestampData, ok := endpointSlice.Annotations[objectmeta.MetricsAnnotationLastSeenTimestamp]
				if !ok {
					return fmt.Errorf("lastSeenTimestampData is absent")
				}
				lastSeenTimestamp, err := time.Parse(objectmeta.MetricsLastSeenTimestampFormat, lastSeenTimestampData)
				if err != nil {
					return fmt.Errorf("lastSeenTimestamp Parse(%s), got %v, want no error", lastSeenTimestamp, err)
				}
				if lastSeenTimestamp.Before(startTime) {
					return fmt.Errorf("lastSeenTimestamp, got %v, want after %v", lastSeenTimestamp, startTime)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Eventually(func() error {
				if err := memberClient.Get(ctx, altEndpointSliceKey, altEndpointSlice); err != nil {
					return fmt.Errorf("endpointSlice Get(%+v), got %v, want no error", altEndpointSliceKey, err)
				}

				uniqueName, ok := altEndpointSlice.Annotations[objectmeta.ExportedObjectAnnotationUniqueName]
				if !ok || !strings.HasPrefix(uniqueName, fmt.Sprintf("%s-%s-%s-", memberClusterID, memberUserNS, altEndpointSliceName)) {
					return fmt.Errorf("endpointSlice unique name, got %s, want prefix %s", uniqueName, fmt.Sprintf("%s-%s-%s-", memberClusterID, memberUserNS, endpointSliceName))
				}

				lastSeenGenerationData, ok := altEndpointSlice.Annotations[objectmeta.MetricsAnnotationLastSeenGeneration]
				if !ok || lastSeenGenerationData != fmt.Sprintf("%d", altEndpointSlice.Generation) {
					return fmt.Errorf("lastSeenGenerationData, got %s, want %d", lastSeenGenerationData, endpointSlice.Generation)
				}

				lastSeenTimestampData, ok := altEndpointSlice.Annotations[objectmeta.MetricsAnnotationLastSeenTimestamp]
				if !ok {
					return fmt.Errorf("lastSeenTimestampData is absent")
				}
				lastSeenTimestamp, err := time.Parse(objectmeta.MetricsLastSeenTimestampFormat, lastSeenTimestampData)
				if err != nil {
					return fmt.Errorf("lastSeenTimestamp Parse(%s), got %v, want no error", lastSeenTimestamp, err)
				}
				if lastSeenTimestamp.Before(startTime) {
					return fmt.Errorf("lastSeenTimestamp, got %v, want after %v", lastSeenTimestamp, startTime)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Eventually(func() error {
				endpointSliceExportList := &fleetnetv1alpha1.EndpointSliceExportList{}
				if err := hubClient.List(ctx, endpointSliceExportList, &client.ListOptions{Namespace: hubNSForMember}); err != nil {
					return fmt.Errorf("endpointSliceExport List(), got %v, want no error", err)
				}

				if len(endpointSliceExportList.Items) != 2 {
					return fmt.Errorf("endpointSliceExport list length, got %d, want %d", len(endpointSliceExportList.Items), 2)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})
	})

	Context("endpointslices when service export becomes invalid", func() {
		var (
			endpointSlice *discoveryv1.EndpointSlice
			svcExport     *fleetnetv1alpha1.ServiceExport

			// Apply an offset of 1 second to account for limited timing precision.
			startTime     = time.Now().Add(-time.Second * 1)
			trueStartTime time.Time
		)

		BeforeEach(func() {
			svcExport = notYetFulfilledServiceExport()
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())
			meta.SetStatusCondition(&svcExport.Status.Conditions, serviceExportValidCondition(memberUserNS, svcName))
			meta.SetStatusCondition(&svcExport.Status.Conditions, serviceExportNoConflictCondition(memberUserNS, svcName))
			Expect(memberClient.Status().Update(ctx, svcExport)).Should(Succeed())

			endpointSlice = managedIPv4EndpointSliceWithoutUniqueNameAnnotation()
			Expect(memberClient.Create(ctx, endpointSlice)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, endpointSlice)).Should(Succeed())
			// Confirm that the EndpointSlice is deleted; this helps make the test less flaky.
			Eventually(endpointSliceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
			// Confirm that the ServiceExport is deleted; this helps make the test less flaky.
			Eventually(serviceExportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should unexport endpointslices when service export becomes invalid", func() {
			// Confirm that the EndpointSlice has been exported.
			Eventually(func() error {
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
					return fmt.Errorf("endpointSlice Get(%+v), got %v, want no error", endpointSliceKey, err)
				}

				uniqueName, ok := endpointSlice.Annotations[objectmeta.ExportedObjectAnnotationUniqueName]
				if !ok || !strings.HasPrefix(uniqueName, fmt.Sprintf("%s-%s-%s-", memberClusterID, memberUserNS, endpointSliceName)) {
					return fmt.Errorf("endpointSlice unique name, got %s, want prefix %s", uniqueName, fmt.Sprintf("%s-%s-%s-", memberClusterID, memberUserNS, endpointSliceName))
				}

				lastSeenGenerationData, ok := endpointSlice.Annotations[objectmeta.MetricsAnnotationLastSeenGeneration]
				if !ok || lastSeenGenerationData != fmt.Sprintf("%d", endpointSlice.Generation) {
					return fmt.Errorf("lastSeenGenerationData, got %s, want %d", lastSeenGenerationData, endpointSlice.Generation)
				}

				lastSeenTimestampData, ok := endpointSlice.Annotations[objectmeta.MetricsAnnotationLastSeenTimestamp]
				if !ok {
					return fmt.Errorf("lastSeenTimestampData is absent")
				}
				lastSeenTimestamp, err := time.Parse(objectmeta.MetricsLastSeenTimestampFormat, lastSeenTimestampData)
				if err != nil {
					return fmt.Errorf("lastSeenTimestamp Parse(%s), got %v, want no error", lastSeenTimestamp, err)
				}
				trueStartTime = lastSeenTimestamp
				if lastSeenTimestamp.Before(startTime) {
					return fmt.Errorf("lastSeenTimestamp, got %v, want after %v", lastSeenTimestamp, startTime)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Eventually(func() error {
				endpointSliceExportList := &fleetnetv1alpha1.EndpointSliceExportList{}
				if err := hubClient.List(ctx, endpointSliceExportList, &client.ListOptions{Namespace: hubNSForMember}); err != nil {
					return fmt.Errorf("endpointSliceExport List(), got %v, want no error", err)
				}

				if len(endpointSliceExportList.Items) != 1 {
					return fmt.Errorf("endpointSliceExport list length, got %d, want %d", len(endpointSliceExportList.Items), 1)
				}

				endpointSliceRef := endpointSliceExportList.Items[0].Spec.EndpointSliceReference
				wantEndpointSliceRef := fleetnetv1alpha1.FromMetaObjects(
					memberClusterID,
					endpointSlice.TypeMeta,
					endpointSlice.ObjectMeta,
					metav1.NewTime(trueStartTime),
				)
				if diff := cmp.Diff(endpointSliceRef, wantEndpointSliceRef); diff != "" {
					return fmt.Errorf("endpointSliceReference (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Update the status of ServiceExport (valid -> invalid).
			meta.SetStatusCondition(&svcExport.Status.Conditions, serviceExportInvalidNotFoundCondition(memberUserNS, svcName))
			Expect(memberClient.Status().Update(ctx, svcExport)).Should(Succeed())

			// Confirm that the EndpointSlice has been unexported.
			Eventually(func() error {
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
					return fmt.Errorf("endpointSlice Get(%+v), got %v, want no error", endpointSliceKey, err)
				}

				if len(endpointSlice.Annotations) != 0 {
					return fmt.Errorf("endpointSlice annotations, got %v, want empty", endpointSlice.Annotations)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Eventually(endpointSliceIsNotExportedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})
	})

	Context("endpointslices when service export becomes conflicted", func() {
		var (
			endpointSlice *discoveryv1.EndpointSlice
			svcExport     *fleetnetv1alpha1.ServiceExport

			// Apply an offset of 1 second to account for limited timing precision.
			startTime     = time.Now().Add(-time.Second * 1)
			trueStartTime time.Time
		)

		BeforeEach(func() {
			svcExport = notYetFulfilledServiceExport()
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())
			meta.SetStatusCondition(&svcExport.Status.Conditions, serviceExportValidCondition(memberUserNS, svcName))
			meta.SetStatusCondition(&svcExport.Status.Conditions, serviceExportNoConflictCondition(memberUserNS, svcName))
			Expect(memberClient.Status().Update(ctx, svcExport)).Should(Succeed())

			endpointSlice = managedIPv4EndpointSliceWithoutUniqueNameAnnotation()
			Expect(memberClient.Create(ctx, endpointSlice)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, endpointSlice)).Should(Succeed())
			// Confirm that the EndpointSlice is deleted; this helps make the test less flaky.
			Eventually(endpointSliceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
			// Confirm that the ServiceExport is deleted; this helps make the test less flaky.
			Eventually(serviceExportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should unexport endpointslices when service export becomes conflicted", func() {
			// Confirm that the EndpointSlice has been exported.
			Eventually(func() error {
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
					return fmt.Errorf("endpointSlice Get(%+v), got %v, want no error", endpointSliceKey, err)
				}

				uniqueName, ok := endpointSlice.Annotations[objectmeta.ExportedObjectAnnotationUniqueName]
				if !ok || !strings.HasPrefix(uniqueName, fmt.Sprintf("%s-%s-%s-", memberClusterID, memberUserNS, endpointSliceName)) {
					return fmt.Errorf("endpointSlice unique name, got %s, want prefix %s", uniqueName, fmt.Sprintf("%s-%s-%s-", memberClusterID, memberUserNS, endpointSliceName))
				}

				lastSeenGenerationData, ok := endpointSlice.Annotations[objectmeta.MetricsAnnotationLastSeenGeneration]
				if !ok || lastSeenGenerationData != fmt.Sprintf("%d", endpointSlice.Generation) {
					return fmt.Errorf("lastSeenGenerationData, got %s, want %d", lastSeenGenerationData, endpointSlice.Generation)
				}

				lastSeenTimestampData, ok := endpointSlice.Annotations[objectmeta.MetricsAnnotationLastSeenTimestamp]
				if !ok {
					return fmt.Errorf("lastSeenTimestampData is absent")
				}
				lastSeenTimestamp, err := time.Parse(objectmeta.MetricsLastSeenTimestampFormat, lastSeenTimestampData)
				if err != nil {
					return fmt.Errorf("lastSeenTimestamp Parse(%s), got %v, want no error", lastSeenTimestamp, err)
				}
				trueStartTime = lastSeenTimestamp
				if lastSeenTimestamp.Before(startTime) {
					return fmt.Errorf("lastSeenTimestamp, got %v, want after %v", lastSeenTimestamp, startTime)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Eventually(func() error {
				endpointSliceExportList := &fleetnetv1alpha1.EndpointSliceExportList{}
				if err := hubClient.List(ctx, endpointSliceExportList, &client.ListOptions{Namespace: hubNSForMember}); err != nil {
					return fmt.Errorf("endpointSliceExport List(), got %v, want no error", err)
				}

				if len(endpointSliceExportList.Items) != 1 {
					return fmt.Errorf("endpointSliceExport list length, got %d, want %d", len(endpointSliceExportList.Items), 1)
				}

				endpointSliceRef := endpointSliceExportList.Items[0].Spec.EndpointSliceReference
				wantEndpointSliceRef := fleetnetv1alpha1.FromMetaObjects(
					memberClusterID,
					endpointSlice.TypeMeta,
					endpointSlice.ObjectMeta,
					metav1.NewTime(trueStartTime),
				)
				if diff := cmp.Diff(endpointSliceRef, wantEndpointSliceRef); diff != "" {
					return fmt.Errorf("endpointSliceReference (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Update the status of ServiceExport (valid -> invalid)
			meta.SetStatusCondition(&svcExport.Status.Conditions, serviceExportConflictedCondition(memberUserNS, svcName))
			Expect(memberClient.Status().Update(ctx, svcExport)).Should(Succeed())

			// Confirm that the EndpointSlice has been unexported
			Eventually(func() error {
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
					return fmt.Errorf("endpointSlice Get(%+v), got %v, want no error", endpointSliceKey, err)
				}

				if len(endpointSlice.Annotations) != 0 {
					return fmt.Errorf("endpointSlice annotations, got %v, want empty", endpointSlice.Annotations)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Eventually(endpointSliceIsNotExportedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})
	})
})
