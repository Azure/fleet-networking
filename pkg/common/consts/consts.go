// Package consts features some shared consts used across fleet networking controllers.
package consts

const (
	// DerivedServiceLabel is a label added to a MCS object, which marks the name of the derived Service
	// associated with the MCS.
	DerivedServiceLabel = "networking.fleet.azure.com/derived-service"
	// ServiceInUseByAnnotationKey is the key of the ServiceInUseBy annotation, which marks the list
	// of member clusters importing an exported Service.
	ServiceInUseByAnnotationKey = "networking.fleet.azure.com/service-in-use-by"
)
