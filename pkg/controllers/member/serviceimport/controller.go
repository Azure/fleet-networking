/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package serviceimport features the serviceimport controller deployed in member cluster to managed
// internalserviceimport according to its corresponding serviceimport.
package serviceimport

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	memberClusterID string

	// The namespace reserved for the current member cluster in the hub cluster.
	hubNamespace string

	hubClient    client.Client
	memberClient client.Client

	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceimports,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=serviceimports/finalizers,verbs=get;update
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=internalserviceimports,verbs=get;list;watch;create;update;patch;delete

// Reconcile in member cluster creates hub cluster internal service import out of member cluster service import.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// fetch serviceimport in member cluster
	serviceImport := &fleetnetv1alpha1.ServiceImport{}
	serviceImportRef := klog.KRef(req.Namespace, req.Name)
	startTime := time.Now()
	klog.V(2).InfoS("Reconciliation starts", "ServiceImport", serviceImportRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Reconciliation ends", "ServiceImport", serviceImportRef, "latency", latency)
	}()

	if err := r.memberClient.Get(ctx, req.NamespacedName, serviceImport); err != nil {
		klog.ErrorS(err, "Failed to get ServiceImport", "ServiceImport", serviceImportRef)
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	internalServiceImportName := formatInternalServiceImportName(serviceImport)
	internalServiceImport := &fleetnetv1alpha1.InternalServiceImport{
		ObjectMeta: metav1.ObjectMeta{
			Name:      internalServiceImportName,
			Namespace: r.hubNamespace,
		},
	}

	// Examine DeletionTimestamp to determine if service import is under deletion.
	if serviceImport.ObjectMeta.DeletionTimestamp != nil {
		// Delete service import dependency when the finalizer is expected then remove the finalizer from service import.
		if controllerutil.ContainsFinalizer(serviceImport, ServiceImportFinalizer) {
			if err := r.hubClient.Delete(ctx, internalServiceImport); err != nil {
				klog.ErrorS(err, "Failed to delete internalserviceimport as required by serviceimport finalizer", "InternalServiceImport", klog.KObj(internalServiceImport), "ServiceImport", serviceImportRef, "finalizer", ServiceImportFinalizer)
				if !apierrors.IsNotFound(err) {
					return ctrl.Result{}, err
				}
			}

			controllerutil.RemoveFinalizer(serviceImport, ServiceImportFinalizer)
			if err := r.memberClient.Update(ctx, serviceImport); err != nil {
				klog.ErrorS(err, "Failed to remove serviceimport finalizer", "ServiceImport", serviceImportRef, "finalizer", ServiceImportFinalizer)
				return ctrl.Result{}, err
			}

			// Stop reconciliation as the item is being deleted
			return ctrl.Result{}, nil
		}
	} else {
		// Add finalizer when it's in service import when not being deleted
		if !controllerutil.ContainsFinalizer(serviceImport, ServiceImportFinalizer) {
			controllerutil.AddFinalizer(serviceImport, ServiceImportFinalizer)
			if err := r.memberClient.Update(ctx, serviceImport); err != nil {
				klog.ErrorS(err, "Failed to add serviceimport finalizer", "ServiceImport", serviceImportRef, "finalizer", ServiceImportFinalizer)
				return ctrl.Result{}, err
			}
		}
	}

	klog.V(2).InfoS("Create or update internal service import", "InternalServiceImport", klog.KObj(internalServiceImport))
	if _, err := controllerutil.CreateOrUpdate(ctx, r.hubClient, internalServiceImport, func() error {
		if internalServiceImport.CreationTimestamp.IsZero() {
			// Set the ServiceReference only when the InternalServiceImport is created; most of the fields in
			// an ExportedObjectReference should be immutable.
			internalServiceImport.Spec.ServiceImportReference = fleetnetv1alpha1.FromMetaObjects(r.memberClusterID, serviceImport.TypeMeta, serviceImport.ObjectMeta)
		}
		internalServiceImport.Spec.ServiceImportReference.UpdateFromMetaObject(serviceImport.ObjectMeta)
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

// formatInternalServiceImportName returns the unique name assigned to an service import
func formatInternalServiceImportName(serviceImport *fleetnetv1alpha1.ServiceImport) string {
	return fmt.Sprintf("%s-%s", serviceImport.Namespace, serviceImport.Name)
}
