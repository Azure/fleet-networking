/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"context"
	"fmt"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2" // nolint:revive
	. "github.com/onsi/gomega"    // nolint:revive
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/uniquename"
	"go.goms.io/fleet-networking/test/e2e/framework"
)

// workloadManager represents a suite of variables of operations required to test exporting an service and more.
type workloadManager struct {
	namespace          string
	service            corev1.Service
	deploymentTemplate appsv1.Deployment
	serviceExport      fleetnetv1alpha1.ServiceExport
	mcs                fleetnetv1alpha1.MultiClusterService
}

func newWorkloadManager() *workloadManager {
	// Using unique namespace decouple tests, especially considering we have test failure, and simply cleanup stage.
	namespaceUnique := fmt.Sprintf("%s-%s", testNamespace, uniquename.RandomLowerCaseAlphabeticString(5))

	appImage := appImage()
	podLabels := map[string]string{"app": "hello-world"}
	var replica int32 = 2
	// NOTE(mainred): resourceDef vs resourceObj
	// resourceDef carries the definition of the resource to create/update/delete the resource, while resourceObj holds the
	// whole information of this resource, and is normally from getting the resource.
	deploymentTemplateDef := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "hello-world",
			Namespace: namespaceUnique,
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
						Env:   []corev1.EnvVar{{Name: "MEMBER_CLUSTER_ID", Value: ""}},
					}},
				},
			},
		},
	}

	svcDef := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "hello-world-service",
			Namespace: namespaceUnique,
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

	serviceExportDef := fleetnetv1alpha1.ServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespaceUnique,
			Name:      svcDef.Name,
		},
	}

	mcsDef := fleetnetv1alpha1.MultiClusterService{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespaceUnique,
			Name:      svcDef.Name,
		},
		Spec: fleetnetv1alpha1.MultiClusterServiceSpec{
			ServiceImport: fleetnetv1alpha1.ServiceImportRef{
				Name: svcDef.Name,
			},
		},
	}

	return &workloadManager{
		namespace:          namespaceUnique,
		service:            svcDef,
		deploymentTemplate: deploymentTemplateDef,
		serviceExport:      serviceExportDef,
		mcs:                mcsDef,
	}
}

// deployment returns an deployment definition base on the cluster name.
func (wm *workloadManager) deployment(clusterName string) *appsv1.Deployment {
	deploymentTemplate := wm.deploymentTemplate
	deploymentTemplate.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{{Name: "MEMBER_CLUSTER_ID", Value: clusterName}}
	return &deploymentTemplate
}

func (wm *workloadManager) assertDeployWorkload() {
	ctx := context.Background()
	By("Creating test namespace")
	for _, m := range append(memberClusters, hubCluster) {
		nsDef := corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: wm.namespace,
			},
		}
		Expect(m.Client().Create(ctx, &nsDef)).Should(Succeed(), "Failed to create namespace %s in cluster %s", wm.namespace, m.Name())
	}

	By("Creating app deployment")
	for _, m := range memberClusters {
		deploymentDef := wm.deployment(m.Name())
		serviceDef := wm.service
		Expect(m.Client().Create(ctx, deploymentDef)).Should(Succeed(), "Failed to create app deployment %s in cluster %s", deploymentDef.Name, m.Name())

		Expect(m.Client().Create(ctx, &serviceDef)).Should(Succeed(), "Failed to create app service %s in cluster %s", serviceDef.Name, m.Name())
	}
}

func (wm *workloadManager) assertRemoveWorkload() {
	ctx := context.Background()
	By("Deleting test namespace")
	for _, m := range append(memberClusters, hubCluster) {
		nsDef := corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: wm.namespace,
			},
		}
		Expect(m.Client().Delete(ctx, &nsDef)).Should(Succeed(), "Failed to delete namespace %s in cluster %s", wm.namespace, m.Name())
	}
}

func (wm *workloadManager) assertExportService() {
	ctx := context.Background()

	By("Creating service export")
	for _, m := range memberClusters {
		serviceExportDef := wm.serviceExport
		serviceExportObj := &fleetnetv1alpha1.ServiceExport{}
		serviceExporKey := types.NamespacedName{Namespace: wm.namespace, Name: serviceExportDef.Name}
		Expect(m.Client().Create(ctx, &serviceExportDef)).Should(Succeed(), "Failed to create service export %s in cluster %s", serviceExportDef.Name, m.Name())
		Eventually(func() string {
			if err := m.Client().Get(ctx, serviceExporKey, serviceExportObj); err != nil {
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
			return cmp.Diff(wantedSvcExportConditions, serviceExportObj.Status.Conditions, svcExportConditionCmpOptions...)
		}, framework.PollTimeout, framework.PollInterval).Should(BeEmpty(), "Validate service export condition mismatch (-want, +got):")
	}

	By("Creating a multi-cluster service")
	mcsObj := &fleetnetv1alpha1.MultiClusterService{}
	memberClusterMCS := memberClusters[0]
	mcs := wm.mcs
	multiClusterSvcKey := types.NamespacedName{Namespace: mcs.Namespace, Name: mcs.Name}
	Expect(memberClusterMCS.Client().Create(ctx, &mcs)).Should(Succeed(), "Failed to create multi-cluster service %s in cluster %s", mcs.Name, memberClusterMCS.Name())
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
		return cmp.Diff(wantedMCSCondition, mcsObj.Status.Conditions, mcsConditionCmpOptions...)
	}, framework.PollTimeout, framework.PollInterval).Should(BeEmpty(), "Validate multi-cluster service condition mismatch (-want, +got):")
}

func (wm *workloadManager) assertUnexportService() {
	ctx := context.Background()
	By("Deleting Multi-cluster service if exists")
	memberClusterMCS := memberClusters[1]
	mcsDef := wm.mcs
	mcsObj := &fleetnetv1alpha1.MultiClusterService{}
	multiClusterSvcKey := types.NamespacedName{Namespace: mcsDef.Namespace, Name: mcsDef.Name}
	err := memberClusterMCS.Client().Delete(ctx, &mcsDef)
	Expect(true).To(Or(Equal(errors.IsNotFound(err)), Equal(err == nil)), "Failed to delete mcs %s in cluster %s", mcsDef.Name, memberClusterMCS.Name())
	Eventually(func() bool {
		return errors.IsNotFound(memberClusterMCS.Client().Get(ctx, multiClusterSvcKey, mcsObj))
	}, framework.PollTimeout, framework.PollInterval).Should(BeTrue(), "Failed to delete mcs")

	By("Unexporting the service if not")
	for _, m := range memberClusters {
		serviceExportDef := wm.serviceExport
		serviceExporKey := types.NamespacedName{Namespace: serviceExportDef.Namespace, Name: serviceExportDef.Name}
		err := m.Client().Delete(ctx, &serviceExportDef)
		Expect(true).To(Or(Equal(errors.IsNotFound(err)), Equal(err == nil)), "Failed to delete service export %s in cluster %s", serviceExportDef.Name, m.Name())
		serviceExportObj := &fleetnetv1alpha1.ServiceExport{}
		Eventually(func() bool {
			return errors.IsNotFound(m.Client().Get(ctx, serviceExporKey, serviceExportObj))
		}, framework.PollTimeout, framework.PollInterval).Should(BeTrue(), "Failed to delete service export")
	}
}
