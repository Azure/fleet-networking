/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package trafficmanagerbackend

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	prometheusclientmodel "github.com/prometheus/client_model/go"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	fleetnetv1beta1 "go.goms.io/fleet-networking/api/v1beta1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/test/common/metrics"
	"go.goms.io/fleet-networking/test/common/trafficmanager/fakeprovider"
	"go.goms.io/fleet-networking/test/common/trafficmanager/validator"
)

const (
	timeout     = time.Second * 10
	longTimeout = time.Second * 60
	interval    = time.Millisecond * 250
)

var (
	testNamespace = fakeprovider.ProfileNamespace
	serviceName   = fakeprovider.ServiceImportName
	backendWeight = int64(10)
	// Define event constants for testing
	wantAcceptedEvent      = corev1.Event{Type: corev1.EventTypeNormal, Reason: backendEventReasonAccepted, ReportingController: ControllerName}
	wantDeletedEvent       = corev1.Event{Type: corev1.EventTypeNormal, Reason: backendEventReasonDeleted, ReportingController: ControllerName}
	wantAzureAPIErrorEvent = corev1.Event{Type: corev1.EventTypeWarning, Reason: backendEventReasonAzureAPIError, ReportingController: ControllerName}
)

func resetTrafficManagerBackendMetricsRegistry() {
	trafficManagerBackendStatusLastTimestampSeconds.Reset()
}

func trafficManagerBackendForTest(name, profileName, serviceImportName string) *fleetnetv1beta1.TrafficManagerBackend {
	return &fleetnetv1beta1.TrafficManagerBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: fleetnetv1beta1.TrafficManagerBackendSpec{
			Profile: fleetnetv1beta1.TrafficManagerProfileRef{
				Name: profileName,
			},
			Backend: fleetnetv1beta1.TrafficManagerBackendRef{
				Name: serviceImportName,
			},
			Weight: ptr.To(backendWeight),
		},
	}
}

func trafficManagerProfileForTest(name string) *fleetnetv1beta1.TrafficManagerProfile {
	return &fleetnetv1beta1.TrafficManagerProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: fleetnetv1beta1.TrafficManagerProfileSpec{
			ResourceGroup: fakeprovider.DefaultResourceGroupName,
		},
	}
}

func buildFalseCondition(generation int64) []metav1.Condition {
	return []metav1.Condition{
		{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetnetv1beta1.TrafficManagerBackendConditionAccepted),
			Reason:             string(fleetnetv1beta1.TrafficManagerBackendReasonInvalid),
			ObservedGeneration: generation,
		},
	}
}

func buildUnknownCondition(generation int64) []metav1.Condition {
	return []metav1.Condition{
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetnetv1beta1.TrafficManagerBackendConditionAccepted),
			Reason:             string(fleetnetv1beta1.TrafficManagerBackendReasonPending),
			ObservedGeneration: generation,
		},
	}
}

func buildTrueCondition(generation int64) []metav1.Condition {
	return []metav1.Condition{
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetnetv1beta1.TrafficManagerBackendConditionAccepted),
			Reason:             string(fleetnetv1beta1.TrafficManagerBackendReasonAccepted),
			ObservedGeneration: generation,
		},
	}
}

func updateTrafficManagerProfileStatusToTrue(ctx context.Context, profile *fleetnetv1beta1.TrafficManagerProfile) {
	cond := metav1.Condition{
		Status:             metav1.ConditionTrue,
		Type:               string(fleetnetv1beta1.TrafficManagerProfileConditionProgrammed),
		ObservedGeneration: profile.Generation,
		Reason:             string(fleetnetv1beta1.TrafficManagerProfileReasonProgrammed),
	}
	meta.SetStatusCondition(&profile.Status.Conditions, cond)
	Expect(k8sClient.Status().Update(ctx, profile)).Should(Succeed())
}

// validateTrafficManagerBackendMetricsEmitted validates the trafficManagerBackend status metrics are emitted and are emitted in the correct order.
func validateTrafficManagerBackendMetricsEmitted(wantMetrics ...*prometheusclientmodel.Metric) {
	Eventually(func() error {
		metricFamilies, err := ctrlmetrics.Registry.Gather()
		if err != nil {
			return fmt.Errorf("failed to gather metrics: %w", err)
		}
		var gotMetrics []*prometheusclientmodel.Metric
		for _, mf := range metricFamilies {
			if mf.GetName() == "fleet_networking_traffic_manager_backend_status_last_timestamp_seconds" {
				gotMetrics = mf.GetMetric()
			}
		}

		if diff := cmp.Diff(gotMetrics, wantMetrics, metrics.CmpOptions...); diff != "" {
			return fmt.Errorf("trafficManagerBackend status metrics mismatch (-got, +want):\n%s", diff)
		}

		return nil
	}, timeout, interval).Should(Succeed(), "failed to validate trafficManagerBackend status metrics")
}

func generateMetrics(
	backend *fleetnetv1beta1.TrafficManagerBackend,
	condition metav1.Condition,
) *prometheusclientmodel.Metric {
	return &prometheusclientmodel.Metric{
		Label: []*prometheusclientmodel.LabelPair{
			{Name: ptr.To("namespace"), Value: &backend.Namespace},
			{Name: ptr.To("name"), Value: &backend.Name},
			{Name: ptr.To("generation"), Value: ptr.To(strconv.FormatInt(backend.Generation, 10))},
			{Name: ptr.To("condition"), Value: ptr.To(condition.Type)},
			{Name: ptr.To("status"), Value: ptr.To(string(condition.Status))},
			{Name: ptr.To("reason"), Value: ptr.To(condition.Reason)},
		},
		Gauge: &prometheusclientmodel.Gauge{
			Value: ptr.To(float64(time.Now().UnixNano()) / 1e9),
		},
	}
}

// validateEmittedEvents validates the events emitted for a trafficManagerBackend.
func validateEmittedEvents(backend *fleetnetv1beta1.TrafficManagerBackend, want []corev1.Event) {
	var got corev1.EventList
	Expect(k8sClient.List(ctx, &got, client.InNamespace(testNamespace),
		client.MatchingFieldsSelector{Selector: fields.OneTermEqualSelector("involvedObject.name", backend.Name)})).Should(Succeed())

	cmpOptions := []cmp.Option{
		cmpopts.SortSlices(func(a, b corev1.Event) bool {
			return a.LastTimestamp.Before(&b.LastTimestamp) // sort by time
		}),
		cmp.Comparer(func(a, b corev1.Event) bool {
			return a.Reason == b.Reason && a.Type == b.Type && a.ReportingController == b.ReportingController
		}),
	}
	diff := cmp.Diff(got.Items, want, cmpOptions...)
	Expect(diff).To(BeEmpty(), "Event list mismatch (-got, +want):\n%s, %v", diff, got.Items)
}

