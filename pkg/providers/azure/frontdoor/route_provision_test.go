/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package frontdoor

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	"k8s.io/utils/ptr"
)

func routeFixture(t *testing.T) (backendProvisioningFixture, armcdn.Route) {
	t.Helper()
	f := backendFixture(t)
	observed, err := BuildHTTPPreviewRequests(f.snapshot.scope, f.snapshot.owner, f.snapshot.model)
	if err != nil {
		t.Fatal(err)
	}
	route := observed.Route
	route.ID = ptr.To(f.graph.Roots.ProfileID + routeSuffix(f))
	route.Name = ptr.To(f.graph.RouteName)
	route.Properties.EndpointName = ptr.To(f.graph.Roots.EndpointName)
	route.Properties.ProvisioningState = ptr.To(armcdn.AfdProvisioningStateSucceeded)
	route.Properties.DeploymentStatus = ptr.To(armcdn.DeploymentStatusSucceeded)
	return f, route
}

func routeSuffix(f backendProvisioningFixture) string {
	return "/afdEndpoints/" + f.graph.Roots.EndpointName + "/routes/" + f.graph.RouteName
}

func routeRead(f backendProvisioningFixture, route armcdn.Route) backendStep {
	return backendStep{method: http.MethodGet, suffix: routeSuffix(f), status: 200, body: route}
}

func routeProvisioningClient(t *testing.T, f backendProvisioningFixture, steps []backendStep,
	after func(int)) (*RouteProvisioner, *int) {
	t.Helper()
	options, calls := backendProvisioningOptions(t, f, steps, after)
	p, err := NewRouteProvisioner(f.snapshot.scope, cleanupCredential{}, options)
	if err != nil {
		t.Fatal(err)
	}
	return p, calls
}

func routeMutationSteps(f backendProvisioningFixture, route armcdn.Route, method string, status int) []backendStep {
	read := routeRead(f, route)
	var want interface{} = f.graph.Route
	if method == http.MethodPut {
		read.status = http.StatusNotFound
	} else {
		route.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
		want = armcdn.RouteUpdateParameters{Properties: &armcdn.RouteUpdatePropertiesParameters{
			EnabledState: ptr.To(armcdn.EnabledStateDisabled),
		}}
	}
	return append(f.reads(), read, backendStep{method: method, suffix: read.suffix, status: status, body: route, want: want})
}

func TestRouteProvisionerRequiresConvergedBackends(t *testing.T) {
	tests := []struct {
		name   string
		change func(*backendProvisioningFixture)
		steps  func(backendProvisioningFixture) []backendStep
		want   BackendProvisioningState
	}{
		{name: "missing root", want: BackendRootsPending, steps: func(f backendProvisioningFixture) []backendStep {
			read := f.roots()[0]
			read.status = http.StatusNotFound
			return []backendStep{read}
		}},
		{name: "one backend write ends invocation", want: BackendGroupPending,
			steps: func(f backendProvisioningFixture) []backendStep { return f.mutationSteps("group create") }},
		{name: "origin update ends invocation", want: BackendOriginsPending,
			steps: func(f backendProvisioningFixture) []backendStep { return f.mutationSteps("origin update") }},
		{name: "approval pending", want: BackendOriginsPending, change: func(f *backendProvisioningFixture) {
			f.changeOrigin(0, func(o *armcdn.AFDOrigin) {
				o.Properties.SharedPrivateLinkResource.Status = ptr.To(armcdn.SharedPrivateLinkResourceStatusPending)
			})
		}},
		{name: "approved while deploying", want: BackendOriginsPending, change: func(f *backendProvisioningFixture) {
			f.changeOrigin(0, func(o *armcdn.AFDOrigin) { o.Properties.DeploymentStatus = ptr.To(armcdn.DeploymentStatusInProgress) })
		}},
		{name: "group not deployed", want: BackendOriginsPending, change: func(f *backendProvisioningFixture) {
			f.group.Properties.DeploymentStatus = ptr.To(armcdn.DeploymentStatusNotStarted)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, _ := routeFixture(t)
			if tt.change != nil {
				tt.change(&f)
			}
			steps := f.reads()
			if tt.steps != nil {
				steps = tt.steps(f)
			}
			p, calls := routeProvisioningClient(t, f, steps, nil)
			got, err := p.EnsureDisabledRoute(context.Background(), f.snapshot.owner, f.snapshot.model)
			if err != nil || got.State != RouteBackendsPending || got.Backends.State != tt.want || *calls != len(steps) {
				t.Fatalf("route accessed before dependencies converged: %#v, %v, calls=%d", got, err, *calls)
			}
			if tt.name == "approval pending" && len(got.Backends.Origins.PendingApprovals) != 1 {
				t.Fatalf("manual approval diagnostic lost: %#v", got)
			}
		})
	}
}

