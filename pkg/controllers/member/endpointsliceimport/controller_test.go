/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package endpointsliceimport

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

const (
	hubNSForMember          = "bravelion"
	memberUserNS            = "work"
	fleetSystemNS           = "fleet-system"
	svcName                 = "app"
	derivedSvcName          = "work-app-1d2ef"
	endpointSliceName       = "app-endpointslice"
	endpointSliceImportName = "bravelion-work-appendpoint-slice-1a2bc"
)

var httpPortName = "http"
var httpPort = int32(80)
var httpPortProtocol = corev1.ProtocolTCP
var httpPortAppProtocol = "www"
var udpPortName = "udp"
var udpPort = int32(81)
var udpPortProtocol = corev1.ProtocolUDP
var udpPortAppProtocol = "example.com/custom"

// Bootstrap the test environment.
func TestMain(m *testing.M) {
	// Add custom APIs to the runtime scheme
	if err := fleetnetv1alpha1.AddToScheme(scheme.Scheme); err != nil {
		log.Fatalf("failed to add custom APIs to the runtime scheme: %v", err)
	}

	os.Exit(m.Run())
}

// ipv4EndpointSliceImport returns an EndpointSliceImport.
func ipv4EndpointSliceImport() *fleetnetv1alpha1.EndpointSliceImport {
	return &fleetnetv1alpha1.EndpointSliceImport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: hubNSForMember,
			Name:      endpointSliceImportName,
		},
		Spec: fleetnetv1alpha1.EndpointSliceExportSpec{
			AddressType: discoveryv1.AddressTypeIPv4,
			Endpoints: []fleetnetv1alpha1.Endpoint{
				{
					Addresses: []string{"1.2.3.4"},
				},
				{
					Addresses: []string{"2.3.4.5"},
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
				ClusterID:       hubNSForMember,
				Kind:            "EndpointSlice",
				Namespace:       memberUserNS,
				Name:            endpointSliceName,
				ResourceVersion: "0",
				Generation:      1,
				UID:             "00000000-0000-0000-0000-000000000000",
			},
			OwnerServiceReference: fleetnetv1alpha1.OwnerServiceReference{
				Namespace: memberUserNS,
				Name:      svcName,
			},
		},
	}
}

// importedIPv4EndpointSlice returns an EndpointSlice.
func importedIPv4EndpointSlice() *discoveryv1.EndpointSlice {
	return &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: fleetSystemNS,
			Name:      endpointSliceImportName,
			Labels: map[string]string{
				discoveryv1.LabelServiceName: derivedSvcName,
				discoveryv1.LabelManagedBy:   controllerID,
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints: []discoveryv1.Endpoint{
			{
				Addresses: []string{"1.2.3.4"},
			},
			{
				Addresses: []string{"2.3.4.5"},
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

// TestScanForDerivedServiceName tests the scanForDerivedServiceName function.
func TestScanForDerivedServiceName(t *testing.T) {
	multiClusterSvcName := "app"
	altMultiClusterSvcName := "app2"

	testCases := []struct {
		name                string
		multiClusterSvcList *fleetnetv1alpha1.MultiClusterServiceList
		want                string
	}{
		{
			name: "should return first found derived svc label",
			multiClusterSvcList: &fleetnetv1alpha1.MultiClusterServiceList{
				Items: []fleetnetv1alpha1.MultiClusterService{
					{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: memberUserNS,
							Name:      multiClusterSvcName,
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: memberUserNS,
							Name:      altMultiClusterSvcName,
							Labels: map[string]string{
								derivedServiceLabel: derivedSvcName,
							},
						},
					},
				},
			},
			want: derivedSvcName,
		},
		{
			name: "no derived svc label",
			multiClusterSvcList: &fleetnetv1alpha1.MultiClusterServiceList{
				Items: []fleetnetv1alpha1.MultiClusterService{
					{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: memberUserNS,
							Name:      multiClusterSvcName,
						},
					},
				},
			},
			want: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := scanForDerivedServiceName(tc.multiClusterSvcList); got != tc.want {
				t.Fatalf("scanForDerivedServiceName(%+v) = %s, want %s", tc.multiClusterSvcList, got, tc.want)
			}
		})
	}
}

// TestFormatEndpointSliceFromImport tests the formatEndpointSliceFromImport function.
func TestFormatEndpointSliceFromImport(t *testing.T) {
	testCases := []struct {
		name                string
		endpointSliceImport *fleetnetv1alpha1.EndpointSliceImport
		want                *discoveryv1.EndpointSlice
	}{
		{
			name:                "should format endpointslice using an endpointslice import",
			endpointSliceImport: ipv4EndpointSliceImport(),
			want:                importedIPv4EndpointSlice(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			endpointSlice := &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: fleetSystemNS,
					Name:      endpointSliceImportName,
				},
			}

			formatEndpointSliceFromImport(endpointSlice, derivedSvcName, tc.endpointSliceImport)
			if !cmp.Equal(endpointSlice, tc.want) {
				t.Fatalf("formatEndpointSliceImport(), got %+v, want %+v", endpointSlice, tc.want)
			}
		})
	}
}

