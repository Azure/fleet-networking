package multiclusterservice

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

const (
	testName                  = "my-mcs"
	testServiceName           = "my-svc"
	testNamespace             = "my-ns"
	systemNamespace           = "fleet-system"
	fleetNetworkingAPIVersion = "networking.fleet.azure.com/v1alpha1"
)

var (
	multiClusterServiceType = metav1.TypeMeta{
		Kind:       "MultiClusterService",
		APIVersion: fleetNetworkingAPIVersion,
	}
	serviceImportType = metav1.TypeMeta{
		Kind:       "ServiceImport",
		APIVersion: fleetNetworkingAPIVersion,
	}
	serviceType = metav1.TypeMeta{
		Kind:       "Service",
		APIVersion: "v1",
	}
	derivedServiceName = fmt.Sprintf("%v-%v", testNamespace, testName)
)

func multiClusterServiceScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	if err := fleetnetv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}
	return scheme
}

func multiClusterServiceForTest() *fleetnetv1alpha1.MultiClusterService {
	return &fleetnetv1alpha1.MultiClusterService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testName,
			Namespace: testNamespace,
		},
		Spec: fleetnetv1alpha1.MultiClusterServiceSpec{
			ServiceImport: fleetnetv1alpha1.ServiceImportRef{
				Name: testServiceName,
			},
		},
	}
}

func multiClusterServiceReconciler(client client.Client) *Reconciler {
	return &Reconciler{
		Client:               client,
		Scheme:               client.Scheme(),
		FleetSystemNamespace: systemNamespace,
	}
}

func multiClusterServiceRequest() ctrl.Request {
	return ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: testNamespace,
			Name:      testName,
		},
	}
}

func TestReconciler_NotFound(t *testing.T) {
	ctx := context.Background()
	fakeClient := fake.NewClientBuilder().
		WithScheme(multiClusterServiceScheme(t)).
		Build()

	r := multiClusterServiceReconciler(fakeClient)
	got, err := r.Reconcile(ctx, multiClusterServiceRequest())
	if err != nil {
		t.Fatalf("failed to reconcile: %v", err)
	}
	want := ctrl.Result{}
	if !cmp.Equal(got, want) {
		t.Errorf("Reconcile() = %+v, want %+v", got, want)
	}
}

func TestHandleDelete(t *testing.T) {
	tests := []struct {
		name          string
		labels        map[string]string
		service       *corev1.Service
		serviceImport *fleetnetv1alpha1.ServiceImport
	}{
		{
			name: "having derived service and service import",
			labels: map[string]string{
				multiClusterServiceLabelService:       testServiceName,
				multiClusterServiceLabelServiceImport: testServiceName,
			},
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: systemNamespace,
				},
			},
			serviceImport: &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testNamespace,
				},
			},
		},
		{
			name: "having derived service",
			labels: map[string]string{
				multiClusterServiceLabelService: testServiceName,
			},
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: systemNamespace,
				},
			},
		},
		{
			name: "having service import",
			labels: map[string]string{
				multiClusterServiceLabelServiceImport: testServiceName,
			},
			serviceImport: &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testNamespace,
				},
			},
		},
		{
			name: "resources have been deleted",
			labels: map[string]string{
				multiClusterServiceLabelService:       testServiceName,
				multiClusterServiceLabelServiceImport: testServiceName,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			mcsObj := multiClusterServiceForTest()
			mcsObj.Finalizers = []string{multiClusterServiceFinalizer}
			mcsObj.ObjectMeta.Labels = tc.labels
			now := metav1.Now()
			mcsObj.DeletionTimestamp = &now
			objects := []client.Object{mcsObj}
			if tc.service != nil {
				objects = append(objects, tc.service)
			}
			if tc.serviceImport != nil {
				objects = append(objects, tc.serviceImport)
			}
			fakeClient := fake.NewClientBuilder().
				WithScheme(multiClusterServiceScheme(t)).
				WithObjects(objects...).
				Build()

			r := multiClusterServiceReconciler(fakeClient)
			got, err := r.handleDelete(ctx, mcsObj)
			if err != nil {
				t.Fatalf("failed to handle delete: %v", err)
			}
			want := ctrl.Result{}
			if !cmp.Equal(got, want) {
				t.Errorf("handleDelete() = %+v, want %+v", got, want)
			}
			mcs := fleetnetv1alpha1.MultiClusterService{}
			if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: testName}, &mcs); !errors.IsNotFound(err) {
				t.Errorf("MultiClusterService Get() %+v, got error %v, want not found error", mcs, err)
			}
			service := corev1.Service{}
			if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: systemNamespace, Name: testServiceName}, &service); !errors.IsNotFound(err) {
				t.Errorf("Service Get() = %+v, got error %v, want not found error", service, err)
			}
			serviceImport := fleetnetv1alpha1.ServiceImport{}
			if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: testServiceName}, &serviceImport); !errors.IsNotFound(err) {
				t.Errorf("ServiceImport Get() = %+v, got error %v, want not found error", serviceImport, err)
			}
		})
	}
}

