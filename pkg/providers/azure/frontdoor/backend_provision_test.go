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
)

type backendProvisioningFixture struct {
	snapshot privateOriginSnapshot
	graph    HTTPPreviewRequests
	endpoint armcdn.AFDEndpoint
	group    armcdn.AFDOriginGroup
}

type backendStep struct {
	method string
	suffix string
	status int
	body   interface{}
	want   interface{}
}

func backendFixture(t *testing.T) backendProvisioningFixture {
	t.Helper()
	snapshot, _, endpoint := rootProvisioningSnapshot(t)
	endpoint.Tags[endpointActivationTag] = ptr.To(endpointActivationStarted)
	graph, err := BuildHTTPPreviewRequests(snapshot.scope, snapshot.owner, snapshot.model)
	if err != nil {
		t.Fatal(err)
	}
	// Keep observed properties independent of the desired mutation bodies.
	observed, err := BuildBackendRequests(snapshot.model)
	if err != nil {
		t.Fatal(err)
	}
	group := observed.OriginGroups[0].Parameters
	group.ID = ptr.To(graph.Roots.ProfileID + "/originGroups/" + graph.Backends.OriginGroups[0].Name)
	group.Name = ptr.To(graph.Backends.OriginGroups[0].Name)
	group.Properties.ProfileName = ptr.To(graph.Roots.ProfileName)
	group.Properties.ProvisioningState = ptr.To(armcdn.AfdProvisioningStateSucceeded)
	group.Properties.DeploymentStatus = ptr.To(armcdn.DeploymentStatusSucceeded)
	return backendProvisioningFixture{snapshot: snapshot, graph: graph, endpoint: endpoint, group: group}
}

func (f backendProvisioningFixture) roots() []backendStep {
	return []backendStep{
		{method: http.MethodGet, status: 200, body: f.snapshot.profile},
		{method: http.MethodGet, suffix: "/afdEndpoints/" + f.graph.Roots.EndpointName, status: 200, body: f.endpoint},
	}
}

func (f backendProvisioningFixture) groupRead() backendStep {
	return backendStep{method: http.MethodGet, suffix: "/originGroups/" + f.graph.Backends.OriginGroups[0].Name, status: 200, body: f.group}
}

func (f backendProvisioningFixture) groupUpdateParameters() armcdn.AFDOriginGroupUpdateParameters {
	desired := f.graph.Backends.OriginGroups[0].Parameters.Properties
	return armcdn.AFDOriginGroupUpdateParameters{Properties: &armcdn.AFDOriginGroupUpdatePropertiesParameters{
		HealthProbeSettings: desired.HealthProbeSettings, SessionAffinityState: desired.SessionAffinityState,
	}}
}

func (f backendProvisioningFixture) originRead(i int) backendStep {
	group := f.graph.Backends.OriginGroups[0]
	ref := OriginReference{OriginGroup: group.Name, Origin: group.Origins[i].Name}
	return backendStep{method: http.MethodGet, suffix: "/originGroups/" + ref.OriginGroup + "/origins/" + ref.Origin,
		status: 200, body: f.snapshot.origins[ref]}
}

func (f backendProvisioningFixture) reads() []backendStep {
	steps := append(f.roots(), f.groupRead())
	for i := range f.graph.Backends.OriginGroups[0].Origins {
		steps = append(steps, f.originRead(i))
	}
	return steps
}

func (f backendProvisioningFixture) changeOrigin(i int, change func(*armcdn.AFDOrigin)) {
	group := f.graph.Backends.OriginGroups[0]
	ref := OriginReference{OriginGroup: group.Name, Origin: group.Origins[i].Name}
	origin := f.snapshot.origins[ref]
	change(&origin)
	f.snapshot.origins[ref] = origin
}

func backendProvisioningClient(t *testing.T, f backendProvisioningFixture, steps []backendStep,
	after func(int)) (*BackendProvisioner, *int) {
	t.Helper()
	options, calls := backendProvisioningOptions(t, f, steps, after)
	provisioner, err := NewBackendProvisioner(f.snapshot.scope, cleanupCredential{}, options)
	if err != nil {
		t.Fatal(err)
	}
	return provisioner, calls
}

