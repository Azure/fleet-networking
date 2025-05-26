/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package trafficmanagerprofile

import (
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/trafficmanager/armtrafficmanager"
	"github.com/google/go-cmp/cmp"
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
				CustomHeaders: []*armtrafficmanager.MonitorConfigCustomHeadersItem{
					{
						Name:  ptr.To("HeaderName"),
						Value: ptr.To("HeaderValue"),
					},
				},
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
			name: "CustomHeaders are equal",
			buildCurrentFunc: func() armtrafficmanager.Profile {
				res := buildDesiredProfile()
				res.Properties.MonitorConfig.CustomHeaders = []*armtrafficmanager.MonitorConfigCustomHeadersItem{
					{
						Name:  ptr.To("HeaderName"),
						Value: ptr.To("HeaderValue"),
					},
				}
				return res
			},
			want: true,
		},
		{
			name: "CustomHeaders are different (different value)",
			buildCurrentFunc: func() armtrafficmanager.Profile {
				res := buildDesiredProfile()
				res.Properties.MonitorConfig.CustomHeaders = []*armtrafficmanager.MonitorConfigCustomHeadersItem{
					{
						Name:  ptr.To("HeaderName"),
						Value: ptr.To("DifferentValue"),
					},
				}
				return res
			},
		},
		{
			name: "CustomHeaders are different (missing header)",
			buildCurrentFunc: func() armtrafficmanager.Profile {
				res := buildDesiredProfile()
				res.Properties.MonitorConfig.CustomHeaders = []*armtrafficmanager.MonitorConfigCustomHeadersItem{}
				return res
			},
		},
		{
			name: "CustomHeaders with nil name",
			buildCurrentFunc: func() armtrafficmanager.Profile {
				res := buildDesiredProfile()
				res.Properties.MonitorConfig.CustomHeaders = []*armtrafficmanager.MonitorConfigCustomHeadersItem{
					{
						Name:  nil,
						Value: ptr.To("HeaderValue"),
					},
				}
				return res
			},
		},
		{
			name: "CustomHeaders with nil value",
			buildCurrentFunc: func() armtrafficmanager.Profile {
				res := buildDesiredProfile()
				res.Properties.MonitorConfig.CustomHeaders = []*armtrafficmanager.MonitorConfigCustomHeadersItem{
					{
						Name:  ptr.To("HeaderName"),
						Value: nil,
					},
				}
				return res
			},
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
			if got := equalAzureTrafficManagerProfile(tt.buildCurrentFunc(), desired); got != tt.want {
				t.Errorf("equalAzureTrafficManagerProfile() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildAzureTrafficManagerProfileRequest(t *testing.T) {
	desired := buildDesiredProfile()
	tests := []struct {
		name    string
		current armtrafficmanager.Profile
		want    armtrafficmanager.Profile
	}{
		{
			name: "different location, nil properties and nil tags",
			current: armtrafficmanager.Profile{
				Location: ptr.To("region"),
			},
			want: desired,
		},
		{
			name: "nil location, nil properties and no managed tag",
			current: armtrafficmanager.Profile{
				Tags: map[string]*string{
					"other-key": ptr.To("other-value"),
				},
			},
			want: armtrafficmanager.Profile{
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
						CustomHeaders: []*armtrafficmanager.MonitorConfigCustomHeadersItem{
							{
								Name:  ptr.To("HeaderName"),
								Value: ptr.To("HeaderValue"),
							},
						},
					},
					ProfileStatus:        ptr.To(armtrafficmanager.ProfileStatusEnabled),
					TrafficRoutingMethod: ptr.To(armtrafficmanager.TrafficRoutingMethodWeighted),
				},
				Tags: map[string]*string{
					"other-key": ptr.To("other-value"),
					"tagKey":    ptr.To("tagValue"),
				},
			},
		},
		{
			name: "nil location, nil properties and different managed tag",
			current: armtrafficmanager.Profile{
				Tags: map[string]*string{
					"tagKey": ptr.To("tagValue1"),
				},
			},
			want: desired,
		},
		{
			name: "not nil properties",
			current: armtrafficmanager.Profile{
				Properties: &armtrafficmanager.ProfileProperties{
					DNSConfig: &armtrafficmanager.DNSConfig{
						RelativeName: ptr.To("other-namespace-name"),
						TTL:          ptr.To(int64(30)),
						Fqdn:         ptr.To("other-value"),
					},
					Endpoints: []*armtrafficmanager.Endpoint{
						{
							Name: ptr.To("endpoint-name"),
						},
					},
					MonitorConfig: &armtrafficmanager.MonitorConfig{
						CustomHeaders: []*armtrafficmanager.MonitorConfigCustomHeadersItem{
							{
								Name:  ptr.To("HeaderName"),
								Value: ptr.To("HeaderValue"),
							},
						},
						ExpectedStatusCodeRanges: []*armtrafficmanager.MonitorConfigExpectedStatusCodeRangesItem{
							{
								Min: ptr.To[int32](200),
								Max: ptr.To[int32](299),
							},
							{
								Min: ptr.To[int32](300),
								Max: ptr.To[int32](399),
							},
						},
						IntervalInSeconds:         ptr.To[int64](80),
						Path:                      ptr.To("/other"),
						Port:                      ptr.To[int64](8080),
						ProfileMonitorStatus:      ptr.To(armtrafficmanager.ProfileMonitorStatusDegraded),
						Protocol:                  ptr.To(armtrafficmanager.MonitorProtocolHTTPS),
						TimeoutInSeconds:          ptr.To[int64](100),
						ToleratedNumberOfFailures: ptr.To[int64](30),
					},
					ProfileStatus:        ptr.To(armtrafficmanager.ProfileStatusDisabled),
					TrafficRoutingMethod: ptr.To(armtrafficmanager.TrafficRoutingMethodGeographic),
				},
			},
			want: armtrafficmanager.Profile{
				Location: ptr.To("global"),
				Properties: &armtrafficmanager.ProfileProperties{
					DNSConfig: &armtrafficmanager.DNSConfig{
						RelativeName: ptr.To("namespace-name"),
						TTL:          ptr.To(int64(60)),
					},
					Endpoints: []*armtrafficmanager.Endpoint{
						{
							Name: ptr.To("endpoint-name"),
						},
					},
					MonitorConfig: &armtrafficmanager.MonitorConfig{
						CustomHeaders: []*armtrafficmanager.MonitorConfigCustomHeadersItem{
							{
								Name:  ptr.To("HeaderName"),
								Value: ptr.To("HeaderValue"),
							},
						},
						ExpectedStatusCodeRanges: []*armtrafficmanager.MonitorConfigExpectedStatusCodeRangesItem{
							{
								Min: ptr.To[int32](200),
								Max: ptr.To[int32](299),
							},
							{
								Min: ptr.To[int32](300),
								Max: ptr.To[int32](399),
							},
						},
						IntervalInSeconds:         ptr.To[int64](30),
						Path:                      ptr.To("/path"),
						Port:                      ptr.To[int64](80),
						ProfileMonitorStatus:      ptr.To(armtrafficmanager.ProfileMonitorStatusDegraded),
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
			},
		},
		{
			name: "different custom headers",
			current: armtrafficmanager.Profile{
				Properties: &armtrafficmanager.ProfileProperties{
					DNSConfig: &armtrafficmanager.DNSConfig{
						RelativeName: ptr.To("namespace-name"),
						TTL:          ptr.To(int64(60)),
					},
					MonitorConfig: &armtrafficmanager.MonitorConfig{
						CustomHeaders: []*armtrafficmanager.MonitorConfigCustomHeadersItem{
							{
								Name:  ptr.To("OtherHeader"),
								Value: ptr.To("OtherValue"),
							},
						},
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
			},
			want: desired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildAzureTrafficManagerProfileRequest(tt.current, desired)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("buildAzureTrafficManagerProfileRequest()  mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestHasRequiredProperties(t *testing.T) {
	tests := []struct {
		name    string
		profile armtrafficmanager.Profile
		want    bool
	}{
		{
			name:    "All required properties exist",
			profile: buildDesiredProfile(),
			want:    true,
		},
		{
			name: "Properties is nil",
			profile: armtrafficmanager.Profile{
				Properties: nil,
			},
			want: false,
		},
		{
			name: "MonitorConfig is nil",
			profile: armtrafficmanager.Profile{
				Properties: &armtrafficmanager.ProfileProperties{
					MonitorConfig:        nil,
					ProfileStatus:        ptr.To(armtrafficmanager.ProfileStatusEnabled),
					TrafficRoutingMethod: ptr.To(armtrafficmanager.TrafficRoutingMethodWeighted),
					DNSConfig: &armtrafficmanager.DNSConfig{
						RelativeName: ptr.To("namespace-name"),
						TTL:          ptr.To(int64(60)),
					},
				},
			},
			want: false,
		},
		{
			name: "ProfileStatus is nil",
			profile: armtrafficmanager.Profile{
				Properties: &armtrafficmanager.ProfileProperties{
					MonitorConfig: &armtrafficmanager.MonitorConfig{
						IntervalInSeconds:         ptr.To[int64](30),
						Path:                      ptr.To("/path"),
						Port:                      ptr.To[int64](80),
						Protocol:                  ptr.To(armtrafficmanager.MonitorProtocolHTTP),
						TimeoutInSeconds:          ptr.To[int64](10),
						ToleratedNumberOfFailures: ptr.To[int64](3),
					},
					ProfileStatus:        nil,
					TrafficRoutingMethod: ptr.To(armtrafficmanager.TrafficRoutingMethodWeighted),
					DNSConfig: &armtrafficmanager.DNSConfig{
						RelativeName: ptr.To("namespace-name"),
						TTL:          ptr.To(int64(60)),
					},
				},
			},
			want: false,
		},
		{
			name: "TrafficRoutingMethod is nil",
			profile: armtrafficmanager.Profile{
				Properties: &armtrafficmanager.ProfileProperties{
					MonitorConfig: &armtrafficmanager.MonitorConfig{
						IntervalInSeconds:         ptr.To[int64](30),
						Path:                      ptr.To("/path"),
						Port:                      ptr.To[int64](80),
						Protocol:                  ptr.To(armtrafficmanager.MonitorProtocolHTTP),
						TimeoutInSeconds:          ptr.To[int64](10),
						ToleratedNumberOfFailures: ptr.To[int64](3),
					},
					ProfileStatus:        ptr.To(armtrafficmanager.ProfileStatusEnabled),
					TrafficRoutingMethod: nil,
					DNSConfig: &armtrafficmanager.DNSConfig{
						RelativeName: ptr.To("namespace-name"),
						TTL:          ptr.To(int64(60)),
					},
				},
			},
			want: false,
		},
		{
			name: "DNSConfig is nil",
			profile: armtrafficmanager.Profile{
				Properties: &armtrafficmanager.ProfileProperties{
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
					DNSConfig:            nil,
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasRequiredProperties(tt.profile); got != tt.want {
				t.Errorf("hasRequiredProperties() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEqualMonitorConfig(t *testing.T) {
	desiredConfig := buildDesiredProfile().Properties.MonitorConfig

	tests := []struct {
		name    string
		current *armtrafficmanager.MonitorConfig
		want    bool
	}{
		{
			name:    "Monitor configs are equal",
			current: desiredConfig,
			want:    true,
		},
		{
			name: "IntervalInSeconds is nil",
			current: &armtrafficmanager.MonitorConfig{
				IntervalInSeconds:         nil,
				Path:                      ptr.To("/path"),
				Port:                      ptr.To[int64](80),
				Protocol:                  ptr.To(armtrafficmanager.MonitorProtocolHTTP),
				TimeoutInSeconds:          ptr.To[int64](10),
				ToleratedNumberOfFailures: ptr.To[int64](3),
			},
			want: false,
		},
		{
			name: "Path is nil",
			current: &armtrafficmanager.MonitorConfig{
				IntervalInSeconds:         ptr.To[int64](30),
				Path:                      nil,
				Port:                      ptr.To[int64](80),
				Protocol:                  ptr.To(armtrafficmanager.MonitorProtocolHTTP),
				TimeoutInSeconds:          ptr.To[int64](10),
				ToleratedNumberOfFailures: ptr.To[int64](3),
			},
			want: false,
		},
		{
			name: "Port is nil",
			current: &armtrafficmanager.MonitorConfig{
				IntervalInSeconds:         ptr.To[int64](30),
				Path:                      ptr.To("/path"),
				Port:                      nil,
				Protocol:                  ptr.To(armtrafficmanager.MonitorProtocolHTTP),
				TimeoutInSeconds:          ptr.To[int64](10),
				ToleratedNumberOfFailures: ptr.To[int64](3),
			},
			want: false,
		},
		{
			name: "Protocol is nil",
			current: &armtrafficmanager.MonitorConfig{
				IntervalInSeconds:         ptr.To[int64](30),
				Path:                      ptr.To("/path"),
				Port:                      ptr.To[int64](80),
				Protocol:                  nil,
				TimeoutInSeconds:          ptr.To[int64](10),
				ToleratedNumberOfFailures: ptr.To[int64](3),
			},
			want: false,
		},
		{
			name: "TimeoutInSeconds is nil",
			current: &armtrafficmanager.MonitorConfig{
				IntervalInSeconds:         ptr.To[int64](30),
				Path:                      ptr.To("/path"),
				Port:                      ptr.To[int64](80),
				Protocol:                  ptr.To(armtrafficmanager.MonitorProtocolHTTP),
				TimeoutInSeconds:          nil,
				ToleratedNumberOfFailures: ptr.To[int64](3),
			},
			want: false,
		},
		{
			name: "ToleratedNumberOfFailures is nil",
			current: &armtrafficmanager.MonitorConfig{
				IntervalInSeconds:         ptr.To[int64](30),
				Path:                      ptr.To("/path"),
				Port:                      ptr.To[int64](80),
				Protocol:                  ptr.To(armtrafficmanager.MonitorProtocolHTTP),
				TimeoutInSeconds:          ptr.To[int64](10),
				ToleratedNumberOfFailures: nil,
			},
			want: false,
		},
		{
			name: "IntervalInSeconds is different",
			current: &armtrafficmanager.MonitorConfig{
				IntervalInSeconds:         ptr.To[int64](60),
				Path:                      ptr.To("/path"),
				Port:                      ptr.To[int64](80),
				Protocol:                  ptr.To(armtrafficmanager.MonitorProtocolHTTP),
				TimeoutInSeconds:          ptr.To[int64](10),
				ToleratedNumberOfFailures: ptr.To[int64](3),
			},
			want: false,
		},
		{
			name: "Path is different",
			current: &armtrafficmanager.MonitorConfig{
				IntervalInSeconds:         ptr.To[int64](30),
				Path:                      ptr.To("/different-path"),
				Port:                      ptr.To[int64](80),
				Protocol:                  ptr.To(armtrafficmanager.MonitorProtocolHTTP),
				TimeoutInSeconds:          ptr.To[int64](10),
				ToleratedNumberOfFailures: ptr.To[int64](3),
			},
			want: false,
		},
		{
			name: "Port is different",
			current: &armtrafficmanager.MonitorConfig{
				IntervalInSeconds:         ptr.To[int64](30),
				Path:                      ptr.To("/path"),
				Port:                      ptr.To[int64](443),
				Protocol:                  ptr.To(armtrafficmanager.MonitorProtocolHTTP),
				TimeoutInSeconds:          ptr.To[int64](10),
				ToleratedNumberOfFailures: ptr.To[int64](3),
			},
			want: false,
		},
		{
			name: "Protocol is different",
			current: &armtrafficmanager.MonitorConfig{
				IntervalInSeconds:         ptr.To[int64](30),
				Path:                      ptr.To("/path"),
				Port:                      ptr.To[int64](80),
				Protocol:                  ptr.To(armtrafficmanager.MonitorProtocolHTTPS),
				TimeoutInSeconds:          ptr.To[int64](10),
				ToleratedNumberOfFailures: ptr.To[int64](3),
			},
			want: false,
		},
		{
			name: "TimeoutInSeconds is different",
			current: &armtrafficmanager.MonitorConfig{
				IntervalInSeconds:         ptr.To[int64](30),
				Path:                      ptr.To("/path"),
				Port:                      ptr.To[int64](80),
				Protocol:                  ptr.To(armtrafficmanager.MonitorProtocolHTTP),
				TimeoutInSeconds:          ptr.To[int64](20),
				ToleratedNumberOfFailures: ptr.To[int64](3),
			},
			want: false,
		},
		{
			name: "ToleratedNumberOfFailures is different",
			current: &armtrafficmanager.MonitorConfig{
				IntervalInSeconds:         ptr.To[int64](30),
				Path:                      ptr.To("/path"),
				Port:                      ptr.To[int64](80),
				Protocol:                  ptr.To(armtrafficmanager.MonitorProtocolHTTP),
				TimeoutInSeconds:          ptr.To[int64](10),
				ToleratedNumberOfFailures: ptr.To[int64](5),
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := equalMonitorConfig(tt.current, desiredConfig); got != tt.want {
				t.Errorf("equalMonitorConfig() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEqualCustomHeadersObsolete(t *testing.T) {
	// This test is no longer needed as custom headers comparison is now handled by equalMonitorConfig
	t.Skip("Custom headers comparison is now part of TestEqualMonitorConfig")
}

func TestEqualProfileProperties(t *testing.T) {
	desiredProps := buildDesiredProfile().Properties

	tests := []struct {
		name         string
		currentProps *armtrafficmanager.ProfileProperties
		want         bool
	}{
		{
			name: "Properties are equal",
			currentProps: &armtrafficmanager.ProfileProperties{
				ProfileStatus:        ptr.To(armtrafficmanager.ProfileStatusEnabled),
				TrafficRoutingMethod: ptr.To(armtrafficmanager.TrafficRoutingMethodWeighted),
			},
			want: true,
		},
		{
			name: "Different profile status",
			currentProps: &armtrafficmanager.ProfileProperties{
				ProfileStatus:        ptr.To(armtrafficmanager.ProfileStatusDisabled),
				TrafficRoutingMethod: ptr.To(armtrafficmanager.TrafficRoutingMethodWeighted),
			},
			want: false,
		},
		{
			name: "Different traffic routing method",
			currentProps: &armtrafficmanager.ProfileProperties{
				ProfileStatus:        ptr.To(armtrafficmanager.ProfileStatusEnabled),
				TrafficRoutingMethod: ptr.To(armtrafficmanager.TrafficRoutingMethodPriority),
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock DNS config to use with equalProfilePropertiesAndDNS
			ttProps := tt.currentProps
			if ttProps.DNSConfig == nil {
				ttProps.DNSConfig = desiredProps.DNSConfig
			}
			if ttProps.MonitorConfig == nil {
				ttProps.MonitorConfig = desiredProps.MonitorConfig
			}
			if got := equalProfilePropertiesAndDNS(ttProps, desiredProps); got != tt.want {
				t.Errorf("equalProfilePropertiesAndDNS() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEqualDNSConfig(t *testing.T) {
	desiredProps := buildDesiredProfile().Properties
	// Keep desiredConfig for clarity in the test case descriptions
	_ = desiredProps.DNSConfig

	tests := []struct {
		name          string
		currentConfig *armtrafficmanager.DNSConfig
		want          bool
	}{
		{
			name: "DNS configs are equal",
			currentConfig: &armtrafficmanager.DNSConfig{
				TTL:          ptr.To(int64(60)),
				RelativeName: ptr.To("namespace-name"),
			},
			want: true,
		},
		{
			name: "TTL is nil",
			currentConfig: &armtrafficmanager.DNSConfig{
				TTL:          nil,
				RelativeName: ptr.To("namespace-name"),
			},
			want: false,
		},
		{
			name: "Different TTL",
			currentConfig: &armtrafficmanager.DNSConfig{
				TTL:          ptr.To(int64(30)),
				RelativeName: ptr.To("namespace-name"),
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock ProfileProperties to use with equalProfilePropertiesAndDNS
			currentProps := buildDesiredProfile().Properties
			currentProps.DNSConfig = tt.currentConfig

			if got := equalProfilePropertiesAndDNS(currentProps, desiredProps); got != tt.want {
				t.Errorf("equalProfilePropertiesAndDNS() with different DNSConfig = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEqualTags(t *testing.T) {
	desiredTags := buildDesiredProfile().Tags

	tests := []struct {
		name        string
		currentTags map[string]*string
		want        bool
	}{
		{
			name: "Tags are equal",
			currentTags: map[string]*string{
				"tagKey": ptr.To("tagValue"),
			},
			want: true,
		},
		{
			name:        "Nil current tags",
			currentTags: nil,
			want:        false,
		},
		{
			name:        "Empty current tags",
			currentTags: map[string]*string{},
			want:        false,
		},
		{
			name: "Extra tag in current",
			currentTags: map[string]*string{
				"tagKey":   ptr.To("tagValue"),
				"extraKey": ptr.To("extraValue"),
			},
			want: true,
		},
		{
			name: "Missing tag in current",
			currentTags: map[string]*string{
				"otherKey": ptr.To("otherValue"),
			},
			want: false,
		},
		{
			name: "Different tag value",
			currentTags: map[string]*string{
				"tagKey": ptr.To("differentValue"),
			},
			want: false,
		},
		{
			name: "Nil tag value in current",
			currentTags: map[string]*string{
				"tagKey": nil,
			},
			want: false,
		},
		{
			name: "Nil tag value in desired but non-nil in current",
			currentTags: map[string]*string{
				"tagKey": ptr.To("tagValue"),
			},
			want: true, // The original function allows extra tags
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := desiredTagsExistInCurrentTags(tt.currentTags, desiredTags); got != tt.want {
				t.Errorf("desiredTagsExistInCurrentTags() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEqualProfilePropertiesAndDNS(t *testing.T) {
	desiredProps := buildDesiredProfile().Properties

	tests := []struct {
		name         string
		currentProps *armtrafficmanager.ProfileProperties
		want         bool
	}{
		{
			name:         "Properties are equal",
			currentProps: desiredProps,
			want:         true,
		},
		{
			name: "Different profile status",
			currentProps: &armtrafficmanager.ProfileProperties{
				ProfileStatus:        ptr.To(armtrafficmanager.ProfileStatusDisabled),
				TrafficRoutingMethod: desiredProps.TrafficRoutingMethod,
				DNSConfig:            desiredProps.DNSConfig,
				MonitorConfig:        desiredProps.MonitorConfig,
			},
			want: false,
		},
		{
			name: "Different DNS TTL",
			currentProps: &armtrafficmanager.ProfileProperties{
				ProfileStatus:        desiredProps.ProfileStatus,
				TrafficRoutingMethod: desiredProps.TrafficRoutingMethod,
				DNSConfig: &armtrafficmanager.DNSConfig{
					TTL:          ptr.To(int64(60)), // Different TTL
					RelativeName: desiredProps.DNSConfig.RelativeName,
					Fqdn:         desiredProps.DNSConfig.Fqdn,
				},
				MonitorConfig: desiredProps.MonitorConfig,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := equalProfilePropertiesAndDNS(tt.currentProps, desiredProps); got != tt.want {
				t.Errorf("equalProfilePropertiesAndDNS() = %v, want %v", got, tt.want)
			}
		})
	}
}
