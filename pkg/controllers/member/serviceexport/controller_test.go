/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package serviceexport

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v4"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/metrics"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
)

const (
	memberUserNS       = "work"
	hubNSForMember     = "bravelion"
	svcName            = "app"
	svcResourceVersion = "0"

	validResourceGroup = "valid-rg"
)

// ignoredCondFields are fields that should be ignored when comparing conditions.
var ignoredCondFields = cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")

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

// serviceExportInvalidIneligibleCondition returns a ServiceExportValid condition for exporting an ineligible Service.
func serviceExportInvalidIneligibleCondition(userNS, svcName string) metav1.Condition {
	return metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportValid),
		Status:             metav1.ConditionStatus(corev1.ConditionFalse),
		LastTransitionTime: metav1.Now(),
		Reason:             svcExportInvalidIneligibleCondReason,
		Message:            fmt.Sprintf("service %s/%s is not eligible for export", userNS, svcName),
	}
}

// serviceExportPendingConflictResolutionCondition returns a ServiceExportConflict condition which reports that
// a confliction resolution is in progress.
func serviceExportPendingConflictResolutionCondition(userNS, svcName string) metav1.Condition {
	return metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportConflict),
		Status:             metav1.ConditionUnknown,
		LastTransitionTime: metav1.Now(),
		Reason:             svcExportPendingConflictResolutionReason,
		Message:            fmt.Sprintf("service %s/%s is pending export conflict resolution", userNS, svcName),
	}
}

// serviceExportNoConflictCondition returns a ServiceExportConflict condition which reports that a service is exported
// with no conflict.
func serviceExportNoConflictCondition(userNS, svcName string) metav1.Condition {
	return metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportConflict),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             "NoConflictDetected",
		Message:            fmt.Sprintf("service %s/%s is exported with no conflict", userNS, svcName),
	}
}

// TestMain bootstraps the test environment.
func TestMain(m *testing.M) {
	// Add custom APIs to the runtime scheme
	if err := fleetnetv1alpha1.AddToScheme(scheme.Scheme); err != nil {
		log.Fatalf("failed to add custom APIs to the runtime scheme: %v", err)
	}

	os.Exit(m.Run())
}

// TestIsServiceEligibleForExport tests the isServiceEligibleForExport function.
func TestIsServiceEligibleForExport(t *testing.T) {
	testCases := []struct {
		name string
		svc  *corev1.Service
		want bool
	}{
		{
			name: "should export regular Service",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
					Ports: []corev1.ServicePort{
						{
							Port: 80,
						},
					},
				},
			},
			want: true,
		},
		{
			name: "should not export ExternalName Service",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
				Spec: corev1.ServiceSpec{
					Type:         corev1.ServiceTypeExternalName,
					ExternalName: "example.com",
				},
			},
			want: false,
		},
		{
			name: "should not export headless Service",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
				Spec: corev1.ServiceSpec{
					Type:      corev1.ServiceTypeClusterIP,
					ClusterIP: "None",
					Ports: []corev1.ServicePort{
						{
							Port: 80,
						},
					},
				},
			},
			want: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isServiceEligibleForExport(tc.svc); got != tc.want {
				t.Errorf("isServiceEligibleForExport(%+v) = %t, want %t", tc.svc, got, tc.want)
			}
		})
	}
}

// TestFormatInternalServiceExportName tests the formatInternalServiceExportName function.
func TestFormatInternalServiceExportName(t *testing.T) {
	testCases := []struct {
		name      string
		svcExport *fleetnetv1alpha1.ServiceExport
		want      string
	}{
		{
			name: "should return formatted name",
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
			},
			want: "work-app",
		},
	}

	for _, tc := range testCases {
		if got := formatInternalServiceExportName(tc.svcExport); got != tc.want {
			t.Fatalf("formatInternalServiceExportName(%+v) = %s, want %s", tc.svcExport, got, tc.want)
		}
	}
}