func backendProvisioningOptions(t *testing.T, f backendProvisioningFixture, steps []backendStep,
	after func(int)) (*arm.ClientOptions, *int) {
	t.Helper()
	calls := 0
	var budget time.Time
	transport := cleanupTransport(func(request *http.Request) (*http.Response, error) {
		if calls >= len(steps) {
			t.Fatalf("unexpected request/poll: %s %s", request.Method, request.URL)
		}
		step := steps[calls]
		calls++
		if request.Method != step.method || request.URL.Path != f.graph.Roots.ProfileID+step.suffix ||
			request.URL.Query().Get("api-version") != testAPIVersion {
			t.Fatalf("unexpected SDK request: %s %s; want %s %s", request.Method, request.URL, step.method, step.suffix)
		}
		deadline, ok := request.Context().Deadline()
		if !ok || time.Until(deadline) > 31*time.Second {
			t.Fatal("backend staging has no bounded deadline")
		}
		if step.suffix == "" {
			budget = deadline
		} else if !deadline.Equal(budget) {
			t.Fatal("backend child request restarted the operation budget")
		}
		if step.want != nil {
			var actual, want interface{}
			if err := json.NewDecoder(request.Body).Decode(&actual); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(step.suffix, "/origins/") {
				if _, found := requestProperties(t, actual)["originHostHeader"]; found {
					t.Fatalf("private origin %s must omit originHostHeader: %#v", request.Method, actual)
				}
			}
			data, err := json.Marshal(step.want)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, want) {
				t.Fatalf("unexpected private mutation body: %#v; want %#v", actual, want)
			}
		}
		body := step.body
		header := http.Header{"Content-Type": []string{"application/json"}}
		if step.status == 202 {
			header.Set("Azure-AsyncOperation", "https://management.azure.com/operations/backend-stage?api-version="+testAPIVersion)
		}
		if step.status >= 400 {
			header.Set("Retry-After", "1")
			body = map[string]interface{}{"error": map[string]string{"code": "TestError", "message": "request failed"}}
		}
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		if after != nil {
			after(calls)
		}
		return &http.Response{StatusCode: step.status, Header: header, Body: io.NopCloser(strings.NewReader(string(data))),
			Request: request}, nil
	})
	return &arm.ClientOptions{
		ClientOptions: policy.ClientOptions{Transport: transport, Retry: policy.RetryOptions{MaxRetries: -1}},
	}, &calls
}

func TestBackendProvisionerRequiresObservedDisabledRoots(t *testing.T) {
	tests := []struct {
		name    string
		change  func(*backendProvisioningFixture)
		missing int
		stopAt  int
		wantErr bool
	}{
		{name: "missing profile", missing: 1, stopAt: 1},
		{name: "profile not ready", stopAt: 1, change: func(f *backendProvisioningFixture) {
			f.snapshot.profile.Properties.ProvisioningState = ptr.To(armcdn.ProfileProvisioningStateCreating)
		}},
		{name: "foreign profile", stopAt: 1, wantErr: true, change: func(f *backendProvisioningFixture) {
			f.snapshot.profile.Tags["networking.fleet.azure.com.hub-uid"] = ptr.To(changedValue)
		}},
		{name: "missing endpoint", missing: 2, stopAt: 2},
		{name: "enabled endpoint", stopAt: 2, change: func(f *backendProvisioningFixture) {
			f.endpoint.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
		}},
		{name: "endpoint updating", stopAt: 2, change: func(f *backendProvisioningFixture) {
			f.endpoint.Properties.ProvisioningState = ptr.To(armcdn.AfdProvisioningStateUpdating)
		}},
		{name: "endpoint deploying", stopAt: 2, change: func(f *backendProvisioningFixture) {
			f.endpoint.Properties.DeploymentStatus = ptr.To(armcdn.DeploymentStatusInProgress)
		}},
		{name: "foreign endpoint", stopAt: 2, wantErr: true, change: func(f *backendProvisioningFixture) {
			f.endpoint.ID = ptr.To(changedValue)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := backendFixture(t)
			if tt.change != nil {
				tt.change(&f)
			}
			steps := f.roots()[:tt.stopAt]
			if tt.missing != 0 {
				steps[tt.missing-1].status = 404
			}
			p, calls := backendProvisioningClient(t, f, steps, nil)
			got, err := p.EnsurePrivateBackends(context.Background(), f.snapshot.owner, f.snapshot.model)
			if (err != nil) != tt.wantErr || *calls != len(steps) {
				t.Fatalf("root prerequisite: %#v, %v, calls=%d", got, err, *calls)
			}
			if err == nil && got.State != BackendRootsPending {
				t.Fatalf("unready root allowed backend access: %#v", got)
			}
			if err != nil && !reflect.DeepEqual(got, BackendProvisioningResult{}) {
				t.Fatalf("failure returned partial success: %#v", got)
			}
		})
	}
}

