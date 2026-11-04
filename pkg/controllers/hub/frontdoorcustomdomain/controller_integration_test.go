/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package frontdoorcustomdomain

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/test/common/azurefrontdoor/fakeprovider"
)

// Minimal integration coverage for the FrontDoorCustomDomain controller.
// Covers the two most important POC-scope paths:
//   - Happy path with a Programmed parent profile: create -> Approved ->
//     Programmed=True with DNSValidationToken populated. (Requires the fake
//     custom-domain client to return Approved validation state on create,
//     which it does; see fakeprovider/customdomain.go for the rationale.)
//   - BYOC guard: verify the POC's hard-reject of BYOC surfaces
//     Programmed=False, Reason=Invalid WITHOUT calling Azure or adding a
//     finalizer (D3 semantics).

const (
	eventuallyTimeout  = 30 * time.Second
	eventuallyInterval = 250 * time.Millisecond
)

// stampProfileProgrammed marks a FrontDoorProfile CR as Programmed=True in its
// status subresource. The customdomain controller waits for this condition
// before contacting Azure (see isProfileProgrammed), and the frontdoorprofile
// controller is NOT wired into this suite (that's covered by
// pkg/controllers/hub/frontdoorprofile). So we synthesize it here.
func stampProfileProgrammed(profile *fleetnetv1alpha1.FrontDoorProfile) {
	profile.Status.EndpointHostname = nil
	meta.SetStatusCondition(&profile.Status.Conditions, metav1.Condition{
		Type:               string(fleetnetv1alpha1.FrontDoorProfileConditionProgrammed),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: profile.Generation,
		Reason:             string(fleetnetv1alpha1.FrontDoorProfileReasonProgrammed),
		Message:            "stamped by test",
	})
	Expect(k8sClient.Status().Update(ctx, profile)).To(Succeed())
}

