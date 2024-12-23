/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package azureprovider provides utils to interact with azure traffic manager resources.
package azureprovider

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/trafficmanager/armtrafficmanager"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/gomega"
)

var (
	cmpProfileOptions = cmp.Options{
		cmpopts.IgnoreFields(armtrafficmanager.Profile{}, "ID", "Name", "Type"),
		cmpopts.IgnoreFields(armtrafficmanager.MonitorConfig{}, "ProfileMonitorStatus"), // cannot predict the monitor status
	}
)

// Validator contains the way of accessing the Azure Traffic Manager resources.
type Validator struct {
	ProfileClient  *armtrafficmanager.ProfilesClient
	EndpointClient *armtrafficmanager.EndpointsClient
	ResourceGroup  string
}

// ValidateProfile validates the traffic manager profile.
func (v *Validator) ValidateProfile(ctx context.Context, name string, want armtrafficmanager.Profile) {
	res, err := v.ProfileClient.Get(ctx, v.ResourceGroup, name, nil)
	Expect(err).Should(Succeed(), "Failed to get the traffic manager profile")
	diff := cmp.Diff(want, res.Profile, cmpProfileOptions)
	Expect(diff).Should(BeEmpty(), "trafficManagerProfile mismatch (-want, +got) :\n%s", diff)
}
