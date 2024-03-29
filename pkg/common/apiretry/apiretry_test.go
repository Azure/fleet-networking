/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package apiretry

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestDo(t *testing.T) {
	counter := 0
	tests := []struct {
		name        string
		do          func() error
		wantError   bool
		wantCounter int
	}{
		{
			name: "timeout error",
			do: func() error {
				if counter == 0 {
					counter = 1
					return errors.NewTimeoutError("timeout", 1)
				}
				counter = 2
				return nil
			},
			wantCounter: 2,
		},
		{
			name: "server timeout error",
			do: func() error {
				if counter == 0 {
					counter = 1
					return errors.NewServerTimeout(schema.GroupResource{}, "server timeout", 1)
				}
				counter = 2
				return nil
			},
			wantCounter: 2,
		},
		{
			name: "too many request error",
			do: func() error {
				if counter == 0 {
					counter = 1
					return errors.NewTooManyRequestsError("too many requests")
				}
				counter = 2
				return nil
			},
			wantCounter: 2,
		},
		{
			name: "other error",
			do: func() error {
				if counter == 0 {
					counter = 1
					return errors.NewAlreadyExists(schema.GroupResource{}, "abc")
				}
				counter = 2
				return nil
			},
			wantError:   true,
			wantCounter: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			counter = 0
			got := Do(tc.do)
			if (got != nil) != tc.wantError {
				t.Errorf("Do() = %v, want error %v", got, tc.wantError)
			}
			if counter != tc.wantCounter {
				t.Errorf("Do() got counter %v, want %v", counter, tc.wantCounter)
			}
		})
	}
}

func TestWaitUntilObjectDeleted(t *testing.T) {
	counter := 0
	tests := []struct {
		name        string
		get         func() error
		wantError   bool
		wantCounter int
	}{
		{
			name: "timeout error",
			get: func() error {
				if counter == 0 {
					counter = 1
					return errors.NewTimeoutError("timeout", 1)
				}
				counter = 2
				return errors.NewNotFound(schema.GroupResource{}, "notfound")
			},
			wantCounter: 2,
		},
		{
			name: "server timeout error",
			get: func() error {
				if counter == 0 {
					counter = 1
					return errors.NewServerTimeout(schema.GroupResource{}, "server timeout", 1)
				}
				counter = 2
				return errors.NewNotFound(schema.GroupResource{}, "notfound")
			},
			wantCounter: 2,
		},
		{
			name: "too many request error",
			get: func() error {
				if counter == 0 {
					counter = 1
					return errors.NewTooManyRequestsError("too many requests")
				}
				counter = 2
				return errors.NewNotFound(schema.GroupResource{}, "notfound")
			},
			wantCounter: 2,
		},
		{
			name: "object is deleted",
			get: func() error {
				if counter == 0 {
					counter = 1
					return nil
				}
				counter = 2
				return errors.NewNotFound(schema.GroupResource{}, "notfound")
			},
			wantCounter: 2,
		},
		{
			name: "other error",
			get: func() error {
				if counter == 0 {
					counter = 1
					return errors.NewAlreadyExists(schema.GroupResource{}, "abc")
				}
				counter = 2
				return errors.NewNotFound(schema.GroupResource{}, "notfound")
			},
			wantError:   true,
			wantCounter: 1,
		},
		{
			name: "object is never deleted",
			get: func() error {
				if counter == 0 {
					counter = 1
					return errors.NewTimeoutError("timeout", 1)
				}
				counter = 2
				return nil
			},
			wantError:   true,
			wantCounter: 2,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			counter = 0
			got := WaitUntilObjectDeleted(context.Background(), tc.get)
			if (got != nil) != tc.wantError {
				t.Errorf("WaitUntilObjectDeleted() = %v, want error %v", got, tc.wantError)
			}
			if counter != tc.wantCounter {
				t.Errorf("WaitUntilObjectDeleted() got counter %v, want %v", counter, tc.wantCounter)
			}
		})
	}
}
