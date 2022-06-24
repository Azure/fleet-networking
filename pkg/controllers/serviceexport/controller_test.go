/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package serviceexport

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetnetworkingapi "go.goms.io/fleet-networking/api/v1alpha1"
)

const (
	memberClusterID                   = "bravelion"
	memberUserNS                      = "work"
	hubNSForMember                    = "bravelion"
	svcExportName                     = "app"
	svcName                           = "app"
	altSvcExportName                  = "app2"
	altSvcName                        = "app2"
	newSvcExportStatusCondType        = "New"
	newSvcExportStatusCondDescription = "NewCond"
)

// ignoredCondFields are fields that should be ignored when comparing conditions.
var ignoredCondFields = cmpopts.IgnoreFields(metav1.Condition{}, "ObservedGeneration", "LastTransitionTime")

// getSvcExportValidCond returns a ServiceExportValid condition for exporting a valid Service.
func getSvcExportValidCond(userNS, svcName string) metav1.Condition {
	return metav1.Condition{
		Type:               string(fleetnetworkingapi.ServiceExportValid),
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             "ServiceIsValid",
		Message:            fmt.Sprintf("service %s/%s is valid for export", userNS, svcName),
	}
}

// getSvcExportConflictCond returns a ServiceExportConflict condition.
func getSvcExportConflictCond(userNS, svcName string) metav1.Condition {
	return metav1.Condition{
		Type:               string(fleetnetworkingapi.ServiceExportConflict),
		Status:             metav1.ConditionUnknown,
		LastTransitionTime: metav1.Now(),
		Reason:             "PendingConflictResolution",
		Message:            fmt.Sprintf("service %s/%s is pending export conflict resolution", userNS, svcName),
	}
}

// getSvcExportInvalidCondNotFound returns a ServiceExportValid condition for exporting a Service that is not found.
func getSvcExportInvalidCondNotFound(userNS, svcName string) metav1.Condition {
	return metav1.Condition{
		Type:               string(fleetnetworkingapi.ServiceExportValid),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             "ServiceNotFound",
		Message:            fmt.Sprintf("service %s/%s is not found", userNS, svcName),
	}
}

// getSvcExportInvalidCondIneligible returns a ServiceExportValid condition for exporting an ineligible Service.
func getSvcExportInvalidCondIneligible(userNS, svcName string) metav1.Condition {
	return metav1.Condition{
		Type:               string(fleetnetworkingapi.ServiceExportValid),
		Status:             metav1.ConditionStatus(corev1.ConditionFalse),
		LastTransitionTime: metav1.Now(),
		Reason:             "ServiceIneligible",
		Message:            fmt.Sprintf("service %s/%s is not eligible for export", userNS, svcName),
	}
}

// getSvcExportNewCond returns a ServiceCondition with a new type.
func getSvcExportNewCond() metav1.Condition {
	return metav1.Condition{
		Type:               newSvcExportStatusCondType,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             newSvcExportStatusCondDescription,
		Message:            newSvcExportStatusCondDescription,
	}
}

// TestIsSvcExportCleanupNeeded tests the isSvcExportCleanupNeeded function.
func TestIsSvcExportCleanupNeeded(t *testing.T) {
	timestamp := metav1.Now()
	testCases := []struct {
		svcExport *fleetnetworkingapi.ServiceExport
		want      bool
		name      string
	}{
		{
			svcExport: &fleetnetworkingapi.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcExportName,
				},
			},
			want: false,
			name: "should not clean up regular ServiceExport",
		},
		{
			svcExport: &fleetnetworkingapi.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:         memberUserNS,
					Name:              svcExportName,
					DeletionTimestamp: &timestamp,
				},
			},
			want: false,
			name: "should not clean up ServiceExport with only DeletionTimestamp set",
		},
		{
			svcExport: &fleetnetworkingapi.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  memberUserNS,
					Name:       svcExportName,
					Finalizers: []string{svcExportCleanupFinalizer},
				},
			},
			want: false,
			name: "should not clean up ServiceExport with cleanup finalizer only",
		},
		{
			svcExport: &fleetnetworkingapi.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:         memberUserNS,
					Name:              svcExportName,
					DeletionTimestamp: &timestamp,
					Finalizers:        []string{svcExportCleanupFinalizer},
				},
			},
			want: true,
			name: "should clean up ServiceExport with both cleanup finalizer and DeletionTimestamp set",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSvcExportCleanupNeeded(tc.svcExport); got != tc.want {
				t.Errorf("is svc export cleanup needed for svc export %+v, got %t, want %t", tc.svcExport, got, tc.want)
			}
		})
	}
}