func TestRouteProvisionerObservesDisabledRoute(t *testing.T) {
	tests := []struct {
		name   string
		change func(*armcdn.Route)
		want   RouteProvisioningState
	}{
		{name: "converged disabled route", want: DisabledRouteObserved},
		{name: "creating", want: DisabledRoutePending, change: func(r *armcdn.Route) {
			r.Properties.ProvisioningState = ptr.To(armcdn.AfdProvisioningStateCreating)
		}},
		{name: "updating", want: DisabledRoutePending, change: func(r *armcdn.Route) {
			r.Properties.ProvisioningState = ptr.To(armcdn.AfdProvisioningStateUpdating)
		}},
		{name: "deleting", want: DisabledRoutePending, change: func(r *armcdn.Route) {
			r.Properties.ProvisioningState = ptr.To(armcdn.AfdProvisioningStateDeleting)
		}},
		{name: "deploying", want: DisabledRoutePending, change: func(r *armcdn.Route) {
			r.Properties.DeploymentStatus = ptr.To(armcdn.DeploymentStatusInProgress)
		}},
		{name: "deployment not started", want: DisabledRoutePending, change: func(r *armcdn.Route) {
			r.Properties.DeploymentStatus = ptr.To(armcdn.DeploymentStatusNotStarted)
		}},
		{name: "ARM casing and empty optional arrays", want: DisabledRouteObserved, change: func(r *armcdn.Route) {
			r.ID = ptr.To(strings.ToUpper(*r.ID))
			r.Name = ptr.To(strings.ToUpper(*r.Name))
			r.Properties.EndpointName = ptr.To(strings.ToUpper(*r.Properties.EndpointName))
			r.Properties.OriginGroup.ID = ptr.To(strings.ToUpper(*r.Properties.OriginGroup.ID))
			r.Properties.OriginPath = ptr.To("")
			r.Properties.CustomDomains = []*armcdn.ActivatedResourceReference{}
			r.Properties.RuleSets = []*armcdn.ResourceReference{}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, route := routeFixture(t)
			if tt.change != nil {
				tt.change(&route)
			}
			steps := append(f.reads(), routeRead(f, route))
			p, calls := routeProvisioningClient(t, f, steps, nil)
			got, err := p.EnsureDisabledRoute(context.Background(), f.snapshot.owner, f.snapshot.model)
			if err != nil || got.State != tt.want || !got.Backends.Origins.OriginsConverged || *calls != len(steps) {
				t.Fatalf("unexpected disabled-route observation: %#v, %v, calls=%d", got, err, *calls)
			}
		})
	}
}

func TestRouteProvisionerMutationAndRestart(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			for _, status := range []int{200, 202} {
				f, route := routeFixture(t)
				if method == http.MethodPut && status == 200 {
					status = http.StatusCreated
				}
				steps := routeMutationSteps(f, route, method, status)
				_, converged := routeFixture(t)
				steps = append(steps, append(f.reads(), routeRead(f, converged))...)
				steps = append(steps, append(f.reads(), routeRead(f, converged))...)
				p, calls := routeProvisioningClient(t, f, steps, nil)
				for _, state := range []RouteProvisioningState{DisabledRoutePending, DisabledRouteObserved, DisabledRouteObserved} {
					p = &RouteProvisioner{backends: p.backends, routes: p.routes}
					got, err := p.EnsureDisabledRoute(context.Background(), f.snapshot.owner, f.snapshot.model)
					if err != nil || got.State != state {
						t.Fatalf("write/restart did not require fresh observation: %#v, %v; want %s", got, err, state)
					}
				}
				if *calls != len(steps) {
					t.Fatalf("redundant/missing write or poll: calls=%d, want %d", *calls, len(steps))
				}
			}
		})
	}
}

