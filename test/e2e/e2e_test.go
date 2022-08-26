/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package e2e hosts e2e tests.
package e2e

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/test/e2e/framework"
)

var (
	hubClusterName     = "hub"
	memberClusterNames = []string{"member-1", "member-2"}

	hubCluster     *framework.Cluster
	memberClusters []*framework.Cluster
	scheme         = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(fleetnetv1alpha1.AddToScheme(scheme))
	utilruntime.Must(fleetv1alpha1.AddToScheme(scheme))
}

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "fleet-networking e2e suite")
}

var _ = BeforeSuite(func() {
	var err error

	// hub cluster setup
	hubCluster, err = framework.NewCluster(hubClusterName, scheme)
	Expect(err).Should(Succeed(), "Failed to initialize hubCluster")

	//member cluster setup
	memberClusters = make([]*framework.Cluster, 0, len(memberClusterNames))
	for _, m := range memberClusterNames {
		cluster, err := framework.NewCluster(m, scheme)
		Expect(err).Should(Succeed(), "Failed to initialize memberCluster %s", m)
		memberClusters = append(memberClusters, cluster)
	}
})
