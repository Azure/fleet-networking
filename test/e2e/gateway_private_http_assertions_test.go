//go:build e2e

/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v4"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
	"go.goms.io/fleet-networking/pkg/providers/azure/frontdoor"
	"go.goms.io/fleet-networking/test/e2e/framework"
)

const privateHTTPTestEdgeReference = "edge-reference"

func TestPrivateHTTPApprovalOrigins(t *testing.T) {
	tests := []struct {
		name     string
		states   [][]frontdoor.PrivateOriginState
		wantErr  []bool
		expected int
	}{
		{"first unknown permits PLS correlation", [][]frontdoor.PrivateOriginState{{frontdoor.PrivateOriginUnknown, frontdoor.PrivateOriginApprovalPending}}, []bool{false}, 2},
		{"second unknown permits PLS correlation", [][]frontdoor.PrivateOriginState{{frontdoor.PrivateOriginApprovalPending, frontdoor.PrivateOriginUnknown}}, []bool{false}, 2},
		{"multiple blockers", [][]frontdoor.PrivateOriginState{{frontdoor.PrivateOriginMissing, frontdoor.PrivateOriginFailed}}, []bool{true}, 2},
		{"missing member", [][]frontdoor.PrivateOriginState{{frontdoor.PrivateOriginApprovalPending}}, []bool{true}, 2},
		{"unexpected count", [][]frontdoor.PrivateOriginState{{frontdoor.PrivateOriginApprovalPending, frontdoor.PrivateOriginApprovalPending}}, []bool{true}, 1},
		{"empty observation", [][]frontdoor.PrivateOriginState{{}}, []bool{true}, 2},
		{"empty expected set", [][]frontdoor.PrivateOriginState{{}}, []bool{true}, 0},
		{"early approval permits PLS correlation", [][]frontdoor.PrivateOriginState{{frontdoor.PrivateOriginApprovalPending, frontdoor.PrivateOriginReady}}, []bool{false}, 2},
		{"both null statuses permit PLS correlation", [][]frontdoor.PrivateOriginState{{frontdoor.PrivateOriginUnknown, frontdoor.PrivateOriginUnknown}}, []bool{false}, 2},
		{"drift cannot reach approval", [][]frontdoor.PrivateOriginState{{frontdoor.PrivateOriginDrifted, frontdoor.PrivateOriginUnknown}}, []bool{true}, 2},
		{"rejected cannot reach approval", [][]frontdoor.PrivateOriginState{{frontdoor.PrivateOriginRejected, frontdoor.PrivateOriginUnknown}}, []bool{true}, 2},
		{"all pending", [][]frontdoor.PrivateOriginState{{frontdoor.PrivateOriginApprovalPending, frontdoor.PrivateOriginApprovalPending}}, []bool{false}, 2},
		{"staggered convergence", [][]frontdoor.PrivateOriginState{
			{frontdoor.PrivateOriginUnknown, frontdoor.PrivateOriginMissing},
			{frontdoor.PrivateOriginApprovalPending, frontdoor.PrivateOriginUnknown},
			{frontdoor.PrivateOriginApprovalPending, frontdoor.PrivateOriginApprovalPending},
			{frontdoor.PrivateOriginUnknown, frontdoor.PrivateOriginApprovalPending},
		}, []bool{true, false, false, false}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for poll, states := range tt.states {
				observation := frontdoor.PrivateOriginsObservation{}
				for i, state := range states {
					member := []string{"member-1", "member-2"}[i]
					observation.Origins = append(observation.Origins, frontdoor.PrivateOriginObservation{
						Reference: frontdoor.OriginReference{OriginGroup: "owned-group", Origin: "origin-" + member},
						Target:    frontdoor.PrivateLinkTarget{ResourceID: "/privateLinkServices/" + member},
						State:     state,
					})
				}
				err := privateHTTPApprovalOrigins(observation, tt.expected)
				if (err != nil) != tt.wantErr[poll] {
					t.Fatalf("poll %d: error = %v, want error %t", poll, err, tt.wantErr[poll])
				}
				if err == nil {
					continue
				}
				for _, origin := range observation.Origins {
					for _, evidence := range []string{origin.Reference.OriginGroup, origin.Reference.Origin, origin.Target.ResourceID, string(origin.State)} {
						if !strings.Contains(err.Error(), evidence) {
							t.Errorf("poll %d: diagnostic %q omits %q", poll, err, evidence)
						}
					}
				}
				if !strings.Contains(err.Error(), "observed=") || !strings.Contains(err.Error(), "expected=") {
					t.Errorf("poll %d: diagnostic omits observed/expected counts: %v", poll, err)
				}
			}
		})
	}
}

