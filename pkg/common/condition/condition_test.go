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

// TestIsConditionSeen tests the isConditionSeen function.
func TestIsConditionSeen(t *testing.T) {
	trueReason := "CondIsTrue"
	falseReason := "CondIsFalse"
	unknownReason := "CondIsUnknown"
	emptyReason := ""

	testCases := []struct {
		name           string
		cond           *metav1.Condition
		expectedStatus metav1.ConditionStatus
		expectedReason string
		minGeneration  int64
		want           bool
	}{
		{
			name: "the condition is seen (has expected status + reason and same generation)",
			cond: &metav1.Condition{
				Status:             metav1.ConditionTrue,
				Reason:             trueReason,
				ObservedGeneration: 0,
			},
			expectedStatus: metav1.ConditionTrue,
			expectedReason: trueReason,
			minGeneration:  0,
			want:           true,
		},
		{
			name: "the condition is seen (has expected status and same generation)",
			cond: &metav1.Condition{
				Status:             metav1.ConditionFalse,
				Reason:             falseReason,
				ObservedGeneration: 1,
			},
			expectedStatus: metav1.ConditionFalse,
			expectedReason: emptyReason,
			minGeneration:  1,
			want:           true,
		},
		{
			name: "the condition is seen (has expected status and newer generation)",
			cond: &metav1.Condition{
				Status:             metav1.ConditionUnknown,
				Reason:             unknownReason,
				ObservedGeneration: 3,
			},
			expectedStatus: metav1.ConditionUnknown,
			expectedReason: emptyReason,
			minGeneration:  2,
			want:           true,
		},
		{
			name: "the condition is not seen (different status)",
			cond: &metav1.Condition{
				Status:             metav1.ConditionTrue,
				Reason:             trueReason,
				ObservedGeneration: 4,
			},
			expectedStatus: metav1.ConditionFalse,
			expectedReason: emptyReason,
			minGeneration:  4,
			want:           false,
		},
		{
			name: "the condition is not seen (different reason)",
			cond: &metav1.Condition{
				Status:             metav1.ConditionFalse,
				Reason:             falseReason,
				ObservedGeneration: 5,
			},
			expectedStatus: metav1.ConditionFalse,
			expectedReason: trueReason,
			minGeneration:  5,
			want:           false,
		},
		{
			name: "the condition is not seen (older generation)",
			cond: &metav1.Condition{
				Status:             metav1.ConditionUnknown,
				Reason:             unknownReason,
				ObservedGeneration: 6,
			},
			expectedStatus: metav1.ConditionUnknown,
			expectedReason: unknownReason,
			minGeneration:  7,
			want:           false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsConditionSeen(tc.cond, tc.expectedStatus, tc.expectedReason, tc.minGeneration); got != tc.want {
				t.Fatalf("isConditionSeen(%+v, %s, %s, %d) = %t, want %t",
					tc.cond, tc.expectedStatus, tc.expectedReason, tc.minGeneration, got, tc.want)
			}
		})
	}
}
