/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package scripts

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func TestAFDCommandFailureStopsWork(t *testing.T) {
	tests := []struct {
		name, script, command, forbidden string
	}{
		{"identity read", "install-afd.sh", "aks show", "role assignment create"},
		{"installation role write", "install-afd.sh", "role assignment create", "helm upgrade"},
		{"member chart", "install-afd.sh", "helm upgrade", "aks show --subscription test-subscription --resource-group test-rg --name hub"},
		{"manifest apply", "install-afd.sh", "kubectl apply", "helm install"},
		{"Gateway chart", "install-afd.sh", "helm install", ""},
		{"PLS read", "afd-pls-reader.sh", "network private-link-service show", "role assignment create"},
		{"PLS role write", "afd-pls-reader.sh", "role assignment create", "role assignment list"},
		{"PLS role observation", "afd-pls-reader.sh", "role assignment list", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			afdTestEnvironment(t)
			log := installAFDCommandStubs(t)
			t.Setenv("AFD_TEST_FAIL_COMMAND", tt.command)
			output, err := runAFDScript(t, afdScriptArguments(tt.script)...)
			if err == nil || !strings.Contains(output, "injected CLI failure: "+tt.command) {
				t.Fatalf("command failure was hidden: %s: %v", output, err)
			}
			commands := readAFDFile(t, log)
			if tt.forbidden != "" && strings.Contains(commands, tt.forbidden) {
				t.Fatalf("work continued after a rejected command: %s", commands)
			}
			if tt.script == "afd-pls-reader.sh" && strings.Count(commands, "role assignment create") > 1 {
				t.Fatalf("failed first binding allowed the second grant: %s", commands)
			}
		})
	}
}