func TestBackendProvisionerStagesPrivateResources(t *testing.T) {
	tests := []struct {
		name       string
		change     func(*backendProvisioningFixture)
		steps      func(backendProvisioningFixture) []backendStep
		want       BackendProvisioningState
		wantOrigin PrivateOriginState
	}{
		{name: "observed approved backends", want: PrivateBackendsConverged},
		{name: "observed nil Host header", want: PrivateBackendsConverged,
			change: func(f *backendProvisioningFixture) {
				f.changeOrigin(0, func(o *armcdn.AFDOrigin) { o.Properties.OriginHostHeader = nil })
			}},
		{name: "observed empty Host header", want: PrivateBackendsConverged,
			change: func(f *backendProvisioningFixture) {
				f.changeOrigin(0, func(o *armcdn.AFDOrigin) { o.Properties.OriginHostHeader = ptr.To("") })
			}},
		{name: "observed load balancing settings do not trigger updates", want: PrivateBackendsConverged,
			change: func(f *backendProvisioningFixture) {
				f.group.Properties.LoadBalancingSettings = &armcdn.LoadBalancingSettingsParameters{
					SampleSize: ptr.To(int32(8)), SuccessfulSamplesRequired: ptr.To(int32(5)),
					AdditionalLatencyInMilliseconds: ptr.To(int32(100)),
				}
			}},
		{name: "create group before origins", want: BackendGroupPending,
			steps: func(f backendProvisioningFixture) []backendStep {
				read := f.groupRead()
				read.status = 404
				return append(f.roots(), read, backendStep{method: http.MethodPut, suffix: read.suffix, status: 202,
					body: f.group, want: f.graph.Backends.OriginGroups[0].Parameters})
			}},
		{name: "update controlled group drift", want: BackendGroupPending,
			change: func(f *backendProvisioningFixture) {
				f.group.Properties.HealthProbeSettings.ProbePath = ptr.To("/drift")
			},
			steps: func(f backendProvisioningFixture) []backendStep {
				read := f.groupRead()
				return append(f.roots(), read, backendStep{method: http.MethodPatch, suffix: read.suffix, status: 202,
					body: f.group, want: f.groupUpdateParameters()})
			}},
		{name: "group in flight blocks origins", want: BackendGroupPending,
			change: func(f *backendProvisioningFixture) {
				f.group.Properties.ProvisioningState = ptr.To(armcdn.AfdProvisioningStateCreating)
			}, steps: func(f backendProvisioningFixture) []backendStep { return append(f.roots(), f.groupRead()) }},
		{name: "group deploying is not convergence", want: BackendOriginsPending,
			change: func(f *backendProvisioningFixture) {
				f.group.Properties.DeploymentStatus = ptr.To(armcdn.DeploymentStatusNotStarted)
			}},
		{name: "missing first origin is private", want: BackendOriginsPending,
			steps: func(f backendProvisioningFixture) []backendStep {
				read := f.originRead(0)
				read.status = 404
				return append(f.roots(), f.groupRead(), read, backendStep{method: http.MethodPut, suffix: read.suffix, status: 202,
					body: f.originRead(0).body, want: f.graph.Backends.OriginGroups[0].Origins[0].Parameters})
			}},
		{name: "weight drift updated privately", want: BackendOriginsPending,
			change: func(f *backendProvisioningFixture) {
				f.changeOrigin(0, func(o *armcdn.AFDOrigin) { o.Properties.Weight = ptr.To(int32(999)) })
			}, steps: func(f backendProvisioningFixture) []backendStep {
				read := f.originRead(0)
				return append(f.roots(), f.groupRead(), read, backendStep{method: http.MethodPatch, suffix: read.suffix, status: 202,
					body: read.body, want: f.graph.Backends.OriginGroups[0].Origins[0].Parameters})
			}},
		{name: "approval pending does not starve later origin", want: BackendOriginsPending,
			change: func(f *backendProvisioningFixture) {
				f.changeOrigin(0, func(o *armcdn.AFDOrigin) {
					o.Properties.ProvisioningState = ptr.To(armcdn.AfdProvisioningStateCreating)
					o.Properties.SharedPrivateLinkResource.Status = ptr.To(armcdn.SharedPrivateLinkResourceStatusPending)
				})
			}, steps: func(f backendProvisioningFixture) []backendStep {
				read := f.originRead(1)
				read.status = 404
				return append(f.roots(), f.groupRead(), f.originRead(0), read,
					backendStep{method: http.MethodPut, suffix: read.suffix, status: 202,
						body: f.originRead(1).body, want: f.graph.Backends.OriginGroups[0].Origins[1].Parameters})
			}},
		{name: "pending approval observed without mutation", want: BackendOriginsPending,
			change: func(f *backendProvisioningFixture) {
				f.changeOrigin(0, func(o *armcdn.AFDOrigin) {
					o.Properties.SharedPrivateLinkResource.Status = ptr.To(armcdn.SharedPrivateLinkResourceStatusPending)
				})
			}},
		{name: "creating origin drift is not resubmitted", want: BackendOriginsPending,
			change: func(f *backendProvisioningFixture) {
				f.changeOrigin(0, func(o *armcdn.AFDOrigin) {
					o.Properties.Weight = ptr.To(int32(999))
					o.Properties.ProvisioningState = ptr.To(armcdn.AfdProvisioningStateCreating)
				})
			}},
		{name: "group need not deploy before origin creation", want: BackendOriginsPending,
			change: func(f *backendProvisioningFixture) {
				f.group.Properties.DeploymentStatus = ptr.To(armcdn.DeploymentStatusNotStarted)
			}, steps: func(f backendProvisioningFixture) []backendStep { return f.mutationSteps("origin create") }},
		{name: "rejection requires manual action, not another write", want: BackendOriginsPending, wantOrigin: PrivateOriginRejected,
			change: func(f *backendProvisioningFixture) {
				f.changeOrigin(0, func(o *armcdn.AFDOrigin) {
					o.Properties.SharedPrivateLinkResource.Status = ptr.To(armcdn.SharedPrivateLinkResourceStatusRejected)
				})
			}},
		{name: "disconnection is not retried as creation", want: BackendOriginsPending, wantOrigin: PrivateOriginDisconnected,
			change: func(f *backendProvisioningFixture) {
				f.changeOrigin(0, func(o *armcdn.AFDOrigin) {
					o.Properties.SharedPrivateLinkResource.Status = ptr.To(armcdn.SharedPrivateLinkResourceStatusDisconnected)
				})
			}},
		{name: "unknown approval stays unready", want: BackendOriginsPending, wantOrigin: PrivateOriginUnknown,
			change: func(f *backendProvisioningFixture) {
				f.changeOrigin(0, func(o *armcdn.AFDOrigin) {
					o.Properties.SharedPrivateLinkResource.Status = nil
				})
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := backendFixture(t)
			if tt.change != nil {
				tt.change(&f)
			}
			steps := f.reads()
			if tt.steps != nil {
				steps = tt.steps(f)
			}
			p, calls := backendProvisioningClient(t, f, steps, nil)
			got, err := p.EnsurePrivateBackends(context.Background(), f.snapshot.owner, f.snapshot.model)
			if err != nil || got.State != tt.want || *calls != len(steps) {
				t.Fatalf("staging result=%#v err=%v calls=%d, want %s/%d", got, err, *calls, tt.want, len(steps))
			}
			if got.State == PrivateBackendsConverged && !got.Origins.OriginsConverged {
				t.Fatal("reported convergence without approved and deployed expected origins")
			}
			if tt.name == "pending approval observed without mutation" && len(got.Origins.PendingApprovals) != 1 {
				t.Fatalf("manual approval diagnostic lost: %#v", got)
			}
			if tt.wantOrigin != "" && got.Origins.Origins[0].State != tt.wantOrigin {
				t.Fatalf("approval state lost: %#v; want %s", got, tt.wantOrigin)
			}
		})
	}
}

