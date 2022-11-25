/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package sustained

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/apiretry"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/test/e2e/framework"
)

const (
	// The number of endpointSlices to export from each member cluster.
	endpointSliceCountPerCluster = 300
	// The channel buffer size.
	bufferSize = 20
	// The number of goroutines to deploy for creating resources and/or performing patches.
	maxParallelism = 10
	// The time length each worker for patching endpointSlices is allowed to run.
	testDuration = time.Minute * 20
	// The maximum number of endpoints to have in an endpointSlice.
	maxEndpointCount = 10
	// The wait period between steps; this helps make sure that checks (e.g. whether a specific service
	// has been successfully exported) performed in this test scenario will not contend with built-in
	// controllers and Fleet networking controllers for resources.
	coolDownPeriod = time.Minute * 5
	// The interval between progress reports.
	progressReportCountInterval = 10
	progressReportTimeInterval  = time.Minute * 1

	fleetSystemNS = "fleet-system"
	workNS        = "work"
	svcName       = "app"
	svcPortName   = "http"
	svcPort       = 80
	svcTargetPort = 8080

	endpointSliceExportImportDurationHistogramName = "fleet_networking_endpointslice_export_import_duration_milliseconds_bucket"

	longEventuallyTimeout  = time.Second * 120
	longEventuallyInterval = time.Second
)

var (
	ctx = context.Background()

	svcOrSvcExportKey = types.NamespacedName{Namespace: workNS, Name: svcName}

	quantilePhis = []float32{0.5, 0.75, 0.9, 0.99, 0.999}
)

// generateRandomEndpointAddress returns an endpoint address with random values.
func generateRandomEndpointAddress() []string {
	return []string{fmt.Sprintf("10.%d.%d.%d", rand.Intn(256), rand.Intn(256), rand.Intn(256))} // nolint: gosec
}

// endpointsPatch is a type for marshalling/unmarshalling patches for endpointSlices.
type endpointsPatch struct {
	Endpoints []discoveryv1.Endpoint `json:"endpoints"`
}

// prepareEndpointsPatchBytes returns the raw patch bytes for patching endpoints in endpointSlices.
func prepareEndpointsPatchBytes() ([]byte, error) {
	endpointCount := 1 + rand.Intn(maxEndpointCount) // nolint:gosec
	endpoints := make([]discoveryv1.Endpoint, 0, endpointCount)
	for i := 0; i < endpointCount; i++ {
		endpoints = append(endpoints, discoveryv1.Endpoint{
			Addresses: generateRandomEndpointAddress(),
		})
	}
	endpointsPatch := endpointsPatch{
		Endpoints: endpoints,
	}

	patchBytes, err := json.Marshal(endpointsPatch)
	if err != nil {
		return []byte{}, fmt.Errorf("Marshal(%+v), got %w, want no error", endpointsPatch, err)
	}
	return patchBytes, nil
}

