package serviceimport

import (
	"fmt"
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
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
)

func unconflictedServiceExportConflictCondition(svcNamespace string, svcName string) metav1.Condition {
	return metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportConflict),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: 0,
		LastTransitionTime: metav1.Now(),
		Reason:             "NoConflictFound",
		Message:            fmt.Sprintf("service %s/%s is exported without conflict", svcNamespace, svcName),
	}
}

func conflictedServiceExportConflictCondition(svcNamespace string, svcName string) metav1.Condition {
	return metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportConflict),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: 0,
		LastTransitionTime: metav1.Now(),
		Reason:             "ConflictFound",
		Message:            fmt.Sprintf("service %s/%s is in conflict with other exported services", svcNamespace, svcName),
	}
}

func deleteInternalServiceExport(internalServiceExport *fleetnetv1alpha1.InternalServiceExport) error {
	key := types.NamespacedName{
		Namespace: internalServiceExport.GetNamespace(),
		Name:      internalServiceExport.GetName(),
	}
	if err := k8sClient.Get(ctx, key, internalServiceExport); err != nil {
		return err
	}
	controllerutil.RemoveFinalizer(internalServiceExport, objectmeta.InternalServiceExportFinalizer)
	if err := k8sClient.Update(ctx, internalServiceExport); err != nil {
		return err
	}
	return k8sClient.Delete(ctx, internalServiceExport)
}