func TestRouteProvisionerRefusesIdentityAndRoutingDrift(t *testing.T) {
	tests := []struct {
		name   string
		change func(*armcdn.Route)
	}{
		{"foreign ARM ID", func(r *armcdn.Route) { r.ID = ptr.To(changedValue) }},
		{"contradictory route name", func(r *armcdn.Route) { r.Name = ptr.To(changedValue) }},
		{"contradictory endpoint", func(r *armcdn.Route) { r.Properties.EndpointName = ptr.To(changedValue) }},
		{"missing properties", func(r *armcdn.Route) { r.Properties = nil }},
		{"wrong backend group", func(r *armcdn.Route) { r.Properties.OriginGroup.ID = ptr.To(changedValue) }},
		{"missing backend group", func(r *armcdn.Route) { r.Properties.OriginGroup = nil }},
		{"different paths", func(r *armcdn.Route) { r.Properties.PatternsToMatch = []*string{ptr.To("/restricted/*")} }},
		{"extra path", func(r *armcdn.Route) {
			r.Properties.PatternsToMatch = append(r.Properties.PatternsToMatch, ptr.To("/extra/*"))
		}},
		{"HTTPS frontend", func(r *armcdn.Route) {
			r.Properties.SupportedProtocols = []*armcdn.AFDEndpointProtocols{ptr.To(armcdn.AFDEndpointProtocolsHTTPS)}
		}},
		{"HTTPS backend", func(r *armcdn.Route) { r.Properties.ForwardingProtocol = ptr.To(armcdn.ForwardingProtocolHTTPSOnly) }},
		{"redirect", func(r *armcdn.Route) { r.Properties.HTTPSRedirect = ptr.To(armcdn.HTTPSRedirectEnabled) }},
		{"default domain disabled", func(r *armcdn.Route) { r.Properties.LinkToDefaultDomain = ptr.To(armcdn.LinkToDefaultDomainDisabled) }},
		{"caching", func(r *armcdn.Route) { r.Properties.CacheConfiguration = &armcdn.AfdRouteCacheConfiguration{} }},
		{"enabled route with caching is not silently rewritten", func(r *armcdn.Route) {
			r.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
			r.Properties.CacheConfiguration = &armcdn.AfdRouteCacheConfiguration{}
		}},
		{"origin path rewrite", func(r *armcdn.Route) { r.Properties.OriginPath = ptr.To("/rewrite") }},
		{"rule set", func(r *armcdn.Route) { r.Properties.RuleSets = []*armcdn.ResourceReference{{ID: ptr.To(changedValue)}} }},
		{"custom domain", func(r *armcdn.Route) {
			r.Properties.CustomDomains = []*armcdn.ActivatedResourceReference{{ID: ptr.To(changedValue)}}
		}},
		{"unknown enabled state", func(r *armcdn.Route) { r.Properties.EnabledState = nil }},
		{"unknown provisioning", func(r *armcdn.Route) { r.Properties.ProvisioningState = nil }},
		{"failed provisioning", func(r *armcdn.Route) { r.Properties.ProvisioningState = ptr.To(armcdn.AfdProvisioningStateFailed) }},
		{"unknown deployment", func(r *armcdn.Route) { r.Properties.DeploymentStatus = nil }},
		{"failed deployment", func(r *armcdn.Route) { r.Properties.DeploymentStatus = ptr.To(armcdn.DeploymentStatusFailed) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, route := routeFixture(t)
			tt.change(&route)
			steps := append(f.reads(), routeRead(f, route))
			p, calls := routeProvisioningClient(t, f, steps, nil)
			got, err := p.EnsureDisabledRoute(context.Background(), f.snapshot.owner, f.snapshot.model)
			if err == nil || !reflect.DeepEqual(got, RouteProvisioningResult{}) || *calls != len(steps) {
				t.Fatalf("drifted route was adopted/rewritten: %#v, %v, calls=%d", got, err, *calls)
			}
		})
	}
}

