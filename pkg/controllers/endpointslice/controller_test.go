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
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetnetworkingapi "go.goms.io/fleet-networking/api/v1alpha1"
)

const (
	memberClusterID      = "bravelion"
	memberUserNS         = "work"
	hubNSForMember       = "bravelion"
	endpointSliceName    = "app-endpointslice"
	altEndpointSliceName = "app-endpointslice-2"
)

func randomLengthString(n int) string {
	alphabet := []rune("abcdefghijklmnopqrstuvwxyz")
	b := make([]rune, n)
	for i := range b {
		b[i] = alphabet[rand.Intn(len(alphabet))] //nolint:gosec
	}
	return string(b)
}

func TestMain(m *testing.M) {
	// Add custom APIs to the runtime scheme
	err := fleetnetworkingapi.AddToScheme(scheme.Scheme)
	if err != nil {
		log.Fatalf("failed to add custom APIs to the runtime scheme: %v", err)
	}

	os.Exit(m.Run())
}

// TestIsEndpointSliceExportable tests the isEndpointSliceExportable function.
func TestIsEndpointSliceExportable(t *testing.T) {
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
			want: true,
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
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if res := isEndpointSliceExportable(tc.endpointSlice); res != tc.want {
				t.Fatalf("isEndpointSliceExport(%+v) = %t, want %t", tc.endpointSlice, res, tc.want)
			}
		})
	}
}

// TestIsEndpointSliceCleanupNeeded tests the isEndpointSliceCleanupNeeded function.
func TestIsEndpointSliceCleanupNeeded(t *testing.T) {
	deletionTimestamp := metav1.Now()
	testCases := []struct {
		name          string
		endpointSlice *discoveryv1.EndpointSlice
		want          bool
	}{
		{
			name: "should need cleanup",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
					Labels: map[string]string{
						endpointSliceUniqueNameLabel: fmt.Sprintf("%s-%s-%s", memberClusterID, memberUserNS, endpointSliceName),
					},
					DeletionTimestamp: &deletionTimestamp,
				},
			},
			want: true,
		},
		{
			name: "should not need cleanup (no unique name label)",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
				},
			},
		},
		{
			name: "should not need cleanup (no deletion timestamp)",
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:         memberUserNS,
					Name:              endpointSliceName,
					DeletionTimestamp: &deletionTimestamp,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if res := isEndpointSliceCleanupNeeded(tc.endpointSlice); res != tc.want {
				t.Fatalf("isEndpointSliceCleanupNeeded(%+v) = %t, want %t", tc.endpointSlice, res, tc.want)
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
		expectedEndpoints []fleetnetworkingapi.Endpoint
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
			expectedEndpoints: []fleetnetworkingapi.Endpoint{
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
	endpointSliceExport := &fleetnetworkingapi.EndpointSliceExport{
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

	fakeMemberClient := fakeclient.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(endpointSlice, unexportedEndpointSlice).
		Build()
	fakeHubClient := fakeclient.NewClientBuilder().
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

	fakeMemberClient := fakeclient.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(endpointSlice).
		Build()
	fakeHubClient := fakeclient.NewClientBuilder().
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
