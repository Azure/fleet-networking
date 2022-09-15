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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/uniquename"
	"go.goms.io/fleet-networking/test/e2e/framework"
)

var _ = Describe("Test exporting service", Serial, Ordered, func() {
	var (
		ctx context.Context

		testNamespaceUnique string
		deployDef           *appsv1.Deployment
		svcDef              *corev1.Service
	)

	BeforeEach(func() {
		ctx = context.Background()

		By("Creating namespace")
		// Using unique namespace decouple tests especially considering we have test failure, and simply cleanup stage
		testNamespaceUnique = fmt.Sprintf("%s-%s", testNamespace, uniquename.RandomLowerCaseAlphabeticString(5))
		for _, m := range append(memberClusters, hubCluster) {
			ns := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: testNamespaceUnique,
				},
			}
			Expect(m.Client().Create(ctx, &ns)).Should(Succeed(), "Failed to create namespace %s cluster %s", testNamespaceUnique, m.Name())
		}

		By("Creating app deployment and service")
		for _, m := range memberClusters {
			appImage := appImage()
			podLabels := map[string]string{"app": "hello-world"}
			var replica int32 = 2
			deployDef = &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "hello-world",
					Namespace: testNamespaceUnique,
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: &replica,
					Selector: &metav1.LabelSelector{
						MatchLabels: podLabels,
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "hello-world",
							Labels: podLabels,
						},
						Spec: corev1.PodSpec{
							NodeSelector: map[string]string{"kubernetes.io/os": "linux"},
							Containers: []corev1.Container{{
								Name:  "python",
								Image: appImage,
								Env:   []corev1.EnvVar{{Name: "MEMBER_CLUSTER_ID", Value: m.Name()}},
							}},
						},
					},
				},
			}
			Expect(m.Client().Create(ctx, deployDef)).Should(Succeed(), "Failed to create app deployment %s in cluster %s", deployDef.Name, m.Name())

			svcDef = &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "hello-world-service",
					Namespace: testNamespaceUnique,
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
					Ports: []corev1.ServicePort{
						{
							Protocol:   corev1.ProtocolTCP,
							Port:       80,
							TargetPort: intstr.FromInt(8080),
						},
					},
					Selector: podLabels,
				},
			}
			Expect(m.Client().Create(ctx, svcDef)).Should(Succeed(), "Failed to create app service %s in cluster %s", svcDef.Name, m.Name())
		}
	})

	Context("Service should be exported successfully", func() {
		It("should distribute service requests to all members", func() {
			By("Creating service export")
			serviceExportDef := &fleetnetv1alpha1.ServiceExport{}
			serviceExporKey := types.NamespacedName{}
			for _, m := range memberClusters {
				serviceExportDef = &fleetnetv1alpha1.ServiceExport{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: testNamespaceUnique,
						Name:      svcDef.Name,
					},
				}
				serviceExporKey = types.NamespacedName{Namespace: testNamespaceUnique, Name: serviceExportDef.Name}

				Expect(m.Client().Create(ctx, serviceExportDef)).Should(Succeed(), "Failed to create service export %s in cluster %s", serviceExportDef.Name, m.Name())
				Eventually(func() error {
					if err := m.Client().Get(ctx, serviceExporKey, serviceExportDef); err != nil {
						return err
					}

					// Validating the exported service invalid and no conflict is detected
					validSvcExportCondition := meta.FindStatusCondition(serviceExportDef.Status.Conditions, string(fleetnetv1alpha1.ServiceExportValid))
					if validSvcExportCondition == nil {
						return fmt.Errorf("Service export condition %s is not present", fleetnetv1alpha1.ServiceExportValid)
					}
					if validSvcExportCondition.Status != metav1.ConditionTrue {
						return fmt.Errorf("Service export condition %s got %s, want %s", fleetnetv1alpha1.ServiceExportValid, validSvcExportCondition.Status, metav1.ConditionTrue)
					}
					conflictSvcExportCondition := meta.FindStatusCondition(serviceExportDef.Status.Conditions, string(fleetnetv1alpha1.ServiceExportConflict))
					if conflictSvcExportCondition == nil {
						return fmt.Errorf("Service export condition %s is not present", fleetnetv1alpha1.ServiceExportConflict)
					}
					if conflictSvcExportCondition.Status != metav1.ConditionFalse {
						return fmt.Errorf("Service export condition %s got %s, want %s", fleetnetv1alpha1.ServiceExportConflict, conflictSvcExportCondition.Status, metav1.ConditionFalse)
					}
					return nil
				}, framework.PollTimeout, framework.PollInterval).Should(BeNil(), "Failed to get expected service export conditions")
			}

			By("Creating multi-cluster service")
			var mcsLBAddr string
			mcsDef := &fleetnetv1alpha1.MultiClusterService{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testNamespaceUnique,
					Name:      svcDef.Name,
				},
				Spec: fleetnetv1alpha1.MultiClusterServiceSpec{
					ServiceImport: fleetnetv1alpha1.ServiceImportRef{
						Name: svcDef.Name,
					},
				},
			}
			// Deploy mcs in the member cluster #1.
			memberCluster := memberClusters[0]
			// NOTE(mainred): The default poll timeout is not always enough for mcs LB allocation and mcs request.
			// We can obtain the latency from the test log to refine the timeout.
			mcsPollTimeout := 40 * time.Second
			multiClusterSvcKey := types.NamespacedName{Namespace: testNamespaceUnique, Name: mcsDef.Name}
			Expect(memberCluster.Client().Create(ctx, mcsDef)).Should(Succeed(), "Failed to create multi-cluster service %s in cluster %s", serviceExportDef.Name, memberCluster.Name())
			Eventually(func() error {
				if err := memberCluster.Client().Get(ctx, multiClusterSvcKey, mcsDef); err != nil {
					return err
				}
				mcsValidCondition := meta.FindStatusCondition(mcsDef.Status.Conditions, string(fleetnetv1alpha1.MultiClusterServiceValid))
				if mcsValidCondition == nil {
					return fmt.Errorf("Multi-cluster service condition %s is not present", fleetnetv1alpha1.MultiClusterServiceValid)
				}
				if mcsValidCondition.Status != metav1.ConditionTrue {
					return fmt.Errorf("Multi-cluster service condition %s, got %s, want %s", fleetnetv1alpha1.MultiClusterServiceValid, mcsValidCondition.Status, metav1.ConditionTrue)
				}
				if len(mcsDef.Status.LoadBalancer.Ingress) != 1 {
					return fmt.Errorf("Multi-cluster service ingress address length, got %d, want %d", 0, 1)
				}

				mcsLBAddr = mcsDef.Status.LoadBalancer.Ingress[0].IP
				if mcsLBAddr == "" {
					return fmt.Errorf("Multi-cluster service load balancer IP, got empty")
				}
				return nil
			}, mcsPollTimeout, framework.PollInterval).Should(BeNil(), "Failed to retrieve multi-cluster service LB address")

			By("Validating multi-cluster service request distribution")
			requestURL := fmt.Sprintf("http://%s:%d", mcsLBAddr, svcDef.Spec.Ports[0].Port)
			clusterNames := make(map[string]struct{})
			for _, m := range memberClusters {
				clusterNames[m.Name()] = struct{}{}
			}
			Eventually(func() error {
				respBodyStr, err := fetchHTTPRequestBody(requestURL)
				if err != nil {
					return err
				}
				for clusterName := range clusterNames {
					if strings.Contains(respBodyStr, clusterName) {
						delete(clusterNames, clusterName)
					}
				}
				if len(clusterNames) == 0 {
					return nil
				}
				return fmt.Errorf("Member clusters not replied the request, got %v, want empty", clusterNames)
			}, mcsPollTimeout, framework.PollInterval).Should(BeNil(), "Failed to distribute mcs request to all member clusters")

			By("Unexporting service export")
			for _, m := range memberClusters {
				Expect(m.Client().Delete(ctx, serviceExportDef)).Should(Succeed(), "Failed to delete service export %s in cluster %s", serviceExportDef.Name, m.Name())
				Eventually(func() bool {
					return errors.IsNotFound(hubCluster.Client().Get(ctx, serviceExporKey, serviceExportDef))
				}, framework.PollTimeout, framework.PollInterval).Should(BeTrue(), "Failed to delete service export")
			}

			By("Validating request status and mcs LB ingress length")
			Eventually(func() error {
				_, err := fetchHTTPRequestBody(requestURL)
				if !os.IsTimeout(err) {
					return err
				}
				if err := memberCluster.Client().Get(ctx, multiClusterSvcKey, mcsDef); err != nil {
					return err
				}
				ingressLenth := len(mcsDef.Status.LoadBalancer.Ingress)
				if ingressLenth != 0 {
					return fmt.Errorf("Multi-cluster service ingress address length, got %d, want 0", ingressLenth)
				}
				return nil
			}, framework.PollTimeout, framework.PollInterval).Should(BeNil(), "Failed to validate unexported service")
		})
	})
})

// TODO(mainred): Before the app image is publicly available, we use the one built from e2e bootstrap.
// The app image construction must be aligned with the steps in test/scripts/bootstrap.sh.
func appImage() string {
	resourceGroupName := os.Getenv("AZURE_RESOURCE_GROUP")
	registryName := strings.ReplaceAll(resourceGroupName, "-", "")
	appImage := fmt.Sprintf("%s.azurecr.io/app", registryName)
	return appImage
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
	respBodyStr := string(respBody)
	return respBodyStr, nil
}