// TestExtractServicePorts tests the extractServicePorts function.
func TestExtractServicePorts(t *testing.T) {
	testCases := []struct {
		name string
		svc  *corev1.Service
		want []fleetnetv1alpha1.ServicePort
	}{
		{
			name: "should extract ports",
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:       "web",
							Protocol:   corev1.ProtocolTCP,
							Port:       80,
							TargetPort: intstr.FromInt(8080),
						},
					},
				},
			},
			want: []fleetnetv1alpha1.ServicePort{
				{
					Name:       "web",
					Protocol:   corev1.ProtocolTCP,
					Port:       80,
					TargetPort: intstr.FromInt(8080),
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			svcExportPorts := extractServicePorts(tc.svc)
			if !cmp.Equal(svcExportPorts, tc.want) {
				t.Fatalf("extractServicePorts(%+v) = %v, want %v", tc.svc, svcExportPorts, tc.want)
			}
		})
	}
}

// TestMarkServiceExportAsInvalidNotFound tests the *Reconciler.markServiceExportAsInvalidNotFound method.
func TestMarkServiceExportAsInvalidNotFound(t *testing.T) {
	testCases := []struct {
		name      string
		svcExport *fleetnetv1alpha1.ServiceExport
		wantConds []metav1.Condition
	}{
		{
			name: "should mark a new svc export as invalid (not found)",
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
			},
			wantConds: []metav1.Condition{
				serviceExportInvalidNotFoundCondition(memberUserNS, svcName),
			},
		},
		{
			name: "should mark a valid svc export as invalid (not found)",
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
				Status: fleetnetv1alpha1.ServiceExportStatus{
					Conditions: []metav1.Condition{
						serviceExportValidCondition(memberUserNS, svcName),
					},
				},
			},
			wantConds: []metav1.Condition{
				serviceExportInvalidNotFoundCondition(memberUserNS, svcName),
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
				HubNamespace: hubNSForMember,
				Recorder:     record.NewFakeRecorder(10),
			}

			if err := reconciler.markServiceExportAsInvalidNotFound(ctx, tc.svcExport); err != nil {
				t.Fatalf("failed to mark svc export: %v", err)
			}

			var updatedSvcExport = &fleetnetv1alpha1.ServiceExport{}
			svcExportKey := types.NamespacedName{Namespace: tc.svcExport.Namespace, Name: tc.svcExport.Name}
			if err := fakeMemberClient.Get(ctx, svcExportKey, updatedSvcExport); err != nil {
				t.Fatalf("svc export Get(%+v): %v", svcExportKey, err)
			}
			conds := updatedSvcExport.Status.Conditions
			if !cmp.Equal(conds, tc.wantConds, ignoredCondFields) {
				t.Fatalf("svc export conditions, got %+v, want %+v", conds, tc.wantConds)
			}
		})
	}
}

// TestMarkServiceExportAsInvalidIneligible tests the *Reconciler.markServiceExportAsInvalidIneligible method.
func TestMarkServiceExportAsInvalidIneligible(t *testing.T) {
	testCases := []struct {
		name      string
		svcExport *fleetnetv1alpha1.ServiceExport
		svc       *corev1.Service
		wantConds []metav1.Condition
	}{
		{
			name: "should mark a new svc export as invalid (ineligible)",
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
			},
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
			},
			wantConds: []metav1.Condition{
				serviceExportInvalidIneligibleCondition(memberUserNS, svcName),
			},
		},
		{
			name: "should mark a valid svc export as invalid (ineligible)",
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
				Status: fleetnetv1alpha1.ServiceExportStatus{
					Conditions: []metav1.Condition{
						serviceExportValidCondition(memberUserNS, svcName),
					},
				},
			},
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
			},
			wantConds: []metav1.Condition{
				serviceExportInvalidIneligibleCondition(memberUserNS, svcName),
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
				HubNamespace: hubNSForMember,
				Recorder:     record.NewFakeRecorder(10),
			}

			if err := reconciler.markServiceExportAsInvalidSvcIneligible(ctx, tc.svcExport, tc.svc); err != nil {
				t.Fatalf("failed to mark svc export: %v", err)
			}

			var updatedSvcExport = &fleetnetv1alpha1.ServiceExport{}
			svcExportKey := types.NamespacedName{Namespace: tc.svcExport.Namespace, Name: tc.svcExport.Name}
			if err := fakeMemberClient.Get(ctx, svcExportKey, updatedSvcExport); err != nil {
				t.Fatalf("svc export Get(%+v), got %v, want no error", svcExportKey, err)
			}
			conds := updatedSvcExport.Status.Conditions
			if !cmp.Equal(conds, tc.wantConds, ignoredCondFields) {
				t.Fatalf("svc export conditions, got %+v, want %+v", conds, tc.wantConds)
			}
		})
	}
}

