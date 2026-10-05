/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package multiclusterservice

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/types"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

type derivedServiceNameHasherInput struct {
	// Note: This struct use exported fields as Golang's JSON marshaller ignores unexported fields.
	MCSNamespace string
	MCSName      string
	MCSUID       string
}

func (r *Reconciler) uniqueDerivedServiceName(mcs *fleetnetv1alpha1.MultiClusterService) (*types.NamespacedName, error) {
	// The name of a derived service is formatted as [MCS-NAMESPACE]-[MCS-NAME]-[HASH-SUFFIX].
	//
	// We might truncate the namespace and name segments to ensure that the total length of the derived service
	// name does not exceed 63 characters, which is the maximum length for a Kubernetes service name.
	//
	// A hash suffix is added as the format [MCS-NAMESPACE]-[MCS-NAME] may lead to name collisions, either due to
	// unexpected dashes in the namespace/name segment, or due to truncation complications.
	//
	// For example, if one MCS has the namespace "team-a" and name "service", and another MCS has the namespace "team" and
	// name "a-service", both would result in the derived service name "team-a-service".

	// Calculate the hash suffix.
	hasherInput := derivedServiceNameHasherInput{
		MCSNamespace: mcs.Namespace,
		MCSName:      mcs.Name,
		MCSUID:       string(mcs.UID),
	}
	hash, err := hashOf(hasherInput)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate hash: %w", err)
	}
	// Use the first 12 characters of the hash as a suffix.
	//
	// Note (chenyu1): a 12 char hash suffix might not be able to fully eliminate collisions, though the chances are extremely low.
	// Should a collision still occur, manual intervention is needed to correct the situation.
	hashSuffix := hash[:12]

	// Truncate the namespace and name segments if needed.

	// The available length of the namespace and name segments (49) is the maximum service name length (63)
	// minus the length of the hash suffix (12) and two dashes (2).
	//
	// Each segment then has a maximum length of 24, which is the available length (49) divided by two, rounded down.
	nameSegMaxLen := 24
	mcsNamespace := mcs.Namespace
	mcsName := mcs.Name

	// Remove all dots from the namespace and name segments, and prefix the namespace with "ns-" if it
	// starts with a numeric character, so that the derived service name remains a valid Kubernetes name.
	mcsNamespace = strings.ReplaceAll(mcsNamespace, ".", "")
	mcsName = strings.ReplaceAll(mcsName, ".", "")
	if len(mcsNamespace) > 0 && mcsNamespace[0] >= '0' && mcsNamespace[0] <= '9' {
		mcsNamespace = "ns-" + mcsNamespace
	}

	if len(mcsNamespace) > nameSegMaxLen {
		mcsNamespace = mcsNamespace[:nameSegMaxLen]
	}
	if len(mcsName) > nameSegMaxLen {
		mcsName = mcsName[:nameSegMaxLen]
	}

	serviceName := fmt.Sprintf("%s-%s-%s", mcsNamespace, mcsName, hashSuffix)

	return &types.NamespacedName{Namespace: r.FleetSystemNamespace, Name: serviceName}, nil
}

func hashOf(input derivedServiceNameHasherInput) (string, error) {
	jsonBytes, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("failed to marshal object into JSON: %w", err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(jsonBytes)), nil
}
