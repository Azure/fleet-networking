/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gatewaymodel

import (
	"fmt"
	"reflect"
)

const (
	maxBackendOriginGroups = 100
	maxBackendOrigins      = 160
	maxOriginsPerBackend   = 40
)

// BackendCapacity counts the backend resources for one Gateway. Different ports
// require different origin groups because the HTTP port is an origin property.
type BackendCapacity struct {
	ServiceImports int
	OriginGroups   int
	Origins        int
}

// PreflightBackendCapacity validates the proposed backend support envelope.
// A provider must also preflight its actual generated routes, domains and rules;
// Kubernetes HTTPRoute counts cannot substitute for those resource counts.
func PreflightBackendCapacity(model GlobalGateway) (BackendCapacity, error) {
	normalized, err := Normalize(model)
	if err != nil {
		return BackendCapacity{}, err
	}
	type backendKey struct {
		namespace string
		name      string
		port      int32
	}
	services := make(map[string]struct{})
	groups := make(map[backendKey][]Origin)
	var capacity BackendCapacity
	for _, route := range normalized.Routes {
		for _, rule := range route.Rules {
			for _, backend := range rule.Backends {
				key := backendKey{namespace: backend.Namespace, name: backend.Name, port: backend.Port}
				if origins, found := groups[key]; found {
					// Never let traversal order choose between inconsistent copies of one backend.
					if !reflect.DeepEqual(origins, backend.Origins) {
						return BackendCapacity{}, fmt.Errorf("inconsistent origins for backend %s/%s:%d", key.namespace, key.name, key.port)
					}
					continue
				}
				if len(backend.Origins) > maxOriginsPerBackend {
					return BackendCapacity{}, fmt.Errorf("backend %s/%s:%d has %d origins, maximum is %d",
						key.namespace, key.name, key.port, len(backend.Origins), maxOriginsPerBackend)
				}
				groups[key] = backend.Origins
				services[key.namespace+"/"+key.name] = struct{}{}
				capacity.OriginGroups++
				capacity.Origins += len(backend.Origins)
				if capacity.OriginGroups > maxBackendOriginGroups || capacity.Origins > maxBackendOrigins {
					return BackendCapacity{}, fmt.Errorf("backend capacity has %d origin groups and %d origins, maximums are %d and %d",
						capacity.OriginGroups, capacity.Origins, maxBackendOriginGroups, maxBackendOrigins)
				}
			}
		}
	}
	capacity.ServiceImports = len(services)
	return capacity, nil
}
