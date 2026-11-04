/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1alpha1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

var _ = Describe("Azure Front Door API validation", func() {
	It("defaults a valid managed Gateway policy", func() {
		policy := validAFDGatewayPolicy()
		Expect(hubClient.Create(ctx, policy)).To(Succeed())

		Expect(policy.Spec.Profile.Mode).To(Equal(fleetnetv1alpha1.AzureFrontDoorProfileModeManaged))
		Expect(policy.Spec.WAF.Required).NotTo(BeNil())
		Expect(*policy.Spec.WAF.Required).To(BeTrue())
	})

	It("rejects a Gateway policy targeting another kind", func() {
		policy := validAFDGatewayPolicy()
		policy.Spec.TargetRef.Kind = "Service"

		err := hubClient.Create(ctx, policy)
		Expect(apierrors.IsInvalid(err)).To(BeTrue(), "error: %v", err)
		Expect(err.Error()).To(ContainSubstring("must reference a Gateway"))
	})

	It("requires WAF enforcement", func() {
		policy := validAFDGatewayPolicy()
		policy.Spec.WAF.Required = ptr.To(false)

		err := hubClient.Create(ctx, policy)
		Expect(apierrors.IsInvalid(err)).To(BeTrue(), "error: %v", err)
		Expect(err.Error()).To(ContainSubstring("required must be true"))
	})

	It("defaults a valid public backend attachment", func() {
		attachment := validAFDBackendAttachment()
		Expect(hubClient.Create(ctx, attachment)).To(Succeed())

		Expect(attachment.Spec.Origin.Protocol).To(Equal(fleetnetv1alpha1.AzureFrontDoorOriginProtocolHTTPS))
		Expect(attachment.Spec.Origin.CertificateSubjectNameCheck).NotTo(BeNil())
		Expect(*attachment.Spec.Origin.CertificateSubjectNameCheck).To(BeTrue())
		Expect(attachment.Spec.HealthProbe.Method).To(Equal(fleetnetv1alpha1.AzureFrontDoorHealthProbeMethodHEAD))
		Expect(attachment.Spec.HealthProbe.Path).To(Equal("/healthz"))
		Expect(attachment.Spec.HealthProbe.IntervalSeconds).To(Equal(int32(30)))
		Expect(attachment.Spec.HealthProbe.SampleSize).To(Equal(int32(4)))
		Expect(attachment.Spec.HealthProbe.SuccessfulSamplesRequired).To(Equal(int32(3)))
		Expect(attachment.Spec.Traffic.DefaultPriority).To(Equal(int32(1)))
		Expect(attachment.Spec.Traffic.DefaultWeight).To(Equal(int32(1000)))
		Expect(attachment.Spec.MemberFailurePolicy).To(Equal(fleetnetv1alpha1.AzureFrontDoorMemberFailurePolicyPartial))
	})

	It("defaults omitted health probe and traffic objects", func() {
		object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(validAFDBackendAttachment())
		Expect(err).NotTo(HaveOccurred())
		unstructured.RemoveNestedField(object, "spec", "healthProbe")
		unstructured.RemoveNestedField(object, "spec", "traffic")

		attachment := &unstructured.Unstructured{Object: object}
		attachment.SetGroupVersionKind(fleetnetv1alpha1.GroupVersion.WithKind("AzureFrontDoorBackendAttachment"))
		Expect(hubClient.Create(ctx, attachment)).To(Succeed())

		defaulted := &fleetnetv1alpha1.AzureFrontDoorBackendAttachment{}
		Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(attachment.Object, defaulted)).To(Succeed())
		Expect(defaulted.Spec.HealthProbe.Protocol).To(Equal(fleetnetv1alpha1.AzureFrontDoorOriginProtocolHTTPS))
		Expect(defaulted.Spec.HealthProbe.Method).To(Equal(fleetnetv1alpha1.AzureFrontDoorHealthProbeMethodHEAD))
		Expect(defaulted.Spec.HealthProbe.Path).To(Equal("/healthz"))
		Expect(defaulted.Spec.HealthProbe.IntervalSeconds).To(Equal(int32(30)))
		Expect(defaulted.Spec.HealthProbe.SampleSize).To(Equal(int32(4)))
		Expect(defaulted.Spec.HealthProbe.SuccessfulSamplesRequired).To(Equal(int32(3)))
		Expect(defaulted.Spec.Traffic.DefaultPriority).To(Equal(int32(1)))
		Expect(defaulted.Spec.Traffic.DefaultWeight).To(Equal(int32(1000)))
	})

	It("rejects Private Link mode without Private Link configuration", func() {
		attachment := validAFDBackendAttachment()
		attachment.Spec.Connectivity.Mode = fleetnetv1alpha1.AzureFrontDoorConnectivityModePrivateLink

		err := hubClient.Create(ctx, attachment)
		Expect(apierrors.IsInvalid(err)).To(BeTrue(), "error: %v", err)
		Expect(err.Error()).To(ContainSubstring("privateLink must be set"))
	})

	It("rejects an invalid health sample count", func() {
		attachment := validAFDBackendAttachment()
		attachment.Spec.HealthProbe.SampleSize = 2
		attachment.Spec.HealthProbe.SuccessfulSamplesRequired = 3

		err := hubClient.Create(ctx, attachment)
		Expect(apierrors.IsInvalid(err)).To(BeTrue(), "error: %v", err)
		Expect(err.Error()).To(ContainSubstring("cannot exceed sampleSize"))
	})

	It("keeps attachment identity immutable", func() {
		attachment := validAFDBackendAttachment()
		Expect(hubClient.Create(ctx, attachment)).To(Succeed())

		attachment.Spec.BackendRef.Port = 8443
		err := hubClient.Update(ctx, attachment)
		Expect(apierrors.IsInvalid(err)).To(BeTrue(), "error: %v", err)
		Expect(err.Error()).To(ContainSubstring("backendRef is immutable"))
	})
})

