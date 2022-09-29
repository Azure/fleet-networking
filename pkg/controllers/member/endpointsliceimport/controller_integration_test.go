/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package endpointsliceimport

import (
	"fmt"
	"strings"
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
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
)

const (
	multiClusterSvcName = "app"

	eventuallyTimeout    = time.Second * 10
	eventuallyInterval   = time.Millisecond * 250
	consistentlyDuration = time.Millisecond * 1000
	consistentlyInterval = time.Millisecond * 150
)

var (
	endpointSliceKey   = types.NamespacedName{Namespace: fleetSystemNS, Name: endpointSliceImportName}
	multiClusterSvcKey = types.NamespacedName{Namespace: memberUserNS, Name: multiClusterSvcName}
	derivedSvcKey      = types.NamespacedName{Namespace: fleetSystemNS, Name: derivedSvcName}
)

var (
	// endpointSliceImportIsAbsentActual runs with Eventually and Consistently assertion to make sure that
	// the EndpointSliceImport referred by endpointSliceImportKey no longer exists.
	endpointSliceImportIsAbsentActual = func() error {
		endpointSliceImport := &fleetnetv1alpha1.EndpointSliceImport{}
		if err := hubClient.Get(ctx, endpointSliceImportKey, endpointSliceImport); !errors.IsNotFound(err) {
			return fmt.Errorf("endpointSliceImport Get(%+v), got %w, want not found", endpointSliceImportKey, err)
		}
		return nil
	}
	// endpointSliceIsAbsentActual runs with Eventually and Consistently assertion to make sure that
	// the EndpointSlice referred by endpointSliceKey no longer exists.
	endpointSliceIsAbsentActual = func() error {
		endpointSlice := &discoveryv1.EndpointSlice{}
		if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); !errors.IsNotFound(err) {
			return fmt.Errorf("endpointSlice Get(%+v), got %w, want not found", endpointSliceKey, err)
		}
		return nil
	}
	// endpointSliceIsNotImportedActual runs with Eventually and Consistently assertion to make sure that
	// no EndpointSlice has been imported.
	endpointSliceIsNotImportedActual = func() error {
		endpointSliceList := discoveryv1.EndpointSliceList{}
		if err := memberClient.List(ctx, &endpointSliceList, client.InNamespace(fleetSystemNS)); err != nil {
			return fmt.Errorf("endpointSlice List(), got %w, want no error", err)
		}

		if len(endpointSliceList.Items) != 0 {
			return fmt.Errorf("endpointSliceList, got %v, want empty list", endpointSliceList.Items)
		}
		return nil
	}
	// multiClusterServiceIsAbsentActual runs with Eventually and Consistently assertion to make sure that
	// the MultiClusterService referred by multiClusterSvcKey no longer exists.
	multiClusterServiceIsAbsentActual = func() error {
		multiClusterSvc := &fleetnetv1alpha1.MultiClusterService{}
		if err := memberClient.Get(ctx, multiClusterSvcKey, multiClusterSvc); !errors.IsNotFound(err) {
			return fmt.Errorf("multiClusterService Get(%+v), got %w, want not found", multiClusterSvcKey, err)
		}
		return nil
	}
	// derivedServiceIsAbsentActual runs with Eventually and Consistently assertion to make sure that
	// the derived Service referred by derivedSvcKey no longer exists.
	derivedServiceIsAbsentActual = func() error {
		derivedSvc := &corev1.Service{}
		if err := memberClient.Get(ctx, derivedSvcKey, derivedSvc); !errors.IsNotFound(err) {
			return fmt.Errorf("service Get(%+v), got %w, want not found", derivedSvcKey, err)
		}
		return nil
	}
)

// fulfilledMultiClusterSvc returns a MultiClusterService that has been fulfilled, i.e. it has successfully
// imported a Service and created its corresponding derived Service.
func fulfilledMultiClusterSvc() *fleetnetv1alpha1.MultiClusterService {
	return &fleetnetv1alpha1.MultiClusterService{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      multiClusterSvcName,
			Labels: map[string]string{
				objectmeta.MultiClusterServiceLabelDerivedService: derivedSvcName,
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
					Name:        tcpPortName,
					Protocol:    tcpPortProtocol,
					AppProtocol: &tcpPortAppProtocol,
					Port:        tcpPort,
				},
			},
		},
	}
}

