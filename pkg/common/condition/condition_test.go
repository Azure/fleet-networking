package condition

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
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
