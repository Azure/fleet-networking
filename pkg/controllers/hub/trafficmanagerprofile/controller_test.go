/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package trafficmanagerprofile

import (
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/trafficmanager/armtrafficmanager"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	fleetnetv1beta1 "go.goms.io/fleet-networking/api/v1beta1"
)

func TestGenerateAzureTrafficManagerProfileName(t *testing.T) {
	profile := &fleetnetv1beta1.TrafficManagerProfile{
		ObjectMeta: metav1.ObjectMeta{
			UID: "abc",
		},
	}
	want := "fleet-abc"
	got := GenerateAzureTrafficManagerProfileName(profile)
	if want != got {
		t.Errorf("GenerateAzureTrafficManagerProfileName() = %s, want %s", got, want)
	}
}

func buildDesiredProfile() armtrafficmanager.Profile {
	return armtrafficmanager.Profile{
		Location: ptr.To("global"),
		Properties: &armtrafficmanager.ProfileProperties{
			DNSConfig: &armtrafficmanager.DNSConfig{
				RelativeName: ptr.To("namespace-name"),
				TTL:          ptr.To(int64(60)),
			},
			MonitorConfig: &armtrafficmanager.MonitorConfig{
				IntervalInSeconds:         ptr.To[int64](30),
				Path:                      ptr.To("/path"),
				Port:                      ptr.To[int64](80),
				Protocol:                  ptr.To(armtrafficmanager.MonitorProtocolHTTP),
				TimeoutInSeconds:          ptr.To[int64](10),
				ToleratedNumberOfFailures: ptr.To[int64](3),
			},
			ProfileStatus:        ptr.To(armtrafficmanager.ProfileStatusEnabled),
			TrafficRoutingMethod: ptr.To(armtrafficmanager.TrafficRoutingMethodWeighted),
		},
		Tags: map[string]*string{
			"tagKey": ptr.To("tagValue"),
		},
	}
}

