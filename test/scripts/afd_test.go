/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package scripts

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAFDConfiguration(t *testing.T) {
	tests := []struct {
		name, enabled, network, group, want string
		fails                               bool
	}{
		{"default off", "", "shared-vnet", "test-rg", "false|false||", false},
		{"AFD without ATM", "true", "shared-vnet", "test-rg", "false|true|true|true", false},
		{"unsupported topology", "true", "peered-vnet", "test-rg", "requires shared-vnet", true},
		{"invalid flag", "yes", "shared-vnet", "test-rg", "must be true or false", true},
		{"missing group", "true", "shared-vnet", "", "AZURE_RESOURCE_GROUP", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			afdTestEnvironment(t)
			t.Setenv("ENABLE_AZURE_FRONT_DOOR", tt.enabled)
			t.Setenv("AZURE_NETWORK_SETTING", tt.network)
			t.Setenv("AZURE_RESOURCE_GROUP", tt.group)
			output, err := runAFDScript(t, "-ec", `source ./e2e-config.sh
printf '%s|%s|%s|%s\n' "$ENABLE_TRAFFIC_MANAGER" "$ENABLE_AZURE_FRONT_DOOR" "${ENABLE_GATEWAY_API:-}" "${ENABLE_AFD_PRIVATE_HTTP_PROOF:-}"`)
			if (err != nil) != tt.fails || !strings.Contains(output, tt.want) {
				t.Fatalf("configuration: output=%s error=%v", output, err)
			}
		})
	}
}

func TestAFDStandardTestCommand(t *testing.T) {
	for _, enabled := range []string{"false", "true"} {
		t.Run(enabled, func(t *testing.T) {
			afdTestEnvironment(t)
			t.Setenv("ENABLE_AZURE_FRONT_DOOR", enabled)
			bin := t.TempDir()
			goStub := "#!/bin/bash\nprintf '%s\\n' \"${AFD_PROOF_CONTROLLER_IMAGE:-}\" \"$@\"\n"
			writeAFDCommand(t, filepath.Join(bin, "go"), goStub)
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			output, err := runAFDScript(t, "./run-e2e.sh")
			if err != nil {
				t.Fatalf("test runner failed: %s: %v", output, err)
			}
			if enabled == "true" {
				for _, want := range []string{
					"testrg.azurecr.io/hub-gateway-controller-manager:candidate",
					"-count=1", "10h", "-ginkgo.timeout=9h30m",
				} {
					if !strings.Contains(output, want) {
						t.Errorf("missing %q in %s", want, output)
					}
				}
				if strings.Contains(output, "label-filter") {
					t.Fatal("standard entry must include baseline scenarios, not only AFD")
				}
			} else if !strings.Contains(output, "50m") || strings.Contains(output, "10h") {
				t.Fatalf("default suite deadline changed: %s", output)
			}
		})
	}
}

func TestAFDPLSReaderScope(t *testing.T) {
	tests := []struct {
		name, target, second, managedBy string
		fails                           bool
		missingBinding                  bool
		wantWrites                      int
	}{
		{"exact test PLS", "proof", "proof", "member-1", false, false, 2},
		{"different PLS", "unrelated", "proof", "member-1", true, false, 0},
		{"different second PLS", "proof", "unrelated", "member-1", true, false, 0},
		{"different cluster ownership", "proof", "proof", "unrelated", true, false, 0},
		{"missing binding observation", "proof", "proof", "member-1", true, true, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			afdTestEnvironment(t)
			t.Setenv("ENABLE_AZURE_FRONT_DOOR", "true")
			t.Setenv("AFD_TEST_MANAGED_BY", tt.managedBy)
			t.Setenv("AFD_TEST_EMPTY_BINDINGS", "")
			if tt.missingBinding {
				t.Setenv("AFD_TEST_EMPTY_BINDINGS", "true")
			}
			log := filepath.Join(t.TempDir(), "commands")
			t.Setenv("AFD_TEST_COMMAND_LOG", log)
			bin := t.TempDir()
			writeAFDCommand(t, filepath.Join(bin, "az"), afdAzureStub)
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			prefix := "/subscriptions/test-subscription/resourceGroups/"
			output, err := runAFDScript(t, "./afd-pls-reader.sh", "proof",
				prefix+"MC_member-1/providers/Microsoft.Network/privateLinkServices/"+tt.target,
				prefix+"MC_member-2/providers/Microsoft.Network/privateLinkServices/"+tt.second)
			if (err != nil) != tt.fails {
				t.Fatalf("scope validation: output=%s error=%v", output, err)
			}
			commands, readErr := os.ReadFile(log)
			if readErr != nil {
				t.Fatal(readErr)
			}
			writes := strings.Count(string(commands), "role assignment create")
			if writes != tt.wantWrites || strings.Contains(string(commands), "approve") {
				t.Fatalf("unexpected Reader writes or approvals: %s", commands)
			}
			if tt.missingBinding && !strings.Contains(output, "not independently observed") {
				t.Fatalf("missing binding evidence did not surface explicitly: %s", output)
			}
		})
	}
}

