package serviceexport

import (
	"fmt"
	"sort"

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

// isSvcEligibleForExport returns if a Service is eligible for export; at this stage, headless Services,
// Services of the ExternalName type, and Services using a non-IPv4 IP family cannot be exported.
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

// updateInternalSvcExport updates a ServiceExport as the spec of a Service changes.
func updateInternalSvcExport(svc *corev1.Service, svcExport *fleetnetworkingapi.ServiceExport) {
}

// areSvcPortsEqual returns if two ServicePorts are equal.
func areSvcPortsEqual(oldPort, newPort *corev1.ServicePort) bool {
	return true
}

// isSvcChange returns if the spec of Service has changed in a way significant enough to trigger a reconciliation
// on the ServiceExport controller's side.
func isSvcChanged(oldSvc, newSvc *corev1.Service) bool {
	// Request a reconciliation if the Service is deleted.
	if isSvcDeleted(newSvc) {
		return true
	}

	// Request a reconciliation when the export eligibility of a Service changes.
	if isSvcEligibleForExport(oldSvc) != isSvcEligibleForExport(newSvc) {
		return true
	}

	// Request a reconciliation when the Service ports change.
	oldPorts := oldSvc.Spec.DeepCopy().Ports
	newPorts := newSvc.Spec.DeepCopy().Ports
	if len(oldPorts) != len(newPorts) {
		return true
	}

	sort.Slice(oldPorts, func(i, j int) bool {
		return oldPorts[i].Port < oldPorts[j].Port
	})
	sort.Slice(newPorts, func(i, j int) bool {
		return newPorts[i].Port < newPorts[j].Port
	})
	for idx := range oldPorts {
		if !areSvcPortsEqual(&oldPorts[idx], &newPorts[idx]) {
			return true
		}
	}

	return false
}
