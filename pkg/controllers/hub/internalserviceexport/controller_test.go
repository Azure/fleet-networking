package internalserviceexport

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

const (
	testName                  = "my-ns-my-svc"
	testServiceName           = "my-svc"
	testNamespace             = "my-ns"
	testMemberNamespace       = "member-1-ns"
	testClusterID             = "member-1"
	fleetNetworkingAPIVersion = "networking.fleet.azure.com/v1alpha1"
)

var (
	serviceImportType = metav1.TypeMeta{
		Kind:       "ServiceImport",
		APIVersion: fleetNetworkingAPIVersion,
	}

	InternalServiceExportType = metav1.TypeMeta{
		Kind:       "InternalServiceExport",
		APIVersion: fleetNetworkingAPIVersion,
	}
)

func internalServiceExportScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	if err := fleetnetv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}
	return scheme
}

func internalServiceExportForTest() *fleetnetv1alpha1.InternalServiceExport {
	appProtocol := "app-protocol"
	return &fleetnetv1alpha1.InternalServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testName,
			Namespace: testMemberNamespace,
		},
		Spec: fleetnetv1alpha1.InternalServiceExportSpec{
			Ports: []fleetnetv1alpha1.ServicePort{
				{
					Name:        "portA",
					Protocol:    "TCP",
					Port:        8080,
					AppProtocol: &appProtocol,
					TargetPort:  intstr.IntOrString{StrVal: "8080"},
				},
				{
					Name:       "portB",
					Protocol:   "TCP",
					Port:       9090,
					TargetPort: intstr.IntOrString{StrVal: "9090"},
				},
			},
			ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
				ClusterID:       testClusterID,
				Kind:            "Service",
				Namespace:       testNamespace,
				Name:            testServiceName,
				ResourceVersion: "0",
				Generation:      0,
				UID:             "0",
			},
		},
	}
}

func internalServiceExportReconciler(client client.Client) *Reconciler {
	return &Reconciler{
		Client: client,
		Scheme: client.Scheme(),
	}
}

func unconflictedServiceExportConflictCondition(svcNamespace string, svcName string) metav1.Condition {
	return metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportConflict),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: 0,
		LastTransitionTime: metav1.Now(),
		Reason:             conditionReasonNoConflictFound,
		Message:            fmt.Sprintf("service %s/%s is exported without conflict", svcNamespace, svcName),
	}
}

func conflictedServiceExportConflictCondition(svcNamespace string, svcName string) metav1.Condition {
	return metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportConflict),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: 0,
		LastTransitionTime: metav1.Now(),
		Reason:             conditionReasonConflictFound,
		Message:            fmt.Sprintf("service %s/%s is in conflict with other exported services", svcNamespace, svcName),
	}
}

func TestReconciler_NotFound(t *testing.T) {
	ctx := context.Background()
	fakeClient := fake.NewClientBuilder().
		WithScheme(internalServiceExportScheme(t)).
		Build()

	r := internalServiceExportReconciler(fakeClient)
	name := types.NamespacedName{
		Namespace: testMemberNamespace,
		Name:      testName,
	}
	got, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: name})
	if err != nil {
		t.Fatalf("failed to reconcile: %v", err)
	}
	want := ctrl.Result{}
	if !cmp.Equal(got, want) {
		t.Errorf("Reconcile() = %+v, want %+v", got, want)
	}
}

