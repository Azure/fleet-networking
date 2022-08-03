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
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/uniquename"
)

const (
	memberClusterID         = "bravelion"
	memberUserNS            = "work"
	hubNSForMember          = "bravelion"
	svcName                 = "app"
	endpointSliceName       = "app-endpointslice"
	endpointSliceUniqueName = "bravelion-work-app-endpointslice"
)

// serviceExportValidCond returns a ServiceExportValid condition for exporting a valid Service.
func serviceExportValidCondition(userNS, svcName string) metav1.Condition {
	return metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportValid),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: 0,
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
		ObservedGeneration: 1,
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
		ObservedGeneration: 2,
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
		ObservedGeneration: 3,
		LastTransitionTime: metav1.Now(),
		Reason:             "ServiceIsConflicted",
		Message:            fmt.Sprintf("service %s/%s is in conflict with other exported Services", userNS, svcName),
	}
}

func TestMain(m *testing.M) {
	// Add custom APIs to the runtime scheme
	if err := fleetnetv1alpha1.AddToScheme(scheme.Scheme); err != nil {
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
			name: "should be exportable (IPv4 endpointslice)",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
				},
				AddressType: discoveryv1.AddressTypeIPv4,
			},
			want: false,
		},
		{
			name: "should not be exportable (IPv6 endpointslice)",
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

// TestUnexportLinkedEndpointSlice tests the *Reconciler.unexportEndpointSlice and the
// *Reconciler.deleteEndpointSliceIfLinked method.
func TestUnexportLinkedEndpointSlice(t *testing.T) {
	testCases := []struct {
		name                string
		endpointSlice       *discoveryv1.EndpointSlice
		endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport
	}{
		{
			name: "should delete endpoint slice export (endpointslice has been exported)",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
					Annotations: map[string]string{
						endpointSliceUniqueNameAnnotation: endpointSliceUniqueName,
					},
					UID: "1",
				},
			},
			endpointSliceExport: &fleetnetv1alpha1.EndpointSliceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMember,
					Name:      endpointSliceUniqueName,
				},
				Spec: fleetnetv1alpha1.EndpointSliceExportSpec{
					EndpointSliceReference: fleetnetv1alpha1.ExportedObjectReference{
						ClusterID:       memberClusterID,
						Kind:            "EndpointSlice",
						Namespace:       memberUserNS,
						Name:            endpointSliceName,
						ResourceVersion: "0",
						Generation:      0,
						UID:             "1",
					},
				},
			},
		},
		{
			name: "should ignore not found endpoint slice export (endpointslice has not been exported yet)",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
					Annotations: map[string]string{
						endpointSliceUniqueNameAnnotation: fmt.Sprintf("%s-%s-%s-%s",
							memberClusterID, memberUserNS, endpointSliceName, uniquename.RandomLowerCaseAlphabeticString(5)),
					},
				},
			},
		},
	}

	ctx := context.Background()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeMemberClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.endpointSlice).
				Build()
			fakeHubClientBuilder := fake.NewClientBuilder().WithScheme(scheme.Scheme)
			if tc.endpointSliceExport != nil {
				fakeHubClientBuilder = fakeHubClientBuilder.WithObjects(tc.endpointSliceExport)
			}
			fakeHubClient := fakeHubClientBuilder.Build()
			reconciler := &Reconciler{
				MemberClient: fakeMemberClient,
				HubClient:    fakeHubClient,
				HubNamespace: hubNSForMember,
			}

			if err := reconciler.unexportEndpointSlice(ctx, tc.endpointSlice); err != nil {
				t.Fatalf("unexportEndpointSlice(%+v), got %v, want no error", tc.endpointSlice, err)
			}

			updatedEndpointSlice := &discoveryv1.EndpointSlice{}
			updatedEndpointSliceKey := types.NamespacedName{
				Namespace: tc.endpointSlice.Namespace,
				Name:      tc.endpointSlice.Name,
			}
			if err := reconciler.MemberClient.Get(ctx, updatedEndpointSliceKey, updatedEndpointSlice); err != nil {
				t.Fatalf("Get(%+v), got %v, want no error", updatedEndpointSliceKey, err)
			}
			if _, ok := updatedEndpointSlice.Annotations[endpointSliceUniqueNameAnnotation]; ok {
				t.Fatalf("endpointSlice annotations, got %+v, want no %s annotation", updatedEndpointSlice.Annotations, endpointSliceUniqueNameAnnotation)
			}

			if tc.endpointSliceExport == nil {
				return
			}
			endpointSliceExportKey := types.NamespacedName{
				Namespace: tc.endpointSliceExport.Namespace,
				Name:      tc.endpointSliceExport.Name,
			}
			if err := reconciler.HubClient.Get(ctx, endpointSliceExportKey, tc.endpointSliceExport); !errors.IsNotFound(err) {
				t.Fatalf("endpointSliceExport Get(%+v), got %v, want not found error", tc.endpointSliceExport, err)
			}
		})
	}
}