// TestIsDerivedServiceValid tests the isDerivedServiceValid function.
func TestIsDerivedServiceValid(t *testing.T) {
	deletionTimestamp := metav1.Now()

	testCases := []struct {
		name           string
		derivedSvcName string
		derivedSvc     *corev1.Service
		want           bool
	}{
		{
			name:           "derived svc is valid",
			derivedSvcName: derivedSvcName,
			derivedSvc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: fleetSystemNS,
					Name:      derivedSvcName,
				},
			},
			want: true,
		},
		{
			name:           "derived svc is invalid (bad name)",
			derivedSvcName: "",
			want:           false,
		},
		{
			name:           "derived svc is invalid (svc not found)",
			derivedSvcName: derivedSvcName,
			want:           false,
		},
		{
			name:           "derived svc is invalid (svc deleted)",
			derivedSvcName: derivedSvcName,
			derivedSvc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:         fleetSystemNS,
					Name:              derivedSvcName,
					DeletionTimestamp: &deletionTimestamp,
				},
			},
			want: false,
		},
	}

	ctx := context.Background()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeMemberClientBuilder := fake.NewClientBuilder().WithScheme(scheme.Scheme)
			if tc.derivedSvc != nil {
				fakeMemberClientBuilder = fakeMemberClientBuilder.WithObjects(tc.derivedSvc)
			}
			fakeMemberClient := fakeMemberClientBuilder.Build()
			fakeHubClient := fake.NewClientBuilder().Build()
			reconciler := Reconciler{
				memberClient:         fakeMemberClient,
				hubClient:            fakeHubClient,
				fleetSystemNamespace: fleetSystemNS,
			}

			if got := reconciler.isDerivedServiceValid(ctx, tc.derivedSvcName); got != tc.want {
				t.Fatalf("isDerivedServiceValid(%+v), got %t, want %t", tc.derivedSvc, got, tc.want)
			}
		})
	}
}

// TestUnimportEndpointSlice_WithFinalizer tests the *Reconciler.unimportEndpointSlice method.
func TestUnimportEndpointSlice_WithFinalizer(t *testing.T) {
	testCases := []struct {
		name                string
		endpointSliceImport *fleetnetv1alpha1.EndpointSliceImport
		endpointSlice       *discoveryv1.EndpointSlice
	}{
		{
			name: "should unimport endpointslice",
			endpointSliceImport: &fleetnetv1alpha1.EndpointSliceImport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMember,
					Name:      endpointSliceImportName,
					Finalizers: []string{
						endpointSliceImportCleanupFinalizer,
					},
				},
			},
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: fleetSystemNS,
					Name:      endpointSliceImportName,
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
				WithScheme(scheme.Scheme).
				WithObjects(tc.endpointSliceImport).
				Build()
			reconciler := Reconciler{
				memberClient:         fakeMemberClient,
				hubClient:            fakeHubClient,
				fleetSystemNamespace: fleetSystemNS,
			}

			if err := reconciler.unimportEndpointSlice(ctx, tc.endpointSliceImport); err != nil {
				t.Fatalf("unimportEndpointSlice(%+v), got %v, want no error", tc.endpointSliceImport, err)
			}

			updateEndpointSliceImport := &fleetnetv1alpha1.EndpointSliceImport{}
			endpointSliceImportKey := types.NamespacedName{Namespace: hubNSForMember, Name: endpointSliceImportName}
			if err := fakeHubClient.Get(ctx, endpointSliceImportKey, updateEndpointSliceImport); err != nil {
				t.Fatalf("endpointSliceImport Get(%+v), got %v, want no error", endpointSliceImportKey, err)
			}

			if len(updateEndpointSliceImport.Finalizers) != 0 {
				t.Fatalf("endpointSliceImport finalizers, got %+v, want %+v", updateEndpointSliceImport.Finalizers, []string{})
			}
		})
	}
}

