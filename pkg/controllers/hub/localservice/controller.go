/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package localservice provisions private origins for explicitly referenced
// cluster-local Services without changing their cluster-local behavior.
package localservice

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/netip"
	"reflect"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/common/privateorigin"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewayresolver"
)

const (
	managedLabel    = objectmeta.LocalOriginManagedLabel
	sourceNameLabel = objectmeta.LocalOriginSourceNameLabel
	sourceUIDLabel  = objectmeta.LocalOriginSourceUIDLabel
	managedBy       = objectmeta.LocalOriginManager
	plsCreate       = "service.beta.kubernetes.io/azure-pls-create"
	pollInterval    = 2 * time.Minute
)

// Reconciler requires an uncached APIReader for both intent and ownership checks.
// Discovery is read-only Azure discovery shared with member ServiceExport.
type Reconciler struct {
	Client    client.Client
	APIReader client.Reader
	Discovery privateorigin.Discoverer
}

func failure(reason string, valid bool, message string) error {
	return &gatewayresolver.ResolutionError{Reason: reason, ReferenceValid: valid, Message: message}
}

func localReference(namespace string, ref gatewayv1.HTTPBackendRef) error {
	if ptr.Deref(ref.Group, "") != "" || ptr.Deref(ref.Kind, "Service") != "Service" {
		return failure("InvalidKind", false, "expected a core Service backend")
	}
	if string(ptr.Deref(ref.Namespace, gatewayv1.Namespace(namespace))) != namespace {
		return failure("RefNotPermitted", false, "local Service must be in the HTTPRoute namespace")
	}
	if len(ref.Filters) != 0 || ptr.Deref(ref.Weight, 1) < 1 || ptr.Deref(ref.Weight, 1) > 1000000 {
		return failure("UnsupportedValue", false, "local Service requires a positive weight and no filters")
	}
	return nil
}

func selectedPort(service *corev1.Service, number *gatewayv1.PortNumber) (corev1.ServicePort, error) {
	if number == nil || *number < 1 || *number > 65535 {
		return corev1.ServicePort{}, failure("PortNotFound", false, "a valid Service port is required")
	}
	var selected *corev1.ServicePort
	for i := range service.Spec.Ports {
		port := &service.Spec.Ports[i]
		if port.Port != int32(*number) || (port.Protocol != "" && port.Protocol != corev1.ProtocolTCP) {
			continue
		}
		if selected != nil {
			return corev1.ServicePort{}, failure("PortNotFound", false, "ambiguous Service TCP port")
		}
		selected = port
	}
	if selected == nil || (ptr.Deref(selected.AppProtocol, "") != "" && *selected.AppProtocol != "http") {
		return corev1.ServicePort{}, failure("PortNotFound", false, "Service requires a matching TCP HTTP port")
	}
	return *selected, nil
}

func sourceAvailable(service *corev1.Service) bool {
	return service.UID != "" && service.DeletionTimestamp == nil && service.Labels[managedLabel] == "" &&
		(service.Spec.Type == corev1.ServiceTypeClusterIP || service.Spec.Type == "")
}

func accepted(conditions []metav1.Condition, generation int64) bool {
	condition := meta.FindStatusCondition(conditions, "Accepted")
	return condition != nil && condition.Status == metav1.ConditionTrue && condition.ObservedGeneration == generation
}

func rootRoute(route *gatewayv1.HTTPRoute) bool {
	if route.DeletionTimestamp != nil || gatewayresolver.ValidateHTTPRoute(route) != nil {
		return false
	}
	for _, rule := range route.Spec.Rules {
		for _, match := range rule.Matches {
			if match.Path != nil && (ptr.Deref(match.Path.Value, "/") != "/" ||
				ptr.Deref(match.Path.Type, gatewayv1.PathMatchPathPrefix) != gatewayv1.PathMatchPathPrefix) {
				return false
			}
		}
	}
	return true
}

