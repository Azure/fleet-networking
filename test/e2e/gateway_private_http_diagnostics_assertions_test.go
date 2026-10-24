//go:build e2e

/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"bytes"
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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"go.goms.io/fleet-networking/pkg/providers/azure/frontdoor"
)

type privateHTTPDiagnosticCredential struct{}

func (privateHTTPDiagnosticCredential) GetToken(context.Context, policy.TokenRequestOptions) (azcore.AccessToken, error) {
	return azcore.AccessToken{Token: "offline-secret-token", ExpiresOn: time.Now().Add(time.Hour)}, nil
}

func TestPrivateHTTPDiagnosticCaptureLimit(t *testing.T) {
	id := "/subscriptions/test-subscription/resourceGroups/test-rg/providers/Microsoft.Cdn/profiles/profile/originGroups/group/origins/origin"
	snapshot := privateHTTPDiagnostics{}
	capture := &privateHTTPDiagnosticCapture{snapshot: &snapshot, paths: map[string]map[string]bool{strings.ToLower(id): {strings.ToLower(id): true}}}
	body := `{"id":"` + id + `","name":"origin","properties":{"sharedPrivateLinkResource":{"status":"Pending"},"unknown":"` +
		strings.Repeat("x", privateHTTPDiagnosticMaxBody) + `"}}`
	transport := privateHTTPDiagnosticTransport(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})
	client, err := armcdn.NewAFDOriginsClient("test-subscription", privateHTTPDiagnosticCredential{},
		&arm.ClientOptions{ClientOptions: policy.ClientOptions{
			Transport: transport, PerRetryPolicies: []policy.Policy{capture},
		}})
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Get(context.Background(), "test-rg", "profile", "group", "origin", nil)
	if err != nil || response.Properties == nil || response.Properties.SharedPrivateLinkResource == nil ||
		ptr.Deref(response.Properties.SharedPrivateLinkResource.Status, "") != armcdn.SharedPrivateLinkResourceStatusPending {
		t.Fatalf("capture changed SDK body consumption: %v", err)
	}
	if len(snapshot.HTTP) != 1 || snapshot.HTTP[0].Error == "" || len(capture.errors) == 0 || snapshot.HTTP[0].Raw != nil {
		t.Fatalf("oversized capture was not explicitly incomplete: %+v", snapshot)
	}
}

func TestPrivateHTTPDiagnosticBodyReplay(t *testing.T) {
	source := io.NopCloser(strings.NewReader("tail"))
	body := &privateHTTPDiagnosticBody{prefix: bytes.NewReader([]byte("prefix")), source: source}
	data, err := io.ReadAll(body)
	if err != nil || string(data) != "prefixtail" {
		t.Fatalf("captured prefix changed the SDK body: %s %v", data, err)
	}
	readErr := errors.New("captured read failed")
	body = &privateHTTPDiagnosticBody{prefix: bytes.NewReader([]byte("prefix")), source: io.NopCloser(strings.NewReader("")), readErr: readErr}
	data, err = io.ReadAll(body)
	if !errors.Is(err, readErr) || string(data) != "prefix" {
		t.Fatalf("capture lost the original read failure: %s %v", data, err)
	}
}

func TestPrivateHTTPDiagnosticForeignCollectionExcluded(t *testing.T) {
	fields, err := projectPrivateHTTPDiagnosticBody([]byte(`{"value":[{"id":"/expected","name":"owned"},{"id":"/foreign","name":"payload-secret"}]}`),
		map[string]bool{"/expected": true}, "owned request")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "payload-secret") || !strings.Contains(string(data), `"valueCount":2`) {
		t.Fatalf("foreign data leaked or total count was hidden: %s", data)
	}
}

