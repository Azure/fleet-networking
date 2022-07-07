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
