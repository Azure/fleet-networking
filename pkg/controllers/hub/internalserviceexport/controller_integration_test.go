package internalserviceexport

import (
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

var _ = Describe("Test InternalServiceExport Controller", func() {
	const (
		timeout  = time.Second * 10
		duration = time.Second * 10
		interval = time.Millisecond * 250
	)

	var (
		appProtocol        = "app-protocol"
		importServicePorts = []fleetnetv1alpha1.ServicePort{
			{
				Name:        "portA",
				Protocol:    "TCP",
				Port:        8080,
				AppProtocol: &appProtocol,
				TargetPort:  intstr.IntOrString{IntVal: 8080},
			},
			{
				Name:       "portB",
				Protocol:   "TCP",
				Port:       9090,
				TargetPort: intstr.IntOrString{IntVal: 9090},
			},
		}
		internalServiceExportSpec = fleetnetv1alpha1.InternalServiceExportSpec{
			Ports: importServicePorts,
			ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
				ClusterID:       testClusterID,
				Kind:            "Service",
				Namespace:       testNamespace,
				Name:            testServiceName,
				ResourceVersion: "0",
				Generation:      0,
				UID:             "0",
				NamespacedName:  testNamespace + "/" + testServiceName,
			},
		}
		serviceImportKey = types.NamespacedName{
			Namespace: testNamespace,
			Name:      testServiceName,
		}
		options = []cmp.Option{
			cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion"),
			cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime"),
			cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ManagedFields"),
		}
	)

	Context("When creating internalServiceExport", func() {
		var serviceImport fleetnetv1alpha1.ServiceImport
		var internalServiceExportA *fleetnetv1alpha1.InternalServiceExport
		var internalServiceExportB *fleetnetv1alpha1.InternalServiceExport

		BeforeEach(func() {
			internalServiceExportA = &fleetnetv1alpha1.InternalServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testName,
					Namespace: testMemberClusterA,
				},
				Spec: internalServiceExportSpec,
			}
			internalServiceExportB = &fleetnetv1alpha1.InternalServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testNamespace + "-" + testServiceName,
					Namespace: testMemberClusterB,
				},
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Ports: importServicePorts,
					ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
						ClusterID:       "member-2",
						Kind:            "Service",
						Namespace:       testNamespace,
						Name:            testServiceName,
						ResourceVersion: "0",
						Generation:      0,
						UID:             "0",
						NamespacedName:  testNamespace + "/" + testServiceName,
					},
				},
			}
		})

		AfterEach(func() {
			By("Deleting serviceImport if exists")
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &serviceImport))).Should(Succeed())
		})

		It("ServiceImport should be created", func() {
			By("Creating internalServiceExportA")
			Expect(k8sClient.Create(ctx, internalServiceExportA)).Should(Succeed())

			By("Checking serviceImport")
			Eventually(func() error {
				return k8sClient.Get(ctx, serviceImportKey, &serviceImport)
			}, timeout, interval).Should(Succeed())

			By("Checking serviceImport status")
			Consistently(func() string {
				want := fleetnetv1alpha1.ServiceImportStatus{}
				return cmp.Diff(want, serviceImport.Status, options...)
			}, duration, interval).Should(BeEmpty())

			By("Checking internalServiceExport")
			Eventually(func() bool {
				key := types.NamespacedName{Namespace: testMemberClusterA, Name: testName}
				if err := k8sClient.Get(ctx, key, internalServiceExportA); err != nil {
					return false
				}
				return controllerutil.ContainsFinalizer(internalServiceExportA, internalServiceExportFinalizer)
			}, timeout, interval).Should(BeTrue())

			By("Updating serviceImport status (the resolved spec is the same as internalServiceImport)")
			serviceImport.Status = fleetnetv1alpha1.ServiceImportStatus{
				Ports: importServicePorts,
				Clusters: []fleetnetv1alpha1.ClusterStatus{
					{
						Cluster: testClusterID,
					},
				},
				Type: fleetnetv1alpha1.ClusterSetIP,
			}
			Expect(k8sClient.Status().Update(ctx, &serviceImport)).Should(Succeed())

			By("Checking internalServiceExportA status")
			Eventually(func() string {
				want := fleetnetv1alpha1.InternalServiceExportStatus{
					Conditions: []metav1.Condition{
						unconflictedServiceExportConflictCondition(testNamespace, testServiceName),
					},
				}
				key := types.NamespacedName{Namespace: testMemberClusterA, Name: testName}
				if err := k8sClient.Get(ctx, key, internalServiceExportA); err != nil {
					return err.Error()
				}
				return cmp.Diff(want, internalServiceExportA.Status, options...)
			}, timeout, interval).Should(BeEmpty())

			By("Creating internalServiceExportB")
			Expect(k8sClient.Create(ctx, internalServiceExportB)).Should(Succeed())

			By("Checking internalServiceExportB status")
			Eventually(func() string {
				want := fleetnetv1alpha1.InternalServiceExportStatus{
					Conditions: []metav1.Condition{
						unconflictedServiceExportConflictCondition(testNamespace, testServiceName),
					},
				}
				key := types.NamespacedName{Namespace: testMemberClusterB, Name: testName}
				if err := k8sClient.Get(ctx, key, internalServiceExportB); err != nil {
					return err.Error()
				}
				return cmp.Diff(want, internalServiceExportB.Status, options...)
			}, timeout, interval).Should(BeEmpty())

			By("Checking serviceImport status")
			Eventually(func() string {
				want := fleetnetv1alpha1.ServiceImportStatus{
					Ports: importServicePorts,
					Clusters: []fleetnetv1alpha1.ClusterStatus{
						{
							Cluster: testClusterID,
						},
						{
							Cluster: "member-2",
						},
					},
					Type: fleetnetv1alpha1.ClusterSetIP,
				}
				if err := k8sClient.Get(ctx, serviceImportKey, &serviceImport); err != nil {
					return err.Error()
				}
				return cmp.Diff(want, serviceImport.Status, options...)
			}, timeout, interval).Should(BeEmpty())

			By("Deleting internalServiceExportA")
			Expect(k8sClient.Delete(ctx, internalServiceExportA)).Should(Succeed())

			By("Checking internalServiceExportA")
			Eventually(func() bool {
				key := types.NamespacedName{Namespace: testMemberClusterA, Name: testName}
				return errors.IsNotFound(k8sClient.Get(ctx, key, internalServiceExportA))
			}, timeout, interval).Should(BeTrue())

			By("Checking serviceImport status")
			Eventually(func() string {
				want := fleetnetv1alpha1.ServiceImportStatus{
					Ports: importServicePorts,
					Clusters: []fleetnetv1alpha1.ClusterStatus{
						{
							Cluster: "member-2",
						},
					},
					Type: fleetnetv1alpha1.ClusterSetIP,
				}
				if err := k8sClient.Get(ctx, serviceImportKey, &serviceImport); err != nil {
					return err.Error()
				}
				return cmp.Diff(want, serviceImport.Status, options...)
			}, timeout, interval).Should(BeEmpty())

			By("Deleting internalServiceExportB")
			Expect(k8sClient.Delete(ctx, internalServiceExportB)).Should(Succeed())

			By("Checking internalServiceExportB")
			Eventually(func() bool {
				key := types.NamespacedName{Namespace: testMemberClusterB, Name: testName}
				return errors.IsNotFound(k8sClient.Get(ctx, key, internalServiceExportA))
			}, timeout, interval).Should(BeTrue())

			By("Checking serviceImport status")
			Eventually(func() string {
				want := fleetnetv1alpha1.ServiceImportStatus{}
				if err := k8sClient.Get(ctx, serviceImportKey, &serviceImport); err != nil {
					return err.Error()
				}
				return cmp.Diff(want, serviceImport.Status, options...)
			}, timeout, interval).Should(BeEmpty())
		})
	})

	Context("Updating existing internalServiceExport", func() {
		var serviceImport fleetnetv1alpha1.ServiceImport
		var internalServiceExportA *fleetnetv1alpha1.InternalServiceExport

		BeforeEach(func() {
			By("Creating serviceImport")
			serviceImport = fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testNamespace,
				},
			}
			Expect(k8sClient.Create(ctx, &serviceImport)).Should(Succeed())
		})

		AfterEach(func() {
			By("Deleting serviceImport if exists")
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &serviceImport))).Should(Succeed())
		})

		It("ServiceImport has same ports spec as internalServiceExportA", func() {
			By("Updating serviceImport status")
			serviceImport.Status = fleetnetv1alpha1.ServiceImportStatus{
				Ports: importServicePorts,
				Clusters: []fleetnetv1alpha1.ClusterStatus{
					{
						Cluster: "other-cluster",
					},
				},
				Type: fleetnetv1alpha1.ClusterSetIP,
			}
			Expect(k8sClient.Status().Update(ctx, &serviceImport)).Should(Succeed())

			By("Creating internalServiceExport")
			internalServiceExportA = &fleetnetv1alpha1.InternalServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testName,
					Namespace: testMemberClusterA,
				},
				Spec: internalServiceExportSpec,
			}
			Expect(k8sClient.Create(ctx, internalServiceExportA)).Should(Succeed())

			By("Checking serviceImport status")
			Eventually(func() string {
				want := fleetnetv1alpha1.ServiceImportStatus{
					Ports: importServicePorts,
					Clusters: []fleetnetv1alpha1.ClusterStatus{
						{
							Cluster: "other-cluster",
						},
						{
							Cluster: testClusterID,
						},
					},
					Type: fleetnetv1alpha1.ClusterSetIP,
				}
				if err := k8sClient.Get(ctx, serviceImportKey, &serviceImport); err != nil {
					return err.Error()
				}
				return cmp.Diff(want, serviceImport.Status, options...)
			}, timeout, interval).Should(BeEmpty())

			By("Checking internalServiceExportA status")
			Eventually(func() string {
				want := fleetnetv1alpha1.InternalServiceExportStatus{
					Conditions: []metav1.Condition{
						unconflictedServiceExportConflictCondition(testNamespace, testServiceName),
					},
				}
				key := types.NamespacedName{Namespace: testMemberClusterA, Name: testName}
				if err := k8sClient.Get(ctx, key, internalServiceExportA); err != nil {
					return err.Error()
				}
				return cmp.Diff(want, internalServiceExportA.Status, options...)
			}, timeout, interval).Should(BeEmpty())

			By("Deleting internalServiceExportA")
			Expect(k8sClient.Delete(ctx, internalServiceExportA)).Should(Succeed())

			By("Checking internalServiceExportA")
			Eventually(func() bool {
				key := types.NamespacedName{Namespace: testMemberClusterA, Name: testName}
				return errors.IsNotFound(k8sClient.Get(ctx, key, internalServiceExportA))
			}, timeout, interval).Should(BeTrue())

			By("Checking serviceImport status")
			Eventually(func() string {
				want := fleetnetv1alpha1.ServiceImportStatus{
					Ports: importServicePorts,
					Clusters: []fleetnetv1alpha1.ClusterStatus{
						{
							Cluster: "other-cluster",
						},
					},
					Type: fleetnetv1alpha1.ClusterSetIP,
				}
				if err := k8sClient.Get(ctx, serviceImportKey, &serviceImport); err != nil {
					return err.Error()
				}
				return cmp.Diff(want, serviceImport.Status, options...)
			}, timeout, interval).Should(BeEmpty())
		})

		It("ServiceImport has different ports spec as internalServiceExportA", func() {
			By("Updating serviceImport status")
			serviceImport.Status = fleetnetv1alpha1.ServiceImportStatus{
				Ports: []fleetnetv1alpha1.ServicePort{
					{
						Name:        "portA",
						Protocol:    "TCP",
						Port:        8080,
						AppProtocol: &appProtocol,
						TargetPort:  intstr.IntOrString{IntVal: 8080},
					},
				},
				Clusters: []fleetnetv1alpha1.ClusterStatus{
					{
						Cluster: "other-cluster",
					},
				},
				Type: fleetnetv1alpha1.ClusterSetIP,
			}
			serviceImportStatus := serviceImport.Status.DeepCopy()
			Expect(k8sClient.Status().Update(ctx, &serviceImport)).Should(Succeed())

			By("Creating internalServiceExport")
			internalServiceExportA = &fleetnetv1alpha1.InternalServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testName,
					Namespace: testMemberClusterA,
				},
				Spec: internalServiceExportSpec,
			}
			Expect(k8sClient.Create(ctx, internalServiceExportA)).Should(Succeed())

			By("Checking internalServiceExportA status")
			Eventually(func() string {
				want := fleetnetv1alpha1.InternalServiceExportStatus{
					Conditions: []metav1.Condition{
						conflictedServiceExportConflictCondition(testNamespace, testServiceName),
					},
				}
				key := types.NamespacedName{Namespace: testMemberClusterA, Name: testName}
				if err := k8sClient.Get(ctx, key, internalServiceExportA); err != nil {
					return err.Error()
				}
				return cmp.Diff(want, internalServiceExportA.Status, options...)
			}, timeout, interval).Should(BeEmpty())

			By("Checking serviceImport status")
			Consistently(func() string {
				if err := k8sClient.Get(ctx, serviceImportKey, &serviceImport); err != nil {
					return err.Error()
				}
				return cmp.Diff(serviceImportStatus, &serviceImport.Status, options...)
			}, duration, interval).Should(BeEmpty())

			By("Deleting internalServiceExportA")
			Expect(k8sClient.Delete(ctx, internalServiceExportA)).Should(Succeed())

			By("Checking internalServiceExportA")
			Eventually(func() bool {
				key := types.NamespacedName{Namespace: testMemberClusterA, Name: testName}
				return errors.IsNotFound(k8sClient.Get(ctx, key, internalServiceExportA))
			}, timeout, interval).Should(BeTrue())

			By("Checking serviceImport status")
			Consistently(func() string {
				if err := k8sClient.Get(ctx, serviceImportKey, &serviceImport); err != nil {
					return err.Error()
				}
				return cmp.Diff(serviceImportStatus, &serviceImport.Status, options...)
			}, timeout, interval).Should(BeEmpty())
		})
	})
})
