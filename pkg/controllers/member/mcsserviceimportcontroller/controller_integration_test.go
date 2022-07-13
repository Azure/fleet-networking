/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package mcsserviceimportcontroller

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"

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
		It("should create ServiceImport with expected specs", func() {
			serviceImport := &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-import-name",
					Namespace: testNamespace,
				},
			}

			By("By creating a service import")
			Expect(memberClient.Create(ctx, serviceImport)).Should(Succeed())
			serviceImportLookupKey := types.NamespacedName{Name: serviceImport.Name, Namespace: serviceImport.Namespace}
			createdServiceImport := &fleetnetv1alpha1.ServiceImport{}
			// We'll need to retry getting this newly created ServiceImport, given that creation may not immediately happen.
			Eventually(func() bool {
				if err := memberClient.Get(ctx, serviceImportLookupKey, createdServiceImport); err != nil {
					return false
				}
				return true
			}, timeout, interval).Should(BeTrue())

			internalServiceImportName := formatInternalServiceImportName(serviceImport)
			internalServiceImportLookupKey := types.NamespacedName{Name: internalServiceImportName, Namespace: hubNamespace}

			createdInternalServiceImport := &fleetnetv1alpha1.InternalServiceImport{}
			By("By checking the cluster ID of the internal service import ServiceImportReference does not change")
			Consistently(func() (fleetnetv1alpha1.ExportedObjectReference, error) {
				if err := hubClient.Get(ctx, internalServiceImportLookupKey, createdInternalServiceImport); err != nil {
					return fleetnetv1alpha1.ExportedObjectReference{}, err
				}
				return createdInternalServiceImport.Spec.ServiceImportReference, nil
			}, duration, interval).Should(Equal(1))
		})
	})
})
