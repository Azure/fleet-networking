/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1beta1

import (
	"errors"
	"fmt"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	fleetnetv1beta1 "go.goms.io/fleet-networking/api/v1beta1"
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
	var trafficManagerProfileSpec = fleetnetv1beta1.TrafficManagerProfileSpec{
		MonitorConfig: &fleetnetv1beta1.MonitorConfig{
			IntervalInSeconds: ptr.To(int64(30)),
			TimeoutInSeconds:  ptr.To(int64(7)),
		},
		ResourceGroup: "test-resource-group",
	}
	var trafficManagerBackendSpec = fleetnetv1beta1.TrafficManagerBackendSpec{
		Profile: fleetnetv1beta1.TrafficManagerProfileRef{
			Name: "traffic-manager-profile-ref-name",
		},
		Backend: fleetnetv1beta1.TrafficManagerBackendRef{
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

	Context("Test TrafficManagerProfile API validation - invalid cases", func() {
		It("should deny creating API with invalid name size", func() {
			// Create the API.
			trafficManagerProfileName := &fleetnetv1beta1.TrafficManagerProfile{
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
			trafficManagerProfileName := &fleetnetv1beta1.TrafficManagerProfile{
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
			trafficManagerProfileName := &fleetnetv1beta1.TrafficManagerProfile{
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
			trafficManagerProfileName := &fleetnetv1beta1.TrafficManagerProfile{
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
			profile := &fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithNameValid,
				Spec: fleetnetv1beta1.TrafficManagerProfileSpec{
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
			profile := &fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithNameValid,
				Spec: fleetnetv1beta1.TrafficManagerProfileSpec{
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
			profile := &fleetnetv1beta1.TrafficManagerProfile{
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

		It("should deny creating API with timeoutInSeconds < 5 when intervalInSeconds is 30", func() {
			profile := &fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithNameValid,
				Spec: fleetnetv1beta1.TrafficManagerProfileSpec{
					MonitorConfig: &fleetnetv1beta1.MonitorConfig{
						IntervalInSeconds: ptr.To(int64(30)),
						TimeoutInSeconds:  ptr.To(int64(4)),
					},
					ResourceGroup: "test-resource-group",
				},
			}
			By("expecting denial of CREATE API with invalid timeoutInSeconds")
			var err = hubClient.Create(ctx, profile)
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("spec.monitorConfig.timeoutInSeconds in body should be greater than or equal to 5"))
			Expect(statusErr.Status().Message).Should(ContainSubstring("timeoutInSeconds must be between 5 and 10 when intervalInSeconds is 30"))
		})

		It("should deny creating API with timeoutInSeconds > 10 when intervalInSeconds is 30", func() {
			profile := &fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithNameValid,
				Spec: fleetnetv1beta1.TrafficManagerProfileSpec{
					MonitorConfig: &fleetnetv1beta1.MonitorConfig{
						IntervalInSeconds: ptr.To(int64(30)),
						TimeoutInSeconds:  ptr.To(int64(15)),
					},
					ResourceGroup: "test-resource-group",
				},
			}
			By("expecting denial of CREATE API with invalid timeoutInSeconds")
			var err = hubClient.Create(ctx, profile)
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("spec.monitorConfig.timeoutInSeconds in body should be less than or equal to 10"))
			Expect(statusErr.Status().Message).Should(ContainSubstring("timeoutInSeconds must be between 5 and 10 when intervalInSeconds is 30"))
		})

		It("should deny creating API with timeoutInSeconds < 5 when intervalInSeconds is 10", func() {
			profile := &fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithNameValid,
				Spec: fleetnetv1beta1.TrafficManagerProfileSpec{
					MonitorConfig: &fleetnetv1beta1.MonitorConfig{
						IntervalInSeconds: ptr.To(int64(10)),
						TimeoutInSeconds:  ptr.To(int64(4)),
					},
					ResourceGroup: "test-resource-group",
				},
			}
			By("expecting denial of CREATE API with invalid timeoutInSeconds")
			var err = hubClient.Create(ctx, profile)
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("spec.monitorConfig.timeoutInSeconds in body should be greater than or equal to 5"))
			Expect(statusErr.Status().Message).Should(ContainSubstring("timeoutInSeconds must be between 5 and 9 when intervalInSeconds is 10"))
		})

		It("should deny creating API with timeoutInSeconds > 9 when intervalInSeconds is 10", func() {
			profile := &fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithNameValid,
				Spec: fleetnetv1beta1.TrafficManagerProfileSpec{
					MonitorConfig: &fleetnetv1beta1.MonitorConfig{
						IntervalInSeconds: ptr.To(int64(10)),
						TimeoutInSeconds:  ptr.To(int64(10)),
					},
					ResourceGroup: "test-resource-group",
				},
			}
			By("expecting denial of CREATE API with invalid timeoutInSeconds")
			var err = hubClient.Create(ctx, profile)
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("spec.monitorConfig: Invalid value: \"object\": timeoutInSeconds must be between 5 and 9 when intervalInSeconds is 10"))
			Expect(statusErr.Status().Message).Should(ContainSubstring("timeoutInSeconds must be between 5 and 9 when intervalInSeconds is 10"))
		})

		It("should deny creating API with timeoutInSeconds < 5 when intervalInSeconds is not defined", func() {
			profile := &fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithNameValid,
				Spec: fleetnetv1beta1.TrafficManagerProfileSpec{
					MonitorConfig: &fleetnetv1beta1.MonitorConfig{
						TimeoutInSeconds: ptr.To(int64(3)),
					},
					ResourceGroup: "test-resource-group",
				},
			}
			By("expecting denial of CREATE API with invalid timeoutInSeconds")
			var err = hubClient.Create(ctx, profile)
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("spec.monitorConfig.timeoutInSeconds in body should be greater than or equal to 5"))
			Expect(statusErr.Status().Message).Should(ContainSubstring("timeoutInSeconds must be between 5 and 10 when intervalInSeconds is 30"))
		})

		It("should deny creating API with timeoutInSeconds > 10 when intervalInSeconds is not defined", func() {
			profile := &fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithNameValid,
				Spec: fleetnetv1beta1.TrafficManagerProfileSpec{
					MonitorConfig: &fleetnetv1beta1.MonitorConfig{
						TimeoutInSeconds: ptr.To(int64(11)),
					},
					ResourceGroup: "test-resource-group",
				},
			}
			By("expecting denial of CREATE API with invalid timeoutInSeconds")
			var err = hubClient.Create(ctx, profile)
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create API call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8serrors.StatusError{})))
			Expect(statusErr.Status().Message).Should(ContainSubstring("spec.monitorConfig.timeoutInSeconds in body should be less than or equal to 10"))
			Expect(statusErr.Status().Message).Should(ContainSubstring("timeoutInSeconds must be between 5 and 10 when intervalInSeconds is 30"))
		})
	})

	Context("Test TrafficManagerProfile API validation - valid cases", func() {
		It("should allow creating API with valid name size", func() {
			// Create the API.
			trafficManagerProfileName := &fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithNameValid,
				Spec:       trafficManagerProfileSpec,
			}
			Expect(hubClient.Create(ctx, trafficManagerProfileName)).Should(Succeed(), "failed to create trafficManagerProfile")
			Expect(hubClient.Delete(ctx, trafficManagerProfileName)).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("should allow creating API with valid name starting with alphabet character", func() {
			// Create the API.
			trafficManagerProfileName := &fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithValidNameStartingAlphabet,
				Spec:       trafficManagerProfileSpec,
			}
			Expect(hubClient.Create(ctx, trafficManagerProfileName)).Should(Succeed(), "failed to create trafficManagerProfile")
			Expect(hubClient.Delete(ctx, trafficManagerProfileName)).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("should allow creating API with valid name starting with numeric character", func() {
			// Create the API.
			trafficManagerProfileName := &fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithValidNameStartingNumber,
				Spec:       trafficManagerProfileSpec,
			}
			Expect(hubClient.Create(ctx, trafficManagerProfileName)).Should(Succeed(), "failed to create trafficManagerProfile")
			Expect(hubClient.Delete(ctx, trafficManagerProfileName)).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("should allow creating API with valid name ending with alphabet character", func() {
			// Create the API.
			trafficManagerProfileName := &fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithValidNameEndingAlphabet,
				Spec:       trafficManagerProfileSpec,
			}
			Expect(hubClient.Create(ctx, trafficManagerProfileName)).Should(Succeed(), "failed to create trafficManagerProfile")
			Expect(hubClient.Delete(ctx, trafficManagerProfileName)).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("should allow creating API with valid name ending with numeric character", func() {
			// Create the API.
			trafficManagerProfileName := &fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithValidNameEndingNumber,
				Spec:       trafficManagerProfileSpec,
			}
			Expect(hubClient.Create(ctx, trafficManagerProfileName)).Should(Succeed(), "failed to create trafficManagerProfile")
			Expect(hubClient.Delete(ctx, trafficManagerProfileName)).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("should allow creating API with valid timeoutInSeconds when intervalInSeconds is 30", func() {
			profile := &fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithNameValid,
				Spec: fleetnetv1beta1.TrafficManagerProfileSpec{
					MonitorConfig: &fleetnetv1beta1.MonitorConfig{
						IntervalInSeconds: ptr.To(int64(30)),
						TimeoutInSeconds:  ptr.To(int64(7)),
					},
					ResourceGroup: "test-resource-group",
				},
			}
			Expect(hubClient.Create(ctx, profile)).Should(Succeed(), "failed to create trafficManagerProfile")
			Expect(hubClient.Delete(ctx, profile)).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("should allow creating API with timeoutInSeconds at lower bound (5) when intervalInSeconds is 30", func() {
			profile := &fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithNameValid,
				Spec: fleetnetv1beta1.TrafficManagerProfileSpec{
					MonitorConfig: &fleetnetv1beta1.MonitorConfig{
						IntervalInSeconds: ptr.To(int64(30)),
						TimeoutInSeconds:  ptr.To(int64(5)),
					},
					ResourceGroup: "test-resource-group",
				},
			}
			Expect(hubClient.Create(ctx, profile)).Should(Succeed(), "failed to create trafficManagerProfile at lower bound")
			Expect(hubClient.Delete(ctx, profile)).Should(Succeed(), "failed to delete trafficManagerProfile at lower bound")
		})

		It("should allow creating API with timeoutInSeconds at upper bound (10) when intervalInSeconds is 30", func() {
			profile := &fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithNameValid,
				Spec: fleetnetv1beta1.TrafficManagerProfileSpec{
					MonitorConfig: &fleetnetv1beta1.MonitorConfig{
						IntervalInSeconds: ptr.To(int64(30)),
						TimeoutInSeconds:  ptr.To(int64(10)),
					},
					ResourceGroup: "test-resource-group",
				},
			}
			Expect(hubClient.Create(ctx, profile)).Should(Succeed(), "failed to create trafficManagerProfile at upper bound")
			Expect(hubClient.Delete(ctx, profile)).Should(Succeed(), "failed to delete trafficManagerProfile at upper bound")
		})

		It("should allow creating API with valid timeoutInSeconds when intervalInSeconds is 10", func() {
			profile := &fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithNameValid,
				Spec: fleetnetv1beta1.TrafficManagerProfileSpec{
					MonitorConfig: &fleetnetv1beta1.MonitorConfig{
						IntervalInSeconds: ptr.To(int64(10)),
						TimeoutInSeconds:  ptr.To(int64(8)),
					},
					ResourceGroup: "test-resource-group",
				},
			}
			Expect(hubClient.Create(ctx, profile)).Should(Succeed(), "failed to create trafficManagerProfile")
			Expect(hubClient.Delete(ctx, profile)).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("should allow creating API with timeoutInSeconds at lower bound (5) when intervalInSeconds is 10", func() {
			profile := &fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithNameValid,
				Spec: fleetnetv1beta1.TrafficManagerProfileSpec{
					MonitorConfig: &fleetnetv1beta1.MonitorConfig{
						IntervalInSeconds: ptr.To(int64(10)),
						TimeoutInSeconds:  ptr.To(int64(5)),
					},
					ResourceGroup: "test-resource-group",
				},
			}
			Expect(hubClient.Create(ctx, profile)).Should(Succeed(), "failed to create trafficManagerProfile at lower bound")
			Expect(hubClient.Delete(ctx, profile)).Should(Succeed(), "failed to delete trafficManagerProfile at lower bound")
		})

		It("should allow creating API with timeoutInSeconds at upper bound (9) when intervalInSeconds is 10", func() {
			profile := &fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithNameValid,
				Spec: fleetnetv1beta1.TrafficManagerProfileSpec{
					MonitorConfig: &fleetnetv1beta1.MonitorConfig{
						IntervalInSeconds: ptr.To(int64(30)),
						TimeoutInSeconds:  ptr.To(int64(9)),
					},
					ResourceGroup: "test-resource-group",
				},
			}
			Expect(hubClient.Create(ctx, profile)).Should(Succeed(), "failed to create trafficManagerProfile at upper bound")
			Expect(hubClient.Delete(ctx, profile)).Should(Succeed(), "failed to delete trafficManagerProfile at upper bound")
		})

		It("should allow creating API with valid timeoutInSeconds when intervalInSeconds is not defined", func() {
			profile := &fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithNameValid,
				Spec: fleetnetv1beta1.TrafficManagerProfileSpec{
					MonitorConfig: &fleetnetv1beta1.MonitorConfig{
						TimeoutInSeconds: ptr.To(int64(10)),
					},
					ResourceGroup: "test-resource-group",
				},
			}
			Expect(hubClient.Create(ctx, profile)).Should(Succeed(), "failed to create trafficManagerProfile")
			Expect(hubClient.Delete(ctx, profile)).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("should allow creating API with timeoutInSeconds at lower bound (5) when intervalInSeconds is not defined", func() {
			profile := &fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithNameValid,
				Spec: fleetnetv1beta1.TrafficManagerProfileSpec{
					MonitorConfig: &fleetnetv1beta1.MonitorConfig{
						TimeoutInSeconds: ptr.To(int64(5)),
					},
					ResourceGroup: "test-resource-group",
				},
			}
			Expect(hubClient.Create(ctx, profile)).Should(Succeed(), "failed to create trafficManagerProfile at lower bound")
			Expect(hubClient.Delete(ctx, profile)).Should(Succeed(), "failed to delete trafficManagerProfile at lower bound")
		})

		It("should allow creating API with timeoutInSeconds at upper bound (10) when intervalInSeconds is not defined", func() {
			profile := &fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: objectMetaWithNameValid,
				Spec: fleetnetv1beta1.TrafficManagerProfileSpec{
					MonitorConfig: &fleetnetv1beta1.MonitorConfig{
						TimeoutInSeconds: ptr.To(int64(10)),
					},
					ResourceGroup: "test-resource-group",
				},
			}
			Expect(hubClient.Create(ctx, profile)).Should(Succeed(), "failed to create trafficManagerProfile at upper bound")
			Expect(hubClient.Delete(ctx, profile)).Should(Succeed(), "failed to delete trafficManagerProfile at upper bound")
		})
	})

	Context("Test TrafficManagerBackend API validation - invalid cases", func() {
		It("should deny creating API with invalid name size", func() {
			// Create the API.
			trafficManagerBackendName := &fleetnetv1beta1.TrafficManagerBackend{
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
			trafficManagerBackendName := &fleetnetv1beta1.TrafficManagerBackend{
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
			trafficManagerBackendName := &fleetnetv1beta1.TrafficManagerBackend{
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
			trafficManagerBackendName := &fleetnetv1beta1.TrafficManagerBackend{
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
			trafficManagerBackendName := &fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: objectMetaWithNameValid,
				Spec:       trafficManagerBackendSpec,
			}
			Expect(hubClient.Create(ctx, trafficManagerBackendName)).Should(Succeed(), "failed to create trafficManagerBackend")
			Expect(hubClient.Delete(ctx, trafficManagerBackendName)).Should(Succeed(), "failed to delete trafficManagerBackend")
		})

		It("should allow creating API with valid name starting with alphabet character", func() {
			// Create the API.
			trafficManagerBackendName := &fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: objectMetaWithValidNameStartingAlphabet,
				Spec:       trafficManagerBackendSpec,
			}
			Expect(hubClient.Create(ctx, trafficManagerBackendName)).Should(Succeed(), "failed to create trafficManagerBackend")
			Expect(hubClient.Delete(ctx, trafficManagerBackendName)).Should(Succeed(), "failed to delete trafficManagerBackend")
		})

		It("should allow creating API with valid name starting with numeric character", func() {
			// Create the API.
			trafficManagerBackendName := &fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: objectMetaWithValidNameStartingNumber,
				Spec:       trafficManagerBackendSpec,
			}
			Expect(hubClient.Create(ctx, trafficManagerBackendName)).Should(Succeed(), "failed to create trafficManagerBackend")
			Expect(hubClient.Delete(ctx, trafficManagerBackendName)).Should(Succeed(), "failed to delete trafficManagerBackend")
		})

		It("should allow creating API with valid name ending with alphabet character", func() {
			// Create the API.
			trafficManagerBackendName := &fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: objectMetaWithValidNameEndingAlphabet,
				Spec:       trafficManagerBackendSpec,
			}
			Expect(hubClient.Create(ctx, trafficManagerBackendName)).Should(Succeed(), "failed to create trafficManagerBackend")
			Expect(hubClient.Delete(ctx, trafficManagerBackendName)).Should(Succeed(), "failed to delete trafficManagerBackend")
		})

		It("should allow creating API with valid name ending with numeric character", func() {
			// Create the API.
			trafficManagerBackendName := &fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: objectMetaWithValidNameEndingNumber,
				Spec:       trafficManagerBackendSpec,
			}
			Expect(hubClient.Create(ctx, trafficManagerBackendName)).Should(Succeed(), "failed to create trafficManagerBackend")
			Expect(hubClient.Delete(ctx, trafficManagerBackendName)).Should(Succeed(), "failed to delete trafficManagerBackend")
		})
	})
})
