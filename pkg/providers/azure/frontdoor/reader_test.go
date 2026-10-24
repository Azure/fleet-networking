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

func TestOriginObserverRead(t *testing.T) {
	tests := []struct {
		name          string
		profileStatus int
		listStatus    int
		secondStatus  int
		foreign       bool
		duplicate     bool
		malformed     bool
		missingName   bool
		wrongID       bool
		wantCalls     int
		wantMissing   bool
		wantReady     bool
		wantError     bool
	}{
		{name: "paginated origins converge", wantCalls: 3, wantReady: true},
		{name: "missing root", profileStatus: 404, wantCalls: 1, wantMissing: true},
		{name: "foreign parent forbids child reads", foreign: true, wantCalls: 1, wantError: true},
		{name: "profile forbidden", profileStatus: 403, wantCalls: 1, wantError: true},
		{name: "profile throttled", profileStatus: 429, wantCalls: 1, wantError: true},
		{name: "missing group", listStatus: 404, wantCalls: 2},
		{name: "group disappears mid-pagination", secondStatus: 404, wantCalls: 3},
		{name: "list forbidden", listStatus: 403, wantCalls: 2, wantError: true},
		{name: "list conflict", listStatus: 409, wantCalls: 2, wantError: true},
		{name: "list throttled", listStatus: 429, wantCalls: 2, wantError: true},
		{name: "later page fails", secondStatus: 403, wantCalls: 3, wantError: true},
		{name: "duplicate origin across pages", duplicate: true, wantCalls: 3, wantError: true},
		{name: "nil listed origin", malformed: true, wantCalls: 2, wantError: true},
		{name: "missing origin name", missingName: true, wantCalls: 2, wantError: true},
		{name: "foreign child ID", wrongID: true, wantCalls: 2, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := originSnapshot(t, backendModel())
			roots, err := BuildRootRequests(snapshot.scope, snapshot.owner)
			if err != nil {
				t.Fatal(err)
			}
			desired, err := BuildBackendRequests(snapshot.model)
			if err != nil {
				t.Fatal(err)
			}
			group := desired.OriginGroups[0]
			first := snapshot.origins[snapshot.first]
			second := snapshot.origins[OriginReference{OriginGroup: group.Name, Origin: group.Origins[1].Name}]
			if tt.foreign {
				snapshot.profile.Tags["networking.fleet.azure.com.hub-uid"] = ptr.To(changedValue)
			}
			if tt.missingName {
				first.Name = nil
			}
			if tt.wrongID {
				first.ID = ptr.To(changedValue)
			}
			if tt.duplicate {
				second = first
			}
			listPath := roots.ProfileID + "/originGroups/" + group.Name + "/origins"
			calls := 0
			transport := cleanupTransport(func(request *http.Request) (*http.Response, error) {
				calls++
				if request.Method != http.MethodGet || request.URL.Query().Get("api-version") != testAPIVersion {
					t.Fatalf("unexpected SDK operation: %s %s", request.Method, request.URL)
				}
				deadline, ok := request.Context().Deadline()
				if !ok || time.Until(deadline) > 31*time.Second {
					t.Fatal("origin observation has no bounded deadline")
				}
				status, path := tt.profileStatus, roots.ProfileID
				var body interface{} = snapshot.profile
				switch calls {
				case 1:
				case 2:
					status, path = tt.listStatus, listPath
					value := []*armcdn.AFDOrigin{&first}
					if tt.malformed {
						value = []*armcdn.AFDOrigin{nil}
					}
					body = armcdn.AFDOriginListResult{
						Value: value,
						NextLink: ptr.To("https://management.azure.com" + listPath +
							"?api-version=" + testAPIVersion + "&page=2"),
					}
				case 3:
					status, path = tt.secondStatus, listPath
					if request.URL.Query().Get("page") != "2" {
						t.Fatal("SDK did not follow the next-page link")
					}
					body = armcdn.AFDOriginListResult{Value: []*armcdn.AFDOrigin{&second}}
				default:
					t.Fatal("unexpected extra request or in-memory polling")
				}
				if request.URL.Path != path {
					t.Fatalf("SDK path=%q, want %q", request.URL.Path, path)
				}
				if status == 0 {
					status = http.StatusOK
				}
				if status >= http.StatusBadRequest {
					body = map[string]interface{}{"error": map[string]string{"code": "UnitTestError", "message": "request failed"}}
				}
				data, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				return &http.Response{
					StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}},
					Body: io.NopCloser(strings.NewReader(string(data))), Request: request,
				}, nil
			})
			reader, err := NewOriginObserver(snapshot.scope, cleanupCredential{}, &arm.ClientOptions{
				ClientOptions: policy.ClientOptions{Transport: transport, Retry: policy.RetryOptions{MaxRetries: -1}},
			})
			if err != nil {
				t.Fatal(err)
			}
			got, err := reader.Observe(context.Background(), snapshot.owner, snapshot.model)
			if (err != nil) != tt.wantError || calls != tt.wantCalls ||
				got.ProfileMissing != tt.wantMissing || got.OriginsConverged != tt.wantReady {
				t.Fatalf("observation=%#v error=%v calls=%d", got, err, calls)
			}
			if tt.wantError && !reflect.DeepEqual(got, PrivateOriginsObservation{}) {
				t.Fatalf("error returned a partial observation: %#v", got)
			}
			if tt.foreign {
				var conflict *OwnershipConflictError
				if !errors.As(err, &conflict) {
					t.Fatalf("lost ownership conflict: %v", err)
				}
			}
			assertOriginReadFailure(t, got, err, tt.listStatus, tt.secondStatus)
		})
	}
}

