/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package mcsserviceimportcontroller

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
			internalServiceImportLookupKey := types.NamespacedName{Name: internalServiceImportName, Namespace: hubNamespace}
			expectedServiceImportRef := fleetnetv1alpha1.FromMetaObjects(memberClusterID, serviceImport.TypeMeta, serviceImport.ObjectMeta)
			createdInternalServiceImport := &fleetnetv1alpha1.InternalServiceImport{}

			By("By checking the ServiceImportReference of internal service import is updated")
			Eventually(func() bool {
				if err := hubClient.Get(ctx, internalServiceImportLookupKey, createdInternalServiceImport); err != nil {
					return false
				}
				return cmp.Equal(expectedServiceImportRef, createdInternalServiceImport.Spec.ServiceImportReference)
				//return exportedObjectReferenceEqual(expectedServiceImportRef, createdInternalServiceImport.Spec.ServiceImportReference), nil
			}, duration, interval).Should(BeTrue())

			By("By updating service import")
			if serviceImport.GetLabels() != nil {
				serviceImport.Labels["fake-key"] = "fake-value"
			} else {
				serviceImport.SetLabels(map[string]string{"fake-key": "fake-value"})
			}
			Expect(memberClient.Update(ctx, serviceImport)).Should(Succeed())
			updatedInternalServiceImport := &fleetnetv1alpha1.InternalServiceImport{}
			By("By checking internalserviceimport is updated")
			Eventually(func() bool {
				if err := hubClient.Get(ctx, internalServiceImportLookupKey, updatedInternalServiceImport); err != nil {
					return false
				}
				return updatedInternalServiceImport.ResourceVersion > createdInternalServiceImport.ResourceVersion
			}, duration, interval).Should(BeTrue())

			By("By deleting the service import")
			Expect(memberClient.Delete(ctx, serviceImport)).Should(Succeed())
			By("By checking the existence of internal service import")
			Eventually(func() (int, error) {
				internalServiceImportList := &fleetnetv1alpha1.InternalServiceImportList{}
				if err := hubClient.List(ctx, internalServiceImportList, &client.ListOptions{Namespace: hubNamespace}); err != nil {
					return -1, err
				}
				return len(internalServiceImportList.Items), nil
			}, duration, interval).Should(Equal(0))
		})
	})
})
