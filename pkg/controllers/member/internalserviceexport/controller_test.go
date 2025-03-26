/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package internalserviceexport

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/prometheus/client_golang/prometheus/testutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/metrics"
)

const (
	memberClusterID    = "bravelion"
	hubNSForMember     = "bravelion"
	memberUserNS       = "work"
	svcName            = "app"
	svcResourceVersion = "0"
)

var (
	internalSvcExportName = fmt.Sprintf("%s-%s", memberUserNS, svcName)

	svcExportKey         = types.NamespacedName{Namespace: memberUserNS, Name: svcName}
	internalSvcExportKey = types.NamespacedName{Namespace: hubNSForMember, Name: internalSvcExportName}

	// ignoredCondFields are fields that should be ignored when comparing conditions.
	ignoredCondFields = cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")
)

// conflictedServiceExportConflictCondition returns a ServiceExportConflict condition that reports an export conflict.
func conflictedServiceExportConflictCondition(svcNamespace string, svcName string, observedGeneration int64) metav1.Condition {
	return metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportConflict),
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.NewTime(time.Now().Round(time.Second)),
		Reason:             "ConflictFound",
		ObservedGeneration: observedGeneration,
		Message:            fmt.Sprintf("service %s/%s is in conflict with other exported services", svcNamespace, svcName),
	}
}

// unconflictedServiceExportConflictCondition returns a ServiceExportConflict condition that reports a successful
// export (no conflict).
func unconflictedServiceExportConflictCondition(svcNamespace string, svcName string, observedGeneration int64) metav1.Condition {
	return metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportConflict),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.NewTime(time.Now().Round(time.Second)),
		Reason:             "NoConflictFound",
		ObservedGeneration: observedGeneration,
		Message:            fmt.Sprintf("service %s/%s is exported without conflict", svcNamespace, svcName),
	}
}

// unknownServiceExportConflictCondition returns a ServiceExportConflict condition that reports an in-progress
// conflict resolution session.
func unknownServiceExportConflictCondition(svcNamespace string, svcName string, observedGeneration int64) metav1.Condition {
	return metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportConflict),
		Status:             metav1.ConditionUnknown,
		LastTransitionTime: metav1.NewTime(time.Now().Round(time.Second)),
		Reason:             "PendingConflictResolution",
		ObservedGeneration: observedGeneration,
		Message:            fmt.Sprintf("service %s/%s is pending export conflict resolution", svcNamespace, svcName),
	}
}

// Bootstrap the test environment.
func TestMain(m *testing.M) {
	// Add custom APIs to the runtime scheme
	err := fleetnetv1alpha1.AddToScheme(scheme.Scheme)
	if err != nil {
		log.Fatalf("failed to add custom APIs to the runtime scheme: %v", err)
	}

	os.Exit(m.Run())
}

// TestReportBackConflictCondition tests the *Reconciler.reportBackConflictCondition method.
func TestReportBackConflictCondition(t *testing.T) {
	exportGeneration := int64(123)
	internalExportGeneration := int64(456)
	testCases := []struct {
		name              string
		svcExport         *fleetnetv1alpha1.ServiceExport
		internalSvcExport *fleetnetv1alpha1.InternalServiceExport
		wantReported      bool
		wantConds         []metav1.Condition
	}{
		{
			name: "should not report back conflict cond (no condition yet)",
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  memberUserNS,
					Name:       svcName,
					Generation: exportGeneration,
				},
				Status: fleetnetv1alpha1.ServiceExportStatus{
					Conditions: []metav1.Condition{
						unknownServiceExportConflictCondition(memberUserNS, svcName, exportGeneration),
					},
				},
			},
			internalSvcExport: &fleetnetv1alpha1.InternalServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  hubNSForMember,
					Name:       internalSvcExportName,
					Generation: internalExportGeneration,
				},
			},
			wantReported: false,
			wantConds: []metav1.Condition{
				unknownServiceExportConflictCondition(memberUserNS, svcName, exportGeneration),
			},
		},
		{
			name: "should not report back conflict cond (no update)",
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  memberUserNS,
					Name:       svcName,
					Generation: exportGeneration,
				},
				Status: fleetnetv1alpha1.ServiceExportStatus{
					Conditions: []metav1.Condition{
						unconflictedServiceExportConflictCondition(memberUserNS, svcName, exportGeneration),
					},
				},
			},
			internalSvcExport: &fleetnetv1alpha1.InternalServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  hubNSForMember,
					Name:       internalSvcExportName,
					Generation: internalExportGeneration,
				},
				Status: fleetnetv1alpha1.InternalServiceExportStatus{
					Conditions: []metav1.Condition{
						unconflictedServiceExportConflictCondition(memberUserNS, svcName, internalExportGeneration),
					},
				},
			},
			wantReported: true,
			wantConds: []metav1.Condition{
				unconflictedServiceExportConflictCondition(memberUserNS, svcName, exportGeneration),
			},
		},
		{
			name: "should report back conflict cond",
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  memberUserNS,
					Name:       svcName,
					Generation: exportGeneration,
				},
				Status: fleetnetv1alpha1.ServiceExportStatus{
					Conditions: []metav1.Condition{},
				},
			},
			internalSvcExport: &fleetnetv1alpha1.InternalServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  hubNSForMember,
					Name:       internalSvcExportName,
					Generation: internalExportGeneration,
				},
				Status: fleetnetv1alpha1.InternalServiceExportStatus{
					Conditions: []metav1.Condition{
						conflictedServiceExportConflictCondition(memberUserNS, svcName, internalExportGeneration),
					},
				},
			},
			wantReported: true,
			wantConds: []metav1.Condition{
				conflictedServiceExportConflictCondition(memberUserNS, svcName, exportGeneration),
			},
		},
	}

	ctx := context.Background()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeMemberClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.svcExport).
				WithStatusSubresource(tc.svcExport).
				Build()
			fakeHubClient := fake.NewClientBuilder().Build()
			reconciler := Reconciler{
				MemberClient: fakeMemberClient,
				HubClient:    fakeHubClient,
				Recorder:     record.NewFakeRecorder(10),
			}

			reported, err := reconciler.reportBackConflictCondition(ctx, tc.svcExport, tc.internalSvcExport)
			if reported != tc.wantReported || err != nil {
				t.Fatalf("reportBackConflictCondition(%+v, %+v) = (%v, %v), want (%v, %v)",
					tc.svcExport, tc.internalSvcExport, reported, err, tc.wantReported, nil)
			}

			var updatedSvcExport = &fleetnetv1alpha1.ServiceExport{}
			if err := fakeMemberClient.Get(ctx, svcExportKey, updatedSvcExport); err != nil {
				t.Fatalf("failed to get updated svc export: %v", err)
			}
			conds := updatedSvcExport.Status.Conditions
			if !cmp.Equal(conds, tc.wantConds, ignoredCondFields) {
				t.Fatalf("conds are not correctly updated, got %+v, want %+v", conds, tc.wantConds)
			}
		})
	}
}