type privateHTTPApprovalStub struct {
	services    map[string]armnetwork.PrivateLinkService
	writes      []string
	readErr     error
	readErrors  map[string]error
	writeErr    error
	keepPending bool
	reads       int
}

func (s *privateHTTPApprovalStub) Get(_ context.Context, group, _ string, _ *armnetwork.PrivateLinkServicesClientGetOptions) (armnetwork.PrivateLinkServicesClientGetResponse, error) {
	s.reads++
	if err := s.readErrors[group]; err != nil {
		return armnetwork.PrivateLinkServicesClientGetResponse{}, err
	}
	return armnetwork.PrivateLinkServicesClientGetResponse{PrivateLinkService: s.services[group]}, s.readErr
}

func (s *privateHTTPApprovalStub) UpdatePrivateEndpointConnection(_ context.Context, group, _ string, name string,
	request armnetwork.PrivateEndpointConnection, _ *armnetwork.PrivateLinkServicesClientUpdatePrivateEndpointConnectionOptions) (armnetwork.PrivateLinkServicesClientUpdatePrivateEndpointConnectionResponse, error) {
	s.writes = append(s.writes, group+"/"+name)
	if s.writeErr == nil && !s.keepPending {
		for _, connection := range s.services[group].Properties.PrivateEndpointConnections {
			if ptr.Deref(connection.Name, "") == name {
				connection.Properties.PrivateLinkServiceConnectionState = request.Properties.PrivateLinkServiceConnectionState
			}
		}
	}
	return armnetwork.PrivateLinkServicesClientUpdatePrivateEndpointConnectionResponse{}, s.writeErr
}

func privateHTTPApprovalFixture() (*privateHTTPApprovalStub, frontdoor.ResourceScope, frontdoor.RootRequests, []frontdoor.PrivateLinkPlan) {
	scope := frontdoor.ResourceScope{SubscriptionID: "test-subscription", ResourceGroup: "test-rg"}
	roots := frontdoor.RootRequests{ProfileName: "owned-profile"}
	stub := &privateHTTPApprovalStub{services: make(map[string]armnetwork.PrivateLinkService)}
	plans := make([]frontdoor.PrivateLinkPlan, 0, 2)
	for _, member := range []string{"member-1", "member-2"} {
		target := "/subscriptions/test-subscription/resourceGroups/" + member + "/providers/Microsoft.Network/privateLinkServices/proof"
		plans = append(plans, frontdoor.PrivateLinkPlan{Target: frontdoor.PrivateLinkTarget{ResourceID: target, Location: "swedencentral"}})
		stub.services[member] = armnetwork.PrivateLinkService{
			ID: ptr.To(target), Location: ptr.To("swedencentral"),
			Properties: &armnetwork.PrivateLinkServiceProperties{
				ProvisioningState: ptr.To(armnetwork.ProvisioningStateSucceeded),
				PrivateEndpointConnections: []*armnetwork.PrivateEndpointConnection{{
					ID: ptr.To(target + "/privateEndpointConnections/" + member), Name: ptr.To(member),
					Properties: &armnetwork.PrivateEndpointConnectionProperties{
						PrivateLinkServiceConnectionState: &armnetwork.PrivateLinkServiceConnectionState{
							Status: ptr.To(privateHTTPLinkPending), Description: ptr.To("Fleet AFD private origin for owned-profile"),
						},
					},
				}},
			},
		}
	}
	return stub, scope, roots, plans
}

