/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package defaulter provides the utils for setting default values for a resource.
package defaulter

import (
	"k8s.io/utils/ptr"

	fleetnetv1beta1 "go.goms.io/fleet-networking/api/v1beta1"
)

// SetDefaultsTrafficManagerProfile sets the default values for TrafficManagerProfile.
func SetDefaultsTrafficManagerProfile(obj *fleetnetv1beta1.TrafficManagerProfile) {
	if obj.Spec.MonitorConfig == nil {
		obj.Spec.MonitorConfig = &fleetnetv1beta1.MonitorConfig{}
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
		obj.Spec.MonitorConfig.Protocol = ptr.To(fleetnetv1beta1.TrafficManagerMonitorProtocolHTTP)
	}

	// TimeoutInSeconds value depends on the IntervalInSeconds, so that the defaulter MUST handle the IntervalInSeconds first.
	// * If the Probing Interval is set to 30 seconds, then you can set the Timeout value between 5 and 10 seconds.
	//   If no value is specified, it uses a default value of 10 seconds.
	// * If the Probing Interval is set to 10 seconds, then you can set the Timeout value between 5 and 9 seconds.
	//   If no Timeout value is specified, it uses a default value of 9 seconds.
	// Reference link: https://learn.microsoft.com/en-us/azure/traffic-manager/traffic-manager-monitoring#configure-endpoint-monitoring
	if obj.Spec.MonitorConfig.TimeoutInSeconds == nil {
		if *obj.Spec.MonitorConfig.IntervalInSeconds == 30 {
			obj.Spec.MonitorConfig.TimeoutInSeconds = ptr.To(int64(10))
		} else if *obj.Spec.MonitorConfig.IntervalInSeconds == 10 {
			obj.Spec.MonitorConfig.TimeoutInSeconds = ptr.To(int64(9))
		}
	}

	if obj.Spec.MonitorConfig.ToleratedNumberOfFailures == nil {
		obj.Spec.MonitorConfig.ToleratedNumberOfFailures = ptr.To(int64(3))
	}
}
