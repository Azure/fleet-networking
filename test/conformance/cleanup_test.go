/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package conformance

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestCleanupFixturesPreservesBackendsUntilGatewayFinalization(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := gatewayv1.Install(scheme); err != nil {
		t.Fatal(err)
	}
	for _, blocked := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "blocked"}[blocked], func(t *testing.T) {
			namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test", UID: "namespace"}}
			service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "backend", Namespace: "test", UID: "service"}}
			gateway := &gatewayv1.Gateway{ObjectMeta: metav1.ObjectMeta{Name: "gateway", Namespace: "test", UID: "gateway"}}
			if blocked {
				gateway.Finalizers = []string{"test/azure-cleanup"}
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(namespace, service, gateway).Build()
			fixtures := []unstructured.Unstructured{
				{Object: map[string]interface{}{
					"apiVersion": "v1", "kind": "Namespace",
					"metadata": map[string]interface{}{"name": "test", "uid": "namespace"},
				}},
				{Object: map[string]interface{}{
					"apiVersion": "v1", "kind": "Service",
					"metadata": map[string]interface{}{"name": "backend", "namespace": "test", "uid": "service"},
				}},
			}
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			err := CleanupFixtures(ctx, c, fixtures)
			if (err != nil) != blocked {
				t.Fatalf("cleanup error=%v, blocked=%v", err, blocked)
			}
			getErr := c.Get(context.Background(), client.ObjectKeyFromObject(service), &corev1.Service{})
			if blocked && getErr != nil {
				t.Fatalf("backend withdrawn before Gateway finalization: %v", getErr)
			}
			if !blocked && !apierrors.IsNotFound(getErr) {
				t.Fatalf("owned backend remained after cleanup: %v", getErr)
			}
			if blocked {
				fixtures[0].SetUID("foreign-namespace")
				if err := CleanupFixtures(context.Background(), c, fixtures); err == nil {
					t.Fatal("replaced namespace must not be cleaned up")
				}
			}
		})
	}
}
