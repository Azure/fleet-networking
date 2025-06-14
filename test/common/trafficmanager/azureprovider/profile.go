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
	"github.com/onsi/gomega"

	"go.goms.io/fleet-networking/pkg/common/azureerrors"
)

var (
	cmpProfileOptions = cmp.Options{
		cmpopts.IgnoreFields(armtrafficmanager.Profile{}, "ID", "Name", "Type"),
		cmpopts.IgnoreFields(armtrafficmanager.MonitorConfig{}, "ProfileMonitorStatus"),                                                           // cannot predict the monitor status
		cmpopts.IgnoreFields(armtrafficmanager.EndpointProperties{}, "TargetResourceID", "EndpointLocation", "EndpointMonitorStatus", "Priority"), // cannot predict the status
		cmpopts.SortSlices(func(e1, e2 *armtrafficmanager.Endpoint) bool {
			return *e1.Name < *e2.Name
		}),
		cmpopts.SortSlices(func(c1, c2 *armtrafficmanager.MonitorConfigCustomHeadersItem) bool {
			return *c1.Name < *c2.Name
		}),
	}
)

// Validator contains the way of accessing the Azure Traffic Manager resources.
type Validator struct {
	ProfileClient  *armtrafficmanager.ProfilesClient
	EndpointClient *armtrafficmanager.EndpointsClient
	ResourceGroup  string
}

// ValidateProfile validates the traffic manager profile and returns the actual Azure traffic manager profile.
func (v *Validator) ValidateProfile(ctx context.Context, name string, want armtrafficmanager.Profile) *armtrafficmanager.Profile {
	res, err := v.ProfileClient.Get(ctx, v.ResourceGroup, name, nil)
	gomega.Expect(err).Should(gomega.Succeed(), "Failed to get the traffic manager profile")
	diff := cmp.Diff(want, res.Profile, cmpProfileOptions)
	gomega.Expect(diff).Should(gomega.BeEmpty(), "trafficManagerProfile mismatch (-want, +got) :\n%s", diff)
	return &res.Profile
}

// IsProfileDeleted validates the traffic manager profile is deleted.
func (v *Validator) IsProfileDeleted(ctx context.Context, name string) {
	_, err := v.ProfileClient.Get(ctx, v.ResourceGroup, name, nil)
	gomega.Expect(azureerrors.IsNotFound(err)).Should(gomega.BeTrue(), "trafficManagerProfile %s still exists or hit error %v", name, err)
}
