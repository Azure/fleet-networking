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
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	"k8s.io/utils/ptr"
)

func disabledGraphClient(t *testing.T, f backendProvisioningFixture, steps []backendStep,
	after func(int)) (*RouteProvisioner, *int) {
	t.Helper()
	options, calls := backendProvisioningOptions(t, f, steps, after)
	transport := options.Transport
	var budget time.Time
	writes := 0
	options.Transport = cleanupTransport(func(request *http.Request) (*http.Response, error) {
		deadline, ok := request.Context().Deadline()
		if !ok || time.Until(deadline) > 30*time.Second {
			t.Fatal("composed staging has no shared bounded deadline")
		}
		if budget.IsZero() {
			budget = deadline
		} else if !budget.Equal(deadline) {
			t.Fatal("composed staging restarted the deadline between stages")
		}
		if request.Method != http.MethodGet {
			writes++
			if writes > 1 {
				t.Fatal("composed staging submitted more than one write")
			}
		}
		return transport.Do(request)
	})
	p, err := NewRouteProvisioner(f.snapshot.scope, cleanupCredential{}, options)
	if err != nil {
		t.Fatal(err)
	}
	return p, calls
}

func TestStageDisabledGraphDependencyOrder(t *testing.T) {
	tests := []struct {
		name     string
		steps    func(backendProvisioningFixture, armcdn.Route) []backendStep
		roots    RootProvisioningState
		route    RouteProvisioningState
		backends BackendProvisioningState
	}{
		{name: "create profile only", roots: ProfileProvisioningPending,
			steps: func(f backendProvisioningFixture, _ armcdn.Route) []backendStep {
				read := f.roots()[0]
				read.status = http.StatusNotFound
				return []backendStep{read, {method: http.MethodPut, status: 202,
					body: f.snapshot.profile, want: f.graph.Roots.Profile}}
			}},
		{name: "wait for profile", roots: ProfileProvisioningPending,
			steps: func(f backendProvisioningFixture, _ armcdn.Route) []backendStep {
				f.snapshot.profile.Properties.ProvisioningState = ptr.To(armcdn.ProfileProvisioningStateCreating)
				return f.roots()[:1]
			}},
		{name: "create endpoint only", roots: EndpointProvisioningPending,
			steps: func(f backendProvisioningFixture, _ armcdn.Route) []backendStep {
				reads := f.roots()
				reads[1].status = http.StatusNotFound
				return append(reads, backendStep{method: http.MethodPut, suffix: reads[1].suffix,
					status: 202, body: f.endpoint, want: f.graph.Roots.Endpoint})
			}},
		{name: "disable endpoint only", roots: EndpointProvisioningPending,
			steps: func(f backendProvisioningFixture, _ armcdn.Route) []backendStep {
				f.endpoint.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
				reads := f.roots()
				return append(reads, backendStep{method: http.MethodPatch, suffix: reads[1].suffix, status: 202,
					body: f.endpoint, want: armcdn.AFDEndpointUpdateParameters{Properties: &armcdn.AFDEndpointPropertiesUpdateParameters{
						EnabledState: ptr.To(armcdn.EnabledStateDisabled),
					}}})
			}},
		{name: "create group only", roots: DisabledRootsObserved, route: RouteBackendsPending, backends: BackendGroupPending,
			steps: func(f backendProvisioningFixture, _ armcdn.Route) []backendStep {
				return append(f.roots(), f.mutationSteps("group create")...)
			}},
		{name: "update origin only", roots: DisabledRootsObserved, route: RouteBackendsPending, backends: BackendOriginsPending,
			steps: func(f backendProvisioningFixture, _ armcdn.Route) []backendStep {
				return append(f.roots(), f.mutationSteps("origin update")...)
			}},
		{name: "create route only", roots: DisabledRootsObserved, route: DisabledRoutePending, backends: PrivateBackendsConverged,
			steps: func(f backendProvisioningFixture, route armcdn.Route) []backendStep {
				return append(f.roots(), routeMutationSteps(f, route, http.MethodPut, 202)...)
			}},
		{name: "disable route only", roots: DisabledRootsObserved, route: DisabledRoutePending, backends: PrivateBackendsConverged,
			steps: func(f backendProvisioningFixture, route armcdn.Route) []backendStep {
				return append(f.roots(), routeMutationSteps(f, route, http.MethodPatch, 202)...)
			}},
		{name: "fresh disabled graph observations", roots: DisabledRootsObserved, route: DisabledRouteObserved, backends: PrivateBackendsConverged,
			steps: func(f backendProvisioningFixture, route armcdn.Route) []backendStep {
				return completeStagingSteps(f, route)
			}},
		{name: "roots disappear between stages", roots: DisabledRootsObserved, route: RouteBackendsPending, backends: BackendRootsPending,
			steps: func(f backendProvisioningFixture, _ armcdn.Route) []backendStep {
				read := f.roots()[0]
				read.status = http.StatusNotFound
				return append(f.roots(), read)
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, route := routeFixture(t)
			steps := tt.steps(f, route)
			p, calls := disabledGraphClient(t, f, steps, nil)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			got, err := p.StageDisabledGraph(ctx, f.snapshot.owner, f.snapshot.model)
			if err != nil || got.Roots.State != tt.roots || got.Route.State != tt.route ||
				got.Route.Backends.State != tt.backends || got.InventoryVerified != (tt.route == DisabledRouteObserved) || *calls != len(steps) {
				t.Fatalf("incorrect staged dependency order: %#v, %v, calls=%d", got, err, *calls)
			}
		})
	}
}