// TestMarkServiceExportAsValid tests the *Reconciler.markServiceExportAsValid method.
func TestMarkServiceExportAsValid(t *testing.T) {
	testCases := []struct {
		name      string
		svcExport *fleetnetv1alpha1.ServiceExport
		svc       *corev1.Service
		wantConds []metav1.Condition
	}{
		{
			name: "should mark a new svc export as valid",
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
			},
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
			},
			wantConds: []metav1.Condition{
				serviceExportValidCondition(memberUserNS, svcName),
				serviceExportPendingConflictResolutionCondition(memberUserNS, svcName),
			},
		},
		{
			name: "should mark an invalid svc export (not found) as valid",
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
				Status: fleetnetv1alpha1.ServiceExportStatus{
					Conditions: []metav1.Condition{
						serviceExportInvalidNotFoundCondition(memberUserNS, svcName),
					},
				},
			},
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
			},
			wantConds: []metav1.Condition{
				serviceExportValidCondition(memberUserNS, svcName),
				serviceExportPendingConflictResolutionCondition(memberUserNS, svcName),
			},
		},
		{
			name: "should mark an invalid svc export (ineligible) as valid",
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
				Status: fleetnetv1alpha1.ServiceExportStatus{
					Conditions: []metav1.Condition{
						serviceExportInvalidIneligibleCondition(memberUserNS, svcName),
					},
				},
			},
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
			},
			wantConds: []metav1.Condition{
				serviceExportValidCondition(memberUserNS, svcName),
				serviceExportPendingConflictResolutionCondition(memberUserNS, svcName),
			},
		},
		{
			name: "should not mark a svc export that is valid already with a conflict condition (pending)",
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
				Status: fleetnetv1alpha1.ServiceExportStatus{
					Conditions: []metav1.Condition{
						serviceExportValidCondition(memberUserNS, svcName),
						serviceExportPendingConflictResolutionCondition(memberUserNS, svcName),
					},
				},
			},
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
			},
			wantConds: []metav1.Condition{
				serviceExportValidCondition(memberUserNS, svcName),
				serviceExportPendingConflictResolutionCondition(memberUserNS, svcName),
			},
		},
		{
			name: "should not mark a svc export that is valid already with a conflict condition (no conflict)",
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
				Status: fleetnetv1alpha1.ServiceExportStatus{
					Conditions: []metav1.Condition{
						serviceExportValidCondition(memberUserNS, svcName),
						serviceExportNoConflictCondition(memberUserNS, svcName),
					},
				},
			},
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
			},
			wantConds: []metav1.Condition{
				serviceExportValidCondition(memberUserNS, svcName),
				serviceExportNoConflictCondition(memberUserNS, svcName),
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
				HubNamespace: hubNSForMember,
				Recorder:     record.NewFakeRecorder(10),
			}

			if err := reconciler.markServiceExportAsValid(ctx, tc.svcExport, tc.svc); err != nil {
				t.Fatalf("failed to mark svc export: %v", err)
			}

			var updatedSvcExport = &fleetnetv1alpha1.ServiceExport{}
			svcExportKey := types.NamespacedName{Namespace: tc.svcExport.Namespace, Name: tc.svcExport.Name}
			if err := fakeMemberClient.Get(ctx, svcExportKey, updatedSvcExport); err != nil {
				t.Fatalf("svc export Get(%+v), got %v, want no error", svcExportKey, err)
			}
			conds := updatedSvcExport.Status.Conditions
			if !cmp.Equal(conds, tc.wantConds, ignoredCondFields) {
				t.Fatalf("svc export conditions, got %+v, want %+v", conds, tc.wantConds)
			}
		})
	}
}