func TestHandleUpdate(t *testing.T) {
	controller := true
	blockOwnerDeletion := true
	ownerRef := metav1.OwnerReference{
		APIVersion:         multiClusterServiceType.APIVersion,
		Kind:               multiClusterServiceType.Kind,
		Name:               testName,
		Controller:         &controller,
		BlockOwnerDeletion: &blockOwnerDeletion}

	importServicePorts := []fleetnetv1alpha1.ServicePort{
		{
			Name:     "portA",
			Protocol: "TCP",
			Port:     8080,
			//TargetPort: intstr.IntOrString{StrVal: "8080"},
		},
		{
			Name:     "portB",
			Protocol: "TCP",
			Port:     9090,
			//TargetPort: intstr.IntOrString{StrVal: "8080"},
		},
	}

	servicePorts := []corev1.ServicePort{
		{
			Name:     "portA",
			Protocol: "TCP",
			Port:     8080,
			//TargetPort: intstr.IntOrString{StrVal: "8080"},
			NodePort: 0,
		},
		{
			Name:     "portB",
			Protocol: "TCP",
			Port:     9090,
			//TargetPort: intstr.IntOrString{StrVal: "8080"},
			NodePort: 0,
		},
	}

	tests := []struct {
		name                string
		labels              map[string]string
		serviceImport       *fleetnetv1alpha1.ServiceImport
		hasOldServiceImport bool
		service             *corev1.Service
		wantServiceImport   *fleetnetv1alpha1.ServiceImport
		wantDerivedService  *corev1.Service
		wantMCS             *fleetnetv1alpha1.MultiClusterService
	}{
		{
			name:   "no service import and its label", // mcs is just created
			labels: map[string]string{},
			wantServiceImport: &fleetnetv1alpha1.ServiceImport{
				TypeMeta: serviceImportType,
				ObjectMeta: metav1.ObjectMeta{
					Name:            testServiceName,
					Namespace:       testNamespace,
					OwnerReferences: []metav1.OwnerReference{ownerRef},
				},
			},
			wantMCS: &fleetnetv1alpha1.MultiClusterService{
				TypeMeta: multiClusterServiceType,
				ObjectMeta: metav1.ObjectMeta{
					Name:      testName,
					Namespace: testNamespace,
					Labels: map[string]string{
						multiClusterServiceLabelServiceImport: testServiceName,
					},
				},
				Spec: fleetnetv1alpha1.MultiClusterServiceSpec{
					ServiceImport: fleetnetv1alpha1.ServiceImportRef{
						Name: testServiceName,
					},
				},
			},
		},
		{
			name: "no updates on mcs (invalid service import) without derived service label",
			labels: map[string]string{
				multiClusterServiceLabelServiceImport: testServiceName,
			},
			serviceImport: &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testNamespace,
				},
			},
			wantServiceImport: &fleetnetv1alpha1.ServiceImport{
				TypeMeta: serviceImportType,
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testNamespace,
				},
			},
			wantMCS: &fleetnetv1alpha1.MultiClusterService{
				TypeMeta: multiClusterServiceType,
				ObjectMeta: metav1.ObjectMeta{
					Name:      testName,
					Namespace: testNamespace,
					Labels: map[string]string{
						multiClusterServiceLabelServiceImport: testServiceName,
					},
				},
				Spec: fleetnetv1alpha1.MultiClusterServiceSpec{
					ServiceImport: fleetnetv1alpha1.ServiceImportRef{
						Name: testServiceName,
					},
				},
			},
		},
		{
			name: "update service import spec on mcs",
			labels: map[string]string{
				multiClusterServiceLabelServiceImport: "old-service",
			},
			serviceImport: &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "old-service",
					Namespace: testNamespace,
				},
			},
			hasOldServiceImport: true,
			wantServiceImport: &fleetnetv1alpha1.ServiceImport{
				TypeMeta: serviceImportType,
				ObjectMeta: metav1.ObjectMeta{
					Name:            testServiceName,
					Namespace:       testNamespace,
					OwnerReferences: []metav1.OwnerReference{ownerRef},
				},
			},
			wantMCS: &fleetnetv1alpha1.MultiClusterService{
				TypeMeta: multiClusterServiceType,
				ObjectMeta: metav1.ObjectMeta{
					Name:      testName,
					Namespace: testNamespace,
					Labels: map[string]string{
						multiClusterServiceLabelServiceImport: testServiceName,
					},
				},
				Spec: fleetnetv1alpha1.MultiClusterServiceSpec{
					ServiceImport: fleetnetv1alpha1.ServiceImportRef{
						Name: testServiceName,
					},
				},
			},
		},
		{
			name: "update service import on the mcs and no old service import resource",
			labels: map[string]string{
				multiClusterServiceLabelServiceImport: "old-service",
			},
			wantServiceImport: &fleetnetv1alpha1.ServiceImport{
				TypeMeta: serviceImportType,
				ObjectMeta: metav1.ObjectMeta{
					Name:            testServiceName,
					Namespace:       testNamespace,
					OwnerReferences: []metav1.OwnerReference{ownerRef},
				},
			},
			wantMCS: &fleetnetv1alpha1.MultiClusterService{
				TypeMeta: multiClusterServiceType,
				ObjectMeta: metav1.ObjectMeta{
					Name:      testName,
					Namespace: testNamespace,
					Labels: map[string]string{
						multiClusterServiceLabelServiceImport: testServiceName,
					},
				},
				Spec: fleetnetv1alpha1.MultiClusterServiceSpec{
					ServiceImport: fleetnetv1alpha1.ServiceImportRef{
						Name: testServiceName,
					},
				},
			},
		},
		{
			name: "no update on service import on the mcs and no service import resource ",
			labels: map[string]string{
				multiClusterServiceLabelServiceImport: testServiceName,
			},
			wantServiceImport: &fleetnetv1alpha1.ServiceImport{
				TypeMeta: serviceImportType,
				ObjectMeta: metav1.ObjectMeta{
					Name:            testServiceName,
					Namespace:       testNamespace,
					OwnerReferences: []metav1.OwnerReference{ownerRef},
				},
			},
			wantMCS: &fleetnetv1alpha1.MultiClusterService{
				TypeMeta: multiClusterServiceType,
				ObjectMeta: metav1.ObjectMeta{
					Name:      testName,
					Namespace: testNamespace,
					Labels: map[string]string{
						multiClusterServiceLabelServiceImport: testServiceName,
					},
				},
				Spec: fleetnetv1alpha1.MultiClusterServiceSpec{
					ServiceImport: fleetnetv1alpha1.ServiceImportRef{
						Name: testServiceName,
					},
				},
			},
		},
		{
			name: "no updates on mcs (invalid service import) without derived service resource",
			labels: map[string]string{
				multiClusterServiceLabelServiceImport: testServiceName,
				multiClusterServiceLabelService:       derivedServiceName,
			},
			serviceImport: &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testNamespace,
				},
			},
			wantServiceImport: &fleetnetv1alpha1.ServiceImport{
				TypeMeta: serviceImportType,
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testNamespace,
				},
			},
			wantMCS: &fleetnetv1alpha1.MultiClusterService{
				TypeMeta: multiClusterServiceType,
				ObjectMeta: metav1.ObjectMeta{
					Name:      testName,
					Namespace: testNamespace,
					Labels: map[string]string{
						multiClusterServiceLabelServiceImport: testServiceName,
					},
				},
				Spec: fleetnetv1alpha1.MultiClusterServiceSpec{
					ServiceImport: fleetnetv1alpha1.ServiceImportRef{
						Name: testServiceName,
					},
				},
			},
		},
		{
			name: "no updates on mcs (invalid service import) with derived service resource",
			labels: map[string]string{
				multiClusterServiceLabelServiceImport: testServiceName,
				multiClusterServiceLabelService:       derivedServiceName,
			},
			serviceImport: &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testNamespace,
				},
			},
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      derivedServiceName,
					Namespace: systemNamespace,
				},
			},
			wantServiceImport: &fleetnetv1alpha1.ServiceImport{
				TypeMeta: serviceImportType,
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testNamespace,
				},
			},
			wantMCS: &fleetnetv1alpha1.MultiClusterService{
				TypeMeta: multiClusterServiceType,
				ObjectMeta: metav1.ObjectMeta{
					Name:      testName,
					Namespace: testNamespace,
					Labels: map[string]string{
						multiClusterServiceLabelServiceImport: testServiceName,
					},
				},
				Spec: fleetnetv1alpha1.MultiClusterServiceSpec{
					ServiceImport: fleetnetv1alpha1.ServiceImportRef{
						Name: testServiceName,
					},
				},
			},
		},
		{
			name: "no updates on the mcs (valid service import) without derived service resource",
			labels: map[string]string{
				multiClusterServiceLabelServiceImport: testServiceName,
			},
			serviceImport: &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testNamespace,
				},
				Status: fleetnetv1alpha1.ServiceImportStatus{
					Ports: importServicePorts,
					Clusters: []fleetnetv1alpha1.ClusterStatus{
						{Cluster: "member1"},
					},
				},
			},
			wantServiceImport: &fleetnetv1alpha1.ServiceImport{
				TypeMeta: serviceImportType,
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testNamespace,
				},
				Status: fleetnetv1alpha1.ServiceImportStatus{
					Ports: importServicePorts,
					Clusters: []fleetnetv1alpha1.ClusterStatus{
						{Cluster: "member1"},
					},
				},
			},
			wantDerivedService: &corev1.Service{
				TypeMeta: serviceType,
				ObjectMeta: metav1.ObjectMeta{
					Name:      derivedServiceName,
					Namespace: systemNamespace,
				},
				Spec: corev1.ServiceSpec{
					Ports: servicePorts,
					Type:  corev1.ServiceTypeLoadBalancer,
				},
			},
			wantMCS: &fleetnetv1alpha1.MultiClusterService{
				TypeMeta: multiClusterServiceType,
				ObjectMeta: metav1.ObjectMeta{
					Name:      testName,
					Namespace: testNamespace,
					Labels: map[string]string{
						multiClusterServiceLabelServiceImport: testServiceName,
						multiClusterServiceLabelService:       derivedServiceName,
					},
				},
				Spec: fleetnetv1alpha1.MultiClusterServiceSpec{
					ServiceImport: fleetnetv1alpha1.ServiceImportRef{
						Name: testServiceName,
					},
				},
			},
		},
		{
			name: "no updates on the mcs (valid service import) without derived service resource",
			labels: map[string]string{
				multiClusterServiceLabelServiceImport: testServiceName,
				multiClusterServiceLabelService:       derivedServiceName,
			},
			serviceImport: &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testNamespace,
				},
				Status: fleetnetv1alpha1.ServiceImportStatus{
					Ports: importServicePorts,
					Clusters: []fleetnetv1alpha1.ClusterStatus{
						{Cluster: "member1"},
					},
				},
			},
			wantServiceImport: &fleetnetv1alpha1.ServiceImport{
				TypeMeta: serviceImportType,
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testNamespace,
				},
				Status: fleetnetv1alpha1.ServiceImportStatus{
					Ports: importServicePorts,
					Clusters: []fleetnetv1alpha1.ClusterStatus{
						{Cluster: "member1"},
					},
				},
			},
			wantDerivedService: &corev1.Service{
				TypeMeta: serviceType,
				ObjectMeta: metav1.ObjectMeta{
					Name:      derivedServiceName,
					Namespace: systemNamespace,
				},
				Spec: corev1.ServiceSpec{
					Ports: servicePorts,
					Type:  corev1.ServiceTypeLoadBalancer,
				},
			},
			wantMCS: &fleetnetv1alpha1.MultiClusterService{
				TypeMeta: multiClusterServiceType,
				ObjectMeta: metav1.ObjectMeta{
					Name:      testName,
					Namespace: testNamespace,
					Labels: map[string]string{
						multiClusterServiceLabelServiceImport: testServiceName,
						multiClusterServiceLabelService:       derivedServiceName,
					},
				},
				Spec: fleetnetv1alpha1.MultiClusterServiceSpec{
					ServiceImport: fleetnetv1alpha1.ServiceImportRef{
						Name: testServiceName,
					},
				},
			},
		},
		{
			name: "service import spec mismatching with derived service",
			labels: map[string]string{
				multiClusterServiceLabelServiceImport: testServiceName,
				multiClusterServiceLabelService:       derivedServiceName,
			},
			serviceImport: &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testNamespace,
				},
				Status: fleetnetv1alpha1.ServiceImportStatus{
					Ports: importServicePorts,
					Clusters: []fleetnetv1alpha1.ClusterStatus{
						{Cluster: "member1"},
					},
				},
			},
			service: &corev1.Service{
				TypeMeta: serviceType,
				ObjectMeta: metav1.ObjectMeta{
					Name:      derivedServiceName,
					Namespace: systemNamespace,
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:     "portA",
							Protocol: "TCP",
							Port:     8080,
							//TargetPort: intstr.IntOrString{StrVal: "8080"},
							NodePort: 0,
						},
					},
					Type: corev1.ServiceTypeNodePort,
				},
			},
			wantServiceImport: &fleetnetv1alpha1.ServiceImport{
				TypeMeta: serviceImportType,
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: testNamespace,
				},
				Status: fleetnetv1alpha1.ServiceImportStatus{
					Ports: importServicePorts,
					Clusters: []fleetnetv1alpha1.ClusterStatus{
						{Cluster: "member1"},
					},
				},
			},
			wantDerivedService: &corev1.Service{
				TypeMeta: serviceType,
				ObjectMeta: metav1.ObjectMeta{
					Name:      derivedServiceName,
					Namespace: systemNamespace,
				},
				Spec: corev1.ServiceSpec{
					Ports: servicePorts,
					Type:  corev1.ServiceTypeLoadBalancer,
				},
			},
			wantMCS: &fleetnetv1alpha1.MultiClusterService{
				TypeMeta: multiClusterServiceType,
				ObjectMeta: metav1.ObjectMeta{
					Name:      testName,
					Namespace: testNamespace,
					Labels: map[string]string{
						multiClusterServiceLabelServiceImport: testServiceName,
						multiClusterServiceLabelService:       derivedServiceName,
					},
				},
				Spec: fleetnetv1alpha1.MultiClusterServiceSpec{
					ServiceImport: fleetnetv1alpha1.ServiceImportRef{
						Name: testServiceName,
					},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			mcsObj := multiClusterServiceForTest()
			mcsObj.ObjectMeta.Labels = tc.labels
			objects := []client.Object{mcsObj}
			if tc.serviceImport != nil {
				objects = append(objects, tc.serviceImport)
			}
			if tc.service != nil {
				objects = append(objects, tc.service)
			}
			fakeClient := fake.NewClientBuilder().
				WithScheme(multiClusterServiceScheme(t)).
				WithObjects(objects...).
				Build()

			r := multiClusterServiceReconciler(fakeClient)
			got, err := r.handleUpdate(ctx, mcsObj)
			if err != nil {
				t.Fatalf("failed to handle update: %v", err)
			}
			want := ctrl.Result{}
			if !cmp.Equal(got, want) {
				t.Errorf("handleUpdate() = %+v, want %+v", got, want)
			}
			serviceImport := fleetnetv1alpha1.ServiceImport{}
			name := types.NamespacedName{Namespace: testNamespace, Name: testServiceName}
			if err := fakeClient.Get(ctx, name, &serviceImport); err != nil {
				t.Fatalf("ServiceImport Get() got error %v, want no error", err)
			}
			options := cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion")
			if diff := cmp.Diff(tc.wantServiceImport, &serviceImport, options); diff != "" {
				t.Errorf("serviceImport Get() mismatch (-want, +got):\n%s", diff)
			}

			service := corev1.Service{}
			name = types.NamespacedName{Namespace: systemNamespace, Name: derivedServiceName}
			if err := fakeClient.Get(ctx, name, &service); err != nil {
				if tc.wantDerivedService != nil || !errors.IsNotFound(err) {
					t.Fatalf("ServiceImport Get() got error %v, want no error", err)
				}
			}
			if tc.wantDerivedService != nil {
				if diff := cmp.Diff(tc.wantDerivedService, &service, options); diff != "" {
					t.Errorf("Service() mismatch (-want, +got):\n%s", diff)
				}
			}

			mcs := fleetnetv1alpha1.MultiClusterService{}
			name = types.NamespacedName{Namespace: testNamespace, Name: testName}
			if err := fakeClient.Get(ctx, name, &mcs); err != nil {
				t.Fatalf("MultiClusterService Get() got error %v, want no error", err)
			}
			if diff := cmp.Diff(tc.wantMCS, &mcs, options); diff != "" {
				t.Errorf("MultiClusterService() mismatch (-want, +got):\n%s", diff)
			}
			if !tc.hasOldServiceImport {
				return
			}
			oldServiceImport := fleetnetv1alpha1.ServiceImport{}
			name = types.NamespacedName{Namespace: tc.serviceImport.Namespace, Name: tc.serviceImport.Name}
			if err := fakeClient.Get(ctx, name, &oldServiceImport); !errors.IsNotFound(err) {
				t.Errorf("Old ServiceImport Get() = %+v, got error %v, want not found error", oldServiceImport, err)
			}
		})
	}
}
