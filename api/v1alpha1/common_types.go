/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// ExportedObjectReference helps operators identify the source of an exported object, e.g. an EndpointSliceExport.
// +structType=atomic
type ExportedObjectReference struct {
	// The ID of the cluster where the object is exported.
	// +kubebuilder:validation:Required
	ClusterID string `json:"clusterId"`
	// The API version of the referred object.
	// +optional
	APIVersion string `json:"apiVersion,omitempty"`
	// The kind of the referred object.
	// +kubebuilder:validation:Required
	Kind string `json:"kind"`
	// The namespace of the referred object.
	// +kubebuilder:validation:Required
	Namespace string `json:"namespace"`
	// The name of the referred object.
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// The resource version of the referred object.
	// +kubebuilder:validation:Required
	ResourceVersion string `json:"resourceVersion"`
	// The generation of the referred object.
	// +kubebuilder:validation:Required
	Generation int64 `json:"generation"`
	// The UID of the referred object.
	// +kubebuilder:validation:Required
	UID types.UID `json:"uid"`
}

// FromMetaObjects builds a new ExportedObjectReference using TypeMeta and ObjectMeta fields from an object.
func FromMetaObjects(clusterID string, typeMeta v1.TypeMeta, objMeta v1.ObjectMeta) ExportedObjectReference {
	return ExportedObjectReference{
		ClusterID:       clusterID,
		APIVersion:      typeMeta.APIVersion,
		Kind:            typeMeta.Kind,
		Namespace:       objMeta.Namespace,
		Name:            objMeta.Name,
		ResourceVersion: objMeta.ResourceVersion,
		Generation:      objMeta.Generation,
		UID:             objMeta.UID,
	}
}

// UpdateFromMetaObject updates an existing ExportedObjectReference using ObjectMeta fields from the
// referenced object. Note that most fields in an ExportedObjectReference should be immutable after creation,
// and this method updates only the mutable fields.
func (e *ExportedObjectReference) UpdateFromMetaObject(objMeta v1.ObjectMeta) {
	e.ResourceVersion = objMeta.ResourceVersion
	e.Generation = objMeta.Generation
}