// TestIsSvcDeleted tests the isSvcDeleted function.
func TestIsSvcDeleted(t *testing.T) {
	timestamp := metav1.Now()
	testCases := []struct {
		svc  *corev1.Service
		want bool
		name string
	}{
		{
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
			},
			want: false,
			name: "should not delete Service with DeletionTimestamp set",
		},
		{
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:         memberUserNS,
					Name:              svcName,
					DeletionTimestamp: &timestamp,
				},
			},
			want: true,
			name: "should delete Service with DeletionTimestamp set",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSvcDeleted(tc.svc); got != tc.want {
				t.Errorf("is svc deleted for svc %+v, got %t, want %t", tc.svc, got, tc.want)
			}
		})
	}
}

// TestIsSvcEligibleForExport tests the isSvcEligibleForExport function.
func TestIsSvcEligibleForExport(t *testing.T) {
	testCases := []struct {
		svc  *corev1.Service
		want bool
		name string
	}{
		{
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
					Ports: []corev1.ServicePort{
						{
							Port: 80,
						},
					},
				},
			},
			want: true,
			name: "should export regular Service",
		},
		{
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
				Spec: corev1.ServiceSpec{
					Type:         corev1.ServiceTypeExternalName,
					ExternalName: "example.com",
				},
			},
			want: false,
			name: "should not export ExternalName Service",
		},
		{
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
				Spec: corev1.ServiceSpec{
					Type:      corev1.ServiceTypeClusterIP,
					ClusterIP: "None",
					Ports: []corev1.ServicePort{
						{
							Port: 80,
						},
					},
				},
			},
			want: false,
			name: "should not export headless Service",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSvcEligibleForExport(tc.svc); got != tc.want {
				t.Errorf("svc eligibility for svc %+v, got %t, want %t", tc.svc, got, tc.want)
			}
		})
	}
}

// TestFormatInternalSvcExportName tests the formatInternalSvcExportName function.
func TestFormatInternalSvcExportName(t *testing.T) {
	svcExport := &fleetnetworkingapi.ServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      svcExportName,
		},
	}
	formattedName := formatInternalSvcExportName(svcExport)
	expectedFormattedName := "work-app"
	if formattedName != expectedFormattedName {
		t.Fatalf("formatted internal svc export name, got %s, want %s", formattedName, expectedFormattedName)
	}
}

