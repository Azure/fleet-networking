/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gateway

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
	"go.goms.io/fleet-networking/pkg/providers/azure/frontdoor"
)

type gatewayStaging struct {
	uid        types.UID
	generation int64
	model      gatewaymodel.GlobalGateway
	result     frontdoor.DisabledGraphStagingResult
	http       *frontdoor.HTTPPreviewProvisioningResult
	owner      frontdoor.GatewayOwner
	invalid    bool
	suspension *frontdoor.EndpointSuspensionResult
	denial     *referenceDenial
	err        error
}

func (r *Reconciler) stageGraph(ctx context.Context, gateway *gatewayv1.Gateway) (*gatewayStaging, error) {
	if r.GraphStager == nil && r.HTTPProvisioner == nil {
		return nil, nil
	}
	if r.GraphStager != nil && r.HTTPProvisioner != nil {
		return nil, fmt.Errorf("disabled-only staging and full HTTP provisioning cannot both be configured")
	}
	if r.HTTPProvisioner != nil {
		var class gatewayv1.GatewayClass
		if err := r.Client.Get(ctx, client.ObjectKey{Name: string(gateway.Spec.GatewayClassName)}, &class); err != nil {
			return nil, client.IgnoreNotFound(err)
		}
		if class.Spec.ControllerName != gatewayv1.GatewayController(objectmeta.GatewayAPIControllerName) {
			return nil, nil
		}
		if !gatewayClassAccepted(&class) {
			return r.suspendUnprogrammableGateway(ctx, gateway)
		}
	}
	// Revocation must not wait for unrelated backend resolution or a complete model.
	if staging, err := r.suspendDeniedReferences(ctx, gateway); staging != nil || err != nil {
		return staging, err
	}
	var class gatewayv1.GatewayClass
	if err := r.Client.Get(ctx, client.ObjectKey{Name: string(gateway.Spec.GatewayClassName)}, &class); err != nil {
		return nil, client.IgnoreNotFound(err)
	}
	if class.Spec.ControllerName != gatewayv1.GatewayController(objectmeta.GatewayAPIControllerName) {
		return nil, nil
	}
	model, err := r.resolve(ctx, gateway.DeepCopy(), &class)
	if err != nil {
		return nil, err
	}
	if model == nil {
		if r.HTTPProvisioner != nil {
			return r.suspendUnprogrammableGateway(ctx, gateway)
		}
		return nil, nil
	}
	if len(model.Routes) == 0 {
		return r.suspendEmptyGateway(ctx, gateway, *model)
	}
	staging := &gatewayStaging{uid: gateway.UID, generation: gateway.Generation, model: *model}
	if r.HTTPProvisioner != nil {
		staging.http = &frontdoor.HTTPPreviewProvisioningResult{}
	}
	owner, err := r.prepareProvisioning(ctx, gateway, *model)
	if err != nil {
		staging.err = err
		return staging, err
	}
	start := time.Now()
	if r.HTTPProvisioner != nil {
		staging.owner = owner
		*staging.http, staging.err = r.HTTPProvisioner.ReconcileHTTPPreview(ctx, owner, *model)
		r.recordGraphOperation(gateway, graphReconcileHTTP, time.Since(start), staging.err)
		return staging, staging.err
	}
	staging.result, staging.err = r.GraphStager.StageDisabledGraph(ctx, owner, *model)
	r.recordGraphOperation(gateway, graphStageDisabled, time.Since(start), staging.err)
	return staging, staging.err
}

func applyGraphStaging(gateway *gatewayv1.Gateway, model *gatewaymodel.GlobalGateway, staging *gatewayStaging) {
	if staging == nil || gateway.UID != staging.uid || gateway.Generation != staging.generation {
		return
	}
	if staging.invalid {
		if model == nil {
			message := fmt.Sprintf("AFD endpoint suspension: %s; current intent cannot be programmed", staging.suspension.State)
			if staging.err != nil {
				message = "AFD endpoint suspension failed: " + staging.err.Error()
			}
			setHTTPPreviewStatus(gateway, false, message)
		}
		return
	}
	if model == nil || !reflect.DeepEqual(*model, staging.model) || staging.http != nil {
		return
	}
	pending := meta.FindStatusCondition(gateway.Status.Conditions, string(gatewayv1.GatewayConditionProgrammed))
	if pending == nil {
		return
	}
	if staging.suspension != nil {
		if staging.err != nil {
			pending.Message = "AFD endpoint suspension failed: " + staging.err.Error()
		} else {
			pending.Message = fmt.Sprintf("AFD endpoint suspension: %s; no effective routes; ingress is not programmed", staging.suspension.State)
		}
		setHTTPPreviewStatus(gateway, false, pending.Message)
		return
	}
	if staging.err != nil {
		pending.Message = "AFD disabled graph staging failed: " + staging.err.Error()
		return
	}
	result := staging.result
	pending.Message = fmt.Sprintf("AFD disabled graph staging: roots=%s, route=%s, backends=%s, inventoryVerified=%t, staleOriginsPending=%d; disabled-only staging does not enable traffic; no address is published",
		result.Roots.State, result.Route.State, result.Route.Backends.State, result.InventoryVerified, result.StaleOriginsPending)
	if approvals := len(result.Route.Backends.Origins.PendingApprovals); approvals != 0 {
		pending.Message += fmt.Sprintf("; AFD Private Link approval is asynchronous; authorized PLS owner must approve %d shared targets", approvals)
	}
}