const afdAzureStub = `#!/bin/bash
set -euo pipefail
printf '%s\n' "$*" >>"$AFD_TEST_COMMAND_LOG"
jq -cn --args '$ARGS.positional' -- az "$@" >>"$AFD_TEST_COMMAND_LOG.jsonl"
kind="$1 $2"; shift 2
verb="${1:-}"
if [[ "$kind" == "role assignment" || "$kind" == "network private-link-service" ]]; then
  kind="$kind $verb"
  shift
fi
case "$kind" in
  "account show") required="--subscription --query -o"; allowed="$required" ;;
  "aks show") required="--subscription --resource-group --name"; allowed="$required" ;;
  "group show") required="--subscription --name"; allowed="$required" ;;
  "network private-link-service show") required="--subscription --ids"; allowed="$required" ;;
  "role assignment create")
    required="--subscription --role --assignee-object-id --assignee-principal-type --scope"; allowed="$required" ;;
  "role assignment list") required="--subscription --scope"; allowed="$required --all" ;;
  *) echo "unexpected Azure operation: $kind" >&2; exit 1 ;;
esac
declare -A options=()
while [[ $# -gt 0 ]]; do
  key="$1"
  [[ " $allowed " == *" $key "* && -z "${options[$key]+found}" ]] || {
    echo "invalid or repeated argument for $kind: $key" >&2; exit 2;
  }
  if [[ "$key" == --all ]]; then
    options[$key]=true
    shift
  else
    [[ $# -ge 2 && -n "$2" && "$2" != -* ]] || { echo "missing value for $key" >&2; exit 2; }
    options[$key]="$2"
    shift 2
  fi
done
for key in $required; do
  [[ -n "${options[$key]:-}" ]] || { echo "missing required argument $key" >&2; exit 2; }
done
[[ "${options[--subscription]}" == test-subscription ]] || { echo "wrong subscription" >&2; exit 2; }
if [[ "$kind" == "role assignment list" && -n "${options[--all]:-}" && -n "${options[--scope]:-}" ]]; then
  echo "ERROR: group or scope are not required when --all is used" >&2
  exit 2
fi
if [[ "${AFD_TEST_FAIL_COMMAND:-}" == "$kind" ]]; then
  echo "injected CLI failure: $kind" >&2
  exit 23
fi
name="${options[--name]:-}"
scope="${options[--scope]:-}"
id="${options[--ids]:-}"
base="/subscriptions/test-subscription/resourceGroups/test-rg/providers/Microsoft.ContainerService/managedClusters"
case "$kind" in
  "account show")
    [[ "${options[--query]}" == tenantId && "${options[-o]}" == tsv ]]
    printf 'test-tenant\n' ;;
  "aks show")
    printf '{"id":"%s/%s","nodeResourceGroup":"MC_%s","location":"test-location","identityProfile":{"kubeletidentity":{"objectId":"%s-principal","clientId":"%s-client"}}}\n' "$base" "$name" "$name" "$name" "$name" ;;
  "group show")
    member="${name#MC_}"
    if [[ "$member" == member-1 ]]; then member="$AFD_TEST_MANAGED_BY"; fi
    printf '{"managedBy":"%s/%s"}\n' "$base" "$member" ;;
  "network private-link-service show")
    printf '{"id":"%s","provisioningState":"Succeeded"}\n' "$id" ;;
  "role assignment create"|"role assignment list")
    if [[ "$verb" == list && "${AFD_TEST_EMPTY_BINDINGS:-}" == true ]]; then printf '[]\n'; exit 0; fi
    printf '[{"principalId":"hub-principal","scope":"%s","roleDefinitionId":"/roles/acdd72a7-3385-48ef-bd42-f606fba81ae7"}]\n' "$scope" ;;
  *) echo "unexpected Azure operation: $kind" >&2; exit 1 ;;
esac
`

