/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package frontdoorbackend

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	fleetnetv1beta1 "go.goms.io/fleet-networking/api/v1beta1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/controllers/hub/frontdoorprofile"
	"go.goms.io/fleet-networking/test/common/azurefrontdoor/fakeprovider"
)

// Integration coverage for the FrontDoorBackend controller. Each Context is
// self-contained: creates its own FrontDoorProfile / FrontDoorBackend and
// (re)deletes them in AfterEach so specs are order-independent.

const (
	eventuallyTimeout  = 30 * time.Second
	eventuallyInterval = 250 * time.Millisecond

	testClusterA = "cluster-a"
	testClusterB = "cluster-b"

	testPLSResourceIDFormat = "/subscriptions/pls-sub/resourceGroups/pls-rg/providers/Microsoft.Network/privateLinkServices/%s"
)

// markProfileProgrammed flips a FrontDoorProfile to Programmed=True so the
// FrontDoorBackend reconciler sees it as ready. In real life this comes from
// the FrontDoorProfile reconciler; here we do it directly to keep specs
// focused on the backend under test.
func markProfileProgrammed(profile *fleetnetv1alpha1.FrontDoorProfile) {
	fresh := &fleetnetv1alpha1.FrontDoorProfile{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: profile.Namespace, Name: profile.Name}, fresh)).To(Succeed())
	meta.SetStatusCondition(&fresh.Status.Conditions, metav1.Condition{
		Type:               string(fleetnetv1alpha1.FrontDoorProfileConditionProgrammed),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: fresh.Generation,
		Reason:             string(fleetnetv1alpha1.FrontDoorProfileReasonProgrammed),
		Message:            "programmed by test",
	})
	Expect(k8sClient.Status().Update(ctx, fresh)).To(Succeed())
}

// newProfile creates a minimal FrontDoorProfile pointing at the fake's
// DefaultResourceGroupName (so the OriginGroup fake accepts our writes).
func newProfile(name string) *fleetnetv1alpha1.FrontDoorProfile {
	return &fleetnetv1alpha1.FrontDoorProfile{
		ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: name},
		Spec: fleetnetv1alpha1.FrontDoorProfileSpec{
			ResourceGroup: fakeprovider.DefaultResourceGroupName,
			Sku:           fleetnetv1alpha1.FrontDoorProfileSkuPremium,
		},
	}
}

func newServiceImport(name string) *fleetnetv1alpha1.ServiceImport {
	return &fleetnetv1alpha1.ServiceImport{
		ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: name},
	}
}

func setServiceImportClusters(si *fleetnetv1alpha1.ServiceImport, clusters ...string) {
	fresh := &fleetnetv1alpha1.ServiceImport{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: si.Namespace, Name: si.Name}, fresh)).To(Succeed())
	fresh.Status.Clusters = nil
	for _, c := range clusters {
		fresh.Status.Clusters = append(fresh.Status.Clusters, fleetnetv1alpha1.ClusterStatus{Cluster: c})
	}
	Expect(k8sClient.Status().Update(ctx, fresh)).To(Succeed())
}

// newInternalServiceExport builds an InternalServiceExport in
// ExportMode=L7-FrontDoor with the given PLS resource ID; the reconciler's
// filterEligibleExports treats these as programmable origins.
func newInternalServiceExport(name, svcName, cluster string, weight int64, withPLS bool) *fleetnetv1alpha1.InternalServiceExport {
	exp := &fleetnetv1alpha1.InternalServiceExport{
		ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: name},
		Spec: fleetnetv1alpha1.InternalServiceExportSpec{
			// Ports is required by the CRD; content is irrelevant to
			// the FrontDoorBackend reconciler which only looks at
			// ExportMode + PrivateLinkServiceResourceID.
			Ports: []fleetnetv1alpha1.ServicePort{{
				Protocol: corev1.ProtocolTCP,
				Port:     80,
			}},
			ServiceReference: fleetnetv1alpha1.ExportedObjectReference{
				ClusterID:       cluster,
				Kind:            "Service",
				Namespace:       testNamespace,
				Name:            svcName,
				ResourceVersion: "1",
				Generation:      1,
				UID:             types.UID(fmt.Sprintf("uid-%s-%s", cluster, svcName)),
				NamespacedName:  fmt.Sprintf("%s/%s", testNamespace, svcName),
			},
			Weight:     ptr.To(weight),
			ExportMode: objectmeta.ExportModeValueFrontDoor,
		},
	}
	if withPLS {
		exp.Spec.PrivateLinkServiceResourceID = ptr.To(fmt.Sprintf(testPLSResourceIDFormat, cluster))
	}
	return exp
}