func TestBackendProvisionerPreservesSDKFailures(t *testing.T) {
	for _, status := range []int{403, 409, 429} {
		for at := 0; at < 5; at++ {
			f := backendFixture(t)
			steps := f.reads()[:at+1]
			steps[at].status = status
			p, calls := backendProvisioningClient(t, f, steps, nil)
			got, err := p.EnsurePrivateBackends(context.Background(), f.snapshot.owner, f.snapshot.model)
			var responseError *azcore.ResponseError
			if !errors.As(err, &responseError) || responseError.StatusCode != status ||
				!reflect.DeepEqual(got, BackendProvisioningResult{}) || *calls != len(steps) {
				t.Fatalf("SDK failure lost: %#v, %v, calls=%d, status=%d", got, err, *calls, status)
			}
		}
	}
}

func TestBackendProvisionerCancellation(t *testing.T) {
	for cancelAt := 0; cancelAt <= 5; cancelAt++ {
		f := backendFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		steps := f.reads()[:cancelAt]
		p, calls := backendProvisioningClient(t, f, steps, func(at int) {
			if at == cancelAt {
				cancel()
			}
		})
		if cancelAt == 0 {
			cancel()
		}
		got, err := p.EnsurePrivateBackends(ctx, f.snapshot.owner, f.snapshot.model)
		cancel()
		if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(got, BackendProvisioningResult{}) || *calls != cancelAt {
			t.Fatalf("cancellation allowed additional operations/partial success: %#v, %v, calls=%d", got, err, *calls)
		}
	}
}

