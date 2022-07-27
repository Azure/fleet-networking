// Package apiretry provides the retry func shared between networking controllers.
package apiretry

import (
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/retry"
)

// Do will retry the do func only when the error is transient.
func Do(do func() error) error {
	backOffPeriod := retry.DefaultBackoff
	backOffPeriod.Cap = time.Second * 1

	return retry.OnError(backOffPeriod, func(err error) bool {
		if apierrors.IsTimeout(err) ||
			apierrors.IsServerTimeout(err) ||
			apierrors.IsTooManyRequests(err) ||
			apierrors.IsConflict(err) {
			return true
		}
		return false
	}, do)
}
