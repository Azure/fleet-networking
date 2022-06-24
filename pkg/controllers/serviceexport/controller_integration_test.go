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
	existingSvcName     = "app3"
	newSvcName          = "app4"
	headlessSvcName     = "app5"
	externalNameSvcName = "app6"

	eventuallyTimeout  = time.Second * 10
	eventuallyInterval = time.Millisecond * 250
)

var memberClient client.Client
var hubClient client.Client

// setUpResources sets up resources in the test environment.
func setUpResources() {
	ctx := context.Background()

	// Add the namespaces
	memberNS := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: memberUserNS,
		},
	}
	err := memberClient.Create(ctx, &memberNS)
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

	// Add the Services and ServiceExports.
	// Add a regular Service.
	existingSvc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      existingSvcName,
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
	err = memberClient.Create(ctx, &existingSvc)
	if err != nil {
		log.Fatalf("failed to create svc %s: %v", existingSvcName, err)
	}

	// Add a ServiceExport.
	svcExport := fleetnetworkingapi.ServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      newSvcName,
		},
	}
	err = memberClient.Create(ctx, &svcExport)
	if err != nil {
		log.Fatalf("failed to create service export %s: %v", newSvcName, err)
	}

	// Add a headless Service.
	headlessSvc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      headlessSvcName,
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
	err = memberClient.Create(ctx, &headlessSvc)
	if err != nil {
		log.Fatalf("failed to create svc %s: %v", headlessSvcName, err)
	}

	// Add a Service of the ExternalName type.
	externalNameSvc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      externalNameSvcName,
		},
		Spec: corev1.ServiceSpec{
			Type:         "ExternalName",
			ExternalName: "example.com",
		},
	}
	err = memberClient.Create(ctx, &externalNameSvc)
	if err != nil {
		log.Fatalf("failed to create service %s: %v", externalNameSvcName, err)
	}
}

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

	// Set up resources.
	setUpResources()

	// Start up the ServiceExport controller.
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

// TestExportExistingService runs a sequence of subtests that verify the workflow of exporting an existing Service.
func TestExportExistingSvc(t *testing.T) {
	// Setup
	ctx := context.Background()
	svcExport := fleetnetworkingapi.ServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      existingSvcName,
		},
	}
	err := memberClient.Create(ctx, &svcExport)
	if err != nil {
		t.Fatalf("failed to create svc export %s: %v", existingSvcName, err)
	}

	// Subtests
	t.Run("should add the cleanup finalizer", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: existingSvcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get svc export %s: %w", existingSvcName, err)
			}
			if !controllerutil.ContainsFinalizer(svcExport, svcExportCleanupFinalizer) {
				return fmt.Errorf("cleanup finalizer is not present in service export %s", existingSvcName)
			}
			return nil
		}).Should(gomega.BeNil())
	})

	t.Run("should add valid + pending conflict resolution conditions", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		expectedValidCond := getSvcExportValidCond(memberUserNS, existingSvcName)
		expectedConflictCond := getSvcExportConflictCond(memberUserNS, existingSvcName)

		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: existingSvcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get svc export %s: %w", existingSvcName, err)
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
				Name:      fmt.Sprintf("%s-%s", memberUserNS, existingSvcName),
			}, internalSvcExport)
			if err != nil {
				return fmt.Errorf("failed to get internal svc export %s-%s: %w", memberUserNS, existingSvcName, err)
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
}

// TestSyncExportedSvc runs a sequence of subtests that verify the workflow of syncing an existing Service as
// its spec changes.
func TestSyncExportedSvc(t *testing.T) {
	// Subtests
	t.Run("should update the svc", func(t *testing.T) {
		ctx := context.Background()
		updatedSvc := corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: memberUserNS,
				Name:      existingSvcName,
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
			t.Fatalf("failed to update svc %s: %v", existingSvcName, err)
		}
	})

	t.Run("should update the exported svc", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			internalSvcExport := &fleetnetworkingapi.InternalServiceExport{}
			err := hubClient.Get(ctx, types.NamespacedName{
				Namespace: hubNSForMember,
				Name:      fmt.Sprintf("%s-%s", memberUserNS, existingSvcName),
			}, internalSvcExport)
			if err != nil {
				return fmt.Errorf("failed to get internal svc export %s-%s: %w", memberClusterID, existingSvcName, err)
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
}

// TestUnexportExistingSvc runs a sequence of subtests that verify the workflow of unexporting an existing Service
// by deleting its corresponding ServiceExport.
func TestUnexportExistingSvc(t *testing.T) {
	// Subtests
	t.Run("should delete the svc export", func(t *testing.T) {
		ctx := context.Background()
		svcExport := fleetnetworkingapi.ServiceExport{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: memberUserNS,
				Name:      existingSvcName,
			},
		}
		err := memberClient.Delete(ctx, &svcExport)
		if err != nil {
			t.Fatalf("failed to delete svc export %s", existingSvcName)
		}
	})

	t.Run("should unexport the svc", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			internalSvcExport := &fleetnetworkingapi.InternalServiceExport{}
			err := hubClient.Get(ctx, types.NamespacedName{
				Namespace: hubNSForMember,
				Name:      fmt.Sprintf("%s-%s", memberUserNS, existingSvcName),
			}, internalSvcExport)
			switch {
			case errors.IsNotFound(err):
				return nil
			case err != nil:
				return fmt.Errorf("failed to get internal svc export %s-%s: %w", memberClusterID, existingSvcName, err)
			default:
				return fmt.Errorf("internal svc export %s-%s is not deleted", memberUserNS, existingSvcName)
			}
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})
}

