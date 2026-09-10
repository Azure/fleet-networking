/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package annotations parses the Azure Front Door annotations supported by the
// Fleet Gateway controller.
package annotations

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	annotationPrefix    = "networking.fleet.azure.com/"
	afdAnnotationPrefix = annotationPrefix + "afd-"

	// AFDSKUAnnotation selects the Azure Front Door SKU for a Gateway.
	AFDSKUAnnotation = afdAnnotationPrefix + "sku"
	// AFDResourceGroupAnnotation is reserved for a future ownership model.
	AFDResourceGroupAnnotation = afdAnnotationPrefix + "resource-group"
	// AFDWAFPolicyIDAnnotation attaches an existing Azure Front Door WAF policy to a Gateway.
	AFDWAFPolicyIDAnnotation = afdAnnotationPrefix + "waf-policy-id"

	// AFDOriginConnectivityAnnotation selects how Azure Front Door reaches a ServiceImport.
	AFDOriginConnectivityAnnotation = afdAnnotationPrefix + "origin-connectivity"
	// AFDHealthProbePathAnnotation configures the origin-group HTTP health probe path.
	AFDHealthProbePathAnnotation = afdAnnotationPrefix + "health-probe-path"
	// AFDOriginHostHeaderAnnotation overrides the Host header sent to origins.
	AFDOriginHostHeaderAnnotation = afdAnnotationPrefix + "origin-host-header"
)

var subscriptionIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// SKU is an Azure Front Door Standard/Premium SKU name.
type SKU string

const (
	// SKUStandard is the Azure Front Door Standard SKU.
	SKUStandard SKU = "Standard_AzureFrontDoor"
	// SKUPremium is the Azure Front Door Premium SKU.
	SKUPremium SKU = "Premium_AzureFrontDoor"
)

// Connectivity selects the origin connectivity mode for every member origin
// represented by one ServiceImport.
type Connectivity string

const (
	// ConnectivityAuto selects a fully private topology when available and otherwise a fully public topology.
	ConnectivityAuto Connectivity = "auto"
	// ConnectivityPublic requires all origins to expose public endpoints.
	ConnectivityPublic Connectivity = "public"
	// ConnectivityPrivateLink requires all origins to expose Azure Private Link Services.
	ConnectivityPrivateLink Connectivity = "private-link"
)

// GatewayConfig is the typed Azure configuration derived from Gateway annotations.
type GatewayConfig struct {
	SKU         SKU
	WAFPolicyID string
}

// ServiceImportConfig is the typed Azure configuration derived from ServiceImport annotations.
type ServiceImportConfig struct {
	Connectivity     Connectivity
	HealthProbePath  string
	OriginHostHeader string
}

// ParseGatewayConfig parses annotations that are valid on a Gateway.
func ParseGatewayConfig(annotations map[string]string, defaultSKU SKU) (GatewayConfig, error) {
	if !defaultSKU.valid() {
		return GatewayConfig{}, fmt.Errorf("default SKU %q is not supported", defaultSKU)
	}

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

	config := GatewayConfig{SKU: defaultSKU}
	if value, found := annotations[AFDSKUAnnotation]; found {
		config.SKU = SKU(value)
		if !config.SKU.valid() {
			return GatewayConfig{}, fmt.Errorf("annotation %q has unsupported value %q; expected %q or %q", AFDSKUAnnotation, value, SKUStandard, SKUPremium)
		}
	}

	if value, found := annotations[AFDWAFPolicyIDAnnotation]; found {
		if err := validateWAFPolicyID(value); err != nil {
			return GatewayConfig{}, fmt.Errorf("annotation %q has invalid value %q: %w", AFDWAFPolicyIDAnnotation, value, err)
		}
		config.WAFPolicyID = value
	}
	return config, nil
}

// ParseServiceImportConfig parses annotations that are valid on a Fleet ServiceImport.
func ParseServiceImportConfig(annotations map[string]string) (ServiceImportConfig, error) {
	allowed := map[string]struct{}{
		AFDOriginConnectivityAnnotation: {},
		AFDHealthProbePathAnnotation:    {},
		AFDOriginHostHeaderAnnotation:   {},
	}
	if err := rejectUnsupportedAFDAnnotations(annotations, allowed); err != nil {
		return ServiceImportConfig{}, err
	}

	config := ServiceImportConfig{
		Connectivity:    ConnectivityAuto,
		HealthProbePath: "/",
	}
	if value, found := annotations[AFDOriginConnectivityAnnotation]; found {
		config.Connectivity = Connectivity(value)
		if !config.Connectivity.valid() {
			return ServiceImportConfig{}, fmt.Errorf("annotation %q has unsupported value %q; expected %q, %q, or %q", AFDOriginConnectivityAnnotation, value, ConnectivityAuto, ConnectivityPublic, ConnectivityPrivateLink)
		}
	}
	if value, found := annotations[AFDHealthProbePathAnnotation]; found {
		if err := validateHealthProbePath(value); err != nil {
			return ServiceImportConfig{}, fmt.Errorf("annotation %q has invalid value %q: %w", AFDHealthProbePathAnnotation, value, err)
		}
		config.HealthProbePath = value
	}
	if value, found := annotations[AFDOriginHostHeaderAnnotation]; found {
		if errs := validation.IsDNS1123Subdomain(value); len(errs) != 0 {
			return ServiceImportConfig{}, fmt.Errorf("annotation %q has invalid value %q: expected a lowercase DNS hostname: %s", AFDOriginHostHeaderAnnotation, value, strings.Join(errs, "; "))
		}
		config.OriginHostHeader = value
	}
	return config, nil
}

// ValidateCompatibility checks settings that span a Gateway and one of its ServiceImport backends.
func ValidateCompatibility(gateway GatewayConfig, serviceImport ServiceImportConfig) error {
	if serviceImport.Connectivity == ConnectivityPrivateLink && gateway.SKU != SKUPremium {
		return fmt.Errorf("connectivity %q requires Azure Front Door SKU %q", ConnectivityPrivateLink, SKUPremium)
	}
	return nil
}

func (s SKU) valid() bool {
	return s == SKUStandard || s == SKUPremium
}

func (c Connectivity) valid() bool {
	return c == ConnectivityAuto || c == ConnectivityPublic || c == ConnectivityPrivateLink
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

func validateHealthProbePath(value string) error {
	if !strings.HasPrefix(value, "/") {
		return fmt.Errorf("expected an absolute path beginning with '/'")
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil {
		return fmt.Errorf("expected a valid URI path: %w", err)
	}
	if parsed.IsAbs() || parsed.Host != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("expected a path without scheme, host, query, or fragment")
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
