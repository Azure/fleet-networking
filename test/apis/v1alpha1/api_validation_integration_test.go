/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	"errors"
	"fmt"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"reflect"

	v1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

var (
	nameContainsUnderscore       = "a_bcdef-123456789-123456789-123456789-123456789-123456789-123456789-123456789"
	nameEndingWithNonAlphanum    = "abcdef-123456789-123456789-123456789-123456789-123456789-"
	nameStartingWithNonAlphanum  = "-abcdef-123456789-123456789-123456789-123456789-123456789"
	nameValid                    = "abc-123456789-123456789-123456789-123456789-123456789-123456789"
	nameValidEndingWithAphabet   = "123456789-abc"
	nameValidEndingWithNumber    = "123456789-123"
	nameValidStartingWithAphabet = "abc-123456789"
	nameValidStartingWithNumber  = "123-123456789"
	nameWithInvalidSize          = "abcdef-123456789-123456789-123456789-123456789-123456789-123456789-123456789"
)

var _ = Describe("Test networking v1alpha1 API validation", func() {

	Context("Test MultiClusterService API validation - invalid cases", func() {
		It("should deny creating API with invalid name size", func() {

			// Create the API.
			multiClusterServiceName := &v1alpha1.MultiClusterService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameWithInvalidSize,
					Namespace: testNamespace,
				},
				Spec: v1alpha1.MultiClusterServiceSpec{
					ServiceImport: v1alpha1.ServiceImportRef{
						Name: "service-import-1",
					},
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameWithInvalidSize))
			var err = hubClient.Create(ctx, multiClusterServiceName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("metadata.name max length is 63"))
		})

		It("should deny creating API with invalid name starting with non-alphanumeric character", func() {

			// Create the API.
			multiClusterServiceName := &v1alpha1.MultiClusterService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameStartingWithNonAlphanum,
					Namespace: testNamespace,
				},
				Spec: v1alpha1.MultiClusterServiceSpec{
					ServiceImport: v1alpha1.ServiceImportRef{
						Name: "service-import-name",
					},
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameStartingWithNonAlphanum))
			err := hubClient.Create(ctx, multiClusterServiceName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("a lowercase RFC 1123 subdomain"))
		})

		It("should deny creating API with invalid name ending with non-alphanumeric character", func() {

			// Create the API.
			multiClusterServiceName := &v1alpha1.MultiClusterService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameEndingWithNonAlphanum,
					Namespace: testNamespace,
				},
				Spec: v1alpha1.MultiClusterServiceSpec{
					ServiceImport: v1alpha1.ServiceImportRef{
						Name: "service-import-name",
					},
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameEndingWithNonAlphanum))
			err := hubClient.Create(ctx, multiClusterServiceName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("a lowercase RFC 1123 subdomain"))
		})

		It("should deny creating API with invalid name containing character that is not alphanumeric and not -", func() {

			// Create the API.
			multiClusterServiceName := &v1alpha1.MultiClusterService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameContainsUnderscore,
					Namespace: testNamespace,
				},
				Spec: v1alpha1.MultiClusterServiceSpec{
					ServiceImport: v1alpha1.ServiceImportRef{
						Name: "service-import-name",
					},
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameContainsUnderscore))
			err := hubClient.Create(ctx, multiClusterServiceName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("a lowercase RFC 1123 subdomain"))
		})
	})

	Context("Test MultiClusterService creation API validation - valid cases", func() {
		It("should allow creating API with valid name size", func() {

			// Create the API.
			multiClusterServiceName := &v1alpha1.MultiClusterService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameValid,
					Namespace: testNamespace,
				},
				Spec: v1alpha1.MultiClusterServiceSpec{
					ServiceImport: v1alpha1.ServiceImportRef{
						Name: "service-import-name",
					},
				},
			}
			Expect(hubClient.Create(ctx, multiClusterServiceName)).Should(Succeed(), "failed to create multiClusterService")
			Expect(hubClient.Delete(ctx, multiClusterServiceName)).Should(Succeed(), "failed to delete multiClusterService")
		})

		It("should allow creating API with valid name starting with alphabet character", func() {

			// Create the API.
			multiClusterServiceName := &v1alpha1.MultiClusterService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameValidStartingWithAphabet,
					Namespace: testNamespace,
				},
				Spec: v1alpha1.MultiClusterServiceSpec{
					ServiceImport: v1alpha1.ServiceImportRef{
						Name: "service-import-name",
					},
				},
			}
			Expect(hubClient.Create(ctx, multiClusterServiceName)).Should(Succeed(), "failed to create multiClusterService")
			Expect(hubClient.Delete(ctx, multiClusterServiceName)).Should(Succeed(), "failed to delete multiClusterService")
		})

		It("should allow creating API with valid name starting with numeric character", func() {

			// Create the API.
			multiClusterServiceName := &v1alpha1.MultiClusterService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameValidStartingWithNumber,
					Namespace: testNamespace,
				},
				Spec: v1alpha1.MultiClusterServiceSpec{
					ServiceImport: v1alpha1.ServiceImportRef{
						Name: "service-import-name",
					},
				},
			}
			Expect(hubClient.Create(ctx, multiClusterServiceName)).Should(Succeed(), "failed to create multiClusterService")
			Expect(hubClient.Delete(ctx, multiClusterServiceName)).Should(Succeed(), "failed to delete multiClusterService")
		})

		It("should allow creating API with valid name ending with alphabet character", func() {

			// Create the API.
			multiClusterServiceName := &v1alpha1.MultiClusterService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameValidEndingWithAphabet,
					Namespace: testNamespace,
				},
				Spec: v1alpha1.MultiClusterServiceSpec{
					ServiceImport: v1alpha1.ServiceImportRef{
						Name: "service-import-name",
					},
				},
			}
			Expect(hubClient.Create(ctx, multiClusterServiceName)).Should(Succeed(), "failed to create multiClusterService")
			Expect(hubClient.Delete(ctx, multiClusterServiceName)).Should(Succeed(), "failed to delete multiClusterService")
		})

		It("should allow creating API with valid name ending with numeric character", func() {

			// Create the API.
			multiClusterServiceName := &v1alpha1.MultiClusterService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameValidEndingWithNumber,
					Namespace: testNamespace,
				},
				Spec: v1alpha1.MultiClusterServiceSpec{
					ServiceImport: v1alpha1.ServiceImportRef{
						Name: "service-import-name",
					},
				},
			}
			Expect(hubClient.Create(ctx, multiClusterServiceName)).Should(Succeed(), "failed to create multiClusterService")
			Expect(hubClient.Delete(ctx, multiClusterServiceName)).Should(Succeed(), "failed to delete multiClusterService")
		})
	})

	Context("Test ServiceExport API validation - invalid cases", func() {
		It("should deny creating API with invalid name size", func() {

			// Create the API.
			serviceExportName := &v1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameWithInvalidSize,
					Namespace: testNamespace,
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameWithInvalidSize))
			var err = hubClient.Create(ctx, serviceExportName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("metadata.name max length is 63"))
		})

		It("should deny creating API with invalid name starting with non-alphanumeric character", func() {

			// Create the API.
			serviceExportName := &v1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameStartingWithNonAlphanum,
					Namespace: testNamespace,
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameStartingWithNonAlphanum))
			var err = hubClient.Create(ctx, serviceExportName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character"))
		})

		It("should deny creating API with invalid name ending with non-alphanumeric character", func() {

			// Create the API.
			serviceExportName := &v1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameEndingWithNonAlphanum,
					Namespace: testNamespace,
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameEndingWithNonAlphanum))
			var err = hubClient.Create(ctx, serviceExportName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character"))
		})

		It("should deny creating API with invalid name containing character that is not alphanumeric and not -", func() {

			// Create the API.
			serviceExportName := &v1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameContainsUnderscore,
					Namespace: testNamespace,
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameContainsUnderscore))
			var err = hubClient.Create(ctx, serviceExportName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character"))
		})
	})

	Context("Test ServiceExport API validation - valid cases", func() {
		It("should allow creating API with valid name size", func() {

			// Create the API.
			serviceExportName := &v1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameValid,
					Namespace: testNamespace,
				},
			}
			Expect(hubClient.Create(ctx, serviceExportName)).Should(Succeed(), "failed to create serviceExport")
			Expect(hubClient.Delete(ctx, serviceExportName)).Should(Succeed(), "failed to delete serviceExport")
		})

		It("should allow creating API with valid name starting with alphabet character", func() {

			// Create the API.
			serviceExportName := &v1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameValidStartingWithAphabet,
					Namespace: testNamespace,
				},
			}
			Expect(hubClient.Create(ctx, serviceExportName)).Should(Succeed(), "failed to create serviceExport")
			Expect(hubClient.Delete(ctx, serviceExportName)).Should(Succeed(), "failed to delete serviceExport")
		})

		It("should allow creating API with valid name starting with numeric character", func() {

			// Create the API.
			serviceExportName := &v1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameValidStartingWithNumber,
					Namespace: testNamespace,
				},
			}
			Expect(hubClient.Create(ctx, serviceExportName)).Should(Succeed(), "failed to create serviceExport")
			Expect(hubClient.Delete(ctx, serviceExportName)).Should(Succeed(), "failed to delete serviceExport")
		})

		It("should allow creating API with valid name ending with alphabet character", func() {

			// Create the API.
			serviceExportName := &v1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameValidEndingWithAphabet,
					Namespace: testNamespace,
				},
			}
			Expect(hubClient.Create(ctx, serviceExportName)).Should(Succeed(), "failed to create serviceExport")
			Expect(hubClient.Delete(ctx, serviceExportName)).Should(Succeed(), "failed to delete serviceExport")
		})

		It("should allow creating API with valid name ending with numeric character", func() {

			// Create the API.
			serviceExportName := &v1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameValidEndingWithNumber,
					Namespace: testNamespace,
				},
			}
			Expect(hubClient.Create(ctx, serviceExportName)).Should(Succeed(), "failed to create serviceExport")
			Expect(hubClient.Delete(ctx, serviceExportName)).Should(Succeed(), "failed to delete serviceExport")
		})
	})

	Context("Test ServiceImport API validation - invalid cases", func() {
		It("should deny creating API with invalid name size", func() {

			// Create the API.
			serviceImportName := &v1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameWithInvalidSize,
					Namespace: testNamespace,
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameWithInvalidSize))
			var err = hubClient.Create(ctx, serviceImportName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("metadata.name max length is 63"))
		})

		It("should deny creating API with invalid name starting with non-alphanumeric character", func() {

			// Create the API.
			serviceImportName := &v1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameStartingWithNonAlphanum,
					Namespace: testNamespace,
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameStartingWithNonAlphanum))
			var err = hubClient.Create(ctx, serviceImportName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character"))
		})

		It("should deny creating API with invalid name ending with non-alphanumeric character", func() {

			// Create the API.
			serviceImportName := &v1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameEndingWithNonAlphanum,
					Namespace: testNamespace,
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameEndingWithNonAlphanum))
			var err = hubClient.Create(ctx, serviceImportName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character"))
		})

		It("should deny creating API with invalid name containing character that is not alphanumeric and not -", func() {

			// Create the API.
			serviceImportName := &v1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameContainsUnderscore,
					Namespace: testNamespace,
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameContainsUnderscore))
			var err = hubClient.Create(ctx, serviceImportName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("metadata.name max length is 63"))
		})
	})

	Context("Test ServiceImport API validation - valid cases", func() {
		It("should allow creating API with valid name size", func() {

			// Create the API.
			serviceImportName := &v1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameValid,
					Namespace: testNamespace,
				},
			}
			Expect(hubClient.Create(ctx, serviceImportName)).Should(Succeed(), "failed to create serviceImport")
			Expect(hubClient.Delete(ctx, serviceImportName)).Should(Succeed(), "failed to delete serviceImport")
		})

		It("should allow creating API with valid name starting with alphabet character", func() {

			// Create the API.
			serviceImportName := &v1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameValidStartingWithAphabet,
					Namespace: testNamespace,
				},
			}
			Expect(hubClient.Create(ctx, serviceImportName)).Should(Succeed(), "failed to create serviceImport")
			Expect(hubClient.Delete(ctx, serviceImportName)).Should(Succeed(), "failed to delete serviceImport")
		})

		It("should allow creating API with valid name starting with numeric character", func() {

			// Create the API.
			serviceImportName := &v1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameValidStartingWithNumber,
					Namespace: testNamespace,
				},
			}
			Expect(hubClient.Create(ctx, serviceImportName)).Should(Succeed(), "failed to create serviceImport")
			Expect(hubClient.Delete(ctx, serviceImportName)).Should(Succeed(), "failed to delete serviceImport")
		})

		It("should allow creating API with valid name ending with alphabet character", func() {

			// Create the API.
			serviceImportName := &v1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameValidEndingWithAphabet,
					Namespace: testNamespace,
				},
			}
			Expect(hubClient.Create(ctx, serviceImportName)).Should(Succeed(), "failed to create serviceImport")
			Expect(hubClient.Delete(ctx, serviceImportName)).Should(Succeed(), "failed to delete serviceImport")
		})

		It("should allow creating API with valid name ending with numeric character", func() {

			// Create the API.
			serviceImportName := &v1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameValidEndingWithNumber,
					Namespace: testNamespace,
				},
			}
			Expect(hubClient.Create(ctx, serviceImportName)).Should(Succeed(), "failed to create serviceImport")
			Expect(hubClient.Delete(ctx, serviceImportName)).Should(Succeed(), "failed to delete serviceImport")
		})
	})

	Context("Test TrafficManagerProfile API validation - invalid cases", func() {
		It("should deny creating API with invalid name size", func() {

			// Create the API.
			trafficManagerProfileName := &v1alpha1.TrafficManagerProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameWithInvalidSize,
					Namespace: testNamespace,
				},
				Spec: v1alpha1.TrafficManagerProfileSpec{
					MonitorConfig: &v1alpha1.MonitorConfig{
						IntervalInSeconds: ptr.To(int64(30)),
						TimeoutInSeconds:  ptr.To(int64(7)),
					},
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameWithInvalidSize))
			var err = hubClient.Create(ctx, trafficManagerProfileName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("metadata.name max length is 63"))
		})

		It("should deny creating API with invalid name starting with non-alphanumeric character", func() {

			// Create the API.
			trafficManagerProfileName := &v1alpha1.TrafficManagerProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameStartingWithNonAlphanum,
					Namespace: testNamespace,
				},
				Spec: v1alpha1.TrafficManagerProfileSpec{
					MonitorConfig: &v1alpha1.MonitorConfig{
						IntervalInSeconds: ptr.To(int64(30)),
						TimeoutInSeconds:  ptr.To(int64(7)),
					},
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameStartingWithNonAlphanum))
			var err = hubClient.Create(ctx, trafficManagerProfileName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character"))
		})

		It("should deny creating API with invalid name ending with non-alphanumeric character", func() {

			// Create the API.
			trafficManagerProfileName := &v1alpha1.TrafficManagerProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameEndingWithNonAlphanum,
					Namespace: testNamespace,
				},
				Spec: v1alpha1.TrafficManagerProfileSpec{
					MonitorConfig: &v1alpha1.MonitorConfig{
						IntervalInSeconds: ptr.To(int64(30)),
						TimeoutInSeconds:  ptr.To(int64(7)),
					},
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameEndingWithNonAlphanum))
			var err = hubClient.Create(ctx, trafficManagerProfileName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character"))
		})

		It("should deny creating API with invalid name containing character that is not alphanumeric and not -", func() {

			// Create the API.
			trafficManagerProfileName := &v1alpha1.TrafficManagerProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameContainsUnderscore,
					Namespace: testNamespace,
				},
				Spec: v1alpha1.TrafficManagerProfileSpec{
					MonitorConfig: &v1alpha1.MonitorConfig{
						IntervalInSeconds: ptr.To(int64(30)),
						TimeoutInSeconds:  ptr.To(int64(7)),
					},
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameContainsUnderscore))
			var err = hubClient.Create(ctx, trafficManagerProfileName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("metadata.name max length is 63"))
		})
	})

	Context("Test TrafficManagerProfile API validation - valid cases", func() {
		It("should allow creating API with valid name size", func() {

			// Create the API.
			trafficManagerProfileName := &v1alpha1.TrafficManagerProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameValid,
					Namespace: testNamespace,
				},
				Spec: v1alpha1.TrafficManagerProfileSpec{
					MonitorConfig: &v1alpha1.MonitorConfig{
						IntervalInSeconds: ptr.To(int64(30)),
						TimeoutInSeconds:  ptr.To(int64(7)),
					},
				},
			}
			Expect(hubClient.Create(ctx, trafficManagerProfileName)).Should(Succeed(), "failed to create trafficManagerProfile")
			Expect(hubClient.Delete(ctx, trafficManagerProfileName)).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("should allow creating API with valid name starting with alphabet character", func() {

			// Create the API.
			trafficManagerProfileName := &v1alpha1.TrafficManagerProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameValidStartingWithAphabet,
					Namespace: testNamespace,
				},
				Spec: v1alpha1.TrafficManagerProfileSpec{
					MonitorConfig: &v1alpha1.MonitorConfig{
						IntervalInSeconds: ptr.To(int64(30)),
						TimeoutInSeconds:  ptr.To(int64(7)),
					},
				},
			}
			Expect(hubClient.Create(ctx, trafficManagerProfileName)).Should(Succeed(), "failed to create trafficManagerProfile")
			Expect(hubClient.Delete(ctx, trafficManagerProfileName)).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("should allow creating API with valid name starting with numeric character", func() {

			// Create the API.
			trafficManagerProfileName := &v1alpha1.TrafficManagerProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameValidStartingWithNumber,
					Namespace: testNamespace,
				},
				Spec: v1alpha1.TrafficManagerProfileSpec{
					MonitorConfig: &v1alpha1.MonitorConfig{
						IntervalInSeconds: ptr.To(int64(30)),
						TimeoutInSeconds:  ptr.To(int64(7)),
					},
				},
			}
			Expect(hubClient.Create(ctx, trafficManagerProfileName)).Should(Succeed(), "failed to create trafficManagerProfile")
			Expect(hubClient.Delete(ctx, trafficManagerProfileName)).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("should allow creating API with valid name ending with alphabet character", func() {

			// Create the API.
			trafficManagerProfileName := &v1alpha1.TrafficManagerProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameValidEndingWithAphabet,
					Namespace: testNamespace,
				},
				Spec: v1alpha1.TrafficManagerProfileSpec{
					MonitorConfig: &v1alpha1.MonitorConfig{
						IntervalInSeconds: ptr.To(int64(30)),
						TimeoutInSeconds:  ptr.To(int64(7)),
					},
				},
			}
			Expect(hubClient.Create(ctx, trafficManagerProfileName)).Should(Succeed(), "failed to create trafficManagerProfile")
			Expect(hubClient.Delete(ctx, trafficManagerProfileName)).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("should allow creating API with valid name ending with numeric character", func() {

			// Create the API.
			trafficManagerProfileName := &v1alpha1.TrafficManagerProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameValidEndingWithNumber,
					Namespace: testNamespace,
				},
				Spec: v1alpha1.TrafficManagerProfileSpec{
					MonitorConfig: &v1alpha1.MonitorConfig{
						IntervalInSeconds: ptr.To(int64(30)),
						TimeoutInSeconds:  ptr.To(int64(7)),
					},
				},
			}
			Expect(hubClient.Create(ctx, trafficManagerProfileName)).Should(Succeed(), "failed to create trafficManagerProfile")
			Expect(hubClient.Delete(ctx, trafficManagerProfileName)).Should(Succeed(), "failed to delete trafficManagerProfile")
		})
	})

	Context("Test TrafficManagerBackend API validation - invalid cases", func() {
		It("should deny creating API with invalid name size", func() {

			// Create the API.
			trafficManagerBackendName := &v1alpha1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameWithInvalidSize,
					Namespace: testNamespace,
				},
				Spec: v1alpha1.TrafficManagerBackendSpec{
					Profile: v1alpha1.TrafficManagerProfileRef{
						Name: "traffic-manager-profile-ref-name",
					},
					Backend: v1alpha1.TrafficManagerBackendRef{
						Name: "traffic-manager-backend-ref-name",
					},
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameWithInvalidSize))
			var err = hubClient.Create(ctx, trafficManagerBackendName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("metadata.name max length is 63"))
		})

		It("should deny creating API with invalid name starting with non-alphanumeric character", func() {

			// Create the API.
			trafficManagerBackendName := &v1alpha1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameStartingWithNonAlphanum,
					Namespace: testNamespace,
				},
				Spec: v1alpha1.TrafficManagerBackendSpec{
					Profile: v1alpha1.TrafficManagerProfileRef{
						Name: "traffic-manager-profile-ref-name",
					},
					Backend: v1alpha1.TrafficManagerBackendRef{
						Name: "traffic-manager-backend-ref-name",
					},
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameStartingWithNonAlphanum))
			var err = hubClient.Create(ctx, trafficManagerBackendName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character"))
		})

		It("should deny creating API with invalid name ending with non-alphanumeric character", func() {

			// Create the API.
			trafficManagerBackendName := &v1alpha1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameEndingWithNonAlphanum,
					Namespace: testNamespace,
				},
				Spec: v1alpha1.TrafficManagerBackendSpec{
					Profile: v1alpha1.TrafficManagerProfileRef{
						Name: "traffic-manager-profile-ref-name",
					},
					Backend: v1alpha1.TrafficManagerBackendRef{
						Name: "traffic-manager-backend-ref-name",
					},
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameEndingWithNonAlphanum))
			var err = hubClient.Create(ctx, trafficManagerBackendName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character"))
		})

		It("should deny creating API with invalid name containing character that is not alphanumeric and not -", func() {

			// Create the API.
			trafficManagerBackendName := &v1alpha1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameContainsUnderscore,
					Namespace: testNamespace,
				},
				Spec: v1alpha1.TrafficManagerBackendSpec{
					Profile: v1alpha1.TrafficManagerProfileRef{
						Name: "traffic-manager-profile-ref-name",
					},
					Backend: v1alpha1.TrafficManagerBackendRef{
						Name: "traffic-manager-backend-ref-name",
					},
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameContainsUnderscore))
			var err = hubClient.Create(ctx, trafficManagerBackendName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("metadata.name max length is 63"))
		})
	})

	Context("Test TrafficManagerBackend API validation - valid cases", func() {
		It("should allow creating API with valid name size", func() {

			// Create the API.
			trafficManagerBackendName := &v1alpha1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameValid,
					Namespace: testNamespace,
				},
				Spec: v1alpha1.TrafficManagerBackendSpec{
					Profile: v1alpha1.TrafficManagerProfileRef{
						Name: "traffic-manager-profile-ref-name",
					},
					Backend: v1alpha1.TrafficManagerBackendRef{
						Name: "traffic-manager-backend-ref-name",
					},
				},
			}
			Expect(hubClient.Create(ctx, trafficManagerBackendName)).Should(Succeed(), "failed to create trafficManagerBackend")
			Expect(hubClient.Delete(ctx, trafficManagerBackendName)).Should(Succeed(), "failed to delete trafficManagerBackend")
		})

		It("should allow creating API with valid name starting with alphabet character", func() {

			// Create the API.
			trafficManagerBackendName := &v1alpha1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameValidStartingWithAphabet,
					Namespace: testNamespace,
				},
				Spec: v1alpha1.TrafficManagerBackendSpec{
					Profile: v1alpha1.TrafficManagerProfileRef{
						Name: "traffic-manager-profile-ref-name",
					},
					Backend: v1alpha1.TrafficManagerBackendRef{
						Name: "traffic-manager-backend-ref-name",
					},
				},
			}
			Expect(hubClient.Create(ctx, trafficManagerBackendName)).Should(Succeed(), "failed to create trafficManagerBackend")
			Expect(hubClient.Delete(ctx, trafficManagerBackendName)).Should(Succeed(), "failed to delete trafficManagerBackend")
		})

		It("should allow creating API with valid name starting with numeric character", func() {

			// Create the API.
			trafficManagerBackendName := &v1alpha1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameValidStartingWithNumber,
					Namespace: testNamespace,
				},
				Spec: v1alpha1.TrafficManagerBackendSpec{
					Profile: v1alpha1.TrafficManagerProfileRef{
						Name: "traffic-manager-profile-ref-name",
					},
					Backend: v1alpha1.TrafficManagerBackendRef{
						Name: "traffic-manager-backend-ref-name",
					},
				},
			}
			Expect(hubClient.Create(ctx, trafficManagerBackendName)).Should(Succeed(), "failed to create trafficManagerBackend")
			Expect(hubClient.Delete(ctx, trafficManagerBackendName)).Should(Succeed(), "failed to delete trafficManagerBackend")
		})

		It("should allow creating API with valid name ending with alphabet character", func() {

			// Create the API.
			trafficManagerBackendName := &v1alpha1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameValidEndingWithAphabet,
					Namespace: testNamespace,
				},
				Spec: v1alpha1.TrafficManagerBackendSpec{
					Profile: v1alpha1.TrafficManagerProfileRef{
						Name: "traffic-manager-profile-ref-name",
					},
					Backend: v1alpha1.TrafficManagerBackendRef{
						Name: "traffic-manager-backend-ref-name",
					},
				},
			}
			Expect(hubClient.Create(ctx, trafficManagerBackendName)).Should(Succeed(), "failed to create trafficManagerBackend")
			Expect(hubClient.Delete(ctx, trafficManagerBackendName)).Should(Succeed(), "failed to delete trafficManagerBackend")
		})

		It("should allow creating API with valid name ending with numeric character", func() {

			// Create the API.
			trafficManagerBackendName := &v1alpha1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nameValidEndingWithNumber,
					Namespace: testNamespace,
				},
				Spec: v1alpha1.TrafficManagerBackendSpec{
					Profile: v1alpha1.TrafficManagerProfileRef{
						Name: "traffic-manager-profile-ref-name",
					},
					Backend: v1alpha1.TrafficManagerBackendRef{
						Name: "traffic-manager-backend-ref-name",
					},
				},
			}
			Expect(hubClient.Create(ctx, trafficManagerBackendName)).Should(Succeed(), "failed to create trafficManagerBackend")
			Expect(hubClient.Delete(ctx, trafficManagerBackendName)).Should(Succeed(), "failed to delete trafficManagerBackend")
		})
	})
})
