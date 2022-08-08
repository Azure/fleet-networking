/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package env

import (
	"os"
	"testing"
)

func TestEnvOrError(t *testing.T) {
	testCases := []struct {
		name             string
		environmentKey   string
		environmentValue string
		foundEnv         bool
	}{
		{
			name:             "environment variable is present",
			environmentKey:   "test-env-present",
			environmentValue: "test-value",
			foundEnv:         true,
		},
		{
			name:             "environment variable is not present",
			environmentKey:   "test-env-not-present",
			environmentValue: "",
			foundEnv:         false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if len(tc.environmentValue) == 0 {
				os.Unsetenv(tc.environmentKey)
			} else {
				os.Setenv(tc.environmentKey, tc.environmentValue)
			}
			val, err := EnvOrError(tc.environmentKey)
			if tc.foundEnv && err != nil {
				t.Errorf("environment variable %s should be present, err: %s", tc.environmentKey, err.Error())
			}

			if tc.foundEnv && err == nil && val != tc.environmentValue {
				t.Errorf("environment variable %s obtained is not expected, want: %s, actual: %s", tc.environmentKey, tc.environmentValue, val)
			}

			if !tc.foundEnv && err == nil {
				t.Errorf("environment variable %s should not be present", tc.environmentKey)
			}
		})
	}
}