// activePort validates the entire route before exposing any of its Services.
// ponytail: scan same-namespace routes; add field indexes if scale warrants it.
func (r *Reconciler) activePort(ctx context.Context, source *corev1.Service) (*corev1.ServicePort, error) {
	var routes gatewayv1.HTTPRouteList
	if err := r.APIReader.List(ctx, &routes, client.InNamespace(source.Namespace)); err != nil {
		return nil, err
	}
	var namespace corev1.Namespace
	if err := r.APIReader.Get(ctx, client.ObjectKey{Name: source.Namespace}, &namespace); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	if namespace.DeletionTimestamp != nil {
		return nil, nil
	}
	var selected *corev1.ServicePort
	for i := range routes.Items {
		route := &routes.Items[i]
		if !rootRoute(route) {
			continue
		}
		attached := false
		for _, parent := range route.Spec.ParentRefs {
			key, recognized := gatewayresolver.GatewayParentKey(route.Namespace, parent)
			if !recognized {
				continue
			}
			var gateway gatewayv1.Gateway
			if err := r.APIReader.Get(ctx, key, &gateway); err != nil {
				if apierrors.IsNotFound(err) {
					continue
				}
				return nil, err
			}
			if gateway.DeletionTimestamp != nil || !accepted(gateway.Status.Conditions, gateway.Generation) {
				continue
			}
			var class gatewayv1.GatewayClass
			if err := r.APIReader.Get(ctx, client.ObjectKey{Name: string(gateway.Spec.GatewayClassName)}, &class); err != nil {
				if apierrors.IsNotFound(err) {
					continue
				}
				return nil, err
			}
			if class.DeletionTimestamp != nil || class.Spec.ParametersRef != nil ||
				class.Spec.ControllerName != gatewayv1.GatewayController(objectmeta.GatewayAPIControllerName) ||
				!accepted(class.Status.Conditions, class.Generation) {
				continue
			}
			attachments, err := gatewayresolver.ResolveAttachments(&gateway, route, parent, namespace.Labels)
			if err == nil && len(attachments) != 0 {
				attached = true
			}
		}
		if !attached {
			continue
		}
		valid := true
		var ports []corev1.ServicePort
		for _, rule := range route.Spec.Rules {
			ref := rule.BackendRefs[0]
			if ptr.Deref(ref.Group, "") == "" && ptr.Deref(ref.Kind, "Service") == "Service" {
				if localReference(route.Namespace, ref) != nil {
					valid = false
					break
				}
				var target corev1.Service
				if err := r.APIReader.Get(ctx, client.ObjectKey{Namespace: route.Namespace, Name: string(ref.Name)}, &target); err != nil {
					if apierrors.IsNotFound(err) {
						valid = false
						break
					}
					return nil, err
				}
				port, err := selectedPort(&target, ref.Port)
				if !sourceAvailable(&target) || err != nil {
					valid = false
					break
				}
				if target.UID == source.UID && target.Name == source.Name {
					ports = append(ports, port)
				}
			} else {
				if gatewayresolver.AuthorizeBackendReference(route.Namespace, ref) != nil {
					valid = false
					break
				}
				var target fleetnetv1alpha1.ServiceImport
				if err := r.APIReader.Get(ctx, client.ObjectKey{Namespace: route.Namespace, Name: string(ref.Name)}, &target); err != nil {
					if apierrors.IsNotFound(err) {
						valid = false
						break
					}
					return nil, err
				}
				_, err := gatewayresolver.Resolve(route.Namespace, ref, &target, nil)
				if resolution, ok := err.(*gatewayresolver.ResolutionError); err != nil &&
					(!ok || !resolution.ReferenceValid || resolution.Reason != "BackendUnavailable") {
					valid = false
					break
				}
			}
		}
		if !valid {
			continue
		}
		for i := range ports {
			if selected != nil && !reflect.DeepEqual(*selected, ports[i]) {
				return nil, failure("ConflictingPorts", true, "local Service is referenced on multiple ports")
			}
			port := ports[i]
			selected = &port
		}
	}
	return selected, nil
}

// CompanionName is the deterministic CCM Service/PLS name for one source UID
// and selected Service port. It is also suitable for Private Link approval tools.
func CompanionName(source *corev1.Service, port corev1.ServicePort) string {
	return fmt.Sprintf("fleet-local-%x", sha256.Sum256([]byte(fmt.Sprintf("%s/%d", source.UID, port.Port))))[:44]
}

func labelsFor(source *corev1.Service) map[string]string {
	return map[string]string{managedLabel: managedBy, sourceNameLabel: source.Name, sourceUIDLabel: string(source.UID)}
}

func owner(object client.Object, kind, name string, uid types.UID) bool {
	reference := metav1.GetControllerOf(object)
	return reference != nil && reference.APIVersion == "v1" && reference.Kind == kind &&
		reference.Name == name && reference.UID == uid
}

func companionOwned(service *corev1.Service, sourceName string) bool {
	uid := types.UID(service.Labels[sourceUIDLabel])
	return uid != "" && service.Labels[managedLabel] == managedBy && service.Labels[sourceNameLabel] == sourceName &&
		owner(service, "Service", sourceName, uid)
}

