/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package multiclusterservice

import (
	"regexp"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

func TestUniqueDerivedServiceName(t *testing.T) {
	tests := []struct {
		name       string
		namespace  string
		mcsName    string
		uid        string
		wantPrefix string
	}{
		{
			name:       "no trimming needed",
			namespace:  "mynamespace",
			mcsName:    "myservice",
			uid:        "11111111-1111-1111-1111-111111111111",
			wantPrefix: "mynamespace-myservice",
		},
		{
			name:       "trims namespace and name to their quotas when too long",
			namespace:  strings.Repeat("a", 63),
			mcsName:    strings.Repeat("b", 63),
			uid:        "22222222-2222-2222-2222-222222222222",
			wantPrefix: strings.Repeat("a", 24) + "-" + strings.Repeat("b", 24),
		},
		{
			name:       "boundary fits exactly",
			namespace:  strings.Repeat("a", 24),
			mcsName:    strings.Repeat("b", 24),
			uid:        "33333333-3333-3333-3333-333333333333",
			wantPrefix: strings.Repeat("a", 24) + "-" + strings.Repeat("b", 24),
		},
		{
			name:       "per-field quotas apply even when total overflow is odd",
			namespace:  strings.Repeat("a", 30),
			mcsName:    strings.Repeat("b", 30),
			uid:        "44444444-4444-4444-4444-444444444444",
			wantPrefix: strings.Repeat("a", 24) + "-" + strings.Repeat("b", 24),
		},
		{
			name:       "long name is trimmed to its own quota",
			namespace:  "shortns",
			mcsName:    strings.Repeat("b", 60),
			uid:        "55555555-5555-5555-5555-555555555555",
			wantPrefix: "shortns-" + strings.Repeat("b", 24),
		},
		{
			name:       "removes dots from namespace and name",
			namespace:  "my.name.space",
			mcsName:    "my.service",
			uid:        "66666666-6666-6666-6666-666666666666",
			wantPrefix: "mynamespace-myservice",
		},
		{
			name:       "prefixes numeric-leading namespace with ns-",
			namespace:  "0namespace",
			mcsName:    "myservice",
			uid:        "77777777-7777-7777-7777-777777777777",
			wantPrefix: "ns-0namespace-myservice",
		},
		{
			name:       "prefixes numeric-leading namespace after removing dots",
			namespace:  "9.namespace",
			mcsName:    "myservice",
			uid:        "88888888-8888-8888-8888-888888888888",
			wantPrefix: "ns-9namespace-myservice",
		},
		{
			name:       "trims numeric-leading namespace to its quota after prefixing",
			namespace:  "1" + strings.Repeat("a", 62),
			mcsName:    "myservice",
			uid:        "99999999-9999-9999-9999-999999999999",
			wantPrefix: ("ns-1" + strings.Repeat("a", 62))[:24] + "-myservice",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &Reconciler{FleetSystemNamespace: "fleet-system"}
			mcs := &fleetnetv1alpha1.MultiClusterService{ObjectMeta: metav1.ObjectMeta{Namespace: tc.namespace, Name: tc.mcsName, UID: types.UID(tc.uid)}}

			got, err := r.uniqueDerivedServiceName(mcs)
			if err != nil {
				t.Fatalf("uniqueDerivedServiceName() error = %v", err)
			}

			if got.Namespace != r.FleetSystemNamespace {
				t.Fatalf("result namespace = %q, want %q", got.Namespace, r.FleetSystemNamespace)
			}

			wantPattern := "^" + regexp.QuoteMeta(tc.wantPrefix) + "-[0-9a-f]{12}$"
			matched, err := regexp.MatchString(wantPattern, got.Name)
			if err != nil {
				t.Fatalf("failed to match pattern %q: %v", wantPattern, err)
			}
			if !matched {
				t.Fatalf("uniqueDerivedServiceName() = %q, want match %q", got.Name, wantPattern)
			}
		})
	}
}
