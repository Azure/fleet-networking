/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package defaulter

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/utils/ptr"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

func TestSetTrafficManagerProfile(t *testing.T) {
	tests := []struct {
		name string
		obj  *fleetnetv1alpha1.TrafficManagerProfile
		want *fleetnetv1alpha1.TrafficManagerProfile
	}{
		{
			name: "TrafficManagerProfile with nil spec",
			obj: &fleetnetv1alpha1.TrafficManagerProfile{
				Spec: fleetnetv1alpha1.TrafficManagerProfileSpec{},
			},
			want: &fleetnetv1alpha1.TrafficManagerProfile{
				Spec: fleetnetv1alpha1.TrafficManagerProfileSpec{
					MonitorConfig: &fleetnetv1alpha1.MonitorConfig{
						IntervalInSeconds:         ptr.To(int64(30)),
						Path:                      ptr.To("/"),
						Port:                      ptr.To(int64(80)),
						Protocol:                  ptr.To(fleetnetv1alpha1.TrafficManagerMonitorProtocolHTTP),
						TimeoutInSeconds:          ptr.To(int64(10)),
						ToleratedNumberOfFailures: ptr.To(int64(3)),
					},
				},
			},
		},
		{
			name: "TrafficManagerProfile with 10 IntervalInSeconds and nil TimeoutInSeconds",
			obj: &fleetnetv1alpha1.TrafficManagerProfile{
				Spec: fleetnetv1alpha1.TrafficManagerProfileSpec{
					MonitorConfig: &fleetnetv1alpha1.MonitorConfig{
						IntervalInSeconds: ptr.To(int64(10)),
					},
				},
			},
			want: &fleetnetv1alpha1.TrafficManagerProfile{
				Spec: fleetnetv1alpha1.TrafficManagerProfileSpec{
					MonitorConfig: &fleetnetv1alpha1.MonitorConfig{
						IntervalInSeconds:         ptr.To(int64(10)),
						Path:                      ptr.To("/"),
						Port:                      ptr.To(int64(80)),
						Protocol:                  ptr.To(fleetnetv1alpha1.TrafficManagerMonitorProtocolHTTP),
						TimeoutInSeconds:          ptr.To(int64(9)),
						ToleratedNumberOfFailures: ptr.To(int64(3)),
					},
				},
			},
		},
		{
			name: "TrafficManagerProfile with values",
			obj: &fleetnetv1alpha1.TrafficManagerProfile{
				Spec: fleetnetv1alpha1.TrafficManagerProfileSpec{
					MonitorConfig: &fleetnetv1alpha1.MonitorConfig{
						IntervalInSeconds:         ptr.To(int64(10)),
						Path:                      ptr.To("/healthz"),
						Port:                      ptr.To(int64(8080)),
						Protocol:                  ptr.To(fleetnetv1alpha1.TrafficManagerMonitorProtocolHTTPS),
						TimeoutInSeconds:          ptr.To(int64(9)),
						ToleratedNumberOfFailures: ptr.To(int64(4)),
					},
				},
			},
			want: &fleetnetv1alpha1.TrafficManagerProfile{
				Spec: fleetnetv1alpha1.TrafficManagerProfileSpec{
					MonitorConfig: &fleetnetv1alpha1.MonitorConfig{
						IntervalInSeconds:         ptr.To(int64(10)),
						Path:                      ptr.To("/healthz"),
						Port:                      ptr.To(int64(8080)),
						Protocol:                  ptr.To(fleetnetv1alpha1.TrafficManagerMonitorProtocolHTTPS),
						TimeoutInSeconds:          ptr.To(int64(9)),
						ToleratedNumberOfFailures: ptr.To(int64(4)),
					},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			SetDefaultsTrafficManagerProfile(tc.obj)
			if diff := cmp.Diff(tc.want, tc.obj); diff != "" {
				t.Errorf("SetDefaultsTrafficManagerProfile() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
