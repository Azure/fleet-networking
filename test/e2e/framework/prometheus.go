package framework

import (
	"context"
	"fmt"
	"time"

	prometheusapi "github.com/prometheus/client_golang/api"
	prometheusapiv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

const (
	// Normally for aggregated histogram quantile queries like `histogram_quantile(%f, sum by (le) (rate(%s[%dm])))`
	// would be used; however rate() will extrapolate data based on the last entry before the given time range
	// and in performance tests, since all data are populated after the tests run, there might not be an entry
	// ready and as a result rate() will yield 0, which fails the histogram_quantile function (returns NaN). To avoid
	// this issue, sums of histogram buckets are calculated directly rather than using the rate() function.
	histogramQuantileAggregatedQueryTmpl = "histogram_quantile(%f, sum by (le) (%s))"
)

// NewPrometheusAPIClient returns a client for Prometheus API server.
func NewPrometheusAPIClient(prometheusAPISvcAddr string) (prometheusapi.Client, error) {
	prometheusAPIClient, err := prometheusapi.NewClient(prometheusapi.Config{
		Address: prometheusAPISvcAddr,
	})

	if err != nil {
		return nil, err
	}
	return prometheusAPIClient, nil
}

// QueryHistogramQuantileAggregated queries the Prometheus API server for the aggregated phi-quantile of a histogram.
func QueryHistogramQuantileAggregated(ctx context.Context, prometheusAPIClient prometheusapi.Client, phi float32, histogramName string) (float64, error) {
	if phi < 0 || phi > 1 {
		return 0, fmt.Errorf("phi must be a value between 0 and 1 (inclusive)")
	}

	prometheusAPI := prometheusapiv1.NewAPI(prometheusAPIClient)
	query := fmt.Sprintf(histogramQuantileAggregatedQueryTmpl, phi, histogramName)
	res, _, err := prometheusAPI.Query(ctx, query, time.Now(), prometheusapiv1.WithTimeout(time.Second*30))
	if err != nil {
		return 0, err
	}

	if res.Type() != model.ValVector {
		return 0, fmt.Errorf("model.Value, got %s, want %s", res.Type().String(), model.ValVector.String())
	}
	vec := res.(model.Vector)
	if vec.Len() != 1 {
		return 0, fmt.Errorf("model.Vector length, got %d, want %d", vec.Len(), 1)
	}
	sampleVal := vec[0]
	return float64(sampleVal.Value), nil
}