func TestPrivateHTTPApprovalCollection(t *testing.T) {
	tests := []struct {
		name              string
		change            func(*privateHTTPApprovalStub)
		wantErr, approved bool
	}{
		{name: "both pending"},
		{name: "one early approval", change: func(s *privateHTTPApprovalStub) {
			s.services["member-1"].Properties.PrivateEndpointConnections[0].Properties.PrivateLinkServiceConnectionState.Status = ptr.To(privateHTTPLinkApproved)
		}},
		{name: "all approved", approved: true, change: func(s *privateHTTPApprovalStub) {
			for _, service := range s.services {
				service.Properties.PrivateEndpointConnections[0].Properties.PrivateLinkServiceConnectionState.Status = ptr.To(privateHTTPLinkApproved)
			}
		}},
		{name: "first read failure still examines second", wantErr: true, change: func(s *privateHTTPApprovalStub) {
			s.readErrors = map[string]error{"member-1": errors.New("first read failed")}
			s.services["member-2"].Properties.PrivateEndpointConnections = nil
		}},
		{name: "ambiguous request", wantErr: true, change: func(s *privateHTTPApprovalStub) {
			service := s.services["member-1"]
			service.Properties.PrivateEndpointConnections = append(service.Properties.PrivateEndpointConnections, service.Properties.PrivateEndpointConnections[0])
		}},
		{name: "rejected request", wantErr: true, change: func(s *privateHTTPApprovalStub) {
			s.services["member-2"].Properties.PrivateEndpointConnections[0].Properties.PrivateLinkServiceConnectionState.Status = ptr.To("Rejected")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub, scope, roots, plans := privateHTTPApprovalFixture()
			if tt.change != nil {
				tt.change(stub)
			}
			requests, err := collectPrivateHTTPApprovalRequests(context.Background(), stub, scope, roots, "proof", plans)
			if (err != nil) != tt.wantErr || stub.reads != 2 || len(stub.writes) != 0 {
				t.Fatalf("collection=%+v err=%v reads=%d writes=%v", requests, err, stub.reads, stub.writes)
			}
			if tt.name == "first read failure still examines second" {
				if !errors.Is(err, stub.readErrors["member-1"]) || !strings.Contains(err.Error(), "member-2") {
					t.Fatalf("missing all-member failure evidence: %v", err)
				}
			}
			if err == nil && (privateHTTPApprovalsComplete(requests) == nil) != tt.approved {
				t.Fatalf("approved=%t, observed=%s", tt.approved, privateHTTPApprovalProgress(requests))
			}
		})
	}
}

func TestPrivateHTTPApprovalSequence(t *testing.T) {
	stub, scope, roots, plans := privateHTTPApprovalFixture()
	origins := frontdoor.PrivateOriginsObservation{Origins: []frontdoor.PrivateOriginObservation{
		{State: frontdoor.PrivateOriginUnknown}, {State: frontdoor.PrivateOriginUnknown},
	}}
	if err := privateHTTPApprovalOrigins(origins, 2); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := collectPrivateHTTPApprovalRequests(context.Background(), stub, scope, roots, "proof", plans)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		member   string
		approved bool
	}{
		{},
		{member: "member-2"},
		{member: "member-1", approved: true},
	} {
		if step.member != "" {
			stub.services[step.member].Properties.PrivateEndpointConnections[0].Properties.PrivateLinkServiceConnectionState.Status = ptr.To(privateHTTPLinkApproved)
		}
		requests, err := collectPrivateHTTPApprovalRequests(context.Background(), stub, scope, roots, "proof", plans)
		if err != nil {
			t.Fatal(err)
		}
		if err := privateHTTPApprovalIdentities(checkpoint, requests); err != nil {
			t.Fatal(err)
		}
		err = privateHTTPApprovalsComplete(requests)
		if (err == nil) != step.approved || origins.OriginsConverged || len(stub.writes) != 0 {
			t.Fatalf("premature approval/readiness or manual write: %v, origins=%+v writes=%v", err, origins, stub.writes)
		}
		if !step.approved {
			for _, request := range requests {
				if !strings.Contains(err.Error(), request.id) {
					t.Fatalf("approval wait omitted a member: %v", err)
				}
			}
		}
	}
}

func TestPrivateHTTPApprovalCollectionCanceled(t *testing.T) {
	stub, scope, roots, plans := privateHTTPApprovalFixture()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	requests, err := collectPrivateHTTPApprovalRequests(ctx, stub, scope, roots, "proof", plans)
	if !errors.Is(err, context.Canceled) || len(requests) != 0 || stub.reads != 0 || len(stub.writes) != 0 {
		t.Fatalf("canceled collection performed work: requests=%+v err=%v reads=%d writes=%v", requests, err, stub.reads, stub.writes)
	}
}

