/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package metrics

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

func TestARMContext_Observe(t *testing.T) {
	// Reset the metrics registry to ensure clean state
	metrics.Registry = prometheus.NewRegistry()
	metrics.Registry.MustRegister(armRequestLatency)

	tests := []struct {
		name       string
		operation  string
		resourceType string
		withErr    bool
		wantLabels prometheus.Labels
	}{
		{
			name:       "successful operation",
			operation:  "Get",
			resourceType: "Profile",
			withErr:    false,
			wantLabels: prometheus.Labels{
				"operation":     "Get",
				"success":       "true",
				"resource_type": "Profile",
			},
		},
		{
			name:       "failed operation",
			operation:  "Create",
			resourceType: "Endpoint",
			withErr:    true,
			wantLabels: prometheus.Labels{
				"operation":     "Create",
				"success":       "false",
				"resource_type": "Endpoint",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a new ARM context
			ctx := BeginARMRequest(tt.operation, tt.resourceType)
			
			// Small sleep to ensure measurable duration
			time.Sleep(10 * time.Millisecond)
			
			// Observe the result
			var err error
			if tt.withErr {
				err = errors.New("test error")
			}
			ctx.Observe(err)
			
			// Verify the metric was recorded
			count, err := testutil.GatherAndCount(metrics.Registry, "fleet_networking_traffic_manager_arm_api_latency_milliseconds")
			if err != nil {
				t.Fatalf("Failed to gather metrics: %v", err)
			}
			if count == 0 {
				t.Errorf("No metrics were recorded")
			}
		})
	}
}

func TestMeasureARMCall(t *testing.T) {
	// Reset the metrics registry to ensure clean state
	metrics.Registry = prometheus.NewRegistry()
	metrics.Registry.MustRegister(armRequestLatency)

	tests := []struct {
		name       string
		operation  string
		resourceType string
		fn         func() error
		wantErr    bool
	}{
		{
			name:       "successful operation",
			operation:  "Get",
			resourceType: "Profile",
			fn: func() error {
				return nil
			},
			wantErr: false,
		},
		{
			name:       "failed operation",
			operation:  "Delete",
			resourceType: "Endpoint",
			fn: func() error {
				return errors.New("operation failed")
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			err := MeasureARMCall(ctx, tt.operation, tt.resourceType, tt.fn)
			
			if (err != nil) != tt.wantErr {
				t.Errorf("MeasureARMCall() error = %v, wantErr %v", err, tt.wantErr)
			}
			
			// Verify the metric was recorded
			count, err := testutil.GatherAndCount(metrics.Registry, "fleet_networking_traffic_manager_arm_api_latency_milliseconds")
			if err != nil {
				t.Fatalf("Failed to gather metrics: %v", err)
			}
			if count == 0 {
				t.Errorf("No metrics were recorded")
			}
		})
	}
}