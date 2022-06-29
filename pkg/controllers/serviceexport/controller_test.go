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

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

const (
	memberUserNS                      = "work"
	hubNSForMember                    = "bravelion"
	svcName                           = "app"
	altSvcName                        = "app2"
	newSvcExportStatusCondType        = "New"
	newSvcExportStatusCondDescription = "NewCond"
)

// ignoredCondFields are fields that should be ignored when comparing conditions.
var ignoredCondFields = cmpopts.IgnoreFields(metav1.Condition{}, "ObservedGeneration", "LastTransitionTime")

// serviceExportValidCond returns a ServiceExportValid condition for exporting a valid Service.
func serviceExportValidCondition(userNS, svcName string) metav1.Condition {
	return metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportValid),
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             svcExportValidCondReason,
		Message:            fmt.Sprintf("service %s/%s is valid for export", userNS, svcName),
	}
}

// serviceExportInvalidNotFoundCond returns a ServiceExportValid condition for exporting a Service that is not found.
func serviceExportInvalidNotFoundCondition(userNS, svcName string) metav1.Condition {
	return metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportValid),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             svcExportInvalidNotFoundCondReason,
		Message:            fmt.Sprintf("service %s/%s is not found", userNS, svcName),
	}
}

// serviceExportNewCond returns a ServiceCondition with a new type.
func serviceExportNewCondition() metav1.Condition {
	return metav1.Condition{
		Type:               newSvcExportStatusCondType,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             newSvcExportStatusCondDescription,
		Message:            newSvcExportStatusCondDescription,
	}
}

// TestIsServiceExportCleanupNeeded tests the isServiceExportCleanupNeeded function.
func TestIsServiceExportCleanupNeeded(t *testing.T) {
	timestamp := metav1.Now()
	testCases := []struct {
		name      string
		svcExport *fleetnetv1alpha1.ServiceExport
		want      bool
	}{
		{
			name: "should not clean up regular ServiceExport",
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
			},
		},
		{
			name: "should not clean up ServiceExport with only DeletionTimestamp set",
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:         memberUserNS,
					Name:              svcName,
					DeletionTimestamp: &timestamp,
				},
			},
		},
		{
			name: "should not clean up ServiceExport with cleanup finalizer only",
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  memberUserNS,
					Name:       svcName,
					Finalizers: []string{svcExportCleanupFinalizer},
				},
			},
		},
		{
			name: "should clean up ServiceExport with both cleanup finalizer and DeletionTimestamp set",
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:         memberUserNS,
					Name:              svcName,
					DeletionTimestamp: &timestamp,
					Finalizers:        []string{svcExportCleanupFinalizer},
				},
			},
			want: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isServiceExportCleanupNeeded(tc.svcExport); got != tc.want {
				t.Fatalf("isServiceExportCleanupNeeded(%+v) = %t, want %t", tc.svcExport, got, tc.want)
			}
		})
	}
}

// TestIsServiceDeleted tests the isServiceDeleted function.
func TestIsServiceDeleted(t *testing.T) {
	timestamp := metav1.Now()
	testCases := []struct {
		name string
		svc  *corev1.Service
		want bool
	}{
		{
			name: "should not delete Service with DeletionTimestamp set",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
			},
		},
		{
			name: "should delete Service with DeletionTimestamp set",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:         memberUserNS,
					Name:              svcName,
					DeletionTimestamp: &timestamp,
				},
			},
			want: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isServiceDeleted(tc.svc); got != tc.want {
				t.Fatalf("isServiceDeleted(%+v) = %t, want %t", tc.svc, got, tc.want)
			}
		})
	}
}

// TestFormatInternalServiceExportName tests the formatInternalServiceExportName function.
func TestFormatInternalServiceExportName(t *testing.T) {
	svcExport := &fleetnetv1alpha1.ServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      svcName,
		},
	}
	got := formatInternalServiceExportName(svcExport)
	want := "work-app"
	if got != want {
		t.Fatalf("formatInternalServiceExportName(%+v) = %s, want %s", svcExport, got, want)
	}
}

