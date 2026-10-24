/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package frontdoor

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"sigs.k8s.io/cloud-provider-azure/pkg/azclient/policy/ratelimit"
)

func defaultRateLimitConfig() *ratelimit.Config {
	return &ratelimit.Config{
		CloudProviderRateLimit:    true,
		CloudProviderRateLimitQPS: 1, CloudProviderRateLimitBucket: 5,
		CloudProviderRateLimitQPSWrite: 1, CloudProviderRateLimitBucketWrite: 5,
	}
}

func TestReconcileHTTPPreviewRateLimitedGraph(t *testing.T) {
	for _, name := range []string{"fresh bucket", "shared bucket already exhausted"} {
		t.Run(name, func(t *testing.T) {
			f, route := routeFixture(t)
			if len(f.graph.Backends.OriginGroups[0].Origins) != 2 {
				t.Fatal("regression requires both member origins")
			}
			steps := append(append(httpPreviewReads(f, route), completeStagingSteps(f, route)...), enableRouteStep(f, route))
			config := defaultRateLimitConfig()
			start := time.Now()
			limitPolicy, err := NewRateLimitPolicy(config)
			if err != nil {
				t.Fatal(err)
			}
			burst := config.CloudProviderRateLimitBucket
			if name == "shared bucket already exhausted" {
				pipeline := runtime.NewPipeline("fleet-networking", "v0.0.0", runtime.PipelineOptions{}, &policy.ClientOptions{
					PerCallPolicies: []policy.Policy{limitPolicy},
					Transport: cleanupTransport(func(request *http.Request) (*http.Response, error) {
						return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: request}, nil
					}),
				})
				for i := 0; i < burst; i++ {
					if _, err := rateLimitTestRequest(context.Background(), pipeline, http.MethodGet); err != nil {
						t.Fatal(err)
					}
				}
				burst = 0
			}
			p, calls := disabledGraphClient(t, f, steps, nil, limitPolicy)
			got, err := p.ReconcileHTTPPreview(context.Background(), f.snapshot.owner, f.snapshot.model)
			if err != nil || got.State != HTTPPreviewRouteEnabling || *calls != len(steps) {
				t.Fatalf("default limits starved the complete two-origin graph: %+v, %v, calls=%d/%d",
					got, err, *calls, len(steps))
			}
			reads := 0
			for _, step := range steps {
				if step.method == http.MethodGet {
					reads++
				}
			}
			minimum := time.Duration(reads-burst) * time.Second
			if elapsed := time.Since(start); elapsed < minimum || elapsed >= 30*time.Second {
				t.Fatalf("graph did not preserve 1-QPS pacing and the 30-second budget: %s, minimum %s", elapsed, minimum)
			}
		})
	}
}

func TestNewRateLimitPolicyConfiguration(t *testing.T) {
	for _, tt := range []struct {
		name   string
		config *ratelimit.Config
		want   bool
	}{
		{"nil", nil, false},
		{"disabled", &ratelimit.Config{}, false},
		{"enabled", defaultRateLimitConfig(), true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewRateLimitPolicy(tt.config)
			if err != nil || (got != nil) != tt.want {
				t.Fatalf("unexpected policy: %v, %v", got, err)
			}
		})
	}
	for _, pool := range []string{"read", "write"} {
		t.Run(pool, func(t *testing.T) {
			for _, tt := range []struct {
				name  string
				qps   float32
				burst int
			}{
				{"zero QPS", 0, 1},
				{"negative QPS", -1, 1},
				{"NaN QPS", float32(math.NaN()), 1},
				{"infinite QPS", float32(math.Inf(1)), 1},
				{"zero burst", 1, 0},
				{"negative burst", 1, -1},
			} {
				t.Run(tt.name, func(t *testing.T) {
					config := defaultRateLimitConfig()
					if pool == "read" {
						config.CloudProviderRateLimitQPS, config.CloudProviderRateLimitBucket = tt.qps, tt.burst
					} else {
						config.CloudProviderRateLimitQPSWrite, config.CloudProviderRateLimitBucketWrite = tt.qps, tt.burst
					}
					got, err := NewRateLimitPolicy(config)
					if got != nil || err == nil || !strings.Contains(err.Error(), pool) {
						t.Fatalf("invalid %s limits were not rejected explicitly: %v, %v", pool, got, err)
					}
				})
			}
		})
	}
}

func rateLimitTestPipeline(t *testing.T, config *ratelimit.Config, transport policy.Transporter) runtime.Pipeline {
	t.Helper()
	limit, err := NewRateLimitPolicy(config)
	if err != nil {
		t.Fatal(err)
	}
	return runtime.NewPipeline("fleet-networking", "v0.0.0", runtime.PipelineOptions{}, &policy.ClientOptions{
		PerCallPolicies: []policy.Policy{limit}, Transport: transport, Retry: policy.RetryOptions{MaxRetries: -1},
	})
}

