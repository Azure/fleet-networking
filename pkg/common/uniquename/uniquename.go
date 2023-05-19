/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package uniquename features utility functions that help format unique names for exporting and importing
// cluster-scoped and fleet-scoped resources.
package uniquename

import (
	"fmt"
	"strings"
	"unicode"

	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/validation"
)

type Format int

const (
	// DNS1123Subdomain dictates that the unique name should be a valid RFC 1123 DNS subdomain.
	DNS1123Subdomain Format = 1
	// DNS1123Label dictates that the unique name should be a valid RFC 1123 DNS label.
	DNS1123Label Format = 2
	// DNS1035Label dictates that the unique name should be a valid RFC 1035 DNS label.
	DNS1035Label Format = 3

	uuidLength = 5
)

// minInt returns the smaller one of two integers.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// startsWithNumericCharacter returns if a string starts with a numeric character.
func startsWithNumericCharacter(s string) bool {
	return unicode.IsDigit(rune(s[0]))
}

// removeDots removes all dot (".") occurrences in a string.
func removeDots(s string) string {
	return strings.ReplaceAll(s, ".", "")
}

// ClusterScopedUniqueName returns a name that is guaranteed to be unique within a cluster.
// The name is formatted using an object's namespace, its name, and a 5 character long UUID suffix; the format
// is [NAMESPACE]-[NAME]-[SUFFIX], e.g. an object `app` from the namespace `work` will be assigned a unique
// name like `work-app-1x2yz`. The function may truncate name components as it sees fit.
// Note: this function assumes that
//   - the input object namespace is a valid RFC 1123 DNS label; and
//   - the input object name follows one of the three formats used in Kubernetes (RFC 1123 DNS subdomain,
//     RFC 1123 DNS label, RFC 1035 DNS label).
func ClusterScopedUniqueName(format Format, namespace, name string) (string, error) {
	reservedSlots := 2 + uuidLength // 2 dashes + 5 character UUID string

	switch format {
	case DNS1123Subdomain:
		availableSlots := validation.DNS1123SubdomainMaxLength // 253 characters
		slotsPerSeg := (availableSlots - reservedSlots) / 2
		uniqueName := fmt.Sprintf("%s-%s-%s",
			namespace[:minInt(slotsPerSeg, len(namespace))],
			name[:minInt(slotsPerSeg, len(name))],
			uuid.NewUUID()[:uuidLength],
		)

		if errs := validation.IsDNS1123Subdomain(uniqueName); len(errs) != 0 {
			return "", fmt.Errorf("failed to format a unique RFC 1123 DNS subdomain name with namespace %s, name %s: %v", namespace, name, errs)
		}
		return uniqueName, nil
	case DNS1123Label:
		availableSlots := validation.DNS1123LabelMaxLength // 63 characters
		slotsPerSeg := (availableSlots - reservedSlots) / 2

		// If the object name is a RFC 1123 DNS subdomain, it may include dot characters, which is not allowed in
		// RFC 1123 DNS labels.
		name = removeDots(name)

		uniqueName := fmt.Sprintf("%s-%s-%s",
			namespace[:minInt(slotsPerSeg, len(namespace))],
			name[:minInt(slotsPerSeg, len(name))],
			uuid.NewUUID()[:uuidLength],
		)

		if errs := validation.IsDNS1123Label(uniqueName); len(errs) != 0 {
			return "", fmt.Errorf("failed to format a unique RFC 1123 DNS label name with namespace %s, name %s: %v", namespace, name, errs)
		}
		return uniqueName, nil
	case DNS1035Label:
		availableSlots := validation.DNS1035LabelMaxLength // 63 characters
		slotsPerSeg := (availableSlots - reservedSlots) / 2

		// Namespace names are RFC 1123 DNS labels, which may start with an alphanumeric character; RFC 1035 DNS
		// labels, on the other hand, does not allow numeric characters at the beginning of the string.
		if startsWithNumericCharacter(namespace) {
			namespace = "ns" + namespace
		}
		// If the object name is a RFC 1123 DNS subdomain, it may include dot characters, which is not allowed in
		// RFC 1035 DNS labels.
		name = removeDots(name)

		uniqueName := fmt.Sprintf("%s-%s-%s",
			namespace[:minInt(slotsPerSeg, len(namespace))],
			name[:minInt(slotsPerSeg, len(name))],
			uuid.NewUUID()[:uuidLength],
		)

		if errs := validation.IsDNS1035Label(uniqueName); len(errs) != 0 {
			return "", fmt.Errorf("failed to format a unique RFC 1035 DNS label name with namespace %s, name %s: %v", namespace, name, errs)
		}
		return uniqueName, nil
	}
	return "", fmt.Errorf("not a valid name format: %d", format)
}

