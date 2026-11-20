/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package main

import (
	"strings"
	"testing"
)

func TestLocalServiceOriginsRequireExplicitSafeStartup(t *testing.T) {
	for _, options := range []managerOptions{
		{enableLocalServiceOrigins: true, leaderElection: true, localServiceResourceGroup: "node-rg"},
		{enableLocalServiceOrigins: true, enableAFD: true, localServiceResourceGroup: "node-rg"},
		{enableLocalServiceOrigins: true, enableAFD: true, leaderElection: true},
	} {
		err := run(options, dependencies{})
		if err == nil || !strings.Contains(err.Error(), "local Service origins require") {
			t.Fatalf("unsafe local-origin startup was not rejected before dependencies: %v", err)
		}
	}
}