func rateLimitTestRequest(ctx context.Context, pipeline runtime.Pipeline, method string) (*http.Response, error) {
	request, err := runtime.NewRequest(ctx, method, "https://management.azure.com/")
	if err != nil {
		return nil, err
	}
	return pipeline.Do(request)
}

func TestRateLimitPolicyIndependentPoolsAndMethods(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			config := defaultRateLimitConfig()
			config.CloudProviderRateLimitQPS, config.CloudProviderRateLimitQPSWrite = 0.1, 0.1
			config.CloudProviderRateLimitBucket, config.CloudProviderRateLimitBucketWrite = 1, 1
			calls := 0
			pipeline := rateLimitTestPipeline(t, config, cleanupTransport(func(request *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: request}, nil
			}))
			opposite := http.MethodGet
			if method == http.MethodGet || method == http.MethodHead {
				opposite = http.MethodPatch
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			for _, admitted := range []string{method, opposite} {
				if _, err := rateLimitTestRequest(ctx, pipeline, admitted); err != nil {
					t.Fatalf("independent pool was exhausted by %s: %v", method, err)
				}
			}
			if _, err := rateLimitTestRequest(ctx, pipeline, method); err == nil || calls != 2 {
				t.Fatalf("request exceeded its original budget or pool: error=%v, calls=%d", err, calls)
			}
		})
	}
}

func TestRateLimitPolicyPreservesCustomLimits(t *testing.T) {
	for _, tt := range []struct {
		method string
		qps    int
	}{
		{http.MethodHead, 50},
		{http.MethodPatch, 25},
	} {
		t.Run(tt.method, func(t *testing.T) {
			config := defaultRateLimitConfig()
			config.CloudProviderRateLimitQPS, config.CloudProviderRateLimitQPSWrite = 50, 25
			config.CloudProviderRateLimitBucket, config.CloudProviderRateLimitBucketWrite = 3, 3
			calls := 0
			start := time.Now()
			pipeline := rateLimitTestPipeline(t, config, cleanupTransport(func(request *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: request}, nil
			}))
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			for i := 0; i < 4; i++ {
				if _, err := rateLimitTestRequest(ctx, pipeline, tt.method); err != nil {
					t.Fatal(err)
				}
			}
			if calls != 4 || time.Since(start) < time.Second/time.Duration(tt.qps) {
				t.Fatalf("configured burst/QPS was bypassed: calls=%d, elapsed=%s", calls, time.Since(start))
			}
		})
	}
}

func TestRateLimitPolicyCancellation(t *testing.T) {
	for _, waiting := range []bool{false, true} {
		name := "before admission"
		if waiting {
			name = "during admission"
		}
		t.Run(name, func(t *testing.T) {
			config := defaultRateLimitConfig()
			config.CloudProviderRateLimitBucket = 1
			calls := 0
			pipeline := rateLimitTestPipeline(t, config, cleanupTransport(func(request *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: request}, nil
			}))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			wantCalls := 0
			if waiting {
				if _, err := rateLimitTestRequest(ctx, pipeline, http.MethodGet); err != nil {
					t.Fatal(err)
				}
				wantCalls = 1
				timer := time.AfterFunc(20*time.Millisecond, cancel)
				defer timer.Stop()
			} else {
				cancel()
			}
			start := time.Now()
			_, err := rateLimitTestRequest(ctx, pipeline, http.MethodGet)
			if !errors.Is(err, context.Canceled) || calls != wantCalls || time.Since(start) >= time.Second {
				t.Fatalf("cancellation reached transport or waited for refill: %v, calls=%d", err, calls)
			}
		})
	}
}

func TestRateLimitPolicyConcurrentRequests(t *testing.T) {
	config := defaultRateLimitConfig()
	config.CloudProviderRateLimitQPS, config.CloudProviderRateLimitBucket = 100, 2
	var calls atomic.Int64
	pipeline := rateLimitTestPipeline(t, config, cleanupTransport(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: request}, nil
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	results := make(chan error, 8)
	for i := 0; i < cap(results); i++ {
		go func() {
			_, err := rateLimitTestRequest(ctx, pipeline, http.MethodGet)
			results <- err
		}()
	}
	for i := 0; i < cap(results); i++ {
		if err := <-results; err != nil {
			t.Errorf("shared limiter rejected concurrent progress: %v", err)
		}
	}
	if calls.Load() != int64(cap(results)) {
		t.Fatalf("lost concurrent requests: %d", calls.Load())
	}
}

func TestRateLimitPolicyPreservesAzureThrottling(t *testing.T) {
	calls := 0
	pipeline := rateLimitTestPipeline(t, defaultRateLimitConfig(), cleanupTransport(func(request *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": []string{"30"}},
			Body: http.NoBody, Request: request}, nil
	}))
	response, err := rateLimitTestRequest(context.Background(), pipeline, http.MethodGet)
	if err != nil || response == nil || response.StatusCode != http.StatusTooManyRequests ||
		response.Header.Get("Retry-After") != "30" || calls != 1 {
		t.Fatalf("ARM throttling response was hidden or changed: %v, %v, calls=%d", response, err, calls)
	}
}
