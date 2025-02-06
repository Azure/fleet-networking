/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package azureerrors

import (
	"errors"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
)

func TestIsNotFound(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "not azure error",
			err:  errors.New("not azure error"),
			want: false,
		},
		{
			name: "bad request error",
			err:  &azcore.ResponseError{StatusCode: 400},
			want: false,
		},
		{
			name: "not found error",
			err:  &azcore.ResponseError{StatusCode: 404},
			want: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsNotFound(tc.err)
			if got != tc.want {
				t.Errorf("IsNotFound() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsClientError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "not azure error",
			err:  errors.New("not azure error"),
			want: false,
		},
		{
			name: "bad request error",
			err:  &azcore.ResponseError{StatusCode: 400},
			want: true,
		},
		{
			name: "not found error",
			err:  &azcore.ResponseError{StatusCode: 404},
			want: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsClientError(tc.err)
			if got != tc.want {
				t.Errorf("IsClientError() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsConflict(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "not azure error",
			err:  errors.New("not azure error"),
			want: false,
		},
		{
			name: "bad request error",
			err:  &azcore.ResponseError{StatusCode: 400},
			want: false,
		},
		{
			name: "conflict error",
			err:  &azcore.ResponseError{StatusCode: 409},
			want: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsConflict(tc.err)
			if got != tc.want {
				t.Errorf("IsConflict() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsThrottled(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "not azure error",
			err:  errors.New("not azure error"),
			want: false,
		},
		{
			name: "bad request error",
			err:  &azcore.ResponseError{StatusCode: 400},
			want: false,
		},
		{
			name: "throttled error",
			err:  &azcore.ResponseError{StatusCode: 429},
			want: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsThrottled(tc.err)
			if got != tc.want {
				t.Errorf("IsThrottled() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsForbidden(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "not azure error",
			err:  errors.New("not azure error"),
			want: false,
		},
		{
			name: "bad request error",
			err:  &azcore.ResponseError{StatusCode: 400},
			want: false,
		},
		{
			name: "forbidden error",
			err:  &azcore.ResponseError{StatusCode: 403},
			want: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsForbidden(tc.err)
			if got != tc.want {
				t.Errorf("IsForbidden() = %v, want %v", got, tc.want)
			}
		})
	}
}
