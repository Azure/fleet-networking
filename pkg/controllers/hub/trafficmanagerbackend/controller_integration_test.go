/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package trafficmanagerbackend

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	fleetnetv1beta1 "go.goms.io/fleet-networking/api/v1beta1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/test/common/trafficmanager/fakeprovider"
	"go.goms.io/fleet-networking/test/common/trafficmanager/validator"
)

const (
	timeout  = time.Second * 10
	interval = time.Millisecond * 250
)

var (
	testNamespace = fakeprovider.ProfileNamespace
	serviceName   = fakeprovider.ServiceImportName
	backendWeight = int64(10)
)

func trafficManagerBackendForTest(name, profileName, serviceImportName string) *fleetnetv1beta1.TrafficManagerBackend {
	return &fleetnetv1beta1.TrafficManagerBackend{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: fleetnetv1beta1.TrafficManagerBackendSpec{
			Profile: fleetnetv1beta1.TrafficManagerProfileRef{
				Name: profileName,
			},
			Backend: fleetnetv1beta1.TrafficManagerBackendRef{
				Name: serviceImportName,
			},
			Weight: ptr.To(backendWeight),
		},
	}
}

func trafficManagerProfileForTest(name string) *fleetnetv1beta1.TrafficManagerProfile {
	return &fleetnetv1beta1.TrafficManagerProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: fleetnetv1beta1.TrafficManagerProfileSpec{
			ResourceGroup: fakeprovider.DefaultResourceGroupName,
		},
	}
}

func buildFalseCondition(generation int64) []metav1.Condition {
	return []metav1.Condition{
		{
			Status:             metav1.ConditionFalse,
			Type:               string(fleetnetv1beta1.TrafficManagerBackendConditionAccepted),
			Reason:             string(fleetnetv1beta1.TrafficManagerBackendReasonInvalid),
			ObservedGeneration: generation,
		},
	}
}

func buildUnknownCondition(generation int64) []metav1.Condition {
	return []metav1.Condition{
		{
			Status:             metav1.ConditionUnknown,
			Type:               string(fleetnetv1beta1.TrafficManagerBackendConditionAccepted),
			Reason:             string(fleetnetv1beta1.TrafficManagerBackendReasonPending),
			ObservedGeneration: generation,
		},
	}
}

func buildTrueCondition(generation int64) []metav1.Condition {
	return []metav1.Condition{
		{
			Status:             metav1.ConditionTrue,
			Type:               string(fleetnetv1beta1.TrafficManagerBackendConditionAccepted),
			Reason:             string(fleetnetv1beta1.TrafficManagerBackendReasonAccepted),
			ObservedGeneration: generation,
		},
	}
}

func updateTrafficManagerProfileStatusToTrue(ctx context.Context, profile *fleetnetv1beta1.TrafficManagerProfile) {
	cond := metav1.Condition{
		Status:             metav1.ConditionTrue,
		Type:               string(fleetnetv1beta1.TrafficManagerProfileConditionProgrammed),
		ObservedGeneration: profile.Generation,
		Reason:             string(fleetnetv1beta1.TrafficManagerProfileReasonProgrammed),
	}
	meta.SetStatusCondition(&profile.Status.Conditions, cond)
	Expect(k8sClient.Status().Update(ctx, profile)).Should(Succeed())
}

