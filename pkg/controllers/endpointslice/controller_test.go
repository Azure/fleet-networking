/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package endpointslice

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

const (
	memberClusterID                    = "bravelion"
	memberUserNS                       = "work"
	hubNSForMember                     = "bravelion"
	svcName                            = "app"
	invalidSvcName                     = "app2"
	conflictedSvcName                  = "app3"
	exportedSvcName                    = "app4"
	svcExportValidCondReason           = "ServiceIsValid"
	svcExportInvalidNotFoundCondReason = "ServiceNotFound"
	svcExportNoConflictCondReason      = "ServiceHasNoConflict"
	svcExportConflictedCondReason      = "ServiceIsConflicted"
	endpointSliceName                  = "app-endpointslice"
	altEndpointSliceName               = "app-endpointslice-2"
	unexportableEndpointSliceName      = "app-endpointslice-3"
	unmanagedEndpointSliceName         = "app-endpointslice-4"
	unmanagedExportedEndpointSliceName = "app-endpointslice-5"
	managedEndpointSliceName           = "app-endpointslice-6"
	managedExportedEndpointSliceName   = "app-endpointslice-7"
	exportedEndpointSliceName          = "app-endpointslice-8"
	altExportedEndpointSliceName       = "app-endpointslice-9"
	unexportedEndpointSliceName        = "app-endpointslice-10"
	altUnexportedEndpointSliceName     = "app-endpointslice-11"
	deletedEndpointSliceName           = "app-endpointslice-12"
)

func randomLengthString(n int) string {
	alphabet := []rune("abcdefghijklmnopqrstuvwxyz")
	b := make([]rune, n)
	for i := range b {
		b[i] = alphabet[rand.Intn(len(alphabet))] //nolint:gosec
	}
	return string(b)
}

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

// serviceExportNoConflictCondition returns a ServiceExportConflict condition for exporting a Service with no
// conflicts.
func serviceExportNoConflictCondition(userNS, svcName string) metav1.Condition {
	return metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportConflict),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             svcExportNoConflictCondReason,
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
		Reason:             svcExportConflictedCondReason,
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

// TestFormatUniqueName tests the formatUniqueName function.
func TestFormatUniqueName(t *testing.T) {
	randomClusterID := randomLengthString(50)
	randomNS := randomLengthString(63)
	randomName := randomLengthString(253)
	testCases := []struct {
		name           string
		clusterID      string
		endpointSlice  *discoveryv1.EndpointSlice
		expectedPrefix string
	}{
		{
			name:      "name within limit",
			clusterID: memberClusterID,
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
				},
			},
			expectedPrefix: fmt.Sprintf("%s-%s-%s", memberClusterID, memberUserNS, endpointSliceName),
		},
		{
			name:      "name over length limit",
			clusterID: randomClusterID,
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: randomNS,
					Name:      randomName,
				},
			},
			expectedPrefix: fmt.Sprintf("%s-%s-%s", randomClusterID, randomNS, randomName[:131]),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			uniqueName := formatFleetUniqueName(tc.clusterID, tc.endpointSlice)
			if !strings.HasPrefix(uniqueName, tc.expectedPrefix) {
				t.Fatalf("formatFleetUniqueName(%s, %+v) = %s, want prefix %s", tc.clusterID, tc.endpointSlice, uniqueName, tc.expectedPrefix)
			}
			if errs := validation.IsDNS1123Subdomain(uniqueName); errs != nil {
				t.Fatalf("IsDNS1123Subdomain(%s), got %v, want no errors", uniqueName, errs)
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
	endpointSlice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      endpointSliceName,
			Labels: map[string]string{
				endpointSliceUniqueNameLabel: fmt.Sprintf("%s-%s-%s", memberClusterID, memberUserNS, endpointSliceName),
			},
		},
	}
	unexportedEndpointSlice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      altEndpointSliceName,
			Labels: map[string]string{
				endpointSliceUniqueNameLabel: fmt.Sprintf("%s-%s-%s-%s",
					memberClusterID, memberUserNS, endpointSliceName, randomLengthString(5)),
			},
		},
	}
	endpointSliceExport := &fleetnetv1alpha1.EndpointSliceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: hubNSForMember,
			Name:      fmt.Sprintf("%s-%s-%s", memberClusterID, memberUserNS, endpointSliceName),
		},
	}
	testCases := []struct {
		name          string
		endpointSlice *discoveryv1.EndpointSlice
	}{
		{
			name:          "should delete endpoint slice export",
			endpointSlice: endpointSlice,
		},
		{
			name:          "should ignore not found endpoint slice export",
			endpointSlice: endpointSlice,
		},
	}

	fakeMemberClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(endpointSlice, unexportedEndpointSlice).
		Build()
	fakeHubClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(endpointSliceExport).
		Build()
	reconciler := &Reconciler{
		memberClient: fakeMemberClient,
		hubClient:    fakeHubClient,
		hubNamespace: hubNSForMember,
	}
	ctx := context.Background()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := reconciler.unexportEndpointSlice(ctx, endpointSlice); err != nil {
				t.Fatalf("unexportEndpointSlice(%+v), got %v, want no error", endpointSlice, err)
			}
		})
	}
}

