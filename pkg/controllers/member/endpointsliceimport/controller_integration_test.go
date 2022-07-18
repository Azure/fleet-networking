/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package endpointsliceimport

import (
	"time"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

const (
	multiClusterSvcName = "app"

	eventuallyTimeout    = time.Second * 10
	eventuallyInterval   = time.Millisecond * 250
	consistentlyDuration = time.Millisecond * 1000
	consistentlyInterval = time.Millisecond * 150
)

var endpointSliceImportKey = types.NamespacedName{
	Namespace: hubNSForMember,
	Name:      endpointSliceImportName,
}
var endpointSliceKey = types.NamespacedName{
	Namespace: fleetSystemNS,
	Name:      endpointSliceImportName,
}

// fulfilledMultiClusterSvc returns a MultiClusterService that has been fulfilled, i.e. it has successfully
// imported a Service and created its corresponding derived Service.
func fulfilledMultiClusterSvc() *fleetnetv1alpha1.MultiClusterService {
	return &fleetnetv1alpha1.MultiClusterService{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      multiClusterSvcName,
			Labels: map[string]string{
				derivedServiceLabel: derivedSvcName,
			},
		},
		Spec: fleetnetv1alpha1.MultiClusterServiceSpec{
			ServiceImport: fleetnetv1alpha1.ServiceImportRef{
				Name: svcName,
			},
		},
	}
}

// svcDerivedByMultiClusterSvc returns a Service that is derived from a ServiceImport by a MultiClusterService.
func svcDerivedByMultiClusterSvc() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: fleetSystemNS,
			Name:      derivedSvcName,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{
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
		},
	}
}

