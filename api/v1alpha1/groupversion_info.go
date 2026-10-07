/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package v1alpha1 contains API Schema definitions for the networking.fleet v1alpha1 API group.
// +kubebuilder:object:generate=true
// +groupName=networking.fleet.azure.com
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

const GroupName = "networking.fleet.azure.com"

var (
	// GroupVersion is group version used to register these objects.
	GroupVersion = schema.GroupVersion{Group: GroupName, Version: "v1alpha1"}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme.
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
	Install     = SchemeBuilder.AddToScheme

	// SchemeGroupVersion is group version used to register these objects
	// Deprecated: use GroupVersion instead.
	SchemeGroupVersion = schema.GroupVersion{Group: GroupName, Version: "v1alpha1"}
)
