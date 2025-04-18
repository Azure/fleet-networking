/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package trafficmanagerprofile

import (
	"fmt"
	"strconv"
	"time"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	prometheusclientmodel "github.com/prometheus/client_model/go"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	fleetnetv1beta1 "go.goms.io/fleet-networking/api/v1beta1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/test/common/metrics"
	"go.goms.io/fleet-networking/test/common/trafficmanager/fakeprovider"
	"go.goms.io/fleet-networking/test/common/trafficmanager/validator"
)

const (
	timeout  = time.Second * 10
	interval = time.Millisecond * 250
)

var (
	customRegistry *prometheus.Registry
)

func initializeTrafficManagerProfileMetricsRegistry() {
	// Create a test registry
	customRegistry = prometheus.NewRegistry()
	Expect(customRegistry.Register(trafficManagerProfileStatusLastTimestampSeconds)).Should(Succeed())
	// Reset metrics before each test
	trafficManagerProfileStatusLastTimestampSeconds.Reset()
}

func unregisterTrafficManagerProfileMetrics(registry *prometheus.Registry) {
	Expect(registry.Unregister(trafficManagerProfileStatusLastTimestampSeconds)).Should(BeTrue())
}

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

// validateTrafficManagerProfileMetricsEmitted validates the trafficManagerProfile status metrics are emitted and are emitted in the correct order.
func validateTrafficManagerProfileMetricsEmitted(registry *prometheus.Registry, wantMetrics ...*prometheusclientmodel.Metric) {
	Eventually(func() error {
		metricFamilies, err := registry.Gather()
		if err != nil {
			return fmt.Errorf("failed to gather metrics: %w", err)
		}
		var gotMetrics []*prometheusclientmodel.Metric
		for _, mf := range metricFamilies {
			if mf.GetName() == "fleet_networking_traffic_manager_profile_status_last_timestamp_seconds" {
				gotMetrics = mf.GetMetric()
			}
		}

		if diff := cmp.Diff(gotMetrics, wantMetrics, metrics.CmpOptions...); diff != "" {
			return fmt.Errorf("trafficManagerProfile status metrics mismatch (-got, +want):\n%s", diff)
		}

		return nil
	}, timeout, interval).Should(Succeed(), "failed to validate the trafficManagerProfile status metrics")
}

func generateMetrics(
	profile *fleetnetv1beta1.TrafficManagerProfile,
	condition metav1.Condition,
) *prometheusclientmodel.Metric {
	return &prometheusclientmodel.Metric{
		Label: []*prometheusclientmodel.LabelPair{
			{Name: ptr.To("namespace"), Value: &profile.Namespace},
			{Name: ptr.To("name"), Value: &profile.Name},
			{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(profile.Generation, 10))},
			{Name: ptr.To("condition"), Value: ptr.To(condition.Type)},
			{Name: ptr.To("status"), Value: ptr.To(string(condition.Status))},
			{Name: ptr.To("reason"), Value: ptr.To(condition.Reason)},
		},
		Gauge: &prometheusclientmodel.Gauge{
			Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
		},
	}
}

