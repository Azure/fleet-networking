// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package controllers

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type ClusterManager struct {
	manager.Manager

	stop context.CancelFunc
}

// NewClusterManager creates a new ClusterManager for a member cluster from its kubeconfig.
func NewClusterManager(name string, kubeconfig *rest.Config, workqueue workqueue.RateLimitingInterface) (*ClusterManager, error) {
	// Initialize the scheme for the cluster manager's API group.
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	mgr, err := ctrl.NewManager(kubeconfig, ctrl.Options{
		Scheme:             scheme,
		LeaderElection:     false,
		MetricsBindAddress: "0",
	})
	if err != nil {
		return nil, err
	}

	// Initialize the service manager.
	if err = (&ServiceManager{
		Name:      name,
		Client:    mgr.GetClient(),
		WorkQueue: workqueue,
		Log:       ctrl.Log.WithName(name),
		Scheme:    mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		return nil, err
	}

	return &ClusterManager{
		Manager: mgr,
	}, nil
}

// Run starts the cluster manager reconciler.
func (mgr *ClusterManager) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	mgr.stop = cancel
	go mgr.Start(ctx) // nolint: errcheck
	return nil
}

// Stop stops cluster manager.
func (mgr *ClusterManager) Stop() {
	mgr.stop()
}
