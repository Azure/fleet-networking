/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package objectmeta defines shared meta const used by the networking objects.
package objectmeta

import (
	"fmt"
	"strconv"
	"strings"

	"k8s.io/klog/v2"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

const (
	fleetNetworkingPrefix = "networking.fleet.azure.com/"
)

// Finalizers
const (
	// InternalServiceExportFinalizer is the finalizer InternalServiceExport controllers adds to mark that a
	// InternalServiceExport can only be deleted after both ServiceImport label and ServiceExport conflict resolution
	// result have been updated.
	InternalServiceExportFinalizer = fleetNetworkingPrefix + "internal-svc-export-cleanup"

	// TrafficManagerProfileFinalizer a finalizer added by the TrafficManagerProfile controller to all trafficManagerProfiles,
	// to make sure that the controller can react to profile deletions if necessary.
	TrafficManagerProfileFinalizer = fleetNetworkingPrefix + "traffic-manager-profile-cleanup"

	// TrafficManagerBackendFinalizer a finalizer added by the TrafficManagerBackend controller to all trafficManagerBackends,
	// to make sure that the controller can react to backend deletions if necessary.
	TrafficManagerBackendFinalizer = fleetNetworkingPrefix + "traffic-manager-backend-cleanup"

	// MetricsFinalizer is the finalizer added by the controller to clean up all metrics.
	MetricsFinalizer = fleetNetworkingPrefix + "metrics-cleanup"
)

// Labels
const (
	// MultiClusterServiceLabelDerivedService is the label added by the MCS controller, which marks the
	// derived Service behind a MCS.
	MultiClusterServiceLabelDerivedService = fleetNetworkingPrefix + "derived-service"
)

// Annotations
const (
	// ServiceImportAnnotationServiceInUseBy is the key of the ServiceInUseBy annotation, which marks the list
	// of member clusters importing an exported Service.
	ServiceImportAnnotationServiceInUseBy = fleetNetworkingPrefix + "service-in-use-by"

	// ExportedObjectAnnotationUniqueName is an annotation that marks the fleet-scoped unique name assigned to
	// an exported object.
	ExportedObjectAnnotationUniqueName = fleetNetworkingPrefix + "fleet-unique-name"

	// ServiceExportAnnotationWeight is an annotation that marks the weight of the ServiceExport.
	ServiceExportAnnotationWeight = fleetNetworkingPrefix + "weight"

	// ServiceAnnotationAzureLoadBalancerInternal is an annotation that marks the Service as an internal load balancer by cloud-provider-azure.
	ServiceAnnotationAzureLoadBalancerInternal = "service.beta.kubernetes.io/azure-load-balancer-internal"

	// ServiceAnnotationLoadBalancerResourceGroup is the annotation used on the service to specify the resource group of
	// load balancer objects that are not in the same resource group as the cluster.
	ServiceAnnotationLoadBalancerResourceGroup = "service.beta.kubernetes.io/azure-load-balancer-resource-group"

	// ServiceAnnotationAzureDNSLabelName is the annotation used on the service to Specify the DNS label name for the
	// service’s public IP address (PIP). If it is set to empty string, DNS in PIP would be deleted. Because of a bug,
	// before v1.15.10/v1.16.7/v1.17.3, the DNS label on PIP would also be deleted if the annotation is not specified.
	// https://cloud-provider-azure.sigs.k8s.io/topics/loadbalancer/
	ServiceAnnotationAzureDNSLabelName = "service.beta.kubernetes.io/azure-dns-label-name"
)

// Azure Resource Tags
var (
	// AzureTrafficManagerProfileTagKey is the key of the Azure Traffic Manager profile tag when the controller creates it.
	// Note: The tag name cannot have reserved characters '<,>,%,&,\\,?,/' or control characters.
	AzureTrafficManagerProfileTagKey = strings.ReplaceAll(fleetNetworkingPrefix, "/", ".") + "trafficManagerProfile"
)

// ExtractWeightFromServiceExport gets the weight from the serviceExport annotation and validates it.
func ExtractWeightFromServiceExport(svcExport *fleetnetv1alpha1.ServiceExport) (int64, error) {
	serviceKObj := klog.KObj(svcExport)
	// Setup the weightAnno for the exported service on the hub cluster.
	weightAnno, found := svcExport.Annotations[ServiceExportAnnotationWeight]
	if !found {
		return int64(1), nil
	}
	// check if the weightAnno on the serviceExport in the member cluster is valid
	// The value should be in the range [0, 1000].
	weight, err := strconv.Atoi(weightAnno)
	if err != nil {
		err = fmt.Errorf("the weight annotation is not a valid integer: %s", weightAnno)
		klog.ErrorS(err, "Failed to parse the weight annotation", "serviceExport", serviceKObj)
		return -1, err
	}
	if weight < 0 || weight > 1000 {
		err = fmt.Errorf("the weight annotation is not in the range [0, 1000]: %s", weightAnno)
		klog.ErrorS(err, "The weight annotation is out of range", "serviceExport", serviceKObj)
		return -1, err
	}
	return int64(weight), nil
}
