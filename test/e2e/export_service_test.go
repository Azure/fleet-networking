/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/test/e2e/framework"
)

const (
	fleetSystemNamespace = "fleet-system"
)

var _ = Describe("Test exporting service", func() {
	var (
		ctx context.Context

		wm *framework.WorkloadManager
	)

	BeforeEach(func() {
		ctx = context.Background()

		wm = framework.NewWorkloadManager(fleet)

		By("Deploying workload")
		Expect(wm.DeployWorkload(ctx)).Should(Succeed())
	})

	AfterEach(func() {
		By("Removing workload")
		Expect(wm.RemoveWorkload(ctx)).Should(Succeed())
	})

	Context("Service should be exported successfully", func() {
		BeforeEach(func() {
			By("Exporting service")
			Expect(wm.ExportService(ctx, wm.ServiceExport())).Should(Succeed())
		})
		AfterEach(func() {
			By("Unexporting service")
			Expect(wm.UnexportService(ctx, wm.ServiceExport())).Should(Succeed())
		})

		It("should distribute service requests to all members", func() {
			By("Creating multi-cluster service")
			Expect(wm.CreateMultiClusterService(ctx, wm.MultiClusterService()))

			By("Fetching mcs Ingress IP address")
			var mcsLBAddr string
			mcsDef := wm.MultiClusterService()
			// NOTE(mainred): The default poll timeout is not always enough for mcs LB allocation and mcs request.
			// We can obtain the latency from the test log to refine the timeout.
			multiClusterSvcKey := types.NamespacedName{Namespace: mcsDef.Namespace, Name: mcsDef.Name}
			mcsObj := &fleetnetv1alpha1.MultiClusterService{}
			Eventually(func() error {
				if err := wm.Fleet.MCSMemberCluster().Client().Get(ctx, multiClusterSvcKey, mcsObj); err != nil {
					return err
				}
				if len(mcsObj.Status.LoadBalancer.Ingress) != 1 {
					return fmt.Errorf("Multi-cluster service ingress address length, got %d, want %d", 0, 1)
				}
				mcsLBAddr = mcsObj.Status.LoadBalancer.Ingress[0].IP
				if mcsLBAddr == "" {
					return fmt.Errorf("Multi-cluster service load balancer IP, got empty, want not empty")
				}
				return nil
			}, framework.MCSLBPollTimeout, framework.PollInterval).Should(Succeed(), "Failed to retrieve multi-cluster service LB address")

			By("Validating service import in hub cluster")
			svcDef := wm.Service()
			svcImportKey := types.NamespacedName{Namespace: svcDef.Namespace, Name: svcDef.Name}
			svcImportObj := &fleetnetv1alpha1.ServiceImport{}
			err := hubCluster.Client().Get(ctx, svcImportKey, svcImportObj)
			Expect(err).Should(BeNil(), "Failed to get service import")
			wantedSvcImportStatus := fleetnetv1alpha1.ServiceImportStatus{
				Clusters: []fleetnetv1alpha1.ClusterStatus{
					{
						Cluster: memberClusters[0].Name(),
					},
					{
						Cluster: memberClusters[1].Name(),
					},
				},
				Type: fleetnetv1alpha1.ClusterSetIP,
				Ports: []fleetnetv1alpha1.ServicePort{
					{
						Port:       svcDef.Spec.Ports[0].Port,
						Protocol:   corev1.ProtocolTCP,
						TargetPort: svcDef.Spec.Ports[0].TargetPort,
					},
				},
			}
			Expect(cmp.Diff(wantedSvcImportStatus, svcImportObj.Status)).Should(BeEmpty(), "Validate service import status mismatch (-want, +got):")

			By("Validating multi-cluster service request distribution")
			requestURL := fmt.Sprintf("http://%s:%d", mcsLBAddr, svcDef.Spec.Ports[0].Port)
			lbDistributionPollTimeout := 180 * time.Second
			unrespondedClusters := make(map[string]struct{})
			for _, m := range memberClusters {
				unrespondedClusters[m.Name()] = struct{}{}
			}
			Eventually(func() error {
				respBodyStr, err := fetchHTTPRequestBody(requestURL)
				if err != nil {
					return err
				}
				for clusterName := range unrespondedClusters {
					if strings.Contains(respBodyStr, clusterName) {
						delete(unrespondedClusters, clusterName)
					}
				}
				if len(unrespondedClusters) == 0 {
					return nil
				}
				return fmt.Errorf("Member clusters not replied the request, got %v, want empty", unrespondedClusters)
			}, lbDistributionPollTimeout, framework.PollInterval).Should(Succeed(), "Failed to distribute mcs request to all member clusters")

			By("Unexporting service")
			Expect(wm.UnexportService(ctx, wm.ServiceExport())).Should(Succeed())

			By("Deleting multi-cluster service")
			Expect(wm.DeleteMultiClusterService(ctx, wm.MultiClusterService())).Should(Succeed())
		})

		It("should allow exporting services with the same name but different namespaces", func() {
			// Each workloadmanager are initialized with resources with the same name but different namespaces.
			wmWithDifferentNS := framework.NewWorkloadManager(wm.Fleet)
			Expect(wmWithDifferentNS.DeployWorkload(ctx)).Should(Succeed())
			serviceExportDef := wmWithDifferentNS.ServiceExport()
			Expect(wmWithDifferentNS.ExportService(ctx, serviceExportDef)).Should(Succeed())
			Expect(wmWithDifferentNS.UnexportService(ctx, serviceExportDef)).Should(Succeed())
			Expect(wmWithDifferentNS.RemoveWorkload(ctx)).Should(Succeed())
		})

		It("should allow exporting different services in the same namespace", func() {
			newSvcName := fmt.Sprintf("%s-new", wm.Service().Name)
			By("Creating a new service sharing namespace with the existing service")
			for _, m := range memberClusters {
				newSvcDef := wm.Service()
				newSvcDef.Name = newSvcName
				Expect(m.Client().Create(ctx, &newSvcDef)).Should(Succeed(), "Failed to create service %s in cluster %s", newSvcDef.Name, m.Name())
			}

			By("Exporting the service with a different name")
			newServiceExportDef := wm.ServiceExport()
			newServiceExportDef.Name = newSvcName
			Expect(wm.ExportService(ctx, newServiceExportDef)).Should(Succeed())

			By("Unexporting the service with a different name")
			Expect(wm.UnexportService(ctx, newServiceExportDef)).Should(Succeed())

			By("Deleting the service with a different name")
			for _, m := range memberClusters {
				newSvcDef := wm.Service()
				newSvcDef.Name = newSvcName
				Expect(m.Client().Delete(ctx, &newSvcDef)).Should(Succeed(), "Failed to delete service %s in cluster %s", newSvcDef.Name, m.Name())
			}
		})
	})

	Context("Service should be unexported successfully", func() {
		BeforeEach(func() {
			By("Exporting the service")
			Expect(wm.ExportService(ctx, wm.ServiceExport())).Should(Succeed())
		})
		AfterEach(func() {
			By("Unexporting the service")
			Expect(wm.UnexportService(ctx, wm.ServiceExport())).Should(Succeed())
		})

		It("should unexport service successfully", func() {
			By("Creating multi-cluster service")
			Expect(wm.CreateMultiClusterService(ctx, wm.MultiClusterService())).Should(Succeed())

			By("Validating a service is exported successfully")
			var mcsLBAddr string
			mcsDef := wm.MultiClusterService()
			multiClusterSvcKey := types.NamespacedName{Namespace: mcsDef.Namespace, Name: mcsDef.Name}
			mcsObj := &fleetnetv1alpha1.MultiClusterService{}
			memberClusterMCS := wm.Fleet.MCSMemberCluster()
			Eventually(func() error {
				if err := memberClusterMCS.Client().Get(ctx, multiClusterSvcKey, mcsObj); err != nil {
					return err
				}
				if len(mcsObj.Status.LoadBalancer.Ingress) != 1 {
					return fmt.Errorf("Multi-cluster service ingress address length, got %d, want %d", 0, 1)
				}
				mcsLBAddr = mcsObj.Status.LoadBalancer.Ingress[0].IP
				if mcsLBAddr == "" {
					return fmt.Errorf("Multi-cluster service load balancer IP, got empty, want not empty")
				}
				return nil
			}, framework.MCSLBPollTimeout, framework.PollInterval).Should(Succeed(), "Failed to retrieve multi-cluster service LB address")

			By("Unexporting the service")
			svcExportDef := wm.ServiceExport()
			svcExportObj := &fleetnetv1alpha1.ServiceExport{}
			svcExporKey := types.NamespacedName{Namespace: svcExportDef.Namespace, Name: svcExportDef.Name}
			for _, m := range memberClusters {
				Expect(m.Client().Delete(ctx, &svcExportDef)).Should(Succeed(), "Failed to delete service export %s in cluster %s", svcExportDef.Name, m.Name())
				Eventually(func() bool {
					return errors.IsNotFound(m.Client().Get(ctx, svcExporKey, svcExportObj))
				}, framework.PollTimeout, framework.PollInterval).Should(BeTrue(), "Failed to delete service export")
			}

			By("Validating multi-cluster service status after unexporting service")
			Eventually(func() string {
				if err := memberClusterMCS.Client().Get(ctx, multiClusterSvcKey, mcsObj); err != nil {
					return err.Error()
				}

				wantedMCSStatus := fleetnetv1alpha1.MultiClusterServiceStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(fleetnetv1alpha1.MultiClusterServiceValid),
							Reason: "UnknownServiceImport",
							Status: metav1.ConditionUnknown,
						},
					},
					LoadBalancer: corev1.LoadBalancerStatus{},
				}
				return cmp.Diff(wantedMCSStatus, mcsObj.Status, framework.MCSConditionCmpOptions...)
			}, framework.PollTimeout, framework.PollInterval).Should(BeEmpty(), "Validate multi-cluster service status mismatch (-want, +got):")

			By("Deleting multi-cluster service")
			Expect(wm.DeleteMultiClusterService(ctx, wm.MultiClusterService())).Should(Succeed())
		})
	})

	Context("Test multi-cluster service", func() {
		BeforeEach(func() {
			By("Exporting the service")
			Expect(wm.ExportService(ctx, wm.ServiceExport())).Should(Succeed())

			By("Creating multi-cluster service")
			Expect(wm.CreateMultiClusterService(ctx, wm.MultiClusterService())).Should(Succeed())
		})
		AfterEach(func() {
			By("Unexporting the service")
			Expect(wm.UnexportService(ctx, wm.ServiceExport())).Should(Succeed())

			By("Deleting multi-cluster service")
			Expect(wm.DeleteMultiClusterService(ctx, wm.MultiClusterService())).Should(Succeed())
		})

		validateMCSStatus := func(memberClusterMCS *framework.Cluster, mcs fleetnetv1alpha1.MultiClusterService, svc corev1.Service) {
			By("Validating the multi-cluster service is importing a service")
			multiClusterSvcKey := types.NamespacedName{Namespace: mcs.Namespace, Name: mcs.Name}
			mcsObj := &fleetnetv1alpha1.MultiClusterService{}
			Eventually(func() string {
				if err := memberClusterMCS.Client().Get(ctx, multiClusterSvcKey, mcsObj); err != nil {
					return err.Error()
				}
				wantedMCSCondition := []metav1.Condition{
					{
						Type:   string(fleetnetv1alpha1.MultiClusterServiceValid),
						Reason: "FoundServiceImport",
						Status: metav1.ConditionTrue,
					},
				}
				return cmp.Diff(wantedMCSCondition, mcsObj.Status.Conditions, framework.MCSConditionCmpOptions...)
			}, framework.PollTimeout, framework.PollInterval).Should(BeEmpty(), "Validate multi-cluster service condition mismatch (-want, +got):")

			By("Validating the multi-cluster service is taking the service spec")
			derivedServiceName := mcsObj.GetLabels()["networking.fleet.azure.com/derived-service"]
			Eventually(func() string {
				derivedServiceKey := types.NamespacedName{Namespace: fleetSystemNamespace, Name: derivedServiceName}
				derivedServiceObj := &corev1.Service{}
				Expect(memberClusterMCS.Client().Get(ctx, derivedServiceKey, derivedServiceObj)).Should(Succeed(), "Failed to get derived service")
				wantedDerivedSvcPortSpec := []corev1.ServicePort{
					{
						Port:       svc.Spec.Ports[0].Port,
						TargetPort: svc.Spec.Ports[0].TargetPort,
					},
				}
				derivedSvcPortSpecCmpOptions := []cmp.Option{
					cmpopts.IgnoreFields(corev1.ServicePort{}, "NodePort", "Protocol"),
				}
				return cmp.Diff(wantedDerivedSvcPortSpec, derivedServiceObj.Spec.Ports, derivedSvcPortSpecCmpOptions...)
			}, framework.PollTimeout, framework.PollInterval).Should(BeEmpty(), "Validate updated derived service (-want, +got):")

			By("Validating the multi-cluster service has loadbalancer ingress IP address")
			Eventually(func() error {
				if err := memberClusterMCS.Client().Get(ctx, multiClusterSvcKey, mcsObj); err != nil {
					return err
				}
				if len(mcsObj.Status.LoadBalancer.Ingress) != 1 {
					return fmt.Errorf("Multi-cluster service ingress address length, got %d, want %d", 0, 1)
				}
				if mcsObj.Status.LoadBalancer.Ingress[0].IP == "" {
					return fmt.Errorf("Multi-cluster service load balancer IP, got empty, want not empty")
				}
				return nil
			}, framework.MCSLBPollTimeout, framework.PollInterval).Should(Succeed(), "Failed to retrieve multi-cluster service LB address")
		}

		It("should allow multiple multi-cluster services to import different services", func() {
			By("Creating and exporting a new service")
			newSvcDef := wm.Service()
			newSvcDef.Name = fmt.Sprintf("%s-new", newSvcDef.Name)
			newSvcDef.Spec.Ports[0].Port = 8080
			memberClusterNewService := wm.Fleet.MemberClusters()[0]
			svcKey := types.NamespacedName{Namespace: newSvcDef.Namespace, Name: newSvcDef.Name}
			Expect(memberClusterNewService.Client().Create(ctx, &newSvcDef)).Should(Succeed(), "Failed to create service %s in cluster %s", svcKey, memberClusterNewService.Name())
			newSvcExportDef := wm.ServiceExport()
			newSvcExportDef.Name = fmt.Sprintf("%s-new", newSvcExportDef.Name)
			svcExportKey := types.NamespacedName{Namespace: newSvcExportDef.Namespace, Name: newSvcExportDef.Name}
			Expect(memberClusterNewService.Client().Create(ctx, &newSvcExportDef)).Should(Succeed(), "Failed to create service export %s in cluster %s", svcExportKey, memberClusterNewService.Name())

			memberClusterMCS := wm.Fleet.MCSMemberCluster()

			By("Creating a new multi-cluster service")
			newMCSDef := wm.MultiClusterService()
			newMCSDef.Name = fmt.Sprintf("%s-new", newMCSDef.Name)
			newMCSDef.Spec.ServiceImport.Name = newSvcDef.Name
			multiClusterSvcKey := types.NamespacedName{Namespace: newMCSDef.Namespace, Name: newMCSDef.Name}
			Expect(memberClusterMCS.Client().Create(ctx, &newMCSDef)).Should(Succeed(), "Failed to create multi-cluster service %s in cluster %s", multiClusterSvcKey, memberClusterMCS.Name())

			By("Validating the new multi-cluster service is importing the new service")
			validateMCSStatus(memberClusterMCS, newMCSDef, newSvcDef)

			// clean up
			By("Deleting the newly created multi-cluster service")
			Expect(memberClusterMCS.Client().Delete(ctx, &newMCSDef)).Should(Succeed(), "Failed to delete multi-cluster service %s in cluster %s", multiClusterSvcKey, memberClusterMCS.Name())

			By("Deleting the newly created service export")
			Expect(memberClusterNewService.Client().Delete(ctx, &newSvcExportDef)).Should(Succeed(), "Failed to delete service export %s in cluster %s", svcExportKey, memberClusterNewService.Name())

			By("Deleting the newly created service")
			Expect(memberClusterNewService.Client().Delete(ctx, &newSvcDef)).Should(Succeed(), "Failed to delete service %s in cluster %s", svcKey, memberClusterNewService.Name())
		})

		It("should allow a new multi-cluster service import the service after the original multi-cluster service is removed when member-cluster services are created in one member cluster", func() {
			memberClusterMCS := wm.Fleet.MCSMemberCluster()

			By("Creating a new multi-cluster service")
			mcsObj := &fleetnetv1alpha1.MultiClusterService{}
			newMCSDef := wm.MultiClusterService()
			newMCSDef.Name = fmt.Sprintf("%s-new", newMCSDef.Name)
			multiClusterSvcKey := types.NamespacedName{Namespace: newMCSDef.Namespace, Name: newMCSDef.Name}
			Expect(memberClusterMCS.Client().Create(ctx, &newMCSDef)).Should(Succeed(), "Failed to create multi-cluster service %s in cluster %s", multiClusterSvcKey, memberClusterMCS.Name())

			By("Validating the new multi-cluster service status is empty consistently")
			Consistently(func() string {
				if err := memberClusterMCS.Client().Get(ctx, multiClusterSvcKey, mcsObj); err != nil {
					return err.Error()
				}
				wantedMCSStatus := fleetnetv1alpha1.MultiClusterServiceStatus{}
				return cmp.Diff(wantedMCSStatus, mcsObj.Status)
			}, framework.PollTimeout, framework.PollInterval).Should(BeEmpty(), "Validate multi-cluster service condition mismatch (-want, +got):")

			By("Deleting the old multi-cluster service")
			oldMCSDef := wm.MultiClusterService()
			oldMCSKey := types.NamespacedName{Namespace: oldMCSDef.Namespace, Name: oldMCSDef.Name}
			Expect(memberClusterMCS.Client().Delete(ctx, &oldMCSDef)).Should(Succeed(), "Failed to delete multi-cluster service %s in cluster %s", oldMCSKey, memberClusterMCS.Name())

			By("Validating the new multi-cluster service is importing the service after the original multi-cluster service is removed")
			validateMCSStatus(memberClusterMCS, newMCSDef, wm.Service())

			// clean up
			By("Deleting the newly created multi-cluster service")
			Expect(memberClusterMCS.Client().Delete(ctx, &newMCSDef)).Should(Succeed(), "Failed to delete multi-cluster service %s in cluster %s", multiClusterSvcKey, memberClusterMCS.Name())
		})

		It("should allow a new multi-cluster service import the service after the original multi-cluster service is removed when member-cluster services are created in different member clusters", func() {
			memberClusterMCS := wm.Fleet.MCSMemberCluster()
			var memberClusterNonDefaultMCS *framework.Cluster
			for _, memberCluster := range wm.Fleet.MemberClusters() {
				if memberCluster.Name() != memberClusterMCS.Name() {
					memberClusterNonDefaultMCS = memberCluster
				}
			}

			By("Creating a new mult-cluster service in non-default MCS member")
			mcsObj := &fleetnetv1alpha1.MultiClusterService{}
			newMCSDef := wm.MultiClusterService()
			newMCSDef.Name = fmt.Sprintf("%s-new", newMCSDef.Name)
			multiClusterSvcKey := types.NamespacedName{Namespace: newMCSDef.Namespace, Name: newMCSDef.Name}
			Expect(memberClusterNonDefaultMCS.Client().Create(ctx, &newMCSDef)).Should(Succeed(), "Failed to create multi-cluster service %s in cluster %s", multiClusterSvcKey, memberClusterNonDefaultMCS.Name())
			By("Validating the new multi-cluster service status is unknown eventually")
			Eventually(func() string {
				if err := memberClusterNonDefaultMCS.Client().Get(ctx, multiClusterSvcKey, mcsObj); err != nil {
					return err.Error()
				}
				wantedMCSStatus := fleetnetv1alpha1.MultiClusterServiceStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(fleetnetv1alpha1.MultiClusterServiceValid),
							Reason: "UnknownServiceImport",
							Status: metav1.ConditionUnknown,
						},
					},
					LoadBalancer: corev1.LoadBalancerStatus{},
				}
				return cmp.Diff(wantedMCSStatus, mcsObj.Status, framework.MCSConditionCmpOptions...)
			}, framework.PollTimeout, framework.PollInterval).Should(BeEmpty(), "Validate multi-cluster service condition mismatch (-want, +got):")

			By("Deleting the old multi-cluster service")
			oldMCSDef := wm.MultiClusterService()
			oldMCSKey := types.NamespacedName{Namespace: oldMCSDef.Namespace, Name: oldMCSDef.Name}
			Expect(memberClusterMCS.Client().Delete(ctx, &oldMCSDef)).Should(Succeed(), "Failed to delete multi-cluster service %s in cluster %s", oldMCSKey, memberClusterMCS.Name())

			By("Validating the new multi-cluster service is importing the service after the original multi-cluster service is removed")
			validateMCSStatus(memberClusterNonDefaultMCS, newMCSDef, wm.Service())

			// clean up
			By("Deleting the newly created multi-cluster service")
			Expect(memberClusterNonDefaultMCS.Client().Delete(ctx, &newMCSDef)).Should(Succeed(), "Failed to delete multi-cluster service %s in cluster %s", multiClusterSvcKey, memberClusterMCS.Name())
		})

		It("should allow multi-cluster service to import a new service", func() {
			By("Creating a new service")
			memberClusterNewService := wm.Fleet.MemberClusters()[0]
			newServiceDef := wm.Service()
			newServiceDef.Name = fmt.Sprintf("%s-new", newServiceDef.Name)
			newServiceDef.Spec.Ports[0].Port = 8080
			svcKey := types.NamespacedName{Namespace: newServiceDef.Namespace, Name: newServiceDef.Name}
			Expect(memberClusterNewService.Client().Create(ctx, &newServiceDef)).Should(Succeed(), "Failed to create service %s in cluster %s", svcKey, memberClusterNewService.Name())

			By("Exporting the new service")
			newSvcExportDef := wm.ServiceExport()
			newSvcExportDef.Name = fmt.Sprintf("%s-new", newSvcExportDef.Name)
			svcExportKey := types.NamespacedName{Namespace: newSvcExportDef.Namespace, Name: newSvcExportDef.Name}
			Expect(memberClusterNewService.Client().Create(ctx, &newSvcExportDef)).Should(Succeed(), "Failed to create service export %s in cluster %s", svcExportKey, memberClusterNewService.Name())

			memberClusterMCS := wm.Fleet.MCSMemberCluster()

			By("Updating multi-cluster service to import the new service")
			mcsDef := wm.MultiClusterService()
			mcsObj := &fleetnetv1alpha1.MultiClusterService{}
			multiClusterSvcKey := types.NamespacedName{Namespace: mcsDef.Namespace, Name: mcsDef.Name}
			Expect(memberClusterMCS.Client().Get(ctx, multiClusterSvcKey, mcsObj)).Should(Succeed(), "Failed to get multi-cluster service %s in cluster %s", multiClusterSvcKey, memberClusterMCS.Name())
			mcsObj.Spec.ServiceImport.Name = newServiceDef.Name
			Expect(memberClusterMCS.Client().Update(ctx, mcsObj)).Should(Succeed(), "Failed to update multi-cluster service %s in cluster %s", multiClusterSvcKey, memberClusterMCS.Name())

			By("Validating the multi-cluster service is updated to import the new service")
			validateMCSStatus(memberClusterMCS, mcsDef, newServiceDef)

			// clean up
			By("Deleting the newly created service")
			Expect(memberClusterNewService.Client().Delete(ctx, &newServiceDef)).Should(Succeed(), "Failed to delete multi-cluster service %s in cluster %s", svcKey, memberClusterNewService.Name())

			By("Deleting the newly created service export")
			Expect(memberClusterNewService.Client().Delete(ctx, &newSvcExportDef)).Should(Succeed(), "Failed to delete service export %s in cluster %s", svcExportKey, memberClusterNewService.Name())
		})
		It("should allow a multi-cluster service to import a service created later than multi-cluster service", func() {
			memberClusterMCS := wm.Fleet.MCSMemberCluster()

			By("Creating a multi-cluster service before the service is created")
			newSvcDef := wm.Service()
			newSvcDef.Name = fmt.Sprintf("%s-new", newSvcDef.Name)
			newSvcDef.Spec.Ports[0].Port = 8080
			newMCSDef := wm.MultiClusterService()
			newMCSDef.Name = fmt.Sprintf("%s-new", newMCSDef.Name)
			newMCSDef.Spec.ServiceImport.Name = newSvcDef.Name
			multiClusterSvcKey := types.NamespacedName{Namespace: newMCSDef.Namespace, Name: newMCSDef.Name}
			Expect(memberClusterMCS.Client().Create(ctx, &newMCSDef)).Should(Succeed(), "Failed to create multi-cluster service %s in cluster %s", multiClusterSvcKey, memberClusterMCS.Name())

			By("Validating the multi-cluster service is importing no service")
			mcsObj := &fleetnetv1alpha1.MultiClusterService{}
			Eventually(func() string {
				if err := memberClusterMCS.Client().Get(ctx, multiClusterSvcKey, mcsObj); err != nil {
					return err.Error()
				}
				wantedMCSStatus := fleetnetv1alpha1.MultiClusterServiceStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(fleetnetv1alpha1.MultiClusterServiceValid),
							Reason: "UnknownServiceImport",
							Status: metav1.ConditionUnknown,
						},
					},
					LoadBalancer: corev1.LoadBalancerStatus{},
				}
				return cmp.Diff(wantedMCSStatus, mcsObj.Status, framework.MCSConditionCmpOptions...)
			}, framework.PollTimeout, framework.PollInterval).Should(BeEmpty(), "Validate multi-cluster service status mismatch (-want, +got):")

			By("Creating and exporting a new service")
			memberClusterNewService := wm.Fleet.MemberClusters()[0]
			svcKey := types.NamespacedName{Namespace: newSvcDef.Namespace, Name: newSvcDef.Name}
			Expect(memberClusterNewService.Client().Create(ctx, &newSvcDef)).Should(Succeed(), "Failed to create service %s in cluster %s", svcKey, memberClusterNewService.Name())
			newSvcExportDef := wm.ServiceExport()
			newSvcExportDef.Name = fmt.Sprintf("%s-new", newSvcExportDef.Name)
			svcExportKey := types.NamespacedName{Namespace: newSvcExportDef.Namespace, Name: newSvcExportDef.Name}
			Expect(memberClusterNewService.Client().Create(ctx, &newSvcExportDef)).Should(Succeed(), "Failed to create service export %s in cluster %s", svcExportKey, memberClusterNewService.Name())

			By("Validating the multi-cluster service is importing the service created later than multi-cluster service")
			validateMCSStatus(memberClusterMCS, newMCSDef, newSvcDef)

			// clean up
			By("Deleting the newly created multi-cluster service")
			Expect(memberClusterMCS.Client().Delete(ctx, &newMCSDef)).Should(Succeed(), "Failed to delete multi-cluster service %s in cluster %s", multiClusterSvcKey, memberClusterMCS.Name())

			By("Deleting the newly created service")
			Expect(memberClusterNewService.Client().Delete(ctx, &newSvcDef)).Should(Succeed(), "Failed to delete multi-cluster service %s in cluster %s", svcKey, memberClusterNewService.Name())

			By("Deleting the newly created service export")
			Expect(memberClusterNewService.Client().Delete(ctx, &newSvcExportDef)).Should(Succeed(), "Failed to delete service export %s in cluster %s", svcExportKey, memberClusterNewService.Name())
		})

		It("should allow other multi-cluster services to import the service after the multi-cluster service importing the service is deleted", func() {
			memberClusterMCS := wm.Fleet.MCSMemberCluster()

			By("Deleting the existing multi-cluster service importing the service")
			mcsSDef := wm.MultiClusterService()
			Expect(memberClusterMCS.Client().Delete(ctx, &mcsSDef)).Should(Succeed(), "Failed to delete multi-cluster service %s in cluster %s", mcsSDef.Name, memberClusterMCS.Name())

			By("Creating a new multi-cluster service")
			newMCSDef := wm.MultiClusterService()
			newMCSDef.Name = fmt.Sprintf("%s-new", newMCSDef.Name)
			multiClusterSvcKey := types.NamespacedName{Namespace: newMCSDef.Namespace, Name: newMCSDef.Name}
			Expect(memberClusterMCS.Client().Create(ctx, &newMCSDef)).Should(Succeed(), "Failed to create multi-cluster service %s in cluster %s", multiClusterSvcKey, memberClusterMCS.Name())

			By("Validating the service can be re-imported by another multi-cluster service")
			validateMCSStatus(memberClusterMCS, newMCSDef, wm.Service())

			// clean up
			By("Deleting the newly created multi-cluster service")
			Expect(memberClusterMCS.Client().Delete(ctx, &newMCSDef)).Should(Succeed(), "Failed to delete multi-cluster service %s in cluster %s", multiClusterSvcKey, memberClusterMCS.Name())
		})
	})

	Context("Test creating service export", func() {
		It("should reject one of the exporting service when exporting services with the same name and namespace but different specs", func() {
			By("Exporting a service in member cluster one")
			memberClusterOne := wm.Fleet.MemberClusters()[0]
			svcExportDef := wm.ServiceExport()
			svcExportKey := types.NamespacedName{Namespace: svcExportDef.Namespace, Name: svcExportDef.Name}
			Expect(memberClusterOne.Client().Create(ctx, &svcExportDef)).Should(Succeed(), "Failed to create service export %s in cluster %s", svcExportKey, memberClusterOne.Name())
			svcExportObj := &fleetnetv1alpha1.ServiceExport{}
			Eventually(func() string {
				if err := memberClusterOne.Client().Get(ctx, svcExportKey, svcExportObj); err != nil {
					return err.Error()
				}
				wantedSvcExportConditions := []metav1.Condition{
					{
						Type:   string(fleetnetv1alpha1.ServiceExportValid),
						Reason: "ServiceIsValid",
						Status: metav1.ConditionTrue,
					},
					{
						Type:   string(fleetnetv1alpha1.ServiceExportConflict),
						Reason: "NoConflictFound",
						Status: metav1.ConditionFalse,
					},
				}
				return cmp.Diff(wantedSvcExportConditions, svcExportObj.Status.Conditions, framework.SvcExportConditionCmpOptions...)
			}, framework.PollTimeout, framework.PollInterval).Should(BeEmpty(), "Validate service export condition mismatch (-want, +got):")

			By("Updating the service with different spec in member cluster two")
			memberClusterTwo := wm.Fleet.MemberClusters()[1]
			svcDef := wm.Service()
			svcDef.Spec.Ports[0].Port++
			svcKey := types.NamespacedName{Namespace: svcExportDef.Namespace, Name: svcExportDef.Name}
			Expect(memberClusterTwo.Client().Update(ctx, &svcDef)).Should(Succeed(), "Failed to update service %s in cluster %s", svcKey, memberClusterTwo.Name())

			By("Exporting the service in member cluster two")
			svcExportDef = wm.ServiceExport()
			Expect(memberClusterTwo.Client().Create(ctx, &svcExportDef)).Should(Succeed(), "Failed to create service export %s in cluster %s", svcExportKey, memberClusterTwo.Name())

			By("Validating exporting service in cluster two has conflict")
			Eventually(func() string {
				if err := memberClusterTwo.Client().Get(ctx, svcExportKey, svcExportObj); err != nil {
					return err.Error()
				}
				wantedSvcExportConditions := []metav1.Condition{
					{
						Type:   string(fleetnetv1alpha1.ServiceExportValid),
						Reason: "ServiceIsValid",
						Status: metav1.ConditionTrue,
					},
					{
						Type:   string(fleetnetv1alpha1.ServiceExportConflict),
						Reason: "ConflictFound",
						Status: metav1.ConditionTrue,
					},
				}
				return cmp.Diff(wantedSvcExportConditions, svcExportObj.Status.Conditions, framework.SvcExportConditionCmpOptions...)
			}, framework.PollTimeout, framework.PollInterval).Should(BeEmpty(), "Validate service export condition mismatch (-want, +got):")

			// clean up
			By("Unexporting the service in member cluster one")
			Expect(memberClusterOne.Client().Delete(ctx, &svcExportDef)).Should(Succeed(), "Failed to delete service export %s in cluster %s", svcExportKey, memberClusterOne.Name())

			By("Deleting service export in member cluster two")
			Expect(memberClusterTwo.Client().Delete(ctx, &svcExportDef)).Should(Succeed(), "Failed to delete service export %s in cluster %s", svcExportKey, memberClusterTwo.Name())
		})

		It("should reject exporting a headless service", func() {
			By("Creating a headless service")
			memberCluster := wm.Fleet.MemberClusters()[0]
			svcDef := wm.Service()
			svcDef.Name = fmt.Sprintf("%s-headless", svcDef.Name)
			svcDef.Spec.Type = corev1.ServiceTypeClusterIP
			svcDef.Spec.ClusterIP = "None"
			svcKey := types.NamespacedName{Namespace: svcDef.Namespace, Name: svcDef.Name}
			Expect(memberCluster.Client().Create(ctx, &svcDef)).Should(Succeed(), "Failed to update service %s in cluster %s", svcKey, memberCluster.Name())

			By("Exporting the headless service")
			svcExportDef := wm.ServiceExport()
			svcExportDef.Name = svcDef.Name
			svcExportKey := types.NamespacedName{Namespace: svcExportDef.Namespace, Name: svcExportDef.Name}
			Expect(memberCluster.Client().Create(ctx, &svcExportDef)).Should(Succeed(), "Failed to create service export %s in cluster %s", svcExportKey, memberCluster.Name())

			By("Validating exporting the headless service should be ineligible")
			svcExportObj := &fleetnetv1alpha1.ServiceExport{}
			Eventually(func() string {
				if err := memberCluster.Client().Get(ctx, svcExportKey, svcExportObj); err != nil {
					return err.Error()
				}
				wantedSvcExportConditions := []metav1.Condition{
					{
						Type:   string(fleetnetv1alpha1.ServiceExportValid),
						Reason: "ServiceIneligible",
						Status: metav1.ConditionFalse,
					},
				}
				return cmp.Diff(wantedSvcExportConditions, svcExportObj.Status.Conditions, framework.SvcExportConditionCmpOptions...)
			}, framework.PollTimeout, framework.PollInterval).Should(BeEmpty(), "Validate service export condition mismatch (-want, +got):")

			// clean up
			By("Unexporting the service")
			Expect(memberCluster.Client().Delete(ctx, &svcExportDef)).Should(Succeed(), "Failed to delete service export %s in cluster %s", svcExportKey, memberCluster.Name())

			By("Deleting the service")
			Expect(memberCluster.Client().Delete(ctx, &svcDef)).Should(Succeed(), "Failed to delete service %s in cluster %s", svcKey, memberCluster.Name())
		})

		It("should reject exporting a service of type ExternalName", func() {
			By("Updating the type of service to ExternalName")
			memberCluster := wm.Fleet.MemberClusters()[0]
			svcDef := wm.Service()
			svcDef.Spec.Type = corev1.ServiceTypeExternalName
			svcDef.Spec.ExternalName = "e2e.fleet-networking.com"
			svcKey := types.NamespacedName{Namespace: svcDef.Namespace, Name: svcDef.Name}
			Expect(memberCluster.Client().Update(ctx, &svcDef)).Should(Succeed(), "Failed to update service %s in cluster %s", svcKey, memberCluster.Name())

			By("Exporting the service of type ExternalName")
			svcExportDef := wm.ServiceExport()
			svcExportKey := types.NamespacedName{Namespace: svcExportDef.Namespace, Name: svcExportDef.Name}
			Expect(memberCluster.Client().Create(ctx, &svcExportDef)).Should(Succeed(), "Failed to create service export %s in cluster %s", svcExportKey, memberCluster.Name())

			By("Validating exporting service of type ExternalName should be ineligible")
			svcExportObj := &fleetnetv1alpha1.ServiceExport{}
			Eventually(func() string {
				if err := memberCluster.Client().Get(ctx, svcExportKey, svcExportObj); err != nil {
					return err.Error()
				}
				wantedSvcExportConditions := []metav1.Condition{
					{
						Type:   string(fleetnetv1alpha1.ServiceExportValid),
						Reason: "ServiceIneligible",
						Status: metav1.ConditionFalse,
					},
				}
				return cmp.Diff(wantedSvcExportConditions, svcExportObj.Status.Conditions, framework.SvcExportConditionCmpOptions...)
			}, framework.PollTimeout, framework.PollInterval).Should(BeEmpty(), "Validate service export condition mismatch (-want, +got):")

			// clean up
			By("Unexporting the service export")
			Expect(memberCluster.Client().Delete(ctx, &svcExportDef)).Should(Succeed(), "Failed to delete service export %s in cluster %s", svcExportKey, memberCluster.Name())
		})
	})
})

func fetchHTTPRequestBody(requestURL string) (string, error) {
	client := &http.Client{
		Timeout: time.Second * 5,
	}
	res, err := client.Get(requestURL)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	respBody, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return "", err
	}
	return string(respBody), nil
}
