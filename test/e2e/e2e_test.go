/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	"go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/test/e2e/framework"
)

const (
	eventuallyTimeout  = time.Second * 10
	eventuallyInterval = time.Millisecond * 250

	hubClusterName       = "hub"
	memberClusterOneName = "member-1"
	memberClusterTwoName = "member-2"
	fleetSystemNamespace = "fleet-system"
)

var (
	hubCluster       *framework.Cluster
	memberClusterOne *framework.Cluster
	memberClusterTwo *framework.Cluster
	scheme           = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
}

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "fleet-networking e2e suite")
}

var _ = BeforeSuite(func() {
	// hub cluster setup
	hubCluster = framework.GetCluster(hubClusterName, scheme)

	//member cluster setup
	memberClusterOne = framework.GetCluster(memberClusterOneName, scheme)
	memberClusterTwo = framework.GetCluster(memberClusterTwoName, scheme)
})
