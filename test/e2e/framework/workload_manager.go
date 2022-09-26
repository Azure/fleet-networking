/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package framework

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/uniquename"
)

// WorkloadManager represents a suite of variables of operations required to test exporting an service and more.
type WorkloadManager struct {
	Fleet              *Fleet
	namespace          string
	service            corev1.Service
	deploymentTemplate appsv1.Deployment
}

// NewWorkloadManager returns a workload manager with default values.
func NewWorkloadManager(fleet *Fleet) *WorkloadManager {
	// Using unique namespace decouple tests, especially considering we have test failure, and simply cleanup stage.
	namespaceUnique := UniqueTestNamespace()

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

	return &WorkloadManager{
		Fleet:              fleet,
		namespace:          namespaceUnique,
		service:            svcDef,
		deploymentTemplate: deploymentTemplateDef,
	}
}

// Service returns the service which workload manager will deploy.
func (wm *WorkloadManager) Service() corev1.Service {
	return wm.service
}

// ServiceExport returns the ServiceExport definition from pre-defined service name and namespace.
func (wm *WorkloadManager) ServiceExport() fleetnetv1alpha1.ServiceExport {
	return fleetnetv1alpha1.ServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: wm.namespace,
			Name:      wm.service.Name,
		},
	}
}

// ServiceExport returns the MultiClusterService definition from pre-defined service name and namespace.
func (wm *WorkloadManager) MultiClusterService() fleetnetv1alpha1.MultiClusterService {
	return fleetnetv1alpha1.MultiClusterService{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: wm.namespace,
			Name:      wm.service.Name,
		},
		Spec: fleetnetv1alpha1.MultiClusterServiceSpec{
			ServiceImport: fleetnetv1alpha1.ServiceImportRef{
				Name: wm.service.Name,
			},
		},
	}
}

// Deployment returns an deployment definition base on the cluster name.
func (wm *WorkloadManager) Deployment(clusterName string) *appsv1.Deployment {
	deployment := wm.deploymentTemplate
	deployment.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{{Name: "MEMBER_CLUSTER_ID", Value: clusterName}}
	return &deployment
}

// DeployWorkload deploys workload(deployment and its service) to member clusters.
func (wm *WorkloadManager) DeployWorkload(ctx context.Context) error {
	for _, m := range wm.Fleet.Clusters() {
		nsDef := corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: wm.namespace,
			},
		}
		if err := m.Client().Create(ctx, &nsDef); err != nil {
			return fmt.Errorf("Failed to create namespace %s in cluster %s: %w", wm.namespace, m.Name(), err)
		}
	}

	for _, m := range wm.Fleet.MemberClusters() {
		deploymentDef := wm.Deployment(m.Name())
		serviceDef := wm.service
		if err := m.Client().Create(ctx, deploymentDef); err != nil {
			return fmt.Errorf("Failed to create app deployment %s in cluster %s: %w", deploymentDef.Name, m.Name(), err)
		}
		if err := m.Client().Create(ctx, &serviceDef); err != nil {
			return fmt.Errorf("Failed to create app service %s in cluster %s: %w", serviceDef.Name, m.Name(), err)
		}
	}
	return nil
}

// DeployWorkload deletes workload(deployment and its service) from member clusters.
func (wm *WorkloadManager) RemoveWorkload(ctx context.Context) error {
	for _, m := range wm.Fleet.MemberClusters() {
		deploymentDef := wm.Deployment(m.Name())
		svcDef := wm.service
		if err := m.Client().Delete(ctx, deploymentDef); err != nil {
			return fmt.Errorf("Failed to delete app deployment %s in cluster %s: %w", deploymentDef.Name, m.Name(), err)
		}
		if err := m.Client().Delete(ctx, &svcDef); err != nil {
			return fmt.Errorf("Failed to delete app service %s in cluster %s: %w", svcDef.Name, m.Name(), err)
		}
	}

	for _, m := range wm.Fleet.Clusters() {
		nsDef := corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: wm.namespace,
			},
		}
		if err := m.Client().Delete(ctx, &nsDef); err != nil {
			return fmt.Errorf("Failed to delete namespace %s in cluster %s: %w", wm.namespace, m.Name(), err)
		}
	}
	return nil
}