// TestAssignUniqueNameAsLabel tests the *Reconciler.assignUniqueNameAsLabel method.
func TestAssignUniqueNameAsLabel(t *testing.T) {
	endpointSlice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      endpointSliceName,
		},
	}
	testCases := []struct {
		name          string
		endpointSlice *discoveryv1.EndpointSlice
		want          string
	}{
		{
			name:          "should assign unique name label",
			endpointSlice: endpointSlice,
			want:          fmt.Sprintf("%s-%s-%s", memberClusterID, memberUserNS, endpointSliceName),
		},
	}

	fakeMemberClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(endpointSlice).
		Build()
	fakeHubClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		Build()
	reconciler := &Reconciler{
		memberClusterID: memberClusterID,
		memberClient:    fakeMemberClient,
		hubClient:       fakeHubClient,
		hubNamespace:    hubNSForMember,
	}
	ctx := context.Background()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			uniqueName, err := reconciler.assignUniqueNameAsLabel(ctx, tc.endpointSlice)
			if err != nil {
				t.Fatalf("assignUniqueNameAsLabel(%+v), got %v, want no error", endpointSlice, err)
			}
			if uniqueName != tc.want {
				t.Fatalf("assignUniqueNameAsLabel(%+v) = %s, want %s", endpointSlice, uniqueName, tc.want)
			}

			var updatedEndpointSlice = discoveryv1.EndpointSlice{}
			updatedEndpointSliceKey := types.NamespacedName{Namespace: memberUserNS, Name: endpointSliceName}
			if err := fakeMemberClient.Get(ctx, updatedEndpointSliceKey, &updatedEndpointSlice); err != nil {
				t.Fatalf("endpointSlice Get(), got %v, want no error", err)
			}
			if setUniqueName := updatedEndpointSlice.Labels[endpointSliceUniqueNameLabel]; setUniqueName != tc.want {
				t.Fatalf("unique name label, got %s, want %s", setUniqueName, tc.want)
			}
		})
	}
}

