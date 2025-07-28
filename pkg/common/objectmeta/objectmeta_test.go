/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package objectmeta

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fleetnetv1beta1 "go.goms.io/fleet-networking/api/v1beta1"
)

func TestAzureTrafficManagerProfileTagKey(t *testing.T) {
	want := "networking.fleet.azure.com.trafficManagerProfile"
	if got := AzureTrafficManagerProfileTagKey; got != want {
		t.Errorf("AzureTrafficManagerProfileTagKey = %v, want %v", got, want)
	}
}

func TestExtractWeightFromServiceExport(t *testing.T) {
	testCases := []struct {
		name       string
		svcExport  *fleetnetv1beta1.ServiceExport
		wantWeight int64
		wantError  bool
	}{
		{
			name: "default weight when annotation is missing",
			svcExport: &fleetnetv1beta1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{},
			},
			wantWeight: 1,
		},
		{
			name: "valid weight annotation",
			svcExport: &fleetnetv1beta1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						ServiceExportAnnotationWeight: "500",
					},
				},
			},
			wantWeight: 500,
		},
		{
			name: "test 0 is valid weight annotation",
			svcExport: &fleetnetv1beta1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						ServiceExportAnnotationWeight: "0",
					},
				},
			},
			wantWeight: 0,
		},
		{
			name: "test 1000 is valid weight annotation",
			svcExport: &fleetnetv1beta1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						ServiceExportAnnotationWeight: "1000",
					},
				},
			},
			wantWeight: 1000,
		},
		{
			name: "invalid weight annotation (non-integer)",
			svcExport: &fleetnetv1beta1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						ServiceExportAnnotationWeight: "invalid",
					},
				},
			},
			wantError: true,
		},
		{
			name: "invalid weight annotation (out of range)",
			svcExport: &fleetnetv1beta1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						ServiceExportAnnotationWeight: "2000",
					},
				},
			},
			wantError: true,
		},
		{
			name: "invalid weight annotation (out of range)",
			svcExport: &fleetnetv1beta1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						ServiceExportAnnotationWeight: "-2",
					},
				},
			},
			wantError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotWeight, err := ExtractWeightFromServiceExport(tc.svcExport)
			if (err != nil) != tc.wantError {
				t.Fatalf("ExtractWeightFromServiceExport() error = %v, want %v", err, tc.wantError)
			}
			if !tc.wantError && gotWeight != tc.wantWeight {
				t.Errorf("ExtractWeightFromServiceExport() weight = %d, want %d", gotWeight, tc.wantWeight)
			}
		})
	}
}