// TestRemoveServiceExportCleanupFinalizer tests the *Reconciler.removeServiceExportCleanupFinalizer method.
func TestRemoveServiceExportCleanupFinalizer(t *testing.T) {
	testCases := []struct {
		name      string
		svcExport *fleetnetv1alpha1.ServiceExport
		want      []string
	}{
		{
			name: "should remove cleanup finalizer",
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  memberUserNS,
					Name:       svcName,
					Finalizers: []string{svcExportCleanupFinalizer},
				},
			},
			want: nil,
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
				HubNamespace: hubNSForMember,
				Recorder:     record.NewFakeRecorder(10),
			}

			if err := reconciler.removeServiceExportCleanupFinalizer(ctx, tc.svcExport); err != nil {
				t.Fatalf("removeServiceExportCleanupFinalizer(%+v) = %v, want no error", tc.svcExport, err)
			}

			var updatedSvcExport = &fleetnetv1alpha1.ServiceExport{}
			updatedSvcExportKey := types.NamespacedName{Namespace: memberUserNS, Name: svcName}
			if err := fakeMemberClient.Get(ctx, updatedSvcExportKey, updatedSvcExport); err != nil {
				t.Fatalf("svc export Get(%+v), got %v, want no error", updatedSvcExportKey, err)
			}

			if !cmp.Equal(updatedSvcExport.ObjectMeta.Finalizers, tc.want) {
				t.Fatalf("svc export finalizer, got %+v, want %+v", updatedSvcExport.ObjectMeta.Finalizers, tc.want)
			}
		})
	}
}

// TestAddServiceExportCleanupFinalizer tests the *Reconciler.addServiceExportCleanupFinalizer method.
func TestAddServiceExportCleanupFinalizer(t *testing.T) {
	testCases := []struct {
		name      string
		svcExport *fleetnetv1alpha1.ServiceExport
		want      []string
	}{
		{
			name: "should add cleanup finalizer",
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
			},
			want: []string{svcExportCleanupFinalizer},
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
				HubNamespace: hubNSForMember,
				Recorder:     record.NewFakeRecorder(10),
			}

			if err := reconciler.addServiceExportCleanupFinalizer(ctx, tc.svcExport); err != nil {
				t.Fatalf("addServiceExportCleanupFinalizer(%+v), got %v, want no error", tc.svcExport, err)
			}

			var updatedSvcExport = &fleetnetv1alpha1.ServiceExport{}
			updatedSvcExportKey := types.NamespacedName{Namespace: memberUserNS, Name: svcName}
			if err := fakeMemberClient.Get(ctx, updatedSvcExportKey, updatedSvcExport); err != nil {
				t.Fatalf("svc export Get(%+v), got %v, want no error", updatedSvcExportKey, err)
			}

			if !cmp.Equal(updatedSvcExport.ObjectMeta.Finalizers, tc.want) {
				t.Fatalf("svc export finalizer, got %+v, want %+v", updatedSvcExport.ObjectMeta.Finalizers, tc.want)
			}
		})
	}
}

