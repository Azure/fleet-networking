/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package internalmembercluster features internalmembercluster controller to report its heartbeat to the hub by updating
// internalMemberCluster and cleanup the resources before leave.
// For example, MCS agent needs to report the heartbeat after join and cleanup the created MCSes before leave.
// For now, there are two kinds of agents exist in the member cluster: MCS agent and ServiceExportImport agent.
package internalmembercluster

import (
	"context"
	"errors"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/apiretry"
)

const (
	conditionReasonJoined = "AgentJoined"
	conditionReasonLeft   = "AgentLeft"
)

// Reconciler reconciles a InternalMemberCluster object.
type Reconciler struct {
	MemberClient client.Client
	HubClient    client.Client
	AgentType    fleetv1alpha1.AgentType
}

//+kubebuilder:rbac:groups=fleet.azure.com,resources=internalmemberclusters,verbs=get;list;watch
//+kubebuilder:rbac:groups=fleet.azure.com,resources=internalmemberclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=multiclusterservices,verbs=get;list;delete
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceexports,verbs=get;list;delete

// Reconcile handles join/leave for the member cluster controllers and updates its heartbeats.
// For the MCS controller, it needs to delete created MCS related in the member clusters.
// For the ServiceExportImport controllers, it needs to delete created serviceExported related in the member clusters.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	imcKRef := klog.KRef(req.Namespace, req.Name)
	startTime := time.Now()
	klog.V(2).InfoS("Reconciliation starts", "internalMemberCluster", imcKRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Reconciliation ends", "internalMemberCluster", imcKRef, "latency", latency)
	}()

	var imc fleetv1alpha1.InternalMemberCluster
	if err := r.HubClient.Get(ctx, req.NamespacedName, &imc); err != nil {
		klog.ErrorS(err, "Failed to get internal member cluster", "internalMemberCluster", imcKRef)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	switch imc.Spec.State {
	case fleetv1alpha1.ClusterStateJoin:
		agentStatus := fleetv1alpha1.AgentStatus{
			Type: r.AgentType,
			Conditions: []metav1.Condition{
				{
					Type:               string(fleetv1alpha1.AgentJoined),
					Status:             metav1.ConditionTrue,
					Reason:             conditionReasonJoined,
					ObservedGeneration: imc.GetGeneration(),
				},
			},
			LastReceivedHeartbeat: metav1.NewTime(time.Now()),
		}
		if err := r.updateAgentStatus(ctx, &imc, agentStatus); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Second * time.Duration(imc.Spec.HeartbeatPeriodSeconds)}, nil
	case fleetv1alpha1.ClusterStateLeave:
		if r.AgentType == fleetv1alpha1.MultiClusterServiceAgent {
			if err := r.cleanupMCSRelatedResources(ctx); err != nil {
				return ctrl.Result{}, err
			}
		}
		if r.AgentType == fleetv1alpha1.ServiceExportImportAgent {
			if err := r.cleanupServiceExportRelatedResources(ctx); err != nil {
				return ctrl.Result{}, err
			}
		}
		agentStatus := fleetv1alpha1.AgentStatus{
			Type: r.AgentType,
			Conditions: []metav1.Condition{
				{
					Type:               string(fleetv1alpha1.AgentJoined),
					Status:             metav1.ConditionFalse,
					Reason:             conditionReasonLeft,
					ObservedGeneration: imc.GetGeneration(),
				},
			},
		}
		return ctrl.Result{}, r.updateAgentStatus(ctx, &imc, agentStatus)
	default:
		klog.ErrorS(errors.New("unknown state"), "internalMemberCluster", imcKRef, "state", imc.Spec.State)
	}
	return ctrl.Result{}, nil
}

func findAgentStatus(status []fleetv1alpha1.AgentStatus, agentType fleetv1alpha1.AgentType) *fleetv1alpha1.AgentStatus {
	for i := range status {
		if status[i].Type == agentType {
			return &status[i]
		}
	}
	return nil
}

// setAgentStatus sets the corresponding condition in conditions based on the new agent status.
// status must be non-nil.
// 1. if the condition of the specified type already exists (all fields of the existing condition are updated to
//    newCondition, LastTransitionTime is set to now if the new status differs from the old status)
// 2. if a condition of the specified type does not exist (LastTransitionTime is set to now() if unset, and newCondition is appended)
func setAgentStatus(status *[]fleetv1alpha1.AgentStatus, newStatus fleetv1alpha1.AgentStatus) {
	existingStatus := findAgentStatus(*status, newStatus.Type)
	if existingStatus == nil {
		for i := range newStatus.Conditions {
			if newStatus.Conditions[i].LastTransitionTime.IsZero() {
				newStatus.Conditions[i].LastTransitionTime = metav1.NewTime(time.Now())
			}
		}
		*status = append(*status, newStatus)
		return
	}
	for i := range newStatus.Conditions {
		meta.SetStatusCondition(&existingStatus.Conditions, newStatus.Conditions[i])
	}
	existingStatus.LastReceivedHeartbeat = newStatus.LastReceivedHeartbeat
}

