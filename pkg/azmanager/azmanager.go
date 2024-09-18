/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package azmanager provides common functionalities for Azure resources management.
package azmanager

import (
	"fmt"
	"regexp"
)

var (
	publicIPAddressIDRegExp = regexp.MustCompile(`^/subscriptions/(?:.*)/resourceGroups/(.+)/providers/Microsoft.Network/publicIPAddresses/(.+)`)
)

// ParsePublicIPAddressID parses the resoure group name and public IP address name from the full resource ID.
func ParsePublicIPAddressID(id string) (resourceGroup string, name string, err error) {
	matches := publicIPAddressIDRegExp.FindStringSubmatch(id)
	if len(matches) != 3 {
		return "", "", fmt.Errorf("invalid public IP address resource ID %q", id)
	}
	return matches[1], matches[2], nil
}
