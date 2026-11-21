/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package main

import (
	"context"
	"strings"
	"testing"
)

func TestLocalServiceOriginsRequireExplicitSafeStartup(t *testing.T) {
	for _, options := range []managerOptions{
		{enableAFD: true, localServiceResourceGroup: "node-rg"},
		{enableAFD: true, leaderElection: true},
		{enableAFD: true, leaderElection: true, localServiceResourceGroup: " "},
	} {
		err := run(options, dependencies{})
		if err == nil || !strings.Contains(err.Error(), "AFD requires") {
			t.Fatalf("unsafe local-origin startup was not rejected before dependencies: %v", err)
		}

	}
}

func TestAFDSetupRequiresLocalCompanionDiscovery(t *testing.T) {
	provider := &managerProfileCleaner{}
	err := setupControllers(context.Background(), nil, afdProviders{
		cleanup: provider, observer: provider, provisioner: provider,
	})
	if err == nil || !strings.Contains(err.Error(), "local Service companion controller with discovery") {
		t.Fatalf("AFD setup silently omitted local Service support: %v", err)
	}
}
