/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gatewayresolver

import (
	"context"
	"testing"

	"fmt"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestCompanionEventsMapVerifiedSourceRoutes(t *testing.T) {
	for _, scenario := range []string{"ready", "foreign-owner", "replaced-source", "foreign-mirror", "unrelated-labels", "read-error"} {
		t.Run(scenario, func(t *testing.T) {
			scheme := runtime.NewScheme()
			for _, install := range []func(*runtime.Scheme) error{corev1.AddToScheme, discoveryv1.AddToScheme, gatewayv1.Install} {
				if err := install(scheme); err != nil {
					t.Fatal(err)
				}
			}
			source := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "store", Name: "api", UID: "source"}}
			companion := &corev1.Service{ObjectMeta: metav1.ObjectMeta{
				Namespace: "store", Name: "companion", UID: "companion",
				Labels: map[string]string{
					objectmeta.LocalOriginManagedLabel:    objectmeta.LocalOriginManager,
					objectmeta.LocalOriginSourceNameLabel: source.Name,
					objectmeta.LocalOriginSourceUIDLabel:  string(source.UID),
				},
				OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(source, corev1.SchemeGroupVersion.WithKind("Service"))},
			}}
			mirror := &discoveryv1.EndpointSlice{ObjectMeta: metav1.ObjectMeta{
				Namespace: "store", Name: "mirror", Labels: map[string]string{
					objectmeta.LocalOriginManagedLabel: objectmeta.LocalOriginManager,
					discoveryv1.LabelServiceName:       companion.Name,
				},
				OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(companion, corev1.SchemeGroupVersion.WithKind("Service"))},
			}}
			switch scenario {
			case "foreign-owner":
				companion.OwnerReferences[0].UID = "foreign"
			case "replaced-source":
				source.UID = "replacement"
			case "foreign-mirror":
				mirror.OwnerReferences[0].UID = "foreign"
			case "unrelated-labels":
				companion.OwnerReferences = nil
			}
			route := indexedRoute()
			route.Spec.Rules[0].BackendRefs[0].Group = ptr.To(gatewayv1.Group(""))
			route.Spec.Rules[0].BackendRefs[0].Kind = ptr.To(gatewayv1.Kind("Service"))
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(source, companion, route).
				WithIndex(&gatewayv1.HTTPRoute{}, HTTPRouteServiceImportField, routeServiceImportIndex).
				WithInterceptorFuncs(interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if scenario == "read-error" {
						return fmt.Errorf("injected API read failure")
					}
					return c.Get(ctx, key, obj, opts...)
				}}).Build()
			for _, event := range []client.Object{companion, mirror} {
				want := 0
				if scenario == "ready" || (scenario == "foreign-mirror" && event == companion) {
					want = 1
				}
				got, err := DependentRoutes(context.Background(), c, event)
				if (err != nil) != (scenario == "read-error") || len(got) != want {
					t.Fatalf("%T event: routes=%d error=%v; want %d", event, len(got), err, want)
				}
			}
		})
	}
}
