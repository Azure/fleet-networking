/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package frontdoor

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	"k8s.io/utils/ptr"

	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
)

type rootStep struct {
	method   string
	endpoint bool
	status   int
}

func rootProvisioningSnapshot(t *testing.T) (privateOriginSnapshot, RootRequests, armcdn.AFDEndpoint) {
	t.Helper()
	snapshot := originSnapshot(t, backendModel())
	requests, err := BuildRootRequests(snapshot.scope, snapshot.owner)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.profile.Properties = &armcdn.ProfileProperties{
		ProvisioningState: ptr.To(armcdn.ProfileProvisioningStateSucceeded),
		ResourceState:     ptr.To(armcdn.ProfileResourceStateActive),
	}
	endpoint := requests.Endpoint
	endpoint.Properties = &armcdn.AFDEndpointProperties{EnabledState: ptr.To(armcdn.EnabledStateDisabled)}
	endpoint.ID = ptr.To(requests.ProfileID + "/afdEndpoints/" + requests.EndpointName)
	endpoint.Name = ptr.To(requests.EndpointName)
	endpoint.Properties.ProfileName = ptr.To(requests.ProfileName)
	endpoint.Properties.HostName = ptr.To("observed-endpoint.z01.azurefd.net")
	endpoint.Properties.ProvisioningState = ptr.To(armcdn.AfdProvisioningStateSucceeded)
	endpoint.Properties.DeploymentStatus = ptr.To(armcdn.DeploymentStatusSucceeded)
	return snapshot, requests, endpoint
}

