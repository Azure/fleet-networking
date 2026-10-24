/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package frontdoor

import (
	"fmt"
	"math"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"sigs.k8s.io/cloud-provider-azure/pkg/azclient/policy/ratelimit"
	"sigs.k8s.io/cloud-provider-azure/pkg/azclient/policy/ratelimit/flowcontrol"
)

type requestRateLimitPolicy struct {
	reader flowcontrol.RateLimiter
	writer flowcontrol.RateLimiter
}

// NewRateLimitPolicy paces AFD requests within their existing context deadlines.
// Share the returned policy across clients to retain the configured aggregate limits.
func NewRateLimitPolicy(config *ratelimit.Config) (policy.Policy, error) {
	if config == nil || !config.CloudProviderRateLimit {
		return nil, nil
	}
	for _, limit := range []struct {
		name  string
		qps   float32
		burst int
	}{
		{"read", config.CloudProviderRateLimitQPS, config.CloudProviderRateLimitBucket},
		{"write", config.CloudProviderRateLimitQPSWrite, config.CloudProviderRateLimitBucketWrite},
	} {
		if limit.qps <= 0 || math.IsNaN(float64(limit.qps)) || math.IsInf(float64(limit.qps), 0) || limit.burst <= 0 {
			return nil, fmt.Errorf("AFD %s rate limit requires finite positive QPS and a positive burst", limit.name)
		}
	}
	return &requestRateLimitPolicy{
		reader: flowcontrol.NewTokenBucketRateLimiter(config.CloudProviderRateLimitQPS, config.CloudProviderRateLimitBucket),
		writer: flowcontrol.NewTokenBucketRateLimiter(config.CloudProviderRateLimitQPSWrite, config.CloudProviderRateLimitBucketWrite),
	}, nil
}

func (p *requestRateLimitPolicy) Do(request *policy.Request) (*http.Response, error) {
	limiter := p.writer
	if request.Raw().Method == http.MethodGet || request.Raw().Method == http.MethodHead {
		limiter = p.reader
	}
	// Fail-fast admission restarts a graph before it can read beyond one burst.
	if err := limiter.Wait(request.Raw().Context()); err != nil {
		return nil, fmt.Errorf("wait for AFD request rate limit: %w", err)
	}
	return request.Next()
}
