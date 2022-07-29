/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package internalserviceimport

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
)

const (
	hubNSForMemberA       = "bravelion"
	clusterIDForMemberA   = "0"
	hubNSForMemberB       = "highflyingcat"
	clusterIDForMemberB   = "1"
	clusterIDForMemberC   = "2"
	memberUserNS          = "work"
	svcName               = "app"
	internalSvcImportName = "work-app"
)

var (
	svcImportKey          = types.NamespacedName{Namespace: memberUserNS, Name: svcName}
	internalSvcImportAKey = types.NamespacedName{Namespace: hubNSForMemberA, Name: internalSvcImportName}

	httpPortName        = "http"
	httpPort            = int32(80)
	httpPortProtocol    = corev1.ProtocolTCP
	httpPortAppProtocol = "www"
	udpPortName         = "udp"
	udpPort             = int32(81)
	udpPortProtocol     = corev1.ProtocolUDP
	udpPortAppProtocol  = "example.com/custom"
)

// fulfilledSvcInUseByAnnotation returns a fulfilled ServiceInUseBy for annotation use.
func fulfilledServiceInUseByAnnotation() *fleetnetv1alpha1.ServiceInUseBy {
	return &fleetnetv1alpha1.ServiceInUseBy{
		MemberClusters: map[fleetnetv1alpha1.ClusterNamespace]fleetnetv1alpha1.ClusterID{
			hubNSForMemberA: clusterIDForMemberA,
		},
	}
}

// fulfilledSvcInUseByAnnotationString returns marshalled ServiceInUseBy data in the string form.
func fulfilledSvcInUseByAnnotationString() string {
	data, _ := json.Marshal(fulfilledServiceInUseByAnnotation())
	return string(data)
}

// fulfilledSvcImport returns a fulfilled ServiceImport.
func fulfilledServiceImport() *fleetnetv1alpha1.ServiceImport {
	return &fleetnetv1alpha1.ServiceImport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      svcName,
			Annotations: map[string]string{
				objectmeta.ServiceInUseByAnnotationKey: fulfilledSvcInUseByAnnotationString(),
			},
			Finalizers: []string{svcImportCleanupFinalizer},
		},
		Status: fleetnetv1alpha1.ServiceImportStatus{
			Type: fleetnetv1alpha1.ClusterSetIP,
			Ports: []fleetnetv1alpha1.ServicePort{
				{
					Name:        httpPortName,
					Protocol:    httpPortProtocol,
					AppProtocol: &httpPortAppProtocol,
					Port:        httpPort,
				},
				{
					Name:        udpPortName,
					Protocol:    udpPortProtocol,
					AppProtocol: &udpPortAppProtocol,
					Port:        udpPort,
				},
			},
			Clusters: []fleetnetv1alpha1.ClusterStatus{
				{
					Cluster: clusterIDForMemberA,
				},
				{
					Cluster: clusterIDForMemberB,
				},
				{
					Cluster: clusterIDForMemberC,
				},
			},
		},
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

// TestExtractServiceInUseByInfoFromServiceImport tests the extractServiceInUseByInfoFromServiceImport function.
func TestExtractServiceInUseByInfoFromServiceImport(t *testing.T) {
	testCases := []struct {
		name           string
		svcImport      *fleetnetv1alpha1.ServiceImport
		wantSvcInUseBy *fleetnetv1alpha1.ServiceInUseBy
	}{
		{
			name: "should return empty service in use by info (no annotation)",
			svcImport: &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
			},
			wantSvcInUseBy: &fleetnetv1alpha1.ServiceInUseBy{
				MemberClusters: map[fleetnetv1alpha1.ClusterNamespace]fleetnetv1alpha1.ClusterID{},
			},
		},
		{
			name: "should return empty service in use by info (bad annotation)",
			svcImport: &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
					Annotations: map[string]string{
						objectmeta.ServiceInUseByAnnotationKey: "xyz",
					},
				},
			},
			wantSvcInUseBy: &fleetnetv1alpha1.ServiceInUseBy{
				MemberClusters: map[fleetnetv1alpha1.ClusterNamespace]fleetnetv1alpha1.ClusterID{},
			},
		},
		{
			name: "should return valid service in use by info",
			svcImport: &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
					Annotations: map[string]string{
						objectmeta.ServiceInUseByAnnotationKey: fulfilledSvcInUseByAnnotationString(),
					},
				},
			},
			wantSvcInUseBy: fulfilledServiceInUseByAnnotation(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			svcInUseBy := extractServiceInUseByInfoFromServiceImport(tc.svcImport)
			if diff := cmp.Diff(svcInUseBy, tc.wantSvcInUseBy); diff != "" {
				t.Fatalf("extractServiceInUseByInfoFromServiceImport(%+v), mismatch (-got, +want)\n%s", tc.svcImport, diff)
			}
		})
	}
}

