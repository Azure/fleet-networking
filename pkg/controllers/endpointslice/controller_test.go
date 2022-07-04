/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package endpointslice

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/helper"
)

const (
	memberClusterID   = "bravelion"
	memberUserNS      = "work"
	hubNSForMember    = "bravelion"
	svcName           = "app"
	endpointSliceName = "app-endpointslice"
)

// setupFakeClient returns a populated fake client.
func setupFakeClient(objs ...client.Object) client.Client {
	return fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(objs...).Build()
}

// serviceExportValidCond returns a ServiceExportValid condition for exporting a valid Service.
func serviceExportValidCondition(userNS, svcName string) metav1.Condition {
	return metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportValid),
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             "ServiceIsValid",
		Message:            fmt.Sprintf("service %s/%s is valid for export", userNS, svcName),
	}
}

// serviceExportInvalidNotFoundCond returns a ServiceExportValid condition for exporting a Service that is not found.
func serviceExportInvalidNotFoundCondition(userNS, svcName string) metav1.Condition {
	return metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportValid),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             "ServiceNotFound",
		Message:            fmt.Sprintf("service %s/%s is not found", userNS, svcName),
	}
}

// serviceExportNoConflictCondition returns a ServiceExportConflict condition for exporting a Service with no
// conflicts.
func serviceExportNoConflictCondition(userNS, svcName string) metav1.Condition {
	return metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportConflict),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             "ServiceHasNoConflict",
		Message:            fmt.Sprintf("service %s/%s has no conflict with other exported Services", userNS, svcName),
	}
}

// serviceExportConflictedCondition returns a ServiceExportConflict condition for exporting a Service in conflict
// with other exported Services.
func serviceExportConflictedCondition(userNS, svcName string) metav1.Condition {
	return metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportConflict),
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             "ServiceIsConflicted",
		Message:            fmt.Sprintf("service %s/%s is in conflict with other exported Services", userNS, svcName),
	}
}

func TestMain(m *testing.M) {
	// Add custom APIs to the runtime scheme
	err := fleetnetv1alpha1.AddToScheme(scheme.Scheme)
	if err != nil {
		log.Fatalf("failed to add custom APIs to the runtime scheme: %v", err)
	}

	os.Exit(m.Run())
}

// TestIsEndpointSlicePermanentlyUnexportable tests the isEndpointSlicePermanentlyUnexportable function.
func TestIsEndpointSlicePermanentlyUnexportable(t *testing.T) {
	testCases := []struct {
		name          string
		endpointSlice *discoveryv1.EndpointSlice
		want          bool
	}{
		{
			name: "should be exportable",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
				},
				AddressType: discoveryv1.AddressTypeIPv4,
			},
		},
		{
			name: "should not be exportable",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
				},
				AddressType: discoveryv1.AddressTypeIPv6,
			},
			want: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if res := isEndpointSlicePermanentlyUnexportable(tc.endpointSlice); res != tc.want {
				t.Fatalf("isEndpointSliceExport(%+v) = %t, want %t", tc.endpointSlice, res, tc.want)
			}
		})
	}
}

// TestExtractEndpointsFromEndpointSlice tests the extractEndpointsFromEndpointSlice function.
func TestExtractEndpointsFromEndpointSlice(t *testing.T) {
	isReady := true
	isNotReady := false
	readyAddress := "1.2.3.4"
	unknownStateAddress := "2.3.4.5"
	notReadyAddress := "3.4.5.6"

	testCases := []struct {
		name              string
		endpointSlice     *discoveryv1.EndpointSlice
		expectedEndpoints []fleetnetv1alpha1.Endpoint
	}{
		{
			name: "should extract ready endpoints only",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
				},
				Endpoints: []discoveryv1.Endpoint{
					{
						Addresses: []string{readyAddress},
						Conditions: discoveryv1.EndpointConditions{
							Ready: &isReady,
						},
					},
					{
						Addresses:  []string{unknownStateAddress},
						Conditions: discoveryv1.EndpointConditions{},
					},
					{
						Addresses: []string{notReadyAddress},
						Conditions: discoveryv1.EndpointConditions{
							Ready: &isNotReady,
						},
					},
				},
			},
			expectedEndpoints: []fleetnetv1alpha1.Endpoint{
				{
					Addresses: []string{readyAddress},
				},
				{
					Addresses: []string{unknownStateAddress},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			extractedEndpoints := extractEndpointsFromEndpointSlice(tc.endpointSlice)
			if !cmp.Equal(extractedEndpoints, tc.expectedEndpoints) {
				t.Fatalf("extractEndpointsFromEndpointSlice(%+v) = %+v, want %+v", tc.endpointSlice, extractedEndpoints, tc.expectedEndpoints)
			}
		})
	}
}

