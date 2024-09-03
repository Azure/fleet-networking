package membercluster

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
)

var (
	fleetMemberNamespace = "fleet-member-%s"
)

const (
	ControllerName = "membercluster-controller"
)

// Reconciler reconciles a MemberCluster object.
type Reconciler struct {
	client.Client
	Recorder record.EventRecorder
	// the wait time in minutes before we force delete a member cluster.
	ForceDeleteWaitTime time.Duration
}

// Reconcile handles the deletion of the member cluster and removes finalizers on all the fleet networking resources
// in the cluster namespace.
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
	// Handle deleting/leaving member cluster, garbage collect all the resources in the cluster namespace
	// after member cluster force delete wait time.
	if !mc.DeletionTimestamp.IsZero() && time.Since(mc.DeletionTimestamp.Time) >= r.ForceDeleteWaitTime {
		klog.V(2).InfoS("The member cluster is leaving", "memberCluster", mcObjRef)
		return r.garbageCollect(ctx, mc.DeepCopy())
	}

	return ctrl.Result{RequeueAfter: r.ForceDeleteWaitTime}, nil
}

func (r *Reconciler) garbageCollect(ctx context.Context, mc *clusterv1beta1.MemberCluster) (ctrl.Result, error) {
	// Garbage collect all the resources in the cluster namespace.
	mcObjRef := klog.KRef(mc.Namespace, mc.Name)
	mcNamespace := fmt.Sprintf(fleetMemberNamespace, mc.Name)
	var endpointSliceImportList fleetnetv1alpha1.EndpointSliceImportList
	err := r.Client.List(ctx, &endpointSliceImportList, client.InNamespace(mcNamespace))
	if err != nil {
		klog.ErrorS(err, "Failed to list endpointSliceImports", "memberCluster", mcObjRef)
		return ctrl.Result{}, err
	}
	if len(endpointSliceImportList.Items) > 0 {
		klog.V(2).InfoS("Remove finalizers for endpointSliceImports", "memberCluster", mcObjRef)
		for i := range endpointSliceImportList.Items {
			esi := &endpointSliceImportList.Items[i]
			esi.SetFinalizers(nil)
			if err := r.Client.Update(ctx, esi); err != nil {
				klog.ErrorS(err, "Failed to remove finalizers for endpointSliceImport", "memberCluster", mcObjRef, "endpointSliceImport", klog.KRef(esi.Namespace, esi.Name))
				return ctrl.Result{}, err
			}
		}
	}
	var endpointSliceExportList fleetnetv1alpha1.EndpointSliceExportList
	err = r.Client.List(ctx, &endpointSliceExportList, client.InNamespace(mcNamespace))
	if err != nil {
		klog.ErrorS(err, "Failed to list endpointSliceExports", "memberCluster", mcObjRef)
		return ctrl.Result{}, err
	}
	if len(endpointSliceExportList.Items) > 0 {
		klog.V(2).InfoS("Remove finalizers for endpointSliceExports", "memberCluster", mcObjRef)
		for i := range endpointSliceExportList.Items {
			ese := &endpointSliceExportList.Items[i]
			ese.SetFinalizers(nil)
			if err := r.Client.Update(ctx, ese); err != nil {
				klog.ErrorS(err, "Failed to remove finalizers for endpointSliceExport", "memberCluster", mcObjRef, "endpointSliceExport", klog.KRef(ese.Namespace, ese.Name))
				return ctrl.Result{}, err
			}
		}
	}
	var internalServiceImportList fleetnetv1alpha1.InternalServiceImportList
	err = r.Client.List(ctx, &internalServiceImportList, client.InNamespace(mcNamespace))
	if err != nil {
		klog.ErrorS(err, "Failed to list internalServiceImports", "memberCluster", mcObjRef)
		return ctrl.Result{}, err
	}
	if len(internalServiceImportList.Items) > 0 {
		klog.V(2).InfoS("Remove finalizers for internalServiceImports", "memberCluster", mcObjRef)
		for i := range internalServiceImportList.Items {
			isi := &internalServiceImportList.Items[i]
			isi.SetFinalizers(nil)
			if err := r.Client.Update(ctx, isi); err != nil {
				klog.ErrorS(err, "Failed to remove finalizers for internalServiceImport", "memberCluster", mcObjRef, "internalServiceImport", klog.KRef(isi.Namespace, isi.Name))
				return ctrl.Result{}, err
			}
		}
	}
	var internalServiceExportList fleetnetv1alpha1.InternalServiceExportList
	err = r.Client.List(ctx, &internalServiceExportList, client.InNamespace(mcNamespace))
	if err != nil {
		klog.ErrorS(err, "Failed to list internalServiceExports", "memberCluster", mcObjRef)
		return ctrl.Result{}, err
	}
	if len(internalServiceExportList.Items) > 0 {
		klog.V(2).InfoS("Remove finalizers for internalServiceExports", "memberCluster", mcObjRef)
		for i := range internalServiceExportList.Items {
			ise := &internalServiceExportList.Items[i]
			ise.SetFinalizers(nil)
			if err := r.Client.Update(ctx, ise); err != nil {
				klog.ErrorS(err, "Failed to remove finalizers for internalServiceExport", "memberCluster", mcObjRef, "internalServiceExport", klog.KRef(ise.Namespace, ise.Name))
				return ctrl.Result{}, err
			}
		}
	}
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