// TestWithdrawServiceImport_AnnotationMatches tests the Reconciler.withdrawServiceImport method.
func TestWithdrawServiceImport_AnnotationMatches(t *testing.T) {
	testCases := []struct {
		name              string
		svcImport         *fleetnetv1alpha1.ServiceImport
		internalSvcImport *fleetnetv1alpha1.InternalServiceImport
	}{
		{
			name:      "should withdraw service import (annotation matches)",
			svcImport: fulfilledServiceImport(),
			internalSvcImport: &fleetnetv1alpha1.InternalServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  hubNSForMemberA,
					Name:       internalSvcImportName,
					Finalizers: []string{internalSvcImportCleanupFinalizer},
				},
			},
		},
	}

	ctx := context.Background()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeHubClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.svcImport, tc.internalSvcImport).
				Build()
			reconciler := Reconciler{
				HubClient: fakeHubClient,
			}

			if res, err := reconciler.withdrawServiceImport(ctx, tc.svcImport, tc.internalSvcImport); !cmp.Equal(res, ctrl.Result{}) || err != nil {
				t.Fatalf("withdrawServiceImport(%+v, %+v) = %+v, %v, want %v, no error", tc.svcImport, tc.internalSvcImport, res, err, ctrl.Result{})
			}

			svcImport := &fleetnetv1alpha1.ServiceImport{}
			if err := fakeHubClient.Get(ctx, svcImportKey, svcImport); err != nil {
				t.Fatalf("serviceImport Get(%+v), got %v, want no error", svcImportKey, err)
			}

			if len(svcImport.Finalizers) != 0 {
				t.Fatalf("serviceImport finalizers, got %v, want no finalizers", svcImport.Finalizers)
			}

			_, ok := svcImport.Annotations[objectmeta.ServiceInUseByAnnotationKey]
			if ok {
				t.Fatalf("serviceInUseBy annotation is present, want absence")
			}

			internalSvcImport := &fleetnetv1alpha1.InternalServiceImport{}
			internalSvcImportKey := types.NamespacedName{Namespace: tc.internalSvcImport.Namespace, Name: tc.internalSvcImport.Name}
			if err := fakeHubClient.Get(ctx, internalSvcImportKey, internalSvcImport); err != nil {
				t.Fatalf("internalServiceImport Get(%+v), got %v, want no error", internalSvcImportAKey, err)
			}

			if len(internalSvcImport.Finalizers) != 0 {
				t.Fatalf("internalServiceImport finalizers, got %v, want no finalizer", internalSvcImport.Finalizers)
			}
		})
	}
}

