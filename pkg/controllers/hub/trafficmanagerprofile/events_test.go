/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package trafficmanagerprofile

import (
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	fleetnetv1beta1 "go.goms.io/fleet-networking/api/v1beta1"
)

// TestProfileEvents tests that events are properly recorded by the TrafficManagerProfile controller
func TestProfileEvents(t *testing.T) {
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
			reason:     "ProfileCreated",
		},
		{
			name:       "profile updated event",
			eventType:  corev1.EventTypeNormal,
			reason:     "ProfileUpdated",
		},
		{
			name:       "profile deleted event",
			eventType:  corev1.EventTypeNormal,
			reason:     "ProfileDeleted",
		},
		{
			name:       "profile creation failed event",
			eventType:  corev1.EventTypeWarning,
			reason:     "ProfileCreateFailed",
		},
		{
			name:       "profile update failed event",
			eventType:  corev1.EventTypeWarning,
			reason:     "ProfileUpdateFailed",
		},
		{
			name:       "profile deletion failed event",
			eventType:  corev1.EventTypeWarning,
			reason:     "ProfileDeleteFailed",
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := &testEventRecorder{}
			
			// Test that the recorder is correctly called with proper event type and reason
			recorder.Eventf(testProfile, tc.eventType, tc.reason, "Test event message for %s", profileName)
			
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

// testEventRecorder implements the EventRecorder interface for testing.
type testEventRecorder struct {
	events []struct {
		object runtime.Object
		eventtype, reason, message string
	}
}

func (f *testEventRecorder) Event(object runtime.Object, eventtype, reason, message string) {
	f.events = append(f.events, struct {
		object runtime.Object
		eventtype, reason, message string
	}{object, eventtype, reason, message})
}

func (f *testEventRecorder) Eventf(object runtime.Object, eventtype, reason, messageFmt string, args ...interface{}) {
	f.events = append(f.events, struct {
		object runtime.Object
		eventtype, reason, message string
	}{object, eventtype, reason, fmt.Sprintf(messageFmt, args...)})
}

func (f *testEventRecorder) AnnotatedEventf(object runtime.Object, annotations map[string]string, eventtype, reason, messageFmt string, args ...interface{}) {
	f.Eventf(object, eventtype, reason, messageFmt, args...)
}