/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	"errors"
	"fmt"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	v1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

var _ = Describe("Test networking v1alpha1 API validation", func() {

	Context("Test MultiClusterService API validation - invalid cases", func() {
		It("should deny creating API with invalid name size", func() {
			var name = "abcdef-123456789-123456789-123456789-123456789-123456789-123456789-123456789"

			// Create the API.
			multiClusterServiceName := &v1alpha1.MultiClusterService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
				Spec: v1alpha1.MultiClusterServiceSpec{
					ServiceImport: v1alpha1.ServiceImportRef{
						Name: "service-import-1",
					},
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", name))
			var err = hubClient.Create(ctx, multiClusterServiceName)
			var statusErr *k8serrors.StatusError
			fmt.Print(err)
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("metadata.name max length is 63"))
		})

		It("should deny creating API with invalid name starting with non-alphanumeric character", func() {
			var name = "-abcdef-123456789-123456789-123456789-123456789-123456789"

			// Create the API.
			multiClusterServiceName := &v1alpha1.MultiClusterService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
				Spec: v1alpha1.MultiClusterServiceSpec{
					ServiceImport: v1alpha1.ServiceImportRef{
						Name: "service-import-name",
					},
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", name))
			err := hubClient.Create(ctx, multiClusterServiceName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("a lowercase RFC 1123 subdomain"))
		})

		It("should deny creating API with invalid name ending with non-alphanumeric character", func() {
			var name = "abcdef-123456789-123456789-123456789-123456789-123456789-"

			// Create the API.
			multiClusterServiceName := &v1alpha1.MultiClusterService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
				Spec: v1alpha1.MultiClusterServiceSpec{
					ServiceImport: v1alpha1.ServiceImportRef{
						Name: "service-import-name",
					},
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", name))
			err := hubClient.Create(ctx, multiClusterServiceName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("a lowercase RFC 1123 subdomain"))
		})

		It("should deny creating API with invalid name containing character that is not alphanumeric and not -", func() {
			var name = "a_bcdef-123456789-123456789-123456789-123456789-123456789-123456789-123456789"

			// Create the API.
			multiClusterServiceName := &v1alpha1.MultiClusterService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
				Spec: v1alpha1.MultiClusterServiceSpec{
					ServiceImport: v1alpha1.ServiceImportRef{
						Name: "service-import-name",
					},
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", name))
			err := hubClient.Create(ctx, multiClusterServiceName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("a lowercase RFC 1123 subdomain"))
		})
	})

	Context("Test Member Cluster Service creation API validation - valid cases", func() {
		It("should allow creating API with valid name size", func() {
			var name = "abc-123456789-123456789-123456789-123456789-123456789-123456789"

			// Create the API.
			multiClusterServiceName := &v1alpha1.MultiClusterService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
				Spec: v1alpha1.MultiClusterServiceSpec{
					ServiceImport: v1alpha1.ServiceImportRef{
						Name: "service-import-name",
					},
				},
			}
			Expect(hubClient.Create(ctx, multiClusterServiceName)).Should(Succeed())
			Expect(hubClient.Delete(ctx, multiClusterServiceName)).Should(Succeed())
		})

		It("should allow creating API with valid name starting with alphabet character", func() {
			var name = "abc-123456789"

			// Create the API.
			multiClusterServiceName := &v1alpha1.MultiClusterService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
				Spec: v1alpha1.MultiClusterServiceSpec{
					ServiceImport: v1alpha1.ServiceImportRef{
						Name: "service-import-name",
					},
				},
			}
			Expect(hubClient.Create(ctx, multiClusterServiceName)).Should(Succeed())
			Expect(hubClient.Delete(ctx, multiClusterServiceName)).Should(Succeed())
		})

		It("should allow creating API with valid name starting with numeric character", func() {
			var name = "123-123456789"

			// Create the API.
			multiClusterServiceName := &v1alpha1.MultiClusterService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
				Spec: v1alpha1.MultiClusterServiceSpec{
					ServiceImport: v1alpha1.ServiceImportRef{
						Name: "service-import-name",
					},
				},
			}
			Expect(hubClient.Create(ctx, multiClusterServiceName)).Should(Succeed())
			Expect(hubClient.Delete(ctx, multiClusterServiceName)).Should(Succeed())
		})

		It("should allow creating API with valid name ending with alphabet character", func() {
			var name = "123456789-abc"

			// Create the API.
			multiClusterServiceName := &v1alpha1.MultiClusterService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
				Spec: v1alpha1.MultiClusterServiceSpec{
					ServiceImport: v1alpha1.ServiceImportRef{
						Name: "service-import-name",
					},
				},
			}
			Expect(hubClient.Create(ctx, multiClusterServiceName)).Should(Succeed())
			Expect(hubClient.Delete(ctx, multiClusterServiceName)).Should(Succeed())
		})

		It("should allow creating API with valid name ending with numeric character", func() {
			var name = "123456789-123"

			// Create the API.
			multiClusterServiceName := &v1alpha1.MultiClusterService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
				Spec: v1alpha1.MultiClusterServiceSpec{
					ServiceImport: v1alpha1.ServiceImportRef{
						Name: "service-import-name",
					},
				},
			}
			Expect(hubClient.Create(ctx, multiClusterServiceName)).Should(Succeed())
			Expect(hubClient.Delete(ctx, multiClusterServiceName)).Should(Succeed())
		})
	})

	Context("Test ServiceExport API validation - invalid cases", func() {
		It("should deny creating API with invalid name size", func() {
			var name = "abcdef-123456789-123456789-123456789-123456789-123456789-123456789-123456789"

			// Create the API.
			serviceExportName := &v1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", name))
			var err = hubClient.Create(ctx, serviceExportName)
			fmt.Println("Print test")
			fmt.Println(err)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("metadata.name max length is 63"))
		})

		It("should deny creating API with invalid name starting with non-alphanumeric character", func() {
			var name = "-abcdef-123456789-123456789-123456789-123456789-123456789-123456789"

			// Create the API.
			serviceExportName := &v1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", name))
			var err = hubClient.Create(ctx, serviceExportName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character"))
		})

		It("should deny creating API with invalid name ending with non-alphanumeric character", func() {
			var name = "abcdef-abcdef-123456789-123456789-123456789-123456789-123456789-"

			// Create the API.
			serviceExportName := &v1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", name))
			var err = hubClient.Create(ctx, serviceExportName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character"))
		})

		It("should deny creating API with invalid name containing character that is not alphanumeric and not -", func() {
			var name = "a_bcdef-123456789-123456789-123456789-123456789-123456789-123456789-123456789"

			// Create the API.
			serviceExportName := &v1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", name))
			var err = hubClient.Create(ctx, serviceExportName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character"))
		})
	})

	Context("Test ServiceExport API validation - valid cases", func() {
		It("should allow creating API with valid name size", func() {
			var name = "abc-123456789-123456789-123456789-123456789-123456789-123456789"

			// Create the API.
			serviceExportName := &v1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
			}
			Expect(hubClient.Create(ctx, serviceExportName)).Should(Succeed())
			Expect(hubClient.Delete(ctx, serviceExportName)).Should(Succeed())
		})

		It("should allow creating API with valid name starting with alphabet character", func() {
			var name = "abc-123456789"

			// Create the API.
			serviceExportName := &v1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
			}
			Expect(hubClient.Create(ctx, serviceExportName)).Should(Succeed())
			Expect(hubClient.Delete(ctx, serviceExportName)).Should(Succeed())
		})

		It("should allow creating API with valid name starting with numeric character", func() {
			var name = "123-123456789"

			// Create the API.
			serviceExportName := &v1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
			}
			Expect(hubClient.Create(ctx, serviceExportName)).Should(Succeed())
			Expect(hubClient.Delete(ctx, serviceExportName)).Should(Succeed())
		})

		It("should allow creating API with valid name ending with alphabet character", func() {
			var name = "123456789-abc"

			// Create the API.
			serviceExportName := &v1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
			}
			Expect(hubClient.Create(ctx, serviceExportName)).Should(Succeed())
			Expect(hubClient.Delete(ctx, serviceExportName)).Should(Succeed())
		})

		It("should allow creating API with valid name ending with numeric character", func() {
			var name = "123456789-123"

			// Create the API.
			serviceExportName := &v1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
			}
			Expect(hubClient.Create(ctx, serviceExportName)).Should(Succeed())
			Expect(hubClient.Delete(ctx, serviceExportName)).Should(Succeed())
		})
	})

	Context("Test ServiceImport API validation - invalid cases", func() {
		It("should deny creating API with invalid name size", func() {
			var name = "abcdef-123456789-123456789-123456789-123456789-123456789-123456789-123456789"

			// Create the API.
			serviceImportName := &v1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", name))
			var err = hubClient.Create(ctx, serviceImportName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("metadata.name max length is 63"))
		})

		It("should deny creating API with invalid name starting with non-alphanumeric character", func() {
			var name = "-abcdef-123456789-123456789-123456789-123456789-123456789"

			// Create the API.
			serviceImportName := &v1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", name))
			var err = hubClient.Create(ctx, serviceImportName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character"))
		})

		It("should deny creating API with invalid name ending with non-alphanumeric character", func() {
			var name = "abcdef-123456789-123456789-123456789-123456789-123456789-"

			// Create the API.
			serviceImportName := &v1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", name))
			var err = hubClient.Create(ctx, serviceImportName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character"))
		})

		It("should deny creating API with invalid name containing character that is not alphanumeric and not -", func() {
			var name = "a_bcdef-123456789-123456789-123456789-123456789-123456789-123456789-123456789"

			// Create the API.
			serviceImportName := &v1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", name))
			var err = hubClient.Create(ctx, serviceImportName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("metadata.name max length is 63"))
		})
	})

	Context("Test ServiceImport API validation - valid cases", func() {
		It("should allow creating API with valid name size", func() {
			var name = "abc-123456789-123456789-123456789-123456789-123456789-123456789"

			// Create the API.
			serviceImportName := &v1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
			}
			Expect(hubClient.Create(ctx, serviceImportName)).Should(Succeed())
			Expect(hubClient.Delete(ctx, serviceImportName)).Should(Succeed())
		})

		It("should allow creating API with valid name starting with alphabet character", func() {
			var name = "abc-123456789"

			// Create the API.
			serviceImportName := &v1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
			}
			Expect(hubClient.Create(ctx, serviceImportName)).Should(Succeed())
			Expect(hubClient.Delete(ctx, serviceImportName)).Should(Succeed())
		})

		It("should allow creating API with valid name starting with numeric character", func() {
			var name = "123-123456789"

			// Create the API.
			serviceImportName := &v1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
			}
			Expect(hubClient.Create(ctx, serviceImportName)).Should(Succeed())
			Expect(hubClient.Delete(ctx, serviceImportName)).Should(Succeed())
		})

		It("should allow creating API with valid name ending with alphabet character", func() {
			var name = "123456789-abc"

			// Create the API.
			serviceImportName := &v1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
			}
			Expect(hubClient.Create(ctx, serviceImportName)).Should(Succeed())
			Expect(hubClient.Delete(ctx, serviceImportName)).Should(Succeed())
		})

		It("should allow creating API with valid name ending with numeric character", func() {
			var name = "123456789-123"

			// Create the API.
			serviceImportName := &v1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
			}
			Expect(hubClient.Create(ctx, serviceImportName)).Should(Succeed())
			Expect(hubClient.Delete(ctx, serviceImportName)).Should(Succeed())
		})
	})

	Context("Test TrafficManagerProfile API validation - invalid cases", func() {
		It("should deny creating API with invalid name size", func() {
			var name = "abcdef-123456789-123456789-123456789-123456789-123456789-123456789-123456789"

			// Create the API.
			trafficManagerProfileName := &v1alpha1.TrafficManagerProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
				Spec: v1alpha1.TrafficManagerProfileSpec{
					MonitorConfig: &v1alpha1.MonitorConfig{
						IntervalInSeconds: ptr.To(int64(30)),
						TimeoutInSeconds:  ptr.To(int64(7)),
					},
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", name))
			var err = hubClient.Create(ctx, trafficManagerProfileName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("metadata.name max length is 63"))
		})

		It("should deny creating API with invalid name starting with non-alphanumeric character", func() {
			var name = "-abcdef-123456789-123456789-123456789-123456789-123456789"

			// Create the API.
			trafficManagerProfileName := &v1alpha1.TrafficManagerProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
				Spec: v1alpha1.TrafficManagerProfileSpec{
					MonitorConfig: &v1alpha1.MonitorConfig{
						IntervalInSeconds: ptr.To(int64(30)),
						TimeoutInSeconds:  ptr.To(int64(7)),
					},
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", name))
			var err = hubClient.Create(ctx, trafficManagerProfileName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character"))
		})

		It("should deny creating API with invalid name ending with non-alphanumeric character", func() {
			var name = "abcdef-123456789-123456789-123456789-123456789-123456789-"

			// Create the API.
			trafficManagerProfileName := &v1alpha1.TrafficManagerProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
				Spec: v1alpha1.TrafficManagerProfileSpec{
					MonitorConfig: &v1alpha1.MonitorConfig{
						IntervalInSeconds: ptr.To(int64(30)),
						TimeoutInSeconds:  ptr.To(int64(7)),
					},
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", name))
			var err = hubClient.Create(ctx, trafficManagerProfileName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character"))
		})

		It("should deny creating API with invalid name containing character that is not alphanumeric and not -", func() {
			var name = "a_bcdef-123456789-123456789-123456789-123456789-123456789-123456789-123456789"

			// Create the API.
			trafficManagerProfileName := &v1alpha1.TrafficManagerProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
				Spec: v1alpha1.TrafficManagerProfileSpec{
					MonitorConfig: &v1alpha1.MonitorConfig{
						IntervalInSeconds: ptr.To(int64(30)),
						TimeoutInSeconds:  ptr.To(int64(7)),
					},
				},
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", name))
			var err = hubClient.Create(ctx, trafficManagerProfileName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("metadata.name max length is 63"))
		})
	})

	Context("Test TrafficManagerProfile API validation - valid cases", func() {
		It("should allow creating API with valid name size", func() {
			var name = "abc-123456789-123456789-123456789-123456789-123456789-123456789"

			// Create the API.
			trafficManagerProfileName := &v1alpha1.TrafficManagerProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
				Spec: v1alpha1.TrafficManagerProfileSpec{
					MonitorConfig: &v1alpha1.MonitorConfig{
						IntervalInSeconds: ptr.To(int64(30)),
						TimeoutInSeconds:  ptr.To(int64(7)),
					},
				},
			}
			Expect(hubClient.Create(ctx, trafficManagerProfileName)).Should(Succeed())
			Expect(hubClient.Delete(ctx, trafficManagerProfileName)).Should(Succeed())
		})

		It("should allow creating API with valid name starting with alphabet character", func() {
			var name = "abc-123456789"

			// Create the API.
			trafficManagerProfileName := &v1alpha1.TrafficManagerProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
				Spec: v1alpha1.TrafficManagerProfileSpec{
					MonitorConfig: &v1alpha1.MonitorConfig{
						IntervalInSeconds: ptr.To(int64(30)),
						TimeoutInSeconds:  ptr.To(int64(7)),
					},
				},
			}
			Expect(hubClient.Create(ctx, trafficManagerProfileName)).Should(Succeed())
			Expect(hubClient.Delete(ctx, trafficManagerProfileName)).Should(Succeed())
		})

		It("should allow creating API with valid name starting with numeric character", func() {
			var name = "123-123456789"

			// Create the API.
			trafficManagerProfileName := &v1alpha1.TrafficManagerProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
				Spec: v1alpha1.TrafficManagerProfileSpec{
					MonitorConfig: &v1alpha1.MonitorConfig{
						IntervalInSeconds: ptr.To(int64(30)),
						TimeoutInSeconds:  ptr.To(int64(7)),
					},
				},
			}
			Expect(hubClient.Create(ctx, trafficManagerProfileName)).Should(Succeed())
			Expect(hubClient.Delete(ctx, trafficManagerProfileName)).Should(Succeed())
		})

		It("should allow creating API with valid name ending with alphabet character", func() {
			var name = "123456789-abc"

			// Create the API.
			trafficManagerProfileName := &v1alpha1.TrafficManagerProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
				Spec: v1alpha1.TrafficManagerProfileSpec{
					MonitorConfig: &v1alpha1.MonitorConfig{
						IntervalInSeconds: ptr.To(int64(30)),
						TimeoutInSeconds:  ptr.To(int64(7)),
					},
				},
			}
			Expect(hubClient.Create(ctx, trafficManagerProfileName)).Should(Succeed())
			Expect(hubClient.Delete(ctx, trafficManagerProfileName)).Should(Succeed())
		})

		It("should allow creating API with valid name ending with numeric character", func() {
			var name = "123456789-123"

			// Create the API.
			trafficManagerProfileName := &v1alpha1.TrafficManagerProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
				},
				Spec: v1alpha1.TrafficManagerProfileSpec{
					MonitorConfig: &v1alpha1.MonitorConfig{
						IntervalInSeconds: ptr.To(int64(30)),
						TimeoutInSeconds:  ptr.To(int64(7)),
					},
				},
			}
			Expect(hubClient.Create(ctx, trafficManagerProfileName)).Should(Succeed())
			Expect(hubClient.Delete(ctx, trafficManagerProfileName)).Should(Succeed())
		})
	})

	Context("Test TrafficManagerBackend API validation - invalid cases", func() {
		It("should deny creating API with invalid name size", func() {
			var name = "abcdef-123456789-123456789-123456789-123456789-123456789-123456789-123456789"

			// Create the API.
			trafficManagerBackendName := &v1alpha1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
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
			By(fmt.Sprintf("expecting denial of CREATE API %s", name))
			var err = hubClient.Create(ctx, trafficManagerBackendName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("metadata.name max length is 63"))
		})

		It("should deny creating API with invalid name starting with non-alphanumeric character", func() {
			var name = "-abcdef-123456789-123456789-123456789-123456789-123456789"

			// Create the API.
			trafficManagerBackendName := &v1alpha1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
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
			By(fmt.Sprintf("expecting denial of CREATE API %s", name))
			var err = hubClient.Create(ctx, trafficManagerBackendName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character"))
		})

		It("should deny creating API with invalid name ending with non-alphanumeric character", func() {
			var name = "abcdef-123456789-123456789-123456789-123456789-123456789-"

			// Create the API.
			trafficManagerBackendName := &v1alpha1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
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
			By(fmt.Sprintf("expecting denial of CREATE API %s", name))
			var err = hubClient.Create(ctx, trafficManagerBackendName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character"))
		})

		It("should deny creating API with invalid name containing character that is not alphanumeric and not -", func() {
			var name = "a_bcdef-123456789-123456789-123456789-123456789-123456789-123456789-123456789"

			// Create the API.
			trafficManagerBackendName := &v1alpha1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
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
			By(fmt.Sprintf("expecting denial of CREATE API %s", name))
			var err = hubClient.Create(ctx, trafficManagerBackendName)
			var statusErr *k8serrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("metadata.name max length is 63"))
		})
	})

	Context("Test TrafficManagerBackend API validation - valid cases", func() {
		It("should allow creating API with valid name size", func() {
			var name = "abc-123456789-123456789-123456789-123456789-123456789-123456789"

			// Create the API.
			trafficManagerBackendName := &v1alpha1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
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
			Expect(hubClient.Create(ctx, trafficManagerBackendName)).Should(Succeed())
			Expect(hubClient.Delete(ctx, trafficManagerBackendName)).Should(Succeed())
		})

		It("should allow creating API with valid name starting with alphabet character", func() {
			var name = "abc-123456789"

			// Create the API.
			trafficManagerBackendName := &v1alpha1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
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
			Expect(hubClient.Create(ctx, trafficManagerBackendName)).Should(Succeed())
			Expect(hubClient.Delete(ctx, trafficManagerBackendName)).Should(Succeed())
		})

		It("should allow creating API with valid name starting with numeric character", func() {
			var name = "123-123456789"

			// Create the API.
			trafficManagerBackendName := &v1alpha1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
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
			Expect(hubClient.Create(ctx, trafficManagerBackendName)).Should(Succeed())
			Expect(hubClient.Delete(ctx, trafficManagerBackendName)).Should(Succeed())
		})

		It("should allow creating API with valid name ending with alphabet character", func() {
			var name = "123456789-abc"

			// Create the API.
			trafficManagerBackendName := &v1alpha1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
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
			Expect(hubClient.Create(ctx, trafficManagerBackendName)).Should(Succeed())
			Expect(hubClient.Delete(ctx, trafficManagerBackendName)).Should(Succeed())
		})

		It("should allow creating API with valid name ending with numeric character", func() {
			var name = "123456789-123"

			// Create the API.
			trafficManagerBackendName := &v1alpha1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "testnamespace",
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
			Expect(hubClient.Create(ctx, trafficManagerBackendName)).Should(Succeed())
			Expect(hubClient.Delete(ctx, trafficManagerBackendName)).Should(Succeed())
		})
	})
})