func desiredCompanion(source *corev1.Service, port corev1.ServicePort) *corev1.Service {
	name := CompanionName(source, port)
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: source.Namespace, Name: name, Labels: labelsFor(source),
			Annotations: map[string]string{objectmeta.ServiceAnnotationAzureLoadBalancerInternal: "true",
				plsCreate: "true", objectmeta.ServiceAnnotationPLSName: name},
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(source, corev1.SchemeGroupVersion.WithKind("Service"))}},
		Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer, IPFamilies: []corev1.IPFamily{corev1.IPv4Protocol},
			IPFamilyPolicy:                ptr.To(corev1.IPFamilyPolicySingleStack),
			ExternalTrafficPolicy:         corev1.ServiceExternalTrafficPolicyCluster,
			InternalTrafficPolicy:         ptr.To(corev1.ServiceInternalTrafficPolicyCluster),
			AllocateLoadBalancerNodePorts: ptr.To(true), SessionAffinity: corev1.ServiceAffinityNone,
			Ports: []corev1.ServicePort{{Name: port.Name, Protocol: corev1.ProtocolTCP, Port: port.Port,
				TargetPort: intstr.FromInt32(port.Port), AppProtocol: port.AppProtocol}}},
	}
}

func companionValid(companion, source *corev1.Service, port corev1.ServicePort) bool {
	expected := desiredCompanion(source, port)
	for key, value := range companion.Annotations {
		if strings.HasPrefix(key, "service.beta.kubernetes.io/azure-") && expected.Annotations[key] != value {
			return false
		}
	}
	return companion.DeletionTimestamp == nil && companionOwned(companion, source.Name) &&
		companion.Labels[sourceUIDLabel] == string(source.UID) && companion.Name == expected.Name &&
		companion.Spec.Type == corev1.ServiceTypeLoadBalancer && len(companion.Spec.Selector) == 0 &&
		len(companion.Spec.Ports) == 1 && companion.Spec.Ports[0].Port == port.Port &&
		companion.Spec.Ports[0].Name == port.Name && companion.Spec.Ports[0].Protocol == corev1.ProtocolTCP &&
		companion.Spec.Ports[0].TargetPort == expected.Spec.Ports[0].TargetPort &&
		reflect.DeepEqual(companion.Spec.Ports[0].AppProtocol, port.AppProtocol) &&
		companion.Spec.LoadBalancerIP == "" && len(companion.Spec.ExternalIPs) == 0 &&
		companion.Spec.LoadBalancerClass == nil &&
		companion.Spec.ExternalTrafficPolicy == expected.Spec.ExternalTrafficPolicy &&
		ptr.Deref(companion.Spec.InternalTrafficPolicy, corev1.ServiceInternalTrafficPolicyCluster) == corev1.ServiceInternalTrafficPolicyCluster &&
		ptr.Deref(companion.Spec.AllocateLoadBalancerNodePorts, true) &&
		(companion.Spec.SessionAffinity == "" || companion.Spec.SessionAffinity == corev1.ServiceAffinityNone) &&
		companion.Spec.SessionAffinityConfig == nil && companion.Spec.HealthCheckNodePort == 0 &&
		len(companion.Spec.LoadBalancerSourceRanges) == 0 && companion.Spec.TrafficDistribution == nil &&
		companion.Spec.ExternalName == "" && !companion.Spec.PublishNotReadyAddresses &&
		reflect.DeepEqual(companion.Spec.IPFamilies, expected.Spec.IPFamilies) &&
		ptr.Deref(companion.Spec.IPFamilyPolicy, corev1.IPFamilyPolicySingleStack) == corev1.IPFamilyPolicySingleStack &&
		companion.Annotations[objectmeta.ServiceAnnotationAzureLoadBalancerInternal] == "true" &&
		companion.Annotations[plsCreate] == "true" && companion.Annotations[objectmeta.ServiceAnnotationPLSName] == expected.Name
}

// guard rechecks current source identity and route authorization before writes.
func (r *Reconciler) guard(ctx context.Context, source *corev1.Service, port corev1.ServicePort) error {
	var current corev1.Service
	if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(source), &current); err != nil {
		return err
	}
	if current.UID != source.UID || current.ResourceVersion != source.ResourceVersion || !sourceAvailable(&current) {
		return fmt.Errorf("source Service changed before private origin mutation")
	}
	active, err := r.activePort(ctx, &current)
	if err != nil {
		return err
	}
	if active == nil || !reflect.DeepEqual(*active, port) {
		return fmt.Errorf("local Service authorization changed before mutation")
	}
	return nil
}