func TestPrivateHTTPTemporaryApproval(t *testing.T) {
	tests := []struct {
		name, flag string
		change     func(*privateHTTPApprovalStub, []frontdoor.PrivateLinkPlan)
		wantErr    bool
		wantWrites int
	}{
		{name: "default off"},
		{name: "explicit false", flag: privateHTTPDisabled},
		{name: "invalid flag", flag: "yes", wantErr: true},
		{name: "both exact requests", flag: privateHTTPEnabled, wantWrites: 2},
		{name: "already approved is not written again", flag: privateHTTPEnabled, change: func(s *privateHTTPApprovalStub, _ []frontdoor.PrivateLinkPlan) {
			s.services["member-1"].Properties.PrivateEndpointConnections[0].Properties.PrivateLinkServiceConnectionState.Status = ptr.To(privateHTTPLinkApproved)
		}, wantWrites: 1},
		{name: "unrelated request is never approved", flag: privateHTTPEnabled, change: func(s *privateHTTPApprovalStub, _ []frontdoor.PrivateLinkPlan) {
			s.services["member-1"].Properties.PrivateEndpointConnections = append(s.services["member-1"].Properties.PrivateEndpointConnections,
				&armnetwork.PrivateEndpointConnection{Properties: &armnetwork.PrivateEndpointConnectionProperties{
					PrivateLinkServiceConnectionState: &armnetwork.PrivateLinkServiceConnectionState{
						Status: ptr.To(privateHTTPLinkPending), Description: ptr.To("foreign request"),
					},
				}})
		}, wantWrites: 2},
		{name: "second target missing", flag: privateHTTPEnabled, change: func(s *privateHTTPApprovalStub, _ []frontdoor.PrivateLinkPlan) {
			delete(s.services, "member-2")
		}, wantErr: true},
		{name: "foreign target identity", flag: privateHTTPEnabled, change: func(s *privateHTTPApprovalStub, _ []frontdoor.PrivateLinkPlan) {
			pls := s.services["member-2"]
			pls.ID = ptr.To("/foreign/privateLinkServices/proof")
			s.services["member-2"] = pls
		}, wantErr: true},
		{name: "wrong request owner", flag: privateHTTPEnabled, change: func(s *privateHTTPApprovalStub, _ []frontdoor.PrivateLinkPlan) {
			s.services["member-2"].Properties.PrivateEndpointConnections[0].Properties.PrivateLinkServiceConnectionState.Description = ptr.To("foreign profile")
		}, wantErr: true},
		{name: "ambiguous owned requests", flag: privateHTTPEnabled, change: func(s *privateHTTPApprovalStub, _ []frontdoor.PrivateLinkPlan) {
			pls := s.services["member-2"]
			pls.Properties.PrivateEndpointConnections = append(pls.Properties.PrivateEndpointConnections, pls.Properties.PrivateEndpointConnections[0])
		}, wantErr: true},
		{name: "foreign connection identity", flag: privateHTTPEnabled, change: func(s *privateHTTPApprovalStub, _ []frontdoor.PrivateLinkPlan) {
			s.services["member-2"].Properties.PrivateEndpointConnections[0].ID = ptr.To("/foreign/connection")
		}, wantErr: true},
		{name: "replaced correlated request is not approved", flag: privateHTTPEnabled, change: func(s *privateHTTPApprovalStub, _ []frontdoor.PrivateLinkPlan) {
			connection := s.services["member-2"].Properties.PrivateEndpointConnections[0]
			connection.Name = ptr.To("replacement")
			connection.ID = ptr.To(strings.TrimSuffix(*connection.ID, "member-2") + "replacement")
		}, wantErr: true},
		{name: "rejected request", flag: privateHTTPEnabled, change: func(s *privateHTTPApprovalStub, _ []frontdoor.PrivateLinkPlan) {
			s.services["member-2"].Properties.PrivateEndpointConnections[0].Properties.PrivateLinkServiceConnectionState.Status = ptr.To("Rejected")
		}, wantErr: true},
		{name: "duplicate target", flag: privateHTTPEnabled, change: func(_ *privateHTTPApprovalStub, plans []frontdoor.PrivateLinkPlan) {
			plans[1] = plans[0]
		}, wantErr: true},
		{name: "outside proof namespace", flag: privateHTTPEnabled, change: func(_ *privateHTTPApprovalStub, plans []frontdoor.PrivateLinkPlan) {
			plans[1].Target.ResourceID += "-foreign"
		}, wantErr: true},
		{name: "read failure", flag: privateHTTPEnabled, change: func(s *privateHTTPApprovalStub, _ []frontdoor.PrivateLinkPlan) {
			s.readErr = errors.New("PLS read denied")
		}, wantErr: true},
		{name: "write failure stops second approval", flag: privateHTTPEnabled, change: func(s *privateHTTPApprovalStub, _ []frontdoor.PrivateLinkPlan) {
			s.writeErr = errors.New("approval denied")
		}, wantErr: true, wantWrites: 1},
		{name: "write is not approval evidence", flag: privateHTTPEnabled, change: func(s *privateHTTPApprovalStub, _ []frontdoor.PrivateLinkPlan) {
			s.keepPending = true
		}, wantErr: true, wantWrites: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ENABLE_AFD_PRIVATE_LINK_AUTO_APPROVAL", tt.flag)
			t.Setenv("ENABLE_AFD_PRIVATE_HTTP_PROOF", privateHTTPEnabled)
			stub, scope, roots, plans := privateHTTPApprovalFixture()
			checkpoint := make([]privateHTTPApprovalRequest, 0, len(plans))
			for _, plan := range plans {
				member := strings.Split(plan.Target.ResourceID, "/")[4]
				checkpoint = append(checkpoint, privateHTTPApprovalRequest{
					target: plan.Target.ResourceID, id: plan.Target.ResourceID + "/privateEndpointConnections/" + member,
				})
			}
			if tt.change != nil {
				tt.change(stub, plans)
			}
			err := approvePrivateHTTPRequests(context.Background(), stub, scope, roots, "proof", plans, checkpoint)
			if (err != nil) != tt.wantErr || len(stub.writes) != tt.wantWrites {
				t.Fatalf("approval error=%v, writes=%v; want error=%t, writes=%d", err, stub.writes, tt.wantErr, tt.wantWrites)
			}
			if (tt.flag == "" || tt.flag == privateHTTPDisabled || tt.flag == "yes") && stub.reads != 0 {
				t.Fatalf("disabled/invalid approval flag made %d Azure reads", stub.reads)
			}
			if tt.name == "read failure" && !errors.Is(err, stub.readErr) ||
				tt.name == "write failure stops second approval" && !errors.Is(err, stub.writeErr) {
				t.Fatalf("SDK failure lost its cause: %v", err)
			}
		})
	}
	t.Run("proof disabled", func(t *testing.T) {
		t.Setenv("ENABLE_AFD_PRIVATE_LINK_AUTO_APPROVAL", privateHTTPEnabled)
		t.Setenv("ENABLE_AFD_PRIVATE_HTTP_PROOF", privateHTTPDisabled)
		stub := &privateHTTPApprovalStub{}
		err := approvePrivateHTTPRequests(context.Background(), stub, frontdoor.ResourceScope{},
			frontdoor.RootRequests{}, "", nil, nil)
		if err == nil || stub.reads != 0 || len(stub.writes) != 0 {
			t.Fatalf("disabled proof permitted approval work: %v, %+v", err, stub)
		}
	})
}

