/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package origintls

import (
	"reflect"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

func TestHeartbeatPreservesSpecAndRejectsReplay(t *testing.T) {
	export := &fleetnetv1alpha1.InternalServiceExport{}
	start := metav1.NewTime(time.Now().UTC().Truncate(time.Second))
	intent := &fleetnetv1alpha1.OriginTLS{ObservedAt: start, Ready: true, Reason: "Ready", ServiceUID: "service-one", Hostname: "origin.example.com", Trust: "System"}
	if err := RefreshObservation(export, intent); err != nil {
		t.Fatal(err)
	}
	before := export.Spec.OriginTLS.DeepCopy()
	intent = intent.DeepCopy()
	intent.ObservedAt = metav1.NewTime(start.Add(time.Minute))
	if err := RefreshObservation(export, intent); err != nil {
		t.Fatal(err)
	}
	observed, err := ObservedAt(export)
	if err != nil || !observed.Equal(intent.ObservedAt.Time) || !reflect.DeepEqual(before, export.Spec.OriginTLS) {
		t.Fatalf("heartbeat changed spec or failed renewal: %+v %v", export, err)
	}
	export.Spec.OriginTLS.ServiceUID = "replacement"
	if _, err := ObservedAt(export); err == nil {
		t.Fatal("heartbeat replay authorized a replacement Service")
	}
	if err := RefreshObservation(export, nil); err != nil {
		t.Fatal(err)
	}
	if export.Spec.OriginTLS != nil || len(export.Annotations) != 0 {
		t.Fatal("disable retained TLS authority")
	}
}