func TestRootProvisionerEnsureDisabledRoots(t *testing.T) {
	profileGet := rootStep{method: http.MethodGet, status: http.StatusOK}
	endpointGet := rootStep{method: http.MethodGet, endpoint: true, status: http.StatusOK}
	tests := []struct {
		name      string
		change    func(*armcdn.Profile, *armcdn.AFDEndpoint)
		steps     []rootStep
		wantState RootProvisioningState
		wantError bool
	}{
		{name: "observed disabled roots are a no-op", steps: []rootStep{profileGet, endpointGet}, wantState: DisabledRootsObserved},
		{name: "create missing profile first", steps: []rootStep{
			{method: http.MethodGet, status: 404}, {method: http.MethodPut, status: 202},
		}, wantState: ProfileProvisioningPending},
		{name: "synchronous profile submission still requires observation", steps: []rootStep{
			{method: http.MethodGet, status: 404}, {method: http.MethodPut, status: 201},
		}, wantState: ProfileProvisioningPending},
		{name: "restart while profile creating", steps: []rootStep{profileGet}, wantState: ProfileProvisioningPending,
			change: func(p *armcdn.Profile, _ *armcdn.AFDEndpoint) {
				p.Properties.ProvisioningState = ptr.To(armcdn.ProfileProvisioningStateCreating)
			}},
		{name: "profile updating", steps: []rootStep{profileGet}, wantState: ProfileProvisioningPending,
			change: func(p *armcdn.Profile, _ *armcdn.AFDEndpoint) {
				p.Properties.ProvisioningState = ptr.To(armcdn.ProfileProvisioningStateUpdating)
			}},
		{name: "profile deleting", steps: []rootStep{profileGet}, wantState: ProfileProvisioningPending,
			change: func(p *armcdn.Profile, _ *armcdn.AFDEndpoint) {
				p.Properties.ResourceState = ptr.To(armcdn.ProfileResourceStateDeleting)
			}},
		{name: "profile failed", steps: []rootStep{profileGet}, wantError: true,
			change: func(p *armcdn.Profile, _ *armcdn.AFDEndpoint) {
				p.Properties.ProvisioningState = ptr.To(armcdn.ProfileProvisioningStateFailed)
			}},
		{name: "missing profile state", steps: []rootStep{profileGet}, wantError: true,
			change: func(p *armcdn.Profile, _ *armcdn.AFDEndpoint) { p.Properties = nil }},
		{name: "unknown profile state", steps: []rootStep{profileGet}, wantError: true,
			change: func(p *armcdn.Profile, _ *armcdn.AFDEndpoint) {
				p.Properties.ProvisioningState = ptr.To(armcdn.ProfileProvisioningState(changedValue))
			}},
		{name: "profile migration is not adoption", steps: []rootStep{profileGet}, wantError: true,
			change: func(p *armcdn.Profile, _ *armcdn.AFDEndpoint) {
				p.Properties.ResourceState = ptr.To(armcdn.ProfileResourceStateMigrating)
			}},
		{name: "foreign owner", steps: []rootStep{profileGet}, wantError: true,
			change: func(p *armcdn.Profile, _ *armcdn.AFDEndpoint) {
				p.Tags["networking.fleet.azure.com.hub-uid"] = ptr.To(changedValue)
			}},
		{name: "SKU drift is not silently migrated", steps: []rootStep{profileGet}, wantError: true,
			change: func(p *armcdn.Profile, _ *armcdn.AFDEndpoint) {
				p.SKU.Name = ptr.To(armcdn.SKUNameStandardAzureFrontDoor)
			}},
		{name: "location drift", steps: []rootStep{profileGet}, wantError: true,
			change: func(p *armcdn.Profile, _ *armcdn.AFDEndpoint) { p.Location = ptr.To("eastus") }},
		{name: "create missing endpoint disabled", steps: []rootStep{profileGet,
			{method: http.MethodGet, endpoint: true, status: 404}, {method: http.MethodPut, endpoint: true, status: 202},
		}, wantState: EndpointProvisioningPending},
		{name: "synchronous endpoint submission still requires observation", steps: []rootStep{profileGet,
			{method: http.MethodGet, endpoint: true, status: 404}, {method: http.MethodPut, endpoint: true, status: 201},
		}, wantState: EndpointProvisioningPending},
		{name: "restart while endpoint creating", steps: []rootStep{profileGet, endpointGet}, wantState: EndpointProvisioningPending,
			change: func(_ *armcdn.Profile, e *armcdn.AFDEndpoint) {
				e.Properties.ProvisioningState = ptr.To(armcdn.AfdProvisioningStateCreating)
			}},
		{name: "endpoint deployment pending", steps: []rootStep{profileGet, endpointGet}, wantState: EndpointProvisioningPending,
			change: func(_ *armcdn.Profile, e *armcdn.AFDEndpoint) {
				e.Properties.DeploymentStatus = ptr.To(armcdn.DeploymentStatusInProgress)
			}},
		{name: "disable observed enabled endpoint", steps: []rootStep{profileGet, endpointGet,
			{method: http.MethodPatch, endpoint: true, status: 202},
		}, wantState: EndpointProvisioningPending,
			change: func(_ *armcdn.Profile, e *armcdn.AFDEndpoint) {
				e.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
			}},
		{name: "foreign endpoint ID", steps: []rootStep{profileGet, endpointGet}, wantError: true,
			change: func(_ *armcdn.Profile, e *armcdn.AFDEndpoint) { e.ID = ptr.To(changedValue) }},
		{name: "contradictory endpoint parent", steps: []rootStep{profileGet, endpointGet}, wantError: true,
			change: func(_ *armcdn.Profile, e *armcdn.AFDEndpoint) { e.Properties.ProfileName = ptr.To(changedValue) }},
		{name: "endpoint failed", steps: []rootStep{profileGet, endpointGet}, wantError: true,
			change: func(_ *armcdn.Profile, e *armcdn.AFDEndpoint) {
				e.Properties.DeploymentStatus = ptr.To(armcdn.DeploymentStatusFailed)
			}},
		{name: "unknown endpoint state", steps: []rootStep{profileGet, endpointGet}, wantError: true,
			change: func(_ *armcdn.Profile, e *armcdn.AFDEndpoint) { e.Properties.ProvisioningState = nil }},
		{name: "endpoint hostname absent", steps: []rootStep{profileGet, endpointGet}, wantError: true,
			change: func(_ *armcdn.Profile, e *armcdn.AFDEndpoint) { e.Properties.HostName = nil }},
		{name: "endpoint properties absent", steps: []rootStep{profileGet, endpointGet}, wantError: true,
			change: func(_ *armcdn.Profile, e *armcdn.AFDEndpoint) { e.Properties = nil }},
		{name: "endpoint location drift", steps: []rootStep{profileGet, endpointGet}, wantError: true,
			change: func(_ *armcdn.Profile, e *armcdn.AFDEndpoint) { e.Location = ptr.To("eastus") }},
		{name: "endpoint name mismatch", steps: []rootStep{profileGet, endpointGet}, wantError: true,
			change: func(_ *armcdn.Profile, e *armcdn.AFDEndpoint) { e.Name = ptr.To(changedValue) }},
		{name: "endpoint enabled state absent", steps: []rootStep{profileGet, endpointGet}, wantError: true,
			change: func(_ *armcdn.Profile, e *armcdn.AFDEndpoint) { e.Properties.EnabledState = nil }},
		{name: "endpoint provisioning failed", steps: []rootStep{profileGet, endpointGet}, wantError: true,
			change: func(_ *armcdn.Profile, e *armcdn.AFDEndpoint) {
				e.Properties.ProvisioningState = ptr.To(armcdn.AfdProvisioningStateFailed)
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot, requests, endpoint := rootProvisioningSnapshot(t)
			if tt.change != nil {
				tt.change(&snapshot.profile, &endpoint)
			}
			provisioner, calls := rootProvisioningClient(t, snapshot, requests, endpoint, tt.steps)
			got, err := provisioner.EnsureDisabledRoots(context.Background(), snapshot.owner, snapshot.model)
			if (err != nil) != tt.wantError || got.State != tt.wantState || *calls != len(tt.steps) {
				t.Fatalf("result=%#v error=%v calls=%d, want %s/error=%v/%d", got, err, *calls, tt.wantState, tt.wantError, len(tt.steps))
			}
			if tt.wantError && !reflect.DeepEqual(got, RootProvisioningResult{}) {
				t.Fatalf("error returned partial convergence: %#v", got)
			}
			if got.State == DisabledRootsObserved && got.EndpointHostName != *endpoint.Properties.HostName {
				t.Fatalf("hostname was not taken from observed Azure endpoint: %#v", got)
			}
			if got.State != DisabledRootsObserved && got.EndpointHostName != "" {
				t.Fatalf("unobserved hostname was published: %#v", got)
			}
		})
	}
}

