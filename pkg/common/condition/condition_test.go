/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package condition

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

var (
	appProtocol   = "app-protocol"
	testClusterID = "test-cluster-id"
)

func TestEqualCondition(t *testing.T) {
	tests := []struct {
		name    string
		current *metav1.Condition
		desired *metav1.Condition
		want    bool
	}{
		{
			name:    "both are nil",
			current: nil,
			desired: nil,
			want:    true,
		},
		{
			name:    "current is nil",
			current: nil,
			desired: &metav1.Condition{
				Type:               string(fleetnetv1alpha1.MultiClusterServiceValid),
				Status:             metav1.ConditionUnknown,
				Reason:             "abc",
				Message:            "unable to find valid service import",
				ObservedGeneration: 1,
			},
			want: false,
		},
		{
			name: "messages are different",
			current: &metav1.Condition{
				Type:               string(fleetnetv1alpha1.MultiClusterServiceValid),
				Status:             metav1.ConditionUnknown,
				Reason:             "abc",
				Message:            "unable to find service import",
				ObservedGeneration: 1,
			},
			desired: &metav1.Condition{
				Type:               string(fleetnetv1alpha1.MultiClusterServiceValid),
				Status:             metav1.ConditionUnknown,
				Reason:             "abc",
				Message:            "unable to find valid service import",
				ObservedGeneration: 1,
			},
			want: true,
		},
		{
			name: "observedGenerations are different (current is larger)",
			current: &metav1.Condition{
				Type:               string(fleetnetv1alpha1.MultiClusterServiceValid),
				Status:             metav1.ConditionUnknown,
				Reason:             "abc",
				Message:            "unable to find service import",
				ObservedGeneration: 2,
			},
			desired: &metav1.Condition{
				Type:               string(fleetnetv1alpha1.MultiClusterServiceValid),
				Status:             metav1.ConditionUnknown,
				Reason:             "abc",
				Message:            "unable to find valid service import",
				ObservedGeneration: 1,
			},
			want: true,
		},
		{
			name: "observedGenerations are different (current is smaller)",
			current: &metav1.Condition{
				Type:               string(fleetnetv1alpha1.MultiClusterServiceValid),
				Status:             metav1.ConditionUnknown,
				Reason:             "abc",
				Message:            "unable to find service import",
				ObservedGeneration: 3,
			},
			desired: &metav1.Condition{
				Type:               string(fleetnetv1alpha1.MultiClusterServiceValid),
				Status:             metav1.ConditionUnknown,
				Reason:             "abc",
				Message:            "unable to find valid service import",
				ObservedGeneration: 4,
			},
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := EqualCondition(tc.current, tc.desired)
			if !cmp.Equal(got, tc.want) {
				t.Errorf("EqualCondition() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestEqualConditionIgnoreReason tests the EqualConditionIgnoreReason function.
func TestEqualConditionIgnoreReason(t *testing.T) {
	condType := "sometype"

	testCases := []struct {
		name    string
		current *metav1.Condition
		desired *metav1.Condition
		want    bool
	}{
		{
			name:    "nil conditions",
			current: nil,
			desired: nil,
			want:    true,
		},
		{
			name:    "current is nil",
			current: nil,
			desired: &metav1.Condition{
				Type:               condType,
				Status:             metav1.ConditionUnknown,
				ObservedGeneration: 7,
			},
			want: false,
		},
		{
			name: "conditions are equal",
			current: &metav1.Condition{
				Type:               condType,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: 0,
			},
			desired: &metav1.Condition{
				Type:               condType,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: 0,
			},
			want: true,
		},
		{
			name: "conditions are equal (different reasons)",
			current: &metav1.Condition{
				Type:               condType,
				Status:             metav1.ConditionTrue,
				Reason:             "some reason",
				ObservedGeneration: 0,
			},
			desired: &metav1.Condition{
				Type:               condType,
				Status:             metav1.ConditionTrue,
				Reason:             "another reason",
				ObservedGeneration: 0,
			},
			want: true,
		},
		{
			name: "conditions are not equal (different type)",
			current: &metav1.Condition{
				Type:               condType,
				Status:             metav1.ConditionUnknown,
				ObservedGeneration: 1,
			},
			desired: &metav1.Condition{
				Type:               "differentype",
				Status:             metav1.ConditionUnknown,
				ObservedGeneration: 1,
			},
			want: false,
		},
		{
			name: "conditions are not equal (different status)",
			current: &metav1.Condition{
				Type:               condType,
				Status:             metav1.ConditionFalse,
				ObservedGeneration: 4,
			},
			desired: &metav1.Condition{
				Type:               condType,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: 4,
			},
			want: false,
		},
		{
			name: "conditions are equal (current condition is newer)",
			current: &metav1.Condition{
				Type:               condType,
				Status:             metav1.ConditionUnknown,
				ObservedGeneration: 3,
			},
			desired: &metav1.Condition{
				Type:               condType,
				Status:             metav1.ConditionUnknown,
				ObservedGeneration: 2,
			},
			want: true,
		},
		{
			name: "conditions are not equal (current condition is older)",
			current: &metav1.Condition{
				Type:               condType,
				Status:             metav1.ConditionFalse,
				ObservedGeneration: 5,
			},
			desired: &metav1.Condition{
				Type:               condType,
				Status:             metav1.ConditionFalse,
				ObservedGeneration: 6,
			},
			want: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EqualConditionIgnoreReason(tc.current, tc.desired); got != tc.want {
				t.Fatalf("EqualConditionIgnoreReason(%+v, %+v) = %t, want %t",
					tc.current, tc.desired, got, tc.want)
			}
		})
	}
}

func TestUnconflictedServiceExportConflictCondition(t *testing.T) {
	input := fleetnetv1alpha1.InternalServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Generation: 456,
		},
		Spec: fleetnetv1alpha1.InternalServiceExportSpec{
			Ports: []fleetnetv1alpha1.ServicePort{
				{
					Name:        "portA",
					Protocol:    "TCP",
					Port:        8080,
					AppProtocol: &appProtocol,
					TargetPort:  intstr.IntOrString{IntVal: 8080},
				},
				{
					Name:       "portB",
					Protocol:   "TCP",
					Port:       9090,
					TargetPort: intstr.IntOrString{IntVal: 9090},
				},
			},
			ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
				ClusterID:       testClusterID,
				Kind:            "Service",
				Namespace:       "test-ns",
				Name:            "test-svc",
				ResourceVersion: "0",
				Generation:      123,
				UID:             "0",
			},
		},
	}
	want := metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportConflict),
		Status:             metav1.ConditionFalse,
		Reason:             conditionReasonNoConflictFound,
		ObservedGeneration: 456,
		Message:            "service test-ns/test-svc is exported without conflict",
	}
	got := UnconflictedServiceExportConflictCondition(input)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("UnconflictedServiceExportConflictCondition() mismatch (-want, +got):\n%s", diff)
	}
}