func TestRouteProvisionerSDKFailures(t *testing.T) {
	for _, status := range []int{403, 409, 429} {
		for at := 0; at < 8; at++ {
			f, route := routeFixture(t)
			steps := append(f.reads(), routeRead(f, route))
			if at < 6 {
				steps = steps[:at+1]
			} else {
				method := http.MethodPut
				if at == 7 {
					method = http.MethodPatch
				}
				steps = routeMutationSteps(f, route, method, 202)
			}
			steps[len(steps)-1].status = status
			p, calls := routeProvisioningClient(t, f, steps, nil)
			got, err := p.EnsureDisabledRoute(context.Background(), f.snapshot.owner, f.snapshot.model)
			var responseError *azcore.ResponseError
			if !errors.As(err, &responseError) || responseError.StatusCode != status ||
				!reflect.DeepEqual(got, RouteProvisioningResult{}) || *calls != len(steps) {
				t.Fatalf("SDK failure returned partial success: %#v, %v, calls=%d", got, err, *calls)
			}
		}
	}
}

func TestRouteProvisionerCancellation(t *testing.T) {
	for at := 0; at <= 6; at++ {
		f, route := routeFixture(t)
		steps := append(f.reads(), routeRead(f, route))[:at]
		ctx, cancel := context.WithCancel(context.Background())
		p, calls := routeProvisioningClient(t, f, steps, func(n int) {
			if n == at {
				cancel()
			}
		})
		if at == 0 {
			cancel()
		}
		got, err := p.EnsureDisabledRoute(ctx, f.snapshot.owner, f.snapshot.model)
		cancel()
		if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(got, RouteProvisioningResult{}) || *calls != len(steps) {
			t.Fatalf("cancellation allowed another operation/partial success: %#v, %v, calls=%d", got, err, *calls)
		}
	}
	for _, method := range []string{http.MethodPut, http.MethodPatch} {
		for _, afterWrite := range []bool{false, true} {
			f, route := routeFixture(t)
			steps := routeMutationSteps(f, route, method, 202)
			if !afterWrite {
				steps = steps[:len(steps)-1]
			}
			ctx, cancel := context.WithCancel(context.Background())
			p, calls := routeProvisioningClient(t, f, steps, func(n int) {
				if n == len(steps) {
					cancel()
				}
			})
			got, err := p.EnsureDisabledRoute(ctx, f.snapshot.owner, f.snapshot.model)
			cancel()
			if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(got, RouteProvisioningResult{}) || *calls != len(steps) {
				t.Fatalf("canceled mutation returned partial success: %#v, %v, calls=%d", got, err, *calls)
			}
		}
	}
}

func TestRouteProvisionerPreflight(t *testing.T) {
	for _, kind := range []string{"wrong owner", "empty graph", "unsupported graph", "missing clients", "expired deadline"} {
		t.Run(kind, func(t *testing.T) {
			f, _ := routeFixture(t)
			p, _ := routeProvisioningClient(t, f, nil, nil)
			ctx := context.Background()
			switch kind {
			case "wrong owner":
				f.snapshot.owner.UID = changedValue
			case "empty graph":
				f.snapshot.model.Routes = nil
			case "unsupported graph":
				f.snapshot.model.Listeners[0].Hostname = "restricted.example.com"
			case "missing clients":
				p = &RouteProvisioner{}
			case "expired deadline":
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer cancel()
			}
			got, err := p.EnsureDisabledRoute(ctx, f.snapshot.owner, f.snapshot.model)
			if err == nil || !reflect.DeepEqual(got, RouteProvisioningResult{}) {
				t.Fatalf("invalid preflight reached ARM: %#v, %v", got, err)
			}
		})
	}
}
