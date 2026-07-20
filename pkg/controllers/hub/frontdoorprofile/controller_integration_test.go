/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package frontdoorprofile

import (
	"fmt"
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
})
