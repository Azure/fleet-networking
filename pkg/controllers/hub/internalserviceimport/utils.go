/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package internalserviceimport

import (
	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

// GetExposedClusterName returns the name of the cluster which exposes the service import and on which multi-cluster
// networking loadbalancer will be provisioned
func GetExposedClusterName(internalServiceImport *fleetnetv1alpha1.InternalServiceImport) string {
	if internalServiceImport != nil {
		return internalServiceImport.Spec.ExposedCluster
	}
	return ""
}

func GetTargetNamespace(internalServiceImport *fleetnetv1alpha1.InternalServiceImport) string {
	if internalServiceImport != nil {
		return internalServiceImport.Spec.TargetNamespace
	}
	return ""
}
