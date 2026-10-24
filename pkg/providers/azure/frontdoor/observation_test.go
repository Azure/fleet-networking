/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package frontdoor

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	"k8s.io/utils/ptr"

	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
)

type privateOriginSnapshot struct {
	scope   ResourceScope
	owner   GatewayOwner
	model   gatewaymodel.GlobalGateway
	profile armcdn.Profile
	origins map[OriginReference]armcdn.AFDOrigin
	first   OriginReference
}

func originSnapshot(t *testing.T, model gatewaymodel.GlobalGateway) privateOriginSnapshot {
	t.Helper()
	scope, owner := ownershipFixture()
	roots, err := BuildRootRequests(scope, owner)
	if err != nil {
		t.Fatal(err)
	}
	requests, err := BuildBackendRequests(model)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := privateOriginSnapshot{
		scope: scope, owner: owner, model: model, profile: roots.Profile,
		origins: make(map[OriginReference]armcdn.AFDOrigin),
	}
	snapshot.profile.ID = ptr.To(roots.ProfileID)
	for _, group := range requests.OriginGroups {
		for _, origin := range group.Origins {
			ref := OriginReference{OriginGroup: group.Name, Origin: origin.Name}
			observed := origin.Parameters
			observed.ID = ptr.To(roots.ProfileID + "/originGroups/" + group.Name + "/origins/" + origin.Name)
			observed.Name = ptr.To(origin.Name)
			observed.Properties.OriginGroupName = ptr.To(group.Name)
			observed.Properties.ProvisioningState = ptr.To(armcdn.AfdProvisioningStateSucceeded)
			observed.Properties.DeploymentStatus = ptr.To(armcdn.DeploymentStatusSucceeded)
			observed.Properties.SharedPrivateLinkResource.Status = ptr.To(armcdn.SharedPrivateLinkResourceStatusApproved)
			snapshot.origins[ref] = observed
			if snapshot.first == (OriginReference{}) {
				snapshot.first = ref
			}
		}
	}
	return snapshot
}

func (s privateOriginSnapshot) observe() (PrivateOriginsObservation, error) {
	return ObservePrivateOrigins(s.scope, s.owner, s.model, s.profile, s.origins)
}

