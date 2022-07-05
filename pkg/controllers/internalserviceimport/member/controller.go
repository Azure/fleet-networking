/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package member

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/controllers/internalserviceimport/consts"
)

const (
	multiClusterServiceExportCluster = "networking.fleet.azure.com/service-resources-cleanup"
	multiClusterServiceLabelService  = "networking.fleet.azure.com/derived-service"
)

// Reconciler reconciles a MultiClusterService object.
type Reconciler struct {
	memberClient client.Client
	hubClient    client.Client
	Scheme       *runtime.Scheme
}

//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceimports,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=internalserviceimports,verbs=get;list;watch;create;update;patch;delete

// Reconcile in member cluster creates hub cluster internal service import out of member cluster service import.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// fetch serviceimport in member cluster
	serviceImport := &fleetnetv1alpha1.ServiceImport{}
	serviceImportRef := klog.KRef(req.Namespace, req.Name)
	err := r.memberClient.Get(ctx, req.NamespacedName, serviceImport)
	if errors.IsNotFound(err) {
		klog.ErrorS(err, "Could not find ServiceImport in member cluser", "ServiceImport", serviceImportRef)
		return reconcile.Result{}, nil
	}
	if err != nil {
		klog.ErrorS(err, "Failed to get ServiceImport", "ServiceImport", serviceImportRef)
		return reconcile.Result{}, err
	}

	// create or update internalserviceimport in member cluster namespace in hub cluster.
	internalServiceImport := &fleetnetv1alpha1.InternalServiceImport{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceImport.Name,
			Namespace: getMemberNamespaceInHub(serviceImport),
		},
	}
	internalServiceImport.SetLabels(map[string]string{
		consts.LabelTargetNamespace:    serviceImport.Namespace,
		consts.LabelExposedClusterName: getExposedClusterName(serviceImport),
	})
	if _, err := controllerutil.CreateOrPatch(ctx, r.hubClient, internalServiceImport, func() error {
		return nil
	}); err != nil {
		klog.ErrorS(err, "Failed to create or update InternalServiceImport from ServiceImport", "InternalServiceImport", klog.KObj(internalServiceImport), "ServiceImport", klog.KObj(serviceImport))
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetnetv1alpha1.ServiceImport{}).
		Complete(r)
}

// TDOO(mainred): remove this and the actual util to obtain hub member cluster namespace.
func getMemberNamespaceInHub(svcImport *fleetnetv1alpha1.ServiceImport) string {
	return "member-x-in-hub-to-be-changed"
}

// TDOO(mainred): remove this function and related code to use actual util to obtain exposed cluster name.
func getExposedClusterName(svcImport *fleetnetv1alpha1.ServiceImport) string {
	return "clustername-to-be-changed"
}