// TestUnimportEndpointSlice_WithoutFinalizer tests the *Reconciler.unimportEndpointSlice method.
func TestUnimportEndpointSlice_WithoutFinalizer(t *testing.T) {
	testCases := []struct {
		name                string
		endpointSliceImport *fleetnetv1alpha1.EndpointSliceImport
		endpointSlice       *discoveryv1.EndpointSlice
	}{
		{
			name: "should ignore endpointslice import with no finalizer",
			endpointSliceImport: &fleetnetv1alpha1.EndpointSliceImport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMember,
					Name:      endpointSliceImportName,
				},
			},
			endpointSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: fleetSystemNS,
					Name:      endpointSliceImportName,
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
				WithScheme(scheme.Scheme).
				WithObjects(tc.endpointSliceImport).
				Build()
			reconciler := Reconciler{
				memberClient:         fakeMemberClient,
				hubClient:            fakeHubClient,
				fleetSystemNamespace: fleetSystemNS,
			}

			if err := reconciler.unimportEndpointSlice(ctx, tc.endpointSliceImport); err != nil {
				t.Fatalf("unimportEndpointSlice(%+v), got %v, want no error", tc.endpointSliceImport, err)
			}

			updateEndpointSliceImport := &fleetnetv1alpha1.EndpointSliceImport{}
			endpointSliceImportKey := types.NamespacedName{Namespace: hubNSForMember, Name: endpointSliceImportName}
			if err := fakeHubClient.Get(ctx, endpointSliceImportKey, updateEndpointSliceImport); err != nil {
				t.Fatalf("endpointSliceImport Get(%+v), got %v, want no error", endpointSliceImportKey, err)
			}

			if len(updateEndpointSliceImport.Finalizers) != 0 {
				t.Fatalf("endpointSliceImport finalizers, got %+v, want %+v", updateEndpointSliceImport.Finalizers, []string{})
			}

			endpointSlice := &discoveryv1.EndpointSlice{}
			endpointSliceKey := types.NamespacedName{Namespace: fleetSystemNS, Name: endpointSliceImportName}
			if err := fakeMemberClient.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
				t.Fatalf("endpointSlice Get(%+v), got %v, want no error", endpointSliceKey, err)
			}
		})
	}
}

// TestRemoveEndpointSliceImportCleanupFinalizer tests the *Reconciler.removeEndpointSliceImportCleanupFinalizer method.
func TestRemoveEndpointSliceImportCleanupFinalizer(t *testing.T) {
	testCases := []struct {
		name                string
		endpointSliceImport *fleetnetv1alpha1.EndpointSliceImport
	}{
		{
			name: "should remove cleanup finalizer",
			endpointSliceImport: &fleetnetv1alpha1.EndpointSliceImport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMember,
					Name:      endpointSliceImportName,
					Finalizers: []string{
						endpointSliceImportCleanupFinalizer,
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
				Build()
			fakeHubClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.endpointSliceImport).
				Build()
			reconciler := Reconciler{
				memberClient:         fakeMemberClient,
				hubClient:            fakeHubClient,
				fleetSystemNamespace: fleetSystemNS,
			}

			if err := reconciler.removeEndpointSliceImportCleanupFinalizer(ctx, tc.endpointSliceImport); err != nil {
				t.Fatalf("removeEndpointSliceImportCleanupFinalizer(%+v), got %v, want no error", tc.endpointSliceImport, err)
			}

			updatedEndpointSliceImport := &fleetnetv1alpha1.EndpointSliceImport{}
			endpointSliceImportKey := types.NamespacedName{Namespace: hubNSForMember, Name: endpointSliceImportName}
			if err := fakeHubClient.Get(ctx, endpointSliceImportKey, updatedEndpointSliceImport); err != nil {
				t.Fatalf("endpointSliceImport Get(%+v), got %v, want no error", endpointSliceImportKey, err)
			}

			if len(updatedEndpointSliceImport.Finalizers) != 0 {
				t.Fatalf("endpointSliceImport finalizers, got %+v, want %+v", updatedEndpointSliceImport.Finalizers, []string{})
			}
		})
	}
}

// TestAddEndpointSliceImportCleanupFinalizer tests the *Reconciler.addEndpointSliceImportCleanupFinalizer method.
func TestAddEndpointSliceImportCleanupFinalizer(t *testing.T) {
	testCases := []struct {
		name                string
		endpointSliceImport *fleetnetv1alpha1.EndpointSliceImport
	}{
		{
			name: "should add cleanup finalizer",
			endpointSliceImport: &fleetnetv1alpha1.EndpointSliceImport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMember,
					Name:      endpointSliceImportName,
				},
			},
		},
	}

	ctx := context.Background()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeMemberClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				Build()
			fakeHubClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.endpointSliceImport).
				Build()
			reconciler := Reconciler{
				memberClient:         fakeMemberClient,
				hubClient:            fakeHubClient,
				fleetSystemNamespace: fleetSystemNS,
			}

			if err := reconciler.addEndpointSliceImportCleanupFinalizer(ctx, tc.endpointSliceImport); err != nil {
				t.Fatalf("addEndpointSliceImportCleanupFinalizer(%+v), got %v, want no error", tc.endpointSliceImport, err)
			}

			updatedEndpointSliceImport := &fleetnetv1alpha1.EndpointSliceImport{}
			endpointSliceImportKey := types.NamespacedName{Namespace: hubNSForMember, Name: endpointSliceImportName}
			if err := fakeHubClient.Get(ctx, endpointSliceImportKey, updatedEndpointSliceImport); err != nil {
				t.Fatalf("endpointSliceImport Get(%+v), got %v, want no error", endpointSliceImportKey, err)
			}

			if !cmp.Equal(updatedEndpointSliceImport.Finalizers, []string{endpointSliceImportCleanupFinalizer}) {
				t.Fatalf("endpointSliceImport finalizer, got %v, want %v",
					updatedEndpointSliceImport.Finalizers,
					[]string{endpointSliceImportCleanupFinalizer})
			}
		})
	}
}
