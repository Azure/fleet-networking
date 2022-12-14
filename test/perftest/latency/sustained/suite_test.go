/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package sustained features the performance test suite for evaluating Fleet networking related latencies under
// sustained loads.
package sustained

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
	clientQPS      = 25
	clientBurstQPS = 50

	metricDashboardSvcNS   = "monitoring"
	metricDashboardSvcName = "metrics-dashboard"

	eventuallyTimeout  = time.Second * 10
	eventuallyInterval = time.Millisecond * 250
)

var (
	hubClusterName     = "hub"
	memberClusterNames = []string{"member-1", "member-2", "member-3", "member-4"}

	hubCluster       *framework.Cluster
	memberClusters   []*framework.Cluster
	hubClusterClient client.Client

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
	hubCluster, err = framework.NewClusterWithBurstQPS(hubClusterName, scheme, clientQPS, clientBurstQPS)
	Expect(err).Should(Succeed(), "Failed to initialize access for hub cluster")
	hubClusterClient = hubCluster.Client()

	// Initialize access for member clusters.
	memberClusters = make([]*framework.Cluster, 0, len(memberClusterNames))
	for _, m := range memberClusterNames {
		cluster, err := framework.NewClusterWithBurstQPS(m, scheme, clientQPS, clientBurstQPS)
		Expect(err).Should(Succeed(), "Failed to initialize access for member cluster %s", m)

		ctx := context.Background()
		Eventually(func() error {
			return cluster.SetupPrometheusAPIServiceAccess(ctx, metricDashboardSvcNS, metricDashboardSvcName)
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		memberClusters = append(memberClusters, cluster)
	}
})