func TestPrivateHTTPTimeoutBudgets(t *testing.T) {
	specMinimum := framework.PollTimeout +
		time.Duration(2+len(memberClusterNames))*privateHTTPTimeout +
		5*privateHTTPConvergenceTimeout + time.Minute + privateHTTPCleanupTimeout
	tests := []struct {
		name         string
		got, minimum time.Duration
	}{
		{"AFD convergence", privateHTTPConvergenceTimeout, 45 * time.Minute},
		{"cloud cleanup", privateHTTPCleanupTimeout, 45 * time.Minute},
		{"cleanup node headroom", privateHTTPCleanupNodeTimeout, privateHTTPCleanupTimeout + time.Minute},
		{"sequential spec phases", privateHTTPSpecTimeout, specMinimum},
		{"ARM polling interval", privateHTTPConvergencePoll, 15 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got < tt.minimum {
				t.Fatalf("budget = %s, need at least %s", tt.got, tt.minimum)
			}
		})
	}
	deferredCleanup := time.Duration(len(memberClusterNames)+2)*privateHTTPCleanupNodeTimeout + time.Minute
	const documentedSuiteTimeout = 9*time.Hour + 30*time.Minute
	if required := privateHTTPSpecTimeout + deferredCleanup + 15*time.Minute; required > documentedSuiteTimeout {
		t.Fatalf("documented Ginkgo suite timeout cannot accommodate spec, cleanup and suite headroom: %s", required)
	}
	if required := documentedSuiteTimeout + 30*time.Minute; required > 10*time.Hour {
		t.Fatalf("documented ten-hour Go timeout cannot accommodate the suite and runner headroom: %s", required)
	}
}