func (f backendProvisioningFixture) mutationSteps(kind string) []backendStep {
	read := f.groupRead()
	want := interface{}(f.graph.Backends.OriginGroups[0].Parameters)
	prefix := f.roots()
	if strings.HasPrefix(kind, "origin") {
		prefix = append(prefix, read)
		read = f.originRead(0)
		want = f.graph.Backends.OriginGroups[0].Origins[0].Parameters
	}
	method := http.MethodPut
	if strings.HasSuffix(kind, "create") {
		read.status = 404
	} else {
		method = http.MethodPatch
		if kind == "group update" {
			group := read.body.(armcdn.AFDOriginGroup)
			group.Properties.HealthProbeSettings.ProbePath = ptr.To("/drift")
			read.body = group
			want = f.groupUpdateParameters()
		} else {
			origin := read.body.(armcdn.AFDOrigin)
			origin.Properties.Weight = ptr.To(int32(999))
			read.body = origin
		}
	}
	return append(prefix, read, backendStep{method: method, suffix: read.suffix, status: 202, body: read.body, want: want})
}

func TestBackendProvisionerMutationFailuresAndCancellation(t *testing.T) {
	for _, kind := range []string{"group create", "group update", "origin create", "origin update"} {
		t.Run(kind, func(t *testing.T) {
			for _, status := range []int{403, 409, 429} {
				f := backendFixture(t)
				steps := f.mutationSteps(kind)
				steps[len(steps)-1].status = status
				p, calls := backendProvisioningClient(t, f, steps, nil)
				got, err := p.EnsurePrivateBackends(context.Background(), f.snapshot.owner, f.snapshot.model)
				var responseError *azcore.ResponseError
				if !errors.As(err, &responseError) || responseError.StatusCode != status ||
					!reflect.DeepEqual(got, BackendProvisioningResult{}) || *calls != len(steps) {
					t.Fatalf("SDK mutation error lost: %#v, %v, calls=%d", got, err, *calls)
				}
			}
			for _, afterWrite := range []bool{false, true} {
				f := backendFixture(t)
				steps := f.mutationSteps(kind)
				if !afterWrite {
					steps = steps[:len(steps)-1]
				}
				ctx, cancel := context.WithCancel(context.Background())
				p, calls := backendProvisioningClient(t, f, steps, func(at int) {
					if at == len(steps) {
						cancel()
					}
				})
				got, err := p.EnsurePrivateBackends(ctx, f.snapshot.owner, f.snapshot.model)
				cancel()
				if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(got, BackendProvisioningResult{}) || *calls != len(steps) {
					t.Fatalf("cancellation permitted write/partial success: %#v, %v, calls=%d", got, err, *calls)
				}
			}
			for _, status := range []int{201, 202} {
				f := backendFixture(t)
				steps := f.mutationSteps(kind)
				steps[len(steps)-1].status = status
				if status == 201 && strings.HasSuffix(kind, "update") {
					// PATCH supports a synchronous 200, not the create-only 201.
					steps[len(steps)-1].status = http.StatusOK
				}
				p, calls := backendProvisioningClient(t, f, steps, nil)
				got, err := p.EnsurePrivateBackends(context.Background(), f.snapshot.owner, f.snapshot.model)
				if err != nil || got.State == PrivateBackendsConverged || *calls != len(steps) ||
					!reflect.DeepEqual(got.Origins, PrivateOriginsObservation{}) {
					t.Fatalf("submission treated as observed convergence: %#v, %v, calls=%d", got, err, *calls)
				}
			}
		})
	}
}