func TestConflictedServiceExportConflictCondition(t *testing.T) {
	input := fleetnetv1alpha1.InternalServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Generation: 123,
		},
		Spec: fleetnetv1alpha1.InternalServiceExportSpec{
			Ports: []fleetnetv1alpha1.ServicePort{
				{
					Name:        "portA",
					Protocol:    "TCP",
					Port:        8080,
					AppProtocol: &appProtocol,
					TargetPort:  intstr.IntOrString{IntVal: 8080},
				},
				{
					Name:       "portB",
					Protocol:   "TCP",
					Port:       9090,
					TargetPort: intstr.IntOrString{IntVal: 9090},
				},
			},
			ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
				ClusterID:       testClusterID,
				Kind:            "Service",
				Namespace:       "test-ns",
				Name:            "test-svc",
				ResourceVersion: "0",
				Generation:      456,
				UID:             "0",
			},
		},
	}
	want := metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportConflict),
		Status:             metav1.ConditionTrue,
		Reason:             conditionReasonConflictFound,
		ObservedGeneration: 123,
		Message:            "service test-ns/test-svc is in conflict with other exported services",
	}
	got := ConflictedServiceExportConflictCondition(input)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ConflictedServiceExportConflictCondition() mismatch (-want, +got):\n%s", diff)
	}
}