func TestObservePrivateOriginStates(t *testing.T) {
	tests := []struct {
		name   string
		change func(*armcdn.AFDOrigin)
		absent bool
		want   PrivateOriginState
	}{
		{name: "approved and deployed", want: PrivateOriginReady},
		{name: "not yet created", absent: true, want: PrivateOriginMissing},
		{name: "missing properties", change: func(o *armcdn.AFDOrigin) { o.Properties = nil }, want: PrivateOriginUnknown},
		{name: "awaiting manual approval", change: func(o *armcdn.AFDOrigin) {
			o.Properties.SharedPrivateLinkResource.Status = ptr.To(armcdn.SharedPrivateLinkResourceStatusPending)
		}, want: PrivateOriginApprovalPending},
		{name: "approved while creating", change: func(o *armcdn.AFDOrigin) {
			o.Properties.ProvisioningState = ptr.To(armcdn.AfdProvisioningStateCreating)
		}, want: PrivateOriginProvisioning},
		{name: "approved while updating", change: func(o *armcdn.AFDOrigin) {
			o.Properties.ProvisioningState = ptr.To(armcdn.AfdProvisioningStateUpdating)
		}, want: PrivateOriginProvisioning},
		{name: "approved while deleting", change: func(o *armcdn.AFDOrigin) {
			o.Properties.ProvisioningState = ptr.To(armcdn.AfdProvisioningStateDeleting)
		}, want: PrivateOriginProvisioning},
		{name: "approved while deploying", change: func(o *armcdn.AFDOrigin) {
			o.Properties.DeploymentStatus = ptr.To(armcdn.DeploymentStatusInProgress)
		}, want: PrivateOriginProvisioning},
		{name: "deployment not started", change: func(o *armcdn.AFDOrigin) {
			o.Properties.DeploymentStatus = ptr.To(armcdn.DeploymentStatusNotStarted)
		}, want: PrivateOriginProvisioning},
		{name: "rejected", change: func(o *armcdn.AFDOrigin) {
			o.Properties.SharedPrivateLinkResource.Status = ptr.To(armcdn.SharedPrivateLinkResourceStatusRejected)
		}, want: PrivateOriginRejected},
		{name: "disconnected", change: func(o *armcdn.AFDOrigin) {
			o.Properties.SharedPrivateLinkResource.Status = ptr.To(armcdn.SharedPrivateLinkResourceStatusDisconnected)
		}, want: PrivateOriginDisconnected},
		{name: "approval timed out", change: func(o *armcdn.AFDOrigin) {
			o.Properties.SharedPrivateLinkResource.Status = ptr.To(armcdn.SharedPrivateLinkResourceStatusTimeout)
		}, want: PrivateOriginTimedOut},
		{name: "provisioning failed", change: func(o *armcdn.AFDOrigin) {
			o.Properties.ProvisioningState = ptr.To(armcdn.AfdProvisioningStateFailed)
		}, want: PrivateOriginFailed},
		{name: "deployment failed", change: func(o *armcdn.AFDOrigin) {
			o.Properties.DeploymentStatus = ptr.To(armcdn.DeploymentStatusFailed)
		}, want: PrivateOriginFailed},
		{name: "missing approval", change: func(o *armcdn.AFDOrigin) {
			o.Properties.SharedPrivateLinkResource.Status = nil
		}, want: PrivateOriginUnknown},
		{name: "unknown approval", change: func(o *armcdn.AFDOrigin) {
			o.Properties.SharedPrivateLinkResource.Status = ptr.To(armcdn.SharedPrivateLinkResourceStatus(changedValue))
		}, want: PrivateOriginUnknown},
		{name: "missing provisioning state", change: func(o *armcdn.AFDOrigin) {
			o.Properties.ProvisioningState = nil
		}, want: PrivateOriginUnknown},
		{name: "unknown provisioning state", change: func(o *armcdn.AFDOrigin) {
			o.Properties.ProvisioningState = ptr.To(armcdn.AfdProvisioningState(changedValue))
		}, want: PrivateOriginUnknown},
		{name: "missing deployment state", change: func(o *armcdn.AFDOrigin) {
			o.Properties.DeploymentStatus = nil
		}, want: PrivateOriginUnknown},
		{name: "unknown deployment state", change: func(o *armcdn.AFDOrigin) {
			o.Properties.DeploymentStatus = ptr.To(armcdn.DeploymentStatus(changedValue))
		}, want: PrivateOriginUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := originSnapshot(t, backendModel())
			origin := snapshot.origins[snapshot.first]
			if tt.change != nil {
				tt.change(&origin)
			}
			snapshot.origins[snapshot.first] = origin
			if tt.absent {
				delete(snapshot.origins, snapshot.first)
			}
			got, err := snapshot.observe()
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Origins) != 2 || got.Origins[0].State != tt.want ||
				got.OriginsConverged != (tt.want == PrivateOriginReady) {
				t.Fatalf("observation=%#v, want first origin %s", got, tt.want)
			}
		})
	}
}

