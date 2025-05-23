/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package trafficmanager provides a custom client for Azure Traffic Manager API with metrics
package trafficmanager

import (
	"context"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/trafficmanager/armtrafficmanager"
	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	"go.goms.io/fleet-networking/pkg/common/metrics"
)

// Interface types to make testing easier

// ProfilesClientInterface defines the interface for Traffic Manager profile operations
type ProfilesClientInterface interface {
	Get(ctx context.Context, resourceGroupName string, profileName string, options *armtrafficmanager.ProfilesClientGetOptions) (armtrafficmanager.ProfilesClientGetResponse, error)
	CreateOrUpdate(ctx context.Context, resourceGroupName string, profileName string, parameters armtrafficmanager.Profile, options *armtrafficmanager.ProfilesClientCreateOrUpdateOptions) (armtrafficmanager.ProfilesClientCreateOrUpdateResponse, error)
	Delete(ctx context.Context, resourceGroupName string, profileName string, options *armtrafficmanager.ProfilesClientDeleteOptions) (armtrafficmanager.ProfilesClientDeleteResponse, error)
}

// EndpointsClientInterface defines the interface for Traffic Manager endpoint operations
type EndpointsClientInterface interface {
	CreateOrUpdate(ctx context.Context, resourceGroupName string, profileName string, endpointType armtrafficmanager.EndpointType, endpointName string, parameters armtrafficmanager.Endpoint, options *armtrafficmanager.EndpointsClientCreateOrUpdateOptions) (armtrafficmanager.EndpointsClientCreateOrUpdateResponse, error)
	Delete(ctx context.Context, resourceGroupName string, profileName string, endpointType armtrafficmanager.EndpointType, endpointName string, options *armtrafficmanager.EndpointsClientDeleteOptions) (armtrafficmanager.EndpointsClientDeleteResponse, error)
}

var (
	// armRequestLatency is a prometheus metric that measures the latency of ARM API calls in milliseconds.
	armRequestLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metrics.MetricsNamespace,
		Subsystem: metrics.MetricsSubsystem,
		Name:      "traffic_manager_arm_api_latency_milliseconds",
		Help:      "Latency of Azure Resource Manager (ARM) API calls for Traffic Manager in milliseconds",
		Buckets:   metrics.ExportDurationMillisecondsBuckets,
	}, []string{
		// The type of operation: Get, CreateOrUpdate, Delete
		"operation",
		// Whether the API call was successful or not
		"success",
		// Type of resource: Profile, Endpoint
		"resource_type",
	})
)

func init() {
	// Register armRequestLatency (fleet_networking_traffic_manager_arm_api_latency_milliseconds)
	// metric with the controller runtime global metrics registry.
	ctrlmetrics.Registry.MustRegister(armRequestLatency)
}

// Custom client implementations

// ProfilesClient is a custom client for Traffic Manager profile operations
type ProfilesClient struct {
	client ProfilesClientInterface
}

// NewProfilesClient creates a new ProfilesClient
func NewProfilesClient(client ProfilesClientInterface) *ProfilesClient {
	return &ProfilesClient{
		client: client,
	}
}

// Get gets a Traffic Manager profile with metrics
func (c *ProfilesClient) Get(ctx context.Context, resourceGroupName string, profileName string, options *armtrafficmanager.ProfilesClientGetOptions) (armtrafficmanager.ProfilesClientGetResponse, error) {
	startTime := time.Now()
	response, err := c.client.Get(ctx, resourceGroupName, profileName, options)
	elapsed := time.Since(startTime).Milliseconds()
	
	success := "true"
	if err != nil {
		success = "false"
	}
	
	armRequestLatency.WithLabelValues("Get", success, "Profile").Observe(float64(elapsed))
	return response, err
}

// CreateOrUpdate creates or updates a Traffic Manager profile with metrics
func (c *ProfilesClient) CreateOrUpdate(ctx context.Context, resourceGroupName string, profileName string, parameters armtrafficmanager.Profile, options *armtrafficmanager.ProfilesClientCreateOrUpdateOptions) (armtrafficmanager.ProfilesClientCreateOrUpdateResponse, error) {
	startTime := time.Now()
	response, err := c.client.CreateOrUpdate(ctx, resourceGroupName, profileName, parameters, options)
	elapsed := time.Since(startTime).Milliseconds()
	
	success := "true"
	if err != nil {
		success = "false"
	}
	
	armRequestLatency.WithLabelValues("CreateOrUpdate", success, "Profile").Observe(float64(elapsed))
	return response, err
}

// Delete deletes a Traffic Manager profile with metrics
func (c *ProfilesClient) Delete(ctx context.Context, resourceGroupName string, profileName string, options *armtrafficmanager.ProfilesClientDeleteOptions) (armtrafficmanager.ProfilesClientDeleteResponse, error) {
	startTime := time.Now()
	response, err := c.client.Delete(ctx, resourceGroupName, profileName, options)
	elapsed := time.Since(startTime).Milliseconds()
	
	success := "true"
	if err != nil {
		success = "false"
	}
	
	armRequestLatency.WithLabelValues("Delete", success, "Profile").Observe(float64(elapsed))
	return response, err
}

// EndpointsClient is a custom client for Traffic Manager endpoint operations
type EndpointsClient struct {
	client EndpointsClientInterface
}

// NewEndpointsClient creates a new EndpointsClient
func NewEndpointsClient(client EndpointsClientInterface) *EndpointsClient {
	return &EndpointsClient{
		client: client,
	}
}

// CreateOrUpdate creates or updates a Traffic Manager endpoint with metrics
func (c *EndpointsClient) CreateOrUpdate(ctx context.Context, resourceGroupName string, profileName string, endpointType armtrafficmanager.EndpointType, endpointName string, parameters armtrafficmanager.Endpoint, options *armtrafficmanager.EndpointsClientCreateOrUpdateOptions) (armtrafficmanager.EndpointsClientCreateOrUpdateResponse, error) {
	startTime := time.Now()
	response, err := c.client.CreateOrUpdate(ctx, resourceGroupName, profileName, endpointType, endpointName, parameters, options)
	elapsed := time.Since(startTime).Milliseconds()
	
	success := "true"
	if err != nil {
		success = "false"
	}
	
	armRequestLatency.WithLabelValues("CreateOrUpdate", success, "Endpoint").Observe(float64(elapsed))
	return response, err
}

// Delete deletes a Traffic Manager endpoint with metrics
func (c *EndpointsClient) Delete(ctx context.Context, resourceGroupName string, profileName string, endpointType armtrafficmanager.EndpointType, endpointName string, options *armtrafficmanager.EndpointsClientDeleteOptions) (armtrafficmanager.EndpointsClientDeleteResponse, error) {
	startTime := time.Now()
	response, err := c.client.Delete(ctx, resourceGroupName, profileName, endpointType, endpointName, options)
	elapsed := time.Since(startTime).Milliseconds()
	
	success := "true"
	if err != nil {
		success = "false"
	}
	
	armRequestLatency.WithLabelValues("Delete", success, "Endpoint").Observe(float64(elapsed))
	return response, err
}