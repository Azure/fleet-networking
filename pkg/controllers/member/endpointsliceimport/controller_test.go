/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package endpointsliceimport

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/prometheus/client_golang/prometheus/testutil"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/metrics"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
)

const (
	memberClusterID                = "bravelion"
	hubNSForMember                 = "bravelion"
	memberUserNS                   = "work"
	fleetSystemNS                  = "fleet-system"
	svcName                        = "app"
	derivedSvcName                 = "work-app-1d2ef"
	endpointSliceName              = "app-endpointslice"
	endpointSliceImportName        = "bravelion-work-appendpoint-slice-1a2bc"
	customDeletionBlockerFinalizer = "custom-deletion-finalizer"
)

var (
	httpPortName        = "http"
	httpPort            = int32(80)
	httpPortProtocol    = corev1.ProtocolTCP
	httpPortAppProtocol = "www"
	tcpPortName         = "tcp"
	tcpPort             = int32(81)
	tcpPortProtocol     = corev1.ProtocolTCP
	tcpPortAppProtocol  = "example.com/custom"
	udpPortName         = "udp"
	udpPort             = int32(82)
	udpPortProtocol     = corev1.ProtocolUDP
	udpPortAppProtocol  = "example.com/custom-2"

	endpointSliceImportKey = types.NamespacedName{Namespace: hubNSForMember, Name: endpointSliceImportName}
)

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
					Name:        &tcpPortName,
					Protocol:    &tcpPortProtocol,
					Port:        &tcpPort,
					AppProtocol: &tcpPortAppProtocol,
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
				ExportedSince:   metav1.NewTime(time.Now().Round(time.Second)),
			},
			OwnerServiceReference: fleetnetv1alpha1.OwnerServiceReference{
				Namespace: memberUserNS,
				Name:      svcName,
			},
		},
	}
}

// ipv4EndpointSliceImportWithHybridProtocol returns an EndpointSliceImport with both TCP and UDP ports.
func ipv4EndpointSliceImportWithHybridProtocol() *fleetnetv1alpha1.EndpointSliceImport {
	endpointSliceImport := ipv4EndpointSliceImport()
	endpointSliceImport.Spec.Ports[0] = discoveryv1.EndpointPort{
		Name:        &udpPortName,
		Protocol:    &udpPortProtocol,
		Port:        &udpPort,
		AppProtocol: &udpPortAppProtocol,
	}
	return endpointSliceImport
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
				Name:        &tcpPortName,
				Protocol:    &tcpPortProtocol,
				Port:        &tcpPort,
				AppProtocol: &tcpPortAppProtocol,
			},
		},
	}
}

// importedIPV4EndpointSliceWithHybridProtocol returns an EndpointSlice with both TCP and UDP ports.
func importedIPv4EndpointSliceWithHybridProtocol() *discoveryv1.EndpointSlice {
	endpointSlice := importedIPv4EndpointSlice()
	endpointSlice.Ports[0] = discoveryv1.EndpointPort{
		Name:        &udpPortName,
		Protocol:    &udpPortProtocol,
		Port:        &udpPort,
		AppProtocol: &udpPortAppProtocol,
	}
	return endpointSlice
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
								objectmeta.MultiClusterServiceLabelDerivedService: derivedSvcName,
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
			if diff := cmp.Diff(endpointSlice, tc.want); diff != "" {
				t.Fatalf("formatEndpointSliceImport(), got diff %s", diff)
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
					Finalizers: []string{
						// Note that fake client will reject an object if it is deleted (has the
						// deletion timestamp) but does not have a finalizer.
						customDeletionBlockerFinalizer,
					},
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
				MemberClient:         fakeMemberClient,
				HubClient:            fakeHubClient,
				FleetSystemNamespace: fleetSystemNS,
			}

			if got, err := reconciler.isDerivedServiceValid(ctx, tc.derivedSvcName); got != tc.want || err != nil {
				t.Fatalf("isDerivedServiceValid(%+v) = %t, %v, want %t, no error", tc.derivedSvcName, got, err, tc.want)
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
		},
	}

	ctx := context.Background()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeMemberClientBuilder := fake.NewClientBuilder().WithScheme(scheme.Scheme)
			if tc.endpointSlice != nil {
				fakeMemberClientBuilder = fakeMemberClientBuilder.WithObjects(tc.endpointSlice)
			}
			fakeMemberClient := fakeMemberClientBuilder.Build()
			fakeHubClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.endpointSliceImport).
				Build()
			reconciler := Reconciler{
				MemberClient:         fakeMemberClient,
				HubClient:            fakeHubClient,
				FleetSystemNamespace: fleetSystemNS,
			}

			if err := reconciler.unimportEndpointSlice(ctx, tc.endpointSliceImport); err != nil {
				t.Fatalf("unimportEndpointSlice(%+v), got %v, want no error", tc.endpointSliceImport, err)
			}

			updateEndpointSliceImport := &fleetnetv1alpha1.EndpointSliceImport{}
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
				MemberClient:         fakeMemberClient,
				HubClient:            fakeHubClient,
				FleetSystemNamespace: fleetSystemNS,
			}

			if err := reconciler.unimportEndpointSlice(ctx, tc.endpointSliceImport); err != nil {
				t.Fatalf("unimportEndpointSlice(%+v), got %v, want no error", tc.endpointSliceImport, err)
			}

			updateEndpointSliceImport := &fleetnetv1alpha1.EndpointSliceImport{}
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
				MemberClient:         fakeMemberClient,
				HubClient:            fakeHubClient,
				FleetSystemNamespace: fleetSystemNS,
			}

			if err := reconciler.addEndpointSliceImportCleanupFinalizer(ctx, tc.endpointSliceImport); err != nil {
				t.Fatalf("addEndpointSliceImportCleanupFinalizer(%+v), got %v, want no error", tc.endpointSliceImport, err)
			}

			updatedEndpointSliceImport := &fleetnetv1alpha1.EndpointSliceImport{}
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

