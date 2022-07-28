/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package endpointsliceexport

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

const (
	hubNSForMemberA         = "bravelion"
	hubNSForMemberB         = "highflyingcat"
	clusterIDForMemberB     = "1"
	hubNSForMemberC         = "singingbutterfly"
	clusterIDForMemberC     = "2"
	memberUserNS            = "work"
	fleetSystemNS           = "fleet-system"
	svcName                 = "app"
	endpointSliceName       = "app-endpointslice"
	endpointSliceExportName = "work-app-endpointslice-1a2bc"
	ipAddr                  = "1.2.3.4"
	altIPAddr               = "2.3.4.5"
)

var (
	endpointSliceKey       = types.NamespacedName{Namespace: fleetSystemNS, Name: endpointSliceExportName}
	endpointSliceExportKey = types.NamespacedName{Namespace: hubNSForMemberA, Name: endpointSliceExportName}

	httpPortName        = "http"
	httpPort            = int32(80)
	httpPortProtocol    = corev1.ProtocolTCP
	httpPortAppProtocol = "www"
	udpPortName         = "udp"
	udpPort             = int32(81)
	udpPortProtocol     = corev1.ProtocolUDP
	udpPortAppProtocol  = "example.com/custom"

	ignoredObjectMetaFields = cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion")
)

// ipv4EndpointSliceExport returns an IPv4 EndpointSliceExport.
func ipv4EndpointSliceExport() *fleetnetv1alpha1.EndpointSliceExport {
	return &fleetnetv1alpha1.EndpointSliceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  hubNSForMemberA,
			Name:       endpointSliceExportName,
			Finalizers: []string{endpointSliceExportCleanupFinalizer},
		},
		Spec: fleetnetv1alpha1.EndpointSliceExportSpec{
			AddressType: discoveryv1.AddressTypeIPv4,
			Endpoints: []fleetnetv1alpha1.Endpoint{
				{
					Addresses: []string{ipAddr},
				},
				{
					Addresses: []string{altIPAddr},
				},
			},
			Ports: []discoveryv1.EndpointPort{
				{
					Name:        &httpPortName,
					Protocol:    &httpPortProtocol,
					Port:        &httpPort,
					AppProtocol: &httpPortAppProtocol,
				},
				{
					Name:        &udpPortName,
					Protocol:    &udpPortProtocol,
					Port:        &udpPort,
					AppProtocol: &udpPortAppProtocol,
				},
			},
			EndpointSliceReference: fleetnetv1alpha1.ExportedObjectReference{
				ClusterID:       hubNSForMemberA,
				Kind:            "EndpointSlice",
				Namespace:       memberUserNS,
				Name:            endpointSliceName,
				ResourceVersion: "0",
				Generation:      1,
				UID:             "00000000-0000-0000-0000-000000000000",
			},
			OwnerServiceReference: fleetnetv1alpha1.OwnerServiceReference{
				Namespace:      memberUserNS,
				Name:           svcName,
				NamespacedName: fmt.Sprintf("%s/%s", memberUserNS, svcName),
			},
		},
	}
}

// ipv4EndpointSlice returns an IPv4 EndpointSlice.
func ipv4EndpointSlice() *discoveryv1.EndpointSlice {
	return &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: fleetSystemNS,
			Name:      endpointSliceExportName,
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints: []discoveryv1.Endpoint{
			{
				Addresses: []string{ipAddr},
			},
			{
				Addresses: []string{altIPAddr},
			},
		},
		Ports: []discoveryv1.EndpointPort{
			{
				Name:        &httpPortName,
				Protocol:    &httpPortProtocol,
				Port:        &httpPort,
				AppProtocol: &httpPortAppProtocol,
			},
			{
				Name:        &udpPortName,
				Protocol:    &udpPortProtocol,
				Port:        &udpPort,
				AppProtocol: &udpPortAppProtocol,
			},
		},
	}
}

// TestMain bootstraps the test environment.
func TestMain(m *testing.M) {
	// Add custom APIs to the runtime scheme.
	if err := fleetnetv1alpha1.AddToScheme(scheme.Scheme); err != nil {
		log.Fatalf("failed to add custom APIs to the runtime scheme: %v", err)
	}

	os.Exit(m.Run())
}

// TestFormatEndpointSliceFromExport tests the formatEndpointSliceFromExport function.
func TestFormatEndpointSliceFromExport(t *testing.T) {
	testCases := []struct {
		name                string
		endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport
		endpointSlice       *discoveryv1.EndpointSlice
	}{
		{
			name:                "should format endpointslice",
			endpointSliceExport: ipv4EndpointSliceExport(),
			endpointSlice:       ipv4EndpointSlice(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			endpointSlice := &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: fleetSystemNS,
					Name:      endpointSliceExportName,
				},
			}
			formatEndpointSliceFromExport(endpointSlice, tc.endpointSliceExport)

			if diff := cmp.Diff(endpointSlice, tc.endpointSlice); diff != "" {
				t.Fatalf("formatEndpointSliceFromExport(), got diff %s", diff)
			}
		})
	}
}