// TestUpdateInternalSvcExport tests the updateInternalSvcExport function.
func TestUpdateInternalSvcExport(t *testing.T) {
	APIVersion := "core/v1"
	kind := "Service"
	resourceVersion := "0"
	UID := types.UID("example-uid")
	portAName := "portA"
	portA := 80
	targetPortA := intstr.FromInt(8080)
	nodePortA := 32000
	portBName := "portB"
	portB := 81
	targetPortB := intstr.FromString("targetPortB")
	nodePortB := 32001
	appProtocol := "example.com/custom"
	svc := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: APIVersion,
			Kind:       kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       memberUserNS,
			Name:            svcName,
			ResourceVersion: resourceVersion,
			UID:             UID,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Name:        portAName,
					Protocol:    corev1.ProtocolTCP,
					AppProtocol: &appProtocol,
					Port:        int32(portA),
					TargetPort:  targetPortA,
					NodePort:    int32(nodePortA),
				},
				{
					Name:        portBName,
					Protocol:    corev1.ProtocolTCP,
					AppProtocol: &appProtocol,
					Port:        int32(portB),
					TargetPort:  targetPortB,
					NodePort:    int32(nodePortB),
				},
			},
		},
	}
	internalSvcExport := &fleetnetworkingapi.InternalServiceExport{}

	updateInternalSvcExport(memberClusterID, svc, internalSvcExport)

	expectedSvcPorts := []fleetnetworkingapi.ServicePort{
		{
			Name:        portAName,
			Protocol:    corev1.ProtocolTCP,
			AppProtocol: &appProtocol,
			Port:        int32(portA),
			TargetPort:  targetPortA,
		},
		{
			Name:        portBName,
			Protocol:    corev1.ProtocolTCP,
			AppProtocol: &appProtocol,
			Port:        int32(portB),
			TargetPort:  targetPortB,
		},
	}
	if !cmp.Equal(internalSvcExport.Spec.Ports, expectedSvcPorts) {
		t.Fatalf("svc ports, got %+v, want %+v", internalSvcExport.Spec.Ports, expectedSvcPorts)
	}

	expectedSvcReference := fleetnetworkingapi.ExportedObjectReference{
		ClusterID:       memberClusterID,
		APIVersion:      APIVersion,
		Kind:            kind,
		Namespace:       memberUserNS,
		Name:            svcName,
		ResourceVersion: resourceVersion,
		UID:             UID,
	}
	if !cmp.Equal(internalSvcExport.Spec.ServiceReference, expectedSvcReference) {
		t.Fatalf("svc ref, got %+v, want %+v", internalSvcExport.Spec.ServiceReference, expectedSvcReference)
	}
}

// TestIsSvcChanged tests the isSvcChanged function.
func TestIsSvcChanged(t *testing.T) {
	timestamp := metav1.Now()
	defaultPort := 80
	newPort := 81
	newTargetPort := intstr.FromInt(8081)
	testCases := []struct {
		oldSvc *corev1.Service
		newSvc *corev1.Service
		want   bool
		name   string
	}{
		{
			oldSvc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
					Ports: []corev1.ServicePort{
						{
							Port: int32(defaultPort),
						},
					},
				},
			},
			newSvc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:         memberUserNS,
					Name:              svcName,
					DeletionTimestamp: &timestamp,
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
					Ports: []corev1.ServicePort{
						{
							Port: int32(defaultPort),
						},
					},
				},
			},
			want: true,
			name: "should report change when new svc is deleted",
		},
		{
			oldSvc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
				Spec: corev1.ServiceSpec{
					Type:      corev1.ServiceTypeClusterIP,
					ClusterIP: "None",
				},
			},
			newSvc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
				Spec: corev1.ServiceSpec{
					Type:      corev1.ServiceTypeClusterIP,
					ClusterIP: "",
				},
			},
			want: true,
			name: "should report change when svc export eligibility changes (invalid -> valid)",
		},
		{
			oldSvc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
				Spec: corev1.ServiceSpec{
					Type:      corev1.ServiceTypeClusterIP,
					ClusterIP: "1.2.3.4",
				},
			},
			newSvc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
				Spec: corev1.ServiceSpec{
					Type:         corev1.ServiceTypeExternalName,
					ExternalName: "example.com",
				},
			},
			want: true,
			name: "should report change when svc export eligibility changes (valid -> invalid)",
		},
		{
			oldSvc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
					UID:       "uid",
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
					Ports: []corev1.ServicePort{
						{
							Port: int32(defaultPort),
						},
					},
				},
			},
			newSvc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
					UID:       "a-different-uid",
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
					Ports: []corev1.ServicePort{
						{
							Port: int32(defaultPort),
						},
					},
				},
			},
			want: true,
			name: "should report change when svc UID changes",
		},
		{
			oldSvc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
					Ports: []corev1.ServicePort{
						{
							Port: int32(defaultPort),
						},
						{
							Port:       int32(newPort),
							TargetPort: newTargetPort,
						},
					},
				},
			},
			newSvc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
					Ports: []corev1.ServicePort{
						{
							Port: int32(defaultPort),
						},
						{
							Port: int32(newPort),
						},
					},
				},
			},
			want: true,
			name: "should report change when svc ports change (target port)",
		},
		{
			oldSvc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
					Ports: []corev1.ServicePort{
						{
							Port: int32(defaultPort),
						},
					},
				},
			},
			newSvc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
					Ports: []corev1.ServicePort{
						{
							Port: int32(defaultPort),
						},
						{
							Port: int32(newPort),
						},
					},
				},
			},
			want: true,
			name: "should report change when svc ports change (new port)",
		},
		{
			oldSvc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
					Ports: []corev1.ServicePort{
						{
							Port: int32(defaultPort),
						},
						{
							Port:       int32(newPort),
							TargetPort: newTargetPort,
						},
					},
				},
			},
			newSvc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
					Ports: []corev1.ServicePort{
						{
							Port: int32(defaultPort),
						},
					},
				},
			},
			want: true,
			name: "should report change when svc ports change (removed port)",
		},
		{
			oldSvc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
					Ports: []corev1.ServicePort{
						{
							Port: int32(defaultPort),
						},
					},
					Selector: map[string]string{
						"app": "mysql",
					},
				},
			},
			newSvc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
					Ports: []corev1.ServicePort{
						{
							Port: int32(defaultPort),
						},
					},
					Selector: map[string]string{
						"app": "redis",
					},
				},
			},
			want: false,
			name: "should not report change when svc spec has no significant change (selector)",
		},
		{
			oldSvc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
					Ports: []corev1.ServicePort{
						{
							Port:     int32(defaultPort),
							NodePort: 32000,
						},
					},
				},
			},
			newSvc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: memberUserNS,
					Name:      svcName,
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
					Ports: []corev1.ServicePort{
						{
							Port:     int32(defaultPort),
							NodePort: 32001,
						},
					},
				},
			},
			want: false,
			name: "should not report change when svc spec has no significant change (node port)",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSvcChanged(tc.oldSvc, tc.newSvc); got != tc.want {
				t.Errorf("is svc changed (old svc %+v -> new svc %+v), got %t, want %t", tc.oldSvc, tc.newSvc, got, tc.want)
			}
		})
	}
}

