package v1alpha1

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
	// The UID of the referred object.
	// +kubebuilder:validation:Required
	UID string `json:"uid"`
}
