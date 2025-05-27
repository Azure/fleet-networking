/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package trafficmanagerbackend

import (
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetnetv1beta1 "go.goms.io/fleet-networking/api/v1beta1"
)

// validateEmittedEvents validates the events emitted for a trafficManagerBackend.
func validateEmittedEvents(backend *fleetnetv1beta1.TrafficManagerBackend, want []corev1.Event) {
	var got corev1.EventList
	Expect(k8sClient.List(ctx, &got, client.InNamespace(testNamespace),
		client.MatchingFieldsSelector{Selector: fields.OneTermEqualSelector("involvedObject.name", backend.Name)})).Should(Succeed())

	cmpOptions := []cmp.Option{
		cmpopts.SortSlices(func(a, b corev1.Event) bool {
			return a.LastTimestamp.Before(&b.LastTimestamp) // sort by time
		}),
		cmp.Comparer(func(a, b corev1.Event) bool {
			return a.Reason == b.Reason && a.Type == b.Type && a.ReportingController == b.ReportingController
		}),
	}
	diff := cmp.Diff(got.Items, want, cmpOptions...)
	Expect(diff).To(BeEmpty(), "Event list mismatch (-got, +want):\n%s", diff)
}