func TestAFDBootstrapStopsBeforeAzureOnPreflightFailure(t *testing.T) {
	afdTestEnvironment(t)
	log := installAFDCommandStubs(t)
	bin := t.TempDir()
	writeAFDCommand(t, filepath.Join(bin, "yq"), "#!/bin/bash\nexit 0\n")
	writeAFDCommand(t, filepath.Join(bin, "docker"), "#!/bin/bash\nexit 0\n")
	writeAFDCommand(t, filepath.Join(bin, "go"), `#!/bin/bash
set -euo pipefail
[[ "$*" == "test -count=1 ./test/scripts" && "${AFD_VALIDATE_LOCAL_TOOLS:-}" == true ]]
echo "injected offline preflight failure" >&2
exit 23
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	output, err := runAFDScript(t, "./bootstrap.sh")
	if err == nil || !strings.Contains(output, "injected offline preflight failure") {
		t.Fatalf("bootstrap did not enforce local validation: %s: %v", output, err)
	}
	if commands, err := os.ReadFile(log); !os.IsNotExist(err) {
		t.Fatalf("bootstrap invoked Azure despite failed preflight: %s, %v", commands, err)
	}
}

func TestAFDAppImageAuthentication(t *testing.T) {
	contents := readAFDFile(t, "bootstrap.sh")
	_, publication, found := strings.Cut(contents, "# Build and publish the app image dedicated for fleet networking test.")
	if !found {
		t.Fatal("bootstrap app-publication section is missing")
	}
	const start = `if [ "${AZURE_NETWORK_SETTING}" != "perf-test" ]`
	_, remaining, found := strings.Cut(publication, start)
	if !found {
		t.Fatal("bootstrap app-publication block is missing")
	}
	block, _, found := strings.Cut(remaining, `if [ "${AZURE_NETWORK_SETTING}" == "perf-test" ]`)
	if !found {
		t.Fatal("bootstrap app-publication block boundary is missing")
	}
	for _, tt := range []struct {
		name, network, failure, commands string
	}{
		{"refresh expired login", "shared-vnet", "", "docker build\naz acr login\ndocker push\n"},
		{"refresh failure stops push", "shared-vnet", "acr login", "docker build\naz acr login\n"},
		{"performance test skips publication", "perf-test", "", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			afdTestEnvironment(t)
			t.Setenv("AZURE_NETWORK_SETTING", tt.network)
			t.Setenv("REGISTRY", "testrg.azurecr.io")
			t.Setenv("REGISTRY_NAME", "testrg")
			t.Setenv("AFD_TEST_FAIL_COMMAND", tt.failure)
			dir := t.TempDir()
			log := filepath.Join(dir, "commands")
			t.Setenv("AFD_TEST_COMMAND_LOG", log)
			t.Setenv("AFD_TEST_AUTH_STATE", filepath.Join(dir, "auth-state"))
			if err := os.WriteFile(log, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			writeAFDCommand(t, filepath.Join(dir, "az"), `#!/bin/bash
set -euo pipefail
[[ "$*" == "acr login -n testrg" ]]
printf 'az acr login\n' >>"$AFD_TEST_COMMAND_LOG"
if [[ "$AFD_TEST_FAIL_COMMAND" == "acr login" ]]; then
  echo "injected registry login failure" >&2
  exit 23
fi
printf 'fresh' >"$AFD_TEST_AUTH_STATE"
`)
			writeAFDCommand(t, filepath.Join(dir, "docker"), `#!/bin/bash
set -euo pipefail
printf 'docker %s\n' "$1" >>"$AFD_TEST_COMMAND_LOG"
case "$1" in
  build)
    [[ "${!#}" == testrg.azurecr.io/app ]]
    printf 'expired' >"$AFD_TEST_AUTH_STATE" ;;
  push)
    [[ $# == 2 && "$2" == testrg.azurecr.io/app ]]
    if [[ "$(cat "$AFD_TEST_AUTH_STATE")" != fresh ]]; then
      echo "stale registry authentication" >&2
      exit 24
    fi ;;
  *) echo "unexpected Docker operation" >&2; exit 2 ;;
esac
`)
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			output, err := runAFDScript(t, "-ec", "set -euo pipefail\n"+start+block)
			if (err != nil) != (tt.failure != "") || readAFDFile(t, log) != tt.commands {
				t.Fatalf("publication output=%s error=%v commands=%q; want %q",
					output, err, readAFDFile(t, log), tt.commands)
			}
			if tt.failure != "" && !strings.Contains(output, "injected registry login failure") {
				t.Fatalf("registry login failure was not surfaced: %s", output)
			}
		})
	}
}

func TestAFDRenderedInstallation(t *testing.T) {
	afdTestEnvironment(t)
	helm, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("Helm is required to render the real installation arguments")
	}
	installAFDCommandStubs(t)
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	rendered := t.TempDir()
	t.Setenv("AFD_TEST_REAL_HELM", helm)
	t.Setenv("AFD_TEST_REPO_ROOT", root)
	t.Setenv("AFD_TEST_RENDERED", rendered)
	output, err := runAFDScript(t, "./install-afd.sh")
	if err != nil {
		t.Fatalf("actual chart rendering rejected installer arguments: %s: %v", output, err)
	}
	for _, cluster := range []string{"member-1", "member-2", "hub"} {
		contents := readAFDFile(t, filepath.Join(rendered, cluster+"-admin.yaml"))
		deployment, cloud := decodeAFDRender(t, contents)
		if cluster == "hub" {
			if cloud != nil {
				t.Fatal("Gateway chart must consume the prepared Secret rather than generate different configuration")
			}
			container := deployment.Spec.Template.Spec.Containers[0]
			if container.Image != "testrg.azurecr.io/hub-gateway-controller-manager:candidate" ||
				deployment.Spec.Template.Spec.ServiceAccountName != "hub-gateway-controller-manager" ||
				!slices.Contains(container.Args, "--enable-afd=true") ||
				!slices.Contains(container.Args, "--afd-resource-group=test-rg") {
				t.Fatalf("installer rendered the wrong Gateway runtime: %+v", deployment)
			}
			foundSecret := false
			for _, volume := range deployment.Spec.Template.Spec.Volumes {
				if volume.Secret != nil && volume.Secret.SecretName == "afd-cloud-config" {
					foundSecret = slices.ContainsFunc(volume.Secret.Items, func(item corev1.KeyToPath) bool {
						return item.Key == "azure.json" && item.Path == "azure.json"
					})
				}
			}
			if !foundSecret {
				t.Fatal("Gateway Deployment does not mount the prepared Azure configuration Secret")
			}
			continue
		}
		container := deployment.Spec.Template.Spec.Containers[0]
		if !slices.Contains(container.Args, "--enable-private-link-origin-discovery=true") ||
			!slices.Contains(container.Args, "--enable-traffic-manager-feature=false") ||
			!slices.Contains(container.Args, "--cloud-config=/etc/kubernetes/provider/azure.json") {
			t.Fatalf("installer rendered the wrong member arguments: %v", container.Args)
		}
		assertAFDCloudConfig(t, cloud, cluster)
	}
	secret := readAFDFile(t, filepath.Join(rendered, "hub-secret.json"))
	var config map[string]interface{}
	if err := json.Unmarshal([]byte(secret), &config); err != nil {
		t.Fatal(err)
	}
	assertAFDCloudConfig(t, config, "hub")
}

// This optional, offline check consults installed command help; it never invokes Azure operations.
func TestAFDInstalledAzureCLIOptions(t *testing.T) {
	if os.Getenv("AFD_VALIDATE_LOCAL_TOOLS") != "true" {
		t.Skip("Set AFD_VALIDATE_LOCAL_TOOLS=true to check installed Azure CLI command/option metadata")
	}
	afdTestEnvironment(t)
	azure, err := exec.LookPath("az")
	if err != nil {
		t.Fatal("Azure CLI is required for the requested local-tool compatibility check")
	}
	log := installAFDCommandStubs(t)
	for _, script := range []string{"install-afd.sh", "afd-pls-reader.sh"} {
		if output, err := runAFDScript(t, afdScriptArguments(script)...); err != nil {
			t.Fatalf("could not capture installer commands: %s: %v", output, err)
		}
	}
	decoder := json.NewDecoder(strings.NewReader(readAFDFile(t, log+".jsonl")))
	help := map[string]string{}
	for {
		var args []string
		if err := decoder.Decode(&args); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if args[0] != "az" {
			continue
		}
		firstOption := 1
		for firstOption < len(args) && !strings.HasPrefix(args[firstOption], "-") {
			firstOption++
		}
		command := strings.Join(args[1:firstOption], " ")
		if _, found := help[command]; !found {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			output, err := exec.CommandContext(ctx, azure, append(args[1:firstOption], "--help")...).CombinedOutput()
			cancel()
			if err != nil {
				t.Fatalf("installed Azure CLI lacks %s: %s: %v", command, output, err)
			}
			help[command] = string(output)
		}
		for _, arg := range args[firstOption:] {
			if strings.HasPrefix(arg, "-") && !regexp.MustCompile(`(^|[\s,])`+
				regexp.QuoteMeta(strings.SplitN(arg, "=", 2)[0])+`([\s,:\]]|$)`).MatchString(help[command]) {
				t.Errorf("installed Azure CLI does not document option %s for %s", arg, command)
			}
		}
	}
}

func installAFDCommandStubs(t *testing.T) string {
	t.Helper()
	t.Setenv("ENABLE_AZURE_FRONT_DOOR", "true")
	t.Setenv("AFD_TEST_MANAGED_BY", "member-1")
	log := filepath.Join(t.TempDir(), "commands")
	t.Setenv("AFD_TEST_COMMAND_LOG", log)
	bin := t.TempDir()
	for name, stub := range map[string]string{"az": afdAzureStub, "helm": afdHelmStub, "kubectl": afdKubectlStub} {
		writeAFDCommand(t, filepath.Join(bin, name), stub)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

func afdScriptArguments(script string) []string {
	if script != "afd-pls-reader.sh" {
		return []string{"./" + script}
	}
	const prefix = "/subscriptions/test-subscription/resourceGroups/"
	const suffix = "/providers/Microsoft.Network/privateLinkServices/proof"
	return []string{"./" + script, "proof", prefix + "MC_member-1" + suffix, prefix + "MC_member-2" + suffix}
}

func readAFDFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func decodeAFDRender(t *testing.T, contents string) (appsv1.Deployment, map[string]interface{}) {
	t.Helper()
	var deployment appsv1.Deployment
	var cloud map[string]interface{}
	decoder := yaml.NewYAMLOrJSONDecoder(strings.NewReader(contents), 4096)
	for {
		var document json.RawMessage
		if err := decoder.Decode(&document); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		var kind metav1.TypeMeta
		if err := json.Unmarshal(document, &kind); err != nil {
			t.Fatal(err)
		}
		switch kind.Kind {
		case "Deployment":
			if err := json.Unmarshal(document, &deployment); err != nil {
				t.Fatal(err)
			}
		case "Secret":
			var secret corev1.Secret
			if err := json.Unmarshal(document, &secret); err != nil {
				t.Fatal(err)
			}
			if data, found := secret.Data["azure.json"]; found {
				if err := json.Unmarshal(data, &cloud); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if deployment.Name == "" {
		t.Fatal("installer rendered no Deployment")
	}
	return deployment, cloud
}

func assertAFDCloudConfig(t *testing.T, got map[string]interface{}, cluster string) {
	t.Helper()
	group := "MC_" + cluster
	if cluster == "hub" {
		group = "test-rg"
	}
	want := map[string]interface{}{
		"cloud": "AzurePublicCloud", "subscriptionId": "test-subscription", "tenantId": "test-tenant",
		"useManagedIdentityExtension": true, "userAssignedIdentityID": cluster + "-client",
		"resourceGroup": group, "location": "test-location",
	}
	for name, value := range want {
		if got[name] != value {
			t.Errorf("%s configuration %s = %v, want %v", cluster, name, got[name], value)
		}
	}
}

const afdHelmStub = `#!/bin/bash
set -euo pipefail
printf 'helm %s\n' "$*" >>"$AFD_TEST_COMMAND_LOG"
verb="$1"; release="$2"; chart="$3"; shift 3
case "$verb $release $chart" in
  "upgrade member-net-controller-manager ./charts/member-net-controller-manager"|\
  "install hub-gateway-controller-manager ./charts/hub-gateway-controller-manager") ;;
  *) echo "invalid Helm command/chart: $verb $release $chart" >&2; exit 2 ;;
