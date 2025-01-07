/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package defaulter

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/utils/ptr"

	fleetnetv1beta1 "go.goms.io/fleet-networking/api/v1beta1"
)

func TestSetDefaultsTrafficManagerBackend(t *testing.T) {
	tests := []struct {
		name string
		obj  *fleetnetv1beta1.TrafficManagerBackend
		want *fleetnetv1beta1.TrafficManagerBackend
	}{
		{
			name: "TrafficManagerBackend with nil weight",
			obj: &fleetnetv1beta1.TrafficManagerBackend{
				Spec: fleetnetv1beta1.TrafficManagerBackendSpec{},
			},
			want: &fleetnetv1beta1.TrafficManagerBackend{
				Spec: fleetnetv1beta1.TrafficManagerBackendSpec{
					Weight: ptr.To(int64(1)),
				},
			},
		},
		{
			name: "TrafficManagerBackend with values",
			obj: &fleetnetv1beta1.TrafficManagerBackend{
				Spec: fleetnetv1beta1.TrafficManagerBackendSpec{
					Weight: ptr.To(int64(100)),
				},
			},
			want: &fleetnetv1beta1.TrafficManagerBackend{
				Spec: fleetnetv1beta1.TrafficManagerBackendSpec{
					Weight: ptr.To(int64(100)),
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			SetDefaultsTrafficManagerBackend(tc.obj)
			if diff := cmp.Diff(tc.want, tc.obj); diff != "" {
				t.Errorf("SetDefaultsTrafficManagerBackend() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
