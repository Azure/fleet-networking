/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package serviceexport

import (
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetnetworkingapi "go.goms.io/fleet-networking/api/v1alpha1"
)

const (
	// svcExportCleanupFinalizer is the finalizer ServiceExport controllers adds to mark that a ServiceExport can
	// only be deleted after its corresponding Service has been unexported from the hub cluster.
	svcExportCleanupFinalizer = "networking.fleet.azure.com/svc-export-cleanup"
)

// isSvcExportCleanupNeeded returns if a ServiceExport needs cleanup.
func isSvcExportCleanupNeeded(svcExport *fleetnetworkingapi.ServiceExport) bool {
	return controllerutil.ContainsFinalizer(svcExport, svcExportCleanupFinalizer) && svcExport.DeletionTimestamp != nil
}

// isSvcDeleted returns if a Service is deleted.
func isSvcDeleted(svc *corev1.Service) bool {
	return svc.ObjectMeta.DeletionTimestamp != nil
}

// isSvcEligibleForExport returns if a Service is eligible for export; at this stage, headless Services
// and Services of the ExternalName type cannot be exported.
func isSvcEligibleForExport(svc *corev1.Service) bool {
	if svc.Spec.Type == corev1.ServiceTypeExternalName || svc.Spec.ClusterIP == "None" {
		return false
	}
	return true
}

// formatInternalSvcExportName returns the unique name assigned to an exported Service.
func formatInternalSvcExportName(svcExport *fleetnetworkingapi.ServiceExport) string {
	return fmt.Sprintf("%s-%s", svcExport.Namespace, svcExport.Name)
}

// updateInternalSvcExport updates a ServiceExport as the spec of a Service changes.
func updateInternalSvcExport(memberClusterID string, svc *corev1.Service, internalSvcExport *fleetnetworkingapi.InternalServiceExport) {
	svcExportPorts := []fleetnetworkingapi.ServicePort{}
	for _, svcPort := range svc.Spec.Ports {
		svcExportPorts = append(svcExportPorts, fleetnetworkingapi.ServicePort{
			Name:        svcPort.Name,
			Protocol:    svcPort.Protocol,
			AppProtocol: svcPort.AppProtocol,
			Port:        svcPort.Port,
			TargetPort:  svcPort.TargetPort,
		})
	}
	internalSvcExport.Spec.Ports = svcExportPorts
	internalSvcExport.Spec.ServiceReference = fleetnetworkingapi.ExportedObjectReference{
		ClusterID:       memberClusterID,
		APIVersion:      svc.APIVersion,
		Kind:            svc.Kind,
		Namespace:       svc.Namespace,
		Name:            svc.Name,
		ResourceVersion: svc.ResourceVersion,
		UID:             svc.UID,
	}
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

	// Request a reconciliation when the UIDs change.
	if oldSvc.UID != newSvc.UID {
		return true
	}

	// Request a reconciliation when the Service ports change.
	oldPorts := oldSvc.Spec.DeepCopy().Ports
	newPorts := newSvc.Spec.DeepCopy().Ports
	// Clear NodePort field as this is not exported
	for idx := range oldPorts {
		oldPorts[idx].NodePort = 0
	}
	for idx := range newPorts {
		newPorts[idx].NodePort = 0
	}
	return !reflect.DeepEqual(oldPorts, newPorts)
}
