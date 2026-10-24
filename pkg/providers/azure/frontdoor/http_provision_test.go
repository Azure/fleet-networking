/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package frontdoor

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	"k8s.io/utils/ptr"
)

const (
	routeActivationPhase = "route activation"
	originCollection     = "origins"
	routeCollection      = "routes"
)

func httpPreviewReads(f backendProvisioningFixture, route armcdn.Route) []backendStep {
	return append(f.roots(), routeRead(f, route))
}

func httpPreviewInventory(f backendProvisioningFixture, route armcdn.Route) []backendStep {
	return append(supportedInventorySteps(f, route), unsupportedInventorySteps()...)
}

func enableRouteStep(f backendProvisioningFixture, route armcdn.Route) backendStep {
	return backendStep{method: http.MethodPatch, suffix: routeSuffix(f), status: http.StatusAccepted, body: route,
		want: armcdn.RouteUpdateParameters{Properties: &armcdn.RouteUpdatePropertiesParameters{
			EnabledState: ptr.To(armcdn.EnabledStateEnabled),
		}}}
}

func enableEndpointStep(f backendProvisioningFixture) backendStep {
	return backendStep{method: http.MethodPatch, suffix: f.roots()[1].suffix, status: http.StatusAccepted, body: f.endpoint,
		want: armcdn.AFDEndpointUpdateParameters{Properties: &armcdn.AFDEndpointPropertiesUpdateParameters{
			EnabledState: ptr.To(armcdn.EnabledStateEnabled),
		}}}
}

func suspendEndpointSteps(f backendProvisioningFixture) []backendStep {
	return append(f.roots(), backendStep{method: http.MethodPatch, suffix: f.roots()[1].suffix, status: http.StatusAccepted, body: f.endpoint,
		want: armcdn.AFDEndpointUpdateParameters{Properties: &armcdn.AFDEndpointPropertiesUpdateParameters{
			EnabledState: ptr.To(armcdn.EnabledStateDisabled),
		}}})
}

func TestReconcileHTTPPreviewLifecycle(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*backendProvisioningFixture, *armcdn.Route) []backendStep
		state HTTPPreviewProvisioningState
	}{
		{"create profile", func(f *backendProvisioningFixture, _ *armcdn.Route) []backendStep {
			read := f.roots()[0]
			read.status = http.StatusNotFound
			return []backendStep{read, read, {method: http.MethodPut, status: http.StatusAccepted,
				body: f.snapshot.profile, want: f.graph.Roots.Profile}}
		}, HTTPPreviewStaging},
		{"manual approval pending", func(f *backendProvisioningFixture, route *armcdn.Route) []backendStep {
			f.changeOrigin(0, func(origin *armcdn.AFDOrigin) {
				origin.Properties.SharedPrivateLinkResource.Status = ptr.To(armcdn.SharedPrivateLinkResourceStatusPending)
			})
			return append(httpPreviewReads(*f, *route), append(f.roots(), f.reads()...)...)
		}, HTTPPreviewStaging},
		{"enable route only", func(f *backendProvisioningFixture, route *armcdn.Route) []backendStep {
			return append(append(httpPreviewReads(*f, *route), completeStagingSteps(*f, *route)...), enableRouteStep(*f, *route))
		}, HTTPPreviewRouteEnabling},
		{"enable endpoint only after fresh enabled-route observation", func(f *backendProvisioningFixture, route *armcdn.Route) []backendStep {
			route.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
			return append(append(httpPreviewReads(*f, *route), httpPreviewInventory(*f, *route)...), enableEndpointStep(*f))
		}, HTTPPreviewEndpointEnabling},
		{"enabled graph is a no-op", func(f *backendProvisioningFixture, route *armcdn.Route) []backendStep {
			f.endpoint.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
			route.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
			return append(httpPreviewReads(*f, *route), httpPreviewInventory(*f, *route)...)
		}, HTTPPreviewProgrammed},
		{"activation is still deploying", func(f *backendProvisioningFixture, _ *armcdn.Route) []backendStep {
			f.endpoint.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
			f.endpoint.Properties.DeploymentStatus = ptr.To(armcdn.DeploymentStatusInProgress)
			return f.roots()
		}, HTTPPreviewPending},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, route := routeFixture(t)
			steps := tt.setup(&f, &route)
			p, calls := disabledGraphClient(t, f, steps, nil)
			got, err := p.ReconcileHTTPPreview(context.Background(), f.snapshot.owner, f.snapshot.model)
			if err != nil || got.State != tt.state || *calls != len(steps) {
				t.Fatalf("unexpected HTTP progress: %+v, %v, calls=%d", got, err, *calls)
			}
			if tt.state == HTTPPreviewProgrammed {
				if got.EndpointHostName != *f.endpoint.Properties.HostName {
					t.Fatalf("hostname was not observed from Azure: %+v", got)
				}
			} else if got.EndpointHostName != "" {
				t.Fatalf("incomplete graph published a hostname: %+v", got)
			}
			if tt.name == "manual approval pending" && len(got.Staging.Route.Backends.Origins.PendingApprovals) != 1 {
				t.Fatalf("lost asynchronous approval diagnostic: %+v", got)
			}
		})
	}
}

