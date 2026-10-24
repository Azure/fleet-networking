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
	"sort"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	"k8s.io/utils/ptr"
)

func retiredOrigin(t *testing.T, f backendProvisioningFixture, identity string) armcdn.AFDOrigin {
	t.Helper()
	plan, err := PlanBackends(f.snapshot.model)
	if err != nil {
		t.Fatal(err)
	}
	group := plan.OriginGroups[0]
	origin := group.Origins[0]
	cluster, uid := origin.Cluster, origin.ServiceUID+"-"+identity
	if identity == "removed-member" {
		cluster, uid = origin.Cluster+"-retired", origin.ServiceUID
	}
	if identity == "replaced-pls" {
		uid = origin.ServiceUID
		origin.PrivateLink.ResourceID += "-retired"
	}
	name := resourceName("fleet-origin", group.Name, cluster, uid,
		origin.PrivateLink.ResourceID, origin.PrivateLink.Location, origin.HostName)
	actual := inventoryCopy(t, f.originRead(0).body.(armcdn.AFDOrigin))
	actual.Name = ptr.To(name)
	actual.ID = ptr.To(f.graph.Roots.ProfileID + "/originGroups/" + group.Name + "/origins/" + name)
	actual.Properties.SharedPrivateLinkResource.PrivateLink.ID = ptr.To(origin.PrivateLink.ResourceID)
	return actual
}

func staleInventorySteps(f backendProvisioningFixture, route armcdn.Route, stale ...armcdn.AFDOrigin) []backendStep {
	steps := completeStagingSteps(f, route)
	at := len(supportedGraphStagingSteps(f, route)) - 1
	page := steps[at].body.(map[string]interface{})
	items := page[inventoryValueField].([]interface{})
	for _, origin := range stale {
		items = append(items, origin)
	}
	page[inventoryValueField] = items
	return steps
}

func staleDeletionSteps(f backendProvisioningFixture, route armcdn.Route, stale ...armcdn.AFDOrigin) []backendStep {
	steps := staleInventorySteps(f, route, stale...)
	ordered := append([]armcdn.AFDOrigin(nil), stale...)
	sort.Slice(ordered, func(i, j int) bool { return strings.ToLower(*ordered[i].ID) < strings.ToLower(*ordered[j].ID) })
	selected := ordered[0]
	suffix := "/originGroups/" + f.graph.Backends.OriginGroups[0].Name + "/origins/" + *selected.Name
	steps = append(steps, backendStep{method: http.MethodGet, suffix: suffix, status: http.StatusOK, body: selected})
	steps = append(steps, f.roots()...)
	return append(steps, backendStep{method: http.MethodDelete, suffix: suffix, status: http.StatusNoContent, body: map[string]interface{}{}})
}

func TestStageDisabledGraphPrunesOneOwnedStaleOrigin(t *testing.T) {
	for _, status := range []int{http.StatusAccepted, http.StatusNoContent, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			f, route := routeFixture(t)
			first, second := retiredOrigin(t, f, "old-service-uid"), retiredOrigin(t, f, "removed-member")
			steps := staleDeletionSteps(f, route, second, first)
			steps[len(steps)-1].status = status
			p, calls := disabledGraphClient(t, f, steps, nil)
			got, err := p.StageDisabledGraph(context.Background(), f.snapshot.owner, f.snapshot.model)
			if err != nil || got.InventoryVerified || got.StaleOriginsPending != 2 ||
				got.Route.State != DisabledRouteObserved || *calls != len(steps) {
				t.Fatalf("owned stale origin was not bounded pending cleanup: %#v, %v, calls=%d", got, err, *calls)
			}
		})
	}
}

