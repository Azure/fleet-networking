//go:build e2e

/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCurrentGatewayCondition(t *testing.T) {
	tests := []struct {
		name       string
		conditions []metav1.Condition
		valid      bool
	}{
		{"current", []metav1.Condition{{Type: "ResolvedRefs", Status: metav1.ConditionTrue, Reason: "ResolvedRefs", ObservedGeneration: 2}}, true},
		{"missing", nil, false},
		{"different type", []metav1.Condition{{Type: "Accepted", Status: metav1.ConditionTrue, Reason: "ResolvedRefs", ObservedGeneration: 2}}, false},
		{"unknown", []metav1.Condition{{Type: "ResolvedRefs", Status: metav1.ConditionUnknown, Reason: "ResolvedRefs", ObservedGeneration: 2}}, false},
		{"false", []metav1.Condition{{Type: "ResolvedRefs", Status: metav1.ConditionFalse, Reason: "ResolvedRefs", ObservedGeneration: 2}}, false},
		{"different reason", []metav1.Condition{{Type: "ResolvedRefs", Status: metav1.ConditionTrue, Reason: "Pending", ObservedGeneration: 2}}, false},
		{"stale generation", []metav1.Condition{{Type: "ResolvedRefs", Status: metav1.ConditionTrue, Reason: "ResolvedRefs", ObservedGeneration: 1}}, false},
		{"unexpected generation", []metav1.Condition{{Type: "ResolvedRefs", Status: metav1.ConditionTrue, Reason: "ResolvedRefs", ObservedGeneration: 3}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := currentGatewayCondition(tt.conditions, "ResolvedRefs", metav1.ConditionTrue, "ResolvedRefs", 2)
			if (err == nil) != tt.valid {
				t.Fatalf("valid=%v, want %v: %v", err == nil, tt.valid, err)
			}
		})
	}
}
