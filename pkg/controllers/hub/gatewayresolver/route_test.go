/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gatewayresolver

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

func routeFixture() (*gatewayv1.Gateway, *gatewayv1.HTTPRoute, gatewayv1.ParentReference) {
	gateway := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Namespace: "store", Name: "global", UID: "gateway"},
		Spec: gatewayv1.GatewaySpec{GatewayClassName: "azure-fleet-afd", Listeners: []gatewayv1.Listener{{
			Name: "http", Protocol: gatewayv1.HTTPProtocolType, Port: 80, Hostname: ptr.To(gatewayv1.Hostname("*.example.com")),
		}}},
	}
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Namespace: "store", Name: "api", UID: "route", CreationTimestamp: metav1.Now()},
		Spec: gatewayv1.HTTPRouteSpec{
			Hostnames: []gatewayv1.Hostname{"store.example.com"},
			Rules:     []gatewayv1.HTTPRouteRule{{BackendRefs: []gatewayv1.HTTPBackendRef{newBackendFixture().ref}}},
		},
	}
	parent := gatewayv1.ParentReference{Name: "global"}
	route.Spec.ParentRefs = []gatewayv1.ParentReference{parent}
	return gateway, route, parent
}

func TestResolveAttachments(t *testing.T) {
	tests := []struct {
		name   string
		change func(*gatewayv1.Gateway, *gatewayv1.HTTPRoute, *gatewayv1.ParentReference)
		reason gatewayv1.RouteConditionReason
	}{
		{name: "same namespace default"},
		{name: "selected listener", change: func(_ *gatewayv1.Gateway, _ *gatewayv1.HTTPRoute, p *gatewayv1.ParentReference) {
			p.SectionName = ptr.To(gatewayv1.SectionName("http"))
		}},
		{name: "cross namespace all", change: func(g *gatewayv1.Gateway, r *gatewayv1.HTTPRoute, p *gatewayv1.ParentReference) {
			r.Namespace = "apps"
			p.Namespace = ptr.To(gatewayv1.Namespace("store"))
			g.Spec.Listeners[0].AllowedRoutes = &gatewayv1.AllowedRoutes{Namespaces: &gatewayv1.RouteNamespaces{From: ptr.To(gatewayv1.NamespacesFromAll)}}
		}},
		{name: "namespace selector", change: func(g *gatewayv1.Gateway, _ *gatewayv1.HTTPRoute, _ *gatewayv1.ParentReference) {
			g.Spec.Listeners[0].AllowedRoutes = &gatewayv1.AllowedRoutes{Namespaces: &gatewayv1.RouteNamespaces{
				From: ptr.To(gatewayv1.NamespacesFromSelector), Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"tenant": "a"}},
			}}
		}},
		{name: "cross namespace default denied", change: func(_ *gatewayv1.Gateway, r *gatewayv1.HTTPRoute, p *gatewayv1.ParentReference) {
			r.Namespace = "apps"
			p.Namespace = ptr.To(gatewayv1.Namespace("store"))
		}, reason: gatewayv1.RouteReasonNotAllowedByListeners},
		{name: "unknown section", change: func(_ *gatewayv1.Gateway, _ *gatewayv1.HTTPRoute, p *gatewayv1.ParentReference) {
			p.SectionName = ptr.To(gatewayv1.SectionName(otherName))
		}, reason: gatewayv1.RouteReasonNoMatchingParent},
		{name: "wrong port", change: func(_ *gatewayv1.Gateway, _ *gatewayv1.HTTPRoute, p *gatewayv1.ParentReference) {
			p.Port = ptr.To(gatewayv1.PortNumber(8080))
		}, reason: gatewayv1.RouteReasonNoMatchingParent},
		{name: "unmatched hostname", change: func(_ *gatewayv1.Gateway, r *gatewayv1.HTTPRoute, _ *gatewayv1.ParentReference) {
			r.Spec.Hostnames = []gatewayv1.Hostname{"other.test"}
		}, reason: gatewayv1.RouteReasonNoMatchingListenerHostname},
		{name: "selector denied", change: func(g *gatewayv1.Gateway, _ *gatewayv1.HTTPRoute, _ *gatewayv1.ParentReference) {
			g.Spec.Listeners[0].AllowedRoutes = &gatewayv1.AllowedRoutes{Namespaces: &gatewayv1.RouteNamespaces{
				From: ptr.To(gatewayv1.NamespacesFromSelector), Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"tenant": otherName}},
			}}
		}, reason: gatewayv1.RouteReasonNotAllowedByListeners},
		{name: "unsupported route kind", change: func(g *gatewayv1.Gateway, _ *gatewayv1.HTTPRoute, _ *gatewayv1.ParentReference) {
			g.Spec.Listeners[0].AllowedRoutes = &gatewayv1.AllowedRoutes{Kinds: []gatewayv1.RouteGroupKind{{Kind: "TCPRoute"}}}
		}, reason: gatewayv1.RouteReasonNotAllowedByListeners},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateway, route, parent := routeFixture()
			if tt.change != nil {
				tt.change(gateway, route, &parent)
			}
			got, err := ResolveAttachments(gateway, route, parent, map[string]string{"tenant": "a"})
			if tt.reason != "" {
				var attachmentErr *AttachmentError
				if !errors.As(err, &attachmentErr) || attachmentErr.Reason != tt.reason {
					t.Fatalf("error = %v, want %s", err, tt.reason)
				}
				return
			}
			if err != nil || len(got) != 1 || got[0].Listener != "http" || !reflect.DeepEqual(got[0].Hostnames, []string{"store.example.com"}) {
				t.Fatalf("attachments = %#v, %v", got, err)
			}
		})
	}
}