func (r *Reconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	if r.APIReader == nil || r.Client == nil {
		return ctrl.Result{}, fmt.Errorf("localservice requires Client and uncached APIReader")
	}
	var source corev1.Service
	err := r.APIReader.Get(ctx, request.NamespacedName, &source)
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	if source.Labels[managedLabel] != "" {
		return ctrl.Result{}, nil
	}
	var port *corev1.ServicePort
	var intentErr error
	if err == nil && sourceAvailable(&source) {
		port, intentErr = r.activePort(ctx, &source)
	}
	keep := ""
	if port != nil && intentErr == nil {
		keep = CompanionName(&source, *port)
	}
	pending, cleanupErr := r.cleanup(ctx, request.NamespacedName, keep)
	if cleanupErr != nil {
		return ctrl.Result{}, cleanupErr
	}
	if intentErr != nil {
		return ctrl.Result{}, intentErr
	}
	if port == nil {
		if pending {
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
		return ctrl.Result{}, nil
	}
	if pending {
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}
	expected := desiredCompanion(&source, *port)
	var companion corev1.Service
	err = r.APIReader.Get(ctx, client.ObjectKeyFromObject(expected), &companion)
	if apierrors.IsNotFound(err) {
		if err := r.guard(ctx, &source, *port); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Client.Create(ctx, expected); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}
	if err != nil {
		return ctrl.Result{}, err
	}
	if !companionOwned(&companion, source.Name) || companion.Labels[sourceUIDLabel] != string(source.UID) {
		return ctrl.Result{}, fmt.Errorf("companion Service %s is not owned by source UID %s", companion.Name, source.UID)
	}
	if companion.DeletionTimestamp != nil {
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}
	if !companionValid(&companion, &source, *port) {
		if err := r.guard(ctx, &source, *port); err != nil {
			return ctrl.Result{}, err
		}
		// Preserve CCM finalizers, allocated cluster/node IPs, and status.
		companion.Spec.Type = expected.Spec.Type
		companion.Spec.Selector = nil
		companion.Spec.LoadBalancerClass = nil
		companion.Spec.LoadBalancerIP = ""
		companion.Spec.ExternalIPs = nil
		companion.Spec.IPFamilies = expected.Spec.IPFamilies
		companion.Spec.IPFamilyPolicy = expected.Spec.IPFamilyPolicy
		companion.Spec.ExternalTrafficPolicy = expected.Spec.ExternalTrafficPolicy
		companion.Spec.InternalTrafficPolicy = expected.Spec.InternalTrafficPolicy
		companion.Spec.AllocateLoadBalancerNodePorts = expected.Spec.AllocateLoadBalancerNodePorts
		companion.Spec.SessionAffinity = expected.Spec.SessionAffinity
		companion.Spec.SessionAffinityConfig = nil
		companion.Spec.HealthCheckNodePort = 0
		companion.Spec.LoadBalancerSourceRanges = nil
		companion.Spec.TrafficDistribution = nil
		companion.Spec.ExternalName = ""
		companion.Spec.PublishNotReadyAddresses = false
		nodePort := int32(0)
		if len(companion.Spec.Ports) == 1 {
			nodePort = companion.Spec.Ports[0].NodePort
		}
		companion.Spec.Ports = expected.Spec.Ports
		companion.Spec.Ports[0].NodePort = nodePort
		if companion.Annotations == nil {
			companion.Annotations = map[string]string{}
		}
		for key := range companion.Annotations {
			if strings.HasPrefix(key, "service.beta.kubernetes.io/azure-") {
				delete(companion.Annotations, key)
			}
		}
		for key, value := range expected.Annotations {
			companion.Annotations[key] = value
		}
		if err := r.Client.Update(ctx, &companion); err != nil {
			return ctrl.Result{}, err
		}
	}
	if err := r.syncSlices(ctx, &source, *port, &companion); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: pollInterval}, nil
}

func deleteCurrent(ctx context.Context, writer client.Client, object client.Object) error {
	uid, version := object.GetUID(), object.GetResourceVersion()
	err := writer.Delete(ctx, object, &client.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &version}})
	return client.IgnoreNotFound(err)
}

// cleanup leaves CCM finalizers intact and reports pending deletion explicitly.
func (r *Reconciler) cleanup(ctx context.Context, source client.ObjectKey, keep string) (bool, error) {
	var companions corev1.ServiceList
	if err := r.APIReader.List(ctx, &companions, client.InNamespace(source.Namespace),
		client.MatchingLabels{managedLabel: managedBy, sourceNameLabel: source.Name}); err != nil {
		return false, err
	}
	pending := false
	for i := range companions.Items {
		candidate := &companions.Items[i]
		if candidate.Name == keep {
			continue
		}
		if !companionOwned(candidate, source.Name) {
			return false, fmt.Errorf("refusing cleanup of foreign companion %s", candidate.Name)
		}
		var current corev1.Service
		if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(candidate), &current); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return false, err
		}
		if current.UID != candidate.UID || !companionOwned(&current, source.Name) {
			return false, fmt.Errorf("companion ownership changed before cleanup")
		}
		// Re-evaluate intent before withdrawing a companion; never delete a
		// currently referenced object based on an older route observation.
		var live corev1.Service
		if err := r.APIReader.Get(ctx, source, &live); err == nil && sourceAvailable(&live) {
			active, err := r.activePort(ctx, &live)
			if err == nil && active != nil && CompanionName(&live, *active) == current.Name {
				continue
			}
			if err != nil {
				if resolution, ok := err.(*gatewayresolver.ResolutionError); !ok || resolution.Reason != "ConflictingPorts" {
					return false, err
				}
			}
		} else if err != nil && !apierrors.IsNotFound(err) {
			return false, err
		}
		if err := r.removeSlices(ctx, &current); err != nil {
			return false, err
		}
		if current.DeletionTimestamp == nil {
			if err := r.guardCleanup(ctx, &current); err != nil {
				return false, err
			}
			if err := deleteCurrent(ctx, r.Client, &current); err != nil {
				return false, err
			}
		}
		// Always revisit after delete: CCM may still be cleaning up Azure.
		pending = true
	}
	return pending, nil
}

