/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1beta1

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	"go.goms.io/fleet/pkg/utils/controller"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/apiretry"
)

const (
	conditionReasonJoined = "AgentJoined"
	conditionReasonLeft   = "AgentLeft"

	// we add +-5% jitter
	jitterPercent = 10
)

// Reconciler reconciles a InternalMemberCluster object.
type Reconciler struct {
	MemberClient client.Client
	HubClient    client.Client
	AgentType    clusterv1beta1.AgentType

	Controllers []controller.MemberController
}

//+kubebuilder:rbac:groups=cluster.kubernetes-fleet.io,resources=internalmemberclusters,verbs=get;list;watch
//+kubebuilder:rbac:groups=cluster.kubernetes-fleet.io,resources=internalmemberclusters/status,verbs=get;update;patch
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

	var imc clusterv1beta1.InternalMemberCluster
	if err := r.HubClient.Get(ctx, req.NamespacedName, &imc); err != nil {
		if apierrors.IsNotFound(err) {
			klog.V(4).InfoS("internal member cluster object is not found", "internalMemberCluster", imcKRef)
			return ctrl.Result{}, nil
		}
		klog.ErrorS(err, "Failed to get internal member cluster object", "internalMemberCluster", imcKRef)
		return ctrl.Result{}, err
	}

	switch imc.Spec.State {
	case clusterv1beta1.ClusterStateLeave:
		// The member cluster is leaving the fleet.
		klog.V(2).InfoS("member cluster has left the fleet; performing cleanup", "internalMemberCluster", imcKRef)
		if err := r.stopControllers(ctx); err != nil {
			klog.ErrorS(err, "Failed to stop member controllers", "internalMemberCluster", imcKRef)
			return ctrl.Result{}, err
		}

		// Clean up fleet networking related resources.
		if r.AgentType == clusterv1beta1.MultiClusterServiceAgent {
			if err := r.cleanupMCSRelatedResources(ctx); err != nil {
				return ctrl.Result{}, err
			}
		}
		if r.AgentType == clusterv1beta1.ServiceExportImportAgent {
			if err := r.cleanupServiceExportRelatedResources(ctx); err != nil {
				return ctrl.Result{}, err
			}
		}

		// Update the agent status.
		return ctrl.Result{}, r.updateAgentStatus(ctx, &imc)
	case clusterv1beta1.ClusterStateJoin:
		if err := r.startControllers(ctx); err != nil {
			klog.ErrorS(err, "Failed to start member controllers", "internalMemberCluster", imcKRef)
			return ctrl.Result{}, err
		}

		// The member cluster still has an active membership in the fleet; update the agent status.
		if err := r.updateAgentStatus(ctx, &imc); err != nil {
			return ctrl.Result{}, err
		}

		// Add jitter to the heartbeat report interval, so as to mitigate the thundering herd problem.
		hbInterval := 1000 * imc.Spec.HeartbeatPeriodSeconds
		jitterRange := int64(hbInterval*jitterPercent) / 100
		requeueAfter := time.Millisecond * (time.Duration(hbInterval) + time.Duration(rand.Int63nRange(0, jitterRange)-jitterRange/2))
		return ctrl.Result{RequeueAfter: requeueAfter}, nil
	default:
		klog.ErrorS(fmt.Errorf("cluster is of an invalid state"), "internalMemberCluster", imcKRef, "clusterState", imc.Spec.State)
	}

	return ctrl.Result{}, nil
}

// updateAgentStatus reports the status of the agent via internal member cluster object.
func (r *Reconciler) updateAgentStatus(ctx context.Context, imc *clusterv1beta1.InternalMemberCluster) error {
	imcKObj := klog.KObj(imc)
	klog.V(2).InfoS("Updating internal member cluster status", "internalMemberCluster", imcKObj, "agentType", r.AgentType)

	agentStatus := imc.GetAgentStatus(r.AgentType)

	if imc.Spec.State == clusterv1beta1.ClusterStateJoin {
		// The member cluster still has an active membership in the fleet.
		meta.SetStatusCondition(&agentStatus.Conditions, metav1.Condition{
			Type:               string(clusterv1beta1.AgentJoined),
			Status:             metav1.ConditionTrue,
			Reason:             conditionReasonJoined,
			ObservedGeneration: imc.GetGeneration(),
		})

		// Update the last received heartbeat value.
		agentStatus.LastReceivedHeartbeat = metav1.NewTime(time.Now())
	} else {
		// The member cluster has left the fleet.
		meta.SetStatusCondition(&agentStatus.Conditions, metav1.Condition{
			Type:               string(clusterv1beta1.AgentJoined),
			Status:             metav1.ConditionFalse,
			Reason:             conditionReasonLeft,
			ObservedGeneration: imc.GetGeneration(),
		})

		// No need to send more heartbeats to the hub cluster as the meber cluster has left.
	}

	if err := r.HubClient.Status().Update(ctx, imc); err != nil {
		if apierrors.IsConflict(err) {
			klog.V(2).InfoS("Failed to update internal member cluster status due to conflicts", "internalMemberCluster", klog.KObj(imc))
			return nil
		}

		klog.ErrorS(err, "Failed to update internal member cluster status", "internalMemberCluster", klog.KObj(imc))
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
		klog.ErrorS(err, "Failed to list MCS")
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
			klog.ErrorS(err, "Failed to delete MCS", "multiClusterService", klog.KObj(&list.Items[i]))
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
			klog.ErrorS(err, "The MCS has not been deleted in time", "multiClusterService", name)
			return err
		}
	}

	klog.V(2).InfoS("Cleanup of MCS related resources has been completed", "objectCounter", len(list.Items))
	return nil
}

// cleanupServiceExportRelatedResources deletes the serviceExport related resources.
// Ideally it should stop the controllers.
// For now, it tries its best to delete the existing serviceExport and won't handle the newly created resources for now.
func (r *Reconciler) cleanupServiceExportRelatedResources(ctx context.Context) error {
	list := &fleetnetv1alpha1.ServiceExportList{}
	if err := r.MemberClient.List(ctx, list); err != nil {
		klog.ErrorS(err, "Failed to list service export")
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
			klog.ErrorS(err, "Failed to delete service export", "serviceExport", klog.KObj(&list.Items[i]))
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
			klog.ErrorS(err, "The service export has not been deleted in time", "serviceExport", name)
			return err
		}
	}

	klog.V(2).InfoS("Cleanup of service export related resources has been completed", "objectCounter", len(list.Items))
	return nil
}

func (r *Reconciler) startControllers(ctx context.Context) error {
	errs, cctx := errgroup.WithContext(ctx)
	for i := range r.Controllers {
		c := r.Controllers[i]
		errs.Go(func() error {
			return c.Join(cctx)
		})
	}
	return errs.Wait()
}

func (r *Reconciler) stopControllers(ctx context.Context) error {
	errs, cctx := errgroup.WithContext(ctx)
	for i := range r.Controllers {
		c := r.Controllers[i]
		errs.Go(func() error {
			return c.Leave(cctx)
		})
	}
	return errs.Wait()
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&clusterv1beta1.InternalMemberCluster{}).
		Complete(r)
}
