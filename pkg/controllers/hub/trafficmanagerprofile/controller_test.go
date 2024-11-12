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

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

func TestGenerateAzureTrafficManagerProfileName(t *testing.T) {
	profile := &fleetnetv1alpha1.TrafficManagerProfile{
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

func TestCompareAzureTrafficManagerProfile(t *testing.T) {
	tests := []struct {
		name    string
		current armtrafficmanager.Profile
		want    bool
	}{
		{
			name: "Profiles are equal though current profile has some different fields from the desired",
			current: armtrafficmanager.Profile{
				ID:       ptr.To("abc"),
				Location: ptr.To("global"),
				Properties: &armtrafficmanager.ProfileProperties{
					DNSConfig: &armtrafficmanager.DNSConfig{
						RelativeName: ptr.To("namespace-name"),
					},
					MonitorConfig: &armtrafficmanager.MonitorConfig{
						IntervalInSeconds:         ptr.To[int64](30),
						Path:                      ptr.To("/path"),
						Port:                      ptr.To[int64](80),
						Protocol:                  ptr.To(armtrafficmanager.MonitorProtocolHTTP),
						TimeoutInSeconds:          ptr.To[int64](10),
						ToleratedNumberOfFailures: ptr.To[int64](3),
					},
					ProfileStatus:               ptr.To(armtrafficmanager.ProfileStatusEnabled),
					TrafficRoutingMethod:        ptr.To(armtrafficmanager.TrafficRoutingMethodWeighted),
					MaxReturn:                   ptr.To(int64(1)),
					TrafficViewEnrollmentStatus: ptr.To(armtrafficmanager.TrafficViewEnrollmentStatusDisabled),
				},
				Tags: map[string]*string{
					"tagKey":   ptr.To("tagValue"),
					"otherKey": ptr.To("otherValue"),
				},
			},
			want: true,
		},
		{
			name: "properties is nil",
			current: armtrafficmanager.Profile{
				ID: ptr.To("abc"),
			},
		},
		{
			name: "MonitorConfig is nil",
			current: armtrafficmanager.Profile{
				Properties: &armtrafficmanager.ProfileProperties{},
			},
		},
		{
			name: "ProfileStatus is nil",
			current: armtrafficmanager.Profile{
				Properties: &armtrafficmanager.ProfileProperties{
					MonitorConfig: &armtrafficmanager.MonitorConfig{},
				},
			},
		},
		{
			name: "TrafficRoutingMethod is nil",
			current: armtrafficmanager.Profile{
				Properties: &armtrafficmanager.ProfileProperties{
					MonitorConfig: &armtrafficmanager.MonitorConfig{},
					ProfileStatus: ptr.To(armtrafficmanager.ProfileStatusEnabled),
				},
			},
		},
		{
			name: "MonitorConfig.IntervalInSeconds is nil",
			current: armtrafficmanager.Profile{
				Properties: &armtrafficmanager.ProfileProperties{
					MonitorConfig:        &armtrafficmanager.MonitorConfig{},
					ProfileStatus:        ptr.To(armtrafficmanager.ProfileStatusEnabled),
					TrafficRoutingMethod: ptr.To(armtrafficmanager.TrafficRoutingMethodWeighted),
				},
			},
		},
		{
			name: "MonitorConfig.IntervalInSeconds is nil",
			current: armtrafficmanager.Profile{
				Properties: &armtrafficmanager.ProfileProperties{
					MonitorConfig:        &armtrafficmanager.MonitorConfig{},
					ProfileStatus:        ptr.To(armtrafficmanager.ProfileStatusEnabled),
					TrafficRoutingMethod: ptr.To(armtrafficmanager.TrafficRoutingMethodWeighted),
				},
			},
		},
		{
			name: "MonitorConfig.Path is nil",
			current: armtrafficmanager.Profile{
				Properties: &armtrafficmanager.ProfileProperties{
					MonitorConfig: &armtrafficmanager.MonitorConfig{
						IntervalInSeconds: ptr.To[int64](30),
					},
					ProfileStatus:        ptr.To(armtrafficmanager.ProfileStatusEnabled),
					TrafficRoutingMethod: ptr.To(armtrafficmanager.TrafficRoutingMethodWeighted),
				},
			},
		},
		{
			name: "MonitorConfig.Port is nil",
			current: armtrafficmanager.Profile{
				Properties: &armtrafficmanager.ProfileProperties{
					MonitorConfig: &armtrafficmanager.MonitorConfig{
						IntervalInSeconds: ptr.To[int64](30),
						Path:              ptr.To("/path"),
					},
					ProfileStatus:        ptr.To(armtrafficmanager.ProfileStatusEnabled),
					TrafficRoutingMethod: ptr.To(armtrafficmanager.TrafficRoutingMethodWeighted),
				},
			},
		},
		{
			name: "MonitorConfig.Protocol is nil",
			current: armtrafficmanager.Profile{
				Properties: &armtrafficmanager.ProfileProperties{
					MonitorConfig: &armtrafficmanager.MonitorConfig{
						IntervalInSeconds: ptr.To[int64](30),
						Path:              ptr.To("/path"),
						Port:              ptr.To[int64](80),
					},
					ProfileStatus:        ptr.To(armtrafficmanager.ProfileStatusEnabled),
					TrafficRoutingMethod: ptr.To(armtrafficmanager.TrafficRoutingMethodWeighted),
				},
			},
		},
		{
			name: "MonitorConfig.TimeoutInSeconds is nil",
			current: armtrafficmanager.Profile{
				Properties: &armtrafficmanager.ProfileProperties{
					MonitorConfig: &armtrafficmanager.MonitorConfig{
						IntervalInSeconds: ptr.To[int64](30),
						Path:              ptr.To("/path"),
						Port:              ptr.To[int64](80),
						Protocol:          ptr.To(armtrafficmanager.MonitorProtocolHTTP),
					},
					ProfileStatus:        ptr.To(armtrafficmanager.ProfileStatusEnabled),
					TrafficRoutingMethod: ptr.To(armtrafficmanager.TrafficRoutingMethodWeighted),
				},
			},
		},
		{
			name: "MonitorConfig.ToleratedNumberOfFailures is nil",
			current: armtrafficmanager.Profile{
				Properties: &armtrafficmanager.ProfileProperties{
					MonitorConfig: &armtrafficmanager.MonitorConfig{
						IntervalInSeconds: ptr.To[int64](30),
						Path:              ptr.To("/path"),
						Port:              ptr.To[int64](80),
						Protocol:          ptr.To(armtrafficmanager.MonitorProtocolHTTP),
						TimeoutInSeconds:  ptr.To[int64](10),
					},
					ProfileStatus:        ptr.To(armtrafficmanager.ProfileStatusEnabled),
					TrafficRoutingMethod: ptr.To(armtrafficmanager.TrafficRoutingMethodWeighted),
				},
			},
		},
		{
			name: "MonitorConfig.IntervalInSeconds is different",
			current: armtrafficmanager.Profile{
				Properties: &armtrafficmanager.ProfileProperties{
					MonitorConfig: &armtrafficmanager.MonitorConfig{
						IntervalInSeconds:         ptr.To[int64](10),
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
		},
		{
			name: "MonitorConfig.Path is different",
			current: armtrafficmanager.Profile{
				Properties: &armtrafficmanager.ProfileProperties{
					MonitorConfig: &armtrafficmanager.MonitorConfig{
						IntervalInSeconds:         ptr.To[int64](30),
						Path:                      ptr.To("/invalid-path"),
						Port:                      ptr.To[int64](80),
						Protocol:                  ptr.To(armtrafficmanager.MonitorProtocolHTTP),
						TimeoutInSeconds:          ptr.To[int64](10),
						ToleratedNumberOfFailures: ptr.To[int64](3),
					},
					ProfileStatus:        ptr.To(armtrafficmanager.ProfileStatusEnabled),
					TrafficRoutingMethod: ptr.To(armtrafficmanager.TrafficRoutingMethodWeighted),
				},
			},
		},
		{
			name: "MonitorConfig.Port is different",
			current: armtrafficmanager.Profile{
				Properties: &armtrafficmanager.ProfileProperties{
					MonitorConfig: &armtrafficmanager.MonitorConfig{
						IntervalInSeconds:         ptr.To[int64](30),
						Path:                      ptr.To("/path"),
						Port:                      ptr.To[int64](8080),
						Protocol:                  ptr.To(armtrafficmanager.MonitorProtocolHTTP),
						TimeoutInSeconds:          ptr.To[int64](10),
						ToleratedNumberOfFailures: ptr.To[int64](3),
					},
				},
			},
		},
		{
			name: "MonitorConfig.Protocol is different",
			current: armtrafficmanager.Profile{
				Properties: &armtrafficmanager.ProfileProperties{
					MonitorConfig: &armtrafficmanager.MonitorConfig{
						IntervalInSeconds:         ptr.To[int64](30),
						Path:                      ptr.To("/path"),
						Port:                      ptr.To[int64](80),
						Protocol:                  ptr.To(armtrafficmanager.MonitorProtocolHTTPS),
						TimeoutInSeconds:          ptr.To[int64](10),
						ToleratedNumberOfFailures: ptr.To[int64](3),
					},
				},
			},
		},
		{
			name: "MonitorConfig.TimeoutInSeconds is different",
			current: armtrafficmanager.Profile{
				Properties: &armtrafficmanager.ProfileProperties{
					MonitorConfig: &armtrafficmanager.MonitorConfig{
						IntervalInSeconds:         ptr.To[int64](30),
						Path:                      ptr.To("/path"),
						Port:                      ptr.To[int64](80),
						Protocol:                  ptr.To(armtrafficmanager.MonitorProtocolHTTP),
						TimeoutInSeconds:          ptr.To[int64](30),
						ToleratedNumberOfFailures: ptr.To[int64](3),
					},
				},
			},
		},
		{
			name: "MonitorConfig.ToleratedNumberOfFailures is different",
			current: armtrafficmanager.Profile{
				Properties: &armtrafficmanager.ProfileProperties{
					MonitorConfig: &armtrafficmanager.MonitorConfig{
						IntervalInSeconds:         ptr.To[int64](30),
						Path:                      ptr.To("/path"),
						Port:                      ptr.To[int64](80),
						Protocol:                  ptr.To(armtrafficmanager.MonitorProtocolHTTP),
						TimeoutInSeconds:          ptr.To[int64](10),
						ToleratedNumberOfFailures: ptr.To[int64](4),
					},
				},
			},
		},
		{
			name: "ProfileStatus is different",
			current: armtrafficmanager.Profile{
				Properties: &armtrafficmanager.ProfileProperties{
					MonitorConfig: &armtrafficmanager.MonitorConfig{
						IntervalInSeconds:         ptr.To[int64](30),
						Path:                      ptr.To("/path"),
						Port:                      ptr.To[int64](80),
						Protocol:                  ptr.To(armtrafficmanager.MonitorProtocolHTTP),
						TimeoutInSeconds:          ptr.To[int64](10),
						ToleratedNumberOfFailures: ptr.To[int64](3),
					},
					ProfileStatus: ptr.To(armtrafficmanager.ProfileStatusDisabled),
				},
			},
		},
		{
			name: "TrafficMethod is different",
			current: armtrafficmanager.Profile{
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
					TrafficRoutingMethod: ptr.To(armtrafficmanager.TrafficRoutingMethodPriority),
				},
			},
		},
		{
			name: "Tags is nil",
			current: armtrafficmanager.Profile{
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
				},
			},
		},
		{
			name: "Tag key is missing",
			current: armtrafficmanager.Profile{
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
				},
				Tags: map[string]*string{
					"otherKey": ptr.To("otherValue"),
				},
			},
		},
		{
			name: "Tag value is different",
			current: armtrafficmanager.Profile{
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
				},
				Tags: map[string]*string{
					"tagKey":   ptr.To("otherValue"),
					"otherKey": ptr.To("otherValue"),
				},
			},
		},
	}
	desired := armtrafficmanager.Profile{
		Location: ptr.To("global"),
		Properties: &armtrafficmanager.ProfileProperties{
			DNSConfig: &armtrafficmanager.DNSConfig{
				RelativeName: ptr.To("namespace-name"),
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
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := compareAzureTrafficManagerProfile(tt.current, desired); got != tt.want {
				t.Errorf("compareAzureTrafficManagerProfile() = %v, want %v", got, tt.want)
			}
		})
	}
}