// TestUnexportEndpointSlice tests the *Reconciler.unexportEndpointSlice method.
func TestUnexportEndpointSlice(t *testing.T) {
	testCases := []struct {
		name                string
		endpointSlice       *discoveryv1.EndpointSlice
		endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport
	}{
		{
			name: "should delete endpoint slice export",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
					Labels: map[string]string{
						endpointSliceUniqueNameLabel: fmt.Sprintf("%s-%s-%s", memberClusterID, memberUserNS, endpointSliceName),
					},
				},
			},
			endpointSliceExport: &fleetnetv1alpha1.EndpointSliceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMember,
					Name:      fmt.Sprintf("%s-%s-%s", memberClusterID, memberUserNS, endpointSliceName),
				},
			},
		},
		{
			name: "should ignore not found endpoint slice export",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
					Labels: map[string]string{
						endpointSliceUniqueNameLabel: fmt.Sprintf("%s-%s-%s-%s",
							memberClusterID, memberUserNS, endpointSliceName, helper.RandomLowerCaseAlphabeticString(5)),
					},
				},
			},
		},
	}

	ctx := context.Background()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeMemberClient := setupFakeClient(tc.endpointSlice)
			fakeHubClient := setupFakeClient()
			if tc.endpointSliceExport != nil {
				fakeHubClient = setupFakeClient(tc.endpointSliceExport)
			}
			reconciler := &Reconciler{
				memberClient: fakeMemberClient,
				hubClient:    fakeHubClient,
				hubNamespace: hubNSForMember,
			}

			if err := reconciler.unexportEndpointSlice(ctx, tc.endpointSlice); err != nil {
				t.Fatalf("unexportEndpointSlice(%+v), got %v, want no error", tc.endpointSlice, err)
			}

			updatedEndpointSlice := &discoveryv1.EndpointSlice{}
			updatedEndpointSliceKey := types.NamespacedName{
				Namespace: tc.endpointSlice.Namespace,
				Name:      tc.endpointSlice.Name,
			}
			if err := reconciler.memberClient.Get(ctx, updatedEndpointSliceKey, updatedEndpointSlice); err != nil {
				t.Fatalf("Get(%+v), got %v, want no error", updatedEndpointSliceKey, err)
			}
			if _, ok := updatedEndpointSlice.Labels[endpointSliceUniqueNameLabel]; ok {
				t.Fatalf("endpointSlice labels, got %+v, want no %s label", updatedEndpointSlice.Labels, endpointSliceUniqueNameLabel)
			}

			if tc.endpointSliceExport == nil {
				return
			}
			endpointSliceExportKey := types.NamespacedName{
				Namespace: tc.endpointSliceExport.Namespace,
				Name:      tc.endpointSliceExport.Name,
			}
			if err := reconciler.hubClient.Get(ctx, endpointSliceExportKey, tc.endpointSliceExport); !errors.IsNotFound(err) {
				t.Fatalf("Get(%+v), got %v, want not found error", tc.endpointSliceExport, err)
			}
		})
	}
}

