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

func TestExtractExportModeFromServiceExport(t *testing.T) {
	testCases := []struct {
		name      string
		svcExport *fleetnetv1beta1.ServiceExport
		wantMode  string
		wantError bool
	}{
		{
			// Missing annotation is the common case for existing manifests;
			// it must resolve to the default so nothing breaks silently.
			name: "annotation absent -> default L4-TrafficManager",
			svcExport: &fleetnetv1beta1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{Name: "no-anno"},
			},
			wantMode: ExportModeValueTrafficManager,
		},
		{
			name: "explicit L4-TrafficManager -> accepted",
			svcExport: &fleetnetv1beta1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name: "explicit-tm",
					Annotations: map[string]string{
						ServiceExportAnnotationExportMode: ExportModeValueTrafficManager,
					},
				},
			},
			wantMode: ExportModeValueTrafficManager,
		},
		{
			name: "explicit L7-FrontDoor -> accepted",
			svcExport: &fleetnetv1beta1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name: "explicit-afd",
					Annotations: map[string]string{
						ServiceExportAnnotationExportMode: ExportModeValueFrontDoor,
					},
				},
			},
			wantMode: ExportModeValueFrontDoor,
		},
		{
			// Empty string is treated as invalid rather than defaulted so a
			// mis-templated Helm value that resolves to "" surfaces loudly
			// instead of silently reverting to TrafficManager.
			name: "empty string -> invalid",
			svcExport: &fleetnetv1beta1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name: "empty",
					Annotations: map[string]string{
						ServiceExportAnnotationExportMode: "",
					},
				},
			},
			wantError: true,
		},
		{
			// Case-sensitive by design: enum values are case-sensitive in
			// the CRD spec, and case-folding here would let ambiguous values
			// pass through to Azure APIs that themselves are case-sensitive.
			name: "wrong case -> invalid",
			svcExport: &fleetnetv1beta1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name: "wrong-case",
					Annotations: map[string]string{
						ServiceExportAnnotationExportMode: "l7-frontdoor",
					},
				},
			},
			wantError: true,
		},
		{
			name: "typo -> invalid",
			svcExport: &fleetnetv1beta1.ServiceExport{
				ObjectMeta: metav1.ObjectMeta{
					Name: "typo",
					Annotations: map[string]string{
						ServiceExportAnnotationExportMode: "L4-Trafficmanager",
					},
				},
			},
			wantError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotMode, err := ExtractExportModeFromServiceExport(tc.svcExport)
			if (err != nil) != tc.wantError {
				t.Fatalf("ExtractExportModeFromServiceExport() error = %v, want error? %v", err, tc.wantError)
			}
			if tc.wantError {
				// On error, mode is documented to be "" so callers cannot
				// accidentally use an unvalidated value.
				if gotMode != "" {
					t.Errorf("ExtractExportModeFromServiceExport() on error returned mode = %q, want empty", gotMode)
				}
				return
			}
			if gotMode != tc.wantMode {
				t.Errorf("ExtractExportModeFromServiceExport() mode = %q, want %q", gotMode, tc.wantMode)
			}
		})
	}
}
