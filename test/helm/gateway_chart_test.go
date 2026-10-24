//go:build helm

/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package helm

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/yaml"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"go.goms.io/fleet-networking/pkg/common/objectmeta"
)

const (
	chartNamespace       = "fleet-system"
	chartRepository      = "fleet-networking/hub-gateway-controller-manager"
	chartImage           = chartRepository + ":dev"
	enabledConfiguration = "afd.enabled=true,afd.resourceGroup=afd-rg,afd.cloudConfigSecret.name=platform-azure"
	helmSetFlag          = "--set"
	helmSetStringFlag    = "--set-string"
	cloudConfigKey       = "azure.json"
	existingAccount      = "platform-gateway"
	verbCreate           = "create"
	verbGet              = "get"
	verbList             = "list"
	verbPatch            = "patch"
	verbUpdate           = "update"
	verbWatch            = "watch"
)

var chartPath = filepath.Join("..", "..", "charts", "hub-gateway-controller-manager")

func TestGatewayChartDisabled(t *testing.T) {
	got := renderChart(t)
	if got.deployment == nil || got.account == nil || got.role == nil || got.binding == nil ||
		got.clusterRole != nil || got.clusterBinding != nil || got.class != nil {
		t.Fatalf("disabled resource inventory = %#v", got)
	}
	deployment, container := got.deployment, got.deployment.Spec.Template.Spec.Containers[0]
	if deployment.Namespace != chartNamespace || got.account.Namespace != chartNamespace ||
		deployment.Spec.Template.Spec.ServiceAccountName != got.account.Name {
		t.Fatal("Deployment and ServiceAccount do not share the release namespace/identity")
	}
	wantArgs := []string{"--enable-afd=false", "--leader-elect=true", "--leader-election-namespace=" + chartNamespace,
		"--metrics-bind-address=:8082", "--health-probe-bind-address=:8083", "--v=2"}
	if container.Image != chartImage || !reflect.DeepEqual(container.Args, wantArgs) {
		t.Fatalf("image/args = %q/%v, want %q/%v", container.Image, container.Args, chartImage, wantArgs)
	}
	if len(deployment.Spec.Template.Spec.Volumes) != 0 || len(container.VolumeMounts) != 0 {
		t.Fatal("disabled mode mounted Azure configuration")
	}
	assertRules(t, got.role.Rules, map[string][]string{
		"coordination.k8s.io/leases": {verbCreate, verbGet, verbUpdate},
		"/events":                    {verbCreate, verbPatch},
	})
	if got.role.Namespace != chartNamespace || got.binding.Namespace != chartNamespace ||
		got.binding.RoleRef.Name != got.role.Name || len(got.binding.Subjects) != 1 ||
		got.binding.Subjects[0].Name != got.account.Name || got.binding.Subjects[0].Namespace != chartNamespace {
		t.Fatal("leader-election binding does not target this release's ServiceAccount")
	}
}

func TestGatewayChartProbes(t *testing.T) {
	got := renderChart(t)
	container := got.deployment.Spec.Template.Spec.Containers[0]
	if container.LivenessProbe == nil || container.ReadinessProbe == nil ||
		container.LivenessProbe.HTTPGet == nil || container.ReadinessProbe.HTTPGet == nil ||
		container.LivenessProbe.HTTPGet.Path != "/healthz" || container.ReadinessProbe.HTTPGet.Path != "/readyz" ||
		container.LivenessProbe.HTTPGet.Port.StrVal != "healthz" || container.ReadinessProbe.HTTPGet.Port.StrVal != "healthz" {
		t.Fatal("manager probes do not match the health/readiness endpoints")
	}
	ports := map[string]int32{}
	for _, port := range container.Ports {
		ports[port.Name] = port.ContainerPort
	}
	if ports["healthz"] != 8083 || ports["metrics"] != 8082 {
		t.Fatalf("manager ports = %v", ports)
	}
}

func TestGatewayChartSecurityContext(t *testing.T) {
	got := renderChart(t)
	pod := got.deployment.Spec.Template.Spec
	container := pod.Containers[0]
	security := pod.SecurityContext
	if security == nil || security.RunAsNonRoot == nil || !*security.RunAsNonRoot ||
		security.RunAsUser == nil || *security.RunAsUser != 65532 ||
		security.RunAsGroup == nil || *security.RunAsGroup != 65532 {
		t.Fatal("pod does not use the image's nonroot identity")
	}
	if container.SecurityContext == nil || container.SecurityContext.AllowPrivilegeEscalation == nil ||
		*container.SecurityContext.AllowPrivilegeEscalation || container.SecurityContext.ReadOnlyRootFilesystem == nil ||
		!*container.SecurityContext.ReadOnlyRootFilesystem {
		t.Fatal("container privilege/filesystem policy is missing")
	}
	if security.SeccompProfile == nil || security.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault ||
		container.SecurityContext.Capabilities == nil ||
		!reflect.DeepEqual(container.SecurityContext.Capabilities.Drop, []corev1.Capability{"ALL"}) {
		t.Fatal("seccomp/capability policy is missing")
	}
}

