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
	endpointSliceKey  = types.NamespacedName{
		Namespace: memberUserNS,
		Name:      endpointSliceName,
	}
)

func managedIPv4EndpointSliceWithoutUniqueNameLabel() *discoveryv1.EndpointSlice {
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

var _ = Describe("endpointslice controller (skip endpointslice)", Serial, func() {
	// consistentlyListActual runs with Consistently assertion to make sure that no EndpointSlice has been
	// exported.
	consistentlyListActual := func() bool {
		endpointSliceExportList := &fleetnetv1alpha1.EndpointSliceExportList{}
		listOption := &client.ListOptions{Namespace: hubNSForMember}
		if err := hubClient.List(ctx, endpointSliceExportList, listOption); err != nil {
			return false
		}

		if len(endpointSliceExportList.Items) > 0 {
			return false
		}
		return true
	}

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
			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
		})

		It("should not export ipv6 endpointslice", func() {
			Consistently(func() bool {
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
					return false
				}

				_, ok := endpointSlice.Labels[endpointSliceUniqueNameLabel]
				return !ok
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())

			Consistently(consistentlyListActual, consistentlyDuration, consistentlyInterval).Should(BeTrue())
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
			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
		})

		It("should not export dangling endpointslice", func() {
			Consistently(func() bool {
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
					return false
				}

				_, ok := endpointSlice.Labels[endpointSliceUniqueNameLabel]
				return !ok
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())

			Consistently(consistentlyListActual, consistentlyDuration, consistentlyInterval).Should(BeTrue())
		})
	})

	Context("endpointslice associated with unexported service", func() {
		var endpointSlice *discoveryv1.EndpointSlice

		BeforeEach(func() {
			endpointSlice = managedIPv4EndpointSliceWithoutUniqueNameLabel()
			Expect(memberClient.Create(ctx, endpointSlice)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, endpointSlice)).Should(Succeed())
		})

		It("should not export endpointslice associated with unexported service", func() {
			Consistently(func() bool {
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
					return false
				}

				_, ok := endpointSlice.Labels[endpointSliceUniqueNameLabel]
				return !ok
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())

			Consistently(consistentlyListActual, consistentlyDuration, consistentlyInterval).Should(BeTrue())
		})
	})

	Context("endpointslice associated with invalid exported service", func() {
		var (
			endpointSlice *discoveryv1.EndpointSlice
			svcExport     *fleetnetv1alpha1.ServiceExport
		)

		BeforeEach(func() {
			endpointSlice = managedIPv4EndpointSliceWithoutUniqueNameLabel()
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
			Expect(memberClient.Delete(ctx, endpointSlice)).Should(Succeed())
			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
		})

		It("should not export endpointslice associated with invalid exported service", func() {
			Consistently(func() bool {
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
					return false
				}

				_, ok := endpointSlice.Labels[endpointSliceUniqueNameLabel]
				return !ok
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())

			Consistently(consistentlyListActual, consistentlyDuration, consistentlyInterval).Should(BeTrue())
		})
	})

	Context("endpointslice associated with conflicted exported service", func() {
		var (
			endpointSlice *discoveryv1.EndpointSlice
			svcExport     *fleetnetv1alpha1.ServiceExport
		)

		BeforeEach(func() {
			endpointSlice = managedIPv4EndpointSliceWithoutUniqueNameLabel()
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
			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
		})

		It("should not export endpointslice associated with invalid exported service", func() {
			Consistently(func() bool {
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
					return false
				}

				_, ok := endpointSlice.Labels[endpointSliceUniqueNameLabel]
				return !ok
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())

			Consistently(consistentlyListActual, consistentlyDuration, consistentlyInterval).Should(BeTrue())
		})
	})
})

