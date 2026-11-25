//go:build helm

/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package helm

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

func TestGatewayChartOriginTLS(t *testing.T) {
	got := renderChart(t, helmSetFlag, enabledConfiguration+",enableOriginTLS=true")
	if !slices.Contains(got.deployment.Spec.Template.Spec.Containers[0].Args, "--enable-origin-tls=true") {
		t.Fatal("enabled origin TLS flag is missing")
	}
	var policies []rbacv1.PolicyRule
	foundCRDRead := false
	for _, rule := range got.clusterRole.Rules {
		if slices.Contains(rule.Resources, "backendtlspolicies") || slices.Contains(rule.Resources, "backendtlspolicies/status") {
			policies = append(policies, rule)
		}
		if slices.Contains(rule.Resources, "customresourcedefinitions") {
			foundCRDRead = true
			if !reflect.DeepEqual(rule.Verbs, []string{verbGet}) || !reflect.DeepEqual(rule.ResourceNames,
				[]string{"backendtlspolicies.gateway.networking.k8s.io", "internalserviceexports.networking.fleet.azure.com"}) {
				t.Fatalf("startup CRD read is not scoped: %#v", rule)
			}
		}
		for _, resource := range rule.Resources {
			if strings.Contains(resource, "secret") {
				t.Fatalf("origin TLS granted Secret permissions: %#v", rule)
			}
		}
	}
	if !foundCRDRead {
		t.Fatal("origin TLS startup cannot read required CRDs")
	}
	assertRules(t, policies, map[string][]string{
		"gateway.networking.k8s.io/backendtlspolicies":        {verbGet, verbList, verbWatch, verbCreate, verbUpdate, verbPatch, "delete"},
		"gateway.networking.k8s.io/backendtlspolicies/status": {verbGet, verbUpdate, verbPatch},
	})
}

func TestMemberChartOriginTLS(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
			flags := []string{}
			if enabled {
				flags = append(flags, helmSetFlag, "enableOriginTLS=true,enablePrivateLinkOriginDiscovery=true")
			}
			args := append(memberTemplateArgs(), flags...)
			output, err := runHelm(t, args...)
			if err != nil {
				t.Fatalf("helm template: %v\n%s", err, output)
			}
			deployment, role := decodeMemberOriginTLSResources(t, output)
			wantFlag := "--enable-origin-tls=false"
			if enabled {
				wantFlag = "--enable-origin-tls=true"
			}
			if deployment == nil || role == nil || !slices.Contains(deployment.Spec.Template.Spec.Containers[0].Args, wantFlag) {
				t.Fatal("member resources or TLS flag are missing")
			}
			var policies []rbacv1.PolicyRule
			for _, rule := range role.Rules {
				for _, resource := range rule.Resources {
					if strings.HasPrefix(resource, "backendtlspolicies") {
						policies = append(policies, rule)
					}
				}
			}
			if enabled {
				assertRules(t, policies, map[string][]string{
					"gateway.networking.k8s.io/backendtlspolicies": {verbGet, verbList, verbWatch},
				})
			} else if len(policies) != 0 {
				t.Fatalf("disabled member has BackendTLSPolicy permissions: %#v", policies)
			}
		})
	}
}

func TestMemberChartRejectsInvalidOriginTLS(t *testing.T) {
	for _, flags := range [][]string{
		{helmSetFlag, "enableOriginTLS=true"},
		{helmSetStringFlag, "enableOriginTLS=false"},
	} {
		output, err := runHelm(t, append(memberTemplateArgs(), flags...)...)
		if err == nil || !strings.Contains(string(output), "enableOriginTLS") {
			t.Fatalf("helm accepted invalid origin TLS settings: %v\n%s", err, output)
		}
	}
}

func memberTemplateArgs() []string {
	return []string{"template", "member", filepath.Join("..", "..", "charts", "member-net-controller-manager"),
		"--namespace", chartNamespace, "--show-only", "templates/deployment.yaml", "--show-only", "templates/rbac.yaml"}
}

func decodeMemberOriginTLSResources(t *testing.T, output []byte) (*appsv1.Deployment, *rbacv1.ClusterRole) {
	t.Helper()
	decoder := clientgoscheme.Codecs.UniversalDeserializer()
	documents := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(output), 4096)
	var deployment *appsv1.Deployment
	var role *rbacv1.ClusterRole
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
		object, _, err := decoder.Decode(raw.Raw, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		switch object := object.(type) {
		case *appsv1.Deployment:
			deployment = object
		case *rbacv1.ClusterRole:
			role = object
		}
	}
	return deployment, role
}