// TestWithdrawServiceImport_AnnotationMismatches tests the Reconciler.withdrawServiceImport method.
func TestWithdrawServiceImport_AnnotationMismatches(t *testing.T) {
	testCases := []struct {
		name               string
		svcImport          *fleetnetv1alpha1.ServiceImport
		internalSvcImport  *fleetnetv1alpha1.InternalServiceImport
		wantSvcInUseByData string
	}{
		{
			name:      "should withdraw service import (annotation mismatches)",
			svcImport: fulfilledServiceImport(),
			internalSvcImport: &fleetnetv1alpha1.InternalServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  hubNSForMemberB,
					Name:       internalSvcImportName,
					Finalizers: []string{internalSvcImportCleanupFinalizer},
				},
			},
			wantSvcInUseByData: fulfilledServiceImport().Annotations[objectmeta.ServiceInUseByAnnotationKey],
		},
	}

	ctx := context.Background()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeHubClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.svcImport, tc.internalSvcImport).
				Build()
			reconciler := Reconciler{
				HubClient: fakeHubClient,
			}

			if res, err := reconciler.withdrawServiceImport(ctx, tc.svcImport, tc.internalSvcImport); !cmp.Equal(res, ctrl.Result{}) || err != nil {
				t.Fatalf("withdrawServiceImport(%+v, %+v) = %+v, %v, want %v, no error", tc.svcImport, tc.internalSvcImport, res, err, ctrl.Result{})
			}

			svcImport := &fleetnetv1alpha1.ServiceImport{}
			if err := fakeHubClient.Get(ctx, svcImportKey, svcImport); err != nil {
				t.Fatalf("serviceImport Get(%+v), got %v, want no error", svcImportKey, err)
			}

			if !cmp.Equal(svcImport.Finalizers, []string{svcImportCleanupFinalizer}) {
				t.Fatalf("serviceImport finalizers, got %v, want %v", svcImport.Finalizers, []string{svcImportCleanupFinalizer})
			}

			data, ok := svcImport.Annotations[objectmeta.ServiceInUseByAnnotationKey]
			if !ok {
				t.Fatalf("serviceInUseBy annotation is absent, want presence")
			}
			if !cmp.Equal(data, tc.wantSvcInUseByData) {
				t.Fatalf("serviceInUseBy annotation, got %s, want %s", data, tc.wantSvcInUseByData)
			}

			internalSvcImport := &fleetnetv1alpha1.InternalServiceImport{}
			internalSvcImportKey := types.NamespacedName{Namespace: tc.internalSvcImport.Namespace, Name: tc.internalSvcImport.Name}
			if err := fakeHubClient.Get(ctx, internalSvcImportKey, internalSvcImport); err != nil {
				t.Fatalf("internalServiceImport Get(%+v), got %v, want no error", internalSvcImportAKey, err)
			}

			if len(internalSvcImport.Finalizers) != 0 {
				t.Fatalf("internalServiceImport finalizers, got %v, want no finalizer", internalSvcImport.Finalizers)
			}
		})
	}
}

// TestWithdrawServiceImport_MultiImports tests the Reconciler.withdrawServiceImport method.
func TestWithdrawServiceImport_MultiImports(t *testing.T) {
	svcInUseBy := fulfilledServiceInUseByAnnotation()
	svcInUseBy.MemberClusters[hubNSForMemberB] = clusterIDForMemberB
	svcInUseByData, err := json.Marshal(svcInUseBy)
	if err != nil {
		panic(err)
	}
	svcImport := fulfilledServiceImport()
	svcImport.Annotations[objectmeta.ServiceInUseByAnnotationKey] = string(svcInUseByData)

	testCases := []struct {
		name               string
		svcImport          *fleetnetv1alpha1.ServiceImport
		internalSvcImport  *fleetnetv1alpha1.InternalServiceImport
		wantSvcInUseByData string
	}{
		{
			name:      "should withdraw service import (multiple imports)",
			svcImport: svcImport,
			internalSvcImport: &fleetnetv1alpha1.InternalServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  hubNSForMemberB,
					Name:       internalSvcImportName,
					Finalizers: []string{internalSvcImportCleanupFinalizer},
				},
			},
			wantSvcInUseByData: fulfilledServiceImport().Annotations[objectmeta.ServiceInUseByAnnotationKey],
		},
	}

	ctx := context.Background()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeHubClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.svcImport, tc.internalSvcImport).
				Build()
			reconciler := Reconciler{
				HubClient: fakeHubClient,
			}

			if res, err := reconciler.withdrawServiceImport(ctx, tc.svcImport, tc.internalSvcImport); !cmp.Equal(res, ctrl.Result{}) || err != nil {
				t.Fatalf("withdrawServiceImport(%+v, %+v) = %+v, %v, want %v, no error", tc.svcImport, tc.internalSvcImport, res, err, ctrl.Result{})
			}

			svcImport := &fleetnetv1alpha1.ServiceImport{}
			if err := fakeHubClient.Get(ctx, svcImportKey, svcImport); err != nil {
				t.Fatalf("serviceImport Get(%+v), got %v, want no error", svcImportKey, err)
			}

			if !cmp.Equal(svcImport.Finalizers, []string{svcImportCleanupFinalizer}) {
				t.Fatalf("serviceImport finalizers, got %v, want %v", svcImport.Finalizers, []string{svcImportCleanupFinalizer})
			}

			data, ok := svcImport.Annotations[objectmeta.ServiceInUseByAnnotationKey]
			if !ok {
				t.Fatalf("serviceInUseBy annotation is absent, want presence")
			}
			if !cmp.Equal(data, tc.wantSvcInUseByData) {
				t.Fatalf("serviceInUseBy annotation, got %s, want %s", data, tc.wantSvcInUseByData)
			}

			internalSvcImport := &fleetnetv1alpha1.InternalServiceImport{}
			internalSvcImportKey := types.NamespacedName{Namespace: tc.internalSvcImport.Namespace, Name: tc.internalSvcImport.Name}
			if err := fakeHubClient.Get(ctx, internalSvcImportKey, internalSvcImport); err != nil {
				t.Fatalf("internalServiceImport Get(%+v), got %v, want no error", internalSvcImportAKey, err)
			}

			if len(internalSvcImport.Finalizers) != 0 {
				t.Fatalf("internalServiceImport finalizers, got %v, want no finalizer", internalSvcImport.Finalizers)
			}
		})
	}
}