// TestObserveMetrics tests the Reconciler.observeMetrics function.
func TestObserveMetrics(t *testing.T) {
	metricMetadata := `
		# HELP fleet_networking_service_export_duration_milliseconds The duration of a service export
		# TYPE fleet_networking_service_export_duration_milliseconds histogram
	`
	startTime := time.Now().Round(time.Second)

	testCases := []struct {
		name              string
		internalSvcExport *fleetnetv1alpha1.InternalServiceExport
		startTime         time.Time
		wantMetricCount   int
		wantHistogram     string
	}{
		{
			name: "should not observe data point (no exportedSince field)",
			internalSvcExport: &fleetnetv1alpha1.InternalServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMember,
					Name:      internalSvcExportName,
				},
			},
			startTime:       startTime,
			wantMetricCount: 0,
			wantHistogram:   "",
		},
		{
			name: "should not observe data point (the object resource version has been observed before)",
			internalSvcExport: &fleetnetv1alpha1.InternalServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMember,
					Name:      internalSvcExportName,
					Annotations: map[string]string{
						metrics.MetricsAnnotationLastObservedResourceVersion: svcResourceVersion,
					},
				},
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
						ResourceVersion: svcResourceVersion,
					},
				},
			},
			startTime:       startTime,
			wantMetricCount: 0,
			wantHistogram:   "",
		},
		{
			name: "should observe a data point",
			internalSvcExport: &fleetnetv1alpha1.InternalServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMember,
					Name:      internalSvcExportName,
				},
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
						NamespacedName:  svcName,
						ResourceVersion: svcResourceVersion,
						ClusterID:       memberClusterID,
						ExportedSince:   metav1.NewTime(startTime.Add(-time.Second)),
					},
				},
			},
			startTime:       startTime,
			wantMetricCount: 1,
			wantHistogram: fmt.Sprintf(`
				fleet_networking_service_export_duration_milliseconds_bucket{origin_cluster_id="%[1]s",le="1000"} 1
				fleet_networking_service_export_duration_milliseconds_bucket{origin_cluster_id="%[1]s",le="2500"} 1
				fleet_networking_service_export_duration_milliseconds_bucket{origin_cluster_id="%[1]s",le="5000"} 1
				fleet_networking_service_export_duration_milliseconds_bucket{origin_cluster_id="%[1]s",le="10000"} 1
				fleet_networking_service_export_duration_milliseconds_bucket{origin_cluster_id="%[1]s",le="25000"} 1
				fleet_networking_service_export_duration_milliseconds_bucket{origin_cluster_id="%[1]s",le="50000"} 1
				fleet_networking_service_export_duration_milliseconds_bucket{origin_cluster_id="%[1]s",le="+Inf"} 1
				fleet_networking_service_export_duration_milliseconds_sum{origin_cluster_id="%[1]s"} 1000
				fleet_networking_service_export_duration_milliseconds_count{origin_cluster_id="%[1]s"} 1
			`, memberClusterID),
		},
		{
			name: "should observe a data point (negative export duration)",
			internalSvcExport: &fleetnetv1alpha1.InternalServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMember,
					Name:      internalSvcExportName,
				},
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
						NamespacedName:  svcName,
						ResourceVersion: svcResourceVersion,
						ClusterID:       memberClusterID,
						ExportedSince:   metav1.NewTime(startTime.Add(time.Second * 2)),
					},
				},
			},
			startTime:       startTime,
			wantMetricCount: 1,
			wantHistogram: fmt.Sprintf(`
				fleet_networking_service_export_duration_milliseconds_bucket{origin_cluster_id="%[1]s",le="1000"} 2
				fleet_networking_service_export_duration_milliseconds_bucket{origin_cluster_id="%[1]s",le="2500"} 2
				fleet_networking_service_export_duration_milliseconds_bucket{origin_cluster_id="%[1]s",le="5000"} 2
				fleet_networking_service_export_duration_milliseconds_bucket{origin_cluster_id="%[1]s",le="10000"} 2
				fleet_networking_service_export_duration_milliseconds_bucket{origin_cluster_id="%[1]s",le="25000"} 2
				fleet_networking_service_export_duration_milliseconds_bucket{origin_cluster_id="%[1]s",le="50000"} 2
				fleet_networking_service_export_duration_milliseconds_bucket{origin_cluster_id="%[1]s",le="+Inf"} 2
				fleet_networking_service_export_duration_milliseconds_sum{origin_cluster_id="%[1]s"} 2000
				fleet_networking_service_export_duration_milliseconds_count{origin_cluster_id="%[1]s"} 2
			`, memberClusterID),
		},
		{
			name: "should observe a data point (large outlier)",
			internalSvcExport: &fleetnetv1alpha1.InternalServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMember,
					Name:      internalSvcExportName,
					Annotations: map[string]string{
						metrics.MetricsAnnotationLastObservedResourceVersion: svcResourceVersion,
					},
				},
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
						NamespacedName:  svcName,
						ResourceVersion: "1",
						ClusterID:       memberClusterID,
						ExportedSince:   metav1.NewTime(startTime.Add(-time.Minute * 5)),
					},
				},
			},
			startTime:       startTime,
			wantMetricCount: 1,
			wantHistogram: fmt.Sprintf(`
				fleet_networking_service_export_duration_milliseconds_bucket{origin_cluster_id="%[1]s",le="1000"} 2
				fleet_networking_service_export_duration_milliseconds_bucket{origin_cluster_id="%[1]s",le="2500"} 2
				fleet_networking_service_export_duration_milliseconds_bucket{origin_cluster_id="%[1]s",le="5000"} 2
				fleet_networking_service_export_duration_milliseconds_bucket{origin_cluster_id="%[1]s",le="10000"} 2
				fleet_networking_service_export_duration_milliseconds_bucket{origin_cluster_id="%[1]s",le="25000"} 2
				fleet_networking_service_export_duration_milliseconds_bucket{origin_cluster_id="%[1]s",le="50000"} 2
				fleet_networking_service_export_duration_milliseconds_bucket{origin_cluster_id="%[1]s",le="+Inf"} 3
				fleet_networking_service_export_duration_milliseconds_sum{origin_cluster_id="%[1]s"} 102000
				fleet_networking_service_export_duration_milliseconds_count{origin_cluster_id="%[1]s"} 3
			`, memberClusterID),
		},
	}

	ctx := context.Background()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeMemberClient := fake.NewClientBuilder().Build()
			fakeHubClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.internalSvcExport).
				Build()
			reconciler := Reconciler{
				MemberClusterID: memberClusterID,
				MemberClient:    fakeMemberClient,
				HubClient:       fakeHubClient,
				Recorder:        record.NewFakeRecorder(10),
			}

			if err := reconciler.observeMetrics(ctx, tc.internalSvcExport, tc.startTime); err != nil {
				t.Fatalf("observeMetrics(%+v), got %v, want no error", tc.internalSvcExport, err)
			}

			internalSvcExport := &fleetnetv1alpha1.InternalServiceExport{}
			if err := fakeHubClient.Get(ctx, internalSvcExportKey, internalSvcExport); err != nil {
				t.Fatalf("internalServiceExport Get(%+v), got %v, want no error", internalSvcExportKey, err)
			}
			lastObserveResourceVersion, ok := internalSvcExport.Annotations[metrics.MetricsAnnotationLastObservedResourceVersion]
			if !ok || lastObserveResourceVersion != tc.internalSvcExport.Spec.ServiceReference.ResourceVersion {
				t.Fatalf("lastObservedResourceVersion, got %s, want %s", lastObserveResourceVersion, tc.internalSvcExport.Spec.ServiceReference.ResourceVersion)
			}

			if c := testutil.CollectAndCount(svcExportDuration); c != tc.wantMetricCount {
				t.Fatalf("metric counts, got %d, want %d", c, tc.wantMetricCount)
			}

			if tc.wantHistogram != "" {
				if err := testutil.CollectAndCompare(svcExportDuration, strings.NewReader(metricMetadata+tc.wantHistogram)); err != nil {
					t.Errorf("%s", err)
				}
			}
		})
	}
}