// TestMarkSvcExportAsInvalid tests the *SvcExportReconciler.markSvcExportAsInvalidIneligible and
// *SvcExportReconciler.markSvcExportAsInvalidNotFound methods.
func TestMarkSvcExportAsInvalid(t *testing.T) {
	svcExportNew := &fleetnetworkingapi.ServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      svcExportName,
		},
	}
	svcExportValid := &fleetnetworkingapi.ServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      altSvcExportName,
		},
		Status: fleetnetworkingapi.ServiceExportStatus{
			Conditions: []metav1.Condition{
				getSvcExportValidCond(memberUserNS, svcName),
				getSvcExportNewCond(),
			},
		},
	}

	fakeMemberClient := fakeclient.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(svcExportNew, svcExportValid).
		Build()
	fakeHubClient := fakeclient.NewClientBuilder().Build()
	reconciler := SvcExportReconciler{
		memberClusterID: memberClusterID,
		memberClient:    fakeMemberClient,
		hubClient:       fakeHubClient,
		hubNS:           hubNSForMember,
	}
	ctx := context.Background()

	t.Run("should mark a new svc export as invalid (ineligible)", func(t *testing.T) {
		err := reconciler.markSvcExportAsInvalidSvcIneligible(ctx, svcExportNew)
		if err != nil {
			t.Errorf("failed to mark svc export")
		}

		var updatedSvcExport = &fleetnetworkingapi.ServiceExport{}
		err = fakeMemberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcExportName}, updatedSvcExport)
		if err != nil {
			t.Errorf("faile to get updated svc export")
		}
		conds := updatedSvcExport.Status.Conditions
		expectedConds := []metav1.Condition{getSvcExportInvalidCondIneligible(memberUserNS, svcName)}
		if !cmp.Equal(conds, expectedConds, ignoredCondFields) {
			t.Errorf("svc export conditions, got %+v, want %+v", conds, expectedConds)
		}
	})

	t.Run("should mark a valid svc export as invalid (ineligible)", func(t *testing.T) {
		err := reconciler.markSvcExportAsInvalidSvcIneligible(ctx, svcExportValid)
		if err != nil {
			t.Errorf("failed to mark svc export")
		}

		var updatedSvcExport = &fleetnetworkingapi.ServiceExport{}
		err = fakeMemberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: altSvcExportName}, updatedSvcExport)
		if err != nil {
			t.Errorf("faile to get updated svc export")
		}
		conds := updatedSvcExport.Status.Conditions
		expectedConds := []metav1.Condition{
			getSvcExportNewCond(),
			getSvcExportInvalidCondIneligible(memberUserNS, altSvcExportName),
		}
		if !cmp.Equal(conds, expectedConds, ignoredCondFields) {
			t.Errorf("svc export conditions, got %+v, want %+v", conds, expectedConds)
		}
	})

	// Reset the fake client.
	fakeMemberClient = fakeclient.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(svcExportNew, svcExportValid).
		Build()
	reconciler = SvcExportReconciler{
		memberClusterID: memberClusterID,
		memberClient:    fakeMemberClient,
		hubClient:       fakeHubClient,
		hubNS:           hubNSForMember,
	}

	t.Run("should mark a new svc export as invalid (not found)", func(t *testing.T) {
		err := reconciler.markSvcExportAsInvalidSvcNotFound(ctx, svcExportNew)
		if err != nil {
			t.Errorf("failed to mark svc export")
		}

		var updatedSvcExport = &fleetnetworkingapi.ServiceExport{}
		err = fakeMemberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcExportName}, updatedSvcExport)
		if err != nil {
			t.Errorf("faile to get updated svc export")
		}
		conds := updatedSvcExport.Status.Conditions
		expectedConds := []metav1.Condition{
			getSvcExportInvalidCondNotFound(memberUserNS, svcExportName),
		}
		if !cmp.Equal(conds, expectedConds, ignoredCondFields) {
			t.Errorf("svc export conditions, got %+v, want %+v", conds, expectedConds)
		}
	})

	t.Run("should mark a valid svc export as invalid (not found)", func(t *testing.T) {
		err := reconciler.markSvcExportAsInvalidSvcNotFound(ctx, svcExportValid)
		if err != nil {
			t.Errorf("failed to mark svc export")
		}

		var updatedSvcExport = &fleetnetworkingapi.ServiceExport{}
		err = fakeMemberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: altSvcExportName}, updatedSvcExport)
		if err != nil {
			t.Errorf("faile to get updated svc export")
		}
		conds := updatedSvcExport.Status.Conditions
		expectedConds := []metav1.Condition{
			getSvcExportNewCond(),
			getSvcExportInvalidCondNotFound(memberUserNS, altSvcExportName),
		}
		if !cmp.Equal(conds, expectedConds, ignoredCondFields) {
			t.Errorf("svc export conditions, got %+v, want %+v", conds, expectedConds)
		}
	})
}

