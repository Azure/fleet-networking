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

var fakeClient client.Client
var clientWithErrorInjection *ClientWithErrorInjection
var c client.Client

func TestMain(m *testing.M) {
	// Setup
	fakeClient = fake.NewClientBuilder().WithObjects(
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "app",
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
				Namespace: "default",
				Name:      "app2",
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
	clientWithErrorInjection = New(fakeClient)
	c = client.Client(clientWithErrorInjection)
}

func TestGetWithAction(t *testing.T) {
	// Setup
	actionName := "example"
	ctx := context.Background()

	t.Run("Should add the action", func(t *testing.T) {
		getAction := GetAction{func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
			if key.Namespace == "default" && key.Name == "app" {
				return errors.NewInternalError(fmt.Errorf("injected error"))
			}
			return nil
		}}
		clientWithErrorInjection.AddGetAction(actionName, getAction)
	})

	t.Run("Should trigger the action", func(t *testing.T) {
		svc := corev1.Service{}
		err := c.Get(ctx, types.NamespacedName{Namespace: "default", Name: "app"}, &svc)
		if !errors.IsInternalError(err) {
			t.Fatalf("action did not run")
		}
	})

	t.Run("Should not trigger the action", func(t *testing.T) {
		svc := corev1.Service{}
		err := c.Get(ctx, types.NamespacedName{Namespace: "default", Name: "app2"}, &svc)
		if err != nil {
			t.Fatalf("failed to get service")
		}
	})

	t.Run("Should remove the action", func(t *testing.T) {
		clientWithErrorInjection.RemoveGetAction(actionName)
	})

	t.Run("Should not trigger the action", func(t *testing.T) {
		svc := corev1.Service{}
		err := c.Get(ctx, types.NamespacedName{Namespace: "default", Name: "app"}, &svc)
		if err != nil {
			t.Fatalf("failed to get service")
		}
	})
}

func TestListWithAction(t *testing.T) {
	// Setup
	actionName := "example"
	ctx := context.Background()

	t.Run("Should add the action", func(t *testing.T) {
		listAction := ListAction{func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
			return errors.NewInternalError(fmt.Errorf("injected error"))
		}}
		clientWithErrorInjection.AddListAction(actionName, listAction)
	})

	t.Run("Should trigger the action", func(t *testing.T) {
		svcList := corev1.ServiceList{}
		err := c.List(ctx, &svcList, client.InNamespace("default"))
		if !errors.IsInternalError(err) {
			t.Fatalf("action did not run")
		}
	})

	t.Run("Should remove the action", func(t *testing.T) {
		clientWithErrorInjection.RemoveListAction(actionName)
	})

	t.Run("Should not trigger the action", func(t *testing.T) {
		svcList := corev1.ServiceList{}
		err := c.List(ctx, &svcList, client.InNamespace("default"))
		if err != nil {
			t.Fatalf("failed to list services")
		}
	})
}

func TestCreateWithAction(t *testing.T) {
	// Setup
	actionName := "example"
	ctx := context.Background()
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
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

	t.Run("Should add the action", func(t *testing.T) {
		createAction := CreateAction{func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
			if obj.GetNamespace() == "default" && obj.GetName() == "app3" {
				return errors.NewInternalError(fmt.Errorf("injected error"))
			}
			return nil
		}}
		clientWithErrorInjection.AddCreateAction(actionName, createAction)
	})

	t.Run("Should trigger the action", func(t *testing.T) {
		err := c.Create(ctx, &svc)
		if !errors.IsInternalError(err) {
			t.Fatalf("action did not run")
		}
	})

	t.Run("Should remove the action", func(t *testing.T) {
		clientWithErrorInjection.RemoveCreateAction(actionName)
	})

	t.Run("Should not trigger the action", func(t *testing.T) {
		err := c.Create(ctx, &svc)
		if err != nil {
			t.Fatalf("failed to create service")
		}
	})
}

func TestDeleteWithAction(t *testing.T) {
	// Setup
	actionName := "example"
	ctx := context.Background()
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "app3",
		},
	}

	t.Run("Should add the action", func(t *testing.T) {
		deleteAction := DeleteAction{func(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
			if obj.GetNamespace() == "default" && obj.GetName() == "app3" {
				return errors.NewInternalError(fmt.Errorf("injected error"))
			}
			return nil
		}}
		clientWithErrorInjection.AddDeleteAction(actionName, deleteAction)
	})

	t.Run("Should trigger the action", func(t *testing.T) {
		err := c.Delete(ctx, &svc)
		if !errors.IsInternalError(err) {
			t.Fatalf("action did not run")
		}
	})

	t.Run("Should remove the action", func(t *testing.T) {
		clientWithErrorInjection.RemoveDeleteAction(actionName)
	})

	t.Run("Should not trigger the action", func(t *testing.T) {
		err := c.Delete(ctx, &svc)
		if err != nil {
			t.Fatalf("failed to delete service")
		}
	})
}

