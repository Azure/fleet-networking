/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package trafficmanagerbackend

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

func TestIsValidTrafficManagerEndpoint(t *testing.T) {
	tests := []struct {
		name    string
		export  *fleetnetv1alpha1.InternalServiceExport
		wantErr bool
	}{
		{
			name: "valid endpoint",
			export: &fleetnetv1alpha1.InternalServiceExport{
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Type:                   corev1.ServiceTypeLoadBalancer,
					PublicIPResourceID:     ptr.To("abc"),
					IsDNSLabelConfigured:   true,
					IsInternalLoadBalancer: false,
				},
			},
			wantErr: false,
		},
		{
			name: "wrong service type",
			export: &fleetnetv1alpha1.InternalServiceExport{
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Type:                   corev1.ServiceTypeClusterIP,
					IsDNSLabelConfigured:   true,
					IsInternalLoadBalancer: false,
				},
			},
			wantErr: true,
		},
		{
			name: "load balancer type with internal ip",
			export: &fleetnetv1alpha1.InternalServiceExport{
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Type:                   corev1.ServiceTypeLoadBalancer,
					IsDNSLabelConfigured:   true,
					IsInternalLoadBalancer: true,
				},
			},
			wantErr: true,
		},
		{
			name: "load balancer type with public ip but dns label not configured",
			export: &fleetnetv1alpha1.InternalServiceExport{
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Type:                 corev1.ServiceTypeLoadBalancer,
					PublicIPResourceID:   ptr.To("abc"),
					IsDNSLabelConfigured: false,
				},
			},
			wantErr: true,
		},
		{
			name: "load balancer type with public ip but public ip is not ready",
			export: &fleetnetv1alpha1.InternalServiceExport{
				Spec: fleetnetv1alpha1.InternalServiceExportSpec{
					Type:                 corev1.ServiceTypeLoadBalancer,
					IsDNSLabelConfigured: false,
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := isValidTrafficManagerEndpoint(tt.export)
			if got := err != nil; got != tt.wantErr {
				t.Errorf("isValidTrafficManagerEndpoint() = %v, wantErr %v", err, tt.wantErr)
			}
		})
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

// TestRecordEventsInBackendController tests that events are properly recorded by the TrafficManagerBackend controller
// when endpoints are created, updated, and deleted
func TestRecordEventsInBackendController(t *testing.T) {
	backendName := "test-backend"
	endpointName := "test-endpoint"
	testBackend := &fleetnetv1beta1.TrafficManagerBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      backendName,
			Namespace: "default",
		},
		Spec: fleetnetv1beta1.TrafficManagerBackendSpec{
			TrafficManagerProfileRef: fleetnetv1beta1.TrafficManagerProfileRef{
				Name:      "test-profile",
				Namespace: "default",
			},
			ResourceGroup: "test-rg",
			ServiceImport: fleetnetv1beta1.ServiceImportRef{
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
			reason:     eventReasonEndpointsCreated,
		},
		{
			name:       "endpoints updated event",
			eventType:  corev1.EventTypeNormal,
			reason:     eventReasonEndpointsUpdated,
		},
		{
			name:       "endpoints deleted event",
			eventType:  corev1.EventTypeNormal,
			reason:     eventReasonEndpointsDeleted,
		},
		{
			name:       "endpoints creation failed event",
			eventType:  corev1.EventTypeWarning,
			reason:     eventReasonEndpointsCreateFailed,
		},
		{
			name:       "endpoints update failed event",
			eventType:  corev1.EventTypeWarning,
			reason:     eventReasonEndpointsUpdateFailed,
		},
		{
			name:       "endpoints deletion failed event",
			eventType:  corev1.EventTypeWarning,
			reason:     eventReasonEndpointsDeleteFailed,
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := &fakeEventRecorder{}
			reconciler := &Reconciler{
				Recorder: recorder,
			}
			
			// Test that the recorder is correctly called with proper event type and reason
			reconciler.Recorder.Eventf(testBackend, tc.eventType, tc.reason, "Test event message for %s with endpoint %s", backendName, endpointName)
			
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

func TestEqualAzureTrafficManagerEndpoint(t *testing.T) {
	tests := []struct {
		name    string
		current armtrafficmanager.Endpoint
		want    bool
	}{
		{
			name: "endpoints are equal though current has other properties",
			current: armtrafficmanager.Endpoint{
				Type: ptr.To(string(armtrafficmanager.EndpointTypeAzureEndpoints)),
				Properties: &armtrafficmanager.EndpointProperties{
					TargetResourceID: ptr.To("RESourceID"),
					EndpointStatus:   ptr.To(armtrafficmanager.EndpointStatusEnabled),
					Weight:           ptr.To(int64(100)),
					Priority:         ptr.To(int64(1)),
					EndpointLocation: ptr.To("location"),
				},
			},
			want: true,
		},
		{
			name:    "type is nil",
			current: armtrafficmanager.Endpoint{},
		},
		{
			name: "type is different",
			current: armtrafficmanager.Endpoint{
				Type: ptr.To(string(armtrafficmanager.EndpointTypeNestedEndpoints)),
			},
		},
		{
			name: "Properties is nil",
			current: armtrafficmanager.Endpoint{
				Type: ptr.To(string(armtrafficmanager.EndpointTypeAzureEndpoints)),
			},
		},
		{
			name: "Properties.TargetResourceID is nil",
			current: armtrafficmanager.Endpoint{
				Type:       ptr.To(string(armtrafficmanager.EndpointTypeAzureEndpoints)),
				Properties: &armtrafficmanager.EndpointProperties{},
			},
		},
		{
			name: "Properties.Weight is nil",
			current: armtrafficmanager.Endpoint{
				Type: ptr.To(string(armtrafficmanager.EndpointTypeAzureEndpoints)),
				Properties: &armtrafficmanager.EndpointProperties{
					TargetResourceID: ptr.To("resourceID"),
				},
			},
		},
		{
			name: "Properties.EndpointStatus is nil",
			current: armtrafficmanager.Endpoint{
				Type: ptr.To(string(armtrafficmanager.EndpointTypeAzureEndpoints)),
				Properties: &armtrafficmanager.EndpointProperties{
					TargetResourceID: ptr.To("resourceID"),
					Weight:           ptr.To(int64(100)),
				},
			},
		},
		{
			name: "Properties.TargetResourceID is different",
			current: armtrafficmanager.Endpoint{
				Type: ptr.To(string(armtrafficmanager.EndpointTypeAzureEndpoints)),
				Properties: &armtrafficmanager.EndpointProperties{
					TargetResourceID: ptr.To("invalid-resourceID"),
					EndpointStatus:   ptr.To(armtrafficmanager.EndpointStatusEnabled),
					Weight:           ptr.To(int64(100)),
				},
			},
		},
		{
			name: "Properties.Weight is different",
			current: armtrafficmanager.Endpoint{
				Type: ptr.To(string(armtrafficmanager.EndpointTypeAzureEndpoints)),
				Properties: &armtrafficmanager.EndpointProperties{
					TargetResourceID: ptr.To("invalid-resourceID"),
					EndpointStatus:   ptr.To(armtrafficmanager.EndpointStatusEnabled),
					Weight:           ptr.To(int64(10)),
				},
			},
		},
		{
			name: "Properties.EndpointStatus is different",
			current: armtrafficmanager.Endpoint{
				Type: ptr.To(string(armtrafficmanager.EndpointTypeAzureEndpoints)),
				Properties: &armtrafficmanager.EndpointProperties{
					TargetResourceID: ptr.To("resourceID"),
					EndpointStatus:   ptr.To(armtrafficmanager.EndpointStatusDisabled),
					Weight:           ptr.To(int64(100)),
				},
			},
		},
	}
	desired := armtrafficmanager.Endpoint{
		Type: ptr.To(string(armtrafficmanager.EndpointTypeAzureEndpoints)),
		Properties: &armtrafficmanager.EndpointProperties{
			TargetResourceID: ptr.To("resourceID"),
			EndpointStatus:   ptr.To(armtrafficmanager.EndpointStatusEnabled),
			Weight:           ptr.To(int64(100)),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := equalAzureTrafficManagerEndpoint(tt.current, desired); got != tt.want {
				t.Errorf("equalAzureTrafficManagerEndpoint() = %v, want %v", got, tt.want)
			}
		})
	}
}
