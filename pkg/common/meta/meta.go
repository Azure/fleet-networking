/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package meta features some metadata consts (labels, annotations, etc.) used across different fleet
// networking controllers
package meta

const (
	// ServiceInUseByAnnotationKey is the key of the ServiceInUseBy annotation, which marks the list
	// of member clusters importing an exported Service.
	ServiceInUseByAnnotationKey = "networking.fleet.azure.com/service-in-use-by"
)