// TestAssignUniqueNameAsLabel tests the *Reconciler.assignUniqueNameAsLabel method.
func TestAssignUniqueNameAsLabel(t *testing.T) {
	testCases := []struct {
		name           string
		endpointSlice  *discoveryv1.EndpointSlice
		expectedPrefix string
	}{
		{
			name: "should assign unique name label",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
				},
			},
			expectedPrefix: fmt.Sprintf("%s-%s-%s-", memberClusterID, memberUserNS, endpointSliceName),
		},
	}

	ctx := context.Background()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeMemberClient := setupFakeClient(tc.endpointSlice)
			fakeHubClient := setupFakeClient()
			reconciler := &Reconciler{
				memberClusterID: memberClusterID,
				memberClient:    fakeMemberClient,
				hubClient:       fakeHubClient,
				hubNamespace:    hubNSForMember,
			}

			uniqueName, err := reconciler.assignUniqueNameAsLabel(ctx, tc.endpointSlice)
			if err != nil {
				t.Fatalf("assignUniqueNameAsLabel(%+v), got %v, want no error", tc.endpointSlice, err)
			}
			if !strings.HasPrefix(uniqueName, tc.expectedPrefix) {
				t.Fatalf("assignUniqueNameAsLabel(%+v) = %s, want prefix %s", tc.endpointSlice, uniqueName, tc.expectedPrefix)
			}

			var updatedEndpointSlice = discoveryv1.EndpointSlice{}
			updatedEndpointSliceKey := types.NamespacedName{Namespace: memberUserNS, Name: endpointSliceName}
			if err := fakeMemberClient.Get(ctx, updatedEndpointSliceKey, &updatedEndpointSlice); err != nil {
				t.Fatalf("endpointSlice Get(), got %v, want no error", err)
			}
			if setUniqueName := updatedEndpointSlice.Labels[endpointSliceUniqueNameLabel]; !strings.HasPrefix(setUniqueName, tc.expectedPrefix) {
				t.Fatalf("unique name label, got %s, want %s", setUniqueName, tc.expectedPrefix)
			}
		})
	}
}

// TestShouldSkipOrUnexportEndpointSlice_NoServiceExport tests the *Reconciler.shouldSkipOrUnexportEndpointSlice method.
func TestShouldSkipOrUnexportEndpointSlice_NoServiceExport(t *testing.T) {
	testCases := []struct {
		name          string
		endpointSlice *discoveryv1.EndpointSlice
		want          skipOrUnexportEndpointSliceOp
	}{
		{
			name: "should skip endpoint slice (unexportable)",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
				},
				AddressType: discoveryv1.AddressTypeIPv6,
			},
			want: shouldSkipEndpointSliceOp,
		},
		{
			name: "should skip endpoint slice (unmanaged)",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
				},
				AddressType: discoveryv1.AddressTypeIPv4,
			},
			want: shouldSkipEndpointSliceOp,
		},
		{
			name: "should unexport endpoint slice (unmanaged yet exported)",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
					Labels: map[string]string{
						endpointSliceUniqueNameLabel: fmt.Sprintf("%s-%s-%s", memberClusterID, memberUserNS, endpointSliceName),
					},
				},
				AddressType: discoveryv1.AddressTypeIPv4,
			},
			want: shouldUnexportEndpointSliceOp,
		},
		{
			name: "should skip endpoint slice (no exported svc)",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
					Labels: map[string]string{
						discoveryv1.LabelServiceName: svcName,
					},
				},
				AddressType: discoveryv1.AddressTypeIPv4,
			},
			want: shouldSkipEndpointSliceOp,
		},
		{
			name: "should unexport endpoint slice (no exported svc yet exported)",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
					Labels: map[string]string{
						discoveryv1.LabelServiceName: svcName,
						endpointSliceUniqueNameLabel: fmt.Sprintf("%s-%s-%s", memberClusterID, memberUserNS, endpointSliceName),
					},
				},
				AddressType: discoveryv1.AddressTypeIPv4,
			},
			want: shouldUnexportEndpointSliceOp,
		},
	}

	ctx := context.Background()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeMemberClient := setupFakeClient(tc.endpointSlice)
			fakeHubClient := setupFakeClient()
			reconciler := &Reconciler{
				memberClient: fakeMemberClient,
				hubClient:    fakeHubClient,
				hubNamespace: hubNSForMember,
			}

			op, err := reconciler.shouldSkipOrUnexportEndpointSlice(ctx, tc.endpointSlice)
			if err != nil {
				t.Fatalf("shouldSkipOrUnexportEndpointSlice(%+v), got %v, want no error", tc.endpointSlice, err)
			}
			if op != tc.want {
				t.Fatalf("shouldSkipOrUnexportEndpointSlice(%+v) = %d, want %d", tc.endpointSlice, op, tc.want)
			}
		})
	}
}

