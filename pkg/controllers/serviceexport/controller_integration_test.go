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
	eventuallyTimeout  = time.Second * 10
	eventuallyInterval = time.Millisecond * 250
)

var memberClient client.Client
var hubClient client.Client

// TestMain helps bootstrap and tear down the test environment before and after running all the tests.
func TestMain(m *testing.M) {
	// Bootstrap the test environment

	// Add custom APIs to the runtime scheme
	err := fleetnetworkingapi.AddToScheme(scheme.Scheme)
	if err != nil {
		log.Fatalf("failed to add custom APIs to the runtime scheme: %v", err)
	}

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
			Name: hubNSForMember,
		},
	}
	err = hubClient.Create(ctx, &hubNS)
	if err != nil {
		log.Fatalf("failed to create namespace %s in the hub cluster: %v", hubNSForMember, err)
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
		hubNS:           hubNSForMember,
	}).SetupWithManager(ctrlMgr)
	if err != nil {
		log.Fatalf("failed to set up svcexport controller with controller manager: %v", err)
	}

	// In newer Kubernetes versions (1.21+), Kubernetes API server will not shut down until all configured custom
	// controllers have gone away; the context and cancelFunc below are added to stop the custom controller used
	// in this test when tearing down the test environment.
	ctrlMgrCtx, ctrlMgrCancelFunc := context.WithCancel(context.Background())
	go func() {
		err := ctrlMgr.Start(ctrlMgrCtx)
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
		t.Fatalf("failed to create svc %s: %v", svcName, err)
	}

	// Subtests
	t.Run("should create a svc export", func(t *testing.T) {
		ctx := context.Background()
		svcExport := fleetnetworkingapi.ServiceExport{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: memberUserNS,
				Name:      svcName,
			},
		}
		err := memberClient.Create(ctx, &svcExport)
		if err != nil {
			t.Fatalf("failed to create svc export %s: %v", svcName, err)
		}
	})

	t.Run("should add the cleanup finalizer", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get svc export %s: %w", svcName, err)
			}
			if !controllerutil.ContainsFinalizer(svcExport, svcExportCleanupFinalizer) {
				return fmt.Errorf("cleanup finalizer is not present in service export %s", svcName)
			}
			return nil
		}).Should(gomega.BeNil())
	})

	t.Run("should add valid + pending conflict resolution conditions", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		expectedValidCond := getSvcExportValidCond(memberUserNS, svcName)
		expectedConflictCond := getSvcExportConflictCond(memberUserNS, svcName)

		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get svc export %s: %w", svcName, err)
			}
			validCondFound := false
			conflictCondFound := false
			for _, cond := range svcExport.Status.Conditions {
				if cond.Type == string(fleetnetworkingapi.ServiceExportValid) {
					validCondFound = true
					if !cmp.Equal(cond, expectedValidCond, ignoredCondFields) {
						return fmt.Errorf("serviceexportvalid cond, got %+v, want %+v", cond, expectedValidCond)
					}
				} else if cond.Type == string(fleetnetworkingapi.ServiceExportConflict) {
					conflictCondFound = true
					if !cmp.Equal(cond, expectedConflictCond, ignoredCondFields) {
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

	t.Run("should export the svc", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			internalSvcExport := &fleetnetworkingapi.InternalServiceExport{}
			err := hubClient.Get(ctx, types.NamespacedName{
				Namespace: hubNSForMember,
				Name:      fmt.Sprintf("%s-%s", memberUserNS, svcName),
			}, internalSvcExport)
			if err != nil {
				return fmt.Errorf("failed to get internal svc export %s-%s: %w", memberUserNS, svcName, err)
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

	t.Run("should update the svc", func(t *testing.T) {
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

	t.Run("should update the exported svc", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			internalSvcExport := &fleetnetworkingapi.InternalServiceExport{}
			err := hubClient.Get(ctx, types.NamespacedName{
				Namespace: hubNSForMember,
				Name:      fmt.Sprintf("%s-%s", memberUserNS, svcName),
			}, internalSvcExport)
			if err != nil {
				return fmt.Errorf("failed to get internal svc export %s-%s: %w", memberClusterID, svcName, err)
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

	t.Run("should delete the svc export", func(t *testing.T) {
		ctx := context.Background()
		svcExport := fleetnetworkingapi.ServiceExport{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: memberUserNS,
				Name:      svcName,
			},
		}
		err := memberClient.Delete(ctx, &svcExport)
		if err != nil {
			t.Fatalf("failed to delete svc export %s", svcName)
		}
	})

	t.Run("should unexport the svc", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			internalSvcExport := &fleetnetworkingapi.InternalServiceExport{}
			err := hubClient.Get(ctx, types.NamespacedName{
				Namespace: hubNSForMember,
				Name:      fmt.Sprintf("%s-%s", memberUserNS, svcName),
			}, internalSvcExport)
			switch {
			case errors.IsNotFound(err):
				return nil
			case err != nil:
				return fmt.Errorf("failed to get internal svc export %s-%s: %w", memberClusterID, svcName, err)
			default:
				return fmt.Errorf("internal svc export %s-%s is not deleted", memberUserNS, svcName)
			}
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	// Clean up
	err = memberClient.Delete(ctx, &svc)
	if err != nil {
		t.Fatalf("failed to delete svc %s: %v", svcName, err)
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
	t.Run("should create a svc export", func(t *testing.T) {
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

	t.Run("should not have cleanup finalizer", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get service export %s: %w", svcName, err)
			}
			if controllerutil.ContainsFinalizer(svcExport, svcExportCleanupFinalizer) {
				return fmt.Errorf("cleanup finalizer is present in service export %s", svcName)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("should add invalid condition", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		expectedInvalidCond := getSvcExportInvalidCondNotFound(memberUserNS, svcName)

		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get svc export %s: %w", svcName, err)
			}
			validCondFound := false
			for _, cond := range svcExport.Status.Conditions {
				if cond.Type == string(fleetnetworkingapi.ServiceExportValid) {
					validCondFound = true
					if !cmp.Equal(cond, expectedInvalidCond, ignoredCondFields) {
						return fmt.Errorf("serviceexportvalid cond, got %+v, want %+v", cond, expectedInvalidCond)
					}
				}
			}
			if !validCondFound {
				return fmt.Errorf("valid cond found, got %t, want true", validCondFound)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("should not export the svc", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			internalSvcExport := &fleetnetworkingapi.InternalServiceExport{}
			err := hubClient.Get(ctx, types.NamespacedName{
				Namespace: hubNSForMember,
				Name:      fmt.Sprintf("%s-%s", memberUserNS, svcName),
			}, internalSvcExport)
			if err == nil {
				return fmt.Errorf("internal svc export %s-%s is present", memberUserNS, svcName)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("should create a svc", func(t *testing.T) {
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
			t.Fatalf("failed to create svc %s: %v", svcName, err)
		}
	})

	t.Run("should add the cleanup finalizer", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get svc export %s: %w", svcName, err)
			}
			if !controllerutil.ContainsFinalizer(svcExport, svcExportCleanupFinalizer) {
				return fmt.Errorf("cleanup finalizer is not present in svc export %s", svcName)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("should add valid + pending conflict resolution conditions", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		expectedValidCond := getSvcExportValidCond(memberUserNS, svcName)
		expectedConflictCond := getSvcExportConflictCond(memberUserNS, svcName)

		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get svc export %s: %w", svcName, err)
			}
			validCondFound := false
			conflictCondFound := false
			for _, cond := range svcExport.Status.Conditions {
				if cond.Type == string(fleetnetworkingapi.ServiceExportValid) {
					validCondFound = true
					if !cmp.Equal(cond, expectedValidCond, ignoredCondFields) {
						return fmt.Errorf("serviceexportvalid cond, got %+v, want %+v", cond, expectedValidCond)
					}
				} else if cond.Type == string(fleetnetworkingapi.ServiceExportConflict) {
					conflictCondFound = true
					if !cmp.Equal(cond, expectedConflictCond, ignoredCondFields) {
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

	t.Run("should export the svc", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			internalSvcExport := &fleetnetworkingapi.InternalServiceExport{}
			err := hubClient.Get(ctx, types.NamespacedName{
				Namespace: hubNSForMember,
				Name:      fmt.Sprintf("%s-%s", memberUserNS, svcName),
			}, internalSvcExport)
			if err != nil {
				return fmt.Errorf("failed to get internal svc export %s-%s: %w", memberUserNS, svcName, err)
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

	t.Run("should delete the svc", func(t *testing.T) {
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
			t.Fatalf("failed to create svc %s: %v", svcName, err)
		}
	})

	t.Run("should unexport the svc", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			internalSvcExport := &fleetnetworkingapi.InternalServiceExport{}
			err := hubClient.Get(ctx, types.NamespacedName{
				Namespace: hubNSForMember,
				Name:      fmt.Sprintf("%s-%s", memberUserNS, svcName),
			}, internalSvcExport)
			switch {
			case errors.IsNotFound(err):
				return nil
			case err != nil:
				return fmt.Errorf("failed to get internal svc export %s-%s: %w", memberClusterID, svcName, err)
			default:
				return fmt.Errorf("internal svc export %s-%s is not deleted", memberUserNS, svcName)
			}
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("should add invalid condition", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		expectedInvalidCond := getSvcExportInvalidCondNotFound(memberUserNS, svcName)

		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get svc export %s: %w", svcName, err)
			}
			validCondFound := false
			for _, cond := range svcExport.Status.Conditions {
				if cond.Type == string(fleetnetworkingapi.ServiceExportValid) {
					validCondFound = true
					if !cmp.Equal(cond, expectedInvalidCond, ignoredCondFields) {
						return fmt.Errorf("serviceexportvalid cond, got %+v, want %+v", cond, expectedInvalidCond)
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
		t.Fatalf("failed to delete svc export %s: %v", svcName, err)
	}
}

// TestExportIneligibleServiceHeadless run a sequence of subtests that verify the workflow of
// * exporting an ineligible Service
// * deleting the ineligible Service
// Note that Kubernetes imposes additional limit on how Services can switch types: once a headless Service is
// created, its Cluster IP will remain "None".
func TestExportIneligibleServiceHeadless(t *testing.T) {
	// Define parameters used in this test
	svcName := "app3"

	// Subtests
	t.Run("should create a headless svc", func(t *testing.T) {
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
			t.Fatalf("failed to create svc %s: %v", svcName, err)
		}
	})

	t.Run("should create a svc export", func(t *testing.T) {
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

	t.Run("should not have cleanup finalizer", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get service export %s: %w", svcName, err)
			}
			if controllerutil.ContainsFinalizer(svcExport, svcExportCleanupFinalizer) {
				return fmt.Errorf("cleanup finalizer is present in service export %s", svcName)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("should add invalid condition", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		expectedInvalidCond := getSvcExportInvalidCondIneligible(memberUserNS, svcName)

		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get service export %s: %w", svcName, err)
			}
			validCondFound := false
			for _, cond := range svcExport.Status.Conditions {
				if cond.Type == string(fleetnetworkingapi.ServiceExportValid) {
					validCondFound = true
					if !cmp.Equal(cond, expectedInvalidCond, ignoredCondFields) {
						return fmt.Errorf("serviceexportvalid cond, got %+v, want %+v", cond, expectedInvalidCond)
					}
				}
			}
			if !validCondFound {
				return fmt.Errorf("valid cond found, got %t, want true", validCondFound)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("should not export the svc", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			internalSvcExport := &fleetnetworkingapi.InternalServiceExport{}
			err := hubClient.Get(ctx, types.NamespacedName{
				Namespace: hubNSForMember,
				Name:      fmt.Sprintf("%s-%s", memberUserNS, svcName),
			}, internalSvcExport)
			if err == nil {
				return fmt.Errorf("internal svc export %s-%s is present", memberUserNS, svcName)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("should delete the svc", func(t *testing.T) {
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

	t.Run("should add invalid condition", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		expectedInvalidCond := getSvcExportInvalidCondNotFound(memberUserNS, svcName)

		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get svc export %s: %w", svcName, err)
			}
			validCondFound := false
			for _, cond := range svcExport.Status.Conditions {
				if cond.Type == string(fleetnetworkingapi.ServiceExportValid) {
					validCondFound = true
					if !cmp.Equal(cond, expectedInvalidCond, ignoredCondFields) {
						return fmt.Errorf("serviceexportvalid cond, got %+v, want %+v", cond, expectedInvalidCond)
					}
				}
			}
			if !validCondFound {
				return fmt.Errorf("valid cond found, got %t, want true", validCondFound)
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
	_ = memberClient.Delete(ctx, &svc)

	svcExport := fleetnetworkingapi.ServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      svcName,
		},
	}
	err := memberClient.Delete(ctx, &svcExport)
	if err != nil {
		t.Fatalf("failed to delete svc %s: %v", svcName, err)
	}
}

// TestExportIneligibleServiceExternalName run a sequence of subtests that verify the workflow of
// * exporting an ineligible Service
// * exporting the Service which later becomes eligible
func TestExportIneligibleServiceExternalName(t *testing.T) {
	// Define parameters used in this test
	svcName := "app4"

	t.Run("should create a svc of externalname type", func(t *testing.T) {
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

	t.Run("should create a svc export", func(t *testing.T) {
		ctx := context.Background()
		svcExport := fleetnetworkingapi.ServiceExport{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: memberUserNS,
				Name:      svcName,
			},
		}
		err := memberClient.Create(ctx, &svcExport)
		if err != nil {
			t.Fatalf("failed to create svc export %s: %v", svcName, err)
		}
	})

	t.Run("should not have cleanup finalizer", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get svc export %s: %w", svcName, err)
			}
			if controllerutil.ContainsFinalizer(svcExport, svcExportCleanupFinalizer) {
				return fmt.Errorf("cleanup finalizer is present in svc export %s", svcName)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("should add invalid condition", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		expectedInvalidCond := getSvcExportInvalidCondIneligible(memberUserNS, svcName)

		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get svc export %s: %w", svcName, err)
			}
			validCondFound := false
			for _, cond := range svcExport.Status.Conditions {
				if cond.Type == string(fleetnetworkingapi.ServiceExportValid) {
					validCondFound = true
					if !cmp.Equal(cond, expectedInvalidCond, ignoredCondFields) {
						return fmt.Errorf("serviceexportvalid cond, got %+v, want %+v", cond, expectedInvalidCond)
					}
				}
			}
			if !validCondFound {
				return fmt.Errorf("valid cond found, got %t, want true", validCondFound)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("should not export the svc", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			internalSvcExport := &fleetnetworkingapi.InternalServiceExport{}
			err := hubClient.Get(ctx, types.NamespacedName{
				Namespace: hubNSForMember,
				Name:      fmt.Sprintf("%s-%s", memberUserNS, svcName),
			}, internalSvcExport)
			if err == nil {
				return fmt.Errorf("internal svc export %s-%s is present", memberUserNS, svcName)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("should update the svc", func(t *testing.T) {
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
			t.Fatalf("failed to update svc %s: %v", svcName, err)
		}
	})

	t.Run("should add the cleanup finalizer", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get svc export %s: %w", svcName, err)
			}
			if !controllerutil.ContainsFinalizer(svcExport, svcExportCleanupFinalizer) {
				return fmt.Errorf("cleanup finalizer is not present in svc export %s", svcName)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("should add valid + pending conflict resolution conditions", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		expectedValidCond := getSvcExportValidCond(memberUserNS, svcName)
		expectedConflictCond := getSvcExportConflictCond(memberUserNS, svcName)

		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: svcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get svc export %s: %w", svcName, err)
			}
			validCondFound := false
			conflictCondFound := false
			for _, cond := range svcExport.Status.Conditions {
				if cond.Type == string(fleetnetworkingapi.ServiceExportValid) {
					validCondFound = true
					if !cmp.Equal(cond, expectedValidCond, ignoredCondFields) {
						return fmt.Errorf("serviceexportvalid cond, got %+v, want %+v", cond, expectedValidCond)
					}
				} else if cond.Type == string(fleetnetworkingapi.ServiceExportConflict) {
					conflictCondFound = true
					if !cmp.Equal(cond, expectedConflictCond, ignoredCondFields) {
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

	t.Run("should export the svc", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			internalSvcExport := &fleetnetworkingapi.InternalServiceExport{}
			err := hubClient.Get(ctx, types.NamespacedName{
				Namespace: hubNSForMember,
				Name:      fmt.Sprintf("%s-%s", memberUserNS, svcName),
			}, internalSvcExport)
			if err != nil {
				return fmt.Errorf("failed to get internal svc export %s-%s: %w", memberUserNS, svcName, err)
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
		t.Fatalf("failed to delete svc %s: %v", svcName, err)
	}

	svcExport := fleetnetworkingapi.ServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      svcName,
		},
	}
	err = memberClient.Delete(ctx, &svcExport)
	if err != nil {
		t.Fatalf("failed to delete svc %s: %v", svcName, err)
	}
}
