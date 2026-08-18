/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package annotations

import (
	"strings"
	"testing"
)

func TestParseGatewayConfig(t *testing.T) {
	const validWAFPolicyID = "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/security-rg/providers/Microsoft.Network/frontdoorWebApplicationFirewallPolicies/store-waf"

	tests := []struct {
		name        string
		annotations map[string]string
		defaultSKU  SKU
		want        GatewayConfig
		wantErr     string
	}{
		{
			name:       "uses controller default",
			defaultSKU: SKUPremium,
			want: GatewayConfig{
				SKU: SKUPremium,
			},
		},
		{
			name: "parses explicit supported values",
			annotations: map[string]string{
				AFDSKUAnnotation:         string(SKUStandard),
				AFDWAFPolicyIDAnnotation: validWAFPolicyID,
			},
			defaultSKU: SKUStandard,
			want: GatewayConfig{
				SKU:         SKUStandard,
				WAFPolicyID: validWAFPolicyID,
			},
		},
		{
			name: "rejects invalid explicit SKU",
			annotations: map[string]string{
				AFDSKUAnnotation: "premium",
			},
			defaultSKU: SKUStandard,
			wantErr:    AFDSKUAnnotation,
		},
		{
			name:       "rejects invalid controller default",
			defaultSKU: SKU("invalid"),
			wantErr:    "default SKU",
		},
		{
			name: "rejects malformed WAF resource ID",
			annotations: map[string]string{
				AFDWAFPolicyIDAnnotation: "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Network/notWaf/name",
			},
			defaultSKU: SKUPremium,
			wantErr:    AFDWAFPolicyIDAnnotation,
		},
		{
			name: "rejects reserved resource group override",
			annotations: map[string]string{
				AFDResourceGroupAnnotation: "application-rg",
			},
			defaultSKU: SKUPremium,
			wantErr:    AFDResourceGroupAnnotation,
		},
		{
			name: "rejects unknown reserved annotation",
			annotations: map[string]string{
				annotationPrefix + "afd-skku": "Premium_AzureFrontDoor",
			},
			defaultSKU: SKUPremium,
			wantErr:    annotationPrefix + "afd-skku",
		},
		{
			name: "rejects backend annotation on Gateway",
			annotations: map[string]string{
				AFDOriginConnectivityAnnotation: string(ConnectivityPublic),
			},
			defaultSKU: SKUPremium,
			wantErr:    AFDOriginConnectivityAnnotation,
		},
		{
			name: "ignores unrelated annotation",
			annotations: map[string]string{
				"example.com/owner": "team-a",
			},
			defaultSKU: SKUStandard,
			want: GatewayConfig{
				SKU: SKUStandard,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseGatewayConfig(tt.annotations, tt.defaultSKU)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("ParseGatewayConfig() error = nil, want containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("ParseGatewayConfig() error = %q, want containing %q", err, tt.wantErr)
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

func TestParseServiceImportConfig(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		want        ServiceImportConfig
		wantErr     string
	}{
		{
			name: "uses defaults",
			want: ServiceImportConfig{
				Connectivity:    ConnectivityAuto,
				HealthProbePath: "/",
			},
		},
		{
			name: "parses explicit values",
			annotations: map[string]string{
				AFDOriginConnectivityAnnotation: string(ConnectivityPrivateLink),
				AFDHealthProbePathAnnotation:    "/healthz",
				AFDOriginHostHeaderAnnotation:   "api.example.com",
			},
			want: ServiceImportConfig{
				Connectivity:     ConnectivityPrivateLink,
				HealthProbePath:  "/healthz",
				OriginHostHeader: "api.example.com",
			},
		},
		{
			name: "rejects invalid connectivity",
			annotations: map[string]string{
				AFDOriginConnectivityAnnotation: "private",
			},
			wantErr: AFDOriginConnectivityAnnotation,
		},
		{
			name: "rejects relative health probe",
			annotations: map[string]string{
				AFDHealthProbePathAnnotation: "healthz",
			},
			wantErr: AFDHealthProbePathAnnotation,
		},
		{
			name: "rejects health probe query",
			annotations: map[string]string{
				AFDHealthProbePathAnnotation: "/healthz?deep=true",
			},
			wantErr: AFDHealthProbePathAnnotation,
		},
		{
			name: "rejects invalid origin host header",
			annotations: map[string]string{
				AFDOriginHostHeaderAnnotation: "https://api.example.com",
			},
			wantErr: AFDOriginHostHeaderAnnotation,
		},
		{
			name: "rejects Gateway annotation on ServiceImport",
			annotations: map[string]string{
				AFDSKUAnnotation: string(SKUPremium),
			},
			wantErr: AFDSKUAnnotation,
		},
		{
			name: "ignores existing Fleet weight annotation",
			annotations: map[string]string{
				annotationPrefix + "weight": "100",
			},
			want: ServiceImportConfig{
				Connectivity:    ConnectivityAuto,
				HealthProbePath: "/",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseServiceImportConfig(tt.annotations)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("ParseServiceImportConfig() error = nil, want containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("ParseServiceImportConfig() error = %q, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseServiceImportConfig() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("ParseServiceImportConfig() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestValidateCompatibility(t *testing.T) {
	tests := []struct {
		name    string
		gateway GatewayConfig
		service ServiceImportConfig
		wantErr string
	}{
		{
			name: "private link with Premium is valid",
			gateway: GatewayConfig{
				SKU: SKUPremium,
			},
			service: ServiceImportConfig{
				Connectivity: ConnectivityPrivateLink,
			},
		},
		{
			name: "private link with Standard is invalid",
			gateway: GatewayConfig{
				SKU: SKUStandard,
			},
			service: ServiceImportConfig{
				Connectivity: ConnectivityPrivateLink,
			},
			wantErr: string(SKUPremium),
		},
		{
			name: "public with Standard is valid",
			gateway: GatewayConfig{
				SKU: SKUStandard,
			},
			service: ServiceImportConfig{
				Connectivity: ConnectivityPublic,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCompatibility(tt.gateway, tt.service)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateCompatibility() error = %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateCompatibility() error = nil, want containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ValidateCompatibility() error = %q, want containing %q", err, tt.wantErr)
			}
		})
	}
}
