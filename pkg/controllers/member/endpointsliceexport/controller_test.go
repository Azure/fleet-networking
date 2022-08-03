/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package endpointsliceexport

import (
	"context"
	"log"
	"os"
	"testing"

	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
)

const (
	memberUserNS            = "work"
	hubNSForMember          = "bravelion"
	endpointSliceName       = "app-endpointslice"
	endpointSliceExportName = "app-endpointsliceexport"
)

// TestMain bootstraps the test environment.
func TestMain(m *testing.M) {
	// Add custom APIs to the runtime scheme
	if err := fleetnetv1alpha1.AddToScheme(scheme.Scheme); err != nil {
		log.Fatalf("failed to add custom APIs to the runtime scheme: %v", err)
	}

	os.Exit(m.Run())
}

// TestIsEndpointSliceExportLinkedWithEndpointSlice tests the isEndpointSliceExportLinkedWithEndpointSlice function.
func TestIsEndpointSliceExportLinkedWithEndpointSlice(t *testing.T) {
	testCases := []struct {
		name                string
		endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport
		endpointSlice       *discoveryv1.EndpointSlice
		want                bool
	}{
		{
			name: "should confirm link",
			endpointSliceExport: &fleetnetv1alpha1.EndpointSliceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMember,
					Name:      endpointSliceExportName,
				},
			},
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
					Annotations: map[string]string{
						objectmeta.EndpointSliceUniqueNameAnnotation: endpointSliceExportName,
					},
				},
			},
			want: true,
		},
		{
			name: "should deny link (no unique name annotation)",
			endpointSliceExport: &fleetnetv1alpha1.EndpointSliceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMember,
					Name:      endpointSliceExportName,
				},
			},
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
				},
			},
			want: false,
		},
		{
			name: "should deny link (unique name does not match)",
			endpointSliceExport: &fleetnetv1alpha1.EndpointSliceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMember,
					Name:      endpointSliceExportName,
				},
			},
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      endpointSliceName,
					Annotations: map[string]string{
						objectmeta.EndpointSliceUniqueNameAnnotation: "app-endpointsliceexport-1",
					},
				},
			},
			want: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if res := isEndpointSliceExportLinkedWithEndpointSlice(tc.endpointSliceExport, tc.endpointSlice); res != tc.want {
				t.Fatalf("isEndpointSliceExportLinkedWithEndpointSlice(%+v, %+v), got %t, want %t",
					tc.endpointSliceExport,
					tc.endpointSlice,
					res,
					tc.want,
				)
			}
		})
	}
}

// TestDeleteEndpointSliceExport tests the *Reconciler.deleteEndpointSliceExport method.
func TestDeleteEndpointSliceExport(t *testing.T) {
	testCases := []struct {
		name                string
		endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport
	}{
		{
			name: "should delete endpoint slice export",
			endpointSliceExport: &fleetnetv1alpha1.EndpointSliceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMember,
					Name:      endpointSliceExportName,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeMemberClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
			fakeHubClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.endpointSliceExport).
				Build()
			reconciler := &Reconciler{
				MemberClient: fakeMemberClient,
				HubClient:    fakeHubClient,
			}
			ctx := context.Background()

			if _, err := reconciler.deleteEndpointSliceExport(ctx, tc.endpointSliceExport); err != nil {
				t.Fatalf("deleteEndpointSliceExport(%+v), got %v, want no error", tc.endpointSliceExport, err)
			}

			endpointSliceExport := &fleetnetv1alpha1.EndpointSliceExport{}
			endpointSliceExportKey := types.NamespacedName{Namespace: hubNSForMember, Name: endpointSliceExportName}
			if err := fakeHubClient.Get(ctx, endpointSliceExportKey, endpointSliceExport); err != nil && !errors.IsNotFound(err) {
				t.Fatalf("endpoint slice export Get(%+v), got %v, want not found error", endpointSliceExportKey, err)
			}
		})
	}
}
