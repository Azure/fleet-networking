/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"os"
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

var (
	HubCluster    = framework.NewCluster(hubClusterName, scheme)
	MemberCluster = framework.NewCluster(memberClusterName, scheme)
	scheme        = runtime.NewScheme()
	hubURL        string
)

const (
	eventuallyTimeout  = time.Second * 10
	eventuallyInterval = time.Millisecond * 250

	hubClusterName       = "kind-hub-testing"
	memberClusterName    = "kind-member-testing"
	FleetSystemNamespace = "fleet-system"
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
	kubeconfig := os.Getenv("KUBECONFIG")
	Expect(kubeconfig).ShouldNot(BeEmpty())

	hubURL = os.Getenv("HUB_SERVER_URL")
	Expect(hubURL).ShouldNot(BeEmpty())

	// hub setup
	HubCluster.HubURL = hubURL
	framework.GetClusterClient(HubCluster)

	//member setup
	MemberCluster.HubURL = hubURL
	framework.GetClusterClient(MemberCluster)
})
