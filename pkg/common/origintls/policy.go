/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package origintls defines Fleet's deliberately restricted TLS policy contract.
package origintls

import (
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/validation"
	gatewayv1alpha3 "sigs.k8s.io/gateway-api/apis/v1alpha3"
)

const (
	ExportAnnotation         = "networking.fleet.azure.com/export-to-fleet"
	ExportAnnotationValue    = "true"
	DefaultCertificateOption = "networking.fleet.azure.com/afd-default-certificate"
	ProjectionLabel          = "networking.fleet.azure.com/origin-tls-projection"
	ProvenanceAnnotation     = "networking.fleet.azure.com/origin-tls-provenance"
	Heartbeat                = time.Minute
	MaxAge                   = 5 * time.Minute
)

// Validate checks trust and exact SNI intent without reading any certificate.
func Validate(policy *gatewayv1alpha3.BackendTLSPolicy) error {
	spec := policy.Spec
	if policy.DeletionTimestamp != nil || len(spec.TargetRefs) != 1 || spec.TargetRefs[0].SectionName != nil ||
		len(spec.Options) != 0 || len(spec.Validation.CACertificateRefs) != 0 || len(spec.Validation.SubjectAltNames) != 0 ||
		spec.Validation.WellKnownCACertificates == nil || *spec.Validation.WellKnownCACertificates != gatewayv1alpha3.WellKnownCACertificatesSystem {
		return fmt.Errorf("requires one whole-resource target, System trust and no custom CA, SAN or TLS options")
	}
	return ValidateHostname(string(spec.Validation.Hostname))
}

// ValidateHostname rejects wildcard/IP identities and non-canonical DNS names.
func ValidateHostname(host string) error {
	if len(validation.IsDNS1123Subdomain(host)) != 0 || !strings.Contains(host, ".") ||
		strings.Contains(host, "*") || strings.Trim(host, "0123456789.") == "" {
		return fmt.Errorf("requires a precise canonical DNS hostname")
	}
	return nil
}
