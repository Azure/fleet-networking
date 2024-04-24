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
	"time"

	"github.com/google/go-cmp/cmp"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/metrics"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/common/uniquename"
)

const (
	memberClusterID                = "bravelion"
	memberUserNS                   = "work"
	hubNSForMember                 = "bravelion"
	svcName                        = "app"
	endpointSliceName              = "app-endpointslice"
	endpointSliceUniqueName        = "bravelion-work-app-endpointslice"
	endpointSliceGeneration        = 1
	customDeletionBlockerFinalizer = "custom-deletion-finalizer"
)

var (
	endpointSliceKey       = types.NamespacedName{Namespace: memberUserNS, Name: endpointSliceName}
	endpointSliceExportKey = types.NamespacedName{Namespace: hubNSForMember, Name: endpointSliceUniqueName}
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
						objectmeta.ExportedObjectAnnotationUniqueName: endpointSliceUniqueName,
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
						objectmeta.ExportedObjectAnnotationUniqueName: fmt.Sprintf("%s-%s-%s-%s",
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
			if err := reconciler.MemberClient.Get(ctx, endpointSliceKey, updatedEndpointSlice); err != nil {
				t.Fatalf("Get(%+v), got %v, want no error", endpointSliceKey, err)
			}
			if _, ok := updatedEndpointSlice.Annotations[objectmeta.ExportedObjectAnnotationUniqueName]; ok {
				t.Fatalf("endpointSlice annotations, got %+v, want no %s annotation", updatedEndpointSlice.Annotations, objectmeta.ExportedObjectAnnotationUniqueName)
			}

			if tc.endpointSliceExport == nil {
				return
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
						objectmeta.ExportedObjectAnnotationUniqueName: endpointSliceUniqueName,
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
			if err := reconciler.MemberClient.Get(ctx, endpointSliceKey, updatedEndpointSlice); err != nil {
				t.Fatalf("Get(%+v), got %v, want no error", endpointSliceKey, err)
			}
			if _, ok := updatedEndpointSlice.Annotations[objectmeta.ExportedObjectAnnotationUniqueName]; ok {
				t.Fatalf("endpointSlice annotations, got %+v, want no %s annotation", updatedEndpointSlice.Annotations, objectmeta.ExportedObjectAnnotationUniqueName)
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
			if err := fakeMemberClient.Get(ctx, endpointSliceKey, &updatedEndpointSlice); err != nil {
				t.Fatalf("endpointSlice Get(), got %v, want no error", err)
			}
			if setUniqueName := updatedEndpointSlice.Annotations[objectmeta.ExportedObjectAnnotationUniqueName]; !strings.HasPrefix(setUniqueName, tc.expectedPrefix) {
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
						objectmeta.ExportedObjectAnnotationUniqueName: endpointSliceUniqueName,
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
						objectmeta.ExportedObjectAnnotationUniqueName: endpointSliceUniqueName,
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
						objectmeta.ExportedObjectAnnotationUniqueName: endpointSliceUniqueName,
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
						objectmeta.ExportedObjectAnnotationUniqueName: endpointSliceUniqueName,
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
						objectmeta.ExportedObjectAnnotationUniqueName: endpointSliceUniqueName,
					},
				},
				AddressType: discoveryv1.AddressTypeIPv4,
			},
			svcExport: &fleetnetv1alpha1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:         memberUserNS,
					Name:              svcName,
					DeletionTimestamp: &deletionTimestamp,
					// Note that fake client will reject object that is deleted (has the deletion
					// timestamp) but does not have finalizers.
					Finalizers: []string{
						customDeletionBlockerFinalizer,
					},
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
					Finalizers: []string{
						// Note that fake client will reject objects that is deleted (has the
						// deletion timestamp) but does not have a finalizer.
						customDeletionBlockerFinalizer,
					},
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
				WithStatusSubresource(tc.endpointSlice, tc.svcExport).
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
						objectmeta.ExportedObjectAnnotationUniqueName: endpointSliceUniqueName,
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
					// Note that fake client will reject object that is deleted (has the deletion
					// timestamp) but does not have finalizers.
					Finalizers: []string{
						customDeletionBlockerFinalizer,
					},
					Labels: map[string]string{
						discoveryv1.LabelServiceName: svcName,
					},
					Annotations: map[string]string{
						objectmeta.ExportedObjectAnnotationUniqueName: endpointSliceUniqueName,
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
					// Note that fake client will reject object that is deleted (has the deletion
					// timestamp) but does not have finalizers.
					Finalizers: []string{
						customDeletionBlockerFinalizer,
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
				WithStatusSubresource(tc.endpointSlice, svcExport).
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

// TestAnnotateLastSeenGenerationAndTimestamp tests the annotateLastSeenGenerationAndTimestamp function.
func TestAnnotateLastSeenGenerationAndTimestamp(t *testing.T) {
	startTime := time.Now()

	testCases := []struct {
		name            string
		endpointSlice   *discoveryv1.EndpointSlice
		startTime       time.Time
		wantAnnotations map[string]string
	}{
		{
			name: "endpointslice with no last seen annotations",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  memberUserNS,
					Name:       endpointSliceName,
					Generation: endpointSliceGeneration,
					Annotations: map[string]string{
						objectmeta.ExportedObjectAnnotationUniqueName: endpointSliceUniqueName,
					},
				},
			},
			startTime: startTime,
			wantAnnotations: map[string]string{
				objectmeta.ExportedObjectAnnotationUniqueName: endpointSliceUniqueName,
				metrics.MetricsAnnotationLastSeenGeneration:   fmt.Sprintf("%d", endpointSliceGeneration),
				metrics.MetricsAnnotationLastSeenTimestamp:    startTime.Format(metrics.MetricsLastSeenTimestampFormat),
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
			fakeHubClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
			reconciler := &Reconciler{
				MemberClient: fakeMemberClient,
				HubClient:    fakeHubClient,
				HubNamespace: hubNSForMember,
			}

			if err := reconciler.annotateLastSeenGenerationAndTimestamp(ctx, tc.endpointSlice, tc.startTime); err != nil {
				t.Fatalf("annotateLastSeenGenerationAndTimestamp(%+v, %v), got %v, want no error", tc.endpointSlice, tc.startTime, err)
			}

			updatedEndpointSlice := &discoveryv1.EndpointSlice{}
			if err := fakeMemberClient.Get(ctx, endpointSliceKey, updatedEndpointSlice); err != nil {
				t.Fatalf("endpointSlice Get(%+v), got %v, want no error", endpointSliceKey, err)
			}

			if diff := cmp.Diff(updatedEndpointSlice.Annotations, tc.wantAnnotations); diff != "" {
				t.Fatalf("endpointSlice annotations (-got, +want): %s", diff)
			}
		})
	}
}

// TestCollectAndVerifyLastSeenGenerationAndTimestamp tests the collectAndVerifyLastSeenGenerationAndTimestamp function.
func TestCollectAndVerifyLastSeenGenerationAndTimestamp(t *testing.T) {
	startTime := time.Now()
	startTimeBefore := startTime.Add(-time.Second * 5)
	startTimeBeforeStr := startTimeBefore.Format(metrics.MetricsLastSeenTimestampFormat)
	startTimeBeforeFlattened, _ := time.Parse(metrics.MetricsLastSeenTimestampFormat, startTimeBeforeStr)
	startTimeAfter := startTime.Add(time.Second * 240)
	wantAnnotations := map[string]string{
		objectmeta.ExportedObjectAnnotationUniqueName: endpointSliceUniqueName,
		metrics.MetricsAnnotationLastSeenGeneration:   fmt.Sprintf("%d", endpointSliceGeneration),
		metrics.MetricsAnnotationLastSeenTimestamp:    startTime.Format(metrics.MetricsLastSeenTimestampFormat),
	}

	testCases := []struct {
		name              string
		endpointSlice     *discoveryv1.EndpointSlice
		startTime         time.Time
		wantExportedSince time.Time
		wantAnnotations   map[string]string
	}{
		{
			name: "endpointslice with no last seen annotations",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  memberUserNS,
					Name:       endpointSliceName,
					Generation: endpointSliceGeneration,
					Annotations: map[string]string{
						objectmeta.ExportedObjectAnnotationUniqueName: endpointSliceUniqueName,
					},
				},
			},
			startTime:         startTime,
			wantExportedSince: startTime,
			wantAnnotations:   wantAnnotations,
		},
		{
			name: "endpointslice with valid last seen annotations",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  memberUserNS,
					Name:       endpointSliceName,
					Generation: endpointSliceGeneration,
					Annotations: map[string]string{
						objectmeta.ExportedObjectAnnotationUniqueName: endpointSliceUniqueName,
						metrics.MetricsAnnotationLastSeenGeneration:   fmt.Sprintf("%d", endpointSliceGeneration),
						metrics.MetricsAnnotationLastSeenTimestamp:    startTimeBefore.Format(metrics.MetricsLastSeenTimestampFormat),
					},
				},
			},
			startTime:         startTime,
			wantExportedSince: startTimeBeforeFlattened,
			wantAnnotations: map[string]string{
				objectmeta.ExportedObjectAnnotationUniqueName: endpointSliceUniqueName,
				metrics.MetricsAnnotationLastSeenGeneration:   fmt.Sprintf("%d", endpointSliceGeneration),
				metrics.MetricsAnnotationLastSeenTimestamp:    startTimeBefore.Format(metrics.MetricsLastSeenTimestampFormat),
			},
		},
		{
			name: "endpointslice with invalid last seen generation (bad data)",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  memberUserNS,
					Name:       endpointSliceName,
					Generation: endpointSliceGeneration,
					Annotations: map[string]string{
						objectmeta.ExportedObjectAnnotationUniqueName: endpointSliceUniqueName,
						metrics.MetricsAnnotationLastSeenGeneration:   "InvalidGenerationData",
						metrics.MetricsAnnotationLastSeenTimestamp:    startTimeBefore.Format(metrics.MetricsLastSeenTimestampFormat),
					},
				},
			},
			startTime:         startTime,
			wantExportedSince: startTime,
			wantAnnotations:   wantAnnotations,
		},
		{
			name: "endpointslice with invalid last seen generation (mismatch)",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  memberUserNS,
					Name:       endpointSliceName,
					Generation: endpointSliceGeneration,
					Annotations: map[string]string{
						objectmeta.ExportedObjectAnnotationUniqueName: endpointSliceUniqueName,
						metrics.MetricsAnnotationLastSeenGeneration:   fmt.Sprintf("%d", endpointSliceGeneration+1),
						metrics.MetricsAnnotationLastSeenTimestamp:    startTimeBefore.Format(metrics.MetricsLastSeenTimestampFormat),
					},
				},
			},
			startTime:         startTime,
			wantExportedSince: startTime,
			wantAnnotations:   wantAnnotations,
		},
		{
			name: "endpointslice with invalid last seen timestamp (bad data)",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  memberUserNS,
					Name:       endpointSliceName,
					Generation: endpointSliceGeneration,
					Annotations: map[string]string{
						objectmeta.ExportedObjectAnnotationUniqueName: endpointSliceUniqueName,
						metrics.MetricsAnnotationLastSeenGeneration:   fmt.Sprintf("%d", endpointSliceGeneration),
						metrics.MetricsAnnotationLastSeenTimestamp:    "InvalidTimestampData",
					},
				},
			},
			startTime:         startTime,
			wantExportedSince: startTime,
			wantAnnotations:   wantAnnotations,
		},
		{
			name: "endpointslice with invalid last seen timestamp (too late timestamp)",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  memberUserNS,
					Name:       endpointSliceName,
					Generation: endpointSliceGeneration,
					Annotations: map[string]string{
						objectmeta.ExportedObjectAnnotationUniqueName: endpointSliceUniqueName,
						metrics.MetricsAnnotationLastSeenGeneration:   fmt.Sprintf("%d", endpointSliceGeneration),
						metrics.MetricsAnnotationLastSeenTimestamp:    startTimeAfter.Format(metrics.MetricsLastSeenTimestampFormat),
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
				WithObjects(tc.endpointSlice).
				Build()
			fakeHubClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
			reconciler := &Reconciler{
				MemberClient: fakeMemberClient,
				HubClient:    fakeHubClient,
				HubNamespace: hubNSForMember,
			}

			exportedSince, err := reconciler.collectAndVerifyLastSeenGenerationAndTimestamp(ctx, tc.endpointSlice, tc.startTime)
			if err != nil || !exportedSince.Equal(tc.wantExportedSince) {
				t.Fatalf("collectAndVerifyLastSeenGenerationAndTimestamp(%+v, %v) = (%v, %v), want (%v, %v)",
					tc.endpointSlice, tc.startTime, exportedSince, err, tc.wantExportedSince, nil)
			}

			updatedEndpointSlice := &discoveryv1.EndpointSlice{}
			if err := fakeMemberClient.Get(ctx, endpointSliceKey, updatedEndpointSlice); err != nil {
				t.Fatalf("endpointSlice Get(%+v), got %v, want no error", endpointSliceKey, err)
			}

			if diff := cmp.Diff(updatedEndpointSlice.Annotations, tc.wantAnnotations); diff != "" {
				t.Fatalf("endpointSlice annotations (-got, +want): %s", diff)
			}
		})
	}
}