func TestBackendProvisionerRejectsContradictorySnapshots(t *testing.T) {
	tests := []struct {
		name   string
		change func(*backendProvisioningFixture)
		stopAt int
	}{
		{"foreign group ID", func(f *backendProvisioningFixture) { f.group.ID = ptr.To(changedValue) }, 3},
		{"foreign group name", func(f *backendProvisioningFixture) { f.group.Name = ptr.To(changedValue) }, 3},
		{"missing group properties", func(f *backendProvisioningFixture) { f.group.Properties = nil }, 3},
		{"foreign group parent", func(f *backendProvisioningFixture) { f.group.Properties.ProfileName = ptr.To(changedValue) }, 3},
		{"failed group provisioning", func(f *backendProvisioningFixture) {
			f.group.Properties.ProvisioningState = ptr.To(armcdn.AfdProvisioningStateFailed)
		}, 3},
		{"unknown group provisioning", func(f *backendProvisioningFixture) { f.group.Properties.ProvisioningState = nil }, 3},
		{"failed group deployment", func(f *backendProvisioningFixture) {
			f.group.Properties.DeploymentStatus = ptr.To(armcdn.DeploymentStatusFailed)
		}, 3},
		{"unknown group deployment", func(f *backendProvisioningFixture) { f.group.Properties.DeploymentStatus = nil }, 3},
		{"foreign origin ID", func(f *backendProvisioningFixture) {
			f.changeOrigin(0, func(o *armcdn.AFDOrigin) { o.ID = ptr.To(changedValue) })
		}, 4},
		{"foreign origin parent", func(f *backendProvisioningFixture) {
			f.changeOrigin(0, func(o *armcdn.AFDOrigin) { o.Properties.OriginGroupName = ptr.To(changedValue) })
		}, 4},
		{"missing origin properties", func(f *backendProvisioningFixture) {
			f.changeOrigin(0, func(o *armcdn.AFDOrigin) { o.Properties = nil })
		}, 4},
		{"public origin", func(f *backendProvisioningFixture) {
			f.changeOrigin(0, func(o *armcdn.AFDOrigin) { o.Properties.SharedPrivateLinkResource = nil })
		}, 4},
		{"Azure origin adoption", func(f *backendProvisioningFixture) {
			f.changeOrigin(0, func(o *armcdn.AFDOrigin) {
				o.Properties.AzureOrigin = &armcdn.ResourceReference{ID: ptr.To(changedValue)}
			})
		}, 4},
		{"different PLS", func(f *backendProvisioningFixture) {
			f.changeOrigin(0, func(o *armcdn.AFDOrigin) {
				o.Properties.SharedPrivateLinkResource.PrivateLink.ID = ptr.To(changedValue)
			})
		}, 4},
		{"different Private Link region", func(f *backendProvisioningFixture) {
			f.changeOrigin(0, func(o *armcdn.AFDOrigin) {
				o.Properties.SharedPrivateLinkResource.PrivateLinkLocation = ptr.To(changedValue)
			})
		}, 4},
		{"unexpected group ID", func(f *backendProvisioningFixture) {
			f.changeOrigin(0, func(o *armcdn.AFDOrigin) { o.Properties.SharedPrivateLinkResource.GroupID = ptr.To("sites") })
		}, 4},
		{"failed origin provisioning", func(f *backendProvisioningFixture) {
			f.changeOrigin(0, func(o *armcdn.AFDOrigin) { o.Properties.ProvisioningState = ptr.To(armcdn.AfdProvisioningStateFailed) })
		}, 4},
		{"unknown origin provisioning", func(f *backendProvisioningFixture) {
			f.changeOrigin(0, func(o *armcdn.AFDOrigin) { o.Properties.ProvisioningState = nil })
		}, 4},
		{"failed origin deployment", func(f *backendProvisioningFixture) {
			f.changeOrigin(0, func(o *armcdn.AFDOrigin) { o.Properties.DeploymentStatus = ptr.To(armcdn.DeploymentStatusFailed) })
		}, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := backendFixture(t)
			tt.change(&f)
			steps := f.reads()[:tt.stopAt]
			p, calls := backendProvisioningClient(t, f, steps, nil)
			got, err := p.EnsurePrivateBackends(context.Background(), f.snapshot.owner, f.snapshot.model)
			if err == nil || !reflect.DeepEqual(got, BackendProvisioningResult{}) || *calls != len(steps) {
				t.Fatalf("contradictory snapshot allowed mutation/success: %#v, %v, calls=%d", got, err, *calls)
			}
		})
	}
}

