// Package objectmeta defines shared meta const used by the networking objects.
package objectmeta

// Finalizers
const (
	// InternalServiceExportFinalizer is the finalizer InternalServiceExport controllers adds to mark that a
	// InternalServiceExport can only be deleted after both ServiceImport label and ServiceExport conflict resolution
	// result have been updated.
	InternalServiceExportFinalizer = "networking.fleet.azure.com/internal-svc-export-cleanup"
)

// Labels
const (
	// MultiClusterServiceLabelDerivedService is the label added by the MCS controller, which marks the
	// derived Service behind a MCS.
	MultiClusterServiceLabelDerivedService = "networking.fleet.azure.com/derived-service"
	// MultiClusterServiceLabelServiceImport is the label added by the MCS controller, which marks the
	//  ServiceImport created by the MCS controller.
	MultiClusterServiceLabelServiceImport = "networking.fleet.azure.com/service-import"
)

// Annotations
const (
	// ServiceImportAnnotationServiceInUseBy is the key of the ServiceInUseBy annotation, which marks the list
	// of member clusters importing an exported Service.
	ServiceImportAnnotationServiceInUseBy = "networking.fleet.azure.com/service-in-use-by"
	// EndpointSliceAnnotationUniqueName is an annotation that marks the fleet-scoped unique name assigned to
	// an EndpointSlice.
	EndpointSliceAnnotationUniqueName = "networking.fleet.azure.com/fleet-unique-name"
)