func TestStageDisabledGraphStaleCleanupRestartRequiresAbsence(t *testing.T) {
	f, route := routeFixture(t)
	stale := retiredOrigin(t, f, "restart")
	steps := staleDeletionSteps(f, route, stale)
	p, calls := disabledGraphClient(t, f, steps, nil)
	got, err := p.StageDisabledGraph(context.Background(), f.snapshot.owner, f.snapshot.model)
	if err != nil || got.InventoryVerified || got.StaleOriginsPending != 1 || *calls != len(steps) {
		t.Fatalf("deletion submission claimed absence: %#v, %v", got, err)
	}
	stale.Properties.ProvisioningState = ptr.To(armcdn.AfdProvisioningStateDeleting)
	steps = staleInventorySteps(f, route, stale)
	p, calls = disabledGraphClient(t, f, steps, nil)
	got, err = p.StageDisabledGraph(context.Background(), f.snapshot.owner, f.snapshot.model)
	if err != nil || got.InventoryVerified || got.StaleOriginsPending != 1 || *calls != len(steps) {
		t.Fatalf("restart repeated an ongoing stale deletion: %#v, %v", got, err)
	}
	steps = completeStagingSteps(f, route)
	p, calls = disabledGraphClient(t, f, steps, nil)
	got, err = p.StageDisabledGraph(context.Background(), f.snapshot.owner, f.snapshot.model)
	if err != nil || !got.InventoryVerified || got.StaleOriginsPending != 0 || *calls != len(steps) {
		t.Fatalf("fresh absence did not restore exact inventory: %#v, %v", got, err)
	}
}

func TestStageDisabledGraphPrunesEquivalentAndFailedStaleOrigins(t *testing.T) {
	tests := []struct {
		name   string
		change func(*armcdn.AFDOrigin)
	}{
		{name: "replaced-pls", change: func(*armcdn.AFDOrigin) {}},
		{name: "stale ARM casing equivalent", change: func(o *armcdn.AFDOrigin) {
			o.ID, o.Name = ptr.To(strings.ToUpper(*o.ID)), ptr.To(strings.ToUpper(*o.Name))
		}},
		{name: "stale approval rejected but removable", change: func(o *armcdn.AFDOrigin) {
			o.Properties.SharedPrivateLinkResource.Status = ptr.To(armcdn.SharedPrivateLinkResourceStatusRejected)
		}},
		{name: "stale provisioning failed but removable", change: func(o *armcdn.AFDOrigin) {
			o.Properties.ProvisioningState = ptr.To(armcdn.AfdProvisioningStateFailed)
		}},
		{name: "stale deployment failed but removable", change: func(o *armcdn.AFDOrigin) {
			o.Properties.DeploymentStatus = ptr.To(armcdn.DeploymentStatusFailed)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, route := routeFixture(t)
			stale := retiredOrigin(t, f, tt.name)
			tt.change(&stale)
			steps := staleDeletionSteps(f, route, stale)
			p, calls := disabledGraphClient(t, f, steps, nil)
			got, err := p.StageDisabledGraph(context.Background(), f.snapshot.owner, f.snapshot.model)
			if err != nil || got.InventoryVerified || got.StaleOriginsPending != 1 || *calls != len(steps) {
				t.Fatalf("eligible private stale origin could not be removed: %#v, %v, calls=%d", got, err, *calls)
			}
		})
	}
}