func (r *Reconciler) updateAgentStatus(ctx context.Context, imc *fleetv1alpha1.InternalMemberCluster, desiredAgentStatus fleetv1alpha1.AgentStatus) error {
	oldStatus := imc.Status.DeepCopy()
	setAgentStatus(&imc.Status.AgentStatus, desiredAgentStatus)

	imcKObj := klog.KObj(imc)
	klog.V(2).InfoS("Updating internalMemberCluster status", "internalMemberCluster", imcKObj, "agentStatus", imc.Status.AgentStatus, "oldAgentStatus", oldStatus.AgentStatus)
	if err := r.HubClient.Status().Update(ctx, imc); err != nil {
		klog.ErrorS(err, "Failed to update internalMemberCluster status", "internalMemberCluster", klog.KObj(imc), "status", imc.Status)
		return err
	}
	return nil
}

// cleanupMCSRelatedResources deletes the MCS related resources.
// Ideally it should stop the controllers.
// For now, it tries its best to delete the existing MCS and won't handle the newly created resources for now.
func (r *Reconciler) cleanupMCSRelatedResources(ctx context.Context) error {
	list := &fleetnetv1alpha1.MultiClusterServiceList{}
	if err := r.MemberClient.List(ctx, list); err != nil {
		klog.ErrorS(err, "Failed to list multiClusterService")
		return err
	}
	for i := range list.Items {
		if list.Items[i].ObjectMeta.DeletionTimestamp != nil {
			continue
		}
		deleteFunc := func() error {
			return r.MemberClient.Delete(ctx, &list.Items[i])
		}
		if err := apiretry.Do(deleteFunc); err != nil && !apierrors.IsNotFound(err) {
			klog.ErrorS(err, "Failed to delete multiClusterService", "multiClusterService", klog.KObj(&list.Items[i]))
			return err
		}
	}

	for i := range list.Items {
		name := types.NamespacedName{Namespace: list.Items[i].GetNamespace(), Name: list.Items[i].GetName()}
		mcs := fleetnetv1alpha1.MultiClusterService{}
		getFunc := func() error {
			err := r.MemberClient.Get(ctx, name, &mcs)
			return err
		}
		if err := apiretry.WaitUntilObjectDeleted(ctx, getFunc); err != nil {
			klog.ErrorS(err, "Wait the multiClusterService to be deleted", "multiClusterService", name)
			return err
		}
	}

	klog.V(2).InfoS("Completed cleanup mcs related resources", "mcsCounter", len(list.Items))
	return nil
}

// cleanupServiceExportRelatedResources deletes the serviceExport related resources.
// Ideally it should stop the controllers.
// For now, it tries its best to delete the existing serviceExport and won't handle the newly created resources for now.
func (r *Reconciler) cleanupServiceExportRelatedResources(ctx context.Context) error {
	list := &fleetnetv1alpha1.ServiceExportList{}
	if err := r.MemberClient.List(ctx, list); err != nil {
		klog.ErrorS(err, "Failed to list serviceExport")
		return err
	}
	for i := range list.Items {
		if list.Items[i].ObjectMeta.DeletionTimestamp != nil {
			continue
		}
		deleteFunc := func() error {
			return r.MemberClient.Delete(ctx, &list.Items[i])
		}
		if err := apiretry.Do(deleteFunc); err != nil && !apierrors.IsNotFound(err) {
			klog.ErrorS(err, "Failed to delete serviceExport", "serviceExport", klog.KObj(&list.Items[i]))
			return err
		}
	}

	for i := range list.Items {
		name := types.NamespacedName{Namespace: list.Items[i].GetNamespace(), Name: list.Items[i].GetName()}
		svcExport := fleetnetv1alpha1.ServiceExport{}
		getFunc := func() error {
			return r.MemberClient.Get(ctx, name, &svcExport)
		}
		if err := apiretry.WaitUntilObjectDeleted(ctx, getFunc); err != nil {
			klog.ErrorS(err, "Failed to get the serviceExport", "serviceExport", name)
			return err
		}
	}

	klog.V(2).InfoS("Completed cleanup serviceExport related resources", "serviceExportCounter", len(list.Items))
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetv1alpha1.InternalMemberCluster{}).
		Complete(r)
}
