// Package objectmeta defines shared meta const used by the networking objects.
package objectmeta

// Finalizers
const (
	// InternalServiceExportFinalizer is the finalizer InternalServiceExport controllers adds to mark that a
	// InternalServiceExport can only be deleted after both ServiceImport label and ServiceExport conflict resolution
	// result have been updated.
	InternalServiceExportFinalizer = "networking.fleet.azure.com/internal-svc-export-cleanup"
	// DerivedServiceLabel is the label added by the MCS controller, which marks the derived Service behind
	// a MCS.
	DerivedServiceLabel = "networking.fleet.azure.com/derived-service"
)

// Labels
const (
	// MultiClusterServiceLabelDerivedService is the label added by the MCS controller, which marks the
	// derived Service behind a MCS.
	MultiClusterServiceLabelDerivedService = "networking.fleet.azure.com/derived-service"
)

// Annotations
const (
	// ServiceImportAnnotationServiceInUseBy is the key of the ServiceInUseBy annotation, which marks the list
	// of member clusters importing an exported Service.
	ServiceImportAnnotationServiceInUseBy = "networking.fleet.azure.com/service-in-use-by"
)