// TestObserveMetrics tests the Reconciler.observeMetrics function.
func TestObserveMetrics(t *testing.T) {
	metricMetadata := `
		# HELP fleet_networking_endpointslice_export_import_duration_milliseconds The duration of an endpointslice export
		# TYPE fleet_networking_endpointslice_export_import_duration_milliseconds histogram
	`
	startTime := time.Now().Round(time.Second)

	testCases := []struct {
		name                string
		endpointSliceImport *fleetnetv1alpha1.EndpointSliceImport
		startTime           time.Time
		wantMetricCount     int
		wantHistogram       string
	}{
		{
			name: "should not observe data point (no exportedSince field)",
			endpointSliceImport: &fleetnetv1alpha1.EndpointSliceImport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMember,
					Name:      endpointSliceImportName,
				},
			},
			startTime:       startTime,
			wantMetricCount: 0,
			wantHistogram:   "",
		},
		{
			name: "should not observe data point (the object generation has been observed before)",
			endpointSliceImport: &fleetnetv1alpha1.EndpointSliceImport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMember,
					Name:      endpointSliceImportName,
					Annotations: map[string]string{
						metrics.MetricsAnnotationLastObservedGeneration: "1",
					},
				},
				Spec: fleetnetv1alpha1.EndpointSliceExportSpec{
					EndpointSliceReference: fleetnetv1alpha1.ExportedObjectReference{
						Generation: 1,
					},
				},
			},
			startTime:       startTime,
			wantMetricCount: 0,
			wantHistogram:   "",
		},
		{
			name: "should observe a data point",
			endpointSliceImport: &fleetnetv1alpha1.EndpointSliceImport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMember,
					Name:      endpointSliceImportName,
				},
				Spec: fleetnetv1alpha1.EndpointSliceExportSpec{
					EndpointSliceReference: fleetnetv1alpha1.ExportedObjectReference{
						NamespacedName: svcName,
						Generation:     2,
						ClusterID:      memberClusterID,
						ExportedSince:  metav1.NewTime(startTime.Add(-time.Second)),
					},
				},
			},
			startTime:       startTime,
			wantMetricCount: 1,
			wantHistogram: fmt.Sprintf(`
				fleet_networking_endpointslice_export_import_duration_milliseconds_bucket{destination_cluster_id="%[1]s",is_first_import="true",origin_cluster_id="%[1]s",le="1000"} 1
            	fleet_networking_endpointslice_export_import_duration_milliseconds_bucket{destination_cluster_id="%[1]s",is_first_import="true",origin_cluster_id="%[1]s",le="2500"} 1
            	fleet_networking_endpointslice_export_import_duration_milliseconds_bucket{destination_cluster_id="%[1]s",is_first_import="true",origin_cluster_id="%[1]s",le="5000"} 1
            	fleet_networking_endpointslice_export_import_duration_milliseconds_bucket{destination_cluster_id="%[1]s",is_first_import="true",origin_cluster_id="%[1]s",le="10000"} 1
            	fleet_networking_endpointslice_export_import_duration_milliseconds_bucket{destination_cluster_id="%[1]s",is_first_import="true",origin_cluster_id="%[1]s",le="25000"} 1
            	fleet_networking_endpointslice_export_import_duration_milliseconds_bucket{destination_cluster_id="%[1]s",is_first_import="true",origin_cluster_id="%[1]s",le="50000"} 1
            	fleet_networking_endpointslice_export_import_duration_milliseconds_bucket{destination_cluster_id="%[1]s",is_first_import="true",origin_cluster_id="%[1]s",le="+Inf"} 1
            	fleet_networking_endpointslice_export_import_duration_milliseconds_sum{destination_cluster_id="%[1]s",is_first_import="true",origin_cluster_id="%[1]s"} 1000
            	fleet_networking_endpointslice_export_import_duration_milliseconds_count{destination_cluster_id="%[1]s",is_first_import="true",origin_cluster_id="%[1]s"} 1
			`, memberClusterID),
		},
		{
			name: "should observe a data point (negative export duration)",
			endpointSliceImport: &fleetnetv1alpha1.EndpointSliceImport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMember,
					Name:      endpointSliceImportName,
				},
				Spec: fleetnetv1alpha1.EndpointSliceExportSpec{
					EndpointSliceReference: fleetnetv1alpha1.ExportedObjectReference{
						NamespacedName: svcName,
						Generation:     3,
						ClusterID:      memberClusterID,
						ExportedSince:  metav1.NewTime(startTime.Add(time.Second * 2)),
					},
				},
			},
			startTime:       startTime,
			wantMetricCount: 1,
			wantHistogram: fmt.Sprintf(`
				fleet_networking_endpointslice_export_import_duration_milliseconds_bucket{destination_cluster_id="%[1]s",is_first_import="true",origin_cluster_id="%[1]s",le="1000"} 2
            	fleet_networking_endpointslice_export_import_duration_milliseconds_bucket{destination_cluster_id="%[1]s",is_first_import="true",origin_cluster_id="%[1]s",le="2500"} 2
            	fleet_networking_endpointslice_export_import_duration_milliseconds_bucket{destination_cluster_id="%[1]s",is_first_import="true",origin_cluster_id="%[1]s",le="5000"} 2
            	fleet_networking_endpointslice_export_import_duration_milliseconds_bucket{destination_cluster_id="%[1]s",is_first_import="true",origin_cluster_id="%[1]s",le="10000"} 2
            	fleet_networking_endpointslice_export_import_duration_milliseconds_bucket{destination_cluster_id="%[1]s",is_first_import="true",origin_cluster_id="%[1]s",le="25000"} 2
            	fleet_networking_endpointslice_export_import_duration_milliseconds_bucket{destination_cluster_id="%[1]s",is_first_import="true",origin_cluster_id="%[1]s",le="50000"} 2
            	fleet_networking_endpointslice_export_import_duration_milliseconds_bucket{destination_cluster_id="%[1]s",is_first_import="true",origin_cluster_id="%[1]s",le="+Inf"} 2
            	fleet_networking_endpointslice_export_import_duration_milliseconds_sum{destination_cluster_id="%[1]s",is_first_import="true",origin_cluster_id="%[1]s"} 2000
            	fleet_networking_endpointslice_export_import_duration_milliseconds_count{destination_cluster_id="%[1]s",is_first_import="true",origin_cluster_id="%[1]s"} 2
			`, memberClusterID),
		},
		{
			name: "should observe a data point (large outlier)",
			endpointSliceImport: &fleetnetv1alpha1.EndpointSliceImport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMember,
					Name:      endpointSliceImportName,
					Annotations: map[string]string{
						metrics.MetricsAnnotationLastObservedGeneration: "3",
					},
				},
				Spec: fleetnetv1alpha1.EndpointSliceExportSpec{
					EndpointSliceReference: fleetnetv1alpha1.ExportedObjectReference{
						NamespacedName: svcName,
						Generation:     4,
						ClusterID:      memberClusterID,
						ExportedSince:  metav1.NewTime(startTime.Add(-time.Minute * 5)),
					},
				},
			},
			startTime:       startTime,
			wantMetricCount: 2,
			wantHistogram: fmt.Sprintf(`
				fleet_networking_endpointslice_export_import_duration_milliseconds_bucket{destination_cluster_id="%[1]s",is_first_import="false",origin_cluster_id="%[1]s",le="1000"} 0
            	fleet_networking_endpointslice_export_import_duration_milliseconds_bucket{destination_cluster_id="%[1]s",is_first_import="false",origin_cluster_id="%[1]s",le="2500"} 0
            	fleet_networking_endpointslice_export_import_duration_milliseconds_bucket{destination_cluster_id="%[1]s",is_first_import="false",origin_cluster_id="%[1]s",le="5000"} 0
            	fleet_networking_endpointslice_export_import_duration_milliseconds_bucket{destination_cluster_id="%[1]s",is_first_import="false",origin_cluster_id="%[1]s",le="10000"} 0
            	fleet_networking_endpointslice_export_import_duration_milliseconds_bucket{destination_cluster_id="%[1]s",is_first_import="false",origin_cluster_id="%[1]s",le="25000"} 0
            	fleet_networking_endpointslice_export_import_duration_milliseconds_bucket{destination_cluster_id="%[1]s",is_first_import="false",origin_cluster_id="%[1]s",le="50000"} 0
            	fleet_networking_endpointslice_export_import_duration_milliseconds_bucket{destination_cluster_id="%[1]s",is_first_import="false",origin_cluster_id="%[1]s",le="+Inf"} 1
            	fleet_networking_endpointslice_export_import_duration_milliseconds_sum{destination_cluster_id="%[1]s",is_first_import="false",origin_cluster_id="%[1]s"} 100000
            	fleet_networking_endpointslice_export_import_duration_milliseconds_count{destination_cluster_id="%[1]s",is_first_import="false",origin_cluster_id="%[1]s"} 1
				fleet_networking_endpointslice_export_import_duration_milliseconds_bucket{destination_cluster_id="%[1]s",is_first_import="true",origin_cluster_id="%[1]s",le="1000"} 2
            	fleet_networking_endpointslice_export_import_duration_milliseconds_bucket{destination_cluster_id="%[1]s",is_first_import="true",origin_cluster_id="%[1]s",le="2500"} 2
            	fleet_networking_endpointslice_export_import_duration_milliseconds_bucket{destination_cluster_id="%[1]s",is_first_import="true",origin_cluster_id="%[1]s",le="5000"} 2
            	fleet_networking_endpointslice_export_import_duration_milliseconds_bucket{destination_cluster_id="%[1]s",is_first_import="true",origin_cluster_id="%[1]s",le="10000"} 2
            	fleet_networking_endpointslice_export_import_duration_milliseconds_bucket{destination_cluster_id="%[1]s",is_first_import="true",origin_cluster_id="%[1]s",le="25000"} 2
            	fleet_networking_endpointslice_export_import_duration_milliseconds_bucket{destination_cluster_id="%[1]s",is_first_import="true",origin_cluster_id="%[1]s",le="50000"} 2
            	fleet_networking_endpointslice_export_import_duration_milliseconds_bucket{destination_cluster_id="%[1]s",is_first_import="true",origin_cluster_id="%[1]s",le="+Inf"} 2
            	fleet_networking_endpointslice_export_import_duration_milliseconds_sum{destination_cluster_id="%[1]s",is_first_import="true",origin_cluster_id="%[1]s"} 2000
            	fleet_networking_endpointslice_export_import_duration_milliseconds_count{destination_cluster_id="%[1]s",is_first_import="true",origin_cluster_id="%[1]s"} 2
			`, memberClusterID),
		},
	}

	ctx := context.Background()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeMemberClient := fake.NewClientBuilder().Build()
			fakeHubClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.endpointSliceImport).
				Build()
			reconciler := Reconciler{
				MemberClusterID:      memberClusterID,
				MemberClient:         fakeMemberClient,
				HubClient:            fakeHubClient,
				FleetSystemNamespace: fleetSystemNS,
			}

			if err := reconciler.observeMetrics(ctx, tc.endpointSliceImport, tc.startTime); err != nil {
				t.Fatalf("observeMetrics(%+v), got %v, want no error", tc.endpointSliceImport, err)
			}

			endpointSliceImport := &fleetnetv1alpha1.EndpointSliceImport{}
			if err := fakeHubClient.Get(ctx, endpointSliceImportKey, endpointSliceImport); err != nil {
				t.Fatalf("internalServiceExport Get(%+v), got %v, want no error", endpointSliceImportKey, err)
			}
			lastObservedGeneration, ok := endpointSliceImport.Annotations[metrics.MetricsAnnotationLastObservedGeneration]
			if !ok || lastObservedGeneration != fmt.Sprintf("%d", tc.endpointSliceImport.Spec.EndpointSliceReference.Generation) {
				t.Fatalf("lastObservedGeneration, got %s, want %d", lastObservedGeneration, tc.endpointSliceImport.Spec.EndpointSliceReference.Generation)
			}

			if c := testutil.CollectAndCount(endpointSliceExportImportDuration); c != tc.wantMetricCount {
				t.Fatalf("metric counts, got %d, want %d", c, tc.wantMetricCount)
			}

			if tc.wantHistogram != "" {
				if err := testutil.CollectAndCompare(endpointSliceExportImportDuration, strings.NewReader(metricMetadata+tc.wantHistogram)); err != nil {
					t.Errorf("%s", err)
				}
			}
		})
	}
}
