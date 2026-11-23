/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gatewayresolver

import (
	"context"
	"crypto/sha256"
	"fmt"
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayv1alpha3 "sigs.k8s.io/gateway-api/apis/v1alpha3"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/origintls"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
)

// TLSProjectionName is target-derived, independent of referencing Gateway order.
func TLSProjectionName(service string) string {
	name := service + "-origin-tls"
	if len(name) <= 253 {
		return name
	}
	return fmt.Sprintf("%s-%x-origin-tls", service[:160], sha256.Sum256([]byte(service)))
}

// TLSProjection builds the exact owned hub policy, never a customer Secret.
func TLSProjection(backend gatewaymodel.Backend) *gatewayv1alpha3.BackendTLSPolicy {
	return &gatewayv1alpha3.BackendTLSPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: backend.Namespace, Name: TLSProjectionName(backend.Name),
			Labels:      map[string]string{origintls.ProjectionLabel: "true"},
			Annotations: map[string]string{origintls.ProvenanceAnnotation: backend.TLSProvenance},
			OwnerReferences: []metav1.OwnerReference{{APIVersion: fleetnetv1alpha1.GroupVersion.String(), Kind: serviceImportKind,
				Name: backend.Name, UID: types.UID(backend.TargetUID), Controller: ptr.To(true)}},
		},
		Spec: gatewayv1alpha3.BackendTLSPolicySpec{
			TargetRefs: []gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName{{
				LocalPolicyTargetReference: gatewayv1alpha2.LocalPolicyTargetReference{
					Group: gatewayv1.Group(fleetnetv1alpha1.GroupVersion.Group), Kind: serviceImportKind, Name: gatewayv1.ObjectName(backend.Name)},
			}},
			Validation: gatewayv1alpha3.BackendTLSPolicyValidation{
				Hostname:                gatewayv1.PreciseHostname(backend.TLSHostname),
				WellKnownCACertificates: ptr.To(gatewayv1alpha3.WellKnownCACertificatesSystem),
			},
		},
	}
}

// OwnsTLSProjection requires exact target UID ownership, not a name or label alone.
func OwnsTLSProjection(policy *gatewayv1alpha3.BackendTLSPolicy, backend gatewaymodel.Backend) bool {
	return policy.Namespace == backend.Namespace && policy.Name == TLSProjectionName(backend.Name) &&
		policy.Labels[origintls.ProjectionLabel] == "true" &&
		reflect.DeepEqual(policy.OwnerReferences, TLSProjection(backend).OwnerReferences)
}

// ValidateTLSProjection ignores Accepted status and checks current spec/provenance.
func ValidateTLSProjection(ctx context.Context, reader client.Reader, backend gatewaymodel.Backend) error {
	var policies gatewayv1alpha3.BackendTLSPolicyList
	if err := reader.List(ctx, &policies, client.InNamespace(backend.Namespace)); err != nil {
		return err
	}
	expected := TLSProjection(backend)
	found := false
	for i := range policies.Items {
		policy := &policies.Items[i]
		targets := policy.Name == expected.Name
		for _, ref := range policy.Spec.TargetRefs {
			if string(ref.Group) == fleetnetv1alpha1.GroupVersion.Group && ref.Kind == serviceImportKind && string(ref.Name) == backend.Name {
				targets = true
			}
		}
		if !targets {
			continue
		}
		if found || policy.DeletionTimestamp != nil || !OwnsTLSProjection(policy, backend) ||
			!reflect.DeepEqual(policy.Spec, expected.Spec) || policy.Annotations[origintls.ProvenanceAnnotation] != backend.TLSProvenance {
			return newResolutionError("InvalidTLSProjection", true, "hub TLS policy is foreign, competing, stale or differs from current member intent")
		}
		found = true
	}
	if !found {
		return newResolutionError("TLSProjectionPending", true, "owned hub TLS policy has not been observed")
	}
	return nil
}
