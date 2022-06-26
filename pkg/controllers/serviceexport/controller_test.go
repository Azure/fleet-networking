/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package serviceexport

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

const (
	memberClusterID                   = "bravelion"
	memberUserNS                      = "work"
	hubNSForMember                    = "bravelion"
	svcName                           = "app"
	altSvcName                        = "app2"
	newSvcExportStatusCondType        = "New"
	newSvcExportStatusCondDescription = "NewCond"
)

// ignoredCondFields are fields that should be ignored when comparing conditions.
var ignoredCondFields = cmpopts.IgnoreFields(metav1.Condition{}, "ObservedGeneration", "LastTransitionTime")

// getSvcExportValidCond returns a ServiceExportValid condition for exporting a valid Service.
func getSvcExportValidCond(userNS, svcName string) metav1.Condition {
	return metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportValid),
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             "ServiceIsValid",
		Message:            fmt.Sprintf("service %s/%s is valid for export", userNS, svcName),
	}
}

// getSvcExportInvalidCondNotFound returns a ServiceExportValid condition for exporting a Service that is not found.
func getSvcExportInvalidCondNotFound(userNS, svcName string) metav1.Condition {
	return metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportValid),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             "ServiceNotFound",
		Message:            fmt.Sprintf("service %s/%s is not found", userNS, svcName),
	}
}

// getSvcExportNewCond returns a ServiceCondition with a new type.
func getSvcExportNewCond() metav1.Condition {
	return metav1.Condition{
		Type:               newSvcExportStatusCondType,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             newSvcExportStatusCondDescription,
		Message:            newSvcExportStatusCondDescription,
	}
}

// TestIsSvcExportCleanupNeeded tests the isSvcExportCleanupNeeded function.
func TestIsSvcExportCleanupNeeded(t *testing.T) {
	timestamp := metav1.Now()
	testCases := []struct {
		svcExport *fleetnetv1alpha1.ServiceExport
		want      bool
		name      string
	}{
		{
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
			},
			want: false,
			name: "should not clean up regular ServiceExport",
		},
		{
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:         memberUserNS,
					Name:              svcName,
					DeletionTimestamp: &timestamp,
				},
			},
			want: false,
			name: "should not clean up ServiceExport with only DeletionTimestamp set",
		},
		{
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  memberUserNS,
					Name:       svcName,
					Finalizers: []string{svcExportCleanupFinalizer},
				},
			},
			want: false,
			name: "should not clean up ServiceExport with cleanup finalizer only",
		},
		{
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:         memberUserNS,
					Name:              svcName,
					DeletionTimestamp: &timestamp,
					Finalizers:        []string{svcExportCleanupFinalizer},
				},
			},
			want: true,
			name: "should clean up ServiceExport with both cleanup finalizer and DeletionTimestamp set",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSvcExportCleanupNeeded(tc.svcExport); got != tc.want {
				t.Errorf("is svc export cleanup needed for svc export %+v, got %t, want %t", tc.svcExport, got, tc.want)
			}
		})
	}
}

// TestIsSvcDeleted tests the isSvcDeleted function.
func TestIsSvcDeleted(t *testing.T) {
	timestamp := metav1.Now()
	testCases := []struct {
		svc  *corev1.Service
		want bool
		name string
	}{
		{
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
			},
			want: false,
			name: "should not delete Service with DeletionTimestamp set",
		},
		{
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:         memberUserNS,
					Name:              svcName,
					DeletionTimestamp: &timestamp,
				},
			},
			want: true,
			name: "should delete Service with DeletionTimestamp set",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSvcDeleted(tc.svc); got != tc.want {
				t.Errorf("is svc deleted for svc %+v, got %t, want %t", tc.svc, got, tc.want)
			}
		})
	}
}

// TestFormatInternalSvcExportName tests the formatInternalSvcExportName function.
func TestFormatInternalSvcExportName(t *testing.T) {
	svcExport := &fleetnetv1alpha1.ServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      svcName,
		},
	}
	formattedName := formatInternalSvcExportName(svcExport)
	expectedFormattedName := "work-app"
	if formattedName != expectedFormattedName {
		t.Fatalf("formatted internal svc export name, got %s, want %s", formattedName, expectedFormattedName)
	}
}

// TestMarkSvcExportAsInvalid tests the *SvcExportReconciler.markSvcExportAsInvalidIneligible and
// *SvcExportReconciler.markSvcExportAsInvalidNotFound methods.
func TestMarkSvcExportAsInvalid(t *testing.T) {
	svcExportNew := &fleetnetv1alpha1.ServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      svcName,
		},
	}
	svcExportValid := &fleetnetv1alpha1.ServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      altSvcName,
		},
		Status: fleetnetv1alpha1.ServiceExportStatus{
			Conditions: []metav1.Condition{
				getSvcExportValidCond(memberUserNS, svcName),
				getSvcExportNewCond(),
			},
		},
	}

	fakeMemberClient := fakeclient.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(svcExportNew, svcExportValid).
		Build()
	fakeHubClient := fakeclient.NewClientBuilder().Build()
	reconciler := SvcExportReconciler{
		memberClient: fakeMemberClient,
		hubClient:    fakeHubClient,
		hubNS:        hubNSForMember,
	}
	ctx := context.Background()

	t.Run("should mark a new svc export as invalid (not found)", func(t *testing.T) {
		err := reconciler.markSvcExportAsInvalidSvcNotFound(ctx, svcExportNew)
		if err != nil {
			t.Errorf("failed to mark svc export")
		}

		var updatedSvcExport = &fleetnetv1alpha1.ServiceExport{}
		err = fakeMemberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcName}, updatedSvcExport)
		if err != nil {
			t.Errorf("faile to get updated svc export")
		}
		conds := updatedSvcExport.Status.Conditions
		expectedConds := []metav1.Condition{
			getSvcExportInvalidCondNotFound(memberUserNS, svcName),
		}
		if !cmp.Equal(conds, expectedConds, ignoredCondFields) {
			t.Errorf("svc export conditions, got %+v, want %+v", conds, expectedConds)
		}
	})

	t.Run("should mark a valid svc export as invalid (not found)", func(t *testing.T) {
		err := reconciler.markSvcExportAsInvalidSvcNotFound(ctx, svcExportValid)
		if err != nil {
			t.Errorf("failed to mark svc export")
		}

		var updatedSvcExport = &fleetnetv1alpha1.ServiceExport{}
		err = fakeMemberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: altSvcName}, updatedSvcExport)
		if err != nil {
			t.Errorf("faile to get updated svc export")
		}
		conds := updatedSvcExport.Status.Conditions
		expectedConds := []metav1.Condition{
			getSvcExportNewCond(),
			getSvcExportInvalidCondNotFound(memberUserNS, altSvcName),
		}
		if !cmp.Equal(conds, expectedConds, ignoredCondFields) {
			t.Errorf("svc export conditions, got %+v, want %+v", conds, expectedConds)
		}
	})
}

