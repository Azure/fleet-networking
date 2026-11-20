/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package conformance

import (
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v9"
	"k8s.io/utils/ptr"
)

// ApprovalConnection validates the exact correlated request; description is not authentication.
// Callers must independently verify the owned AFD profile and the companion's LB/PLS binding.
func ApprovalConnection(pls armnetwork.PrivateLinkService, target, description, checkpoint string) (*armnetwork.PrivateEndpointConnection, error) {
	if !strings.EqualFold(ptr.Deref(pls.ID, ""), target) || pls.Properties == nil ||
		ptr.Deref(pls.Properties.ProvisioningState, "") != armnetwork.ProvisioningStateSucceeded {
		return nil, fmt.Errorf("PLS identity/provisioning does not match verified target %s", target)
	}
	var result *armnetwork.PrivateEndpointConnection
	for _, connection := range pls.Properties.PrivateEndpointConnections {
		if connection == nil || connection.Properties == nil || connection.Properties.PrivateLinkServiceConnectionState == nil {
			continue
		}
		state := connection.Properties.PrivateLinkServiceConnectionState
		if ptr.Deref(state.Description, "") != description {
			continue
		}
		name := ptr.Deref(connection.Name, "")
		id := ptr.Deref(connection.ID, "")
		if result != nil || name == "" || strings.ContainsAny(name, "/\\") ||
			!strings.EqualFold(id, target+"/privateEndpointConnections/"+name) ||
			(checkpoint != "" && !strings.EqualFold(checkpoint, id)) ||
			(ptr.Deref(state.Status, "") != "Pending" && ptr.Deref(state.Status, "") != "Approved") {
			return nil, fmt.Errorf("ambiguous, changed, or invalid PLS request on %s", target)
		}
		result = connection
	}
	return result, nil
}