func TestStageDisabledGraphRefusesUnverifiableStaleOrigins(t *testing.T) {
	tests := []struct {
		name   string
		change func(*armcdn.AFDOrigin)
	}{
		{name: "unrecognized stale name", change: func(o *armcdn.AFDOrigin) { o.Name = ptr.To("unrecognized-cleanup-origin") }},
		{name: "stale name absent", change: func(o *armcdn.AFDOrigin) { o.Name = nil }},
		{name: "stale ID absent", change: func(o *armcdn.AFDOrigin) { o.ID = nil }},
		{name: "stale foreign parent", change: func(o *armcdn.AFDOrigin) { o.ID = ptr.To(changedValue) }},
		{name: "stale properties absent", change: func(o *armcdn.AFDOrigin) { o.Properties = nil }},
		{name: "stale public conversion", change: func(o *armcdn.AFDOrigin) { o.Properties.SharedPrivateLinkResource = nil }},
		{name: "stale public IP", change: func(o *armcdn.AFDOrigin) { o.Properties.HostName = ptr.To("8.8.8.8") }},
		{name: "stale invalid PLS", change: func(o *armcdn.AFDOrigin) {
			o.Properties.SharedPrivateLinkResource.PrivateLink.ID = ptr.To(changedValue)
		}},
		{name: "stale incorrect group", change: func(o *armcdn.AFDOrigin) { o.Properties.OriginGroupName = ptr.To(changedValue) }},
		{name: "stale unexpected target group", change: func(o *armcdn.AFDOrigin) { o.Properties.SharedPrivateLinkResource.GroupID = ptr.To(changedValue) }},
		{name: "stale provisioning unknown", change: func(o *armcdn.AFDOrigin) { o.Properties.ProvisioningState = nil }},
		{name: "stale deployment unknown", change: func(o *armcdn.AFDOrigin) { o.Properties.DeploymentStatus = nil }},
		{name: "stale approval unknown", change: func(o *armcdn.AFDOrigin) { o.Properties.SharedPrivateLinkResource.Status = nil }},
		{name: "stale port mismatches group", change: func(o *armcdn.AFDOrigin) { o.Properties.HTTPPort = ptr.To(ptr.Deref(o.Properties.HTTPPort, 0) + 1) }},
		{name: "stale weight malformed", change: func(o *armcdn.AFDOrigin) { o.Properties.Weight = ptr.To(int32(0)) }},
		{name: "stale Host override", change: func(o *armcdn.AFDOrigin) { o.Properties.OriginHostHeader = ptr.To(changedValue) }},
		{name: "stale certificate policy changed", change: func(o *armcdn.AFDOrigin) { o.Properties.EnforceCertificateNameCheck = ptr.To(false) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, route := routeFixture(t)
			stale := retiredOrigin(t, f, tt.name)
			tt.change(&stale)
			steps := staleInventorySteps(f, route, stale)
			stop := len(supportedGraphStagingSteps(f, route))
			p, calls := disabledGraphClient(t, f, steps[:stop], nil)
			got, err := p.StageDisabledGraph(context.Background(), f.snapshot.owner, f.snapshot.model)
			if err == nil || !reflect.DeepEqual(got, DisabledGraphStagingResult{}) || *calls != stop {
				t.Fatalf("unverifiable origin authorized cleanup: %#v, %v, calls=%d", got, err, *calls)
			}
		})
	}
}

func TestStageDisabledGraphStaleCleanupRequiresOtherInventory(t *testing.T) {
	for collection := range unsupportedInventorySteps() {
		f, route := routeFixture(t)
		steps := staleInventorySteps(f, route, retiredOrigin(t, f, "protected"))
		at := len(supportedGraphStagingSteps(f, route)) + collection
		steps[at].body = map[string]interface{}{inventoryValueField: []interface{}{nil}}
		steps = steps[:at+1]
		p, calls := disabledGraphClient(t, f, steps, nil)
		got, err := p.StageDisabledGraph(context.Background(), f.snapshot.owner, f.snapshot.model)
		if err == nil || !reflect.DeepEqual(got, DisabledGraphStagingResult{}) || *calls != len(steps) {
			t.Fatalf("unsupported assets did not block stale cleanup: %#v, %v, calls=%d", got, err, *calls)
		}
	}
}

func TestStageDisabledGraphStaleCleanupRechecksRoots(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		f, route := routeFixture(t)
		steps := staleDeletionSteps(f, route, retiredOrigin(t, f, "root-drift"))
		at := len(steps) - 3
		if enabled {
			at++
			endpoint := inventoryCopy(t, f.endpoint)
			endpoint.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
			steps[at].body = endpoint
		} else {
			profile := inventoryCopy(t, f.snapshot.profile)
			profile.Tags = nil
			steps[at].body = profile
		}
		steps = steps[:at+1]
		p, calls := disabledGraphClient(t, f, steps, nil)
		got, err := p.StageDisabledGraph(context.Background(), f.snapshot.owner, f.snapshot.model)
		if err == nil || !reflect.DeepEqual(got, DisabledGraphStagingResult{}) || *calls != len(steps) {
			t.Fatalf("stale cleanup used old root proof: %#v, %v, calls=%d", got, err, *calls)
		}
	}
}

