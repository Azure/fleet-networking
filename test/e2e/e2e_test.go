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
	memberCluster1Name   = "member-1"
	memberCluster2Name   = "member-1"
	fleetSystemNamespace = "fleet-system"
)

var (
	HubCluster     = framework.NewCluster(hubClusterName, scheme)
	memberCluster1 = framework.NewCluster(memberCluster1Name, scheme)
	memberCluster2 = framework.NewCluster(memberCluster2Name, scheme)
	scheme         = runtime.NewScheme()
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
	// hub setup
	framework.GetClusterClient(HubCluster)

	//member setup
	framework.GetClusterClient(memberCluster1)
	framework.GetClusterClient(memberCluster2)
})
