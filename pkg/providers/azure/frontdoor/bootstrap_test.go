/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package frontdoor

import (
	"context"
	"maps"
	"net/http"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	"k8s.io/utils/ptr"
)

func bootstrapFixture(t *testing.T) (backendProvisioningFixture, armcdn.Route) {
	t.Helper()
	f, route := routeFixture(t)
	f.endpoint.Tags["networking.fleet.azure.com.activation"] = ptr.To("pending")
	f.endpoint.Properties.DeploymentStatus = ptr.To(armcdn.DeploymentStatusNotStarted)
	f.group.Properties.DeploymentStatus = ptr.To(armcdn.DeploymentStatusNotStarted)
	route.Properties.DeploymentStatus = ptr.To(armcdn.DeploymentStatusNotStarted)
	for i := range f.graph.Backends.OriginGroups[0].Origins {
		f.changeOrigin(i, func(origin *armcdn.AFDOrigin) {
			origin.Properties.DeploymentStatus = ptr.To(armcdn.DeploymentStatusNotStarted)
		})
	}
	return f, route
}

func missingBootstrapRoute(f backendProvisioningFixture, route armcdn.Route) backendStep {
	read := routeRead(f, route)
	read.status = http.StatusNotFound
	return read
}

func TestReconcileHTTPPreviewBootstrap(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*backendProvisioningFixture, *armcdn.Route) []backendStep
		state HTTPPreviewProvisioningState
	}{
		{"provisioned empty endpoint can create its first group", func(f *backendProvisioningFixture, route *armcdn.Route) []backendStep {
			group := f.graph.Backends.OriginGroups[0]
			missing := f.groupRead()
			missing.status = http.StatusNotFound
			steps := append(f.roots(), missingBootstrapRoute(*f, *route))
			steps = append(steps, f.roots()...)
			steps = append(steps, f.roots()...)
			return append(steps, missing, backendStep{
				method: http.MethodPut, suffix: missing.suffix, status: http.StatusAccepted,
				body: f.group, want: group.Parameters,
			})
		}, HTTPPreviewStaging},
		{"approved provisioned backends can acquire a route before deployment", func(f *backendProvisioningFixture, route *armcdn.Route) []backendStep {
			steps := append(f.roots(), missingBootstrapRoute(*f, *route))
			steps = append(steps, f.roots()...)
			steps = append(steps, f.reads()...)
			return append(steps, missingBootstrapRoute(*f, *route), backendStep{
				method: http.MethodPut, suffix: routeSuffix(*f), status: http.StatusAccepted,
				body: *route, want: f.graph.Route,
			})
		}, HTTPPreviewStaging},
		{"pending approval does not starve the next member origin", func(f *backendProvisioningFixture, route *armcdn.Route) []backendStep {
			f.changeOrigin(0, func(origin *armcdn.AFDOrigin) {
				origin.Properties.SharedPrivateLinkResource.Status = ptr.To(armcdn.SharedPrivateLinkResourceStatusPending)
			})
			missing := f.originRead(1)
			missing.status = http.StatusNotFound
			steps := append(f.roots(), missingBootstrapRoute(*f, *route))
			steps = append(steps, f.roots()...)
			steps = append(steps, f.roots()...)
			steps = append(steps, f.groupRead(), f.originRead(0), missing)
			return append(steps, backendStep{
				method: http.MethodPut, suffix: missing.suffix, status: http.StatusAccepted,
				body: missing.body, want: f.graph.Backends.OriginGroups[0].Origins[1].Parameters,
			})
		}, HTTPPreviewStaging},
		{"unapproved origins never activate", func(f *backendProvisioningFixture, route *armcdn.Route) []backendStep {
			f.changeOrigin(0, func(origin *armcdn.AFDOrigin) {
				origin.Properties.SharedPrivateLinkResource.Status = ptr.To(armcdn.SharedPrivateLinkResourceStatusPending)
			})
			return append(httpPreviewReads(*f, *route), append(f.roots(), f.reads()...)...)
		}, HTTPPreviewStaging},
		{"complete private bootstrap inventory enables only the route", func(f *backendProvisioningFixture, route *armcdn.Route) []backendStep {
			return append(append(httpPreviewReads(*f, *route), completeStagingSteps(*f, *route)...), enableRouteStep(*f, *route))
		}, HTTPPreviewRouteEnabling},
		{"endpoint activation atomically records that bootstrap ended", func(f *backendProvisioningFixture, route *armcdn.Route) []backendStep {
			route.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
			step := enableEndpointStep(*f)
			parameters := step.want.(armcdn.AFDEndpointUpdateParameters)
			parameters.Tags = maps.Clone(f.endpoint.Tags)
			parameters.Tags["networking.fleet.azure.com.activation"] = ptr.To("started")
			step.want = parameters
			return append(append(httpPreviewReads(*f, *route), httpPreviewInventory(*f, *route)...), step)
		}, HTTPPreviewEndpointEnabling},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, route := bootstrapFixture(t)
			steps := tt.setup(&f, &route)
			p, calls := disabledGraphClient(t, f, steps, nil)
			got, err := p.ReconcileHTTPPreview(context.Background(), f.snapshot.owner, f.snapshot.model)
			if err != nil || got.State != tt.state || got.EndpointHostName != "" || *calls != len(steps) {
				t.Fatalf("unexpected bootstrap progress: %+v, %v, calls=%d/%d", got, err, *calls, len(steps))
			}
			if *f.endpoint.Tags["networking.fleet.azure.com.activation"] != "pending" {
				t.Fatal("activation mutated the caller's observed endpoint tags")
			}
		})
	}
}

