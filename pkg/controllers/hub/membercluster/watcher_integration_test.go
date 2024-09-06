package membercluster

import (
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	memberClusterName       = "member-1"
	fleetMemberNS           = "fleet-member-member-1"
	endpointSliceImportName = "test-endpoint-slice-import"

	eventuallyTimeout  = time.Minute * 2
	eventuallyInterval = time.Second * 5
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

			// Create the EndpointSliceImport.
			esi := &fleetnetv1alpha1.EndpointSliceImport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  fleetMemberNS,
					Name:       endpointSliceImportName,
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
			Expect(hubClient.Create(ctx, esi)).Should(Succeed())
		})

		It("should remove finalizer on EndpointSliceImport on fleet member namespace, after force delete wait time is crossed", func() {
			// ensure EndpointSliceImport has finalizer.
			var esi fleetnetv1alpha1.EndpointSliceImport
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: endpointSliceImportName, Namespace: fleetMemberNS}, &esi)).Should(Succeed())
			Expect(esi.GetFinalizers()).ShouldNot(BeEmpty())
			// delete member cluster to trigger MC watcher reconcile.
			var mc clusterv1beta1.MemberCluster
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: memberClusterName}, &mc)).Should(Succeed())
			Expect(hubClient.Delete(ctx, &mc)).Should(Succeed())
			// the force delete wait time is set to 1 minute for this IT.
			Eventually(func() error {
				var esi fleetnetv1alpha1.EndpointSliceImport
				if err := hubClient.Get(ctx, types.NamespacedName{Name: endpointSliceImportName, Namespace: fleetMemberNS}, &esi); err != nil {
					return err
				}
				if len(esi.GetFinalizers()) != 0 {
					return errors.New("finalizers on EndpointSliceImport have not been removed")
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		})
	})
})
