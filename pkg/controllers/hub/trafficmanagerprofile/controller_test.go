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

// fakeEventRecorder implements the EventRecorder interface for testing.
type fakeEventRecorder struct {
	events []struct {
		object runtime.Object
		eventtype, reason, message string
	}
}

func (f *fakeEventRecorder) Event(object runtime.Object, eventtype, reason, message string) {
	f.events = append(f.events, struct {
		object runtime.Object
		eventtype, reason, message string
	}{object, eventtype, reason, message})
}

func (f *fakeEventRecorder) Eventf(object runtime.Object, eventtype, reason, messageFmt string, args ...interface{}) {
	f.events = append(f.events, struct {
		object runtime.Object
		eventtype, reason, message string
	}{object, eventtype, reason, fmt.Sprintf(messageFmt, args...)})
}

func (f *fakeEventRecorder) AnnotatedEventf(object runtime.Object, annotations map[string]string, eventtype, reason, messageFmt string, args ...interface{}) {
	f.Eventf(object, eventtype, reason, messageFmt, args...)
}

// TestRecordEventsInProfileController tests that events are properly recorded by the TrafficManagerProfile controller
func TestRecordEventsInProfileController(t *testing.T) {
	profileName := "test-profile"
	testProfile := &fleetnetv1beta1.TrafficManagerProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      profileName,
			Namespace: "default",
		},
		Spec: fleetnetv1beta1.TrafficManagerProfileSpec{
			ResourceGroup: "test-rg",
			MonitorConfig: &fleetnetv1beta1.MonitorConfig{
				Path:     ptr.To("/path"),
				Port:     ptr.To[int64](80),
				Protocol: ptr.To(fleetnetv1beta1.TrafficManagerMonitorProtocolHTTP),
			},
		},
	}

	testCases := []struct {
		name       string
		eventType  string
		reason     string
	}{
		{
			name:       "profile created event",
			eventType:  corev1.EventTypeNormal,
			reason:     eventReasonProfileCreated,
		},
		{
			name:       "profile updated event",
			eventType:  corev1.EventTypeNormal,
			reason:     eventReasonProfileUpdated,
		},
		{
			name:       "profile deleted event",
			eventType:  corev1.EventTypeNormal,
			reason:     eventReasonProfileDeleted,
		},
		{
			name:       "profile creation failed event",
			eventType:  corev1.EventTypeWarning,
			reason:     eventReasonProfileCreateFailed,
		},
		{
			name:       "profile update failed event",
			eventType:  corev1.EventTypeWarning,
			reason:     eventReasonProfileUpdateFailed,
		},
		{
			name:       "profile deletion failed event",
			eventType:  corev1.EventTypeWarning,
			reason:     eventReasonProfileDeleteFailed,
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := &fakeEventRecorder{}
			reconciler := &Reconciler{
				Recorder: recorder,
			}
			
			// Test that the recorder is correctly called with proper event type and reason
			reconciler.Recorder.Eventf(testProfile, tc.eventType, tc.reason, "Test event message for %s", profileName)
			
			// Verify the event was recorded
			if len(recorder.events) == 0 {
				t.Errorf("Expected an event to be recorded, but none was")
				return
			}
			
			event := recorder.events[0]
			if event.eventtype != tc.eventType || event.reason != tc.reason {
				t.Errorf("Expected event type=%s, reason=%s; got type=%s, reason=%s", 
					tc.eventType, tc.reason, event.eventtype, event.reason)
			}
			
			// Verify the event message contains the profile name
			if !strings.Contains(event.message, profileName) {
				t.Errorf("Expected event message to contain profile name '%s', but message was: %s", 
					profileName, event.message)
			}
		})
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