func resolverClient(t *testing.T, objects ...client.Object) client.WithWatch {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, install := range []func(*runtime.Scheme) error{gatewayv1.Install, gatewayv1beta1.Install, fleetnetv1alpha1.AddToScheme} {
		if err := install(scheme); err != nil {
			t.Fatal(err)
		}
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).
		WithIndex(&fleetnetv1alpha1.InternalServiceExport{}, ExportServiceField, exportServiceIndex).Build()
}

func TestCrossNamespaceGrantRemoval(t *testing.T) {
	fixture := newBackendFixture()
	fixture.ref.Namespace = ptr.To(gatewayv1.Namespace("store"))
	grant := &gatewayv1beta1.ReferenceGrant{
		ObjectMeta: metav1.ObjectMeta{Namespace: "store", Name: "allow"},
		Spec: gatewayv1beta1.ReferenceGrantSpec{
			From: []gatewayv1beta1.ReferenceGrantFrom{{Group: gatewayv1.GroupName, Kind: "HTTPRoute", Namespace: "apps"}},
			To:   []gatewayv1beta1.ReferenceGrantTo{{Group: gatewayv1.Group(fleetnetv1alpha1.GroupVersion.Group), Kind: serviceImportKind, Name: ptr.To(gatewayv1.ObjectName("api"))}},
		},
	}
	c := resolverClient(t, fixture.service, &fixture.exports[0], &fixture.exports[1], grant)
	ctx := context.Background()
	if _, err := ResolveBackend(ctx, c, "apps", fixture.ref); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(ctx, grant); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveBackend(ctx, c, "apps", fixture.ref)
	var resolutionErr *ResolutionError
	if !errors.As(err, &resolutionErr) || resolutionErr.ReferenceValid || resolutionErr.Reason != string(gatewayv1.RouteReasonRefNotPermitted) {
		t.Fatalf("revoked grant did not revoke reference: %v", err)
	}
}

func TestResolveHTTPRouteChecksEveryReference(t *testing.T) {
	fixture := newBackendFixture()
	fixture.exports[0].Spec.PrivateLinkOrigin = nil
	_, route, _ := routeFixture()
	route.Spec.Rules = append(route.Spec.Rules, gatewayv1.HTTPRouteRule{BackendRefs: []gatewayv1.HTTPBackendRef{fixture.ref}})
	route.Spec.Rules[1].BackendRefs[0].Name = "missing"
	c := resolverClient(t, fixture.service, &fixture.exports[0], &fixture.exports[1])
	_, err := ResolveHTTPRoute(context.Background(), c, route, nil)
	var resolutionErr *ResolutionError
	if !errors.As(err, &resolutionErr) || resolutionErr.ReferenceValid || resolutionErr.Reason != "BackendNotFound" {
		t.Fatalf("pending origin hid a later invalid reference: %v", err)
	}
}

func TestResolveHTTPRouteHandlesWrappedResolutionErrors(t *testing.T) {
	for _, referenceValid := range []bool{false, true} {
		t.Run(fmt.Sprintf("reference-valid=%v", referenceValid), func(t *testing.T) {
			fixture := newBackendFixture()
			_, route, _ := routeFixture()
			missing := route.Spec.Rules[0].DeepCopy()
			missing.BackendRefs[0].Name = "missing"
			route.Spec.Rules = append(route.Spec.Rules, *missing)
			reads := 0
			c := interceptor.NewClient(resolverClient(t), interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
					reads++
					if key.Name == string(fixture.ref.Name) {
						return fmt.Errorf("wrapped backend failure: %w", &ResolutionError{
							Reason: "PrivateOriginUnavailable", ReferenceValid: referenceValid, Message: "discovery is pending",
						})
					}
					return c.Get(ctx, key, object, options...)
				},
			})
			_, err := ResolveHTTPRoute(context.Background(), c, route, nil)
			wantReason, wantReads := "PrivateOriginUnavailable", 1
			if referenceValid {
				wantReason, wantReads = "BackendNotFound", 2
			}
			var resolutionErr *ResolutionError
			if !errors.As(err, &resolutionErr) || resolutionErr.Reason != wantReason || reads != wantReads {
				t.Fatalf("wrapped resolution changed reference priority: reads=%d, error=%v", reads, err)
			}
		})
	}
}