func TestPrivateHTTPControllerDiagnosticRedaction(t *testing.T) {
	origin := privateHTTPDiagnosticOrigin{OriginID: "/expected-origin", PLSID: "/expected-pls"}
	detail := "RESPONSE 403: Forbidden ERROR CODE: LinkedAuthorizationFailed Microsoft.Network/privateLinkServices/read /expected-pls Authorization: Bearer payload-secret"
	data, err := json.Marshal(map[string]interface{}{"namespace": "proof", "name": "gateway", "controller": "gateway",
		"error": detail, "credential": "payload-secret"})
	if err != nil {
		t.Fatal(err)
	}
	records := projectPrivateHTTPControllerDiagnostics("2026-10-30T18:03:34Z\tERROR\tReconciler error\t"+string(data), "proof", "gateway",
		[]privateHTTPDiagnosticOrigin{origin})
	data, err = json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	for _, evidence := range []string{"LinkedAuthorizationFailed", "403", "Microsoft.Network/privateLinkServices/read", "/expected-pls"} {
		if !strings.Contains(string(data), evidence) {
			t.Errorf("controller diagnostics omit %q: %s", evidence, data)
		}
	}
	if strings.Contains(string(data), "payload-secret") {
		t.Fatal("controller diagnostics emitted a raw error body")
	}
	if records := projectPrivateHTTPControllerDiagnostics(string(data), "foreign", "gateway", nil); len(records) != 0 {
		t.Fatal("controller diagnostics included another namespace")
	}
}

type privateHTTPDiagnosticTransport func(*http.Request) (*http.Response, error)

func (transport privateHTTPDiagnosticTransport) Do(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestPrivateHTTPDiagnosticFieldShapes(t *testing.T) {
	tests := []struct {
		name, properties string
		wantStatus       interface{}
		present          bool
	}{
		{"omitted", `{"sharedPrivateLinkResource":{}}`, nil, false},
		{"null", `{"sharedPrivateLinkResource":{"status":null}}`, nil, true},
		{"unknown", `{"sharedPrivateLinkResource":{"status":"ServerNewState"}}`, "ServerNewState", true},
		{"pending", `{"sharedPrivateLinkResource":{"status":"Pending"}}`, "Pending", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(`{"id":"/expected","properties":` + tt.properties + `,"credential":"payload-secret"}`)
			fields, err := projectPrivateHTTPDiagnosticBody(body, map[string]bool{"/expected": true}, "owned request")
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			var decoded struct {
				Properties struct {
					Link map[string]interface{} `json:"sharedPrivateLinkResource"`
				} `json:"properties"`
			}
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatal(err)
			}
			value, present := decoded.Properties.Link["status"]
			if present != tt.present || value != tt.wantStatus || strings.Contains(string(data), "payload-secret") {
				t.Fatalf("status=%v present=%t, projected body=%s", value, present, data)
			}
		})
	}
}

func TestPrivateHTTPDiagnosticCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	scope := frontdoor.ResourceScope{SubscriptionID: "test-subscription", ResourceGroup: "test-rg"}
	owner := frontdoor.GatewayOwner{HubUID: "hub-uid", Namespace: "proof", Name: "gateway", UID: "gateway-uid"}
	roots, err := frontdoor.BuildRootRequests(scope, owner)
	if err != nil {
		t.Fatal(err)
	}
	plan := frontdoor.BackendPlan{ProfileName: roots.ProfileName, OriginGroups: []frontdoor.OriginGroup{{Name: "group"}}}
	transport := privateHTTPDiagnosticTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("canceled diagnostics reached ARM")
		return nil, errors.New("unexpected ARM request")
	})
	snapshot, err := collectPrivateHTTPDiagnostics(ctx, scope, owner, plan, privateHTTPDiagnosticCredential{},
		&arm.ClientOptions{ClientOptions: policy.ClientOptions{Transport: transport}})
	if !errors.Is(err, context.Canceled) || snapshot.Complete || len(snapshot.Issues) == 0 {
		t.Fatalf("cancellation was hidden: %+v %v", snapshot, err)
	}
}