// TestWithdrawAllEndpointSliceImports tests the Reconciler.withdrawAllEndpointSliceImports method.
func TestWithdrawAllEndpointSliceImports(t *testing.T) {
	endpointSliceExportSpec := ipv4EndpointSliceExport().Spec

	testCases := []struct {
		name                 string
		endpointSliceExport  *fleetnetv1alpha1.EndpointSliceExport
		endpointSliceImports []*fleetnetv1alpha1.EndpointSliceImport
	}{
		{
			name:                "should withdraw all endpointslices distributed",
			endpointSliceExport: ipv4EndpointSliceExport(),
			endpointSliceImports: []*fleetnetv1alpha1.EndpointSliceImport{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: hubNSForMemberB,
						Name:      endpointSliceExportName,
					},
					Spec: *endpointSliceExportSpec.DeepCopy(),
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: hubNSForMemberC,
						Name:      endpointSliceExportName,
					},
					Spec: *endpointSliceExportSpec.DeepCopy(),
				},
			},
		},
		{
			name:                "should withdraw all endpointslices distributed (no distributed endpointslices)",
			endpointSliceExport: ipv4EndpointSliceExport(),
		},
	}

	ctx := context.Background()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeHubClientBuilder := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.endpointSliceExport)
			for idx := range tc.endpointSliceImports {
				fakeHubClientBuilder = fakeHubClientBuilder.WithObjects(tc.endpointSliceImports[idx])
			}
			fakeHubClient := fakeHubClientBuilder.Build()
			reconciler := Reconciler{
				HubClient:            fakeHubClient,
				FleetSystemNamespace: fleetSystemNS,
			}

			if err := reconciler.withdrawAllEndpointSliceImports(ctx, tc.endpointSliceExport); err != nil {
				t.Fatalf("withdrawAllEndpointSliceImports(%+v), got %v, want no error", tc.endpointSliceExport, err)
			}

			endpointSliceImportList := &fleetnetv1alpha1.EndpointSliceImportList{}
			if err := fakeHubClient.List(ctx, endpointSliceImportList); err != nil {
				t.Fatalf("endpointSliceImport List(), got %v, want no error", err)
			}

			if len(endpointSliceImportList.Items) != 0 {
				t.Fatalf("endpointSliceImportList.Items, got %+v, want empty list", endpointSliceImportList.Items)
			}
		})
	}
}

// TestRemoveHubEndpointSliceCopyAndCleanupFinalizer tests the Reconciler.removeHubEndpointSliceCopyAndCleanupFinalizer method.
func TestRemoveHubEndpointSliceCopyAndCleanupFinalizer(t *testing.T) {
	testCases := []struct {
		name                string
		endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport
		endpointSlice       *discoveryv1.EndpointSlice
	}{
		{
			name:                "should remove endpointslice copy in the hub and cleanup finalizer on endpointsliceexport",
			endpointSliceExport: ipv4EndpointSliceExport(),
			endpointSlice:       ipv4EndpointSlice(),
		},
		{
			name:                "should remove cleanup finalizer on endpointsliceexport (no endpointslice copy)",
			endpointSliceExport: ipv4EndpointSliceExport(),
		},
	}

	ctx := context.Background()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeHubClientBuilder := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.endpointSliceExport)
			if tc.endpointSlice != nil {
				fakeHubClientBuilder.WithObjects(tc.endpointSlice)
			}
			fakeHubClient := fakeHubClientBuilder.Build()
			reconciler := Reconciler{
				HubClient:            fakeHubClient,
				FleetSystemNamespace: fleetSystemNS,
			}

			if err := reconciler.removeHubEndpointSliceCopyAndCleanupFinalizer(ctx, tc.endpointSliceExport); err != nil {
				t.Fatalf("removeHubEndpointSliceCopyAndCleanupFinalizer(%+v), got %v, want no error", tc.endpointSliceExport, err)
			}

			endpointSlice := &discoveryv1.EndpointSlice{}
			if err := fakeHubClient.Get(ctx, endpointSliceKey, endpointSlice); !errors.IsNotFound(err) {
				t.Fatalf("endpointSlice Get(%+v), got %v, want no error", endpointSliceKey, err)
			}

			endpointSliceExport := &fleetnetv1alpha1.EndpointSliceExport{}
			if err := fakeHubClient.Get(ctx, endpointSliceExportKey, endpointSliceExport); err != nil {
				t.Fatalf("endpointSliceExport Get(%+v), got %v, want no error", endpointSliceExportKey, err)
			}

			if len(endpointSliceExport.Finalizers) != 0 {
				t.Fatalf("endpointSliceExport finalizers, got %v, want no finalizers", endpointSliceExport.Finalizers)
			}
		})
	}
}