// TestUnexportService tests the *Reconciler.unexportService method.
func TestUnexportService(t *testing.T) {
	internalSvcExportName := fmt.Sprintf("%s-%s", memberUserNS, svcName)

	testCases := []struct {
		name              string
		svcExport         *fleetnetv1alpha1.ServiceExport
		internalSvcExport *fleetnetv1alpha1.InternalServiceExport
	}{
		{
			name: "should unexport svc",
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  memberUserNS,
					Name:       svcName,
					Finalizers: []string{svcExportCleanupFinalizer},
				},
			},
			internalSvcExport: &fleetnetv1alpha1.InternalServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMember,
					Name:      internalSvcExportName,
				},
			},
		},
		{
			name: "should unexport partially exported svc (internal svc export not yet created)",
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  memberUserNS,
					Name:       svcName,
					Finalizers: []string{svcExportCleanupFinalizer},
				},
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
			fakeHubClientBuilder := fake.NewClientBuilder().WithScheme(scheme.Scheme)
			if tc.internalSvcExport != nil {
				fakeHubClientBuilder = fakeHubClientBuilder.WithObjects(tc.internalSvcExport)
			}
			fakeHubClient := fakeHubClientBuilder.Build()
			reconciler := Reconciler{
				MemberClient: fakeMemberClient,
				HubClient:    fakeHubClient,
				HubNamespace: hubNSForMember,
				Recorder:     record.NewFakeRecorder(10),
			}

			res, err := reconciler.unexportService(ctx, tc.svcExport)
			if !cmp.Equal(res, ctrl.Result{}) || err != nil {
				t.Fatalf("unexportService() = %+v, %v, want %+v, %v", res, err, ctrl.Result{}, err)
			}

			var updatedSvcExport = &fleetnetv1alpha1.ServiceExport{}
			updatedSvcExportKey := types.NamespacedName{Namespace: tc.svcExport.Namespace, Name: tc.svcExport.Name}
			if err := fakeMemberClient.Get(ctx, updatedSvcExportKey, updatedSvcExport); err != nil {
				t.Fatalf("svc export Get(%+v), got %v, want no error", updatedSvcExportKey, err)
			}
			if updatedSvcExport.ObjectMeta.Finalizers != nil {
				t.Fatalf("svc export finalizer, got %+v, want %+v", updatedSvcExport.ObjectMeta.Finalizers, nil)
			}

			if tc.internalSvcExport == nil {
				return
			}

			var deletedInternalSvcExport = &fleetnetv1alpha1.InternalServiceExport{}
			internalSvcExportKey := types.NamespacedName{Namespace: tc.internalSvcExport.Namespace, Name: internalSvcExportName}
			if err := fakeHubClient.Get(ctx, internalSvcExportKey, deletedInternalSvcExport); !apierrors.IsNotFound(err) {
				t.Fatalf("internalSvcExport Get(%+v), got error %v, want not found error", internalSvcExportKey, err)
			}
		})
	}
}