func TestHandleDelete(t *testing.T) {
	appProtocol := "app-protocol"
	importServicePorts := []fleetnetv1alpha1.ServicePort{
		{
			Name:        "portA",
			Protocol:    "TCP",
			Port:        8080,
			AppProtocol: &appProtocol,
			TargetPort:  intstr.IntOrString{IntVal: 8080},
		},
		{
			Name:       "portB",
			Protocol:   "TCP",
			Port:       9090,
			TargetPort: intstr.IntOrString{IntVal: 9090},
		},
	}
	newImportServicePorts := []fleetnetv1alpha1.ServicePort{
		{
			Name:        "portA",
			Protocol:    "TCP",
			Port:        7777,
			AppProtocol: &appProtocol,
		},
	}
	tests := []struct {
		name                           string
		existingInternalSvcExports     []fleetnetv1alpha1.InternalServiceExport
		serviceImport                  *fleetnetv1alpha1.ServiceImport
		wantServiceImport              *fleetnetv1alpha1.ServiceImport
		wantExistingInternalSvcExports []fleetnetv1alpha1.InternalServiceExport
	}{
		{
			name: "serviceImport has been deleted",
		},
		{
			name: "the deleting internalServiceExport is the last exported service",
			serviceImport: &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testNamespace,
				},
				Status: fleetnetv1alpha1.ServiceImportStatus{
					Ports: importServicePorts,
					Clusters: []fleetnetv1alpha1.ClusterStatus{
						{
							Cluster: testClusterID,
						},
					},
				},
			},
		},
		{
			name: "there is another serviceExport with the same spec as the deleting one",
			existingInternalSvcExports: []fleetnetv1alpha1.InternalServiceExport{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testName,
						Namespace: "member-2-ns",
					},
					Spec: fleetnetv1alpha1.InternalServiceExportSpec{
						Ports: importServicePorts,
						ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
							ClusterID:       "member-2",
							Kind:            "Service",
							Namespace:       testNamespace,
							Name:            testServiceName,
							ResourceVersion: "0",
							Generation:      0,
							UID:             "0",
						},
					},
					Status: fleetnetv1alpha1.InternalServiceExportStatus{
						Conditions: []metav1.Condition{
							unconflictedServiceExportConflictCondition(testNamespace, testServiceName),
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testName,
						Namespace: "member-3-ns",
					},
					Spec: fleetnetv1alpha1.InternalServiceExportSpec{
						Ports: newImportServicePorts,
						ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
							ClusterID:       "member-3",
							Kind:            "Service",
							Namespace:       testNamespace,
							Name:            testServiceName,
							ResourceVersion: "0",
							Generation:      0,
							UID:             "0",
						},
					},
					Status: fleetnetv1alpha1.InternalServiceExportStatus{
						Conditions: []metav1.Condition{
							conflictedServiceExportConflictCondition(testNamespace, testServiceName),
						},
					},
				},
			},
			serviceImport: &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testNamespace,
				},
				Status: fleetnetv1alpha1.ServiceImportStatus{
					Ports: importServicePorts,
					Clusters: []fleetnetv1alpha1.ClusterStatus{
						{
							Cluster: testClusterID,
						},
						{
							Cluster: "member-2",
						},
					},
					Type: fleetnetv1alpha1.ClusterSetIP,
				},
			},
			wantServiceImport: &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testNamespace,
				},
				TypeMeta: serviceImportType,
				Status: fleetnetv1alpha1.ServiceImportStatus{
					Ports: importServicePorts,
					Clusters: []fleetnetv1alpha1.ClusterStatus{
						{
							Cluster: "member-2",
						},
					},
					Type: fleetnetv1alpha1.ClusterSetIP,
				},
			},
			wantExistingInternalSvcExports: []fleetnetv1alpha1.InternalServiceExport{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testName,
						Namespace: "member-2-ns",
					},
					TypeMeta: InternalServiceExportType,
					Spec: fleetnetv1alpha1.InternalServiceExportSpec{
						Ports: importServicePorts,
						ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
							ClusterID:       "member-2",
							Kind:            "Service",
							Namespace:       testNamespace,
							Name:            testServiceName,
							ResourceVersion: "0",
							Generation:      0,
							UID:             "0",
						},
					},
					Status: fleetnetv1alpha1.InternalServiceExportStatus{
						Conditions: []metav1.Condition{
							unconflictedServiceExportConflictCondition(testNamespace, testServiceName),
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testName,
						Namespace: "member-3-ns",
					},
					Spec: fleetnetv1alpha1.InternalServiceExportSpec{
						Ports: newImportServicePorts,
						ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
							ClusterID:       "member-3",
							Kind:            "Service",
							Namespace:       testNamespace,
							Name:            testServiceName,
							ResourceVersion: "0",
							Generation:      0,
							UID:             "0",
						},
					},
					TypeMeta: InternalServiceExportType,
					Status: fleetnetv1alpha1.InternalServiceExportStatus{
						Conditions: []metav1.Condition{
							conflictedServiceExportConflictCondition(testNamespace, testServiceName),
						},
					},
				},
			},
		},
		{
			name: "existing serviceExports have different spec as the deleting one",
			existingInternalSvcExports: []fleetnetv1alpha1.InternalServiceExport{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testName,
						Namespace: "member-2-ns",
					},
					Spec: fleetnetv1alpha1.InternalServiceExportSpec{
						Ports: newImportServicePorts,
						ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
							ClusterID:       "member-2",
							Kind:            "Service",
							Namespace:       testNamespace,
							Name:            testServiceName,
							ResourceVersion: "0",
							Generation:      0,
							UID:             "0",
						},
					},
					TypeMeta: InternalServiceExportType,
					Status: fleetnetv1alpha1.InternalServiceExportStatus{
						Conditions: []metav1.Condition{
							conflictedServiceExportConflictCondition(testNamespace, testServiceName),
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testName,
						Namespace: "member-3-ns",
					},
					Spec: fleetnetv1alpha1.InternalServiceExportSpec{
						Ports: newImportServicePorts,
						ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
							ClusterID:       "member-3",
							Kind:            "Service",
							Namespace:       testNamespace,
							Name:            testServiceName,
							ResourceVersion: "0",
							Generation:      0,
							UID:             "0",
						},
					},
					TypeMeta: InternalServiceExportType,
					Status: fleetnetv1alpha1.InternalServiceExportStatus{
						Conditions: []metav1.Condition{
							conflictedServiceExportConflictCondition(testNamespace, testServiceName),
						},
					},
				},
			},
			serviceImport: &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testNamespace,
				},
				Status: fleetnetv1alpha1.ServiceImportStatus{
					Ports: importServicePorts,
					Clusters: []fleetnetv1alpha1.ClusterStatus{
						{
							Cluster: testClusterID,
						},
					},
					Type: fleetnetv1alpha1.ClusterSetIP,
				},
			},
			wantServiceImport: &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testNamespace,
				},
				TypeMeta: serviceImportType,
				Status: fleetnetv1alpha1.ServiceImportStatus{
					Ports: newImportServicePorts,
					Clusters: []fleetnetv1alpha1.ClusterStatus{
						{
							Cluster: "member-2",
						},
						{
							Cluster: "member-3",
						},
					},
					Type: fleetnetv1alpha1.ClusterSetIP,
				},
			},
			wantExistingInternalSvcExports: []fleetnetv1alpha1.InternalServiceExport{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testName,
						Namespace: "member-2-ns",
					},
					TypeMeta: InternalServiceExportType,
					Spec: fleetnetv1alpha1.InternalServiceExportSpec{
						Ports: newImportServicePorts,
						ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
							ClusterID:       "member-2",
							Kind:            "Service",
							Namespace:       testNamespace,
							Name:            testServiceName,
							ResourceVersion: "0",
							Generation:      0,
							UID:             "0",
						},
					},
					Status: fleetnetv1alpha1.InternalServiceExportStatus{
						Conditions: []metav1.Condition{
							unconflictedServiceExportConflictCondition(testNamespace, testServiceName),
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testName,
						Namespace: "member-3-ns",
					},
					TypeMeta: InternalServiceExportType,
					Spec: fleetnetv1alpha1.InternalServiceExportSpec{
						Ports: newImportServicePorts,
						ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
							ClusterID:       "member-3",
							Kind:            "Service",
							Namespace:       testNamespace,
							Name:            testServiceName,
							ResourceVersion: "0",
							Generation:      0,
							UID:             "0",
						},
					},
					Status: fleetnetv1alpha1.InternalServiceExportStatus{
						Conditions: []metav1.Condition{
							unconflictedServiceExportConflictCondition(testNamespace, testServiceName),
						},
					},
				},
			},
		},
		{
			name: "existing serviceExports all have different spec as the deleting one",
			existingInternalSvcExports: []fleetnetv1alpha1.InternalServiceExport{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testName,
						Namespace: "member-2-ns",
					},
					Spec: fleetnetv1alpha1.InternalServiceExportSpec{
						Ports: newImportServicePorts,
						ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
							ClusterID:       "member-2",
							Kind:            "Service",
							Namespace:       testNamespace,
							Name:            testServiceName,
							ResourceVersion: "0",
							Generation:      0,
							UID:             "0",
						},
					},
					TypeMeta: InternalServiceExportType,
					Status: fleetnetv1alpha1.InternalServiceExportStatus{
						Conditions: []metav1.Condition{
							conflictedServiceExportConflictCondition(testNamespace, testServiceName),
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testName,
						Namespace: "member-3-ns",
					},
					Spec: fleetnetv1alpha1.InternalServiceExportSpec{
						Ports: []fleetnetv1alpha1.ServicePort{
							{
								Name:        "portA",
								Protocol:    "UDP",
								Port:        7777,
								AppProtocol: &appProtocol,
							},
						},
						ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
							ClusterID:       "member-3",
							Kind:            "Service",
							Namespace:       testNamespace,
							Name:            testServiceName,
							ResourceVersion: "0",
							Generation:      0,
							UID:             "0",
						},
					},
					TypeMeta: InternalServiceExportType,
					Status: fleetnetv1alpha1.InternalServiceExportStatus{
						Conditions: []metav1.Condition{
							conflictedServiceExportConflictCondition(testNamespace, testServiceName),
						},
					},
				},
			},
			serviceImport: &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testNamespace,
				},
				Status: fleetnetv1alpha1.ServiceImportStatus{
					Ports: importServicePorts,
					Clusters: []fleetnetv1alpha1.ClusterStatus{
						{
							Cluster: testClusterID,
						},
					},
					Type: fleetnetv1alpha1.ClusterSetIP,
				},
			},
			wantServiceImport: &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testNamespace,
				},
				TypeMeta: serviceImportType,
				Status: fleetnetv1alpha1.ServiceImportStatus{
					Ports: newImportServicePorts,
					Clusters: []fleetnetv1alpha1.ClusterStatus{
						{
							Cluster: "member-2",
						},
					},
					Type: fleetnetv1alpha1.ClusterSetIP,
				},
			},
			wantExistingInternalSvcExports: []fleetnetv1alpha1.InternalServiceExport{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testName,
						Namespace: "member-2-ns",
					},
					TypeMeta: InternalServiceExportType,
					Spec: fleetnetv1alpha1.InternalServiceExportSpec{
						Ports: newImportServicePorts,
						ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
							ClusterID:       "member-2",
							Kind:            "Service",
							Namespace:       testNamespace,
							Name:            testServiceName,
							ResourceVersion: "0",
							Generation:      0,
							UID:             "0",
						},
					},
					Status: fleetnetv1alpha1.InternalServiceExportStatus{
						Conditions: []metav1.Condition{
							unconflictedServiceExportConflictCondition(testNamespace, testServiceName),
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testName,
						Namespace: "member-3-ns",
					},
					TypeMeta: InternalServiceExportType,
					Spec: fleetnetv1alpha1.InternalServiceExportSpec{
						Ports: []fleetnetv1alpha1.ServicePort{
							{
								Name:        "portA",
								Protocol:    "UDP",
								Port:        7777,
								AppProtocol: &appProtocol,
							},
						},
						ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
							ClusterID:       "member-3",
							Kind:            "Service",
							Namespace:       testNamespace,
							Name:            testServiceName,
							ResourceVersion: "0",
							Generation:      0,
							UID:             "0",
						},
					},
					Status: fleetnetv1alpha1.InternalServiceExportStatus{
						Conditions: []metav1.Condition{
							conflictedServiceExportConflictCondition(testNamespace, testServiceName),
						},
					},
				},
			},
		},
		{
			name: "last reconcile fails before updating the existing internal service export",
			existingInternalSvcExports: []fleetnetv1alpha1.InternalServiceExport{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testName,
						Namespace: "member-2-ns",
					},
					Spec: fleetnetv1alpha1.InternalServiceExportSpec{
						Ports: newImportServicePorts,
						ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
							ClusterID:       "member-2",
							Kind:            "Service",
							Namespace:       testNamespace,
							Name:            testServiceName,
							ResourceVersion: "0",
							Generation:      0,
							UID:             "0",
						},
					},
					TypeMeta: InternalServiceExportType,
					Status: fleetnetv1alpha1.InternalServiceExportStatus{
						Conditions: []metav1.Condition{
							conflictedServiceExportConflictCondition(testNamespace, testServiceName),
						},
					},
				},
			},
			serviceImport: &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testNamespace,
				},
				Status: fleetnetv1alpha1.ServiceImportStatus{
					Ports: newImportServicePorts,
					Clusters: []fleetnetv1alpha1.ClusterStatus{
						{
							Cluster: "member-2",
						},
					},
					Type: fleetnetv1alpha1.ClusterSetIP,
				},
			},
			wantServiceImport: &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testNamespace,
				},
				TypeMeta: serviceImportType,
				Status: fleetnetv1alpha1.ServiceImportStatus{
					Ports: newImportServicePorts,
					Clusters: []fleetnetv1alpha1.ClusterStatus{
						{
							Cluster: "member-2",
						},
					},
					Type: fleetnetv1alpha1.ClusterSetIP,
				},
			},
			wantExistingInternalSvcExports: []fleetnetv1alpha1.InternalServiceExport{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testName,
						Namespace: "member-2-ns",
					},
					TypeMeta: InternalServiceExportType,
					Spec: fleetnetv1alpha1.InternalServiceExportSpec{
						Ports: newImportServicePorts,
						ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
							ClusterID:       "member-2",
							Kind:            "Service",
							Namespace:       testNamespace,
							Name:            testServiceName,
							ResourceVersion: "0",
							Generation:      0,
							UID:             "0",
						},
					},
					Status: fleetnetv1alpha1.InternalServiceExportStatus{
						Conditions: []metav1.Condition{
							unconflictedServiceExportConflictCondition(testNamespace, testServiceName),
						},
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			internalSvcExportObj := internalServiceExportForTest()
			internalSvcExportObj.Finalizers = []string{internalServiceExportFinalizer}
			now := metav1.Now()
			internalSvcExportObj.DeletionTimestamp = &now
			objects := []client.Object{internalSvcExportObj}
			for i := range tc.existingInternalSvcExports {
				objects = append(objects, &tc.existingInternalSvcExports[i])
			}
			if tc.serviceImport != nil {
				objects = append(objects, tc.serviceImport)
			}
			fakeClient := fake.NewClientBuilder().
				WithScheme(internalServiceExportScheme(t)).
				WithObjects(objects...).
				Build()

			r := internalServiceExportReconciler(fakeClient)
			got, err := r.handleDelete(ctx, internalSvcExportObj)
			if err != nil {
				t.Fatalf("failed to handle delete: %v", err)
			}
			want := ctrl.Result{}
			if !cmp.Equal(got, want) {
				t.Errorf("handleDelete() = %+v, want %+v", got, want)
			}

			internalSvcExport := fleetnetv1alpha1.InternalServiceExport{}
			if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: testMemberNamespace, Name: testName}, &internalSvcExport); !errors.IsNotFound(err) {
				t.Errorf("InternalServiceExport Get() = %+v, got error %v, want not found error", internalSvcExport, err)
			}

			gotServiceImport := fleetnetv1alpha1.ServiceImport{}
			if err = fakeClient.Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: testServiceName}, &gotServiceImport); err != nil {
				if tc.wantServiceImport != nil || !errors.IsNotFound(err) {
					t.Fatalf("ServiceImport Get() got error %v, want no error", err)
				}
			}
			options := []cmp.Option{
				cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion"),
				cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime"),
			}
			if tc.wantServiceImport != nil {
				if diff := cmp.Diff(tc.wantServiceImport, &gotServiceImport, options...); diff != "" {
					t.Errorf("ServiceImport() mismatch (-want, +got):\n%s", diff)
				}
			}
			for _, want := range tc.wantExistingInternalSvcExports {
				got := fleetnetv1alpha1.InternalServiceExport{}
				name := types.NamespacedName{
					Namespace: want.Namespace,
					Name:      want.Name,
				}
				if err := fakeClient.Get(ctx, name, &got); err != nil {
					t.Fatalf("existing InternalServiceExport Get() got error %v, want not error", err)
				}

				if diff := cmp.Diff(want, got, options...); diff != "" {
					t.Errorf("existing InternalServiceExport() mismatch (-want, +got):\n%s", diff)
				}
			}
		})
	}
}

