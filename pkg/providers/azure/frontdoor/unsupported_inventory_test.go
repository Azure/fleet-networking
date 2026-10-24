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
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
)

func unsupportedInventorySteps() []backendStep {
	steps := make([]backendStep, 0, 4)
	for _, suffix := range []string{"/customDomains", "/ruleSets", "/securityPolicies", "/secrets"} {
		steps = append(steps, backendStep{method: http.MethodGet, suffix: suffix, status: http.StatusOK,
			body: map[string]interface{}{inventoryValueField: []interface{}{}}})
	}
	return steps
}

func TestStageDisabledGraphRejectsUnsupportedInventory(t *testing.T) {
	for collection := range unsupportedInventorySteps() {
		for _, scenario := range []string{"nonempty", "nil record", "omitted resource array", "null resource array"} {
			t.Run(fmt.Sprintf("%d/%s", collection, scenario), func(t *testing.T) {
				f, route := routeFixture(t)
				steps := completeStagingSteps(f, route)
				at := len(steps) - len(unsupportedInventorySteps()) + collection
				page := map[string]interface{}{}
				switch scenario {
				case "nonempty":
					page[inventoryValueField] = []interface{}{map[string]interface{}{
						"id": f.graph.Roots.ProfileID + steps[at].suffix + "/unsupported-resource",
					}}
				case "nil record":
					page[inventoryValueField] = []interface{}{nil}
				case "null resource array":
					page[inventoryValueField] = nil
				}
				steps[at].body = page
				steps = steps[:at+1]
				p, calls := disabledGraphClient(t, f, steps, nil)
				got, err := p.StageDisabledGraph(context.Background(), f.snapshot.owner, f.snapshot.model)
				if err == nil || !reflect.DeepEqual(got, DisabledGraphStagingResult{}) || *calls != len(steps) {
					t.Fatalf("unsupported or ambiguous inventory was accepted: %#v, %v, calls=%d", got, err, *calls)
				}
			})
		}
	}
}

func TestStageDisabledGraphUnsupportedInventoryPagination(t *testing.T) {
	tests := []struct {
		name      string
		extra     bool
		ambiguous bool
		status    int
	}{
		{name: "explicit empty pages", status: http.StatusOK},
		{name: "unsupported later resource", extra: true, status: http.StatusOK},
		{name: "ambiguous later page", ambiguous: true, status: http.StatusOK},
		{name: "later page forbidden", status: http.StatusForbidden},
		{name: "later page missing", status: http.StatusNotFound},
	}
	for collection := range unsupportedInventorySteps() {
		for _, tt := range tests {
			t.Run(fmt.Sprintf("%d/%s", collection, tt.name), func(t *testing.T) {
				f, route := routeFixture(t)
				steps := completeStagingSteps(f, route)
				at := len(steps) - len(unsupportedInventorySteps()) + collection
				first, second := steps[at], steps[at]
				second.suffix += "/unsupported-page-2"
				first.body = map[string]interface{}{inventoryValueField: []interface{}{},
					inventoryNextLinkField: "https://management.azure.com" + f.graph.Roots.ProfileID + second.suffix + "?api-version=" + testAPIVersion}
				second.status = tt.status
				if tt.extra {
					second.body = map[string]interface{}{inventoryValueField: []interface{}{nil}}
				} else if tt.ambiguous {
					second.body = map[string]interface{}{}
				}
				paged := append([]backendStep(nil), steps[:at]...)
				paged = append(paged, first, second)
				fails := tt.extra || tt.ambiguous || tt.status != http.StatusOK
				if !fails {
					paged = append(paged, steps[at+1:]...)
				}
				p, calls := disabledGraphClient(t, f, paged, nil)
				got, err := p.StageDisabledGraph(context.Background(), f.snapshot.owner, f.snapshot.model)
				if fails {
					if err == nil || !reflect.DeepEqual(got, DisabledGraphStagingResult{}) {
						t.Fatalf("later unsupported inventory failure was ignored: %#v, %v", got, err)
					}
				} else if err != nil || !got.InventoryVerified || got.Route.State != DisabledRouteObserved {
					t.Fatalf("explicit empty inventory did not converge: %#v, %v", got, err)
				}
				if tt.status != http.StatusOK {
					var responseError *azcore.ResponseError
					if !errors.As(err, &responseError) || responseError.StatusCode != tt.status {
						t.Fatalf("later page SDK error was lost: %v", err)
					}
				}
				if *calls != len(paged) {
					t.Fatalf("unsupported inventory pages were skipped: calls=%d, want=%d", *calls, len(paged))
				}
			})
		}
	}
}