// TestUnexportUnlinkedEndpointSlice tests the *Reconciler.unexportEndpointSlice and the
// *Reconciler.deleteEndpointSliceIfLinked method.
func TestUnexportUnlinkedEndpointSlice(t *testing.T) {
	testCases := []struct {
		name                string
		endpointSlice       *discoveryv1.EndpointSlice
		endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport
	}{
		{
			name: "should not unexport unlinked endpoint slice",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
					Annotations: map[string]string{
						endpointSliceUniqueNameAnnotation: endpointSliceUniqueName,
					},
					UID: "2",
				},
			},
			endpointSliceExport: &fleetnetv1alpha1.EndpointSliceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMember,
					Name:      endpointSliceUniqueName,
				},
				Spec: fleetnetv1alpha1.EndpointSliceExportSpec{
					EndpointSliceReference: fleetnetv1alpha1.ExportedObjectReference{
						ClusterID:       memberClusterID,
						Kind:            "EndpointSlice",
						Namespace:       memberUserNS,
						Name:            endpointSliceName,
						ResourceVersion: "0",
						Generation:      0,
						UID:             "3",
					},
				},
			},
		},
	}

	ctx := context.Background()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeMemberClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.endpointSlice).
				Build()
			fakeHubClient := fake.NewClientBuilder().
				WithObjects(tc.endpointSliceExport).
				WithScheme(scheme.Scheme).
				Build()
			reconciler := &Reconciler{
				MemberClient: fakeMemberClient,
				HubClient:    fakeHubClient,
				HubNamespace: hubNSForMember,
			}

			if err := reconciler.unexportEndpointSlice(ctx, tc.endpointSlice); err != nil {
				t.Fatalf("unexportEndpointSlice(%+v), got %v, want no error", tc.endpointSlice, err)
			}

			updatedEndpointSlice := &discoveryv1.EndpointSlice{}
			updatedEndpointSliceKey := types.NamespacedName{
				Namespace: tc.endpointSlice.Namespace,
				Name:      tc.endpointSlice.Name,
			}
			if err := reconciler.MemberClient.Get(ctx, updatedEndpointSliceKey, updatedEndpointSlice); err != nil {
				t.Fatalf("Get(%+v), got %v, want no error", updatedEndpointSliceKey, err)
			}
			if _, ok := updatedEndpointSlice.Annotations[endpointSliceUniqueNameAnnotation]; ok {
				t.Fatalf("endpointSlice annotations, got %+v, want no %s annotation", updatedEndpointSlice.Annotations, endpointSliceUniqueNameAnnotation)
			}

			endpointSliceExportKey := types.NamespacedName{
				Namespace: tc.endpointSliceExport.Namespace,
				Name:      tc.endpointSliceExport.Name,
			}
			if err := reconciler.HubClient.Get(ctx, endpointSliceExportKey, tc.endpointSliceExport); err != nil {
				t.Fatalf("endpointSliceExport Get(%+v), got %v, want no error", tc.endpointSliceExport, err)
			}
		})
	}
}