// TestShouldSkipOrUnexportEndpointSlice_InvalidOrConflictedServiceExport tests the
// *Reconciler.shouldSkipOrUnexportEndpointSlice method.
func TestShouldSkipOrUnexportEndpointSlice_InvalidOrConflictedServiceExport(t *testing.T) {
	testCases := []struct {
		name          string
		endpointSlice *discoveryv1.EndpointSlice
		svcExport     *fleetnetv1alpha1.ServiceExport
		want          skipOrUnexportEndpointSliceOp
	}{
		{
			name: "should unexport endpoint slice (invalid svc export)",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
					Labels: map[string]string{
						discoveryv1.LabelServiceName: svcName,
						endpointSliceUniqueNameLabel: fmt.Sprintf("%s-%s-%s", memberClusterID, memberUserNS, endpointSliceName),
					},
				},
				AddressType: discoveryv1.AddressTypeIPv4,
			},
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
				Status: fleetnetv1alpha1.ServiceExportStatus{
					Conditions: []metav1.Condition{
						serviceExportInvalidNotFoundCondition(memberClusterID, svcName),
					},
				},
			},
			want: shouldUnexportEndpointSliceOp,
		},
		{
			name: "should unexport endpoint slice (conflicted svc export)",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
					Labels: map[string]string{
						discoveryv1.LabelServiceName: svcName,
						endpointSliceUniqueNameLabel: fmt.Sprintf("%s-%s-%s", memberClusterID, memberUserNS, endpointSliceName),
					},
				},
				AddressType: discoveryv1.AddressTypeIPv4,
			},
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
				Status: fleetnetv1alpha1.ServiceExportStatus{
					Conditions: []metav1.Condition{
						serviceExportValidCondition(memberUserNS, svcName),
						serviceExportConflictedCondition(memberClusterID, svcName),
					},
				},
			},
			want: shouldUnexportEndpointSliceOp,
		},
		{
			name: "should skip endpoint slice (invalid svc export)",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
					Labels: map[string]string{
						discoveryv1.LabelServiceName: svcName,
					},
				},
				AddressType: discoveryv1.AddressTypeIPv4,
			},
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
				Status: fleetnetv1alpha1.ServiceExportStatus{
					Conditions: []metav1.Condition{
						serviceExportInvalidNotFoundCondition(memberClusterID, svcName),
					},
				},
			},
			want: shouldSkipEndpointSliceOp,
		},
		{
			name: "should skip endpoint slice (conflicted svc export)",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
					Labels: map[string]string{
						discoveryv1.LabelServiceName: svcName,
					},
				},
				AddressType: discoveryv1.AddressTypeIPv4,
			},
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
				Status: fleetnetv1alpha1.ServiceExportStatus{
					Conditions: []metav1.Condition{
						serviceExportValidCondition(memberUserNS, svcName),
						serviceExportConflictedCondition(memberClusterID, svcName),
					},
				},
			},
			want: shouldSkipEndpointSliceOp,
		},
	}

	ctx := context.Background()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeMemberClient := setupFakeClient(tc.endpointSlice, tc.svcExport)
			fakeHubClient := setupFakeClient()
			reconciler := &Reconciler{
				memberClusterID: memberClusterID,
				memberClient:    fakeMemberClient,
				hubClient:       fakeHubClient,
				hubNamespace:    hubNSForMember,
			}

			op, err := reconciler.shouldSkipOrUnexportEndpointSlice(ctx, tc.endpointSlice)
			if err != nil {
				t.Fatalf("shouldSkipOrUnexportEndpointSlice(%+v), got %v, want no error", tc.endpointSlice, err)
			}
			if op != tc.want {
				t.Fatalf("shouldSkipOrUnexportEndpointSlice(%+v) = %d, want %d", tc.endpointSlice, op, tc.want)
			}
		})
	}
}