// TestShouldSkipOrUnexportEndpointSlice_NoServiceExport tests the *Reconciler.shouldSkipOrUnexportEndpointSlice method.
func TestShouldSkipOrUnexportEndpointSlice_NoServiceExport(t *testing.T) {
	unexportableEndpointSlice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      unexportableEndpointSliceName,
		},
		AddressType: discoveryv1.AddressTypeIPv6,
	}

	unmanagedEndpointSlice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      unmanagedEndpointSliceName,
		},
		AddressType: discoveryv1.AddressTypeIPv4,
	}
	unmanagedExportedEndpointSlice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      unmanagedExportedEndpointSliceName,
			Labels: map[string]string{
				endpointSliceUniqueNameLabel: fmt.Sprintf("%s-%s-%s", memberClusterID, memberUserNS, unmanagedExportedEndpointSliceName),
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
	}

	managedEndpointSlice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      managedEndpointSliceName,
			Labels: map[string]string{
				discoveryv1.LabelServiceName: svcName,
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
	}
	managedExportedEndpointSlice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      managedExportedEndpointSliceName,
			Labels: map[string]string{
				discoveryv1.LabelServiceName: svcName,
				endpointSliceUniqueNameLabel: fmt.Sprintf("%s-%s-%s", memberClusterID, memberUserNS, managedExportedEndpointSliceName),
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
	}

	fakeMemberClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(unexportableEndpointSlice,
			managedEndpointSlice,
			managedExportedEndpointSlice,
			unmanagedEndpointSlice,
			unmanagedExportedEndpointSlice).
		Build()
	fakeHubClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		Build()
	reconciler := &Reconciler{
		memberClusterID: memberClusterID,
		memberClient:    fakeMemberClient,
		hubClient:       fakeHubClient,
		hubNamespace:    hubNSForMember,
	}
	ctx := context.Background()

	testCases := []struct {
		name          string
		endpointSlice *discoveryv1.EndpointSlice
		want          skipOrUnexportEndpointSliceOp
	}{
		{
			name:          "should skip endpoint slice (unexportable)",
			endpointSlice: unexportableEndpointSlice,
			want:          shouldSkipEndpointSliceOp,
		},
		{
			name:          "should skip endpoint slice (unmanaged)",
			endpointSlice: unmanagedEndpointSlice,
			want:          shouldSkipEndpointSliceOp,
		},
		{
			name:          "should unexport endpoint slice (unmanaged yet exported)",
			endpointSlice: unmanagedExportedEndpointSlice,
			want:          shouldUnexportEndpointSliceOp,
		},
		{
			name:          "should skip endpoint slice (no exported svc)",
			endpointSlice: managedEndpointSlice,
			want:          shouldSkipEndpointSliceOp,
		},
		{
			name:          "should unexport endpoint slice (no exported svc yet exported)",
			endpointSlice: managedExportedEndpointSlice,
			want:          shouldUnexportEndpointSliceOp,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
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
	exportedEndpointSlice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      exportedEndpointSliceName,
			Labels: map[string]string{
				discoveryv1.LabelServiceName: invalidSvcName,
				endpointSliceUniqueNameLabel: fmt.Sprintf("%s-%s-%s", memberClusterID, memberUserNS, exportedEndpointSliceName),
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
	}
	altExportedEndpointSlice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      altExportedEndpointSliceName,
			Labels: map[string]string{
				discoveryv1.LabelServiceName: conflictedSvcName,
				endpointSliceUniqueNameLabel: fmt.Sprintf("%s-%s-%s", memberClusterID, memberUserNS, altExportedEndpointSliceName),
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
	}

	unexportedEndpointSice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      unexportedEndpointSliceName,
			Labels: map[string]string{
				discoveryv1.LabelServiceName: invalidSvcName,
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
	}
	altUnexportedEndpointSlice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      altUnexportedEndpointSliceName,
			Labels: map[string]string{
				discoveryv1.LabelServiceName: conflictedSvcName,
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
	}

	invalidSvcExport := &fleetnetv1alpha1.ServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      invalidSvcName,
		},
		Status: fleetnetv1alpha1.ServiceExportStatus{
			Conditions: []metav1.Condition{
				serviceExportInvalidNotFoundCondition(memberClusterID, invalidSvcName),
			},
		},
	}
	conflictedSvcExport := &fleetnetv1alpha1.ServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      conflictedSvcName,
		},
		Status: fleetnetv1alpha1.ServiceExportStatus{
			Conditions: []metav1.Condition{
				serviceExportValidCondition(memberUserNS, conflictedSvcName),
				serviceExportConflictedCondition(memberClusterID, conflictedSvcName),
			},
		},
	}

	fakeMemberClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(exportedEndpointSlice,
			altExportedEndpointSlice,
			unexportedEndpointSice,
			altUnexportedEndpointSlice,
			invalidSvcExport,
			conflictedSvcExport).
		Build()
	fakeHubClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		Build()
	reconciler := &Reconciler{
		memberClusterID: memberClusterID,
		memberClient:    fakeMemberClient,
		hubClient:       fakeHubClient,
		hubNamespace:    hubNSForMember,
	}
	ctx := context.Background()

	testCases := []struct {
		name          string
		endpointSlice *discoveryv1.EndpointSlice
		want          skipOrUnexportEndpointSliceOp
	}{
		{
			name:          "should unexport endpoint slice (invalid svc export)",
			endpointSlice: exportedEndpointSlice,
			want:          shouldUnexportEndpointSliceOp,
		},
		{
			name:          "should unexport endpoint slice (conflicted svc export)",
			endpointSlice: altExportedEndpointSlice,
			want:          shouldUnexportEndpointSliceOp,
		},
		{
			name:          "should skip endpoint slice (invalid svc export)",
			endpointSlice: unexportedEndpointSice,
			want:          shouldSkipEndpointSliceOp,
		},
		{
			name:          "should skip endpoint slice (conflicted svc export)",
			endpointSlice: altUnexportedEndpointSlice,
			want:          shouldSkipEndpointSliceOp,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
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
	deletedTimestamp := metav1.Now()
	exportedEndpointSlice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      exportedEndpointSliceName,
			Labels: map[string]string{
				discoveryv1.LabelServiceName: exportedSvcName,
				endpointSliceUniqueNameLabel: fmt.Sprintf("%s-%s-%s", memberClusterID, memberUserNS, exportedEndpointSliceName),
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
	}
	unexportedEndpointSice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      unexportedEndpointSliceName,
			Labels: map[string]string{
				discoveryv1.LabelServiceName: exportedSvcName,
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
	}
	deletedEndpointSliceName := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:         memberUserNS,
			Name:              deletedEndpointSliceName,
			DeletionTimestamp: &deletedTimestamp,
			Labels: map[string]string{
				discoveryv1.LabelServiceName: exportedSvcName,
				endpointSliceUniqueNameLabel: fmt.Sprintf("%s-%s-%s", memberClusterID, memberUserNS, deletedEndpointSliceName),
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
	}

	svcExport := &fleetnetv1alpha1.ServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      exportedSvcName,
		},
		Status: fleetnetv1alpha1.ServiceExportStatus{
			Conditions: []metav1.Condition{
				serviceExportValidCondition(memberUserNS, exportedSvcName),
				serviceExportNoConflictCondition(memberUserNS, exportedSvcName),
			},
		},
	}

	fakeMemberClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(exportedEndpointSlice,
			unexportedEndpointSice,
			svcExport).
		Build()
	fakeHubClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		Build()
	reconciler := &Reconciler{
		memberClusterID: memberClusterID,
		memberClient:    fakeMemberClient,
		hubClient:       fakeHubClient,
		hubNamespace:    hubNSForMember,
	}
	ctx := context.Background()

	testCases := []struct {
		name          string
		endpointSlice *discoveryv1.EndpointSlice
		want          skipOrUnexportEndpointSliceOp
	}{
		{
			name:          "should export endpoint slice (update)",
			endpointSlice: exportedEndpointSlice,
			want:          noSkipOrUnexportNeededOp,
		},
		{
			name:          "should export endpoint slice (create)",
			endpointSlice: unexportedEndpointSice,
			want:          noSkipOrUnexportNeededOp,
		},
		{
			name:          "should unexport endpoint slice (deleted)",
			endpointSlice: deletedEndpointSliceName,
			want:          shouldUnexportEndpointSliceOp,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
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