// svcDerivedByMultiClusterSvcWithHybridProtocol returns a Service that is derived from a ServiceImport by
// a MultiClusterService with both TCP and UDP ports.
func svcDerivedByMultiClusterSvcWithHybridProtocol() *corev1.Service {
	svc := svcDerivedByMultiClusterSvc()
	svc.Spec.Ports[0] = corev1.ServicePort{
		Name:        udpPortName,
		Protocol:    udpPortProtocol,
		AppProtocol: &udpPortAppProtocol,
		Port:        udpPort,
	}
	return svc
}

var _ = Describe("endpointsliceimport controller", func() {
	Context("deleted endpointsliceimport", func() {
		var (
			endpointSliceImport *fleetnetv1alpha1.EndpointSliceImport
			endpointSlice       *discoveryv1.EndpointSlice
		)

		BeforeEach(func() {
			endpointSlice = importedIPv4EndpointSlice()
			Expect(memberClient.Create(ctx, endpointSlice)).Should(Succeed())

			endpointSliceImport = ipv4EndpointSliceImport()
			endpointSliceImport.Finalizers = []string{endpointSliceImportCleanupFinalizer}
			Expect(hubClient.Create(ctx, endpointSliceImport)).Should(Succeed())
			Expect(hubClient.Delete(ctx, endpointSliceImport)).Should(Succeed())
		})

		It("should unimport endpointslice", func() {
			Eventually(endpointSliceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(endpointSliceImportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})
	})

	Context("no matching multiclusterservice", func() {
		var endpointSliceImport *fleetnetv1alpha1.EndpointSliceImport

		BeforeEach(func() {
			endpointSliceImport = ipv4EndpointSliceImport()
			Expect(hubClient.Create(ctx, endpointSliceImport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, endpointSliceImport)).Should(Succeed())
			// Confirm that the EndpointSliceImport has been deleted; this helps make the test less flaky.
			Eventually(endpointSliceImportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should not import endpointslice", func() {
			Consistently(endpointSliceIsNotImportedActual, consistentlyDuration, consistentlyInterval).Should(BeNil())
		})
	})

	Context("no valid derived service (bad label)", func() {
		var (
			endpointSliceImport *fleetnetv1alpha1.EndpointSliceImport
			multiClusterSvc     *fleetnetv1alpha1.MultiClusterService
		)

		BeforeEach(func() {
			endpointSliceImport = ipv4EndpointSliceImport()
			Expect(hubClient.Create(ctx, endpointSliceImport)).Should(Succeed())

			multiClusterSvc = fulfilledMultiClusterSvc()
			multiClusterSvc.Labels = map[string]string{
				objectmeta.MultiClusterServiceLabelDerivedService: "",
			}
			Expect(memberClient.Create(ctx, multiClusterSvc)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, endpointSliceImport)).Should(Succeed())
			Expect(memberClient.Delete(ctx, multiClusterSvc)).Should(Succeed())

			// Confirm that all created objects have been deleted; this helps make the test less flaky.
			Eventually(endpointSliceImportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(multiClusterServiceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should not import endpointslice", func() {
			Consistently(endpointSliceIsNotImportedActual, consistentlyDuration, consistentlyInterval).Should(BeNil())
		})
	})

	Context("no valid derived service (not exist)", func() {
		var (
			endpointSliceImport *fleetnetv1alpha1.EndpointSliceImport
			multiClusterSvc     *fleetnetv1alpha1.MultiClusterService
		)

		BeforeEach(func() {
			endpointSliceImport = ipv4EndpointSliceImport()
			Expect(hubClient.Create(ctx, endpointSliceImport)).Should(Succeed())

			multiClusterSvc = fulfilledMultiClusterSvc()
			Expect(memberClient.Create(ctx, multiClusterSvc)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, endpointSliceImport)).Should(Succeed())
			Expect(memberClient.Delete(ctx, multiClusterSvc)).Should(Succeed())

			// Confirm that all created objects have been deleted; this helps make the test less flaky.
			Eventually(endpointSliceImportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(multiClusterServiceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should not import endpointslice", func() {
			Consistently(endpointSliceIsNotImportedActual, consistentlyDuration, consistentlyInterval).Should(BeNil())
		})
	})

	Context("no valid derived service (deleted)", func() {
		var (
			endpointSliceImport *fleetnetv1alpha1.EndpointSliceImport
			multiClusterSvc     *fleetnetv1alpha1.MultiClusterService
			derivedSvc          *corev1.Service
		)

		BeforeEach(func() {
			endpointSliceImport = ipv4EndpointSliceImport()
			Expect(hubClient.Create(ctx, endpointSliceImport)).Should(Succeed())

			multiClusterSvc = fulfilledMultiClusterSvc()
			Expect(memberClient.Create(ctx, multiClusterSvc)).Should(Succeed())

			derivedSvc = svcDerivedByMultiClusterSvc()
			// Add a finalizer to prevent premature deletion of derived Services.
			derivedSvc.Finalizers = []string{"networking.fleet.azure.com/test"}
			Expect(memberClient.Create(ctx, derivedSvc)).Should(Succeed())
			Expect(memberClient.Delete(ctx, derivedSvc)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, endpointSliceImport)).Should(Succeed())
			Expect(memberClient.Delete(ctx, multiClusterSvc)).Should(Succeed())

			// Remove the finalizer.
			Eventually(func() error {
				if err := memberClient.Get(ctx, derivedSvcKey, derivedSvc); err != nil {
					return fmt.Errorf("service Get(%+v), got %w, want no error", derivedSvcKey, err)
				}

				derivedSvc.Finalizers = []string{}
				if err := memberClient.Update(ctx, derivedSvc); err != nil {
					return fmt.Errorf("service Update(%+v), got %w, want no error", derivedSvc, err)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Confirm that all created objects have been deleted; this helps make the test less flaky.
			Eventually(endpointSliceImportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(multiClusterServiceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(derivedServiceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should not import endpointslice", func() {
			Consistently(endpointSliceIsNotImportedActual, consistentlyDuration, consistentlyInterval).Should(BeNil())
		})
	})

	Context("import endpointslice", func() {
		var (
			endpointSliceImport *fleetnetv1alpha1.EndpointSliceImport
			multiClusterSvc     *fleetnetv1alpha1.MultiClusterService
			derivedSvc          *corev1.Service
		)

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

			// Confirm that all created objects have been deleted; this helps make the test less flaky.
			Eventually(endpointSliceImportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(multiClusterServiceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(derivedServiceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Make sure that all imported EndpointSlices are removed.
			Eventually(endpointSliceIsNotImportedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should import endpointslice", func() {
			Eventually(func() error {
				if err := hubClient.Get(ctx, endpointSliceImportKey, endpointSliceImport); err != nil {
					return fmt.Errorf("endpointsliceImport Get(%+v), got %w, want no error", endpointSliceImportKey, err)
				}

				if !cmp.Equal(endpointSliceImport.Finalizers, []string{endpointSliceImportCleanupFinalizer}) {
					return fmt.Errorf("endpointSliceImport finalizers, got %v, want %v", endpointSliceImport.Finalizers, []string{endpointSliceImportCleanupFinalizer})
				}

				lastObservedGeneration, ok := endpointSliceImport.Annotations[objectmeta.MetricsAnnotationLastObservedGeneration]
				if !ok || lastObservedGeneration != fmt.Sprintf("%d", endpointSliceImport.Spec.EndpointSliceReference.Generation) {
					return fmt.Errorf("lastObservedGeneration, got %s, want %d",
						lastObservedGeneration, endpointSliceImport.Spec.EndpointSliceReference.Generation)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			endpointSlice := &discoveryv1.EndpointSlice{}
			expectedEndpointSlice := importedIPv4EndpointSlice()
			Eventually(func() error {
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
					return fmt.Errorf("endpointSlice Get(%+v), got %w, want no error", endpointSliceKey, err)
				}

				if endpointSlice.AddressType != discoveryv1.AddressTypeIPv4 {
					return fmt.Errorf("endpointSlice address type, got %v, want %v", endpointSlice.AddressType, discoveryv1.AddressTypeIPv4)
				}

				if diff := cmp.Diff(endpointSlice.Ports, expectedEndpointSlice.Ports); diff != "" {
					return fmt.Errorf("endpointSlice ports (-got, +want): %s", diff)
				}

				if diff := cmp.Diff(endpointSlice.Endpoints, expectedEndpointSlice.Endpoints); diff != "" {
					return fmt.Errorf("endpointSlice endpoints (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})
	})

	// This test is expected to fail in Kubernetes versions earlier than 1.24, as hybrid protocol service support
	// has not yet been enabled by default.
	Context("import endpointslice (hybrid protocol)", func() {
		var (
			endpointSliceImport *fleetnetv1alpha1.EndpointSliceImport
			multiClusterSvc     *fleetnetv1alpha1.MultiClusterService
			derivedSvc          *corev1.Service
		)

		BeforeEach(func() {
			derivedSvc = svcDerivedByMultiClusterSvcWithHybridProtocol()
			err := memberClient.Create(ctx, derivedSvc)
			if errors.IsInvalid(err) && strings.Contains(err.Error(), "may not contain more than 1 protocol") {
				Skip("hybrid protocol service is not supported in current environment")
			}
			Expect(err).To(BeNil())

			multiClusterSvc = fulfilledMultiClusterSvc()
			Expect(memberClient.Create(ctx, multiClusterSvc)).Should(Succeed())

			endpointSliceImport = ipv4EndpointSliceImportWithHybridProtocol()
			Expect(hubClient.Create(ctx, endpointSliceImport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(client.IgnoreNotFound(hubClient.Delete(ctx, endpointSliceImport))).Should(Succeed())
			Expect(client.IgnoreNotFound(memberClient.Delete(ctx, derivedSvc))).Should(Succeed())
			Expect(client.IgnoreNotFound(memberClient.Delete(ctx, multiClusterSvc))).Should(Succeed())
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
			expectedEndpointSlice := importedIPv4EndpointSliceWithHybridProtocol()
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
		var (
			endpointSliceImport *fleetnetv1alpha1.EndpointSliceImport
			endpointSlice       *discoveryv1.EndpointSlice
			multiClusterSvc     *fleetnetv1alpha1.MultiClusterService
			derivedSvc          *corev1.Service
		)

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

			Eventually(func() error {
				if err := hubClient.Get(ctx, endpointSliceImportKey, endpointSliceImport); err != nil {
					return fmt.Errorf("endpointSliceImport Get(%+v), got %w, want no error", endpointSliceImportKey, err)
				}

				endpointSliceImport.Spec.Endpoints[0].Addresses[0] = newAddr
				if err := hubClient.Update(ctx, endpointSliceImport); err != nil {
					return fmt.Errorf("endpointSliceImport Update(%+v), got %w, want no error", endpointSliceImport, err)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		AfterEach(func() {
			Expect(hubClient.Delete(ctx, endpointSliceImport)).Should(Succeed())
			Expect(memberClient.Delete(ctx, derivedSvc)).Should(Succeed())
			Expect(memberClient.Delete(ctx, multiClusterSvc)).Should(Succeed())

			// Confirm that all created objects have been deleted; this helps make the test less flaky.
			Eventually(endpointSliceImportIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(multiClusterServiceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(derivedServiceIsAbsentActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Make sure that all imported EndpointSlices are removed.
			Eventually(endpointSliceIsNotImportedActual, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should update imported endpointslice", func() {
			endpointSlice := &discoveryv1.EndpointSlice{}
			expectedEndpointSlice := importedIPv4EndpointSlice()
			expectedEndpointSlice.Endpoints[0].Addresses[0] = newAddr
			Eventually(func() error {
				if err := memberClient.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
					return fmt.Errorf("endpointSlice Get(%+v), got %w, want no error", endpointSliceKey, err)
				}

				if endpointSlice.AddressType != discoveryv1.AddressTypeIPv4 {
					return fmt.Errorf("endpointSlice address type, got %v, want %v", endpointSlice.AddressType, discoveryv1.AddressTypeIPv4)
				}

				if diff := cmp.Diff(endpointSlice.Ports, expectedEndpointSlice.Ports); diff != "" {
					return fmt.Errorf("endpointSlice ports (-got, +want): %s", diff)
				}

				if diff := cmp.Diff(endpointSlice.Endpoints, expectedEndpointSlice.Endpoints); diff != "" {
					return fmt.Errorf("endpointSlice endpoints (-got, +want): %s", diff)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})
	})
})
