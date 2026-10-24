/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package annotations

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseGatewayConfig(t *testing.T) {
	const validWAFPolicyID = "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/security-rg/providers/Microsoft.Network/frontdoorWebApplicationFirewallPolicies/store-waf"

	tests := []struct {
		name        string
		annotations map[string]string
		want        GatewayConfig
		wantErr     string
	}{
		{
			name: "defaults to Premium without annotations",
			want: GatewayConfig{SKU: SKUPremium},
		},
		{
			name:        "defaults to Premium with empty annotations",
			annotations: map[string]string{},
			want:        GatewayConfig{SKU: SKUPremium},
		},
		{
			name: "accepts explicit Premium and existing WAF policy",
			annotations: map[string]string{
				AFDSKUAnnotation:         string(SKUPremium),
				AFDWAFPolicyIDAnnotation: validWAFPolicyID,
			},
			want: GatewayConfig{SKU: SKUPremium, WAFPolicyID: validWAFPolicyID},
		},
		{
			name: "rejects Standard rather than upgrading silently",
			annotations: map[string]string{
				AFDSKUAnnotation: "Standard_AzureFrontDoor",
			},
			wantErr: AFDSKUAnnotation,
		},
		{
			name: "rejects empty explicit SKU",
			annotations: map[string]string{
				AFDSKUAnnotation: "",
			},
			wantErr: AFDSKUAnnotation,
		},
		{
			name: "rejects misspelled explicit SKU",
			annotations: map[string]string{
				AFDSKUAnnotation: "premium",
			},
			wantErr: AFDSKUAnnotation,
		},
		{
			name: "rejects malformed WAF resource ID",
			annotations: map[string]string{
				AFDWAFPolicyIDAnnotation: "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Network/notWaf/name",
			},
			wantErr: AFDWAFPolicyIDAnnotation,
		},
		{
			name: "rejects invalid WAF subscription GUID",
			annotations: map[string]string{
				AFDWAFPolicyIDAnnotation: strings.Replace(validWAFPolicyID, "00000000-0000-0000-0000-000000000000", "invalid", 1),
			},
			wantErr: AFDWAFPolicyIDAnnotation,
		},
		{
			name: "rejects missing WAF policy name",
			annotations: map[string]string{
				AFDWAFPolicyIDAnnotation: strings.TrimSuffix(validWAFPolicyID, "store-waf"),
			},
			wantErr: AFDWAFPolicyIDAnnotation,
		},
		{
			name: "rejects reserved resource group override",
			annotations: map[string]string{
				AFDResourceGroupAnnotation: "application-rg",
			},
			wantErr: AFDResourceGroupAnnotation,
		},
		{
			name: "rejects unknown reserved annotation",
			annotations: map[string]string{
				annotationPrefix + "afd-skku": "Premium_AzureFrontDoor",
			},
			wantErr: annotationPrefix + "afd-skku",
		},
		{
			name: "rejects backend annotation on Gateway",
			annotations: map[string]string{
				annotationPrefix + "afd-origin-connectivity": "private-link",
			},
			wantErr: annotationPrefix + "afd-origin-connectivity",
		},
		{
			name: "preserves unrelated Fleet and third-party annotations",
			annotations: map[string]string{
				annotationPrefix + "weight": "100",
				"example.com/owner":         "team-a",
			},
			want: GatewayConfig{SKU: SKUPremium},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := copyAnnotations(tt.annotations)
			got, err := ParseGatewayConfig(tt.annotations)
			if !reflect.DeepEqual(tt.annotations, before) {
				t.Fatal("ParseGatewayConfig() mutated annotations")
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ParseGatewayConfig() error = %v, want containing %q", err, tt.wantErr)
				}
				if got != (GatewayConfig{}) {
					t.Errorf("ParseGatewayConfig() returned configuration on failure: %#v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseGatewayConfig() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("ParseGatewayConfig() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestValidateServiceImportAnnotations(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		wantErr     string
	}{
		{name: "requires no annotations"},
		{name: "accepts empty annotations", annotations: map[string]string{}},
		{
			name: "preserves unrelated Fleet and third-party annotations",
			annotations: map[string]string{
				annotationPrefix + "weight": "100",
				"example.com/owner":         "team-a",
			},
		},
		{
			name: "rejects Gateway annotation on ServiceImport",
			annotations: map[string]string{
				AFDSKUAnnotation: string(SKUPremium),
			},
			wantErr: AFDSKUAnnotation,
		},
		{
			name: "rejects unknown reserved annotation",
			annotations: map[string]string{
				annotationPrefix + "afd-probe-path": "/healthz",
			},
			wantErr: annotationPrefix + "afd-probe-path",
		},
	}
	for _, value := range []string{"auto", "public", "private-link", ""} {
		tests = append(tests, struct {
			name        string
			annotations map[string]string
			wantErr     string
		}{
			name:        "rejects retired connectivity " + value,
			annotations: map[string]string{annotationPrefix + "afd-origin-connectivity": value},
			wantErr:     annotationPrefix + "afd-origin-connectivity",
		})
	}
	for _, key := range []string{"afd-health-probe-path", "afd-origin-host-header"} {
		tests = append(tests, struct {
			name        string
			annotations map[string]string
			wantErr     string
		}{
			name:        "rejects retired " + key,
			annotations: map[string]string{annotationPrefix + key: "/healthz"},
			wantErr:     annotationPrefix + key,
		})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := copyAnnotations(tt.annotations)
			err := ValidateServiceImportAnnotations(tt.annotations)
			if !reflect.DeepEqual(tt.annotations, before) {
				t.Fatal("ValidateServiceImportAnnotations() mutated annotations")
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ValidateServiceImportAnnotations() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateServiceImportAnnotations() error = %v", err)
			}
		})
	}
}

func copyAnnotations(annotations map[string]string) map[string]string {
	if annotations == nil {
		return nil
	}
	result := make(map[string]string, len(annotations))
	for key, value := range annotations {
		result[key] = value
	}
	return result
}
