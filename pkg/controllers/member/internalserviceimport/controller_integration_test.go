package internalserviceimport

import (
	"time"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

var _ = Describe("Test InternalServiceImport Controller", func() {
	const (
		timeout  = time.Second * 10
		interval = time.Millisecond * 250
	)

	Context("Dangling internalServiceImport", func() {
		var danglingInternalServiceImport *fleetnetv1alpha1.InternalServiceImport

		BeforeEach(func() {

			By("Creating dangling internalServiceImport")
			danglingInternalServiceImport = &fleetnetv1alpha1.InternalServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testFleetNamespace,
					Name:      testName,
				},
				Spec: fleetnetv1alpha1.InternalServiceImportSpec{
					ServiceImportReference: fleetnetv1alpha1.ExportedObjectReference{
						Namespace: testNamespace,
						Name:      testName,
					},
				},
			}
			Expect(hubClient.Create(ctx, danglingInternalServiceImport)).Should(Succeed())
		})

		It("Should remove dangling internalServiceImport", func() {
			internalServiceImportKey := types.NamespacedName{
				Namespace: testFleetNamespace,
				Name:      testName,
			}
			Eventually(func() bool {
				return errors.IsNotFound(hubClient.Get(ctx, internalServiceImportKey, danglingInternalServiceImport))
			}, timeout, interval).Should(BeTrue())
		})
	})

	Context("Handling internalServiceImport", func() {
		var internalServiceImport *fleetnetv1alpha1.InternalServiceImport
		var serviceImport *fleetnetv1alpha1.ServiceImport

		BeforeEach(func() {
			By("Creating serviceImport")
			serviceImport = &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testNamespace,
					Name:      testName,
				},
			}
			Expect(memberClient.Create(ctx, serviceImport)).Should(Succeed())

			By("Creating internalServiceImport")
			internalServiceImport = &fleetnetv1alpha1.InternalServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testFleetNamespace,
					Name:      testName,
				},
				Spec: fleetnetv1alpha1.InternalServiceImportSpec{
					ServiceImportReference: fleetnetv1alpha1.ExportedObjectReference{
						Namespace: testNamespace,
						Name:      testName,
					},
				},
				Status: fleetnetv1alpha1.ServiceImportStatus{
					Type: fleetnetv1alpha1.ClusterSetIP,
					Clusters: []fleetnetv1alpha1.ClusterStatus{
						{
							Cluster: "member1",
						},
						{
							Cluster: "member2",
						},
					},
					Ports: []fleetnetv1alpha1.ServicePort{
						{
							Name:     "http",
							Port:     8080,
							Protocol: corev1.ProtocolTCP,
						},
						{
							Name:     "https",
							Port:     8843,
							Protocol: corev1.ProtocolTCP,
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, internalServiceImport)).Should(Succeed())
			Expect(hubClient.Status().Update(ctx, internalServiceImport)).Should(Succeed())
		})
		AfterEach(func() {
			By("Deleting internalServiceImport")
			Expect(hubClient.Delete(ctx, internalServiceImport)).Should(Succeed())

			By("Deleting serviceImport")
			Expect(memberClient.Delete(ctx, serviceImport)).Should(Succeed())
		})

		It("Reporting back serviceImport status from the fleet to the member cluster", func() {
			By("Checking serviceImport status")
			serviceImportKey := types.NamespacedName{
				Namespace: testNamespace,
				Name:      testName,
			}
			Eventually(func() bool {
				if err := memberClient.Get(ctx, serviceImportKey, serviceImport); err != nil {
					return false
				}
				return cmp.Equal(internalServiceImport.Status, serviceImport.Status)
			}, timeout, interval).Should(BeTrue())

			By("Updating internalServiceImport status")
			internalServiceImport.Status.Type = fleetnetv1alpha1.Headless
			Expect(hubClient.Status().Update(ctx, internalServiceImport)).Should(Succeed())

			By("Checking serviceImport status")
			Eventually(func() bool {
				if err := memberClient.Get(ctx, serviceImportKey, serviceImport); err != nil {
					return false
				}
				return cmp.Equal(internalServiceImport.Status, serviceImport.Status)
			}, timeout, interval).Should(BeTrue())
		})
	})
})
