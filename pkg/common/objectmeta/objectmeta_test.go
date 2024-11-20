/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package objectmeta

import "testing"

func TestAzureTrafficManagerProfileTagKey(t *testing.T) {
	want := "networking.fleet.azure.com.trafficManagerProfile"
	if got := AzureTrafficManagerProfileTagKey; got != want {
		t.Errorf("AzureTrafficManagerProfileTagKey = %v, want %v", got, want)
	}
}