func TestObservePrivateOriginDrift(t *testing.T) {
	tests := []struct {
		name   string
		change func(*armcdn.AFDOriginProperties)
	}{
		{"public origin", func(p *armcdn.AFDOriginProperties) { p.SharedPrivateLinkResource = nil }},
		{"missing private target", func(p *armcdn.AFDOriginProperties) { p.SharedPrivateLinkResource.PrivateLink = nil }},
		{"missing private ID", func(p *armcdn.AFDOriginProperties) { p.SharedPrivateLinkResource.PrivateLink.ID = nil }},
		{"different PLS", func(p *armcdn.AFDOriginProperties) { p.SharedPrivateLinkResource.PrivateLink.ID = ptr.To(changedValue) }},
		{"different region", func(p *armcdn.AFDOriginProperties) {
			p.SharedPrivateLinkResource.PrivateLinkLocation = ptr.To(changedValue)
		}},
		{"different target group", func(p *armcdn.AFDOriginProperties) { p.SharedPrivateLinkResource.GroupID = ptr.To("sites") }},
		{"Azure origin fallback", func(p *armcdn.AFDOriginProperties) {
			p.AzureOrigin = &armcdn.ResourceReference{ID: ptr.To(changedValue)}
		}},
		{"different host", func(p *armcdn.AFDOriginProperties) { p.HostName = ptr.To(changedValue) }},
		{"different port", func(p *armcdn.AFDOriginProperties) { p.HTTPPort = ptr.To[int32](80) }},
		{"different weight", func(p *armcdn.AFDOriginProperties) { p.Weight = ptr.To[int32](1) }},
		{"different priority", func(p *armcdn.AFDOriginProperties) { p.Priority = ptr.To[int32](2) }},
		{"disabled", func(p *armcdn.AFDOriginProperties) { p.EnabledState = ptr.To(armcdn.EnabledStateDisabled) }},
		{"host override", func(p *armcdn.AFDOriginProperties) { p.OriginHostHeader = ptr.To(changedValue) }},
		{"certificate validation disabled", func(p *armcdn.AFDOriginProperties) { p.EnforceCertificateNameCheck = ptr.To(false) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := originSnapshot(t, backendModel())
			tt.change(snapshot.origins[snapshot.first].Properties)
			got, err := snapshot.observe()
			if err != nil || got.OriginsConverged || got.Origins[0].State != PrivateOriginDrifted {
				t.Fatalf("drift counted as current configuration: %#v, %v", got, err)
			}
		})
	}
}

func TestObservePrivateOriginsRejectsInvalidIdentity(t *testing.T) {
	tests := []struct {
		name   string
		change func(*privateOriginSnapshot)
	}{
		{"foreign profile", func(s *privateOriginSnapshot) {
			s.profile.Tags["networking.fleet.azure.com.hub-uid"] = ptr.To(changedValue)
		}},
		{"different Gateway UID", func(s *privateOriginSnapshot) { s.model.UID = changedValue }},
		{"different Gateway namespace", func(s *privateOriginSnapshot) { s.model.Namespace = changedValue }},
		{"different Gateway name", func(s *privateOriginSnapshot) { s.model.Name = changedValue }},
		{"invalid backend", func(s *privateOriginSnapshot) { s.model.Routes[0].Rules[0].Backends[0].Port = 0 }},
		{"foreign origin ID", func(s *privateOriginSnapshot) {
			o := s.origins[s.first]
			o.ID = ptr.To(changedValue)
			s.origins[s.first] = o
		}},
		{"missing origin ID", func(s *privateOriginSnapshot) {
			o := s.origins[s.first]
			o.ID = nil
			s.origins[s.first] = o
		}},
		{"contradictory origin name", func(s *privateOriginSnapshot) {
			o := s.origins[s.first]
			o.Name = ptr.To(changedValue)
			s.origins[s.first] = o
		}},
		{"contradictory group name", func(s *privateOriginSnapshot) {
			s.origins[s.first].Properties.OriginGroupName = ptr.To(changedValue)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := originSnapshot(t, backendModel())
			tt.change(&snapshot)
			got, err := snapshot.observe()
			if err == nil || !reflect.DeepEqual(got, PrivateOriginsObservation{}) {
				t.Fatalf("invalid identity returned usable observation: %#v, %v", got, err)
			}
		})
	}
}