// TestShouldSkipOrUnexportEndpointSlice_ExportedService tests the *Reconciler.shouldSkipOrUnexportEndpointSlice
// method.
func TestShouldSkipOrUnexportEndpointSlice_ExportedService(t *testing.T) {
	deletionTimestamp := metav1.Now()
	svcExport := &fleetnetv1alpha1.ServiceExport{
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
	}

	testCases := []struct {
		name          string
		endpointSlice *discoveryv1.EndpointSlice
		want          skipOrUnexportEndpointSliceOp
	}{
		{
			name: "should export endpoint slice (update)",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
					Labels: map[string]string{
						discoveryv1.LabelServiceName: svcName,
						endpointSliceUniqueNameLabel: fmt.Sprintf("%s-%s-%s", memberClusterID, memberUserNS, endpointSliceName),
					},
				},
				AddressType: discoveryv1.AddressTypeIPv4,
			},
			want: noSkipOrUnexportNeededOp,
		},
		{
			name: "should export endpoint slice (create)",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
					Labels: map[string]string{
						discoveryv1.LabelServiceName: svcName,
					},
				},
				AddressType: discoveryv1.AddressTypeIPv4,
			},
			want: noSkipOrUnexportNeededOp,
		},
		{
			name: "should unexport endpoint slice (deleted)",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:         memberUserNS,
					Name:              endpointSliceName,
					DeletionTimestamp: &deletionTimestamp,
					Labels: map[string]string{
						discoveryv1.LabelServiceName: svcName,
						endpointSliceUniqueNameLabel: fmt.Sprintf("%s-%s-%s", memberClusterID, memberUserNS, endpointSliceName),
					},
				},
				AddressType: discoveryv1.AddressTypeIPv4,
			},
			want: shouldUnexportEndpointSliceOp,
		},
	}

	ctx := context.Background()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeMemberClient := setupFakeClient(tc.endpointSlice, svcExport)
			fakeHubClient := setupFakeClient()
			reconciler := &Reconciler{
				memberClusterID: memberClusterID,
				memberClient:    fakeMemberClient,
				hubClient:       fakeHubClient,
				hubNamespace:    hubNSForMember,
			}

			op, err := reconciler.shouldSkipOrUnexportEndpointSlice(ctx, tc.endpointSlice)
			if err != nil {
				t.Fatalf("shouldSkipOrUnexportEndpointSlice(%+v), got %v, want no error", tc.endpointSlice, err)
			}
			if op != tc.want {
				t.Fatalf("shouldSkipOrUnexportEndpointSlice(%+v) = %d, want %d", tc.endpointSlice, op, tc.want)
			}
		})
	}
}

// TestIsServiceExportValidWithNoConflict tests the isServiceExportValidWithNoConflict function.
func TestIsServiceExportValidWithNoConflict(t *testing.T) {

}

// TestIsUniqueNameValid tests the isUniqueNameValid function.
func TestIsUniqueNameValid(t *testing.T) {

}

// TestIsEndpointSliceExportLinkedWithEndpointSlice tests the isEndpointSliceExportLinkedWithEndpointSlice function.
func TestIsEndpointSliceExportLinkedWithEndpointSlice(t *testing.T) {

}
