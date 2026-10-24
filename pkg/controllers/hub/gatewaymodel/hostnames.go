/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gatewaymodel

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

// IntersectHostnames computes Gateway API hostname intersections. The boolean
// distinguishes an unconstrained match (nil, true) from no intersection.
func IntersectHostnames(listener string, routes []string) ([]string, bool, error) {
	if err := validateHostname(listener); err != nil {
		return nil, false, err
	}
	if len(routes) == 0 {
		if listener == "" {
			return nil, true, nil
		}
		return []string{listener}, true, nil
	}
	var result []string
	for _, route := range routes {
		if route == "" {
			return nil, false, fmt.Errorf("route hostname must not be empty")
		}
		if err := validateHostname(route); err != nil {
			return nil, false, err
		}
		switch {
		case listener == "" || listener == route:
			result = append(result, route)
		case hostnameCovers(listener, route):
			result = append(result, route)
		case hostnameCovers(route, listener):
			result = append(result, listener)
		}
	}
	return canonicalHostnames(result), len(result) != 0, nil
}

func hostnameCovers(pattern, hostname string) bool {
	// Gateway wildcards span multiple labels, but do not match the suffix apex.
	return strings.HasPrefix(pattern, "*.") && strings.HasSuffix(hostname, pattern[1:]) &&
		hostname != pattern[2:]
}

func validateHostname(hostname string) error {
	if hostname == "" {
		return nil
	}
	base := strings.TrimPrefix(hostname, "*.")
	if _, err := netip.ParseAddr(hostname); err == nil {
		return fmt.Errorf("hostname %q must not be an IP address", hostname)
	}
	if len(hostname) > 253 || len(validation.IsDNS1123Subdomain(base)) != 0 {
		return fmt.Errorf("invalid hostname %q", hostname)
	}
	return nil
}

func canonicalHostnames(hostnames []string) []string {
	if len(hostnames) == 0 {
		return nil
	}
	result := append([]string(nil), hostnames...)
	sort.Strings(result)
	unique := result[:0]
	for _, hostname := range result {
		if len(unique) == 0 || unique[len(unique)-1] != hostname {
			unique = append(unique, hostname)
		}
	}
	return unique
}