func acceptedCondition(nn types.NamespacedName) *metav1.Condition {
	backend := &fleetnetv1alpha1.FrontDoorBackend{}
	if err := k8sClient.Get(ctx, nn, backend); err != nil {
		return nil
	}
	return meta.FindStatusCondition(backend.Status.Conditions, string(fleetnetv1alpha1.FrontDoorBackendConditionAccepted))
}

var _ = Describe("FrontDoorBackend Controller Integration", func() {
	// Every spec constructs its own profile+backend+import; AfterEach
	// unconditionally deletes them. Using unique names per Context keeps
	// specs order-independent even if a previous AfterEach flakes.

	Context("Happy path — Programmed with two eligible exports", func() {
		var (
			profileName = "afdp-happy"
			backendName = "afdb-happy"
			svcName     = "svc-happy"
		)

		It("programs one OriginGroup and one Origin per eligible export", func() {
			By("creating a Programmed FrontDoorProfile")
			profile := newProfile(profileName)
			Expect(k8sClient.Create(ctx, profile)).To(Succeed())
			markProfileProgrammed(profile)

			By("creating a ServiceImport with two member-cluster entries")
			si := newServiceImport(svcName)
			Expect(k8sClient.Create(ctx, si)).To(Succeed())
			setServiceImportClusters(si, testClusterA, testClusterB)

			By("creating two eligible InternalServiceExports")
			expA := newInternalServiceExport("ise-a", svcName, testClusterA, 2, true)
			expB := newInternalServiceExport("ise-b", svcName, testClusterB, 1, true)
			Expect(k8sClient.Create(ctx, expA)).To(Succeed())
			Expect(k8sClient.Create(ctx, expB)).To(Succeed())

			By("creating the FrontDoorBackend")
			backend := &fleetnetv1alpha1.FrontDoorBackend{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: backendName},
				Spec: fleetnetv1alpha1.FrontDoorBackendSpec{
					Profile: fleetnetv1alpha1.FrontDoorProfileRef{Name: profileName},
					Backend: fleetnetv1alpha1.FrontDoorBackendRef{Name: svcName},
					Weight:  ptr.To[int64](100),
				},
			}
			Expect(k8sClient.Create(ctx, backend)).To(Succeed())
			nn := types.NamespacedName{Namespace: testNamespace, Name: backendName}

			By("expecting Accepted=True with two origins programmed")
			Eventually(func(g Gomega) {
				cond := acceptedCondition(nn)
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal(string(fleetnetv1alpha1.FrontDoorBackendReasonAccepted)))

				// Fetch fresh copy so we see the reconciler's UID
				// after Kubernetes assigned it.
				fresh := &fleetnetv1alpha1.FrontDoorBackend{}
				g.Expect(k8sClient.Get(ctx, nn, fresh)).To(Succeed())
				azProfile := frontdoorprofile.AzureProfileName(profile)
				azOG := AzureOriginGroupName(fresh)
				g.Expect(originGroupFake.Has(azProfile, azOG)).To(BeTrue())
				g.Expect(originFake.Count(azProfile, azOG)).To(Equal(2))
				g.Expect(fresh.Status.Origins).To(HaveLen(2))
				g.Expect(fresh.Status.OriginGroupResourceID).NotTo(BeEmpty())
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		})

		AfterEach(func() {
			cleanupNamespaceObjects(profileName, backendName, svcName)
		})
	})

	Context("Pending — parent profile not yet Programmed", func() {
		var (
			profileName = "afdp-pending"
			backendName = "afdb-pending"
			svcName     = "svc-pending"
		)

		It("stays Accepted=Unknown/Pending until the profile flips", func() {
			By("creating a FrontDoorProfile WITHOUT Programmed=True")
			profile := newProfile(profileName)
			Expect(k8sClient.Create(ctx, profile)).To(Succeed())

			si := newServiceImport(svcName)
			Expect(k8sClient.Create(ctx, si)).To(Succeed())
			setServiceImportClusters(si, testClusterA)

			exp := newInternalServiceExport("ise-p", svcName, testClusterA, 1, true)
			Expect(k8sClient.Create(ctx, exp)).To(Succeed())

			backend := &fleetnetv1alpha1.FrontDoorBackend{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: backendName},
				Spec: fleetnetv1alpha1.FrontDoorBackendSpec{
					Profile: fleetnetv1alpha1.FrontDoorProfileRef{Name: profileName},
					Backend: fleetnetv1alpha1.FrontDoorBackendRef{Name: svcName},
					Weight:  ptr.To[int64](1),
				},
			}
			Expect(k8sClient.Create(ctx, backend)).To(Succeed())
			nn := types.NamespacedName{Namespace: testNamespace, Name: backendName}

			By("expecting Accepted=Unknown/Pending")
			Eventually(func(g Gomega) {
				cond := acceptedCondition(nn)
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionUnknown))
				g.Expect(cond.Reason).To(Equal(string(fleetnetv1alpha1.FrontDoorBackendReasonPending)))
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

			By("flipping the profile to Programmed=True")
			markProfileProgrammed(profile)

			By("expecting the backend to converge to Accepted=True (Profile watch wakes it)")
			Eventually(func(g Gomega) {
				cond := acceptedCondition(nn)
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		})

		AfterEach(func() {
			cleanupNamespaceObjects(profileName, backendName, svcName)
		})
	})

	Context("Conflict — a TrafficManagerBackend already claims the same ServiceImport", func() {
		var (
			profileName = "afdp-conflict"
			backendName = "afdb-conflict"
			svcName     = "svc-conflict"
			tmbName     = "tmb-conflict"
		)

		It("refuses to program and sets Reason=Conflict, then clears when TMB is deleted", func() {
			profile := newProfile(profileName)
			Expect(k8sClient.Create(ctx, profile)).To(Succeed())
			markProfileProgrammed(profile)

			si := newServiceImport(svcName)
			Expect(k8sClient.Create(ctx, si)).To(Succeed())
			setServiceImportClusters(si, testClusterA)

			exp := newInternalServiceExport("ise-c", svcName, testClusterA, 1, true)
			Expect(k8sClient.Create(ctx, exp)).To(Succeed())

			By("creating a TrafficManagerBackend that claims the same ServiceImport")
			tmb := &fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: tmbName},
				Spec: fleetnetv1beta1.TrafficManagerBackendSpec{
					Profile: fleetnetv1beta1.TrafficManagerProfileRef{Name: "atm-profile-not-used"},
					Backend: fleetnetv1beta1.TrafficManagerBackendRef{Name: svcName},
					Weight:  ptr.To[int64](1),
				},
			}
			Expect(k8sClient.Create(ctx, tmb)).To(Succeed())

			backend := &fleetnetv1alpha1.FrontDoorBackend{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: backendName},
				Spec: fleetnetv1alpha1.FrontDoorBackendSpec{
					Profile: fleetnetv1alpha1.FrontDoorProfileRef{Name: profileName},
					Backend: fleetnetv1alpha1.FrontDoorBackendRef{Name: svcName},
					Weight:  ptr.To[int64](1),
				},
			}
			Expect(k8sClient.Create(ctx, backend)).To(Succeed())
			nn := types.NamespacedName{Namespace: testNamespace, Name: backendName}

			By("expecting Accepted=False/Reason=Conflict")
			Eventually(func(g Gomega) {
				cond := acceptedCondition(nn)
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal(string(fleetnetv1alpha1.FrontDoorBackendReasonConflict)))
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

			By("deleting the TMB")
			Expect(k8sClient.Delete(ctx, tmb)).To(Succeed())

			By("expecting the backend to converge to Accepted=True once the guard clears")
			Eventually(func(g Gomega) {
				cond := acceptedCondition(nn)
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		})

		AfterEach(func() {
			cleanupNamespaceObjects(profileName, backendName, svcName)
			// TMB may have already been deleted mid-spec; ignore NotFound.
			tmb := &fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: tmbName},
			}
			_ = k8sClient.Delete(ctx, tmb)
		})
	})

	Context("Invalid — parent FrontDoorProfile does not exist", func() {
		var (
			backendName = "afdb-invalid"
			svcName     = "svc-invalid"
		)

		It("sets Accepted=False/Reason=Invalid", func() {
			backend := &fleetnetv1alpha1.FrontDoorBackend{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: backendName},
				Spec: fleetnetv1alpha1.FrontDoorBackendSpec{
					Profile: fleetnetv1alpha1.FrontDoorProfileRef{Name: "does-not-exist"},
					Backend: fleetnetv1alpha1.FrontDoorBackendRef{Name: svcName},
					Weight:  ptr.To[int64](1),
				},
			}
			Expect(k8sClient.Create(ctx, backend)).To(Succeed())
			nn := types.NamespacedName{Namespace: testNamespace, Name: backendName}

			Eventually(func(g Gomega) {
				cond := acceptedCondition(nn)
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal(string(fleetnetv1alpha1.FrontDoorBackendReasonInvalid)))
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		})

		AfterEach(func() {
			cleanupNamespaceObjects("", backendName, svcName)
		})
	})

	Context("Deletion — origin group is cleaned up", func() {
		var (
			profileName = "afdp-del"
			backendName = "afdb-del"
			svcName     = "svc-del"
		)

		It("deletes the OriginGroup and removes the finalizer", func() {
			profile := newProfile(profileName)
			Expect(k8sClient.Create(ctx, profile)).To(Succeed())
			markProfileProgrammed(profile)

			si := newServiceImport(svcName)
			Expect(k8sClient.Create(ctx, si)).To(Succeed())
			setServiceImportClusters(si, testClusterA)

			exp := newInternalServiceExport("ise-d", svcName, testClusterA, 1, true)
			Expect(k8sClient.Create(ctx, exp)).To(Succeed())

			backend := &fleetnetv1alpha1.FrontDoorBackend{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: backendName},
				Spec: fleetnetv1alpha1.FrontDoorBackendSpec{
					Profile: fleetnetv1alpha1.FrontDoorProfileRef{Name: profileName},
					Backend: fleetnetv1alpha1.FrontDoorBackendRef{Name: svcName},
					Weight:  ptr.To[int64](1),
				},
			}
			Expect(k8sClient.Create(ctx, backend)).To(Succeed())
			nn := types.NamespacedName{Namespace: testNamespace, Name: backendName}

			By("waiting until the backend has been programmed")
			var azOG string
			Eventually(func(g Gomega) {
				cond := acceptedCondition(nn)
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				fresh := &fleetnetv1alpha1.FrontDoorBackend{}
				g.Expect(k8sClient.Get(ctx, nn, fresh)).To(Succeed())
				azOG = AzureOriginGroupName(fresh)
				g.Expect(originGroupFake.Has(frontdoorprofile.AzureProfileName(profile), azOG)).To(BeTrue())
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

			By("deleting the backend")
			Expect(k8sClient.Delete(ctx, backend)).To(Succeed())

			By("expecting the CR to disappear and the OriginGroup to be deleted from Azure")
			Eventually(func(g Gomega) {
				fresh := &fleetnetv1alpha1.FrontDoorBackend{}
				err := k8sClient.Get(ctx, nn, fresh)
				g.Expect(err).To(HaveOccurred())
				g.Expect(originGroupFake.Has(frontdoorprofile.AzureProfileName(profile), azOG)).To(BeFalse())
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		})

		AfterEach(func() {
			cleanupNamespaceObjects(profileName, backendName, svcName)
		})
	})
})

// cleanupNamespaceObjects removes the profile/backend/serviceImport and all
// InternalServiceExports in the test namespace. Safe against NotFound so
// specs that half-created the fixture still clean up.
func cleanupNamespaceObjects(profileName, backendName, svcName string) {
	if backendName != "" {
		b := &fleetnetv1alpha1.FrontDoorBackend{
			ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: backendName},
		}
		_ = k8sClient.Delete(ctx, b)
		// Wait for finalizer to drain so successive specs don't collide.
		Eventually(func() bool {
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: backendName}, &fleetnetv1alpha1.FrontDoorBackend{}) != nil
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
	}
	if profileName != "" {
		p := &fleetnetv1alpha1.FrontDoorProfile{
			ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: profileName},
		}
		_ = k8sClient.Delete(ctx, p)
	}
	if svcName != "" {
		si := &fleetnetv1alpha1.ServiceImport{
			ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: svcName},
		}
		_ = k8sClient.Delete(ctx, si)
	}
	// Nuke any lingering InternalServiceExports in the namespace.
	list := &fleetnetv1alpha1.InternalServiceExportList{}
	if err := k8sClient.List(ctx, list); err == nil {
		for i := range list.Items {
			if list.Items[i].Namespace == testNamespace {
				_ = k8sClient.Delete(ctx, &list.Items[i])
			}
		}
	}
}