// TestMarkServiceExportAsInvalid tests the *Reconciler.markServiceExportAsInvalidNotFound method.
func TestMarkServiceExportAsInvalid(t *testing.T) {
	// Setup
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
				serviceExportValidCondition(memberUserNS, svcName),
				serviceExportNewCondition(),
			},
		},
	}

	fakeMemberClient := fakeclient.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(svcExportNew, svcExportValid).
		Build()
	fakeHubClient := fakeclient.NewClientBuilder().Build()
	reconciler := Reconciler{
		memberClient: fakeMemberClient,
		hubClient:    fakeHubClient,
		hubNamespace: hubNSForMember,
	}
	ctx := context.Background()

	testCases := []struct {
		name          string
		svcExport     *fleetnetv1alpha1.ServiceExport
		expectedConds []metav1.Condition
	}{
		{
			name:      "should mark a new svc export as invalid (not found)",
			svcExport: svcExportNew,
			expectedConds: []metav1.Condition{
				serviceExportInvalidNotFoundCondition(memberUserNS, svcName),
			},
		},
		{
			name:      "should mark a valid svc export as invalid (not found)",
			svcExport: svcExportValid,
			expectedConds: []metav1.Condition{
				serviceExportInvalidNotFoundCondition(memberUserNS, altSvcName),
				serviceExportNewCondition(),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := reconciler.markServiceExportAsInvalidNotFound(ctx, tc.svcExport); err != nil {
				t.Fatalf("failed to mark svc export: %v", err)
			}

			var updatedSvcExport = &fleetnetv1alpha1.ServiceExport{}
			if err := fakeMemberClient.Get(ctx,
				types.NamespacedName{Namespace: tc.svcExport.Namespace, Name: tc.svcExport.Name},
				updatedSvcExport); err != nil {
				t.Fatalf("failed to get updated svc export: %v", err)
			}
			conds := updatedSvcExport.Status.Conditions
			if !cmp.Equal(conds, tc.expectedConds, ignoredCondFields) {
				t.Fatalf("svc export conditions, got %+v, want %+v", conds, tc.expectedConds)
			}
		})
	}
}

// TestRemoveServiceExportCleanupFinalizer tests the *Reconciler.removeServiceExportCleanupFinalizer method.
func TestRemoveServiceExportCleanupFinalizer(t *testing.T) {
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
	reconciler := Reconciler{
		memberClient: fakeMemberClient,
		hubClient:    fakeHubClient,
		hubNamespace: hubNSForMember,
	}
	ctx := context.Background()

	res, err := reconciler.removeServiceExportCleanupFinalizer(ctx, svcExportWithCleanupFinalizer)
	if !cmp.Equal(res, ctrl.Result{}) || err != nil {
		t.Fatalf("removeServiceExportCleanupFinalizer() = %+v, %v, want %+v, %v", res, err, ctrl.Result{}, nil)
	}

	var updatedSvcExport = &fleetnetv1alpha1.ServiceExport{}
	if err := fakeMemberClient.Get(ctx,
		types.NamespacedName{Namespace: memberUserNS, Name: svcName},
		updatedSvcExport); err != nil {
		t.Fatalf("failed to get updated svc export: %v", err)
	}

	if updatedSvcExport.ObjectMeta.Finalizers != nil {
		t.Fatalf("svc export finalizer, got %+v, want %+v", updatedSvcExport.ObjectMeta.Finalizers, nil)
	}
}

// TestUnexportService tests the *Reconciler.unexportService method.
func TestUnexportService(t *testing.T) {
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
	reconciler := Reconciler{
		memberClient: fakeMemberClient,
		hubClient:    fakeHubClient,
		hubNamespace: hubNSForMember,
	}
	ctx := context.Background()

	testCases := []struct {
		name              string
		svcExport         *fleetnetv1alpha1.ServiceExport
		internalSvcExport *fleetnetv1alpha1.InternalServiceExport
	}{
		{
			name:      "should unexport svc",
			svcExport: svcExport,
		},
		{
			name:              "should unexport partially exported svc",
			svcExport:         altSvcExport,
			internalSvcExport: internalSvcExport,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := reconciler.unexportService(ctx, tc.svcExport)
			if !cmp.Equal(res, ctrl.Result{}) || err != nil {
				t.Fatalf("unexportService() = %+v, %v, want %+v, %v", res, err, ctrl.Result{}, err)
			}

			var updatedSvcExport = &fleetnetv1alpha1.ServiceExport{}
			if err := fakeMemberClient.Get(ctx,
				types.NamespacedName{Namespace: tc.svcExport.Namespace, Name: tc.svcExport.Name},
				updatedSvcExport); err != nil {
				t.Fatalf("failed to get updated svc export: %v", err)
			}
			if updatedSvcExport.ObjectMeta.Finalizers != nil {
				t.Fatalf("svc export finalizer, got %+v, want %+v", updatedSvcExport.ObjectMeta.Finalizers, nil)
			}

			if tc.internalSvcExport == nil {
				return
			}

			var deletedInternalSvcExport = &fleetnetv1alpha1.InternalServiceExport{}
			if err := fakeHubClient.Get(ctx,
				types.NamespacedName{Namespace: tc.internalSvcExport.Namespace, Name: internalSvcExportName},
				deletedInternalSvcExport); !errors.IsNotFound(err) {
				t.Fatalf("internalSvcExport Get(), got error %v, want not found error", err)
			}
		})
	}
}
