/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package errorinjector

import (
	"context"
	"fmt"
	"os"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	NS         = "default"
	svcName    = "app"
	altSvcName = "app2"
)

var fakeClient client.Client
var errorInjector *ClientWithErrorInjection
var clientWithErrorInjector client.Client

func TestMain(m *testing.M) {
	// Setup
	fakeClient = fake.NewClientBuilder().WithObjects(
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: NS,
				Name:      svcName,
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{
						Port: 80,
					},
				},
			},
		},
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: NS,
				Name:      altSvcName,
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{
						Port: 81,
					},
				},
			},
		},
	).Build()

	// Run the tests.
	os.Exit(m.Run())
}

func TestImplementedClientInterface(t *testing.T) {
	errorInjector = New(fakeClient)
	clientWithErrorInjector = client.Client(errorInjector)
}

func TestGetWithAction(t *testing.T) {
	// Setup
	ctx := context.Background()
	getAction := GetAction{func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
		if key.Namespace == NS && key.Name == svcName {
			return errors.NewInternalError(fmt.Errorf("injected error"))
		}
		return nil
	}}
	errorInjector.GetAction = getAction

	t.Run("Should trigger the action", func(t *testing.T) {
		svc := corev1.Service{}
		err := clientWithErrorInjector.Get(ctx, types.NamespacedName{Namespace: NS, Name: svcName}, &svc)
		if !errors.IsInternalError(err) {
			t.Fatalf("action did not run")
		}
	})

	t.Run("Should not trigger the action", func(t *testing.T) {
		svc := corev1.Service{}
		err := clientWithErrorInjector.Get(ctx, types.NamespacedName{Namespace: NS, Name: altSvcName}, &svc)
		if err != nil {
			t.Fatalf("failed to get service")
		}
	})

	// Cleanup
	errorInjector.GetAction = GetAction{}

	t.Run("Should not trigger the action", func(t *testing.T) {
		svc := corev1.Service{}
		err := clientWithErrorInjector.Get(ctx, types.NamespacedName{Namespace: NS, Name: svcName}, &svc)
		if err != nil {
			t.Fatalf("failed to get service")
		}
	})
}

func TestListWithAction(t *testing.T) {
	// Setup
	ctx := context.Background()
	listAction := ListAction{func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
		return errors.NewInternalError(fmt.Errorf("injected error"))
	}}
	errorInjector.ListAction = listAction

	t.Run("Should trigger the action", func(t *testing.T) {
		svcList := corev1.ServiceList{}
		err := clientWithErrorInjector.List(ctx, &svcList, client.InNamespace(NS))
		if !errors.IsInternalError(err) {
			t.Fatalf("action did not run")
		}
	})

	// Cleanup
	errorInjector.ListAction = ListAction{}

	t.Run("Should not trigger the action", func(t *testing.T) {
		svcList := corev1.ServiceList{}
		err := clientWithErrorInjector.List(ctx, &svcList, client.InNamespace(NS))
		if err != nil {
			t.Fatalf("failed to list services")
		}
	})
}

func TestCreateWithAction(t *testing.T) {
	// Setup
	ctx := context.Background()
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: NS,
			Name:      "app3",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port: 82,
				},
			},
		},
	}

	createAction := CreateAction{func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
		if obj.GetNamespace() == NS && obj.GetName() == "app3" {
			return errors.NewInternalError(fmt.Errorf("injected error"))
		}
		return nil
	}}
	errorInjector.CreateAction = createAction

	t.Run("Should trigger the action", func(t *testing.T) {
		err := clientWithErrorInjector.Create(ctx, &svc)
		if !errors.IsInternalError(err) {
			t.Fatalf("action did not run")
		}
	})

	// Cleanup
	errorInjector.CreateAction = CreateAction{}

	t.Run("Should not trigger the action", func(t *testing.T) {
		err := clientWithErrorInjector.Create(ctx, &svc)
		if err != nil {
			t.Fatalf("failed to create service")
		}
	})
}

func TestDeleteWithAction(t *testing.T) {
	// Setup
	ctx := context.Background()
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: NS,
			Name:      "app3",
		},
	}

	deleteAction := DeleteAction{func(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
		if obj.GetNamespace() == NS && obj.GetName() == "app3" {
			return errors.NewInternalError(fmt.Errorf("injected error"))
		}
		return nil
	}}
	errorInjector.DeleteAction = deleteAction

	t.Run("Should trigger the action", func(t *testing.T) {
		err := clientWithErrorInjector.Delete(ctx, &svc)
		if !errors.IsInternalError(err) {
			t.Fatalf("action did not run")
		}
	})

	// Cleanup
	errorInjector.DeleteAction = DeleteAction{}

	t.Run("Should not trigger the action", func(t *testing.T) {
		err := errorInjector.Delete(ctx, &svc)
		if err != nil {
			t.Fatalf("failed to delete service")
		}
	})
}

