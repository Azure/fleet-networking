/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package metrics features some consts and variables used for exposing metrics.
package metrics

import (
	"time"
)

// Metrics related annoations.
const (
	// MetricsAnnotationLastSeenGeneration is an annotation that marks the last seen generation of an
	// an object; this annotation is reserved for the purpose of metric collection.
	MetricsAnnotationLastSeenGeneration = "networking.fleet.azure.com/last-seen-generation"
	// MetricsAnnotationLastSeenTimestamp is an annotation that marks the last seen timestamp of
	// a specific generation of an object; this annotation is reserved for the purpose of metric collection.
	MetricsAnnotationLastSeenTimestamp = "networking.fleet.azure.com/last-seen-timestamp"
	// MetricsAnnotationLastObservedGeneration is an annotation that marks the last generation of an
	// object from which a metric data point has been observed.
	MetricsAnnotationLastObservedGeneration = "networking.fleet.azure.com/last-observed-generation"
)

// Metrics related values.
const (
	MetricsNamespace = "fleet"
	MetricsSubsystem = "networking"

	// The format to use with MetricsAnnotationLastSeenTimestamp.
	//
	// Why use RFC 3339
	//
	// Generally speaking, RFC 3339 offers only a low time resolution (second-based) compared to
	// later standards such as RFC 3339 Nano; however, this choice should be good enough for
	// the current use case, as
	// a) clock drifts between machines + time sync limitations make it difficult to compare
	//    (very) close cross-cluster timestamps
	// b) Fleet networking SLO for endpoint slice propagation is on the scale of seconds.
	MetricsLastSeenTimestampFormat = time.RFC3339
)

// Metrics related settings.
var (
	// The buckets are tailored after common network service performance scenarios in local and cross-region
	// deployments. Larger buckets are used to account for time resolution limitations and clock drifts; for further
	// discussion, see the comment about RFC 3339 in the earlier section.
	//
	// The buckets are [0, 1], [1, 2.5], [2.5, 5], [5, 10], [10, 25], [25, 50], [50, inf] (seconds).
	ExportDurationMillisecondsBuckets = []float64{1000, 2500, 5000, 10000, 25000, 50000}
	// The right bound of export durations; any data point beyond this limit will be capped.
	ExportDurationRightBound = ExportDurationMillisecondsBuckets[len(ExportDurationMillisecondsBuckets)-1] * 2
)
