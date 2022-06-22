/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package serviceexport

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	fleetnetworkingapi "go.goms.io/fleet-networking/api/v1alpha1"
)

const (
	memberUserNS           = "work"
	memberClusterID        = "bravelion"
	hubReservedNSForMember = "bravelion"

	eventuallyTimeout  = time.Second * 10
	eventuallyInterval = time.Millisecond * 250
)

var memberClient client.Client
var hubClient client.Client

// TestMain helps bootstrap and tear down the test environment before and after running all the tests.
func TestMain(m *testing.M) {
	// Bootstrap the test environment
	memberTestEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	memberCfg, err := memberTestEnv.Start()
	if err != nil || memberCfg == nil {
		log.Fatalf("failed to set member cluster test environment: %v", err)
	}
	hubTestEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	hubCfg, err := hubTestEnv.Start()
	if err != nil || hubCfg == nil {
		log.Fatalf("failed to set hub cluster test environment: %v", err)
	}

	// Add custom APIs to the runtime scheme
	err = fleetnetworkingapi.AddToScheme(scheme.Scheme)
	if err != nil {
		log.Fatalf("failed to add custom APIs to the runtime scheme: %v", err)
	}

	// Set up clients for member and hub clusters
	memberClient, err = client.New(memberCfg, client.Options{Scheme: scheme.Scheme})
	if err != nil || memberClient == nil {
		log.Fatalf("failed to create client for member cluster: %v", err)
	}
	hubClient, err = client.New(hubCfg, client.Options{Scheme: scheme.Scheme})
	if err != nil || hubClient == nil {
		log.Fatalf("failed to create client for hub cluster: %v", err)
	}

	// Add the namespaces
	ctx := context.Background()
	memberNS := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: memberUserNS,
		},
	}
	err = memberClient.Create(ctx, &memberNS)
	if err != nil {
		log.Fatalf("failed to create namespace %s in the member cluster: %v", memberUserNS, err)
	}

	hubNS := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: hubReservedNSForMember,
		},
	}
	err = hubClient.Create(ctx, &hubNS)
	if err != nil {
		log.Fatalf("failed to create namespace %s in the hub cluster: %v", hubReservedNSForMember, err)
	}

	// Start up the ServiceExport controller
	ctrlMgr, err := ctrl.NewManager(memberCfg, ctrl.Options{Scheme: scheme.Scheme})
	if err != nil {
		log.Fatalf("failed to create controller manager: %v", err)
	}
	err = (&SvcExportReconciler{
		memberClusterID: memberClusterID,
		memberClient:    memberClient,
		hubClient:       hubClient,
		hubNS:           hubReservedNSForMember,
	}).SetupWithManager(ctrlMgr)
	if err != nil {
		log.Fatalf("failed to set up svcexport controller with controller manager: %v", err)
	}

	// In newer Kubernetes versions (1.21+), Kubernetes API server will not shut down until all configured custom
	// controllers have gone away; the context and cancelFunc below are added to stop the custom controller used
	// in this test when tearing down the test environment.
	ctrlMgrCtx, ctrlMgrCancelFunc := context.WithCancel(context.Background())
	go func() {
		err = ctrlMgr.Start(ctrlMgrCtx)
		if err != nil {
			log.Fatalf("failed to start controller manager: %v", err)
		}
	}()

	exitCode := m.Run()

	// Tear down the test environment

	// Stop the ServiceExport controller.
	ctrlMgrCancelFunc()

	// Shut down the test environment.
	err = memberTestEnv.Stop()
	if err != nil {
		log.Fatalf("failed to stop member cluster test environment: %v", err)
	}
	err = hubTestEnv.Stop()
	if err != nil {
		log.Fatalf("failed to stop hub cluster test environment: %v", err)
	}

	// Exit.
	os.Exit(exitCode)
}

