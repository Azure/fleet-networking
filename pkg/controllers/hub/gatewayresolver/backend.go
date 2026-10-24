/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package gatewayresolver resolves Fleet backend snapshots into private origins.
package gatewayresolver

import (
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	fleetnetv1beta1 "go.goms.io/fleet-networking/api/v1beta1"
	"go.goms.io/fleet-networking/pkg/annotations"
	"go.goms.io/fleet-networking/pkg/common/hubconfig"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
)

const serviceImportKind = "ServiceImport"

// ResolutionError distinguishes invalid references from unavailable private
// origins. ReferenceValid does not imply provisioning or AFD connection approval.
type ResolutionError struct {
	Reason         string
	ReferenceValid bool
	Message        string
}

func (e *ResolutionError) Error() string {
	return e.Reason + ": " + e.Message
}

// Resolve validates an already fetched backend snapshot and returns no partial
// backend on failure. The caller must enforce ReferenceGrant authorization before
// invoking Resolve for a cross-namespace reference.
func Resolve(routeNamespace string, ref gatewayv1.HTTPBackendRef, service *fleetnetv1alpha1.ServiceImport,
	exports []fleetnetv1alpha1.InternalServiceExport) (gatewaymodel.Backend, error) {
	if ptr.Deref(ref.Group, "") != gatewayv1.Group(fleetnetv1alpha1.GroupVersion.Group) ||
		ptr.Deref(ref.Kind, "Service") != serviceImportKind {
		return resolutionFailure("InvalidKind", false, "only %s/ServiceImport backends are supported", fleetnetv1alpha1.GroupVersion.Group)
	}
	namespace := string(ptr.Deref(ref.Namespace, gatewayv1.Namespace(routeNamespace)))
	if namespace == "" || ref.Name == "" || service == nil || service.Namespace != namespace ||
		service.Name != string(ref.Name) || service.DeletionTimestamp != nil {
		return resolutionFailure("BackendNotFound", false, "ServiceImport %s/%s is unavailable", namespace, ref.Name)
	}
	weight := ptr.Deref(ref.Weight, 1)
	if weight < 0 || weight > 1000000 || len(ref.Filters) != 0 {
		return resolutionFailure("UnsupportedValue", false, "backend weight must be in [0, 1000000] and backend filters are not supported")
	}
	if err := validateHTTPPort(ref.Port, service.Status.Ports); err != nil {
		return gatewaymodel.Backend{}, err
	}
	if err := annotations.ValidateServiceImportAnnotations(service.Annotations); err != nil {
		return resolutionFailure("UnsupportedConfiguration", true, "%v", err)
	}
	if service.Status.Type != "" && service.Status.Type != fleetnetv1alpha1.ClusterSetIP {
		return resolutionFailure("UnsupportedTopology", true, "headless ServiceImports are not supported")
	}
	exportsByCluster, err := selectBackendExports(namespace, service, exports)
	if err != nil {
		return gatewaymodel.Backend{}, err
	}
	backend := gatewaymodel.Backend{
		Namespace: namespace, Name: service.Name, Port: int32(*ref.Port),
		RouteWeight: weight, HealthProbe: gatewaymodel.HTTPPreviewHealthProbe(),
	}
	clusterNames := make([]string, 0, len(exportsByCluster))
	for cluster := range exportsByCluster {
		clusterNames = append(clusterNames, cluster)
	}
	sort.Strings(clusterNames)
	for _, cluster := range clusterNames {
		export := exportsByCluster[cluster]
		if export == nil {
			return resolutionFailure("BackendUnavailable", true, "export for cluster %q is unavailable", cluster)
		}
		origin, err := resolveExportOrigin(namespace, service, export)
		if err != nil {
			return gatewaymodel.Backend{}, err
		}
		if origin.Weight != 0 {
			backend.Origins = append(backend.Origins, origin)
		}
	}
	if len(backend.Origins) == 0 {
		return resolutionFailure("BackendUnavailable", true, "ServiceImport has no active private origins")
	}
	return backend, nil
}

func validateHTTPPort(number *gatewayv1.PortNumber, ports []fleetnetv1alpha1.ServicePort) error {
	if number == nil || *number < 1 || *number > 65535 {
		return newResolutionError("PortNotFound", false, "a valid ServiceImport backend port is required")
	}
	var selected *fleetnetv1alpha1.ServicePort
	var exists bool
	for i := range ports {
		port := &ports[i]
		if port.Port != int32(*number) {
			continue
		}
		exists = true
		if port.Protocol != "" && port.Protocol != corev1.ProtocolTCP {
			continue
		}
		if selected != nil {
			return newResolutionError("PortNotFound", false, "ServiceImport port %d is ambiguous", *number)
		}
		selected = port
	}
	if !exists {
		return newResolutionError("PortNotFound", false, "ServiceImport has no port %d", *number)
	}
	if selected == nil || (ptr.Deref(selected.AppProtocol, "") != "" && *selected.AppProtocol != "http") {
		return newResolutionError("UnsupportedProtocol", false, "the HTTP preview requires a TCP port with absent or http appProtocol")
	}
	return nil
}

