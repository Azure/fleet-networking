/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	fleetv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	fleetnetv1beta1 "go.goms.io/fleet-networking/api/v1beta1"
	"go.goms.io/fleet-networking/test/e2e/framework"
)

// Serial - Ginkgo will guarantee that these specs will never run in parallel with other specs.
// Ordered - Ginkgo will guarantee that specs in an Ordered container will run sequentially, in the order they are written.
// When member cluster leaves, it will delete all the networking resources and cause the flaky behaviors.
var _ = Describe("Test Join/Leave workflow", Serial, Ordered, func() {
	var (
		memberClusterName      = memberClusterNames[0]
		memberClusterNamespace = "fleet-member-" + memberClusterName
		imcKey                 = types.NamespacedName{Namespace: memberClusterNamespace, Name: memberClusterName}
		imc                    fleetv1beta1.InternalMemberCluster
		memberCluster          *framework.Cluster
		cmpOptions             = []cmp.Option{
			cmpopts.IgnoreFields(fleetv1beta1.AgentStatus{}, "LastReceivedHeartbeat"),
			cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "ObservedGeneration"),
			cmpopts.SortSlices(func(status1, status2 fleetv1beta1.AgentStatus) bool { return status1.Type < status2.Type }),
		}
	)
	const (
		heartbeatPeriod = 2
	)

	Context("Member cluster agents should join/leave fleet", func() {
		BeforeEach(func() {
			By("Creating internalMemberCluster")
			imc = fleetv1beta1.InternalMemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      memberClusterName,
					Namespace: memberClusterNamespace,
				},
				Spec: fleetv1beta1.InternalMemberClusterSpec{
					State:                  fleetv1beta1.ClusterStateJoin,
					HeartbeatPeriodSeconds: int32(heartbeatPeriod),
				},
			}
			Expect(hubCluster.Client().Create(ctx, &imc)).Should(Succeed(), "Failed to create internalMemberCluster %s", memberClusterName)
			memberCluster = memberClusters[0]
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
				return cmp.Diff(want, imc.Status.AgentStatus, cmpOptions...)
			}, framework.PollTimeout, framework.PollInterval).Should(BeEmpty(), "Validate internalMemberCluster mismatch (-want, +got):")
			Expect(imc.Status.AgentStatus[0].LastReceivedHeartbeat).ShouldNot(BeNil(), "heartbeat should not be nil")
			Expect(imc.Status.AgentStatus[1].LastReceivedHeartbeat).ShouldNot(BeNil(), "heartbeat should not be nil")
		})

		It("Unjoin member cluster and should cleanup multiClusterService related resources", func() {
			By("Creating multiClusterService")
			serviceName := "my-svc"
			mcs := fleetnetv1alpha1.MultiClusterService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-mcs",
					Namespace: testNamespace,
				},
				Spec: fleetnetv1alpha1.MultiClusterServiceSpec{
					ServiceImport: fleetnetv1alpha1.ServiceImportRef{
						Name: serviceName,
					},
				},
			}
			Expect(memberCluster.Client().Create(ctx, &mcs)).Should(Succeed())

			By("Checking serviceImport in the member cluster")
			Eventually(func() error {
				serviceImport := fleetnetv1alpha1.ServiceImport{}
				key := types.NamespacedName{Namespace: testNamespace, Name: serviceName}
				return memberCluster.Client().Get(ctx, key, &serviceImport)
			}, framework.PollTimeout, framework.PollInterval).Should(Succeed(), "Failed to get serviceImport created by the multiClusterService controller")

			By("Updating internalMemberCluster spec to leave")
			Eventually(func() error {
				if err := hubCluster.Client().Get(ctx, imcKey, &imc); err != nil {
					return err
				}
				imc.Spec.State = fleetv1beta1.ClusterStateLeave
				return hubCluster.Client().Update(ctx, &imc)
			}, framework.PollTimeout, framework.PollInterval).Should(Succeed(), "Failed to update internalMemberCluster spec")

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
				return cmp.Diff(want, imc.Status.AgentStatus, cmpOptions...)
			}, framework.PollTimeout, framework.PollInterval).Should(BeEmpty(), "Validate internalMemberCluster mismatch (-want, +got):")

			By("Validating multiClusterService resources")
			mcsList := &fleetnetv1alpha1.MultiClusterServiceList{}
			Expect(memberCluster.Client().List(ctx, mcsList)).Should(Succeed())
			Expect(len(mcsList.Items) == 0).Should(BeTrue(), "Failed to cleanup multiClusterService resources")

			By("Validating serviceImport resources in the member cluster")
			serviceImportList := &fleetnetv1alpha1.ServiceImportList{}
			Expect(memberCluster.Client().List(ctx, serviceImportList)).Should(Succeed())
			Expect(len(serviceImportList.Items) == 0).Should(BeTrue(), "Failed to cleanup serviceImport resources")
		})

		It("Unjoin member cluster and should cleanup serviceExport related resources", func() {
			By("Creating serviceExport")
			serviceName := "my-svc"
			serviceExport := fleetnetv1beta1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: testNamespace,
				},
			}
			Expect(memberCluster.Client().Create(ctx, &serviceExport)).Should(Succeed())

			By("Updating internalMemberCluster spec to leave")
			Eventually(func() error {
				if err := hubCluster.Client().Get(ctx, imcKey, &imc); err != nil {
					return err
				}
				imc.Spec.State = fleetv1beta1.ClusterStateLeave
				return hubCluster.Client().Update(ctx, &imc)
			}, framework.PollTimeout, framework.PollInterval).Should(Succeed(), "Failed to update internalMemberCluster spec")

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
				return cmp.Diff(want, imc.Status.AgentStatus, cmpOptions...)
			}, framework.PollTimeout, framework.PollInterval).Should(BeEmpty(), "Validate internalMemberCluster mismatch (-want, +got):")

			By("Validating serviceExport resources")
			serviceExportList := &fleetnetv1beta1.ServiceExportList{}
			Expect(memberCluster.Client().List(ctx, serviceExportList)).Should(Succeed())
			Expect(len(serviceExportList.Items) == 0).Should(BeTrue(), "Failed to cleanup serviceExport resources")
		})

		It("Unjoin member cluster first and then join", func() {
			By("Updating internalMemberCluster spec to leave")
			Eventually(func() error {
				if err := hubCluster.Client().Get(ctx, imcKey, &imc); err != nil {
					return err
				}
				imc.Spec.State = fleetv1beta1.ClusterStateLeave
				return hubCluster.Client().Update(ctx, &imc)
			}, framework.PollTimeout, framework.PollInterval).Should(Succeed(), "Failed to update internalMemberCluster spec")

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
				return cmp.Diff(want, imc.Status.AgentStatus, cmpOptions...)
			}, framework.PollTimeout, framework.PollInterval).Should(BeEmpty(), "Validate internalMemberCluster mismatch (-want, +got):")

			By("Updating internalMemberCluster spec to join")
			Eventually(func() error {
				if err := hubCluster.Client().Get(ctx, imcKey, &imc); err != nil {
					return err
				}
				imc.Spec.State = fleetv1beta1.ClusterStateJoin
				return hubCluster.Client().Update(ctx, &imc)
			}, framework.PollTimeout, framework.PollInterval).Should(Succeed(), "Failed to update internalMemberCluster spec")

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
				return cmp.Diff(want, imc.Status.AgentStatus, cmpOptions...)
			}, framework.PollTimeout, framework.PollInterval).Should(BeEmpty(), "Validate internalMemberCluster mismatch (-want, +got):")
			Expect(imc.Status.AgentStatus[0].LastReceivedHeartbeat).ShouldNot(BeNil(), "heartbeat should not be nil")
			Expect(imc.Status.AgentStatus[1].LastReceivedHeartbeat).ShouldNot(BeNil(), "heartbeat should not be nil")
		})
	})
})