var _ = Describe("Test TrafficManagerBackend Controller", func() {
	Context("When creating trafficManagerBackend with invalid profile", Ordered, func() {
		name := fakeprovider.ValidBackendName
		namespacedName := types.NamespacedName{Namespace: testNamespace, Name: name}
		var backend *fleetnetv1beta1.TrafficManagerBackend

		It("Creating TrafficManagerBackend", func() {
			backend = trafficManagerBackendForTest(name, "not-exist", "not-exist")
			Expect(k8sClient.Create(ctx, backend)).Should(Succeed())
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: testNamespace,
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildFalseCondition(backend.Generation),
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
		var profile *fleetnetv1beta1.TrafficManagerProfile
		backendName := fakeprovider.ValidBackendName
		backendNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: backendName}
		var backend *fleetnetv1beta1.TrafficManagerBackend

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
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      backendName,
					Namespace: testNamespace,
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildUnknownCondition(backend.Generation),
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want)
		})

		It("Updating TrafficManagerProfile status to programmed true and it should trigger controller", func() {
			By("By updating TrafficManagerProfile status")
			updateTrafficManagerProfileStatusToTrue(ctx, profile)
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      backendName,
					Namespace: testNamespace,
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildFalseCondition(backend.Generation),
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

	Context("When creating trafficManagerBackend and failing to get Azure Traffic Manager profile", Ordered, func() {
		profileName := fakeprovider.RequestTimeoutProfileName
		profileNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: profileName}
		var profile *fleetnetv1beta1.TrafficManagerProfile
		backendName := fakeprovider.ValidBackendName
		backendNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: backendName}
		var backend *fleetnetv1beta1.TrafficManagerBackend

		It("Creating a new TrafficManagerProfile", func() {
			By("By creating a new TrafficManagerProfile")
			profile = trafficManagerProfileForTest(profileName)
			Expect(k8sClient.Create(ctx, profile)).Should(Succeed())
		})

		It("Updating TrafficManagerProfile status to programmed true", func() {
			By("By updating TrafficManagerProfile status")
			updateTrafficManagerProfileStatusToTrue(ctx, profile)
		})

		It("Creating TrafficManagerBackend", func() {
			backend = trafficManagerBackendForTest(backendName, profileName, "not-exist")
			Expect(k8sClient.Create(ctx, backend)).Should(Succeed())
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      backendName,
					Namespace: testNamespace,
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildUnknownCondition(backend.Generation),
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

	Context("When creating trafficManagerBackend with Azure Traffic Manager profile which has nil properties", Ordered, func() {
		profileName := fakeprovider.ValidProfileWithNilPropertiesName
		profileNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: profileName}
		var profile *fleetnetv1beta1.TrafficManagerProfile
		backendName := fakeprovider.ValidBackendName
		backendNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: backendName}
		var backend *fleetnetv1beta1.TrafficManagerBackend

		It("Creating a new TrafficManagerProfile", func() {
			By("By creating a new TrafficManagerProfile")
			profile = trafficManagerProfileForTest(profileName)
			Expect(k8sClient.Create(ctx, profile)).Should(Succeed())
		})

		It("Updating TrafficManagerProfile status to programmed true", func() {
			By("By updating TrafficManagerProfile status")
			updateTrafficManagerProfileStatusToTrue(ctx, profile)
		})

		It("Creating TrafficManagerBackend", func() {
			backend = trafficManagerBackendForTest(backendName, profileName, "not-exist")
			Expect(k8sClient.Create(ctx, backend)).Should(Succeed())
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      backendName,
					Namespace: testNamespace,
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildFalseCondition(backend.Generation),
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

	Context("When creating trafficManagerBackend with not accepted profile", Ordered, func() {
		profileName := fakeprovider.ValidProfileWithEndpointsName
		profileNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: profileName}
		var profile *fleetnetv1beta1.TrafficManagerProfile
		backendName := fakeprovider.ValidBackendName
		backendNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: backendName}
		var backend *fleetnetv1beta1.TrafficManagerBackend

		It("Creating a new TrafficManagerProfile", func() {
			By("By creating a new TrafficManagerProfile")
			profile = trafficManagerProfileForTest(profileName)
			Expect(k8sClient.Create(ctx, profile)).Should(Succeed())
		})

		It("Updating TrafficManagerProfile status to programmed false", func() {
			By("By updating TrafficManagerProfile status")
			cond := metav1.Condition{
				Status:             metav1.ConditionFalse,
				Type:               string(fleetnetv1beta1.TrafficManagerProfileConditionProgrammed),
				ObservedGeneration: profile.Generation,
				Reason:             string(fleetnetv1beta1.TrafficManagerProfileReasonInvalid),
			}
			meta.SetStatusCondition(&profile.Status.Conditions, cond)
			Expect(k8sClient.Status().Update(ctx, profile)).Should(Succeed())
		})

		It("Creating TrafficManagerBackend", func() {
			backend = trafficManagerBackendForTest(backendName, profileName, "not-exist")
			Expect(k8sClient.Create(ctx, backend)).Should(Succeed())
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      backendName,
					Namespace: testNamespace,
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildFalseCondition(backend.Generation),
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want)
		})

		It("Updating TrafficManagerProfile status to accepted unknown and it should trigger controller", func() {
			By("By updating TrafficManagerProfile status")
			cond := metav1.Condition{
				Status:             metav1.ConditionUnknown,
				Type:               string(fleetnetv1beta1.TrafficManagerProfileConditionProgrammed),
				ObservedGeneration: profile.Generation,
				Reason:             string(fleetnetv1beta1.TrafficManagerProfileReasonPending),
			}
			meta.SetStatusCondition(&profile.Status.Conditions, cond)
			Expect(k8sClient.Status().Update(ctx, profile)).Should(Succeed())
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      backendName,
					Namespace: testNamespace,
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildUnknownCondition(backend.Generation),
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

	Context("When creating trafficManagerBackend with invalid serviceImport (successfully delete stale endpoints)", Ordered, func() {
		profileName := fakeprovider.ValidProfileWithEndpointsName
		profileNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: profileName}
		var profile *fleetnetv1beta1.TrafficManagerProfile
		backendName := fakeprovider.ValidBackendName
		backendNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: backendName}
		var backend *fleetnetv1beta1.TrafficManagerBackend

		It("Creating a new TrafficManagerProfile", func() {
			By("By creating a new TrafficManagerProfile")
			profile = trafficManagerProfileForTest(profileName)
			Expect(k8sClient.Create(ctx, profile)).Should(Succeed())
		})

		It("Updating TrafficManagerProfile status to programmed true", func() {
			By("By updating TrafficManagerProfile status")
			updateTrafficManagerProfileStatusToTrue(ctx, profile)
		})

		It("Creating TrafficManagerBackend", func() {
			backend = trafficManagerBackendForTest(backendName, profileName, "not-exist")
			Expect(k8sClient.Create(ctx, backend)).Should(Succeed())
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      backendName,
					Namespace: testNamespace,
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildFalseCondition(backend.Generation),
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

	Context("When creating trafficManagerBackend with invalid serviceImport (fail to delete stale endpoints)", Ordered, func() {
		profileName := fakeprovider.ValidProfileWithFailToDeleteEndpointName
		profileNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: profileName}
		var profile *fleetnetv1beta1.TrafficManagerProfile
		backendName := fakeprovider.ValidBackendName
		backendNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: backendName}
		var backend *fleetnetv1beta1.TrafficManagerBackend

		It("Creating a new TrafficManagerProfile", func() {
			By("By creating a new TrafficManagerProfile")
			profile = trafficManagerProfileForTest(profileName)
			Expect(k8sClient.Create(ctx, profile)).Should(Succeed())
		})

		It("Updating TrafficManagerProfile status to accepted true", func() {
			By("By updating TrafficManagerProfile status")
			updateTrafficManagerProfileStatusToTrue(ctx, profile)
		})

		It("Creating TrafficManagerBackend", func() {
			backend = trafficManagerBackendForTest(backendName, profileName, "not-exist")
			Expect(k8sClient.Create(ctx, backend)).Should(Succeed())
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      backendName,
					Namespace: testNamespace,
				},
				Spec: backend.Spec,
				// not able to set the condition
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

	Context("When creating trafficManagerBackend with valid serviceImport but internalServiceExport is not found", Ordered, func() {
		profileName := fakeprovider.ValidProfileWithEndpointsName
		profileNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: profileName}
		var profile *fleetnetv1beta1.TrafficManagerProfile
		backendName := fakeprovider.ValidBackendName
		backendNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: backendName}
		var backend *fleetnetv1beta1.TrafficManagerBackend

		var serviceImport *fleetnetv1alpha1.ServiceImport

		It("Creating a new TrafficManagerProfile", func() {
			By("By creating a new TrafficManagerProfile")
			profile = trafficManagerProfileForTest(profileName)
			Expect(k8sClient.Create(ctx, profile)).Should(Succeed())
		})

		It("Updating TrafficManagerProfile status to programmed true", func() {
			By("By updating TrafficManagerProfile status")
			updateTrafficManagerProfileStatusToTrue(ctx, profile)
		})

		It("Creating TrafficManagerBackend", func() {
			backend = trafficManagerBackendForTest(backendName, profileName, serviceName)
			Expect(k8sClient.Create(ctx, backend)).Should(Succeed())
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      backendName,
					Namespace: testNamespace,
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildFalseCondition(backend.Generation),
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want)
		})

		It("Creating a new ServiceImport", func() {
			serviceImport = &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: testNamespace,
				},
			}
			Expect(k8sClient.Create(ctx, serviceImport)).Should(Succeed(), "failed to create serviceImport")
		})

		It("Validating trafficManagerBackend and should trigger controller to reconcile", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      backendName,
					Namespace: testNamespace,
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildUnknownCondition(backend.Generation),
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want)
		})

		It("Updating the ServiceImport status", func() {
			serviceImport.Status = fleetnetv1alpha1.ServiceImportStatus{
				Clusters: []fleetnetv1alpha1.ClusterStatus{
					{
						Cluster: memberClusterNames[2], // not found internalServiceExport
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, serviceImport)).Should(Succeed(), "failed to create serviceImport")
		})

		It("Validating trafficManagerBackend consistently and should trigger controller to reconcile", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      backendName,
					Namespace: testNamespace,
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildUnknownCondition(backend.Generation),
				},
			}
			validator.ValidateTrafficManagerBackendConsistently(ctx, k8sClient, &want)
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

		It("Deleting serviceImport", func() {
			deleteServiceImport(types.NamespacedName{Namespace: testNamespace, Name: serviceName})
		})
	})

	Context("When creating trafficManagerBackend with valid serviceImport and internalServiceExports", Ordered, func() {
		profileName := fakeprovider.ValidProfileWithEndpointsName
		profileNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: profileName}
		var profile *fleetnetv1beta1.TrafficManagerProfile
		backendName := fakeprovider.ValidBackendName
		backendNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: backendName}
		var backend *fleetnetv1beta1.TrafficManagerBackend

		var serviceImport *fleetnetv1alpha1.ServiceImport

		It("Creating a new TrafficManagerProfile", func() {
			By("By creating a new TrafficManagerProfile")
			profile = trafficManagerProfileForTest(profileName)
			Expect(k8sClient.Create(ctx, profile)).Should(Succeed())
		})

		It("Updating TrafficManagerProfile status to programmed true", func() {
			By("By updating TrafficManagerProfile status")
			updateTrafficManagerProfileStatusToTrue(ctx, profile)
		})

		It("Creating TrafficManagerBackend", func() {
			backend = trafficManagerBackendForTest(backendName, profileName, serviceName)
			Expect(k8sClient.Create(ctx, backend)).Should(Succeed())
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      backendName,
					Namespace: testNamespace,
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildFalseCondition(backend.Generation),
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want)
			validator.ValidateTrafficManagerBackendConsistently(ctx, k8sClient, &want)
		})

		It("Creating a new ServiceImport", func() {
			serviceImport = &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: testNamespace,
				},
			}
			Expect(k8sClient.Create(ctx, serviceImport)).Should(Succeed(), "failed to create serviceImport")
		})

		It("Validating trafficManagerBackend and should trigger controller to reconcile", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      backendName,
					Namespace: testNamespace,
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildUnknownCondition(backend.Generation),
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want)
			validator.ValidateTrafficManagerBackendConsistently(ctx, k8sClient, &want)
		})

		It("Updating the ServiceImport status", func() {
			serviceImport.Status = fleetnetv1alpha1.ServiceImportStatus{
				Clusters: []fleetnetv1alpha1.ClusterStatus{
					{
						Cluster: memberClusterNames[0], // valid endpoint
					},
					{
						Cluster: memberClusterNames[1], // invalid endpoint
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, serviceImport)).Should(Succeed(), "failed to create serviceImport")
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildFalseCondition(backend.Generation),
					Endpoints: []fleetnetv1beta1.TrafficManagerEndpointStatus{
						{
							Name: fmt.Sprintf(AzureResourceEndpointNameFormat, backendName+"#", serviceName, memberClusterNames[0]),
							From: &fleetnetv1beta1.FromCluster{
								ClusterStatus: fleetnetv1beta1.ClusterStatus{
									Cluster: memberClusterNames[0],
								},
								Weight: ptr.To(int64(1)), // the original weight is default to 1
							},
							Weight: ptr.To(backendWeight), // populate the weight using atm endpoint
							Target: ptr.To(fakeprovider.ValidEndpointTarget),
						},
					},
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want)
			validator.ValidateTrafficManagerBackendConsistently(ctx, k8sClient, &want)
		})

		It("Updating the ServiceImport status", func() {
			// The test is running in sequence, so it's safe to set this value.
			fakeprovider.EnableEndpointForbiddenErr()
			serviceImport.Status = fleetnetv1alpha1.ServiceImportStatus{
				Clusters: []fleetnetv1alpha1.ClusterStatus{
					{
						Cluster: memberClusterNames[6], // 403 error
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, serviceImport)).Should(Succeed(), "failed to create serviceImport")
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildFalseCondition(backend.Generation),
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want)
			validator.ValidateTrafficManagerBackendConsistently(ctx, k8sClient, &want)
		})

		It("Setting the fake provider server to stop returning 403 error", func() {
			fakeprovider.DisableEndpointForbiddenErr()
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildTrueCondition(backend.Generation),
					Endpoints: []fleetnetv1beta1.TrafficManagerEndpointStatus{
						{
							Name: fmt.Sprintf(AzureResourceEndpointNameFormat, backendName+"#", serviceName, memberClusterNames[6]),
							From: &fleetnetv1beta1.FromCluster{
								ClusterStatus: fleetnetv1beta1.ClusterStatus{
									Cluster: memberClusterNames[6],
								},
								Weight: ptr.To(int64(1)), // the original weight is default to 1
							},
							Weight: ptr.To(backendWeight), // populate the weight using atm endpoint
							Target: ptr.To(fakeprovider.ValidEndpointTarget),
						},
					},
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want)
			validator.ValidateTrafficManagerBackendConsistently(ctx, k8sClient, &want)
			fakeprovider.EnableEndpointForbiddenErr()
		})

		It("Updating the ServiceImport status", func() {
			serviceImport.Status = fleetnetv1alpha1.ServiceImportStatus{
				Clusters: []fleetnetv1alpha1.ClusterStatus{
					{
						Cluster: memberClusterNames[0], // valid endpoint
					},
					{
						Cluster: memberClusterNames[3], // new endpoint
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, serviceImport)).Should(Succeed(), "failed to create serviceImport")
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildTrueCondition(backend.Generation),
					Endpoints: []fleetnetv1beta1.TrafficManagerEndpointStatus{
						{
							Name: fmt.Sprintf(AzureResourceEndpointNameFormat, backendName+"#", serviceName, memberClusterNames[0]),
							From: &fleetnetv1beta1.FromCluster{
								ClusterStatus: fleetnetv1beta1.ClusterStatus{
									Cluster: memberClusterNames[0],
								},
								Weight: ptr.To(int64(1)),
							},
							Weight: ptr.To(backendWeight / 2), // populate the weight using atm endpoint
							Target: ptr.To(fakeprovider.ValidEndpointTarget),
						},
						{
							Name: fmt.Sprintf(AzureResourceEndpointNameFormat, backendName+"#", serviceName, memberClusterNames[3]),
							From: &fleetnetv1beta1.FromCluster{
								ClusterStatus: fleetnetv1beta1.ClusterStatus{
									Cluster: memberClusterNames[3],
								},
								Weight: ptr.To(int64(1)),
							},
							Weight: ptr.To(backendWeight / 2), // populate the weight using atm endpoint
							Target: ptr.To(fakeprovider.ValidEndpointTarget),
						},
					},
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want)
			validator.ValidateTrafficManagerBackendConsistently(ctx, k8sClient, &want)
		})

		It("Updating the internalServiceExport weight", func() {
			internalServiceExport := &fleetnetv1alpha1.InternalServiceExport{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: internalServiceExports[0].Namespace, Name: internalServiceExports[0].Name}, internalServiceExport)).Should(Succeed())
			internalServiceExport.Spec.Weight = ptr.To(int64(2))
			Expect(k8sClient.Update(ctx, internalServiceExport)).Should(Succeed(), "failed to create internalServiceExport")
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildTrueCondition(backend.Generation),
					Endpoints: []fleetnetv1beta1.TrafficManagerEndpointStatus{
						{
							Name: fmt.Sprintf(AzureResourceEndpointNameFormat, backendName+"#", serviceName, memberClusterNames[0]),
							From: &fleetnetv1beta1.FromCluster{
								ClusterStatus: fleetnetv1beta1.ClusterStatus{
									Cluster: memberClusterNames[0],
								},
								Weight: ptr.To(int64(2)),
							},
							Weight: ptr.To(int64(7)), // 2/3 of the 10
							Target: ptr.To(fakeprovider.ValidEndpointTarget),
						},
						{
							Name: fmt.Sprintf(AzureResourceEndpointNameFormat, backendName+"#", serviceName, memberClusterNames[3]),
							From: &fleetnetv1beta1.FromCluster{
								ClusterStatus: fleetnetv1beta1.ClusterStatus{
									Cluster: memberClusterNames[3],
								},
								Weight: ptr.To(int64(1)),
							},
							Weight: ptr.To(int64(4)), // 1/3 of 10
							Target: ptr.To(fakeprovider.ValidEndpointTarget),
						},
					},
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want)
			validator.ValidateTrafficManagerBackendConsistently(ctx, k8sClient, &want)
		})

		It("Updating the ServiceImport status", func() {
			serviceImport.Status = fleetnetv1alpha1.ServiceImportStatus{
				Clusters: []fleetnetv1alpha1.ClusterStatus{
					{
						Cluster: memberClusterNames[4], // would fail to create atm endpoint
					},
					{
						Cluster: memberClusterNames[0], // valid endpoint
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, serviceImport)).Should(Succeed(), "failed to create serviceImport")
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildFalseCondition(backend.Generation),
					Endpoints: []fleetnetv1beta1.TrafficManagerEndpointStatus{
						{
							Name: fmt.Sprintf(AzureResourceEndpointNameFormat, backendName+"#", serviceName, memberClusterNames[0]),
							From: &fleetnetv1beta1.FromCluster{
								ClusterStatus: fleetnetv1beta1.ClusterStatus{
									Cluster: memberClusterNames[0],
								},
								Weight: ptr.To(int64(2)),
							},
							Weight: ptr.To(int64(7)), // 2/3 of the 10 as the weight is calculated before the endpoints are created
							Target: ptr.To(fakeprovider.ValidEndpointTarget),
						},
					},
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want)
			validator.ValidateTrafficManagerBackendConsistently(ctx, k8sClient, &want)
		})

		It("Updating the ServiceImport status", func() {
			serviceImport.Status = fleetnetv1alpha1.ServiceImportStatus{
				Clusters: []fleetnetv1alpha1.ClusterStatus{
					{
						Cluster: memberClusterNames[5], // would fail to create atm endpoint
					},
					{
						Cluster: memberClusterNames[0], // valid endpoint
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, serviceImport)).Should(Succeed(), "failed to create serviceImport")
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildUnknownCondition(backend.Generation),
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want)
			validator.ValidateTrafficManagerBackendConsistently(ctx, k8sClient, &want)
		})

		It("Updating the ServiceImport status", func() {
			serviceImport.Status = fleetnetv1alpha1.ServiceImportStatus{
				Clusters: []fleetnetv1alpha1.ClusterStatus{
					{
						Cluster: memberClusterNames[0], // valid endpoint
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, serviceImport)).Should(Succeed(), "failed to create serviceImport")
		})

		It("Updating the internalServiceExport to invalid endpoint", func() {
			internalServiceExport := &fleetnetv1alpha1.InternalServiceExport{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: internalServiceExports[0].Namespace, Name: internalServiceExports[0].Name}, internalServiceExport)).Should(Succeed())
			internalServiceExport.Spec.IsInternalLoadBalancer = true
			Expect(k8sClient.Update(ctx, internalServiceExport)).Should(Succeed(), "failed to create internalServiceExport")
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildFalseCondition(backend.Generation),
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want)
			validator.ValidateTrafficManagerBackendConsistently(ctx, k8sClient, &want)
		})

		It("Updating the internalServiceExport to valid endpoint", func() {
			internalServiceExport := &fleetnetv1alpha1.InternalServiceExport{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: internalServiceExports[0].Namespace, Name: internalServiceExports[0].Name}, internalServiceExport)).Should(Succeed())
			internalServiceExport.Spec.IsInternalLoadBalancer = false
			Expect(k8sClient.Update(ctx, internalServiceExport)).Should(Succeed(), "failed to create internalServiceExport")
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildTrueCondition(backend.Generation),
					Endpoints: []fleetnetv1beta1.TrafficManagerEndpointStatus{
						{
							Name: fmt.Sprintf(AzureResourceEndpointNameFormat, backendName+"#", serviceName, memberClusterNames[0]),
							From: &fleetnetv1beta1.FromCluster{
								ClusterStatus: fleetnetv1beta1.ClusterStatus{
									Cluster: memberClusterNames[0],
								},
								Weight: ptr.To(int64(2)),
							},
							Weight: ptr.To(int64(10)), // only 1 endpoint
							Target: ptr.To(fakeprovider.ValidEndpointTarget),
						},
					},
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want)
			validator.ValidateTrafficManagerBackendConsistently(ctx, k8sClient, &want)
		})

		It("Updating weight to 0", func() {
			Expect(k8sClient.Get(ctx, backendNamespacedName, backend)).Should(Succeed(), "failed to get trafficManagerBackend")
			backend.Spec.Weight = ptr.To(int64(0))
			Expect(k8sClient.Update(ctx, backend)).Should(Succeed(), "failed to update trafficManagerBackend")
		})

		It("Validating trafficManagerBackend", func() {
			want := fleetnetv1beta1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1beta1.TrafficManagerBackendStatus{
					Conditions: buildTrueCondition(backend.Generation),
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want)
			validator.ValidateTrafficManagerBackendConsistently(ctx, k8sClient, &want)
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

		It("Deleting serviceImport", func() {
			deleteServiceImport(types.NamespacedName{Namespace: testNamespace, Name: serviceName})
		})
	})
})

func deleteServiceImport(name types.NamespacedName) {
	serviceImport := &fleetnetv1alpha1.ServiceImport{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name.Name,
			Namespace: name.Namespace,
		},
	}
	Expect(k8sClient.Delete(ctx, serviceImport)).Should(Succeed(), "failed to delete serviceImport")

	Eventually(func() error {
		if err := k8sClient.Get(ctx, name, serviceImport); !errors.IsNotFound(err) {
			return fmt.Errorf("serviceImport %s still exists or an unexpected error occurred: %w", name, err)
		}
		return nil
	}, timeout, interval).Should(Succeed(), "Failed to remove serviceImport %s ", name)
}