func TestReconcileHTTPPreviewActivationFailures(t *testing.T) {
	for _, endpoint := range []bool{false, true} {
		for _, status := range []int{http.StatusForbidden, http.StatusConflict, http.StatusTooManyRequests} {
			f, route := routeFixture(t)
			var steps []backendStep
			if endpoint {
				route.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
				steps = append(append(httpPreviewReads(f, route), httpPreviewInventory(f, route)...), enableEndpointStep(f))
			} else {
				steps = append(append(httpPreviewReads(f, route), completeStagingSteps(f, route)...), enableRouteStep(f, route))
			}
			steps[len(steps)-1].status = status
			p, calls := disabledGraphClient(t, f, steps, nil)
			got, err := p.ReconcileHTTPPreview(context.Background(), f.snapshot.owner, f.snapshot.model)
			var responseError *azcore.ResponseError
			if !errors.As(err, &responseError) || responseError.StatusCode != status ||
				!reflect.DeepEqual(got, HTTPPreviewProvisioningResult{}) || *calls != len(steps) {
				t.Fatalf("activation failure returned readiness: %+v, %v, calls=%d", got, err, *calls)
			}
		}
	}
}

func TestReconcileHTTPPreviewCancellation(t *testing.T) {
	for _, phase := range []string{routeActivationPhase, "endpoint activation", "enabled graph"} {
		t.Run(phase, func(t *testing.T) {
			f, route := routeFixture(t)
			if phase != routeActivationPhase {
				route.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
			}
			if phase == "enabled graph" {
				f.endpoint.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
			}
			inventory := httpPreviewInventory(f, route)
			if phase == routeActivationPhase {
				inventory = completeStagingSteps(f, route)
			}
			reads := append(httpPreviewReads(f, route), inventory...)
			testHTTPPreviewCancellation(t, f, reads)
		})
	}
}

func testHTTPPreviewCancellation(t *testing.T, f backendProvisioningFixture, reads []backendStep) {
	t.Helper()
	for at := 0; at <= len(reads); at++ {
		ctx, cancel := context.WithCancel(context.Background())
		p, calls := disabledGraphClient(t, f, reads[:at], func(n int) {
			if n == at {
				cancel()
			}
		})
		if at == 0 {
			cancel()
		}
		got, err := p.ReconcileHTTPPreview(ctx, f.snapshot.owner, f.snapshot.model)
		cancel()
		if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(got, HTTPPreviewProvisioningResult{}) || *calls != at {
			t.Fatalf("cancellation allowed activation/readiness: %+v, %v, calls=%d", got, err, *calls)
		}
	}
}

