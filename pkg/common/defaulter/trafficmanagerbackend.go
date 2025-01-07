/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package defaulter

import (
	"k8s.io/utils/ptr"

	fleetnetv1beta1 "go.goms.io/fleet-networking/api/v1beta1"
)

// SetDefaultsTrafficManagerBackend sets the default values for TrafficManagerBackend.
func SetDefaultsTrafficManagerBackend(obj *fleetnetv1beta1.TrafficManagerBackend) {
	if obj.Spec.Weight == nil {
		obj.Spec.Weight = ptr.To(int64(1))
	}
}