func TestValidateHTTPRouteRejectsUnsupportedIntent(t *testing.T) {
	tests := []struct {
		name   string
		change func(*gatewayv1.HTTPRoute)
	}{
		{"header", func(r *gatewayv1.HTTPRoute) {
			r.Spec.Rules[0].Matches = []gatewayv1.HTTPRouteMatch{{Headers: []gatewayv1.HTTPHeaderMatch{{Name: "x-test", Value: "a"}}}}
		}},
		{"method", func(r *gatewayv1.HTTPRoute) {
			r.Spec.Rules[0].Matches = []gatewayv1.HTTPRouteMatch{{Method: ptr.To(gatewayv1.HTTPMethodGet)}}
		}},
		{"filter", func(r *gatewayv1.HTTPRoute) {
			r.Spec.Rules[0].Filters = []gatewayv1.HTTPRouteFilter{{Type: gatewayv1.HTTPRouteFilterRequestMirror}}
		}},
		{"timeouts", func(r *gatewayv1.HTTPRoute) { r.Spec.Rules[0].Timeouts = &gatewayv1.HTTPRouteTimeouts{} }},
		{"retry", func(r *gatewayv1.HTTPRoute) { r.Spec.Rules[0].Retry = &gatewayv1.HTTPRouteRetry{} }},
		{"session persistence", func(r *gatewayv1.HTTPRoute) { r.Spec.Rules[0].SessionPersistence = &gatewayv1.SessionPersistence{} }},
		{"query", func(r *gatewayv1.HTTPRoute) {
			r.Spec.Rules[0].Matches = []gatewayv1.HTTPRouteMatch{{QueryParams: []gatewayv1.HTTPQueryParamMatch{{Name: "query", Value: "value"}}}}
		}},
		{"multiple backends", func(r *gatewayv1.HTTPRoute) {
			r.Spec.Rules[0].BackendRefs = append(r.Spec.Rules[0].BackendRefs, r.Spec.Rules[0].BackendRefs[0])
		}},
		{"zero backend weight", func(r *gatewayv1.HTTPRoute) { r.Spec.Rules[0].BackendRefs[0].Weight = ptr.To(int32(0)) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, route, _ := routeFixture()
			tt.change(route)
			if err := ValidateHTTPRoute(route); err == nil {
				t.Fatal("unsupported intent was ignored")
			}
		})
	}
}

func TestReferenceGrantRequiresMatchingFromAndTo(t *testing.T) {
	tests := []struct {
		name   string
		change func(*gatewayv1beta1.ReferenceGrant)
		allow  bool
	}{
		{"matching", func(*gatewayv1beta1.ReferenceGrant) {}, true},
		{"wildcard target name", func(g *gatewayv1beta1.ReferenceGrant) { g.Spec.To[0].Name = nil }, true},
		{"other source namespace", func(g *gatewayv1beta1.ReferenceGrant) { g.Spec.From[0].Namespace = otherName }, false},
		{"other source group", func(g *gatewayv1beta1.ReferenceGrant) { g.Spec.From[0].Group = "other.example" }, false},
		{"other source kind", func(g *gatewayv1beta1.ReferenceGrant) { g.Spec.From[0].Kind = "TCPRoute" }, false},
		{"other target name", func(g *gatewayv1beta1.ReferenceGrant) { g.Spec.To[0].Name = ptr.To(gatewayv1.ObjectName(otherName)) }, false},
		{"other target group", func(g *gatewayv1beta1.ReferenceGrant) { g.Spec.To[0].Group = "" }, false},
		{"other target kind", func(g *gatewayv1beta1.ReferenceGrant) { g.Spec.To[0].Kind = "Service" }, false},
		{"deleting grant", func(g *gatewayv1beta1.ReferenceGrant) { now := metav1.Now(); g.DeletionTimestamp = &now }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			grant := gatewayv1beta1.ReferenceGrant{Spec: gatewayv1beta1.ReferenceGrantSpec{
				From: []gatewayv1beta1.ReferenceGrantFrom{{Group: gatewayv1.GroupName, Kind: "HTTPRoute", Namespace: "apps"}},
				To:   []gatewayv1beta1.ReferenceGrantTo{{Group: gatewayv1.Group(fleetnetv1alpha1.GroupVersion.Group), Kind: serviceImportKind, Name: ptr.To(gatewayv1.ObjectName("api"))}},
			}}
			tt.change(&grant)
			if allowed := referencePermitted("apps", "api", []gatewayv1beta1.ReferenceGrant{grant}); allowed != tt.allow {
				t.Fatalf("grant allowed=%v, want %v", allowed, tt.allow)
			}
		})
	}
}

func TestResolveBackendDoesNotReadUnauthorizedTarget(t *testing.T) {
	fixture := newBackendFixture()
	fixture.ref.Namespace = ptr.To(gatewayv1.Namespace("store"))
	read := false
	c := interceptor.NewClient(resolverClient(t), interceptor.Funcs{
		Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
			read = true
			return errors.New("target read must not occur")
		},
	})
	_, err := ResolveBackend(context.Background(), c, "apps", fixture.ref)
	var resolutionErr *ResolutionError
	if read || !errors.As(err, &resolutionErr) || resolutionErr.Reason != "RefNotPermitted" {
		t.Fatalf("unauthorized reference read target: read=%v, error=%v", read, err)
	}
}
