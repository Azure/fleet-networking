/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package objectmeta defines shared meta const used by the networking objects.
package objectmeta

import "strings"

const (
	fleetNetworkingPrefix = "networking.fleet.azure.com/"
)

// Finalizers
const (
	// InternalServiceExportFinalizer is the finalizer InternalServiceExport controllers adds to mark that a
	// InternalServiceExport can only be deleted after both ServiceImport label and ServiceExport conflict resolution
	// result have been updated.
	InternalServiceExportFinalizer = fleetNetworkingPrefix + "internal-svc-export-cleanup"

	// TrafficManagerProfileFinalizer a finalizer added by the TrafficManagerProfile controller to all trafficManagerProfiles,
	// to make sure that the controller can react to profile deletions if necessary.
	TrafficManagerProfileFinalizer = fleetNetworkingPrefix + "traffic-manager-profile-cleanup"

	// TrafficManagerBackendFinalizer a finalizer added by the TrafficManagerBackend controller to all trafficManagerBackends,
	// to make sure that the controller can react to backend deletions if necessary.
	TrafficManagerBackendFinalizer = fleetNetworkingPrefix + "traffic-manager-backend-cleanup"
)

// Labels
const (
	// MultiClusterServiceLabelDerivedService is the label added by the MCS controller, which marks the
	// derived Service behind a MCS.
	MultiClusterServiceLabelDerivedService = fleetNetworkingPrefix + "derived-service"
)

// Annotations
const (
	// ServiceImportAnnotationServiceInUseBy is the key of the ServiceInUseBy annotation, which marks the list
	// of member clusters importing an exported Service.
	ServiceImportAnnotationServiceInUseBy = fleetNetworkingPrefix + "service-in-use-by"

	// ExportedObjectAnnotationUniqueName is an annotation that marks the fleet-scoped unique name assigned to
	// an exported object.
	ExportedObjectAnnotationUniqueName = fleetNetworkingPrefix + "fleet-unique-name"

	// ServiceAnnotationAzureLoadBalancerInternal is an annotation that marks the Service as an internal load balancer by cloud-provider-azure.
	ServiceAnnotationAzureLoadBalancerInternal = "service.beta.kubernetes.io/azure-load-balancer-internal"

	// ServiceAnnotationLoadBalancerResourceGroup is the annotation used on the service to specify the resource group of
	// load balancer objects that are not in the same resource group as the cluster.
	ServiceAnnotationLoadBalancerResourceGroup = "service.beta.kubernetes.io/azure-load-balancer-resource-group"
)

// Azure Resource Tags
var (
	// AzureTrafficManagerProfileTagKey is the key of the Azure Traffic Manager profile tag when the controller creates it.
	// Note: The tag name cannot have reserved characters '<,>,%,&,\\,?,/' or control characters.
	AzureTrafficManagerProfileTagKey = strings.ReplaceAll(fleetNetworkingPrefix, "/", ".") + "trafficManagerProfile"
)
