/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package conformance

import (
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v9"
	"k8s.io/utils/ptr"
)

func TestApprovalConnectionRejectsForeignOrChangedRequests(t *testing.T) {
	const target = "/subscriptions/test/resourceGroups/node/providers/Microsoft.Network/privateLinkServices/companion"
	connection := &armnetwork.PrivateEndpointConnection{
		Name: ptr.To("connection"), ID: ptr.To(target + "/privateEndpointConnections/connection"),
		Properties: &armnetwork.PrivateEndpointConnectionProperties{
			PrivateLinkServiceConnectionState: &armnetwork.PrivateLinkServiceConnectionState{
				Description: ptr.To("owned-profile"), Status: ptr.To("Pending"),
			},
		},
	}
	pls := armnetwork.PrivateLinkService{ID: ptr.To(target), Properties: &armnetwork.PrivateLinkServiceProperties{
		ProvisioningState:          ptr.To(armnetwork.ProvisioningStateSucceeded),
		PrivateEndpointConnections: []*armnetwork.PrivateEndpointConnection{connection},
	}}
	got, err := ApprovalConnection(pls, target, "owned-profile", "")
	if err != nil || got != connection {
		t.Fatalf("valid exact request rejected: %v", err)
	}
	if _, err := ApprovalConnection(pls, target, "owned-profile", target+"/privateEndpointConnections/old"); err == nil {
		t.Fatal("replacement connection accepted")
	}
	if got, err := ApprovalConnection(pls, target, "foreign-profile", ""); err != nil || got != nil {
		t.Fatalf("foreign request selected: %v", err)
	}
	pls.Properties.PrivateEndpointConnections = append(pls.Properties.PrivateEndpointConnections, connection)
	if _, err := ApprovalConnection(pls, target, "owned-profile", ""); err == nil {
		t.Fatal("duplicate correlated requests accepted")
	}
}
