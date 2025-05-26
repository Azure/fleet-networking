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
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	prometheusclientmodel "github.com/prometheus/client_model/go"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

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

func resetTrafficManagerProfileMetricsRegistry() {
	// Reset metrics before each test
	trafficManagerProfileStatusLastTimestampSeconds.Reset()
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
func validateTrafficManagerProfileMetricsEmitted(wantMetrics ...*prometheusclientmodel.Metric) {
	Eventually(func() error {
		metricFamilies, err := ctrlmetrics.Registry.Gather()
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

func validateEmittedEvents(profile *fleetnetv1beta1.TrafficManagerProfile, want []corev1.Event) {
	var got corev1.EventList
	Expect(k8sClient.List(ctx, &got, client.InNamespace(testNamespace),
		client.MatchingFieldsSelector{Selector: fields.OneTermEqualSelector("involvedObject.name", profile.Name)})).Should(Succeed())

	cmpOptions := []cmp.Option{
		cmpopts.SortSlices(func(a, b corev1.Event) bool {
			return a.LastTimestamp.Before(&b.LastTimestamp) // sort by time
		}),
		cmp.Comparer(func(a, b corev1.Event) bool {
			return a.Reason == b.Reason && a.Type == b.Type && a.ReportingController == b.ReportingController
		}),
	}
	diff := cmp.Diff(got.Items, want, cmpOptions...)
	Expect(diff).To(BeEmpty(), "Event list mismatch (-got, +want):\n%s", diff)
}

var _ = Describe("Test TrafficManagerProfile Controller", func() {
	Context("When updating existing valid trafficManagerProfile", Ordered, func() {
		name := fakeprovider.ValidProfileName
		var profile *fleetnetv1beta1.TrafficManagerProfile
		profileResourceID := fmt.Sprintf(fakeprovider.ProfileResourceIDFormat, fakeprovider.DefaultSubscriptionID, fakeprovider.DefaultResourceGroupName, name)

		relativeDNSName := fmt.Sprintf(DNSRelativeNameFormat, testNamespace, name)
		fqdn := fmt.Sprintf(fakeprovider.ProfileDNSNameFormat, relativeDNSName)
		var wantMetrics []*prometheusclientmodel.Metric
		var wantEvents []corev1.Event

		BeforeAll(func() {
			By("By Reset the metrics in registry")
			resetTrafficManagerProfileMetricsRegistry()

			By("By deleting all the events")
			Expect(k8sClient.DeleteAllOf(ctx, &corev1.Event{}, client.InNamespace(testNamespace))).Should(Succeed(), "failed to delete the events")
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
			validateTrafficManagerProfileMetricsEmitted(wantMetrics...)

			By("By validating events")
			event := corev1.Event{Type: corev1.EventTypeNormal, Reason: profileEventReasonProgrammed, ReportingController: ControllerName}
			wantEvents = append(wantEvents, event)
			validateEmittedEvents(profile, wantEvents)
		})

		It("Update the trafficManagerProfile spec and should fail", func() {
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: name}, profile)).Should(Succeed(), "failed to get the trafficManagerProfile")
			profile.Spec.MonitorConfig.IntervalInSeconds = ptr.To[int64](10)
			profile.Spec.MonitorConfig.TimeoutInSeconds = ptr.To[int64](10)
			Expect(k8sClient.Update(ctx, profile)).ShouldNot(Succeed(), "failed to update the trafficManagerProfile")
		})

		It("Updating trafficManagerProfile spec to valid and validating trafficManagerProfile status", func() {
			profile.Spec.MonitorConfig.IntervalInSeconds = ptr.To[int64](30)
			profile.Spec.MonitorConfig.TimeoutInSeconds = ptr.To[int64](10)
			Expect(k8sClient.Update(ctx, profile)).Should(Succeed(), "failed to update the trafficManagerProfile")

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
			// It overwrites the previous one as they have the same condition.
			validateTrafficManagerProfileMetricsEmitted(wantMetrics...)

			By("By validating events")
			validateEmittedEvents(profile, wantEvents)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating trafficManagerProfile is deleted", func() {
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, types.NamespacedName{Namespace: testNamespace, Name: name}, timeout)

			By("By validating the status metrics")
			validateTrafficManagerProfileMetricsEmitted()

			By("By validating event for deletion")
			event := corev1.Event{Type: corev1.EventTypeNormal, Reason: profileEventReasonDeleted, ReportingController: ControllerName}
			wantEvents = append(wantEvents, event)
			validateEmittedEvents(profile, wantEvents)
		})
	})

	Context("When updating existing valid trafficManagerProfile with no changes", Ordered, func() {
		name := fakeprovider.ValidProfileName
		var profile *fleetnetv1beta1.TrafficManagerProfile
		profileResourceID := fmt.Sprintf(fakeprovider.ProfileResourceIDFormat, fakeprovider.DefaultSubscriptionID, fakeprovider.DefaultResourceGroupName, name)
		var wantMetrics []*prometheusclientmodel.Metric

		BeforeAll(func() {
			By("By Reset the metrics in registry")
			resetTrafficManagerProfileMetricsRegistry()

			By("By deleting all the events")
			Expect(k8sClient.DeleteAllOf(ctx, &corev1.Event{}, client.InNamespace(testNamespace))).Should(Succeed(), "failed to delete the events")
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
			validateTrafficManagerProfileMetricsEmitted(wantMetrics...)

			By("By validating events and want no events")
			validateEmittedEvents(profile, nil)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating trafficManagerProfile is deleted", func() {
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, types.NamespacedName{Namespace: testNamespace, Name: name}, timeout)

			By("By validating the status metrics")
			validateTrafficManagerProfileMetricsEmitted()

			By("By validating events and want no events") // since we don't create any azure resource
			validateEmittedEvents(profile, nil)
		})
	})

	Context("When creating valid trafficManagerProfile with no changes, while Azure traffic manager returns nil DNS and nil ID", Ordered, func() {
		name := fakeprovider.ValidProfileWithUnexpectedResponse
		var profile *fleetnetv1beta1.TrafficManagerProfile
		var wantMetrics []*prometheusclientmodel.Metric
		var wantEvents []corev1.Event

		BeforeAll(func() {
			By("By Reset the metrics in registry")
			resetTrafficManagerProfileMetricsRegistry()

			By("By deleting all the events")
			Expect(k8sClient.DeleteAllOf(ctx, &corev1.Event{}, client.InNamespace(testNamespace))).Should(Succeed(), "failed to delete the events")
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
			validateTrafficManagerProfileMetricsEmitted(wantMetrics...)

			By("By validating events")
			event := corev1.Event{Type: corev1.EventTypeNormal, Reason: profileEventReasonProgrammed, ReportingController: ControllerName}
			wantEvents = append(wantEvents, event)
			validateEmittedEvents(profile, wantEvents)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating trafficManagerProfile is deleted", func() {
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, types.NamespacedName{Namespace: testNamespace, Name: name}, timeout)
			By("By validating the status metrics")
			validateTrafficManagerProfileMetricsEmitted()

			By("By validating events")
			event := corev1.Event{Type: corev1.EventTypeNormal, Reason: profileEventReasonDeleted, ReportingController: ControllerName}
			wantEvents = append(wantEvents, event)
			validateEmittedEvents(profile, wantEvents)
		})
	})

	Context("When creating trafficManagerProfile and DNS name is not available", Ordered, func() {
		name := fakeprovider.ConflictErrProfileName
		var profile *fleetnetv1beta1.TrafficManagerProfile
		var wantMetrics []*prometheusclientmodel.Metric
		var wantEvents []corev1.Event

		BeforeAll(func() {
			By("By Reset the metrics in registry")
			resetTrafficManagerProfileMetricsRegistry()

			By("By deleting all the events")
			Expect(k8sClient.DeleteAllOf(ctx, &corev1.Event{}, client.InNamespace(testNamespace))).Should(Succeed(), "failed to delete the events")
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
			validateTrafficManagerProfileMetricsEmitted(wantMetrics...)

			By("By validating events")
			event := corev1.Event{Type: corev1.EventTypeWarning, Reason: profileEventReasonAzureAPIError, ReportingController: ControllerName}
			wantEvents = append(wantEvents, event)
			validateEmittedEvents(profile, wantEvents)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating trafficManagerProfile is deleted", func() {
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, types.NamespacedName{Namespace: testNamespace, Name: name}, timeout)

			By("By validating the status metrics")
			validateTrafficManagerProfileMetricsEmitted()

			By("By validating events")
			event := corev1.Event{Type: corev1.EventTypeNormal, Reason: profileEventReasonDeleted, ReportingController: ControllerName}
			wantEvents = append(wantEvents, event)
			validateEmittedEvents(profile, wantEvents)
		})
	})

	Context("When creating trafficManagerProfile and azure request failed because of too many requests", Ordered, func() {
		name := fakeprovider.ThrottledErrProfileName
		var profile *fleetnetv1beta1.TrafficManagerProfile
		var wantMetrics []*prometheusclientmodel.Metric
		var wantEvents []corev1.Event

		BeforeAll(func() {
			By("By Reset the metrics in registry")
			resetTrafficManagerProfileMetricsRegistry()

			By("By deleting all the events")
			Expect(k8sClient.DeleteAllOf(ctx, &corev1.Event{}, client.InNamespace(testNamespace))).Should(Succeed(), "failed to delete the events")
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
			validateTrafficManagerProfileMetricsEmitted(wantMetrics...)

			By("By validating events")
			event := corev1.Event{Type: corev1.EventTypeWarning, Reason: profileEventReasonAzureAPIError, ReportingController: ControllerName}
			wantEvents = append(wantEvents, event)
			validateEmittedEvents(profile, wantEvents)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating trafficManagerProfile is deleted", func() {
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, types.NamespacedName{Namespace: testNamespace, Name: name}, timeout)

			By("By validating the status metrics")
			validateTrafficManagerProfileMetricsEmitted()

			By("By validating events")
			event := corev1.Event{Type: corev1.EventTypeNormal, Reason: profileEventReasonDeleted, ReportingController: ControllerName}
			wantEvents = append(wantEvents, event)
			validateEmittedEvents(profile, wantEvents)
		})
	})

	Context("When creating trafficManagerProfile and azure request failed because of client side error", Ordered, func() {
		name := "bad-request"
		var profile *fleetnetv1beta1.TrafficManagerProfile
		var wantMetrics []*prometheusclientmodel.Metric
		var wantEvents []corev1.Event

		BeforeAll(func() {
			By("By Reset the metrics in registry")
			resetTrafficManagerProfileMetricsRegistry()

			By("By deleting all the events")
			Expect(k8sClient.DeleteAllOf(ctx, &corev1.Event{}, client.InNamespace(testNamespace))).Should(Succeed(), "failed to delete the events")
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
			validateTrafficManagerProfileMetricsEmitted(wantMetrics...)

			By("By validating events")
			event := corev1.Event{Type: corev1.EventTypeWarning, Reason: profileEventReasonAzureAPIError, ReportingController: ControllerName}
			wantEvents = append(wantEvents, event)
			validateEmittedEvents(profile, wantEvents)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating trafficManagerProfile is deleted", func() {
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, types.NamespacedName{Namespace: testNamespace, Name: name}, timeout)

			By("By validating the status metrics")
			validateTrafficManagerProfileMetricsEmitted()

			By("By validating events")
			event := corev1.Event{Type: corev1.EventTypeNormal, Reason: profileEventReasonDeleted, ReportingController: ControllerName}
			wantEvents = append(wantEvents, event)
			validateEmittedEvents(profile, wantEvents)
		})
	})

	Context("When creating trafficManagerProfile and azure request failed because of internal server error", Ordered, func() {
		name := fakeprovider.InternalServerErrProfileName
		var profile *fleetnetv1beta1.TrafficManagerProfile
		var wantMetrics []*prometheusclientmodel.Metric
		var wantEvents []corev1.Event

		BeforeAll(func() {
			By("By Reset the metrics in registry")
			resetTrafficManagerProfileMetricsRegistry()

			By("By deleting all the events")
			Expect(k8sClient.DeleteAllOf(ctx, &corev1.Event{}, client.InNamespace(testNamespace))).Should(Succeed(), "failed to delete the events")
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
			validateTrafficManagerProfileMetricsEmitted(wantMetrics...)

			By("By validating events")
			event := corev1.Event{Type: corev1.EventTypeWarning, Reason: profileEventReasonAzureAPIError, ReportingController: ControllerName}
			wantEvents = append(wantEvents, event)
			validateEmittedEvents(profile, wantEvents)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating trafficManagerProfile is deleted", func() {
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, types.NamespacedName{Namespace: testNamespace, Name: name}, timeout)

			By("By validating the status metrics")
			validateTrafficManagerProfileMetricsEmitted()

			By("By validating events")
			event := corev1.Event{Type: corev1.EventTypeNormal, Reason: profileEventReasonDeleted, ReportingController: ControllerName}
			wantEvents = append(wantEvents, event)
			validateEmittedEvents(profile, wantEvents)
		})
	})

	Context("When creating trafficManagerProfile and azure request failed because of not found resource group", Ordered, func() {
		name := fakeprovider.ValidProfileName
		var profile *fleetnetv1beta1.TrafficManagerProfile
		var wantMetrics []*prometheusclientmodel.Metric
		var wantEvents []corev1.Event

		BeforeAll(func() {
			By("By Reset the metrics in registry")
			resetTrafficManagerProfileMetricsRegistry()

			By("By deleting all the events")
			Expect(k8sClient.DeleteAllOf(ctx, &corev1.Event{}, client.InNamespace(testNamespace))).Should(Succeed(), "failed to delete the events")
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
			validateTrafficManagerProfileMetricsEmitted(wantMetrics...)

			By("By validating events")
			event := corev1.Event{Type: corev1.EventTypeWarning, Reason: profileEventReasonAzureAPIError, ReportingController: ControllerName}
			wantEvents = append(wantEvents, event)
			validateEmittedEvents(profile, wantEvents)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating if trafficManagerProfile is deleted", func() {
			name := types.NamespacedName{Namespace: testNamespace, Name: name}
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, name, timeout)

			By("By validating the status metrics")
			validateTrafficManagerProfileMetricsEmitted()

			By("By validating events")
			validateEmittedEvents(profile, wantEvents) // azure api error
		})
	})

	Context("When creating trafficManagerProfile with custom headers", Ordered, func() {
		name := fakeprovider.ValidProfileName
		var profile *fleetnetv1beta1.TrafficManagerProfile
		profileResourceID := fmt.Sprintf(fakeprovider.ProfileResourceIDFormat, fakeprovider.DefaultSubscriptionID, fakeprovider.DefaultResourceGroupName, name)
		hostHeaderName := "Host"
		hostHeaderValue := "myapp.example.com"
		var wantMetrics []*prometheusclientmodel.Metric

		It("Reset the metrics in registry", func() {
			resetTrafficManagerProfileMetricsRegistry()
		})

		It("AzureTrafficManager should be configured with custom headers", func() {
			By("By creating a new TrafficManagerProfile with custom headers")
			profile = trafficManagerProfileForTest(name)
			profile.Spec.MonitorConfig.CustomHeaders = []fleetnetv1beta1.MonitorConfigCustomHeader{
				{
					Name:  hostHeaderName,
					Value: hostHeaderValue,
				},
			}
			Expect(k8sClient.Create(ctx, profile)).Should(Succeed())

			relativeDNSName := fmt.Sprintf(DNSRelativeNameFormat, testNamespace, name)
			fqdn := fmt.Sprintf(fakeprovider.ProfileDNSNameFormat, relativeDNSName)

			By("By checking profile with custom headers")
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
			validateTrafficManagerProfileMetricsEmitted(wantMetrics...)
		})

		It("Updating trafficManagerProfile with different custom headers", func() {
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: name}, profile)).Should(Succeed(), "failed to get the trafficManagerProfile")

			// Update custom headers
			profile.Spec.MonitorConfig.CustomHeaders = []fleetnetv1beta1.MonitorConfigCustomHeader{
				{
					Name:  hostHeaderName,
					Value: "updated.example.com", // Changed value
				},
				{
					Name:  "X-Custom-Header",
					Value: "custom-value", // Added new header
				},
			}

			Expect(k8sClient.Update(ctx, profile)).Should(Succeed(), "failed to update the trafficManagerProfile")
			
			// Re-get the profile to get the updated generation number
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: name}, profile)).Should(Succeed(), "failed to get the updated trafficManagerProfile")

			relativeDNSName := fmt.Sprintf(DNSRelativeNameFormat, testNamespace, name)
			fqdn := fmt.Sprintf(fakeprovider.ProfileDNSNameFormat, relativeDNSName)

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

			// Clear the previous metrics and create a new one with the updated generation
			wantMetrics = []*prometheusclientmodel.Metric{generateMetrics(profile, want.Status.Conditions[0])}
			validateTrafficManagerProfileMetricsEmitted(wantMetrics...)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating trafficManagerProfile is deleted", func() {
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, types.NamespacedName{Namespace: testNamespace, Name: name}, timeout)

			By("By validating the status metrics")
			validateTrafficManagerProfileMetricsEmitted()
		})
	})
})