// TestAssignUniqueNameAsAnnotation tests the *Reconciler.assignUniqueNameAsAnnotation method.
func TestAssignUniqueNameAsAnnotation(t *testing.T) {
	testCases := []struct {
		name           string
		endpointSlice  *discoveryv1.EndpointSlice
		expectedPrefix string
	}{
		{
			name: "should assign unique name annotation",
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
			fakeMemberClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.endpointSlice).
				Build()
			fakeHubClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
			reconciler := &Reconciler{
				MemberClusterID: memberClusterID,
				MemberClient:    fakeMemberClient,
				HubClient:       fakeHubClient,
				HubNamespace:    hubNSForMember,
			}

			uniqueName, err := reconciler.assignUniqueNameAsAnnotation(ctx, tc.endpointSlice)
			if err != nil {
				t.Fatalf("assignUniqueNameAsAnnotation(%+v), got %v, want no error", tc.endpointSlice, err)
			}
			if !strings.HasPrefix(uniqueName, tc.expectedPrefix) {
				t.Fatalf("assignUniqueNameAsAnnotation(%+v) = %s, want prefix %s", tc.endpointSlice, uniqueName, tc.expectedPrefix)
			}

			var updatedEndpointSlice = discoveryv1.EndpointSlice{}
			updatedEndpointSliceKey := types.NamespacedName{Namespace: memberUserNS, Name: endpointSliceName}
			if err := fakeMemberClient.Get(ctx, updatedEndpointSliceKey, &updatedEndpointSlice); err != nil {
				t.Fatalf("endpointSlice Get(), got %v, want no error", err)
			}
			if setUniqueName := updatedEndpointSlice.Annotations[endpointSliceUniqueNameAnnotation]; !strings.HasPrefix(setUniqueName, tc.expectedPrefix) {
				t.Fatalf("unique name annotation, got %s, want %s", setUniqueName, tc.expectedPrefix)
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
					Annotations: map[string]string{
						endpointSliceUniqueNameAnnotation: endpointSliceUniqueName,
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
					},
					Annotations: map[string]string{
						endpointSliceUniqueNameAnnotation: endpointSliceUniqueName,
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
			fakeMemberClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.endpointSlice).
				Build()
			fakeHubClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
			reconciler := &Reconciler{
				MemberClient: fakeMemberClient,
				HubClient:    fakeHubClient,
				HubNamespace: hubNSForMember,
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
	deletionTimestamp := metav1.Now()

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
					},
					Annotations: map[string]string{
						endpointSliceUniqueNameAnnotation: endpointSliceUniqueName,
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
					},
					Annotations: map[string]string{
						endpointSliceUniqueNameAnnotation: endpointSliceUniqueName,
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
			name: "should unexport endpoint slice (svc export is deleted)",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
					Labels: map[string]string{
						discoveryv1.LabelServiceName: svcName,
					},
					Annotations: map[string]string{
						endpointSliceUniqueNameAnnotation: endpointSliceUniqueName,
					},
				},
				AddressType: discoveryv1.AddressTypeIPv4,
			},
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:         memberUserNS,
					Name:              svcName,
					DeletionTimestamp: &deletionTimestamp,
				},
				Status: fleetnetv1alpha1.ServiceExportStatus{
					Conditions: []metav1.Condition{
						serviceExportValidCondition(memberUserNS, svcName),
						serviceExportNoConflictCondition(memberClusterID, svcName),
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
		{
			name: "should skip endpoint slice (svc export is deleted)",
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
					Namespace:         memberUserNS,
					Name:              svcName,
					DeletionTimestamp: &deletionTimestamp,
				},
				Status: fleetnetv1alpha1.ServiceExportStatus{
					Conditions: []metav1.Condition{
						serviceExportValidCondition(memberUserNS, svcName),
						serviceExportNoConflictCondition(memberClusterID, svcName),
					},
				},
			},
			want: shouldSkipEndpointSliceOp,
		},
	}

	ctx := context.Background()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeMemberClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.endpointSlice, tc.svcExport).
				Build()
			fakeHubClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
			reconciler := &Reconciler{
				MemberClient: fakeMemberClient,
				HubClient:    fakeHubClient,
				HubNamespace: hubNSForMember,
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
					},
					Annotations: map[string]string{
						endpointSliceUniqueNameAnnotation: endpointSliceUniqueName,
					},
				},
				AddressType: discoveryv1.AddressTypeIPv4,
			},
			want: continueReconcileOp,
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
			want: continueReconcileOp,
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
					},
					Annotations: map[string]string{
						endpointSliceUniqueNameAnnotation: endpointSliceUniqueName,
					},
				},
				AddressType: discoveryv1.AddressTypeIPv4,
			},
			want: shouldUnexportEndpointSliceOp,
		},
		{
			name: "should skip endpoint slice (deleted)",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:         memberUserNS,
					Name:              endpointSliceName,
					DeletionTimestamp: &deletionTimestamp,
					Labels: map[string]string{
						discoveryv1.LabelServiceName: svcName,
					},
				},
				AddressType: discoveryv1.AddressTypeIPv4,
			},
			want: shouldSkipEndpointSliceOp,
		},
	}

	ctx := context.Background()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeMemberClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.endpointSlice, svcExport).
				Build()
			fakeHubClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
			reconciler := &Reconciler{
				MemberClient: fakeMemberClient,
				HubClient:    fakeHubClient,
				HubNamespace: hubNSForMember,
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
	deletionTimestamp := metav1.Now()

	testCases := []struct {
		name      string
		svcExport *fleetnetv1alpha1.ServiceExport
		want      bool
	}{
		{
			name: "svc export is valid with no conflicts",
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
			want: true,
		},
		{
			name: "svc export is invalid",
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
			want: false,
		},
		{
			name: "svc export is valid but with conflicts",
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
				Status: fleetnetv1alpha1.ServiceExportStatus{
					Conditions: []metav1.Condition{
						serviceExportValidCondition(memberUserNS, svcName),
						serviceExportConflictedCondition(memberUserNS, svcName),
					},
				},
			},
			want: false,
		},
		{
			name: "svc export has no conditions set yet",
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
			},
			want: false,
		},
		{
			name: "svc export is valid with no conflicts, but has been deleted",
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:         memberUserNS,
					Name:              svcName,
					DeletionTimestamp: &deletionTimestamp,
				},
				Status: fleetnetv1alpha1.ServiceExportStatus{
					Conditions: []metav1.Condition{
						serviceExportValidCondition(memberUserNS, svcName),
						serviceExportNoConflictCondition(memberUserNS, svcName),
					},
				},
			},
			want: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if res := isServiceExportValidWithNoConflict(tc.svcExport); res != tc.want {
				t.Fatalf("isServiceExportValidWithNoConflict(%+v) = %t, want %t", tc.svcExport, res, tc.want)
			}
		})
	}
}

