package membercluster

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
)

// Reconciler reconciles a MemberCluster object.
type Reconciler struct {
	client.Client
	Recorder record.EventRecorder
}

// Reconcile handles the deletion of the member cluster and garbage collects all the resources in the cluster namespace.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	mcObjRef := klog.KRef(req.Namespace, req.Name)
	startTime := time.Now()
	klog.V(2).InfoS("Reconciliation starts", "memberCluster", mcObjRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Reconciliation ends", "memberCluster", mcObjRef, "latency", latency)
	}()
	var mc clusterv1beta1.MemberCluster
	if err := r.Client.Get(ctx, req.NamespacedName, &mc); err != nil {
		if errors.IsNotFound(err) {
			klog.V(4).InfoS("Ignoring NotFound memberCluster", "memberCluster", mcObjRef)
			return ctrl.Result{}, nil
		}
		klog.ErrorS(err, "Failed to get memberCluster", "memberCluster", mcObjRef)
		return ctrl.Result{}, err
	}
	// Handle deleting/leaving member cluster, garbage collect all the resources in the cluster namespace.
	if !mc.DeletionTimestamp.IsZero() {
		klog.V(2).InfoS("The member cluster is leaving", "memberCluster", mcObjRef)
		return r.handleDelete(ctx, mc.DeepCopy())
	}

	return ctrl.Result{}, nil
}

func (r *Reconciler) handleDelete(ctx context.Context, mc *clusterv1beta1.MemberCluster) (ctrl.Result, error) {
	// Garbage collect all the resources in the cluster namespace.
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	customPredicate := predicate.Funcs{
		// Ignoring creation and deletion events because the clusterSchedulingPolicySnapshot status is updated when bindings are create/deleted clusterSchedulingPolicySnapshot
		// controller enqueues the CRP name for reconciling whenever clusterSchedulingPolicySnapshot is updated.
		CreateFunc: func(e event.CreateEvent) bool {
			// Ignore creation events.
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return true
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			// Ignore update events.
			return false
		},
	}
	// Watch for changes to primary resource MemberCluster
	return ctrl.NewControllerManagedBy(mgr).
		For(&clusterv1beta1.MemberCluster{}).
		WithEventFilter(customPredicate).
		Complete(r)
}
