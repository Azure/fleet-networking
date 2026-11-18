/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package scripts

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPeeredVNetMemberAuthentication(t *testing.T) {
	bin := t.TempDir()
	log := filepath.Join(bin, "members.log")
	writeAFDCommand(t, filepath.Join(bin, "az"), `#!/bin/bash
set -euo pipefail
case "$1 $2 $3" in
  "network vnet create"|"network vnet peering") ;;
  "network vnet show") printf '/subscriptions/test/resourceGroups/test/providers/Microsoft.Network/virtualNetworks/test\n' ;;
  "aks create --location")
    shift 2
    name=""; aad=false; rbac=false
    while [[ $# -gt 0 ]]; do
      case "$1" in
        --name) name="$2"; shift 2 ;;
        --enable-aad) aad=true; shift ;;
        --enable-azure-rbac) rbac=true; shift ;;
        *) shift ;;
      esac
    done
    [[ "$aad" == true && "$rbac" == true ]] || {
      echo "missing Entra authentication or Azure RBAC for $name" >&2
      exit 1
    }
    printf '%s\n' "$name" >>"$AKS_TEST_COMMAND_LOG" ;;
  *) echo "unexpected Azure command: $*" >&2; exit 1 ;;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AKS_TEST_COMMAND_LOG", log)
	for name, value := range map[string]string{
		"MEMBER_1_LOCATION":     "eastus2",
		"MEMBER_2_LOCATION":     "westus",
		"MEMBER_CLUSTER_1":      "member-1",
		"MEMBER_CLUSTER_2":      "member-2",
		"RESOURCE_GROUP":        "test-rg",
		"AZURE_SUBSCRIPTION_ID": "test-subscription",
		"NODE_COUNT":            "2",
	} {
		t.Setenv(name, value)
	}
	if output, err := runAFDScript(t, "./aks-peered-vnet.sh"); err != nil {
		t.Fatalf("peered-VNet setup failed: %s: %v", output, err)
	}
	if got := readAFDFile(t, log); got != "member-1\nmember-2\n" {
		t.Fatalf("created member clusters = %q, want both members with Entra authentication and Azure RBAC", got)
	}
}
