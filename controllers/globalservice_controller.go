// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package controllers

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/cloud-provider-azure/pkg/azureclients/loadbalancerclient"
	"sigs.k8s.io/cloud-provider-azure/pkg/azureclients/publicipclient"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	networkingv1alpha1 "github.com/Azure/multi-cluster-networking/api/v1alpha1"
	"github.com/Azure/multi-cluster-networking/azureclients"
	"github.com/go-logr/logr"
)

const (
	// FinalizerName is the name for the finalizer.
	FinalizerName = "mcn.networking.aks.io"
)

// GlobalServiceReconciler reconciles a GlobalService object
type GlobalServiceReconciler struct {
	client.Client
	Scheme  *runtime.Scheme
	Manager ctrl.Manager
	Log     logr.Logger

	AzureConfig          *azureclients.AzureConfig
	LoadBalancerClient   loadbalancerclient.Interface
	PublicIPClient       publicipclient.Interface
	AKSClusterReconciler *AKSClusterReconciler
	AzureConfigSecret    string
	AzureConfigNamespace string

	JitterPeriod time.Duration
	WorkQueue    workqueue.RateLimitingInterface
}

//+kubebuilder:rbac:groups=networking.aks.io,resources=globalservices,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.aks.io,resources=globalservices/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.aks.io,resources=globalservices/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=services/status,verbs=get

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the GlobalService object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *GlobalServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithValues("globalservice", req.NamespacedName)

	var globalService networkingv1alpha1.GlobalService
	if err := r.Get(ctx, req.NamespacedName, &globalService); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("GlobalService not found")
			return ctrl.Result{}, nil
		}

		log.Error(err, "unable to fetch GlobalService")
		return ctrl.Result{}, err
	}

	if !globalService.ObjectMeta.DeletionTimestamp.IsZero() {
		// Delete the global load balancer rule
		log.Info("Deleting global load balancer rule because the global service is under deleting")
		if err := r.reconcileGLB(&globalService, false); err != nil {
			log.Error(err, "unable to cleanup glb")
			return ctrl.Result{}, err
		}

		globalService.ObjectMeta.Finalizers = RemoveItemFromSlice(globalService.Finalizers, FinalizerName)
		if err := r.Update(ctx, &globalService); err != nil {
			log.Error(err, "unable to update finalizer")
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	if !ContainsString(globalService.ObjectMeta.Finalizers, FinalizerName) {
		globalService.ObjectMeta.Finalizers = append(globalService.ObjectMeta.Finalizers, FinalizerName)
		if err := r.Update(ctx, &globalService); err != nil {
			log.Error(err, "unable to update finalizer")
			return ctrl.Result{}, err
		}
	}

	if ret, err := r.reconcileGlobalEndpoints(ctx, &globalService); err != nil {
		return ret, err
	}

	if len(globalService.Status.Endpoints) == 0 {
		// Delete the global load balancer rule
		log.Info("Deleting global load balancer rule because no endpints found for global service")
		return ctrl.Result{}, r.reconcileGLB(&globalService, false)
	}

	if err := r.reconcileGLB(&globalService, true); err != nil {
		log.Error(err, "unable to reconcile global load balancer")
		return ctrl.Result{}, err
	}

	log.Info("reconciled global service")
	return ctrl.Result{}, nil
}

func (r *GlobalServiceReconciler) reconcileGlobalEndpoints(ctx context.Context, globalService *networkingv1alpha1.GlobalService) (ctrl.Result, error) {
	namespacedName := types.NamespacedName{Namespace: globalService.Namespace, Name: globalService.Name}
	log := log.FromContext(ctx).WithValues("globalservice", namespacedName)
	if len(globalService.Spec.ClusterSet) == 0 {
		log.Info("skipping the reconciler since its ClusterSet is not set")
		return ctrl.Result{}, nil
	}

	var clusterSet networkingv1alpha1.ClusterSet
	if err := r.Get(ctx, types.NamespacedName{Namespace: globalService.Namespace, Name: globalService.Spec.ClusterSet}, &clusterSet); err != nil {
		log.WithValues("ClusterSet", globalService.Spec.ClusterSet).Error(err, "uname to fetch ClusterSet")
		return ctrl.Result{}, err
	}

	r.AKSClusterReconciler.Lock.Lock()
	defer r.AKSClusterReconciler.Lock.Unlock()
	for _, clusterName := range clusterSet.Spec.Clusters {
		clusterNamespacedName := types.NamespacedName{Namespace: globalService.Namespace, Name: clusterName}
		if clusterManager, ok := r.AKSClusterReconciler.ClusterManagers[clusterNamespacedName.String()]; ok {
			if !clusterManager.GetCache().WaitForCacheSync(context.Background()) {
				log.Error(fmt.Errorf("unable to sync cache for cluster %s", clusterNamespacedName.String()), "cache not synced")
				return ctrl.Result{}, fmt.Errorf("unable to sync cache for cluster %s", clusterNamespacedName.String())
			}

			client := clusterManager.GetClient()

			var cluster networkingv1alpha1.AKSCluster
			if err := r.Get(ctx, clusterNamespacedName, &cluster); err != nil {
				log.WithValues("cluster", clusterNamespacedName).Error(err, "unable to fetch AKSCluster")
				continue
			}

			var service corev1.Service
			if err := client.Get(ctx, namespacedName, &service); err != nil {
				if apierrors.IsNotFound(err) {
					log.WithValues("cluster", clusterNamespacedName, "service", namespacedName).Info("service not found")
					continue
				}

				// We continue to fetch next clusters if there're something wrong on one of them.
				log.WithValues("cluster", namespacedName).Error(err, "unable to fetch Service")
				continue
			}

			loadBalancerIP := ""
			if len(service.Status.LoadBalancer.Ingress) > 0 && service.ObjectMeta.DeletionTimestamp.IsZero() {
				loadBalancerIP = service.Status.LoadBalancer.Ingress[0].IP
			}
			ret, err := r.reconcileServiceEndpoints(ServiceEndpoints{
				Cluster:        clusterNamespacedName.String(),
				Service:        namespacedName,
				LoadBalancerIP: loadBalancerIP,
				// TODO: add Endpoints here
			})
			if err != nil {
				return ret, err
			}
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GlobalServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Manager = mgr
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkingv1alpha1.GlobalService{}).
		Complete(r)
}

func RemoveItemFromSlice(slice []string, s string) []string {
	var result []string
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return result
}

func ContainsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}
