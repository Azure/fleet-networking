/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package framework

import (
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// PollInterval defines the interval time for a poll operation.
	PollInterval = 1 * time.Second
	// PollTimeout defines the time after which the poll operation times out.
	// Increase the poll timeout to capture the service export condition changes.
	PollTimeout = 60 * time.Second

	// MCSLBPollTimeout defines the time to wait a MCS to be assigned with LoadBalancer IP address.
	// As MCS depending on handling service related to cloud provider, more time is required.
	MCSLBPollTimeout = 360 * time.Second

	// TestNamespacePrefix defines the prefix of test namespaces.
	TestNamespacePrefix = "my-ns"
)

var (
	// SvcExportConditionCmpOptions configures comparison behaviors foo service export conditions.
	SvcExportConditionCmpOptions = []cmp.Option{
		cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "ObservedGeneration", "Message"),
		cmpopts.SortSlices(func(condition1, condition2 metav1.Condition) bool { return condition1.Type < condition2.Type }),
	}

	// MCSConditionCmpOptions configures comparison behaviors foo multi-cluster service conditions.
	MCSConditionCmpOptions = []cmp.Option{
		cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "ObservedGeneration", "Message"),
		cmpopts.SortSlices(func(condition1, condition2 metav1.Condition) bool { return condition1.Type < condition2.Type }),
	}
)
