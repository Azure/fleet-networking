/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"context"
	"fmt"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"go.goms.io/fleet-networking/test/framework"
)

var _ = Describe("Test Join/Unjoin workflow", func() {
	var (
		ctx                    = context.Background()
		memberClusterName      = memberClusters[0].Name()
		memberClusterNamespace = "fleet-member-" + memberClusterName
		heartbeatPeriod        = 2
		imcKey                 = types.NamespacedName{Namespace: memberClusterNamespace, Name: memberClusterName}
		imc                    fleetv1alpha1.InternalMemberCluster
		options                = []cmp.Option{
			cmpopts.IgnoreFields(fleetv1alpha1.AgentStatus{}, "LastReceivedHeartbeat"),
			cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "ObservedGeneration"),
		}
	)

	Context("Member cluster agents need to join fleet", func() {
		BeforeEach(func() {
			By("Creating internalMemberCluster")
			imc = fleetv1alpha1.InternalMemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      memberClusterName,
					Namespace: memberClusterNamespace,
				},
				Spec: fleetv1alpha1.InternalMemberClusterSpec{
					State:                  fleetv1alpha1.ClusterStateJoin,
					HeartbeatPeriodSeconds: int32(heartbeatPeriod),
				},
			}
			Expect(hubCluster.Client().Create(ctx, &imc)).Should(Succeed(), "failed to create internalMemberCluster %s", )
		})

		It("MultiClusterService Agent should join", func() {
			Eventually(func() string {
				if err := hubCluster.Client().Get(ctx, imcKey, &imc); err != nil {
					return err.Error()
				}
				want := []fleetv1alpha1.AgentStatus{
					{
						Type: fleetv1alpha1.MultiClusterServiceAgent,
						Conditions: []metav1.Condition{
							{
								Type:   string(fleetv1alpha1.AgentJoined),
								Status: metav1.ConditionTrue,
								Reason: "AgentJoined",
							},
						},
					},
				}
				got
				for _, v := range imc.Status.AgentStatus {
					if v.Type != fleetv1alpha1.MultiClusterServiceAgent {
						continue
					}
				}
				if len(imc.Status.AgentStatus) != 1 || len(imc.Status.AgentStatus[0].Conditions) != 1 {
					return fmt.Sprintf("got empty agent status, want %+v", want)
				}
				return cmp.Diff(want, imc.Status.AgentStatus, options...)
			}, framework.PollTimeout, framework.PollInterval).Should(BeEmpty())
		})
	})
})
