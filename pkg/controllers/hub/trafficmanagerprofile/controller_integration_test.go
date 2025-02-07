/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package trafficmanagerprofile

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	fleetnetv1beta1 "go.goms.io/fleet-networking/api/v1beta1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/test/common/trafficmanager/fakeprovider"
	"go.goms.io/fleet-networking/test/common/trafficmanager/validator"
)

func trafficManagerProfileForTest(name string) *fleetnetv1beta1.TrafficManagerProfile {
	return &fleetnetv1beta1.TrafficManagerProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: fleetnetv1beta1.TrafficManagerProfileSpec{
			MonitorConfig: &fleetnetv1beta1.MonitorConfig{
				IntervalInSeconds:         ptr.To[int64](30),
				Path:                      ptr.To("/healthz"),
				Port:                      ptr.To[int64](8080),
				Protocol:                  ptr.To(fleetnetv1beta1.TrafficManagerMonitorProtocolHTTPS),
				TimeoutInSeconds:          ptr.To[int64](10),
				ToleratedNumberOfFailures: ptr.To[int64](5),
			},
			ResourceGroup: fakeprovider.DefaultResourceGroupName,
		},
	}
}

var _ = Describe("Test TrafficManagerProfile Controller", func() {
	Context("When updating existing valid trafficManagerProfile", Ordered, func() {
		name := fakeprovider.ValidProfileName
		var profile *fleetnetv1beta1.TrafficManagerProfile
		relativeDNSName := fmt.Sprintf(DNSRelativeNameFormat, testNamespace, name)
		fqdn := fmt.Sprintf(fakeprovider.ProfileDNSNameFormat, relativeDNSName)

		It("AzureTrafficManager should be configured", func() {
			By("By creating a new TrafficManagerProfile")
			profile = trafficManagerProfileForTest(name)
			Expect(k8sClient.Create(ctx, profile)).Should(Succeed())

			By("By checking profile")
			want := fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:       name,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerProfileFinalizer},
				},
				Spec: profile.Spec,
				Status: fleetnetv1beta1.TrafficManagerProfileStatus{
					DNSName: ptr.To(fqdn),
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionTrue,
							Type:               string(fleetnetv1beta1.TrafficManagerProfileConditionProgrammed),
							Reason:             string(fleetnetv1beta1.TrafficManagerProfileReasonProgrammed),
							ObservedGeneration: profile.Generation,
						},
					},
				},
			}
			validator.ValidateTrafficManagerProfile(ctx, k8sClient, &want)
		})

		It("Update the trafficManagerProfile spec", func() {
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: name}, profile)).Should(Succeed(), "failed to get the trafficManagerProfile")
			profile.Spec.MonitorConfig.IntervalInSeconds = ptr.To[int64](10)
			profile.Spec.MonitorConfig.TimeoutInSeconds = ptr.To[int64](10)
			Expect(k8sClient.Update(ctx, profile)).Should(Succeed(), "failed to update the trafficManagerProfile")
		})

		It("Validating trafficManagerProfile status and update should fail", func() {
			want := fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:       name,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerProfileFinalizer},
				},
				Spec: profile.Spec,
				Status: fleetnetv1beta1.TrafficManagerProfileStatus{
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionFalse,
							Type:               string(fleetnetv1beta1.TrafficManagerProfileConditionProgrammed),
							Reason:             string(fleetnetv1beta1.TrafficManagerProfileReasonInvalid),
							ObservedGeneration: profile.Generation,
						},
					},
				},
			}
			validator.ValidateTrafficManagerProfile(ctx, k8sClient, &want)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating trafficManagerProfile is deleted", func() {
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, types.NamespacedName{Namespace: testNamespace, Name: name})
		})
	})

	Context("When updating existing valid trafficManagerProfile with no changes", Ordered, func() {
		name := fakeprovider.ValidProfileName
		var profile *fleetnetv1beta1.TrafficManagerProfile

		It("AzureTrafficManager should be configured", func() {
			By("By creating a new TrafficManagerProfile")
			profile = &fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:       name,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerProfileFinalizer},
				},
				Spec: fleetnetv1beta1.TrafficManagerProfileSpec{
					MonitorConfig: &fleetnetv1beta1.MonitorConfig{
						IntervalInSeconds:         ptr.To[int64](10),
						Path:                      ptr.To("/healthz"),
						Port:                      ptr.To[int64](8080),
						Protocol:                  ptr.To(fleetnetv1beta1.TrafficManagerMonitorProtocolHTTP),
						TimeoutInSeconds:          ptr.To[int64](9),
						ToleratedNumberOfFailures: ptr.To[int64](4),
					},
					ResourceGroup: fakeprovider.DefaultResourceGroupName,
				},
			}
			Expect(k8sClient.Create(ctx, profile)).Should(Succeed())

			By("By checking profile")
			want := fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:       name,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerProfileFinalizer},
				},
				Spec: profile.Spec,
				Status: fleetnetv1beta1.TrafficManagerProfileStatus{
					// The DNS name is returned by the fake Azure GET call.
					DNSName: ptr.To(fmt.Sprintf(fakeprovider.ProfileDNSNameFormat, name)),
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionTrue,
							Type:               string(fleetnetv1beta1.TrafficManagerProfileConditionProgrammed),
							Reason:             string(fleetnetv1beta1.TrafficManagerProfileReasonProgrammed),
							ObservedGeneration: profile.Generation,
						},
					},
				},
			}
			validator.ValidateTrafficManagerProfile(ctx, k8sClient, &want)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating trafficManagerProfile is deleted", func() {
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, types.NamespacedName{Namespace: testNamespace, Name: name})
		})
	})

	Context("When creating trafficManagerProfile and DNS name is not available", Ordered, func() {
		name := fakeprovider.ConflictErrProfileName
		var profile *fleetnetv1beta1.TrafficManagerProfile

		It("AzureTrafficManager should not be configured", func() {
			By("By creating a new TrafficManagerProfile")
			profile = trafficManagerProfileForTest(name)
			Expect(k8sClient.Create(ctx, profile)).Should(Succeed())

			By("By checking profile")
			want := fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:       name,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerProfileFinalizer},
				},
				Spec: profile.Spec,
				Status: fleetnetv1beta1.TrafficManagerProfileStatus{
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionFalse,
							Type:               string(fleetnetv1beta1.TrafficManagerProfileConditionProgrammed),
							Reason:             string(fleetnetv1beta1.TrafficManagerProfileReasonDNSNameNotAvailable),
							ObservedGeneration: profile.Generation,
						},
					},
				},
			}
			validator.ValidateTrafficManagerProfile(ctx, k8sClient, &want)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating trafficManagerProfile is deleted", func() {
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, types.NamespacedName{Namespace: testNamespace, Name: name})
		})
	})

	Context("When creating trafficManagerProfile and azure request failed because of too many requests", Ordered, func() {
		name := fakeprovider.ThrottledErrProfileName
		var profile *fleetnetv1beta1.TrafficManagerProfile

		It("AzureTrafficManager should not be configured", func() {
			By("By creating a new TrafficManagerProfile")
			profile = trafficManagerProfileForTest(name)
			Expect(k8sClient.Create(ctx, profile)).Should(Succeed())

			By("By checking profile")
			want := fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:       name,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerProfileFinalizer},
				},
				Spec: profile.Spec,
				Status: fleetnetv1beta1.TrafficManagerProfileStatus{
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionUnknown,
							Type:               string(fleetnetv1beta1.TrafficManagerProfileConditionProgrammed),
							Reason:             string(fleetnetv1beta1.TrafficManagerProfileReasonPending),
							ObservedGeneration: profile.Generation,
						},
					},
				},
			}
			validator.ValidateTrafficManagerProfile(ctx, k8sClient, &want)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating trafficManagerProfile is deleted", func() {
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, types.NamespacedName{Namespace: testNamespace, Name: name})
		})
	})

	Context("When creating trafficManagerProfile and azure request failed because of client side error", Ordered, func() {
		name := "bad-request"
		var profile *fleetnetv1beta1.TrafficManagerProfile

		It("AzureTrafficManager should not be configured", func() {
			By("By creating a new TrafficManagerProfile")
			profile = trafficManagerProfileForTest(name)
			Expect(k8sClient.Create(ctx, profile)).Should(Succeed())

			By("By checking profile")
			want := fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:       name,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerProfileFinalizer},
				},
				Spec: profile.Spec,
				Status: fleetnetv1beta1.TrafficManagerProfileStatus{
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionFalse,
							Type:               string(fleetnetv1beta1.TrafficManagerProfileConditionProgrammed),
							Reason:             string(fleetnetv1beta1.TrafficManagerProfileReasonInvalid),
							ObservedGeneration: profile.Generation,
						},
					},
				},
			}
			validator.ValidateTrafficManagerProfile(ctx, k8sClient, &want)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating trafficManagerProfile is deleted", func() {
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, types.NamespacedName{Namespace: testNamespace, Name: name})
		})
	})

	Context("When creating trafficManagerProfile and azure request failed because of internal server error", Ordered, func() {
		name := fakeprovider.InternalServerErrProfileName
		var profile *fleetnetv1beta1.TrafficManagerProfile

		It("AzureTrafficManager should not be configured", func() {
			By("By creating a new TrafficManagerProfile")
			profile = trafficManagerProfileForTest(name)
			Expect(k8sClient.Create(ctx, profile)).Should(Succeed())

			By("By checking profile")
			want := fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:       name,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerProfileFinalizer},
				},
				Spec: profile.Spec,
				Status: fleetnetv1beta1.TrafficManagerProfileStatus{
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionUnknown,
							Type:               string(fleetnetv1beta1.TrafficManagerProfileConditionProgrammed),
							Reason:             string(fleetnetv1beta1.TrafficManagerProfileReasonPending),
							ObservedGeneration: profile.Generation,
						},
					},
				},
			}
			validator.ValidateTrafficManagerProfile(ctx, k8sClient, &want)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating trafficManagerProfile is deleted", func() {
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, types.NamespacedName{Namespace: testNamespace, Name: name})
		})
	})

	Context("When creating trafficManagerProfile and azure request failed because of not found resource group", Ordered, func() {
		name := fakeprovider.ValidProfileName
		var profile *fleetnetv1beta1.TrafficManagerProfile

		It("TrafficManagerProfile should be invalid", func() {
			By("By creating a new TrafficManagerProfile")
			profile = trafficManagerProfileForTest(name)
			// reset the resourceGroup
			profile.Spec.ResourceGroup = "not-found-resource-group"
			Expect(k8sClient.Create(ctx, profile)).Should(Succeed())

			By("By checking profile")
			want := fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: testNamespace,
				},
				Spec: profile.Spec,
				Status: fleetnetv1beta1.TrafficManagerProfileStatus{
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionFalse,
							Type:               string(fleetnetv1beta1.TrafficManagerProfileConditionProgrammed),
							Reason:             string(fleetnetv1beta1.TrafficManagerProfileReasonInvalid),
							ObservedGeneration: profile.Generation,
						},
					},
				},
			}
			validator.ValidateTrafficManagerProfile(ctx, k8sClient, &want)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating if trafficManagerProfile is deleted", func() {
			name := types.NamespacedName{Namespace: testNamespace, Name: name}
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, name)
		})
	})
})
