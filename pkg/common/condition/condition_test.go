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
			name: "observedGenerations are different",
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
