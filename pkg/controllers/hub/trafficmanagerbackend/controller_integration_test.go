/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package trafficmanagerbackend

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/test/common/trafficmanager/fakeprovider"
	"go.goms.io/fleet-networking/test/common/trafficmanager/validator"
)

func trafficManagerBackendForTest(name, profileName, serviceImportName string) *fleetnetv1alpha1.TrafficManagerBackend {
	return &fleetnetv1alpha1.TrafficManagerBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: fleetnetv1alpha1.TrafficManagerBackendSpec{
			Profile: fleetnetv1alpha1.TrafficManagerProfileRef{
				Name: profileName,
			},
			Backend: fleetnetv1alpha1.TrafficManagerBackendRef{
				Name: serviceImportName,
			},
			Weight: ptr.To(int64(10)),
		},
	}
}

func trafficManagerProfileForTest(name string) *fleetnetv1alpha1.TrafficManagerProfile {
	return &fleetnetv1alpha1.TrafficManagerProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: fleetnetv1alpha1.TrafficManagerProfileSpec{},
	}
}

var _ = Describe("Test TrafficManagerBackend Controller", func() {
	Context("When creating trafficManagerBackend with invalid profile", Ordered, func() {
		name := fakeprovider.ValidBackendName
		namespacedName := types.NamespacedName{Namespace: testNamespace, Name: name}
		var backend *fleetnetv1alpha1.TrafficManagerBackend

		It("Creating TrafficManagerBackend", func() {
			backend = trafficManagerBackendForTest(name, "not-exist", "not-exist")
			Expect(k8sClient.Create(ctx, backend)).Should(Succeed())
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1alpha1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       name,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1alpha1.TrafficManagerBackendStatus{
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionFalse,
							Type:   string(fleetnetv1alpha1.TrafficManagerBackendReasonAccepted),
							Reason: string(fleetnetv1alpha1.TrafficManagerBackendReasonInvalid),
						},
					},
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want)
		})

		It("Deleting trafficManagerBackend", func() {
			err := k8sClient.Delete(ctx, backend)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerBackend")
		})

		It("Validating trafficManagerBackend is deleted", func() {
			validator.IsTrafficManagerBackendDeleted(ctx, k8sClient, namespacedName)
		})
	})

	Context("When creating trafficManagerBackend with not found Azure Traffic Manager profile", Ordered, func() {
		profileName := "not-found-azure-profile"
		profileNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: profileName}
		var profile *fleetnetv1alpha1.TrafficManagerProfile
		backendName := fakeprovider.ValidBackendName
		backendNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: backendName}
		var backend *fleetnetv1alpha1.TrafficManagerBackend

		It("Creating a new TrafficManagerProfile", func() {
			By("By creating a new TrafficManagerProfile")
			profile = trafficManagerProfileForTest(profileName)
			Expect(k8sClient.Create(ctx, profile)).Should(Succeed())
		})

		It("Creating TrafficManagerBackend", func() {
			backend = trafficManagerBackendForTest(backendName, profileName, "not-exist")
			Expect(k8sClient.Create(ctx, backend)).Should(Succeed())
		})

		It("Validating trafficManagerBackend", func() {
			validator.IsTrafficManagerBackendFinalizerAdded(ctx, k8sClient, backendNamespacedName)
		})

		It("Deleting trafficManagerBackend", func() {
			err := k8sClient.Delete(ctx, backend)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerBackend")
		})

		It("Validating trafficManagerBackend is deleted", func() {
			validator.IsTrafficManagerBackendDeleted(ctx, k8sClient, backendNamespacedName)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating trafficManagerProfile is deleted", func() {
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, profileNamespacedName)
		})
	})

	Context("When creating trafficManagerBackend with Azure Traffic Manager profile which has nil properties", Ordered, func() {
		profileName := fakeprovider.ValidProfileWithNilPropertiesName
		profileNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: profileName}
		var profile *fleetnetv1alpha1.TrafficManagerProfile
		backendName := fakeprovider.ValidBackendName
		backendNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: backendName}
		var backend *fleetnetv1alpha1.TrafficManagerBackend

		It("Creating a new TrafficManagerProfile", func() {
			By("By creating a new TrafficManagerProfile")
			profile = trafficManagerProfileForTest(profileName)
			Expect(k8sClient.Create(ctx, profile)).Should(Succeed())
		})

		It("Creating TrafficManagerBackend", func() {
			backend = trafficManagerBackendForTest(backendName, profileName, "not-exist")
			Expect(k8sClient.Create(ctx, backend)).Should(Succeed())
		})

		It("Validating trafficManagerBackend", func() {
			validator.IsTrafficManagerBackendFinalizerAdded(ctx, k8sClient, backendNamespacedName)
		})

		It("Deleting trafficManagerBackend", func() {
			err := k8sClient.Delete(ctx, backend)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerBackend")
		})

		It("Validating trafficManagerBackend is deleted", func() {
			validator.IsTrafficManagerBackendDeleted(ctx, k8sClient, backendNamespacedName)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating trafficManagerProfile is deleted", func() {
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, profileNamespacedName)
		})
	})

	Context("When creating trafficManagerBackend with valid profile", Ordered, func() {
		profileName := fakeprovider.ValidProfileWithEndpointsName
		profileNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: profileName}
		var profile *fleetnetv1alpha1.TrafficManagerProfile
		backendName := fakeprovider.ValidBackendName
		backendNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: backendName}
		var backend *fleetnetv1alpha1.TrafficManagerBackend

		It("Creating a new TrafficManagerProfile", func() {
			By("By creating a new TrafficManagerProfile")
			profile = trafficManagerProfileForTest(profileName)
			Expect(k8sClient.Create(ctx, profile)).Should(Succeed())
		})

		It("Creating TrafficManagerBackend", func() {
			backend = trafficManagerBackendForTest(backendName, profileName, "not-exist")
			Expect(k8sClient.Create(ctx, backend)).Should(Succeed())
		})

		It("Validating trafficManagerBackend", func() {
			validator.IsTrafficManagerBackendFinalizerAdded(ctx, k8sClient, backendNamespacedName)
		})

		It("Deleting trafficManagerBackend", func() {
			err := k8sClient.Delete(ctx, backend)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerBackend")
		})

		It("Validating trafficManagerBackend is deleted", func() {
			validator.IsTrafficManagerBackendDeleted(ctx, k8sClient, backendNamespacedName)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating trafficManagerProfile is deleted", func() {
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, profileNamespacedName)
		})
	})

	Context("When creating trafficManagerBackend with not accepted profile", Ordered, func() {
		profileName := fakeprovider.ValidProfileWithEndpointsName
		profileNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: profileName}
		var profile *fleetnetv1alpha1.TrafficManagerProfile
		backendName := fakeprovider.ValidBackendName
		backendNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: backendName}
		var backend *fleetnetv1alpha1.TrafficManagerBackend

		It("Creating a new TrafficManagerProfile", func() {
			By("By creating a new TrafficManagerProfile")
			profile = trafficManagerProfileForTest(profileName)
			Expect(k8sClient.Create(ctx, profile)).Should(Succeed())
		})

		It("Updating TrafficManagerProfile status to accepted false", func() {
			By("By updating TrafficManagerProfile status")
			cond := metav1.Condition{
				Status:             metav1.ConditionFalse,
				Type:               string(fleetnetv1alpha1.TrafficManagerProfileConditionProgrammed),
				ObservedGeneration: profile.Generation,
				Reason:             string(fleetnetv1alpha1.TrafficManagerProfileReasonInvalid),
			}
			meta.SetStatusCondition(&profile.Status.Conditions, cond)
			Expect(k8sClient.Status().Update(ctx, profile)).Should(Succeed())
		})

		It("Creating TrafficManagerBackend", func() {
			backend = trafficManagerBackendForTest(backendName, profileName, "not-exist")
			Expect(k8sClient.Create(ctx, backend)).Should(Succeed())
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1alpha1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1alpha1.TrafficManagerBackendStatus{
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionFalse,
							Type:   string(fleetnetv1alpha1.TrafficManagerBackendReasonAccepted),
							Reason: string(fleetnetv1alpha1.TrafficManagerBackendReasonInvalid),
						},
					},
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want)
		})

		It("Updating TrafficManagerProfile status to accepted unknown and it should trigger controller", func() {
			By("By updating TrafficManagerProfile status")
			cond := metav1.Condition{
				Status:             metav1.ConditionUnknown,
				Type:               string(fleetnetv1alpha1.TrafficManagerProfileConditionProgrammed),
				ObservedGeneration: profile.Generation,
				Reason:             string(fleetnetv1alpha1.TrafficManagerProfileReasonPending),
			}
			meta.SetStatusCondition(&profile.Status.Conditions, cond)
			Expect(k8sClient.Status().Update(ctx, profile)).Should(Succeed())
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1alpha1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1alpha1.TrafficManagerBackendStatus{
					Conditions: []metav1.Condition{
						{
							Status: metav1.ConditionUnknown,
							Type:   string(fleetnetv1alpha1.TrafficManagerBackendReasonAccepted),
							Reason: string(fleetnetv1alpha1.TrafficManagerBackendReasonPending),
						},
					},
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want)
		})

		It("Deleting trafficManagerBackend", func() {
			err := k8sClient.Delete(ctx, backend)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerBackend")
		})

		It("Validating trafficManagerBackend is deleted", func() {
			validator.IsTrafficManagerBackendDeleted(ctx, k8sClient, backendNamespacedName)
		})

		It("Deleting trafficManagerProfile", func() {
			err := k8sClient.Delete(ctx, profile)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerProfile")
		})

		It("Validating trafficManagerProfile is deleted", func() {
			validator.IsTrafficManagerProfileDeleted(ctx, k8sClient, profileNamespacedName)
		})
	})
})
