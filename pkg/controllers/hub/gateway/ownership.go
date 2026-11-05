/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gateway

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
	"go.goms.io/fleet-networking/pkg/providers/azure/frontdoor"
)

var errProvisioningIntentChanged = errors.New("Gateway provisioning identity or desired intent changed")

// prepareProvisioning persists cleanup ownership without calling Azure and
// rechecks current intent; sequential reads are not transactional authorization.
func (r *Reconciler) prepareProvisioning(ctx context.Context, gateway *gatewayv1.Gateway,
	model gatewaymodel.GlobalGateway) (frontdoor.GatewayOwner, error) {
	if err := ctx.Err(); err != nil {
		return frontdoor.GatewayOwner{}, err
	}
	if r.Client == nil || r.APIReader == nil || r.ProfileCleanup == nil {
		return frontdoor.GatewayOwner{}, fmt.Errorf("Kubernetes writer, uncached reader and AFD cleanup provider are required before provisioning")
	}
	if gateway.UID == "" || model.UID != string(gateway.UID) || model.Namespace != gateway.Namespace || model.Name != gateway.Name ||
		len(model.Routes) == 0 {
		return frontdoor.GatewayOwner{}, fmt.Errorf("nonempty supported Gateway intent and matching owner identity are required before provisioning")
	}
	if err := preflightHTTPPreview(model); err != nil {
		return frontdoor.GatewayOwner{}, err
	}
	hubUID, err := r.provisioningHubUID(ctx)
	if err != nil {
		return frontdoor.GatewayOwner{}, err
	}
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current, err := r.currentProvisioningIntent(ctx, gateway, model, hubUID)
		if err != nil {
			return err
		}
		if controllerutil.ContainsFinalizer(current, objectmeta.GatewayAFDCleanupFinalizer) {
			return nil
		}
		before := current.DeepCopy()
		controllerutil.AddFinalizer(current, objectmeta.GatewayAFDCleanupFinalizer)
		if err := ctx.Err(); err != nil {
			return err
		}
		return r.Client.Patch(ctx, current, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
	})
	if err != nil {
		return frontdoor.GatewayOwner{}, err
	}
	// A successful patch is not enough: reobserve both finalizer and current intent.
	current, err := r.currentProvisioningIntent(ctx, gateway, model, hubUID)
	if err != nil {
		return frontdoor.GatewayOwner{}, err
	}
	if !controllerutil.ContainsFinalizer(current, objectmeta.GatewayAFDCleanupFinalizer) {
		return frontdoor.GatewayOwner{}, fmt.Errorf("Gateway cleanup finalizer was not observed after persistence")
	}
	return frontdoor.GatewayOwner{
		HubUID: hubUID, Namespace: current.Namespace, Name: current.Name, UID: string(current.UID),
	}, nil
}

func (r *Reconciler) provisioningHubUID(ctx context.Context) (string, error) {
	var hub corev1.Namespace
	if err := r.APIReader.Get(ctx, client.ObjectKey{Name: metav1.NamespaceSystem}, &hub); err != nil {
		return "", fmt.Errorf("read trusted hub identity before AFD provisioning: %w", err)
	}
	if hub.UID == "" {
		return "", fmt.Errorf("hub kube-system Namespace UID is required before AFD provisioning")
	}
	return string(hub.UID), nil
}

func (r *Reconciler) currentProvisioningIntent(ctx context.Context, expected *gatewayv1.Gateway,
	model gatewaymodel.GlobalGateway, hubUID string) (*gatewayv1.Gateway, error) {
	current, class, err := r.currentGatewayIdentity(ctx, expected, hubUID)
	if err != nil {
		return nil, err
	}
	actual, err := resolveGateway(ctx, r.APIReader, current.DeepCopy(), class, false)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if actual == nil || !reflect.DeepEqual(*actual, model) {
		return nil, errProvisioningIntentChanged
	}
	return current, nil
}

func (r *Reconciler) currentGatewayIdentity(ctx context.Context, expected *gatewayv1.Gateway,
	hubUID string) (*gatewayv1.Gateway, *gatewayv1.GatewayClass, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	var current gatewayv1.Gateway
	if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(expected), &current); err != nil {
		return nil, nil, fmt.Errorf("read current Gateway before AFD provisioning: %w", err)
	}
	if current.UID != expected.UID || current.Generation != expected.Generation || current.DeletionTimestamp != nil {
		return nil, nil, errProvisioningIntentChanged
	}
	currentHub, err := r.provisioningHubUID(ctx)
	if err != nil {
		return nil, nil, err
	}
	if currentHub != hubUID {
		return nil, nil, errProvisioningIntentChanged
	}
	var class gatewayv1.GatewayClass
	if err := r.APIReader.Get(ctx, client.ObjectKey{Name: string(current.Spec.GatewayClassName)}, &class); err != nil {
		return nil, nil, fmt.Errorf("read current GatewayClass before AFD provisioning: %w", err)
	}
	if class.Spec.ControllerName != gatewayv1.GatewayController(objectmeta.GatewayAPIControllerName) {
		return nil, nil, errProvisioningIntentChanged
	}
	return &current, &class, nil
}