func TestServicePortsEqualIgnoreOrder(t *testing.T) {
	appProtocolA := "app-protocol-a"
	appProtocolB := "app-protocol-a"
	tests := []struct {
		name string
		a    []fleetnetv1alpha1.ServicePort
		b    []fleetnetv1alpha1.ServicePort
		want bool
	}{
		{
			name: "semantic equal",
			a: []fleetnetv1alpha1.ServicePort{
				{
					Name:        "portA",
					Protocol:    "TCP",
					Port:        8080,
					AppProtocol: &appProtocolA,
					TargetPort:  intstr.IntOrString{StrVal: "8080"},
				},
				{
					Name:       "portB",
					Protocol:   "TCP",
					Port:       9090,
					TargetPort: intstr.IntOrString{StrVal: "9090"},
				},
			},
			b: []fleetnetv1alpha1.ServicePort{
				{
					Name:        "portA",
					Protocol:    "TCP",
					Port:        8080,
					AppProtocol: &appProtocolB,
					TargetPort:  intstr.IntOrString{StrVal: "8080"},
				},
				{
					Name:       "portB",
					Protocol:   "TCP",
					Port:       9090,
					TargetPort: intstr.IntOrString{StrVal: "9090"},
				},
			},
			want: true,
		},
		{
			name: "one is empty and the other is nil",
			a:    []fleetnetv1alpha1.ServicePort{},
			want: true,
		},
		{
			name: "order is different",
			a: []fleetnetv1alpha1.ServicePort{
				{
					Name:        "portA",
					Protocol:    "TCP",
					Port:        8080,
					AppProtocol: &appProtocolA,
					TargetPort:  intstr.IntOrString{StrVal: "8080"},
				},
				{
					Name:       "portB",
					Protocol:   "TCP",
					Port:       9090,
					TargetPort: intstr.IntOrString{StrVal: "9090"},
				},
			},
			b: []fleetnetv1alpha1.ServicePort{
				{
					Name:       "portB",
					Protocol:   "TCP",
					Port:       9090,
					TargetPort: intstr.IntOrString{StrVal: "9090"},
				},
				{
					Name:        "portA",
					Protocol:    "TCP",
					Port:        8080,
					AppProtocol: &appProtocolA,
					TargetPort:  intstr.IntOrString{StrVal: "8080"},
				},
			},
			want: true,
		},
		{
			name: "value is different",
			a: []fleetnetv1alpha1.ServicePort{
				{
					Name:        "portA",
					Protocol:    "TCP",
					Port:        8080,
					AppProtocol: &appProtocolA,
					TargetPort:  intstr.IntOrString{StrVal: "8080"},
				},
				{
					Name:       "portB",
					Protocol:   "TCP",
					Port:       9090,
					TargetPort: intstr.IntOrString{StrVal: "9090"},
				},
			},
			b: []fleetnetv1alpha1.ServicePort{
				{
					Name:       "portA",
					Protocol:   "TCP",
					Port:       9090,
					TargetPort: intstr.IntOrString{StrVal: "9091"},
				},
				{
					Name:        "portA",
					Protocol:    "TCP",
					Port:        8080,
					AppProtocol: &appProtocolA,
					TargetPort:  intstr.IntOrString{StrVal: "8080"},
				},
			},
			want: false,
		},
		{
			name: "number of ports is different",
			a: []fleetnetv1alpha1.ServicePort{
				{
					Name:       "portB",
					Protocol:   "TCP",
					Port:       9090,
					TargetPort: intstr.IntOrString{StrVal: "9090"},
				},
			},
			b: []fleetnetv1alpha1.ServicePort{
				{
					Name:       "portA",
					Protocol:   "TCP",
					Port:       9090,
					TargetPort: intstr.IntOrString{StrVal: "9091"},
				},
				{
					Name:        "portA",
					Protocol:    "TCP",
					Port:        8080,
					AppProtocol: &appProtocolA,
					TargetPort:  intstr.IntOrString{StrVal: "8080"},
				},
			},
			want: false,
		},
		{
			name: "port name is different",
			a: []fleetnetv1alpha1.ServicePort{
				{
					Name:        "portA",
					Protocol:    "TCP",
					Port:        8080,
					AppProtocol: &appProtocolA,
					TargetPort:  intstr.IntOrString{StrVal: "8080"},
				},
			},
			b: []fleetnetv1alpha1.ServicePort{
				{
					Name:        "portC",
					Protocol:    "TCP",
					Port:        8080,
					AppProtocol: &appProtocolB,
					TargetPort:  intstr.IntOrString{StrVal: "8080"},
				},
			},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := servicePortsEqualIgnoreOrder(tc.a, tc.b)
			if got != tc.want {
				t.Fatalf("servicePortsEqualIgnoreOrder() = %v, want %v", got, tc.want)
			}
		})
	}
}
