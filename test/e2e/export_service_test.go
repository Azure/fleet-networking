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
	"os"
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

var (
	svcExportConditionCmpOptions = []cmp.Option{
		cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "ObservedGeneration", "Message"),
		cmpopts.SortSlices(func(condition1, condition2 metav1.Condition) bool { return condition1.Type < condition2.Type }),
	}
	mcsConditionCmpOptions = []cmp.Option{
		cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "ObservedGeneration", "Message"),
		cmpopts.SortSlices(func(condition1, condition2 metav1.Condition) bool { return condition1.Type < condition2.Type }),
	}
)

var _ = Describe("Test exporting service", func() {
	var (
		ctx context.Context
		wm  *workloadManager

		// memberClusterMCS is picked from member cluster list to host LBs for multi-cluster service
		memberClusterMCS *framework.Cluster
	)

	BeforeEach(func() {
		ctx = context.Background()

		memberClusterMCS = memberClusters[0]

		wm = newWorkloadManager()
		wm.assertDeployWorkload()
	})

	AfterEach(func() {
		wm.assertRemoveWorkload()
	})

	Context("Service should be exported successfully", func() {
		BeforeEach(func() {
			wm.assertExportService()
		})
		AfterEach(func() {
			wm.assertUnexportService()
		})

		It("should distribute service requests to all members", func() {
			By("Fetching mcs Ingress IP address")
			var mcsLBAddr string
			mcsDef := wm.mcs
			// NOTE(mainred): The default poll timeout is not always enough for mcs LB allocation and mcs request.
			// We can obtain the latency from the test log to refine the timeout.
			multiClusterSvcKey := types.NamespacedName{Namespace: mcsDef.Namespace, Name: mcsDef.Name}
			mcsObj := &fleetnetv1alpha1.MultiClusterService{}
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
			}, framework.MCSPollTimeout, framework.PollInterval).Should(Succeed(), "Failed to retrieve multi-cluster service LB address")

			By("Validating service import in hub cluster")
			svcDef := wm.service
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
			}, framework.MCSPollTimeout, framework.PollInterval).Should(Succeed(), "Failed to distribute mcs request to all member clusters")

			By("Unexporting service")
			serviceExportDef := wm.serviceExport
			serviceExportObj := &fleetnetv1alpha1.ServiceExport{}
			serviceExporKey := types.NamespacedName{Namespace: serviceExportDef.Namespace, Name: serviceExportDef.Name}
			for _, m := range memberClusters {
				Expect(m.Client().Delete(ctx, &serviceExportDef)).Should(Succeed(), "Failed to delete service export %s in cluster %s", serviceExportDef.Name, m.Name())
				Eventually(func() bool {
					return errors.IsNotFound(m.Client().Get(ctx, serviceExporKey, serviceExportObj))
				}, framework.PollTimeout, framework.PollInterval).Should(BeTrue(), "Failed to delete service export")
			}

			By("Deleting multi-cluster service")
			Expect(memberClusterMCS.Client().Delete(ctx, &mcsDef)).Should(Succeed(), "Failed to delete multi-cluster service", mcsDef.Name)
			Eventually(func() bool {
				return errors.IsNotFound(memberClusterMCS.Client().Get(ctx, multiClusterSvcKey, mcsObj))
			}, framework.PollTimeout, framework.PollInterval).Should(BeTrue(), "Failed to delete multi-cluster service")
		})

		It("should allow exporting services with the same name but different namespaces", func() {
			// Each workloadmanager are initialized with resources with the same name but different namespaces.
			wmWithDifferentNS := newWorkloadManager()
			wmWithDifferentNS.assertDeployWorkload()
			wmWithDifferentNS.assertExportService()
			wmWithDifferentNS.assertUnexportService()
			wmWithDifferentNS.assertRemoveWorkload()
		})

		It("should allow exporting different services in the same namespace", func() {
			originalServiceName := wm.service.Name
			defer func() {
				wm.service.Name = originalServiceName
				wm.serviceExport.Name = originalServiceName
				wm.mcs.Name = originalServiceName
			}()
			newServiceName := fmt.Sprintf("%s-new", wm.service.Name)
			wm.service.Name = newServiceName

			By("Creating a new service sharing namespace with the existing service")
			for _, m := range memberClusters {
				serviceDef := wm.service
				Expect(m.Client().Create(ctx, &serviceDef)).Should(Succeed(), "Failed to create service %s in cluster %s", serviceDef.Name, m.Name())
			}
			wm.serviceExport.Name = newServiceName
			wm.mcs.Name = newServiceName
			wm.mcs.Spec.ServiceImport.Name = newServiceName
			wm.assertExportService()
			wm.assertUnexportService()
		})
	})

	Context("Service should be unexported successfully", func() {
		BeforeEach(func() {
			wm.assertExportService()
		})
		AfterEach(func() {
			wm.assertUnexportService()
		})

		It("should unexport service successfully", func() {
			By("Validating a service is exported successfully")
			var mcsLBAddr string
			mcsDef := wm.mcs
			multiClusterSvcKey := types.NamespacedName{Namespace: mcsDef.Namespace, Name: mcsDef.Name}
			mcsObj := &fleetnetv1alpha1.MultiClusterService{}
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
			}, framework.MCSPollTimeout, framework.PollInterval).Should(Succeed(), "Failed to retrieve multi-cluster service LB address")

			By("Unexporting the service")
			serviceExportDef := wm.serviceExport
			serviceExportObj := &fleetnetv1alpha1.ServiceExport{}
			serviceExporKey := types.NamespacedName{Namespace: serviceExportDef.Namespace, Name: serviceExportDef.Name}
			for _, m := range memberClusters {
				Expect(m.Client().Delete(ctx, &serviceExportDef)).Should(Succeed(), "Failed to delete service export %s in cluster %s", serviceExportDef.Name, m.Name())
				Eventually(func() bool {
					return errors.IsNotFound(m.Client().Get(ctx, serviceExporKey, serviceExportObj))
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
				return cmp.Diff(wantedMCSStatus, mcsObj.Status, mcsConditionCmpOptions...)
			}, framework.PollTimeout, framework.PollInterval).Should(BeEmpty(), "Validate multi-cluster service status mismatch (-want, +got):")
		})
	})

})

// TODO(mainred): Before the app image is publicly available, we use the one built from e2e bootstrap.
// The app image construction must be aligned with the steps in test/scripts/bootstrap.sh.
func appImage() string {
	resourceGroupName := os.Getenv("AZURE_RESOURCE_GROUP")
	registryName := strings.ReplaceAll(resourceGroupName, "-", "")
	return fmt.Sprintf("%s.azurecr.io/app", registryName)
}

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