func TestStageDisabledGraphStaleCleanupRechecksCandidate(t *testing.T) {
	tests := []struct {
		name    string
		pending bool
		change  func(*backendStep)
	}{
		{name: "fresh stale target changed", change: func(step *backendStep) {
			origin := step.body.(armcdn.AFDOrigin)
			origin.Properties.SharedPrivateLinkResource.PrivateLink.ID = ptr.To(*origin.Properties.SharedPrivateLinkResource.PrivateLink.ID + "-replacement")
		}},
		{name: "fresh stale weight changed", change: func(step *backendStep) {
			origin := step.body.(armcdn.AFDOrigin)
			origin.Properties.Weight = ptr.To(ptr.Deref(origin.Properties.Weight, 0) + 1)
		}},
		{name: "fresh stale ID contradicted", change: func(step *backendStep) {
			origin := step.body.(armcdn.AFDOrigin)
			origin.ID = ptr.To(changedValue)
			step.body = origin
		}},
		{name: "fresh response swaps valid child identity", change: func(step *backendStep) {
			origin := step.body.(armcdn.AFDOrigin)
			name := resourceName("fleet-origin", "swapped-cleanup-response")
			origin.ID = ptr.To((*origin.ID)[:strings.LastIndex(*origin.ID, "/")+1] + name)
			origin.Name = ptr.To(name)
			step.body = origin
		}},
		{name: "fresh stale deletion started", pending: true, change: func(step *backendStep) {
			step.body.(armcdn.AFDOrigin).Properties.ProvisioningState = ptr.To(armcdn.AfdProvisioningStateDeleting)
		}},
		{name: "fresh stale update started", pending: true, change: func(step *backendStep) {
			step.body.(armcdn.AFDOrigin).Properties.ProvisioningState = ptr.To(armcdn.AfdProvisioningStateUpdating)
		}},
		{name: "fresh stale already absent", pending: true, change: func(step *backendStep) { step.status = http.StatusNotFound }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, route := routeFixture(t)
			steps := staleDeletionSteps(f, route, retiredOrigin(t, f, tt.name))
			at := len(completeStagingSteps(f, route))
			steps[at].body = inventoryCopy(t, steps[at].body.(armcdn.AFDOrigin))
			tt.change(&steps[at])
			steps = steps[:at+1]
			p, calls := disabledGraphClient(t, f, steps, nil)
			got, err := p.StageDisabledGraph(context.Background(), f.snapshot.owner, f.snapshot.model)
			if tt.pending {
				if err != nil || got.InventoryVerified || got.StaleOriginsPending != 1 {
					t.Fatalf("fresh ongoing/absent cleanup was not pending: %#v, %v", got, err)
				}
			} else if err == nil || !reflect.DeepEqual(got, DisabledGraphStagingResult{}) {
				t.Fatalf("changed candidate was deleted: %#v, %v", got, err)
			}
			if *calls != len(steps) {
				t.Fatalf("candidate recheck did not stop cleanup: calls=%d, want=%d", *calls, len(steps))
			}
		})
	}
}

