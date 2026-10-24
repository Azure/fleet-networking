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
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	"k8s.io/utils/ptr"
)

type cleanupTransport func(*http.Request) (*http.Response, error)

func (f cleanupTransport) Do(request *http.Request) (*http.Response, error) {
	return f(request)
}

type cleanupCredential struct{}

func (cleanupCredential) GetToken(context.Context, policy.TokenRequestOptions) (azcore.AccessToken, error) {
	return azcore.AccessToken{Token: "unit-test-token", ExpiresOn: time.Now().Add(time.Hour)}, nil
}

func TestProfileCleanupDeleteProfile(t *testing.T) {
	tests := []struct {
		name         string
		getStatus    int
		deleteStatus int
		change       func(*armcdn.Profile)
		wantCalls    int
		deleted      bool
		wantError    bool
		collision    bool
	}{
		{name: "absent root", getStatus: 404, wantCalls: 1, deleted: true},
		{name: "accepted deletion is not completion", getStatus: 200, deleteStatus: 202, wantCalls: 2},
		{name: "completed submission still requires observation", getStatus: 200, deleteStatus: 204, wantCalls: 2},
		{name: "concurrent deletion already gone", getStatus: 200, deleteStatus: 404, wantCalls: 2, deleted: true},
		{name: "restart during deletion", getStatus: 200, wantCalls: 1,
			change: func(p *armcdn.Profile) {
				p.Properties = &armcdn.ProfileProperties{ResourceState: ptr.To(armcdn.ProfileResourceStateDeleting)}
			}},
		{name: "foreign owner", getStatus: 200, wantCalls: 1, wantError: true, collision: true,
			change: func(p *armcdn.Profile) { p.Tags["networking.fleet.azure.com.hub-uid"] = ptr.To("another-hub") }},
		{name: "ambiguous response", getStatus: 200, wantCalls: 1, wantError: true, collision: true,
			change: func(p *armcdn.Profile) { p.ID = nil }},
		{name: "GET forbidden", getStatus: 403, wantCalls: 1, wantError: true},
		{name: "GET throttled", getStatus: 429, wantCalls: 1, wantError: true},
		{name: "DELETE forbidden", getStatus: 200, deleteStatus: 403, wantCalls: 2, wantError: true},
		{name: "DELETE conflict", getStatus: 200, deleteStatus: 409, wantCalls: 2, wantError: true},
		{name: "DELETE throttled", getStatus: 200, deleteStatus: 429, wantCalls: 2, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scope, owner := ownershipFixture()
			requests, err := BuildRootRequests(scope, owner)
			if err != nil {
				t.Fatal(err)
			}
			profile := requests.Profile
			profile.ID, profile.Name = ptr.To(requests.ProfileID), ptr.To(requests.ProfileName)
			if tt.change != nil {
				tt.change(&profile)
			}
			data, err := json.Marshal(profile)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			transport := cleanupTransport(func(request *http.Request) (*http.Response, error) {
				calls++
				if request.URL.Path != requests.ProfileID || request.URL.Query().Get("api-version") != testAPIVersion {
					t.Fatalf("unexpected SDK request: %s", request.URL)
				}
				deadline, ok := request.Context().Deadline()
				if !ok || time.Until(deadline) > 31*time.Second {
					t.Fatal("Azure cleanup request has no bounded deadline")
				}
				status, body := tt.getStatus, string(data)
				switch calls {
				case 1:
					if request.Method != http.MethodGet {
						t.Fatalf("first operation must observe ownership, got %s", request.Method)
					}
				case 2:
					if request.Method != http.MethodDelete {
						t.Fatalf("second operation must delete only the root, got %s", request.Method)
					}
					status, body = tt.deleteStatus, ""
				default:
					t.Fatalf("unexpected poll or repeated mutation: %s", request.Method)
				}
				if status >= 400 {
					body = `{"error":{"code":"UnitTestError","message":"request failed"}}`
				}
				header := http.Header{"Content-Type": []string{"application/json"}}
				if status == http.StatusAccepted {
					header.Set("Azure-AsyncOperation", "https://management.azure.com/operations/deleting?api-version="+testAPIVersion)
				}
				return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
			})
			cleanup, err := NewProfileCleanup(scope, cleanupCredential{}, &arm.ClientOptions{
				ClientOptions: policy.ClientOptions{Transport: transport, Retry: policy.RetryOptions{MaxRetries: -1}},
			})
			if err != nil {
				t.Fatal(err)
			}
			deleted, err := cleanup.DeleteProfile(context.Background(), owner)
			if deleted != tt.deleted || (err != nil) != tt.wantError || calls != tt.wantCalls {
				t.Fatalf("deleted=%v err=%v calls=%d; want %v/error=%v/%d", deleted, err, calls, tt.deleted, tt.wantError, tt.wantCalls)
			}
			if tt.collision {
				var collision *OwnershipConflictError
				if !errors.As(err, &collision) {
					t.Fatalf("lost ownership refusal: %v", err)
				}
			}
		})
	}
}

func TestProfileCleanupRequiresClientAndValidOwner(t *testing.T) {
	scope, owner := ownershipFixture()
	for _, invalidOwner := range []bool{false, true} {
		t.Run(map[bool]string{false: "uninitialized client", true: "missing hub identity"}[invalidOwner], func(t *testing.T) {
			if invalidOwner {
				owner.HubUID = ""
			}
			deleted, err := (&ProfileCleanup{scope: scope}).DeleteProfile(context.Background(), owner)
			if err == nil || deleted {
				t.Fatalf("invalid cleanup configuration succeeded: deleted=%v, err=%v", deleted, err)
			}
		})
	}
}
