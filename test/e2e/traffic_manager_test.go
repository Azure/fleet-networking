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
	var profileName types.NamespacedName
	var hubClient client.Client
	var atmProfileName string
	var atmProfile armtrafficmanager.Profile

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
		profileName = types.NamespacedName{Namespace: profile.Namespace, Name: profile.Name}
		profile = *validator.ValidateIfTrafficManagerProfileIsProgrammed(ctx, hubClient, profileName)

		By("Validating the Azure traffic manager profile")
		atmProfileName = fmt.Sprintf(trafficmanagerprofile.AzureResourceProfileNameFormat, profile.UID)
		atmProfile = buildDesiredATMProfile(profile, nil)
		atmValidator.ValidateProfile(ctx, atmProfileName, atmProfile)
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
		validator.IsTrafficManagerProfileDeleted(ctx, hubClient, profileName)

		By("Validating Azure traffic manager profile")
		atmValidator.IsProfileDeleted(ctx, atmProfileName)
	})

	Context("Test updating trafficManagerProfile", Ordered, func() {
		BeforeAll(func() {
			By("Updating Azure traffic manager profile")
			atmProfile.Properties.DNSConfig.TTL = ptr.To(int64(30))
			atmProfile.Properties.TrafficViewEnrollmentStatus = ptr.To(armtrafficmanager.TrafficViewEnrollmentStatusEnabled)
			_, err := atmValidator.ProfileClient.CreateOrUpdate(ctx, atmValidator.ResourceGroup, atmProfileName, atmProfile, nil)
			Expect(err).Should(Succeed(), "Failed to update the Azure traffic manager profile")

			By("Updating the trafficManagerProfile spec")
			profile.Spec.MonitorConfig.ToleratedNumberOfFailures = ptr.To(int64(5))
			Expect(hubClient.Update(ctx, &profile)).Should(Succeed(), "Failed to update the trafficManagerProfile")
		})

		It("Validating the trafficManagerProfile status", func() {
			validator.ValidateIfTrafficManagerProfileIsProgrammed(ctx, hubClient, profileName)

			By("Validating the Azure traffic manager profile")
			atmProfile = buildDesiredATMProfile(profile, nil)
			// Controller does not set the trafficViewEnrollmentStatus.
			atmProfile.Properties.TrafficViewEnrollmentStatus = ptr.To(armtrafficmanager.TrafficViewEnrollmentStatusEnabled)
			atmValidator.ValidateProfile(ctx, atmProfileName, atmProfile)
		})
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
			status := validator.ValidateTrafficManagerBackendIfAcceptedAndIgnoringEndpointName(ctx, hubClient, name, false, nil)
			validator.ValidateTrafficManagerBackendStatusAndIgnoringEndpointNameConsistently(ctx, hubClient, name, status)

			By("Exporting service with no DNS label assigned")
			Expect(wm.ExportService(ctx, wm.ServiceExport())).Should(Succeed())

			By("Validating the trafficManagerBackend status")
			status = validator.ValidateTrafficManagerBackendIfAcceptedAndIgnoringEndpointName(ctx, hubClient, name, false, nil)
			validator.ValidateTrafficManagerBackendStatusAndIgnoringEndpointNameConsistently(ctx, hubClient, name, status)

			By("Adding DNS label to the service on member-1")
			Eventually(func() error {
				return wm.AddServiceDNSLabel(ctx, memberClusters[0])
			}, framework.PollTimeout, framework.PollInterval).Should(Succeed(), "Failed to add DNS label to the service")

			By("Validating the trafficManagerBackend status")
			wantEndpoints := []fleetnetv1alpha1.TrafficManagerEndpointStatus{
				{
					Weight: ptr.To(int64(100)),
					Target: ptr.To(fmt.Sprintf(azureDNSFormat, wm.BuildServiceDNSLabelName(memberClusters[0]), clusterLocation)),
					From: &fleetnetv1alpha1.FromCluster{
						ClusterStatus: fleetnetv1alpha1.ClusterStatus{
							Cluster: memberClusterNames[0],
						},
					},
				},
			}
			status = validator.ValidateTrafficManagerBackendIfAcceptedAndIgnoringEndpointName(ctx, hubClient, name, false, wantEndpoints)
			validator.ValidateTrafficManagerBackendStatusAndIgnoringEndpointNameConsistently(ctx, hubClient, name, status)

			By("Validating the Azure traffic manager profile")
			atmProfile = buildDesiredATMProfile(profile, status.Endpoints)
			atmValidator.ValidateProfile(ctx, atmProfileName, atmProfile)

			By("Adding DNS label to the service on member-2")
			Eventually(func() error {
				return wm.AddServiceDNSLabel(ctx, memberClusters[1])
			}, framework.PollTimeout, framework.PollInterval).Should(Succeed(), "Failed to add DNS label to the service")

			By("Validating the trafficManagerBackend status")
			wantEndpoints = []fleetnetv1alpha1.TrafficManagerEndpointStatus{
				{
					Weight: ptr.To(int64(50)),
					Target: ptr.To(fmt.Sprintf(azureDNSFormat, wm.BuildServiceDNSLabelName(memberClusters[0]), clusterLocation)),
					From: &fleetnetv1alpha1.FromCluster{
						ClusterStatus: fleetnetv1alpha1.ClusterStatus{
							Cluster: memberClusterNames[0],
						},
					},
				},
				{
					Weight: ptr.To(int64(50)),
					Target: ptr.To(fmt.Sprintf(azureDNSFormat, wm.BuildServiceDNSLabelName(memberClusters[1]), clusterLocation)),
					From: &fleetnetv1alpha1.FromCluster{
						ClusterStatus: fleetnetv1alpha1.ClusterStatus{
							Cluster: memberClusterNames[0],
						},
					},
				},
			}
			status = validator.ValidateTrafficManagerBackendIfAcceptedAndIgnoringEndpointName(ctx, hubClient, name, true, wantEndpoints)
			validator.ValidateTrafficManagerBackendStatusAndIgnoringEndpointNameConsistently(ctx, hubClient, name, status)

			By("Validating the Azure traffic manager profile")
			atmProfile = buildDesiredATMProfile(profile, status.Endpoints)
			atmValidator.ValidateProfile(ctx, atmProfileName, atmProfile)
		})
	})
})