func assertOriginReadFailure(t *testing.T, got PrivateOriginsObservation, err error, firstStatus, secondStatus int) {
	t.Helper()
	for _, status := range []int{firstStatus, secondStatus} {
		switch status {
		case http.StatusNotFound:
			if len(got.Origins) != 2 || got.Origins[0].State != PrivateOriginMissing || got.Origins[1].State != PrivateOriginMissing {
				t.Fatalf("missing group retained partial observations: %#v", got)
			}
		case http.StatusForbidden, http.StatusConflict, http.StatusTooManyRequests:
			var responseError *azcore.ResponseError
			if !errors.As(err, &responseError) || responseError.StatusCode != status {
				t.Fatalf("lost SDK error status %d: %v", status, err)
			}
		}
	}
}

func TestOriginObserverPreflightAndCancellation(t *testing.T) {
	tests := []struct {
		name   string
		change func(*privateOriginSnapshot)
		cancel bool
		noSDK  bool
	}{
		{"wrong Gateway", func(s *privateOriginSnapshot) { s.model.UID = changedValue }, false, false},
		{"missing hub identity", func(s *privateOriginSnapshot) { s.owner.HubUID = "" }, false, false},
		{"invalid backend", func(s *privateOriginSnapshot) { s.model.Routes[0].Rules[0].Backends[0].Port = 0 }, false, false},
		{"uninitialized SDK", nil, false, true},
		{"canceled observation", nil, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := originSnapshot(t, backendModel())
			if tt.change != nil {
				tt.change(&snapshot)
			}
			transport := cleanupTransport(func(*http.Request) (*http.Response, error) {
				t.Fatal("invalid or canceled observation reached ARM")
				return nil, errors.New("unexpected request")
			})
			reader, err := NewOriginObserver(snapshot.scope, cleanupCredential{}, &arm.ClientOptions{
				ClientOptions: policy.ClientOptions{Transport: transport},
			})
			if err != nil {
				t.Fatal(err)
			}
			if tt.noSDK {
				reader = &OriginObserver{scope: snapshot.scope}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancel {
				cancel()
			}
			got, err := reader.Observe(ctx, snapshot.owner, snapshot.model)
			if err == nil || !reflect.DeepEqual(got, PrivateOriginsObservation{}) {
				t.Fatalf("invalid observation did not fail closed: %#v, %v", got, err)
			}
			if tt.cancel && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
			}
		})
	}
}

