/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"

	fleetv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/test/e2e/framework"
)

// Serial - Ginkgo will guarantee that these specs will never run in parallel with other specs.
// Ordered - Ginkgo will guarantee that specs in an Ordered container will run sequentially, in the order they are written.
// When member cluster leaves, it will delete all the networking resources and cause the flaky behaviors.
var _ = Describe("Test Join/Leave workflow", Serial, Ordered, func() {
	var (
		ctx               context.Context
		memberClusterName = memberClusterNames[0]
		memberCluster     *framework.Cluster
		serviceName       = "my-svc"
	)

	Context("Member cluster agents should join/leave fleet", Serial, Ordered, func() {
		BeforeEach(func() {
			ctx = context.Background()
			memberCluster = memberClusters[0]
		})

		AfterEach(func() {
			By("Updating internalMemberCluster spec to join")
			setInternalMemberClusterState(ctx, memberClusterName, fleetv1beta1.ClusterStateJoin)
			checkIfMemberClusterHasJoined(ctx, memberClusterName)
		})

		It("Unjoin member cluster and should cleanup multiClusterService related resources", func() {
			By("Creating multiClusterService")
			mcs := framework.MultiClusterService(testNamespace, "my-mcs", serviceName)
			Expect(memberCluster.Client().Create(ctx, mcs)).Should(Succeed())

			By("Checking serviceImport in the member cluster")
			Eventually(func() error {
				serviceImport := fleetnetv1alpha1.ServiceImport{}
				key := types.NamespacedName{Namespace: testNamespace, Name: serviceName}
				return memberCluster.Client().Get(ctx, key, &serviceImport)
			}, framework.PollTimeout, framework.PollInterval).Should(Succeed(), "Failed to get serviceImport created by the multiClusterService controller")

			By("Updating internalMemberCluster spec to leave")
			setInternalMemberClusterState(ctx, memberClusterName, fleetv1beta1.ClusterStateLeave)

			By("Validating MultiClusterServiceAgent and ServiceExportImportAgent join status")
			checkIfMemberClusterHasLeft(ctx, memberClusterName)

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
			serviceExport := framework.ServiceExport(testNamespace, serviceName)
			Expect(memberCluster.Client().Create(ctx, serviceExport)).Should(Succeed())

			By("Updating internalMemberCluster spec to leave")
			setInternalMemberClusterState(ctx, memberClusterName, fleetv1beta1.ClusterStateLeave)

			By("Validating MultiClusterServiceAgent and ServiceExportImportAgent join status")
			checkIfMemberClusterHasLeft(ctx, memberClusterName)

			By("Validating serviceExport resources")
			serviceExportList := &fleetnetv1alpha1.ServiceExportList{}
			Expect(memberCluster.Client().List(ctx, serviceExportList)).Should(Succeed())
			Expect(len(serviceExportList.Items) == 0).Should(BeTrue(), "Failed to cleanup serviceExport resources")
		})

		It("Unjoin member cluster and should not create multiClusterService related resources", func() {
			By("Updating internalMemberCluster spec to leave")
			setInternalMemberClusterState(ctx, memberClusterName, fleetv1beta1.ClusterStateLeave)

			By("Validating MultiClusterServiceAgent and ServiceExportImportAgent join status")
			checkIfMemberClusterHasLeft(ctx, memberClusterName)

			By("Creating multiClusterService")
			mcs := framework.MultiClusterService(testNamespace, "my-mcs", serviceName)
			Expect(memberCluster.Client().Create(ctx, mcs)).Should(Succeed())

			By("Validating serviceImport resources in the member cluster")
			Consistently(func() error {
				serviceImportList := &fleetnetv1alpha1.ServiceImportList{}
				if err := memberCluster.Client().List(ctx, serviceImportList); err != nil {
					return err
				}
				if len(serviceImportList.Items) != 0 {
					return fmt.Errorf("serviceImport got %v, want 0", len(serviceImportList.Items))
				}
				return nil
			}, framework.ConsistentlyDuration, framework.ConsistentlyInterval).Should(Succeed(), "Failed to stop creating networking resources")
		})
	})
})
