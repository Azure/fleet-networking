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
	"go.goms.io/fleet-networking/test/e2e/framework"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	fleetv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	heartbeatPeriod              = 2
	memberClusterNamespaceFormat = "fleet-member-%s"
)

var (
	imcCmpOptions = []cmp.Option{
		cmpopts.IgnoreFields(fleetv1beta1.AgentStatus{}, "LastReceivedHeartbeat"),
		cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "ObservedGeneration"),
		cmpopts.SortSlices(func(status1, status2 fleetv1beta1.AgentStatus) bool { return status1.Type < status2.Type }),
	}
)

func createInternalMemberCluster(ctx context.Context, name string) {
	By("Creating internalMemberCluster")
	imc := fleetv1beta1.InternalMemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: fmt.Sprintf(memberClusterNamespaceFormat, name),
		},
		Spec: fleetv1beta1.InternalMemberClusterSpec{
			State:                  fleetv1beta1.ClusterStateJoin,
			HeartbeatPeriodSeconds: int32(heartbeatPeriod),
		},
	}
	Expect(hubCluster.Client().Create(ctx, &imc)).Should(Succeed(), "Failed to create internalMemberCluster %s", name)
}

func checkIfMemberClusterHasJoined(ctx context.Context, name string) {
	var imc fleetv1beta1.InternalMemberCluster
	namespace := fmt.Sprintf(memberClusterNamespaceFormat, name)
	imcKey := types.NamespacedName{Namespace: namespace, Name: name}

	By("Validating MultiClusterServiceAgent and ServiceExportImportAgent join status")

	Eventually(func() string {
		if err := hubCluster.Client().Get(ctx, imcKey, &imc); err != nil {
			return err.Error()
		}
		want := []fleetv1beta1.AgentStatus{
			{
				Type: fleetv1beta1.MultiClusterServiceAgent,
				Conditions: []metav1.Condition{
					{
						Type:   string(fleetv1beta1.AgentJoined),
						Status: metav1.ConditionTrue,
						Reason: "AgentJoined",
					},
				},
			},
			{
				Type: fleetv1beta1.ServiceExportImportAgent,
				Conditions: []metav1.Condition{
					{
						Type:   string(fleetv1beta1.AgentJoined),
						Status: metav1.ConditionTrue,
						Reason: "AgentJoined",
					},
				},
			},
		}
		return cmp.Diff(want, imc.Status.AgentStatus, imcCmpOptions...)
	}, framework.PollTimeout, framework.PollInterval).Should(BeEmpty(), "Validate internalMemberCluster mismatch (-want, +got):")
	Expect(imc.Status.AgentStatus[0].LastReceivedHeartbeat).ShouldNot(BeNil(), "heartbeat should not be nil")
	Expect(imc.Status.AgentStatus[1].LastReceivedHeartbeat).ShouldNot(BeNil(), "heartbeat should not be nil")
}

func setInternalMemberClusterState(ctx context.Context, name string, state fleetv1beta1.ClusterState) {
	var imc fleetv1beta1.InternalMemberCluster
	namespace := fmt.Sprintf(memberClusterNamespaceFormat, name)
	imcKey := types.NamespacedName{Namespace: namespace, Name: name}

	Eventually(func() error {
		if err := hubCluster.Client().Get(ctx, imcKey, &imc); err != nil {
			return err
		}
		imc.Spec.State = state
		return hubCluster.Client().Update(ctx, &imc)
	}, framework.PollTimeout, framework.PollInterval).Should(Succeed(), "Failed to update internalMemberCluster spec")
}

func checkIfMemberClusterHasLeft(ctx context.Context, name string) {
	var imc fleetv1beta1.InternalMemberCluster
	namespace := fmt.Sprintf(memberClusterNamespaceFormat, name)
	imcKey := types.NamespacedName{Namespace: namespace, Name: name}

	Eventually(func() string {
		if err := hubCluster.Client().Get(ctx, imcKey, &imc); err != nil {
			return err.Error()
		}
		want := []fleetv1beta1.AgentStatus{
			{
				Type: fleetv1beta1.MultiClusterServiceAgent,
				Conditions: []metav1.Condition{
					{
						Type:   string(fleetv1beta1.AgentJoined),
						Status: metav1.ConditionFalse,
						Reason: "AgentLeft",
					},
				},
			},
			{
				Type: fleetv1beta1.ServiceExportImportAgent,
				Conditions: []metav1.Condition{
					{
						Type:   string(fleetv1beta1.AgentJoined),
						Status: metav1.ConditionFalse,
						Reason: "AgentLeft",
					},
				},
			},
		}
		return cmp.Diff(want, imc.Status.AgentStatus, imcCmpOptions...)
	}, framework.PollTimeout, framework.PollInterval).Should(BeEmpty(), "Validate internalMemberCluster mismatch (-want, +got):")
}

func deleteInternalMemberCluster(ctx context.Context, name string) {
	namespace := fmt.Sprintf(memberClusterNamespaceFormat, name)
	imc := fleetv1beta1.InternalMemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	By("Deleting internalMemberCluster")
	Expect(hubCluster.Client().Delete(ctx, &imc)).Should(Succeed())
	imcKey := types.NamespacedName{Namespace: namespace, Name: name}
	Eventually(func() bool {
		return errors.IsNotFound(hubCluster.Client().Get(ctx, imcKey, &imc))
	}, framework.PollTimeout, framework.PollInterval).Should(BeTrue(), "Failed to delete internalMemberCluster")
}
