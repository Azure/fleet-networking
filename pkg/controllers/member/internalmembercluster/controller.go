/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package internalmembercluster features internalmembercluster controller to report its heartbeat to the hub by updating
// internalMemberCluster.
package internalmembercluster

import (
	"context"
	"errors"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/apiretry"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
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

//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=internalmemberclusters,verbs=get;list;watch
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=internalmemberclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=multiclusterservices,verbs=list
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceimports,verbs=delete

// Reconcile handles join/leave for the member cluster controllers and updates its heartbeats.
// For the MCS controller, it needs to delete created serviceImport in the member clusters.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	imcKRef := klog.KRef(req.Namespace, req.Name)
	startTime := time.Now()
	klog.V(2).InfoS("Reconciliation starts", "internalMemberCluster", imcKRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Reconciliation ends", "internalServiceImport", imcKRef, "latency", latency)
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
		if err := r.updateStatus(ctx, &imc, agentStatus); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Second * time.Duration(imc.Spec.HeartbeatPeriodSeconds)}, nil
	case fleetv1alpha1.ClusterStateLeave:
		if r.AgentType == fleetv1alpha1.MultiClusterServiceAgent {
			if err := r.cleanupMCSCreatedResources(ctx); err != nil {
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
		return ctrl.Result{}, r.updateStatus(ctx, &imc, agentStatus)
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

func (r *Reconciler) updateStatus(ctx context.Context, imc *fleetv1alpha1.InternalMemberCluster, desiredAgentStatus fleetv1alpha1.AgentStatus) error {
	oldStatus := imc.Status.DeepCopy()
	// TODO to be deleted after fleet changes the API
	imc.Status = fleetv1alpha1.InternalMemberClusterStatus{
		Conditions:    []metav1.Condition{},
		Capacity:      corev1.ResourceList{},
		Allocatable:   corev1.ResourceList{},
		ResourceUsage: fleetv1alpha1.ResourceUsage{},
		AgentStatus:   imc.Status.AgentStatus,
	}
	setAgentStatus(&imc.Status.AgentStatus, desiredAgentStatus)

	imcKObj := klog.KObj(imc)
	klog.V(2).InfoS("Updating internalMemberCluster status", "internalMemberCluster", imcKObj, "agentStatus", imc.Status.AgentStatus, "oldAgentStatus", oldStatus.AgentStatus)
	updateFunc := func() error {
		return r.HubClient.Status().Update(ctx, imc)
	}
	if err := apiretry.Do(updateFunc); err != nil {
		klog.ErrorS(err, "Failed to update internalMemberCluster status", "internalMemberCluster", klog.KObj(imc), "status", imc.Status)
		return err
	}
	return nil
}

// cleanupMCSCreatedResources deletes the serviceImport created by the MCS controller.
func (r *Reconciler) cleanupMCSCreatedResources(ctx context.Context) error {
	list := &fleetnetv1alpha1.MultiClusterServiceList{}
	if err := r.MemberClient.List(ctx, list); err != nil {
		klog.ErrorS(err, "Failed to list multiClusterService")
		return err
	}
	for _, v := range list.Items {
		name, ok := v.Labels[objectmeta.MultiClusterServiceLabelServiceImport]
		if !ok {
			continue
		}
		serviceImport := &fleetnetv1alpha1.ServiceImport{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: v.Namespace,
				Name:      name,
			},
		}
		deleteFunc := func() error {
			return r.MemberClient.Delete(ctx, serviceImport)
		}
		if err := apiretry.Do(deleteFunc); err != nil && !apierrors.IsNotFound(err) {
			klog.ErrorS(err, "Failed to delete serviceImport", "serviceImport", klog.KObj(serviceImport))
			return err
		}
	}
	klog.V(2).InfoS("Completed cleanup mcs created resources", "mcsCounter", len(list.Items))
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetv1alpha1.InternalMemberCluster{}).
		Complete(r)
}