var _ = Describe("FrontDoorCustomDomain Controller Integration", func() {
	Context("Happy path — parent Programmed, Managed TLS", func() {
		const (
			profileName = "test-cd-happy-parent"
			domainName  = "test-cd-happy"
		)

		AfterEach(func() {
			// Delete children first so their finalizers can run against the
			// still-present parent profile (the customdomain reconciler
			// resolves the parent during delete).
			cd := &fleetnetv1alpha1.FrontDoorCustomDomain{}
			cdKey := types.NamespacedName{Namespace: testNamespace, Name: domainName}
			if err := k8sClient.Get(ctx, cdKey, cd); err == nil {
				Expect(k8sClient.Delete(ctx, cd)).To(Succeed())
			}
			Eventually(func() bool {
				return k8sClient.Get(ctx, cdKey, &fleetnetv1alpha1.FrontDoorCustomDomain{}) != nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			profile := &fleetnetv1alpha1.FrontDoorProfile{}
			pKey := types.NamespacedName{Namespace: testNamespace, Name: profileName}
			if err := k8sClient.Get(ctx, pKey, profile); err == nil {
				// The profile has no reconciler in this suite, so it has no
				// finalizer; a plain Delete removes it immediately.
				Expect(k8sClient.Delete(ctx, profile)).To(Succeed())
			}
		})

		It("should program the AFD custom domain and expose the validation token", func() {
			By("creating a Programmed parent FrontDoorProfile")
			profile := &fleetnetv1alpha1.FrontDoorProfile{
				ObjectMeta: metav1.ObjectMeta{Name: profileName, Namespace: testNamespace},
				Spec: fleetnetv1alpha1.FrontDoorProfileSpec{
					ResourceGroup: fakeprovider.DefaultResourceGroupName,
					Sku:           fleetnetv1alpha1.FrontDoorProfileSkuPremium,
				},
			}
			Expect(k8sClient.Create(ctx, profile)).To(Succeed())
			stampProfileProgrammed(profile)

			By("creating a FrontDoorCustomDomain CR")
			cd := &fleetnetv1alpha1.FrontDoorCustomDomain{
				ObjectMeta: metav1.ObjectMeta{Name: domainName, Namespace: testNamespace},
				Spec: fleetnetv1alpha1.FrontDoorCustomDomainSpec{
					ProfileRef: fleetnetv1alpha1.FrontDoorProfileReference{Name: profileName},
					Hostname:   "www.contoso.com",
					TLS:        fleetnetv1alpha1.FrontDoorTLSConfig{Mode: fleetnetv1alpha1.FrontDoorTLSModeManaged},
				},
			}
			Expect(k8sClient.Create(ctx, cd)).To(Succeed())

			By("waiting for Programmed=True with populated status")
			Eventually(func(g Gomega) {
				got := &fleetnetv1alpha1.FrontDoorCustomDomain{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: domainName}, got)).To(Succeed())

				cond := meta.FindStatusCondition(got.Status.Conditions,
					string(fleetnetv1alpha1.FrontDoorCustomDomainConditionProgrammed))
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal(string(fleetnetv1alpha1.FrontDoorCustomDomainReasonProgrammed)))
				g.Expect(cond.ObservedGeneration).To(Equal(got.Generation))

				g.Expect(got.Status.ValidationState).To(Equal(fleetnetv1alpha1.FrontDoorDomainValidationStateApproved))
				g.Expect(got.Status.DNSValidationToken).NotTo(BeNil())
				g.Expect(*got.Status.DNSValidationToken).To(Equal(fakeprovider.FakeValidationToken))
				g.Expect(got.Status.ResourceID).NotTo(BeEmpty())
				g.Expect(got.Finalizers).To(ContainElement(objectmeta.FrontDoorCustomDomainFinalizer))
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		})
	})

	Context("BYOC guard — POC hard-rejects BYOC mode", func() {
		const (
			profileName = "test-cd-byoc-parent"
			domainName  = "test-cd-byoc"
		)

		AfterEach(func() {
			cd := &fleetnetv1alpha1.FrontDoorCustomDomain{}
			cdKey := types.NamespacedName{Namespace: testNamespace, Name: domainName}
			if err := k8sClient.Get(ctx, cdKey, cd); err == nil {
				Expect(k8sClient.Delete(ctx, cd)).To(Succeed())
			}
			profile := &fleetnetv1alpha1.FrontDoorProfile{}
			pKey := types.NamespacedName{Namespace: testNamespace, Name: profileName}
			if err := k8sClient.Get(ctx, pKey, profile); err == nil {
				Expect(k8sClient.Delete(ctx, profile)).To(Succeed())
			}
		})

		It("should surface Programmed=False, Reason=Invalid without adding a finalizer", func() {
			// Parent still needs to exist because the CR is namespaced; the
			// reconciler returns early on BYOC BEFORE resolving the parent
			// (see handleUpdate ordering), so a Programmed parent is NOT
			// required for this branch — a bare CR is enough to trigger it.
			// We still create a parent so the spec matches the shape of the
			// happy-path spec above.
			Expect(k8sClient.Create(ctx, &fleetnetv1alpha1.FrontDoorProfile{
				ObjectMeta: metav1.ObjectMeta{Name: profileName, Namespace: testNamespace},
				Spec: fleetnetv1alpha1.FrontDoorProfileSpec{
					ResourceGroup: fakeprovider.DefaultResourceGroupName,
					Sku:           fleetnetv1alpha1.FrontDoorProfileSkuPremium,
				},
			})).To(Succeed())

			cd := &fleetnetv1alpha1.FrontDoorCustomDomain{
				ObjectMeta: metav1.ObjectMeta{Name: domainName, Namespace: testNamespace},
				Spec: fleetnetv1alpha1.FrontDoorCustomDomainSpec{
					ProfileRef: fleetnetv1alpha1.FrontDoorProfileReference{Name: profileName},
					Hostname:   "www.contoso.com",
					TLS: fleetnetv1alpha1.FrontDoorTLSConfig{
						Mode: fleetnetv1alpha1.FrontDoorTLSModeBYOC,
						KeyVaultCertificate: &fleetnetv1alpha1.FrontDoorKeyVaultCertificate{
							VaultURI:        "https://example.vault.azure.net",
							CertificateName: "example-cert",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, cd)).To(Succeed())

			By("observing Programmed=False, Reason=Invalid with NO finalizer")
			Eventually(func(g Gomega) {
				got := &fleetnetv1alpha1.FrontDoorCustomDomain{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: domainName}, got)).To(Succeed())

				cond := meta.FindStatusCondition(got.Status.Conditions,
					string(fleetnetv1alpha1.FrontDoorCustomDomainConditionProgrammed))
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal(string(fleetnetv1alpha1.FrontDoorCustomDomainReasonInvalid)))

				// D3 semantics: BYOC rejection must NOT install a finalizer
				// (otherwise the CR would be undeletable, since the delete
				// path has no Azure resource to clean up).
				g.Expect(got.Finalizers).NotTo(ContainElement(objectmeta.FrontDoorCustomDomainFinalizer))
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		})
	})
})
