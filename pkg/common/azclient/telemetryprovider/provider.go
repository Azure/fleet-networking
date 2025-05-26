/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package telemetryprovider provides a metrics provider for the controller-runtime metrics registry.
// It uses OpenTelemetry to export metrics to Prometheus.
// The provider is initialized with a default meter and a Prometheus exporter.
package telemetryprovider

import (
	"context"
	"fmt"

	prometheusexporter "go.opentelemetry.io/otel/exporters/prometheus"
	apimetric "go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

// Provider is a metrics provider that uses OpenTelemetry to export metrics to Prometheus.
type Provider struct {
	meterProvider *sdkmetric.MeterProvider
	defaultMeter  apimetric.Meter
}

// New creates a default metrics provider.
func New(name string) (*Provider, error) {
	// Register the default registry with the controller-runtime metrics registry
	exporter, err := prometheusexporter.New(prometheusexporter.WithRegisterer(ctrlmetrics.Registry))
	if err != nil {
		return nil, fmt.Errorf("failed to initialize prometheus exporter: %w", err)
	}

	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	defaultMeter := meterProvider.Meter(name)

	return &Provider{
		meterProvider: meterProvider,
		defaultMeter:  defaultMeter,
	}, nil
}

// DefaultMeter returns the default meter.
func (p *Provider) DefaultMeter() apimetric.Meter {
	return p.defaultMeter
}

// Stop stops the provider.
func (p *Provider) Stop(ctx context.Context) error {
	errC := make(chan error, 1)
	go func() {
		errC <- p.meterProvider.Shutdown(ctx)
	}()

	if err := <-errC; err != nil {
		return fmt.Errorf("failed to stop telemetry provider: %w", err)
	}
	return nil
}