// ExportService exports the service by creating a service export.
func (wm *WorkloadManager) ExportService(ctx context.Context, svcExport fleetnetv1alpha1.ServiceExport) error {
	for _, m := range wm.Fleet.MemberClusters() {
		// NOTE: since `Create` function provided by controller-runtime will update the k8s definition variable, resuing
		// this variable for another `Create` will raise for non-empty resourceVersion.
		svcExportDef := svcExport
		svcExportObj := &fleetnetv1alpha1.ServiceExport{}
		svcExporKey := types.NamespacedName{Namespace: svcExportDef.Namespace, Name: svcExportDef.Name}
		if err := m.Client().Create(ctx, &svcExportDef); err != nil {
			return fmt.Errorf("Failed to create service export %s in cluster %s: %w", svcExportDef.Name, m.Name(), err)
		}

		// wait until service export condition is correct or raise error when the wait times out.
		if err := retry.OnError(defaultBackOff(), func(error) bool { return true }, func() error {
			if err := m.Client().Get(ctx, svcExporKey, svcExportObj); err != nil {
				return err
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
			svcExportConditionCmpRlt := cmp.Diff(wantedSvcExportConditions, svcExportObj.Status.Conditions, SvcExportConditionCmpOptions...)
			if len(svcExportConditionCmpRlt) != 0 {
				return fmt.Errorf("Validate service export condition mismatch (-want, +got): %s", svcExportConditionCmpRlt)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

// CreateMultiClusterService create a mcs from caller and wait until service import is found.
func (wm *WorkloadManager) CreateMultiClusterService(ctx context.Context, mcs fleetnetv1alpha1.MultiClusterService) error {
	mcsObj := &fleetnetv1alpha1.MultiClusterService{}
	memberClusterMCS := wm.Fleet.MCSMemberCluster()
	multiClusterSvcKey := types.NamespacedName{Namespace: mcs.Namespace, Name: mcs.Name}
	if err := memberClusterMCS.Client().Create(ctx, &mcs); err != nil {
		return fmt.Errorf("Failed to create multi-cluster service %s in cluster %s: %w", mcs.Name, memberClusterMCS.Name(), err)
	}
	return retry.OnError(defaultBackOff(), func(error) bool { return true }, func() error {
		if err := memberClusterMCS.Client().Get(ctx, multiClusterSvcKey, mcsObj); err != nil {
			return err
		}
		wantedMCSCondition := []metav1.Condition{
			{
				Type:   string(fleetnetv1alpha1.MultiClusterServiceValid),
				Reason: "FoundServiceImport",
				Status: metav1.ConditionTrue,
			},
		}
		mcsConditionCmpRlt := cmp.Diff(wantedMCSCondition, mcsObj.Status.Conditions, MCSConditionCmpOptions...)
		if len(mcsConditionCmpRlt) != 0 {
			return fmt.Errorf("Validate multi-cluster service condition mismatch (-want, +got): %s", mcsConditionCmpRlt)
		}
		return nil
	})
}

// DeleteMultiClusterService deletes the mcs specified from caller and wait until the mcs is not found.
func (wm *WorkloadManager) DeleteMultiClusterService(ctx context.Context, mcs fleetnetv1alpha1.MultiClusterService) error {
	memberClusterMCS := wm.Fleet.MCSMemberCluster()
	multiClusterSvcKey := types.NamespacedName{Namespace: mcs.Namespace, Name: mcs.Name}
	if err := memberClusterMCS.Client().Delete(ctx, &mcs); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("Failed to delete mcs %s in cluster %s: %w", multiClusterSvcKey, memberClusterMCS.Name(), err)
	}
	return retry.OnError(defaultBackOff(), func(error) bool { return true }, func() error {
		mcsObj := &fleetnetv1alpha1.MultiClusterService{}
		if err := memberClusterMCS.Client().Get(ctx, multiClusterSvcKey, mcsObj); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("Failed to delete mutl-cluster service %s in cluster %s, %w", multiClusterSvcKey, memberClusterMCS.Name(), err)
		}
		return nil
	})
}

// UnexportService deletes the ServiceExport specified by caller and wait until the ServiceExport is not found.
func (wm *WorkloadManager) UnexportService(ctx context.Context, svcExport fleetnetv1alpha1.ServiceExport) error {
	for _, m := range wm.Fleet.MemberClusters() {
		serviceExporKey := types.NamespacedName{Namespace: svcExport.Namespace, Name: svcExport.Name}
		if err := m.Client().Delete(ctx, &svcExport); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("Failed to delete service export %s in cluster %s: %w", serviceExporKey, m.Name(), err)
		}
		if err := retry.OnError(defaultBackOff(), func(error) bool { return true }, func() error {
			serviceExportObj := &fleetnetv1alpha1.ServiceExport{}
			if err := m.Client().Get(ctx, serviceExporKey, serviceExportObj); err != nil && !errors.IsNotFound(err) {
				return fmt.Errorf("Failed to delete service export %s in cluster %s, %w", serviceExporKey, m.Name(), err)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

// defaultBackOff return an exponential backoff which will add up to about 25 seconds.
func defaultBackOff() wait.Backoff {
	backoff := wait.Backoff{
		Steps:    8,
		Duration: 1 * time.Second,
		Factor:   1.4,
		Jitter:   0.1,
	}
	return backoff
}

// TODO(mainred): Before the app image is publicly available, we use the one built from e2e bootstrap.
// The app image construction must be aligned with the steps in test/scripts/bootstrap.sh.
func appImage() string {
	resourceGroupName := os.Getenv("AZURE_RESOURCE_GROUP")
	registryName := strings.ReplaceAll(resourceGroupName, "-", "")
	return fmt.Sprintf("%s.azurecr.io/app", registryName)
}

// UniqueTestNamespace gives a unique namespace name.
func UniqueTestNamespace() string {
	return fmt.Sprintf("%s-%s", TestNamespacePrefix, uniquename.RandomLowerCaseAlphabeticString(5))
}
