/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestAzureFrontDoorTypesAreRegistered(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}

	for _, object := range []runtime.Object{
		&AzureFrontDoorGatewayPolicy{},
		&AzureFrontDoorGatewayPolicyList{},
		&AzureFrontDoorBackendAttachment{},
		&AzureFrontDoorBackendAttachmentList{},
	} {
		gvks, _, err := scheme.ObjectKinds(object)
		if err != nil {
			t.Fatalf("ObjectKinds(%T) error = %v", object, err)
		}
		if len(gvks) != 1 || gvks[0].GroupVersion() != GroupVersion {
			t.Errorf("ObjectKinds(%T) = %v, want one %s GVK", object, gvks, GroupVersion)
		}
	}
}

func TestAzureFrontDoorBackendAttachmentDeepCopy(t *testing.T) {
	attachment := &AzureFrontDoorBackendAttachment{
		Spec: AzureFrontDoorBackendAttachmentSpec{
			GatewayRef: gatewayv1.LocalObjectReference{Name: "global"},
			Connectivity: AzureFrontDoorConnectivitySpec{
				Mode:        AzureFrontDoorConnectivityModePrivateLink,
				PrivateLink: &AzureFrontDoorPrivateLinkSpec{Approval: "Manual"},
			},
		},
		Status: AzureFrontDoorBackendAttachmentStatus{
			AzureFrontDoorResourceStatus: AzureFrontDoorResourceStatus{
				Conditions: []metav1.Condition{{Type: AzureFrontDoorConditionAccepted}},
			},
		},
	}

	copied := attachment.DeepCopy()
	copied.Spec.Connectivity.PrivateLink.Approval = "Changed"
	copied.Status.Conditions[0].Type = AzureFrontDoorConditionResolvedRefs

	if attachment.Spec.Connectivity.PrivateLink.Approval != "Manual" {
		t.Error("DeepCopy() aliased private link configuration")
	}
	if attachment.Status.Conditions[0].Type != AzureFrontDoorConditionAccepted {
		t.Error("DeepCopy() aliased status conditions")
	}
}
