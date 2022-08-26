/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"context"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"

	"go.goms.io/fleet-networking/test/e2e/framework"
)

var _ = Describe("Test Join/Unjoin workflow", func() {
	var (
		ctx                    context.Context
		memberClusterName      = memberClusterNames[0]
		memberClusterNamespace = "fleet-member-" + memberClusterName
		imcKey                 = types.NamespacedName{Namespace: memberClusterNamespace, Name: memberClusterName}
		imc                    fleetv1alpha1.InternalMemberCluster
		cmpOptions             = []cmp.Option{
			cmpopts.IgnoreFields(fleetv1alpha1.AgentStatus{}, "LastReceivedHeartbeat"),
			cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "ObservedGeneration"),
			cmpopts.SortSlices(func(status1, status2 fleetv1alpha1.AgentStatus) bool { return status1.Type < status2.Type }),
		}
	)
	const (
		heartbeatPeriod = 2
	)

	Context("Member cluster agents should join fleet", func() {
		BeforeEach(func() {
			By("Creating internalMemberCluster")
			ctx = context.Background()
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
			Expect(hubCluster.Client().Create(ctx, &imc)).Should(Succeed(), "Failed to create internalMemberCluster %s", memberClusterName)
		})

		AfterEach(func() {
			By("Deleting internalMemberCluster")
			Expect(hubCluster.Client().Delete(ctx, &imc)).Should(Succeed())
			Eventually(func() bool {
				return errors.IsNotFound(hubCluster.Client().Get(ctx, imcKey, &imc))
			}, framework.PollTimeout, framework.PollInterval).Should(BeTrue(), "Failed to delete internalMemberCluster")
		})

		It("InternalMemberCluster is just created with empty status", func() {
			By("Validating MultiClusterServiceAgent and ServiceExportImportAgent join status")
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
					{
						Type: fleetv1alpha1.ServiceExportImportAgent,
						Conditions: []metav1.Condition{
							{
								Type:   string(fleetv1alpha1.AgentJoined),
								Status: metav1.ConditionTrue,
								Reason: "AgentJoined",
							},
						},
					},
				}
				return cmp.Diff(want, imc.Status.AgentStatus, cmpOptions...)
			}, framework.PollTimeout, framework.PollInterval).Should(BeEmpty(), "Validate internalMemberCluster mismatch (-want, +got):")
			Expect(imc.Status.AgentStatus[0].LastReceivedHeartbeat).ShouldNot(BeNil(), "heartbeat should not be nil")
			Expect(imc.Status.AgentStatus[1].LastReceivedHeartbeat).ShouldNot(BeNil(), "heartbeat should not be nil")
		})

		// TODO add more test cases
	})
})