func TestPrivateHTTPDiagnosticControllerPodOwnership(t *testing.T) {
	tests := []struct {
		name      string
		change    func(*appsv1.Deployment, *appsv1.ReplicaSet, *corev1.Pod)
		wantCount int
		wantErr   bool
	}{
		{name: "owned", wantCount: 1},
		{name: "foreign deployment", change: func(_ *appsv1.Deployment, rs *appsv1.ReplicaSet, _ *corev1.Pod) {
			rs.OwnerReferences[0].UID = "foreign"
		}},
		{name: "changed ReplicaSet identity", change: func(_ *appsv1.Deployment, _ *appsv1.ReplicaSet, pod *corev1.Pod) {
			pod.OwnerReferences[0].UID = "foreign"
		}, wantErr: true},
		{name: "no explicit selector", change: func(deployment *appsv1.Deployment, _ *appsv1.ReplicaSet, _ *corev1.Pod) {
			deployment.Spec.Selector = nil
		}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := corev1.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			if err := appsv1.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			deployment := appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: "proof", Name: "controller", UID: types.UID("deployment-uid")},
				Spec: appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "controller"}}}}
			rs := appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Namespace: "proof", Name: "controller-rs", UID: types.UID("rs-uid"),
				OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: deployment.Name, UID: deployment.UID, Controller: ptr.To(true)}}}}
			pod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "proof", Name: "controller-pod", Labels: map[string]string{"app": "controller"},
				OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID, Controller: ptr.To(true)}}}}
			if tt.change != nil {
				tt.change(&deployment, &rs, &pod)
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&deployment, &rs, &pod).Build()
			pods, err := privateHTTPControllerPods(context.Background(), c, &deployment)
			if (err != nil) != tt.wantErr || len(pods) != tt.wantCount {
				t.Fatalf("ownership result=%v err=%v; want count=%d err=%t", pods, err, tt.wantCount, tt.wantErr)
			}
		})
	}
}

func assertPrivateHTTPDiagnosticEvidence(t *testing.T, snapshot privateHTTPDiagnostics, wantNullStatus bool) string {
	t.Helper()
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	evidence := []string{"origin-member-1", "origin-member-2", "privateEndpointConnections", "request-id",
		"2024-02-01", `"status":"Pending"`, `"sdk"`}
	if wantNullStatus {
		evidence = append(evidence, `"status":null`)
	}
	for _, evidence := range evidence {
		if !strings.Contains(text, evidence) {
			t.Errorf("diagnostics omit %q: %s", evidence, text)
		}
	}
	for _, secret := range []string{"offline-secret-token", "response-secret-token", "payload-secret"} {
		if strings.Contains(text, secret) {
			t.Errorf("diagnostic output leaked %q", secret)
		}
	}
	return text
}

