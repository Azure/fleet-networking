/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package metrics provides utilities for collecting ARM API metrics
package metrics

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// armRequestLatency is a prometheus metric that measures the latency of ARM API calls in milliseconds.
	armRequestLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: MetricsNamespace,
		Subsystem: MetricsSubsystem,
		Name:      "traffic_manager_arm_api_latency_milliseconds",
		Help:      "Latency of Azure Resource Manager (ARM) API calls for Traffic Manager in milliseconds",
		Buckets:   ExportDurationMillisecondsBuckets,
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

// ARMContext is the context for ARM metrics.
type ARMContext struct {
	startedAt   time.Time
	operation   string
	resourceType string
}

// BeginARMRequest creates a new ARMContext for an ARM request.
func BeginARMRequest(operation, resourceType string) *ARMContext {
	return &ARMContext{
		startedAt:   time.Now(),
		operation:   operation,
		resourceType: resourceType,
	}
}

// Observe observes the result of the ARM request.
func (c *ARMContext) Observe(err error) {
	success := "true"
	if err != nil {
		success = "false"
	}
	
	elapsed := time.Since(c.startedAt).Milliseconds()
	armRequestLatency.WithLabelValues(c.operation, success, c.resourceType).Observe(float64(elapsed))
}

// IsAzureResponseError checks if the error is an Azure response error
func IsAzureResponseError(err error) bool {
	var responseError *azcore.ResponseError
	return err != nil && errors.As(err, &responseError)
}

// GetAzureResponseErrorCode extracts the error code from an Azure response error
func GetAzureResponseErrorCode(err error) string {
	var responseError *azcore.ResponseError
	if err != nil && errors.As(err, &responseError) {
		return responseError.ErrorCode
	}
	return ""
}

// IsNotFound checks if the error is a not found error
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	
	var responseError *azcore.ResponseError
	if errors.As(err, &responseError) {
		return responseError.StatusCode == 404 || strings.EqualFold(responseError.ErrorCode, "notfound")
	}
	
	return strings.Contains(strings.ToLower(err.Error()), "not found") ||
		strings.Contains(strings.ToLower(err.Error()), "notfound")
}

// MeasureARMCall wraps a function call with ARM metrics
func MeasureARMCall(ctx context.Context, operation string, resourceType string, fn func() error) error {
	metricsCtx := BeginARMRequest(operation, resourceType)
	err := fn()
	metricsCtx.Observe(err)
	return err
}