// TestExportNonExistentSvc runs a sequence of subtests that verify the workflow of exporting a Service that does
// not exist.
func TestExportNonExistentSvc(t *testing.T) {
	// Subtests
	t.Run("should not have cleanup finalizer", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: newSvcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get svc export %s: %w", newSvcName, err)
			}
			if controllerutil.ContainsFinalizer(svcExport, svcExportCleanupFinalizer) {
				return fmt.Errorf("cleanup finalizer is present in svc export %s", newSvcName)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("should add invalid condition", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		expectedInvalidCond := getSvcExportInvalidCondNotFound(memberUserNS, newSvcName)

		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: newSvcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get svc export %s: %w", newSvcName, err)
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
				Name:      fmt.Sprintf("%s-%s", memberUserNS, newSvcName),
			}, internalSvcExport)
			if err == nil {
				return fmt.Errorf("internal svc export %s-%s is present", memberUserNS, newSvcName)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})
}

// TestExportNewSvc runs a sequence of subtests that verify the workflow of exporting a Service that is created
// after it is exported.
func TestExportNewSvc(t *testing.T) {
	// Setup
	ctx := context.Background()
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      newSvcName,
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
		t.Fatalf("failed to create svc %s: %v", newSvcName, err)
	}

	// Subtests
	t.Run("should add the cleanup finalizer", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: newSvcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get svc export %s: %w", newSvcName, err)
			}
			if !controllerutil.ContainsFinalizer(svcExport, svcExportCleanupFinalizer) {
				return fmt.Errorf("cleanup finalizer is not present in svc export %s", newSvcName)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("should add valid + pending conflict resolution conditions", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		expectedValidCond := getSvcExportValidCond(memberUserNS, newSvcName)
		expectedConflictCond := getSvcExportConflictCond(memberUserNS, newSvcName)

		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: newSvcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get svc export %s: %w", newSvcName, err)
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
				Name:      fmt.Sprintf("%s-%s", memberUserNS, newSvcName),
			}, internalSvcExport)
			if err != nil {
				return fmt.Errorf("failed to get internal svc export %s-%s: %w", memberUserNS, newSvcName, err)
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
}

// TestUnexportNewSvc runs a sequence of subtests that verify the workflow of unexporting a Service by deleting
// the Service itself.
func TestUnexportNewSvc(t *testing.T) {
	// Setup
	ctx := context.Background()
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      newSvcName,
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
		t.Fatalf("failed to create svc %s: %v", newSvcName, err)
	}

	// Subtests
	t.Run("should unexport the svc", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			internalSvcExport := &fleetnetworkingapi.InternalServiceExport{}
			err := hubClient.Get(ctx, types.NamespacedName{
				Namespace: hubNSForMember,
				Name:      fmt.Sprintf("%s-%s", memberUserNS, newSvcName),
			}, internalSvcExport)
			switch {
			case errors.IsNotFound(err):
				return nil
			case err != nil:
				return fmt.Errorf("failed to get internal svc export %s-%s: %w", memberClusterID, newSvcName, err)
			default:
				return fmt.Errorf("internal svc export %s-%s is not deleted", memberUserNS, newSvcName)
			}
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("should add invalid condition", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		expectedInvalidCond := getSvcExportInvalidCondNotFound(memberUserNS, newSvcName)

		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: newSvcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get svc export %s: %w", newSvcName, err)
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
}

// TestExportIneligibleSvcHeadless run a sequence of subtests that verify the workflow of exporting a headless Service.
func TestExportIneligibleSvcHeadless(t *testing.T) {
	// Setup
	ctx := context.Background()
	svcExport := fleetnetworkingapi.ServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      headlessSvcName,
		},
	}
	err := memberClient.Create(ctx, &svcExport)
	if err != nil {
		t.Fatalf("failed to create svc export %s: %v", headlessSvcName, err)
	}

	// Subtests
	t.Run("should not have cleanup finalizer", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: headlessSvcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get svc export %s: %w", headlessSvcName, err)
			}
			if controllerutil.ContainsFinalizer(svcExport, svcExportCleanupFinalizer) {
				return fmt.Errorf("cleanup finalizer is present in svc export %s", headlessSvcName)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("should add invalid condition", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		expectedInvalidCond := getSvcExportInvalidCondIneligible(memberUserNS, headlessSvcName)

		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: headlessSvcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get svc export %s: %w", headlessSvcName, err)
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
				Name:      fmt.Sprintf("%s-%s", memberUserNS, headlessSvcName),
			}, internalSvcExport)
			if err == nil {
				return fmt.Errorf("internal svc export %s-%s is present", memberUserNS, headlessSvcName)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})
}

