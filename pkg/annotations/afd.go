/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package annotations parses the Azure Front Door annotations supported by the
// Fleet Gateway controller.
package annotations

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	annotationPrefix    = "networking.fleet.azure.com/"
	afdAnnotationPrefix = annotationPrefix + "afd-"

	// AFDSKUAnnotation optionally declares the required Premium SKU for a Gateway.
	AFDSKUAnnotation = afdAnnotationPrefix + "sku"
	// AFDResourceGroupAnnotation is reserved for a future ownership model.
	AFDResourceGroupAnnotation = afdAnnotationPrefix + "resource-group"
	// AFDWAFPolicyIDAnnotation attaches an existing Azure Front Door WAF policy to a Gateway.
	AFDWAFPolicyIDAnnotation = afdAnnotationPrefix + "waf-policy-id"
)

var subscriptionIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// SKU is the Azure Front Door SKU used for private origins.
type SKU string

const (
	// SKUPremium is the Azure Front Door Premium SKU.
	SKUPremium SKU = "Premium_AzureFrontDoor"
)

// GatewayConfig is the typed Azure configuration derived from Gateway annotations.
type GatewayConfig struct {
	SKU         SKU
	WAFPolicyID string
}

// ParseGatewayConfig parses annotations that are valid on a Gateway.
func ParseGatewayConfig(annotations map[string]string) (GatewayConfig, error) {
	allowed := map[string]struct{}{
		AFDSKUAnnotation:           {},
		AFDResourceGroupAnnotation: {},
		AFDWAFPolicyIDAnnotation:   {},
	}
	if err := rejectUnsupportedAFDAnnotations(annotations, allowed); err != nil {
		return GatewayConfig{}, err
	}

	// Resource-group selection is intentionally controller-wide until resource
	// adoption, RBAC scoping, and deletion ownership are designed together.
	if value, found := annotations[AFDResourceGroupAnnotation]; found {
		return GatewayConfig{}, fmt.Errorf("annotation %q with value %q is reserved and not supported", AFDResourceGroupAnnotation, value)
	}

	config := GatewayConfig{SKU: SKUPremium}
	if value, found := annotations[AFDSKUAnnotation]; found {
		config.SKU = SKU(value)
		if config.SKU != SKUPremium {
			return GatewayConfig{}, fmt.Errorf("annotation %q has unsupported value %q; Private Link origins require %q", AFDSKUAnnotation, value, SKUPremium)
		}
	}

	if value, found := annotations[AFDWAFPolicyIDAnnotation]; found {
		// Parsing preserves declared protection; it does not mean WAF is associated.
		if err := validateWAFPolicyID(value); err != nil {
			return GatewayConfig{}, fmt.Errorf("annotation %q has invalid value %q: %w", AFDWAFPolicyIDAnnotation, value, err)
		}
		config.WAFPolicyID = value
	}
	return config, nil
}

// ValidateServiceImportAnnotations rejects customer AFD configuration on generated
// Fleet ServiceImports without affecting unrelated annotations.
func ValidateServiceImportAnnotations(annotations map[string]string) error {
	for key, value := range annotations {
		if strings.HasPrefix(key, afdAnnotationPrefix) {
			return fmt.Errorf("annotation %q with value %q is not supported on ServiceImport; remove it because AFD backend settings are platform-owned", key, value)
		}
	}
	return nil
}

func rejectUnsupportedAFDAnnotations(annotations map[string]string, allowed map[string]struct{}) error {
	for key, value := range annotations {
		if !strings.HasPrefix(key, afdAnnotationPrefix) {
			continue
		}
		if _, found := allowed[key]; !found {
			// Rejecting unknown reserved keys prevents misspellings from becoming
			// silent no-ops, which is the primary validation risk of annotations.
			return fmt.Errorf("annotation %q with value %q is not supported on this resource", key, value)
		}
	}
	return nil
}

func validateWAFPolicyID(value string) error {
	parts := strings.Split(strings.Trim(value, "/"), "/")
	if len(parts) != 8 ||
		!strings.EqualFold(parts[0], "subscriptions") ||
		!strings.EqualFold(parts[2], "resourceGroups") ||
		!strings.EqualFold(parts[4], "providers") ||
		!strings.EqualFold(parts[5], "Microsoft.Network") ||
		!strings.EqualFold(parts[6], "frontdoorWebApplicationFirewallPolicies") {
		return fmt.Errorf("expected an Azure Front Door WAF policy resource ID")
	}
	if !subscriptionIDPattern.MatchString(parts[1]) {
		return fmt.Errorf("subscription ID %q is not a GUID", parts[1])
	}
	if parts[3] == "" || parts[7] == "" {
		return fmt.Errorf("resource group and policy name must not be empty")
	}
	return nil
}