// TestMarkSvcExportAsValid tests the *SvcExportReconciler.markSvcExportAsValid method.
func TestMarkSvcExportAsValid(t *testing.T) {
	svcExportNew := &fleetnetworkingapi.ServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      svcExportName,
		},
	}
	svcExportInvalid := &fleetnetworkingapi.ServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      altSvcExportName,
		},
		Status: fleetnetworkingapi.ServiceExportStatus{
			Conditions: []metav1.Condition{
				getSvcExportInvalidCondNotFound(memberUserNS, altSvcExportName),
				getSvcExportNewCond(),
			},
		},
	}

	fakeMemberClient := fakeclient.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(svcExportNew, svcExportInvalid).
		Build()
	fakeHubClient := fakeclient.NewClientBuilder().Build()
	reconciler := SvcExportReconciler{
		memberClusterID: memberClusterID,
		memberClient:    fakeMemberClient,
		hubClient:       fakeHubClient,
		hubNS:           hubNSForMember,
	}
	ctx := context.Background()

	t.Run("should mark a new svc export as valid", func(t *testing.T) {
		err := reconciler.markSvcExportAsValid(ctx, svcExportNew)
		if err != nil {
			t.Errorf("failed to mark svc export")
		}

		var updatedSvcExport = &fleetnetworkingapi.ServiceExport{}
		err = fakeMemberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcExportName}, updatedSvcExport)
		if err != nil {
			t.Errorf("faile to get updated svc export")
		}
		conds := updatedSvcExport.Status.Conditions
		expectedConds := []metav1.Condition{
			getSvcExportValidCond(memberUserNS, svcExportName),
			getSvcExportConflictCond(memberUserNS, svcExportName),
		}
		if !cmp.Equal(conds, expectedConds, ignoredCondFields) {
			t.Errorf("svc export conditions, got %+v, want %+v", conds, expectedConds)
		}
	})

	t.Run("should mark an invalid svc export as valid", func(t *testing.T) {
		err := reconciler.markSvcExportAsValid(ctx, svcExportInvalid)
		if err != nil {
			t.Errorf("failed to mark svc export: %v", err)
		}

		var updatedSvcExport = &fleetnetworkingapi.ServiceExport{}
		err = fakeMemberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: altSvcExportName}, updatedSvcExport)
		if err != nil {
			t.Errorf("faile to get updated svc export: %v", err)
		}
		conds := updatedSvcExport.Status.Conditions
		expectedConds := []metav1.Condition{
			getSvcExportNewCond(),
			getSvcExportValidCond(memberUserNS, altSvcExportName),
			getSvcExportConflictCond(memberUserNS, altSvcExportName),
		}
		if !cmp.Equal(conds, expectedConds, ignoredCondFields) {
			t.Errorf("svc export conditions, got %+v, want %+v", conds, expectedConds)
		}
	})
}

