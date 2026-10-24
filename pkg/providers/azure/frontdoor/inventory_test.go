/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package frontdoor

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	"k8s.io/utils/ptr"
)

const (
	inventoryValueField      = "value"
	inventoryPropertiesField = "properties"
	inventoryNameField       = "name"
	inventoryNextLinkField   = "nextLink"
)

func supportedInventorySteps(f backendProvisioningFixture, route armcdn.Route) []backendStep {
	origins := make([]interface{}, 0, len(f.graph.Backends.OriginGroups[0].Origins))
	for i := range f.graph.Backends.OriginGroups[0].Origins {
		origins = append(origins, f.originRead(i).body)
	}
	collections := []backendStep{
		{suffix: "/afdEndpoints", body: []interface{}{f.endpoint}},
		{suffix: "/afdEndpoints/" + f.graph.Roots.EndpointName + "/routes", body: []interface{}{route}},
		{suffix: "/originGroups", body: []interface{}{f.group}},
		{suffix: "/originGroups/" + f.graph.Backends.OriginGroups[0].Name + "/origins", body: origins},
	}
	for i := range collections {
		collections[i].method, collections[i].status = http.MethodGet, http.StatusOK
		collections[i].body = map[string]interface{}{inventoryValueField: collections[i].body}
	}
	return append(f.roots(), collections...)
}

func supportedGraphStagingSteps(f backendProvisioningFixture, route armcdn.Route) []backendStep {
	steps := append(append(f.roots(), f.reads()...), routeRead(f, route))
	return append(steps, supportedInventorySteps(f, route)...)
}

func completeStagingSteps(f backendProvisioningFixture, route armcdn.Route) []backendStep {
	return append(supportedGraphStagingSteps(f, route), unsupportedInventorySteps()...)
}

func TestStageDisabledGraphRefusesNonexactInventory(t *testing.T) {
	for collection := 0; collection < 4; collection++ {
		for _, scenario := range []string{"unexpected child", "missing", "duplicate", "nil item", "contradictory name", "absent inventory identity"} {
			t.Run(fmt.Sprintf("%d/%s", collection, scenario), func(t *testing.T) {
				f, route := routeFixture(t)
				steps := supportedGraphStagingSteps(f, route)
				at := len(steps) - 4 + collection
				page := steps[at].body.(map[string]interface{})
				items := page[inventoryValueField].([]interface{})
				id := f.graph.Roots.ProfileID + steps[at].suffix + "/unexpected-inventory-resource"
				switch scenario {
				case "unexpected child":
					items = append(items, map[string]interface{}{"id": id})
				case "missing":
					items = items[1:]
				case "duplicate":
					items = append(items, items[0])
				case "nil item":
					items = []interface{}{nil}
				case "absent inventory identity":
					items = []interface{}{map[string]interface{}{inventoryPropertiesField: map[string]interface{}{}}}
				case "contradictory name":
					identities := []string{f.graph.Roots.ProfileID + "/afdEndpoints/" + f.graph.Roots.EndpointName,
						*route.ID, *f.group.ID, *f.originRead(0).body.(armcdn.AFDOrigin).ID}
					items = []interface{}{map[string]interface{}{"id": identities[collection], inventoryNameField: changedValue, inventoryPropertiesField: map[string]interface{}{}}}
				}
				page[inventoryValueField] = items
				steps = steps[:at+1]
				p, calls := disabledGraphClient(t, f, steps, nil)
				got, err := p.StageDisabledGraph(context.Background(), f.snapshot.owner, f.snapshot.model)
				if err == nil || !reflect.DeepEqual(got, DisabledGraphStagingResult{}) || *calls != len(steps) {
					t.Fatalf("nonexact inventory was accepted: %#v, %v, calls=%d", got, err, *calls)
				}
			})
		}
	}
}