// TestEqualConditionWithMessage tests the EqualConditionWithMessage function.
func TestEqualConditionWithMessage(t *testing.T) {
	tests := map[string]struct {
		current *metav1.Condition
		desired *metav1.Condition
		want    bool
	}{
		"both conditions are nil": {
			current: nil,
			desired: nil,
			want:    true,
		},
		"current condition is nil": {
			current: nil,
			desired: &metav1.Condition{
				Type:               "SomeType",
				Status:             metav1.ConditionTrue,
				Reason:             "SomeReason",
				Message:            "SomeMessage",
				ObservedGeneration: 1,
			},
			want: false,
		},
		"desired condition is nil": {
			current: &metav1.Condition{
				Type:               "SomeType",
				Status:             metav1.ConditionTrue,
				Reason:             "SomeReason",
				Message:            "SomeMessage",
				ObservedGeneration: 1,
			},
			desired: nil,
			want:    false,
		},
		"conditions are equal": {
			current: &metav1.Condition{
				Type:               "SomeType",
				Status:             metav1.ConditionTrue,
				Reason:             "SomeReason",
				Message:            "SomeMessage",
				ObservedGeneration: 1,
			},
			desired: &metav1.Condition{
				Type:               "SomeType",
				Status:             metav1.ConditionTrue,
				Reason:             "SomeReason",
				Message:            "SomeMessage",
				ObservedGeneration: 1,
			},
			want: true,
		},
		"conditions have different types": {
			current: &metav1.Condition{
				Type:               "SomeType",
				Status:             metav1.ConditionTrue,
				Reason:             "SomeReason",
				Message:            "SomeMessage",
				ObservedGeneration: 1,
			},
			desired: &metav1.Condition{
				Type:               "DifferentType",
				Status:             metav1.ConditionTrue,
				Reason:             "SomeReason",
				Message:            "SomeMessage",
				ObservedGeneration: 1,
			},
			want: false,
		},
		"conditions have different statuses": {
			current: &metav1.Condition{
				Type:               "SomeType",
				Status:             metav1.ConditionFalse,
				Reason:             "SomeReason",
				Message:            "SomeMessage",
				ObservedGeneration: 1,
			},
			desired: &metav1.Condition{
				Type:               "SomeType",
				Status:             metav1.ConditionTrue,
				Reason:             "SomeReason",
				Message:            "SomeMessage",
				ObservedGeneration: 1,
			},
			want: false,
		},
		"conditions have different reasons": {
			current: &metav1.Condition{
				Type:               "SomeType",
				Status:             metav1.ConditionTrue,
				Reason:             "SomeReason",
				Message:            "SomeMessage",
				ObservedGeneration: 1,
			},
			desired: &metav1.Condition{
				Type:               "SomeType",
				Status:             metav1.ConditionTrue,
				Reason:             "DifferentReason",
				Message:            "SomeMessage",
				ObservedGeneration: 1,
			},
			want: false,
		},
		"conditions have different messages": {
			current: &metav1.Condition{
				Type:               "SomeType",
				Status:             metav1.ConditionTrue,
				Reason:             "SomeReason",
				Message:            "SomeMessage",
				ObservedGeneration: 1,
			},
			desired: &metav1.Condition{
				Type:               "SomeType",
				Status:             metav1.ConditionTrue,
				Reason:             "SomeReason",
				Message:            "DifferentMessage",
				ObservedGeneration: 1,
			},
			want: false,
		},
		"current condition has newer observed generation": {
			current: &metav1.Condition{
				Type:               "SomeType",
				Status:             metav1.ConditionTrue,
				Reason:             "SomeReason",
				Message:            "SomeMessage",
				ObservedGeneration: 2,
			},
			desired: &metav1.Condition{
				Type:               "SomeType",
				Status:             metav1.ConditionTrue,
				Reason:             "SomeReason",
				Message:            "SomeMessage",
				ObservedGeneration: 1,
			},
			want: true,
		},
		"desired condition has newer observed generation": {
			current: &metav1.Condition{
				Type:               "SomeType",
				Status:             metav1.ConditionTrue,
				Reason:             "SomeReason",
				Message:            "SomeMessage",
				ObservedGeneration: 1,
			},
			desired: &metav1.Condition{
				Type:               "SomeType",
				Status:             metav1.ConditionTrue,
				Reason:             "SomeReason",
				Message:            "SomeMessage",
				ObservedGeneration: 2,
			},
			want: false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := EqualConditionWithMessage(tt.current, tt.desired); got != tt.want {
				t.Errorf("EqualConditionWithMessage() = %v, want %v", got, tt.want)
			}
		})
	}
}