func TestStageDisabledGraphApprovalAndRestart(t *testing.T) {
	for _, approved := range []bool{false, true, true} {
		f, route := routeFixture(t)
		if !approved {
			f.changeOrigin(0, func(origin *armcdn.AFDOrigin) {
				origin.Properties.SharedPrivateLinkResource.Status = ptr.To(armcdn.SharedPrivateLinkResourceStatusPending)
			})
		}
		steps := append(f.roots(), f.reads()...)
		if approved {
			steps = append(steps, routeRead(f, route))
			steps = append(steps, supportedInventorySteps(f, route)...)
			steps = append(steps, unsupportedInventorySteps()...)
		}
		p, calls := disabledGraphClient(t, f, steps, nil)
		got, err := p.StageDisabledGraph(context.Background(), f.snapshot.owner, f.snapshot.model)
		if err != nil || *calls != len(steps) {
			t.Fatalf("restart failed to reobserve dependencies: %#v, %v, calls=%d", got, err, *calls)
		}
		if approved {
			if got.Route.State != DisabledRouteObserved || !got.InventoryVerified {
				t.Fatalf("approved disabled graph was not observed: %#v", got)
			}
		} else if got.Route.State != RouteBackendsPending || len(got.Route.Backends.Origins.PendingApprovals) != 1 {
			t.Fatalf("asynchronous approval diagnostic lost: %#v", got)
		}
	}
}

func TestStageDisabledGraphErrorsAndCancellation(t *testing.T) {
	f, route := routeFixture(t)
	reads := completeStagingSteps(f, route)
	for _, status := range []int{403, 409, 429} {
		for at := range reads {
			steps := append([]backendStep(nil), reads[:at+1]...)
			steps[at].status = status
			p, calls := disabledGraphClient(t, f, steps, nil)
			got, err := p.StageDisabledGraph(context.Background(), f.snapshot.owner, f.snapshot.model)
			var responseError *azcore.ResponseError
			if !errors.As(err, &responseError) || responseError.StatusCode != status ||
				!reflect.DeepEqual(got, DisabledGraphStagingResult{}) || *calls != len(steps) {
				t.Fatalf("staging failure returned partial success: %#v, %v, calls=%d", got, err, *calls)
			}
		}
	}
	for at := 0; at <= len(reads); at++ {
		ctx, cancel := context.WithCancel(context.Background())
		steps := reads[:at]
		p, calls := disabledGraphClient(t, f, steps, func(n int) {
			if n == at {
				cancel()
			}
		})
		if at == 0 {
			cancel()
		}
		got, err := p.StageDisabledGraph(ctx, f.snapshot.owner, f.snapshot.model)
		cancel()
		if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(got, DisabledGraphStagingResult{}) || *calls != len(steps) {
			t.Fatalf("cancellation allowed partial success/another operation: %#v, %v, calls=%d", got, err, *calls)
		}
	}
}

func TestStageDisabledGraphPreflight(t *testing.T) {
	const (
		emptyGraphCase = "empty graph"
		wrongOwnerCase = "wrong owner"
	)
	for _, kind := range []string{wrongOwnerCase, emptyGraphCase, "missing routes client", "missing backend client",
		"missing domain inventory client", "missing rule-set inventory client", "missing policy inventory client", "missing secret inventory client", "nil provisioner"} {
		t.Run(kind, func(t *testing.T) {
			f, _ := routeFixture(t)
			p, calls := disabledGraphClient(t, f, nil, nil)
			switch kind {
			case wrongOwnerCase:
				f.snapshot.owner.UID = changedValue
			case emptyGraphCase:
				f.snapshot.model.Routes = nil
			case "missing routes client":
				p.routes = nil
			case "missing backend client":
				p.backends.origins = nil
			case "missing domain inventory client":
				p.domains = nil
			case "missing rule-set inventory client":
				p.ruleSets = nil
			case "missing policy inventory client":
				p.policies = nil
			case "missing secret inventory client":
				p.secrets = nil
			case "nil provisioner":
				p = nil
			}
			got, err := p.StageDisabledGraph(context.Background(), f.snapshot.owner, f.snapshot.model)
			if err == nil || !reflect.DeepEqual(got, DisabledGraphStagingResult{}) || *calls != 0 {
				t.Fatalf("invalid staging reached ARM: %#v, %v, calls=%d", got, err, *calls)
			}
		})
	}
}