func TestOriginObserverOperationBudget(t *testing.T) {
	tests := []struct {
		name        string
		cancelAfter int
		expired     bool
	}{
		{name: "caller deadline already exceeded", expired: true},
		{name: "canceled after profile observation", cancelAfter: 1},
		{name: "canceled between origin pages", cancelAfter: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := originSnapshot(t, backendModel())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.expired {
				var deadlineCancel context.CancelFunc
				ctx, deadlineCancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer deadlineCancel()
			}
			calls := 0
			var operationDeadline time.Time
			transport := cleanupTransport(func(request *http.Request) (*http.Response, error) {
				calls++
				deadline, ok := request.Context().Deadline()
				if !ok {
					t.Fatal("SDK request has no deadline")
				}
				if calls == 1 {
					operationDeadline = deadline
				} else if !operationDeadline.Equal(deadline) {
					t.Fatal("pagination restarted the operation timeout")
				}
				var body interface{} = snapshot.profile
				if calls > 1 {
					body = armcdn.AFDOriginListResult{
						Value:    []*armcdn.AFDOrigin{ptr.To(snapshot.origins[snapshot.first])},
						NextLink: ptr.To(request.URL.String() + "&page=2"),
					}
				}
				if calls >= tt.cancelAfter {
					cancel()
				}
				data, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				return &http.Response{
					StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}},
					Body: io.NopCloser(strings.NewReader(string(data))), Request: request,
				}, nil
			})
			reader, err := NewOriginObserver(snapshot.scope, cleanupCredential{}, &arm.ClientOptions{
				ClientOptions: policy.ClientOptions{Transport: transport, Retry: policy.RetryOptions{MaxRetries: -1}},
			})
			if err != nil {
				t.Fatal(err)
			}
			got, err := reader.Observe(ctx, snapshot.owner, snapshot.model)
			wantError := context.Canceled
			if tt.expired {
				wantError = context.DeadlineExceeded
			}
			if !errors.Is(err, wantError) || !reflect.DeepEqual(got, PrivateOriginsObservation{}) ||
				calls != tt.cancelAfter {
				t.Fatalf("observation=%#v error=%v calls=%d, want %v/%d", got, err, calls, wantError, tt.cancelAfter)
			}
		})
	}
}

func TestOriginObserverRepeatedRead(t *testing.T) {
	model := backendModel()
	appendBackend(&model).Name = otherName
	snapshot := originSnapshot(t, model)
	approved := false
	calls := 0
	transport := cleanupTransport(func(request *http.Request) (*http.Response, error) {
		calls++
		var body interface{} = snapshot.profile
		if strings.HasSuffix(request.URL.Path, "/origins") {
			var values []*armcdn.AFDOrigin
			for ref, origin := range snapshot.origins {
				if !strings.HasSuffix(request.URL.Path, "/originGroups/"+ref.OriginGroup+"/origins") {
					continue
				}
				status := armcdn.SharedPrivateLinkResourceStatusPending
				if approved {
					status = armcdn.SharedPrivateLinkResourceStatusApproved
				}
				origin.Properties.SharedPrivateLinkResource.Status = ptr.To(status)
				values = append(values, ptr.To(origin))
			}
			body = armcdn.AFDOriginListResult{Value: values}
		}
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(string(data))), Request: request,
		}, nil
	})
	reader, err := NewOriginObserver(snapshot.scope, cleanupCredential{}, &arm.ClientOptions{
		ClientOptions: policy.ClientOptions{Transport: transport},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, ready := range []bool{false, true} {
		approved = ready
		got, err := reader.Observe(context.Background(), snapshot.owner, model)
		if err != nil || got.OriginsConverged != ready || len(got.Origins) != 4 {
			t.Fatalf("fresh observation did not reflect approval=%v: %#v, %v", ready, got, err)
		}
	}
	if calls != 6 {
		t.Fatalf("observations did not reread the profile and both groups: calls=%d", calls)
	}
}