func TestStageDisabledGraphStaleCleanupRequiresCompleteExpectedOrigins(t *testing.T) {
	for _, scenario := range []string{"expected member absent", "expected approval regressed", "duplicate retired origin"} {
		t.Run(scenario, func(t *testing.T) {
			f, route := routeFixture(t)
			stale := retiredOrigin(t, f, scenario)
			steps := staleInventorySteps(f, route, stale)
			at := len(supportedGraphStagingSteps(f, route)) - 1
			page := steps[at].body.(map[string]interface{})
			items := page[inventoryValueField].([]interface{})
			switch scenario {
			case "expected member absent":
				items = items[1:]
			case "expected approval regressed":
				origin := inventoryCopy(t, items[0].(armcdn.AFDOrigin))
				origin.Properties.SharedPrivateLinkResource.Status = ptr.To(armcdn.SharedPrivateLinkResourceStatusPending)
				items[0] = origin
			case "duplicate retired origin":
				duplicate := inventoryCopy(t, stale)
				duplicate.ID, duplicate.Name = ptr.To(strings.ToUpper(*duplicate.ID)), ptr.To(strings.ToUpper(*duplicate.Name))
				items = append(items, duplicate)
			}
			page[inventoryValueField] = items
			steps = steps[:at+1]
			p, calls := disabledGraphClient(t, f, steps, nil)
			got, err := p.StageDisabledGraph(context.Background(), f.snapshot.owner, f.snapshot.model)
			if err == nil || !reflect.DeepEqual(got, DisabledGraphStagingResult{}) || *calls != len(steps) {
				t.Fatalf("incomplete or contradictory graph allowed cleanup: %#v, %v", got, err)
			}
		})
	}
}

func TestStageDisabledGraphStaleCleanupRequiresExactOtherChildren(t *testing.T) {
	for collection := 0; collection < 3; collection++ {
		f, route := routeFixture(t)
		steps := staleInventorySteps(f, route, retiredOrigin(t, f, "other-child"))
		at := len(supportedGraphStagingSteps(f, route)) - 4 + collection
		page := steps[at].body.(map[string]interface{})
		page[inventoryValueField] = append(page[inventoryValueField].([]interface{}),
			map[string]interface{}{"id": f.graph.Roots.ProfileID + steps[at].suffix + "/unexpected-cleanup-child"})
		steps = steps[:at+1]
		p, calls := disabledGraphClient(t, f, steps, nil)
		got, err := p.StageDisabledGraph(context.Background(), f.snapshot.owner, f.snapshot.model)
		if err == nil || !reflect.DeepEqual(got, DisabledGraphStagingResult{}) || *calls != len(steps) {
			t.Fatalf("unexpected non-origin child allowed cleanup: %#v, %v", got, err)
		}
	}
}

func TestStageDisabledGraphStaleCleanupErrorsAndCancellation(t *testing.T) {
	f, route := routeFixture(t)
	all := staleDeletionSteps(f, route, retiredOrigin(t, f, "operation-failures"))
	start := len(completeStagingSteps(f, route))
	for _, status := range []int{http.StatusForbidden, http.StatusConflict, http.StatusTooManyRequests} {
		for at := start; at < len(all); at++ {
			steps := append([]backendStep(nil), all[:at+1]...)
			steps[at].status = status
			p, calls := disabledGraphClient(t, f, steps, nil)
			got, err := p.StageDisabledGraph(context.Background(), f.snapshot.owner, f.snapshot.model)
			var responseError *azcore.ResponseError
			if !errors.As(err, &responseError) || responseError.StatusCode != status ||
				!reflect.DeepEqual(got, DisabledGraphStagingResult{}) || *calls != len(steps) {
				t.Fatalf("stale cleanup error was lost: %#v, %v, calls=%d", got, err, *calls)
			}
		}
	}
	for at := start; at <= len(all); at++ {
		ctx, cancel := context.WithCancel(context.Background())
		p, calls := disabledGraphClient(t, f, all[:at], func(n int) {
			if n == at {
				cancel()
			}
		})
		got, err := p.StageDisabledGraph(ctx, f.snapshot.owner, f.snapshot.model)
		cancel()
		if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(got, DisabledGraphStagingResult{}) || *calls != at {
			t.Fatalf("stale cleanup ignored cancellation: %#v, %v, calls=%d", got, err, *calls)
		}
	}
}
