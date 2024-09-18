/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package azmanager

import "testing"

func TestParsePublicIPAddressID(t *testing.T) {
	tests := []struct {
		name              string
		ipID              string
		wantResourceGroup string
		wantName          string
		wantErr           bool
	}{
		{
			name:              "valid public IP ID",
			ipID:              "/subscriptions/sub1/resourceGroups/rg/providers/Microsoft.Network/publicIPAddresses/pip",
			wantResourceGroup: "rg",
			wantName:          "pip",
		},
		{
			name:    "invalid ID",
			ipID:    "/subscription/sub1/resourceGroups/rg/providers/Microsoft.Network/publicIPAddresses/pip",
			wantErr: true,
		},
		{
			name:    "empty resource group name",
			ipID:    "/subscriptions/sub1/resourceGroups//providers/Microsoft.Network/publicIPAddresses/pip",
			wantErr: true,
		},
		{
			name:    "empty name",
			ipID:    "/subscriptions/sub1/resourceGroups/rg/providers/Microsoft.Network/publicIPAddresses/",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotResourceGroup, gotName, err := ParsePublicIPAddressID(tt.ipID)
			if got, want := err != nil, tt.wantErr; got != want {
				t.Errorf("ParsePublicIPAddressID() = error %v, want %v", got, want)
			}
			if gotResourceGroup != tt.wantResourceGroup {
				t.Errorf("ParsePublicIPAddressID() gotResourceGroup = %v, want %v", gotResourceGroup, tt.wantResourceGroup)
			}
			if gotName != tt.wantName {
				t.Errorf("ParsePublicIPAddressID() gotName = %v, want %v", gotName, tt.wantName)
			}
		})
	}
}