// TestRemoveSvcExportCleanupFinalizer tests the *SvcExportReconciler.removeSvcExportCleanupFinalizer method.
func TestRemoveSvcExportCleanupFinalizer(t *testing.T) {
	svcExportWithCleanupFinalizer := &fleetnetworkingapi.ServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  memberUserNS,
			Name:       svcExportName,
			Finalizers: []string{svcExportCleanupFinalizer},
		},
	}

	fakeMemberClient := fakeclient.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(svcExportWithCleanupFinalizer).
		Build()
	fakeHubClient := fakeclient.NewClientBuilder().Build()
	reconciler := SvcExportReconciler{
		memberClusterID: memberClusterID,
		memberClient:    fakeMemberClient,
		hubClient:       fakeHubClient,
		hubNS:           hubNSForMember,
	}
	ctx := context.Background()

	res, err := reconciler.removeSvcExportCleanupFinalizer(ctx, svcExportWithCleanupFinalizer)
	if err != nil || !cmp.Equal(res, ctrl.Result{}) {
		t.Errorf("failed to remove cleanup finalizer: %v; result: %v", err, res)
	}

	var updatedSvcExport = &fleetnetworkingapi.ServiceExport{}
	err = fakeMemberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcExportName}, updatedSvcExport)
	if err != nil {
		t.Errorf("failed to get updated svc export: %v", err)
	}

	if controllerutil.ContainsFinalizer(updatedSvcExport, svcExportCleanupFinalizer) {
		t.Error("svc export cleanup finalizer is not removed")
	}
}