func TestAFDInstallationWithoutATM(t *testing.T) {
	afdTestEnvironment(t)
	log := installAFDCommandStubs(t)
	output, err := runAFDScript(t, "./install-afd.sh")
	if err != nil {
		t.Fatalf("installation failed: %s: %v", output, err)
	}
	commands, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"--role CDN Profile Contributor", "--role Reader",
		"--scope /subscriptions/test-subscription/resourceGroups/MC_member-1",
		"--scope /subscriptions/test-subscription/resourceGroups/MC_member-2",
		"helm install hub-gateway-controller-manager", "--set afd.enabled=true",
		"--set enablePrivateLinkOriginDiscovery=true", "--kube-context member-1-admin",
		"--kube-context member-2-admin", "testrg.azurecr.io/hub-gateway-controller-manager",
		"--assignee-object-id member-1-principal", "--assignee-object-id member-2-principal",
	} {
		if !strings.Contains(string(commands), want) {
			t.Errorf("missing installation prerequisite %q in %s", want, commands)
		}
	}
	if strings.Contains(string(commands), "Network Contributor") || strings.Contains(string(commands), "approve") ||
		strings.Count(string(commands), "role assignment create") != 3 {
		t.Fatalf("installation exceeded the bounded role preparation: %s", commands)
	}
}

func afdTestEnvironment(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Run POSIX harness tests with Go inside WSL, not a Windows Go process")
	}
	for _, name := range []string{
		"ENABLE_TRAFFIC_MANAGER", "ENABLE_AZURE_FRONT_DOOR", "ENABLE_GATEWAY_API",
		"ENABLE_AFD_PRIVATE_HTTP_PROOF", "AFD_PROOF_CONTROLLER_IMAGE", "REGISTRY",
		"HUB_CLUSTER", "MEMBER_CLUSTER_1", "MEMBER_CLUSTER_2",
		"HUB_GATEWAY_CONTROLLER_MANAGER_IMAGE_NAME", "HUB_GATEWAY_CONTROLLER_MANAGER_IMAGE_VERSION",
		"AFD_PROOF_CONTROLLER_NAMESPACE", "AFD_PROOF_CONTROLLER_DEPLOYMENT",
		"AFD_TEST_FAIL_COMMAND", "AFD_TEST_EMPTY_BINDINGS", "AFD_TEST_REAL_HELM",
		"AFD_TEST_RENDERED",
	} {
		t.Setenv(name, "")
	}
	t.Setenv("AZURE_SUBSCRIPTION_ID", "test-subscription")
	t.Setenv("AZURE_RESOURCE_GROUP", "test-rg")
	t.Setenv("AZURE_NETWORK_SETTING", "shared-vnet")
	t.Setenv("TAG", "candidate")
}

func runAFDScript(t *testing.T, args ...string) (string, error) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("Bash is required for the E2E script tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "bash", args...).CombinedOutput()
	return string(output), err
}

func writeAFDCommand(t *testing.T, path, contents string) {
	t.Helper()
	// #nosec G306 -- Private mock executables need owner read/execute, with no group/world access.
	if err := os.WriteFile(path, []byte(contents), 0o500); err != nil {
		t.Fatal(err)
	}
}