func TestReconcileHTTPPreviewBootstrapDoesNotBypassDeployment(t *testing.T) {
	for _, marker := range []string{"", "started"} {
		for _, enabled := range []armcdn.EnabledState{armcdn.EnabledStateDisabled, armcdn.EnabledStateEnabled} {
			t.Run(marker+"/"+string(enabled), func(t *testing.T) {
				f, _ := bootstrapFixture(t)
				delete(f.endpoint.Tags, "networking.fleet.azure.com.activation")
				if marker != "" {
					f.endpoint.Tags["networking.fleet.azure.com.activation"] = ptr.To(marker)
				}
				f.endpoint.Properties.EnabledState = ptr.To(enabled)
				steps := f.roots()
				p, calls := disabledGraphClient(t, f, steps, nil)
				got, err := p.ReconcileHTTPPreview(context.Background(), f.snapshot.owner, f.snapshot.model)
				if err != nil || got.State != HTTPPreviewPending || got.EndpointHostName != "" || *calls != len(steps) {
					t.Fatalf("non-bootstrap endpoint bypassed deployment: %+v, %v, calls=%d", got, err, *calls)
				}
			})
		}
	}
}

func TestHTTPPreviewBootstrapCancellation(t *testing.T) {
	for _, activatingEndpoint := range []bool{false, true} {
		f, route := bootstrapFixture(t)
		reads := completeStagingSteps(f, route)
		if activatingEndpoint {
			route.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
			reads = httpPreviewInventory(f, route)
		}
		testHTTPPreviewCancellation(t, f, append(httpPreviewReads(f, route), reads...))
	}
}

func TestBootstrapInventoryRejectsChangedResources(t *testing.T) {
	tests := []struct {
		name       string
		collection int
		change     func(*testing.T, backendProvisioningFixture, armcdn.Route) interface{}
	}{
		{"activation marker changed", 0, func(t *testing.T, f backendProvisioningFixture, _ armcdn.Route) interface{} {
			endpoint := inventoryCopy(t, f.endpoint)
			endpoint.Tags[endpointActivationTag] = ptr.To(endpointActivationStarted)
			return &endpoint
		}},
		{"route deployment failed", 1, func(t *testing.T, _ backendProvisioningFixture, route armcdn.Route) interface{} {
			route = inventoryCopy(t, route)
			route.Properties.DeploymentStatus = ptr.To(armcdn.DeploymentStatusFailed)
			return &route
		}},
		{"group provisioning regressed", 2, func(t *testing.T, f backendProvisioningFixture, _ armcdn.Route) interface{} {
			group := inventoryCopy(t, f.group)
			group.Properties.ProvisioningState = ptr.To(armcdn.AfdProvisioningStateUpdating)
			return &group
		}},
		{"origin approval regressed", 3, func(t *testing.T, f backendProvisioningFixture, _ armcdn.Route) interface{} {
			origin := inventoryCopy(t, f.originRead(0).body.(armcdn.AFDOrigin))
			origin.Properties.SharedPrivateLinkResource.Status = ptr.To(armcdn.SharedPrivateLinkResourceStatusPending)
			return &origin
		}},
		{"origin private target changed", 3, func(t *testing.T, f backendProvisioningFixture, _ armcdn.Route) interface{} {
			origin := inventoryCopy(t, f.originRead(0).body.(armcdn.AFDOrigin))
			origin.Properties.SharedPrivateLinkResource.PrivateLink.ID = ptr.To("/different/privateLinkServices/target")
			return &origin
		}},
		{"origin is still provisioning", 3, func(t *testing.T, f backendProvisioningFixture, _ armcdn.Route) interface{} {
			origin := inventoryCopy(t, f.originRead(0).body.(armcdn.AFDOrigin))
			origin.Properties.ProvisioningState = ptr.To(armcdn.AfdProvisioningStateUpdating)
			return &origin
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, route := bootstrapFixture(t)
			assertListedGraphChange(t, f, route, tt.collection, tt.change(t, f, route), "")
		})
	}
}