// TestClearInternalServiceImportStatus tests the Reconciler.clearInternalServiceImportStatus method.
func TestClearInternalServiceImportStatus(t *testing.T) {
	testCases := []struct {
		name              string
		internalSvcImport *fleetnetv1alpha1.InternalServiceImport
	}{
		{
			name: "should remove cleanup finalizer (finalizer set) + should clear status",
			internalSvcImport: &fleetnetv1alpha1.InternalServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  hubNSForMemberA,
					Name:       internalSvcImportName,
					Finalizers: []string{internalSvcImportCleanupFinalizer},
				},
				Status: fulfilledServiceImport().Status,
			},
		},
		{
			name: "should remove cleanup finalizer (no finalizer) + should clear status",
			internalSvcImport: &fleetnetv1alpha1.InternalServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  hubNSForMemberA,
					Name:       internalSvcImportName,
					Finalizers: []string{internalSvcImportCleanupFinalizer},
				},
				Status: fulfilledServiceImport().Status,
			},
		},
	}

	ctx := context.Background()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeHubClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(tc.internalSvcImport).Build()
			reconciler := Reconciler{
				HubClient: fakeHubClient,
			}

			if res, err := reconciler.clearInternalServiceImportStatus(ctx, tc.internalSvcImport); !cmp.Equal(res, ctrl.Result{}) || err != nil {
				t.Fatalf("clearInternalServiceImportStatus(%+v) = %+v, %v, want %+v, no error", tc.internalSvcImport, res, err, ctrl.Result{})
			}

			internalSvcImport := &fleetnetv1alpha1.InternalServiceImport{}
			if err := fakeHubClient.Get(ctx, internalSvcImportAKey, internalSvcImport); err != nil {
				t.Fatalf("internalServiceImport Get(%+v), got %v, want no error", internalSvcImportAKey, err)
			}

			if len(internalSvcImport.Finalizers) != 0 {
				t.Fatalf("internalServiceImport finalizers, got %v, want no finalizer", internalSvcImport.Finalizers)
			}

			if diff := cmp.Diff(internalSvcImport.Status, fleetnetv1alpha1.ServiceImportStatus{}); diff != "" {
				t.Fatalf("internalServiceImport status mismatch (-got, +want)\n%s", diff)
			}
		})
	}
}

