/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package membercluster

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/hubconfig"
)

const (
	memberClusterName = "member-1"

	eventuallyTimeout  = time.Minute * 2
	eventuallyInterval = time.Second * 5
)

var (
	fleetMemberNS = fmt.Sprintf(hubconfig.HubNamespaceNameFormat, "member-1")
)

var (
	endpointSliceImportNames = []string{"test-endpoint-slice-import-1", "test-endpoint-slice-import-2"}
)

var _ = Describe("Test MemberCluster Controller", func() {
	Context("Test MemberCluster controller, handle force delete", func() {
		BeforeEach(func() {
			mc := clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: memberClusterName,
					// finalizer is added to ensure MC is not deleted before the force delete wait time,
					// ideally added and removed by fleet hub member cluster controller.
					Finalizers: []string{"test-member-cluster-cleanup-finalizer"},
				},
				Spec: clusterv1beta1.MemberClusterSpec{
					Identity: rbacv1.Subject{
						Name:      "test-subject",
						Kind:      "ServiceAccount",
						Namespace: "fleet-system",
						APIGroup:  "",
					},
				},
			}
			Expect(hubClient.Create(ctx, &mc)).Should(Succeed())

			// Create the fleet member namespace.
			memberNS := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: fleetMemberNS,
				},
			}
			Expect(hubClient.Create(ctx, &memberNS)).Should(Succeed())

			// Create the EndpointSliceImports.
			for i := range endpointSliceImportNames {
				esi := buildEndpointSliceImport(endpointSliceImportNames[i])
				Expect(hubClient.Create(ctx, esi)).Should(Succeed())
			}
		})

		It("should remove finalizer on EndpointSliceImport on fleet member namespace, after force delete wait time is crossed", func() {
			// ensure EndpointSliceImports have a finalizer.
			var esi fleetnetv1alpha1.EndpointSliceImport
			for i := range endpointSliceImportNames {
				Expect(hubClient.Get(ctx, types.NamespacedName{Name: endpointSliceImportNames[i], Namespace: fleetMemberNS}, &esi)).Should(Succeed())
				Expect(esi.GetFinalizers()).ShouldNot(BeEmpty())
			}
			// delete member cluster to trigger member cluster controller reconcile.
			var mc clusterv1beta1.MemberCluster
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: memberClusterName}, &mc)).Should(Succeed())
			Expect(hubClient.Delete(ctx, &mc)).Should(Succeed())
			// the force delete wait time is set to 1 minute for this IT.
			Eventually(func() error {
				var endpointSliceImportList fleetnetv1alpha1.EndpointSliceImportList
				if err := hubClient.List(ctx, &endpointSliceImportList, client.InNamespace(fleetMemberNS)); err != nil {
					return err
				}
				if len(endpointSliceImportList.Items) != len(endpointSliceImportNames) {
					return fmt.Errorf("length of listed endpointSliceImports %d doesn't match length of endpointSliceImports created %d",
						len(endpointSliceImportList.Items), len(endpointSliceImportNames))
				}
				for i := range endpointSliceImportList.Items {
					esi := &endpointSliceImportList.Items[i]
					if len(esi.GetFinalizers()) != 0 {
						return fmt.Errorf("finalizers on EndpointSliceImport %s/%s have not been removed", esi.Namespace, esi.Name)
					}
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		})

		AfterEach(func() {
			// Delete the namespace, the namespace controller doesn't run in this IT
			// hence it won't be removed.
			ns := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: fleetMemberNS,
				},
			}
			Expect(hubClient.Delete(ctx, &ns)).Should(Succeed())
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: fleetMemberNS}, &ns)).Should(Succeed())
			Expect(ns.DeletionTimestamp != nil).Should(BeTrue())
			// Namespace controller doesn't run in IT, so we manually delete and ensure EndpointSliceImports are deleted.
			for i := range endpointSliceImportNames {
				esi := fleetnetv1alpha1.EndpointSliceImport{
					ObjectMeta: metav1.ObjectMeta{
						Name:      endpointSliceImportNames[i],
						Namespace: fleetMemberNS,
					},
				}
				Expect(hubClient.Delete(ctx, &esi))
			}
			var esi fleetnetv1alpha1.EndpointSliceImport
			Eventually(func() bool {
				for i := range endpointSliceImportNames {
					if !apierrors.IsNotFound(hubClient.Get(ctx, types.NamespacedName{Name: endpointSliceImportNames[i], Namespace: fleetMemberNS}, &esi)) {
						return false
					}
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
			// Remove finalizer from MemberCluster.
			var mc clusterv1beta1.MemberCluster
			Eventually(func() error {
				if err := hubClient.Get(ctx, types.NamespacedName{Name: memberClusterName}, &mc); err != nil {
					return err
				}
				mc.SetFinalizers(nil)
				return hubClient.Update(ctx, &mc)
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
			// Ensure MemberCluster is deleted.
			Eventually(func() bool {
				return apierrors.IsNotFound(hubClient.Get(ctx, types.NamespacedName{Name: memberClusterName}, &mc))
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
		})
	})
})

func buildEndpointSliceImport(name string) *fleetnetv1alpha1.EndpointSliceImport {
	return &fleetnetv1alpha1.EndpointSliceImport{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  fleetMemberNS,
			Finalizers: []string{"networking.fleet.azure.com/endpointsliceimport-cleanup"},
		},
		Spec: fleetnetv1alpha1.EndpointSliceExportSpec{
			AddressType: discoveryv1.AddressTypeIPv4,
			Endpoints: []fleetnetv1alpha1.Endpoint{
				{
					Addresses: []string{"1.2.3.4"},
				},
			},
			EndpointSliceReference: fleetnetv1alpha1.ExportedObjectReference{
				ClusterID:       memberClusterName,
				Kind:            "EndpointSlice",
				Namespace:       fleetMemberNS,
				Name:            "test-endpoint-slice",
				ResourceVersion: "0",
				Generation:      1,
				UID:             "00000000-0000-0000-0000-000000000000",
				ExportedSince:   metav1.NewTime(time.Now().Round(time.Second)),
			},
			OwnerServiceReference: fleetnetv1alpha1.OwnerServiceReference{
				Namespace: "work",
				Name:      "test-service",
			},
		},
	}
}
