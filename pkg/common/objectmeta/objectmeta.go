// Package objectmeta defines shared meta const used by the networking objects.
package objectmeta

// finalizers
const (
	// InternalServiceExportFinalizer is the finalizer InternalServiceExport controllers adds to mark that a
	// InternalServiceExport can only be deleted after both ServiceImport label and ServiceExport conflict resolution
	// result have been updated.
	InternalServiceExportFinalizer = "networking.fleet.azure.com/internal-svc-export-cleanup"
	// ServiceInUseByAnnotationKey is the key of the ServiceInUseBy annotation, which marks the list
	// of member clusters importing an exported Service.
	ServiceInUseByAnnotationKey = "networking.fleet.azure.com/service-in-use-by"
	// DerivedServiceLabel is the label added by the MCS controller, which marks the derived Service behind
	// a MCS.
	DerivedServiceLabel = "networking.fleet.azure.com/derived-service"
)