func rootProvisioningClient(t *testing.T, snapshot privateOriginSnapshot, requests RootRequests,
	endpoint armcdn.AFDEndpoint, steps []rootStep) (*RootProvisioner, *int) {
	t.Helper()
	calls := 0
	var budget time.Time
	transport := cleanupTransport(func(request *http.Request) (*http.Response, error) {
		if calls >= len(steps) {
			t.Fatalf("unexpected repeated request/poll: %s %s", request.Method, request.URL)
		}
		step := steps[calls]
		calls++
		path := requests.ProfileID
		var body interface{} = snapshot.profile
		if step.endpoint {
			path += "/afdEndpoints/" + requests.EndpointName
			body = endpoint
		}
		if request.Method != step.method || request.URL.Path != path || request.URL.Query().Get("api-version") != testAPIVersion {
			t.Fatalf("unexpected SDK request: %s %s; want %s %s", request.Method, request.URL, step.method, path)
		}
		deadline, ok := request.Context().Deadline()
		if !ok || time.Until(deadline) > 31*time.Second {
			t.Fatal("provisioning operation has no bounded deadline")
		}
		if step.method == http.MethodGet && !step.endpoint {
			budget = deadline
		} else if !deadline.Equal(budget) {
			t.Fatal("child operation restarted the timeout")
		}
		if step.method != http.MethodGet {
			assertRootMutationBody(t, request, requests, step.endpoint)
		}
		header := http.Header{"Content-Type": []string{"application/json"}}
		if step.status == http.StatusAccepted {
			header.Set("Azure-AsyncOperation", "https://management.azure.com/operations/root-create?api-version="+testAPIVersion)
		}
		if step.status >= http.StatusBadRequest {
			header.Set("Retry-After", "1")
			body = map[string]interface{}{"error": map[string]string{"code": "UnitTestError", "message": "request failed"}}
		}
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: step.status, Header: header, Body: io.NopCloser(strings.NewReader(string(data))), Request: request}, nil
	})
	provisioner, err := NewRootProvisioner(snapshot.scope, cleanupCredential{}, &arm.ClientOptions{
		ClientOptions: policy.ClientOptions{Transport: transport, Retry: policy.RetryOptions{MaxRetries: -1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return provisioner, &calls
}

func assertRootMutationBody(t *testing.T, request *http.Request, roots RootRequests, endpoint bool) {
	t.Helper()
	var actual interface{}
	if err := json.NewDecoder(request.Body).Decode(&actual); err != nil {
		t.Fatal(err)
	}
	var desired interface{} = roots.Profile
	if endpoint {
		desired = roots.Endpoint
	}
	if request.Method == http.MethodPatch {
		desired = armcdn.AFDEndpointUpdateParameters{Properties: &armcdn.AFDEndpointPropertiesUpdateParameters{
			EnabledState: ptr.To(armcdn.EnabledStateDisabled),
		}}
	}
	data, err := json.Marshal(desired)
	if err != nil {
		t.Fatal(err)
	}
	var expected interface{}
	if err := json.Unmarshal(data, &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("unexpected enabled/public/adoption mutation body: %#v, want %#v", actual, expected)
	}
}

func TestRootProvisionerPreservesSDKFailures(t *testing.T) {
	for _, status := range []int{403, 409, 429} {
		for _, operation := range []string{"profile GET", "profile PUT", "endpoint GET", "endpoint PUT", "endpoint PATCH"} {
			t.Run(http.StatusText(status)+"/"+operation, func(t *testing.T) {
				snapshot, requests, endpoint := rootProvisioningSnapshot(t)
				steps := []rootStep{{method: http.MethodGet, status: 200}}
				switch operation {
				case "profile GET":
					steps[0].status = status
				case "profile PUT":
					steps[0].status = 404
					steps = append(steps, rootStep{method: http.MethodPut, status: status})
				case "endpoint GET":
					steps = append(steps, rootStep{method: http.MethodGet, endpoint: true, status: status})
				case "endpoint PUT":
					steps = append(steps, rootStep{method: http.MethodGet, endpoint: true, status: 404}, rootStep{method: http.MethodPut, endpoint: true, status: status})
				case "endpoint PATCH":
					endpoint.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
					steps = append(steps, rootStep{method: http.MethodGet, endpoint: true, status: 200}, rootStep{method: http.MethodPatch, endpoint: true, status: status})
				}
				provisioner, calls := rootProvisioningClient(t, snapshot, requests, endpoint, steps)
				got, err := provisioner.EnsureDisabledRoots(context.Background(), snapshot.owner, snapshot.model)
				var responseError *azcore.ResponseError
				if !errors.As(err, &responseError) || responseError.StatusCode != status ||
					!reflect.DeepEqual(got, RootProvisioningResult{}) || *calls != len(steps) {
					t.Fatalf("lost SDK failure or returned partial convergence: %#v, %v, calls=%d", got, err, *calls)
				}
			})
		}
	}
}

func TestRootProvisionerRestartAndNoOp(t *testing.T) {
	snapshot, roots, endpoint := rootProvisioningSnapshot(t)
	steps := []rootStep{
		{method: http.MethodGet, status: 404}, {method: http.MethodPut, status: 202},
		{method: http.MethodGet, status: 200}, {method: http.MethodGet, endpoint: true, status: 404},
		{method: http.MethodPut, endpoint: true, status: 202},
		{method: http.MethodGet, status: 200}, {method: http.MethodGet, endpoint: true, status: 200},
		{method: http.MethodGet, status: 200}, {method: http.MethodGet, endpoint: true, status: 200},
	}
	provisioner, calls := rootProvisioningClient(t, snapshot, roots, endpoint, steps)
	states := []RootProvisioningState{
		ProfileProvisioningPending, EndpointProvisioningPending, DisabledRootsObserved, DisabledRootsObserved,
	}
	for _, want := range states {
		got, err := provisioner.EnsureDisabledRoots(context.Background(), snapshot.owner, snapshot.model)
		if err != nil || got.State != want {
			t.Fatalf("restart did not resume by observation: %#v, %v; want %s", got, err, want)
		}
	}
	if *calls != len(steps) {
		t.Fatalf("restart did not reread roots or repeated a mutation: calls=%d", *calls)
	}
}

func TestRootProvisionerPreflight(t *testing.T) {
	tests := []struct {
		name   string
		change func(*privateOriginSnapshot)
		cancel bool
		noSDK  bool
	}{
		{"wrong owner", func(s *privateOriginSnapshot) { s.model.UID = changedValue }, false, false},
		{"missing hub identity", func(s *privateOriginSnapshot) { s.owner.HubUID = "" }, false, false},
		{"unsupported execution intent", func(s *privateOriginSnapshot) {
			s.model.Routes[0].Rules[0].Matches = []gatewaymodel.HTTPMatch{{PathType: graphExactPathType, Path: "/"}}
		}, false, false},
		{"invalid backend", func(s *privateOriginSnapshot) { s.model.Routes[0].Rules[0].Backends[0].Port = 0 }, false, false},
		{"empty graph", func(s *privateOriginSnapshot) { s.model.Routes = nil }, false, false},
		{"uninitialized SDK", nil, false, true},
		{"canceled operation", nil, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot, roots, endpoint := rootProvisioningSnapshot(t)
			if tt.change != nil {
				tt.change(&snapshot)
			}
			provisioner, _ := rootProvisioningClient(t, snapshot, roots, endpoint, nil)
			if tt.noSDK {
				provisioner = &RootProvisioner{scope: snapshot.scope}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancel {
				cancel()
			}
			got, err := provisioner.EnsureDisabledRoots(ctx, snapshot.owner, snapshot.model)
			if err == nil || !reflect.DeepEqual(got, RootProvisioningResult{}) {
				t.Fatalf("invalid/canceled operation did not fail before ARM: %#v, %v", got, err)
			}
			if tt.cancel && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
			}
		})
	}
}

func TestRootProvisionerCancellationPreventsNextOperation(t *testing.T) {
	tests := []struct {
		name     string
		cancelAt int
		missing  bool
	}{
		{"before profile creation", 1, true},
		{"before child read", 1, false},
		{"before endpoint creation", 2, true},
		{"before endpoint disabling", 2, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot, _, endpoint := rootProvisioningSnapshot(t)
			endpoint.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			transport := cleanupTransport(func(request *http.Request) (*http.Response, error) {
				calls++
				if request.Method != http.MethodGet || calls > tt.cancelAt {
					t.Fatalf("canceled operation proceeded to mutation/child read: %s", request.Method)
				}
				status := http.StatusOK
				var body interface{} = snapshot.profile
				if calls == 2 {
					body = endpoint
				}
				data, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				if calls == tt.cancelAt {
					cancel()
					if tt.missing {
						status = http.StatusNotFound
						data = []byte(`{"error":{"code":"NotFound"}}`)
					}
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}},
					Body: io.NopCloser(strings.NewReader(string(data))), Request: request}, nil
			})
			provisioner, err := NewRootProvisioner(snapshot.scope, cleanupCredential{}, &arm.ClientOptions{
				ClientOptions: policy.ClientOptions{Transport: transport, Retry: policy.RetryOptions{MaxRetries: -1}},
			})
			if err != nil {
				t.Fatal(err)
			}
			got, err := provisioner.EnsureDisabledRoots(ctx, snapshot.owner, snapshot.model)
			if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(got, RootProvisioningResult{}) || calls != tt.cancelAt {
				t.Fatalf("cancellation returned a success-shaped result: %#v, %v, calls=%d", got, err, calls)
			}
		})
	}
}

func TestRootProvisionerExpiredDeadline(t *testing.T) {
	snapshot, roots, endpoint := rootProvisioningSnapshot(t)
	provisioner, _ := rootProvisioningClient(t, snapshot, roots, endpoint, nil)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	got, err := provisioner.EnsureDisabledRoots(ctx, snapshot.owner, snapshot.model)
	if !errors.Is(err, context.DeadlineExceeded) || !reflect.DeepEqual(got, RootProvisioningResult{}) {
		t.Fatalf("expired operation did not fail before ARM: %#v, %v", got, err)
	}
}
