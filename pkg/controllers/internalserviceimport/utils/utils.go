/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package utils

import (
	"go.goms.io/fleet-networking/pkg/controllers/internalserviceimport/consts"
)

// GetExposedClusterName returns the name of the cluster which exposes the service import and on which multi-cluster
// networking loadbalancer will be provisioned
func GetExposedClusterName(labels map[string]string) string {
	return labels[consts.LabelExposedClusterName]
}

func GetTargetNamespace(labels map[string]string) string {
	return labels[consts.LabelTargetNamespace]
}