// TestUnexportIneligibleSvcHeadless runs a sequence of subtests that verify the workflow of unexporting a
// headless Service.
func TestUnexportIneligibleSvcHeadless(t *testing.T) {
	// Setup
	ctx := context.Background()
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      headlessSvcName,
		},
	}
	err := memberClient.Delete(ctx, &svc)
	if err != nil {
		t.Fatalf("failed to delete svc export %s: %v", headlessSvcName, err)
	}

	// Subtests
	t.Run("should add invalid condition", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		expectedInvalidCond := getSvcExportInvalidCondNotFound(memberUserNS, headlessSvcName)

		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: headlessSvcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get svc export %s: %w", headlessSvcName, err)
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
}

// TestExportIneligibleSvcExternalName run a sequence of subtests that verify the workflow of
// exporting a Service of the ExternalName type.
func TestExportIneligibleSvcExternalName(t *testing.T) {
	// Setup
	ctx := context.Background()
	svcExport := fleetnetworkingapi.ServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      externalNameSvcName,
		},
	}
	err := memberClient.Create(ctx, &svcExport)
	if err != nil {
		t.Fatalf("failed to create svc export %s: %v", externalNameSvcName, err)
	}

	// Subtests
	t.Run("should not have cleanup finalizer", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: externalNameSvcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get svc export %s: %w", externalNameSvcName, err)
			}
			if controllerutil.ContainsFinalizer(svcExport, svcExportCleanupFinalizer) {
				return fmt.Errorf("cleanup finalizer is present in svc export %s", externalNameSvcName)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("should add invalid condition", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		expectedInvalidCond := getSvcExportInvalidCondIneligible(memberUserNS, externalNameSvcName)

		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: externalNameSvcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get svc export %s: %w", externalNameSvcName, err)
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
				Name:      fmt.Sprintf("%s-%s", memberUserNS, externalNameSvcName),
			}, internalSvcExport)
			if err == nil {
				return fmt.Errorf("internal svc export %s-%s is present", memberUserNS, externalNameSvcName)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})
}

// TestExportSvcTurnedEligible runs a sequence of subtests that verify the workflow of exporting a Service
// that is ineligible for export in the first place and becomes eligible for export due to a spec change.
func TestExportSvcTurnedEligible(t *testing.T) {
	// Setup
	ctx := context.Background()
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: memberUserNS,
			Name:      externalNameSvcName,
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
		t.Fatalf("failed to update svc %s: %v", externalNameSvcName, err)
	}

	// Subtests
	t.Run("should add the cleanup finalizer", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)
		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: externalNameSvcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get svc export %s: %w", externalNameSvcName, err)
			}
			if !controllerutil.ContainsFinalizer(svcExport, svcExportCleanupFinalizer) {
				return fmt.Errorf("cleanup finalizer is not present in svc export %s", externalNameSvcName)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	})

	t.Run("should add valid + pending conflict resolution conditions", func(t *testing.T) {
		g := gomega.NewGomegaWithT(t)

		expectedValidCond := getSvcExportValidCond(memberUserNS, externalNameSvcName)
		expectedConflictCond := getSvcExportConflictCond(memberUserNS, externalNameSvcName)

		g.Eventually(func() error {
			ctx := context.Background()
			svcExport := &fleetnetworkingapi.ServiceExport{}
			err := memberClient.Get(ctx, types.NamespacedName{Namespace: memberUserNS, Name: externalNameSvcName}, svcExport)
			if err != nil {
				return fmt.Errorf("failed to get svc export %s: %w", externalNameSvcName, err)
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
				Name:      fmt.Sprintf("%s-%s", memberUserNS, externalNameSvcName),
			}, internalSvcExport)
			if err != nil {
				return fmt.Errorf("failed to get internal svc export %s-%s: %w", memberUserNS, externalNameSvcName, err)
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
}
