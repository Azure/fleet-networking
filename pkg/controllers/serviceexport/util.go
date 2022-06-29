/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package serviceexport

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

const (
	// svcExportCleanupFinalizer is the finalizer ServiceExport controllers adds to mark that a ServiceExport can
	// only be deleted after its corresponding Service has been unexported from the hub cluster.
	svcExportCleanupFinalizer = "networking.fleet.azure.com/svc-export-cleanup"
)

// isServiceExportCleanupNeeded returns if a ServiceExport needs cleanup.
func isServiceExportCleanupNeeded(svcExport *fleetnetv1alpha1.ServiceExport) bool {
	return controllerutil.ContainsFinalizer(svcExport, svcExportCleanupFinalizer) && svcExport.DeletionTimestamp != nil
}

// isServiceDeleted returns if a Service is deleted.
func isServiceDeleted(svc *corev1.Service) bool {
	return svc.ObjectMeta.DeletionTimestamp != nil
}

// formatInternalServiceExportName returns the unique name assigned to an exported Service.
func formatInternalServiceExportName(svcExport *fleetnetv1alpha1.ServiceExport) string {
	return fmt.Sprintf("%s-%s", svcExport.Namespace, svcExport.Name)
}
