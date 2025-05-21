/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package trafficmanagerclient

import (
	"context"
	"errors"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/trafficmanager/armtrafficmanager"
	"github.com/google/go-cmp/cmp"
	prometheusclientmodel "github.com/prometheus/client_model/go"
	"k8s.io/utils/ptr"
	armmetrics "sigs.k8s.io/cloud-provider-azure/pkg/azclient/metrics"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	"go.goms.io/fleet-networking/pkg/common/azclient/telemetryprovider"
	"go.goms.io/fleet-networking/test/common/metrics"
	"go.goms.io/fleet-networking/test/common/trafficmanager/fakeprovider"
)

const (
	meterName = "myapp"
)

var (
	commonProfileLabels = []*prometheusclientmodel.LabelPair{
		{
			Name:  ptr.To("otel_scope_name"),
			Value: ptr.To(meterName),
		},
		{
			Name:  ptr.To("otel_scope_version"),
			Value: ptr.To(""),
		},
		{
			Name:  ptr.To("resource"),
			Value: ptr.To("TrafficManagerProfile"),
		},
		{
			Name:  ptr.To("resource_group"),
			Value: ptr.To(fakeprovider.DefaultResourceGroupName),
		},
		{
			Name:  ptr.To("subscription_id"),
			Value: ptr.To(fakeprovider.DefaultSubscriptionID),
		},
	}
	durationHistogram = &prometheusclientmodel.Histogram{
		SampleCount: ptr.To[uint64](1),
		Bucket: []*prometheusclientmodel.Bucket{
			{
				CumulativeCount: ptr.To[uint64](1),
				UpperBound:      ptr.To(0.1),
			},
			{
				CumulativeCount: ptr.To[uint64](1),
				UpperBound:      ptr.To(0.25),
			},
			{
				CumulativeCount: ptr.To[uint64](1),
				UpperBound:      ptr.To(0.5),
			},
			{
				CumulativeCount: ptr.To[uint64](1),
				UpperBound:      ptr.To(1.0),
			},
			{
				CumulativeCount: ptr.To[uint64](1),
				UpperBound:      ptr.To(2.5),
			},
			{
				CumulativeCount: ptr.To[uint64](1),
				UpperBound:      ptr.To[float64](5),
			},
			{
				CumulativeCount: ptr.To[uint64](1),
				UpperBound:      ptr.To[float64](10),
			},
			{
				CumulativeCount: ptr.To[uint64](1),
				UpperBound:      ptr.To[float64](60),
			},
			{
				CumulativeCount: ptr.To[uint64](1),
				UpperBound:      ptr.To[float64](300),
			},
			{
				CumulativeCount: ptr.To[uint64](1),
				UpperBound:      ptr.To[float64](600),
			},
		},
	}
)

