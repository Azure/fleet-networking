/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package scripts

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestSharedGroupConfiguration(t *testing.T) {
	afdTestEnvironment(t)
	run := func(id string) (string, error) {
		t.Helper()
		t.Setenv("E2E_RUN_ID", id)
		t.Setenv("AZURE_SUBSCRIPTION_ID", "test-subscription")
		t.Setenv("AZURE_RESOURCE_GROUP", "fleet-networking-e2e")
		t.Setenv("AZURE_NETWORK_SETTING", "shared-vnet")
		return runAFDScript(t, "-ec", `source ./e2e-config.sh
printf '%s|%s|%s|%s|%s\n' "$E2E_RESOURCE_PREFIX" "$HUB_AZURE_CLUSTER" "$MEMBER_1_AZURE_CLUSTER" "$MEMBER_2_AZURE_CLUSTER" "$REGISTRY_NAME"`)
	}
	first, err := run("123-atm-1")
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := run("123-atm-1")
	if err != nil || repeated != first {
		t.Fatalf("run identity must be deterministic: %q, %q, %v", first, repeated, err)
	}
	other, err := run("123-afd-1")
	if err != nil || other == first {
		t.Fatalf("concurrent scenarios must not share names: %q, %q, %v", first, other, err)
	}
	for _, invalid := range []string{"", "../other", "ABC", "trailing space"} {
		output, err := run(invalid)
		if err == nil || !strings.Contains(output, "E2E_RUN_ID") {
			t.Fatalf("invalid run ID %q was accepted: %s (%v)", invalid, output, err)
		}
	}
	t.Setenv("ENABLE_AZURE_FRONT_DOOR", "true")
	t.Setenv("ENABLE_AFD_PRIVATE_LINK_AUTO_APPROVAL", "true")
	t.Setenv("E2E_TEST_PRINCIPAL_ID", "")
	output, err := run("123-afd-1")
	if err == nil || !strings.Contains(output, "E2E_TEST_PRINCIPAL_ID") {
		t.Fatalf("shared AFD must reject missing principal ID: %s (%v)", output, err)
	}
}

