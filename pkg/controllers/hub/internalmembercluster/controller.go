package internalmembercluster

import (
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	"go.goms.io/fleet/pkg/utils/condition"
)

// Reconciler reconciles the distribution of EndpointSlices across the fleet.
type Reconciler struct {
	HubClient client.Client
}

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
			klog.V(4).InfoS("Internal member cluster object is not found", "internalMemberCluster", imcKRef)
			return ctrl.Result{}, nil
		}
		klog.ErrorS(err, "Failed to get internal member cluster object", "internalMemberCluster", imcKRef)
		return ctrl.Result{}, err
	}

	if imc.Spec.state != clusterv1beta1.ClusterStateLeave {
		klog.V(2).Info("Skipping handling the internalMemberCluster non leave state", "internalMemberCluster", imcKRef, "clusterState", imc.Spec.State)
		return ctrl.Result{}, nil
	}

	if condition.IsConditionStatusTrue(imc.GetConditionWithType(clusterv1beta1.MultiClusterServiceAgent, clusterv1beta1.AgentJoined))
	 && condition.IsConditionStatusTrue(imc.GetConditionWithType(clusterv1beta1.ServiceExportImportAgent, clusterv1beta1.AgentJoined)) {
		 // remove finalizer for internal member cluster if any in the reserved member namespace
	}
	return ctrl.Result{}, nil
}

func (r *Reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetnetv1alpha1.InternalMemberCluster{}).
		Complete(r)
}
