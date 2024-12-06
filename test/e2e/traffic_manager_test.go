/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/test/common/trafficmanager/validator"
	"go.goms.io/fleet-networking/test/e2e/framework"
)

var _ = Describe("Test exporting service via Azure traffic manager", func() {
	var wm *framework.WorkloadManager
	var profile fleetnetv1alpha1.TrafficManagerProfile
	var hubClient client.Client
	//var dnsName string

	BeforeEach(func() {
		wm = framework.NewWorkloadManager(fleet)
		hubClient = wm.Fleet.HubCluster().Client()

		By("Deploying workload")
		Expect(wm.DeployWorkload(ctx)).Should(Succeed(), "Failed to deploy workloads")

		By("Creating trafficManagerProfile")
		profile = wm.TrafficManagerProfile()
		Expect(hubClient.Create(ctx, &profile)).Should(Succeed(), "Failed to creat the trafficManagerProfile")

		By("Validating the trafficManagerProfile status")
		validator.ValidateIfTrafficManagerProfileIsProgrammed(ctx, hubClient, types.NamespacedName{Namespace: profile.Namespace, Name: profile.Name})
	})

	AfterEach(func() {
		By("Removing workload")
		Expect(wm.RemoveWorkload(ctx)).Should(Succeed())

		By("Deleting trafficManagerProfile")
		Expect(hubClient.Delete(ctx, &profile)).Should(Succeed(), "Failed to delete the trafficManagerProfile")

		By("Validating trafficManagerProfile is deleted")
		validator.IsTrafficManagerProfileDeleted(ctx, hubClient, types.NamespacedName{Namespace: profile.Namespace, Name: profile.Name})
	})

	Context("Test invalid trafficManagerBackend (invalid serviceImport)", Ordered, func() {
		var backend fleetnetv1alpha1.TrafficManagerBackend
		var name types.NamespacedName
		BeforeAll(func() {
			By("Creating trafficManagerBackend")
			backend = wm.TrafficManagerBackend()
			name = types.NamespacedName{Namespace: backend.Namespace, Name: backend.Name}
			Expect(hubClient.Create(ctx, &backend)).Should(Succeed(), "Failed to create the trafficManagerBackend")
		})

		AfterAll(func() {
			By("Deleting trafficManagerBackend")
			Expect(hubClient.Delete(ctx, &backend)).Should(Succeed(), "Failed to delete the trafficManagerBackend")
			validator.IsTrafficManagerBackendDeleted(ctx, hubClient, name)
		})

		It("Validating the trafficManagerBackend status", func() {
			status := validator.ValidateTrafficManagerBackendIfAcceptedAndIgnoringEndpointName(ctx, hubClient, name, nil)
			validator.ValidateTrafficManagerBackendStatusAndIgnoringEndpointNameConsistently(ctx, hubClient, name, status)
		})
	})
})
