/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package serviceexport

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// isServiceEligibleForExport returns if a Service is eligible for export; at this stage, headless Services
// and Services of the ExternalName type cannot be exported.
func isServiceEligibleForExport(svc *corev1.Service) bool {
	if svc.Spec.Type == corev1.ServiceTypeExternalName || svc.Spec.ClusterIP == "None" {
		return false
	}
	return true
}

// isConditionSeen returns if a condition has been seen before and requires no further action.
// If any reason will do, pass an empty string as the expected reason.
func isConditionSeen(cond *metav1.Condition, expectedStatus metav1.ConditionStatus, expectedReason string, minGeneration int64) bool {
	if cond == nil {
		return false
	}

	statusAsExpected := (cond.Status == expectedStatus)
	reasonAsExpected := (cond.Reason == expectedReason)
	if expectedReason == "" {
		reasonAsExpected = true
	}
	sameOrNewerGeneration := (cond.ObservedGeneration >= minGeneration)

	if statusAsExpected && reasonAsExpected && sameOrNewerGeneration {
		return true
	}
	return false
}

// extractServicePorts extracts ports in use from Service.
func extractServicePorts(svc *corev1.Service) []fleetnetv1alpha1.ServicePort {
	svcExportPorts := []fleetnetv1alpha1.ServicePort{}
	for _, svcPort := range svc.Spec.Ports {
		svcExportPorts = append(svcExportPorts, fleetnetv1alpha1.ServicePort{
			Name:        svcPort.Name,
			Protocol:    svcPort.Protocol,
			AppProtocol: svcPort.AppProtocol,
			Port:        svcPort.Port,
			TargetPort:  svcPort.TargetPort,
		})
	}

	return svcExportPorts
}