esac
context=""; namespace=""; wait=false; timeout=""; reuse=false; values=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --kube-context) context="$2"; shift 2 ;;
    --namespace) namespace="$2"; shift 2 ;;
    --wait) wait=true; shift ;;
    --timeout) timeout="$2"; shift 2 ;;
    --reuse-values) reuse=true; shift ;;
    --set|--set-string|--set-json) values+=("$1" "$2"); shift 2 ;;
    *) echo "invalid Helm argument: $1" >&2; exit 2 ;;
  esac
done
[[ "$wait" == true && "$timeout" == 10m && -n "$context" && -n "$namespace" ]]
if [[ "$verb" == upgrade ]]; then
  [[ "$reuse" == true && "$namespace" == default && "$context" =~ ^member-[12]-admin$ ]]
else
  [[ "$reuse" == false && "$context" == hub-admin && "$namespace" == fleet-system ]]
fi
if [[ "${AFD_TEST_FAIL_COMMAND:-}" == "helm $verb" ]]; then
  echo "injected CLI failure: helm $verb" >&2; exit 23
fi
if [[ -n "${AFD_TEST_REAL_HELM:-}" ]]; then
  "$AFD_TEST_REAL_HELM" template "$release" "$AFD_TEST_REPO_ROOT/${chart#./}" --namespace "$namespace" \
    "${values[@]}" >"$AFD_TEST_RENDERED/$context.yaml"
