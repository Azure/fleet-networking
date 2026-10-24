/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package frontdoor

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	"k8s.io/utils/ptr"
)

// Detach later collection records from the preceding GET response fixtures.
func inventoryCopy[T any](t *testing.T, value T) T {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var copied T
	if err := json.Unmarshal(data, &copied); err != nil {
		t.Fatal(err)
	}
	return copied
}

func assertListedGraphChange(t *testing.T, f backendProvisioningFixture, route armcdn.Route, collection int,
	record interface{}, state PrivateOriginState) {
	t.Helper()
	steps := completeStagingSteps(f, route)
	at := len(supportedGraphStagingSteps(f, route)) - 4 + collection
	page := steps[at].body.(map[string]interface{})
	page[inventoryValueField].([]interface{})[0] = record
	p, calls := disabledGraphClient(t, f, steps, nil)
	got, err := p.StageDisabledGraph(context.Background(), f.snapshot.owner, f.snapshot.model)
	if err == nil || !reflect.DeepEqual(got, DisabledGraphStagingResult{}) || *calls != at+1 {
		t.Fatalf("later conflicting observation retained convergence: %#v, %v, calls=%d, want=%d", got, err, *calls, at+1)
	}
	if state != "" && !strings.Contains(err.Error(), string(state)) {
		t.Fatalf("current private-origin state was lost: %v, want %s", err, state)
	}
	if state == PrivateOriginApprovalPending &&
		(!strings.Contains(err.Error(), "approval is asynchronous") || !strings.Contains(err.Error(), "PLS owner")) {
		t.Fatalf("approval regression lost manual action guidance: %v", err)
	}
}

func TestStageDisabledGraphRechecksListedEndpoint(t *testing.T) {
	tests := []struct {
		name   string
		change func(*armcdn.AFDEndpointProperties)
	}{
		{name: "listed endpoint enabled", change: func(p *armcdn.AFDEndpointProperties) { p.EnabledState = ptr.To(armcdn.EnabledStateEnabled) }},
		{name: "listed endpoint still updating", change: func(p *armcdn.AFDEndpointProperties) {
			p.ProvisioningState = ptr.To(armcdn.AfdProvisioningStateUpdating)
		}},
		{name: "listed endpoint still deploying", change: func(p *armcdn.AFDEndpointProperties) {
			p.DeploymentStatus = ptr.To(armcdn.DeploymentStatusInProgress)
		}},
		{name: "listed endpoint enabled state missing", change: func(p *armcdn.AFDEndpointProperties) { p.EnabledState = nil }},
		{name: "listed endpoint provisioning missing", change: func(p *armcdn.AFDEndpointProperties) { p.ProvisioningState = nil }},
		{name: "listed endpoint deployment missing", change: func(p *armcdn.AFDEndpointProperties) { p.DeploymentStatus = nil }},
		{name: "listed endpoint deployment failed", change: func(p *armcdn.AFDEndpointProperties) { p.DeploymentStatus = ptr.To(armcdn.DeploymentStatusFailed) }},
		{name: "listed endpoint hostname missing", change: func(p *armcdn.AFDEndpointProperties) { p.HostName = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, route := routeFixture(t)
			listed := inventoryCopy(t, f.endpoint)
			tt.change(listed.Properties)
			assertListedGraphChange(t, f, route, 0, listed, "")
		})
	}
}

func TestStageDisabledGraphRechecksListedRoute(t *testing.T) {
	tests := []struct {
		name   string
		change func(*armcdn.RouteProperties)
	}{
		{name: "listed route enabled", change: func(p *armcdn.RouteProperties) { p.EnabledState = ptr.To(armcdn.EnabledStateEnabled) }},
		{name: "listed route still updating", change: func(p *armcdn.RouteProperties) {
			p.ProvisioningState = ptr.To(armcdn.AfdProvisioningStateUpdating)
		}},
		{name: "listed route still deploying", change: func(p *armcdn.RouteProperties) { p.DeploymentStatus = ptr.To(armcdn.DeploymentStatusInProgress) }},
		{name: "listed route paths changed", change: func(p *armcdn.RouteProperties) { p.PatternsToMatch = []*string{ptr.To("/changed/*")} }},
		{name: "listed route backend changed", change: func(p *armcdn.RouteProperties) { p.OriginGroup.ID = ptr.To(changedValue) }},
		{name: "listed route caching changed", change: func(p *armcdn.RouteProperties) { p.CacheConfiguration = &armcdn.AfdRouteCacheConfiguration{} }},
		{name: "listed route enabled state missing", change: func(p *armcdn.RouteProperties) { p.EnabledState = nil }},
		{name: "listed route provisioning missing", change: func(p *armcdn.RouteProperties) { p.ProvisioningState = nil }},
		{name: "listed route deployment missing", change: func(p *armcdn.RouteProperties) { p.DeploymentStatus = nil }},
		{name: "listed route deployment failed", change: func(p *armcdn.RouteProperties) { p.DeploymentStatus = ptr.To(armcdn.DeploymentStatusFailed) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, route := routeFixture(t)
			listed := inventoryCopy(t, route)
			tt.change(listed.Properties)
			assertListedGraphChange(t, f, route, 1, listed, "")
		})
	}
}