func TestBackendProvisionerRejectsOriginHostOverride(t *testing.T) {
	tests := []struct {
		name        string
		weightDrift bool
	}{
		{name: "Host override only"},
		{name: "Host override and weight drift", weightDrift: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := backendFixture(t)
			f.changeOrigin(0, func(o *armcdn.AFDOrigin) {
				o.Properties.OriginHostHeader = ptr.To("unexpected.example")
				if tt.weightDrift {
					o.Properties.Weight = ptr.To(int32(999))
				}
			})
			steps := f.reads()[:4]
			p, calls := backendProvisioningClient(t, f, steps, nil)
			got, err := p.EnsurePrivateBackends(context.Background(), f.snapshot.owner, f.snapshot.model)
			if err == nil || !strings.Contains(err.Error(), "Host header override") ||
				!reflect.DeepEqual(got, BackendProvisioningResult{}) || *calls != len(steps) {
				t.Fatalf("Host override allowed mutation/success: %#v, %v, calls=%d", got, err, *calls)
			}
		})
	}
}

func TestBackendProvisionerRestartAndNoOp(t *testing.T) {
	f := backendFixture(t)
	group := f.mutationSteps("group create")
	first := f.mutationSteps("origin create")
	pending := backendFixture(t)
	for i := range pending.graph.Backends.OriginGroups[0].Origins {
		pending.changeOrigin(i, func(o *armcdn.AFDOrigin) {
			o.Properties.SharedPrivateLinkResource.Status = ptr.To(armcdn.SharedPrivateLinkResourceStatusPending)
		})
	}
	secondRead := pending.originRead(1)
	secondRead.status = 404
	second := append(pending.roots(), pending.groupRead(), pending.originRead(0), secondRead,
		backendStep{method: http.MethodPut, suffix: secondRead.suffix, status: 202,
			body: pending.originRead(1).body, want: pending.graph.Backends.OriginGroups[0].Origins[1].Parameters})
	steps := append(group, first...)
	steps = append(steps, second...)
	steps = append(steps, pending.reads()...)
	steps = append(steps, f.reads()...)
	steps = append(steps, f.reads()...)
	p, calls := backendProvisioningClient(t, f, steps, nil)
	for _, state := range []BackendProvisioningState{
		BackendGroupPending, BackendOriginsPending, BackendOriginsPending, BackendOriginsPending,
		PrivateBackendsConverged, PrivateBackendsConverged,
	} {
		// No operation result/poller is retained by a replacement provisioner.
		p = &BackendProvisioner{roots: p.roots, groups: p.groups, origins: p.origins}
		got, err := p.EnsurePrivateBackends(context.Background(), f.snapshot.owner, f.snapshot.model)
		if err != nil || got.State != state {
			t.Fatalf("restart failed to resume from observations: %#v, %v; want %s", got, err, state)
		}
	}
	if *calls != len(steps) {
		t.Fatalf("unexpected duplicate/missing SDK operations: calls=%d want=%d", *calls, len(steps))
	}
}

func TestBackendProvisionerPreflight(t *testing.T) {
	for _, kind := range []string{"wrong owner", "empty graph", "unsupported graph", "missing clients", "expired deadline"} {
		t.Run(kind, func(t *testing.T) {
			f := backendFixture(t)
			p, _ := backendProvisioningClient(t, f, nil, nil)
			ctx := context.Background()
			switch kind {
			case "wrong owner":
				f.snapshot.owner.UID = changedValue
			case "empty graph":
				f.snapshot.model.Routes = nil
			case "unsupported graph":
				f.snapshot.model.Listeners[0].Hostname = "restricted.example.com"
			case "missing clients":
				p = &BackendProvisioner{}
			case "expired deadline":
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer cancel()
			}
			got, err := p.EnsurePrivateBackends(ctx, f.snapshot.owner, f.snapshot.model)
			if err == nil || !reflect.DeepEqual(got, BackendProvisioningResult{}) {
				t.Fatalf("invalid preflight reached ARM: %#v, %v", got, err)
			}
		})
	}
}