func TestSharedAFDInstallation(t *testing.T) {
	afdTestEnvironment(t)
	t.Setenv("E2E_RUN_ID", "123-afd-1")
	t.Setenv("ENABLE_AZURE_FRONT_DOOR", "true")
	t.Setenv("ENABLE_AFD_PRIVATE_LINK_AUTO_APPROVAL", "true")
	t.Setenv("E2E_TEST_PRINCIPAL_ID", "44444444-4444-4444-4444-444444444444")
	log := installAFDCommandStubs(t)
	output, err := runAFDScript(t, "./install-afd.sh")
	if err != nil {
		t.Fatalf("shared AFD installation: %s (%v)", output, err)
	}

	testTopologyNames := func(t *testing.T) {
		tests := []struct {
			name, script, network, atm string
		}{
			{"traffic manager", "./aks-shared-vnet.sh", "shared-vnet", "true"},
			{"front door", "./aks-shared-vnet.sh", "shared-vnet", "false"},
			{"peered", "./aks-peered-vnet.sh", "peered-vnet", "false"},
			{"dynamic", "./aks-dynamic-ip-allocation.sh", "dynamic-ip-allocation", "false"},
			{"unsupported", "./aks-no-networking.sh", "unsupported", "false"},
			{"performance", "./perf-test.sh", "perf-test", "false"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				afdTestEnvironment(t)
				t.Setenv("E2E_RUN_ID", "123-"+tt.network+"-1")
				t.Setenv("ENABLE_TRAFFIC_MANAGER", tt.atm)
				t.Setenv("AZURE_NETWORK_SETTING", tt.network)
				t.Setenv("RESOURCE_GROUP", "test-rg")
				t.Setenv("NODE_COUNT", "2")
				t.Setenv("MEMBER_1_LOCATION", "eastus2")
				t.Setenv("MEMBER_2_LOCATION", "westus")
				t.Setenv("MEMBER_LOCATION_1", "eastus2")
				t.Setenv("MEMBER_LOCATION_2", "westus")
				t.Setenv("MEMBER_CLUSTER_1_AKS_IDENTITY_ID", "/identity/member1-control")
				t.Setenv("MEMBER_CLUSTER_1_AKS_KUBELET_IDENTITY_ID", "/identity/member1-kubelet")
				t.Setenv("MEMBER_CLUSTER_2_AKS_IDENTITY_ID", "/identity/member2-control")
				t.Setenv("MEMBER_CLUSTER_2_AKS_KUBELET_IDENTITY_ID", "/identity/member2-kubelet")
				dir := t.TempDir()
				t.Setenv("TEST_CALLS", filepath.Join(dir, "calls"))
				t.Setenv("KUBECONFIG", filepath.Join(dir, "kubeconfig"))
				writeAFDCommand(t, filepath.Join(dir, "az"), `#!/bin/bash
	printf '%s\n' "$*" >> "$TEST_CALLS"
	if [[ "$*" == "network vnet show "* ]]; then echo "/subscriptions/test-subscription/resourceGroups/test-rg/providers/Microsoft.Network/virtualNetworks/test"; fi
	`)
				t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
				output, err := runAFDScript(t, tt.script)
				if err != nil {
					t.Fatalf("topology: %s (%v)", output, err)
				}
				hash := sha256.Sum256([]byte("test-subscription/test-rg/123-" + tt.network + "-1"))
				prefix := fmt.Sprintf("fne%x", hash[:8])
				calls := readAFDFile(t, filepath.Join(dir, "calls"))
				clusterNames := []string{prefix + "-member1", prefix + "-member2"}
				if tt.network == "perf-test" {
					clusterNames = append(clusterNames, prefix+"-member3", prefix+"-member4")
				}
				for _, name := range clusterNames {
					if !strings.Contains(calls, "--name "+name) {
						t.Errorf("missing physical cluster %s in %s", name, calls)
					}
				}
				if strings.Count(calls, "fleet-e2e-run=123-"+tt.network+"-1") < len(clusterNames) {
					t.Fatalf("cluster ownership tags missing: %s", calls)
				}
				if tt.network != "unsupported" {
					created := 0
					for _, call := range strings.Split(calls, "\n") {
						if strings.HasPrefix(call, "network vnet create ") {
							created++
							if !strings.Contains(call, "--name "+prefix+"-") {
								t.Fatalf("VNet name is not run-specific: %s", call)
							}
						}
					}
					if created == 0 {
						t.Fatalf("no VNet created: %s", calls)
					}
				}
			})
		}
	}

	testBootstrapPreflight := func(t *testing.T) {
		for _, tc := range []struct {
			name                     string
			conflict, existingConfig bool
		}{{"clean", false, false}, {"resource collision", true, false}, {"existing kubeconfig", false, true}} {
			t.Run(tc.name, func(t *testing.T) {
				afdTestEnvironment(t)
				t.Setenv("E2E_RUN_ID", "123-atm-1")
				t.Setenv("AZURE_RESOURCE_GROUP", "fleet-networking-e2e")
				dir := t.TempDir()
				t.Setenv("KUBECONFIG", filepath.Join(dir, "kubeconfig"))
				t.Setenv("TEST_CALLS", filepath.Join(dir, "calls"))
				t.Setenv("GITHUB_OUTPUT", filepath.Join(dir, "github-output"))
				if tc.existingConfig {
					if err := os.WriteFile(filepath.Join(dir, "kubeconfig"), []byte("existing"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				if tc.conflict {
					hash := sha256.Sum256([]byte("test-subscription/fleet-networking-e2e/123-atm-1"))
					t.Setenv("TEST_RESOURCE_NAME", fmt.Sprintf("fne%x-hub", hash[:8]))
				} else {
					t.Setenv("TEST_RESOURCE_NAME", "")
				}
				writeAFDCommand(t, filepath.Join(dir, "yq"), "#!/bin/bash\nexit 0\n")
				writeAFDCommand(t, filepath.Join(dir, "az"), `#!/bin/bash
	set -euo pipefail
	printf '%s\n' "$*" >> "$TEST_CALLS"
	case "$1 $2" in
	  "account set") ;;
	  "group show") printf '%s\n' '{"id":"/subscriptions/test-subscription/resourceGroups/fleet-networking-e2e","tags":{"managedBy":"aks-rp/ci-resources","workload":"fleet-networking-e2e"}}' ;;
	  "resource list") if [[ -n "$TEST_RESOURCE_NAME" ]]; then jq -cn --arg name "$TEST_RESOURCE_NAME" '[{name:$name,tags:{}}]'; else echo '[]'; fi ;;
	  "acr create") echo "injected create failure" >&2; exit 23 ;;
	  *) echo "unexpected Azure operation: $*" >&2; exit 1 ;;
	esac
	`)
				t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
				output, err := runAFDScript(t, "./bootstrap.sh")
				if err == nil {
					t.Fatal("expected a preflight conflict or injected create failure")
				}
				calls := readAFDFile(t, filepath.Join(dir, "calls"))
				if strings.Contains(calls, "group create") || strings.Contains(calls, "group delete") {
					t.Fatalf("shared setup touched the resource group: %s", calls)
				}
				if (tc.conflict || tc.existingConfig) && (strings.Contains(calls, "acr create") || strings.Contains(output, "##vso[task.setvariable variable=e2eResourcesStarted]true")) {
					t.Fatalf("collision was not rejected before cleanup became eligible: %s, %s", calls, output)
				}
				if !tc.conflict && !tc.existingConfig && (!strings.Contains(calls, "acr create") || !strings.Contains(output, "##vso[task.setvariable variable=e2eResourcesStarted]true")) {
					t.Fatalf("successful preflight did not enable cleanup before resource creation: %s, %s", calls, output)
				}
				marker, markerErr := os.ReadFile(filepath.Join(dir, "github-output"))
				if tc.conflict || tc.existingConfig {
					if !os.IsNotExist(markerErr) {
						t.Fatalf("failed preflight enabled GitHub cleanup: %q, %v", marker, markerErr)
					}
				} else if markerErr != nil || string(marker) != "resources_started=true\n" {
					t.Fatalf("successful preflight did not enable GitHub cleanup: %q, %v", marker, markerErr)
				}
			})
		}
	}
	t.Run("topology names", testTopologyNames)
	t.Run("bootstrap preflight", testBootstrapPreflight)
	hash := sha256.Sum256([]byte("test-subscription/test-rg/123-afd-1"))
	prefix := fmt.Sprintf("fne%x", hash[:8])
	calls := readAFDFile(t, log)
	for _, want := range []string{
		"--name " + prefix + "-hub",
		"--name " + prefix + "-member1",
		"--name " + prefix + "-member2",
		"--kube-context member-1-admin",
		"--kube-context member-2-admin",
		"--assignee-object-id 44444444-4444-4444-4444-444444444444",
		"--scope /subscriptions/test-subscription/resourceGroups/MC_" + prefix + "-member1",
		"--scope /subscriptions/test-subscription/resourceGroups/MC_" + prefix + "-member2",
	} {
		if !strings.Contains(calls, want) {
			t.Errorf("missing %q in calls:\n%s", want, calls)
		}
	}
	if strings.Count(calls, "--role Network Contributor") != 2 {
		t.Fatalf("expected one test-caller Network Contributor grant per member: %s", calls)
	}
}

func TestSharedGroupCleanupOwnership(t *testing.T) {
	tests := []struct {
		name        string
		conflict    bool
		profile     bool
		wantDeletes bool
	}{
		{"owned resources", false, false, true},
		{"foreign resource with colliding name", true, false, false},
		{"controller profile still present", false, true, false},
	}

	testControllerAndPeerings := func(t *testing.T) {
		for _, tc := range []struct {
			name                                                                  string
			invalidOwner, foreignPeer, lateProfile, principalMismatch, noProfiles bool
		}{{"valid", false, false, false, false, false}, {"wrong hub UID", true, false, false, false, false},
			{"foreign peering", false, true, false, false, false}, {"profile changed after stop", false, false, true, false, false},
			{"different test principal", false, false, false, true, false}, {"no profiles yet", false, false, false, false, true}} {
			t.Run(tc.name, func(t *testing.T) {
				afdTestEnvironment(t)
				t.Setenv("E2E_RUN_ID", "123-afd-1")
				t.Setenv("AZURE_SUBSCRIPTION_ID", "test-subscription")
				t.Setenv("AZURE_RESOURCE_GROUP", "fleet-networking-e2e")
				t.Setenv("E2E_TEST_PRINCIPAL_ID", "44444444-4444-4444-4444-444444444444")
				if tc.principalMismatch {
					t.Setenv("E2E_TEST_PRINCIPAL_ID", "66666666-6666-6666-6666-666666666666")
				}
				dir := t.TempDir()
				t.Setenv("KUBECONFIG", filepath.Join(dir, "kubeconfig"))
				group := "/subscriptions/test-subscription/resourceGroups/fleet-networking-e2e"
				hash := sha256.Sum256([]byte("test-subscription/fleet-networking-e2e/123-afd-1"))
				prefix := fmt.Sprintf("fne%x", hash[:8])
				hub := prefix + "-hub"
				hubID := group + "/providers/Microsoft.ContainerService/managedClusters/" + hub
				nodeGroup := "MC_fleet_e2e_hub"
				nodeID := "/subscriptions/test-subscription/resourceGroups/" + nodeGroup
				vnet1 := group + "/providers/Microsoft.Network/virtualNetworks/" + prefix + "-member-1-vnet"
				vnet2 := group + "/providers/Microsoft.Network/virtualNetworks/" + prefix + "-member-2-vnet"
				tags := map[string]string{"source": "fleet-networking", "fleet-e2e-run": "123-afd-1",
					"fleet-e2e-atm": "false", "fleet-e2e-afd": "true", "fleet-e2e-auto-approval": "true",
					"fleet-e2e-test-principal": "44444444-4444-4444-4444-444444444444"}
				uid := "11111111-1111-1111-1111-111111111111"
				gatewayUID := "22222222-2222-2222-2222-222222222222"
				gatewayHash := sha256.Sum256([]byte(fmt.Sprintf("%d:%s", len(gatewayUID), gatewayUID)))
				profileName := fmt.Sprintf("fleet-afd-%x", gatewayHash[:16])
				profileID := group + "/providers/Microsoft.Cdn/profiles/" + profileName
				trafficProfileID := group + "/providers/Microsoft.Network/trafficManagerProfiles/fleet-55555555-5555-5555-5555-555555555555"
				profileTags := map[string]string{
					"networking.fleet.azure.com.controller":        "networking.fleet.azure.com/afd",
					"networking.fleet.azure.com.hub-uid":           uid,
					"networking.fleet.azure.com.gateway-namespace": prefix + "-abcde",
					"networking.fleet.azure.com.gateway-name":      "private-http",
					"networking.fleet.azure.com.gateway-uid":       gatewayUID,
				}
				if tc.invalidOwner {
					profileTags["networking.fleet.azure.com.hub-uid"] = "33333333-3333-3333-3333-333333333333"
				}
				items := []map[string]interface{}{
					{"id": hubID, "name": hub, "type": "Microsoft.ContainerService/managedClusters", "tags": tags,
						"nodeResourceGroup": nodeGroup, "identity": map[string]interface{}{"principalId": "hub-principal"},
						"identityProfile": map[string]interface{}{"kubeletidentity": map[string]string{"objectId": "hub-kubelet"}}},
					{"id": vnet1, "name": prefix + "-member-1-vnet", "type": "Microsoft.Network/virtualNetworks", "tags": tags},
					{"id": vnet2, "name": prefix + "-member-2-vnet", "type": "Microsoft.Network/virtualNetworks", "tags": tags},
					{"id": group + "/providers/Microsoft.ContainerService/managedClusters/" + prefix + "-member3",
						"name": prefix + "-member3", "type": "Microsoft.ContainerService/managedClusters", "tags": tags},
					{"id": group + "/providers/Microsoft.ContainerService/managedClusters/" + prefix + "-member4",
						"name": prefix + "-member4", "type": "Microsoft.ContainerService/managedClusters", "tags": tags},
					{"id": profileID, "name": profileName, "type": "Microsoft.Cdn/profiles", "tags": profileTags},
					{"id": trafficProfileID, "name": "fleet-55555555-5555-5555-5555-555555555555", "type": "Microsoft.Network/trafficManagerProfiles",
						"tags": map[string]string{"networking.fleet.azure.com.trafficManagerProfile": prefix + "-fghij/hello-world-service"}},
				}
				if tc.noProfiles {
					items = items[:5]
				}
				peerings := []map[string]interface{}{
					{"id": vnet1 + "/virtualNetworkPeerings/to-two", "name": "to-two", "remoteVirtualNetwork": map[string]string{"id": vnet2}},
					{"id": vnet2 + "/virtualNetworkPeerings/to-one", "name": "to-one", "remoteVirtualNetwork": map[string]string{"id": vnet1}},
				}
				if tc.foreignPeer {
					peerings[0]["remoteVirtualNetwork"] = map[string]string{"id": group + "/providers/Microsoft.Network/virtualNetworks/foreign"}
				}
				assignments := []map[string]string{
					{"id": group + "/providers/Microsoft.Authorization/roleAssignments/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "scope": group, "name": "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
						"principalId": "hub-principal", "roleDefinitionId": "/providers/Microsoft.Authorization/roleDefinitions/de2b316d-7a2c-4143-b4cd-c148f6a355a1"},
					{"id": group + "/providers/Microsoft.Authorization/roleAssignments/foreign", "scope": group,
						"principalId": "foreign-principal", "roleDefinitionId": "/providers/Microsoft.Authorization/roleDefinitions/de2b316d-7a2c-4143-b4cd-c148f6a355a1"},
					{"id": nodeID + "/providers/Microsoft.Authorization/roleAssignments/bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", "scope": nodeID, "name": "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
						"principalId": "hub-kubelet", "roleDefinitionId": "/providers/Microsoft.Authorization/roleDefinitions/acdd72a7-3385-48ef-bd42-f606fba81ae7"},
					{"id": nodeID + "/providers/Microsoft.Authorization/roleAssignments/cccccccc-cccc-cccc-cccc-cccccccccccc", "scope": nodeID, "name": "cccccccc-cccc-cccc-cccc-cccccccccccc",
						"principalId": "44444444-4444-4444-4444-444444444444", "roleDefinitionId": "/providers/Microsoft.Authorization/roleDefinitions/4d97b98b-1d4f-4787-a291-c67834d212e7"},
				}
				for name, value := range map[string]interface{}{"inventory": items, "peerings": peerings, "assignments": assignments} {
					data, err := json.Marshal(value)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
						t.Fatal(err)
					}
				}
				t.Setenv("TEST_DIR", dir)
				t.Setenv("TEST_UID", uid)
				t.Setenv("TEST_HUB_ID", hubID)
				t.Setenv("TEST_NODE_ID", nodeID)
				t.Setenv("TEST_LATE_PROFILE", strconv.FormatBool(tc.lateProfile))
				writeAFDCommand(t, filepath.Join(dir, "az"), `#!/bin/bash
	set -euo pipefail
	printf 'az %s\n' "$*" >> "$TEST_DIR/calls"
	option() { local key="$1"; shift; while (($#)); do if [[ "$1" == "$key" ]]; then printf '%s' "$2"; return; fi; shift; done; return 1; }
	case "$1 $2 $3" in
	  "group show --subscription")
	    name="$(option --name "$@")"
	    if [[ "$name" == fleet-networking-e2e ]]; then
	      printf '%s\n' '{"id":"/subscriptions/test-subscription/resourceGroups/fleet-networking-e2e","tags":{"managedBy":"aks-rp/ci-resources","workload":"fleet-networking-e2e"}}'
	    else jq -cn --arg id "$TEST_NODE_ID" --arg owner "$TEST_HUB_ID" '{id:$id,managedBy:$owner}'; fi ;;
	  "group exists --subscription") echo true ;;
	  "group wait --subscription") ;;
	  "resource list --subscription")
	    count=0; if [[ -e "$TEST_DIR/listed" ]]; then count="$(cat "$TEST_DIR/listed")"; fi
	    count=$((count + 1)); echo "$count" > "$TEST_DIR/listed"
	    if ((count == 2)) && [[ "$TEST_LATE_PROFILE" == true ]]; then
	      jq 'map(if .type == "Microsoft.Cdn/profiles" then .tags.changed = "true" else . end)' "$TEST_DIR/inventory"
	    elif ((count <= 2)); then cat "$TEST_DIR/inventory"; else echo '[]'; fi ;;
	  "resource show --ids") jq --arg id "$4" '.[] | select(.id == $id)' "$TEST_DIR/inventory" ;;
	  "aks show --subscription") name="$(option --name "$@")"; jq --arg name "$name" '.[] | select(.name == $name)' "$TEST_DIR/inventory" ;;
	  "aks get-credentials --subscription"|"aks delete --subscription"|"aks wait --subscription"|"resource delete --ids") ;;
	  "network vnet peering")
	    if [[ "$4" == list ]]; then
	      name="$(option --vnet-name "$@")"
	      jq --arg name "$name" '[.[] | select(.id | contains("/virtualNetworks/" + $name + "/"))]' "$TEST_DIR/peerings"
	    elif [[ "$4" == delete ]]; then :; else exit 1; fi ;;
	  "role assignment list") cat "$TEST_DIR/assignments" ;;
	  "role assignment delete") ;;
	  *) echo "unexpected az command: $*" >&2; exit 1 ;;
	esac
	`)
				writeAFDCommand(t, filepath.Join(dir, "kubectl"), `#!/bin/bash
	set -euo pipefail
	printf 'kubectl %s\n' "$*" >> "$TEST_DIR/calls"
	case "$*" in
	  *"get namespace kube-system"*) printf '%s' "$TEST_UID" ;;
	  *"get deployment"*) echo deployment/hub-net-controller-manager ;;
	  *"scale"*|*"rollout status"*) ;;
	  *) exit 1 ;;
	esac
	`)
				t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
				output, err := runAFDScript(t, "./cleanup.sh")
				if (err != nil) != (tc.invalidOwner || tc.foreignPeer || tc.lateProfile || tc.principalMismatch) {
					t.Fatalf("cleanup: %s (%v)", output, err)
				}
				calls := readAFDFile(t, filepath.Join(dir, "calls"))
				if tc.invalidOwner || tc.foreignPeer || tc.lateProfile || tc.principalMismatch {
					if strings.Contains(calls, " delete ") || (!tc.lateProfile && strings.Contains(calls, " scale ")) {
						t.Fatalf("unsafe mutation after owner mismatch: %s", calls)
					}
					return
				}
				wants := []string{"aks delete", "aks wait", "kubectl --context hub-admin -n fleet-system scale",
					"--name " + prefix + "-member3", "--name " + prefix + "-member4",
					"role assignment delete --ids " + group + "/providers/Microsoft.Authorization/roleAssignments/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
					"role assignment delete --ids " + nodeID + "/providers/Microsoft.Authorization/roleAssignments/bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
					"role assignment delete --ids " + nodeID + "/providers/Microsoft.Authorization/roleAssignments/cccccccc-cccc-cccc-cccc-cccccccccccc",
					"group wait",
					"network vnet peering delete --ids " + vnet1, "network vnet peering delete --ids " + vnet2}
				if !tc.noProfiles {
					wants = append(wants, "resource delete --ids "+profileID, "resource delete --ids "+trafficProfileID)
				}
				for _, want := range wants {
					if !strings.Contains(calls, want) {
						t.Errorf("missing %q in calls:\n%s", want, calls)
					}
				}
				if strings.Contains(calls, "roleAssignments/foreign") || strings.Contains(calls, "group delete") {
					t.Fatalf("cleanup touched foreign resources: %s", calls)
				}
				if strings.Index(calls, "roleAssignments/bbbbbbbb") > strings.Index(calls, "aks delete") ||
					strings.Index(calls, "roleAssignments/aaaaaaaa") < strings.Index(calls, "aks wait") {
					t.Fatalf("role deletion must preserve control-plane network access through AKS deletion: %s", calls)
				}
			})
		}
	}
	t.Run("controller and peerings", testControllerAndPeerings)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			afdTestEnvironment(t)
			t.Setenv("E2E_RUN_ID", "123-atm-1")
			t.Setenv("AZURE_SUBSCRIPTION_ID", "test-subscription")
			t.Setenv("AZURE_RESOURCE_GROUP", "fleet-networking-e2e")
			dir := t.TempDir()
			group := "/subscriptions/test-subscription/resourceGroups/fleet-networking-e2e"
			hash := sha256.Sum256([]byte("test-subscription/fleet-networking-e2e/123-atm-1"))
			prefix := fmt.Sprintf("fne%x", hash[:8])
			registry := prefix + "acr"
			tags := map[string]string{"source": "fleet-networking", "fleet-e2e-run": "123-atm-1"}
			if tt.conflict {
				tags["fleet-e2e-run"] = "another-run"
			}
			items := []map[string]interface{}{
				{"id": group + "/providers/Microsoft.ContainerRegistry/registries/" + registry,
					"name": registry, "type": "Microsoft.ContainerRegistry/registries", "tags": tags},
				{"id": group + "/providers/Microsoft.Network/virtualNetworks/other-run",
					"name": "other-run", "type": "Microsoft.Network/virtualNetworks",
					"tags": map[string]string{"fleet-e2e-run": "another-run"}},
			}
			if tt.profile {
				items = append(items, map[string]interface{}{
					"id":   group + "/providers/Microsoft.Network/trafficManagerProfiles/fleet-123",
					"name": "fleet-123", "type": "Microsoft.Network/trafficManagerProfiles",
					"tags": map[string]string{"networking.fleet.azure.com.trafficManagerProfile": prefix + "-abcde/service"},
				})
			}
			contents, err := json.Marshal(items)
			if err != nil {
				t.Fatal(err)
			}
			inventory := filepath.Join(dir, "inventory.json")
			if err := os.WriteFile(inventory, contents, 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("TEST_INVENTORY", inventory)
			t.Setenv("TEST_CALLS", filepath.Join(dir, "calls"))
			t.Setenv("TEST_LISTED", filepath.Join(dir, "listed"))
			writeAFDCommand(t, filepath.Join(dir, "az"), `#!/bin/bash
	set -euo pipefail
	printf '%s\n' "$*" >> "$TEST_CALLS"
	case "$1 $2" in
	  "group show") printf '%s\n' '{"id":"/subscriptions/test-subscription/resourceGroups/fleet-networking-e2e","tags":{"managedBy":"aks-rp/ci-resources","workload":"fleet-networking-e2e"}}' ;;
	  "resource list")
	    if [[ -e "$TEST_LISTED" ]]; then echo '[]'; else touch "$TEST_LISTED"; cat "$TEST_INVENTORY"; fi ;;
	  "resource show") jq --arg id "$4" '.[] | select(.id == $id)' "$TEST_INVENTORY" ;;
	  "role assignment") [[ "$3" == list ]] && echo '[]' ;;
	  "resource delete") ;;
	  *) echo "unexpected az command: $*" >&2; exit 1 ;;
	esac
	`)
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			output, err := runAFDScript(t, "./cleanup.sh")
			if (err == nil) != tt.wantDeletes {
				t.Fatalf("cleanup: %s (%v)", output, err)
			}
			calls := readAFDFile(t, filepath.Join(dir, "calls"))
			if strings.Contains(calls, "group delete") || strings.Contains(calls, "other-run") {
				t.Fatalf("cleanup touched foreign resources: %s", calls)
			}
			if strings.Contains(calls, "resource delete") != tt.wantDeletes {
				t.Fatalf("unexpected resource deletion: %s", calls)
			}
		})
	}
}