// TestCollectAndVerifyLastSeenResourceVersionAndTimestamp tests the
// *Reconciler.collectAndVerifyLastSeenResourceVersionAndTimestamp method.
func TestCollectAndVerifyLastSeenResourceVersionAndTimestamp(t *testing.T) {
	startTime := time.Now().Round(time.Second)
	startTimeBefore := startTime.Add(-time.Second * 5)
	startTimeBeforeStr := startTimeBefore.Format(metrics.MetricsLastSeenTimestampFormat)
	startTimeBeforeFlattened, _ := time.Parse(metrics.MetricsLastSeenTimestampFormat, startTimeBeforeStr)
	startTimeAfter := startTime.Add(time.Second * 240)
	wantAnnotations := map[string]string{
		metrics.MetricsAnnotationLastSeenResourceVersion: svcResourceVersion,
		metrics.MetricsAnnotationLastSeenTimestamp:       startTime.Format(metrics.MetricsLastSeenTimestampFormat),
	}

	testCases := []struct {
		name              string
		svc               *corev1.Service
		svcExport         *fleetnetv1alpha1.ServiceExport
		startTime         time.Time
		wantExportedSince time.Time
		wantAnnotations   map[string]string
	}{
		{
			name: "serviceExport with no last seen annotations",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:       memberUserNS,
					Name:            svcName,
					ResourceVersion: svcResourceVersion,
				},
			},
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
			},
			startTime:         startTime,
			wantExportedSince: startTime,
			wantAnnotations:   wantAnnotations,
		},
		{
			name: "serviceExport with valid last seen annotations",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:       memberUserNS,
					Name:            svcName,
					ResourceVersion: svcResourceVersion,
				},
			},
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
					Annotations: map[string]string{
						metrics.MetricsAnnotationLastSeenResourceVersion: svcResourceVersion,
						metrics.MetricsAnnotationLastSeenTimestamp:       startTimeBefore.Format(metrics.MetricsLastSeenTimestampFormat),
					},
				},
			},
			startTime:         startTime,
			wantExportedSince: startTimeBeforeFlattened,
			wantAnnotations: map[string]string{
				metrics.MetricsAnnotationLastSeenResourceVersion: svcResourceVersion,
				metrics.MetricsAnnotationLastSeenTimestamp:       startTimeBefore.Format(metrics.MetricsLastSeenTimestampFormat),
			},
		},
		{
			name: "serviceExport with invalid last seen resource version (mismatch)",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:       memberUserNS,
					Name:            svcName,
					ResourceVersion: svcResourceVersion,
				},
			},
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
					Annotations: map[string]string{
						metrics.MetricsAnnotationLastSeenResourceVersion: "-1",
						metrics.MetricsAnnotationLastSeenTimestamp:       startTimeBefore.Format(metrics.MetricsLastSeenTimestampFormat),
					},
				},
			},
			startTime:         startTime,
			wantExportedSince: startTime,
			wantAnnotations:   wantAnnotations,
		},
		{
			name: "serviceExport with invalid last seen timestamp (bad data)",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:       memberUserNS,
					Name:            svcName,
					ResourceVersion: svcResourceVersion,
				},
			},
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
					Annotations: map[string]string{
						metrics.MetricsAnnotationLastSeenResourceVersion: svcResourceVersion,
						metrics.MetricsAnnotationLastSeenTimestamp:       "InvalidTimestampData",
					},
				},
			},
			startTime:         startTime,
			wantExportedSince: startTime,
			wantAnnotations:   wantAnnotations,
		},
		{
			name: "endpointslice with invalid last seen timestamp (too late timestamp)",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:       memberUserNS,
					Name:            svcName,
					ResourceVersion: svcResourceVersion,
				},
			},
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
					Annotations: map[string]string{
						metrics.MetricsAnnotationLastSeenResourceVersion: svcResourceVersion,
						metrics.MetricsAnnotationLastSeenTimestamp:       startTimeAfter.Format(metrics.MetricsLastSeenTimestampFormat),
					},
				},
			},
			startTime:         startTime,
			wantExportedSince: startTime,
			wantAnnotations:   wantAnnotations,
		},
	}

	ctx := context.Background()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeMemberClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.svc, tc.svcExport).
				Build()
			fakeHubClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
			reconciler := Reconciler{
				MemberClient: fakeMemberClient,
				HubClient:    fakeHubClient,
				HubNamespace: hubNSForMember,
				Recorder:     record.NewFakeRecorder(10),
			}

			exportedSince, err := reconciler.collectAndVerifyLastSeenResourceVersionAndTimestamp(ctx, tc.svc, tc.svcExport, tc.startTime)
			if err != nil || !exportedSince.Equal(tc.wantExportedSince) {
				t.Fatalf("collectAndVerifyLastSeenResourceVersionAndTimestamp(%+v, %+v, %v) = (%v, %v), want (%v, %v)",
					tc.svc, tc.svcExport, tc.startTime, exportedSince, err, tc.wantExportedSince, nil)
			}

			svcExport := &fleetnetv1alpha1.ServiceExport{}
			if err := fakeMemberClient.Get(ctx, svcOrSvcExportKey, svcExport); err != nil {
				t.Fatalf("serviceExport Get(%+v), got %v, want no error", svcOrSvcExportKey, err)
			}

			if diff := cmp.Diff(svcExport.Annotations, tc.wantAnnotations); diff != "" {
				t.Fatalf("serviceExport annotations (-got, +want): %s", diff)
			}
		})
	}
}

