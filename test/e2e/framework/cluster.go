/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package framework provides common functionalities for handling a Kubernertes cluster.
package framework

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	// PollInterval defines the interval time for a poll operation.
	PollInterval = 5 * time.Second
	// PollTimeout defines the time after which the poll operation times out.
	PollTimeout = 30 * time.Second
)

// Cluster represents a Kubernetes cluster.
type Cluster struct {
	Scheme      *runtime.Scheme
	KubeClient  client.Client
	ClusterName string
}

// GetCluster returns a cluster from the cluster name.
func GetCluster(name string, scheme *runtime.Scheme) *Cluster {
	cluster := &Cluster{
		Scheme:      scheme,
		ClusterName: name,
	}
	cluster.initClusterClient()
	return cluster
}

func (c *Cluster) initClusterClient() {
	clusterConfig := c.getClientConfig()

	restConfig, err := clusterConfig.ClientConfig()
	if err != nil {
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
	}

	client, err := client.New(restConfig, client.Options{Scheme: c.Scheme})
	gomega.Expect(err).ShouldNot(gomega.HaveOccurred())

	c.KubeClient = client
}

func (c *Cluster) getClientConfig() clientcmd.ClientConfig {
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: getKubeConfig()},
		&clientcmd.ConfigOverrides{
			CurrentContext: fmt.Sprintf("%s-admin", c.ClusterName),
		})
}

func getKubeConfig() string {
	kubeconfigEnvKey := "KUBECONFIG"
	kubeConfigPath := os.Getenv(kubeconfigEnvKey)
	if len(kubeConfigPath) == 0 {
		homeDir, err := os.UserHomeDir()
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
		kubeConfigPath = filepath.Join(homeDir, "/.kube/config")
	}
	gomega.Expect(kubeConfigPath).ShouldNot(gomega.BeEmpty())
	_, err := os.Stat(kubeConfigPath)

	gomega.Expect(errors.Is(err, os.ErrNotExist)).Should(gomega.BeFalse(), "kubeconfig file %s does not exist", kubeConfigPath)
	return kubeConfigPath
}