// TestRemoveEndpointSliceExportCleanupFinalizer tests the Reconciler.removeEndpointSliceExportCleanupFinalizer method.
func TestRemoveEndpointSliceExportCleanupFinalizer(t *testing.T) {
	testCases := []struct {
		name                string
		endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport
	}{
		{
			name:                "should remove cleanup finalizer",
			endpointSliceExport: ipv4EndpointSliceExport(),
		},
	}

	ctx := context.Background()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeHubClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.endpointSliceExport).
				Build()
			reconciler := Reconciler{
				HubClient:            fakeHubClient,
				FleetSystemNamespace: fleetSystemNS,
			}

			if err := reconciler.removeEndpointSliceExportCleanupFinalizer(ctx, tc.endpointSliceExport); err != nil {
				t.Fatalf("removeEndpointSliceExportCleanupFinalizer(%+v), got %v, want no error", tc.endpointSliceExport, err)
			}

			endpointSliceExport := &fleetnetv1alpha1.EndpointSliceExport{}
			if err := fakeHubClient.Get(ctx, endpointSliceExportKey, endpointSliceExport); err != nil {
				t.Fatalf("endpointSliceExport Get(%+v), got %v, want no error", endpointSliceExportKey, err)
			}

			if len(endpointSliceExport.Finalizers) != 0 {
				t.Fatalf("endpointSliceExport finalizers, got %+v, want empty list", endpointSliceExport.Finalizers)
			}
		})
	}
}

// TestAddEndpointSliceExportCleanupFinalizer tests the Reconciler.addEndpointSliceExportCleanupFinalizer method.
func TestAddEndpointSliceExportCleanupFinalizer(t *testing.T) {
	endpointSliceExport := ipv4EndpointSliceExport()
	endpointSliceExport.Finalizers = []string{}

	testCases := []struct {
		name                string
		endpointSliceExport *fleetnetv1alpha1.EndpointSliceExport
	}{}

	ctx := context.Background()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeHubClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.endpointSliceExport).
				Build()
			reconciler := Reconciler{
				HubClient:            fakeHubClient,
				FleetSystemNamespace: fleetSystemNS,
			}

			if err := reconciler.addEndpointSliceExportCleanupFinalizer(ctx, tc.endpointSliceExport); err != nil {
				t.Fatalf("addEndpointSliceExportCleanupFinalizer(%+v), got %v, want no error", tc.endpointSliceExport, err)
			}

			endpointSliceExport := &fleetnetv1alpha1.EndpointSliceExport{}
			if err := fakeHubClient.Get(ctx, endpointSliceExportKey, endpointSliceExport); err != nil {
				t.Fatalf("endpointSliceExport Get(%+v), got %v, want no error", endpointSliceExportKey, err)
			}

			if !cmp.Equal(endpointSliceExport.Finalizers, []string{endpointSliceExportCleanupFinalizer}) {
				t.Fatalf("endpointSliceExport finalizers, got %+v, want %+v",
					endpointSliceExport.Finalizers, []string{endpointSliceExportCleanupFinalizer})
			}
		})
	}
}

