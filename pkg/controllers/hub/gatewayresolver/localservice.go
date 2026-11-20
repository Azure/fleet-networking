/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gatewayresolver

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
)

// LocalServiceResolver resolves explicitly enabled cluster-local private origins.
type LocalServiceResolver interface {
	ResolveLocalService(context.Context, string, gatewayv1.HTTPBackendRef) (gatewaymodel.Backend, error)
}

type localServiceReader struct {
	client.Reader
	LocalServiceResolver
}

// WithLocalServices retains the reader's cache/uncached boundary and enables Service references.
func WithLocalServices(reader client.Reader, resolver LocalServiceResolver) client.Reader {
	if resolver == nil {
		return reader
	}
	return localServiceReader{Reader: reader, LocalServiceResolver: resolver}
}
