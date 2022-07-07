/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package mcsserviceimportcontroller features the internalserviceimport controller deployed in member cluster to managed
// internalserviceimport according to its corresponding serviceimport.
package mcsserviceimportcontroller

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
)

const (
	ServiceImportFinalizer = "networking.fleet.azure.com/serviceimport-cleanup"
)

// Reconciler reconciles a InternalServceImport object.
type Reconciler struct {
	memberClient client.Client
	hubClient    client.Client
	Scheme       *runtime.Scheme
}

//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceimports,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceimports/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceimports/finalizers,verbs=get;update
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

	internalServiceImport := &fleetnetv1alpha1.InternalServiceImport{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceImport.Name,
			Namespace: getMemberNamespaceInHub(serviceImport),
		},
	}

	// Examine DeletionTimestamp to determine if service import is under deletion.
	if serviceImport.ObjectMeta.DeletionTimestamp.IsZero() {
		// Add finalizer when it's in service import when not being deleted
		if !controllerutil.ContainsFinalizer(serviceImport, ServiceImportFinalizer) {
			controllerutil.AddFinalizer(serviceImport, ServiceImportFinalizer)
			if err := r.memberClient.Update(ctx, serviceImport); err != nil {
				klog.ErrorS(err, "Failed to add serviceimport finalizer", "ServiceImport", serviceImportRef, "finalizer", ServiceImportFinalizer)
				return ctrl.Result{}, err
			}
		}
	} else {
		// Delete service import dependency when the finalizer is expected then remove the finalizer from service import.
		if controllerutil.ContainsFinalizer(serviceImport, ServiceImportFinalizer) {
			if err := r.hubClient.Delete(ctx, internalServiceImport); err != nil {
				klog.ErrorS(err, "Failed to delete internalserviceimport as requried by serviceimport finalizer", "InternalServiceImport", klog.KObj(internalServiceImport), "ServiceImport", serviceImportRef, "finalizer", ServiceImportFinalizer)
				return ctrl.Result{}, err
			}

			controllerutil.RemoveFinalizer(serviceImport, ServiceImportFinalizer)
			if err := r.memberClient.Update(ctx, serviceImport); err != nil {
				klog.ErrorS(err, "Failed to remove serviceimport finalizer", "ServiceImport", serviceImportRef, "finalizer", ServiceImportFinalizer)
				return ctrl.Result{}, err
			}

			// Stop reconciliation as the item is being deleted
			return ctrl.Result{}, nil
		}
	}

	// create or update internalserviceimport in member cluster namespace in hub cluster.

	// NOTE(mainred): As a service import can be exposed by other cluster, we don't override the exposed cluster when
	// it's specified.
	// Targetnamespace must be the same one as service import, so we update targetnamespace anyway.
	if len(internalServiceImport.Spec.ExposedCluster) != 0 {
		klog.V(2).InfoS("Don't update exposed cluster of InternalServiceImport as it has been set", "InternalServiceImport", klog.KObj(internalServiceImport), "exposed cluster", internalServiceImport.Spec.ExposedCluster)
		return reconcile.Result{}, nil
	}
	internalServiceImport.Spec.ExposedCluster = getExposedClusterName(serviceImport)
	internalServiceImport.Spec.TargetNamespace = serviceImport.Namespace
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