func TestReconcileHTTPPreviewActiveGraphSafety(t *testing.T) {
	tests := []struct {
		name    string
		change  func(*backendProvisioningFixture, *armcdn.Route)
		failAt  string
		page    interface{}
		suspend bool
	}{
		{name: "approval revoked", failAt: originCollection, suspend: true, change: func(f *backendProvisioningFixture, _ *armcdn.Route) {
			f.changeOrigin(0, func(origin *armcdn.AFDOrigin) {
				origin.Properties.SharedPrivateLinkResource.Status = ptr.To(armcdn.SharedPrivateLinkResourceStatusRejected)
			})
		}},
		{name: "approval became pending", failAt: originCollection, suspend: true, change: func(f *backendProvisioningFixture, _ *armcdn.Route) {
			f.changeOrigin(0, func(origin *armcdn.AFDOrigin) {
				origin.Properties.SharedPrivateLinkResource.Status = ptr.To(armcdn.SharedPrivateLinkResourceStatusPending)
			})
		}},
		{name: "private configuration changed", failAt: originCollection, suspend: true, change: func(f *backendProvisioningFixture, _ *armcdn.Route) {
			f.changeOrigin(0, func(origin *armcdn.AFDOrigin) {
				origin.Properties.SharedPrivateLinkResource = nil
			})
		}},
		{name: "route disabled", suspend: true, change: func(_ *backendProvisioningFixture, route *armcdn.Route) {
			route.Properties.EnabledState = ptr.To(armcdn.EnabledStateDisabled)
		}},
		{name: "route policy changed", failAt: routeCollection, suspend: true, change: func(_ *backendProvisioningFixture, route *armcdn.Route) {
			route.Properties.PatternsToMatch = []*string{ptr.To("/changed")}
		}},
		{name: "unknown origin approval is not withdrawal", failAt: originCollection, change: func(f *backendProvisioningFixture, _ *armcdn.Route) {
			f.changeOrigin(0, func(origin *armcdn.AFDOrigin) {
				origin.Properties.SharedPrivateLinkResource.Status = nil
			})
		}},
		{name: "missing route in explicit inventory", failAt: routeCollection, suspend: true, page: map[string]interface{}{
			inventoryValueField: []interface{}{},
		}},
		{name: "malformed route inventory", failAt: routeCollection, page: map[string]interface{}{inventoryValueField: []interface{}{nil}}},
		{name: "ambiguous endpoint inventory", failAt: "afdEndpoints", page: map[string]interface{}{inventoryValueField: nil}},
		{name: "unsupported domain", failAt: "customDomains", suspend: true,
			page: map[string]interface{}{inventoryValueField: []interface{}{map[string]string{"id": "unexpected-domain"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, route := routeFixture(t)
			f.endpoint.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
			route.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
			if tt.change != nil {
				tt.change(&f, &route)
			}
			steps := httpPreviewReads(f, route)
			if tt.failAt != "" {
				for _, step := range httpPreviewInventory(f, route) {
					if strings.HasSuffix(step.suffix, "/"+tt.failAt) {
						if tt.page != nil {
							step.body = tt.page
						}
						steps = append(steps, step)
						break
					}
					steps = append(steps, step)
				}
			}
			if tt.suspend {
				steps = append(steps, suspendEndpointSteps(f)...)
			}
			p, calls := disabledGraphClient(t, f, steps, nil)
			got, err := p.ReconcileHTTPPreview(context.Background(), f.snapshot.owner, f.snapshot.model)
			if *calls != len(steps) || got.EndpointHostName != "" {
				t.Fatalf("unsafe progression: %+v, %v, calls=%d", got, err, *calls)
			}
			if tt.suspend {
				if err != nil || got.State != HTTPPreviewSuspending || got.Diagnostic == "" ||
					got.Suspension.State != EndpointSuspensionPending {
					t.Fatalf("confirmed drift did not request endpoint suspension: %+v, %v", got, err)
				}
			} else if err == nil || !reflect.DeepEqual(got, HTTPPreviewProvisioningResult{}) {
				t.Fatalf("ambiguous evidence authorized a write/readiness: %+v, %v", got, err)
			}
		})
	}
}

func TestReconcileHTTPPreviewReadFailures(t *testing.T) {
	f, route := routeFixture(t)
	f.endpoint.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
	route.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
	reads := append(httpPreviewReads(f, route), httpPreviewInventory(f, route)...)
	for at := range reads {
		t.Run(fmt.Sprint(at), func(t *testing.T) {
			steps := append([]backendStep(nil), reads[:at+1]...)
			steps[at].status = http.StatusForbidden
			p, calls := disabledGraphClient(t, f, steps, nil)
			got, err := p.ReconcileHTTPPreview(context.Background(), f.snapshot.owner, f.snapshot.model)
			var responseError *azcore.ResponseError
			if !errors.As(err, &responseError) || responseError.StatusCode != http.StatusForbidden ||
				!reflect.DeepEqual(got, HTTPPreviewProvisioningResult{}) || *calls != len(steps) {
				t.Fatalf("read failure was reinterpreted as confirmed drift: %+v, %v, calls=%d", got, err, *calls)
			}
		})
	}
}

func TestReconcileHTTPPreviewRestartAndCleanup(t *testing.T) {
	f, route := routeFixture(t)
	for _, state := range []HTTPPreviewProvisioningState{
		HTTPPreviewRouteEnabling, HTTPPreviewEndpointEnabling, HTTPPreviewProgrammed, HTTPPreviewProgrammed,
	} {
		var steps []backendStep
		switch state {
		case HTTPPreviewRouteEnabling:
			steps = append(append(httpPreviewReads(f, route), completeStagingSteps(f, route)...), enableRouteStep(f, route))
		case HTTPPreviewEndpointEnabling:
			steps = append(append(httpPreviewReads(f, route), httpPreviewInventory(f, route)...), enableEndpointStep(f))
		case HTTPPreviewProgrammed:
			steps = append(httpPreviewReads(f, route), httpPreviewInventory(f, route)...)
		}
		p, calls := disabledGraphClient(t, f, steps, nil)
		got, err := p.ReconcileHTTPPreview(context.Background(), f.snapshot.owner, f.snapshot.model)
		if err != nil || got.State != state || *calls != len(steps) {
			t.Fatalf("restarted provider lost lifecycle progress: %+v, %v, calls=%d", got, err, *calls)
		}
		switch state {
		case HTTPPreviewRouteEnabling:
			route.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
		case HTTPPreviewEndpointEnabling:
			f.endpoint.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
		}
	}
	for _, absent := range []bool{false, true} {
		steps := f.roots()[:1]
		if absent {
			steps[0].status = http.StatusNotFound
		} else {
			steps = append(steps, backendStep{method: http.MethodDelete, status: http.StatusAccepted})
		}
		options, calls := backendProvisioningOptions(t, f, steps, nil)
		cleanup, err := NewProfileCleanup(f.snapshot.scope, cleanupCredential{}, options)
		if err != nil {
			t.Fatal(err)
		}
		gone, err := cleanup.DeleteProfile(context.Background(), f.snapshot.owner)
		if err != nil || gone != absent || *calls != len(steps) {
			t.Fatalf("cleanup did not require observed owned-root absence: %v, %v, calls=%d", gone, err, *calls)
		}
	}
}

func TestReconcileHTTPPreviewApprovalLostBeforeEndpointActivation(t *testing.T) {
	f, route := routeFixture(t)
	route.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
	f.changeOrigin(0, func(origin *armcdn.AFDOrigin) {
		origin.Properties.SharedPrivateLinkResource.Status = ptr.To(armcdn.SharedPrivateLinkResourceStatusPending)
	})
	inventory := supportedInventorySteps(f, route)
	steps := append(httpPreviewReads(f, route), inventory...)
	steps = append(steps, append(f.roots(), f.reads()...)...)
	p, calls := disabledGraphClient(t, f, steps, nil)
	got, err := p.ReconcileHTTPPreview(context.Background(), f.snapshot.owner, f.snapshot.model)
	if err != nil || got.State != HTTPPreviewStaging || got.Diagnostic == "" || got.EndpointHostName != "" ||
		len(got.Staging.Route.Backends.Origins.PendingApprovals) != 1 || *calls != len(steps) {
		t.Fatalf("lost approval allowed endpoint activation: %+v, %v, calls=%d", got, err, *calls)
	}
	f.changeOrigin(0, func(origin *armcdn.AFDOrigin) {
		origin.Properties.SharedPrivateLinkResource.Status = ptr.To(armcdn.SharedPrivateLinkResourceStatusApproved)
	})
	steps = append(append(httpPreviewReads(f, route), httpPreviewInventory(f, route)...), enableEndpointStep(f))
	p, calls = disabledGraphClient(t, f, steps, nil)
	got, err = p.ReconcileHTTPPreview(context.Background(), f.snapshot.owner, f.snapshot.model)
	if err != nil || got.State != HTTPPreviewEndpointEnabling || got.EndpointHostName != "" || *calls != len(steps) {
		t.Fatalf("restored approval did not resume bounded activation: %+v, %v, calls=%d", got, err, *calls)
	}
}

func TestReconcileHTTPPreviewUnexpectedPublicOrigin(t *testing.T) {
	f, route := routeFixture(t)
	f.endpoint.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
	route.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
	steps := append(httpPreviewReads(f, route), supportedInventorySteps(f, route)...)
	page := steps[len(steps)-1].body.(map[string]interface{})
	page[inventoryValueField] = append(page[inventoryValueField].([]interface{}), armcdn.AFDOrigin{
		ID:         ptr.To(f.graph.Roots.ProfileID + "/originGroups/" + f.graph.Backends.OriginGroups[0].Name + "/origins/unexpected"),
		Name:       ptr.To("unexpected"),
		Properties: &armcdn.AFDOriginProperties{HostName: ptr.To("public.example")},
	})
	steps = append(steps, suspendEndpointSteps(f)...)
	p, calls := disabledGraphClient(t, f, steps, nil)
	got, err := p.ReconcileHTTPPreview(context.Background(), f.snapshot.owner, f.snapshot.model)
	if err != nil || got.State != HTTPPreviewSuspending || got.Diagnostic == "" ||
		got.EndpointHostName != "" || *calls != len(steps) {
		t.Fatalf("unexpected public origin did not suspend the endpoint: %+v, %v, calls=%d", got, err, *calls)
	}
}

func TestReconcileHTTPPreviewPreconditions(t *testing.T) {
	const (
		foreignProfileCase = "foreign profile"
		invalidOwnerCase   = "invalid owner"
	)
	for _, scenario := range []string{"nil client", "incomplete clients", invalidOwnerCase, foreignProfileCase} {
		t.Run(scenario, func(t *testing.T) {
			f, _ := routeFixture(t)
			owner := f.snapshot.owner
			var steps []backendStep
			if scenario == foreignProfileCase {
				f.snapshot.profile.Tags["networking.fleet.azure.com.hub-uid"] = ptr.To(changedValue)
				steps = f.roots()[:1]
			}
			p, calls := disabledGraphClient(t, f, steps, nil)
			switch scenario {
			case "nil client":
				p = nil
			case "incomplete clients":
				p.secrets = nil
			case invalidOwnerCase:
				owner.UID = changedValue
			}
			got, err := p.ReconcileHTTPPreview(context.Background(), owner, f.snapshot.model)
			if err == nil || !reflect.DeepEqual(got, HTTPPreviewProvisioningResult{}) || *calls != len(steps) {
				t.Fatalf("invalid preconditions authorized ARM access: %+v, %v, calls=%d", got, err, *calls)
			}
		})
	}
}

func TestVerifyEnabledHTTPPreviewReadOnly(t *testing.T) {
	const (
		enabledGraphCase     = "enabled graph"
		disabledEndpointCase = "disabled endpoint"
		pendingApprovalCase  = "approval pending"
	)
	for _, scenario := range []string{enabledGraphCase, disabledEndpointCase, pendingApprovalCase} {
		t.Run(scenario, func(t *testing.T) {
			f, route := routeFixture(t)
			f.endpoint.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
			route.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
			if scenario == disabledEndpointCase {
				f.endpoint.Properties.EnabledState = ptr.To(armcdn.EnabledStateDisabled)
			}
			if scenario == pendingApprovalCase {
				f.changeOrigin(0, func(origin *armcdn.AFDOrigin) {
					origin.Properties.SharedPrivateLinkResource.Status = ptr.To(armcdn.SharedPrivateLinkResourceStatusPending)
				})
			}
			steps := f.roots()
			if scenario != disabledEndpointCase {
				inventory := httpPreviewInventory(f, route)
				if scenario == pendingApprovalCase {
					inventory = inventory[:len(inventory)-len(unsupportedInventorySteps())]
				}
				steps = append(steps, inventory...)
			}
			p, calls := disabledGraphClient(t, f, steps, nil)
			hostname, err := p.VerifyEnabledHTTPPreview(context.Background(), f.snapshot.owner, f.snapshot.model)
			if (err == nil) != (scenario == enabledGraphCase) || *calls != len(steps) {
				t.Fatalf("read-only graph verification: %q, %v, calls=%d", hostname, err, *calls)
			}
			if err != nil && hostname != "" {
				t.Fatalf("unverified graph returned a hostname: %q", hostname)
			}
			if err == nil && hostname != *f.endpoint.Properties.HostName {
				t.Fatalf("verification did not return the observed Azure hostname: %q", hostname)
			}
		})
	}
}