func TestEqualAzureTrafficManagerProfile(t *testing.T) {
	tests := []struct {
		name             string
		buildCurrentFunc func() armtrafficmanager.Profile
		want             bool
	}{
		{
			name: "Profiles are equal though buildCurrentFunc profile has some different fields from the desired",
			buildCurrentFunc: func() armtrafficmanager.Profile {
				res := buildDesiredProfile()
				res.ID = ptr.To("abc")
				res.Tags = map[string]*string{
					"tagKey":   ptr.To("tagValue"),
					"otherKey": ptr.To("otherValue"),
				}
				res.Properties.MaxReturn = ptr.To(int64(1))
				return res
			},
			want: true,
		},
		{
			name: "properties is nil",
			buildCurrentFunc: func() armtrafficmanager.Profile {
				res := buildDesiredProfile()
				res.Properties = nil
				return res
			},
		},
		{
			name: "MonitorConfig is nil",
			buildCurrentFunc: func() armtrafficmanager.Profile {
				res := buildDesiredProfile()
				res.Properties.MonitorConfig = nil
				return res
			},
		},
		{
			name: "ProfileStatus is nil",
			buildCurrentFunc: func() armtrafficmanager.Profile {
				res := buildDesiredProfile()
				res.Properties.ProfileStatus = nil
				return res
			},
		},
		{
			name: "TrafficRoutingMethod is nil",
			buildCurrentFunc: func() armtrafficmanager.Profile {
				res := buildDesiredProfile()
				res.Properties.TrafficRoutingMethod = nil
				return res
			},
		},
		{
			name: "DNSConfig is nil",
			buildCurrentFunc: func() armtrafficmanager.Profile {
				res := buildDesiredProfile()
				res.Properties.DNSConfig = nil
				return res
			},
		},
		{
			name: "MonitorConfig.IntervalInSeconds is nil",
			buildCurrentFunc: func() armtrafficmanager.Profile {
				res := buildDesiredProfile()
				res.Properties.MonitorConfig.IntervalInSeconds = nil
				return res
			},
		},
		{
			name: "MonitorConfig.Path is nil",
			buildCurrentFunc: func() armtrafficmanager.Profile {
				res := buildDesiredProfile()
				res.Properties.MonitorConfig.Path = nil
				return res
			},
		},
		{
			name: "MonitorConfig.Port is nil",
			buildCurrentFunc: func() armtrafficmanager.Profile {
				res := buildDesiredProfile()
				res.Properties.MonitorConfig.Port = nil
				return res
			},
		},
		{
			name: "MonitorConfig.Protocol is nil",
			buildCurrentFunc: func() armtrafficmanager.Profile {
				res := buildDesiredProfile()
				res.Properties.MonitorConfig.Protocol = nil
				return res
			},
		},
		{
			name: "MonitorConfig.TimeoutInSeconds is nil",
			buildCurrentFunc: func() armtrafficmanager.Profile {
				res := buildDesiredProfile()
				res.Properties.MonitorConfig.TimeoutInSeconds = nil
				return res
			},
		},
		{
			name: "MonitorConfig.ToleratedNumberOfFailures is nil",
			buildCurrentFunc: func() armtrafficmanager.Profile {
				res := buildDesiredProfile()
				res.Properties.MonitorConfig.ToleratedNumberOfFailures = nil
				return res
			},
		},
		{
			name: "MonitorConfig.IntervalInSeconds is different",
			buildCurrentFunc: func() armtrafficmanager.Profile {
				res := buildDesiredProfile()
				res.Properties.MonitorConfig.IntervalInSeconds = ptr.To[int64](10)
				return res
			},
		},
		{
			name: "MonitorConfig.Path is different",
			buildCurrentFunc: func() armtrafficmanager.Profile {
				res := buildDesiredProfile()
				res.Properties.MonitorConfig.Path = ptr.To("/invalid-path")
				return res
			},
		},
		{
			name: "MonitorConfig.Port is different",
			buildCurrentFunc: func() armtrafficmanager.Profile {
				res := buildDesiredProfile()
				res.Properties.MonitorConfig.Port = ptr.To[int64](8080)
				return res
			},
		},
		{
			name: "MonitorConfig.Protocol is different",
			buildCurrentFunc: func() armtrafficmanager.Profile {
				res := buildDesiredProfile()
				res.Properties.MonitorConfig.Protocol = ptr.To(armtrafficmanager.MonitorProtocolHTTPS)
				return res
			},
		},
		{
			name: "MonitorConfig.TimeoutInSeconds is different",
			buildCurrentFunc: func() armtrafficmanager.Profile {
				res := buildDesiredProfile()
				res.Properties.MonitorConfig.TimeoutInSeconds = ptr.To[int64](30)
				return res
			},
		},
		{
			name: "MonitorConfig.ToleratedNumberOfFailures is different",
			buildCurrentFunc: func() armtrafficmanager.Profile {
				res := buildDesiredProfile()
				res.Properties.MonitorConfig.ToleratedNumberOfFailures = ptr.To[int64](4)
				return res
			},
		},
		{
			name: "ProfileStatus is different",
			buildCurrentFunc: func() armtrafficmanager.Profile {
				res := buildDesiredProfile()
				res.Properties.ProfileStatus = ptr.To(armtrafficmanager.ProfileStatusDisabled)
				return res
			},
		},
		{
			name: "TrafficMethod is different",
			buildCurrentFunc: func() armtrafficmanager.Profile {
				res := buildDesiredProfile()
				res.Properties.TrafficRoutingMethod = ptr.To(armtrafficmanager.TrafficRoutingMethodPriority)
				return res
			},
		},
		{
			name: "DNS TTL is nil",
			buildCurrentFunc: func() armtrafficmanager.Profile {
				res := buildDesiredProfile()
				res.Properties.DNSConfig.TTL = nil
				return res
			},
		},
		{
			name: "DNS TTL is different",
			buildCurrentFunc: func() armtrafficmanager.Profile {
				res := buildDesiredProfile()
				res.Properties.DNSConfig.TTL = ptr.To(int64(10))
				return res
			},
		},
		{
			name: "Tags is nil",
			buildCurrentFunc: func() armtrafficmanager.Profile {
				res := buildDesiredProfile()
				res.Tags = nil
				return res
			},
		},
		{
			name: "Tag key is missing",
			buildCurrentFunc: func() armtrafficmanager.Profile {
				res := buildDesiredProfile()
				res.Tags = map[string]*string{
					"otherKey": ptr.To("otherValue"),
				}
				return res
			},
		},
		{
			name: "Tag value is different",
			buildCurrentFunc: func() armtrafficmanager.Profile {
				res := buildDesiredProfile()
				res.Tags["tagKey"] = ptr.To("otherValue")
				return res
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desired := buildDesiredProfile()
			if got := EqualAzureTrafficManagerProfile(tt.buildCurrentFunc(), desired); got != tt.want {
				t.Errorf("EqualAzureTrafficManagerProfile() = %v, want %v", got, tt.want)
			}
		})
	}
}