// TestScanForEndpointSliceImports tests the Reconciler.scanForEndpointSliceImports method.
func TestScanForEndpointSliceImports(t *testing.T) {
	endpointSliceExportSpec := ipv4EndpointSliceExport().Spec

	testCases := []struct {
		name                 string
		endpointSliceExport  *fleetnetv1alpha1.EndpointSliceExport
		svcInUseBy           *fleetnetv1alpha1.ServiceInUseBy
		endpointSliceImports []*fleetnetv1alpha1.EndpointSliceImport
		wantToWithdraw       []*fleetnetv1alpha1.EndpointSliceImport
		wantToCreateOrUpdate []*fleetnetv1alpha1.EndpointSliceImport
	}{
		{
			name:                "should withdraw endpointsliceimports",
			endpointSliceExport: ipv4EndpointSliceExport(),
			svcInUseBy:          &fleetnetv1alpha1.ServiceInUseBy{},
			endpointSliceImports: []*fleetnetv1alpha1.EndpointSliceImport{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: hubNSForMemberB,
						Name:      endpointSliceExportName,
					},
					Spec: *endpointSliceExportSpec.DeepCopy(),
				},
			},
			wantToWithdraw: []*fleetnetv1alpha1.EndpointSliceImport{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: hubNSForMemberB,
						Name:      endpointSliceExportName,
					},
					Spec: *endpointSliceExportSpec.DeepCopy(),
				},
			},
		},
		{
			name:                "should update endpointsliceimports",
			endpointSliceExport: ipv4EndpointSliceExport(),
			svcInUseBy: &fleetnetv1alpha1.ServiceInUseBy{
				MemberClusters: map[fleetnetv1alpha1.ClusterNamespace]fleetnetv1alpha1.ClusterID{
					hubNSForMemberB: clusterIDForMemberB,
				},
			},
			endpointSliceImports: []*fleetnetv1alpha1.EndpointSliceImport{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: hubNSForMemberB,
						Name:      endpointSliceExportName,
					},
					Spec: *endpointSliceExportSpec.DeepCopy(),
				},
			},
			wantToCreateOrUpdate: []*fleetnetv1alpha1.EndpointSliceImport{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: hubNSForMemberB,
						Name:      endpointSliceExportName,
					},
					Spec: *endpointSliceExportSpec.DeepCopy(),
				},
			},
		},
		{
			name:                "should create endpointsliceimports",
			endpointSliceExport: ipv4EndpointSliceExport(),
			svcInUseBy: &fleetnetv1alpha1.ServiceInUseBy{
				MemberClusters: map[fleetnetv1alpha1.ClusterNamespace]fleetnetv1alpha1.ClusterID{
					hubNSForMemberB: clusterIDForMemberB,
				},
			},
			endpointSliceImports: []*fleetnetv1alpha1.EndpointSliceImport{},
			wantToCreateOrUpdate: []*fleetnetv1alpha1.EndpointSliceImport{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: hubNSForMemberB,
						Name:      endpointSliceExportName,
					},
				},
			},
		},
		{
			name:                "should delete, create and update endpointsliceimports",
			endpointSliceExport: ipv4EndpointSliceExport(),
			svcInUseBy: &fleetnetv1alpha1.ServiceInUseBy{
				MemberClusters: map[fleetnetv1alpha1.ClusterNamespace]fleetnetv1alpha1.ClusterID{
					hubNSForMemberB: clusterIDForMemberB,
					hubNSForMemberC: clusterIDForMemberC,
				},
			},
			endpointSliceImports: []*fleetnetv1alpha1.EndpointSliceImport{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: hubNSForMemberA,
						Name:      endpointSliceExportName,
					},
					Spec: *endpointSliceExportSpec.DeepCopy(),
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: hubNSForMemberB,
						Name:      endpointSliceExportName,
					},
					Spec: *endpointSliceExportSpec.DeepCopy(),
				},
			},
			wantToCreateOrUpdate: []*fleetnetv1alpha1.EndpointSliceImport{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: hubNSForMemberB,
						Name:      endpointSliceExportName,
					},
					Spec: *endpointSliceExportSpec.DeepCopy(),
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: hubNSForMemberC,
						Name:      endpointSliceExportName,
					},
				},
			},
			wantToWithdraw: []*fleetnetv1alpha1.EndpointSliceImport{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: hubNSForMemberA,
						Name:      endpointSliceExportName,
					},
					Spec: *endpointSliceExportSpec.DeepCopy(),
				},
			},
		},
	}

	ctx := context.Background()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeHubClientBuilder := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.endpointSliceExport)
			for idx := range tc.endpointSliceImports {
				fakeHubClientBuilder = fakeHubClientBuilder.WithObjects(tc.endpointSliceImports[idx])
			}
			fakeHubClient := fakeHubClientBuilder.Build()
			reconciler := Reconciler{
				HubClient:            fakeHubClient,
				FleetSystemNamespace: fleetSystemNS,
			}

			toWithdraw, toCreateOrUpdate, err := reconciler.scanForEndpointSliceImports(ctx, tc.endpointSliceExport, tc.svcInUseBy)
			if err != nil {
				t.Fatalf("scanForEndpointSliceImports(%+v, %v), got %v, want no error", tc.endpointSliceExport, tc.svcInUseBy, err)
			}

			if diff := cmp.Diff(toWithdraw, tc.wantToWithdraw, ignoredObjectMetaFields); diff != "" {
				t.Fatalf("endpointSliceImportsToWithdraw, got diff %s", diff)
			}

			if diff := cmp.Diff(toCreateOrUpdate, tc.wantToCreateOrUpdate, ignoredObjectMetaFields); diff != "" {
				t.Fatalf("endpointSliceImportsToCreateOrUpdate, got diff %s", diff)
			}
		})
	}
}