func TestStageDisabledGraphInventoryPagination(t *testing.T) {
	for collection := 0; collection < 4; collection++ {
		for _, extraOnLaterPage := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/extra=%t", collection, extraOnLaterPage), func(t *testing.T) {
				f, route := routeFixture(t)
				steps := supportedGraphStagingSteps(f, route)
				at := len(steps) - 4 + collection
				first, second := steps[at], steps[at]
				second.suffix += "/inventory-page-2"
				firstItems := []interface{}{}
				if extraOnLaterPage {
					firstItems = first.body.(map[string]interface{})[inventoryValueField].([]interface{})
					second.body = map[string]interface{}{inventoryValueField: []interface{}{map[string]interface{}{
						"id": f.graph.Roots.ProfileID + steps[at].suffix + "/unexpected-later-resource",
					}}}
				}
				first.body = map[string]interface{}{inventoryValueField: firstItems,
					inventoryNextLinkField: "https://management.azure.com" + f.graph.Roots.ProfileID + second.suffix + "?api-version=" + testAPIVersion}
				paged := append([]backendStep(nil), steps[:at]...)
				paged = append(paged, first, second)
				if !extraOnLaterPage {
					paged = append(paged, steps[at+1:]...)
					paged = append(paged, unsupportedInventorySteps()...)
				}
				p, calls := disabledGraphClient(t, f, paged, nil)
				got, err := p.StageDisabledGraph(context.Background(), f.snapshot.owner, f.snapshot.model)
				if extraOnLaterPage {
					if err == nil || !reflect.DeepEqual(got, DisabledGraphStagingResult{}) {
						t.Fatalf("later-page stale resource was ignored: %#v, %v", got, err)
					}
				} else if err != nil || got.Route.State != DisabledRouteObserved || !got.InventoryVerified {
					t.Fatalf("empty first inventory page was treated as absence: %#v, %v", got, err)
				}
				if *calls != len(paged) {
					t.Fatalf("inventory did not exhaust the required pages: calls=%d, want=%d", *calls, len(paged))
				}
			})
		}
	}
}

func TestStageDisabledGraphInventoryRequiresExplicitPages(t *testing.T) {
	tests := []struct {
		name     string
		page     map[string]interface{}
		explicit bool
	}{
		{name: "omitted supported resource array", page: map[string]interface{}{}},
		{name: "null supported resource array", page: map[string]interface{}{inventoryValueField: nil}},
		{name: "explicit empty supported resource array", page: map[string]interface{}{inventoryValueField: []interface{}{}}, explicit: true},
	}
	for collection := 0; collection < 4; collection++ {
		for _, tt := range tests {
			for _, firstPage := range []bool{true, false} {
				t.Run(fmt.Sprintf("%d/%s/first=%t", collection, tt.name, firstPage), func(t *testing.T) {
					f, route := routeFixture(t)
					steps := completeStagingSteps(f, route)
					at := len(supportedGraphStagingSteps(f, route)) - 4 + collection
					first, second := steps[at], steps[at]
					second.suffix += "/explicit-inventory-page-2"
					if firstPage {
						first.body = maps.Clone(tt.page)
					} else {
						second.body = maps.Clone(tt.page)
					}
					first.body.(map[string]interface{})[inventoryNextLinkField] =
						"https://management.azure.com" + f.graph.Roots.ProfileID + second.suffix + "?api-version=" + testAPIVersion
					paged := append([]backendStep(nil), steps[:at]...)
					paged = append(paged, first, second)
					paged = append(paged, steps[at+1:]...)
					p, calls := disabledGraphClient(t, f, paged, nil)
					got, err := p.StageDisabledGraph(context.Background(), f.snapshot.owner, f.snapshot.model)
					wantCalls := len(paged)
					if tt.explicit {
						if err != nil || !got.InventoryVerified || got.Route.State != DisabledRouteObserved {
							t.Fatalf("explicit empty inventory page was refused: %#v, %v", got, err)
						}
					} else {
						wantCalls = at + 2
						if firstPage {
							wantCalls--
						}
						if err == nil || !reflect.DeepEqual(got, DisabledGraphStagingResult{}) {
							t.Fatalf("ambiguous supported page claimed completeness: %#v, %v", got, err)
						}
						if !strings.Contains(err.Error(), "explicit resource array") {
							t.Fatalf("missing inventory evidence was not reported: %v", err)
						}
					}
					if *calls != wantCalls {
						t.Fatalf("incorrect request count after inventory page: calls=%d, want=%d", *calls, wantCalls)
					}
				})
			}
		}
	}
}

