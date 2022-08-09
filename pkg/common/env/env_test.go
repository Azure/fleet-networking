/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package env

import (
	"os"
	"testing"
)

func TestLookup(t *testing.T) {
	testCases := []struct {
		name    string
		key     string
		value   string
		wantErr bool
	}{
		{
			name:    "environment variable is present",
			key:     "test-env-present",
			value:   "test-value",
			wantErr: false,
		},
		{
			name:    "environment variable is not present",
			key:     "test-env-not-present",
			value:   "",
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if len(tc.value) == 0 {
				os.Unsetenv(tc.key)
			} else {
				os.Setenv(tc.key, tc.value)
			}
			val, err := Lookup(tc.key)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Lookup(%v) got err %v, want err %v", tc.key, err, tc.wantErr)
			}

			if tc.wantErr {
				return
			}

			if val != tc.value {
				t.Errorf("Lookup(%v) = %v, want %v", tc.key, tc.value, val)
			}
		})
	}
}

func TestLookupMemberClusterName(t *testing.T) {
	testCases := []struct {
		name    string
		key     string
		value   string
		wantErr bool
	}{
		{
			name:    "environment variable is present",
			key:     memberClusterNameEnvKey,
			value:   "test-value",
			wantErr: false,
		},
		{
			name:    "environment variable is not present",
			key:     memberClusterNameEnvKey,
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if len(tc.value) == 0 {
				os.Unsetenv(tc.key)
			} else {
				os.Setenv(tc.key, tc.value)
			}
			val, err := LookupMemberClusterName()
			if (err != nil) != tc.wantErr {
				t.Fatalf("LookupMemberClusterName() got err %v, want err %v", err, tc.wantErr)
			}

			if tc.wantErr {
				return
			}

			if val != tc.value {
				t.Errorf("LookupMemberClusterName() = %v, want %v", tc.value, val)
			}
		})
	}
}
