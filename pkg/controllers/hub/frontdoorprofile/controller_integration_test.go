/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package frontdoorprofile

import (
	"fmt"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/frontdoor/armfrontdoor"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/test/common/azurefrontdoor/fakeprovider"
)

// Minimal integration coverage for the FrontDoorProfile controller. The suite
// intentionally starts with just the happy path (create -> Programmed=True
// with hostname populated) so the scaffolding is proven and later work items
// (WAFPolicy, ComplianceMode, error injection) can grow specs incrementally.

const (
	// eventuallyTimeout is generous because envtest's initial informer sync
	// plus the fake LRO round-trip can take a couple of hundred ms on a cold
	// cache; a shorter budget is flaky under CI load.
	eventuallyTimeout  = 30 * time.Second
	eventuallyInterval = 250 * time.Millisecond
)

var _ = Describe("FrontDoorProfile Controller Integration", func() {
	Context("Happy path — create a FrontDoorProfile", func() {
		const profileName = "test-happy-path"

		AfterEach(func() {
			// Delete the CR to exercise the finalizer path so state does not
			// leak between specs. Not-found on Delete is fine because a
			// failing test may already have deleted it.
			profile := &fleetnetv1alpha1.FrontDoorProfile{}
			key := types.NamespacedName{Namespace: testNamespace, Name: profileName}
			if err := k8sClient.Get(ctx, key, profile); err == nil {
				Expect(k8sClient.Delete(ctx, profile)).To(Succeed())
			}
			// Wait for the finalizer to run and the CR to disappear so the
			// next spec starts from a clean state.
			Eventually(func() bool {
				err := k8sClient.Get(ctx, key, &fleetnetv1alpha1.FrontDoorProfile{})
				return err != nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue(), "profile should be deleted after finalizer runs")
		})

		It("should program the AFD profile and default endpoint", func() {
			By("creating a FrontDoorProfile CR")
			profile := &fleetnetv1alpha1.FrontDoorProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      profileName,
					Namespace: testNamespace,
				},
				Spec: fleetnetv1alpha1.FrontDoorProfileSpec{
					ResourceGroup: fakeprovider.DefaultResourceGroupName,
					Sku:           fleetnetv1alpha1.FrontDoorProfileSkuPremium,
				},
			}
			Expect(k8sClient.Create(ctx, profile)).To(Succeed())

			By("waiting for Programmed=True condition")
			Eventually(func(g Gomega) {
				got := &fleetnetv1alpha1.FrontDoorProfile{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: profileName}, got)).To(Succeed())

				cond := meta.FindStatusCondition(got.Status.Conditions,
					string(fleetnetv1alpha1.FrontDoorProfileConditionProgrammed))
				g.Expect(cond).NotTo(BeNil(), "Programmed condition should be present")
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal(string(fleetnetv1alpha1.FrontDoorProfileReasonProgrammed)))
				g.Expect(cond.ObservedGeneration).To(Equal(got.Generation))

				// EndpointHostname is derived from the fake's synthetic
				// AFDEndpointProperties.HostName (fakeprovider.EndpointHostnameFormat).
				g.Expect(got.Status.EndpointHostname).NotTo(BeNil())
				g.Expect(*got.Status.EndpointHostname).To(Equal(fmt.Sprintf(fakeprovider.EndpointHostnameFormat, AzureEndpointName(got))))

				// ResourceID uses the RG-and-below suffix format
				// (see azureResourceIDForProfile). Assert the shape rather
				// than the exact UID (UID is server-assigned).
				g.Expect(got.Status.ResourceID).To(Equal(fmt.Sprintf(
					"/resourceGroups/%s/providers/Microsoft.Cdn/profiles/%s",
					fakeprovider.DefaultResourceGroupName, AzureProfileName(got))))

				// Finalizer must be present so the controller can clean up
				// Azure state on delete.
				g.Expect(got.Finalizers).To(ContainElement(objectmeta.FrontDoorProfileFinalizer))
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		})
	})

	// deleteProfile is a shared teardown helper for the WAF specs below.
	// Mirrors the happy-path Context's AfterEach: delete the CR (if present)
	// and wait for the finalizer to run so the next spec starts clean.
	// Factored here rather than duplicated so a change to teardown semantics
	// (e.g. tightening the timeout) lands in one place.
	deleteProfile := func(profileName string) {
		key := types.NamespacedName{Namespace: testNamespace, Name: profileName}
		profile := &fleetnetv1alpha1.FrontDoorProfile{}
		if err := k8sClient.Get(ctx, key, profile); err == nil {
			Expect(k8sClient.Delete(ctx, profile)).To(Succeed())
		}
		Eventually(func() bool {
			return k8sClient.Get(ctx, key, &fleetnetv1alpha1.FrontDoorProfile{}) != nil
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue(),
			"profile %s should be deleted after finalizer runs", profileName)
	}

	Context("WAF happy path — resolve + attach a Prevention-mode policy", func() {
		const (
			profileName = "test-waf-happy"
			wafRG       = fakeprovider.DefaultResourceGroupName
			wafName     = "waf-happy"
		)

		BeforeEach(func() {
			// Seed the referenced WAF policy in Prevention mode BEFORE
			// creating the profile CR so the very first reconcile pass
			// resolves it. If we seeded after, the first reconcile would
			// briefly observe WAFPolicyNotFound and flake the assertion.
			wafPolicyFake.SetPolicy(wafRG, wafName, armfrontdoor.PolicyModePrevention)
		})

		AfterEach(func() {
			deleteProfile(profileName)
			wafPolicyFake.DeletePolicy(wafRG, wafName)
		})

		It("should program the profile AND write a SecurityPolicy binding the WAF policy to the default endpoint", func() {
			By("creating a FrontDoorProfile CR that references the seeded WAF policy")
			wafID := fakeprovider.FormatWAFPolicyResourceID(wafRG, wafName)
			profile := &fleetnetv1alpha1.FrontDoorProfile{
				ObjectMeta: metav1.ObjectMeta{Name: profileName, Namespace: testNamespace},
				Spec: fleetnetv1alpha1.FrontDoorProfileSpec{
					ResourceGroup: fakeprovider.DefaultResourceGroupName,
					Sku:           fleetnetv1alpha1.FrontDoorProfileSkuPremium,
					WAFPolicy:     &fleetnetv1alpha1.FrontDoorWAFPolicyRef{ResourceID: wafID},
				},
			}
			Expect(k8sClient.Create(ctx, profile)).To(Succeed())

			By("waiting for Programmed=True and a SecurityPolicy stored in the fake")
			Eventually(func(g Gomega) {
				got := &fleetnetv1alpha1.FrontDoorProfile{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: profileName}, got)).To(Succeed())

				cond := meta.FindStatusCondition(got.Status.Conditions,
					string(fleetnetv1alpha1.FrontDoorProfileConditionProgrammed))
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal(string(fleetnetv1alpha1.FrontDoorProfileReasonProgrammed)))

				// SecurityPolicy is named deterministically off the CR UID
				// so the reconciler and the assertion agree without
				// coordinating out-of-band.
				secName := fmt.Sprintf("fleet-waf-%s", got.UID)
				_, ok := securityPolicyFake.GetStored(AzureProfileName(got), secName)
				g.Expect(ok).To(BeTrue(), "SecurityPolicy %s should be stored in the fake", secName)
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

			By("asserting the stored SecurityPolicy binds the correct WAF policy and endpoint")
			got := &fleetnetv1alpha1.FrontDoorProfile{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: profileName}, got)).To(Succeed())
			secName := fmt.Sprintf("fleet-waf-%s", got.UID)
			stored, ok := securityPolicyFake.GetStored(AzureProfileName(got), secName)
			Expect(ok).To(BeTrue())
			// Parameters is a classification interface (SDK-level union to
			// leave room for future policy types). Type-assert to the WAF
			// variant to inspect the fields we actually populate.
			Expect(stored.Properties).NotTo(BeNil())
			wafParams, isWAF := stored.Properties.Parameters.(*armcdn.SecurityPolicyWebApplicationFirewallParameters)
			Expect(isWAF).To(BeTrue(), "stored SecurityPolicy should carry WebApplicationFirewall params")
			Expect(wafParams.WafPolicy).NotTo(BeNil())
			Expect(wafParams.WafPolicy.ID).NotTo(BeNil())
			Expect(*wafParams.WafPolicy.ID).To(Equal(wafID))
			Expect(wafParams.Associations).To(HaveLen(1))
			Expect(wafParams.Associations[0].Domains).To(HaveLen(1))
			Expect(wafParams.Associations[0].Domains[0].ID).NotTo(BeNil())
			// Endpoint ID uses the fake's synthetic format so we can
			// assert exact equality (proves the reconciler propagated the
			// Get response's ID into the SecurityPolicy Association).
			expectedEndpointID := fmt.Sprintf(fakeprovider.EndpointResourceIDFormat,
				fakeprovider.DefaultSubscriptionID, fakeprovider.DefaultResourceGroupName,
				AzureProfileName(got), AzureEndpointName(got))
			Expect(*wafParams.Associations[0].Domains[0].ID).To(Equal(expectedEndpointID))
		})
	})

	Context("WAF NotFound — reference an unseeded policy", func() {
		const profileName = "test-waf-notfound"

		AfterEach(func() { deleteProfile(profileName) })

		It("should surface Programmed=False with Reason=WAFPolicyNotFound", func() {
			// Deliberately DO NOT seed the referenced policy so the WAF
			// resolver's Get returns 404. Uses a distinctive name so a
			// future test that DOES seed cannot collide.
			wafID := fakeprovider.FormatWAFPolicyResourceID(fakeprovider.DefaultResourceGroupName, "waf-never-seeded")
			profile := &fleetnetv1alpha1.FrontDoorProfile{
				ObjectMeta: metav1.ObjectMeta{Name: profileName, Namespace: testNamespace},
				Spec: fleetnetv1alpha1.FrontDoorProfileSpec{
					ResourceGroup: fakeprovider.DefaultResourceGroupName,
					Sku:           fleetnetv1alpha1.FrontDoorProfileSkuPremium,
					WAFPolicy:     &fleetnetv1alpha1.FrontDoorWAFPolicyRef{ResourceID: wafID},
				},
			}
			Expect(k8sClient.Create(ctx, profile)).To(Succeed())

			By("waiting for Programmed=False with Reason=WAFPolicyNotFound")
			Eventually(func(g Gomega) {
				got := &fleetnetv1alpha1.FrontDoorProfile{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: profileName}, got)).To(Succeed())

				cond := meta.FindStatusCondition(got.Status.Conditions,
					string(fleetnetv1alpha1.FrontDoorProfileConditionProgrammed))
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal(string(fleetnetv1alpha1.FrontDoorProfileReasonWAFPolicyNotFound)))
				g.Expect(cond.Message).To(ContainSubstring(wafID),
					"WAFPolicyNotFound message should include the exact ARM ID so operators can diff spec vs Azure")
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		})
	})

	Context("WAF SFI-NS253 rejects Detection-mode policy", func() {
		const (
			profileName = "test-waf-sfi-detection"
			wafRG       = fakeprovider.DefaultResourceGroupName
			wafName     = "waf-detection"
		)

		BeforeEach(func() {
			// Seed a valid (existent) policy but in Detection mode. This
			// exercises the SFI-specific tightening: without SFI this
			// would be accepted; with SFI it must be rejected because
			// Detection-mode WAF only logs, it does not block, which
			// violates the NS253 "block by default" posture.
			wafPolicyFake.SetPolicy(wafRG, wafName, armfrontdoor.PolicyModeDetection)
		})

		AfterEach(func() {
			deleteProfile(profileName)
			wafPolicyFake.DeletePolicy(wafRG, wafName)
		})

		It("should surface Programmed=False with Reason=WAFPolicyNotInPreventionMode", func() {
			wafID := fakeprovider.FormatWAFPolicyResourceID(wafRG, wafName)
			profile := &fleetnetv1alpha1.FrontDoorProfile{
				ObjectMeta: metav1.ObjectMeta{Name: profileName, Namespace: testNamespace},
				Spec: fleetnetv1alpha1.FrontDoorProfileSpec{
					ResourceGroup:  fakeprovider.DefaultResourceGroupName,
					Sku:            fleetnetv1alpha1.FrontDoorProfileSkuPremium,
					ComplianceMode: fleetnetv1alpha1.FrontDoorProfileComplianceModeSFINS253,
					WAFPolicy:      &fleetnetv1alpha1.FrontDoorWAFPolicyRef{ResourceID: wafID},
				},
			}
			Expect(k8sClient.Create(ctx, profile)).To(Succeed())

			By("waiting for Programmed=False with Reason=WAFPolicyNotInPreventionMode")
			Eventually(func(g Gomega) {
				got := &fleetnetv1alpha1.FrontDoorProfile{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: profileName}, got)).To(Succeed())

				cond := meta.FindStatusCondition(got.Status.Conditions,
					string(fleetnetv1alpha1.FrontDoorProfileConditionProgrammed))
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal(string(fleetnetv1alpha1.FrontDoorProfileReasonWAFPolicyNotInPreventionMode)))
				g.Expect(cond.Message).To(ContainSubstring("Prevention"),
					"message should name the required mode so operators know exactly what to fix")

				// Sanity: no SecurityPolicy should have been written — the
				// reconciler short-circuits before the attach step on this
				// rejection. Asserts we do not leak a partial WAF binding
				// with a Detection-mode policy behind it.
				secName := fmt.Sprintf("fleet-waf-%s", got.UID)
				_, ok := securityPolicyFake.GetStored(AzureProfileName(got), secName)
				g.Expect(ok).To(BeFalse(), "no SecurityPolicy should be attached when SFI rejects the WAF policy")
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		})
	})
})
