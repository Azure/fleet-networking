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

func buildFalseCondition() []metav1.Condition {
	return []metav1.Condition{
		{
			Status: metav1.ConditionFalse,
			Type:   string(fleetnetv1alpha1.TrafficManagerBackendConditionAccepted),
			Reason: string(fleetnetv1alpha1.TrafficManagerBackendReasonInvalid),
		},
	}
}

func buildUnknownCondition() []metav1.Condition {
	return []metav1.Condition{
		{
			Status: metav1.ConditionUnknown,
			Type:   string(fleetnetv1alpha1.TrafficManagerBackendConditionAccepted),
			Reason: string(fleetnetv1alpha1.TrafficManagerBackendReasonPending),
		},
	}
}

func buildTrueCondition() []metav1.Condition {
	return []metav1.Condition{
		{
			Status: metav1.ConditionTrue,
			Type:   string(fleetnetv1alpha1.TrafficManagerBackendConditionAccepted),
			Reason: string(fleetnetv1alpha1.TrafficManagerBackendReasonAccepted),
		},
	}
}

func updateTrafficManagerProfileStatusToTrue(ctx context.Context, profile *fleetnetv1alpha1.TrafficManagerProfile) {
	cond := metav1.Condition{
		Status:             metav1.ConditionTrue,
		Type:               string(fleetnetv1alpha1.TrafficManagerProfileConditionProgrammed),
		ObservedGeneration: profile.Generation,
		Reason:             string(fleetnetv1alpha1.TrafficManagerProfileReasonProgrammed),
	}
	meta.SetStatusCondition(&profile.Status.Conditions, cond)
	Expect(k8sClient.Status().Update(ctx, profile)).Should(Succeed())
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
					Conditions: buildFalseCondition(),
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
			want := fleetnetv1alpha1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1alpha1.TrafficManagerBackendStatus{
					Conditions: buildUnknownCondition(),
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want)
		})

		It("Updating TrafficManagerProfile status to programmed true and it should trigger controller", func() {
			By("By updating TrafficManagerProfile status")
			updateTrafficManagerProfileStatusToTrue(ctx, profile)
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
					Conditions: buildFalseCondition(),
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
		var profile *fleetnetv1alpha1.TrafficManagerProfile
		backendName := fakeprovider.ValidBackendName
		backendNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: backendName}
		var backend *fleetnetv1alpha1.TrafficManagerBackend

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
			want := fleetnetv1alpha1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1alpha1.TrafficManagerBackendStatus{
					Conditions: buildUnknownCondition(),
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want)
		})

		It("Deleting trafficManagerBackend", func() {
			err := k8sClient.Delete(ctx, backend)
			Expect(err).Should(Succeed(), "failed to delete trafficManagerBackend")
		})

		It("Validating trafficManagerBackend cannot be deleted", func() {
			validator.ValidateTrafficManagerConsistentlyExist(ctx, k8sClient, backendNamespacedName)
		})

		It("Removing the finalizer from trafficManagerBackend", func() {
			Eventually(func() error {
				if err := k8sClient.Get(ctx, backendNamespacedName, backend); err != nil {
					return err
				}
				backend.Finalizers = nil
				return k8sClient.Update(ctx, backend)
			}, timeout, interval).Should(Succeed(), "failed to remove trafficManagerBackend finalizer")
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

		It("Updating TrafficManagerProfile status to programmed true", func() {
			By("By updating TrafficManagerProfile status")
			updateTrafficManagerProfileStatusToTrue(ctx, profile)
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

		It("Updating TrafficManagerProfile status to programmed false", func() {
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
					Conditions: buildFalseCondition(),
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
					Conditions: buildUnknownCondition(),
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
		var profile *fleetnetv1alpha1.TrafficManagerProfile
		backendName := fakeprovider.ValidBackendName
		backendNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: backendName}
		var backend *fleetnetv1alpha1.TrafficManagerBackend

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
			want := fleetnetv1alpha1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1alpha1.TrafficManagerBackendStatus{
					Conditions: buildFalseCondition(),
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
		var profile *fleetnetv1alpha1.TrafficManagerProfile
		backendName := fakeprovider.ValidBackendName
		backendNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: backendName}
		var backend *fleetnetv1alpha1.TrafficManagerBackend

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
			want := fleetnetv1alpha1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer},
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

		It("Validating trafficManagerBackend cannot be deleted", func() {
			validator.ValidateTrafficManagerConsistentlyExist(ctx, k8sClient, backendNamespacedName)
		})

		It("Removing the finalizer from trafficManagerBackend", func() {
			Eventually(func() error {
				if err := k8sClient.Get(ctx, backendNamespacedName, backend); err != nil {
					return err
				}
				backend.Finalizers = nil
				return k8sClient.Update(ctx, backend)
			}, timeout, interval).Should(Succeed(), "failed to remove trafficManagerBackend finalizer")
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
		var profile *fleetnetv1alpha1.TrafficManagerProfile
		backendName := fakeprovider.ValidBackendName
		backendNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: backendName}
		var backend *fleetnetv1alpha1.TrafficManagerBackend

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
			want := fleetnetv1alpha1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1alpha1.TrafficManagerBackendStatus{
					Conditions: buildFalseCondition(),
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
			want := fleetnetv1alpha1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1alpha1.TrafficManagerBackendStatus{
					Conditions: buildUnknownCondition(),
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
			want := fleetnetv1alpha1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1alpha1.TrafficManagerBackendStatus{
					Conditions: buildUnknownCondition(),
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
		var profile *fleetnetv1alpha1.TrafficManagerProfile
		backendName := fakeprovider.ValidBackendName
		backendNamespacedName := types.NamespacedName{Namespace: testNamespace, Name: backendName}
		var backend *fleetnetv1alpha1.TrafficManagerBackend

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
			want := fleetnetv1alpha1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1alpha1.TrafficManagerBackendStatus{
					Conditions: buildFalseCondition(),
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
			want := fleetnetv1alpha1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1alpha1.TrafficManagerBackendStatus{
					Conditions: buildUnknownCondition(),
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want)
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
			want := fleetnetv1alpha1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1alpha1.TrafficManagerBackendStatus{
					Conditions: buildFalseCondition(),
					Endpoints: []fleetnetv1alpha1.TrafficManagerEndpointStatus{
						{
							Name: fmt.Sprintf(AzureResourceEndpointNameFormat, backendName+"#", serviceName, memberClusterNames[0]),
							Cluster: &fleetnetv1alpha1.ClusterStatus{
								Cluster: memberClusterNames[0],
							},
							Weight: ptr.To(fakeprovider.Weight), // populate the weight using atm endpoint
							Target: ptr.To(fakeprovider.ValidEndpointTarget),
						},
					},
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want)
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
			want := fleetnetv1alpha1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1alpha1.TrafficManagerBackendStatus{
					Conditions: buildTrueCondition(),
					Endpoints: []fleetnetv1alpha1.TrafficManagerEndpointStatus{
						{
							Name: fmt.Sprintf(AzureResourceEndpointNameFormat, backendName+"#", serviceName, memberClusterNames[0]),
							Cluster: &fleetnetv1alpha1.ClusterStatus{
								Cluster: memberClusterNames[0],
							},
							Weight: ptr.To(fakeprovider.Weight), // populate the weight using atm endpoint
							Target: ptr.To(fakeprovider.ValidEndpointTarget),
						},
						{
							Name: fmt.Sprintf(AzureResourceEndpointNameFormat, backendName+"#", serviceName, memberClusterNames[3]),
							Cluster: &fleetnetv1alpha1.ClusterStatus{
								Cluster: memberClusterNames[3],
							},
							Weight: ptr.To(fakeprovider.Weight), // populate the weight using atm endpoint
							Target: ptr.To(fakeprovider.ValidEndpointTarget),
						},
					},
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want)
		})

		It("Updating the ServiceImport status", func() {
			serviceImport.Status = fleetnetv1alpha1.ServiceImportStatus{
				Clusters: []fleetnetv1alpha1.ClusterStatus{
					{
						Cluster: memberClusterNames[0], // valid endpoint
					},
					{
						Cluster: memberClusterNames[4], // would fail to create atm endpoint
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, serviceImport)).Should(Succeed(), "failed to create serviceImport")
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
					Conditions: buildFalseCondition(),
					Endpoints: []fleetnetv1alpha1.TrafficManagerEndpointStatus{
						{
							Name: fmt.Sprintf(AzureResourceEndpointNameFormat, backendName+"#", serviceName, memberClusterNames[0]),
							Cluster: &fleetnetv1alpha1.ClusterStatus{
								Cluster: memberClusterNames[0],
							},
							Weight: ptr.To(fakeprovider.Weight), // populate the weight using atm endpoint
							Target: ptr.To(fakeprovider.ValidEndpointTarget),
						},
					},
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want)
		})

		It("Updating the ServiceImport status", func() {
			serviceImport.Status = fleetnetv1alpha1.ServiceImportStatus{
				Clusters: []fleetnetv1alpha1.ClusterStatus{
					{
						Cluster: memberClusterNames[0], // valid endpoint
					},
					{
						Cluster: memberClusterNames[5], // would fail to create atm endpoint
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, serviceImport)).Should(Succeed(), "failed to create serviceImport")
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
					Conditions: buildUnknownCondition(),
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want)
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
			want := fleetnetv1alpha1.TrafficManagerBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:       backendName,
					Namespace:  testNamespace,
					Finalizers: []string{objectmeta.TrafficManagerBackendFinalizer},
				},
				Spec: backend.Spec,
				Status: fleetnetv1alpha1.TrafficManagerBackendStatus{
					Conditions: buildFalseCondition(),
				},
			}
			validator.ValidateTrafficManagerBackend(ctx, k8sClient, &want)
		})

		It("Updating the internalServiceExport to valid endpoint", func() {
			internalServiceExport := &fleetnetv1alpha1.InternalServiceExport{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: internalServiceExports[0].Namespace, Name: internalServiceExports[0].Name}, internalServiceExport)).Should(Succeed())
			internalServiceExport.Spec.IsInternalLoadBalancer = false
			Expect(k8sClient.Update(ctx, internalServiceExport)).Should(Succeed(), "failed to create internalServiceExport")
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
					Conditions: buildTrueCondition(),
					Endpoints: []fleetnetv1alpha1.TrafficManagerEndpointStatus{
						{
							Name: fmt.Sprintf(AzureResourceEndpointNameFormat, backendName+"#", serviceName, memberClusterNames[0]),
							Cluster: &fleetnetv1alpha1.ClusterStatus{
								Cluster: memberClusterNames[0],
							},
							Weight: ptr.To(fakeprovider.Weight), // populate the weight using atm endpoint
							Target: ptr.To(fakeprovider.ValidEndpointTarget),
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
