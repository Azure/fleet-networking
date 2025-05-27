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

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/uniquename"
)

var (
	nameContainsUnderscore        = "a_bcdef-123456789-123456789-123456789-123456789-123456789-123456789-123456789"
	nameEndingWithNonAlphanum     = "abcdef-123456789-123456789-123456789-123456789-123456789-"
	nameStartingWithNonAlphanum   = "-abcdef-123456789-123456789-123456789-123456789-123456789"
	nameValid                     = "abc-123456789-123456789-123456789-123456789-123456789-123456789"
	nameValidEndingWithAlphabet   = "123456789-abc"
	nameValidEndingWithNumber     = "123456789-123"
	nameValidStartingWithAlphabet = "abc-123456789"
	nameValidStartingWithNumber   = "123-123456789"
	nameWithInvalidSize           = "abcdef-123456789-123456789-123456789-123456789-123456789-123456789-123456789"
)

var _ = Describe("Test networking v1alpha1 API validation", func() {
	var statusErr *k8serrors.StatusError
	var multiClusterServiceSpec = fleetnetv1alpha1.MultiClusterServiceSpec{
		ServiceImport: fleetnetv1alpha1.ServiceImportRef{
			Name: "service-import-1",
		},
	}
	var trafficManagerProfileSpec = fleetnetv1alpha1.TrafficManagerProfileSpec{
		MonitorConfig: &fleetnetv1alpha1.MonitorConfig{
			IntervalInSeconds: ptr.To(int64(30)),
			TimeoutInSeconds:  ptr.To(int64(7)),
		},
		ResourceGroup: "test-resource-group",
	}
	var trafficManagerBackendSpec = fleetnetv1alpha1.TrafficManagerBackendSpec{
		Profile: fleetnetv1alpha1.TrafficManagerProfileRef{
			Name: "traffic-manager-profile-ref-name",
		},
		Backend: fleetnetv1alpha1.TrafficManagerBackendRef{
			Name: "traffic-manager-backend-ref-name",
		},
	}
	var objectMetaWithNameSizeInvalid = metav1.ObjectMeta{
		Name:      nameWithInvalidSize,
		Namespace: testNamespace,
	}
	var objectMetaWithNameStartingNonAlphanum = metav1.ObjectMeta{
		Name:      nameStartingWithNonAlphanum,
		Namespace: testNamespace,
	}
	var objectMetaWithNameEndingNonAlphanum = metav1.ObjectMeta{
		Name:      nameEndingWithNonAlphanum,
		Namespace: testNamespace,
	}
	var objectMetaWithNameContainingUnderscore = metav1.ObjectMeta{
		Name:      nameContainsUnderscore,
		Namespace: testNamespace,
	}
	var objectMetaWithNameValid = metav1.ObjectMeta{
		Name:      nameValid,
		Namespace: testNamespace,
	}
	var objectMetaWithValidNameStartingAlphabet = metav1.ObjectMeta{
		Name:      nameValidStartingWithAlphabet,
		Namespace: testNamespace,
	}
	var objectMetaWithValidNameStartingNumber = metav1.ObjectMeta{
		Name:      nameValidStartingWithNumber,
		Namespace: testNamespace,
	}
	var objectMetaWithValidNameEndingAlphabet = metav1.ObjectMeta{
		Name:      nameValidEndingWithAlphabet,
		Namespace: testNamespace,
	}
	var objectMetaWithValidNameEndingNumber = metav1.ObjectMeta{
		Name:      nameValidEndingWithNumber,
		Namespace: testNamespace,
	}

	Context("Test MultiClusterService API validation - invalid cases", func() {
		It("should deny creating API with invalid name size", func() {
			// Create the API.
			multiClusterServiceName := &fleetnetv1alpha1.MultiClusterService{
				ObjectMeta: objectMetaWithNameSizeInvalid,
				Spec:       multiClusterServiceSpec,
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameWithInvalidSize))
			var err = hubClient.Create(ctx, multiClusterServiceName)
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("metadata.name max length is 63"))
		})

		It("should deny creating API with invalid name starting with non-alphanumeric character", func() {
			// Create the API.
			multiClusterServiceName := &fleetnetv1alpha1.MultiClusterService{
				ObjectMeta: objectMetaWithNameStartingNonAlphanum,
				Spec:       multiClusterServiceSpec,
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameStartingWithNonAlphanum))
			err := hubClient.Create(ctx, multiClusterServiceName)
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("a lowercase RFC 1123 subdomain"))
		})

		It("should deny creating API with invalid name ending with non-alphanumeric character", func() {
			// Create the API.
			multiClusterServiceName := &fleetnetv1alpha1.MultiClusterService{
				ObjectMeta: objectMetaWithNameEndingNonAlphanum,
				Spec:       multiClusterServiceSpec,
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameEndingWithNonAlphanum))
			err := hubClient.Create(ctx, multiClusterServiceName)
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("a lowercase RFC 1123 subdomain"))
		})

		It("should deny creating API with invalid name containing character that is not alphanumeric and not -", func() {
			// Create the API.
			multiClusterServiceName := &fleetnetv1alpha1.MultiClusterService{
				ObjectMeta: objectMetaWithNameContainingUnderscore,
				Spec:       multiClusterServiceSpec,
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameContainsUnderscore))
			err := hubClient.Create(ctx, multiClusterServiceName)
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("a lowercase RFC 1123 subdomain"))
		})
	})

	Context("Test MultiClusterService creation API validation - valid cases", func() {
		It("should allow creating API with valid name size", func() {
			// Create the API.
			multiClusterServiceName := &fleetnetv1alpha1.MultiClusterService{
				ObjectMeta: objectMetaWithNameValid,
				Spec:       multiClusterServiceSpec,
			}
			Expect(hubClient.Create(ctx, multiClusterServiceName)).Should(Succeed(), "failed to create multiClusterService")
			Expect(hubClient.Delete(ctx, multiClusterServiceName)).Should(Succeed(), "failed to delete multiClusterService")
		})

		It("should allow creating API with valid name starting with alphabet character", func() {
			// Create the API.
			multiClusterServiceName := &fleetnetv1alpha1.MultiClusterService{
				ObjectMeta: objectMetaWithValidNameStartingAlphabet,
				Spec:       multiClusterServiceSpec,
			}
			Expect(hubClient.Create(ctx, multiClusterServiceName)).Should(Succeed(), "failed to create multiClusterService")
			Expect(hubClient.Delete(ctx, multiClusterServiceName)).Should(Succeed(), "failed to delete multiClusterService")
		})

		It("should allow creating API with valid name starting with numeric character", func() {
			// Create the API.
			multiClusterServiceName := &fleetnetv1alpha1.MultiClusterService{
				ObjectMeta: objectMetaWithValidNameStartingNumber,
				Spec:       multiClusterServiceSpec,
			}
			Expect(hubClient.Create(ctx, multiClusterServiceName)).Should(Succeed(), "failed to create multiClusterService")
			Expect(hubClient.Delete(ctx, multiClusterServiceName)).Should(Succeed(), "failed to delete multiClusterService")
		})

		It("should allow creating API with valid name ending with alphabet character", func() {
			// Create the API.
			multiClusterServiceName := &fleetnetv1alpha1.MultiClusterService{
				ObjectMeta: objectMetaWithValidNameEndingAlphabet,
				Spec:       multiClusterServiceSpec,
			}
			Expect(hubClient.Create(ctx, multiClusterServiceName)).Should(Succeed(), "failed to create multiClusterService")
			Expect(hubClient.Delete(ctx, multiClusterServiceName)).Should(Succeed(), "failed to delete multiClusterService")
		})

		It("should allow creating API with valid name ending with numeric character", func() {
			// Create the API.
			multiClusterServiceName := &fleetnetv1alpha1.MultiClusterService{
				ObjectMeta: objectMetaWithValidNameEndingNumber,
				Spec:       multiClusterServiceSpec,
			}
			Expect(hubClient.Create(ctx, multiClusterServiceName)).Should(Succeed(), "failed to create multiClusterService")
			Expect(hubClient.Delete(ctx, multiClusterServiceName)).Should(Succeed(), "failed to delete multiClusterService")
		})
	})

	Context("Test ServiceExport API validation - invalid cases", func() {
		It("should deny creating API with invalid name size", func() {
			// Create the API.
			serviceExportName := &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: objectMetaWithNameSizeInvalid,
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameWithInvalidSize))
			var err = hubClient.Create(ctx, serviceExportName)
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("metadata.name max length is 63"))
		})

		It("should deny creating API with invalid name starting with non-alphanumeric character", func() {
			// Create the API.
			serviceExportName := &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: objectMetaWithNameStartingNonAlphanum,
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameStartingWithNonAlphanum))
			var err = hubClient.Create(ctx, serviceExportName)
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character"))
		})

		It("should deny creating API with invalid name ending with non-alphanumeric character", func() {
			// Create the API.
			serviceExportName := &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: objectMetaWithNameEndingNonAlphanum,
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameEndingWithNonAlphanum))
			var err = hubClient.Create(ctx, serviceExportName)
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character"))
		})

		It("should deny creating API with invalid name containing character that is not alphanumeric and not -", func() {
			// Create the API.
			serviceExportName := &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: objectMetaWithNameContainingUnderscore,
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameContainsUnderscore))
			var err = hubClient.Create(ctx, serviceExportName)
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character"))
		})
	})

	Context("Test ServiceExport API validation - valid cases", func() {
		It("should allow creating API with valid name size", func() {
			// Create the API.
			serviceExportName := &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: objectMetaWithNameValid,
			}
			Expect(hubClient.Create(ctx, serviceExportName)).Should(Succeed(), "failed to create serviceExport")
			Expect(hubClient.Delete(ctx, serviceExportName)).Should(Succeed(), "failed to delete serviceExport")
		})

		It("should allow creating API with valid name starting with alphabet character", func() {
			// Create the API.
			serviceExportName := &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: objectMetaWithValidNameStartingAlphabet,
			}
			Expect(hubClient.Create(ctx, serviceExportName)).Should(Succeed(), "failed to create serviceExport")
			Expect(hubClient.Delete(ctx, serviceExportName)).Should(Succeed(), "failed to delete serviceExport")
		})

		It("should allow creating API with valid name starting with numeric character", func() {
			// Create the API.
			serviceExportName := &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: objectMetaWithValidNameStartingNumber,
			}
			Expect(hubClient.Create(ctx, serviceExportName)).Should(Succeed(), "failed to create serviceExport")
			Expect(hubClient.Delete(ctx, serviceExportName)).Should(Succeed(), "failed to delete serviceExport")
		})

		It("should allow creating API with valid name ending with alphabet character", func() {
			// Create the API.
			serviceExportName := &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: objectMetaWithValidNameEndingAlphabet,
			}
			Expect(hubClient.Create(ctx, serviceExportName)).Should(Succeed(), "failed to create serviceExport")
			Expect(hubClient.Delete(ctx, serviceExportName)).Should(Succeed(), "failed to delete serviceExport")
		})

		It("should allow creating API with valid name ending with numeric character", func() {
			// Create the API.
			serviceExportName := &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: objectMetaWithValidNameEndingNumber,
			}
			Expect(hubClient.Create(ctx, serviceExportName)).Should(Succeed(), "failed to create serviceExport")
			Expect(hubClient.Delete(ctx, serviceExportName)).Should(Succeed(), "failed to delete serviceExport")
		})
	})

	Context("Test ServiceImport API validation - invalid cases", func() {
		It("should deny creating API with invalid name size", func() {
			// Create the API.
			serviceImportName := &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: objectMetaWithNameSizeInvalid,
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameWithInvalidSize))
			var err = hubClient.Create(ctx, serviceImportName)
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("metadata.name max length is 63"))
		})

		It("should deny creating API with invalid name starting with non-alphanumeric character", func() {
			// Create the API.
			serviceImportName := &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: objectMetaWithNameStartingNonAlphanum,
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameStartingWithNonAlphanum))
			var err = hubClient.Create(ctx, serviceImportName)
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character"))
		})

		It("should deny creating API with invalid name ending with non-alphanumeric character", func() {
			// Create the API.
			serviceImportName := &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: objectMetaWithNameEndingNonAlphanum,
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameEndingWithNonAlphanum))
			var err = hubClient.Create(ctx, serviceImportName)
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character"))
		})

		It("should deny creating API with invalid name containing character that is not alphanumeric and not -", func() {
			// Create the API.
			serviceImportName := &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: objectMetaWithNameContainingUnderscore,
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameContainsUnderscore))
			var err = hubClient.Create(ctx, serviceImportName)
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("metadata.name max length is 63"))
		})
	})

	Context("Test ServiceImport API validation - valid cases", func() {
		It("should allow creating API with valid name size", func() {
			// Create the API.
			serviceImportName := &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: objectMetaWithNameValid,
			}
			Expect(hubClient.Create(ctx, serviceImportName)).Should(Succeed(), "failed to create serviceImport")
			Expect(hubClient.Delete(ctx, serviceImportName)).Should(Succeed(), "failed to delete serviceImport")
		})

		It("should allow creating API with valid name starting with alphabet character", func() {
			// Create the API.
			serviceImportName := &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: objectMetaWithValidNameStartingAlphabet,
			}
			Expect(hubClient.Create(ctx, serviceImportName)).Should(Succeed(), "failed to create serviceImport")
			Expect(hubClient.Delete(ctx, serviceImportName)).Should(Succeed(), "failed to delete serviceImport")
		})

		It("should allow creating API with valid name starting with numeric character", func() {
			// Create the API.
			serviceImportName := &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: objectMetaWithValidNameStartingNumber,
			}
			Expect(hubClient.Create(ctx, serviceImportName)).Should(Succeed(), "failed to create serviceImport")
			Expect(hubClient.Delete(ctx, serviceImportName)).Should(Succeed(), "failed to delete serviceImport")
		})

		It("should allow creating API with valid name ending with alphabet character", func() {
			// Create the API.
			serviceImportName := &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: objectMetaWithValidNameEndingAlphabet,
			}
			Expect(hubClient.Create(ctx, serviceImportName)).Should(Succeed(), "failed to create serviceImport")
			Expect(hubClient.Delete(ctx, serviceImportName)).Should(Succeed(), "failed to delete serviceImport")
		})

		It("should allow creating API with valid name ending with numeric character", func() {
			// Create the API.
			serviceImportName := &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: objectMetaWithValidNameEndingNumber,
			}
			Expect(hubClient.Create(ctx, serviceImportName)).Should(Succeed(), "failed to create serviceImport")
			Expect(hubClient.Delete(ctx, serviceImportName)).Should(Succeed(), "failed to delete serviceImport")
		})
	})

	Context("Test TrafficManagerProfile API validation - invalid cases", func() {
		It("should deny creating API with invalid name size", func() {
			// Create the API.
			trafficManagerProfileName := &fleetnetv1alpha1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithNameSizeInvalid,
				Spec:       trafficManagerProfileSpec,
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameWithInvalidSize))
			var err = hubClient.Create(ctx, trafficManagerProfileName)
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("metadata.name max length is 63"))
		})

		It("should deny creating API with invalid name starting with non-alphanumeric character", func() {
			// Create the API.
			trafficManagerProfileName := &fleetnetv1alpha1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithNameStartingNonAlphanum,
				Spec:       trafficManagerProfileSpec,
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameStartingWithNonAlphanum))
			var err = hubClient.Create(ctx, trafficManagerProfileName)
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character"))
		})

		It("should deny creating API with invalid name ending with non-alphanumeric character", func() {
			// Create the API.
			trafficManagerProfileName := &fleetnetv1alpha1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithNameEndingNonAlphanum,
				Spec:       trafficManagerProfileSpec,
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameEndingWithNonAlphanum))
			var err = hubClient.Create(ctx, trafficManagerProfileName)
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character"))
		})

		It("should deny creating API with invalid name containing character that is not alphanumeric and not -", func() {
			// Create the API.
			trafficManagerProfileName := &fleetnetv1alpha1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithNameContainingUnderscore,
				Spec:       trafficManagerProfileSpec,
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameContainsUnderscore))
			var err = hubClient.Create(ctx, trafficManagerProfileName)
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("metadata.name max length is 63"))
		})

		It("should deny creating API with empty resourceGroup", func() {
			// Create the API.
			profile := &fleetnetv1alpha1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithNameValid,
				Spec: fleetnetv1alpha1.TrafficManagerProfileSpec{
					MonitorConfig: trafficManagerProfileSpec.MonitorConfig,
				},
			}
			By("expecting denial of CREATE API with empty resourceGroup")
			var err = hubClient.Create(ctx, profile)
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("spec.resourceGroup in body should be at least 1 chars long"))
		})

		It("should deny creating API with invalid resourceGroup (length > 90)", func() {
			// Create the API.
			profile := &fleetnetv1alpha1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithNameValid,
				Spec: fleetnetv1alpha1.TrafficManagerProfileSpec{
					MonitorConfig: trafficManagerProfileSpec.MonitorConfig,
					ResourceGroup: uniquename.RandomLowerCaseAlphabeticString(91),
				},
			}
			By("expecting denial of CREATE API with resourceGroup (length > 90)")
			var err = hubClient.Create(ctx, profile)
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("spec.resourceGroup: Too long: may not be longer than 90"))
		})

		It("should deny update of resourceGroup", func() {
			// Create the API.
			profile := &fleetnetv1alpha1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithNameValid,
				Spec:       trafficManagerProfileSpec,
			}
			Expect(hubClient.Create(ctx, profile)).Should(Succeed(), "failed to create trafficManagerProfile")
			profile.Spec.ResourceGroup = "new-resource-group"
			By("expecting denial of UPDATE API with resourceGroup")
			var err = hubClient.Update(ctx, profile)
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Update API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("resourceGroup is immutable"))
			Expect(hubClient.Delete(ctx, profile)).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("should deny creating API with duplicate customHeaders", func() {
			// Create the API.
			trafficManagerProfileWithDuplicateHeaders := &fleetnetv1alpha1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithNameValid,
				Spec: fleetnetv1alpha1.TrafficManagerProfileSpec{
					MonitorConfig: &fleetnetv1alpha1.MonitorConfig{
						CustomHeaders: []fleetnetv1alpha1.MonitorConfigCustomHeader{
							{
								Name:  "Header1",
								Value: "Value1",
							},
							{
								Name:  "Header1", // Duplicate header name
								Value: "Value2",
							},
						},
					},
					ResourceGroup: trafficManagerProfileSpec.ResourceGroup,
				},
			}
			By("expecting denial of CREATE API with duplicate customHeaders")
			var err = hubClient.Create(ctx, trafficManagerProfileWithDuplicateHeaders)
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("Duplicate value: map[string]interface {}{\"name\":\"Header1\""))
		})

		It("should deny creating API with empty customHeader name", func() {
			// Create the API.
			trafficManagerProfileWithDuplicateHeaders := &fleetnetv1alpha1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithNameValid,
				Spec: fleetnetv1alpha1.TrafficManagerProfileSpec{
					MonitorConfig: &fleetnetv1alpha1.MonitorConfig{
						CustomHeaders: []fleetnetv1alpha1.MonitorConfigCustomHeader{
							{
								Value: "Value1",
							},
						},
					},
					ResourceGroup: trafficManagerProfileSpec.ResourceGroup,
				},
			}
			By("expecting denial of CREATE API with duplicate customHeaders")
			var err = hubClient.Create(ctx, trafficManagerProfileWithDuplicateHeaders)
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("spec.monitorConfig.customHeaders[0].name in body should be at least 1 chars long"))
		})

		It("should deny creating API with empty customHeader value", func() {
			// Create the API.
			trafficManagerProfileWithDuplicateHeaders := &fleetnetv1alpha1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithNameValid,
				Spec: fleetnetv1alpha1.TrafficManagerProfileSpec{
					MonitorConfig: &fleetnetv1alpha1.MonitorConfig{
						CustomHeaders: []fleetnetv1alpha1.MonitorConfigCustomHeader{
							{
								Name: "Header1",
							},
						},
					},
					ResourceGroup: trafficManagerProfileSpec.ResourceGroup,
				},
			}
			By("expecting denial of CREATE API with duplicate customHeaders")
			var err = hubClient.Create(ctx, trafficManagerProfileWithDuplicateHeaders)
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("spec.monitorConfig.customHeaders[0].value in body should be at least 1 chars long"))
		})

		It("should deny creating API with 9 headers", func() {
			// Create the API.
			var headers []fleetnetv1alpha1.MonitorConfigCustomHeader
			for i := range 9 {
				headers = append(headers, fleetnetv1alpha1.MonitorConfigCustomHeader{
					Name:  fmt.Sprintf("Header%d", i),
					Value: fmt.Sprintf("Value%d", i),
				})
			}
			trafficManagerProfileWithDuplicateHeaders := &fleetnetv1alpha1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithNameValid,
				Spec: fleetnetv1alpha1.TrafficManagerProfileSpec{
					MonitorConfig: &fleetnetv1alpha1.MonitorConfig{
						CustomHeaders: headers,
					},
					ResourceGroup: trafficManagerProfileSpec.ResourceGroup,
				},
			}
			By("expecting denial of CREATE API with duplicate customHeaders")
			var err = hubClient.Create(ctx, trafficManagerProfileWithDuplicateHeaders)
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("spec.monitorConfig.customHeaders: Too many: 9: must have at most 8 items,"))
		})
	})

	Context("Test TrafficManagerProfile API validation - valid cases", func() {
		It("should allow creating API with valid name size", func() {
			// Create the API.
			trafficManagerProfileName := &fleetnetv1alpha1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithNameValid,
				Spec:       trafficManagerProfileSpec,
			}
			Expect(hubClient.Create(ctx, trafficManagerProfileName)).Should(Succeed(), "failed to create trafficManagerProfile")
			Expect(hubClient.Delete(ctx, trafficManagerProfileName)).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("should allow creating API with valid name starting with alphabet character", func() {
			// Create the API.
			trafficManagerProfileName := &fleetnetv1alpha1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithValidNameStartingAlphabet,
				Spec:       trafficManagerProfileSpec,
			}
			Expect(hubClient.Create(ctx, trafficManagerProfileName)).Should(Succeed(), "failed to create trafficManagerProfile")
			Expect(hubClient.Delete(ctx, trafficManagerProfileName)).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("should allow creating API with valid name starting with numeric character", func() {
			// Create the API.
			trafficManagerProfileName := &fleetnetv1alpha1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithValidNameStartingNumber,
				Spec:       trafficManagerProfileSpec,
			}
			Expect(hubClient.Create(ctx, trafficManagerProfileName)).Should(Succeed(), "failed to create trafficManagerProfile")
			Expect(hubClient.Delete(ctx, trafficManagerProfileName)).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("should allow creating API with valid name ending with alphabet character", func() {
			// Create the API.
			trafficManagerProfileName := &fleetnetv1alpha1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithValidNameEndingAlphabet,
				Spec:       trafficManagerProfileSpec,
			}
			Expect(hubClient.Create(ctx, trafficManagerProfileName)).Should(Succeed(), "failed to create trafficManagerProfile")
			Expect(hubClient.Delete(ctx, trafficManagerProfileName)).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("should allow creating API with valid name ending with numeric character", func() {
			// Create the API.
			trafficManagerProfileName := &fleetnetv1alpha1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithValidNameEndingNumber,
				Spec:       trafficManagerProfileSpec,
			}
			Expect(hubClient.Create(ctx, trafficManagerProfileName)).Should(Succeed(), "failed to create trafficManagerProfile")
			Expect(hubClient.Delete(ctx, trafficManagerProfileName)).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("should allow creating API with valid custom headers", func() {
			// Create the API.
			trafficManagerProfileName := &fleetnetv1alpha1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithNameValid,
				Spec:       trafficManagerProfileSpec,
			}
			trafficManagerProfileName.Spec.MonitorConfig.CustomHeaders = []fleetnetv1alpha1.MonitorConfigCustomHeader{
				{
					Name:  "Header1",
					Value: "Value1",
				},
				{
					Name:  "Header2",
					Value: "Value2",
				},
			}
			Expect(hubClient.Create(ctx, trafficManagerProfileName)).Should(Succeed(), "failed to create trafficManagerProfile")
			Expect(hubClient.Delete(ctx, trafficManagerProfileName)).Should(Succeed(), "failed to delete trafficManagerProfile")
		})
	})

	Context("Test TrafficManagerBackend API validation - invalid cases", func() {
		It("should deny creating API with invalid name size", func() {
			// Create the API.
			trafficManagerBackendName := &fleetnetv1alpha1.TrafficManagerBackend{
				ObjectMeta: objectMetaWithNameSizeInvalid,
				Spec:       trafficManagerBackendSpec,
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameWithInvalidSize))
			var err = hubClient.Create(ctx, trafficManagerBackendName)
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("metadata.name max length is 63"))
		})

		It("should deny creating API with invalid name starting with non-alphanumeric character", func() {
			// Create the API.
			trafficManagerBackendName := &fleetnetv1alpha1.TrafficManagerBackend{
				ObjectMeta: objectMetaWithNameStartingNonAlphanum,
				Spec:       trafficManagerBackendSpec,
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameStartingWithNonAlphanum))
			var err = hubClient.Create(ctx, trafficManagerBackendName)
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character"))
		})

		It("should deny creating API with invalid name ending with non-alphanumeric character", func() {
			// Create the API.
			trafficManagerBackendName := &fleetnetv1alpha1.TrafficManagerBackend{
				ObjectMeta: objectMetaWithNameEndingNonAlphanum,
				Spec:       trafficManagerBackendSpec,
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameEndingWithNonAlphanum))
			var err = hubClient.Create(ctx, trafficManagerBackendName)
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character"))
		})

		It("should deny creating API with invalid name containing character that is not alphanumeric and not -", func() {
			// Create the API.
			trafficManagerBackendName := &fleetnetv1alpha1.TrafficManagerBackend{
				ObjectMeta: objectMetaWithNameContainingUnderscore,
				Spec:       trafficManagerBackendSpec,
			}
			By(fmt.Sprintf("expecting denial of CREATE API %s", nameContainsUnderscore))
			var err = hubClient.Create(ctx, trafficManagerBackendName)
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("metadata.name max length is 63"))
		})
	})

	Context("Test TrafficManagerBackend API validation - valid cases", func() {
		It("should allow creating API with valid name size", func() {
			// Create the API.
			trafficManagerBackendName := &fleetnetv1alpha1.TrafficManagerBackend{
				ObjectMeta: objectMetaWithNameValid,
				Spec:       trafficManagerBackendSpec,
			}
			Expect(hubClient.Create(ctx, trafficManagerBackendName)).Should(Succeed(), "failed to create trafficManagerBackend")
			Expect(hubClient.Delete(ctx, trafficManagerBackendName)).Should(Succeed(), "failed to delete trafficManagerBackend")
		})

		It("should allow creating API with valid name starting with alphabet character", func() {
			// Create the API.
			trafficManagerBackendName := &fleetnetv1alpha1.TrafficManagerBackend{
				ObjectMeta: objectMetaWithValidNameStartingAlphabet,
				Spec:       trafficManagerBackendSpec,
			}
			Expect(hubClient.Create(ctx, trafficManagerBackendName)).Should(Succeed(), "failed to create trafficManagerBackend")
			Expect(hubClient.Delete(ctx, trafficManagerBackendName)).Should(Succeed(), "failed to delete trafficManagerBackend")
		})

		It("should allow creating API with valid name starting with numeric character", func() {
			// Create the API.
			trafficManagerBackendName := &fleetnetv1alpha1.TrafficManagerBackend{
				ObjectMeta: objectMetaWithValidNameStartingNumber,
				Spec:       trafficManagerBackendSpec,
			}
			Expect(hubClient.Create(ctx, trafficManagerBackendName)).Should(Succeed(), "failed to create trafficManagerBackend")
			Expect(hubClient.Delete(ctx, trafficManagerBackendName)).Should(Succeed(), "failed to delete trafficManagerBackend")
		})

		It("should allow creating API with valid name ending with alphabet character", func() {
			// Create the API.
			trafficManagerBackendName := &fleetnetv1alpha1.TrafficManagerBackend{
				ObjectMeta: objectMetaWithValidNameEndingAlphabet,
				Spec:       trafficManagerBackendSpec,
			}
			Expect(hubClient.Create(ctx, trafficManagerBackendName)).Should(Succeed(), "failed to create trafficManagerBackend")
			Expect(hubClient.Delete(ctx, trafficManagerBackendName)).Should(Succeed(), "failed to delete trafficManagerBackend")
		})

		It("should allow creating API with valid name ending with numeric character", func() {
			// Create the API.
			trafficManagerBackendName := &fleetnetv1alpha1.TrafficManagerBackend{
				ObjectMeta: objectMetaWithValidNameEndingNumber,
				Spec:       trafficManagerBackendSpec,
			}
			Expect(hubClient.Create(ctx, trafficManagerBackendName)).Should(Succeed(), "failed to create trafficManagerBackend")
			Expect(hubClient.Delete(ctx, trafficManagerBackendName)).Should(Succeed(), "failed to delete trafficManagerBackend")
		})
	})
})
