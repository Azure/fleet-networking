/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package frontdoor

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	"k8s.io/utils/ptr"
)

func TestWithdrawHTTPPreviewRejectsUnsafeWrites(t *testing.T) {
	for _, failure := range []string{"unsupported", "route-read", "root-state", "canceled"} {
		t.Run(failure, func(t *testing.T) {
			f, route := routeFixture(t)
			inventory := httpPreviewInventory(f, route)
			steps := append(f.roots(), inventory...)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch failure {
			case "unsupported":
				for i := range inventory {
					if strings.Contains(inventory[i].suffix, "/customDomains") {
						inventory[i].body = map[string]interface{}{inventoryValueField: []interface{}{map[string]interface{}{"name": "foreign"}}}
						steps = append(f.roots(), inventory[:i+1]...)
						break
					}
				}
			case "route-read":
				read := routeRead(f, route)
				read.status, read.body = http.StatusForbidden, nil
				steps = append(steps, read)
			case "root-state":
				steps = append(steps, routeRead(f, route))
				roots := f.roots()
				endpoint := f.endpoint
				properties := *endpoint.Properties
				properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
				endpoint.Properties = &properties
				roots[len(roots)-1].body = endpoint
				steps = append(steps, roots...)
			case "canceled":
				cancel()
				steps = nil
			}
			p, calls := disabledGraphClient(t, f, steps, nil)
			if _, err := p.WithdrawHTTPPreview(ctx, f.snapshot.owner); err == nil {
				t.Fatal("unsafe withdrawal did not report an error")
			}
			if *calls != len(steps) {
				t.Fatalf("unexpected withdrawal request count: %d/%d", *calls, len(steps))
			}
		})
	}
}

func TestWithdrawHTTPPreviewRetainsRoots(t *testing.T) {
	for _, phase := range []string{"disable", "route", "origin", "empty"} {
		t.Run(phase, func(t *testing.T) {
			f, route := routeFixture(t)
			for i := range f.graph.Backends.OriginGroups[0].Origins {
				f.changeOrigin(i, func(origin *armcdn.AFDOrigin) {
					origin.Properties.SharedPrivateLinkResource.Status = nil
					origin.Properties.DeploymentStatus = ptr.To(armcdn.DeploymentStatusNotStarted)
				})
			}
			if phase == "disable" {
				f.endpoint.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
				steps := suspendEndpointSteps(f)
				p, calls := disabledGraphClient(t, f, steps, nil)
				got, err := p.WithdrawHTTPPreview(context.Background(), f.snapshot.owner)
				if err != nil || got.State != EndpointSuspensionPending || *calls != len(steps) {
					t.Fatalf("withdrawal did not disable first: %+v, %v", got, err)
				}
				return
			}
			route.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
			inventory := httpPreviewInventory(f, route)
			if phase != "route" {
				inventory[len(f.roots())+1].body = map[string]interface{}{inventoryValueField: []interface{}{}}
			}
			if phase == "empty" {
				inventory[len(f.roots())+3].body = map[string]interface{}{inventoryValueField: []interface{}{}}
			}
			steps := append(f.roots(), inventory...)
			if phase == "route" {
				steps = append(steps, routeRead(f, route))
				steps = append(steps, f.roots()...)
				steps = append(steps,
					backendStep{method: http.MethodDelete, suffix: routeSuffix(f), status: http.StatusNoContent})
			} else if phase == "origin" {
				first := f.originRead(0).body.(armcdn.AFDOrigin)
				second := f.originRead(1).body.(armcdn.AFDOrigin)
				selected := first
				if *second.ID < *first.ID {
					selected = second
				}
				suffix := "/originGroups/" + f.graph.Backends.OriginGroups[0].Name + "/origins/" + *selected.Name
				steps = append(steps, backendStep{method: http.MethodGet, suffix: suffix, status: http.StatusOK, body: selected})
				steps = append(steps, f.roots()...)
				steps = append(steps, backendStep{method: http.MethodDelete, suffix: suffix, status: http.StatusNoContent})
			}
			p, calls := disabledGraphClient(t, f, steps, nil)
			got, err := p.WithdrawHTTPPreview(context.Background(), f.snapshot.owner)
			want := EndpointSuspensionPending
			if phase == "empty" {
				want = EndpointSuspended
			}
			if err != nil || got.State != want || *calls != len(steps) {
				t.Fatalf("bounded withdrawal did not retain roots: %+v, %v, calls=%d/%d", got, err, *calls, len(steps))
			}
		})
	}
}
