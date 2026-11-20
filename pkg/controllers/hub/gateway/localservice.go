/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gateway

import (
	"context"
	"errors"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewayresolver"
)

type pendingLocalServiceResolver struct {
	resolver gatewayresolver.LocalServiceResolver
	pending  bool
}

func (r *pendingLocalServiceResolver) ResolveLocalService(ctx context.Context, namespace string, ref gatewayv1.HTTPBackendRef) (gatewaymodel.Backend, error) {
	backend, err := r.resolver.ResolveLocalService(ctx, namespace, ref)
	var unavailable *gatewayresolver.ResolutionError
	if errors.As(err, &unavailable) && unavailable.ReferenceValid && unavailable.Reason == "BackendUnavailable" {
		// Azure PLS readiness can change without a Kubernetes watch event.
		r.pending = true
	}
	return backend, err
}