// TestRemoveInternalServiceImportCleanupFinalizer tests the Reconciler.removeInternalServiceImportCleanupFinalizer method.
func TestRemoveInternalServiceImportCleanupFinalizer(t *testing.T) {
	testCases := []struct {
		name              string
		internalSvcImport *fleetnetv1alpha1.InternalServiceImport
	}{
		{
			name: "should remove cleanup finalizer",
			internalSvcImport: &fleetnetv1alpha1.InternalServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  hubNSForMemberA,
					Name:       internalSvcImportName,
					Finalizers: []string{internalSvcImportCleanupFinalizer},
				},
			},
		},
	}

	ctx := context.Background()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeHubClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(tc.internalSvcImport).Build()
			reconciler := Reconciler{
				HubClient: fakeHubClient,
			}

			if err := reconciler.removeInternalServiceImportCleanupFinalizer(ctx, tc.internalSvcImport); err != nil {
				t.Fatalf("removeInternalServiceImportCleanupFinalizer(%+v), got %v, want no error", tc.internalSvcImport, err)
			}

			internalSvcImport := &fleetnetv1alpha1.InternalServiceImport{}
			if err := fakeHubClient.Get(ctx, internalSvcImportAKey, internalSvcImport); err != nil {
				t.Fatalf("internalServiceImport Get(%+v), got %v, want no error", internalSvcImportAKey, err)
			}

			if len(internalSvcImport.Finalizers) != 0 {
				t.Fatalf("internalServiceImport finalizers, got %v, want no finalizer", internalSvcImport.Finalizers)
			}
		})
	}
}

// TestAddInternalServiceImportCleanupFinalizer tests the Reconciler.addInternalServiceImportCleanupFinalizer method.
func TestAddInternalServiceImportCleanupFinalizer(t *testing.T) {
	testCases := []struct {
		name              string
		internalSvcImport *fleetnetv1alpha1.InternalServiceImport
	}{
		{
			name: "should add cleanup finalizer",
			internalSvcImport: &fleetnetv1alpha1.InternalServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMemberA,
					Name:      internalSvcImportName,
				},
			},
		},
	}

	ctx := context.Background()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeHubClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(tc.internalSvcImport).Build()
			reconciler := Reconciler{
				HubClient: fakeHubClient,
			}

			if err := reconciler.addInternalServiceImportCleanupFinalizer(ctx, tc.internalSvcImport); err != nil {
				t.Fatalf("removeInternalServiceImportCleanupFinalizer(%+v), got %v, want no error", tc.internalSvcImport, err)
			}

			internalSvcImport := &fleetnetv1alpha1.InternalServiceImport{}
			if err := fakeHubClient.Get(ctx, internalSvcImportAKey, internalSvcImport); err != nil {
				t.Fatalf("internalServiceImport Get(%+v), got %v, want no error", internalSvcImportAKey, err)
			}

			if !cmp.Equal(internalSvcImport.Finalizers, []string{internalSvcImportCleanupFinalizer}) {
				t.Fatalf("internalServiceImport finalizers, got %v, want %v", internalSvcImport.Finalizers, []string{internalSvcImportCleanupFinalizer})
			}
		})
	}
}

// TestAnnotateServiceImportWithServiceInUseByInfo tests the Reconciler.annotateServiceImportWithServiceInUseByInfo method.
func TestAnnotateServiceImportWithServiceInUseByInfo(t *testing.T) {
	testCases := []struct {
		name       string
		svcImport  *fleetnetv1alpha1.ServiceImport
		svcInUseBy *fleetnetv1alpha1.ServiceInUseBy
	}{
		{
			name: "should add service in use by info",
			svcImport: &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
			},
			svcInUseBy: fulfilledServiceInUseByAnnotation(),
		},
		{
			name: "should overwrite service in use by info",
			svcImport: &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
					Annotations: map[string]string{
						objectmeta.ServiceInUseByAnnotationKey: "xyz",
					},
				},
			},
			svcInUseBy: fulfilledServiceInUseByAnnotation(),
		},
	}

	ctx := context.Background()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeHubClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.svcImport).
				Build()
			reconciler := Reconciler{
				HubClient: fakeHubClient,
			}

			if err := reconciler.annotateServiceImportWithServiceInUseByInfo(ctx, tc.svcImport, tc.svcInUseBy); err != nil {
				t.Fatalf("annotateServiceImportWithServiceInUseByInfo(%+v, %+v), got %v, want no error", tc.svcImport, tc.svcInUseBy, err)
			}

			svcImport := &fleetnetv1alpha1.ServiceImport{}
			if err := fakeHubClient.Get(ctx, svcImportKey, svcImport); err != nil {
				t.Fatalf("serviceImport Get(%+v), got %v, want no error", svcImportKey, err)
			}

			wantSvcInUseByAnnotation := fulfilledSvcInUseByAnnotationString()
			if !cmp.Equal(svcImport.Annotations[objectmeta.ServiceInUseByAnnotationKey], wantSvcInUseByAnnotation) {
				t.Fatalf("serviceImport ServiceInUseBy annotation, got %s, want %s",
					svcImport.Annotations[objectmeta.ServiceInUseByAnnotationKey], wantSvcInUseByAnnotation)
			}
		})
	}
}

