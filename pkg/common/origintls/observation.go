/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package origintls

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

const observationAnnotation = "networking.fleet.azure.com/origin-tls-observation"

type heartbeat struct {
	ObservedAt   metav1.Time `json:"observedAt"`
	IntentDigest string      `json:"intentDigest"`
}

func intentDigest(intent *fleetnetv1alpha1.OriginTLS) (string, error) {
	data, err := json.Marshal(intent)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

// RefreshObservation binds a metadata-only heartbeat to the typed TLS snapshot.
// Unchanged intent must not increment generation and invalidate MCS conflict
// acknowledgement every minute.
func RefreshObservation(export *fleetnetv1alpha1.InternalServiceExport, intent *fleetnetv1alpha1.OriginTLS) error {
	if intent == nil {
		export.Spec.OriginTLS = nil
		delete(export.Annotations, observationAnnotation)
		return nil
	}
	observedAt := intent.ObservedAt
	if previous := export.Spec.OriginTLS; previous != nil {
		comparable := *intent
		comparable.ObservedAt = previous.ObservedAt
		if reflect.DeepEqual(&comparable, previous) {
			intent = &comparable
		}
	}
	digest, err := intentDigest(intent)
	if err != nil {
		return err
	}
	data, err := json.Marshal(heartbeat{ObservedAt: observedAt, IntentDigest: digest})
	if err != nil {
		return err
	}
	if export.Annotations == nil {
		export.Annotations = make(map[string]string)
	}
	export.Annotations[observationAnnotation] = string(data)
	export.Spec.OriginTLS = intent
	return nil
}

// ObservedAt accepts only a heartbeat bound to this exact typed source snapshot.
func ObservedAt(export *fleetnetv1alpha1.InternalServiceExport) (time.Time, error) {
	raw, found := export.Annotations[observationAnnotation]
	if !found {
		if export.Spec.OriginTLS == nil {
			return time.Time{}, fmt.Errorf("TLS observation missing")
		}
		return export.Spec.OriginTLS.ObservedAt.Time, nil
	}
	var observation heartbeat
	if err := json.Unmarshal([]byte(raw), &observation); err != nil {
		return time.Time{}, fmt.Errorf("malformed TLS heartbeat")
	}
	digest, err := intentDigest(export.Spec.OriginTLS)
	if err != nil {
		return time.Time{}, err
	}
	if observation.IntentDigest != digest || observation.ObservedAt.IsZero() {
		return time.Time{}, fmt.Errorf("TLS heartbeat does not match source intent")
	}
	return observation.ObservedAt.Time, nil
}