func TestUpdateWithAction(t *testing.T) {
	// Setup
	ctx := context.Background()
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: NS,
			Name:      svcName,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port: 83,
				},
			},
		},
	}

	updateAction := UpdateAction{func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
		if obj.GetNamespace() == NS && obj.GetName() == svcName {
			return errors.NewInternalError(fmt.Errorf("injected error"))
		}
		return nil
	}}
	errorInjector.UpdateAction = updateAction

	t.Run("Should trigger the action", func(t *testing.T) {
		err := errorInjector.Update(ctx, &svc)
		if !errors.IsInternalError(err) {
			t.Fatalf("action did not run")
		}
	})

	// Cleanup
	errorInjector.UpdateAction = UpdateAction{}

	t.Run("Should not trigger the action", func(t *testing.T) {
		err := clientWithErrorInjector.Update(ctx, &svc)
		if err != nil {
			t.Fatalf("failed to get service")
		}
	})
}

func TestPatchWithAction(t *testing.T) {
	// Setup
	ctx := context.Background()
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: NS,
			Name:      svcName,
		},
	}
	patch := []byte(`{"metadata":{"annotations":{"patched": "true"}}}`)

	patchAction := PatchAction{func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
		if obj.GetNamespace() == NS && obj.GetName() == svcName {
			return errors.NewInternalError(fmt.Errorf("injected error"))
		}
		return nil
	}}
	errorInjector.PatchAction = patchAction

	t.Run("Should trigger the action", func(t *testing.T) {
		err := clientWithErrorInjector.Patch(ctx, &svc, client.RawPatch(types.StrategicMergePatchType, patch))
		if !errors.IsInternalError(err) {
			t.Fatalf("action did not run")
		}
	})

	// Cleanup
	errorInjector.PatchAction = PatchAction{}

	t.Run("Should not trigger the action", func(t *testing.T) {
		err := clientWithErrorInjector.Patch(ctx, &svc, client.RawPatch(types.StrategicMergePatchType, patch))
		if err != nil {
			t.Fatalf("failed to patch service")
		}
	})
}

func TestStatusUpdateWithAction(t *testing.T) {
	// Setup
	ctx := context.Background()
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: NS,
			Name:      svcName,
		},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{
					{
						IP: "1.2.3.4",
					},
				},
			},
		},
	}

	statusUpdateAction := StatusUpdateAction{func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
		if obj.GetNamespace() == NS && obj.GetName() == svcName {
			return errors.NewInternalError(fmt.Errorf("injected error"))
		}
		return nil
	}}
	errorInjector.DelegatedStatusWriter.UpdateAction = statusUpdateAction

	t.Run("Should trigger the action", func(t *testing.T) {
		err := clientWithErrorInjector.Status().Update(ctx, &svc)
		if !errors.IsInternalError(err) {
			t.Fatalf("action did not run")
		}
	})

	// Cleanup
	errorInjector.DelegatedStatusWriter.UpdateAction = StatusUpdateAction{}

	t.Run("Should not trigger the action", func(t *testing.T) {
		err := clientWithErrorInjector.Status().Update(ctx, &svc)
		if err != nil {
			t.Fatalf("failed to update service status")
		}
	})
}

func TestStatusPatchWithAction(t *testing.T) {
	// Setup
	ctx := context.Background()
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: NS,
			Name:      svcName,
		},
	}
	patch := []byte(`{"status":{"loadBalancer":{"ingress": [{"ip": "1.2.3.4"}]}}}`)

	statusPatchAction := StatusPatchAction{func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
		if obj.GetNamespace() == NS && obj.GetName() == svcName {
			return errors.NewInternalError(fmt.Errorf("injected error"))
		}
		return nil
	}}
	errorInjector.DelegatedStatusWriter.PatchAction = statusPatchAction

	t.Run("Should trigger the action", func(t *testing.T) {
		err := clientWithErrorInjector.Status().Patch(ctx, &svc, client.RawPatch(types.StrategicMergePatchType, patch))
		if !errors.IsInternalError(err) {
			t.Fatalf("action did not run")
		}
	})

	// Cleanup
	errorInjector.DelegatedStatusWriter.PatchAction = StatusPatchAction{}

	t.Run("Should not trigger the action", func(t *testing.T) {
		err := clientWithErrorInjector.Status().Patch(ctx, &svc, client.RawPatch(types.StrategicMergePatchType, patch))
		if err != nil {
			t.Fatalf("failed to patch service status")
		}
	})
}

func TestDeleteAllOfWithAction(t *testing.T) {
	// Setup
	ctx := context.Background()
	deleteAllOfAction := DeleteAllOfAction{func(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
		return errors.NewInternalError(fmt.Errorf("injected error"))
	}}
	errorInjector.DeleteAllOfAction = deleteAllOfAction

	t.Run("Should trigger the action", func(t *testing.T) {
		err := clientWithErrorInjector.DeleteAllOf(ctx, &corev1.Service{}, client.InNamespace(NS))
		if !errors.IsInternalError(err) {
			t.Fatalf("action did not run")
		}
	})

	// Cleanup
	errorInjector.DeleteAllOfAction = DeleteAllOfAction{}

	t.Run("Should not trigger the action", func(t *testing.T) {
		err := clientWithErrorInjector.DeleteAllOf(ctx, &corev1.Service{}, client.InNamespace(NS))
		if err != nil {
			t.Fatalf("failed to delete all services")
		}
	})
}
