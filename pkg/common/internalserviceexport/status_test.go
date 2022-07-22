package internalserviceexport

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

const (
	testNamespace = "member-cluster-a"
	testName      = "my-ns-my-svc"
)

var (
	unconflictedCondition = metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportConflict),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: 0,
		LastTransitionTime: metav1.Now(),
		Reason:             "NoConflictFound",
		Message:            "service my-ns/my-svc is exported without conflict",
	}

	conflictedCondition = metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportConflict),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: 0,
		LastTransitionTime: metav1.Now(),
		Reason:             "ConflictFound",
		Message:            "service my-ns/my-svc is in conflict with other exported services",
	}
	options = []cmp.Option{
		cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime"),
	}
)

func serviceScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	if err := fleetnetv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}
	return scheme
}

func TestUpdateStatus(t *testing.T) {
	tests := []struct {
		name       string
		status     fleetnetv1alpha1.InternalServiceExportStatus
		conflict   bool
		withRetry  bool
		wantStatus fleetnetv1alpha1.InternalServiceExportStatus
	}{
		{
			name: "no condition change",
			status: fleetnetv1alpha1.InternalServiceExportStatus{
				Conditions: []metav1.Condition{
					conflictedCondition,
				},
			},
			conflict:  true,
			withRetry: false,
			wantStatus: fleetnetv1alpha1.InternalServiceExportStatus{
				Conditions: []metav1.Condition{
					conflictedCondition,
				},
			},
		},
		{
			name: "condition is changed",
			status: fleetnetv1alpha1.InternalServiceExportStatus{
				Conditions: []metav1.Condition{
					conflictedCondition,
				},
			},
			conflict:  false,
			withRetry: false,
			wantStatus: fleetnetv1alpha1.InternalServiceExportStatus{
				Conditions: []metav1.Condition{
					unconflictedCondition,
				},
			},
		},
		{
			name: "update condition with retry",
			status: fleetnetv1alpha1.InternalServiceExportStatus{
				Conditions: []metav1.Condition{
					conflictedCondition,
				},
			},
			conflict:  false,
			withRetry: true,
			wantStatus: fleetnetv1alpha1.InternalServiceExportStatus{
				Conditions: []metav1.Condition{
					unconflictedCondition,
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			internalServiceExport := fleetnetv1alpha1.InternalServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testName,
					Namespace: testNamespace,
				},
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Ports: []fleetnetv1alpha1.ServicePort{
						{
							Name:       "portA",
							Protocol:   "TCP",
							Port:       8080,
							TargetPort: intstr.IntOrString{IntVal: 8080},
						},
					},
					ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
						ClusterID:       "member-cluster-b",
						Kind:            "Service",
						Namespace:       "my-ns",
						Name:            "my-svc",
						ResourceVersion: "0",
						Generation:      0,
						UID:             "0",
						NamespacedName:  "my-ns/my-svc",
					},
				},
			}
			internalServiceExport.Status = tc.status
			objects := []client.Object{&internalServiceExport}
			fakeClient := fake.NewClientBuilder().
				WithScheme(serviceScheme(t)).
				WithObjects(objects...).
				Build()
			ctx := context.Background()
			if err := UpdateStatus(ctx, fakeClient, &internalServiceExport, tc.conflict, tc.withRetry); err != nil {
				t.Fatalf("failed to update status: %v", err)
			}

			got := fleetnetv1alpha1.InternalServiceExport{}
			name := types.NamespacedName{Namespace: testNamespace, Name: testName}
			if err := fakeClient.Get(ctx, name, &got); err != nil {
				t.Fatalf("InternalServiceExport Get() got error %v, want no error", err)
			}

			if diff := cmp.Diff(tc.wantStatus, got.Status, options...); diff != "" {
				t.Errorf("InternalServiceExport status mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestUpdateStatusWithError(t *testing.T) {
	internalServiceExport := fleetnetv1alpha1.InternalServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testName,
			Namespace: testNamespace,
		},
		Spec: fleetnetv1alpha1.InternalServiceExportSpec{
			Ports: []fleetnetv1alpha1.ServicePort{
				{
					Name:       "portA",
					Protocol:   "TCP",
					Port:       8080,
					TargetPort: intstr.IntOrString{IntVal: 8080},
				},
			},
			ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
				ClusterID:       "member-cluster-b",
				Kind:            "Service",
				Namespace:       "my-ns",
				Name:            "my-svc",
				ResourceVersion: "0",
				Generation:      0,
				UID:             "0",
				NamespacedName:  "my-ns/my-svc",
			},
		},
		Status: fleetnetv1alpha1.InternalServiceExportStatus{
			Conditions: []metav1.Condition{
				conflictedCondition,
			},
		},
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(serviceScheme(t)).
		Build()
	ctx := context.Background()
	if err := UpdateStatus(ctx, fakeClient, &internalServiceExport, false, true); !errors.IsNotFound(err) {
		t.Fatalf("UpdateStatus() got no err, want not found error: %v", err)
	}
}