func privateHTTPGatewayFixture() gatewayv1.Gateway {
	conditions := []metav1.Condition{
		{Type: privateHTTPAccepted, Status: metav1.ConditionTrue, Reason: privateHTTPAccepted, ObservedGeneration: 3},
		{Type: privateHTTPProgrammed, Status: metav1.ConditionTrue, Reason: privateHTTPProgrammed, ObservedGeneration: 3},
	}
	return gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{UID: types.UID("owned-gateway"), Generation: 3,
			Finalizers: []string{objectmeta.GatewayAFDCleanupFinalizer}},
		Spec: gatewayv1.GatewaySpec{Listeners: []gatewayv1.Listener{{Name: privateHTTPListener, Protocol: gatewayv1.HTTPProtocolType, Port: 80}}},
		Status: gatewayv1.GatewayStatus{
			Conditions: conditions,
			Addresses:  []gatewayv1.GatewayStatusAddress{{Type: ptr.To(gatewayv1.HostnameAddressType), Value: "proof.azurefd.net"}},
			Listeners: []gatewayv1.ListenerStatus{{
				Name: privateHTTPListener, AttachedRoutes: 1, Conditions: append([]metav1.Condition(nil), conditions...),
			}},
		},
	}
}

func TestPrivateHTTPGatewayEvidence(t *testing.T) {
	for _, scenario := range []string{"current Gateway", "stale gateway", "stale listener", "missing owner", "wrong listener", "no routes", "missing address", "IP address"} {
		t.Run(scenario, func(t *testing.T) {
			gateway := privateHTTPGatewayFixture()
			switch scenario {
			case "stale gateway":
				gateway.Status.Conditions[1].ObservedGeneration--
			case "stale listener":
				gateway.Status.Listeners[0].Conditions[1].ObservedGeneration--
			case "missing owner":
				gateway.Finalizers = nil
			case "wrong listener":
				gateway.Status.Listeners[0].Name = "other"
			case "no routes":
				gateway.Status.Listeners[0].AttachedRoutes = 0
			case "missing address":
				gateway.Status.Addresses = nil
			case "IP address":
				gateway.Status.Addresses[0].Type = ptr.To(gatewayv1.IPAddressType)
			}
			hostname, err := privateHTTPGatewayState(&gateway, true, 1)
			if (err == nil) != (scenario == "current Gateway") || (err != nil && hostname != "") {
				t.Fatalf("invalid programmed evidence: %q, %v", hostname, err)
			}
		})
	}
}

func TestPrivateHTTPResponseEvidence(t *testing.T) {
	const body = `{"proof_id":"run","cluster_id":"member-1","host":"proof.azurefd.net","request_id":"fresh"}`
	const memberName = "member-1"
	for _, scenario := range []string{"current response", "stale request", "wrong host", "wrong HTTP workload", "unknown member", "redirect", "no edge evidence", "invalid JSON"} {
		t.Run(scenario, func(t *testing.T) {
			reply := privateHTTPReply{Status: http.StatusOK, Body: []byte(body), AzureRef: privateHTTPTestEdgeReference}
			switch scenario {
			case "stale request":
				reply.Body = []byte(strings.ReplaceAll(body, "fresh", "old"))
			case "wrong host":
				reply.Body = []byte(strings.ReplaceAll(body, "proof.azurefd.net", "origin.internal"))
			case "wrong HTTP workload":
				reply.Body = []byte(strings.ReplaceAll(body, `"run"`, `"different"`))
			case "unknown member":
				reply.Body = []byte(strings.ReplaceAll(body, memberName, "unexpected"))
			case "redirect":
				reply.Status = http.StatusMovedPermanently
			case "no edge evidence":
				reply.AzureRef = ""
			case "invalid JSON":
				reply.Body = []byte("not-json")
			}
			member, err := validatePrivateHTTPResponse(reply, "run", "fresh", "proof.azurefd.net", map[string]bool{memberName: true})
			if (err == nil) != (scenario == "current response") || (err != nil && member != "") {
				t.Fatalf("invalid application evidence: %q, %v", member, err)
			}
		})
	}
}