var _ = Describe("endpointslice controller (unexport endpointslice)", Serial, func() {
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
	endpointSliceExportKey := types.NamespacedName{
		Namespace: hubNSForMember,
		Name:      endpointSliceUniqueName,
	}

	Context("exported dangling endpointslice (endpointslice with no associated service)", func() {
		var (
			endpointSlice       *discoveryv1.EndpointSlice
			endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport
		)

		BeforeEach(func() {
			endpointSlice = &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
					// Must add the unique name label later; controller may reconcile too quickly for the desired
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
			)
			Expect(hubClient.Create(ctx, endpointSliceExport)).Should(Succeed())

			// Add the unique name label now.
			endpointSlice.Labels = map[string]string{
				endpointSliceUniqueNameLabel: endpointSliceUniqueName,
			}
			Expect(memberClient.Update(ctx, endpointSlice)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, endpointSlice)).Should(Succeed())
		})

		It("should remove exported dangling endpointslice", func() {
			Eventually(func() bool {
				if err := hubClient.Get(ctx, endpointSliceExportKey, endpointSliceExport); !errors.IsNotFound(err) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
			Eventually(func() bool {
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
					return false
				}

				if _, ok := endpointSlice.Labels[endpointSliceUniqueNameLabel]; ok {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("exported endpointslice from unexported service", func() {
		var (
			endpointSlice       *discoveryv1.EndpointSlice
			endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport
		)

		BeforeEach(func() {
			// Must add the unique name label later; controller may reconcile too quickly for the desired
			// test case to happen.
			endpointSlice = managedIPv4EndpointSliceWithoutUniqueNameLabel()

			Expect(memberClient.Create(ctx, endpointSlice)).Should(Succeed())

			endpointSliceExport = endpointSliceExportTemplate.DeepCopy()
			endpointSliceExport.Spec.EndpointSliceReference = fleetnetv1alpha1.FromMetaObjects(
				memberClusterID,
				endpointSlice.TypeMeta,
				endpointSlice.ObjectMeta,
			)
			Expect(hubClient.Create(ctx, endpointSliceExport)).Should(Succeed())

			// Add the unique name label now.
			endpointSlice.Labels = map[string]string{
				endpointSliceUniqueNameLabel: endpointSliceUniqueName,
			}
			Expect(memberClient.Update(ctx, endpointSlice)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, endpointSlice)).Should(Succeed())
		})

		It("should remove exported endpointslice from unexported service", func() {
			Eventually(func() bool {
				if err := hubClient.Get(ctx, endpointSliceExportKey, endpointSliceExport); !errors.IsNotFound(err) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
			Eventually(func() bool {
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
					return false
				}

				if _, ok := endpointSlice.Labels[endpointSliceUniqueNameLabel]; ok {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("exported endpointslice with invalid exported service", func() {
		var (
			endpointSlice       *discoveryv1.EndpointSlice
			endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport
			svcExport           *fleetnetv1alpha1.ServiceExport
		)

		BeforeEach(func() {
			// Must add the unique name label later; controller may reconcile too quickly for the desired
			// test case to happen.
			endpointSlice = managedIPv4EndpointSliceWithoutUniqueNameLabel()
			Expect(memberClient.Create(ctx, endpointSlice)).Should(Succeed())

			endpointSliceExport = endpointSliceExportTemplate.DeepCopy()
			endpointSliceExport.Spec.EndpointSliceReference = fleetnetv1alpha1.FromMetaObjects(
				memberClusterID,
				endpointSlice.TypeMeta,
				endpointSlice.ObjectMeta,
			)
			Expect(hubClient.Create(ctx, endpointSliceExport)).Should(Succeed())

			svcExport = notYetFulfilledServiceExport()
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())
			meta.SetStatusCondition(&svcExport.Status.Conditions, serviceExportInvalidNotFoundCondition(memberUserNS, svcName))
			Expect(memberClient.Status().Update(ctx, svcExport)).Should(Succeed())

			// Add the unique name label now.
			endpointSlice.Labels = map[string]string{
				endpointSliceUniqueNameLabel: endpointSliceUniqueName,
			}
			Expect(memberClient.Update(ctx, endpointSlice)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, endpointSlice)).Should(Succeed())
			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
		})

		It("should remove exported endpointslice from invalid service", func() {
			Eventually(func() bool {
				if err := hubClient.Get(ctx, endpointSliceExportKey, endpointSliceExport); !errors.IsNotFound(err) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
			Eventually(func() bool {
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
					return false
				}

				if _, ok := endpointSlice.Labels[endpointSliceUniqueNameLabel]; ok {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("exported endpointslice with conflicted exported service", func() {
		var (
			endpointSlice       *discoveryv1.EndpointSlice
			endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport
			svcExport           *fleetnetv1alpha1.ServiceExport
		)

		BeforeEach(func() {
			// Must add the unique name label later; controller may reconcile too quickly for the desired
			// test case to happen.
			endpointSlice = managedIPv4EndpointSliceWithoutUniqueNameLabel()
			Expect(memberClient.Create(ctx, endpointSlice)).Should(Succeed())

			endpointSliceExport = endpointSliceExportTemplate.DeepCopy()
			endpointSliceExport.Spec.EndpointSliceReference = fleetnetv1alpha1.FromMetaObjects(
				memberClusterID,
				endpointSlice.TypeMeta,
				endpointSlice.ObjectMeta,
			)
			Expect(hubClient.Create(ctx, endpointSliceExport)).Should(Succeed())

			svcExport = notYetFulfilledServiceExport()
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())
			meta.SetStatusCondition(&svcExport.Status.Conditions, serviceExportConflictedCondition(memberUserNS, svcName))
			Expect(memberClient.Status().Update(ctx, svcExport)).Should(Succeed())

			// Add the unique name label now.
			endpointSlice.Labels = map[string]string{
				endpointSliceUniqueNameLabel: endpointSliceUniqueName,
			}
			Expect(memberClient.Update(ctx, endpointSlice)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, endpointSlice)).Should(Succeed())
			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
		})

		It("should remove exported endpointslice from conflicted exported service", func() {
			Eventually(func() bool {
				if err := hubClient.Get(ctx, endpointSliceExportKey, endpointSliceExport); !errors.IsNotFound(err) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
			Eventually(func() bool {
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
					return false
				}

				if _, ok := endpointSlice.Labels[endpointSliceUniqueNameLabel]; ok {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("exported but deleted endpointslice", func() {
		var (
			endpointSlice       *discoveryv1.EndpointSlice
			endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport
			svcExport           *fleetnetv1alpha1.ServiceExport
		)

		BeforeEach(func() {
			// Must add the unique name label later; controller may reconcile too quickly for the desired
			// test case to happen.
			endpointSlice = managedIPv4EndpointSliceWithoutUniqueNameLabel()
			Expect(memberClient.Create(ctx, endpointSlice)).Should(Succeed())

			endpointSliceExport = endpointSliceExportTemplate.DeepCopy()
			endpointSliceExport.Spec.EndpointSliceReference = fleetnetv1alpha1.FromMetaObjects(
				memberClusterID,
				endpointSlice.TypeMeta,
				endpointSlice.ObjectMeta,
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

			// Remove the finalizer.
			Expect(memberClient.Get(ctx, endpointSliceKey, endpointSlice)).Should(Succeed())
			endpointSlice.ObjectMeta.Finalizers = []string{}
			Expect(memberClient.Update(ctx, endpointSlice)).Should(Succeed())

			Eventually(func() bool {
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); !errors.IsNotFound(err) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		})

		It("should remove exported but deleted endpointslice", func() {
			// Add the unique name label now; a finalizer is also added to prevent premature deletion.
			endpointSlice.Labels = map[string]string{
				endpointSliceUniqueNameLabel: endpointSliceUniqueName,
			}
			endpointSlice.ObjectMeta.Finalizers = []string{"networking.fleet.azure.com/test"}
			Expect(memberClient.Update(ctx, endpointSlice)).Should(Succeed())

			// Set the deletion timestamp.
			Expect(memberClient.Delete(ctx, endpointSlice)).Should(Succeed())

			Eventually(func() bool {
				if err := hubClient.Get(ctx, endpointSliceExportKey, endpointSliceExport); !errors.IsNotFound(err) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})
})

var _ = Describe("endpointslice controller (export endpointslice or update exported endpointslice", Serial, func() {
	Context("new endpointslice for export", func() {
		var (
			endpointSlice *discoveryv1.EndpointSlice
			svcExport     *fleetnetv1alpha1.ServiceExport
		)

		BeforeEach(func() {
			svcExport = notYetFulfilledServiceExport()
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())
			meta.SetStatusCondition(&svcExport.Status.Conditions, serviceExportValidCondition(memberUserNS, svcName))
			meta.SetStatusCondition(&svcExport.Status.Conditions, serviceExportNoConflictCondition(memberUserNS, svcName))
			Expect(memberClient.Status().Update(ctx, svcExport)).Should(Succeed())

			endpointSlice = managedIPv4EndpointSliceWithoutUniqueNameLabel()
			Expect(memberClient.Create(ctx, endpointSlice)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, endpointSlice)).Should(Succeed())
			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
			Expect(hubClient.DeleteAllOf(ctx, &fleetnetv1alpha1.EndpointSliceExport{}, client.InNamespace(hubNSForMember))).Should(Succeed())
		})

		It("should export the new endpointslice", func() {
			Eventually(func() bool {
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
					return false
				}

				uniqueName, ok := endpointSlice.Labels[endpointSliceUniqueNameLabel]
				if !ok {
					return false
				}

				if !strings.HasPrefix(uniqueName, fmt.Sprintf("%s-%s-%s-", memberClusterID, memberUserNS, endpointSliceName)) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Eventually(func() bool {
				endpointSliceExportList := &fleetnetv1alpha1.EndpointSliceExportList{}
				if err := hubClient.List(ctx, endpointSliceExportList, &client.ListOptions{Namespace: hubNSForMember}); err != nil {
					return false
				}

				if len(endpointSliceExportList.Items) != 1 {
					return false
				}

				endpointSliceRef := endpointSliceExportList.Items[0].Spec.EndpointSliceReference
				if endpointSliceRef.Name != endpointSliceName || endpointSliceRef.UID != endpointSlice.UID {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("updated exported endpointslice", func() {
		var (
			endpointSlice *discoveryv1.EndpointSlice
			svcExport     *fleetnetv1alpha1.ServiceExport
		)

		BeforeEach(func() {
			svcExport = notYetFulfilledServiceExport()
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())
			meta.SetStatusCondition(&svcExport.Status.Conditions, serviceExportValidCondition(memberUserNS, svcName))
			meta.SetStatusCondition(&svcExport.Status.Conditions, serviceExportNoConflictCondition(memberUserNS, svcName))
			Expect(memberClient.Status().Update(ctx, svcExport)).Should(Succeed())

			endpointSlice = managedIPv4EndpointSliceWithoutUniqueNameLabel()
			Expect(memberClient.Create(ctx, endpointSlice)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, endpointSlice)).Should(Succeed())
			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
			Expect(hubClient.DeleteAllOf(ctx, &fleetnetv1alpha1.EndpointSliceExport{}, client.InNamespace(hubNSForMember))).Should(Succeed())
		})

		It("should update exported endpointslice", func() {
			// Verify first that the EndpointSlice has been exported.
			Eventually(func() bool {
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
					return false
				}

				uniqueName, ok := endpointSlice.Labels[endpointSliceUniqueNameLabel]
				if !ok {
					return false
				}

				if !strings.HasPrefix(uniqueName, fmt.Sprintf("%s-%s-%s-", memberClusterID, memberUserNS, endpointSliceName)) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			var endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport
			Eventually(func() bool {
				endpointSliceExportList := &fleetnetv1alpha1.EndpointSliceExportList{}
				if err := hubClient.List(ctx, endpointSliceExportList, &client.ListOptions{Namespace: hubNSForMember}); err != nil {
					return false
				}

				if len(endpointSliceExportList.Items) != 1 {
					return false
				}

				endpointSliceExport = &endpointSliceExportList.Items[0]
				endpointSliceRef := endpointSliceExport.Spec.EndpointSliceReference
				if endpointSliceRef.Name != endpointSliceName || endpointSliceRef.UID != endpointSlice.UID {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// Update the EndpointSlice.
			endpointSlice.Endpoints = append(endpointSlice.Endpoints, discoveryv1.Endpoint{
				Addresses: []string{altIPv4Addr},
			})
			Expect(memberClient.Update(ctx, endpointSlice)).Should(Succeed())

			// Confirm that the EndpointSlice has been updated.
			endpointSliceExportKey := types.NamespacedName{
				Namespace: hubNSForMember,
				Name:      endpointSliceExport.Name,
			}
			Eventually(func() bool {
				if err := hubClient.Get(ctx, endpointSliceExportKey, endpointSliceExport); err != nil {
					return false
				}

				expectedEndpoints := []fleetnetv1alpha1.Endpoint{
					{
						Addresses: []string{ipv4Addr},
					},
					{
						Addresses: []string{altIPv4Addr},
					},
				}
				return cmp.Equal(endpointSliceExport.Spec.Endpoints, expectedEndpoints)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("exported endpointslice with tampered invalid unique name label", func() {
		var (
			endpointSlice *discoveryv1.EndpointSlice
			svcExport     *fleetnetv1alpha1.ServiceExport
		)

		BeforeEach(func() {
			svcExport = notYetFulfilledServiceExport()
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())
			meta.SetStatusCondition(&svcExport.Status.Conditions, serviceExportValidCondition(memberUserNS, svcName))
			meta.SetStatusCondition(&svcExport.Status.Conditions, serviceExportNoConflictCondition(memberUserNS, svcName))
			Expect(memberClient.Status().Update(ctx, svcExport)).Should(Succeed())

			endpointSlice = managedIPv4EndpointSliceWithoutUniqueNameLabel()
			Expect(memberClient.Create(ctx, endpointSlice)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, endpointSlice)).Should(Succeed())
			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
			Expect(hubClient.DeleteAllOf(ctx, &fleetnetv1alpha1.EndpointSliceExport{}, client.InNamespace(hubNSForMember))).Should(Succeed())
		})

		It("should export the endpointslice with the invalid label again with a new assigned unique name", func() {
			// Verify first that the EndpointSlice has been exported.
			Eventually(func() bool {
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
					return false
				}

				uniqueName, ok := endpointSlice.Labels[endpointSliceUniqueNameLabel]
				if !ok {
					return false
				}

				if !strings.HasPrefix(uniqueName, fmt.Sprintf("%s-%s-%s-", memberClusterID, memberUserNS, endpointSliceName)) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			var originalEndpointSliceExport *fleetnetv1alpha1.EndpointSliceExport
			Eventually(func() bool {
				endpointSliceExportList := &fleetnetv1alpha1.EndpointSliceExportList{}
				if err := hubClient.List(ctx, endpointSliceExportList, &client.ListOptions{Namespace: hubNSForMember}); err != nil {
					return false
				}

				if len(endpointSliceExportList.Items) != 1 {
					return false
				}

				originalEndpointSliceExport = &endpointSliceExportList.Items[0]
				endpointSliceRef := originalEndpointSliceExport.Spec.EndpointSliceReference
				if endpointSliceRef.Name != endpointSliceName || endpointSliceRef.UID != endpointSlice.UID {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// Tamper with the unique name label.
			endpointSlice.Labels[endpointSliceUniqueNameLabel] = "x_y" // "x_y" is not a valid DNS subdomain.
			Expect(memberClient.Update(ctx, endpointSlice)).Should(Succeed())

			// Confirm that the EndpointSlice has been exported again with a new name.
			Eventually(func() bool {
				endpointSliceExportList := &fleetnetv1alpha1.EndpointSliceExportList{}
				if err := hubClient.List(ctx, endpointSliceExportList, &client.ListOptions{Namespace: hubNSForMember}); err != nil {
					return false
				}

				if len(endpointSliceExportList.Items) != 2 {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Expect(memberClient.Get(ctx, endpointSliceKey, endpointSlice)).Should(Succeed())
			newEndpointSliceExportName := endpointSlice.Labels[endpointSliceUniqueNameLabel]
			Expect(strings.HasPrefix(newEndpointSliceExportName, fmt.Sprintf("%s-%s-%s-", memberClusterID, memberUserNS, endpointSliceName)))
			Expect(newEndpointSliceExportName != originalEndpointSliceExport.Name).Should(BeTrue())

			newEndpointSliceExport := &fleetnetv1alpha1.EndpointSliceExport{}
			newEndpointSliceExportKey := types.NamespacedName{
				Namespace: hubNSForMember,
				Name:      newEndpointSliceExportName,
			}
			Expect(hubClient.Get(ctx, newEndpointSliceExportKey, newEndpointSliceExport)).Should(Succeed())
			endpointSliceExportRef := newEndpointSliceExport.Spec.EndpointSliceReference
			Expect(endpointSliceExportRef.Name == endpointSliceName).Should(BeTrue())
			Expect(endpointSliceExportRef.UID == endpointSlice.UID).Should(BeTrue())
		})
	})

	Context("exported endpointslice with tampered used unique name label", func() {
		var (
			endpointSlice *discoveryv1.EndpointSlice
			svcExport     *fleetnetv1alpha1.ServiceExport
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

			endpointSlice = managedIPv4EndpointSliceWithoutUniqueNameLabel()
			Expect(memberClient.Create(ctx, endpointSlice)).Should(Succeed())

			Expect(hubClient.Create(ctx, altEndpointSliceExport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, endpointSlice)).Should(Succeed())
			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
			Expect(hubClient.DeleteAllOf(ctx, &fleetnetv1alpha1.EndpointSliceExport{}, client.InNamespace(hubNSForMember))).Should(Succeed())
		})

		It("should export the endpointslice with the used label again with a new assigned unique name", func() {
			// Verify first that the EndpointSlice has been exported.
			Eventually(func() bool {
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
					return false
				}

				uniqueName, ok := endpointSlice.Labels[endpointSliceUniqueNameLabel]
				if !ok {
					return false
				}

				if !strings.HasPrefix(uniqueName, fmt.Sprintf("%s-%s-%s-", memberClusterID, memberUserNS, endpointSliceName)) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			var originalEndpointSliceExport *fleetnetv1alpha1.EndpointSliceExport
			Eventually(func() bool {
				endpointSliceExportList := &fleetnetv1alpha1.EndpointSliceExportList{}
				if err := hubClient.List(ctx, endpointSliceExportList, &client.ListOptions{Namespace: hubNSForMember}); err != nil {
					return false
				}

				if len(endpointSliceExportList.Items) != 2 {
					return false
				}

				for idx := range endpointSliceExportList.Items {
					endpointSliceExport := endpointSliceExportList.Items[idx]
					endpointSliceExportRef := endpointSliceExport.Spec.EndpointSliceReference
					if endpointSliceExportRef.Name == endpointSliceName && endpointSliceExportRef.UID == endpointSlice.UID {
						originalEndpointSliceExport = &endpointSliceExport
						break
					}
				}
				return originalEndpointSliceExport != nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// Tamper with the unique name label.
			endpointSlice.Labels[endpointSliceUniqueNameLabel] = endpointSliceUniqueName
			Expect(memberClient.Update(ctx, endpointSlice)).Should(Succeed())

			// Confirm that the EndpointSlice has been exported again with a new name.
			Eventually(func() bool {
				endpointSliceExportList := &fleetnetv1alpha1.EndpointSliceExportList{}
				if err := hubClient.List(ctx, endpointSliceExportList, &client.ListOptions{Namespace: hubNSForMember}); err != nil {
					return false
				}

				if len(endpointSliceExportList.Items) != 3 {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Expect(memberClient.Get(ctx, endpointSliceKey, endpointSlice)).Should(Succeed())
			newEndpointSliceExportName := endpointSlice.Labels[endpointSliceUniqueNameLabel]
			Expect(strings.HasPrefix(newEndpointSliceExportName, fmt.Sprintf("%s-%s-%s-", memberClusterID, memberUserNS, endpointSliceName)))
			Expect(newEndpointSliceExportName != originalEndpointSliceExport.Name).Should(BeTrue())

			newEndpointSliceExport := &fleetnetv1alpha1.EndpointSliceExport{}
			newEndpointSliceExportKey := types.NamespacedName{
				Namespace: hubNSForMember,
				Name:      newEndpointSliceExportName,
			}
			Expect(hubClient.Get(ctx, newEndpointSliceExportKey, newEndpointSliceExport)).Should(Succeed())
			endpointSliceExportRef := newEndpointSliceExport.Spec.EndpointSliceReference
			Expect(endpointSliceExportRef.Name == endpointSliceName).Should(BeTrue())
			Expect(endpointSliceExportRef.UID == endpointSlice.UID).Should(BeTrue())
		})
	})
})

var _ = Describe("endpointslice controller (service export status changes)", Serial, func() {
	Context("endpointslices when service export becomes valid with no conflicts", func() {
		var endpointSlice *discoveryv1.EndpointSlice
		altEndpointSlice := &discoveryv1.EndpointSlice{
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
		altEndpointSliceKey := types.NamespacedName{
			Namespace: memberUserNS,
			Name:      altEndpointSliceName,
		}
		var svcExport *fleetnetv1alpha1.ServiceExport

		BeforeEach(func() {
			endpointSlice = managedIPv4EndpointSliceWithoutUniqueNameLabel()
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
			Expect(memberClient.Delete(ctx, altEndpointSlice)).Should(Succeed())
			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
			Expect(hubClient.DeleteAllOf(ctx, &fleetnetv1alpha1.EndpointSliceExport{}, client.InNamespace(hubNSForMember))).Should(Succeed())
		})

		It("should export endpointslices when service export becomes valid", func() {
			Eventually(func() bool {
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
					return false
				}

				uniqueName, ok := endpointSlice.Labels[endpointSliceUniqueNameLabel]
				if !ok {
					return false
				}

				if !strings.HasPrefix(uniqueName, fmt.Sprintf("%s-%s-%s-", memberClusterID, memberUserNS, endpointSliceName)) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Eventually(func() bool {
				if err := memberClient.Get(ctx, altEndpointSliceKey, altEndpointSlice); err != nil {
					return false
				}

				uniqueName, ok := altEndpointSlice.Labels[endpointSliceUniqueNameLabel]
				if !ok {
					return false
				}

				if !strings.HasPrefix(uniqueName, fmt.Sprintf("%s-%s-%s-", memberClusterID, memberUserNS, altEndpointSliceName)) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Eventually(func() bool {
				endpointSliceExportList := &fleetnetv1alpha1.EndpointSliceExportList{}
				if err := hubClient.List(ctx, endpointSliceExportList, &client.ListOptions{Namespace: hubNSForMember}); err != nil {
					return false
				}

				if len(endpointSliceExportList.Items) != 2 {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("endpointslices when service export becomes invalid", func() {
		var (
			endpointSlice *discoveryv1.EndpointSlice
			svcExport     *fleetnetv1alpha1.ServiceExport
		)

		BeforeEach(func() {
			svcExport = notYetFulfilledServiceExport()
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())
			meta.SetStatusCondition(&svcExport.Status.Conditions, serviceExportValidCondition(memberUserNS, svcName))
			meta.SetStatusCondition(&svcExport.Status.Conditions, serviceExportNoConflictCondition(memberUserNS, svcName))
			Expect(memberClient.Status().Update(ctx, svcExport)).Should(Succeed())

			endpointSlice = managedIPv4EndpointSliceWithoutUniqueNameLabel()
			Expect(memberClient.Create(ctx, endpointSlice)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, endpointSlice)).Should(Succeed())
			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
		})

		It("should unexport endpointslices when service export becomes invalid", func() {
			// Confirm that the EndpointSlice has been exported.
			Eventually(func() bool {
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
					return false
				}

				uniqueName, ok := endpointSlice.Labels[endpointSliceUniqueNameLabel]
				if !ok {
					return false
				}

				if !strings.HasPrefix(uniqueName, fmt.Sprintf("%s-%s-%s-", memberClusterID, memberUserNS, endpointSliceName)) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Eventually(func() bool {
				endpointSliceExportList := &fleetnetv1alpha1.EndpointSliceExportList{}
				if err := hubClient.List(ctx, endpointSliceExportList, &client.ListOptions{Namespace: hubNSForMember}); err != nil {
					return false
				}

				if len(endpointSliceExportList.Items) != 1 {
					return false
				}

				endpointSliceRef := endpointSliceExportList.Items[0].Spec.EndpointSliceReference
				if endpointSliceRef.Name != endpointSliceName || endpointSliceRef.UID != endpointSlice.UID {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// Update the status of ServiceExport (valid -> invalid)
			meta.SetStatusCondition(&svcExport.Status.Conditions, serviceExportInvalidNotFoundCondition(memberUserNS, svcName))
			Expect(memberClient.Status().Update(ctx, svcExport)).Should(Succeed())

			// Confirm that the EndpointSlice has been unexported
			Eventually(func() bool {
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
					return false
				}

				_, ok := endpointSlice.Labels[endpointSliceUniqueNameLabel]
				return !ok
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Eventually(func() bool {
				endpointSliceExportList := &fleetnetv1alpha1.EndpointSliceExportList{}
				if err := hubClient.List(ctx, endpointSliceExportList, &client.ListOptions{Namespace: hubNSForMember}); err != nil {
					return false
				}

				return len(endpointSliceExportList.Items) == 0
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("endpointslices when service export becomes conflicted", func() {
		var (
			endpointSlice *discoveryv1.EndpointSlice
			svcExport     *fleetnetv1alpha1.ServiceExport
		)

		BeforeEach(func() {
			svcExport = notYetFulfilledServiceExport()
			Expect(memberClient.Create(ctx, svcExport)).Should(Succeed())
			meta.SetStatusCondition(&svcExport.Status.Conditions, serviceExportValidCondition(memberUserNS, svcName))
			meta.SetStatusCondition(&svcExport.Status.Conditions, serviceExportNoConflictCondition(memberUserNS, svcName))
			Expect(memberClient.Status().Update(ctx, svcExport)).Should(Succeed())

			endpointSlice = managedIPv4EndpointSliceWithoutUniqueNameLabel()
			Expect(memberClient.Create(ctx, endpointSlice)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(memberClient.Delete(ctx, endpointSlice)).Should(Succeed())
			Expect(memberClient.Delete(ctx, svcExport)).Should(Succeed())
		})

		It("should unexport endpointslices when service export becomes conflicted", func() {
			// Confirm that the EndpointSlice has been exported.
			Eventually(func() bool {
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
					return false
				}

				uniqueName, ok := endpointSlice.Labels[endpointSliceUniqueNameLabel]
				if !ok {
					return false
				}

				if !strings.HasPrefix(uniqueName, fmt.Sprintf("%s-%s-%s-", memberClusterID, memberUserNS, endpointSliceName)) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Eventually(func() bool {
				endpointSliceExportList := &fleetnetv1alpha1.EndpointSliceExportList{}
				if err := hubClient.List(ctx, endpointSliceExportList, &client.ListOptions{Namespace: hubNSForMember}); err != nil {
					return false
				}

				if len(endpointSliceExportList.Items) != 1 {
					return false
				}

				endpointSliceRef := endpointSliceExportList.Items[0].Spec.EndpointSliceReference
				if endpointSliceRef.Name != endpointSliceName || endpointSliceRef.UID != endpointSlice.UID {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// Update the status of ServiceExport (valid -> invalid)
			meta.SetStatusCondition(&svcExport.Status.Conditions, serviceExportConflictedCondition(memberUserNS, svcName))
			Expect(memberClient.Status().Update(ctx, svcExport)).Should(Succeed())

			// Confirm that the EndpointSlice has been unexported
			Eventually(func() bool {
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
					return false
				}

				_, ok := endpointSlice.Labels[endpointSliceUniqueNameLabel]
				return !ok
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Eventually(func() bool {
				endpointSliceExportList := &fleetnetv1alpha1.EndpointSliceExportList{}
				if err := hubClient.List(ctx, endpointSliceExportList, &client.ListOptions{Namespace: hubNSForMember}); err != nil {
					return false
				}

				return len(endpointSliceExportList.Items) == 0
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})
})