func (r *Reconciler) guardCleanup(ctx context.Context, companion *corev1.Service) error {
	var current corev1.Service
	if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(companion), &current); err != nil {
		return err
	}
	name := companion.Labels[sourceNameLabel]
	if current.UID != companion.UID || current.ResourceVersion != companion.ResourceVersion || !companionOwned(&current, name) {
		return fmt.Errorf("companion changed before cleanup mutation")
	}
	var source corev1.Service
	if err := r.APIReader.Get(ctx, client.ObjectKey{Namespace: companion.Namespace, Name: name}, &source); err != nil {
		return client.IgnoreNotFound(err)
	}
	if !sourceAvailable(&source) {
		return nil
	}
	port, err := r.activePort(ctx, &source)
	if err != nil {
		if resolution, ok := err.(*gatewayresolver.ResolutionError); ok && resolution.Reason == "ConflictingPorts" {
			return nil
		}
		return err
	}
	if port != nil && CompanionName(&source, *port) == companion.Name {
		return fmt.Errorf("companion became referenced before cleanup mutation")
	}
	return nil
}

func (r *Reconciler) removeSlices(ctx context.Context, companion *corev1.Service) error {
	var slices discoveryv1.EndpointSliceList
	if err := r.APIReader.List(ctx, &slices, client.InNamespace(companion.Namespace),
		client.MatchingLabels{discoveryv1.LabelServiceName: companion.Name}); err != nil {
		return err
	}
	for i := range slices.Items {
		var current discoveryv1.EndpointSlice
		if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(&slices.Items[i]), &current); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return err
		}
		if !owner(&current, "Service", companion.Name, companion.UID) || current.Labels[discoveryv1.LabelManagedBy] != managedBy {
			return fmt.Errorf("refusing cleanup of foreign EndpointSlice %s", current.Name)
		}
		if err := r.guardCleanup(ctx, companion); err != nil {
			return err
		}
		if err := deleteCurrent(ctx, r.Client, &current); err != nil {
			return err
		}
	}
	return nil
}

func mirroredSlice(source *corev1.Service, port corev1.ServicePort, companion *corev1.Service,
	slice *discoveryv1.EndpointSlice) *discoveryv1.EndpointSlice {
	if slice.DeletionTimestamp != nil || slice.AddressType != discoveryv1.AddressTypeIPv4 ||
		slice.Labels[discoveryv1.LabelManagedBy] == managedBy {
		return nil
	}
	if reference := metav1.GetControllerOf(slice); reference != nil && reference.Kind == "Service" &&
		(reference.Name != source.Name || reference.UID != source.UID || reference.APIVersion != "v1") {
		return nil
	}
	var matching *discoveryv1.EndpointPort
	for i := range slice.Ports {
		candidate := &slice.Ports[i]
		if ptr.Deref(candidate.Name, "") != port.Name || ptr.Deref(candidate.Protocol, corev1.ProtocolTCP) != corev1.ProtocolTCP ||
			candidate.Port == nil || *candidate.Port < 1 || *candidate.Port > 65535 {
			continue
		}
		if matching != nil {
			return nil
		}
		matching = candidate
	}
	if matching == nil {
		return nil
	}
	result := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{Namespace: source.Namespace,
			Name:            fmt.Sprintf("%s-%x", companion.Name, sha256.Sum256([]byte(slice.Name+"/"+string(slice.UID))))[:63],
			Labels:          labelsFor(source),
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(companion, corev1.SchemeGroupVersion.WithKind("Service"))}},
		AddressType: discoveryv1.AddressTypeIPv4,
		Ports: []discoveryv1.EndpointPort{{Name: ptr.To(port.Name), Port: ptr.To(*matching.Port),
			Protocol: ptr.To(corev1.ProtocolTCP), AppProtocol: port.AppProtocol}},
	}
	result.Labels[discoveryv1.LabelServiceName] = companion.Name
	result.Labels[discoveryv1.LabelManagedBy] = managedBy
	for _, endpoint := range slice.Endpoints {
		if !ptr.Deref(endpoint.Conditions.Ready, true) || ptr.Deref(endpoint.Conditions.Terminating, false) {
			continue
		}
		addresses := []string{}
		for _, raw := range endpoint.Addresses {
			address, err := netip.ParseAddr(raw)
			if err == nil && address.Is4() && address.IsGlobalUnicast() {
				addresses = append(addresses, address.String())
			}
		}
		if len(addresses) != 0 {
			result.Endpoints = append(result.Endpoints, discoveryv1.Endpoint{Addresses: addresses,
				Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)}, NodeName: endpoint.NodeName, Zone: endpoint.Zone})
		}
	}
	if len(result.Endpoints) == 0 {
		return nil
	}
	return result
}

