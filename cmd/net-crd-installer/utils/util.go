/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package utils contains utility functions for the CRD installer.
package utils

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	// CRDInstallerLabelKey is the label key used to indicate that a CRD is managed by the installer.
	CRDInstallerLabelKey = "crd-installer.azurefleet.io/managed"
	// AzureManagedLabelKey is the label key used to indicate that a CRD is managed by an azure resource.
	AzureManagedLabelKey = "kubernetes.azure.com/managedby"
	// FleetLabelValue is the value for the AzureManagedLabelKey indicating management by Fleet.
	FleetLabelValue = "fleet"
)

var (
	// memberIncludedCRDs defines CRDs that should be included in member clusters
	memberIncludedCRDs = map[string]bool{
		"serviceexports.networking.fleet.azure.com":       true,
		"serviceimports.networking.fleet.azure.com":       true,
		"multiclusterservices.networking.fleet.azure.com": true,
	}
)

// InstallCRD creates/updates a Custom Resource Definition (CRD) from the provided CRD object.
func InstallCRD(ctx context.Context, client client.Client, crd *apiextensionsv1.CustomResourceDefinition, isE2ETest bool) error {
	klog.V(2).Infof("Installing CRD: %s", crd.Name)

	existingCRD := apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: crd.Name,
		},
	}

	createOrUpdateRes, err := controllerutil.CreateOrUpdate(ctx, client, &existingCRD, func() error {
		// Copy spec from our decoded CRD to the object we're creating/updating.
		existingCRD.Spec = crd.Spec

		// Add an additional ownership label to indicate this CRD is managed by the installer.
		if existingCRD.Labels == nil {
			existingCRD.Labels = make(map[string]string)
		}
		// Ensure the label for management by the installer is set.
		existingCRD.Labels[CRDInstallerLabelKey] = "true"
		// Only set the Azure managed label if this is not an E2E test.
		// This label is needed for clean up of CRD by kube-addon-manager in production,
		// but can interfere with E2E test as CRDs are removed from AKS clusters which
		// are actually not hub/member clusters.
		if !isE2ETest {
			existingCRD.Labels[AzureManagedLabelKey] = FleetLabelValue
		}
		return nil
	})

	if err != nil {
		klog.ErrorS(err, "Failed to create or update CRD", "name", crd.Name, "operation", createOrUpdateRes)
		return err
	}

	klog.Infof("Successfully created/updated CRD %s", crd.Name)
	return nil
}

// CollectCRDs collects CRDs from the specified path based on the mode either hub/member.
func CollectCRDs(crdDirectoryPath, mode string, scheme *runtime.Scheme) ([]apiextensionsv1.CustomResourceDefinition, error) {
	// Set of CRDs to install based on mode.
	crdsToInstall := []apiextensionsv1.CustomResourceDefinition{}

	// Walk through the CRD directory and collect filenames.
	if err := filepath.WalkDir(crdDirectoryPath, func(crdpath string, d fs.DirEntry, err error) error {
		// Handle errors from WalkDir.
		if err != nil {
			return err
		}

		// Skip root directory i.e. config/crd/bases and any other directory since none should exist.
		if d.IsDir() {
			return nil
		}

		crd, err := GetCRDFromPath(crdpath, scheme)
		if err != nil {
			return err
		}

		group := crd.Spec.Group
		// Check if the group equals "networking.fleet.azure.com".
		if group == "networking.fleet.azure.com" {
			crdName := crd.Name

			if mode == "hub" {
				crdsToInstall = append(crdsToInstall, *crd)
			}

			if mode == "member" {
				// Apply member inclusion logic - only install specific CRDs
				if !memberIncludedCRDs[crdName] {
					klog.V(2).Infof("Excluding CRD %s from member cluster", crdName)
					return nil
				}
				crdsToInstall = append(crdsToInstall, *crd)
			}
		}

		return nil
	}); err != nil {
		return nil, fmt.Errorf("failed to walk CRD directory: %w", err)
	}

	return crdsToInstall, nil
}

// GetCRDFromPath reads a CRD from the specified path and decodes it into a CustomResourceDefinition object.
func GetCRDFromPath(crdPath string, scheme *runtime.Scheme) (*apiextensionsv1.CustomResourceDefinition, error) {
	// Read and parse CRD file.
	crdBytes, err := os.ReadFile(crdPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read CRD file %s: %w", crdPath, err)
	}

	// Create decoder for converting raw bytes to Go types.
	codecFactory := serializer.NewCodecFactory(scheme)
	decoder := codecFactory.UniversalDeserializer()

	// Decode YAML into a structured CRD object.
	obj, gvk, err := decoder.Decode(crdBytes, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decode CRD from %s: %w", crdPath, err)
	}

	// Type assertion to make sure we have a CRD.
	crd, ok := obj.(*apiextensionsv1.CustomResourceDefinition)
	if !ok {
		return nil, fmt.Errorf("unexpected type from %s, expected %s but got %s", crdPath, gvk, apiextensionsv1.SchemeGroupVersion.WithKind("CustomResourceDefinition"))
	}

	return crd, nil
}