func TestPrivateHTTPDiagnosticAllMembers(t *testing.T) {
	tests := []struct {
		name                                         string
		failFirstGET, unsafeNextLink, foreignProfile bool
		wantCalls                                    int
	}{
		{name: "LIST GET disagreement", wantCalls: 7},
		{name: "first GET denied", failFirstGET: true, wantCalls: 7},
		{name: "foreign pagination is never requested", unsafeNextLink: true, wantCalls: 6},
		{name: "foreign profile blocks origin reads", foreignProfile: true, wantCalls: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scope := frontdoor.ResourceScope{SubscriptionID: "test-subscription", ResourceGroup: "test-rg"}
			owner := frontdoor.GatewayOwner{HubUID: "hub-uid", Namespace: "proof", Name: "gateway", UID: "gateway-uid"}
			roots, err := frontdoor.BuildRootRequests(scope, owner)
			if err != nil {
				t.Fatal(err)
			}
			roots.Profile.ID = ptr.To(roots.ProfileID)
			if tt.foreignProfile {
				roots.Profile.Tags["networking.fleet.azure.com.hub-uid"] = ptr.To("foreign-hub")
			}
			profile, err := json.Marshal(roots.Profile)
			if err != nil {
				t.Fatal(err)
			}
			plan := frontdoor.BackendPlan{ProfileName: roots.ProfileName, OriginGroups: []frontdoor.OriginGroup{{Name: "owned-group"}}}
			originBodies, plsBodies := make(map[string]string), make(map[string]string)
			for _, member := range []string{"member-1", "member-2"} {
				target := "/subscriptions/test-subscription/resourceGroups/" + member + "/providers/Microsoft.Network/privateLinkServices/proof"
				origin := frontdoor.Origin{Name: "origin-" + member, Cluster: member,
					PrivateLink: frontdoor.PrivateLinkTarget{ResourceID: target, Location: "swedencentral"}}
				plan.OriginGroups[0].Origins = append(plan.OriginGroups[0].Origins, origin)
				plan.PrivateLinks = append(plan.PrivateLinks, frontdoor.PrivateLinkPlan{Target: origin.PrivateLink})
				id := roots.ProfileID + "/originGroups/owned-group/origins/" + origin.Name
				originBodies[id] = `{"id":"` + id + `","name":"` + origin.Name + `","properties":{"provisioningState":"Succeeded","deploymentStatus":"NotStarted","sharedPrivateLinkResource":{"privateLink":{"id":"` + target + `"},"status":"Pending"},"credential":"payload-secret"}}`
				plsBodies[target] = `{"id":"` + target + `","location":"swedencentral","properties":{"provisioningState":"Succeeded","privateEndpointConnections":[{"id":"` + target + `/privateEndpointConnections/connection","name":"connection","properties":{"privateLinkServiceConnectionState":{"status":"Pending","description":"Fleet AFD private origin for ` + roots.ProfileName + `"}}}]}}`
			}
			var calls []string
			transport := privateHTTPDiagnosticTransport(func(request *http.Request) (*http.Response, error) {
				if request.Method != http.MethodGet {
					t.Fatalf("diagnostics attempted a write: %s", request.Method)
				}
				if request.URL.Host != "management.azure.com" {
					t.Fatal("foreign diagnostic pagination reached the transport")
				}
				if deadline, ok := request.Context().Deadline(); !ok || time.Until(deadline) > privateHTTPDiagnosticTimeout+time.Second {
					t.Fatal("unbounded diagnostic read")
				}
				calls = append(calls, request.URL.Path)
				body, status := string(profile), http.StatusOK
				listPath := roots.ProfileID + "/originGroups/owned-group/origins"
				switch {
				case request.URL.Path == roots.ProfileID:
				case request.URL.Path == listPath:
					member := "member-1"
					next := `,"nextLink":"https://management.azure.com` + listPath + `?api-version=2024-02-01&page=2"`
					if tt.unsafeNextLink {
						next = `,"nextLink":"https://example.invalid` + listPath + `?api-version=2024-02-01&page=2"`
					}
					if request.URL.Query().Get("page") == "2" {
						member, next = "member-2", ""
					}
					id := listPath + "/origin-" + member
					value := strings.Replace(originBodies[id], `"status":"Pending"`, `"status":null`, 1)
					body = `{"value":[` + value + `]` + next + `}`
				case originBodies[request.URL.Path] != "":
					body = originBodies[request.URL.Path]
					if tt.failFirstGET && strings.HasSuffix(request.URL.Path, "origin-member-1") {
						status, body = http.StatusForbidden, `{"error":{"code":"LinkedAuthorizationFailed","message":"payload-secret"}}`
					}
				case plsBodies[request.URL.Path] != "":
					body = plsBodies[request.URL.Path]
				default:
					t.Fatalf("unexpected diagnostic path: %s", request.URL)
				}
				return &http.Response{StatusCode: status,
					Header: http.Header{"Content-Type": {"application/json"}, "X-Ms-Request-Id": {"request-id"},
						"Authorization": {"response-secret-token"}},
					Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
			})
			snapshot, err := collectPrivateHTTPDiagnostics(context.Background(), scope, owner, plan,
				privateHTTPDiagnosticCredential{}, &arm.ClientOptions{ClientOptions: policy.ClientOptions{Transport: transport}})
			wantError := tt.failFirstGET || tt.unsafeNextLink || tt.foreignProfile
			if (err != nil) != wantError || len(calls) != tt.wantCalls || snapshot.Complete == wantError {
				t.Fatalf("capture error=%v, calls=%v", err, calls)
			}
			text := assertPrivateHTTPDiagnosticEvidence(t, snapshot, !tt.foreignProfile)
			if tt.failFirstGET {
				var responseError *azcore.ResponseError
				if !errors.As(err, &responseError) || responseError.StatusCode != http.StatusForbidden ||
					!strings.Contains(text, "LinkedAuthorizationFailed") {
					t.Fatalf("diagnostic failure lost original SDK status: %v", err)
				}
			}
		})
	}
}
