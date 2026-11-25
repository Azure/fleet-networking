/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package origintls configures application-only AKV CSI fixtures, never secret contents.
package origintls

import (
	"fmt"
	"regexp"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/utils/ptr"

	"go.goms.io/fleet-networking/pkg/common/origintls"
)

const (
	ServingAccount           = "origin-serving"
	CSIGroupVersion          = "secrets-store.csi.x-k8s.io/v1"
	CSIDriver                = "secrets-store.csi.k8s.io"
	WorkloadIdentityAudience = "api://AzureADTokenExchange"
)

var (
	uuid          = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	vaultName     = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9-]{1,22}[a-zA-Z0-9]$`)
	objectName    = regexp.MustCompile(`^[a-zA-Z0-9-]{1,127}$`)
	objectVersion = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)
)

// Config contains identifiers supplied by the owner of the approved test environment.
type Config struct {
	Namespace, Hostname, VaultName, CertificateName, CertificateVersion, TenantID, ClientID string
}

// FromEnv rejects legacy runner-key inputs rather than silently falling back to them.
func FromEnv(getenv func(string) string) (Config, error) {
	if getenv("ORIGIN_CERT_CHAIN_FILE") != "" || getenv("ORIGIN_KEY_FILE") != "" {
		return Config{}, fmt.Errorf("runner-local certificate inputs are unsupported; use the AKV CSI fixture")
	}
	config := Config{
		Namespace: getenv("ORIGIN_TLS_NAMESPACE"), Hostname: getenv("ORIGIN_TLS_HOSTNAME"),
		VaultName: getenv("ORIGIN_AKV_NAME"), CertificateName: getenv("ORIGIN_AKV_CERT_NAME"),
		CertificateVersion: getenv("ORIGIN_AKV_CERT_VERSION"),
		TenantID:           getenv("ORIGIN_AKV_TENANT_ID"), ClientID: getenv("ORIGIN_AKV_CLIENT_ID"),
	}
	if err := config.validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (c Config) validate() error {
	if !strings.HasPrefix(c.Namespace, "https-proof-") || c.Namespace == "https-proof-" ||
		len(validation.IsDNS1123Label(c.Namespace)) != 0 {
		return fmt.Errorf("ORIGIN_TLS_NAMESPACE must be a fresh https-proof-* namespace")
	}
	if origintls.ValidateHostname(c.Hostname) != nil {
		return fmt.Errorf("ORIGIN_TLS_HOSTNAME must be a precise DNS identity")
	}
	if !vaultName.MatchString(c.VaultName) || strings.Contains(c.VaultName, "--") {
		return fmt.Errorf("ORIGIN_AKV_NAME must be a valid Key Vault name")
	}
	if !objectName.MatchString(c.CertificateName) {
		return fmt.Errorf("ORIGIN_AKV_CERT_NAME must name the certificate's backing secret")
	}
	if c.CertificateVersion != "" && !objectVersion.MatchString(c.CertificateVersion) {
		return fmt.Errorf("ORIGIN_AKV_CERT_VERSION must be empty or a 32-digit hexadecimal secret version")
	}
	if !uuid.MatchString(c.TenantID) || !uuid.MatchString(c.ClientID) {
		return fmt.Errorf("ORIGIN_AKV_TENANT_ID and ORIGIN_AKV_CLIENT_ID must be UUIDs")
	}
	return nil
}

// Resources creates member-only identity/reference objects, with no Secret sync.
func (c Config) Resources() (*corev1.ServiceAccount, *unstructured.Unstructured, error) {
	if err := c.validate(); err != nil {
		return nil, nil, err
	}
	account := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Namespace: c.Namespace, Name: ServingAccount},
		AutomountServiceAccountToken: ptr.To(false)}
	objects := fmt.Sprintf("array:\n  - |\n    objectName: %s\n    objectType: secret\n    objectFormat: pem\n    objectAlias: serving.pem\n    objectVersion: %q\n    filePermission: \"0400\"\n",
		c.CertificateName, c.CertificateVersion)
	provider := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": CSIGroupVersion, "kind": "SecretProviderClass",
		"metadata": map[string]interface{}{"namespace": c.Namespace, "name": ServingAccount},
		"spec": map[string]interface{}{"provider": "azure", "parameters": map[string]interface{}{
			"usePodIdentity": "false", "useVMManagedIdentity": "false", "clientID": c.ClientID,
			"keyvaultName": c.VaultName, "tenantId": c.TenantID, "objects": objects,
		}},
	}}
	return account, provider, nil
}

// Configure mounts the CSI bundle directly, without exposing Azure tokens to the app.
func (c Config) Configure(deployment *appsv1.Deployment) error {
	if err := c.validate(); err != nil {
		return err
	}
	if len(deployment.Spec.Template.Spec.Containers) != 1 {
		return fmt.Errorf("TLS fixture requires exactly one application container")
	}
	deployment.Namespace = c.Namespace
	pod := &deployment.Spec.Template.Spec
	pod.ServiceAccountName, pod.AutomountServiceAccountToken = ServingAccount, ptr.To(false)
	pod.Volumes = append(pod.Volumes, corev1.Volume{Name: "tls", VolumeSource: corev1.VolumeSource{
		CSI: &corev1.CSIVolumeSource{Driver: CSIDriver, ReadOnly: ptr.To(true),
			VolumeAttributes: map[string]string{"secretProviderClass": ServingAccount}},
	}})
	pod.Containers[0].VolumeMounts = append(pod.Containers[0].VolumeMounts, corev1.VolumeMount{Name: "tls", MountPath: "/tls", ReadOnly: true})
	pod.Containers[0].Env = append(pod.Containers[0].Env, corev1.EnvVar{Name: "ORIGIN_TLS_HOSTNAME", Value: c.Hostname})
	return nil
}