fi
`

const afdKubectlStub = `#!/bin/bash
set -euo pipefail
printf 'kubectl %s\n' "$*" >>"$AFD_TEST_COMMAND_LOG"
[[ "$1" == --context && "$2" == hub-admin ]]; shift 2
namespace=""
if [[ "$1" == --namespace ]]; then namespace="$2"; shift 2; fi
verb="$1"
if [[ "${AFD_TEST_FAIL_COMMAND:-}" == "kubectl $verb" ]]; then
  echo "injected CLI failure: kubectl $verb" >&2; exit 23
fi
case "$*" in
  "create namespace fleet-system --dry-run=client -o yaml")
    [[ -z "$namespace" ]]
    printf '{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"fleet-system"}}\n' ;;
  "apply -f -")
    jq -e '(.kind == "Namespace" or .kind == "Secret") and .metadata.name != null' >/dev/null ;;
  *)
    [[ "$namespace" == fleet-system && $# == 8 &&
       "$1 $2 $3 $4" == "create secret generic afd-cloud-config" &&
       "$5" == --from-literal=azure.json=* && "$6 $7 $8" == "--dry-run=client -o yaml" ]] || {
      echo "invalid kubectl command: $*" >&2; exit 2;
    }
    config="${5#--from-literal=azure.json=}"
    jq -e . <<<"$config" >/dev/null
    if [[ -n "${AFD_TEST_RENDERED:-}" ]]; then printf '%s\n' "$config" >"$AFD_TEST_RENDERED/hub-secret.json"; fi
    jq -cn --arg value "$config" '{apiVersion:"v1",kind:"Secret",
      metadata:{name:"afd-cloud-config",namespace:"fleet-system"},data:{"azure.json":($value|@base64)}}' ;;
esac
`
