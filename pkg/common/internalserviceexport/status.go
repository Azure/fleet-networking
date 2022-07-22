// Package internalserviceexport provides common util for internalServiceExport CRD.
package internalserviceexport

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/condition"
)

const (
	conditionReasonNoConflictFound = "NoConflictFound"
	conditionReasonConflictFound   = "ConflictFound"
)

// UpdateStatus updates internalServiceExport status only when the desired condition is different from current condition.
func UpdateStatus(ctx context.Context, client client.Client, internalServiceExport *fleetnetv1alpha1.InternalServiceExport, conflict, withRetry bool) error {
	svcName := types.NamespacedName{
		Namespace: internalServiceExport.Spec.ServiceReference.Namespace,
		Name:      internalServiceExport.Spec.ServiceReference.Name,
	}
	desiredCond := metav1.Condition{
		Type:               string(fleetnetv1alpha1.ServiceExportConflict),
		Status:             metav1.ConditionFalse,
		Reason:             conditionReasonNoConflictFound,
		ObservedGeneration: internalServiceExport.Spec.ServiceReference.Generation, // use the generation of the original object
		Message:            fmt.Sprintf("service %s is exported without conflict", svcName),
	}
	if conflict {
		desiredCond = metav1.Condition{
			Type:               string(fleetnetv1alpha1.ServiceExportConflict),
			Status:             metav1.ConditionTrue,
			Reason:             conditionReasonConflictFound,
			ObservedGeneration: internalServiceExport.Spec.ServiceReference.Generation, // use the generation of the original object
			Message:            fmt.Sprintf("service %s is in conflict with other exported services", svcName),
		}
	}
	currentCond := meta.FindStatusCondition(internalServiceExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportConflict))
	if condition.EqualCondition(currentCond, &desiredCond) {
		return nil
	}
	exportKObj := klog.KObj(internalServiceExport)
	oldStatus := internalServiceExport.Status.DeepCopy()
	meta.SetStatusCondition(&internalServiceExport.Status.Conditions, desiredCond)

	klog.V(2).InfoS("Updating internalServiceExport status", "internalServiceExport", exportKObj, "status", internalServiceExport.Status, "oldStatus", oldStatus)
	if !withRetry {
		if err := client.Status().Update(ctx, internalServiceExport); err != nil {
			klog.ErrorS(err, "Failed to update internalServiceExport status", "internalServiceExport", exportKObj, "status", internalServiceExport.Status, "oldStatus", oldStatus)
			return err
		}
		return nil
	}
	if err := updateStatusWithRetry(ctx, client, internalServiceExport); err != nil {
		klog.ErrorS(err, "Failed to update internalServiceExport status with retry", "internalServiceExport", exportKObj, "status", internalServiceExport.Status, "oldStatus", oldStatus)
		return err
	}
	return nil
}

func updateStatusWithRetry(ctx context.Context, client client.Client, internalServiceExport *fleetnetv1alpha1.InternalServiceExport) error {
	backOffPeriod := retry.DefaultBackoff
	backOffPeriod.Cap = time.Second * 1

	return retry.OnError(backOffPeriod,
		func(err error) bool {
			if apierrors.IsNotFound(err) || apierrors.IsInvalid(err) {
				return false
			}
			return true
		},
		func() error {
			return client.Status().Update(ctx, internalServiceExport)
		})
}
