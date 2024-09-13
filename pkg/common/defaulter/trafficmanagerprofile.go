/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package defaulter provides the utils for setting default values for a resource.
package defaulter

import (
	"k8s.io/utils/ptr"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

// SetDefaultsTrafficManagerProfile sets the default values for TrafficManagerProfile.
func SetDefaultsTrafficManagerProfile(obj *fleetnetv1alpha1.TrafficManagerProfile) {
	if obj.Spec.MonitorConfig == nil {
		obj.Spec.MonitorConfig = &fleetnetv1alpha1.MonitorConfig{}
	}

	if obj.Spec.MonitorConfig.IntervalInSeconds == nil {
		obj.Spec.MonitorConfig.IntervalInSeconds = ptr.To(int64(30))
	}

	if obj.Spec.MonitorConfig.Path == nil {
		obj.Spec.MonitorConfig.Path = ptr.To("/")
	}

	if obj.Spec.MonitorConfig.Port == nil {
		obj.Spec.MonitorConfig.Port = ptr.To(int64(80))
	}

	if obj.Spec.MonitorConfig.Protocol == nil {
		obj.Spec.MonitorConfig.Protocol = ptr.To(fleetnetv1alpha1.TrafficManagerMonitorProtocolHTTP)
	}

	// TimeoutInSeconds value depends on the IntervalInSeconds, so that the defaulter MUST handle the IntervalInSeconds first.
	if obj.Spec.MonitorConfig.TimeoutInSeconds == nil {
		if *obj.Spec.MonitorConfig.IntervalInSeconds == 30 {
			obj.Spec.MonitorConfig.TimeoutInSeconds = ptr.To(int64(10))
		} else { // assuming validation wehbook should validate the  intervalInSeconds first
			obj.Spec.MonitorConfig.TimeoutInSeconds = ptr.To(int64(9))
		}
	}

	if obj.Spec.MonitorConfig.ToleratedNumberOfFailures == nil {
		obj.Spec.MonitorConfig.ToleratedNumberOfFailures = ptr.To(int64(3))
	}
}
