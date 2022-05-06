// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package controllers

import (
	"context"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ServiceManager reconciles service objects inside member clusters.
type ServiceManager struct {
	client.Client
	Name      string
	Log       logr.Logger
	Scheme    *runtime.Scheme
	WorkQueue workqueue.RateLimitingInterface
}

// ServiceEndpoints defines the endpoints for the service.
type ServiceEndpoints struct {
	Cluster        string
	Service        types.NamespacedName
	LoadBalancerIP string
	Endpoints      string // slice couldn't be used here because it would be used as map key.
}

// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=services/status,verbs=get
// +kubebuilder:rbac:groups=core,resources=endpoints,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=endpoints/status,verbs=get

// SetupWithManager registers ServiceManager reconciler.
func (r *ServiceManager) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Service{}).
		Named(r.Name).
		Complete(r)
}

// Reconcile reconciles the service from member cluster.
func (r *ServiceManager) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("ServiceManager", req.NamespacedName)

	var service corev1.Service
	if err := r.Get(ctx, req.NamespacedName, &service); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}

		log.Error(err, "unable to fetch Service")
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !service.ObjectMeta.DeletionTimestamp.IsZero() {
		log.Info("deleting the service")
		serviceEndpoints := ServiceEndpoints{
			Cluster: r.Name,
			Service: req.NamespacedName,
			// TODO: fetch and update Endpoints
			LoadBalancerIP: "",
		}
		r.WorkQueue.Add(serviceEndpoints)
		return ctrl.Result{}, nil
	}

	if len(service.Status.LoadBalancer.Ingress) > 0 {
		log.Info("reconciling the service")
		loadBalancerIP := service.Status.LoadBalancer.Ingress[0].IP
		serviceEndpoints := ServiceEndpoints{
			Cluster: r.Name,
			Service: req.NamespacedName,
			// TODO: fetch and update Endpoints
			LoadBalancerIP: loadBalancerIP,
		}
		r.WorkQueue.Add(serviceEndpoints)
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}