func selectBackendExports(namespace string, service *fleetnetv1alpha1.ServiceImport,
	exports []fleetnetv1alpha1.InternalServiceExport) (map[string]*fleetnetv1alpha1.InternalServiceExport, error) {
	selected := make(map[string]*fleetnetv1alpha1.InternalServiceExport, len(service.Status.Clusters))
	for _, cluster := range service.Status.Clusters {
		if _, duplicate := selected[cluster.Cluster]; duplicate || cluster.Cluster == "" {
			return nil, newResolutionError("InvalidExport", true, "ServiceImport has an empty or duplicate exporting cluster")
		}
		selected[cluster.Cluster] = nil
	}
	for i := range exports {
		export := &exports[i]
		source := export.Spec.ServiceReference
		if _, active := selected[source.ClusterID]; !active || export.DeletionTimestamp != nil ||
			source.Namespace != namespace || source.Name != service.Name {
			continue
		}
		if selected[source.ClusterID] != nil {
			return nil, newResolutionError("ConflictingExports", true, "multiple active exports for cluster %q", source.ClusterID)
		}
		selected[source.ClusterID] = export
	}
	return selected, nil
}

func resolveExportOrigin(namespace string, service *fleetnetv1alpha1.ServiceImport,
	export *fleetnetv1alpha1.InternalServiceExport) (gatewaymodel.Origin, error) {
	source := export.Spec.ServiceReference
	cluster := source.ClusterID
	conflict := meta.FindStatusCondition(export.Status.Conditions, string(fleetnetv1beta1.ServiceExportConflict))
	// A previous generation's success cannot authorize changed ports or source metadata.
	if export.Namespace != fmt.Sprintf(hubconfig.HubNamespaceNameFormat, cluster) || source.UID == "" ||
		source.NamespacedName != namespace+"/"+service.Name ||
		(source.Kind != "" && source.Kind != "Service") || (source.APIVersion != "" && source.APIVersion != "v1") ||
		!equality.Semantic.DeepEqual(export.Spec.Ports, service.Status.Ports) || conflict == nil ||
		conflict.Status != metav1.ConditionFalse || conflict.ObservedGeneration != export.Generation {
		return gatewaymodel.Origin{}, newResolutionError("InvalidExport", true, "cluster %q requires matching source identity, compatible ports and current unconflicted status", cluster)
	}
	memberWeight := ptr.Deref(export.Spec.Weight, 1)
	if memberWeight < 0 || memberWeight > 1000 {
		return gatewaymodel.Origin{}, newResolutionError("InvalidExport", true, "cluster %q weight is outside [0, 1000]", cluster)
	}
	if memberWeight == 0 {
		// Disabled members contribute no traffic and need no ready private origin.
		return gatewaymodel.Origin{}, nil
	}
	if export.Spec.Type != corev1.ServiceTypeLoadBalancer || !export.Spec.IsInternalLoadBalancer {
		return gatewaymodel.Origin{}, newResolutionError("UnsupportedTopology", true, "cluster %q requires an internal LoadBalancer Service", cluster)
	}
	private := export.Spec.PrivateLinkOrigin
	if private == nil || !private.Ready {
		return gatewaymodel.Origin{}, newResolutionError("PrivateOriginUnavailable", true, "cluster %q has no ready private origin discovery", cluster)
	}
	origin := gatewaymodel.Origin{
		Cluster: cluster, ServiceUID: string(source.UID), Endpoint: private.InternalIP, Weight: memberWeight,
		PrivateLinkResourceID: private.ResourceID, PrivateLinkLocation: private.Location,
	}
	if err := origin.Validate(); err != nil {
		return gatewaymodel.Origin{}, newResolutionError("InvalidExport", true, "%v", err)
	}
	return origin, nil
}

func resolutionFailure(reason string, referenceValid bool, format string, args ...any) (gatewaymodel.Backend, error) {
	return gatewaymodel.Backend{}, newResolutionError(reason, referenceValid, format, args...)
}

func newResolutionError(reason string, referenceValid bool, format string, args ...any) *ResolutionError {
	return &ResolutionError{
		Reason: reason, ReferenceValid: referenceValid, Message: fmt.Sprintf(format, args...),
	}
}