func TestGatewayChartEnabled(t *testing.T) {
	for _, createClass := range []bool{true, false} {
		name := "create class"
		flags := []string{helmSetFlag, enabledConfiguration}
		if !createClass {
			name = "reuse class"
			flags = append(flags, helmSetFlag, "gatewayClass.create=false")
		}
		t.Run(name, func(t *testing.T) {
			got := renderChart(t, flags...)
			if got.clusterRole == nil || got.clusterBinding == nil || (got.class != nil) != createClass {
				t.Fatalf("enabled resource inventory = %#v", got)
			}
			if createClass && (got.class.Name != "azure-fleet-afd" || got.class.Spec.ControllerName != gatewayv1.GatewayController(objectmeta.GatewayAPIControllerName)) {
				t.Fatalf("GatewayClass = %#v", got.class)
			}
			pod := got.deployment.Spec.Template.Spec
			container := pod.Containers[0]
			for _, arg := range []string{"--enable-afd=true", "--afd-resource-group=afd-rg", "--cloud-config=/etc/kubernetes/provider/azure.json"} {
				if !slices.Contains(container.Args, arg) {
					t.Errorf("missing enabled argument %q", arg)
				}
			}
			if len(pod.Volumes) != 1 || pod.Volumes[0].Secret == nil || pod.Volumes[0].Secret.SecretName != "platform-azure" ||
				len(pod.Volumes[0].Secret.Items) != 1 || pod.Volumes[0].Secret.Items[0].Key != cloudConfigKey ||
				pod.Volumes[0].Secret.Items[0].Path != cloudConfigKey || len(container.VolumeMounts) != 1 ||
				!container.VolumeMounts[0].ReadOnly || container.VolumeMounts[0].MountPath != "/etc/kubernetes/provider" {
				t.Fatal("enabled mode does not mount only the existing platform Secret")
			}
			if got.clusterBinding.RoleRef.Name != got.clusterRole.Name || len(got.clusterBinding.Subjects) != 1 ||
				got.clusterBinding.Subjects[0].Name != got.account.Name || got.clusterBinding.Subjects[0].Namespace != chartNamespace {
				t.Fatal("cluster binding does not target this release's ServiceAccount")
			}
			assertRules(t, got.clusterRole.Rules, map[string][]string{
				"gateway.networking.k8s.io/gatewayclasses":            {verbGet, verbList, verbPatch, verbWatch},
				"gateway.networking.k8s.io/gateways":                  {verbGet, verbList, verbPatch, verbWatch},
				"gateway.networking.k8s.io/httproutes":                {verbGet, verbList, verbWatch},
				"gateway.networking.k8s.io/referencegrants":           {verbGet, verbList, verbWatch},
				"gateway.networking.k8s.io/gatewayclasses/status":     {verbGet, verbPatch, verbUpdate},
				"gateway.networking.k8s.io/gateways/status":           {verbGet, verbPatch, verbUpdate},
				"gateway.networking.k8s.io/httproutes/status":         {verbGet, verbPatch, verbUpdate},
				"gateway.networking.k8s.io/gatewayclasses/finalizers": {verbUpdate},
				"gateway.networking.k8s.io/gateways/finalizers":       {verbUpdate},
				"networking.fleet.azure.com/serviceimports":           {verbGet, verbList, verbWatch},
				"networking.fleet.azure.com/internalserviceexports":   {verbGet, verbList, verbWatch},
				"/namespaces": {verbGet, verbList, verbWatch},
				"/events":     {verbCreate, verbPatch},
			})
		})
	}
}

func TestGatewayChartRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
		flags   []string
		want    string
	}{
		{"missing repository", false, []string{helmSetStringFlag, "image.repository="}, "image.repository"},
		{"missing tag", false, []string{helmSetStringFlag, "image.tag="}, "image.tag"},
		{"missing group", true, []string{helmSetStringFlag, "afd.resourceGroup="}, "afd.resourceGroup"},
		{"missing Secret", true, []string{helmSetStringFlag, "afd.cloudConfigSecret.name="}, "afd.cloudConfigSecret.name"},
		{"missing key", true, []string{helmSetStringFlag, "afd.cloudConfigSecret.key="}, "afd.cloudConfigSecret.key"},
		{"missing class name", true, []string{helmSetStringFlag, "gatewayClass.name="}, "gatewayClass.name"},
		{"missing existing account", false, []string{helmSetFlag, "serviceAccount.create=false"}, "serviceAccount.name"},
		{"string feature flag", false, []string{helmSetStringFlag, "afd.enabled=false"}, "afd.enabled"},
		{"string leader flag", false, []string{helmSetStringFlag, "leaderElection=false"}, "leaderElection"},
		{"string class flag", false, []string{helmSetStringFlag, "gatewayClass.create=false"}, "gatewayClass.create"},
		{"string account flag", false, []string{helmSetStringFlag, "serviceAccount.create=false"}, "serviceAccount.create"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := templateArgs()
			if tt.enabled {
				args = append(args, helmSetFlag, enabledConfiguration)
			}
			output, err := runHelm(t, append(args, tt.flags...)...)
			if err == nil || !strings.Contains(string(output), tt.want) {
				t.Fatalf("helm error = %v, output = %s; want %q", err, output, tt.want)
			}
		})
	}
}

func TestGatewayChartDisabledWithAzureConfiguration(t *testing.T) {
	got := renderChart(t, helmSetFlag, "afd.resourceGroup=unused,afd.cloudConfigSecret.name=unused")
	pod := got.deployment.Spec.Template.Spec
	if got.clusterRole != nil || got.clusterBinding != nil || got.class != nil ||
		len(pod.Volumes) != 0 || len(pod.Containers[0].VolumeMounts) != 0 {
		t.Fatal("Azure settings enabled resources or mounts without the feature flag")
	}
	for _, arg := range pod.Containers[0].Args {
		if strings.HasPrefix(arg, "--cloud-config=") || strings.HasPrefix(arg, "--afd-resource-group=") {
			t.Fatalf("disabled mode passed Azure argument %q", arg)
		}
	}
}

func TestGatewayChartWithoutLeaderElection(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		name := "disabled"
		flags := []string{helmSetFlag, "leaderElection=false"}
		if enabled {
			name = "enabled"
			flags = append(flags, helmSetFlag, enabledConfiguration)
		}
		t.Run(name, func(t *testing.T) {
			got := renderChart(t, flags...)
			if got.role != nil || got.binding != nil ||
				!slices.Contains(got.deployment.Spec.Template.Spec.Containers[0].Args, "--leader-elect=false") {
				t.Fatal("leader-election configuration and permissions disagree")
			}
			if (got.clusterRole != nil) != enabled || (got.clusterBinding != nil) != enabled {
				t.Fatal("leader-election setting changed feature permissions")
			}
		})
	}
}

func TestGatewayChartNamespaceAndSecretKey(t *testing.T) {
	const namespace = "alternate-system"
	got := renderChart(t, "--namespace", namespace, helmSetFlag, enabledConfiguration,
		helmSetStringFlag, "afd.cloudConfigSecret.key=platform.json")
	if got.deployment.Namespace != namespace || got.account.Namespace != namespace ||
		got.role.Namespace != namespace || got.binding.Namespace != namespace ||
		got.binding.Subjects[0].Namespace != namespace || got.clusterBinding.Subjects[0].Namespace != namespace {
		t.Fatal("namespaced resources and bindings do not follow the release namespace")
	}
	if !slices.Contains(got.deployment.Spec.Template.Spec.Containers[0].Args, "--leader-election-namespace="+namespace) ||
		got.deployment.Spec.Template.Spec.Volumes[0].Secret.Items[0].Key != "platform.json" ||
		got.deployment.Spec.Template.Spec.Volumes[0].Secret.Items[0].Path != cloudConfigKey {
		t.Fatal("namespace flag or selected Secret key does not match the configuration")
	}
	defaultNamespace := renderChart(t, helmSetFlag, enabledConfiguration)
	if got.clusterRole.Name == defaultNamespace.clusterRole.Name || got.clusterBinding.Name == defaultNamespace.clusterBinding.Name {
		t.Fatal("different namespaces collided on cluster RBAC names")
	}
}

func TestGatewayChartSelectorLabels(t *testing.T) {
	got := renderChart(t, helmSetStringFlag, `podLabels.app\.kubernetes\.io/name=wrong,podLabels.app\.kubernetes\.io/instance=wrong`)
	for key, value := range got.deployment.Spec.Selector.MatchLabels {
		if got.deployment.Spec.Template.Labels[key] != value {
			t.Fatalf("pod label %q does not match the Deployment selector", key)
		}
	}
}