func (r *Reconciler) syncSlices(ctx context.Context, source *corev1.Service, port corev1.ServicePort, companion *corev1.Service) error {
	var sources, existing discoveryv1.EndpointSliceList
	if err := r.APIReader.List(ctx, &sources, client.InNamespace(source.Namespace),
		client.MatchingLabels{discoveryv1.LabelServiceName: source.Name}); err != nil {
		return err
	}
	desired := map[string]*discoveryv1.EndpointSlice{}
	observed := map[string]*discoveryv1.EndpointSlice{}
	for i := range sources.Items {
		slice := mirroredSlice(source, port, companion, &sources.Items[i])
		if slice != nil {
			desired[slice.Name] = slice
			observed[slice.Name] = &sources.Items[i]
		}
	}
	if err := r.APIReader.List(ctx, &existing, client.InNamespace(source.Namespace),
		client.MatchingLabels{discoveryv1.LabelServiceName: companion.Name}); err != nil {
		return err
	}
	for i := range existing.Items {
		if !owner(&existing.Items[i], "Service", companion.Name, companion.UID) ||
			existing.Items[i].Labels[discoveryv1.LabelManagedBy] != managedBy {
			return fmt.Errorf("foreign companion EndpointSlice %s", existing.Items[i].Name)
		}
		if desired[existing.Items[i].Name] != nil {
			continue
		}
		if err := r.guard(ctx, source, port); err != nil {
			return err
		}
		var current discoveryv1.EndpointSlice
		if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(&existing.Items[i]), &current); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return err
		}
		if current.UID != existing.Items[i].UID || !owner(&current, "Service", companion.Name, companion.UID) ||
			current.Labels[discoveryv1.LabelManagedBy] != managedBy || current.Labels[discoveryv1.LabelServiceName] != companion.Name {
			return fmt.Errorf("EndpointSlice ownership changed")
		}
		if err := deleteCurrent(ctx, r.Client, &current); err != nil {
			return err
		}
	}
	for _, wanted := range desired {
		if err := r.guard(ctx, source, port); err != nil {
			return err
		}
		var liveSourceSlice discoveryv1.EndpointSlice
		original := observed[wanted.Name]
		if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(original), &liveSourceSlice); err != nil {
			return err
		}
		if original.UID != liveSourceSlice.UID || original.ResourceVersion != liveSourceSlice.ResourceVersion {
			return fmt.Errorf("source EndpointSlice changed before mutation")
		}
		var liveCompanion corev1.Service
		if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(companion), &liveCompanion); err != nil {
			return err
		}
		if liveCompanion.UID != companion.UID || !companionValid(&liveCompanion, source, port) {
			return fmt.Errorf("companion changed before EndpointSlice mutation")
		}
		var current discoveryv1.EndpointSlice
		err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(wanted), &current)
		if apierrors.IsNotFound(err) {
			if err := r.Client.Create(ctx, wanted); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if !owner(&current, "Service", companion.Name, companion.UID) || current.Labels[discoveryv1.LabelManagedBy] != managedBy {
			return fmt.Errorf("EndpointSlice name collision %s", current.Name)
		}
		if current.DeletionTimestamp != nil {
			return fmt.Errorf("EndpointSlice %s is deleting", current.Name)
		}
		if current.AddressType != wanted.AddressType {
			if err := deleteCurrent(ctx, r.Client, &current); err != nil {
				return err
			}
			return fmt.Errorf("recreating companion EndpointSlice %s with changed address type", current.Name)
		}
		if reflect.DeepEqual(current.Endpoints, wanted.Endpoints) && reflect.DeepEqual(current.Ports, wanted.Ports) &&
			reflect.DeepEqual(current.Labels, wanted.Labels) {
			continue
		}
		current.Endpoints, current.Ports, current.AddressType = wanted.Endpoints, wanted.Ports, wanted.AddressType
		current.Labels = wanted.Labels
		if err := r.Client.Update(ctx, &current); err != nil {
			return err
		}
	}
	return nil
}

