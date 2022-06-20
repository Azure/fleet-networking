package serviceexport

import (
	"fmt"

	fleetnetworkingapi "go.goms.io/fleet-networking/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

const (
	// svcExportCleanupFinalizer is the finalizer ServiceExport controllers adds to mark that a ServiceExport can
	// only be deleted after its corresponding Service has been unexported from the hub cluster.
	svcExportCleanupFinalizer = "networking.fleet.azure.com/svc-export-cleanup"
)

// hasSvcExportCleanupFinalizer returns if a ServiceExport has the cleanup finalizer added.
func hasSvcExportCleanupFinalizer(svcExport *fleetnetworkingapi.ServiceExport) bool {
	for _, finalizer := range svcExport.ObjectMeta.Finalizers {
		if finalizer == svcExportCleanupFinalizer {
			return true
		}
	}
	return false
}

// isSvcExportCleanupNeeded returns if a ServiceExport needs cleanup.
func isSvcExportCleanupNeeded(svcExport *fleetnetworkingapi.ServiceExport) bool {
	return hasSvcExportCleanupFinalizer(svcExport) && svcExport.DeletionTimestamp != nil
}

// isSvcDeleted returns if a Service is deleted.
func isSvcDeleted(svc *corev1.Service) bool {
	return svc.ObjectMeta.DeletionTimestamp != nil
}

// isSvcEligibleForExport returns if a Service is eligible for export; at this stage headless Services and
// Services of the ExternalName type cannot be exported.
func isSvcEligibleForExport(svc *corev1.Service) bool {
	if svc.Spec.Type == corev1.ServiceTypeExternalName || svc.Spec.ClusterIP == "" {
		return false
	}

	return true
}

// formatInternalSvcExportName returns the unique name assigned to an exported Service.
func formatInternalSvcExportName(svcExport *fleetnetworkingapi.ServiceExport) string {
	return fmt.Sprintf("%s-%s", svcExport.Namespace, svcExport.Name)
}

func updateInternalSvcExport(svc *corev1.Service, svcExport *fleetnetworkingapi.ServiceExport) {
}