func TestUpdateWithAction(t *testing.T) {
	// Setup
	actionName := "example"
	ctx := context.Background()
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "app",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port: 83,
				},
			},
		},
	}

	t.Run("Should add the action", func(t *testing.T) {
		updateAction := UpdateAction{func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
			if obj.GetNamespace() == "default" && obj.GetName() == "app" {
				return errors.NewInternalError(fmt.Errorf("injected error"))
			}
			return nil
		}}
		clientWithErrorInjection.AddUpdateAction(actionName, updateAction)
	})

	t.Run("Should trigger the action", func(t *testing.T) {
		err := c.Update(ctx, &svc)
		if !errors.IsInternalError(err) {
			t.Fatalf("action did not run")
		}
	})

	t.Run("Should remove the action", func(t *testing.T) {
		clientWithErrorInjection.RemoveUpdateAction(actionName)
	})

	t.Run("Should not trigger the action", func(t *testing.T) {
		err := c.Update(ctx, &svc)
		if err != nil {
			t.Fatalf("failed to get service")
		}
	})
}

func TestPatchWithAction(t *testing.T) {
	// Setup
	actionName := "example"
	ctx := context.Background()
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "app",
		},
	}
	patch := []byte(`{"metadata":{"annotations":{"patched": "true"}}}`)

	t.Run("Should add the action", func(t *testing.T) {
		patchAction := PatchAction{func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			if obj.GetNamespace() == "default" && obj.GetName() == "app" {
				return errors.NewInternalError(fmt.Errorf("injected error"))
			}
			return nil
		}}
		clientWithErrorInjection.AddPatchAction(actionName, patchAction)
	})

	t.Run("Should trigger the action", func(t *testing.T) {
		err := c.Patch(ctx, &svc, client.RawPatch(types.StrategicMergePatchType, patch))
		if !errors.IsInternalError(err) {
			t.Fatalf("action did not run")
		}
	})

	t.Run("Should remove the action", func(t *testing.T) {
		clientWithErrorInjection.RemovePatchAction(actionName)
	})

	t.Run("Should not trigger the action", func(t *testing.T) {
		err := c.Patch(ctx, &svc, client.RawPatch(types.StrategicMergePatchType, patch))
		if err != nil {
			t.Fatalf("failed to patch service")
		}
	})
}

func TestStatusUpdateWithAction(t *testing.T) {
	// Setup
	actionName := "example"
	ctx := context.Background()
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "app",
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

	t.Run("Should add the action", func(t *testing.T) {
		statusUpdateAction := StatusUpdateAction{func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
			if obj.GetNamespace() == "default" && obj.GetName() == "app" {
				return errors.NewInternalError(fmt.Errorf("injected error"))
			}
			return nil
		}}
		clientWithErrorInjection.AddStatusUpdateAction(actionName, statusUpdateAction)
	})

	t.Run("Should trigger the action", func(t *testing.T) {
		err := c.Status().Update(ctx, &svc)
		if !errors.IsInternalError(err) {
			t.Fatalf("action did not run")
		}
	})

	t.Run("Should remove the action", func(t *testing.T) {
		clientWithErrorInjection.RemoveStatusUpdateAction(actionName)
	})

	t.Run("Should not trigger the action", func(t *testing.T) {
		err := c.Status().Update(ctx, &svc)
		if err != nil {
			t.Fatalf("failed to update service status")
		}
	})
}

func TestStatusPatchWithAction(t *testing.T) {
	// Setup
	actionName := "example"
	ctx := context.Background()
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "app",
		},
	}
	patch := []byte(`{"status":{"loadBalancer":{"ingress": [{"ip": "1.2.3.4"}]}}}`)

	t.Run("Should add the action", func(t *testing.T) {
		statusPatchAction := StatusPatchAction{func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			if obj.GetNamespace() == "default" && obj.GetName() == "app" {
				return errors.NewInternalError(fmt.Errorf("injected error"))
			}
			return nil
		}}
		clientWithErrorInjection.AddStatusPatchAction(actionName, statusPatchAction)
	})

	t.Run("Should trigger the action", func(t *testing.T) {
		err := c.Status().Patch(ctx, &svc, client.RawPatch(types.StrategicMergePatchType, patch))
		if !errors.IsInternalError(err) {
			t.Fatalf("action did not run")
		}
	})

	t.Run("Should remove the action", func(t *testing.T) {
		clientWithErrorInjection.RemoveStatusPatchAction(actionName)
	})

	t.Run("Should not trigger the action", func(t *testing.T) {
		err := c.Status().Patch(ctx, &svc, client.RawPatch(types.StrategicMergePatchType, patch))
		if err != nil {
			t.Fatalf("failed to patch service status")
		}
	})
}

func TestDeleteAllOfWithAction(t *testing.T) {
	// Setup
	actionName := "example"
	ctx := context.Background()

	t.Run("Should add the action", func(t *testing.T) {
		deleteAllOfAction := DeleteAllOfAction{func(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
			return errors.NewInternalError(fmt.Errorf("injected error"))
		}}
		clientWithErrorInjection.AddDeleteAllOfAction(actionName, deleteAllOfAction)
	})

	t.Run("Should trigger the action", func(t *testing.T) {
		err := c.DeleteAllOf(ctx, &corev1.Service{}, client.InNamespace("default"))
		if !errors.IsInternalError(err) {
			t.Fatalf("action did not run")
		}
	})

	t.Run("Should remove the action", func(t *testing.T) {
		clientWithErrorInjection.RemoveDeleteAllOfAction(actionName)
	})

	t.Run("Should not trigger the action", func(t *testing.T) {
		err := c.DeleteAllOf(ctx, &corev1.Service{}, client.InNamespace("default"))
		if err != nil {
			t.Fatalf("failed to delete all services")
		}
	})
}
