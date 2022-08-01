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
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

const (
	hubNSForMember = "bravelion"
	memberUserNS   = "work"
	svcName        = "app"
)

// ignoredCondFields are fields that should be ignored when comparing conditions.
var ignoredCondFields = cmpopts.IgnoreFields(metav1.Condition{}, "ObservedGeneration", "LastTransitionTime")

// conflictedServiceExportConflictCondition returns a ServiceExportConflict condition that reports an export conflict.
func conflictedServiceExportConflictCondition(svcNamespace string, svcName string) metav1.Condition {
	return metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportConflict),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: 1,
		LastTransitionTime: metav1.Now(),
		Reason:             "ConflictFound",
		Message:            fmt.Sprintf("service %s/%s is in conflict with other exported services", svcNamespace, svcName),
	}
}

// unconflictedServiceExportConflictCondition returns a ServiceExportConflict condition that reports a successful
// export (no conflict).
func unconflictedServiceExportConflictCondition(svcNamespace string, svcName string) metav1.Condition {
	return metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportConflict),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: 2,
		LastTransitionTime: metav1.Now(),
		Reason:             "NoConflictFound",
		Message:            fmt.Sprintf("service %s/%s is exported without conflict", svcNamespace, svcName),
	}
}

// unknownServiceExportConflictCondition returns a ServiceExportConflict condition that reports an in-progress
// conflict resolution session.
func unknownServiceExportConflictCondition(svcNamespace string, svcName string) metav1.Condition {
	return metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportConflict),
		Status:             metav1.ConditionUnknown,
		ObservedGeneration: 0,
		LastTransitionTime: metav1.Now(),
		Reason:             "PendingConflictResolution",
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
	testCases := []struct {
		name              string
		svcExport         *fleetnetv1alpha1.ServiceExport
		internalSvcExport *fleetnetv1alpha1.InternalServiceExport
		expectedConds     []metav1.Condition
	}{
		{
			name: "should not report back conflict cond (no condition yet)",
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
				Status: fleetnetv1alpha1.ServiceExportStatus{
					Conditions: []metav1.Condition{
						unknownServiceExportConflictCondition(memberUserNS, svcName),
					},
				},
			},
			internalSvcExport: &fleetnetv1alpha1.InternalServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMember,
					Name:      fmt.Sprintf("%s-%s", memberUserNS, svcName),
				},
			},
			expectedConds: []metav1.Condition{
				unknownServiceExportConflictCondition(memberUserNS, svcName),
			},
		},
		{
			name: "should not report back conflict cond (no update)",
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
				Status: fleetnetv1alpha1.ServiceExportStatus{
					Conditions: []metav1.Condition{
						unconflictedServiceExportConflictCondition(memberUserNS, svcName),
					},
				},
			},
			internalSvcExport: &fleetnetv1alpha1.InternalServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMember,
					Name:      fmt.Sprintf("%s-%s", memberUserNS, svcName),
				},
				Status: fleetnetv1alpha1.InternalServiceExportStatus{
					Conditions: []metav1.Condition{
						unconflictedServiceExportConflictCondition(memberUserNS, svcName),
					},
				},
			},
			expectedConds: []metav1.Condition{
				unconflictedServiceExportConflictCondition(memberUserNS, svcName),
			},
		},
		{
			name: "should report back conflict cond",
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
				Status: fleetnetv1alpha1.ServiceExportStatus{
					Conditions: []metav1.Condition{},
				},
			},
			internalSvcExport: &fleetnetv1alpha1.InternalServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMember,
					Name:      fmt.Sprintf("%s-%s", memberUserNS, svcName),
				},
				Status: fleetnetv1alpha1.InternalServiceExportStatus{
					Conditions: []metav1.Condition{
						conflictedServiceExportConflictCondition(memberUserNS, svcName),
					},
				},
			},
			expectedConds: []metav1.Condition{
				conflictedServiceExportConflictCondition(memberUserNS, svcName),
			},
		},
	}

	ctx := context.Background()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeMemberClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.svcExport).
				Build()
			fakeHubClient := fake.NewClientBuilder().Build()
			reconciler := Reconciler{
				MemberClient: fakeMemberClient,
				HubClient:    fakeHubClient,
			}

			if err := reconciler.reportBackConflictCondition(ctx, tc.svcExport, tc.internalSvcExport); err != nil {
				t.Fatalf("failed to report back conflict cond: %v", err)
			}

			var updatedSvcExport = &fleetnetv1alpha1.ServiceExport{}
			updatedSvcExportKey := types.NamespacedName{Namespace: tc.svcExport.Namespace, Name: tc.svcExport.Name}
			if err := fakeMemberClient.Get(ctx, updatedSvcExportKey, updatedSvcExport); err != nil {
				t.Fatalf("failed to get updated svc export: %v", err)
			}
			conds := updatedSvcExport.Status.Conditions
			if !cmp.Equal(conds, tc.expectedConds, ignoredCondFields) {
				t.Fatalf("conds are not correctly updated, got %+v, want %+v", conds, tc.expectedConds)
			}
		})
	}
}