func TestStageDisabledGraphRechecksListedGroup(t *testing.T) {
	tests := []struct {
		name   string
		change func(*armcdn.AFDOriginGroupProperties)
	}{
		{name: "listed group probe changed", change: func(p *armcdn.AFDOriginGroupProperties) { p.HealthProbeSettings.ProbePath = ptr.To("/changed") }},
		{name: "listed group session policy changed", change: func(p *armcdn.AFDOriginGroupProperties) {
			p.SessionAffinityState = ptr.To(armcdn.EnabledStateEnabled)
		}},
		{name: "listed group still updating", change: func(p *armcdn.AFDOriginGroupProperties) {
			p.ProvisioningState = ptr.To(armcdn.AfdProvisioningStateUpdating)
		}},
		{name: "listed group still deploying", change: func(p *armcdn.AFDOriginGroupProperties) {
			p.DeploymentStatus = ptr.To(armcdn.DeploymentStatusInProgress)
		}},
		{name: "listed group provisioning missing", change: func(p *armcdn.AFDOriginGroupProperties) { p.ProvisioningState = nil }},
		{name: "listed group deployment missing", change: func(p *armcdn.AFDOriginGroupProperties) { p.DeploymentStatus = nil }},
		{name: "listed group provisioning failed", change: func(p *armcdn.AFDOriginGroupProperties) {
			p.ProvisioningState = ptr.To(armcdn.AfdProvisioningStateFailed)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, route := routeFixture(t)
			listed := inventoryCopy(t, f.group)
			tt.change(listed.Properties)
			assertListedGraphChange(t, f, route, 2, listed, "")
		})
	}
}

func TestStageDisabledGraphRechecksListedOrigin(t *testing.T) {
	tests := []struct {
		name   string
		state  PrivateOriginState
		change func(*armcdn.AFDOriginProperties)
	}{
		{name: "listed origin approval regressed", state: PrivateOriginApprovalPending, change: func(p *armcdn.AFDOriginProperties) {
			p.SharedPrivateLinkResource.Status = ptr.To(armcdn.SharedPrivateLinkResourceStatusPending)
		}},
		{name: "listed origin approval rejected", state: PrivateOriginRejected, change: func(p *armcdn.AFDOriginProperties) {
			p.SharedPrivateLinkResource.Status = ptr.To(armcdn.SharedPrivateLinkResourceStatusRejected)
		}},
		{name: "listed origin disconnected", state: PrivateOriginDisconnected, change: func(p *armcdn.AFDOriginProperties) {
			p.SharedPrivateLinkResource.Status = ptr.To(armcdn.SharedPrivateLinkResourceStatusDisconnected)
		}},
		{name: "listed origin approval timed out", state: PrivateOriginTimedOut, change: func(p *armcdn.AFDOriginProperties) {
			p.SharedPrivateLinkResource.Status = ptr.To(armcdn.SharedPrivateLinkResourceStatusTimeout)
		}},
		{name: "listed origin public target", state: PrivateOriginDrifted, change: func(p *armcdn.AFDOriginProperties) { p.SharedPrivateLinkResource = nil }},
		{name: "listed origin PLS replaced", state: PrivateOriginDrifted, change: func(p *armcdn.AFDOriginProperties) {
			p.SharedPrivateLinkResource.PrivateLink.ID = ptr.To(changedValue)
		}},
		{name: "listed origin weight changed", state: PrivateOriginDrifted, change: func(p *armcdn.AFDOriginProperties) { p.Weight = ptr.To(ptr.Deref(p.Weight, 0) + 1) }},
		{name: "listed origin Host changed", state: PrivateOriginDrifted, change: func(p *armcdn.AFDOriginProperties) { p.OriginHostHeader = ptr.To("unexpected.example") }},
		{name: "listed origin port changed", state: PrivateOriginDrifted, change: func(p *armcdn.AFDOriginProperties) { p.HTTPPort = ptr.To(ptr.Deref(p.HTTPPort, 0) + 1) }},
		{name: "listed origin still updating", state: PrivateOriginProvisioning, change: func(p *armcdn.AFDOriginProperties) { p.ProvisioningState = ptr.To(armcdn.AfdProvisioningStateUpdating) }},
		{name: "listed origin still deploying", state: PrivateOriginProvisioning, change: func(p *armcdn.AFDOriginProperties) { p.DeploymentStatus = ptr.To(armcdn.DeploymentStatusInProgress) }},
		{name: "listed origin approval unknown", state: PrivateOriginUnknown, change: func(p *armcdn.AFDOriginProperties) { p.SharedPrivateLinkResource.Status = nil }},
		{name: "listed origin provisioning unknown", state: PrivateOriginUnknown, change: func(p *armcdn.AFDOriginProperties) { p.ProvisioningState = nil }},
		{name: "listed origin deployment failed", state: PrivateOriginFailed, change: func(p *armcdn.AFDOriginProperties) { p.DeploymentStatus = ptr.To(armcdn.DeploymentStatusFailed) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, route := routeFixture(t)
			listed := inventoryCopy(t, f.originRead(0).body.(armcdn.AFDOrigin))
			tt.change(listed.Properties)
			assertListedGraphChange(t, f, route, 3, listed, tt.state)
		})
	}
}

