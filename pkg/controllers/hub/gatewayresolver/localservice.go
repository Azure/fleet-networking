/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gatewayresolver

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"go.goms.io/fleet-networking/pkg/common/objectmeta"
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

// Companion events refer to the source backend, not the companion's own name.
// This only schedules reconciliation; origin authorization remains in the resolver.
func localOriginSource(ctx context.Context, reader client.Reader, service *corev1.Service) (*corev1.Service, error) {
	if service.Labels[objectmeta.LocalOriginManagedLabel] != objectmeta.LocalOriginManager {
		return service, nil
	}
	name := service.Labels[objectmeta.LocalOriginSourceNameLabel]
	uid := service.Labels[objectmeta.LocalOriginSourceUIDLabel]
	owner := metav1.GetControllerOf(service)
	if name == "" || uid == "" || owner == nil || owner.APIVersion != "v1" || owner.Kind != "Service" ||
		owner.Name != name || string(owner.UID) != uid {
		return nil, nil
	}
	source := &corev1.Service{}
	if err := reader.Get(ctx, client.ObjectKey{Namespace: service.Namespace, Name: name}, source); err != nil {
		return nil, client.IgnoreNotFound(err)
	}
	if string(source.UID) != uid || source.Labels[objectmeta.LocalOriginManagedLabel] != "" {
		return nil, nil
	}
	return source, nil
}