var _ = Describe("Test ServiceImport Controller", func() {
	const (
		timeout  = time.Second * 10
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

	Context("ServiceImport has empty ports spec", func() {
		var serviceImport *fleetnetv1alpha1.ServiceImport
		var internalServiceExportA *fleetnetv1alpha1.InternalServiceExport
		var internalServiceExportB *fleetnetv1alpha1.InternalServiceExport
		var internalServiceExportC *fleetnetv1alpha1.InternalServiceExport
		var internalServiceExportAA *fleetnetv1alpha1.InternalServiceExport

		BeforeEach(func() {
			internalServiceExportA = &fleetnetv1alpha1.InternalServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testNamespace + "-" + testServiceName,
					Namespace: testMemberClusterA,
				},
				Spec: internalServiceExportSpec,
			}
			controllerutil.AddFinalizer(internalServiceExportA, objectmeta.InternalServiceExportFinalizer)
			internalServiceExportB = &fleetnetv1alpha1.InternalServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testNamespace + "-" + testServiceName,
					Namespace: testMemberClusterB,
				},
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Ports: []fleetnetv1alpha1.ServicePort{
						{
							Name:        "portA",
							Protocol:    "TCP",
							Port:        8080,
							AppProtocol: &appProtocol,
							TargetPort:  intstr.IntOrString{IntVal: 8080},
						},
					},
					ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
						ClusterID:       "member-cluster-b",
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
			controllerutil.AddFinalizer(internalServiceExportB, objectmeta.InternalServiceExportFinalizer)
			internalServiceExportC = &fleetnetv1alpha1.InternalServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testNamespace + "-othersvc",
					Namespace: testMemberClusterA,
				},
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Ports: importServicePorts,
					ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
						ClusterID:       "member-cluster-c",
						Kind:            "Service",
						Namespace:       testNamespace,
						Name:            "othersvc",
						ResourceVersion: "0",
						Generation:      0,
						UID:             "0",
						NamespacedName:  testNamespace + "/" + "othersvc",
					},
				},
			}
			controllerutil.AddFinalizer(internalServiceExportC, objectmeta.InternalServiceExportFinalizer)
			internalServiceExportAA = &fleetnetv1alpha1.InternalServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testNamespace + "-" + testServiceName,
					Namespace: testMemberClusterAA,
				},
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Ports: importServicePorts,
					ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
						ClusterID:       "member-cluster-aa",
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
			controllerutil.AddFinalizer(internalServiceExportAA, objectmeta.InternalServiceExportFinalizer)
		})

		AfterEach(func() {
			By("Deleting serviceImport if exists")
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, serviceImport))).Should(Succeed())

			By("Deleting internalServiceExportA if exists")
			Eventually(func() error {
				return client.IgnoreNotFound(deleteInternalServiceExport(internalServiceExportA))
			}, timeout, interval).Should(Succeed())

			By("Deleting internalServiceExportB if exists")
			Eventually(func() error {
				return client.IgnoreNotFound(deleteInternalServiceExport(internalServiceExportB))
			}, timeout, interval).Should(Succeed())

			By("Deleting internalServiceExportC if exists")
			Eventually(func() error {
				return client.IgnoreNotFound(deleteInternalServiceExport(internalServiceExportC))
			}, timeout, interval).Should(Succeed())

			By("Deleting internalServiceExportAA if exists")
			Eventually(func() error {
				return client.IgnoreNotFound(deleteInternalServiceExport(internalServiceExportAA))
			}, timeout, interval).Should(Succeed())
		})

		It("There are no internalServiceExports and serviceImport should be deleted", func() {
			By("Creating serviceImport")
			serviceImport = &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testNamespace,
				},
			}
			Expect(k8sClient.Create(ctx, serviceImport)).Should(Succeed())

			By("Checking serviceImport")
			Eventually(func() bool {
				return errors.IsNotFound(k8sClient.Get(ctx, serviceImportKey, serviceImport))
			}, timeout, interval).Should(BeTrue())
		})

		It("InternalServiceExports are just created and have no status", func() {
			By("Creating internalServiceExportA")
			Expect(k8sClient.Create(ctx, internalServiceExportA)).Should(Succeed())

			By("Creating internalServiceExportB")
			Expect(k8sClient.Create(ctx, internalServiceExportB)).Should(Succeed())

			By("Creating internalServiceExportC")
			Expect(k8sClient.Create(ctx, internalServiceExportC)).Should(Succeed())

			By("Creating internalServiceExportAA")
			Expect(k8sClient.Create(ctx, internalServiceExportAA)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, internalServiceExportAA)).Should(Succeed())

			By("Creating serviceImport")
			serviceImport = &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testNamespace,
				},
			}
			Expect(k8sClient.Create(ctx, serviceImport)).Should(Succeed())

			By("Checking serviceImport")
			resolvedClusterID := testClusterID
			Eventually(func() string {
				if err := k8sClient.Get(ctx, serviceImportKey, serviceImport); err != nil {
					return err.Error()
				}
				want := fleetnetv1alpha1.ServiceImportStatus{
					Clusters: []fleetnetv1alpha1.ClusterStatus{
						{
							Cluster: testClusterID,
						},
					},
					Type:  fleetnetv1alpha1.ClusterSetIP,
					Ports: internalServiceExportA.Spec.Ports,
				}
				if len(serviceImport.Status.Clusters) != 1 {
					return fmt.Sprintf("got %v cluster, want 1", len(serviceImport.Status.Clusters))
				}
				resolvedClusterID = serviceImport.Status.Clusters[0].Cluster
				if resolvedClusterID != testClusterID {
					want = fleetnetv1alpha1.ServiceImportStatus{
						Clusters: []fleetnetv1alpha1.ClusterStatus{
							{
								Cluster: "member-cluster-b",
							},
						},
						Type:  fleetnetv1alpha1.ClusterSetIP,
						Ports: internalServiceExportB.Spec.Ports,
					}
				}
				return cmp.Diff(want, serviceImport.Status, options...)
			}, timeout, interval).Should(BeEmpty())

			By("Checking internalServiceExportA condition")
			Eventually(func() string {
				key := types.NamespacedName{
					Namespace: internalServiceExportA.GetNamespace(),
					Name:      internalServiceExportA.GetName(),
				}
				var got fleetnetv1alpha1.InternalServiceExport
				if err := k8sClient.Get(ctx, key, &got); err != nil {
					return err.Error()
				}
				want := fleetnetv1alpha1.InternalServiceExport{
					Spec:       internalServiceExportSpec,
					ObjectMeta: internalServiceExportA.ObjectMeta,
					Status: fleetnetv1alpha1.InternalServiceExportStatus{
						Conditions: []metav1.Condition{
							unconflictedServiceExportConflictCondition(testNamespace, testServiceName),
						},
					},
				}
				if resolvedClusterID != testClusterID {
					want.Status.Conditions[0] = conflictedServiceExportConflictCondition(testNamespace, testServiceName)
				}
				return cmp.Diff(want, got, options...)
			}, timeout, interval).Should(BeEmpty())

			By("Checking internalServiceExportB condition")
			Eventually(func() string {
				key := types.NamespacedName{
					Namespace: internalServiceExportB.GetNamespace(),
					Name:      internalServiceExportB.GetName(),
				}
				var got fleetnetv1alpha1.InternalServiceExport
				if err := k8sClient.Get(ctx, key, &got); err != nil {
					return err.Error()
				}
				want := fleetnetv1alpha1.InternalServiceExport{
					Spec: fleetnetv1alpha1.InternalServiceExportSpec{
						Ports: []fleetnetv1alpha1.ServicePort{
							{
								Name:        "portA",
								Protocol:    "TCP",
								Port:        8080,
								AppProtocol: &appProtocol,
								TargetPort:  intstr.IntOrString{IntVal: 8080},
							},
						},
						ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
							ClusterID:       "member-cluster-b",
							Kind:            "Service",
							Namespace:       testNamespace,
							Name:            testServiceName,
							ResourceVersion: "0",
							Generation:      0,
							UID:             "0",
							NamespacedName:  testNamespace + "/" + testServiceName,
						},
					},
					ObjectMeta: internalServiceExportB.ObjectMeta,
					Status: fleetnetv1alpha1.InternalServiceExportStatus{
						Conditions: []metav1.Condition{
							conflictedServiceExportConflictCondition(testNamespace, testServiceName),
						},
					},
				}
				if resolvedClusterID != testClusterID {
					want.Status.Conditions[0] = unconflictedServiceExportConflictCondition(testNamespace, testServiceName)
				}
				return cmp.Diff(want, got, options...)
			}, timeout, interval).Should(BeEmpty())

			By("Checking internalServiceExportC condition and should no change")
			Eventually(func() string {
				key := types.NamespacedName{
					Namespace: internalServiceExportC.GetNamespace(),
					Name:      internalServiceExportC.GetName(),
				}
				var got fleetnetv1alpha1.InternalServiceExport
				if err := k8sClient.Get(ctx, key, &got); err != nil {
					return err.Error()
				}
				want := fleetnetv1alpha1.InternalServiceExport{
					ObjectMeta: internalServiceExportC.ObjectMeta,
					Spec: fleetnetv1alpha1.InternalServiceExportSpec{
						Ports: importServicePorts,
						ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
							ClusterID:       "member-cluster-c",
							Kind:            "Service",
							Namespace:       testNamespace,
							Name:            "othersvc",
							ResourceVersion: "0",
							Generation:      0,
							UID:             "0",
							NamespacedName:  testNamespace + "/" + "othersvc",
						},
					},
				}
				return cmp.Diff(want, got, options...)
			}, timeout, interval).Should(BeEmpty())
		})

		It("InternalServiceExport ports spec is updated", func() {
			By("Creating internalServiceExportA")
			Expect(k8sClient.Create(ctx, internalServiceExportA)).Should(Succeed())

			internalServiceExportA.Status = fleetnetv1alpha1.InternalServiceExportStatus{
				Conditions: []metav1.Condition{
					conflictedServiceExportConflictCondition(testNamespace, testServiceName),
				},
			}
			Expect(k8sClient.Status().Update(ctx, internalServiceExportA))

			By("Creating serviceImport")
			serviceImport = &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testNamespace,
				},
			}
			Expect(k8sClient.Create(ctx, serviceImport)).Should(Succeed())

			By("Checking serviceImport")
			Eventually(func() string {
				if err := k8sClient.Get(ctx, serviceImportKey, serviceImport); err != nil {
					return err.Error()
				}
				want := fleetnetv1alpha1.ServiceImportStatus{
					Ports: internalServiceExportA.Spec.Ports,
					Clusters: []fleetnetv1alpha1.ClusterStatus{
						{
							Cluster: testClusterID,
						},
					},
					Type: fleetnetv1alpha1.ClusterSetIP,
				}
				return cmp.Diff(want, serviceImport.Status, options...)
			}, timeout, interval).Should(BeEmpty())

			By("Checking internalServiceExportA condition and should mark as unconflicted")
			Eventually(func() string {
				key := types.NamespacedName{
					Namespace: internalServiceExportA.GetNamespace(),
					Name:      internalServiceExportA.GetName(),
				}
				var got fleetnetv1alpha1.InternalServiceExport
				if err := k8sClient.Get(ctx, key, &got); err != nil {
					return err.Error()
				}
				want := fleetnetv1alpha1.InternalServiceExport{
					Spec:       internalServiceExportSpec,
					ObjectMeta: internalServiceExportA.ObjectMeta,
					Status: fleetnetv1alpha1.InternalServiceExportStatus{
						Conditions: []metav1.Condition{
							unconflictedServiceExportConflictCondition(testNamespace, testServiceName),
						},
					},
				}
				return cmp.Diff(want, got, options...)
			}, timeout, interval).Should(BeEmpty())
		})

		It("InternalServiceExport is in the deleting state", func() {
			By("Creating internalServiceExportA")
			Expect(k8sClient.Create(ctx, internalServiceExportA)).Should(Succeed())

			internalServiceExportA.Status = fleetnetv1alpha1.InternalServiceExportStatus{
				Conditions: []metav1.Condition{
					conflictedServiceExportConflictCondition(testNamespace, testServiceName),
				},
			}
			Expect(k8sClient.Delete(ctx, internalServiceExportA))

			By("Creating serviceImport")
			serviceImport = &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testNamespace,
				},
			}
			Expect(k8sClient.Create(ctx, serviceImport)).Should(Succeed())

			By("Checking serviceImport")
			Eventually(func() bool {
				return errors.IsNotFound(k8sClient.Get(ctx, serviceImportKey, serviceImport))
			}, timeout, interval).Should(BeTrue())
		})
	})

	Context("ServiceImport has empty ports spec", func() {
		var serviceImport *fleetnetv1alpha1.ServiceImport
		var internalServiceExport *fleetnetv1alpha1.InternalServiceExport
		BeforeEach(func() {
			internalServiceExport = &fleetnetv1alpha1.InternalServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testNamespace + "-" + testServiceName,
					Namespace: testMemberClusterA,
				},
				Spec: internalServiceExportSpec,
			}

			By("Creating internalServiceExport")
			Expect(k8sClient.Create(ctx, internalServiceExport)).Should(Succeed())
		})
		AfterEach(func() {
			By("Deleting serviceImport if exists")
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, serviceImport))).Should(Succeed())
		})

		It("internalServiceExport is just created without finalizer and serviceImport should be deleted", func() {
			By("Creating serviceImport")
			serviceImport = &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testNamespace,
				},
			}
			Expect(k8sClient.Create(ctx, serviceImport)).Should(Succeed())

			By("Checking serviceImport")
			Eventually(func() bool {
				return errors.IsNotFound(k8sClient.Get(ctx, serviceImportKey, serviceImport))
			}, timeout, interval).Should(BeTrue())
		})
	})
})