// TestAnnotateLastSeenResourceVersionAndTimestamp tests the *Reconciler.annotateLastSeenResourceVersionAndTimestamp method.
func TestAnnotateLastSeenResourceVersionAndTimestamp(t *testing.T) {
	startTime := time.Now().Round(time.Second)

	testCases := []struct {
		name            string
		svc             *corev1.Service
		svcExport       *fleetnetv1alpha1.ServiceExport
		wantAnnotations map[string]string
	}{
		{
			name: "should annotate last seen resource version and timestamp",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:       memberUserNS,
					Name:            svcName,
					ResourceVersion: svcResourceVersion,
				},
			},
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
			},
			wantAnnotations: map[string]string{
				metrics.MetricsAnnotationLastSeenResourceVersion: svcResourceVersion,
				metrics.MetricsAnnotationLastSeenTimestamp:       startTime.Format(metrics.MetricsLastSeenTimestampFormat),
			},
		},
	}

	ctx := context.Background()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeMemberClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.svc, tc.svcExport).
				Build()
			fakeHubClientBuilder := fake.NewClientBuilder().WithScheme(scheme.Scheme)
			fakeHubClient := fakeHubClientBuilder.Build()
			reconciler := Reconciler{
				MemberClient: fakeMemberClient,
				HubClient:    fakeHubClient,
				HubNamespace: hubNSForMember,
				Recorder:     record.NewFakeRecorder(10),
			}

			if err := reconciler.annotateLastSeenResourceVersionAndTimestamp(ctx, tc.svc, tc.svcExport, startTime); err != nil {
				t.Fatalf("annotateLastSeenResourceVersionAndTimestamp(%+v, %+v, %v), got %v, want no error", tc.svc, tc.svcExport, startTime, err)
			}

			svcExport := &fleetnetv1alpha1.ServiceExport{}
			if err := fakeMemberClient.Get(ctx, svcOrSvcExportKey, svcExport); err != nil {
				t.Fatalf("serviceExport Get(%+v), got %v, want no error", svcOrSvcExportKey, err)
			}

			if diff := cmp.Diff(svcExport.Annotations, tc.wantAnnotations); diff != "" {
				t.Fatalf("serviceExport annotations (-got, +want): %s", diff)
			}
		})
	}
}

