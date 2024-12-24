/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package e2e

import (
	"fmt"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/trafficmanager/armtrafficmanager"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/controllers/hub/trafficmanagerprofile"
	"go.goms.io/fleet-networking/test/common/trafficmanager/validator"
	"go.goms.io/fleet-networking/test/e2e/framework"
)

var (
	enabled = os.Getenv("ENABLE_TRAFFIC_MANAGER") == "true"
)

var _ = Describe("Test exporting service via Azure traffic manager", func() {
	var wm *framework.WorkloadManager
	var profile fleetnetv1alpha1.TrafficManagerProfile
	var hubClient client.Client

	BeforeEach(func() {
		if !enabled {
			Skip("Skipping setting up when traffic manager is not enabled")
		}

		wm = framework.NewWorkloadManager(fleet)
		hubClient = wm.Fleet.HubCluster().Client()

		By("Deploying workload")
		Expect(wm.DeployWorkload(ctx)).Should(Succeed(), "Failed to deploy workloads")

		By("Creating trafficManagerProfile")
		profile = wm.TrafficManagerProfile()
		Expect(hubClient.Create(ctx, &profile)).Should(Succeed(), "Failed to creat the trafficManagerProfile")

		By("Validating the trafficManagerProfile status")
		wantDNSName := validator.ValidateIfTrafficManagerProfileIsProgrammed(ctx, hubClient, types.NamespacedName{Namespace: profile.Namespace, Name: profile.Name})

		By("Validating the Azure traffic manager profile")
		atmName := fmt.Sprintf(trafficmanagerprofile.AzureResourceProfileNameFormat, profile.UID)
		monitorConfig := profile.Spec.MonitorConfig
		namespacedName := types.NamespacedName{Name: profile.Name, Namespace: profile.Namespace}
		want := armtrafficmanager.Profile{
			Location: ptr.To("global"),
			Tags: map[string]*string{
				objectmeta.AzureTrafficManagerProfileTagKey: ptr.To(namespacedName.String()),
			},
			Properties: &armtrafficmanager.ProfileProperties{
				ProfileStatus:        ptr.To(armtrafficmanager.ProfileStatusEnabled),
				TrafficRoutingMethod: ptr.To(armtrafficmanager.TrafficRoutingMethodWeighted),
				DNSConfig: &armtrafficmanager.DNSConfig{
					RelativeName: ptr.To(fmt.Sprintf(trafficmanagerprofile.DNSRelativeNameFormat, profile.Namespace, profile.Name)),
					Fqdn:         &wantDNSName,
					TTL:          ptr.To(trafficmanagerprofile.DefaultDNSTTL),
				},
				MonitorConfig: &armtrafficmanager.MonitorConfig{
					IntervalInSeconds:         monitorConfig.IntervalInSeconds,
					Path:                      monitorConfig.Path,
					Port:                      monitorConfig.Port,
					Protocol:                  ptr.To(armtrafficmanager.MonitorProtocol(*monitorConfig.Protocol)),
					TimeoutInSeconds:          monitorConfig.TimeoutInSeconds,
					ToleratedNumberOfFailures: monitorConfig.ToleratedNumberOfFailures,
				},
				Endpoints:                   []*armtrafficmanager.Endpoint{},
				TrafficViewEnrollmentStatus: ptr.To(armtrafficmanager.TrafficViewEnrollmentStatusDisabled),
			},
		}
		atmValidator.ValidateProfile(ctx, atmName, want)
	})

	AfterEach(func() {
		if !enabled {
			Skip("Skipping deleting when traffic manager is not enabled")
		}

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