// FleetScopedUniqueName returns a name that is guaranteed to be unique within a cluster.
// The name is formatted using an object's origin cluster, an object's namespace, its name, and a 5 character
// long UUID suffix; the format is [CLUSTER ID]-[NAMESPACE]-[NAME]-[SUFFIX], e.g. an object `app` from the namespace
// `work` in cluster `bravelion` will be assigned a unique name like `bravelion-work-app-1x2yz`. The function may
// truncate name components as it sees fit.
// Note: this function assumes that
//   - the input cluster ID is a valid RFC 1123 DNS subdomain; and
//   - the input object namespace is a valid RFC 1123 DNS label; and
//   - the input object name follows one of the three formats used in Kubernetes (RFC 1123 DNS subdomain,
//     RFC 1123 DNS label, RFC 1035 DNS label).
func FleetScopedUniqueName(format Format, clusterID, namespace, name string) (string, error) {
	reservedSlots := 3 + uuidLength // 3 dashes + 5 character UUID string

	switch format {
	case DNS1123Subdomain:
		availableSlots := validation.DNS1123SubdomainMaxLength // 253 characters
		slotsPerSeg := (availableSlots - reservedSlots) / 3
		uniqueName := fmt.Sprintf("%s-%s-%s-%s",
			clusterID[:minInt(slotsPerSeg, len(clusterID))],
			namespace[:minInt(slotsPerSeg, len(namespace))],
			name[:minInt(slotsPerSeg, len(name))],
			uuid.NewUUID()[:uuidLength],
		)

		if errs := validation.IsDNS1123Subdomain(uniqueName); len(errs) != 0 {
			return "", fmt.Errorf("failed to format a unique RFC 1123 DNS subdomain name with cluster ID %s, namespace %s, name %s: %v",
				clusterID, namespace, name, errs)
		}
		return uniqueName, nil
	case DNS1123Label:
		availableSlots := validation.DNS1123LabelMaxLength // 63 characters
		slotsPerSeg := (availableSlots - reservedSlots) / 3

		// If the cluster ID and object name are valid RFC 1123 DNS subdomains, they may include dot characters,
		// which is not allowed in RFC 1123 DNS labels.
		clusterID = removeDots(clusterID)
		name = removeDots(name)

		uniqueName := fmt.Sprintf("%s-%s-%s-%s",
			clusterID[:minInt(slotsPerSeg, len(clusterID))],
			namespace[:minInt(slotsPerSeg, len(namespace))],
			name[:minInt(slotsPerSeg, len(name))],
			uuid.NewUUID()[:uuidLength],
		)

		if errs := validation.IsDNS1123Label(uniqueName); len(errs) != 0 {
			return "", fmt.Errorf("failed to format a unique RFC 1123 DNS label name with cluster ID %s, namespace %s, name %s: %v",
				clusterID, namespace, name, errs)
		}
		return uniqueName, nil
	case DNS1035Label:
		availableSlots := validation.DNS1035LabelMaxLength // 63 characters
		slotsPerSeg := (availableSlots - reservedSlots) / 3

		// If the cluster ID and object name are valid RFC 1123 DNS subdomains, they may include dot characters,
		// which is not allowed in RFC 1123 DNS labels.
		clusterID = removeDots(clusterID)
		name = removeDots(name)

		uniqueName := fmt.Sprintf("%s-%s-%s-%s",
			clusterID[:minInt(slotsPerSeg, len(clusterID))],
			namespace[:minInt(slotsPerSeg, len(namespace))],
			name[:minInt(slotsPerSeg, len(name))],
			uuid.NewUUID()[:uuidLength],
		)

		if errs := validation.IsDNS1035Label(uniqueName); len(errs) != 0 {
			return "", fmt.Errorf("failed to format a unique RFC 1035 DNS label name with cluster ID %s, namespace %s, name %s: %v",
				clusterID, namespace, name, errs)
		}
		return uniqueName, nil
	}
	return "", fmt.Errorf("not a valid name format: %d", format)
}

// RandomLowerCaseAlphabeticString returns a string of lower case alphabetic characters only. This function
// is best used for fallback cases where one cannot format a unique name as expected, as a lower case
// alphabetic string of proper length is always a valid Kubernetes object name, regardless of the required name
// format for the object (RFC 1123 DNS subdomain, RFC 1123 DNS label, or RFC 1035 DNS label).
func RandomLowerCaseAlphabeticString(n int) string {
	alphabet := []rune("abcdefghijklmnopqrstuvwxyz")
	b := make([]rune, n)
	for i := range b {
		b[i] = alphabet[rand.Intn(len(alphabet))] //nolint:gosec
	}
	return string(b)
}
