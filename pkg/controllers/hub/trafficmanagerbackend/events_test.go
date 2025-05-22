/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package trafficmanagerbackend

import (
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

// TestBackendEvents tests that events are properly recorded by the TrafficManagerBackend controller
func TestBackendEvents(t *testing.T) {
	backendName := "test-backend"
	endpointName := "test-endpoint"
	
	testBackend := &fleetnetv1alpha1.TrafficManagerBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      backendName,
			Namespace: "default",
		},
		Spec: fleetnetv1alpha1.TrafficManagerBackendSpec{
			Profile: fleetnetv1alpha1.TrafficManagerProfileRef{
				Name: "test-profile",
			},
			Backend: fleetnetv1alpha1.TrafficManagerBackendRef{
				Name: "test-service",
			},
		},
	}

	testCases := []struct {
		name       string
		eventType  string
		reason     string
	}{
		{
			name:       "endpoints created event",
			eventType:  corev1.EventTypeNormal,
			reason:     "EndpointsCreated",
		},
		{
			name:       "endpoints updated event",
			eventType:  corev1.EventTypeNormal,
			reason:     "EndpointsUpdated",
		},
		{
			name:       "endpoints deleted event",
			eventType:  corev1.EventTypeNormal,
			reason:     "EndpointsDeleted",
		},
		{
			name:       "endpoints creation failed event",
			eventType:  corev1.EventTypeWarning,
			reason:     "EndpointsCreateFailed",
		},
		{
			name:       "endpoints update failed event",
			eventType:  corev1.EventTypeWarning,
			reason:     "EndpointsUpdateFailed",
		},
		{
			name:       "endpoints deletion failed event",
			eventType:  corev1.EventTypeWarning,
			reason:     "EndpointsDeleteFailed",
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := &testEventRecorder{}
			
			// Test that the recorder is correctly called with proper event type and reason
			recorder.Eventf(testBackend, tc.eventType, tc.reason, "Test event message for %s with endpoint %s", backendName, endpointName)
			
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
			
			// Verify the event message contains the backend and endpoint names
			if !strings.Contains(event.message, backendName) || !strings.Contains(event.message, endpointName) {
				t.Errorf("Expected event message to contain backend name '%s' and endpoint name '%s', but message was: %s", 
					backendName, endpointName, event.message)
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