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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
	"go.goms.io/fleet-networking/pkg/providers/azure/frontdoor"
)

type gatewayObservation struct {
	uid        types.UID
	generation int64
	model      gatewaymodel.GlobalGateway
	result     frontdoor.PrivateOriginsObservation
	err        error
}

func (r *Reconciler) observeOrigins(ctx context.Context, gateway *gatewayv1.Gateway) (*gatewayObservation, error) {
	if r.OriginObserver == nil {
		return nil, nil
	}
	var class gatewayv1.GatewayClass
	if err := r.Client.Get(ctx, client.ObjectKey{Name: string(gateway.Spec.GatewayClassName)}, &class); err != nil {
		return nil, client.IgnoreNotFound(err)
	}
	if class.Spec.ControllerName != gatewayv1.GatewayController(objectmeta.GatewayAPIControllerName) {
		return nil, nil
	}
	model, err := r.resolve(ctx, gateway.DeepCopy(), &class)
	if err != nil || model == nil {
		return nil, err
	}
	if len(model.Routes) == 0 {
		return nil, nil
	}
	observation := &gatewayObservation{uid: gateway.UID, generation: gateway.Generation, model: *model}
	observation.result, observation.err = r.readOrigins(ctx, gateway, *model)
	return observation, observation.err
}

func (r *Reconciler) readOrigins(ctx context.Context, gateway *gatewayv1.Gateway,
	model gatewaymodel.GlobalGateway) (frontdoor.PrivateOriginsObservation, error) {
	if r.APIReader == nil {
		return frontdoor.PrivateOriginsObservation{}, fmt.Errorf("uncached API reader is required for AFD observation")
	}
	var hub corev1.Namespace
	if err := r.APIReader.Get(ctx, client.ObjectKey{Name: metav1.NamespaceSystem}, &hub); err != nil {
		return frontdoor.PrivateOriginsObservation{}, fmt.Errorf("read trusted hub identity for AFD observation: %w", err)
	}
	if hub.UID == "" {
		return frontdoor.PrivateOriginsObservation{}, fmt.Errorf("hub kube-system Namespace UID is required for AFD observation")
	}
	start := time.Now()
	result, err := r.OriginObserver.Observe(ctx, frontdoor.GatewayOwner{
		HubUID: string(hub.UID), Namespace: gateway.Namespace, Name: gateway.Name, UID: string(gateway.UID),
	}, model)
	recordOriginObservation(time.Since(start), err)
	return result, err
}

func applyOriginObservation(gateway *gatewayv1.Gateway, model *gatewaymodel.GlobalGateway, observation *gatewayObservation) bool {
	if observation == nil || model == nil || gateway.UID != observation.uid ||
		gateway.Generation != observation.generation || !reflect.DeepEqual(*model, observation.model) {
		return false
	}
	pending := meta.FindStatusCondition(gateway.Status.Conditions, string(gatewayv1.GatewayConditionProgrammed))
	if pending != nil {
		// Origin convergence is not complete graph readiness; never publish addresses or Programmed=True here.
		pending.Message = originObservationMessage(observation)
		return true
	}
	return false
}

func originObservationMessage(observation *gatewayObservation) string {
	const incomplete = "full enabled graph readiness is not established by origin observation"
	if observation.err != nil {
		return "AFD observation failed: " + observation.err.Error()
	}
	result := observation.result
	switch {
	case result.ProfileMissing:
		return "AFD profile has not been created; " + incomplete
	case len(result.PendingApprovals) != 0:
		target := result.PendingApprovals[0].Target
		return fmt.Sprintf("AFD Private Link approval pending for %d shared targets; authorized PLS owner must approve asynchronously (first target %s in %s); %s",
			len(result.PendingApprovals), target.ResourceID, target.Location, incomplete)
	case result.OriginsConverged:
		return "Private origins are approved and deployed; " + incomplete
	}
	for _, origin := range result.Origins {
		if origin.State != frontdoor.PrivateOriginReady {
			return fmt.Sprintf("AFD private origin %s/%s is %s; %s",
				origin.Reference.OriginGroup, origin.Reference.Origin, origin.State, incomplete)
		}
	}
	return "AFD private origin convergence is not established; " + incomplete
}

func progressRequeue(active bool) ctrl.Result {
	if !active {
		return ctrl.Result{}
	}
	return ctrl.Result{RequeueAfter: wait.Jitter(30*time.Second, 0.2)}
}
