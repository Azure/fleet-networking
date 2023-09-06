/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1beta1

import (
	"fmt"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

const (
	workNamespaceName                  = "work"
	memberClusterReservedNamespaceName = "fleet-member-member-cluster"

	mcsName1       = "svc-1"
	mcsName2       = "svc-2"
	svcExportName1 = "svcexport-1"
	svcExportName2 = "svcexport-2"
)

const (
	eventuallyTimeout  = time.Second * 10
	eventuallyInterval = time.Millisecond * 500
)

var (
	lessFuncAgentStatus = func(a, b clusterv1beta1.AgentStatus) bool {
		return a.Type < b.Type
	}
)

var _ = Describe("Test InternalMemberCluster Controllers", Ordered, func() {
	startTime := metav1.NewTime(time.Now())

	BeforeAll(func() {
		// Set up resources for testing purposes.

		workNS := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: workNamespaceName,
			},
		}
		Expect(memberClient.Create(ctx, workNS)).To(Succeed())

		multiClusterSvc1 := &fleetnetv1alpha1.MultiClusterService{
			ObjectMeta: metav1.ObjectMeta{
				Name:      mcsName1,
				Namespace: workNamespaceName,
			},
			Spec: fleetnetv1alpha1.MultiClusterServiceSpec{
				ServiceImport: fleetnetv1alpha1.ServiceImportRef{
					Name: mcsName1,
				},
			},
		}
		Expect(memberClient.Create(ctx, multiClusterSvc1)).To(Succeed())

		multiClusterSvc2 := &fleetnetv1alpha1.MultiClusterService{
			ObjectMeta: metav1.ObjectMeta{
				Name:      mcsName2,
				Namespace: workNamespaceName,
			},
			Spec: fleetnetv1alpha1.MultiClusterServiceSpec{
				ServiceImport: fleetnetv1alpha1.ServiceImportRef{
					Name: mcsName2,
				},
			},
		}
		Expect(memberClient.Create(ctx, multiClusterSvc2)).To(Succeed())

		svcExport1 := &fleetnetv1alpha1.ServiceExport{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svcExportName1,
				Namespace: workNamespaceName,
			},
		}
		Expect(memberClient.Create(ctx, svcExport1)).To(Succeed())

		svcExport2 := &fleetnetv1alpha1.ServiceExport{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svcExportName2,
				Namespace: workNamespaceName,
			},
		}
		Expect(memberClient.Create(ctx, svcExport2)).To(Succeed())

		memberClusterReservedNS := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: memberClusterReservedNamespaceName,
			},
		}
		Expect(hubClient.Create(ctx, memberClusterReservedNS)).To(Succeed())

		internalMemberCluster := &clusterv1beta1.InternalMemberCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      memberClusterName,
				Namespace: memberClusterReservedNamespaceName,
			},
			Spec: clusterv1beta1.InternalMemberClusterSpec{
				State: clusterv1beta1.ClusterStateJoin,
			},
		}
		Expect(hubClient.Create(ctx, internalMemberCluster)).To(Succeed())
	})

	It("should update internal member cluster status when a member cluster joins", func() {
		Eventually(func() error {
			internalMemberCluster := &clusterv1beta1.InternalMemberCluster{}
			if err := hubClient.Get(ctx, types.NamespacedName{Name: memberClusterName, Namespace: memberClusterReservedNamespaceName}, internalMemberCluster); err != nil {
				return err
			}

			wantAgentStatus := []clusterv1beta1.AgentStatus{
				{
					Type: mcsAgentType,
					Conditions: []metav1.Condition{
						{
							Type:               string(clusterv1beta1.AgentJoined),
							Status:             metav1.ConditionTrue,
							Reason:             conditionReasonJoined,
							ObservedGeneration: internalMemberCluster.GetGeneration(),
						},
					},
				},
				{
					Type: serviceExportImportAgentType,
					Conditions: []metav1.Condition{
						{
							Type:               string(clusterv1beta1.AgentJoined),
							Status:             metav1.ConditionTrue,
							Reason:             conditionReasonJoined,
							ObservedGeneration: internalMemberCluster.GetGeneration(),
						},
					},
				},
			}
			if diff := cmp.Diff(
				internalMemberCluster.Status.AgentStatus, wantAgentStatus,
				ignoreConditionLTTAndMessageFields,
				ignoreAgentStatusLastReceivedHeartbeatField,
				cmpopts.SortSlices(lessFuncAgentStatus),
			); diff != "" {
				return fmt.Errorf("agent status diff (-got, +want): %s", diff)
			}

			lastReceivedHeartbeat := internalMemberCluster.Status.AgentStatus[0].LastReceivedHeartbeat
			if lastReceivedHeartbeat.Before(&startTime) {
				return fmt.Errorf("lastReceivedHeartbeat = %v, want before %v", lastReceivedHeartbeat, startTime)
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed(), "Failed to update internal member cluster status as expected")
	})

	It("can set the member cluster to leave the fleet", func() {
		internalMemberCluster := &clusterv1beta1.InternalMemberCluster{}
		Expect(hubClient.Get(ctx, types.NamespacedName{Name: memberClusterName, Namespace: memberClusterReservedNamespaceName}, internalMemberCluster)).To(Succeed())

		internalMemberCluster.Spec.State = clusterv1beta1.ClusterStateLeave
		Expect(hubClient.Update(ctx, internalMemberCluster)).To(Succeed())
	})

	It("should update internal member cluster status when a member cluster leaves", func() {
		Eventually(func() error {
			internalMemberCluster := &clusterv1beta1.InternalMemberCluster{}
			if err := hubClient.Get(ctx, types.NamespacedName{Name: memberClusterName, Namespace: memberClusterReservedNamespaceName}, internalMemberCluster); err != nil {
				return err
			}

			wantAgentStatus := []clusterv1beta1.AgentStatus{
				{
					Type: mcsAgentType,
					Conditions: []metav1.Condition{
						{
							Type:               string(clusterv1beta1.AgentJoined),
							Status:             metav1.ConditionFalse,
							Reason:             conditionReasonLeft,
							ObservedGeneration: internalMemberCluster.GetGeneration(),
						},
					},
				},
				{
					Type: serviceExportImportAgentType,
					Conditions: []metav1.Condition{
						{
							Type:               string(clusterv1beta1.AgentJoined),
							Status:             metav1.ConditionFalse,
							Reason:             conditionReasonLeft,
							ObservedGeneration: internalMemberCluster.GetGeneration(),
						},
					},
				},
			}
			if diff := cmp.Diff(
				internalMemberCluster.Status.AgentStatus, wantAgentStatus,
				ignoreConditionLTTAndMessageFields,
				ignoreAgentStatusLastReceivedHeartbeatField,
				cmpopts.SortSlices(lessFuncAgentStatus),
			); diff != "" {
				return fmt.Errorf("agent status diff (-got, +want): %s", diff)
			}

			// Heartbeats are no longer updated; skip the check.

			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed(), "Failed to update internal member cluster status as expected")
	})

	It("should clean up all MCS related resources when a member cluster leaves", func() {
		Eventually(func() error {
			mcsList := &fleetnetv1alpha1.MultiClusterServiceList{}
			if err := memberClient.List(ctx, mcsList); err != nil {
				return err
			}

			if len(mcsList.Items) != 0 {
				return fmt.Errorf("MCS count = %v, want 0", len(mcsList.Items))
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed(), "Failed to clean up MCS related resources")
	})

	It("should clean up all service export related resources when a member cluster leaves", func() {
		Eventually(func() error {
			svcExportList := &fleetnetv1alpha1.ServiceExportList{}
			if err := memberClient.List(ctx, svcExportList); err != nil {
				return err
			}

			if len(svcExportList.Items) != 0 {
				return fmt.Errorf("ServiceExport count = %v, want 0", len(svcExportList.Items))
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed(), "Failed to clean up service export related resources")
	})
})