func TestPrivateHTTPWithdrawalEvidence(t *testing.T) {
	for _, status := range []int{200, 301, 404, 408, 429, 503} {
		reply := privateHTTPReply{Status: status, Body: []byte("disabled"), AzureRef: privateHTTPTestEdgeReference}
		err := validatePrivateHTTPBlocked(reply, "unique-run")
		if (err == nil) != (status == 404 || status == 503) {
			t.Fatalf("HTTP %d: invalid withdrawal proof: %v", status, err)
		}
	}
	for _, reply := range []privateHTTPReply{
		{Status: 503, Body: []byte("application unique-run"), AzureRef: privateHTTPTestEdgeReference},
		{Status: 404, Body: []byte("disabled")},
		{},
	} {
		if err := validatePrivateHTTPBlocked(reply, "unique-run"); err == nil {
			t.Fatalf("application/no-edge/no-response evidence passed withdrawal: %+v", reply)
		}
	}
}

func TestPrivateHTTPProbeEvidence(t *testing.T) {
	const entry = `WARNING: {"fleet_private_http_proof":"run","method":"GET","path":"/","host":"observed-probe-host","user_agent":"Edge Health Probe"}`
	for _, scenario := range []string{"probe", "wrong probe workload", "application request", "wrong path", "wrong method", "no host", "absent probe record"} {
		t.Run(scenario, func(t *testing.T) {
			logs := entry
			switch scenario {
			case "wrong probe workload":
				logs = strings.ReplaceAll(logs, `"run"`, `"old-run"`)
			case "application request":
				logs = strings.ReplaceAll(logs, "Edge Health Probe", "Go-http-client/1.1")
			case "wrong path":
				logs = strings.ReplaceAll(logs, `"path":"/"`, `"path":"/healthz"`)
			case "wrong method":
				logs = strings.ReplaceAll(logs, "GET", "HEAD")
			case "no host":
				logs = strings.ReplaceAll(logs, "observed-probe-host", "")
			case "absent probe record":
				logs = ""
			}
			host, err := privateHTTPProbeEvidence(logs, "run")
			if (err == nil) != (scenario == "probe") || (err != nil && host != "") {
				t.Fatalf("invalid probe evidence: %q, %v", host, err)
			}
		})
	}
}

func TestPrivateHTTPRequestBoundaries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte(strings.Repeat("x", privateHTTPMaxBody+1)))
	}))
	defer server.Close()
	c := privateHTTPClient()
	defer c.CloseIdleConnections()
	reply, err := requestPrivateHTTP(context.Background(), c, server.URL+"/redirect", "nonce")
	if err != nil || reply.Status != http.StatusFound {
		t.Fatalf("redirect followed instead of observed: %+v, %v", reply, err)
	}
	if _, err := requestPrivateHTTP(context.Background(), c, server.URL, "nonce"); err == nil {
		t.Fatal("oversized application response accepted")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := requestPrivateHTTP(canceled, c, server.URL, "nonce"); err == nil {
		t.Fatal("canceled request accepted as traffic evidence")
	}
}

func TestPrivateHTTPFrontendEvidence(t *testing.T) {
	const plsID = "/subscriptions/sub/resourceGroups/member/providers/Microsoft.Network/privateLinkServices/pls"
	const frontendID = "/subscriptions/sub/resourceGroups/member/providers/Microsoft.Network/loadBalancers/ilb/frontendIPConfigurations/private"
	for _, scenario := range []string{"private", "public address", "public prefix", "missing subnet", "source replaced", "wrong IP", "unbound frontend", "wrong PLS"} {
		t.Run(scenario, func(t *testing.T) {
			service := corev1.Service{
				ObjectMeta: metav1.ObjectMeta{UID: "service-uid", Annotations: map[string]string{
					objectmeta.ServiceAnnotationAzureLoadBalancerInternal: privateHTTPEnabled,
				}},
				Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
				Status: corev1.ServiceStatus{LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: []corev1.LoadBalancerIngress{{IP: "10.0.0.5"}},
				}},
			}
			origin := gatewaymodel.Origin{Cluster: "member-1", ServiceUID: "service-uid", Endpoint: "10.0.0.5",
				Weight: 1, PrivateLinkResourceID: plsID, PrivateLinkLocation: "eastus2"}
			pls := armnetwork.PrivateLinkService{ID: ptr.To(plsID), Location: ptr.To("eastus2"),
				Properties: &armnetwork.PrivateLinkServiceProperties{
					ProvisioningState:                    ptr.To(armnetwork.ProvisioningStateSucceeded),
					LoadBalancerFrontendIPConfigurations: []*armnetwork.FrontendIPConfiguration{{ID: ptr.To(frontendID)}},
				}}
			frontend := armnetwork.FrontendIPConfiguration{ID: ptr.To(frontendID),
				Properties: &armnetwork.FrontendIPConfigurationPropertiesFormat{
					PrivateIPAddress: ptr.To("10.0.0.5"), Subnet: &armnetwork.Subnet{ID: ptr.To("subnet-id")},
					ProvisioningState: ptr.To(armnetwork.ProvisioningStateSucceeded),
				}}
			switch scenario {
			case "public address":
				frontend.Properties.PublicIPAddress = &armnetwork.PublicIPAddress{ID: ptr.To("public-id")}
			case "public prefix":
				frontend.Properties.PublicIPPrefix = &armnetwork.SubResource{ID: ptr.To("public-prefix")}
			case "missing subnet":
				frontend.Properties.Subnet = nil
			case "source replaced":
				service.UID = "new-service-uid"
			case "wrong IP":
				frontend.Properties.PrivateIPAddress = ptr.To("10.0.0.6")
			case "unbound frontend":
				pls.Properties.LoadBalancerFrontendIPConfigurations = nil
			case "wrong PLS":
				pls.ID = ptr.To(plsID + "-different")
			}
			err := validatePrivateHTTPFrontend(&service, origin, pls, frontend)
			if (err == nil) != (scenario == "private") {
				t.Fatalf("invalid private frontend evidence: %v", err)
			}
		})
	}
}

