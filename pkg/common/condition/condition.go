// Package condition provides condition related utils.
package condition

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// EqualCondition compares current with desired and ignores the LastTransitionTime and Message.
func EqualCondition(current, desired *metav1.Condition) bool {
	if current == nil && desired == nil {
		return true
	}
	return current != nil &&
		desired != nil &&
		current.Type == desired.Type &&
		current.Status == desired.Status &&
		current.Reason == desired.Reason &&
		current.ObservedGeneration == desired.ObservedGeneration
}

// isConditionSeen returns if a condition has been seen before and requires no further action.
// If any reason will do, pass an empty string as the expected reason.
func IsConditionSeen(cond *metav1.Condition, expectedStatus metav1.ConditionStatus, expectedReason string, minGeneration int64) bool {
	if cond == nil {
		return false
	}

	statusAsExpected := (cond.Status == expectedStatus)
	reasonAsExpected := (cond.Reason == expectedReason)
	if expectedReason == "" {
		reasonAsExpected = true
	}
	sameOrNewerGeneration := (cond.ObservedGeneration >= minGeneration)

	if statusAsExpected && reasonAsExpected && sameOrNewerGeneration {
		return true
	}
	return false
}
