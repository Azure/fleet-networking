/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package sustained features the performance test suite for evaluating Fleet networking related latencies under
// peak loads.
package perftest

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/test/e2e/framework"
)

const (
	metricDashboardSvcNS   = "monitoring"
	metricDashboardSvcName = "metrics-dashboard"

	eventuallyTimeout  = time.Second * 10
	eventuallyInterval = time.Millisecond * 250
)

var (
	hubClusterName     = "hub"
	memberClusterNames = []string{"member-1", "member-2", "member-3", "member-4"}

	hubCluster           *framework.Cluster
	memberClusters       []*framework.Cluster
	hubClusterClient     client.Client
	memberCluster1       *framework.Cluster
	memberCluster1Client client.Client
	memberCluster2       *framework.Cluster
	memberCluster2Client client.Client
	memberCluster3       *framework.Cluster
	memberCluster3Client client.Client

	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(fleetnetv1alpha1.AddToScheme(scheme))
	utilruntime.Must(fleetv1alpha1.AddToScheme(scheme))
}

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "fleet-networking performance test suite")
}

var _ = BeforeSuite(func() {
	var err error
	// Initialize access for hub cluster.
	hubCluster, err = framework.NewCluster(hubClusterName, scheme)
	Expect(err).Should(Succeed(), "Failed to initialize access for hub cluster")
	hubClusterClient = hubCluster.Client()

	// Initialize access for member clusters.
	memberClusters = make([]*framework.Cluster, 0, len(memberClusterNames))
	for _, m := range memberClusterNames {
		cluster, err := framework.NewCluster(m, scheme)
		Expect(err).Should(Succeed(), "Failed to initialize access for member cluster %s", m)

		ctx := context.Background()
		Eventually(func() error {
			return cluster.SetupPrometheusAPIServiceAccess(ctx, metricDashboardSvcNS, metricDashboardSvcName)
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		memberClusters = append(memberClusters, cluster)
	}

	// These variables are defined primarily for convenience access in specific test scenarios, e.g.
	// light workload latency tests, as member cluster used in these scenarios are fixed; the rest
	// of the member clusters are accessed by index and not through these convenience variables.
	memberCluster1 = memberClusters[0]
	memberCluster1Client = memberCluster1.Client()
	memberCluster2 = memberClusters[1]
	memberCluster2Client = memberCluster2.Client()
	memberCluster3 = memberClusters[2]
	memberCluster3Client = memberCluster3.Client()
})