// TestRemoveSvcExportCleanupFinalizer tests the *SvcExportReconciler.removeSvcExportCleanupFinalizer method.
func TestRemoveSvcExportCleanupFinalizer(t *testing.T) {
	svcExportWithCleanupFinalizer := &fleetnetv1alpha1.ServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  memberUserNS,
			Name:       svcName,
			Finalizers: []string{svcExportCleanupFinalizer},
		},
	}

	fakeMemberClient := fakeclient.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(svcExportWithCleanupFinalizer).
		Build()
	fakeHubClient := fakeclient.NewClientBuilder().Build()
	reconciler := SvcExportReconciler{
		memberClient: fakeMemberClient,
		hubClient:    fakeHubClient,
		hubNS:        hubNSForMember,
	}
	ctx := context.Background()

	res, err := reconciler.removeSvcExportCleanupFinalizer(ctx, svcExportWithCleanupFinalizer)
	if err != nil || !cmp.Equal(res, ctrl.Result{}) {
		t.Errorf("failed to remove cleanup finalizer: %v; result: %v", err, res)
	}

	var updatedSvcExport = &fleetnetv1alpha1.ServiceExport{}
	err = fakeMemberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcName}, updatedSvcExport)
	if err != nil {
		t.Errorf("failed to get updated svc export: %v", err)
	}

	if controllerutil.ContainsFinalizer(updatedSvcExport, svcExportCleanupFinalizer) {
		t.Error("svc export cleanup finalizer is not removed")
	}
}

// TestUnexportSvc tests the *SvcExportReconciler.unexportSvc method.
func TestUnexportSvc(t *testing.T) {
	internalSvcExportName := fmt.Sprintf("%s-%s", memberUserNS, svcName)
	svcExport := &fleetnetv1alpha1.ServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  memberUserNS,
			Name:       svcName,
			Finalizers: []string{svcExportCleanupFinalizer},
		},
	}
	altSvcExport := &fleetnetv1alpha1.ServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  memberUserNS,
			Name:       altSvcName,
			Finalizers: []string{svcExportCleanupFinalizer},
		},
	}
	internalSvcExport := &fleetnetv1alpha1.InternalServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: hubNSForMember,
			Name:      internalSvcExportName,
		},
	}

	fakeMemberClient := fakeclient.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(svcExport, altSvcExport).
		Build()
	fakeHubClient := fakeclient.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(internalSvcExport).
		Build()
	reconciler := SvcExportReconciler{
		memberClient: fakeMemberClient,
		hubClient:    fakeHubClient,
		hubNS:        hubNSForMember,
	}
	ctx := context.Background()

	t.Run("should unexport svc", func(t *testing.T) {
		res, err := reconciler.unexportSvc(ctx, svcExport)
		if err != nil || !cmp.Equal(res, ctrl.Result{}) {
			t.Errorf("failed to unexport svc: %v; result: %v", err, res)
		}

		var updatedSvcExport = &fleetnetv1alpha1.ServiceExport{}
		err = fakeMemberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcName}, updatedSvcExport)
		if err != nil {
			t.Errorf("failed to get updated svc export: %v", err)
		}
		if controllerutil.ContainsFinalizer(updatedSvcExport, svcExportCleanupFinalizer) {
			t.Error("svc export cleanup finalizer is not removed")
		}

		var deletedInternalSvcExport = &fleetnetv1alpha1.InternalServiceExport{}
		err = fakeHubClient.Get(ctx, types.NamespacedName{Namespace: hubNSForMember, Name: internalSvcExportName}, deletedInternalSvcExport)
		if !errors.IsNotFound(err) {
			t.Error("internal svc export is not removed")
		}
	})

	t.Run("should unexport partially exported svc", func(t *testing.T) {
		res, err := reconciler.unexportSvc(ctx, altSvcExport)
		if err != nil || !cmp.Equal(res, ctrl.Result{}) {
			t.Errorf("failed to unexport svc: %v; result: %v", err, res)
		}

		var updatedSvcExport = &fleetnetv1alpha1.ServiceExport{}
		err = fakeMemberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: altSvcName}, updatedSvcExport)
		if err != nil {
			t.Errorf("failed to get updated svc export: %v", err)
		}
		if controllerutil.ContainsFinalizer(updatedSvcExport, svcExportCleanupFinalizer) {
			t.Error("svc export cleanup finalizer is not removed")
		}
	})
}