func buildDesiredATMProfile(profile fleetnetv1alpha1.TrafficManagerProfile, endpoints []fleetnetv1alpha1.TrafficManagerEndpointStatus) armtrafficmanager.Profile {
	monitorConfig := profile.Spec.MonitorConfig
	namespacedName := types.NamespacedName{Name: profile.Name, Namespace: profile.Namespace}
	res := armtrafficmanager.Profile{
		Location: ptr.To("global"),
		Tags: map[string]*string{
			objectmeta.AzureTrafficManagerProfileTagKey: ptr.To(namespacedName.String()),
		},
		Properties: &armtrafficmanager.ProfileProperties{
			ProfileStatus:        ptr.To(armtrafficmanager.ProfileStatusEnabled),
			TrafficRoutingMethod: ptr.To(armtrafficmanager.TrafficRoutingMethodWeighted),
			DNSConfig: &armtrafficmanager.DNSConfig{
				RelativeName: ptr.To(fmt.Sprintf(trafficmanagerprofile.DNSRelativeNameFormat, profile.Namespace, profile.Name)),
				Fqdn:         profile.Status.DNSName,
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
	for _, e := range endpoints {
		res.Properties.Endpoints = append(res.Properties.Endpoints, &armtrafficmanager.Endpoint{
			Name: ptr.To(e.Name),
			Type: ptr.To("Microsoft.Network/trafficManagerProfiles/azureEndpoints"),
			Properties: &armtrafficmanager.EndpointProperties{
				Target:         e.Target,
				Weight:         e.Weight,
				EndpointStatus: ptr.To(armtrafficmanager.EndpointStatusEnabled),
				AlwaysServe:    ptr.To(armtrafficmanager.AlwaysServeDisabled),
			},
		})
	}
	return res
}
