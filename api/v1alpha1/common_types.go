/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	// The namespaced name of the referred object.
	// +kubebuilder:validation:Required
	NamespacedName string `json:"namespacedName"`
	// The timestamp from a local clock when the generation of the object is exported.
	// This field is marked as optional for backwards compatibility reasons.
	// +kubebuilder:validation:Optional
	ExportedSince metav1.Time `json:"exportedSince,omitempty"`
}

// FromMetaObjects builds a new ExportedObjectReference using TypeMeta and ObjectMeta fields from an object.
func FromMetaObjects(clusterID string, typeMeta metav1.TypeMeta, objMeta metav1.ObjectMeta, exportedSince metav1.Time) ExportedObjectReference {
	return ExportedObjectReference{
		ClusterID:       clusterID,
		APIVersion:      typeMeta.APIVersion,
		Kind:            typeMeta.Kind,
		Namespace:       objMeta.Namespace,
		Name:            objMeta.Name,
		ResourceVersion: objMeta.ResourceVersion,
		Generation:      objMeta.Generation,
		UID:             objMeta.UID,
		NamespacedName:  types.NamespacedName{Namespace: objMeta.Namespace, Name: objMeta.Name}.String(),
		ExportedSince:   exportedSince,
	}
}

// UpdateFromMetaObject updates an existing ExportedObjectReference.
// Note that most fields in an ExportedObjectReference should be immutable after creation.
func (e *ExportedObjectReference) UpdateFromMetaObject(objMeta metav1.ObjectMeta, exportedSince metav1.Time) {
	e.ResourceVersion = objMeta.ResourceVersion
	e.ExportedSince = exportedSince
	e.Generation = objMeta.Generation
}

// ClusterID is the ID of a member cluster.
type ClusterID string

// ClusterNamespace is the namespace reserved for a specific member cluster in the hub cluster.
type ClusterNamespace string

// ServiceInUseBy describes the member clusters that have requested to import a Service from the hub cluster.
// This object is not provided directly as a part of fleet networking API, but provided as a contract for
// marshaling/unmarshaling ServiceImport annotations, specifically for
// * the InternalServiceImport controller to annotate on a ServiceImport which member clusters have requested to
//   import the Service; and
// * the EndpointSliceExport controller to find out from annotations on a ServiceImport which member clusters
//   have requested to import the Service.
type ServiceInUseBy struct {
	MemberClusters map[ClusterNamespace]ClusterID
}