func TestStageDisabledGraphInventoryRequiresFreshDisabledOwnership(t *testing.T) {
	for _, scenario := range []string{"lost profile ownership", "endpoint enabled again"} {
		t.Run(scenario, func(t *testing.T) {
			f, route := routeFixture(t)
			steps := supportedGraphStagingSteps(f, route)
			at := len(steps) - len(supportedInventorySteps(f, route))
			switch scenario {
			case "lost profile ownership":
				profile := f.snapshot.profile
				profile.Tags = nil
				steps[at].body = profile
			case "endpoint enabled again":
				at++
				endpoint := f.endpoint
				properties := *endpoint.Properties
				properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
				endpoint.Properties = &properties
				steps[at].body = endpoint
			}
			steps = steps[:at+1]
			p, calls := disabledGraphClient(t, f, steps, nil)
			got, err := p.StageDisabledGraph(context.Background(), f.snapshot.owner, f.snapshot.model)
			if err == nil || !reflect.DeepEqual(got, DisabledGraphStagingResult{}) || *calls != len(steps) {
				t.Fatalf("inventory read without current disabled ownership: %#v, %v, calls=%d", got, err, *calls)
			}
		})
	}
}

func TestStageDisabledGraphInventory404IsNotEmptySuccess(t *testing.T) {
	for collection := 0; collection < 4; collection++ {
		f, route := routeFixture(t)
		steps := supportedGraphStagingSteps(f, route)
		at := len(steps) - 4 + collection
		steps[at].status = http.StatusNotFound
		steps = steps[:at+1]
		p, calls := disabledGraphClient(t, f, steps, nil)
		got, err := p.StageDisabledGraph(context.Background(), f.snapshot.owner, f.snapshot.model)
		var responseError *azcore.ResponseError
		if !errors.As(err, &responseError) || responseError.StatusCode != http.StatusNotFound ||
			!reflect.DeepEqual(got, DisabledGraphStagingResult{}) || *calls != len(steps) {
			t.Fatalf("inventory 404 was interpreted as complete absence: %#v, %v, calls=%d", got, err, *calls)
		}
	}
}

func TestResourceInventoryARMIdentity(t *testing.T) {
	const id = "/subscriptions/sub/resourceGroups/group/providers/Microsoft.Cdn/profiles/profile/afdEndpoints/endpoint"
	tests := []struct {
		name string
		ids  []*string
		fail bool
	}{
		{name: "case insensitive", ids: []*string{ptr.To(strings.ToUpper(id))}},
		{name: "same name under another parent", ids: []*string{ptr.To(strings.Replace(id, "/profile/", "/other/", 1))}, fail: true},
		{name: "duplicate across pages", ids: []*string{ptr.To(id), ptr.To(strings.ToUpper(id))}, fail: true},
		{name: "nil identity", ids: []*string{nil}, fail: true},
		{name: "empty identity", ids: []*string{ptr.To("")}, fail: true},
		{name: "empty collection", fail: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inventory := newResourceInventory(id)
			var err error
			for _, actual := range tt.ids {
				if err = inventory.observe(actual); err != nil {
					break
				}
			}
			if err == nil {
				err = inventory.complete()
			}
			if (err != nil) != tt.fail {
				t.Fatalf("incorrect ARM membership decision: %v", err)
			}
		})
	}
}
