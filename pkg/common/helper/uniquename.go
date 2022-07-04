/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package helper

import (
	"fmt"
	"math/rand"
	"strings"

	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/validation"
)

type UniqueNameFormat int

const (
	DNS1123Subdomain UniqueNameFormat = 1
	DNS1123Label     UniqueNameFormat = 2
	DNS1035Label     UniqueNameFormat = 3

	UUIDLength = 5
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
	if s[0] >= '0' && s[0] <= '9' {
		return true
	}
	return false
}

// removeDots removes all dot (".") occurences in a string.
func removeDots(s string) string {
	return strings.ReplaceAll(s, ".", "")
}

// ClusterScopedUniqueName returns a name that is guaranteed to be unique within a cluster.
// The name is formatted using an object's namespace, its name, and a 5 character long UUID suffix; the format
// is [NAMESPACE]-[NAME]-[SUFFIX], e.g. an object `app` from the namespace `work` will be assigned a unique
// name like `work-app-1x2yz`. The function may truncate name components as it sees fit.
// Note: this function assumes that
// * the input object namespace is a valid RFC 1123 DNS label; and
// * the input object name follows one of the three formats used in Kubernetes (RFC 1123 DNS subdomain,
// 	 RFC 1123 DNS label, RFC 1035 DNS label).
func ClusterScopedUniqueName(format UniqueNameFormat, namespace string, name string) (string, error) {
	reservedSlots := 2 + UUIDLength // 2 dashes + 5 character UUID string

	switch format {
	case DNS1123Subdomain:
		availableSlots := validation.DNS1123SubdomainMaxLength // 253 characters
		slotsPerSeg := (availableSlots - reservedSlots) / 2
		uniqueName := fmt.Sprintf("%s-%s-%s",
			namespace[:minInt(slotsPerSeg, len(namespace))],
			name[:minInt(slotsPerSeg, len(name))],
			uuid.NewUUID()[:UUIDLength],
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
			uuid.NewUUID()[:UUIDLength],
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
			uuid.NewUUID()[:UUIDLength],
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
// * the input cluster ID is a valid RFC 1123 DNS subdomain; and
// * the input object namespace is a valid RFC 1123 DNS label; and
// * the input object name follows one of the three formats used in Kubernetes (RFC 1123 DNS subdomain,
// 	 RFC 1123 DNS label, RFC 1035 DNS label).
func FleetScopedUniqueName(format UniqueNameFormat, clusterID string, namespace string, name string) (string, error) {
	reservedSlots := 3 + UUIDLength // 2 dashes + 5 character UUID string

	switch format {
	case DNS1123Subdomain:
		availableSlots := validation.DNS1123SubdomainMaxLength // 253 characters
		slotsPerSeg := (availableSlots - reservedSlots) / 3
		uniqueName := fmt.Sprintf("%s-%s-%s-%s",
			clusterID[:minInt(slotsPerSeg, len(clusterID))],
			namespace[:minInt(slotsPerSeg, len(namespace))],
			name[:minInt(slotsPerSeg, len(name))],
			uuid.NewUUID()[:UUIDLength],
		)

		if errs := validation.IsDNS1123Subdomain(uniqueName); len(errs) != 0 {
			return "", fmt.Errorf("failed to format a unique RFC 1123 DNS subdomain name with namespace %s, name %s: %v", namespace, name, errs)
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
			uuid.NewUUID()[:UUIDLength],
		)

		if errs := validation.IsDNS1123Label(uniqueName); len(errs) != 0 {
			return "", fmt.Errorf("failed to format a unique RFC 1123 DNS label name with namespace %s, name %s: %v", namespace, name, errs)
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
			uuid.NewUUID()[:UUIDLength],
		)

		if errs := validation.IsDNS1035Label(uniqueName); len(errs) != 0 {
			return "", fmt.Errorf("failed to format a unique RFC 1035 DNS label name with namespace %s, name %s: %v", namespace, name, errs)
		}
		return uniqueName, nil
	}
	return "", fmt.Errorf("not a valid name format: %d", format)
}

// RandomLowerCaseAlphabeticString returns a string of lower case alphabetic characters only.
func RandomLowerCaseAlphabeticString(n int) string {
	alphabet := []rune("abcdefghijklmnopqrstuvwxyz")
	b := make([]rune, n)
	for i := range b {
		b[i] = alphabet[rand.Intn(len(alphabet))] //nolint:gosec
	}
	return string(b)
}
