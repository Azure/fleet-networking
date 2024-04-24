/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package apiretry provides the retry func shared between networking controllers.
package apiretry

import (
	"context"
	"errors"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
)

// Do will retry the do func only when the error is transient.
func Do(do func() error) error {
	backOffPeriod := retry.DefaultBackoff
	backOffPeriod.Cap = time.Second * 1

	return retry.OnError(backOffPeriod, func(err error) bool {
		return retriable(err)
	}, do)
}
func retriable(err error) bool {
	if apierrors.IsTimeout(err) ||
		apierrors.IsServerTimeout(err) ||
		apierrors.IsTooManyRequests(err) {
		return true
	}
	return false
}

// WaitUntilObjectDeleted will ensure obj is deleted until it hits the backoff cap.
// It will retry only when it gets the object or the error is transient.
func WaitUntilObjectDeleted(ctx context.Context, get func() error) error {
	backOffPeriod := wait.Backoff{
		Steps:    5,
		Duration: 500 * time.Millisecond,
		Factor:   1.6,
		Jitter:   0.2,
	}
	backOffPeriod.Cap = time.Second * 5

	var lastErr error
	err := wait.ExponentialBackoffWithContext(ctx, backOffPeriod, func(_ context.Context) (bool, error) {
		err := get()
		switch {
		case err == nil:
			lastErr = err
			return false, nil
		case apierrors.IsNotFound(err):
			return true, nil
		case retriable(err):
			lastErr = err
			return false, nil
		default:
			return false, err
		}
	})
	if wait.Interrupted(err) {
		if lastErr == nil {
			return wait.ErrorInterrupted(errors.New("timed out or the context ended"))
		}
		err = lastErr
	}
	return err
}
