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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/test/e2e/framework"
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
			By("Uneporting the service")
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
