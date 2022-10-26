/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package framework provides common functionalities for e2e tests.
package framework

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Cluster represents a Kubernetes cluster.
type Cluster struct {
	scheme                   *runtime.Scheme
	kubeClient               client.Client
	name                     string
	prometheusAPIServiceAddr string
}

// NewCluster creates Cluster and initializes its kubernetes client.
func NewCluster(name string, scheme *runtime.Scheme) (*Cluster, error) {
	cluster := &Cluster{
		scheme: scheme,
		name:   name,
	}
	if err := cluster.initClusterClient(); err != nil {
		return nil, err
	}
	return cluster, nil
}

// Name returns the cluster name.
func (c *Cluster) Name() string {
	return c.name
}

// Client returns the kubernetes client.
func (c *Cluster) Client() client.Client {
	return c.kubeClient
}

// PrometheusAPIServiceAddress returns the address of the Prometheus API service.
func (c *Cluster) PrometheusAPIServiceAddress() string {
	return c.prometheusAPIServiceAddr
}

// SetupPrometheusAPIServiceAccess retrieves address of the Prometheus API service.
func (c *Cluster) SetupPrometheusAPIServiceAccess(ctx context.Context, prometheusAPISvcNS, prometheusAPISvcName string) error {
	kubeClient := c.Client()
	prometheusAPISvcKey := types.NamespacedName{Namespace: prometheusAPISvcNS, Name: prometheusAPISvcName}
	prometheusAPISvc := &corev1.Service{}
	if err := kubeClient.Get(ctx, prometheusAPISvcKey, prometheusAPISvc); err != nil {
		return err
	}

	if len(prometheusAPISvc.Status.LoadBalancer.Ingress) == 0 {
		return fmt.Errorf("no load balancer is available")
	}
	prometheusAPISvcAddr := prometheusAPISvc.Status.LoadBalancer.Ingress[0].IP

	if len(prometheusAPISvc.Spec.Ports) == 0 {
		return fmt.Errorf("no port is available")
	}
	prometheusAPISvcPort := prometheusAPISvc.Spec.Ports[0].Port
	c.prometheusAPIServiceAddr = fmt.Sprintf("http://%s:%d", prometheusAPISvcAddr, prometheusAPISvcPort)
	return nil
}

func (c *Cluster) initClusterClient() error {
	clusterConfig, err := c.buildClientConfig()
	if err != nil {
		return err
	}

	restConfig, err := clusterConfig.ClientConfig()
	if err != nil {
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
	}

	kubeClient, err := client.New(restConfig, client.Options{Scheme: c.scheme})
	if err != nil {
		return err
	}
	c.kubeClient = kubeClient
	return nil
}

func (c *Cluster) buildClientConfig() (clientcmd.ClientConfig, error) {
	kubeConfig, err := fetchKubeConfig()
	if err != nil {
		return nil, err
	}
	cf := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeConfig},
		&clientcmd.ConfigOverrides{CurrentContext: fmt.Sprintf("%s-admin", c.name)},
	)
	return cf, nil
}

func fetchKubeConfig() (string, error) {
	kubeConfigEnvKey := "KUBECONFIG"
	kubeConfigPath := os.Getenv(kubeConfigEnvKey)
	if len(kubeConfigPath) == 0 {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		kubeConfigPath = filepath.Join(homeDir, "/.kube/config")
	}
	if _, err := os.Stat(kubeConfigPath); err != nil {
		return "", fmt.Errorf("failed to find kubeconfig file %s: %w", kubeConfigPath, err)
	}
	return kubeConfigPath, nil
}
