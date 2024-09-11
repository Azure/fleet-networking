/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package objectmeta defines shared meta const used by the networking objects.
package objectmeta

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
)

// Azure Resource Tags
const (
	// AzureTrafficManagerProfileTagKey is the key of the Azure Traffic Manager profile tag when the controller creates it.
	AzureTrafficManagerProfileTagKey = fleetNetworkingPrefix + "trafficManagerProfile"
)
