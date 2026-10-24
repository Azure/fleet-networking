/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gateway

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
	"go.goms.io/fleet-networking/pkg/providers/azure/frontdoor"
)

func (r *Reconciler) applyHTTPPreview(ctx context.Context, gateway *gatewayv1.Gateway,
	model *gatewaymodel.GlobalGateway, staging *gatewayStaging) (bool, error) {
	if staging == nil || staging.http == nil || model == nil || gateway.UID != staging.uid ||
		gateway.Generation != staging.generation || !reflect.DeepEqual(*model, staging.model) {
		return false, nil
	}
	result := staging.http
	message := fmt.Sprintf("AFD private HTTP graph: %s", result.State)
	if staging.err != nil {
		setHTTPPreviewStatus(gateway, false, "AFD private HTTP provisioning failed: "+staging.err.Error())
		return false, staging.err
	}
	switch result.State {
	case frontdoor.HTTPPreviewStaging, frontdoor.HTTPPreviewPending, frontdoor.HTTPPreviewRouteEnabling,
		frontdoor.HTTPPreviewEndpointEnabling, frontdoor.HTTPPreviewSuspending:
		if approvals := len(result.Staging.Route.Backends.Origins.PendingApprovals); approvals != 0 {
			message += fmt.Sprintf("; AFD Private Link approval is asynchronous; authorized PLS owner must approve %d shared targets", approvals)
		}
		if result.Diagnostic != "" {
			message += "; " + result.Diagnostic
		}
		setHTTPPreviewStatus(gateway, false, message)
		return true, nil
	case frontdoor.HTTPPreviewProgrammed:
		host := result.EndpointHostName
		if len(validation.IsDNS1123Subdomain(host)) != 0 || !strings.HasSuffix(host, ".azurefd.net") {
			err := fmt.Errorf("converged AFD graph has no usable Azure hostname")
			setHTTPPreviewStatus(gateway, false, err.Error())
			return false, err
		}
		// Recheck uncached authorization/source identities after external work and
		// on every status retry. This is not a transactional Kubernetes/ARM snapshot.
		current, err := r.currentProvisioningIntent(ctx, gateway, *model, staging.owner.HubUID)
		if err == nil && !controllerutil.ContainsFinalizer(current, objectmeta.GatewayAFDCleanupFinalizer) {
			err = fmt.Errorf("Gateway cleanup ownership was lost during HTTP provisioning")
		}
		if err != nil {
			setHTTPPreviewStatus(gateway, false, "AFD ready evidence is no longer current: "+err.Error())
			return false, err
		}
		setHTTPPreviewStatus(gateway, true, "Observed complete enabled AFD Premium private HTTP graph; live application health is not implied")
		gateway.Status.Addresses = []gatewayv1.GatewayStatusAddress{{Type: ptr.To(gatewayv1.HostnameAddressType), Value: host}}
		return true, nil
	default:
		err := fmt.Errorf("AFD provider returned unknown HTTP provisioning state %q", result.State)
		setHTTPPreviewStatus(gateway, false, err.Error())
		return false, err
	}
}

func setHTTPPreviewStatus(gateway *gatewayv1.Gateway, ready bool, message string) {
	status, reason := metav1.ConditionFalse, string(gatewayv1.GatewayReasonPending)
	if ready {
		status, reason = metav1.ConditionTrue, string(gatewayv1.GatewayReasonProgrammed)
	}
	gateway.Status.Addresses = nil
	condition := metav1.Condition{Type: string(gatewayv1.GatewayConditionProgrammed), Status: status, Reason: reason,
		Message: message, ObservedGeneration: gateway.Generation}
	meta.SetStatusCondition(&gateway.Status.Conditions, condition)
	for i := range gateway.Status.Listeners {
		meta.SetStatusCondition(&gateway.Status.Listeners[i].Conditions, condition)
	}
}

func preserveProgrammedTransitions(gateway, before *gatewayv1.Gateway) {
	preserve := func(current, previous []metav1.Condition) {
		condition := meta.FindStatusCondition(current, string(gatewayv1.GatewayConditionProgrammed))
		old := meta.FindStatusCondition(previous, string(gatewayv1.GatewayConditionProgrammed))
		if condition != nil && old != nil && condition.Status == old.Status {
			condition.LastTransitionTime = old.LastTransitionTime
		}
	}
	preserve(gateway.Status.Conditions, before.Status.Conditions)
	for i := range gateway.Status.Listeners {
		for _, previous := range before.Status.Listeners {
			if gateway.Status.Listeners[i].Name == previous.Name {
				preserve(gateway.Status.Listeners[i].Conditions, previous.Conditions)
			}
		}
	}
}

func (r *Reconciler) suspendEndpoint(ctx context.Context, owner frontdoor.GatewayOwner) (frontdoor.EndpointSuspensionResult, error) {
	if r.HTTPProvisioner != nil {
		return r.HTTPProvisioner.SuspendEndpoint(ctx, owner)
	}
	if r.GraphStager != nil {
		return r.GraphStager.SuspendEndpoint(ctx, owner)
	}
	return frontdoor.EndpointSuspensionResult{}, fmt.Errorf("AFD endpoint suspension provider is not configured")
}

func (r *Reconciler) suspendUnprogrammableGateway(ctx context.Context, gateway *gatewayv1.Gateway) (*gatewayStaging, error) {
	if !controllerutil.ContainsFinalizer(gateway, objectmeta.GatewayAFDCleanupFinalizer) {
		return nil, nil
	}
	staging := &gatewayStaging{uid: gateway.UID, generation: gateway.Generation, invalid: true,
		suspension: &frontdoor.EndpointSuspensionResult{}}
	owner, err := r.unprogrammableOwner(ctx, gateway)
	if err != nil {
		staging.err = err
		return staging, err
	}
	start := time.Now()
	*staging.suspension, staging.err = r.suspendEndpoint(ctx, owner)
	r.recordGraphOperation(gateway, graphSuspendInvalid, time.Since(start), staging.err)
	return staging, staging.err
}

func (r *Reconciler) unprogrammableOwner(ctx context.Context, gateway *gatewayv1.Gateway) (frontdoor.GatewayOwner, error) {
	if r.APIReader == nil || r.ProfileCleanup == nil || gateway.UID == "" {
		return frontdoor.GatewayOwner{}, fmt.Errorf("uncached reader, cleanup provider and existing identity are required before endpoint suspension")
	}
	hubUID, err := r.provisioningHubUID(ctx)
	if err != nil {
		return frontdoor.GatewayOwner{}, err
	}
	current, class, err := r.currentGatewayIdentity(ctx, gateway, hubUID)
	if err != nil {
		return frontdoor.GatewayOwner{}, err
	}
	if !controllerutil.ContainsFinalizer(current, objectmeta.GatewayAFDCleanupFinalizer) {
		return frontdoor.GatewayOwner{}, errProvisioningIntentChanged
	}
	actual, err := resolveGateway(ctx, r.APIReader, current.DeepCopy(), class, false)
	if err != nil {
		return frontdoor.GatewayOwner{}, err
	}
	if actual != nil {
		return frontdoor.GatewayOwner{}, errProvisioningIntentChanged
	}
	if err := ctx.Err(); err != nil {
		return frontdoor.GatewayOwner{}, err
	}
	return frontdoor.GatewayOwner{HubUID: hubUID, Namespace: current.Namespace, Name: current.Name, UID: string(current.UID)}, nil
}
