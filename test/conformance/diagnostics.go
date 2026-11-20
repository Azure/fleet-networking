/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// FixtureDiagnostics excludes Secrets and Pod specifications/environment values.
func FixtureDiagnostics(ctx context.Context, c client.Client, namespaces []string) ([]byte, error) {
	snapshot := make(map[string]interface{})
	var issues []error
	for _, namespace := range namespaces {
		resources := make(map[string]interface{})
		for _, entry := range []struct {
			name string
			list client.ObjectList
		}{
			{"services", &corev1.ServiceList{}},
			{"endpointSlices", &discoveryv1.EndpointSliceList{}},
			{"gateways", &gatewayv1.GatewayList{}},
			{"httpRoutes", &gatewayv1.HTTPRouteList{}},
			{"events", &corev1.EventList{}},
		} {
			if err := c.List(ctx, entry.list, client.InNamespace(namespace)); err != nil {
				issues = append(issues, fmt.Errorf("snapshot %s/%s: %w", namespace, entry.name, err))
				resources[entry.name] = map[string]string{"error": err.Error()}
				continue
			}
			resources[entry.name] = entry.list
		}
		var pods corev1.PodList
		if err := c.List(ctx, &pods, client.InNamespace(namespace)); err != nil {
			issues = append(issues, fmt.Errorf("snapshot pods/%s: %w", namespace, err))
			resources["pods"] = map[string]string{"error": err.Error()}
		} else {
			statuses := make([]interface{}, 0, len(pods.Items))
			for _, pod := range pods.Items {
				statuses = append(statuses, map[string]interface{}{
					"name": pod.Name, "uid": pod.UID, "node": pod.Spec.NodeName,
					"deletionTimestamp": pod.DeletionTimestamp, "status": pod.Status,
				})
			}
			resources["pods"] = statuses
		}
		snapshot[namespace] = resources
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	return data, errors.Join(append(issues, err)...)
}
