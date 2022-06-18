package multiclusterservice

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
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
	testName         = "my-mcs"
	testServiceName  = "my-svc"
	testNamespace    = "my-ns"
	systemNamepspace = "fleet-system"
)

func multiClusterServiceScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	scheme.AddKnownTypes(fleetnetv1alpha1.GroupVersion, &fleetnetv1alpha1.MultiClusterService{})
	scheme.AddKnownTypes(corev1.SchemeGroupVersion, &corev1.Service{})
	scheme.AddKnownTypes(fleetnetv1alpha1.GroupVersion, &fleetnetv1alpha1.ServiceImport{})
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

func multiClusterServiceReconciler(client client.Client) *MultiClusterServiceReconciler {
	return &MultiClusterServiceReconciler{
		Client:          client,
		Scheme:          client.Scheme(),
		SystemNamespace: systemNamepspace,
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
		WithScheme(multiClusterServiceScheme()).
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

func TestReconciler_AddFinalizer(t *testing.T) {
	tests := []struct {
		name       string
		finalizers []string
		want       []string
	}{
		{
			name:       "having mcs finalizer",
			finalizers: []string{multiClusterServiceFinalizer},
			want:       []string{multiClusterServiceFinalizer},
		},
		{
			name:       "having no mcs finalizer",
			finalizers: []string{"other-finalizer"},
			want:       []string{"other-finalizer", multiClusterServiceFinalizer},
		},
		{
			name:       "empty finalizer list",
			finalizers: []string{},
			want:       []string{multiClusterServiceFinalizer},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			mcsObj := multiClusterServiceForTest()
			mcsObj.Finalizers = tc.finalizers
			fakeClient := fake.NewClientBuilder().
				WithScheme(multiClusterServiceScheme()).
				WithObjects(mcsObj).
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
			mcs := fleetnetv1alpha1.MultiClusterService{}
			if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: testName}, &mcs); err != nil {
				t.Errorf("MultiClusterService Get() %+v, got error %v, want no error", mcs, err)
			}
			if got := mcs.Finalizers; !cmp.Equal(got, tc.want) {
				t.Errorf("MultiClusterService got finalizers %v, want %v", got, tc.want)
			}
		})
	}
}

func TestReconciler_Delete(t *testing.T) {
	tests := []struct {
		name          string
		labels        map[string]string
		service       *corev1.Service
		serviceImport *fleetnetv1alpha1.ServiceImport
	}{
		{
			name: "having derived service and service import",
			labels: map[string]string{
				multiClusterServiceLabelService: testServiceName,
			},
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testServiceName,
					Namespace: systemNamepspace,
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
					Namespace: systemNamepspace,
				},
			},
		},
		{
			name: "having service import",
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
				multiClusterServiceLabelService: testServiceName,
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
				WithScheme(multiClusterServiceScheme()).
				WithObjects(objects...).
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
			mcs := fleetnetv1alpha1.MultiClusterService{}
			if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: testName}, &mcs); err == nil || !errors.IsNotFound(err) {
				t.Errorf("MultiClusterService Get() %+v, got error %v, want not found error", mcs, err)
			}
			service := corev1.Service{}
			if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: systemNamepspace, Name: testServiceName}, &service); err == nil || !errors.IsNotFound(err) {
				t.Errorf("Service Get() = %+v, got error %v, want not found error", service, err)
			}
			serviceImport := fleetnetv1alpha1.ServiceImport{}
			if err := fakeClient.Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: testServiceName}, &serviceImport); err == nil || !errors.IsNotFound(err) {
				t.Errorf("ServiceImport Get() = %+v, got error %v, want not found error", serviceImport, err)
			}
		})
	}
}
