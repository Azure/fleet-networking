/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package multiclusterservice

import (
	"fmt"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
)

var _ = Describe("Test MultiClusterService Controller", func() {
	const (
		timeout  = time.Second * 10
		duration = time.Second * 10
		interval = time.Millisecond * 250
	)

	Context("When creating new MultiClusterService", func() {
		It("Should create service import and derived service", func() {
			By("By creating a new MultiClusterService")
			multiClusterService := multiClusterServiceForTest()
			Expect(k8sClient.Create(ctx, multiClusterService)).Should(Succeed())

			mcsLookupKey := types.NamespacedName{Name: testName, Namespace: testNamespace}
			createdMultiClusterService := &fleetnetv1alpha1.MultiClusterService{}

			Eventually(func() bool {
				if err := k8sClient.Get(ctx, mcsLookupKey, createdMultiClusterService); err != nil {
					return false
				}
				return createdMultiClusterService.GetLabels()[multiClusterServiceLabelServiceImport] == testServiceName
			}, timeout, interval).Should(BeTrue())

			By("By checking mcs condition and expecting unknown")
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, mcsLookupKey, createdMultiClusterService); err != nil {
					return false
				}
				expected := metav1.Condition{
					Type:   string(fleetnetv1alpha1.MultiClusterServiceValid),
					Status: metav1.ConditionUnknown,
					Reason: conditionReasonUnknownServiceImport,
				}
				return len(createdMultiClusterService.Status.Conditions) == 1 &&
					cmp.Equal(createdMultiClusterService.Status.Conditions[0], expected, cmpopts.IgnoreFields(metav1.Condition{}, "Message", "LastTransitionTime", "ObservedGeneration"))
			}, timeout, interval).Should(BeTrue())

			By("By checking derived service label")
			_, ok := createdMultiClusterService.GetLabels()[objectmeta.MultiClusterServiceLabelDerivedService]
			Expect(ok).Should(BeFalse())

			serviceImportLookupKey := types.NamespacedName{Name: testServiceName, Namespace: testNamespace}
			createdServiceImport := &fleetnetv1alpha1.ServiceImport{}

			By("By checking service import")
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, serviceImportLookupKey, createdServiceImport); err != nil {
					return false
				}
				return true
			}, timeout, interval).Should(BeTrue())

			By("By checking derived Service in the fleet-system")
			Consistently(func() (int, error) {
				serviceList := &corev1.ServiceList{}
				if err := k8sClient.List(ctx, serviceList, &client.ListOptions{Namespace: systemNamespace}); err != nil {
					return -1, err
				}
				return len(serviceList.Items), nil
			}, duration, interval).Should(Equal(0))

			By("By updating service import status")
			createdServiceImport.Status = fleetnetv1alpha1.ServiceImportStatus{
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
			}
			Expect(k8sClient.Status().Update(ctx, createdServiceImport)).Should(Succeed())

			Eventually(func() bool {
				if err := k8sClient.Get(ctx, mcsLookupKey, createdMultiClusterService); err != nil {
					return false
				}
				return createdMultiClusterService.Status.Conditions[0].Status == metav1.ConditionTrue
			}, duration, interval).Should(BeTrue())

			derivedServiceLookupKey := types.NamespacedName{Name: createdMultiClusterService.GetLabels()[objectmeta.MultiClusterServiceLabelDerivedService], Namespace: systemNamespace}
			createdService := &corev1.Service{}
			By("By checking derived service")
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, derivedServiceLookupKey, createdService); err != nil {
					return false
				}
				return true
			}, timeout, interval).Should(BeTrue())

			By("By checking mcs condition and expecting valid")
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, mcsLookupKey, createdMultiClusterService); err != nil {
					return false
				}
				expected := metav1.Condition{
					Type:   string(fleetnetv1alpha1.MultiClusterServiceValid),
					Status: metav1.ConditionTrue,
					Reason: conditionReasonFoundServiceImport,
				}
				option := cmpopts.IgnoreFields(metav1.Condition{}, "Message", "LastTransitionTime", "ObservedGeneration")
				return len(createdMultiClusterService.Status.Conditions) == 1 &&
					cmp.Equal(createdMultiClusterService.Status.Conditions[0], expected, option)
			}, timeout, interval).Should(BeTrue())

			By("By updating service status")
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, derivedServiceLookupKey, createdService); err != nil {
					return false
				}
				createdService.Status = corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{
								IP: "10.0.0.1",
							},
						},
					},
				}
				if err := k8sClient.Status().Update(ctx, createdService); err != nil {
					return false
				}
				return true
			}, timeout, interval).Should(BeTrue())

			By("By checking mcs load balancer status")
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, mcsLookupKey, createdMultiClusterService); err != nil {
					return false
				}
				if err := k8sClient.Get(ctx, derivedServiceLookupKey, createdService); err != nil {
					return false
				}
				return cmp.Equal(createdMultiClusterService.Status.LoadBalancer, createdService.Status.LoadBalancer)
			}, timeout, interval).Should(BeTrue())

			By("By updating mcs spec to use unknown service")
			newServiceImport := "my-new-svc"
			createdMultiClusterService.Spec.ServiceImport.Name = newServiceImport
			Expect(k8sClient.Update(ctx, createdMultiClusterService)).Should(Succeed())

			By("By checking derived service")
			Eventually(func() bool {
				return errors.IsNotFound(k8sClient.Get(ctx, derivedServiceLookupKey, createdService))
			}, timeout, interval).Should(BeTrue())

			By("By checking old service import")
			Eventually(func() bool {
				return errors.IsNotFound(k8sClient.Get(ctx, serviceImportLookupKey, createdServiceImport))
			}, timeout, interval).Should(BeTrue())

			By("By checking new service import")
			newServiceImportLookupKey := types.NamespacedName{Name: newServiceImport, Namespace: testNamespace}
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, newServiceImportLookupKey, createdServiceImport); err != nil {
					return false
				}
				return true
			}, timeout, interval).Should(BeTrue())

			By("By checking mcs condition and expecting unknown")
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, mcsLookupKey, createdMultiClusterService); err != nil {
					return false
				}
				expected := metav1.Condition{
					Type:   string(fleetnetv1alpha1.MultiClusterServiceValid),
					Status: metav1.ConditionUnknown,
					Reason: conditionReasonUnknownServiceImport,
				}
				return len(createdMultiClusterService.Status.Conditions) == 1 &&
					cmp.Equal(createdMultiClusterService.Status.Conditions[0], expected, cmpopts.IgnoreFields(metav1.Condition{}, "Message", "LastTransitionTime", "ObservedGeneration"))
			}, timeout, interval).Should(BeTrue())

			By("By deleting mcs")
			Expect(k8sClient.Delete(ctx, multiClusterService)).Should(Succeed())

			By("By checking service import")
			Eventually(func() (int, error) {
				serviceImportList := &fleetnetv1alpha1.ServiceImportList{}
				if err := k8sClient.List(ctx, serviceImportList, &client.ListOptions{Namespace: testNamespace}); err != nil {
					return -1, err
				}
				return len(serviceImportList.Items), nil
			}, duration, interval).Should(Equal(0))

			By("By checking derived Service in the fleet-system")
			Eventually(func() (int, error) {
				serviceList := &corev1.ServiceList{}
				if err := k8sClient.List(ctx, serviceList, &client.ListOptions{Namespace: systemNamespace}); err != nil {
					return -1, err
				}
				return len(serviceList.Items), nil
			}, duration, interval).Should(Equal(0))

			By("By checking mcs")
			Eventually(func() bool {
				return errors.IsNotFound(k8sClient.Get(ctx, mcsLookupKey, createdMultiClusterService))
			}, timeout, interval).Should(BeTrue())
		})

		It("ServiceImport has been owned by other MultiClusterService", func() {
			controller := true
			blockOwnerDeletion := false // so the test could delete service import
			By("Creating serviceImport which is owned by other mcs")
			serviceImport := &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testNamespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         multiClusterServiceType.APIVersion,
							Kind:               multiClusterServiceType.Kind,
							Name:               "another-mcs",
							Controller:         &controller,
							BlockOwnerDeletion: &blockOwnerDeletion,
							UID:                "12345",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, serviceImport)).Should(Succeed(), "Failed to create serviceImport")

			multiClusterService := multiClusterServiceForTest()
			Expect(k8sClient.Create(ctx, multiClusterService)).Should(Succeed())

			mcsLookupKey := types.NamespacedName{Name: testName, Namespace: testNamespace}
			createdMultiClusterService := &fleetnetv1alpha1.MultiClusterService{}

			Eventually(func() bool {
				if err := k8sClient.Get(ctx, mcsLookupKey, createdMultiClusterService); err != nil {
					return false
				}
				return createdMultiClusterService.GetLabels()[multiClusterServiceLabelServiceImport] == testServiceName
			}, timeout, interval).Should(BeTrue(), "ServiceImport label value got %v, want %v", createdMultiClusterService.GetLabels()[multiClusterServiceLabelServiceImport], testServiceName)

			By("By checking derived service label")
			Expect(createdMultiClusterService.GetLabels()[objectmeta.MultiClusterServiceLabelDerivedService]).Should(BeEmpty())

			By("By checking mcs condition and want unknown")
			Eventually(func() error {
				if err := k8sClient.Get(ctx, mcsLookupKey, createdMultiClusterService); err != nil {
					return err
				}
				want := fleetnetv1alpha1.MultiClusterServiceStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(fleetnetv1alpha1.MultiClusterServiceValid),
							Status: metav1.ConditionUnknown,
							Reason: conditionReasonUnknownServiceImport,
						},
					},
				}
				option := cmpopts.IgnoreFields(metav1.Condition{}, "Message", "LastTransitionTime", "ObservedGeneration")
				if diff := cmp.Diff(want, createdMultiClusterService.Status, option); diff != "" {
					return fmt.Errorf("mcs status mismatch (-want, +got):\n%s", diff)
				}
				return nil
			}, timeout, interval).Should(Succeed(), "Failed to validate mcs status")

			By("Deleting serviceImport")
			Expect(k8sClient.Delete(ctx, serviceImport)).Should(Succeed())

			By("By checking derived service label")
			_, ok := createdMultiClusterService.GetLabels()[objectmeta.MultiClusterServiceLabelDerivedService]
			Expect(ok).Should(BeFalse())

			By("By checking service import")
			serviceImportLookupKey := types.NamespacedName{Name: testServiceName, Namespace: testNamespace}
			createdServiceImport := &fleetnetv1alpha1.ServiceImport{}
			Eventually(func() error {
				blockOwnerDeletion = true
				want := []metav1.OwnerReference{
					{
						APIVersion:         multiClusterServiceType.APIVersion,
						Kind:               multiClusterServiceType.Kind,
						Name:               multiClusterService.Name,
						Controller:         &controller,
						BlockOwnerDeletion: &blockOwnerDeletion,
					},
				}
				if err := k8sClient.Get(ctx, serviceImportLookupKey, createdServiceImport); err != nil {
					return nil
				}
				option := cmpopts.IgnoreFields(metav1.OwnerReference{}, "UID")
				if diff := cmp.Diff(want, createdServiceImport.OwnerReferences, option); diff != "" {
					return fmt.Errorf("serviceImport ownerReferences mismatch (-want, +got):\n%s", diff)
				}
				return nil
			}, timeout, interval).Should(Succeed(), "Failed to validate serviceImport ownerReferences")

			By("By deleting mcs")
			Expect(k8sClient.Delete(ctx, multiClusterService)).Should(Succeed())

			By("By checking mcs")
			Eventually(func() bool {
				return errors.IsNotFound(k8sClient.Get(ctx, mcsLookupKey, createdMultiClusterService))
			}, timeout, interval).Should(BeTrue())
		})
	})
})
