/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package origintls

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"
)

func fixtureEnvironment() map[string]string {
	return map[string]string{
		"ORIGIN_TLS_NAMESPACE": "https-proof-csi", "ORIGIN_TLS_HOSTNAME": "origin.example.com",
		"ORIGIN_AKV_NAME": "fixture-vault", "ORIGIN_AKV_CERT_NAME": "serving-certificate",
		"ORIGIN_AKV_TENANT_ID": "11111111-1111-1111-1111-111111111111",
		"ORIGIN_AKV_CLIENT_ID": "22222222-2222-2222-2222-222222222222",
	}
}

func TestCSIEnvironmentRejectsMissingAndUnsafeReferences(t *testing.T) {
	for name := range fixtureEnvironment() {
		t.Run("missing "+name, func(t *testing.T) {
			env := fixtureEnvironment()
			delete(env, name)
			if _, err := FromEnv(func(key string) string { return env[key] }); err == nil {
				t.Fatal("missing input accepted")
			}
		})
	}
	for _, tt := range []struct{ name, value string }{
		{"ORIGIN_TLS_NAMESPACE", "kube-system"}, {"ORIGIN_TLS_NAMESPACE", "https-proof-"},
		{"ORIGIN_TLS_HOSTNAME", "*.example.com"}, {"ORIGIN_AKV_NAME", "https://vault.example"},
		{"ORIGIN_AKV_CERT_NAME", "cert\nobjectType: cert"}, {"ORIGIN_AKV_CERT_VERSION", "latest"},
		{"ORIGIN_AKV_TENANT_ID", "tenant"}, {"ORIGIN_AKV_CLIENT_ID", "client-secret"},
		{"ORIGIN_KEY_FILE", "/tmp/key.pem"}, {"ORIGIN_CERT_CHAIN_FILE", "/tmp/chain.pem"},
	} {
		t.Run(tt.name+"="+tt.value, func(t *testing.T) {
			env := fixtureEnvironment()
			env[tt.name] = tt.value
			if _, err := FromEnv(func(key string) string { return env[key] }); err == nil {
				t.Fatal("unsafe or legacy input accepted")
			}
		})
	}
}

func TestCSIMountBindsWorkloadIdentityAndPrivateBundleWithoutSecretSync(t *testing.T) {
	env := fixtureEnvironment()
	env["ORIGIN_AKV_CERT_VERSION"] = "0123456789abcdef0123456789abcdef"
	config, err := FromEnv(func(key string) string { return env[key] })
	if err != nil {
		t.Fatal(err)
	}
	account, provider, err := config.Resources()
	if err != nil {
		t.Fatal(err)
	}
	parameters, found, err := unstructured.NestedStringMap(provider.Object, "spec", "parameters")
	if err != nil || !found {
		t.Fatalf("invalid CSI parameters: %v", err)
	}
	if parameters["clientID"] != config.ClientID || parameters["tenantId"] != config.TenantID ||
		parameters["keyvaultName"] != config.VaultName || parameters["useVMManagedIdentity"] != "false" {
		t.Fatalf("wrong identity/reference configuration: %+v", parameters)
	}
	var objects struct {
		Array []string `json:"array"`
	}
	if err := yaml.UnmarshalStrict([]byte(parameters["objects"]), &objects); err != nil {
		t.Fatal(err)
	}
	if len(objects.Array) != 1 {
		t.Fatalf("expected one certificate bundle: %+v", objects)
	}
	var object map[string]string
	if err := yaml.UnmarshalStrict([]byte(objects.Array[0]), &object); err != nil {
		t.Fatal(err)
	}
	if object["objectType"] != "secret" || object["objectFormat"] != "pem" || object["objectAlias"] != "serving.pem" ||
		object["objectName"] != config.CertificateName || object["objectVersion"] != config.CertificateVersion || object["filePermission"] != "0400" {
		t.Fatalf("bundle could lose private key, full chain or permissions: %+v", object)
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(provider.Object, "spec", "secretObjects"); found {
		t.Fatal("CSI must not sync secret contents to the API server")
	}
	deployment := &appsv1.Deployment{}
	deployment.Spec.Template.Spec.Containers = []corev1.Container{{Name: "app"}}
	if err := config.Configure(deployment); err != nil {
		t.Fatal(err)
	}
	assertCSIApplicationMount(t, config, account, provider, deployment)
}

func assertCSIApplicationMount(t *testing.T, config Config, account *corev1.ServiceAccount, provider *unstructured.Unstructured, deployment *appsv1.Deployment) {
	t.Helper()
	pod := deployment.Spec.Template.Spec
	if account.Namespace != config.Namespace || deployment.Namespace != config.Namespace || pod.ServiceAccountName != account.Name ||
		ptr.Deref(account.AutomountServiceAccountToken, true) || ptr.Deref(pod.AutomountServiceAccountToken, true) {
		t.Fatal("workload identity subject or token isolation changed")
	}
	mount, volume := pod.Containers[0].VolumeMounts[0], pod.Volumes[0]
	if mount.Name != volume.Name || !mount.ReadOnly || mount.SubPath != "" || volume.Secret != nil ||
		volume.CSI == nil || volume.CSI.Driver != CSIDriver || !ptr.Deref(volume.CSI.ReadOnly, false) ||
		volume.CSI.VolumeAttributes["secretProviderClass"] != provider.GetName() {
		t.Fatal("application does not directly mount the expected read-only CSI bundle")
	}
}