// ResolveLocalService only reads current intent, companion state, and Azure.
// A ServiceExport is neither read nor required.
func (r *Reconciler) ResolveLocalService(ctx context.Context, namespace string, ref gatewayv1.HTTPBackendRef) (gatewaymodel.Backend, error) {
	empty := gatewaymodel.Backend{}
	if err := localReference(namespace, ref); err != nil {
		return empty, err
	}
	if r.APIReader == nil {
		return empty, fmt.Errorf("localservice requires uncached APIReader")
	}
	var source corev1.Service
	if err := r.APIReader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: string(ref.Name)}, &source); err != nil {
		if apierrors.IsNotFound(err) {
			return empty, failure("BackendNotFound", false, "Service is unavailable")
		}
		return empty, err
	}
	if source.DeletionTimestamp != nil {
		return empty, failure("BackendNotFound", false, "Service is deleting")
	}
	port, err := selectedPort(&source, ref.Port)
	if err != nil {
		return empty, err
	}
	unavailable := func(message string) (gatewaymodel.Backend, error) {
		return empty, failure("BackendUnavailable", true, message)
	}
	if !sourceAvailable(&source) {
		return unavailable("local origin requires an unmanaged ClusterIP or headless Service")
	}
	active, err := r.activePort(ctx, &source)
	if err != nil {
		return empty, err
	}
	if active == nil || !reflect.DeepEqual(*active, port) {
		return unavailable("no eligible local Service route")
	}
	var companion corev1.Service
	if err := r.APIReader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: CompanionName(&source, port)}, &companion); err != nil {
		if apierrors.IsNotFound(err) {
			return unavailable("waiting for companion LoadBalancer")
		}
		return empty, err
	}
	if !companionValid(&companion, &source, port) {
		return unavailable("companion ownership or configuration is invalid")
	}
	if err := r.readyMirrors(ctx, &source, port, &companion); err != nil {
		return unavailable(err.Error())
	}
	if r.Discovery == nil {
		return unavailable("private origin discovery is not configured")
	}
	snapshot, err := r.Discovery.Discover(ctx, &companion)
	if err != nil {
		return unavailable(fmt.Sprintf("private origin discovery failed: %v", err))
	}
	if snapshot == nil || !snapshot.Ready {
		reason := "no discovery snapshot"
		if snapshot != nil {
			reason = snapshot.Reason
		}
		return unavailable("waiting for verified internal LoadBalancer and PLS: " + reason)
	}
	if err := r.readyMirrors(ctx, &source, port, &companion); err != nil {
		return unavailable(err.Error())
	}
	if err := r.guard(ctx, &source, port); err != nil {
		return unavailable(err.Error())
	}
	var current corev1.Service
	if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(&companion), &current); err != nil {
		return unavailable("companion changed during discovery")
	}
	if current.UID != companion.UID || current.ResourceVersion != companion.ResourceVersion || !companionValid(&current, &source, port) {
		return unavailable("companion changed during discovery")
	}
	origin := gatewaymodel.Origin{Cluster: "local", ServiceUID: string(source.UID), Endpoint: snapshot.InternalIP, Weight: 1,
		PrivateLinkResourceID: snapshot.ResourceID, PrivateLinkLocation: snapshot.Location}
	if err := origin.Validate(); err != nil {
		return unavailable(err.Error())
	}
	return gatewaymodel.Backend{Kind: "Service", Namespace: namespace, Name: source.Name, Port: port.Port,
		RouteWeight: ptr.Deref(ref.Weight, 1), HealthProbe: gatewaymodel.HTTPPreviewHealthProbe(), Origins: []gatewaymodel.Origin{origin}}, nil
}