// TestAddSvcExportCleanupFinalizer tests the *SvcExportReconciler.addSvcExportCleanupFinalizer method.
func TestAddSvcExportCleanupFinalizer(t *testing.T) {
	svcExportWithoutCleanupFinalizer := &fleetnetworkingapi.ServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      svcExportName,
		},
	}

	fakeMemberClient := fakeclient.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(svcExportWithoutCleanupFinalizer).
		Build()
	fakeHubClient := fakeclient.NewClientBuilder().Build()
	reconciler := SvcExportReconciler{
		memberClusterID: memberClusterID,
		memberClient:    fakeMemberClient,
		hubClient:       fakeHubClient,
		hubNS:           hubNSForMember,
	}
	ctx := context.Background()

	err := reconciler.addSvcExportCleanupFinalizer(ctx, svcExportWithoutCleanupFinalizer)
	if err != nil {
		t.Errorf("failed to add cleanup finalizer: %v", err)
	}

	var updatedSvcExport = &fleetnetworkingapi.ServiceExport{}
	err = fakeMemberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcExportName}, updatedSvcExport)
	if err != nil {
		t.Errorf("failed to get updated svc export: %v", err)
	}

	if !controllerutil.ContainsFinalizer(updatedSvcExport, svcExportCleanupFinalizer) {
		t.Error("svc export cleanup finalizer is not added")
	}
}

// TestUnexportSvc tests the *SvcExportReconciler.unexportSvc method.
func TestUnexportSvc(t *testing.T) {
	internalSvcExportName := fmt.Sprintf("%s-%s", memberUserNS, svcExportName)
	svcExport := &fleetnetworkingapi.ServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  memberUserNS,
			Name:       svcExportName,
			Finalizers: []string{svcExportCleanupFinalizer},
		},
	}
	altSvcExport := &fleetnetworkingapi.ServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  memberUserNS,
			Name:       altSvcExportName,
			Finalizers: []string{svcExportCleanupFinalizer},
		},
	}
	internalSvcExport := &fleetnetworkingapi.InternalServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: hubNSForMember,
			Name:      internalSvcExportName,
		},
	}

	fakeMemberClient := fakeclient.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(svcExport, altSvcExport).
		Build()
	fakeHubClient := fakeclient.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(internalSvcExport).
		Build()
	reconciler := SvcExportReconciler{
		memberClusterID: memberClusterID,
		memberClient:    fakeMemberClient,
		hubClient:       fakeHubClient,
		hubNS:           hubNSForMember,
	}
	ctx := context.Background()

	t.Run("should unexport svc", func(t *testing.T) {
		res, err := reconciler.unexportSvc(ctx, svcExport)
		if err != nil || !cmp.Equal(res, ctrl.Result{}) {
			t.Errorf("failed to unexport svc: %v; result: %v", err, res)
		}

		var updatedSvcExport = &fleetnetworkingapi.ServiceExport{}
		err = fakeMemberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcExportName}, updatedSvcExport)
		if err != nil {
			t.Errorf("failed to get updated svc export: %v", err)
		}
		if controllerutil.ContainsFinalizer(updatedSvcExport, svcExportCleanupFinalizer) {
			t.Error("svc export cleanup finalizer is not removed")
		}

		var deletedInternalSvcExport = &fleetnetworkingapi.InternalServiceExport{}
		err = fakeHubClient.Get(ctx, types.NamespacedName{Namespace: hubNSForMember, Name: internalSvcExportName}, deletedInternalSvcExport)
		if !errors.IsNotFound(err) {
			t.Error("internal svc export is not removed")
		}
	})

	t.Run("should unexport partially exported svc", func(t *testing.T) {
		res, err := reconciler.unexportSvc(ctx, altSvcExport)
		if err != nil || !cmp.Equal(res, ctrl.Result{}) {
			t.Errorf("failed to unexport svc: %v; result: %v", err, res)
		}

		var updatedSvcExport = &fleetnetworkingapi.ServiceExport{}
		err = fakeMemberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: altSvcExportName}, updatedSvcExport)
		if err != nil {
			t.Errorf("failed to get updated svc export: %v", err)
		}
		if controllerutil.ContainsFinalizer(updatedSvcExport, svcExportCleanupFinalizer) {
			t.Error("svc export cleanup finalizer is not removed")
		}
	})
}