func TestObservePrivateOriginsSharedApproval(t *testing.T) {
	model := backendModel()
	backend := &model.Routes[0].Rules[0].Backends[0]
	backend.Origins[1].PrivateLinkResourceID = backend.Origins[0].PrivateLinkResourceID
	appendBackend(&model).Name = otherName
	snapshot := originSnapshot(t, model)
	for _, origin := range snapshot.origins {
		origin.Properties.SharedPrivateLinkResource.Status = ptr.To(armcdn.SharedPrivateLinkResourceStatusPending)
	}
	got, err := snapshot.observe()
	if err != nil || got.OriginsConverged || len(got.PendingApprovals) != 1 ||
		len(got.PendingApprovals[0].Origins) != 4 || len(got.Origins) != 4 {
		t.Fatalf("shared approval was not deduplicated: %#v, %v", got, err)
	}
	got.PendingApprovals[0].Origins[0].Origin = changedValue
	again, err := snapshot.observe()
	if err != nil || again.PendingApprovals[0].Origins[0].Origin == changedValue {
		t.Fatalf("observation aliases another invocation: %#v, %v", again, err)
	}
	snapshot.origins[snapshot.first].Properties.SharedPrivateLinkResource.Status = ptr.To(armcdn.SharedPrivateLinkResourceStatusApproved)
	got, err = snapshot.observe()
	if err != nil || got.OriginsConverged || len(got.PendingApprovals) != 1 {
		t.Fatalf("one approved origin incorrectly approved its shared peers: %#v, %v", got, err)
	}
	for _, origin := range snapshot.origins {
		origin.Properties.SharedPrivateLinkResource.Status = ptr.To(armcdn.SharedPrivateLinkResourceStatusApproved)
	}
	got, err = snapshot.observe()
	if err != nil || !got.OriginsConverged || len(got.PendingApprovals) != 0 {
		t.Fatalf("approval without an intent edit did not converge: %#v, %v", got, err)
	}
}

func TestObservePrivateOriginsSnapshotSafety(t *testing.T) {
	snapshot := originSnapshot(t, backendModel())
	for ref, origin := range snapshot.origins {
		origin.ID = ptr.To(strings.ToUpper(*origin.ID))
		origin.Properties.SharedPrivateLinkResource.PrivateLink.ID = ptr.To(strings.ToUpper(*origin.Properties.SharedPrivateLinkResource.PrivateLink.ID))
		origin.Properties.SharedPrivateLinkResource.PrivateLinkLocation = ptr.To("EASTUS")
		origin.Properties.SharedPrivateLinkResource.GroupID = ptr.To("")
		origin.Properties.OriginHostHeader = nil
		origin.Properties.HTTPSPort = ptr.To[int32](443)
		snapshot.origins[ref] = origin
	}
	before, err := json.Marshal(snapshot.origins[snapshot.first])
	if err != nil {
		t.Fatal(err)
	}
	// Exercise SDK deserialization rather than relying only on constructed Go values.
	var decoded armcdn.AFDOrigin
	if err := json.Unmarshal(before, &decoded); err != nil {
		t.Fatal(err)
	}
	snapshot.origins[snapshot.first] = decoded
	got, err := snapshot.observe()
	if err != nil || !got.OriginsConverged {
		t.Fatalf("equivalent Azure representation did not converge: %#v, %v", got, err)
	}
	after, err := json.Marshal(snapshot.origins[snapshot.first])
	if err != nil || string(before) != string(after) {
		t.Fatal("observation mutated its SDK input")
	}
	snapshot.origins[OriginReference{OriginGroup: otherName, Origin: otherName}] = snapshot.origins[snapshot.first]
	delete(snapshot.origins, snapshot.first)
	got, err = snapshot.observe()
	if err != nil || got.OriginsConverged || got.Origins[0].State != PrivateOriginMissing {
		t.Fatalf("unrelated approved origin satisfied missing intent: %#v, %v", got, err)
	}
	snapshot.origins = nil
	got, err = snapshot.observe()
	if err != nil || got.OriginsConverged || len(got.Origins) != 2 || len(got.PendingApprovals) != 0 {
		t.Fatalf("absent snapshot fabricated convergence or an approval request: %#v, %v", got, err)
	}
	snapshot.model.Routes = nil
	got, err = snapshot.observe()
	if err != nil || got.OriginsConverged || len(got.Origins) != 0 {
		t.Fatalf("empty intent claimed readiness: %#v, %v", got, err)
	}
}