func TestStageDisabledGraphRechecksApprovalOnLaterOriginPage(t *testing.T) {
	f, route := routeFixture(t)
	steps := completeStagingSteps(f, route)
	at := len(supportedGraphStagingSteps(f, route)) - 1
	first, second := steps[at], steps[at]
	items := first.body.(map[string]interface{})[inventoryValueField].([]interface{})
	later := inventoryCopy(t, items[1].(armcdn.AFDOrigin))
	later.Properties.SharedPrivateLinkResource.Status = ptr.To(armcdn.SharedPrivateLinkResourceStatusPending)
	second.suffix += "/approval-regressed-page"
	second.body = map[string]interface{}{inventoryValueField: []interface{}{later}}
	first.body = map[string]interface{}{
		inventoryValueField:    items[:1],
		inventoryNextLinkField: "https://management.azure.com" + f.graph.Roots.ProfileID + second.suffix + "?api-version=" + testAPIVersion,
	}
	paged := append([]backendStep(nil), steps[:at]...)
	paged = append(paged, first, second)
	paged = append(paged, steps[at+1:]...)
	p, calls := disabledGraphClient(t, f, paged, nil)
	got, err := p.StageDisabledGraph(context.Background(), f.snapshot.owner, f.snapshot.model)
	if err == nil || !reflect.DeepEqual(got, DisabledGraphStagingResult{}) || *calls != at+2 {
		t.Fatalf("later origin page retained stale approval: %#v, %v, calls=%d", got, err, *calls)
	}
	if !strings.Contains(err.Error(), string(PrivateOriginApprovalPending)) {
		t.Fatalf("later-page approval state was lost: %v", err)
	}
}

func TestStageDisabledGraphPreservesEquivalentListedProperties(t *testing.T) {
	f, route := routeFixture(t)
	steps := completeStagingSteps(f, route)
	at := len(supportedGraphStagingSteps(f, route)) - 4
	endpoint := inventoryCopy(t, f.endpoint)
	endpoint.ID, endpoint.Name = ptr.To(strings.ToUpper(*endpoint.ID)), ptr.To(strings.ToUpper(*endpoint.Name))
	steps[at].body = map[string]interface{}{inventoryValueField: []interface{}{endpoint}}
	listedRoute := inventoryCopy(t, route)
	listedRoute.ID = ptr.To(strings.ToUpper(*listedRoute.ID))
	listedRoute.Properties.OriginGroup.ID = ptr.To(strings.ToUpper(*listedRoute.Properties.OriginGroup.ID))
	listedRoute.Properties.CustomDomains = []*armcdn.ActivatedResourceReference{}
	listedRoute.Properties.RuleSets = []*armcdn.ResourceReference{}
	steps[at+1].body = map[string]interface{}{inventoryValueField: []interface{}{listedRoute}}
	group := inventoryCopy(t, f.group)
	group.ID, group.Name = ptr.To(strings.ToUpper(*group.ID)), ptr.To(strings.ToUpper(*group.Name))
	group.Properties.LoadBalancingSettings = &armcdn.LoadBalancingSettingsParameters{
		SampleSize: ptr.To(int32(4)), SuccessfulSamplesRequired: ptr.To(int32(3)),
	}
	steps[at+2].body = map[string]interface{}{inventoryValueField: []interface{}{group}}
	page := steps[at+3].body.(map[string]interface{})
	items := page[inventoryValueField].([]interface{})
	for i := range items {
		origin := inventoryCopy(t, items[i].(armcdn.AFDOrigin))
		origin.ID, origin.Name = ptr.To(strings.ToUpper(*origin.ID)), ptr.To(strings.ToUpper(*origin.Name))
		link := origin.Properties.SharedPrivateLinkResource
		link.PrivateLink.ID = ptr.To(strings.ToUpper(*link.PrivateLink.ID))
		link.PrivateLinkLocation = ptr.To(strings.ToUpper(*link.PrivateLinkLocation))
		items[i] = origin
	}
	p, calls := disabledGraphClient(t, f, steps, nil)
	got, err := p.StageDisabledGraph(context.Background(), f.snapshot.owner, f.snapshot.model)
	if err != nil || !got.InventoryVerified || got.Route.State != DisabledRouteObserved || *calls != len(steps) {
		t.Fatalf("equivalent listed properties were treated as drift: %#v, %v, calls=%d", got, err, *calls)
	}
}