// TestExportUnexportExistingService run a sequence of subtests that verify the workflow of
// * exporting an existing Service
// * syncing the exported Service
// * unexporting the Service
func TestExportUnexportExistingService(t *testing.T) {
	// Define parameters used in this test
	svcName := "app"

	// Setup
	ctx := context.Background()
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      svcName,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name: "port1",
					Port: 80,
				},
				{
					Name: "port2",
					Port: 81,
				},
			},
		},
	}
	err := memberClient.Create(ctx, &svc)
	if err != nil {
		t.Fatalf("failed to create service %s: %v", svcName, err)
	}

	// Subtests
	t.Run("Should create a ServiceExport", func(t *testing.T) {
		ctx := context.Background()
		svcExport := fleetnetworkingapi.ServiceExport{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: memberUserNS,
				Name:      svcName,
			},
		}
		err := memberClient.Create(ctx, &svcExport)
		if err != nil {
			t.Fatalf("failed to create service export %s: %v", svcName, err)
		}
	})

	t.Run("Should add the cleanup finalizer", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get service export %s: %v", svcName, err)
			}
			if !controllerutil.ContainsFinalizer(svcExport, svcExportCleanupFinalizer) {
				return fmt.Errorf("cleanup finalizer is not present in service export %s", svcName)
			}
			return nil
		}).Should(gomega.BeNil())
	})

	t.Run("Should add valid + pending conflict resolution conditions", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		expectedValidCond := metav1.Condition{
			Type:               string(fleetnetworkingapi.ServiceExportValid),
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             "ServiceIsValid",
			Message:            fmt.Sprintf("service %s/%s is valid for export", memberUserNS, svcName),
		}
		expectedConflictCond := metav1.Condition{
			Type:               string(fleetnetworkingapi.ServiceExportConflict),
			Status:             metav1.ConditionUnknown,
			LastTransitionTime: metav1.Now(),
			Reason:             "PendingConflictResolution",
			Message:            fmt.Sprintf("service %s/%s is pending export conflict resolution", memberUserNS, svcName),
		}
		ignoredFields := cmpopts.IgnoreFields(metav1.Condition{}, "ObservedGeneration", "LastTransitionTime")

		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get service export %s: %v", svcName, err)
			}
			validCondFound := false
			conflictCondFound := false
			for _, cond := range svcExport.Status.Conditions {
				if cond.Type == string(fleetnetworkingapi.ServiceExportValid) {
					validCondFound = true
					if !cmp.Equal(cond, expectedValidCond, ignoredFields) {
						return fmt.Errorf("serviceexportvalid cond, got %+v, want %+v", cond, expectedValidCond)
					}
				} else if cond.Type == string(fleetnetworkingapi.ServiceExportConflict) {
					conflictCondFound = true
					if !cmp.Equal(cond, expectedConflictCond, ignoredFields) {
						return fmt.Errorf("serviceexportconflict cond, got %+v, want %+v", cond, expectedConflictCond)
					}
				}
			}
			if !validCondFound || !conflictCondFound {
				return fmt.Errorf("valid cond found, got %t, want true; conflict cond found, got %t, want true", validCondFound, conflictCondFound)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("Should export the Service", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			internalSvcExport := &fleetnetworkingapi.InternalServiceExport{}
			err := hubClient.Get(ctx, types.NamespacedName{
				Namespace: hubReservedNSForMember,
				Name:      fmt.Sprintf("%s-%s", memberUserNS, svcName),
			}, internalSvcExport)
			if err != nil {
				return fmt.Errorf("failed to get internal service export %s-%s: %v", memberUserNS, svcName, err)
			}
			expected := []fleetnetworkingapi.ServicePort{
				{
					Name:       "port1",
					Protocol:   "TCP",
					Port:       80,
					TargetPort: intstr.FromInt(80),
				},
				{
					Name:       "port2",
					Protocol:   "TCP",
					Port:       81,
					TargetPort: intstr.FromInt(81),
				},
			}
			if !cmp.Equal(internalSvcExport.Spec.Ports, expected) {
				return fmt.Errorf("internalsvcexport.spec.ports, got %+v, want %+v", internalSvcExport.Spec.Ports, expected)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("Should update the Service", func(t *testing.T) {
		ctx := context.Background()
		updatedSvc := corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: memberUserNS,
				Name:      svcName,
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{
						Name: "port1",
						Port: 81,
					},
					{
						Name: "port2",
						Port: 82,
					},
				},
			},
		}
		err := memberClient.Update(ctx, &updatedSvc)
		if err != nil {
			t.Fatalf("failed to update svc %s: %v", svcName, err)
		}
	})

	t.Run("Should update the exported Service", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			internalSvcExport := &fleetnetworkingapi.InternalServiceExport{}
			err := hubClient.Get(ctx, types.NamespacedName{
				Namespace: hubReservedNSForMember,
				Name:      fmt.Sprintf("%s-%s", memberUserNS, svcName),
			}, internalSvcExport)
			if err != nil {
				return fmt.Errorf("failed to get internal service export %s-%s: %v", memberClusterID, svcName, err)
			}
			expected := []fleetnetworkingapi.ServicePort{
				{
					Name:       "port1",
					Protocol:   "TCP",
					Port:       81,
					TargetPort: intstr.FromInt(81),
				},
				{
					Name:       "port2",
					Protocol:   "TCP",
					Port:       82,
					TargetPort: intstr.FromInt(82),
				},
			}
			if !cmp.Equal(internalSvcExport.Spec.Ports, expected) {
				return fmt.Errorf("internalsvcexport.spec.ports, got %+v, want %+v", internalSvcExport.Spec.Ports, expected)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("Should delete the Service Export", func(t *testing.T) {
		ctx := context.Background()
		svcExport := fleetnetworkingapi.ServiceExport{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: memberUserNS,
				Name:      svcName,
			},
		}
		err := memberClient.Delete(ctx, &svcExport)
		if err != nil {
			t.Fatalf("failed to delete service export %s", svcName)
		}
	})

	t.Run("Should unexport the Service", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			internalSvcExport := &fleetnetworkingapi.InternalServiceExport{}
			err := hubClient.Get(ctx, types.NamespacedName{
				Namespace: hubReservedNSForMember,
				Name:      fmt.Sprintf("%s-%s", memberUserNS, svcName),
			}, internalSvcExport)
			switch {
			case errors.IsNotFound(err):
				return nil
			case err != nil:
				return fmt.Errorf("failed to get internal service export %s-%s: %v", memberClusterID, svcName, err)
			default:
				return fmt.Errorf("internal svc export %s-%s is not deleted", memberUserNS, svcName)
			}
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	// Clean up
	err = memberClient.Delete(ctx, &svc)
	if err != nil {
		t.Fatalf("failed to delete service %s: %v", svcName, err)
	}
}

// TestExportUnexportNewService run a sequence of subtests that verify the workflow of
// * exporting an non-existent Service
// * exporting a new Service
// * unexporting a deleted Service
func TestExportUnexportNewService(t *testing.T) {
	// Define parameters used in this test
	svcName := "app2"

	// Subtests
	t.Run("Should create a ServiceExport", func(t *testing.T) {
		ctx := context.Background()
		svcExport := fleetnetworkingapi.ServiceExport{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: memberUserNS,
				Name:      svcName,
			},
		}
		err := memberClient.Create(ctx, &svcExport)
		if err != nil {
			t.Fatalf("failed to create service export %s: %v", svcName, err)
		}
	})

	t.Run("Should not have cleanup finalizer", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get service export %s: %v", svcName, err)
			}
			if controllerutil.ContainsFinalizer(svcExport, svcExportCleanupFinalizer) {
				return fmt.Errorf("cleanup finalizer is present in service export %s", svcName)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("Should add invalid condition", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		expectedValidCond := metav1.Condition{
			Type:               string(fleetnetworkingapi.ServiceExportValid),
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             "ServiceNotFound",
			Message:            fmt.Sprintf("service %s/%s is not found", memberUserNS, svcName),
		}
		ignoredFields := cmpopts.IgnoreFields(metav1.Condition{}, "ObservedGeneration", "LastTransitionTime")

		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get service export %s: %v", svcName, err)
			}
			validCondFound := false
			for _, cond := range svcExport.Status.Conditions {
				if cond.Type == string(fleetnetworkingapi.ServiceExportValid) {
					validCondFound = true
					if !cmp.Equal(cond, expectedValidCond, ignoredFields) {
						return fmt.Errorf("serviceexportvalid cond, got %+v, want %+v", cond, expectedValidCond)
					}
				}
			}
			if !validCondFound {
				return fmt.Errorf("valid cond found, got %t, want true", validCondFound)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("Should not export the Service", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			internalSvcExport := &fleetnetworkingapi.InternalServiceExport{}
			err := hubClient.Get(ctx, types.NamespacedName{
				Namespace: hubReservedNSForMember,
				Name:      fmt.Sprintf("%s-%s", memberUserNS, svcName),
			}, internalSvcExport)
			if err == nil {
				return fmt.Errorf("internal service export %s-%s is present: %v", memberUserNS, svcName, err)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("Should create a Service", func(t *testing.T) {
		ctx := context.Background()
		svc := corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: memberUserNS,
				Name:      svcName,
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{
						Port: 80,
					},
				},
			},
		}
		err := memberClient.Create(ctx, &svc)
		if err != nil {
			t.Fatalf("failed to create service %s: %v", svcName, err)
		}
	})

	t.Run("Should add the cleanup finalizer", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get service export %s: %v", svcName, err)
			}
			if !controllerutil.ContainsFinalizer(svcExport, svcExportCleanupFinalizer) {
				return fmt.Errorf("cleanup finalizer is not present in service export %s", svcName)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("Should add valid + pending conflict resolution conditions", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		expectedValidCond := metav1.Condition{
			Type:               string(fleetnetworkingapi.ServiceExportValid),
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             "ServiceIsValid",
			Message:            fmt.Sprintf("service %s/%s is valid for export", memberUserNS, svcName),
		}
		expectedConflictCond := metav1.Condition{
			Type:               string(fleetnetworkingapi.ServiceExportConflict),
			Status:             metav1.ConditionUnknown,
			LastTransitionTime: metav1.Now(),
			Reason:             "PendingConflictResolution",
			Message:            fmt.Sprintf("service %s/%s is pending export conflict resolution", memberUserNS, svcName),
		}
		ignoredFields := cmpopts.IgnoreFields(metav1.Condition{}, "ObservedGeneration", "LastTransitionTime")

		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get service export %s: %v", svcName, err)
			}
			validCondFound := false
			conflictCondFound := false
			for _, cond := range svcExport.Status.Conditions {
				if cond.Type == string(fleetnetworkingapi.ServiceExportValid) {
					validCondFound = true
					if !cmp.Equal(cond, expectedValidCond, ignoredFields) {
						return fmt.Errorf("serviceexportvalid cond, got %+v, want %+v", cond, expectedValidCond)
					}
				} else if cond.Type == string(fleetnetworkingapi.ServiceExportConflict) {
					conflictCondFound = true
					if !cmp.Equal(cond, expectedConflictCond, ignoredFields) {
						return fmt.Errorf("serviceexportconflict cond, got %+v, want %+v", cond, expectedConflictCond)
					}
				}
			}
			if !validCondFound || !conflictCondFound {
				return fmt.Errorf("valid cond found, got %t, want true; conflict cond found, got %t, want true", validCondFound, conflictCondFound)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("Should export the Service", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			internalSvcExport := &fleetnetworkingapi.InternalServiceExport{}
			err := hubClient.Get(ctx, types.NamespacedName{
				Namespace: hubReservedNSForMember,
				Name:      fmt.Sprintf("%s-%s", memberUserNS, svcName),
			}, internalSvcExport)
			if err != nil {
				return fmt.Errorf("failed to get internal service export %s-%s: %v", memberUserNS, svcName, err)
			}
			expected := []fleetnetworkingapi.ServicePort{
				{
					Protocol:   "TCP",
					Port:       80,
					TargetPort: intstr.FromInt(80),
				},
			}
			if !cmp.Equal(internalSvcExport.Spec.Ports, expected) {
				return fmt.Errorf("internalsvcexport.spec.ports, got %+v, want %+v", internalSvcExport.Spec.Ports, expected)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("Should delete the Service", func(t *testing.T) {
		ctx := context.Background()
		svc := corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: memberUserNS,
				Name:      svcName,
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{
						Port: 80,
					},
				},
			},
		}
		err := memberClient.Delete(ctx, &svc)
		if err != nil {
			t.Fatalf("failed to create service %s: %v", svcName, err)
		}
	})

	t.Run("Should unexport the Service", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			internalSvcExport := &fleetnetworkingapi.InternalServiceExport{}
			err := hubClient.Get(ctx, types.NamespacedName{
				Namespace: hubReservedNSForMember,
				Name:      fmt.Sprintf("%s-%s", memberUserNS, svcName),
			}, internalSvcExport)
			switch {
			case errors.IsNotFound(err):
				return nil
			case err != nil:
				return fmt.Errorf("failed to get internal service export %s-%s: %v", memberClusterID, svcName, err)
			default:
				return fmt.Errorf("internal svc export %s-%s is not deleted", memberUserNS, svcName)
			}
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("Should add invalid condition", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		expectedValidCond := metav1.Condition{
			Type:               string(fleetnetworkingapi.ServiceExportValid),
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             "ServiceNotFound",
			Message:            fmt.Sprintf("service %s/%s is not found", memberUserNS, svcName),
		}
		ignoredFields := cmpopts.IgnoreFields(metav1.Condition{}, "ObservedGeneration", "LastTransitionTime")

		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get service export %s: %v", svcName, err)
			}
			validCondFound := false
			for _, cond := range svcExport.Status.Conditions {
				if cond.Type == string(fleetnetworkingapi.ServiceExportValid) {
					validCondFound = true
					if !cmp.Equal(cond, expectedValidCond, ignoredFields) {
						return fmt.Errorf("serviceexportvalid cond, got %+v, want %+v", cond, expectedValidCond)
					}
				}
			}
			if !validCondFound {
				return fmt.Errorf("valid cond found, got %t, want true", validCondFound)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	// Clean up the ServiceExport
	ctx := context.Background()
	svcExport := fleetnetworkingapi.ServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      svcName,
		},
	}
	err := memberClient.Delete(ctx, &svcExport)
	if err != nil {
		t.Fatalf("failed to delete service export %s: %v", svcName, err)
	}
}

// TestExportIneligibleService run a sequence of subtests that verify the workflow of
// * exporting a Service ineligible for export
// * exporting the Service which becomes eligible for export
// * exporting the Service which becomes ineligible for export again
// Note that Kubernetes imposes additional limit on how Services can switch types: once a headless Service is
// created, its Cluster IP will remain "None".
func TestExportIneligibleService(t *testing.T) {
	// Define parameters used in this test
	svcName := "app3"

	// Subtests
	t.Run("Should create a headless Service", func(t *testing.T) {
		ctx := context.Background()
		svc := corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: memberUserNS,
				Name:      svcName,
			},
			Spec: corev1.ServiceSpec{
				Type:      "ClusterIP",
				ClusterIP: "None",
				Ports: []corev1.ServicePort{
					{
						Port: 80,
					},
				},
			},
		}
		err := memberClient.Create(ctx, &svc)
		if err != nil {
			t.Fatalf("failed to create service %s: %v", svcName, err)
		}
	})

	t.Run("Should create a ServiceExport", func(t *testing.T) {
		ctx := context.Background()
		svcExport := fleetnetworkingapi.ServiceExport{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: memberUserNS,
				Name:      svcName,
			},
		}
		err := memberClient.Create(ctx, &svcExport)
		if err != nil {
			t.Fatalf("failed to create service export %s: %v", svcName, err)
		}
	})

	t.Run("Should not have cleanup finalizer", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get service export %s: %v", svcName, err)
			}
			if controllerutil.ContainsFinalizer(svcExport, svcExportCleanupFinalizer) {
				return fmt.Errorf("cleanup finalizer is present in service export %s", svcName)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("Should add invalid condition", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		expectedValidCond := metav1.Condition{
			Type:               string(fleetnetworkingapi.ServiceExportValid),
			Status:             metav1.ConditionStatus(corev1.ConditionFalse),
			LastTransitionTime: metav1.Now(),
			Reason:             "ServiceIneligible",
			Message:            fmt.Sprintf("service %s/%s is not eligible for export", memberUserNS, svcName),
		}
		ignoredFields := cmpopts.IgnoreFields(metav1.Condition{}, "ObservedGeneration", "LastTransitionTime")

		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get service export %s: %v", svcName, err)
			}
			validCondFound := false
			for _, cond := range svcExport.Status.Conditions {
				if cond.Type == string(fleetnetworkingapi.ServiceExportValid) {
					validCondFound = true
					if !cmp.Equal(cond, expectedValidCond, ignoredFields) {
						return fmt.Errorf("serviceexportvalid cond, got %+v, want %+v", cond, expectedValidCond)
					}
				}
			}
			if !validCondFound {
				return fmt.Errorf("valid cond found, got %t, want true", validCondFound)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("Should not export the Service", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			internalSvcExport := &fleetnetworkingapi.InternalServiceExport{}
			err := hubClient.Get(ctx, types.NamespacedName{
				Namespace: hubReservedNSForMember,
				Name:      fmt.Sprintf("%s-%s", memberUserNS, svcName),
			}, internalSvcExport)
			if err == nil {
				return fmt.Errorf("internal service export %s-%s is present: %v", memberUserNS, svcName, err)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("Should delete the Service", func(t *testing.T) {
		ctx := context.Background()
		svc := corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: memberUserNS,
				Name:      svcName,
			},
		}
		err := memberClient.Delete(ctx, &svc)
		if err != nil {
			t.Fatalf("failed to delete service %s: %v", svcName, err)
		}
	})

	t.Run("Should add invalid condition", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		expectedValidCond := metav1.Condition{
			Type:               string(fleetnetworkingapi.ServiceExportValid),
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             "ServiceNotFound",
			Message:            fmt.Sprintf("service %s/%s is not found", memberUserNS, svcName),
		}
		ignoredFields := cmpopts.IgnoreFields(metav1.Condition{}, "ObservedGeneration", "LastTransitionTime")

		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get service export %s: %v", svcName, err)
			}
			validCondFound := false
			for _, cond := range svcExport.Status.Conditions {
				if cond.Type == string(fleetnetworkingapi.ServiceExportValid) {
					validCondFound = true
					if !cmp.Equal(cond, expectedValidCond, ignoredFields) {
						return fmt.Errorf("serviceexportvalid cond, got %+v, want %+v", cond, expectedValidCond)
					}
				}
			}
			if !validCondFound {
				return fmt.Errorf("valid cond found, got %t, want true", validCondFound)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("Should create a Service of ExternalName type", func(t *testing.T) {
		ctx := context.Background()
		svc := corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: memberUserNS,
				Name:      svcName,
			},
			Spec: corev1.ServiceSpec{
				Type:         "ExternalName",
				ExternalName: "example.com",
			},
		}
		err := memberClient.Create(ctx, &svc)
		if err != nil {
			t.Fatalf("failed to create service %s: %v", svcName, err)
		}
	})

	t.Run("Should not have cleanup finalizer", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get service export %s: %v", svcName, err)
			}
			if controllerutil.ContainsFinalizer(svcExport, svcExportCleanupFinalizer) {
				return fmt.Errorf("cleanup finalizer is present in service export %s", svcName)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("Should add invalid condition", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		expectedValidCond := metav1.Condition{
			Type:               string(fleetnetworkingapi.ServiceExportValid),
			Status:             metav1.ConditionStatus(corev1.ConditionFalse),
			LastTransitionTime: metav1.Now(),
			Reason:             "ServiceIneligible",
			Message:            fmt.Sprintf("service %s/%s is not eligible for export", memberUserNS, svcName),
		}
		ignoredFields := cmpopts.IgnoreFields(metav1.Condition{}, "ObservedGeneration", "LastTransitionTime")

		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get service export %s: %v", svcName, err)
			}
			validCondFound := false
			for _, cond := range svcExport.Status.Conditions {
				if cond.Type == string(fleetnetworkingapi.ServiceExportValid) {
					validCondFound = true
					if !cmp.Equal(cond, expectedValidCond, ignoredFields) {
						return fmt.Errorf("serviceexportvalid cond, got %+v, want %+v", cond, expectedValidCond)
					}
				}
			}
			if !validCondFound {
				return fmt.Errorf("valid cond found, got %t, want true", validCondFound)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("Should not export the Service", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			internalSvcExport := &fleetnetworkingapi.InternalServiceExport{}
			err := hubClient.Get(ctx, types.NamespacedName{
				Namespace: hubReservedNSForMember,
				Name:      fmt.Sprintf("%s-%s", memberUserNS, svcName),
			}, internalSvcExport)
			if err == nil {
				return fmt.Errorf("internal service export %s-%s is present: %v", memberUserNS, svcName, err)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("Should update the Service", func(t *testing.T) {
		ctx := context.Background()
		svc := corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: memberUserNS,
				Name:      svcName,
			},
			Spec: corev1.ServiceSpec{
				Type: "ClusterIP",
				Ports: []corev1.ServicePort{
					{
						Port: 80,
					},
				},
			},
		}
		err := memberClient.Update(ctx, &svc)
		if err != nil {
			t.Fatalf("failed to update service %s: %v", svcName, err)
		}
	})

	t.Run("Should add the cleanup finalizer", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get service export %s: %v", svcName, err)
			}
			if !controllerutil.ContainsFinalizer(svcExport, svcExportCleanupFinalizer) {
				return fmt.Errorf("cleanup finalizer is not present in service export %s", svcName)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("Should add valid + pending conflict resolution conditions", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		expectedValidCond := metav1.Condition{
			Type:               string(fleetnetworkingapi.ServiceExportValid),
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             "ServiceIsValid",
			Message:            fmt.Sprintf("service %s/%s is valid for export", memberUserNS, svcName),
		}
		expectedConflictCond := metav1.Condition{
			Type:               string(fleetnetworkingapi.ServiceExportConflict),
			Status:             metav1.ConditionUnknown,
			LastTransitionTime: metav1.Now(),
			Reason:             "PendingConflictResolution",
			Message:            fmt.Sprintf("service %s/%s is pending export conflict resolution", memberUserNS, svcName),
		}
		ignoredFields := cmpopts.IgnoreFields(metav1.Condition{}, "ObservedGeneration", "LastTransitionTime")

		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get service export %s: %v", svcName, err)
			}
			validCondFound := false
			conflictCondFound := false
			for _, cond := range svcExport.Status.Conditions {
				if cond.Type == string(fleetnetworkingapi.ServiceExportValid) {
					validCondFound = true
					if !cmp.Equal(cond, expectedValidCond, ignoredFields) {
						return fmt.Errorf("serviceexportvalid cond, got %+v, want %+v", cond, expectedValidCond)
					}
				} else if cond.Type == string(fleetnetworkingapi.ServiceExportConflict) {
					conflictCondFound = true
					if !cmp.Equal(cond, expectedConflictCond, ignoredFields) {
						return fmt.Errorf("serviceexportconflict cond, got %+v, want %+v", cond, expectedConflictCond)
					}
				}
			}
			if !validCondFound || !conflictCondFound {
				return fmt.Errorf("valid cond found, got %t, want true; conflict cond found, got %t, want true", validCondFound, conflictCondFound)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("Should export the Service", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			internalSvcExport := &fleetnetworkingapi.InternalServiceExport{}
			err := hubClient.Get(ctx, types.NamespacedName{
				Namespace: hubReservedNSForMember,
				Name:      fmt.Sprintf("%s-%s", memberUserNS, svcName),
			}, internalSvcExport)
			if err != nil {
				return fmt.Errorf("failed to get internal service export %s-%s: %v", memberUserNS, svcName, err)
			}
			expected := []fleetnetworkingapi.ServicePort{
				{
					Protocol:   "TCP",
					Port:       80,
					TargetPort: intstr.FromInt(80),
				},
			}
			if !cmp.Equal(internalSvcExport.Spec.Ports, expected) {
				return fmt.Errorf("internalsvcexport.spec.ports, got %+v, want %+v", internalSvcExport.Spec.Ports, expected)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	// Clean up
	ctx := context.Background()
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      svcName,
		},
	}
	err := memberClient.Delete(ctx, &svc)
	if err != nil {
		t.Fatalf("failed to delete service %s: %v", svcName, err)
	}

	svcExport := fleetnetworkingapi.ServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      svcName,
		},
	}
	err = memberClient.Delete(ctx, &svcExport)
	if err != nil {
		t.Fatalf("failed to delete service %s: %v", svcName, err)
	}
}