func TestPrivateHTTPUnprogrammedEvidence(t *testing.T) {
	for _, retainedAddress := range []bool{false, true} {
		gateway := privateHTTPGatewayFixture()
		gateway.Status.Conditions[1].Status = metav1.ConditionFalse
		gateway.Status.Conditions[1].Reason = privateHTTPPending
		gateway.Status.Listeners[0].Conditions[1] = gateway.Status.Conditions[1]
		gateway.Status.Listeners[0].AttachedRoutes = 0
		if !retainedAddress {
			gateway.Status.Addresses = nil
		}
		_, err := privateHTTPGatewayState(&gateway, false, 0)
		if (err == nil) != !retainedAddress {
			t.Fatalf("withdrawn Gateway retained an address: %v", err)
		}
	}
}

func TestPrivateHTTPInstalledControllerEvidence(t *testing.T) {
	for _, scenario := range []string{"installed", "stale rollout", "disabled", "wrong image", "old pod image", "unready pod", "no image identity"} {
		t.Run(scenario, func(t *testing.T) {
			deployment := appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{UID: "controller-uid", Generation: 2},
				Spec: appsv1.DeploymentSpec{Replicas: ptr.To[int32](1), Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{ServiceAccountName: "gateway", Containers: []corev1.Container{{
						Name: privateHTTPManager, Image: "registry/controller:proof",
						Args: []string{"--enable-afd=true", "--afd-resource-group=test-rg"},
					}}},
				}},
				Status: appsv1.DeploymentStatus{ObservedGeneration: 2, UpdatedReplicas: 1, ReadyReplicas: 1, AvailableReplicas: 1},
			}
			pod := corev1.Pod{Spec: corev1.PodSpec{ServiceAccountName: "gateway", Containers: []corev1.Container{{
				Name: privateHTTPManager, Image: "registry/controller:proof",
			}}}, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
				Name: privateHTTPManager, Ready: true, ImageID: "registry/controller@sha256:actual-image", ContainerID: "containerd://running",
				State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
			}}}}
			switch scenario {
			case "stale rollout":
				deployment.Status.ObservedGeneration--
			case "disabled":
				deployment.Spec.Template.Spec.Containers[0].Args = nil
			case "wrong image":
				deployment.Spec.Template.Spec.Containers[0].Image = "different:tag"
			case "old pod image":
				pod.Spec.Containers[0].Image = "registry/controller:old"
			case "unready pod":
				pod.Status.ContainerStatuses[0].Ready = false
			case "no image identity":
				pod.Status.ContainerStatuses[0].ImageID = ""
			}
			images, err := privateHTTPControllerEvidence(&deployment, []corev1.Pod{pod}, "registry/controller:proof", "test-rg")
			if (err == nil) != (scenario == "installed") || (err == nil && len(images) != 1) {
				t.Fatalf("invalid installed-controller evidence: %v, %v", images, err)
			}
		})
	}
}
