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

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	"k8s.io/utils/ptr"
)

func suspensionMutation(f backendProvisioningFixture, status int) []backendStep {
	f.endpoint.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
	reads := f.roots()
	return append(reads, backendStep{method: http.MethodPatch, suffix: reads[1].suffix, status: status,
		body: f.endpoint, want: armcdn.AFDEndpointUpdateParameters{Properties: &armcdn.AFDEndpointPropertiesUpdateParameters{
			EnabledState: ptr.To(armcdn.EnabledStateDisabled),
		}}})
}

func TestSuspendEndpointObservations(t *testing.T) {
	tests := []struct {
		name   string
		change func(*backendProvisioningFixture)
		steps  func(backendProvisioningFixture) []backendStep
		want   EndpointSuspensionState
	}{
		{name: "profile absent", want: EndpointAbsent, steps: func(f backendProvisioningFixture) []backendStep {
			reads := f.roots()[:1]
			reads[0].status = http.StatusNotFound
			return reads
		}},
		{name: "endpoint absent", want: EndpointAbsent, steps: func(f backendProvisioningFixture) []backendStep {
			reads := f.roots()
			reads[1].status = http.StatusNotFound
			return reads
		}},
		{name: "observed disabled deployment", want: EndpointSuspended},
		{name: "profile readiness does not block withdrawal", want: EndpointSuspended, change: func(f *backendProvisioningFixture) {
			f.snapshot.profile.Properties = nil
		}},
		{name: "hostname is unnecessary for suspension", want: EndpointSuspended, change: func(f *backendProvisioningFixture) {
			f.endpoint.Properties.HostName = nil
		}},
		{name: "updating endpoint", want: EndpointSuspensionPending, change: func(f *backendProvisioningFixture) {
			f.endpoint.Properties.ProvisioningState = ptr.To(armcdn.AfdProvisioningStateUpdating)
		}},
		{name: "disabled but deploying", want: EndpointSuspensionPending, change: func(f *backendProvisioningFixture) {
			f.endpoint.Properties.DeploymentStatus = ptr.To(armcdn.DeploymentStatusInProgress)
		}},
		{name: "disable only", want: EndpointSuspensionPending, steps: func(f backendProvisioningFixture) []backendStep {
			return suspensionMutation(f, 202)
		}},
		{name: "synchronous submission needs fresh observation", want: EndpointSuspensionPending, steps: func(f backendProvisioningFixture) []backendStep {
			return suspensionMutation(f, 200)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, _ := routeFixture(t)
			if tt.change != nil {
				tt.change(&f)
			}
			steps := f.roots()
			if tt.steps != nil {
				steps = tt.steps(f)
			}
			p, calls := disabledGraphClient(t, f, steps, nil)
			// Withdrawal must not depend on route/backend SDK clients.
			p.routes, p.backends.groups, p.backends.origins = nil, nil, nil
			got, err := p.SuspendEndpoint(context.Background(), f.snapshot.owner)
			if err != nil || got.State != tt.want || *calls != len(steps) {
				t.Fatalf("incorrect owned endpoint suspension: %#v, %v, calls=%d", got, err, *calls)
			}
		})
	}
}

func TestSuspendEndpointIdentityAndStateFailures(t *testing.T) {
	const (
		foreignProfileCase = "foreign profile"
		foreignOwnerCase   = "foreign owner"
	)
	tests := []struct {
		name   string
		change func(*backendProvisioningFixture)
		reads  int
	}{
		{name: foreignProfileCase, reads: 1, change: func(f *backendProvisioningFixture) {
			f.snapshot.profile.ID = ptr.To(changedValue)
		}},
		{name: foreignOwnerCase, reads: 1, change: func(f *backendProvisioningFixture) {
			for key := range f.snapshot.profile.Tags {
				f.snapshot.profile.Tags[key] = ptr.To(changedValue)
				break
			}
		}},
		{name: "foreign endpoint", reads: 2, change: func(f *backendProvisioningFixture) {
			f.endpoint.ID = ptr.To(changedValue)
		}},
		{name: "missing endpoint properties", reads: 2, change: func(f *backendProvisioningFixture) {
			f.endpoint.Properties = nil
		}},
		{name: "failed provisioning", reads: 2, change: func(f *backendProvisioningFixture) {
			f.endpoint.Properties.ProvisioningState = ptr.To(armcdn.AfdProvisioningStateFailed)
		}},
		{name: "unknown enabled state", reads: 2, change: func(f *backendProvisioningFixture) {
			f.endpoint.Properties.EnabledState = nil
		}},
		{name: "failed disabled deployment", reads: 2, change: func(f *backendProvisioningFixture) {
			f.endpoint.Properties.DeploymentStatus = ptr.To(armcdn.DeploymentStatusFailed)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, _ := routeFixture(t)
			tt.change(&f)
			steps := f.roots()[:tt.reads]
			p, calls := disabledGraphClient(t, f, steps, nil)
			got, err := p.SuspendEndpoint(context.Background(), f.snapshot.owner)
			if err == nil || got != (EndpointSuspensionResult{}) || *calls != len(steps) {
				t.Fatalf("unverified suspension returned success: %#v, %v, calls=%d", got, err, *calls)
			}
		})
	}
}

