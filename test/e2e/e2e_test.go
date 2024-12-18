/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package e2e hosts e2e tests.
package e2e

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	fleetv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/test/e2e/framework"
)

var (
	hubClusterName     = "hub"
	memberClusterNames = []string{"member-1", "member-2"}

	testNamespace string

	hubCluster     *framework.Cluster
	memberClusters []*framework.Cluster
	fleet          *framework.Fleet

	scheme = runtime.NewScheme()
	ctx    = context.Background()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(fleetnetv1alpha1.AddToScheme(scheme))
	utilruntime.Must(fleetv1beta1.AddToScheme(scheme))
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

	fleet = framework.NewFleet(memberClusters, memberClusters[0], hubCluster)

	testNamespace = framework.UniqueTestNamespace()
	createTestNamespace(context.Background())
})

func createTestNamespace(ctx context.Context) {
	ns := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNamespace,
		},
	}
	Expect(hubCluster.Client().Create(ctx, &ns)).Should(Succeed(), "Failed to create namespace %s cluster %s", testNamespace, hubClusterName)

	for _, m := range memberClusters {
		ns = corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		} // reset ns object
		Expect(m.Client().Create(ctx, &ns)).Should(Succeed(), "Failed to create namespace %s cluster %s", testNamespace, m.Name())
	}
}

var _ = AfterSuite(func() {
	ns := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNamespace,
		},
	}
	Expect(hubCluster.Client().Delete(ctx, &ns)).Should(Succeed(), "Failed to delete namespace %s cluster %s", testNamespace, hubClusterName)
	for _, m := range memberClusters {
		Expect(m.Client().Delete(ctx, &ns)).Should(Succeed(), "Failed to delete namespace %s cluster %s", testNamespace, m.Name())
	}
})
