/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package serviceimport

import (
	"fmt"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

var (
	testNamespace string
)

var _ = Describe("Create or update a service import", func() {

	BeforeEach(func() {
		// Unique name is used to make sure tests don't influence one another
		testNamespace = fmt.Sprintf("%s-%s", testNamespacePrefix, uuid.NewUUID()[:5])
		By("Create test namespace")
		testNS := corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		}
		Expect(memberClient.Create(ctx, &testNS)).Should(Succeed())
	})

	AfterEach(func() {
		By("Delete test namespace")
		testNS := corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		}
		Expect(memberClient.Delete(ctx, &testNS)).Should(Succeed())
	})

	When("Create InternalServiceImport from ServiceImport", func() {
		It("should create InternalServiceImport with expected specs", func() {
			// 1. Check internal service import is created with expected info after service import is created.
			serviceImport := &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-import-name",
					Namespace: testNamespace,
				},
			}
			By("By creating a service import")
			Expect(memberClient.Create(ctx, serviceImport)).Should(Succeed())
			serviceImportLookupKey := types.NamespacedName{Name: serviceImport.Name, Namespace: serviceImport.Namespace}
			// We'll need to retry getting this newly created ServiceImport, given that creation may not immediately happen.
			Eventually(func() bool {
				if err := memberClient.Get(ctx, serviceImportLookupKey, serviceImport); err != nil {
					return false
				}
				finalizers := serviceImport.GetFinalizers()
				for _, finalizer := range finalizers {
					if finalizer == ServiceImportFinalizer {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())
			internalServiceImportName := formatInternalServiceImportName(serviceImport)
			internalServiceImportLookupKey := types.NamespacedName{Name: internalServiceImportName, Namespace: HubNamespace}
			expectedServiceImportRef := fleetnetv1alpha1.FromMetaObjects(MemberClusterID, serviceImport.TypeMeta, serviceImport.ObjectMeta, serviceImport.CreationTimestamp)
			internalServiceImport := &fleetnetv1alpha1.InternalServiceImport{}
			By("By checking the ServiceImportReference of internal service import is updated")
			Eventually(func() bool {
				if err := hubClient.Get(ctx, internalServiceImportLookupKey, internalServiceImport); err != nil {
					return false
				}
				return cmp.Equal(expectedServiceImportRef, internalServiceImport.Spec.ServiceImportReference)
			}, duration, interval).Should(BeTrue())

			// 2. Check internal service import is updated after service import is updated.
			By("By updating service import")
			testLabelKey := "fake-key"
			testLabelValue := "fake-value"
			if serviceImport.GetLabels() != nil {
				serviceImport.Labels[testLabelKey] = testLabelValue
			} else {
				serviceImport.SetLabels(map[string]string{testLabelKey: testLabelValue})
			}
			Expect(memberClient.Update(ctx, serviceImport)).Should(Succeed())
			By("By fetching updated service import")
			updatedServiceImport := &fleetnetv1alpha1.ServiceImport{}
			Eventually(func() bool {
				if err := memberClient.Get(ctx, serviceImportLookupKey, updatedServiceImport); err != nil {
					return false
				}
				return updatedServiceImport.GetLabels() != nil && updatedServiceImport.GetLabels()[testLabelKey] == testLabelValue
			}, timeout, interval).Should(BeTrue())
			By("By checking internal service import is updated")
			expectedServiceImportRef = fleetnetv1alpha1.FromMetaObjects(MemberClusterID, updatedServiceImport.TypeMeta, updatedServiceImport.ObjectMeta, serviceImport.CreationTimestamp)
			updatedInternalServiceImport := &fleetnetv1alpha1.InternalServiceImport{}
			Eventually(func() bool {
				if err := hubClient.Get(ctx, internalServiceImportLookupKey, updatedInternalServiceImport); err != nil {
					return false
				}
				return cmp.Equal(expectedServiceImportRef, updatedInternalServiceImport.Spec.ServiceImportReference)
			}, duration, interval).Should(BeTrue())

			// 3. Check internal service import is deleted after service import is deleted.
			By("By deleting the service import")
			Expect(memberClient.Delete(ctx, serviceImport)).Should(Succeed())
			By("By checking the existence of  service import")
			serviceImportList := &fleetnetv1alpha1.ServiceImportList{}
			Eventually(func() (int, error) {
				if err := memberClient.List(ctx, serviceImportList, &client.ListOptions{Namespace: testNamespace}); err != nil {
					return -1, err
				}
				return len(serviceImportList.Items), nil
			}, duration, interval).Should(Equal(0))
			By("By checking the existence of internal service import")
			internalServiceImportList := &fleetnetv1alpha1.InternalServiceImportList{}
			Eventually(func() (int, error) {
				if err := hubClient.List(ctx, internalServiceImportList, &client.ListOptions{Namespace: HubNamespace}); err != nil {
					return -1, err
				}
				return len(internalServiceImportList.Items), nil
			}, duration, interval).Should(Equal(0))
		})
	})
})
