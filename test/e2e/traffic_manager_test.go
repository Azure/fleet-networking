/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package e2e

import (
	"fmt"
	"os"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v4"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/trafficmanager/armtrafficmanager"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	fleetnetv1beta1 "go.goms.io/fleet-networking/api/v1beta1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/common/uniquename"
	"go.goms.io/fleet-networking/pkg/controllers/hub/trafficmanagerprofile"
	"go.goms.io/fleet-networking/test/common/trafficmanager/validator"
	"go.goms.io/fleet-networking/test/e2e/framework"
)

var (
	enabled = os.Getenv("ENABLE_TRAFFIC_MANAGER") == "true"
)

const (
	defaultTimeout = time.Second * 10
	// Expect the controller will call the Azure API and it can be finished within one minute.
	lightAzureOperationTimeout = 1 * time.Minute
	// Expect the controller will call multiple Azure APIs and it can be finished within 10 minutes.
	// For example, TrafficManagerBackend controller needs to wait until the ip address on the service is ready before setting
	// the condition.
	heavyAzureOperationTimeout = 10 * time.Minute
)

var _ = Describe("Test exporting service via Azure traffic manager", Ordered, func() {
	var wm *framework.WorkloadManager
	var profile fleetnetv1beta1.TrafficManagerProfile
	var profileName types.NamespacedName
	var hubClient client.Client
	var atmProfileName string
	var profileResourceID string
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
		profile = wm.TrafficManagerProfile(atmResourceGroup)
		Expect(hubClient.Create(ctx, &profile)).Should(Succeed(), "Failed to creat the trafficManagerProfile")

		By("Validating the trafficManagerProfile status")
		profileName = types.NamespacedName{Namespace: profile.Namespace, Name: profile.Name}
		atmProfileName = fmt.Sprintf(trafficmanagerprofile.AzureResourceProfileNameFormat, profile.UID)
		profileResourceID = fmt.Sprintf(azureTrafficManagerProfileResourceIDFormat, subscriptionID, atmResourceGroup, atmProfileName)
		profile = *validator.ValidateIfTrafficManagerProfileIsProgrammed(ctx, hubClient, profileName, true, profileResourceID, lightAzureOperationTimeout)

		By("Validating the Azure traffic manager profile")
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
		err := hubClient.Delete(ctx, &profile)
		Expect(err).Should(SatisfyAny(Succeed(), WithTransform(errors.IsNotFound, BeTrue())), "Failed to delete the trafficManagerProfile")

		By("Validating trafficManagerProfile is deleted")
		validator.IsTrafficManagerProfileDeleted(ctx, hubClient, profileName, lightAzureOperationTimeout)

		By("Validating Azure traffic manager profile")
		atmValidator.IsProfileDeleted(ctx, atmProfileName)
	})

	Context("Test invalid trafficManagerProfile", Ordered, func() {
		var invalidProfile fleetnetv1beta1.TrafficManagerProfile
		var invalidProfileName types.NamespacedName
		var backend fleetnetv1beta1.TrafficManagerBackend
		var backendName types.NamespacedName

		BeforeEach(func() {
			By("Creating trafficManagerProfile with invalid resource group")
			invalidProfile = wm.TrafficManagerProfile("invalid-resource-group")
			invalidProfile.Name = "invalid-profile-name"
			Expect(hubClient.Create(ctx, &invalidProfile)).Should(Succeed(), "Failed to create the invalid trafficManagerProfile")

			By("Validating the trafficManagerProfile status")
			invalidProfileName = types.NamespacedName{Namespace: invalidProfile.Namespace, Name: invalidProfile.Name}
			validator.ValidateIfTrafficManagerProfileIsProgrammed(ctx, hubClient, invalidProfileName, false, "", lightAzureOperationTimeout)
		})

		AfterEach(func() {
			By("Deleting trafficManagerProfile")
			err := hubClient.Delete(ctx, &invalidProfile)
			Expect(err).Should(SatisfyAny(Succeed(), WithTransform(errors.IsNotFound, BeTrue())), "Failed to delete the trafficManagerProfile")

			By("Validating trafficManagerProfile is deleted")
			validator.IsTrafficManagerProfileDeleted(ctx, hubClient, invalidProfileName, defaultTimeout)
		})

		It("Creating trafficManagerBackend with invalid profile", func() {
			By("Creating trafficManagerBackend")
			backend = wm.TrafficManagerBackend()
			// update the profile to invalid one
			backend.Spec.Profile = fleetnetv1beta1.TrafficManagerProfileRef{
				Name: invalidProfile.Name,
			}
			backendName = types.NamespacedName{Namespace: backend.Namespace, Name: backend.Name}
			Expect(hubClient.Create(ctx, &backend)).Should(Succeed(), "Failed to create the trafficManagerBackend")

			status := validator.ValidateTrafficManagerBackendIfAcceptedAndIgnoringEndpointName(ctx, hubClient, backendName, false, nil, defaultTimeout)
			validator.ValidateTrafficManagerBackendStatusAndIgnoringEndpointNameConsistently(ctx, hubClient, backendName, status)

			By("Deleting trafficManagerBackend")
			Expect(hubClient.Delete(ctx, &backend)).Should(Succeed(), "Failed to delete the trafficManagerBackend")
			validator.IsTrafficManagerBackendDeleted(ctx, hubClient, backendName, defaultTimeout)
		})
	})

	Context("Test updating trafficManagerProfile", Ordered, func() {
		var wantProfile armtrafficmanager.Profile

		AfterEach(func() {
			By("Updating the trafficManagerProfile spec")
			profile.Spec.MonitorConfig.ToleratedNumberOfFailures = ptr.To(int64(5))
			Expect(hubClient.Update(ctx, &profile)).Should(Succeed(), "Failed to update the trafficManagerProfile")

			validator.ValidateIfTrafficManagerProfileIsProgrammed(ctx, hubClient, profileName, true, profileResourceID, lightAzureOperationTimeout)

			By("Validating the Azure traffic manager profile")
			wantProfile.Properties.MonitorConfig.ToleratedNumberOfFailures = ptr.To(int64(5))
			atmValidator.ValidateProfile(ctx, atmProfileName, wantProfile)
		})

		It("Updating Azure traffic manager profile directly", func() {
			atmProfile.Properties.DNSConfig.TTL = ptr.To(int64(30))                                                          // should be reset
			atmProfile.Properties.TrafficRoutingMethod = ptr.To(armtrafficmanager.TrafficRoutingMethodGeographic)            // should be reset
			atmProfile.Properties.TrafficViewEnrollmentStatus = ptr.To(armtrafficmanager.TrafficViewEnrollmentStatusEnabled) // should keep
			statusRange := []*armtrafficmanager.MonitorConfigExpectedStatusCodeRangesItem{
				{
					Min: ptr.To[int32](200),
					Max: ptr.To[int32](299),
				},
				{
					Min: ptr.To[int32](300),
					Max: ptr.To[int32](399),
				},
			}
			atmProfile.Properties.MonitorConfig.ExpectedStatusCodeRanges = statusRange // should keep

			_, err := atmValidator.ProfileClient.CreateOrUpdate(ctx, atmValidator.ResourceGroup, atmProfileName, atmProfile, nil)
			Expect(err).Should(Succeed(), "Failed to update the Azure traffic manager profile")

			wantProfile = buildDesiredATMProfile(profile, nil)
			// The following fields will be kept.
			wantProfile.Properties.TrafficViewEnrollmentStatus = ptr.To(armtrafficmanager.TrafficViewEnrollmentStatusEnabled)
			wantProfile.Properties.MonitorConfig.ExpectedStatusCodeRanges = statusRange
		})

		It("Deleting Azure traffic manager profile directly", func() {
			wantProfile = buildDesiredATMProfile(profile, nil)
			_, err := atmValidator.ProfileClient.Delete(ctx, atmValidator.ResourceGroup, atmProfileName, nil)
			Expect(err).Should(Succeed(), "Failed to delete the Azure traffic manager profile")
		})
	})

	Context("Test invalid trafficManagerBackend (invalid serviceImport)", Ordered, func() {
		var backend fleetnetv1beta1.TrafficManagerBackend
		var name types.NamespacedName
		memberDNSLabels := make([]string, 2)

		BeforeAll(func() {
			By("Creating trafficManagerBackend")
			backend = wm.TrafficManagerBackend()
			name = types.NamespacedName{Namespace: backend.Namespace, Name: backend.Name}
			Expect(hubClient.Create(ctx, &backend)).Should(Succeed(), "Failed to create the trafficManagerBackend")
		})

		AfterAll(func() {
			By("Deleting trafficManagerBackend")
			Expect(hubClient.Delete(ctx, &backend)).Should(Succeed(), "Failed to delete the trafficManagerBackend")
			validator.IsTrafficManagerBackendDeleted(ctx, hubClient, name, lightAzureOperationTimeout)

			By("Validating the Azure traffic manager profile")
			atmProfileName = fmt.Sprintf(trafficmanagerprofile.AzureResourceProfileNameFormat, profile.UID)
			atmProfile = buildDesiredATMProfile(profile, nil)
			atmValidator.ValidateProfile(ctx, atmProfileName, atmProfile)
		})

		It("Validating the trafficManagerBackend status", func() {
			status := validator.ValidateTrafficManagerBackendIfAcceptedAndIgnoringEndpointName(ctx, hubClient, name, false, nil, defaultTimeout)
			validator.ValidateTrafficManagerBackendStatusAndIgnoringEndpointNameConsistently(ctx, hubClient, name, status)

			By("Exporting service with no DNS label assigned")
			Expect(wm.ExportService(ctx, wm.ServiceExport())).Should(Succeed(), "Failed to export the service")

			By("Validating the trafficManagerBackend status")
			status = validator.ValidateTrafficManagerBackendIfAcceptedAndIgnoringEndpointName(ctx, hubClient, name, false, nil, defaultTimeout)
			validator.ValidateTrafficManagerBackendStatusAndIgnoringEndpointNameConsistently(ctx, hubClient, name, status)

			By("Adding DNS label to the service on member-1")
			memberDNSLabels[0] = wm.BuildServiceDNSLabelName(memberClusters[0])
			Eventually(func() error {
				return wm.AddServiceDNSLabel(ctx, memberClusters[0], memberDNSLabels[0])
			}, defaultTimeout, framework.PollInterval).Should(Succeed(), "Failed to add DNS label to the service")

			By("Validating the trafficManagerBackend status")
			wantEndpoints := []fleetnetv1beta1.TrafficManagerEndpointStatus{
				{
					Weight: ptr.To(int64(100)),
					Target: ptr.To(fmt.Sprintf(azureDNSFormat, memberDNSLabels[0], clusterLocation)),
					From: &fleetnetv1beta1.FromCluster{
						ClusterStatus: fleetnetv1beta1.ClusterStatus{Cluster: memberClusters[0].Name()},
						Weight:        ptr.To(int64(1)),
					},
				},
			}
			status = validator.ValidateTrafficManagerBackendIfAcceptedAndIgnoringEndpointName(ctx, hubClient, name, false, wantEndpoints, heavyAzureOperationTimeout)
			validator.ValidateTrafficManagerBackendStatusAndIgnoringEndpointNameConsistently(ctx, hubClient, name, status)

			By("Validating the Azure traffic manager profile")
			atmProfile = buildDesiredATMProfile(profile, status.Endpoints)
			atmValidator.ValidateProfile(ctx, atmProfileName, atmProfile)

			By("Adding DNS label to the service on member-2")
			memberDNSLabels[1] = wm.BuildServiceDNSLabelName(memberClusters[1])
			Eventually(func() error {
				return wm.AddServiceDNSLabel(ctx, memberClusters[1], memberDNSLabels[1])
			}, defaultTimeout, framework.PollInterval).Should(Succeed(), "Failed to add DNS label to the service")

			By("Validating the trafficManagerBackend status")
			wantEndpoints = []fleetnetv1beta1.TrafficManagerEndpointStatus{
				{
					Weight: ptr.To(int64(50)),
					Target: ptr.To(fmt.Sprintf(azureDNSFormat, memberDNSLabels[0], clusterLocation)),
					From: &fleetnetv1beta1.FromCluster{
						ClusterStatus: fleetnetv1beta1.ClusterStatus{Cluster: memberClusters[0].Name()},
						Weight:        ptr.To(int64(1)),
					},
				},
				{
					Weight: ptr.To(int64(50)),
					Target: ptr.To(fmt.Sprintf(azureDNSFormat, memberDNSLabels[1], clusterLocation)),
					From: &fleetnetv1beta1.FromCluster{
						ClusterStatus: fleetnetv1beta1.ClusterStatus{Cluster: memberClusters[1].Name()},
						Weight:        ptr.To(int64(1)),
					},
				},
			}
			status = validator.ValidateTrafficManagerBackendIfAcceptedAndIgnoringEndpointName(ctx, hubClient, name, true, wantEndpoints, heavyAzureOperationTimeout)
			validator.ValidateTrafficManagerBackendStatusAndIgnoringEndpointNameConsistently(ctx, hubClient, name, status)

			By("Validating the Azure traffic manager profile")
			atmProfile = buildDesiredATMProfile(profile, status.Endpoints)
			atmValidator.ValidateProfile(ctx, atmProfileName, atmProfile)
		})
	})

	Context("Test invalid trafficManagerBackend (invalid profile)", Ordered, func() {
		var backend fleetnetv1beta1.TrafficManagerBackend
		var backendName types.NamespacedName
		memberDNSLabels := make([]string, 2)

		BeforeEach(func() {
			// create valid serviceImport
			By("Adding DNS label to the service on member-1 & member-2")
			for i := range memberClusters {
				memberDNSLabels[i] = wm.BuildServiceDNSLabelName(memberClusters[i])
				Eventually(func() error {
					return wm.AddServiceDNSLabel(ctx, memberClusters[i], memberDNSLabels[i])
				}, defaultTimeout, framework.PollInterval).Should(Succeed(), "Failed to add DNS label to the service")
			}

			By("Exporting service with DNS label assigned")
			Expect(wm.ExportService(ctx, wm.ServiceExport())).Should(Succeed(), "Failed to export the service")
		})

		AfterEach(func() {
			// make sure each test will create the trafficManagerBackend
			By("Deleting trafficManagerBackend")
			Expect(hubClient.Delete(ctx, &backend)).Should(Succeed(), "Failed to delete the trafficManagerBackend")
			validator.IsTrafficManagerBackendDeleted(ctx, hubClient, backendName, defaultTimeout)
		})

		It("Creating trafficManagerBackend with invalid profile", func() {
			By("Creating trafficManagerBackend")
			backend = wm.TrafficManagerBackend()
			// update the profile to invalid one
			backend.Spec.Profile = fleetnetv1beta1.TrafficManagerProfileRef{
				Name: "invalid-profile",
			}
			backendName = types.NamespacedName{Namespace: backend.Namespace, Name: backend.Name}
			Expect(hubClient.Create(ctx, &backend)).Should(Succeed(), "Failed to create the trafficManagerBackend")

			status := validator.ValidateTrafficManagerBackendIfAcceptedAndIgnoringEndpointName(ctx, hubClient, backendName, false, nil, defaultTimeout)
			validator.ValidateTrafficManagerBackendStatusAndIgnoringEndpointNameConsistently(ctx, hubClient, backendName, status)
		})

		It("Deleting trafficManagerProfile during runtime", func() {
			By("Creating trafficManagerBackend")
			backend = wm.TrafficManagerBackend()
			backendName = types.NamespacedName{Namespace: backend.Namespace, Name: backend.Name}
			Expect(hubClient.Create(ctx, &backend)).Should(Succeed(), "Failed to create the trafficManagerBackend")

			By("Validating the trafficManagerBackend status")
			wantEndpoints := []fleetnetv1beta1.TrafficManagerEndpointStatus{
				{
					Weight: ptr.To(int64(50)),
					Target: ptr.To(fmt.Sprintf(azureDNSFormat, memberDNSLabels[0], clusterLocation)),
					From: &fleetnetv1beta1.FromCluster{
						ClusterStatus: fleetnetv1beta1.ClusterStatus{Cluster: memberClusters[0].Name()},
						Weight:        ptr.To(int64(1)),
					},
				},
				{
					Weight: ptr.To(int64(50)),
					Target: ptr.To(fmt.Sprintf(azureDNSFormat, memberDNSLabels[1], clusterLocation)),
					From: &fleetnetv1beta1.FromCluster{
						ClusterStatus: fleetnetv1beta1.ClusterStatus{Cluster: memberClusters[1].Name()},
						Weight:        ptr.To(int64(1)),
					},
				},
			}
			status := validator.ValidateTrafficManagerBackendIfAcceptedAndIgnoringEndpointName(ctx, hubClient, backendName, true, wantEndpoints, heavyAzureOperationTimeout)
			validator.ValidateTrafficManagerBackendStatusAndIgnoringEndpointNameConsistently(ctx, hubClient, backendName, status)

			By("Deleting trafficManagerProfile")
			Expect(hubClient.Delete(ctx, &profile)).Should(Succeed(), "Failed to delete the trafficManagerProfile")

			By("Validating trafficManagerProfile is deleted")
			validator.IsTrafficManagerProfileDeleted(ctx, hubClient, profileName, lightAzureOperationTimeout)

			By("Validating the trafficManagerBackend status")
			status = validator.ValidateTrafficManagerBackendIfAcceptedAndIgnoringEndpointName(ctx, hubClient, backendName, false, nil, lightAzureOperationTimeout)
			validator.ValidateTrafficManagerBackendStatusAndIgnoringEndpointNameConsistently(ctx, hubClient, backendName, status)
		})
	})

	Context("Test valid trafficManagerBackend", Ordered, func() {
		var backend fleetnetv1beta1.TrafficManagerBackend
		var backendName types.NamespacedName
		memberDNSLabels := make([]string, 2)

		var extraTrafficManagerEndpoint *armtrafficmanager.Endpoint
		BeforeEach(func() {
			// create valid serviceImport
			By("Adding DNS label to the service on member-1 & member-2")
			for i := range memberClusters {
				memberDNSLabels[i] = wm.BuildServiceDNSLabelName(memberClusters[i])
				Eventually(func() error {
					return wm.AddServiceDNSLabel(ctx, memberClusters[i], memberDNSLabels[i])
				}, defaultTimeout, framework.PollInterval).Should(Succeed(), "Failed to add DNS label to the service")
				By(fmt.Sprintf("Added DNS label to the service on %s", memberClusters[i].Name()))
			}

			By("Exporting service with DNS label assigned")
			Expect(wm.ExportService(ctx, wm.ServiceExport())).Should(Succeed(), "Failed to export the service")

			By("Creating trafficManagerBackend")
			backend = wm.TrafficManagerBackend()
			backendName = types.NamespacedName{Namespace: backend.Namespace, Name: backend.Name}
			Expect(hubClient.Create(ctx, &backend)).Should(Succeed(), "Failed to create the trafficManagerBackend")

			By("Validating the trafficManagerBackend status")
			wantEndpoints := []fleetnetv1beta1.TrafficManagerEndpointStatus{
				{
					Weight: ptr.To(int64(50)),
					Target: ptr.To(fmt.Sprintf(azureDNSFormat, memberDNSLabels[0], clusterLocation)),
					From: &fleetnetv1beta1.FromCluster{
						ClusterStatus: fleetnetv1beta1.ClusterStatus{Cluster: memberClusters[0].Name()},
						Weight:        ptr.To(int64(1)),
					},
				},
				{
					Weight: ptr.To(int64(50)),
					Target: ptr.To(fmt.Sprintf(azureDNSFormat, memberDNSLabels[1], clusterLocation)),
					From: &fleetnetv1beta1.FromCluster{
						ClusterStatus: fleetnetv1beta1.ClusterStatus{Cluster: memberClusters[1].Name()},
						Weight:        ptr.To(int64(1)),
					},
				},
			}
			status := validator.ValidateTrafficManagerBackendIfAcceptedAndIgnoringEndpointName(ctx, hubClient, backendName, true, wantEndpoints, heavyAzureOperationTimeout)
			validator.ValidateTrafficManagerBackendStatusAndIgnoringEndpointNameConsistently(ctx, hubClient, backendName, status)

			By("Validating the Azure traffic manager profile")
			atmProfile = buildDesiredATMProfile(profile, status.Endpoints)
			atmProfile = *atmValidator.ValidateProfile(ctx, atmProfileName, atmProfile)

			// reset extra endpoint
			extraTrafficManagerEndpoint = nil
		})

		AfterEach(func() {
			By("Deleting trafficManagerBackend")
			Expect(hubClient.Delete(ctx, &backend)).Should(Succeed(), "Failed to delete the trafficManagerBackend")
			validator.IsTrafficManagerBackendDeleted(ctx, hubClient, backendName, lightAzureOperationTimeout)

			By("Validating the Azure traffic manager profile")
			atmProfileName = fmt.Sprintf(trafficmanagerprofile.AzureResourceProfileNameFormat, profile.UID)
			atmProfile = buildDesiredATMProfile(profile, nil)
			if extraTrafficManagerEndpoint != nil {
				atmProfile.Properties.Endpoints = append(atmProfile.Properties.Endpoints, extraTrafficManagerEndpoint)
			}
			atmValidator.ValidateProfile(ctx, atmProfileName, atmProfile)
		})

		It("Updating the trafficManagerProfile spec", func() {
			profile.Spec.MonitorConfig.ToleratedNumberOfFailures = ptr.To(int64(5))
			Expect(hubClient.Update(ctx, &profile)).Should(Succeed(), "Failed to update the trafficManagerProfile")

			validator.ValidateIfTrafficManagerProfileIsProgrammed(ctx, hubClient, profileName, true, profileResourceID, lightAzureOperationTimeout)

			By("Validating the Azure traffic manager profile")
			// All the fields excluding ToleratedNumberOfFailures should be unchanged.
			atmProfile.Properties.MonitorConfig.ToleratedNumberOfFailures = ptr.To(int64(5))
			atmValidator.ValidateProfile(ctx, atmProfileName, atmProfile)
		})

		It("Creating extra Azure traffic manager endpoint directly and then updating trafficManagerBackend", func() {
			By("Creating a public IP address")
			publicIPAddressName := fmt.Sprintf("e2e-test-public-ip-%s", uniquename.RandomLowerCaseAlphabeticString(5))
			publicIPReq := armnetwork.PublicIPAddress{
				Name:     ptr.To(publicIPAddressName),
				Location: ptr.To(clusterLocation),
				Properties: &armnetwork.PublicIPAddressPropertiesFormat{
					PublicIPAllocationMethod: ptr.To(armnetwork.IPAllocationMethodStatic),
					DNSSettings: &armnetwork.PublicIPAddressDNSSettings{
						DomainNameLabel: ptr.To(publicIPAddressName),
					},
				},
				SKU: &armnetwork.PublicIPAddressSKU{
					Name: ptr.To(armnetwork.PublicIPAddressSKUNameStandard),
				},
			}
			publicIPResp, err := pipClient.CreateOrUpdate(ctx, atmValidator.ResourceGroup, publicIPAddressName, publicIPReq)
			Expect(err).Should(Succeed(), "Failed to create public IP address")

			By("Creating new Azure traffic manager endpoint directly")
			atmEndpointReq := armtrafficmanager.Endpoint{
				Name: ptr.To("extra-endpoint"),
				Type: ptr.To("Microsoft.Network/trafficManagerProfiles/azureEndpoints"),
				Properties: &armtrafficmanager.EndpointProperties{
					TargetResourceID: publicIPResp.ID,
					EndpointStatus:   ptr.To(armtrafficmanager.EndpointStatusEnabled),
					Weight:           ptr.To(int64(10)),
				},
			}
			atmEndpointResp, err := atmValidator.EndpointClient.CreateOrUpdate(ctx, atmValidator.ResourceGroup, atmProfileName, armtrafficmanager.EndpointTypeAzureEndpoints, *atmEndpointReq.Name, atmEndpointReq, nil)
			Expect(err).Should(Succeed(), "Failed to create the extra traffic manager endpoint")
			extraTrafficManagerEndpoint = &atmEndpointResp.Endpoint

			By("Updating the trafficManagerBackend spec")
			Eventually(func() error {
				if err := hubClient.Get(ctx, backendName, &backend); err != nil {
					return err
				}
				backend.Spec.Weight = ptr.To(int64(10))
				return hubClient.Update(ctx, &backend)
			}, defaultTimeout, framework.PollInterval).Should(Succeed(), "Failed to update the trafficManagerBackend")

			By("Validating the trafficManagerBackend status")
			wantEndpoints := []fleetnetv1beta1.TrafficManagerEndpointStatus{
				{
					Weight: ptr.To(int64(5)),
					Target: ptr.To(fmt.Sprintf(azureDNSFormat, memberDNSLabels[0], clusterLocation)),
					From: &fleetnetv1beta1.FromCluster{
						ClusterStatus: fleetnetv1beta1.ClusterStatus{Cluster: memberClusters[0].Name()},
						Weight:        ptr.To(int64(1)),
					},
				},
				{
					Weight: ptr.To(int64(5)),
					Target: ptr.To(fmt.Sprintf(azureDNSFormat, memberDNSLabels[1], clusterLocation)),
					From: &fleetnetv1beta1.FromCluster{
						ClusterStatus: fleetnetv1beta1.ClusterStatus{Cluster: memberClusters[1].Name()},
						Weight:        ptr.To(int64(1)),
					},
				},
			}
			status := validator.ValidateTrafficManagerBackendIfAcceptedAndIgnoringEndpointName(ctx, hubClient, backendName, true, wantEndpoints, lightAzureOperationTimeout)
			validator.ValidateTrafficManagerBackendStatusAndIgnoringEndpointNameConsistently(ctx, hubClient, backendName, status)

			By("Validating the Azure traffic manager profile")
			atmProfile = buildDesiredATMProfile(profile, status.Endpoints)
			atmProfile.Properties.Endpoints = append(atmProfile.Properties.Endpoints, extraTrafficManagerEndpoint)
			atmValidator.ValidateProfile(ctx, atmProfileName, atmProfile)

			// The endpoint should be deleted when deleting the profile.
			By("Deleting the public ip address")
			Expect(pipClient.Delete(ctx, atmValidator.ResourceGroup, publicIPAddressName)).Should(Succeed(), "Failed to delete public IP address")
		})

		It("Updating the Azure traffic manager endpoint directly and then updating trafficManagerBackend", func() {
			By("Updating the Azure traffic manager endpoint")
			headers := []*armtrafficmanager.EndpointPropertiesCustomHeadersItem{
				{Name: ptr.To("header1"), Value: ptr.To("value1")},
			}
			atmProfile.Properties.Endpoints[0].Properties.Weight = ptr.To(int64(10)) // set the weight to 10 explicitly,
			// the controller should reset All the changes.
			for i := range atmProfile.Properties.Endpoints {
				atmProfile.Properties.Endpoints[i].Properties.EndpointStatus = ptr.To(armtrafficmanager.EndpointStatusDisabled)
				atmProfile.Properties.Endpoints[i].Properties.CustomHeaders = headers
			}
			_, err := atmValidator.ProfileClient.CreateOrUpdate(ctx, atmValidator.ResourceGroup, atmProfileName, atmProfile, nil)
			Expect(err).Should(Succeed(), "Failed to update the Azure traffic manager profile")

			By("Updating the trafficManagerBackend spec")
			Eventually(func() error {
				if err := hubClient.Get(ctx, backendName, &backend); err != nil {
					return err
				}
				backend.Spec.Weight = ptr.To(int64(10))
				return hubClient.Update(ctx, &backend)
			}, defaultTimeout, framework.PollInterval).Should(Succeed(), "Failed to update the trafficManagerBackend")

			By("Validating the trafficManagerBackend status")
			wantEndpoints := []fleetnetv1beta1.TrafficManagerEndpointStatus{
				{
					Weight: ptr.To(int64(5)),
					Target: ptr.To(fmt.Sprintf(azureDNSFormat, memberDNSLabels[0], clusterLocation)),
					From: &fleetnetv1beta1.FromCluster{
						ClusterStatus: fleetnetv1beta1.ClusterStatus{Cluster: memberClusters[0].Name()},
						Weight:        ptr.To(int64(1)),
					},
				},
				{
					Weight: ptr.To(int64(5)),
					Target: ptr.To(fmt.Sprintf(azureDNSFormat, memberDNSLabels[1], clusterLocation)),
					From: &fleetnetv1beta1.FromCluster{
						ClusterStatus: fleetnetv1beta1.ClusterStatus{Cluster: memberClusters[1].Name()},
						Weight:        ptr.To(int64(1)),
					},
				},
			}
			status := validator.ValidateTrafficManagerBackendIfAcceptedAndIgnoringEndpointName(ctx, hubClient, backendName, true, wantEndpoints, lightAzureOperationTimeout)
			validator.ValidateTrafficManagerBackendStatusAndIgnoringEndpointNameConsistently(ctx, hubClient, backendName, status)

			By("Validating the Azure traffic manager profile")
			atmProfile = buildDesiredATMProfile(profile, status.Endpoints)
			// The controller should reset all the endpoint changes.
			atmValidator.ValidateProfile(ctx, atmProfileName, atmProfile)
		})

		It("Deleting the Azure traffic manager endpoint directly and then updating trafficManagerBackend", func() {
			By("Deleting one of the Azure traffic manager endpoint")
			atmProfile.Properties.Endpoints = atmProfile.Properties.Endpoints[1:]
			_, err := atmValidator.ProfileClient.CreateOrUpdate(ctx, atmValidator.ResourceGroup, atmProfileName, atmProfile, nil)
			Expect(err).Should(Succeed(), "Failed to update the Azure traffic manager profile")

			By("Updating the trafficManagerBackend spec")
			Eventually(func() error {
				if err := hubClient.Get(ctx, backendName, &backend); err != nil {
					return err
				}
				backend.Spec.Weight = ptr.To(int64(10))
				return hubClient.Update(ctx, &backend)
			}, defaultTimeout, framework.PollInterval).Should(Succeed(), "Failed to update the trafficManagerBackend")

			By("Validating the trafficManagerBackend status")
			wantEndpoints := []fleetnetv1beta1.TrafficManagerEndpointStatus{
				{
					Weight: ptr.To(int64(5)),
					Target: ptr.To(fmt.Sprintf(azureDNSFormat, memberDNSLabels[0], clusterLocation)),
					From: &fleetnetv1beta1.FromCluster{
						ClusterStatus: fleetnetv1beta1.ClusterStatus{Cluster: memberClusters[0].Name()},
						Weight:        ptr.To(int64(1)),
					},
				},
				{
					Weight: ptr.To(int64(5)),
					Target: ptr.To(fmt.Sprintf(azureDNSFormat, memberDNSLabels[1], clusterLocation)),
					From: &fleetnetv1beta1.FromCluster{
						ClusterStatus: fleetnetv1beta1.ClusterStatus{Cluster: memberClusters[1].Name()},
						Weight:        ptr.To(int64(1)),
					},
				},
			}
			status := validator.ValidateTrafficManagerBackendIfAcceptedAndIgnoringEndpointName(ctx, hubClient, backendName, true, wantEndpoints, lightAzureOperationTimeout)
			validator.ValidateTrafficManagerBackendStatusAndIgnoringEndpointNameConsistently(ctx, hubClient, backendName, status)

			By("Validating the Azure traffic manager profile")
			atmProfile = buildDesiredATMProfile(profile, status.Endpoints)
			// The controller should reset all the endpoint changes.
			atmValidator.ValidateProfile(ctx, atmProfileName, atmProfile)
		})

		It("Updating the service type", func() {
			By("Updating the service type to clusterIP type in member-1")
			Eventually(func() error {
				return wm.UpdateServiceType(ctx, memberClusters[0], corev1.ServiceTypeClusterIP, false)
			}, framework.PollTimeout, framework.PollInterval).Should(Succeed(), "Failed to update the service type to clusterIP type")

			By("Validating the trafficManagerBackend status")
			wantEndpoints := []fleetnetv1beta1.TrafficManagerEndpointStatus{
				{
					Weight: ptr.To(int64(100)),
					Target: ptr.To(fmt.Sprintf(azureDNSFormat, memberDNSLabels[1], clusterLocation)),
					From: &fleetnetv1beta1.FromCluster{
						ClusterStatus: fleetnetv1beta1.ClusterStatus{Cluster: memberClusters[1].Name()},
						Weight:        ptr.To(int64(1)),
					},
				},
			}
			status := validator.ValidateTrafficManagerBackendIfAcceptedAndIgnoringEndpointName(ctx, hubClient, backendName, false, wantEndpoints, lightAzureOperationTimeout)
			validator.ValidateTrafficManagerBackendStatusAndIgnoringEndpointNameConsistently(ctx, hubClient, backendName, status)

			By("Validating the Azure traffic manager profile")
			atmProfile = buildDesiredATMProfile(profile, status.Endpoints)
			atmValidator.ValidateProfile(ctx, atmProfileName, atmProfile)

			By("Updating the service type to internal load balancer type in member-2")
			Eventually(func() error {
				return wm.UpdateServiceType(ctx, memberClusters[1], corev1.ServiceTypeLoadBalancer, true)
			}, defaultTimeout, framework.PollInterval).Should(Succeed(), "Failed to update the service type to internal load balancer type")

			By("Validating the trafficManagerBackend status")
			status = validator.ValidateTrafficManagerBackendIfAcceptedAndIgnoringEndpointName(ctx, hubClient, backendName, false, nil, lightAzureOperationTimeout)
			validator.ValidateTrafficManagerBackendStatusAndIgnoringEndpointNameConsistently(ctx, hubClient, backendName, status)

			By("Validating the Azure traffic manager profile")
			atmProfile = buildDesiredATMProfile(profile, status.Endpoints)
			atmValidator.ValidateProfile(ctx, atmProfileName, atmProfile)
		})

		It("Deleting serviceExports during runtime", func() {
			By("Deleting serviceExports")
			Expect(wm.UnexportService(ctx, wm.ServiceExport())).Should(Succeed(), "Failed to unexport the service")

			By("Validating the trafficManagerBackend status")
			status := validator.ValidateTrafficManagerBackendIfAcceptedAndIgnoringEndpointName(ctx, hubClient, backendName, false, nil, lightAzureOperationTimeout)
			validator.ValidateTrafficManagerBackendStatusAndIgnoringEndpointNameConsistently(ctx, hubClient, backendName, status)

			By("Validating the Azure traffic manager profile")
			atmProfile = buildDesiredATMProfile(profile, status.Endpoints)
			atmValidator.ValidateProfile(ctx, atmProfileName, atmProfile)
		})

		It("Updating the weight to 0", func() {
			By("Updating the trafficManagerBackend spec")
			Eventually(func() error {
				if err := hubClient.Get(ctx, backendName, &backend); err != nil {
					return err
				}
				backend.Spec.Weight = ptr.To(int64(0))
				return hubClient.Update(ctx, &backend)
			}, defaultTimeout, framework.PollInterval).Should(Succeed(), "Failed to update the trafficManagerBackend")

			By("Validating the trafficManagerBackend status")
			status := validator.ValidateTrafficManagerBackendIfAcceptedAndIgnoringEndpointName(ctx, hubClient, backendName, true, nil, lightAzureOperationTimeout)
			validator.ValidateTrafficManagerBackendStatusAndIgnoringEndpointNameConsistently(ctx, hubClient, backendName, status)

			By("Validating the Azure traffic manager profile")
			atmProfile = buildDesiredATMProfile(profile, status.Endpoints)
			atmValidator.ValidateProfile(ctx, atmProfileName, atmProfile)
		})

		It("Updating the serviceExport weight to 0", func() {
			By("Updating the serviceExport weight on member-1")
			Eventually(func() error {
				return wm.UpdateServiceExportWeight(ctx, memberClusters[0], 0)
			}, defaultTimeout, framework.PollInterval).Should(Succeed(), "Failed to add DNS label to the service")

			By("Validating the serviceExport condition")
			wantValidConditionWithZeroWeight := metav1.Condition{
				Type:    string(fleetnetv1alpha1.ServiceExportValid),
				Status:  metav1.ConditionTrue,
				Reason:  "ServiceIsValid",
				Message: fmt.Sprintf("exported service %s/%s with 0 weight", wm.ServiceExport().Namespace, wm.ServiceExport().Name),
			}
			By("Validating serviceExport valid condition on member-1")
			Eventually(func() error {
				return wm.ValidateServiceExportCondition(ctx, memberClusters[0], wantValidConditionWithZeroWeight)
			}, defaultTimeout, framework.PollInterval).Should(Succeed(), "Failed to validate the valid condition on serviceExport")

			By("Validating the trafficManagerBackend status")
			wantEndpoints := []fleetnetv1beta1.TrafficManagerEndpointStatus{
				{
					Weight: ptr.To(int64(100)),
					Target: ptr.To(fmt.Sprintf(azureDNSFormat, memberDNSLabels[1], clusterLocation)),
					From: &fleetnetv1beta1.FromCluster{
						ClusterStatus: fleetnetv1beta1.ClusterStatus{Cluster: memberClusters[1].Name()},
						Weight:        ptr.To(int64(1)),
					},
				},
			}
			status := validator.ValidateTrafficManagerBackendIfAcceptedAndIgnoringEndpointName(ctx, hubClient, backendName, true, wantEndpoints, lightAzureOperationTimeout)
			validator.ValidateTrafficManagerBackendStatusAndIgnoringEndpointNameConsistently(ctx, hubClient, backendName, status)

			By("Validating the Azure traffic manager profile")
			atmProfile = buildDesiredATMProfile(profile, status.Endpoints)
			atmValidator.ValidateProfile(ctx, atmProfileName, atmProfile)

			By("Updating the serviceExport weight on member-2")
			Eventually(func() error {
				return wm.UpdateServiceExportWeight(ctx, memberClusters[1], 0)
			}, defaultTimeout, framework.PollInterval).Should(Succeed(), "Failed to add DNS label to the service")

			By("Validating serviceExport valid condition on member-2")
			Eventually(func() error {
				return wm.ValidateServiceExportCondition(ctx, memberClusters[1], wantValidConditionWithZeroWeight)
			}, defaultTimeout, framework.PollInterval).Should(Succeed(), "Failed to validate the valid condition on serviceExport")

			By("Validating the trafficManagerBackend status")
			// the serviceImport is invalid in this case
			status = validator.ValidateTrafficManagerBackendIfAcceptedAndIgnoringEndpointName(ctx, hubClient, backendName, false, nil, lightAzureOperationTimeout)
			validator.ValidateTrafficManagerBackendStatusAndIgnoringEndpointNameConsistently(ctx, hubClient, backendName, status)

			By("Validating the Azure traffic manager profile")
			atmProfile = buildDesiredATMProfile(profile, status.Endpoints)
			atmValidator.ValidateProfile(ctx, atmProfileName, atmProfile)
		})
	})
})

func buildDesiredATMProfile(profile fleetnetv1beta1.TrafficManagerProfile, endpoints []fleetnetv1beta1.TrafficManagerEndpointStatus) armtrafficmanager.Profile {
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
			ID:   &e.ResourceID,
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