var _ = Describe("Test TrafficManagerBackend Controller", func() {
	BeforeEach(OncePerOrdered, func() {
		By("By Reset the metrics in registry")
		resetTrafficManagerBackendMetricsRegistry()

		By("By deleting all the events")
		Expect(k8sClient.DeleteAllOf(ctx, &corev1.Event{}, client.InNamespace(testNamespace))).Should(Succeed(), "failed to delete the events")
	})

	Context("When creating trafficManagerBackend with invalid profile", Ordered, func() {
		name := fakeprovider.ValidBackendName
		namespacedName := types.NamespacedName{Namespace: testNamespace, Name: name}
		var backend *fleetnetv1beta1.TrafficManagerBackend

		var wantMetrics []*prometheusclientmodel.Metric

		It("Creating TrafficManagerBackend", func() {
			backend = trafficManagerBackendForTest(name, "not-exist", "not-exist")
			Expect(k8sClient.Create(ctx, backend)).Should(Succeed())
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       name,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.MetricsFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildFalseCondition(backend.Generation),
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want, timeout)

			By("By validating the status metrics")
			wantMetrics = append(wantMetrics, generateMetrics(backend, want.Status.Conditions[0]))
			validateTrafficManagerBackendMetricsEmitted(wantMetrics...)
		})

		It("Deleting trafficManagerBackend", func() {
			err := k8sClient.Delete(ctx, backend)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerBackend")
		})

		It("Validating trafficManagerBackend is deleted", func() {
			validator.IsTrafficManagerBackendDeleted(ctx, k8sClient, namespacedName, timeout)

			By("By validating the status metrics")
			validateTrafficManagerBackendMetricsEmitted()
		})
	})

	Context("When creating trafficManagerBackend with not found Azure Traffic Manager profile", Ordered, func() {
		profileName := "not-found-azure-profile"
		profileNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: profileName}
		var profile *fleetnetv1beta1.TrafficManagerProfile
		backendName := fakeprovider.ValidBackendName
		backendNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: backendName}
		var backend *fleetnetv1beta1.TrafficManagerBackend

		var wantMetrics []*prometheusclientmodel.Metric
		var wantEvents []corev1.Event

		It("Creating a new TrafficManagerProfile", func() {
			By("By creating a new TrafficManagerProfile")
			profile = trafficManagerProfileForTest(profileName)
			Expect(k8sClient.Create(ctx, profile)).Should(Succeed())
		})

		It("Creating TrafficManagerBackend", func() {
			backend = trafficManagerBackendForTest(backendName, profileName, "not-exist")
			Expect(k8sClient.Create(ctx, backend)).Should(Succeed())
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.MetricsFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildUnknownCondition(backend.Generation),
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want, timeout)

			By("By validating the status metrics")
			wantMetrics = append(wantMetrics, generateMetrics(backend, want.Status.Conditions[0]))
			validateTrafficManagerBackendMetricsEmitted(wantMetrics...)

			By("By validating events")
			// No events should be emitted for the backend as the profile is invalid.
			validateEmittedEvents(backend, nil)
		})

		It("Updating TrafficManagerProfile status to programmed true and it should trigger controller", func() {
			By("By updating TrafficManagerProfile status")
			updateTrafficManagerProfileStatusToTrue(ctx, profile)
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.MetricsFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildFalseCondition(backend.Generation),
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want, timeout)

			By("By validating the status metrics")
			wantMetrics = append(wantMetrics, generateMetrics(backend, want.Status.Conditions[0]))
			validateTrafficManagerBackendMetricsEmitted(wantMetrics...)

			By("By validating events")
			wantEvents = append(wantEvents, wantAzureAPIErrorEvent)
			validateEmittedEvents(backend, wantEvents)
		})

		It("Deleting trafficManagerBackend", func() {
			err := k8sClient.Delete(ctx, backend)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerBackend")
		})

		It("Validating trafficManagerBackend is deleted", func() {
			validator.IsTrafficManagerBackendDeleted(ctx, k8sClient, backendNamespacedName, timeout)

			By("By validating the status metrics")
			validateTrafficManagerBackendMetricsEmitted()

			By("By validating events")
			// No events should be emitted for deletion as the backend was never accepted.
			validateEmittedEvents(backend, wantEvents)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating trafficManagerProfile is deleted", func() {
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, profileNamespacedName, timeout)
		})
	})

	Context("When creating trafficManagerBackend and failing to get Azure Traffic Manager profile", Ordered, func() {
		profileName := fakeprovider.RequestTimeoutProfileName
		profileNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: profileName}
		var profile *fleetnetv1beta1.TrafficManagerProfile
		backendName := fakeprovider.ValidBackendName
		backendNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: backendName}
		var backend *fleetnetv1beta1.TrafficManagerBackend

		var wantMetrics []*prometheusclientmodel.Metric
		var wantEvents []corev1.Event

		It("Creating a new TrafficManagerProfile", func() {
			By("By creating a new TrafficManagerProfile")
			profile = trafficManagerProfileForTest(profileName)
			Expect(k8sClient.Create(ctx, profile)).Should(Succeed())
		})

		It("Updating TrafficManagerProfile status to programmed true", func() {
			By("By updating TrafficManagerProfile status")
			updateTrafficManagerProfileStatusToTrue(ctx, profile)
		})

		It("Creating TrafficManagerBackend", func() {
			backend = trafficManagerBackendForTest(backendName, profileName, "not-exist")
			Expect(k8sClient.Create(ctx, backend)).Should(Succeed())
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.MetricsFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildUnknownCondition(backend.Generation),
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want, timeout)

			By("By validating the status metrics")
			wantMetrics = append(wantMetrics, generateMetrics(backend, want.Status.Conditions[0]))
			validateTrafficManagerBackendMetricsEmitted(wantMetrics...)

			By("By validating events")
			wantEvents = append(wantEvents, wantAzureAPIErrorEvent)
			validateEmittedEvents(backend, wantEvents)
		})

		It("Deleting trafficManagerBackend", func() {
			err := k8sClient.Delete(ctx, backend)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerBackend")
		})

		It("Validating trafficManagerBackend is deleted", func() {
			validator.IsTrafficManagerBackendDeleted(ctx, k8sClient, backendNamespacedName, timeout)

			By("By validating the status metrics")
			validateTrafficManagerBackendMetricsEmitted()

			By("By validating events")
			// No events should be emitted for deletion as the backend was never accepted.
			validateEmittedEvents(backend, wantEvents)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating trafficManagerProfile is deleted", func() {
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, profileNamespacedName, timeout)
		})
	})

	Context("When creating trafficManagerBackend with Azure Traffic Manager profile which has nil properties", Ordered, func() {
		profileName := fakeprovider.ValidProfileWithNilPropertiesName
		profileNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: profileName}
		var profile *fleetnetv1beta1.TrafficManagerProfile
		backendName := fakeprovider.ValidBackendName
		backendNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: backendName}
		var backend *fleetnetv1beta1.TrafficManagerBackend

		var wantMetrics []*prometheusclientmodel.Metric

		It("Creating a new TrafficManagerProfile", func() {
			By("By creating a new TrafficManagerProfile")
			profile = trafficManagerProfileForTest(profileName)
			Expect(k8sClient.Create(ctx, profile)).Should(Succeed())
		})

		It("Updating TrafficManagerProfile status to programmed true", func() {
			By("By updating TrafficManagerProfile status")
			updateTrafficManagerProfileStatusToTrue(ctx, profile)
		})

		It("Creating TrafficManagerBackend", func() {
			backend = trafficManagerBackendForTest(backendName, profileName, "not-exist")
			Expect(k8sClient.Create(ctx, backend)).Should(Succeed())
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.MetricsFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildFalseCondition(backend.Generation),
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want, timeout)

			By("By validating the status metrics")
			wantMetrics = append(wantMetrics, generateMetrics(backend, want.Status.Conditions[0]))
			validateTrafficManagerBackendMetricsEmitted(wantMetrics...)
		})

		It("Deleting trafficManagerBackend", func() {
			err := k8sClient.Delete(ctx, backend)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerBackend")
		})

		It("Validating trafficManagerBackend is deleted", func() {
			validator.IsTrafficManagerBackendDeleted(ctx, k8sClient, backendNamespacedName, timeout)

			By("By validating the status metrics")
			validateTrafficManagerBackendMetricsEmitted()

			By("By validating events")
			// No events should be emitted for deletion as the backend was never accepted.
			validateEmittedEvents(backend, nil)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating trafficManagerProfile is deleted", func() {
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, profileNamespacedName, timeout)
		})
	})

	Context("When creating trafficManagerBackend with not accepted profile", Ordered, func() {
		profileName := fakeprovider.ValidProfileWithEndpointsName
		profileNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: profileName}
		var profile *fleetnetv1beta1.TrafficManagerProfile
		backendName := fakeprovider.ValidBackendName
		backendNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: backendName}
		var backend *fleetnetv1beta1.TrafficManagerBackend
		var wantMetrics []*prometheusclientmodel.Metric

		It("Creating a new TrafficManagerProfile", func() {
			By("By creating a new TrafficManagerProfile")
			profile = trafficManagerProfileForTest(profileName)
			Expect(k8sClient.Create(ctx, profile)).Should(Succeed())
		})

		It("Updating TrafficManagerProfile status to programmed false", func() {
			By("By updating TrafficManagerProfile status")
			cond := metav1.Condition{
				Status:             metav1.ConditionFalse,
				Type:               string(fleetnetv1beta1.TrafficManagerProfileConditionProgrammed),
				ObservedGeneration: profile.Generation,
				Reason:             string(fleetnetv1beta1.TrafficManagerProfileReasonInvalid),
			}
			meta.SetStatusCondition(&profile.Status.Conditions, cond)
			Expect(k8sClient.Status().Update(ctx, profile)).Should(Succeed())
		})

		It("Creating TrafficManagerBackend", func() {
			backend = trafficManagerBackendForTest(backendName, profileName, "not-exist")
			Expect(k8sClient.Create(ctx, backend)).Should(Succeed())
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.MetricsFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildFalseCondition(backend.Generation),
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want, timeout)

			By("By validating the status metrics")
			wantMetrics = append(wantMetrics, generateMetrics(backend, want.Status.Conditions[0]))
			validateTrafficManagerBackendMetricsEmitted(wantMetrics...)
		})

		It("Updating TrafficManagerProfile status to accepted unknown and it should trigger controller", func() {
			By("By updating TrafficManagerProfile status")
			cond := metav1.Condition{
				Status:             metav1.ConditionUnknown,
				Type:               string(fleetnetv1beta1.TrafficManagerProfileConditionProgrammed),
				ObservedGeneration: profile.Generation,
				Reason:             string(fleetnetv1beta1.TrafficManagerProfileReasonPending),
			}
			meta.SetStatusCondition(&profile.Status.Conditions, cond)
			Expect(k8sClient.Status().Update(ctx, profile)).Should(Succeed())
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.MetricsFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildUnknownCondition(backend.Generation),
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want, timeout)

			By("By validating the status metrics")
			wantMetrics = append(wantMetrics, generateMetrics(backend, want.Status.Conditions[0]))
			validateTrafficManagerBackendMetricsEmitted(wantMetrics...)
		})

		It("Deleting trafficManagerBackend", func() {
			err := k8sClient.Delete(ctx, backend)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerBackend")
		})

		It("Validating trafficManagerBackend is deleted", func() {
			validator.IsTrafficManagerBackendDeleted(ctx, k8sClient, backendNamespacedName, timeout)

			By("By validating the status metrics")
			validateTrafficManagerBackendMetricsEmitted()

			By("By validating events")
			// No events should be emitted for deletion as the backend was never accepted.
			validateEmittedEvents(backend, nil)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating trafficManagerProfile is deleted", func() {
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, profileNamespacedName, timeout)
		})
	})

	Context("When creating trafficManagerBackend with invalid serviceImport (successfully delete stale endpoints)", Ordered, func() {
		profileName := fakeprovider.ValidProfileWithEndpointsName
		profileNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: profileName}
		var profile *fleetnetv1beta1.TrafficManagerProfile
		backendName := fakeprovider.ValidBackendName
		backendNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: backendName}
		var backend *fleetnetv1beta1.TrafficManagerBackend
		var wantMetrics []*prometheusclientmodel.Metric

		It("Creating a new TrafficManagerProfile", func() {
			By("By creating a new TrafficManagerProfile")
			profile = trafficManagerProfileForTest(profileName)
			Expect(k8sClient.Create(ctx, profile)).Should(Succeed())
		})

		It("Updating TrafficManagerProfile status to programmed true", func() {
			By("By updating TrafficManagerProfile status")
			updateTrafficManagerProfileStatusToTrue(ctx, profile)
		})

		It("Creating TrafficManagerBackend", func() {
			backend = trafficManagerBackendForTest(backendName, profileName, "not-exist")
			Expect(k8sClient.Create(ctx, backend)).Should(Succeed())
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.MetricsFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildFalseCondition(backend.Generation),
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want, timeout)

			By("By validating the status metrics")
			wantMetrics = append(wantMetrics, generateMetrics(backend, want.Status.Conditions[0]))
			validateTrafficManagerBackendMetricsEmitted(wantMetrics...)
		})

		It("Deleting trafficManagerBackend", func() {
			err := k8sClient.Delete(ctx, backend)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerBackend")
		})

		It("Validating trafficManagerBackend is deleted", func() {
			validator.IsTrafficManagerBackendDeleted(ctx, k8sClient, backendNamespacedName, timeout)

			By("By validating the status metrics")
			validateTrafficManagerBackendMetricsEmitted()

			By("By validating events")
			// No events should be emitted for deletion as the backend was never accepted.
			validateEmittedEvents(backend, nil)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating trafficManagerProfile is deleted", func() {
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, profileNamespacedName, timeout)
		})
	})

	Context("When creating trafficManagerBackend with invalid serviceImport (fail to delete stale endpoints)", Ordered, func() {
		profileName := fakeprovider.ValidProfileWithFailToDeleteEndpointName
		profileNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: profileName}
		var profile *fleetnetv1beta1.TrafficManagerProfile
		backendName := fakeprovider.ValidBackendName
		backendNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: backendName}
		var backend *fleetnetv1beta1.TrafficManagerBackend

		var wantEvents []corev1.Event

		It("Creating a new TrafficManagerProfile", func() {
			By("By creating a new TrafficManagerProfile")
			profile = trafficManagerProfileForTest(profileName)
			Expect(k8sClient.Create(ctx, profile)).Should(Succeed())
		})

		It("Updating TrafficManagerProfile status to accepted true", func() {
			By("By updating TrafficManagerProfile status")
			updateTrafficManagerProfileStatusToTrue(ctx, profile)
		})

		It("Creating TrafficManagerBackend", func() {
			backend = trafficManagerBackendForTest(backendName, profileName, "not-exist")
			Expect(k8sClient.Create(ctx, backend)).Should(Succeed())
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.MetricsFinalizer},
				},
				Spec: backend.Spec,
				// not able to set the condition
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want, timeout)

			By("By validating the status metrics")
			validateTrafficManagerBackendMetricsEmitted()

			By("By validating events")
			wantEvents = append(wantEvents, wantAzureAPIErrorEvent)
			validateEmittedEvents(backend, wantEvents)
		})

		It("Deleting trafficManagerBackend", func() {
			err := k8sClient.Delete(ctx, backend)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerBackend")
		})

		It("Validating trafficManagerBackend is deleted", func() {
			validator.IsTrafficManagerBackendDeleted(ctx, k8sClient, backendNamespacedName, timeout)

			By("By validating the status metrics")
			validateTrafficManagerBackendMetricsEmitted()

			By("By validating events")
			// No events should be emitted for deletion as the backend was never accepted.
			validateEmittedEvents(backend, wantEvents)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating trafficManagerProfile is deleted", func() {
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, profileNamespacedName, timeout)
		})
	})

	Context("When creating trafficManagerBackend with valid serviceImport but internalServiceExport is not found", Ordered, func() {
		profileName := fakeprovider.ValidProfileWithEndpointsName
		profileNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: profileName}
		var profile *fleetnetv1beta1.TrafficManagerProfile
		backendName := fakeprovider.ValidBackendName
		backendNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: backendName}
		var backend *fleetnetv1beta1.TrafficManagerBackend

		var serviceImport *fleetnetv1alpha1.ServiceImport

		var wantMetrics []*prometheusclientmodel.Metric

		It("Creating a new TrafficManagerProfile", func() {
			By("By creating a new TrafficManagerProfile")
			profile = trafficManagerProfileForTest(profileName)
			Expect(k8sClient.Create(ctx, profile)).Should(Succeed())
		})

		It("Updating TrafficManagerProfile status to programmed true", func() {
			By("By updating TrafficManagerProfile status")
			updateTrafficManagerProfileStatusToTrue(ctx, profile)
		})

		It("Creating TrafficManagerBackend", func() {
			backend = trafficManagerBackendForTest(backendName, profileName, serviceName)
			Expect(k8sClient.Create(ctx, backend)).Should(Succeed())
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.MetricsFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildFalseCondition(backend.Generation),
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want, timeout)

			By("By validating the status metrics")
			wantMetrics = append(wantMetrics, generateMetrics(backend, want.Status.Conditions[0]))
			validateTrafficManagerBackendMetricsEmitted(wantMetrics...)
		})

		It("Creating a new ServiceImport", func() {
			serviceImport = &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: testNamespace,
				},
			}
			Expect(k8sClient.Create(ctx, serviceImport)).Should(Succeed(), "failed to create serviceImport")
		})

		It("Updating the ServiceImport status", func() {
			serviceImport.Status = fleetnetv1alpha1.ServiceImportStatus{
				Clusters: []fleetnetv1alpha1.ClusterStatus{
					{
						Cluster: memberClusterNames[2], // not found internalServiceExport
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, serviceImport)).Should(Succeed(), "failed to create serviceImport")
		})

		It("Validating trafficManagerBackend and should trigger controller to reconcile", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.MetricsFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildUnknownCondition(backend.Generation),
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want, timeout)

			By("By validating the status metrics")
			wantMetrics = append(wantMetrics, generateMetrics(backend, want.Status.Conditions[0]))
			validateTrafficManagerBackendMetricsEmitted(wantMetrics...)
		})

		It("Deleting trafficManagerBackend", func() {
			err := k8sClient.Delete(ctx, backend)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerBackend")
		})

		It("Validating trafficManagerBackend is deleted", func() {
			validator.IsTrafficManagerBackendDeleted(ctx, k8sClient, backendNamespacedName, timeout)

			By("By validating the status metrics")
			validateTrafficManagerBackendMetricsEmitted()

			By("By validating events")
			// No events should be emitted for deletion as the backend was never accepted.
			validateEmittedEvents(backend, nil)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating trafficManagerProfile is deleted", func() {
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, profileNamespacedName, timeout)
		})

		It("Deleting serviceImport", func() {
			deleteServiceImport(types.NamespacedName{Namespace: testNamespace, Name: serviceName})
		})
	})

	// The test below is too complex to validate the emitted metrics. Skipping it for now.
	Context("When creating trafficManagerBackend with valid serviceImport and internalServiceExports", Ordered, func() {
		profileName := fakeprovider.ValidProfileWithEndpointsName
		profileNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: profileName}
		var profile *fleetnetv1beta1.TrafficManagerProfile
		backendName := fakeprovider.ValidBackendName
		backendNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: backendName}
		var backend *fleetnetv1beta1.TrafficManagerBackend

		var serviceImport *fleetnetv1alpha1.ServiceImport

		var wantMetrics []*prometheusclientmodel.Metric

		It("Creating a new TrafficManagerProfile", func() {
			By("By creating a new TrafficManagerProfile")
			profile = trafficManagerProfileForTest(profileName)
			Expect(k8sClient.Create(ctx, profile)).Should(Succeed())
		})

		It("Updating TrafficManagerProfile status to programmed true", func() {
			By("By updating TrafficManagerProfile status")
			updateTrafficManagerProfileStatusToTrue(ctx, profile)
		})

		It("Creating TrafficManagerBackend", func() {
			backend = trafficManagerBackendForTest(backendName, profileName, serviceName)
			Expect(k8sClient.Create(ctx, backend)).Should(Succeed())
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.MetricsFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildFalseCondition(backend.Generation),
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want, timeout)
			validator.ValidateTrafficManagerBackendConsistently(ctx, k8sClient, &want)

			By("By validating the status metrics")
			// Metrics are sorted by timestamp
			// * false
			wantMetrics = append(wantMetrics, generateMetrics(backend, want.Status.Conditions[0]))
			validateTrafficManagerBackendMetricsEmitted(wantMetrics...)
		})

		It("Creating a new ServiceImport", func() {
			serviceImport = &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: testNamespace,
				},
			}
			Expect(k8sClient.Create(ctx, serviceImport)).Should(Succeed(), "failed to create serviceImport")
		})

		It("Updating the ServiceImport status", func() {
			serviceImport.Status = fleetnetv1alpha1.ServiceImportStatus{
				Clusters: []fleetnetv1alpha1.ClusterStatus{
					{
						Cluster: memberClusterNames[0], // valid endpoint
					},
					{
						Cluster: memberClusterNames[1], // invalid endpoint
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, serviceImport)).Should(Succeed(), "failed to create serviceImport")
		})

		It("Validating trafficManagerBackend", func() {
			atmEndpointName := fmt.Sprintf(AzureResourceEndpointNameFormat, backendName+"#", serviceName, memberClusterNames[0])
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer, objectmeta.MetricsFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildFalseCondition(backend.Generation),
					Endpoints: []fleetnetv1beta1.TrafficManagerEndpointStatus{
						{
							Name: atmEndpointName,
							From: &fleetnetv1beta1.FromCluster{
								ClusterStatus: fleetnetv1beta1.ClusterStatus{
									Cluster: memberClusterNames[0],
								},
								Weight: ptr.To(int64(1)), // the original weight is default to 1
							},
							Weight:     ptr.To(backendWeight), // populate the weight using atm endpoint
							Target:     ptr.To(fakeprovider.ValidEndpointTarget),
							ResourceID: fmt.Sprintf(fakeprovider.EndpointResourceIDFormat, fakeprovider.DefaultSubscriptionID, fakeprovider.DefaultResourceGroupName, profileName, atmEndpointName),
						},
					},
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want, timeout)
			validator.ValidateTrafficManagerBackendConsistently(ctx, k8sClient, &want)

			By("By validating the status metrics")
			// Metrics are sorted by timestamp
			// * false
			validateTrafficManagerBackendMetricsEmitted(wantMetrics...)
		})

		It("Updating the ServiceImport status", func() {
			serviceImport.Status = fleetnetv1alpha1.ServiceImportStatus{
				Clusters: []fleetnetv1alpha1.ClusterStatus{
					{
						Cluster: memberClusterNames[0], // valid endpoint
					},
					{
						Cluster: memberClusterNames[3], // new endpoint
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, serviceImport)).Should(Succeed(), "failed to create serviceImport")
		})

		It("Validating trafficManagerBackend", func() {
			atmEndpointNames := []string{
				fmt.Sprintf(AzureResourceEndpointNameFormat, backendName+"#", serviceName, memberClusterNames[0]),
				fmt.Sprintf(AzureResourceEndpointNameFormat, backendName+"#", serviceName, memberClusterNames[3]),
			}
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer, objectmeta.MetricsFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildTrueCondition(backend.Generation),
					Endpoints: []fleetnetv1beta1.TrafficManagerEndpointStatus{
						{
							Name: atmEndpointNames[0],
							From: &fleetnetv1beta1.FromCluster{
								ClusterStatus: fleetnetv1beta1.ClusterStatus{
									Cluster: memberClusterNames[0],
								},
								Weight: ptr.To(int64(1)),
							},
							Weight:     ptr.To(backendWeight / 2), // populate the weight using atm endpoint
							Target:     ptr.To(fakeprovider.ValidEndpointTarget),
							ResourceID: fmt.Sprintf(fakeprovider.EndpointResourceIDFormat, fakeprovider.DefaultSubscriptionID, fakeprovider.DefaultResourceGroupName, profileName, atmEndpointNames[0]),
						},
						{
							Name: atmEndpointNames[1],
							From: &fleetnetv1beta1.FromCluster{
								ClusterStatus: fleetnetv1beta1.ClusterStatus{
									Cluster: memberClusterNames[3],
								},
								Weight: ptr.To(int64(1)),
							},
							Weight:     ptr.To(backendWeight / 2), // populate the weight using atm endpoint
							Target:     ptr.To(fakeprovider.ValidEndpointTarget),
							ResourceID: fmt.Sprintf(fakeprovider.EndpointResourceIDFormat, fakeprovider.DefaultSubscriptionID, fakeprovider.DefaultResourceGroupName, profileName, atmEndpointNames[1]),
						},
					},
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want, timeout)
			validator.ValidateTrafficManagerBackendConsistently(ctx, k8sClient, &want)

			By("By validating the status metrics")
			// Metrics are sorted by timestamp
			// * false
			// * true
			wantMetrics = append(wantMetrics, generateMetrics(backend, want.Status.Conditions[0]))
			validateTrafficManagerBackendMetricsEmitted(wantMetrics...)
		})

		It("Updating the internalServiceExport weight", func() {
			internalServiceExport := &fleetnetv1alpha1.InternalServiceExport{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: internalServiceExports[0].Namespace, Name: internalServiceExports[0].Name}, internalServiceExport)).Should(Succeed())
			internalServiceExport.Spec.Weight = ptr.To(int64(2))
			Expect(k8sClient.Update(ctx, internalServiceExport)).Should(Succeed(), "failed to create internalServiceExport")
		})

		It("Validating trafficManagerBackend", func() {
			atmEndpointNames := []string{
				fmt.Sprintf(AzureResourceEndpointNameFormat, backendName+"#", serviceName, memberClusterNames[0]),
				fmt.Sprintf(AzureResourceEndpointNameFormat, backendName+"#", serviceName, memberClusterNames[3]),
			}
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer, objectmeta.MetricsFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildTrueCondition(backend.Generation),
					Endpoints: []fleetnetv1beta1.TrafficManagerEndpointStatus{
						{
							Name: atmEndpointNames[0],
							From: &fleetnetv1beta1.FromCluster{
								ClusterStatus: fleetnetv1beta1.ClusterStatus{
									Cluster: memberClusterNames[0],
								},
								Weight: ptr.To(int64(2)),
							},
							Weight:     ptr.To(int64(7)), // 2/3 of the 10
							Target:     ptr.To(fakeprovider.ValidEndpointTarget),
							ResourceID: fmt.Sprintf(fakeprovider.EndpointResourceIDFormat, fakeprovider.DefaultSubscriptionID, fakeprovider.DefaultResourceGroupName, profileName, atmEndpointNames[0]),
						},
						{
							Name: atmEndpointNames[1],
							From: &fleetnetv1beta1.FromCluster{
								ClusterStatus: fleetnetv1beta1.ClusterStatus{
									Cluster: memberClusterNames[3],
								},
								Weight: ptr.To(int64(1)),
							},
							Weight:     ptr.To(int64(4)), // 1/3 of 10
							Target:     ptr.To(fakeprovider.ValidEndpointTarget),
							ResourceID: fmt.Sprintf(fakeprovider.EndpointResourceIDFormat, fakeprovider.DefaultSubscriptionID, fakeprovider.DefaultResourceGroupName, profileName, atmEndpointNames[1]),
						},
					},
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want, timeout)
			validator.ValidateTrafficManagerBackendConsistently(ctx, k8sClient, &want)

			By("By validating the status metrics")
			// Metrics are sorted by timestamp
			// * false
			// * true
			// It overwrites the previous one as they have the same condition.
			validateTrafficManagerBackendMetricsEmitted(wantMetrics...)
		})

		It("Updating the ServiceImport status", func() {
			serviceImport.Status = fleetnetv1alpha1.ServiceImportStatus{
				Clusters: []fleetnetv1alpha1.ClusterStatus{
					{
						Cluster: memberClusterNames[4], // would fail to create atm endpoint
					},
					{
						Cluster: memberClusterNames[0], // valid endpoint
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, serviceImport)).Should(Succeed(), "failed to create serviceImport")
		})

		It("Validating trafficManagerBackend", func() {
			atmEndpointName := fmt.Sprintf(AzureResourceEndpointNameFormat, backendName+"#", serviceName, memberClusterNames[0])
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer, objectmeta.MetricsFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildFalseCondition(backend.Generation),
					Endpoints: []fleetnetv1beta1.TrafficManagerEndpointStatus{
						{
							Name: atmEndpointName,
							From: &fleetnetv1beta1.FromCluster{
								ClusterStatus: fleetnetv1beta1.ClusterStatus{
									Cluster: memberClusterNames[0],
								},
								Weight: ptr.To(int64(2)),
							},
							Weight:     ptr.To(int64(7)), // 2/3 of the 10 as the weight is calculated before the endpoints are created
							Target:     ptr.To(fakeprovider.ValidEndpointTarget),
							ResourceID: fmt.Sprintf(fakeprovider.EndpointResourceIDFormat, fakeprovider.DefaultSubscriptionID, fakeprovider.DefaultResourceGroupName, profileName, atmEndpointName),
						},
					},
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want, timeout)
			validator.ValidateTrafficManagerBackendConsistently(ctx, k8sClient, &want)

			By("By validating the status metrics")
			// Metrics are sorted by timestamp
			// * true
			// * false
			wantMetrics = wantMetrics[1:] // The new one will overwrite the first one.
			wantMetrics = append(wantMetrics, generateMetrics(backend, want.Status.Conditions[0]))
			validateTrafficManagerBackendMetricsEmitted(wantMetrics...)
		})

		It("Updating the ServiceImport status", func() {
			serviceImport.Status = fleetnetv1alpha1.ServiceImportStatus{
				Clusters: []fleetnetv1alpha1.ClusterStatus{
					{
						Cluster: memberClusterNames[5], // would fail to create atm endpoint
					},
					{
						Cluster: memberClusterNames[0], // valid endpoint
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, serviceImport)).Should(Succeed(), "failed to create serviceImport")
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer, objectmeta.MetricsFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildUnknownCondition(backend.Generation),
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want, timeout)
			validator.ValidateTrafficManagerBackendConsistently(ctx, k8sClient, &want)

			By("By validating the status metrics")
			// Metrics are sorted by timestamp
			// * true
			// * false
			// * unknown
			wantMetrics = append(wantMetrics, generateMetrics(backend, want.Status.Conditions[0]))
			validateTrafficManagerBackendMetricsEmitted(wantMetrics...)
		})

		It("Updating the ServiceImport status", func() {
			serviceImport.Status = fleetnetv1alpha1.ServiceImportStatus{
				Clusters: []fleetnetv1alpha1.ClusterStatus{
					{
						Cluster: memberClusterNames[0], // valid endpoint
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, serviceImport)).Should(Succeed(), "failed to create serviceImport")
		})

		It("Validating trafficManagerBackend", func() {
			atmEndpointName := fmt.Sprintf(AzureResourceEndpointNameFormat, backendName+"#", serviceName, memberClusterNames[0])
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer, objectmeta.MetricsFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildTrueCondition(backend.Generation),
					Endpoints: []fleetnetv1beta1.TrafficManagerEndpointStatus{
						{
							Name: atmEndpointName,
							From: &fleetnetv1beta1.FromCluster{
								ClusterStatus: fleetnetv1beta1.ClusterStatus{
									Cluster: memberClusterNames[0],
								},
								Weight: ptr.To(int64(2)),
							},
							Weight:     ptr.To(int64(10)),
							Target:     ptr.To(fakeprovider.ValidEndpointTarget),
							ResourceID: fmt.Sprintf(fakeprovider.EndpointResourceIDFormat, fakeprovider.DefaultSubscriptionID, fakeprovider.DefaultResourceGroupName, profileName, atmEndpointName),
						},
					},
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want, timeout)
			validator.ValidateTrafficManagerBackendConsistently(ctx, k8sClient, &want)

			By("By validating the status metrics")
			// Metrics are sorted by timestamp
			// * false
			// * unknown
			// * true
			wantMetrics = wantMetrics[1:] // The new one will overwrite the first one.
			wantMetrics = append(wantMetrics, generateMetrics(backend, want.Status.Conditions[0]))
			validateTrafficManagerBackendMetricsEmitted(wantMetrics...)
		})

		It("Updating the internalServiceExport to invalid endpoint", func() {
			internalServiceExport := &fleetnetv1alpha1.InternalServiceExport{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: internalServiceExports[0].Namespace, Name: internalServiceExports[0].Name}, internalServiceExport)).Should(Succeed())
			internalServiceExport.Spec.IsInternalLoadBalancer = true
			Expect(k8sClient.Update(ctx, internalServiceExport)).Should(Succeed(), "failed to create internalServiceExport")
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer, objectmeta.MetricsFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildFalseCondition(backend.Generation),
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want, timeout)
			validator.ValidateTrafficManagerBackendConsistently(ctx, k8sClient, &want)

			By("By validating the status metrics")
			// Metrics are sorted by timestamp
			// * unknown
			// * true
			// * false
			wantMetrics = wantMetrics[1:] // The new one will overwrite the first one.
			wantMetrics = append(wantMetrics, generateMetrics(backend, want.Status.Conditions[0]))
			validateTrafficManagerBackendMetricsEmitted(wantMetrics...)
		})

		It("Updating the internalServiceExport to valid endpoint", func() {
			internalServiceExport := &fleetnetv1alpha1.InternalServiceExport{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: internalServiceExports[0].Namespace, Name: internalServiceExports[0].Name}, internalServiceExport)).Should(Succeed())
			internalServiceExport.Spec.IsInternalLoadBalancer = false
			Expect(k8sClient.Update(ctx, internalServiceExport)).Should(Succeed(), "failed to create internalServiceExport")
		})

		It("Validating trafficManagerBackend", func() {
			atmEndpointName := fmt.Sprintf(AzureResourceEndpointNameFormat, backendName+"#", serviceName, memberClusterNames[0])
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer, objectmeta.MetricsFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildTrueCondition(backend.Generation),
					Endpoints: []fleetnetv1beta1.TrafficManagerEndpointStatus{
						{
							Name: atmEndpointName,
							From: &fleetnetv1beta1.FromCluster{
								ClusterStatus: fleetnetv1beta1.ClusterStatus{
									Cluster: memberClusterNames[0],
								},
								Weight: ptr.To(int64(2)),
							},
							Weight:     ptr.To(int64(10)), // only 1 endpoint
							Target:     ptr.To(fakeprovider.ValidEndpointTarget),
							ResourceID: fmt.Sprintf(fakeprovider.EndpointResourceIDFormat, fakeprovider.DefaultSubscriptionID, fakeprovider.DefaultResourceGroupName, profileName, atmEndpointName),
						},
					},
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want, timeout)
			validator.ValidateTrafficManagerBackendConsistently(ctx, k8sClient, &want)

			By("By validating the status metrics")
			// Metrics are sorted by timestamp
			// * unknown
			// * false
			// * true
			wantMetrics = append(wantMetrics[:1], wantMetrics[2:]...) // The new one will overwrite the middle one.
			wantMetrics = append(wantMetrics, generateMetrics(backend, want.Status.Conditions[0]))
			validateTrafficManagerBackendMetricsEmitted(wantMetrics...)
		})

		It("Updating weight to 0", func() {
			Expect(k8sClient.Get(ctx, backendNamespacedName, backend)).Should(Succeed(), "failed to get trafficManagerBackend")
			backend.Spec.Weight = ptr.To(int64(0))
			Expect(k8sClient.Update(ctx, backend)).Should(Succeed(), "failed to update trafficManagerBackend")
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer, objectmeta.MetricsFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildTrueCondition(backend.Generation),
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want, timeout)
			validator.ValidateTrafficManagerBackendConsistently(ctx, k8sClient, &want)

			By("By validating the status metrics")
			// Metrics are sorted by timestamp
			// * unknown
			// * false
			// * true
			// * true with new generation
			wantMetrics = append(wantMetrics, generateMetrics(backend, want.Status.Conditions[0]))
			validateTrafficManagerBackendMetricsEmitted(wantMetrics...)
		})

		It("Deleting trafficManagerBackend", func() {
			err := k8sClient.Delete(ctx, backend)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerBackend")
		})

		It("Validating trafficManagerBackend is deleted", func() {
			validator.IsTrafficManagerBackendDeleted(ctx, k8sClient, backendNamespacedName, timeout)

			By("By validating the status metrics")
			validateTrafficManagerBackendMetricsEmitted()
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating trafficManagerProfile is deleted", func() {
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, profileNamespacedName, timeout)
		})

		It("Deleting serviceImport", func() {
			deleteServiceImport(types.NamespacedName{Namespace: testNamespace, Name: serviceName})
		})
	})

	Context("When creating trafficManagerBackend with valid serviceImport and internalServiceExports (403 error)", Ordered, func() {
		profileName := fakeprovider.ValidProfileWithEndpointsName
		profileNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: profileName}
		var profile *fleetnetv1beta1.TrafficManagerProfile
		backendName := fakeprovider.ValidBackendName
		backendNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: backendName}
		var backend *fleetnetv1beta1.TrafficManagerBackend

		var serviceImport *fleetnetv1alpha1.ServiceImport

		var wantMetrics []*prometheusclientmodel.Metric
		var wantEvents []corev1.Event

		It("Creating a new TrafficManagerProfile", func() {
			By("By creating a new TrafficManagerProfile")
			profile = trafficManagerProfileForTest(profileName)
			Expect(k8sClient.Create(ctx, profile)).Should(Succeed())
		})

		It("Updating TrafficManagerProfile status to programmed true", func() {
			By("By updating TrafficManagerProfile status")
			updateTrafficManagerProfileStatusToTrue(ctx, profile)
		})

		It("Creating a new ServiceImport", func() {
			serviceImport = &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: testNamespace,
				},
			}
			Expect(k8sClient.Create(ctx, serviceImport)).Should(Succeed(), "failed to create serviceImport")
		})

		It("Simulating a 403 error response from the provider server and updating the ServiceImport status", func() {
			// The test is running in sequence, so it's safe to set this value.
			fakeprovider.EnableEndpointForbiddenErr()
			serviceImport.Status = fleetnetv1alpha1.ServiceImportStatus{
				Clusters: []fleetnetv1alpha1.ClusterStatus{
					{
						Cluster: memberClusterNames[6], // 403 error
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, serviceImport)).Should(Succeed(), "failed to create serviceImport")
		})

		It("Creating TrafficManagerBackend", func() {
			backend = trafficManagerBackendForTest(backendName, profileName, serviceName)
			Expect(k8sClient.Create(ctx, backend)).Should(Succeed())
		})

		It("Should set trafficManagerBackend as accepted false", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer, objectmeta.MetricsFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildFalseCondition(backend.Generation),
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want, timeout)
			validator.ValidateTrafficManagerBackendConsistently(ctx, k8sClient, &want)

			By("By validating the status metrics")
			// Metrics are sorted by timestamp
			// * false
			wantMetrics = append(wantMetrics, generateMetrics(backend, want.Status.Conditions[0]))
			validateTrafficManagerBackendMetricsEmitted(wantMetrics...)

			By("By validating events")
			wantEvents = append(wantEvents, wantAzureAPIErrorEvent)
			validateEmittedEvents(backend, wantEvents)
		})

		It("Should reconcile the endpoint back after the provider server stops returning 403", func() {
			fakeprovider.DisableEndpointForbiddenErr()
			atmEndpointName := fmt.Sprintf(AzureResourceEndpointNameFormat, backendName+"#", serviceName, memberClusterNames[6])
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer, objectmeta.MetricsFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildTrueCondition(backend.Generation),
					Endpoints: []fleetnetv1beta1.TrafficManagerEndpointStatus{
						{
							Name: atmEndpointName,
							From: &fleetnetv1beta1.FromCluster{
								ClusterStatus: fleetnetv1beta1.ClusterStatus{
									Cluster: memberClusterNames[6],
								},
								Weight: ptr.To(int64(1)), // the original weight is default to 1
							},
							Weight:     ptr.To(backendWeight), // populate the weight using atm endpoint
							Target:     ptr.To(fakeprovider.ValidEndpointTarget),
							ResourceID: fmt.Sprintf(fakeprovider.EndpointResourceIDFormat, fakeprovider.DefaultSubscriptionID, fakeprovider.DefaultResourceGroupName, profileName, atmEndpointName),
						},
					},
				},
			}
			// The controller should reconcile the trafficManagerBackend using backoff algorithm.
			// It may take longer and depends on the failure times.
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want, longTimeout)
			validator.ValidateTrafficManagerBackendConsistently(ctx, k8sClient, &want)

			By("By validating the status metrics")
			// Metrics are sorted by timestamp
			// * false
			// * true
			wantMetrics = append(wantMetrics, generateMetrics(backend, want.Status.Conditions[0]))
			validateTrafficManagerBackendMetricsEmitted(wantMetrics...)

			By("By validating events")
			wantEvents = append(wantEvents, wantAcceptedEvent)
			validateEmittedEvents(backend, wantEvents)
		})

		It("Deleting trafficManagerBackend", func() {
			err := k8sClient.Delete(ctx, backend)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerBackend")
		})

		It("Validating trafficManagerBackend is deleted", func() {
			validator.IsTrafficManagerBackendDeleted(ctx, k8sClient, backendNamespacedName, timeout)

			By("By validating the status metrics")
			validateTrafficManagerBackendMetricsEmitted()

			By("By validating events")
			wantEvents = append(wantEvents, wantDeletedEvent)
			validateEmittedEvents(backend, wantEvents)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating trafficManagerProfile is deleted", func() {
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, profileNamespacedName, timeout)
		})

		It("Deleting serviceImport", func() {
			deleteServiceImport(types.NamespacedName{Namespace: testNamespace, Name: serviceName})
		})
	})
})

func deleteServiceImport(name types.NamespacedName) {
	serviceImport := &fleetnetv1alpha1.ServiceImport{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name.Name,
			Namespace: name.Namespace,
		},
	}
	Expect(k8sClient.Delete(ctx, serviceImport)).Should(Succeed(), "failed to delete serviceImport")

	Eventually(func() error {
		if err := k8sClient.Get(ctx, name, serviceImport); !errors.IsNotFound(err) {
			return fmt.Errorf("serviceImport %s still exists or an unexpected error occurred: %w", name, err)
		}
		return nil
	}, timeout, interval).Should(Succeed(), "Failed to remove serviceImport %s ", name)
}