func TestGatewayChartExistingIdentity(t *testing.T) {
	got := renderChart(t, helmSetFlag, enabledConfiguration, helmSetFlag, "serviceAccount.create=false,serviceAccount.name="+existingAccount,
		helmSetStringFlag, `podLabels.azure\.workload\.identity/use=true`)
	if got.account != nil || got.deployment.Spec.Template.Spec.ServiceAccountName != existingAccount ||
		got.deployment.Spec.Template.Labels["azure.workload.identity/use"] != "true" ||
		got.clusterBinding.Subjects[0].Name != existingAccount || got.binding.Subjects[0].Name != existingAccount {
		t.Fatal("existing identity reuse is inconsistent")
	}
	got = renderChart(t, helmSetStringFlag, `serviceAccount.annotations.azure\.workload\.identity/client-id=test-client`)
	if got.account.Annotations["azure.workload.identity/client-id"] != "test-client" {
		t.Fatal("ServiceAccount identity annotations were dropped")
	}
}

func TestGatewayChartLint(t *testing.T) {
	output, err := runHelm(t, "lint", "--strict", chartPath, helmSetStringFlag, "image.repository="+chartRepository, helmSetStringFlag, "image.tag=dev")
	if err != nil {
		t.Fatalf("helm lint: %v\n%s", err, output)
	}
}

func templateArgs() []string {
	return []string{"template", "gateway", chartPath, "--namespace", chartNamespace,
		helmSetStringFlag, "image.repository=" + chartRepository, helmSetStringFlag, "image.tag=dev"}
}

func runHelm(t *testing.T, args ...string) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "helm", args...).CombinedOutput() // #nosec G204 -- Fixed executable and test-owned arguments; no shell.
}

type renderedChart struct {
	deployment     *appsv1.Deployment
	account        *corev1.ServiceAccount
	role           *rbacv1.Role
	binding        *rbacv1.RoleBinding
	clusterRole    *rbacv1.ClusterRole
	clusterBinding *rbacv1.ClusterRoleBinding
	class          *gatewayv1.GatewayClass
}

func renderChart(t *testing.T, flags ...string) renderedChart {
	t.Helper()
	output, err := runHelm(t, append(templateArgs(), flags...)...)
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, output)
	}
	scheme := runtime.NewScheme()
	for _, install := range []func(*runtime.Scheme) error{clientgoscheme.AddToScheme, gatewayv1.Install} {
		if err := install(scheme); err != nil {
			t.Fatal(err)
		}
	}
	decoder := serializer.NewCodecFactory(scheme).UniversalDeserializer()
	documents := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(output), 4096)
	var got renderedChart
	seen := make(map[string]bool)
	for {
		var raw runtime.RawExtension
		if err := documents.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatal(err)
		}
		if len(raw.Raw) == 0 || string(raw.Raw) == "null" {
			continue
		}
		object, kind, err := decoder.Decode(raw.Raw, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if seen[kind.Kind] {
			t.Fatalf("duplicate rendered kind %s", kind.Kind)
		}
		seen[kind.Kind] = true
		switch object := object.(type) {
		case *appsv1.Deployment:
			got.deployment = object
		case *corev1.ServiceAccount:
			got.account = object
		case *rbacv1.Role:
			got.role = object
		case *rbacv1.RoleBinding:
			got.binding = object
		case *rbacv1.ClusterRole:
			got.clusterRole = object
		case *rbacv1.ClusterRoleBinding:
			got.clusterBinding = object
		case *gatewayv1.GatewayClass:
			got.class = object
		default:
			t.Fatalf("unexpected rendered resource %T", object)
		}
	}
	return got
}

func assertRules(t *testing.T, rules []rbacv1.PolicyRule, want map[string][]string) {
	t.Helper()
	got := make(map[string][]string)
	for _, rule := range rules {
		if len(rule.NonResourceURLs) != 0 || len(rule.ResourceNames) != 0 {
			t.Fatal("unexpected nonresource or name-specific RBAC")
		}
		for _, group := range rule.APIGroups {
			for _, resource := range rule.Resources {
				key := group + "/" + resource
				got[key] = append(got[key], rule.Verbs...)
			}
		}
	}
	for key, verbs := range got {
		slices.Sort(verbs)
		got[key] = slices.Compact(verbs)
	}
	for _, verbs := range want {
		slices.Sort(verbs)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RBAC = %v, want %v", got, want)
	}
}