var _ = Describe("Test TrafficManagerProfile Controller", func() {
	Context("When updating existing valid trafficManagerProfile", Ordered, func() {
		name := fakeprovider.ValidProfileName
		var profile *fleetnetv1beta1.TrafficManagerProfile
		profileResourceID := fmt.Sprintf(fakeprovider.ProfileResourceIDFormat, fakeprovider.DefaultSubscriptionID, fakeprovider.DefaultResourceGroupName, name)

		relativeDNSName := fmt.Sprintf(DNSRelativeNameFormat, testNamespace, name)
		fqdn := fmt.Sprintf(fakeprovider.ProfileDNSNameFormat, relativeDNSName)
		var wantMetrics []*prometheusclientmodel.Metric

		It("Initializing the custom registry", func() {
			initializeTrafficManagerProfileMetricsRegistry()
		})

		It("AzureTrafficManager should be configured", func() {
			By("By creating a new TrafficManagerProfile")
			profile = trafficManagerProfileForTest(name)
			Expect(k8sClient.Create(ctx, profile)).Should(Succeed())

			By("By checking profile")
			want := fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:       name,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerProfileFinalizer, objectmeta.MetricsFinalizer},
				},
				Spec: profile.Spec,
				Status: fleetnetv1beta1.TrafficManagerProfileStatus{
					DNSName:    ptr.To(fqdn),
					ResourceID: profileResourceID,
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
			validator.ValidateTrafficManagerProfile(ctx, k8sClient, &want, timeout)

			By("By validating the status metrics")
			wantMetrics = append(wantMetrics, generateMetrics(profile, want.Status.Conditions[0]))
			validateTrafficManagerProfileMetricsEmitted(customRegistry, wantMetrics...)
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
					Finalizers: []string{objectmeta.TrafficManagerProfileFinalizer, objectmeta.MetricsFinalizer},
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
			validator.ValidateTrafficManagerProfile(ctx, k8sClient, &want, timeout)

			By("By validating the status metrics")
			wantMetrics = append(wantMetrics, generateMetrics(profile, want.Status.Conditions[0]))
			validateTrafficManagerProfileMetricsEmitted(customRegistry, wantMetrics...)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating trafficManagerProfile is deleted", func() {
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, types.NamespacedName{Namespace: testNamespace, Name: name}, timeout)

			By("By validating the status metrics")
			validateTrafficManagerProfileMetricsEmitted(customRegistry)
		})

		It("Unregister the metrics", func() {
			// Unregister the custom registry
			unregisterTrafficManagerProfileMetrics(customRegistry)
		})
	})

	Context("When updating existing valid trafficManagerProfile with no changes", Ordered, func() {
		name := fakeprovider.ValidProfileName
		var profile *fleetnetv1beta1.TrafficManagerProfile
		profileResourceID := fmt.Sprintf(fakeprovider.ProfileResourceIDFormat, fakeprovider.DefaultSubscriptionID, fakeprovider.DefaultResourceGroupName, name)
		var wantMetrics []*prometheusclientmodel.Metric

		It("Initializing the custom registry", func() {
			initializeTrafficManagerProfileMetricsRegistry()
		})

		It("AzureTrafficManager should be configured", func() {
			By("By creating a new TrafficManagerProfile")
			profile = &fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: testNamespace,
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
					Name:      name,
					Namespace: testNamespace,
					// Since the controller does not create the Azure resource, so it won't add the profile finalizer here.
					Finalizers: []string{objectmeta.MetricsFinalizer},
				},
				Spec: profile.Spec,
				Status: fleetnetv1beta1.TrafficManagerProfileStatus{
					// The DNS name is returned by the fake Azure GET call.
					DNSName:    ptr.To(fmt.Sprintf(fakeprovider.ProfileDNSNameFormat, name)),
					ResourceID: profileResourceID,
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
			validator.ValidateTrafficManagerProfile(ctx, k8sClient, &want, timeout)

			By("By validating the status metrics")
			wantMetrics = append(wantMetrics, generateMetrics(profile, want.Status.Conditions[0]))
			validateTrafficManagerProfileMetricsEmitted(customRegistry, wantMetrics...)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating trafficManagerProfile is deleted", func() {
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, types.NamespacedName{Namespace: testNamespace, Name: name}, timeout)

			By("By validating the status metrics")
			validateTrafficManagerProfileMetricsEmitted(customRegistry)
		})

		It("Unregister the metrics", func() {
			// Unregister the custom registry
			unregisterTrafficManagerProfileMetrics(customRegistry)
		})
	})

	Context("When creating valid trafficManagerProfile with no changes, while Azure traffic manager returns nil DNS and nil ID", Ordered, func() {
		name := fakeprovider.ValidProfileWithUnexpectedResponse
		var profile *fleetnetv1beta1.TrafficManagerProfile
		var wantMetrics []*prometheusclientmodel.Metric

		It("Initializing the custom registry", func() {
			initializeTrafficManagerProfileMetricsRegistry()
		})

		It("AzureTrafficManager should be configured", func() {
			By("By creating a new TrafficManagerProfile")
			profile = &fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: testNamespace,
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
					Finalizers: []string{objectmeta.TrafficManagerProfileFinalizer, objectmeta.MetricsFinalizer},
				},
				Spec: profile.Spec,
				Status: fleetnetv1beta1.TrafficManagerProfileStatus{
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
			validator.ValidateTrafficManagerProfile(ctx, k8sClient, &want, timeout)

			By("By validating the status metrics")
			wantMetrics = append(wantMetrics, generateMetrics(profile, want.Status.Conditions[0]))
			validateTrafficManagerProfileMetricsEmitted(customRegistry, wantMetrics...)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating trafficManagerProfile is deleted", func() {
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, types.NamespacedName{Namespace: testNamespace, Name: name}, timeout)
			By("By validating the status metrics")
			validateTrafficManagerProfileMetricsEmitted(customRegistry)
		})

		It("Unregister the metrics", func() {
			// Unregister the custom registry
			unregisterTrafficManagerProfileMetrics(customRegistry)
		})
	})

	Context("When creating trafficManagerProfile and DNS name is not available", Ordered, func() {
		name := fakeprovider.ConflictErrProfileName
		var profile *fleetnetv1beta1.TrafficManagerProfile
		var wantMetrics []*prometheusclientmodel.Metric

		It("Initializing the custom registry", func() {
			initializeTrafficManagerProfileMetricsRegistry()
		})

		It("AzureTrafficManager should not be configured", func() {
			By("By creating a new TrafficManagerProfile")
			profile = trafficManagerProfileForTest(name)
			Expect(k8sClient.Create(ctx, profile)).Should(Succeed())

			By("By checking profile")
			want := fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:       name,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerProfileFinalizer, objectmeta.MetricsFinalizer},
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
			validator.ValidateTrafficManagerProfile(ctx, k8sClient, &want, timeout)

			By("By validating the status metrics")
			wantMetrics = append(wantMetrics, generateMetrics(profile, want.Status.Conditions[0]))
			validateTrafficManagerProfileMetricsEmitted(customRegistry, wantMetrics...)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating trafficManagerProfile is deleted", func() {
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, types.NamespacedName{Namespace: testNamespace, Name: name}, timeout)

			By("By validating the status metrics")
			validateTrafficManagerProfileMetricsEmitted(customRegistry)
		})

		It("Unregister the metrics", func() {
			// Unregister the custom registry
			unregisterTrafficManagerProfileMetrics(customRegistry)
		})
	})

	Context("When creating trafficManagerProfile and azure request failed because of too many requests", Ordered, func() {
		name := fakeprovider.ThrottledErrProfileName
		var profile *fleetnetv1beta1.TrafficManagerProfile
		var wantMetrics []*prometheusclientmodel.Metric

		It("Initializing the custom registry", func() {
			initializeTrafficManagerProfileMetricsRegistry()
		})

		It("AzureTrafficManager should not be configured", func() {
			By("By creating a new TrafficManagerProfile")
			profile = trafficManagerProfileForTest(name)
			Expect(k8sClient.Create(ctx, profile)).Should(Succeed())

			By("By checking profile")
			want := fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:       name,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerProfileFinalizer, objectmeta.MetricsFinalizer},
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
			validator.ValidateTrafficManagerProfile(ctx, k8sClient, &want, timeout)

			By("By validating the status metrics")
			wantMetrics = append(wantMetrics, generateMetrics(profile, want.Status.Conditions[0]))
			validateTrafficManagerProfileMetricsEmitted(customRegistry, wantMetrics...)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating trafficManagerProfile is deleted", func() {
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, types.NamespacedName{Namespace: testNamespace, Name: name}, timeout)

			By("By validating the status metrics")
			validateTrafficManagerProfileMetricsEmitted(customRegistry)
		})

		It("Unregister the metrics", func() {
			// Unregister the custom registry
			unregisterTrafficManagerProfileMetrics(customRegistry)
		})
	})

	Context("When creating trafficManagerProfile and azure request failed because of client side error", Ordered, func() {
		name := "bad-request"
		var profile *fleetnetv1beta1.TrafficManagerProfile
		var wantMetrics []*prometheusclientmodel.Metric

		It("Initializing the custom registry", func() {
			initializeTrafficManagerProfileMetricsRegistry()
		})

		It("AzureTrafficManager should not be configured", func() {
			By("By creating a new TrafficManagerProfile")
			profile = trafficManagerProfileForTest(name)
			Expect(k8sClient.Create(ctx, profile)).Should(Succeed())

			By("By checking profile")
			want := fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:       name,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerProfileFinalizer, objectmeta.MetricsFinalizer},
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
			validator.ValidateTrafficManagerProfile(ctx, k8sClient, &want, timeout)

			By("By validating the status metrics")
			wantMetrics = append(wantMetrics, generateMetrics(profile, want.Status.Conditions[0]))
			validateTrafficManagerProfileMetricsEmitted(customRegistry, wantMetrics...)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating trafficManagerProfile is deleted", func() {
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, types.NamespacedName{Namespace: testNamespace, Name: name}, timeout)

			By("By validating the status metrics")
			validateTrafficManagerProfileMetricsEmitted(customRegistry)
		})

		It("Unregister the metrics", func() {
			// Unregister the custom registry
			unregisterTrafficManagerProfileMetrics(customRegistry)
		})
	})

	Context("When creating trafficManagerProfile and azure request failed because of internal server error", Ordered, func() {
		name := fakeprovider.InternalServerErrProfileName
		var profile *fleetnetv1beta1.TrafficManagerProfile
		var wantMetrics []*prometheusclientmodel.Metric

		It("Initializing the custom registry", func() {
			initializeTrafficManagerProfileMetricsRegistry()
		})

		It("AzureTrafficManager should not be configured", func() {
			By("By creating a new TrafficManagerProfile")
			profile = trafficManagerProfileForTest(name)
			Expect(k8sClient.Create(ctx, profile)).Should(Succeed())

			By("By checking profile")
			want := fleetnetv1beta1.TrafficManagerProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:       name,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerProfileFinalizer, objectmeta.MetricsFinalizer},
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
			validator.ValidateTrafficManagerProfile(ctx, k8sClient, &want, timeout)

			By("By validating the status metrics")
			wantMetrics = append(wantMetrics, generateMetrics(profile, want.Status.Conditions[0]))
			validateTrafficManagerProfileMetricsEmitted(customRegistry, wantMetrics...)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating trafficManagerProfile is deleted", func() {
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, types.NamespacedName{Namespace: testNamespace, Name: name}, timeout)

			By("By validating the status metrics")
			validateTrafficManagerProfileMetricsEmitted(customRegistry)
		})

		It("Unregister the metrics", func() {
			// Unregister the custom registry
			unregisterTrafficManagerProfileMetrics(customRegistry)
		})
	})

	Context("When creating trafficManagerProfile and azure request failed because of not found resource group", Ordered, func() {
		name := fakeprovider.ValidProfileName
		var profile *fleetnetv1beta1.TrafficManagerProfile
		var wantMetrics []*prometheusclientmodel.Metric

		It("Initializing the custom registry", func() {
			initializeTrafficManagerProfileMetricsRegistry()
		})

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
					// no profile finalizer
					Finalizers: []string{objectmeta.MetricsFinalizer},
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
			validator.ValidateTrafficManagerProfile(ctx, k8sClient, &want, timeout)

			By("By validating the status metrics")
			wantMetrics = append(wantMetrics, generateMetrics(profile, want.Status.Conditions[0]))
			validateTrafficManagerProfileMetricsEmitted(customRegistry, wantMetrics...)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating if trafficManagerProfile is deleted", func() {
			name := types.NamespacedName{Namespace: testNamespace, Name: name}
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, name, timeout)

			By("By validating the status metrics")
			validateTrafficManagerProfileMetricsEmitted(customRegistry)
		})

		It("Unregister the metrics", func() {
			// Unregister the custom registry
			unregisterTrafficManagerProfileMetrics(customRegistry)
		})
	})
})