var _ = Describe("endpointsliceimport controller", func() {
	Context("deleted endpointsliceimport", func() {
		var deletedEndpointSliceImport *fleetnetv1alpha1.EndpointSliceImport
		var importedEndpointSlice *discoveryv1.EndpointSlice

		BeforeEach(func() {
			importedEndpointSlice = importedIPv4EndpointSlice()
			Expect(memberClient.Create(ctx, importedEndpointSlice)).Should(Succeed())

			deletedEndpointSliceImport = ipv4EndpointSliceImport()
			deletedEndpointSliceImport.Finalizers = []string{endpointSliceImportCleanupFinalizer}
			Expect(hubClient.Create(ctx, deletedEndpointSliceImport)).Should(Succeed())
			Expect(hubClient.Delete(ctx, deletedEndpointSliceImport)).Should(Succeed())
		})

		It("should unimport endpointslice", func() {
			Eventually(func() bool {
				return errors.IsNotFound(memberClient.Get(ctx, endpointSliceKey, importedEndpointSlice))
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			Eventually(func() bool {
				return errors.IsNotFound(hubClient.Get(ctx, endpointSliceImportKey, deletedEndpointSliceImport))
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("no matching multiclusterservice", func() {
		var phantomEndpointSliceImport *fleetnetv1alpha1.EndpointSliceImport

		BeforeEach(func() {
			phantomEndpointSliceImport = ipv4EndpointSliceImport()
			Expect(hubClient.Create(ctx, phantomEndpointSliceImport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, phantomEndpointSliceImport)).Should(Succeed())
		})

		It("should not import endpointslice", func() {
			Consistently(func() bool {
				endpointSliceList := discoveryv1.EndpointSliceList{}
				if err := memberClient.List(ctx, &endpointSliceList, client.InNamespace(fleetSystemNS)); err != nil {
					return false
				}

				return len(endpointSliceList.Items) == 0
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())
		})
	})

	Context("no valid derived service (bad label)", func() {
		var phantomEndpointSliceImport *fleetnetv1alpha1.EndpointSliceImport
		var multiClusterSvc *fleetnetv1alpha1.MultiClusterService

		BeforeEach(func() {
			phantomEndpointSliceImport = ipv4EndpointSliceImport()
			Expect(hubClient.Create(ctx, phantomEndpointSliceImport)).Should(Succeed())

			multiClusterSvc = fulfilledMultiClusterSvc()
			multiClusterSvc.Labels = map[string]string{
				derivedServiceLabel: "",
			}
			Expect(memberClient.Create(ctx, multiClusterSvc)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, phantomEndpointSliceImport)).Should(Succeed())
			Expect(memberClient.Delete(ctx, multiClusterSvc)).Should(Succeed())
		})

		It("should not import endpointslice", func() {
			Consistently(func() bool {
				endpointSliceList := discoveryv1.EndpointSliceList{}
				if err := memberClient.List(ctx, &endpointSliceList, client.InNamespace(fleetSystemNS)); err != nil {
					return false
				}

				return len(endpointSliceList.Items) == 0
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())
		})
	})

	Context("no valid derived service (not exist)", func() {
		var phantomEndpointSliceImport *fleetnetv1alpha1.EndpointSliceImport
		var multiClusterSvc *fleetnetv1alpha1.MultiClusterService

		BeforeEach(func() {
			phantomEndpointSliceImport = ipv4EndpointSliceImport()
			Expect(hubClient.Create(ctx, phantomEndpointSliceImport)).Should(Succeed())

			multiClusterSvc = fulfilledMultiClusterSvc()
			Expect(memberClient.Create(ctx, multiClusterSvc)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, phantomEndpointSliceImport)).Should(Succeed())
			Expect(memberClient.Delete(ctx, multiClusterSvc)).Should(Succeed())
		})

		It("should not import endpointslice", func() {
			Consistently(func() bool {
				endpointSliceList := discoveryv1.EndpointSliceList{}
				if err := memberClient.List(ctx, &endpointSliceList, client.InNamespace(fleetSystemNS)); err != nil {
					return false
				}

				return len(endpointSliceList.Items) == 0
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())
		})
	})

	Context("no valid derived service (deleted)", func() {
		var phantomEndpointSliceImport *fleetnetv1alpha1.EndpointSliceImport
		var multiClusterSvc *fleetnetv1alpha1.MultiClusterService
		var derivedSvc *corev1.Service

		BeforeEach(func() {
			phantomEndpointSliceImport = ipv4EndpointSliceImport()
			Expect(hubClient.Create(ctx, phantomEndpointSliceImport)).Should(Succeed())

			multiClusterSvc = fulfilledMultiClusterSvc()
			Expect(memberClient.Create(ctx, multiClusterSvc)).Should(Succeed())

			derivedSvc = svcDerivedByMultiClusterSvc()
			// Add a finalizer to prevent premature deletion of derived Services.
			derivedSvc.Finalizers = []string{"networking.fleet.azure.com/test"}
			Expect(memberClient.Create(ctx, derivedSvc)).Should(Succeed())
			Expect(memberClient.Delete(ctx, derivedSvc)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, phantomEndpointSliceImport)).Should(Succeed())
			Expect(memberClient.Delete(ctx, multiClusterSvc)).Should(Succeed())

			// Remove the finalizer.
			derivedSvcKey := types.NamespacedName{
				Namespace: fleetSystemNS,
				Name:      derivedSvcName,
			}
			Expect(memberClient.Get(ctx, derivedSvcKey, derivedSvc)).Should(Succeed())
			derivedSvc.Finalizers = []string{}
			Expect(memberClient.Update(ctx, derivedSvc)).Should(Succeed())
		})

		It("should not import endpointslice", func() {
			Consistently(func() bool {
				endpointSliceList := discoveryv1.EndpointSliceList{}
				if err := memberClient.List(ctx, &endpointSliceList, client.InNamespace(fleetSystemNS)); err != nil {
					return false
				}

				return len(endpointSliceList.Items) == 0
			}, consistentlyDuration, consistentlyInterval).Should(BeTrue())
		})
	})

	Context("import endpointslice", func() {
		var endpointSliceImport *fleetnetv1alpha1.EndpointSliceImport
		var multiClusterSvc *fleetnetv1alpha1.MultiClusterService
		var derivedSvc *corev1.Service

		BeforeEach(func() {
			multiClusterSvc = fulfilledMultiClusterSvc()
			Expect(memberClient.Create(ctx, multiClusterSvc)).Should(Succeed())

			derivedSvc = svcDerivedByMultiClusterSvc()
			Expect(memberClient.Create(ctx, derivedSvc)).Should(Succeed())

			endpointSliceImport = ipv4EndpointSliceImport()
			Expect(hubClient.Create(ctx, endpointSliceImport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, endpointSliceImport)).Should(Succeed())
			Expect(memberClient.Delete(ctx, derivedSvc)).Should(Succeed())
			Expect(memberClient.Delete(ctx, multiClusterSvc)).Should(Succeed())
			// Make sure that the imported EndpointSlice is removed.
			Eventually(func() bool {
				endpointSlice := &discoveryv1.EndpointSlice{}
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); errors.IsNotFound(err) {
					return true
				}
				return false
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})

		It("should import endpointslice", func() {
			Eventually(func() bool {
				if err := hubClient.Get(ctx, endpointSliceImportKey, endpointSliceImport); err != nil {
					return false
				}

				return cmp.Equal(endpointSliceImport.Finalizers, []string{endpointSliceImportCleanupFinalizer})
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			endpointSlice := &discoveryv1.EndpointSlice{}
			expectedEndpointSlice := importedIPv4EndpointSlice()
			Eventually(func() bool {
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
					return false
				}

				if endpointSlice.AddressType != discoveryv1.AddressTypeIPv4 {
					return false
				}

				if !cmp.Equal(endpointSlice.Ports, expectedEndpointSlice.Ports) {
					return false
				}

				return cmp.Equal(endpointSlice.Endpoints, expectedEndpointSlice.Endpoints)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("update imported endpointslice", func() {
		var endpointSliceImport *fleetnetv1alpha1.EndpointSliceImport
		var endpointSlice *discoveryv1.EndpointSlice
		var multiClusterSvc *fleetnetv1alpha1.MultiClusterService
		var derivedSvc *corev1.Service

		newAddr := "3.4.5.6"

		BeforeEach(func() {
			endpointSlice = importedIPv4EndpointSlice()
			Expect(memberClient.Create(ctx, endpointSlice)).Should(Succeed())

			multiClusterSvc = fulfilledMultiClusterSvc()
			Expect(memberClient.Create(ctx, multiClusterSvc)).Should(Succeed())

			derivedSvc = svcDerivedByMultiClusterSvc()
			Expect(memberClient.Create(ctx, derivedSvc)).Should(Succeed())

			endpointSliceImport = ipv4EndpointSliceImport()
			endpointSliceImport.Finalizers = []string{endpointSliceImportCleanupFinalizer}
			Expect(hubClient.Create(ctx, endpointSliceImport)).Should(Succeed())

			endpointSliceImport.Spec.Endpoints[0].Addresses[0] = newAddr
			Expect(hubClient.Update(ctx, endpointSliceImport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, endpointSliceImport)).Should(Succeed())
			Expect(memberClient.Delete(ctx, derivedSvc)).Should(Succeed())
			Expect(memberClient.Delete(ctx, multiClusterSvc)).Should(Succeed())
			// Make sure that the imported EndpointSlice is removed.
			Eventually(func() bool {
				endpointSlice := &discoveryv1.EndpointSlice{}
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); errors.IsNotFound(err) {
					return true
				}
				return false
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})

		It("should update imported endpointslice", func() {
			endpointSlice := &discoveryv1.EndpointSlice{}
			expectedEndpointSlice := importedIPv4EndpointSlice()
			expectedEndpointSlice.Endpoints[0].Addresses[0] = newAddr
			Eventually(func() bool {
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
					return false
				}

				if endpointSlice.AddressType != discoveryv1.AddressTypeIPv4 {
					return false
				}

				if !cmp.Equal(endpointSlice.Ports, expectedEndpointSlice.Ports) {
					return false
				}

				return cmp.Equal(endpointSlice.Endpoints, expectedEndpointSlice.Endpoints)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})
})
