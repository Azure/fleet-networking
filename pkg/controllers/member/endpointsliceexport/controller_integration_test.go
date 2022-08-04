/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package endpointsliceexport

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
)

const (
	MemberClusterID = "bravelion"

	eventuallyTimeout    = time.Second * 10
	eventuallyInterval   = time.Millisecond * 250
	consistentlyDuration = time.Millisecond * 1000
	ConsistentlyInterval = time.Millisecond * 50
)

var _ = Describe("endpointsliceexport controller", func() {
	endpointSlicePort := int32(80)

	Context("dangling endpointsliceexport", func() {
		var danglingEndpointSliceExport *fleetnetv1alpha1.EndpointSliceExport

		BeforeEach(func() {
			danglingEndpointSliceExport = &fleetnetv1alpha1.EndpointSliceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMember,
					Name:      endpointSliceExportName,
				},
				Spec: fleetnetv1alpha1.EndpointSliceExportSpec{
					AddressType: discoveryv1.AddressTypeIPv4,
					Endpoints: []fleetnetv1alpha1.Endpoint{
						{
							Addresses: []string{"1.2.3.4"},
						},
					},
					Ports: []discoveryv1.EndpointPort{
						{
							Port: &endpointSlicePort,
						},
					},
					EndpointSliceReference: fleetnetv1alpha1.ExportedObjectReference{
						ClusterID:       MemberClusterID,
						Kind:            "EndpointSlice",
						Namespace:       memberUserNS,
						Name:            endpointSliceName,
						ResourceVersion: "0",
						Generation:      0,
						UID:             "0",
					},
				},
			}
			Expect(HubClient.Create(ctx, danglingEndpointSliceExport)).Should(Succeed())
		})

		It("should remove dangling endpointsliceexport", func() {
			endpointSliceExportKey := types.NamespacedName{
				Namespace: hubNSForMember,
				Name:      endpointSliceExportName,
			}
			Eventually(func() bool {
				return errors.IsNotFound(HubClient.Get(ctx, endpointSliceExportKey, danglingEndpointSliceExport))
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("unlinked endpointsliceexport", func() {
		var unlinkedEndpointSlice *discoveryv1.EndpointSlice
		var unlinkedEndpointSliceExport *fleetnetv1alpha1.EndpointSliceExport

		BeforeEach(func() {
			unlinkedEndpointSlice = &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
				},
				AddressType: discoveryv1.AddressTypeIPv4,
				Endpoints: []discoveryv1.Endpoint{
					{
						Addresses: []string{"2.3.4.5"},
					},
				},
				Ports: []discoveryv1.EndpointPort{
					{
						Port: &endpointSlicePort,
					},
				},
			}
			Expect(MemberClient.Create(ctx, unlinkedEndpointSlice)).Should(Succeed())

			unlinkedEndpointSliceExport = &fleetnetv1alpha1.EndpointSliceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMember,
					Name:      endpointSliceExportName,
				},
				Spec: fleetnetv1alpha1.EndpointSliceExportSpec{
					AddressType: discoveryv1.AddressTypeIPv4,
					Endpoints: []fleetnetv1alpha1.Endpoint{
						{
							Addresses: []string{"1.2.3.4"},
						},
					},
					Ports: []discoveryv1.EndpointPort{
						{
							Port: &endpointSlicePort,
						},
					},
					EndpointSliceReference: fleetnetv1alpha1.ExportedObjectReference{
						ClusterID:       MemberClusterID,
						Kind:            "EndpointSlice",
						Namespace:       memberUserNS,
						Name:            endpointSliceName,
						ResourceVersion: "1",
						Generation:      1,
						UID:             "1",
					},
				},
			}
			Expect(HubClient.Create(ctx, unlinkedEndpointSliceExport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(MemberClient.Delete(ctx, unlinkedEndpointSlice)).Should(Succeed())
		})

		It("should remove unlinked endpointsliceexport", func() {
			endpointSliceExportKey := types.NamespacedName{
				Namespace: hubNSForMember,
				Name:      endpointSliceExportName,
			}
			Eventually(func() bool {
				return errors.IsNotFound(HubClient.Get(ctx, endpointSliceExportKey, unlinkedEndpointSliceExport))
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})

	Context("linked endpointsliceexport", func() {
		var linkedEndpointSlice *discoveryv1.EndpointSlice
		var linkedEndpointSliceExport *fleetnetv1alpha1.EndpointSliceExport

		BeforeEach(func() {
			linkedEndpointSlice = &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
					Annotations: map[string]string{
						objectmeta.EndpointSliceAnnotationUniqueName: endpointSliceExportName,
					},
				},
				AddressType: discoveryv1.AddressTypeIPv4,
				Endpoints: []discoveryv1.Endpoint{
					{
						Addresses: []string{"2.3.4.5"},
					},
				},
				Ports: []discoveryv1.EndpointPort{
					{
						Port: &endpointSlicePort,
					},
				},
			}
			Expect(MemberClient.Create(ctx, linkedEndpointSlice)).Should(Succeed())

			linkedEndpointSliceExport = &fleetnetv1alpha1.EndpointSliceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMember,
					Name:      endpointSliceExportName,
				},
				Spec: fleetnetv1alpha1.EndpointSliceExportSpec{
					AddressType: discoveryv1.AddressTypeIPv4,
					Endpoints: []fleetnetv1alpha1.Endpoint{
						{
							Addresses: []string{"1.2.3.4"},
						},
					},
					Ports: []discoveryv1.EndpointPort{
						{
							Port: &endpointSlicePort,
						},
					},
					EndpointSliceReference: fleetnetv1alpha1.ExportedObjectReference{
						ClusterID:       MemberClusterID,
						Kind:            "EndpointSlice",
						Namespace:       memberUserNS,
						Name:            endpointSliceName,
						ResourceVersion: "1",
						Generation:      1,
						UID:             "1",
					},
				},
			}
			Expect(HubClient.Create(ctx, linkedEndpointSliceExport)).Should(Succeed())
		})

		AfterEach(func() {
			Expect(HubClient.Delete(ctx, linkedEndpointSliceExport)).Should(Succeed())
			Expect(MemberClient.Delete(ctx, linkedEndpointSlice)).Should(Succeed())
		})

		It("should keep linked endpointsliceexport", func() {
			Consistently(func() error {
				endpointSliceExportKey := types.NamespacedName{
					Namespace: hubNSForMember,
					Name:      endpointSliceExportName,
				}
				return HubClient.Get(ctx, endpointSliceExportKey, linkedEndpointSliceExport)
			}, consistentlyDuration, ConsistentlyInterval).Should(BeNil())
		})
	})
})
