/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package framework

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/gomega"
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

type Cluster struct {
	Scheme      *runtime.Scheme
	KubeClient  client.Client
	ClusterName string
}

func NewCluster(name string, scheme *runtime.Scheme) *Cluster {
	return &Cluster{
		Scheme:      scheme,
		ClusterName: name,
	}
}

// GetClusterClient returns a Cluster client for the cluster.
func GetClusterClient(cluster *Cluster) {
	clusterConfig := GetClientConfig(cluster)

	restConfig, err := clusterConfig.ClientConfig()
	if err != nil {
		Expect(err).ShouldNot(HaveOccurred())
	}

	client, err := client.New(restConfig, client.Options{Scheme: cluster.Scheme})
	Expect(err).ShouldNot(HaveOccurred())

	cluster.KubeClient = client
}

func GetClientConfig(cluster *Cluster) clientcmd.ClientConfig {
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: getKubeConfig()},
		&clientcmd.ConfigOverrides{
			CurrentContext: fmt.Sprintf("%s-admin", cluster.ClusterName),
		})
}

func getKubeConfig() string {
	var kubeConfigPath string
	kubeconfigEnvKey := "KUBECONFIG"
	kubeconfig := os.Getenv(kubeconfigEnvKey)
	if len(kubeconfig) == 0 {
		homeDir, err := os.UserHomeDir()
		Expect(err).ShouldNot(HaveOccurred())
		kubeConfigPath = filepath.Join(homeDir, "/.kube/config")
	}
	Expect(kubeConfigPath).ShouldNot(BeEmpty())
	_, err := os.Stat(kubeConfigPath)

	Expect(errors.Is(err, os.ErrNotExist)).Should(BeFalse(), "kubeconfig file %s does not exist", kubeConfigPath)
	return kubeConfigPath
}
