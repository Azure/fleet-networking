// Package objectmeta defines shared meta const used by the networking objects.
package objectmeta

// finalizers
const (
	// InternalServiceExportFinalizer is the finalizer InternalServiceExport controllers adds to mark that a
	// InternalServiceExport can only be deleted after both ServiceImport label and ServiceExport conflict resolution
	// result have been updated.
	InternalServiceExportFinalizer = "networking.fleet.azure.com/internal-svc-export-cleanup"
)