func TestProfileClients_Get(t *testing.T) {
	durationMetrics := prometheusclientmodel.Metric{
		Label: append(commonProfileLabels, []*prometheusclientmodel.LabelPair{
			{
				Name:  ptr.To("method"),
				Value: ptr.To("get"),
			},
		}...),
		Histogram: durationHistogram,
	}
	tests := []struct {
		name        string
		profileName string
		wantErr     bool
		statusCode  int
		wantMetrics map[string][]*prometheusclientmodel.Metric
	}{
		{
			name:        "200 success",
			profileName: fakeprovider.ValidProfileName,
			statusCode:  200,
			wantMetrics: map[string][]*prometheusclientmodel.Metric{
				"arm.request.duration_seconds": {
					&durationMetrics,
				},
			},
		},
		{
			name:        "404 not found",
			profileName: "not-found-profile",
			wantErr:     true,
			statusCode:  404,
			wantMetrics: map[string][]*prometheusclientmodel.Metric{
				"arm.request.duration_seconds": {
					&durationMetrics,
				},
				"arm.request.errors.counter_total": {
					{
						Label: append(commonProfileLabels, []*prometheusclientmodel.LabelPair{
							{
								Name:  ptr.To("error_code"),
								Value: ptr.To("NotFoundError"),
							},
							{
								Name:  ptr.To("status_code"),
								Value: ptr.To("404"),
							},
							{
								Name:  ptr.To("method"),
								Value: ptr.To("get"),
							},
						}...),
						Counter: &prometheusclientmodel.Counter{
							Value: ptr.To[float64](1),
						},
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			telemetryProvider, err := telemetryprovider.New(meterName)
			if err != nil {
				t.Fatalf("Failed to create telemetry provider: %v", err)
			}
			defer func() {
				if err := telemetryProvider.Stop(ctx); err != nil {
					t.Fatalf("Failed to stop telemetry provider: %v", err)
				}
			}()

			if err := armmetrics.Setup(telemetryProvider.DefaultMeter()); err != nil {
				t.Fatalf("Failed to setup metrics: %v", err)
			}
			fakeProfileClient, err := fakeprovider.NewProfileClient()
			if err != nil {
				t.Fatalf("failed to create fake provider: %v", err)
			}
			profilesClient := NewProfilesClient(fakeProfileClient, fakeprovider.DefaultSubscriptionID)
			_, err = profilesClient.Get(ctx, fakeprovider.DefaultResourceGroupName, tc.profileName, nil)
			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Fatalf("Get() error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				var responseError *azcore.ResponseError
				if !errors.As(err, &responseError) || responseError.StatusCode != tc.statusCode {
					t.Fatalf("Get() error = %v, want status code %d", err, tc.statusCode)
				}
			}
			validateARMMetricsEmitted(t, tc.wantMetrics)
		})
	}
}

func TestProfileClients_CreateOrUpdate(t *testing.T) {
	durationMetrics := prometheusclientmodel.Metric{
		Label: append(commonProfileLabels, []*prometheusclientmodel.LabelPair{
			{
				Name:  ptr.To("method"),
				Value: ptr.To("createOrUpdate"),
			},
		}...),
		Histogram: durationHistogram,
	}
	tests := []struct {
		name        string
		profileName string
		wantErr     bool
		statusCode  int
		wantMetrics map[string][]*prometheusclientmodel.Metric
	}{
		{
			name:        "200 success",
			profileName: fakeprovider.ValidProfileName,
			statusCode:  200,
			wantMetrics: map[string][]*prometheusclientmodel.Metric{
				"arm.request.duration_seconds": {
					&durationMetrics,
				},
			},
		},
		{
			name:        "400 bad request",
			profileName: "bad-request-profile",
			wantErr:     true,
			statusCode:  400,
			wantMetrics: map[string][]*prometheusclientmodel.Metric{
				"arm.request.duration_seconds": {
					&durationMetrics,
				},
				"arm.request.errors.counter_total": {
					{
						Label: append(commonProfileLabels, []*prometheusclientmodel.LabelPair{
							{
								Name:  ptr.To("error_code"),
								Value: ptr.To("BadRequestError"),
							},
							{
								Name:  ptr.To("status_code"),
								Value: ptr.To("400"),
							},
							{
								Name:  ptr.To("method"),
								Value: ptr.To("createOrUpdate"),
							},
						}...),
						Counter: &prometheusclientmodel.Counter{
							Value: ptr.To[float64](1),
						},
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			telemetryProvider, err := telemetryprovider.New(meterName)
			if err != nil {
				t.Fatalf("Failed to create telemetry provider: %v", err)
			}
			defer func() {
				if err := telemetryProvider.Stop(ctx); err != nil {
					t.Fatalf("Failed to stop telemetry provider: %v", err)
				}
			}()

			if err := armmetrics.Setup(telemetryProvider.DefaultMeter()); err != nil {
				t.Fatalf("Failed to setup metrics: %v", err)
			}
			fakeProfileClient, err := fakeprovider.NewProfileClient()
			if err != nil {
				t.Fatalf("failed to create fake provider: %v", err)
			}
			profilesClient := NewProfilesClient(fakeProfileClient, fakeprovider.DefaultSubscriptionID)
			profile := armtrafficmanager.Profile{
				Name: ptr.To(tc.profileName),
				Properties: &armtrafficmanager.ProfileProperties{
					MonitorConfig: &armtrafficmanager.MonitorConfig{},
					DNSConfig: &armtrafficmanager.DNSConfig{
						RelativeName: ptr.To(tc.profileName),
					},
				},
			}
			_, err = profilesClient.CreateOrUpdate(ctx, fakeprovider.DefaultResourceGroupName, tc.profileName, profile, nil)
			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Fatalf("CreateOrUpdate() error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				var responseError *azcore.ResponseError
				if !errors.As(err, &responseError) || responseError.StatusCode != tc.statusCode {
					t.Fatalf("CreateOrUpdate() error = %v, want status code %d", err, tc.statusCode)
				}
			}
			validateARMMetricsEmitted(t, tc.wantMetrics)
		})
	}
}

func TestProfileClients_Delete(t *testing.T) {
	durationMetrics := prometheusclientmodel.Metric{
		Label: append(commonProfileLabels, []*prometheusclientmodel.LabelPair{
			{
				Name:  ptr.To("method"),
				Value: ptr.To("delete"),
			},
		}...),
		Histogram: durationHistogram,
	}
	tests := []struct {
		name        string
		profileName string
		wantErr     bool
		statusCode  int
		wantMetrics map[string][]*prometheusclientmodel.Metric
	}{
		{
			name:        "200 success",
			profileName: fakeprovider.ValidProfileName,
			statusCode:  200,
			wantMetrics: map[string][]*prometheusclientmodel.Metric{
				"arm.request.duration_seconds": {
					&durationMetrics,
				},
			},
		},
		{
			name:        "404 not found",
			profileName: "not-found-profile",
			wantErr:     true,
			statusCode:  404,
			wantMetrics: map[string][]*prometheusclientmodel.Metric{
				"arm.request.duration_seconds": {
					&durationMetrics,
				},
				"arm.request.errors.counter_total": {
					{
						Label: append(commonProfileLabels, []*prometheusclientmodel.LabelPair{
							{
								Name:  ptr.To("error_code"),
								Value: ptr.To("NotFoundError"),
							},
							{
								Name:  ptr.To("status_code"),
								Value: ptr.To("404"),
							},
							{
								Name:  ptr.To("method"),
								Value: ptr.To("delete"),
							},
						}...),
						Counter: &prometheusclientmodel.Counter{
							Value: ptr.To[float64](1),
						},
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			telemetryProvider, err := telemetryprovider.New(meterName)
			if err != nil {
				t.Fatalf("Failed to create telemetry provider: %v", err)
			}
			defer func() {
				if err := telemetryProvider.Stop(ctx); err != nil {
					t.Fatalf("Failed to stop telemetry provider: %v", err)
				}
			}()

			if err := armmetrics.Setup(telemetryProvider.DefaultMeter()); err != nil {
				t.Fatalf("Failed to setup metrics: %v", err)
			}
			fakeProfileClient, err := fakeprovider.NewProfileClient()
			if err != nil {
				t.Fatalf("failed to create fake provider: %v", err)
			}
			profilesClient := NewProfilesClient(fakeProfileClient, fakeprovider.DefaultSubscriptionID)
			_, err = profilesClient.Delete(ctx, fakeprovider.DefaultResourceGroupName, tc.profileName, nil)
			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Fatalf("Delete() error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				var responseError *azcore.ResponseError
				if !errors.As(err, &responseError) || responseError.StatusCode != tc.statusCode {
					t.Fatalf("Delete() error = %v, want status code %d", err, tc.statusCode)
				}
			}
			validateARMMetricsEmitted(t, tc.wantMetrics)
		})
	}
}

func validateARMMetricsEmitted(t *testing.T, wantMetrics map[string][]*prometheusclientmodel.Metric) {
	metricFamilies, err := ctrlmetrics.Registry.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}
	for _, mf := range metricFamilies {
		got := mf.GetMetric()
		want := wantMetrics[mf.GetName()]
		if want == nil {
			continue // ignore metrics not in wantMetrics
		}
		if diff := cmp.Diff(got, want, metrics.CmpOptions...); diff != "" {
			t.Errorf("ARM metrics %v metrics mismatch (-got, +want):\n%s", mf.GetName(), diff)
		}
		delete(wantMetrics, mf.GetName())
	}
	if len(wantMetrics) > 0 { // the number of remaining metrics should be 0
		t.Errorf("Missing ARM metrics %v", wantMetrics)
	}
}
