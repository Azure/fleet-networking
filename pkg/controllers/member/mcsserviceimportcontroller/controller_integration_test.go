/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package mcsserviceimportcontroller

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

const (
	timeout  = time.Second * 10
	duration = time.Second * 10
	interval = time.Millisecond * 250

	testNamespace = "fake-namespace"
)

var _ = Describe("Create or update a service import", func() {

	BeforeEach(func() {
		By("Create multiClusterService namespace")
		ns := corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		}
		Expect(memberClient.Create(ctx, &ns)).Should(Succeed())
	})

	AfterEach(func() {
		By("delete multiClusterService namespace")
		ns := corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		}
		Expect(memberClient.Delete(ctx, &ns)).Should(Succeed())
	})

	When("Exposed cluster ID is found", func() {
		It("should not return early", func() {
			serviceImport := &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "serivce-import-name",
					Namespace: testNamespace,
				},
			}
			By("By creating a internal service import ")
			existingMemberClusterID := fmt.Sprintf("not-%s", memberClusterID)
			internalServiceImportName := formatInternalServiceImportName(serviceImport)
			internalServiceImport := &fleetnetv1alpha1.InternalServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      internalServiceImportName,
					Namespace: hubNamespace,
				},
				Spec: fleetnetv1alpha1.InternalServiceImportSpec{
					ServiceImportReference: fleetnetv1alpha1.ExportedObjectReference{
						ClusterID: existingMemberClusterID,
					},
				},
			}
			Expect(memberClient.Create(ctx, internalServiceImport)).Should(Succeed())
			internalServiceImportLookupKey := types.NamespacedName{Name: internalServiceImport.Name, Namespace: internalServiceImport.Namespace}
			createdInternalServiceImport := &fleetnetv1alpha1.InternalServiceImport{}
			// We'll need to retry getting this newly created InternalServiceImport, given that creation may not immediately happen.
			Eventually(func() bool {
				if err := memberClient.Get(ctx, internalServiceImportLookupKey, createdInternalServiceImport); err != nil {
					return false
				}
				return true
			}, timeout, interval).Should(BeTrue())
			Expect(createdInternalServiceImport.Spec.ServiceImportReference.ClusterID).Should(Equal(existingMemberClusterID))

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

			By("By checking the cluster ID of the internal service import ServiceImportReference does not change")
			Consistently(func() (string, error) {
				if err := memberClient.Get(ctx, internalServiceImportLookupKey, createdInternalServiceImport); err != nil {
					return "", err
				}
				return createdInternalServiceImport.Spec.ServiceImportReference.ClusterID, nil
			}, duration, interval).Should(Equal(existingMemberClusterID))
		})
	})

	When("InternalServiceImport is not found", func() {
		It("should not return early", func() {
			serviceImport := &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "serivce-import-name",
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

			expectedClusterID := memberClusterID
			expectedNamespace := hubNamespace

			internalServiceImportName := formatInternalServiceImportName(serviceImport)
			internalServiceImportLookupKey := types.NamespacedName{Name: internalServiceImportName, Namespace: hubNamespace}
			createdInternalServiceImport := &fleetnetv1alpha1.InternalServiceImport{}
			By("By checking the cluster ID and namespace of the internal service import ServiceImportReference is updated")
			Consistently(func() ([]string, error) {
				if err := memberClient.Get(ctx, internalServiceImportLookupKey, createdInternalServiceImport); err != nil {
					return []string{}, err
				}
				return []string{createdInternalServiceImport.Spec.ServiceImportReference.ClusterID, createdInternalServiceImport.Spec.ServiceImportReference.Namespace}, nil
			}, duration, interval).Should(Equal([]string{expectedClusterID, expectedNamespace}))
		})
	})

})
