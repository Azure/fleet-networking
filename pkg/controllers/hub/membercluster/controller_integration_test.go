package membercluster

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
)

const (
	eventuallyTimeout  = time.Minute * 2
	eventuallyInterval = time.Second * 5
)

var _ = Describe("Test MemberCluster Controller", func() {
	Context("Test membercluster controller, handle force delete", func() {
		It("should remove finalizer on endpointsliceimport on fleet member namespace, after force delete wait time is crossed", func() {
			ctx = context.Background()
			var mc clusterv1beta1.MemberCluster
			Expect(hubClient.Get(ctx, types.NamespacedName{Name: memberClusterName}, &mc)).Should(Succeed())
			Expect(hubClient.Delete(ctx, &mc)).Should(Succeed())
			Eventually(func() error {
				var esi fleetnetv1alpha1.EndpointSliceImport
				if err := hubClient.Get(ctx, types.NamespacedName{Name: endpointSliceImportName, Namespace: fleetMemberNS}, &esi); err != nil {
					return err
				}
				if len(esi.GetFinalizers()) != 0 {
					return errors.New("finalizers on endpointsliceimport have not been removed")
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		})
	})
})