func TestBootstrapMetadataValidation(t *testing.T) {
	tests := []struct {
		name   string
		change func(*armcdn.AFDEndpoint)
	}{
		{"missing hostname", func(endpoint *armcdn.AFDEndpoint) { endpoint.Properties.HostName = nil }},
		{"empty phase", func(endpoint *armcdn.AFDEndpoint) { endpoint.Tags[endpointActivationTag] = ptr.To("") }},
		{"nil phase", func(endpoint *armcdn.AFDEndpoint) { endpoint.Tags[endpointActivationTag] = nil }},
		{"unknown phase", func(endpoint *armcdn.AFDEndpoint) { endpoint.Tags[endpointActivationTag] = ptr.To("unknown") }},
		{"duplicate phase", func(endpoint *armcdn.AFDEndpoint) {
			endpoint.Tags["NETWORKING.FLEET.AZURE.COM.ACTIVATION"] = ptr.To(endpointActivationPending)
		}},
		{"enabled without activation marker", func(endpoint *armcdn.AFDEndpoint) {
			endpoint.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
			endpoint.Properties.DeploymentStatus = ptr.To(armcdn.DeploymentStatusSucceeded)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, _ := bootstrapFixture(t)
			tt.change(&f.endpoint)
			steps := f.roots()
			p, calls := disabledGraphClient(t, f, steps, nil)
			got, err := p.ReconcileHTTPPreview(context.Background(), f.snapshot.owner, f.snapshot.model)
			if err == nil || got.EndpointHostName != "" || *calls != len(steps) {
				t.Fatalf("invalid bootstrap metadata advanced: %+v, %v, calls=%d", got, err, *calls)
			}
		})
	}
}

func TestActivatedHTTPPreviewRequiresDeployedChildren(t *testing.T) {
	tests := []struct {
		name       string
		collection int
		change     func(*backendProvisioningFixture, *armcdn.Route)
	}{
		{"route", 1, func(_ *backendProvisioningFixture, route *armcdn.Route) {
			route.Properties.DeploymentStatus = ptr.To(armcdn.DeploymentStatusNotStarted)
		}},
		{"origin group", 2, func(f *backendProvisioningFixture, _ *armcdn.Route) {
			f.group.Properties.DeploymentStatus = ptr.To(armcdn.DeploymentStatusNotStarted)
		}},
		{"origin", 3, func(f *backendProvisioningFixture, _ *armcdn.Route) {
			f.changeOrigin(0, func(origin *armcdn.AFDOrigin) {
				origin.Properties.DeploymentStatus = ptr.To(armcdn.DeploymentStatusNotStarted)
			})
		}},
	}
	for _, tt := range tests {
		for _, readOnly := range []bool{false, true} {
			operation := "reconcile"
			if readOnly {
				operation = "verify"
			}
			t.Run(tt.name+"/"+operation, func(t *testing.T) {
				f, route := routeFixture(t)
				f.endpoint.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
				route.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
				tt.change(&f, &route)
				steps := f.roots()
				if !readOnly {
					steps = httpPreviewReads(f, route)
				}
				inventory := httpPreviewInventory(f, route)
				steps = append(steps, inventory[:len(f.roots())+tt.collection+1]...)
				p, calls := disabledGraphClient(t, f, steps, nil)
				var hostname string
				var err error
				if readOnly {
					hostname, err = p.VerifyEnabledHTTPPreview(context.Background(), f.snapshot.owner, f.snapshot.model)
				} else {
					var result HTTPPreviewProvisioningResult
					result, err = p.ReconcileHTTPPreview(context.Background(), f.snapshot.owner, f.snapshot.model)
					hostname = result.EndpointHostName
				}
				if err == nil || hostname != "" || *calls != len(steps) {
					t.Fatalf("undeployed active child published readiness: %q, %v, calls=%d/%d", hostname, err, *calls, len(steps))
				}
			})
		}
	}
}

func TestEndpointActivationPreservesTags(t *testing.T) {
	for _, key := range []string{endpointActivationTag, "NETWORKING.FLEET.AZURE.COM.ACTIVATION"} {
		t.Run(key, func(t *testing.T) {
			endpoint := armcdn.AFDEndpoint{Tags: map[string]*string{
				key: ptr.To(endpointActivationPending), "unmanaged": ptr.To("preserve"),
			}}
			before := maps.Clone(endpoint.Tags)
			parameters, err := endpointActivationParameters(endpoint)
			if err != nil {
				t.Fatal(err)
			}
			if ptr.Deref(parameters.Properties.EnabledState, "") != armcdn.EnabledStateEnabled ||
				len(parameters.Tags) != 2 || ptr.Deref(parameters.Tags[endpointActivationTag], "") != endpointActivationStarted ||
				ptr.Deref(parameters.Tags["unmanaged"], "") != "preserve" {
				t.Fatalf("activation did not preserve unrelated tags and canonicalize its phase: %+v", parameters)
			}
			if !maps.Equal(before, endpoint.Tags) || ptr.Deref(endpoint.Tags[key], "") != endpointActivationPending {
				t.Fatal("activation mutated the caller's observed endpoint tags")
			}
		})
	}
}
