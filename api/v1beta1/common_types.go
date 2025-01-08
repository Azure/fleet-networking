/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1beta1

// ClusterStatus contains service configuration mapped to a specific source cluster.
type ClusterStatus struct {
	// cluster is the name of the exporting cluster. Must be a valid RFC-1123 DNS
	// label.
	Cluster string `json:"cluster"`
}
