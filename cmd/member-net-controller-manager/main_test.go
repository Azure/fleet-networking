/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package main

import (
	"testing"

	"go.goms.io/fleet/pkg/utils/cloudconfig/azure"
)

func TestInitAzureNetworkClientsFeatureCombinations(t *testing.T) {
	for _, trafficManager := range []bool{false, true} {
		for _, privateOrigins := range []bool{false, true} {
			t.Run(map[bool]string{false: "no-TM", true: "TM"}[trafficManager]+"/"+map[bool]string{false: "no-PLS", true: "PLS"}[privateOrigins], func(t *testing.T) {
				config := &azure.CloudConfig{}
				config.Cloud = "AzurePublicCloud"
				config.SubscriptionID = "00000000-0000-0000-0000-000000000000"
				config.ResourceGroup = "node-rg"
				config.UseManagedIdentityExtension = true
				clients, err := initAzureNetworkClients(config, trafficManager, privateOrigins)
				if err != nil {
					t.Fatal(err)
				}
				if (clients.publicIPAddresses != nil) != trafficManager ||
					(clients.loadBalancers != nil) != privateOrigins ||
					(clients.privateLinkServices != nil) != privateOrigins {
					t.Fatalf("unexpected clients for trafficManager=%v privateOrigins=%v: %#v", trafficManager, privateOrigins, clients)
				}
			})
		}
	}
}

func TestInitAzureNetworkClientsRejectsMissingPrivateOriginResourceGroup(t *testing.T) {
	config := &azure.CloudConfig{}
	if _, err := initAzureNetworkClients(config, false, true); err == nil {
		t.Fatal("private origin discovery accepted a missing resource group")
	}
}
