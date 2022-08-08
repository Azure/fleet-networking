/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package env provides shared functions to handle environment variables.
package env

import (
	"fmt"
	"os"
)

func EnvOrError(envKey string) (string, error) {
	value, ok := os.LookupEnv(envKey)
	if !ok {
		return "", fmt.Errorf("failed to retrieve the environment variable value from %s", envKey)
	}
	return value, nil
}