// readyMirrors prevents resolution while a withdrawal or endpoint change has
// reached the source but not the companion.
func (r *Reconciler) readyMirrors(ctx context.Context, source *corev1.Service, port corev1.ServicePort, companion *corev1.Service) error {
	var sources, mirrors discoveryv1.EndpointSliceList
	if err := r.APIReader.List(ctx, &sources, client.InNamespace(source.Namespace),
		client.MatchingLabels{discoveryv1.LabelServiceName: source.Name}); err != nil {
		return err
	}
	expected := map[string]*discoveryv1.EndpointSlice{}
	for i := range sources.Items {
		if wanted := mirroredSlice(source, port, companion, &sources.Items[i]); wanted != nil {
			expected[wanted.Name] = wanted
		}
	}
	if len(expected) == 0 {
		return fmt.Errorf("Service has no ready IPv4 endpoints")
	}
	if err := r.APIReader.List(ctx, &mirrors, client.InNamespace(source.Namespace),
		client.MatchingLabels{discoveryv1.LabelServiceName: companion.Name}); err != nil {
		return err
	}
	if len(mirrors.Items) != len(expected) {
		return fmt.Errorf("waiting for current companion EndpointSlices")
	}
	for i := range mirrors.Items {
		mirror := &mirrors.Items[i]
		wanted := expected[mirror.Name]
		if wanted == nil || mirror.DeletionTimestamp != nil || !owner(mirror, "Service", companion.Name, companion.UID) ||
			mirror.Labels[discoveryv1.LabelManagedBy] != managedBy || mirror.AddressType != wanted.AddressType ||
			!reflect.DeepEqual(mirror.Ports, wanted.Ports) || !reflect.DeepEqual(mirror.Endpoints, wanted.Endpoints) {
			return fmt.Errorf("companion EndpointSlices are stale or unowned")
		}
		var current discoveryv1.EndpointSlice
		if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(mirror), &current); err != nil {
			return err
		}
		if current.UID != mirror.UID || current.ResourceVersion != mirror.ResourceVersion {
			return fmt.Errorf("companion EndpointSlice changed during readiness checks")
		}
	}
	// Check the source set again after comparing mirrors: draining or removal
	// can race the two list operations even without an Azure discovery delay.
	var currentSources discoveryv1.EndpointSliceList
	if err := r.APIReader.List(ctx, &currentSources, client.InNamespace(source.Namespace),
		client.MatchingLabels{discoveryv1.LabelServiceName: source.Name}); err != nil {
		return err
	}
	if len(currentSources.Items) != len(sources.Items) {
		return fmt.Errorf("source EndpointSlices changed during readiness checks")
	}
	versions := map[client.ObjectKey]string{}
	for i := range sources.Items {
		slice := &sources.Items[i]
		versions[client.ObjectKeyFromObject(slice)] = string(slice.UID) + "/" + slice.ResourceVersion
	}
	for i := range currentSources.Items {
		slice := &currentSources.Items[i]
		if versions[client.ObjectKeyFromObject(slice)] != string(slice.UID)+"/"+slice.ResourceVersion {
			return fmt.Errorf("source EndpointSlice changed during readiness checks")
		}
	}
	return nil
}

func (r *Reconciler) SetupWithManager(manager ctrl.Manager) error {
	if r.Client == nil {
		r.Client = manager.GetClient()
	}
	if r.APIReader == nil {
		r.APIReader = manager.GetAPIReader()
	}
	return ctrl.NewControllerManagedBy(manager).Named("local-service-origin").For(&corev1.Service{}).
		Watches(&corev1.Service{}, handler.EnqueueRequestsFromMapFunc(r.sourcesForService)).
		Watches(&discoveryv1.EndpointSlice{}, handler.EnqueueRequestsFromMapFunc(r.sourcesForSlice)).
		Watches(&gatewayv1.HTTPRoute{}, handler.EnqueueRequestsFromMapFunc(r.allSources)).
		Watches(&gatewayv1.Gateway{}, handler.EnqueueRequestsFromMapFunc(r.allSources)).
		Watches(&gatewayv1.GatewayClass{}, handler.EnqueueRequestsFromMapFunc(r.allSources)).
		Watches(&corev1.Namespace{}, handler.EnqueueRequestsFromMapFunc(r.allSources)).
		Complete(r)
}

func (r *Reconciler) sourcesForService(_ context.Context, object client.Object) []reconcile.Request {
	name := object.GetName()
	if object.GetLabels()[managedLabel] == managedBy {
		name = object.GetLabels()[sourceNameLabel]
	}
	if name == "" {
		return nil
	}
	return []reconcile.Request{{NamespacedName: client.ObjectKey{Namespace: object.GetNamespace(), Name: name}}}
}

func (r *Reconciler) sourcesForSlice(_ context.Context, object client.Object) []reconcile.Request {
	if object.GetLabels()[discoveryv1.LabelManagedBy] == managedBy {
		return nil
	}
	name := object.GetLabels()[discoveryv1.LabelServiceName]
	if name == "" {
		return nil
	}
	return []reconcile.Request{{NamespacedName: client.ObjectKey{Namespace: object.GetNamespace(), Name: name}}}
}

func (r *Reconciler) allSources(ctx context.Context, _ client.Object) []reconcile.Request {
	var sources corev1.ServiceList
	if err := r.APIReader.List(ctx, &sources); err != nil {
		ctrl.LoggerFrom(ctx).Error(err, "list local origin sources")
		return nil
	}
	result := []reconcile.Request{}
	for _, source := range sources.Items {
		if source.Labels[managedLabel] == "" {
			result = append(result, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&source)})
		}
	}
	return result
}