// TestFulfillInternalServiceImport tests the Reconciler.fulfillInternalServiceImport method.
func TestFulfillInternalServiceImport(t *testing.T) {
	testCases := []struct {
		name              string
		svcImport         *fleetnetv1alpha1.ServiceImport
		internalSvcImport *fleetnetv1alpha1.InternalServiceImport
	}{
		{
			name:      "should fulfill internalserviceimport",
			svcImport: fulfilledServiceImport(),
			internalSvcImport: &fleetnetv1alpha1.InternalServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNSForMemberA,
					Name:      internalSvcImportName,
				},
			},
		},
	}

	ctx := context.Background()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeHubClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.internalSvcImport).
				Build()
			reconciler := Reconciler{
				HubClient: fakeHubClient,
			}

			if err := reconciler.fulfillInternalServiceImport(ctx, tc.svcImport, tc.internalSvcImport); err != nil {
				t.Fatalf("fulfillInternalServiceImport(%+v, %+v), got %v, want no error", tc.svcImport, tc.internalSvcImport, err)
			}

			internalSvcImport := &fleetnetv1alpha1.InternalServiceImport{}
			if err := fakeHubClient.Get(ctx, internalSvcImportAKey, internalSvcImport); err != nil {
				t.Fatalf("internalServiceImport Get(%+v), got %v, want no error", internalSvcImportAKey, err)
			}

			if diff := cmp.Diff(internalSvcImport.Status, tc.svcImport.Status); diff != "" {
				t.Fatalf("internalServiceImport status mismatch (-got, +want)\n%s", diff)
			}
		})
	}
}

// TestClearServiceInUseByInfoFromServiceImport tests the Reconciler.clearServiceInUseByInfoFromServiceImport method.
func TestClearServiceInUseByInfoFromServiceImport(t *testing.T) {
	testCases := []struct {
		name      string
		svcImport *fleetnetv1alpha1.ServiceImport
	}{
		{
			name:      "should clear service in use by info + cleanup finalizer",
			svcImport: fulfilledServiceImport(),
		},
	}

	ctx := context.Background()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeHubClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.svcImport).
				Build()
			reconciler := Reconciler{
				HubClient: fakeHubClient,
			}

			if err := reconciler.clearServiceInUseByInfoFromServiceImport(ctx, tc.svcImport); err != nil {
				t.Fatalf("clearServiceInUseByInfoFromServiceImport(%+v), got %v, want no error", tc.svcImport, err)
			}

			svcImport := &fleetnetv1alpha1.ServiceImport{}
			if err := fakeHubClient.Get(ctx, svcImportKey, svcImport); err != nil {
				t.Fatalf("serviceImport Get(%+v), got %v, want no error", svcImportKey, err)
			}

			if len(svcImport.Finalizers) != 0 {
				t.Fatalf("serviceImport finalizers, got %v, want no finalizers", svcImport.Finalizers)
			}

			if _, ok := svcImport.Annotations[objectmeta.ServiceInUseByAnnotationKey]; ok {
				t.Fatalf("serviceImport ServiceInUseBy annotation is present")
			}
		})
	}
}
