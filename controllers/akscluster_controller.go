// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package controllers

import (
	"context"
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	networkingv1alpha1 "github.com/Azure/multi-cluster-networking/api/v1alpha1"
)

// AKSClusterReconciler reconciles a AKSCluster object
type AKSClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	WorkQueue       workqueue.RateLimitingInterface
	Lock            sync.Mutex
	ClusterManagers map[string]*ClusterManager
}

//+kubebuilder:rbac:groups=networking.aks.io,resources=aksclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.aks.io,resources=aksclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.aks.io,resources=aksclusters/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the AKSCluster object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *AKSClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithValues("akscluster", req.NamespacedName)

	var cluster networkingv1alpha1.AKSCluster
	if err := r.Get(ctx, req.NamespacedName, &cluster); err != nil {
		log.Error(err, "unable to fetch AKSCluster")
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	r.Lock.Lock()
	defer r.Lock.Unlock()

	// Stop ClusterManager on cluster deletion.
	clusterName := req.NamespacedName.String()
	if !cluster.ObjectMeta.DeletionTimestamp.IsZero() {
		if mgr, ok := r.ClusterManagers[clusterName]; ok {
			mgr.Stop()
			delete(r.ClusterManagers, clusterName)
		}

		return ctrl.Result{}, nil
	}

	// Return if ClusterManager has already started.
	if _, ok := r.ClusterManagers[clusterName]; ok {
		return ctrl.Result{}, nil
	}

	// Fetch the kubeconfig for the cluster.
	kubeconfig, err := r.getKubeConfig(cluster.Spec.KubeConfigSecret, cluster.Namespace)
	if err != nil {
		log.Error(err, "unable to get kubeconfig from its secret")
		return ctrl.Result{}, err
	}
	restConfig, err := clientcmd.BuildConfigFromKubeconfigGetter("", func() (*clientcmdapi.Config, error) {
		return clientcmd.Load([]byte(kubeconfig))
	})
	if err != nil {
		log.Error(err, "unable to parse kubeconfig")
		return ctrl.Result{}, err
	}

	// Create and start a new ClusterManager for the new cluster.
	mgr, err := NewClusterManager(clusterName, restConfig, r.WorkQueue)
	if err != nil {
		return ctrl.Result{}, err
	}
	if err := mgr.Run(); err != nil {
		return ctrl.Result{}, err
	}
	r.ClusterManagers[clusterName] = mgr

	return ctrl.Result{}, nil
}

func (r *AKSClusterReconciler) getKubeConfig(secretName, secretNamespace string) (string, error) {
	var secret corev1.Secret
	namespacedName := types.NamespacedName{Namespace: secretNamespace, Name: secretName}
	if err := r.Get(context.Background(), namespacedName, &secret); err != nil {
		return "", err
	}

	kubeconfig, ok := secret.Data["kubeconfig"]
	if !ok || len(kubeconfig) == 0 {
		return "", fmt.Errorf("kubeconfig not found in secret %s", namespacedName)
	}

	return string(kubeconfig), nil
}

// GetClusterManager gets the cluster manager by cluster name.
func (r *AKSClusterReconciler) GetClusterManager(clusterName string) *ClusterManager {
	r.Lock.Lock()
	defer r.Lock.Unlock()

	if mgr, ok := r.ClusterManagers[clusterName]; ok {
		return mgr
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AKSClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkingv1alpha1.AKSCluster{}).
		Complete(r)
}