func TestSuspendEndpointErrorsAndCancellation(t *testing.T) {
	t.Run("PATCH not found is not observed absence", func(t *testing.T) {
		f, _ := routeFixture(t)
		steps := suspensionMutation(f, http.StatusNotFound)
		p, calls := disabledGraphClient(t, f, steps, nil)
		got, err := p.SuspendEndpoint(context.Background(), f.snapshot.owner)
		var responseError *azcore.ResponseError
		if !errors.As(err, &responseError) || responseError.StatusCode != http.StatusNotFound ||
			got != (EndpointSuspensionResult{}) || *calls != len(steps) {
			t.Fatalf("failed PATCH was mistaken for absence: %#v, %v", got, err)
		}
	})
	for _, status := range []int{403, 409, 429} {
		for at := 0; at < 3; at++ {
			f, _ := routeFixture(t)
			steps := suspensionMutation(f, 202)[:at+1]
			steps[at].status = status
			p, calls := disabledGraphClient(t, f, steps, nil)
			got, err := p.SuspendEndpoint(context.Background(), f.snapshot.owner)
			var responseError *azcore.ResponseError
			if !errors.As(err, &responseError) || responseError.StatusCode != status ||
				got != (EndpointSuspensionResult{}) || *calls != len(steps) {
				t.Fatalf("SDK error returned suspension: %#v, %v, calls=%d", got, err, *calls)
			}
		}
	}
	for at := 0; at <= 3; at++ {
		f, _ := routeFixture(t)
		steps := suspensionMutation(f, 202)[:at]
		ctx, cancel := context.WithCancel(context.Background())
		p, calls := disabledGraphClient(t, f, steps, func(n int) {
			if n == at {
				cancel()
			}
		})
		if at == 0 {
			cancel()
		}
		got, err := p.SuspendEndpoint(ctx, f.snapshot.owner)
		cancel()
		if !errors.Is(err, context.Canceled) || got != (EndpointSuspensionResult{}) || *calls != len(steps) {
			t.Fatalf("cancellation allowed more operations/success: %#v, %v, calls=%d", got, err, *calls)
		}
	}
}

func TestSuspendEndpointRestartAndPreflight(t *testing.T) {
	for _, kind := range []string{"invalid owner", "missing root clients", "nil suspension provider"} {
		t.Run(kind, func(t *testing.T) {
			f, _ := routeFixture(t)
			p, calls := disabledGraphClient(t, f, nil, nil)
			switch kind {
			case "invalid owner":
				f.snapshot.owner.UID = ""
			case "missing root clients":
				p.backends.roots.endpoints = nil
			case "nil suspension provider":
				p = nil
			}
			got, err := p.SuspendEndpoint(context.Background(), f.snapshot.owner)
			if err == nil || !reflect.DeepEqual(got, EndpointSuspensionResult{}) || *calls != 0 {
				t.Fatalf("invalid suspension reached SDK: %#v, %v", got, err)
			}
		})
	}
	for _, disabled := range []bool{false, true, true} {
		f, _ := routeFixture(t)
		steps, want := f.roots(), EndpointSuspended
		if !disabled {
			steps, want = suspensionMutation(f, 202), EndpointSuspensionPending
		}
		p, calls := disabledGraphClient(t, f, steps, nil)
		got, err := p.SuspendEndpoint(context.Background(), f.snapshot.owner)
		if err != nil || got.State != want || *calls != len(steps) {
			t.Fatalf("restart did not require fresh suspension evidence: %#v, %v", got, err)
		}
	}
}