func validAFDGatewayPolicy() *fleetnetv1alpha1.AzureFrontDoorGatewayPolicy {
	return &fleetnetv1alpha1.AzureFrontDoorGatewayPolicy{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "afd-policy-",
			Namespace:    testNamespace,
		},
		Spec: fleetnetv1alpha1.AzureFrontDoorGatewayPolicySpec{
			TargetRef: gatewayv1.LocalObjectReference{
				Group: gatewayv1.Group(gatewayv1.GroupName),
				Kind:  "Gateway",
				Name:  "global",
			},
			Profile: fleetnetv1alpha1.AzureFrontDoorProfileSpec{
				SKU:           fleetnetv1alpha1.AzureFrontDoorProfileSKUPremium,
				ResourceGroup: "fleet-global",
				Name:          "fleet-global",
			},
			WAF: fleetnetv1alpha1.AzureFrontDoorWAFSpec{
				PolicyResourceID: "/subscriptions/sub/resourceGroups/security/providers/Microsoft.Network/frontDoorWebApplicationFirewallPolicies/fleet-waf",
			},
		},
	}
}

func validAFDBackendAttachment() *fleetnetv1alpha1.AzureFrontDoorBackendAttachment {
	return &fleetnetv1alpha1.AzureFrontDoorBackendAttachment{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "afd-attachment-",
			Namespace:    testNamespace,
		},
		Spec: fleetnetv1alpha1.AzureFrontDoorBackendAttachmentSpec{
			GatewayRef: gatewayv1.LocalObjectReference{
				Group: gatewayv1.Group(gatewayv1.GroupName),
				Kind:  "Gateway",
				Name:  "global",
			},
			BackendRef: fleetnetv1alpha1.AzureFrontDoorBackendReference{
				LocalObjectReference: gatewayv1.LocalObjectReference{
					Group: gatewayv1.Group(fleetnetv1alpha1.GroupVersion.Group),
					Kind:  "ServiceImport",
					Name:  "store",
				},
				Port: 443,
			},
			Connectivity: fleetnetv1alpha1.AzureFrontDoorConnectivitySpec{
				Mode: fleetnetv1alpha1.AzureFrontDoorConnectivityModePublic,
			},
			Origin: fleetnetv1alpha1.AzureFrontDoorOriginSpec{
				HostHeader: "store.internal.contoso.example",
			},
		},
	}
}
