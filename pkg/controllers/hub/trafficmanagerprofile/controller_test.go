/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package trafficmanagerprofile

import (
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/trafficmanager/armtrafficmanager"
	"github.com/google/go-cmp/cmp"
	"k8s.io/utils/ptr"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

func TestConvertToTrafficManagerProfileSpec(t *testing.T) {
	tests := []struct {
		name    string
		profile *armtrafficmanager.Profile
		want    fleetnetv1alpha1.TrafficManagerProfileSpec
	}{
		{
			name:    "nil properties", // not possible in production
			profile: &armtrafficmanager.Profile{},
			want:    fleetnetv1alpha1.TrafficManagerProfileSpec{},
		},
		{
			name: "nil monitor protocol", // not possible in production
			profile: &armtrafficmanager.Profile{
				Properties: &armtrafficmanager.ProfileProperties{
					MonitorConfig: &armtrafficmanager.MonitorConfig{
						IntervalInSeconds:         ptr.To(int64(10)),
						Path:                      ptr.To("/healthz"),
						Port:                      ptr.To(int64(8080)),
						TimeoutInSeconds:          ptr.To(int64(9)),
						ToleratedNumberOfFailures: ptr.To(int64(4)),
					},
				},
			},
			want: fleetnetv1alpha1.TrafficManagerProfileSpec{
				MonitorConfig: &fleetnetv1alpha1.MonitorConfig{
					IntervalInSeconds:         ptr.To(int64(10)),
					Path:                      ptr.To("/healthz"),
					Port:                      ptr.To(int64(8080)),
					Protocol:                  (*fleetnetv1alpha1.TrafficManagerMonitorProtocol)(ptr.To("")),
					TimeoutInSeconds:          ptr.To(int64(9)),
					ToleratedNumberOfFailures: ptr.To(int64(4)),
				},
			},
		},
		{
			name: "invalid monitor protocol", // not possible in production
			profile: &armtrafficmanager.Profile{
				Properties: &armtrafficmanager.ProfileProperties{
					MonitorConfig: &armtrafficmanager.MonitorConfig{
						IntervalInSeconds:         ptr.To(int64(10)),
						Path:                      ptr.To("/healthz"),
						Port:                      ptr.To(int64(8080)),
						Protocol:                  (*armtrafficmanager.MonitorProtocol)(ptr.To("UDP")),
						TimeoutInSeconds:          ptr.To(int64(9)),
						ToleratedNumberOfFailures: ptr.To(int64(4)),
					},
				},
			},
			want: fleetnetv1alpha1.TrafficManagerProfileSpec{
				MonitorConfig: &fleetnetv1alpha1.MonitorConfig{
					IntervalInSeconds:         ptr.To(int64(10)),
					Path:                      ptr.To("/healthz"),
					Port:                      ptr.To(int64(8080)),
					Protocol:                  (*fleetnetv1alpha1.TrafficManagerMonitorProtocol)(ptr.To("UDP")),
					TimeoutInSeconds:          ptr.To(int64(9)),
					ToleratedNumberOfFailures: ptr.To(int64(4)),
				},
			},
		},
		{
			name: "valid profile", // not possible in production
			profile: &armtrafficmanager.Profile{
				Properties: &armtrafficmanager.ProfileProperties{
					MonitorConfig: &armtrafficmanager.MonitorConfig{
						IntervalInSeconds:         ptr.To(int64(10)),
						Path:                      ptr.To("/healthz"),
						Port:                      ptr.To(int64(8080)),
						Protocol:                  ptr.To(armtrafficmanager.MonitorProtocolHTTP),
						TimeoutInSeconds:          ptr.To(int64(9)),
						ToleratedNumberOfFailures: ptr.To(int64(4)),
					},
				},
			},
			want: fleetnetv1alpha1.TrafficManagerProfileSpec{
				MonitorConfig: &fleetnetv1alpha1.MonitorConfig{
					IntervalInSeconds:         ptr.To(int64(10)),
					Path:                      ptr.To("/healthz"),
					Port:                      ptr.To(int64(8080)),
					Protocol:                  ptr.To(fleetnetv1alpha1.TrafficManagerMonitorProtocolHTTP),
					TimeoutInSeconds:          ptr.To(int64(9)),
					ToleratedNumberOfFailures: ptr.To(int64(4)),
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := convertToTrafficManagerProfileSpec(tc.profile)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("convertToTrafficManagerProfileSpec() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
