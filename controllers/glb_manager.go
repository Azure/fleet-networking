// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package controllers

import (
	"context"
	"fmt"
	"time"

	networkingv1alpha1 "github.com/Azure/multi-cluster-networking/api/v1alpha1"
	"github.com/Azure/multi-cluster-networking/azureclients"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func (r *GlobalServiceReconciler) initializeAzureClient() error {
	if r.AzureConfig != nil {
		return nil
	}

	azConfig, env, err := azureclients.GetAzureConfigFromSecret(r.Client, r.AzureConfigNamespace, r.AzureConfigSecret)
	if err != nil {
		return err
	}

	publicIPClient, err := azureclients.NewPublicIPClient(&azConfig.AzureAuthConfig, env)
	if err != nil {
		return err
	}

	loadBalancerClient, err := azureclients.NewLoadBalancerClient(&azConfig.AzureAuthConfig, env)
	if err != nil {
		return err
	}

	r.AzureConfig = azConfig
	r.PublicIPClient = publicIPClient
	r.LoadBalancerClient = loadBalancerClient
	return nil
}

// StartReconcileLoop starts the reconciler loop for glb.
func (r *GlobalServiceReconciler) StartReconcileLoop(ctx context.Context) error {
	go wait.UntilWithContext(ctx, r.serviceEndpointsWorker, 0)
	return nil
}

func (r *GlobalServiceReconciler) serviceEndpointsWorker(ctx context.Context) {
	for r.processNextServiceEndpoints(ctx) {
	}
}

func (r *GlobalServiceReconciler) processNextServiceEndpoints(ctx context.Context) bool {
	log := log.FromContext(ctx)

	if !r.Manager.GetCache().WaitForCacheSync(ctx) {
		log.Info("Caches not synced yet")
		time.Sleep(time.Second)
		return true
	}

	if err := r.initializeAzureClient(); err != nil {
		log.Error(err, "unable to initialize Azure clients")
		time.Sleep(time.Second)
		return true
	}

	obj, shutdown := r.WorkQueue.Get()
	if shutdown {

		return false
	}

	// We call Done here so the workqueue knows we have finished
	// processing this item. We also must remember to call Forget if we
	// do not want this work item being re-queued. For example, we do
	// not call Forget if a transient error occurs, instead the item is
	// put back on the workqueue and attempted again after a back-off
	// period.
	defer r.WorkQueue.Done(obj)

	return r.handleServiceEndpoints(ctx, obj)
}

func (r *GlobalServiceReconciler) handleServiceEndpoints(ctx context.Context, obj interface{}) bool {
	log := log.FromContext(ctx)

	var req ServiceEndpoints
	var ok bool
	if req, ok = obj.(ServiceEndpoints); !ok {
		// As the item in the workqueue is actually invalid, we call
		// Forget here else we'd go into a loop of attempting to
		// process a work item that is invalid.
		r.WorkQueue.Forget(obj)
		log.Error(nil, "Queue item was not a ServiceEndpoints", "type", fmt.Sprintf("%T", obj), "value", obj)
		// Return true, don't take a break
		return true
	}
	// RunInformersAndControllers the syncHandler, passing it the namespace/Name string of the
	// resource to be synced.
	if result, err := r.reconcileServiceEndpoints(req); err != nil {
		r.WorkQueue.AddRateLimited(req)
		r.Log.Error(err, "Reconciler error", "request", req)
		return false
	} else if result.RequeueAfter > 0 {
		// The result.RequeueAfter request will be lost, if it is returned
		// along with a non-nil error. But this is intended as
		// We need to drive to stable reconcile loops before queuing due
		// to result.RequestAfter
		r.WorkQueue.Forget(obj)
		r.WorkQueue.AddAfter(req, result.RequeueAfter)
		return true
	} else if result.Requeue {
		r.WorkQueue.AddRateLimited(req)
		return true
	}

	// Finally, if no error occurs we Forget this item so it does not
	// get queued again until another change happens.
	r.WorkQueue.Forget(obj)

	// Return true, don't take a break
	return true
}

func (r *GlobalServiceReconciler) reconcileServiceEndpoints(req ServiceEndpoints) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("serviceEndpoints", req.Service.String())
	log.Info("reconciling service endpoints")

	var globalService networkingv1alpha1.GlobalService
	if err := r.Get(ctx, req.Service, &globalService); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}

		r.Log.Error(err, "unable to fetch GlobalService")
		return ctrl.Result{}, err
	}

	// Fetch the cluster set from globalService.Spec.
	if len(globalService.Spec.ClusterSet) == 0 {
		log.Info("skipping the reconciler since its ClusterSet is not set")
		return ctrl.Result{}, nil
	}
	var clusterSet networkingv1alpha1.ClusterSet
	if err := r.Get(ctx, types.NamespacedName{Namespace: globalService.Namespace, Name: globalService.Spec.ClusterSet}, &clusterSet); err != nil {
		log.WithValues("ClusterSet", globalService.Spec.ClusterSet).Error(err, "uname to fetch ClusterSet")
		return ctrl.Result{}, err
	}

	// Filter the cluster from cluster set.
	clusterFound := false
	for _, cluster := range clusterSet.Spec.Clusters {
		clusterFullName := fmt.Sprintf("%s/%s", req.Service.Namespace, cluster)
		if clusterFullName == req.Cluster {
			clusterFound = true
			break
		}
	}
	if !clusterFound {
		log.Info("skipping the reconciler since it's not part of  globalService.Spec.ClusterSet")
		return ctrl.Result{}, nil
	}

	// Endpoints don't need any further actions when deleting global service.
	if !globalService.ObjectMeta.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	endpoints := globalService.Status.Endpoints
	needUpdateEndpoints := false
	if req.LoadBalancerIP != "" {
		// Add loadBalancerIP to global service endpoints
		serviceFound := false
		for i := range endpoints {
			if endpoints[i].Cluster == req.Cluster {
				serviceFound = true
				if req.LoadBalancerIP != endpoints[i].IP {
					endpoints[i].IP = req.LoadBalancerIP
					endpoints[i].Service = req.Service.String()
					needUpdateEndpoints = true
					break
				}
				// TODO: update Endpoints from service.
			}
		}
		if !serviceFound {
			endpoints = append(endpoints, networkingv1alpha1.GlobalEndpoint{
				Cluster: req.Cluster,
				Service: req.Service.String(),
				IP:      req.LoadBalancerIP,
			})
			needUpdateEndpoints = true
		}
	} else {
		// Delete loadBalancerIP to global service endpoints
		for i := range endpoints {
			if endpoints[i].Cluster == req.Cluster {
				endpoints = append(endpoints[:i], endpoints[i+1:]...)
				needUpdateEndpoints = true
				break
			}
		}
	}

	if needUpdateEndpoints {
		globalService.Status.Endpoints = endpoints
		if err := r.Status().Update(ctx, &globalService); err != nil {
			r.Log.Error(err, "unable to update GlobalService status")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}