var _ = Describe("evaluate sustained endpointslice export/import latency", Serial, Ordered, func() {
	type workItem struct {
		endpointSlice    *discoveryv1.EndpointSlice
		endpointSliceKey types.NamespacedName
		ownerClusterIdx  int
	}

	allWorkItems := make([]workItem, 0, endpointSliceCountPerCluster*(len(memberClusterNames)-1))
	for i := 0; i < len(memberClusterNames)-1; i++ {
		for j := 0; j < endpointSliceCountPerCluster; j++ {
			endpointSliceName := fmt.Sprintf("endpointslice-%d-%d", i, j)
			endpointSlice := framework.ManuallyManagedIPv4EndpointSlice(workNS, endpointSliceName, svcName, svcPortName, svcTargetPort, generateRandomEndpointAddress())
			allWorkItems = append(allWorkItems, workItem{
				endpointSlice:    endpointSlice,
				endpointSliceKey: types.NamespacedName{Namespace: workNS, Name: endpointSliceName},
				ownerClusterIdx:  i,
			})
		}
	}

	// This test case exports a service, along with a pre-configured number of endpointSlices, from three member
	// clusters, member-1, member-2, and member-3, to the remaining member cluster (member-4).
	BeforeAll(func() {
		// Create namespaces in all clusters.
		Expect(hubClusterClient.Create(ctx, framework.Namespace(workNS))).Should(Succeed(), "Failed to create namespace %s in cluster %s", workNS, hubCluster.Name())
		for _, memberCluster := range memberClusters {
			memberClusterClient := memberCluster.Client()
			Expect(memberClusterClient.Create(ctx, framework.Namespace(workNS))).Should(Succeed(), "Failed to create namespace %s in cluster %s", workNS, memberCluster.Name())
		}

		// Create a service in cluster member-1, member-2, and member-3; export them from the three clusters.
		for i := 0; i < len(memberClusters)-1; i++ {
			memberClusterClient := memberClusters[i].Client()
			svc := framework.ClusterIPServiceWithNoSelector(workNS, svcName, svcPortName, svcPort, svcTargetPort)
			Expect(memberClusterClient.Create(ctx, svc)).Should(Succeed(), "Failed to create service %s in cluster %s", svcName, memberClusters[i].Name())
			svcExport := framework.ServiceExport(workNS, svcName)
			Expect(memberClusterClient.Create(ctx, svcExport)).Should(Succeed(), "Failed to export service %s from cluster %s", svcName, memberClusters[i].Name())

			// Verify that the export has succeeded.
			Eventually(func() error {
				svcExport := &fleetnetv1alpha1.ServiceExport{}
				if err := memberClusterClient.Get(ctx, svcOrSvcExportKey, svcExport); err != nil {
					return fmt.Errorf("serviceExport Get(%+v), got %w, want no error", svcOrSvcExportKey, err)
				}

				svcExportValidCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportValid))
				if svcExportValidCond == nil || svcExportValidCond.Status != metav1.ConditionTrue {
					return fmt.Errorf("serviceExportValid condition, got %+v, want true condition", svcExportValidCond)
				}

				svcExportConflictCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportConflict))
				if svcExportConflictCond == nil || svcExportConflictCond.Status != metav1.ConditionFalse {
					return fmt.Errorf("serviceExportConflict condition, got %+v, want false condition", svcExportConflictCond)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed(), "Failed to export service %s from cluster %s", svcName, memberClusters[i].Name())
		}

		// Create a multi-cluster service in cluster member-4, which imports the service exported by cluster member-1,
		// member-2, and member-3.
		memberCluster := memberClusters[len(memberClusters)-1]
		memberClusterClient := memberCluster.Client()
		mcs := framework.MultiClusterService(workNS, svcName, svcName)
		Expect(memberClusterClient.Create(ctx, mcs)).Should(Succeed(), "Failed to create multi-cluster service %s in cluster %s", svcName, memberCluster.Name())

		// Create endpointSlices behind each exported service in parallel using a producer/consumer pattern.
		workItemCh := make(chan workItem, bufferSize)

		var wg sync.WaitGroup

		// Single producer
		wg.Add(1)
		go func() {
			defer wg.Done()

			for _, w := range allWorkItems {
				workItemCh <- w
			}

			// Close the channel; signal consumers to stop.
			close(workItemCh)
		}()

		// Multiple consumers
		for i := 0; i < maxParallelism-1; i++ {
			wg.Add(1)
			// Assign a new variable as worker index rather than reusing the loop variable to avoid value reassignment
			// in for-loops.
			workerIdx := i

			go func() {
				defer GinkgoRecover()
				defer wg.Done()

				completedWorkCount := 0
				for w := range workItemCh {
					memberCluster := memberClusters[w.ownerClusterIdx]
					memberClusterClient := memberCluster.Client()
					err := apiretry.Do(func() error {
						return memberClusterClient.Create(ctx, w.endpointSlice)
					})
					Expect(err).To(Succeed(), "Failed to create endpointSlice %s in cluster %s", w.endpointSlice.Name, memberCluster.Name())

					completedWorkCount++
					if completedWorkCount%progressReportCountInterval == 0 {
						fmt.Fprintf(GinkgoWriter, "worker %d: %d endpointslices created\n", workerIdx, completedWorkCount)
					}
				}
			}()
		}

		// Wait until all work is done.
		wg.Wait()

		// Verify that all endpointSlices have been exported in parallel using a producer/consumer pattern.
		workItemCh = make(chan workItem, bufferSize)

		wg = sync.WaitGroup{}

		// Single producer.
		wg.Add(1)
		go func() {
			defer wg.Done()

			for _, w := range allWorkItems {
				workItemCh <- w
			}

			// Close the channel; signal consumers to stop.
			close(workItemCh)
		}()

		// Multiple consumers.
		for i := 0; i < maxParallelism-1; i++ {
			wg.Add(1)
			// Assign a new variable as worker index rather than reusing the loop variable to avoid value reassignment
			// in for-loops.
			workerIdx := i

			destMemberCluster := memberClusters[len(memberClusters)-1]
			destMemberClusterClient := destMemberCluster.Client()
			go func() {
				defer GinkgoRecover()
				defer wg.Done()

				completedWorkCount := 0
				for w := range workItemCh {
					memberCluster := memberClusters[w.ownerClusterIdx]
					memberClusterClient := memberCluster.Client()
					Eventually(func() error {
						endpointSlice := &discoveryv1.EndpointSlice{}
						if err := memberClusterClient.Get(ctx, w.endpointSliceKey, endpointSlice); err != nil {
							return fmt.Errorf("endpointSlice Get(%+v), got %w, want no error", w.endpointSliceKey, err)
						}

						endpointSliceUniqueName, ok := endpointSlice.Annotations[objectmeta.ExportedObjectAnnotationUniqueName]
						if !ok {
							return fmt.Errorf("endpointSlice %s not yet exported (no unique name assigned)", w.endpointSlice.Name)
						}

						exportedEndpointSliceKey := types.NamespacedName{Namespace: fleetSystemNS, Name: endpointSliceUniqueName}
						exportedEndpointSlice := &discoveryv1.EndpointSlice{}
						if err := destMemberClusterClient.Get(ctx, exportedEndpointSliceKey, exportedEndpointSlice); err != nil {
							return fmt.Errorf("endpointSlice Get(%+v), got %w, want no error", exportedEndpointSliceKey, err)
						}
						return nil
					}, longEventuallyTimeout, longEventuallyInterval).Should(Succeed(), "Failed to export endpointSlice %s from cluster %s", w.endpointSlice.Name, memberCluster.Name())

					completedWorkCount++
					if completedWorkCount%progressReportCountInterval == 0 {
						fmt.Fprintf(GinkgoWriter, "worker %d: %d endpointslices propagated\n", workerIdx, completedWorkCount)
					}
				}
			}()
		}

		// Wait until all checks are done.
		wg.Wait()
	})

	It("emulate frequent endpoint changes in exported service over a long period of time", func() {
		fmt.Fprintf(GinkgoWriter, "cool down for %d minutes\n", coolDownPeriod/time.Minute)
		time.Sleep(coolDownPeriod)

		var wg sync.WaitGroup

		for i := 0; i < maxParallelism; i++ {
			wg.Add(1)
			// Assign a new variable as worker index rather than reusing the loop variable to avoid value reassignment
			// in for-loops.
			workerIdx := i

			go func() {
				defer GinkgoRecover()
				defer wg.Done()

				startTime := time.Now()
				timer := time.After(testDuration)
				ticker := time.Tick(progressReportTimeInterval)
				patchedEndpointSliceCount := 0

				for {
					select {
					case <-timer:
						// The worker has timed out.
						fmt.Fprintf(GinkgoWriter,
							"worker %d exited: patched endpointSlices for %d times, time lapsed: %f milliseconds\n",
							workerIdx, patchedEndpointSliceCount, float64(time.Since(startTime)/time.Millisecond))
						return
					case <-ticker:
						// Report progress periodically.
						fmt.Fprintf(GinkgoWriter, "worker %d: %d endpointSlices patched\n", workerIdx, patchedEndpointSliceCount)
					default:
						// Pick an endpointSlice to patch in random.
						w := allWorkItems[rand.Intn(len(allWorkItems))] // nolint:gosec
						memberCluster := memberClusters[w.ownerClusterIdx]
						memberClusterClient := memberCluster.Client()

						patchBytes, err := prepareEndpointsPatchBytes()
						if err != nil {
							// Skip and issue another patch if an error has occurred.
							fmt.Fprintf(GinkgoWriter, "prepareEndpointsPatchBytes (worker %d), got %v, want no error", workerIdx, err)
							continue
						}

						endpointSlice := &discoveryv1.EndpointSlice{
							ObjectMeta: metav1.ObjectMeta{
								Namespace: w.endpointSlice.Namespace,
								Name:      w.endpointSlice.Name,
							},
						}
						if err := memberClusterClient.Patch(ctx, endpointSlice, client.RawPatch(types.StrategicMergePatchType, patchBytes)); err != nil {
							// Skip and issue another patch if an error has occurred.
							fmt.Fprintf(GinkgoWriter, "Patch(%+v) (worker %d), got %v, want no error", endpointSlice, workerIdx, err)
							continue
						}
						patchedEndpointSliceCount++
					}
				}
			}()
		}

		wg.Wait()
	})

	It("collect metrics", func() {
		fmt.Fprintf(GinkgoWriter, "cool down for %d minutes\n", coolDownPeriod/time.Minute)
		time.Sleep(coolDownPeriod)

		endpointSliceExportImportDurationQuantileSums := make([]float64, len(quantilePhis))

		memberCluster := memberClusters[len(memberClusters)-1]
		for phiIdx, phi := range quantilePhis {
			prometheusAPIClient, err := framework.NewPrometheusAPIClient(memberCluster.PrometheusAPIServiceAddress())
			Expect(err).To(BeNil())

			endpointSliceExportImportQuantile, err := framework.QueryHistogramQuantileAggregated(ctx,
				prometheusAPIClient, phi, endpointSliceExportImportDurationHistogramName)
			Expect(err).To(BeNil())
			endpointSliceExportImportDurationQuantileSums[phiIdx] += endpointSliceExportImportQuantile
		}

		for phiIdx, phi := range quantilePhis {
			fmt.Fprintf(GinkgoWriter, "endpointslice export/import duration: phi %f, time %f milliseconds\n",
				phi, endpointSliceExportImportDurationQuantileSums[phiIdx]/float64(len(memberClusters)))
		}
	})
})