// TestIsUniqueNameValid tests the isUniqueNameValid function.
func TestIsUniqueNameValid(t *testing.T) {
	testCases := []struct {
		name       string
		uniqueName string
		want       bool
	}{
		{
			name:       "is a valid unique name",
			uniqueName: fmt.Sprintf("%s-%s-%s-%s", memberClusterID, memberUserNS, endpointSliceName, "1x2yz"),
			want:       true,
		},
		{
			name:       "is not a valid unique name",
			uniqueName: "-*",
			want:       false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if res := isUniqueNameValid(tc.uniqueName); res != tc.want {
				t.Fatalf("isUniqueNameValid(%s) = %t, want %t", tc.uniqueName, res, tc.want)
			}
		})
	}
}

// TestIsEndpointSliceExportLinkedWithEndpointSlice tests the isEndpointSliceExportLinkedWithEndpointSlice function.
func TestIsEndpointSliceExportLinkedWithEndpointSlice(t *testing.T) {
	testCases := []struct {
		name                string
		endpointSlice       *discoveryv1.EndpointSlice
		endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport
		want                bool
	}{
		{
			name: "endpoint slice is linked with endpoint slice export",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
					UID:       "1",
				},
			},
			endpointSliceExport: &fleetnetv1alpha1.EndpointSliceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMember,
					Name:      endpointSliceName,
				},
				Spec: fleetnetv1alpha1.EndpointSliceExportSpec{
					EndpointSliceReference: fleetnetv1alpha1.ExportedObjectReference{
						UID: "1",
					},
				},
			},
			want: true,
		},
		{
			name: "endpoint slice is not linked with endpoint slice export",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
					UID:       "2",
				},
			},
			endpointSliceExport: &fleetnetv1alpha1.EndpointSliceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMember,
					Name:      endpointSliceName,
				},
				Spec: fleetnetv1alpha1.EndpointSliceExportSpec{
					EndpointSliceReference: fleetnetv1alpha1.ExportedObjectReference{
						UID: "3",
					},
				},
			},
			want: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if res := isEndpointSliceExportLinkedWithEndpointSlice(tc.endpointSliceExport, tc.endpointSlice); res != tc.want {
				t.Fatalf("isEndpointSliceExportLinkedWithEndpointSlice(%+v, %+v) = %t, want %t",
					tc.endpointSliceExport, tc.endpointSlice, res, tc.want)
			}
		})
	}
}