func TestSetAzureRelatedInformation(t *testing.T) {
	tests := []struct {
		name                           string
		service                        *corev1.Service
		publicIPAddressListResponse    []*armnetwork.PublicIPAddress
		publicIPAddressListResponseErr error
		want                           *fleetnetv1alpha1.InternalServiceExport
		wantErr                        bool
	}{
		{
			name: "load balancer type with public ip",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					UID: "uid",
					Annotations: map[string]string{
						objectmeta.ServiceAnnotationLoadBalancerResourceGroup: "   ",
						objectmeta.ServiceAnnotationAzureLoadBalancerInternal: "True", // case sensitive
					},
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{
								IP: "1.2.3.4",
							},
						},
					},
				},
			},
			publicIPAddressListResponse: []*armnetwork.PublicIPAddress{
				{
					Properties: &armnetwork.PublicIPAddressPropertiesFormat{
						DNSSettings: &armnetwork.PublicIPAddressDNSSettings{
							DomainNameLabel: ptr.To("dnsLabel"),
						},
						IPAddress: ptr.To("1.2.3.4"),
					},
					ID: ptr.To("/subscriptions/sub1/resourceGroups/rg/providers/Microsoft.Network/publicIPAddresses/pip"),
				},
				{
					Properties: &armnetwork.PublicIPAddressPropertiesFormat{
						IPAddress: ptr.To("1.2.5.6"),
					},
				},
			},
			want: &fleetnetv1alpha1.InternalServiceExport{
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Type:                   corev1.ServiceTypeLoadBalancer,
					IsDNSLabelConfigured:   true,
					IsInternalLoadBalancer: false,
					PublicIPResourceID:     ptr.To("/subscriptions/sub1/resourceGroups/rg/providers/Microsoft.Network/publicIPAddresses/pip"),
				},
			},
		},
		{
			name: "load balancer type with public ip and dns label is not set",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					UID: "uid",
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{
								IP: "1.2.3.4",
							},
						},
					},
				},
			},
			publicIPAddressListResponse: []*armnetwork.PublicIPAddress{
				{
					Properties: &armnetwork.PublicIPAddressPropertiesFormat{
						DNSSettings: &armnetwork.PublicIPAddressDNSSettings{},
						IPAddress:   ptr.To("1.2.3.4"),
					},
					ID: ptr.To("/subscriptions/sub1/resourceGroups/rg/providers/Microsoft.Network/publicIPAddresses/pip"),
				},
				{
					Properties: &armnetwork.PublicIPAddressPropertiesFormat{},
				},
			},
			want: &fleetnetv1alpha1.InternalServiceExport{
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Type:               corev1.ServiceTypeLoadBalancer,
					PublicIPResourceID: ptr.To("/subscriptions/sub1/resourceGroups/rg/providers/Microsoft.Network/publicIPAddresses/pip"),
				},
			},
		},
		{
			name: "load balancer type with internal ip",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					UID: "uid",
					Annotations: map[string]string{
						objectmeta.ServiceAnnotationAzureLoadBalancerInternal: "true",
					},
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
				},
			},
			want: &fleetnetv1alpha1.InternalServiceExport{
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Type:                   corev1.ServiceTypeLoadBalancer,
					IsInternalLoadBalancer: true,
				},
			},
		},
		{
			name: "NodePort type service",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					UID: "uid",
					Annotations: map[string]string{
						objectmeta.ServiceAnnotationAzureLoadBalancerInternal: "true",
					},
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeNodePort,
				},
			},
			want: &fleetnetv1alpha1.InternalServiceExport{
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Type: corev1.ServiceTypeNodePort,
				},
			},
		},
		{
			name: "error when getting public ip resource",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					UID: "uid",
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{
								IP: "1.2.3.4",
							},
						},
					},
				},
			},
			publicIPAddressListResponseErr: errors.New("error"),
			wantErr:                        true,
		},
		{
			name: "stale service ingress ip",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					UID: "uid",
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{
								IP: "1.2.3.4",
							},
						},
					},
				},
			},
			publicIPAddressListResponse: []*armnetwork.PublicIPAddress{
				{
					Properties: &armnetwork.PublicIPAddressPropertiesFormat{
						DNSSettings: &armnetwork.PublicIPAddressDNSSettings{},
						IPAddress:   ptr.To("1.2.3.7"),
					},
					ID: ptr.To("/subscriptions/sub1/resourceGroups/rg/providers/Microsoft.Network/publicIPAddresses/pip"),
				},
				{
					Properties: &armnetwork.PublicIPAddressPropertiesFormat{},
				},
			},
			want: &fleetnetv1alpha1.InternalServiceExport{
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Type: corev1.ServiceTypeLoadBalancer,
				},
			},
		},
		{
			name: "service ingress ip is not set",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					UID: "uid",
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
				},
			},
			want: &fleetnetv1alpha1.InternalServiceExport{
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Type: corev1.ServiceTypeLoadBalancer,
				},
			},
		},
		{
			name: "service ingress ip is set but empty",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					UID: "uid",
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{
								IP: "",
							},
						},
					},
				},
			},
			want: &fleetnetv1alpha1.InternalServiceExport{
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Type: corev1.ServiceTypeLoadBalancer,
				},
			},
		},
		{
			name: "invalid load balancer resource group",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					UID: "uid",
					Annotations: map[string]string{
						objectmeta.ServiceAnnotationLoadBalancerResourceGroup: "invalid",
					},
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{
								IP: "1.2.3.4",
							},
						},
					},
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Reconciler{
				AzurePublicIPAddressClient: &fakePublicIPAddressClient{ListResponse: tt.publicIPAddressListResponse, ListError: tt.publicIPAddressListResponseErr},
				ResourceGroupName:          validResourceGroup,
			}
			got := &fleetnetv1alpha1.InternalServiceExport{}
			err := r.setAzureRelatedInformation(context.Background(), tt.service, got)
			if (err != nil) != tt.wantErr {
				t.Errorf("setAzureRelatedInformation() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("setAzureRelatedInformation() internalServiceExport mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

type fakePublicIPAddressClient struct {
	ListResponse []*armnetwork.PublicIPAddress
	ListError    error
}

func (c *fakePublicIPAddressClient) Get(_ context.Context, _ string, _ string, _ *string) (*armnetwork.PublicIPAddress, error) {
	return nil, nil
}

func (c *fakePublicIPAddressClient) CreateOrUpdate(_ context.Context, _ string, _ string, _ armnetwork.PublicIPAddress) (*armnetwork.PublicIPAddress, error) {
	return nil, nil
}

func (c *fakePublicIPAddressClient) Delete(_ context.Context, _ string, _ string) error {
	return nil
}

func (c *fakePublicIPAddressClient) List(_ context.Context, rg string) ([]*armnetwork.PublicIPAddress, error) {
	if rg == validResourceGroup {
		return c.ListResponse, c.ListError
	}
	return nil, errors.New("invalid resource group")
}
