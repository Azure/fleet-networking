/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package peak

import (
	"context"
	"fmt"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/test/e2e/framework"
)

const (
	prometheusScrapeInterval = time.Second * 10

	svcExportDurationHistogramName                 = "fleet_networking_service_export_duration_milliseconds_bucket"
	endpointSliceExportImportDurationHistogramName = "fleet_networking_endpointslice_export_import_duration_milliseconds_bucket"

	pollInterval           = time.Millisecond * 100
	longEventuallyTimeout  = time.Second * 120
	longEventuallyInterval = time.Second
)

var (
	quantilePhis = []float32{0.5, 0.75, 0.9, 0.99, 0.999}
)

var (
	workNS            = "work"
	fleetSystemNS     = "fleet-system"
	svcName           = "app"
	svcPortName       = "http"
	svcPort           = int32(80)
	svcTargetPort     = int32(8080)
	endpointSliceName = "app-endpointslice"
	endpointAddr      = "1.2.3.4"

	nsKey             = types.NamespacedName{Name: workNS}
	svcOrSvcExportKey = types.NamespacedName{Namespace: workNS, Name: svcName}
	endpointSliceKey  = types.NamespacedName{Namespace: workNS, Name: endpointSliceName}

	ctx = context.Background()
)

var _ = Describe("evaluate service export and endpointslice export/import latency", Serial, Ordered, func() {
	Context("light-load export/import latency between clusters in the same region", Serial, Ordered, func() {
		// Cluster member-1 and member-2 are two clusters from the the same region; this test case exports a
		// Service from member-1 and imports it (as a multi-cluster service) to member-2.
		BeforeAll(func() {
			Expect(hubClusterClient.Create(ctx, framework.Namespace(workNS))).Should(Succeed(), "Failed to create namespace %s in cluster %s", workNS, hubCluster.Name())
			Expect(memberCluster1Client.Create(ctx, framework.Namespace(workNS))).Should(Succeed(), "Failed to create namespace %s in cluster %s", workNS, memberCluster1.Name())
			Expect(memberCluster2Client.Create(ctx, framework.Namespace(workNS))).Should(Succeed(), "Failed to create namespace %s in cluster %s", workNS, memberCluster2.Name())

			svc := framework.ClusterIPServiceWithNoSelector(workNS, svcName, svcPortName, svcPort, svcTargetPort)
			Expect(memberCluster1Client.Create(ctx, svc)).Should(Succeed(), "Failed to create service %s in cluster %s", svcName, memberCluster1.Name())
		})

		It("export the service", func() {
			svcExport := framework.ServiceExport(workNS, svcName)
			Expect(memberCluster1Client.Create(ctx, svcExport)).Should(Succeed(), "Failed to create serviceExport %s in cluster %s", svcName, memberCluster1.Name())

			// Wait until the export completes.
			Eventually(func() error {
				svcExport := &fleetnetv1alpha1.ServiceExport{}
				if err := memberCluster1Client.Get(ctx, svcOrSvcExportKey, svcExport); err != nil {
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
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed(), "Failed to export service %s in cluster %s (export is not valid or in conflict)", svcName, memberCluster1.Name())
		})

		It("import the service", func() {
			multiClusterSvc := framework.MultiClusterService(workNS, svcName, svcName)
			Expect(memberCluster2Client.Create(ctx, multiClusterSvc)).Should(Succeed(), "Failed to create multiClusterService %s in cluster %s", svcName, memberCluster2.Name())

			// Wait until the service is imported.
			// Use a longer timeout/interval setting as it may take additional time for a MCS to get ready.
			Eventually(func() error {
				multiClusterSvc := &fleetnetv1alpha1.MultiClusterService{}
				if err := memberCluster2Client.Get(ctx, svcOrSvcExportKey, multiClusterSvc); err != nil {
					return fmt.Errorf("multiClusterService Get(%+v), got %w, want no error", svcOrSvcExportKey, err)
				}

				if _, ok := multiClusterSvc.Labels[objectmeta.MultiClusterServiceLabelDerivedService]; !ok {
					return fmt.Errorf("no derived service is created")
				}
				return nil
			}, eventuallyTimeout*2, eventuallyInterval*2).Should(Succeed(), "Failed to create multiClusterService %s in cluster %s (no load balancer IP assigned)", svcName, memberCluster2.Name())
		})

		It("propagate an endpointSlice", func() {
			endpointSlice := framework.ManuallyManagedIPv4EndpointSlice(workNS, endpointSliceName, svcName, svcPortName, svcTargetPort, []string{endpointAddr})
			Expect(memberCluster1Client.Create(ctx, endpointSlice)).Should(Succeed(), "Failed to create endpointSlice %s in cluster %s", endpointSliceName, memberCluster1.Name())

			// Wait until the endpointSlice is imported.
			Eventually(func() error {
				endpointSlice := &discoveryv1.EndpointSlice{}
				if err := memberCluster1Client.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
					return fmt.Errorf("endpointSlice Get(%+v), got %w, want no error", endpointSliceKey, err)
				}

				endpointSliceUniqueName, ok := endpointSlice.Annotations[objectmeta.ExportedObjectAnnotationUniqueName]
				if !ok {
					return fmt.Errorf("endpointSlice %s not yet exported (no unique name assigned)", endpointSliceName)
				}

				exportedEndpointSliceKey := types.NamespacedName{Namespace: fleetSystemNS, Name: endpointSliceUniqueName}
				exportedEndpointSlice := &discoveryv1.EndpointSlice{}
				if err := memberCluster2Client.Get(ctx, exportedEndpointSliceKey, exportedEndpointSlice); err != nil {
					return fmt.Errorf("endpointSlice Get(%+v), got %w, want no error", exportedEndpointSliceKey, err)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed(), "Failed to export endpointSlice %s (no unique name assigned) from cluster %s to cluster %s", endpointSliceName, memberCluster1.Name(), memberCluster2.Name())
		})

		It("collect metrics", func() {
			fmt.Fprintf(GinkgoWriter, "wait for %d seconds to give Prometheus some time to scrape the metric data points\n", prometheusScrapeInterval*3/time.Second)
			time.Sleep(prometheusScrapeInterval * 3)

			prometheusAPIClient1, err := framework.NewPrometheusAPIClient(memberCluster1.PrometheusAPIServiceAddress())
			Expect(err).To(BeNil())
			prometheusAPIClient2, err := framework.NewPrometheusAPIClient(memberCluster2.PrometheusAPIServiceAddress())
			Expect(err).To(BeNil())

			for _, phi := range quantilePhis {
				res, err := framework.QueryHistogramQuantileAggregated(ctx, prometheusAPIClient1, phi, svcExportDurationHistogramName)
				Expect(err).To(BeNil())
				fmt.Fprintf(GinkgoWriter, "service export duration: phi %f, time %f milliseconds\n", phi, res)

				res, err = framework.QueryHistogramQuantileAggregated(ctx, prometheusAPIClient2, phi, endpointSliceExportImportDurationHistogramName)
				Expect(err).To(BeNil())
				fmt.Fprintf(GinkgoWriter, "endpointslice export/import duration: phi %f, time %f milliseconds\n", phi, res)
			}
		})

		AfterAll(func() {
			Expect(memberCluster1Client.Delete(ctx, framework.Namespace(workNS))).Should(Succeed(), "Failed to delete namespace %s from cluster %s", workNS, memberCluster1.Name())
			// Confirm that the namespace has been deleted; this helps make the test less flaky.
			// Use a longer timeout/interval as it may take additional time to clean up resources.
			Eventually(func() error {
				ns := &corev1.Namespace{}
				if err := memberCluster1Client.Get(ctx, nsKey, ns); !errors.IsNotFound(err) {
					return fmt.Errorf("namespace Get(%+v), got %w, want not found error", nsKey, err)
				}
				return nil
			}, eventuallyTimeout*3, eventuallyInterval*3).Should(Succeed(), "Failed to delete namespace %s from cluster %s", workNS, memberCluster1.Name())

			Expect(memberCluster2Client.Delete(ctx, framework.Namespace(workNS))).Should(Succeed(), "Failed to delete namespace %s from cluster %s", workNS, memberCluster2.Name())
			// Confirm that the namespace has been deleted; this helps make the test less flaky.
			// Use a longer timeout/interval as it may take additional time to clean up resources.
			Eventually(func() error {
				ns := &corev1.Namespace{}
				if err := memberCluster2Client.Get(ctx, nsKey, ns); !errors.IsNotFound(err) {
					return fmt.Errorf("namespace Get(%+v), got %w, want not found error", nsKey, err)
				}
				return nil
			}, eventuallyTimeout*3, eventuallyInterval*3).Should(Succeed(), "Failed to delete namespace %s from cluster %s", workNS, memberCluster2.Name())

			Expect(hubClusterClient.Delete(ctx, framework.Namespace(workNS))).Should(Succeed(), "Failed to delete namespace %s from cluster %s", workNS, hubCluster.Name())
			// Confirm that the namespace has been deleted; this helps make the test less flaky.
			// Use a longer timeout/interval as it may take additional time to clean up resources.
			Eventually(func() error {
				ns := &corev1.Namespace{}
				if err := hubClusterClient.Get(ctx, nsKey, ns); !errors.IsNotFound(err) {
					return fmt.Errorf("namespace Get(%+v), got %w, want not found error", nsKey, err)
				}
				return nil
			}, eventuallyTimeout*3, eventuallyInterval*3).Should(Succeed(), "Failed to delete namespace %s from cluster %s", workNS, hubCluster.Name())
		})
	})

	Context("light-load object export/import latency between clusters cross regions", Serial, Ordered, func() {
		// Cluster member-2 and member-3 are two clusters from the different regions; this test case exports a
		// Service from cluster member-2 and imports it (as a multi-cluster service) into member-3.
		BeforeAll(func() {
			Expect(hubClusterClient.Create(ctx, framework.Namespace(workNS))).Should(Succeed(), "Failed to create namespace %s in cluster %s", workNS, hubCluster.Name())
			Expect(memberCluster2Client.Create(ctx, framework.Namespace(workNS))).Should(Succeed(), "Failed to create namespace %s in cluster %s", workNS, memberCluster2.Name())
			Expect(memberCluster3Client.Create(ctx, framework.Namespace(workNS))).Should(Succeed(), "Failed to create namespace %s in cluster %s", workNS, memberCluster3.Name())

			svc := framework.ClusterIPServiceWithNoSelector(workNS, svcName, svcPortName, svcPort, svcTargetPort)
			Expect(memberCluster2Client.Create(ctx, svc)).Should(Succeed(), "Failed to create service %s in cluster %s", svcName, memberCluster2.Name())
		})

		It("export the service", func() {
			svcExport := framework.ServiceExport(workNS, svcName)
			Expect(memberCluster2Client.Create(ctx, svcExport)).Should(Succeed(), "Failed to create serviceExport %s in cluster %s", svcName, memberCluster2.Name())

			// Wait until the export completes.
			Eventually(func() error {
				svcExport := &fleetnetv1alpha1.ServiceExport{}
				if err := memberCluster2Client.Get(ctx, svcOrSvcExportKey, svcExport); err != nil {
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
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed(), "Failed to export service %s in cluster %s (export is not valid or in conflict)", svcName, memberCluster2.Name())
		})

		It("import the service", func() {
			multiClusterSvc := framework.MultiClusterService(workNS, svcName, svcName)
			Expect(memberCluster3Client.Create(ctx, multiClusterSvc)).Should(Succeed(), "Failed to create multiClusterService %s in cluster %s", svcName, memberCluster3.Name())

			// Wait until the service is imported.
			// Use a longer timeout/interval setting as it may take additional time for a MCS to get ready.
			Eventually(func() error {
				multiClusterSvc := &fleetnetv1alpha1.MultiClusterService{}
				if err := memberCluster3Client.Get(ctx, svcOrSvcExportKey, multiClusterSvc); err != nil {
					return fmt.Errorf("multiClusterService Get(%+v), got %w, want no error", svcOrSvcExportKey, err)
				}

				if _, ok := multiClusterSvc.Labels[objectmeta.MultiClusterServiceLabelDerivedService]; !ok {
					return fmt.Errorf("no derived service is created")
				}
				return nil
			}, eventuallyTimeout*2, eventuallyInterval*2).Should(Succeed(), "Failed to create multiClusterService %s in cluster %s (no load balancer IP assigned)", svcName, memberCluster3.Name())
		})

		It("propagate an endpointSlice", func() {
			endpointSlice := framework.ManuallyManagedIPv4EndpointSlice(workNS, endpointSliceName, svcName, svcPortName, svcTargetPort, []string{endpointAddr})
			Expect(memberCluster2Client.Create(ctx, endpointSlice)).Should(Succeed(), "Failed to create endpointSlice %s in cluster %s", endpointSliceName, memberCluster2.Name())

			// Wait until the endpointSlice is imported.
			Eventually(func() error {
				endpointSlice := &discoveryv1.EndpointSlice{}
				if err := memberCluster2Client.Get(ctx, endpointSliceKey, endpointSlice); err != nil {
					return fmt.Errorf("endpointSlice Get(%+v), got %w, want no error", endpointSliceKey, err)
				}

				endpointSliceUniqueName, ok := endpointSlice.Annotations[objectmeta.ExportedObjectAnnotationUniqueName]
				if !ok {
					return fmt.Errorf("endpointSlice %s not yet exported (no unique name assigned)", endpointSliceName)
				}

				exportedEndpointSliceKey := types.NamespacedName{Namespace: fleetSystemNS, Name: endpointSliceUniqueName}
				exportedEndpointSlice := &discoveryv1.EndpointSlice{}
				if err := memberCluster3Client.Get(ctx, exportedEndpointSliceKey, exportedEndpointSlice); err != nil {
					return fmt.Errorf("endpointSlice Get(%+v), got %w, want no error", exportedEndpointSliceKey, err)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed(), "Failed to export endpointSlice %s (no unique name assigned) from cluster %s to cluster %s", endpointSliceName, memberCluster2.Name(), memberCluster3.Name())
		})

		It("collect metrics", func() {
			fmt.Fprintf(GinkgoWriter, "wait for %d seconds to give Prometheus some time to scrape the metric data points\n", prometheusScrapeInterval*3/time.Second)
			time.Sleep(prometheusScrapeInterval * 3)

			prometheusAPIClient2, err := framework.NewPrometheusAPIClient(memberCluster2.PrometheusAPIServiceAddress())
			Expect(err).To(BeNil())
			prometheusAPIClient3, err := framework.NewPrometheusAPIClient(memberCluster3.PrometheusAPIServiceAddress())
			Expect(err).To(BeNil())

			for _, phi := range quantilePhis {
				res, err := framework.QueryHistogramQuantileAggregated(ctx, prometheusAPIClient2, phi, svcExportDurationHistogramName)
				Expect(err).To(BeNil())
				fmt.Fprintf(GinkgoWriter, "service export duration: phi %f, time %f milliseconds\n", phi, res)

				res, err = framework.QueryHistogramQuantileAggregated(ctx, prometheusAPIClient3, phi, endpointSliceExportImportDurationHistogramName)
				Expect(err).To(BeNil())
				fmt.Fprintf(GinkgoWriter, "endpointslice export/import duration: phi %f, time %f milliseconds\n", phi, res)
			}
		})

		AfterAll(func() {
			Expect(memberCluster2Client.Delete(ctx, framework.Namespace(workNS))).Should(Succeed(), "Failed to delete namespace %s from cluster %s", workNS, memberCluster2.Name())
			// Confirm that the namespace has been deleted; this helps make the test less flaky.
			// Use a longer timeout/interval as it may take additional time to clean up resources.
			Eventually(func() error {
				ns := &corev1.Namespace{}
				if err := memberCluster2Client.Get(ctx, nsKey, ns); !errors.IsNotFound(err) {
					return fmt.Errorf("namespace Get(%+v), got %w, want not found error", nsKey, err)
				}
				return nil
			}, eventuallyTimeout*3, eventuallyInterval*3).Should(Succeed(), "Failed to delete namespace %s from cluster %s", workNS, memberCluster2.Name())

			Expect(memberCluster3Client.Delete(ctx, framework.Namespace(workNS))).Should(Succeed(), "Failed to delete namespace %s from cluster %s", workNS, memberCluster3.Name())
			// Confirm that the namespace has been deleted; this helps make the test less flaky.
			// Use a longer timeout/interval as it may take additional time to clean up resources.
			Eventually(func() error {
				ns := &corev1.Namespace{}
				if err := memberCluster3Client.Get(ctx, nsKey, ns); !errors.IsNotFound(err) {
					return fmt.Errorf("namespace Get(%+v), got %w, want not found error", nsKey, err)
				}
				return nil
			}, eventuallyTimeout*3, eventuallyInterval*3).Should(Succeed(), "Failed to delete namespace %s from cluster %s", workNS, memberCluster3.Name())

			Expect(hubClusterClient.Delete(ctx, framework.Namespace(workNS))).Should(Succeed(), "Failed to delete namespace %s from cluster %s", workNS, hubCluster.Name())
			// Confirm that the namespace has been deleted; this helps make the test less flaky.
			// Use a longer timeout/interval as it may take additional time to clean up resources.
			Eventually(func() error {
				ns := &corev1.Namespace{}
				if err := hubClusterClient.Get(ctx, nsKey, ns); !errors.IsNotFound(err) {
					return fmt.Errorf("namespace Get(%+v), got %w, want not found error", nsKey, err)
				}
				return nil
			}, eventuallyTimeout*3, eventuallyInterval*3).Should(Succeed(), "Failed to delete namespace %s from cluster %s", workNS, hubCluster.Name())
		})
	})

	// Note: this test scenario may need a longer global timeout value to complete.
	Context("heavy-load object export/import latency between clusters", Serial, Ordered, func() {
		type workRecord struct {
			ns                             *corev1.Namespace
			svc                            *corev1.Service
			multiClusterSvc                *fleetnetv1alpha1.MultiClusterService
			multiClusterSvcOwnerClusterIdx int
			endpointSlice                  *discoveryv1.EndpointSlice
		}

		type workItem struct {
			k8sObjType      string
			namespacedKey   types.NamespacedName
			k8sObj          client.Object
			ownerClusterIdx int
			retries         int
			lastSeenError   error
		}

		var (
			// Total number of fleet-scoped services to created.
			// Specifically, for each work record the test will create:
			// * 1 namespace in each hub + member cluster; and
			// * 1 multi-cluster service in one of the member clusters (picked in a round-robin manner); and
			// * 1 service in the remaining three member clusters, 3 in total; and
			// * 1 endpointSlice in each of the remaining three member clusters, 3 in total
			workRecordCount = 80
			workRecords     = []workRecord{}
			// The channel buffer size.
			bufferSize = 20
			// The number of goroutines to deploy for creating resources and/or performing checks.
			maxParallelism = 5
			// The number for each goroutine to retry a specific op.
			maxRetries = 3
			// The wait period between steps; this helps make sure that checks (e.g. whether a specific service
			// has been successfully exported) performed in this test scenario will not contend with built-in
			// controllers and Fleet networking controllers for resources.
			coolDownPeriod = time.Minute * 5

			endpointSliceAddrTpl = "10.0.0.%d"
		)

		for i := 0; i < workRecordCount; i++ {
			workNS := fmt.Sprintf("work-%d", i)
			svcName := fmt.Sprintf("svc-%d", i)
			endpointSliceName := fmt.Sprintf("endpointslice-%d", i)

			w := workRecord{
				ns:                             framework.Namespace(workNS),
				svc:                            framework.ClusterIPServiceWithNoSelector(workNS, svcName, svcPortName, svcPort, svcTargetPort),
				multiClusterSvc:                framework.MultiClusterService(workNS, svcName, svcName),
				multiClusterSvcOwnerClusterIdx: i % 4,
				endpointSlice: framework.ManuallyManagedIPv4EndpointSlice(
					workNS, endpointSliceName, svcName, svcPortName, svcTargetPort, []string{fmt.Sprintf(endpointSliceAddrTpl, 255)}),
			}
			workRecords = append(workRecords, w)
		}

		BeforeAll(func() {
			// Create objects in parallel using a producer/consumer pattern.
			workItemCh := make(chan workItem, bufferSize)
			deadLetterCh := make(chan workItem)

			var wg sync.WaitGroup

			// Single producer
			wg.Add(1)
			go func() {
				defer wg.Done()

				for i := 0; i < workRecordCount; i++ {
					r := workRecords[i]
					// Add a work item to create a namespace in the hub cluster.
					workItemCh <- workItem{
						k8sObjType:    "namespace",
						namespacedKey: types.NamespacedName{Name: r.ns.Name},
						k8sObj:        r.ns.DeepCopy(),
						// Index -1 is reserved for the hub cluster.
						ownerClusterIdx: -1,
					}

					// Add work items to create namespaces + services in member clusters.
					for j := 0; j < len(memberClusters); j++ {
						workItemCh <- workItem{
							k8sObjType:      "namespace",
							namespacedKey:   types.NamespacedName{Name: r.ns.Name},
							k8sObj:          r.ns.DeepCopy(),
							ownerClusterIdx: j,
						}

						if j != r.multiClusterSvcOwnerClusterIdx {
							workItemCh <- workItem{
								k8sObjType:      "service",
								namespacedKey:   types.NamespacedName{Namespace: r.ns.Name, Name: r.svc.Name},
								k8sObj:          r.svc.DeepCopy(),
								ownerClusterIdx: j,
							}
						}
					}
				}

				// Close the channel; signal consumers to stop.
				close(workItemCh)
			}()

			// Multiple consumers
			for i := 0; i < maxParallelism-1; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()

					for w := range workItemCh {
						clusterIdx := w.ownerClusterIdx
						var client client.Client
						if clusterIdx == -1 {
							client = hubClusterClient
						} else {
							client = memberClusters[clusterIdx].Client()
						}

						if err := client.Create(ctx, w.k8sObj); err != nil && !errors.IsAlreadyExists(err) {
							w.retries++
							w.lastSeenError = err
							if w.retries > maxRetries {
								deadLetterCh <- w
								return
							}
							workItemCh <- w
						}
					}
				}()
			}

			stopCh := make(chan struct{})
			// Spin up a goroutine to wait for producer + consumer goroutines to exit.
			go func() {
				defer close(stopCh)
				wg.Wait()
			}()

			// Wait until all works are done; or an error has occurred.
			Eventually(func() error {
				for {
					select {
					case <-stopCh:
						return nil
					case w := <-deadLetterCh:
						Fail(fmt.Sprintf("an %s object cannot be created: %v", w.k8sObjType, w.lastSeenError))
					case <-time.After(pollInterval):
						return fmt.Errorf("no completion or error within time limit")
					}
				}
			}, longEventuallyTimeout, longEventuallyInterval).Should(Succeed(), "Failed to create namespaces and/or services in all clusters")
		})

		It("export services", func() {
			// Create objects in parallel using a producer/consumer pattern.
			workItemCh := make(chan workItem, bufferSize)
			deadLetterCh := make(chan workItem)

			var wg sync.WaitGroup

			// Single producer
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < workRecordCount; i++ {
					r := workRecords[i]
					// Add a work item to export a service from a member cluster.
					for j := 0; j < len(memberClusters); j++ {
						if j == r.multiClusterSvcOwnerClusterIdx {
							continue
						}

						workItemCh <- workItem{
							k8sObjType:    "serviceExport",
							namespacedKey: types.NamespacedName{Namespace: r.ns.Name, Name: r.svc.Name},
							k8sObj: &fleetnetv1alpha1.ServiceExport{
								ObjectMeta: metav1.ObjectMeta{
									Namespace: r.ns.Name,
									Name:      r.svc.Name,
								},
							},
							ownerClusterIdx: j,
						}
					}
				}

				// Close the channel; signal consumers to stop.
				close(workItemCh)
			}()

			// Multiple consumers
			for i := 0; i < maxParallelism-1; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()

					for w := range workItemCh {
						memberClusterClient := memberClusters[w.ownerClusterIdx].Client()
						if err := memberClusterClient.Create(ctx, w.k8sObj); err != nil && !errors.IsAlreadyExists(err) {
							w.retries++
							w.lastSeenError = err
							if w.retries > maxRetries {
								deadLetterCh <- w
								return
							}
							workItemCh <- w
						}
					}
				}()
			}

			stopCh := make(chan struct{})
			// Spin up a goroutine to wait for producer + consumer goroutines to exit.
			go func() {
				defer close(stopCh)
				wg.Wait()
			}()

			// Wait until all works are done.
			Eventually(func() error {
				for {
					select {
					case <-stopCh:
						return nil
					case w := <-deadLetterCh:
						Fail(fmt.Sprintf("an %s object cannot be created: %v", w.k8sObjType, w.lastSeenError))
					case <-time.After(pollInterval):
						return fmt.Errorf("no completion or error within time limit")
					}
				}
			}, longEventuallyTimeout, longEventuallyInterval).Should(Succeed(), "Failed to create namespaces and/or services in all clusters")
		})

		It("wait for service exports to complete", func() {
			// Cool down a while; this helps make the test less flaky.
			fmt.Fprintf(GinkgoWriter, "cool down for %d minutes\n", coolDownPeriod/time.Minute)
			time.Sleep(coolDownPeriod)

			// Check export status in parallel using a producer/consumer pattern.
			workItemCh := make(chan workItem, bufferSize)

			var wg sync.WaitGroup

			// Single producer
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < workRecordCount; i++ {
					r := workRecords[i]

					for j := 0; j < len(memberClusters); j++ {
						if j == r.multiClusterSvcOwnerClusterIdx {
							continue
						}

						workItemCh <- workItem{
							k8sObjType:      "serviceExport",
							namespacedKey:   types.NamespacedName{Namespace: r.ns.Name, Name: r.svc.Name},
							ownerClusterIdx: j,
						}
					}
				}

				// Close the channel; signal consumers to stop.
				close(workItemCh)
			}()

			// Multiple consumers
			// Use longer timeout/interval as it may take additional time to export a service under heavy load.
			for i := 0; i < maxParallelism-1; i++ {
				wg.Add(1)
				go func() {
					defer GinkgoRecover()
					defer wg.Done()

					for w := range workItemCh {
						fmt.Fprintf(GinkgoWriter, "exporting service %+v from cluster %d\n", w.namespacedKey, w.ownerClusterIdx)

						memberClusterClient := memberClusters[w.ownerClusterIdx].Client()
						svcExportKey := w.namespacedKey
						Eventually(func() error {
							svcExport := &fleetnetv1alpha1.ServiceExport{}
							if err := memberClusterClient.Get(ctx, svcExportKey, svcExport); err != nil {
								return fmt.Errorf("serviceExport Get(%+v), got %w, want no error", svcExportKey, err)
							}

							svcExportValidCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportValid))
							if svcExportValidCond == nil || svcExportValidCond.Status != metav1.ConditionTrue {
								return fmt.Errorf("serviceExportValid condition, got %+v, want true condition", svcExportValidCond)
							}

							svcExportConflictCond := meta.FindStatusCondition(svcExport.Status.Conditions, string(fleetnetv1alpha1.ServiceExportConflict))
							if svcExportConflictCond == nil || svcExportConflictCond.Status != metav1.ConditionFalse {
								return fmt.Errorf("serviceExportConflict condition, got %+v, want false condition", svcExportConflictCond)
							}

							fmt.Fprintf(GinkgoWriter, "finished exporting service %+v from cluster %d\n", w.namespacedKey, w.ownerClusterIdx)
							return nil
						}, eventuallyTimeout*2, eventuallyInterval*2).Should(Succeed(),
							"Failed to export service %+v (not valid/in conflict) from cluster %s",
							w.namespacedKey, memberClusters[w.ownerClusterIdx].Name())
					}
				}()
			}

			// Wait until all work is done.
			wg.Wait()
		})

		It("import services", func() {
			// Create objects in parallel using a producer/consumer pattern.
			workItemCh := make(chan workItem, bufferSize)
			deadLetterCh := make(chan workItem)

			var wg sync.WaitGroup

			// Single producer
			wg.Add(1)
			go func() {
				defer wg.Done()

				for i := 0; i < workRecordCount; i++ {
					r := workRecords[i]

					workItemCh <- workItem{
						k8sObjType:      "multiClusterService",
						namespacedKey:   types.NamespacedName{Namespace: r.ns.Name, Name: r.svc.Name},
						k8sObj:          r.multiClusterSvc.DeepCopy(),
						ownerClusterIdx: r.multiClusterSvcOwnerClusterIdx,
					}
				}

				// Close the channel; signal consumers to stop.
				close(workItemCh)
			}()

			// Multiple consumers
			for i := 0; i < maxParallelism-1; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()

					for w := range workItemCh {
						memberClusterClient := memberClusters[w.ownerClusterIdx].Client()
						if err := memberClusterClient.Create(ctx, w.k8sObj); err != nil && !errors.IsAlreadyExists(err) {
							w.retries++
							w.lastSeenError = err
							if w.retries > maxRetries {
								deadLetterCh <- w
								return
							}
							workItemCh <- w
						}
					}
				}()
			}

			stopCh := make(chan struct{})
			// Spin up a goroutine to wait for producer + consumer goroutines to exit.
			go func() {
				defer close(stopCh)
				wg.Wait()
			}()

			// Wait until all works are done.
			Eventually(func() error {
				for {
					select {
					case <-stopCh:
						return nil
					case w := <-deadLetterCh:
						Fail(fmt.Sprintf("an %s object cannot be created: %v", w.k8sObjType, w.lastSeenError))
					case <-time.After(pollInterval):
						return fmt.Errorf("no completion or error within time limit")
					}
				}
			}, longEventuallyTimeout, longEventuallyInterval).Should(Succeed(), "Failed to create multiClusterServices in all clusters")
		})

		It("wait for service imports to complete", func() {
			// Cool down a while; this helps make the test less flaky.
			fmt.Fprintf(GinkgoWriter, "cool down for %d minutes\n", coolDownPeriod/time.Minute)
			time.Sleep(coolDownPeriod)

			// Check export status in parallel using a producer/consumer pattern.
			workItemCh := make(chan workItem, bufferSize)

			var wg sync.WaitGroup

			// Single producer
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < workRecordCount; i++ {
					r := workRecords[i]

					workItemCh <- workItem{
						k8sObjType:      "multiClusterService",
						namespacedKey:   types.NamespacedName{Namespace: r.ns.Name, Name: r.svc.Name},
						ownerClusterIdx: r.multiClusterSvcOwnerClusterIdx,
					}
				}

				// Close the channel; signal consumers to stop.
				close(workItemCh)
			}()

			// Multiple consumers
			// Use longer timeout/interval as it may take additional time for a MCS to get ready.
			for i := 0; i < maxParallelism-1; i++ {
				wg.Add(1)
				go func() {
					defer GinkgoRecover()
					defer wg.Done()

					for w := range workItemCh {
						fmt.Fprintf(GinkgoWriter, "processing MCS %+v from cluster %d\n", w.namespacedKey, w.ownerClusterIdx)

						memberClusterClient := memberClusters[w.ownerClusterIdx].Client()
						multiClusterSvcKey := w.namespacedKey
						Eventually(func() error {
							multiClusterSvc := &fleetnetv1alpha1.MultiClusterService{}
							if err := memberClusterClient.Get(ctx, multiClusterSvcKey, multiClusterSvc); err != nil {
								return fmt.Errorf("multiClusterService Get(%+v), got %w, want no error", multiClusterSvcKey, err)
							}

							if _, ok := multiClusterSvc.Labels[objectmeta.MultiClusterServiceLabelDerivedService]; !ok {
								return fmt.Errorf("no derived service is created")
							}

							fmt.Fprintf(GinkgoWriter, "finished processing MCS %+v from cluster %d\n", w.namespacedKey, w.ownerClusterIdx)
							return nil
						}, eventuallyTimeout*2, eventuallyInterval*2).Should(Succeed(),
							"Failed to import service as multiClusterService %+v from cluster %s (no load balancer assigned)",
							w.namespacedKey, memberClusters[w.ownerClusterIdx].Name())
					}
				}()
			}

			// Wait until all work is done.
			wg.Wait()
		})

		It("propagate endpointSlices", func() {
			// Create objects in parallel using a producer/consumer pattern.
			workItemCh := make(chan workItem, bufferSize)
			deadLetterCh := make(chan workItem)

			var wg sync.WaitGroup

			// Single producer
			wg.Add(1)
			go func() {
				defer wg.Done()

				for i := 0; i < workRecordCount; i++ {
					r := workRecords[i]

					for j := 0; j < len(memberClusters); j++ {
						if j == r.multiClusterSvcOwnerClusterIdx {
							continue
						}

						endpointSlice := r.endpointSlice.DeepCopy()
						endpointSlice.Endpoints[0].Addresses = []string{fmt.Sprintf(endpointSliceAddrTpl, j)}
						workItemCh <- workItem{
							k8sObjType:      "endpointSlice",
							namespacedKey:   types.NamespacedName{Namespace: r.ns.Name, Name: r.endpointSlice.Name},
							k8sObj:          endpointSlice,
							ownerClusterIdx: j,
						}
					}
				}

				// Close the channel; signal consumers to stop.
				close(workItemCh)
			}()

			// Multiple consumers
			for i := 0; i < maxParallelism-1; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()

					for w := range workItemCh {
						memberClusterClient := memberClusters[w.ownerClusterIdx].Client()
						if err := memberClusterClient.Create(ctx, w.k8sObj); err != nil && !errors.IsAlreadyExists(err) {
							w.retries++
							w.lastSeenError = err
							if w.retries > maxRetries {
								deadLetterCh <- w
								return
							}
							workItemCh <- w
						}
					}
				}()
			}

			stopCh := make(chan struct{})
			// Spin up a goroutine to wait for producer + consumer goroutines to exit.
			go func() {
				defer close(stopCh)
				wg.Wait()
			}()

			// Wait until all works are done.
			Eventually(func() error {
				for {
					select {
					case <-stopCh:
						return nil
					case w := <-deadLetterCh:
						Fail(fmt.Sprintf("an %s object cannot be created: %v", w.k8sObjType, w.lastSeenError))
					case <-time.After(pollInterval):
						return fmt.Errorf("no completion or error within time limit")
					}
				}
			}, longEventuallyTimeout, longEventuallyInterval).Should(Succeed(), "Failed to create endpointSlices in all clusters")
		})

		It("wait for endpointSlice imports to complete", func() {
			// Cool down a while; this helps make the test less flaky.
			fmt.Fprintf(GinkgoWriter, "cool down for %d minutes\n", coolDownPeriod/time.Minute)
			time.Sleep(coolDownPeriod)

			// Check export status in parallel using a producer/consumer pattern.
			workItemCh := make(chan workItem, bufferSize)

			var wg sync.WaitGroup

			// Single producer
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < workRecordCount; i++ {
					r := workRecords[i]

					workItemCh <- workItem{
						k8sObjType:      "multiClusterService",
						namespacedKey:   types.NamespacedName{Namespace: r.ns.Name, Name: r.svc.Name},
						ownerClusterIdx: r.multiClusterSvcOwnerClusterIdx,
					}
				}

				// Close the channel; signal consumers to stop.
				close(workItemCh)
			}()

			// Multiple consumers
			// Use longer timeout/interval as it may take additional time to propagate endpointSlices.
			for i := 0; i < maxParallelism-1; i++ {
				wg.Add(1)
				go func() {
					defer GinkgoRecover()
					defer wg.Done()

					for w := range workItemCh {
						fmt.Fprintf(GinkgoWriter, "processing MCS %+v from cluster %d\n", w.namespacedKey, w.ownerClusterIdx)

						memberClusterClient := memberClusters[w.ownerClusterIdx].Client()
						multiClusterSvcKey := w.namespacedKey
						Eventually(func() error {
							multiClusterSvc := &fleetnetv1alpha1.MultiClusterService{}
							if err := memberClusterClient.Get(ctx, multiClusterSvcKey, multiClusterSvc); err != nil {
								return fmt.Errorf("multiClusterService Get(%+v), got %w, want no error", multiClusterSvcKey, err)
							}

							derivedSvcName, ok := multiClusterSvc.Labels[objectmeta.MultiClusterServiceLabelDerivedService]
							if !ok {
								return fmt.Errorf("multiClusterService derived service label is not present")
							}

							endpointSliceList := &discoveryv1.EndpointSliceList{}
							endpointSliceListOpts := client.ListOptions{
								Namespace: fleetSystemNS,
								LabelSelector: labels.SelectorFromSet(labels.Set{
									discoveryv1.LabelServiceName: derivedSvcName,
								}),
							}
							if err := memberClusterClient.List(ctx, endpointSliceList, &endpointSliceListOpts); err != nil {
								return fmt.Errorf("endpointSlice List(), got %w, want no error", err)
							}

							if len(endpointSliceList.Items) != len(memberClusters)-1 {
								return fmt.Errorf("endpointSliceList length, got %d, want %d", len(endpointSliceList.Items), len(memberClusters)-1)
							}

							fmt.Fprintf(GinkgoWriter, "finished processing MCS %+v from cluster %d\n", w.namespacedKey, w.ownerClusterIdx)
							return nil
						}, eventuallyTimeout*3, eventuallyInterval*3).Should(Succeed(),
							"Failed to import all endpointSlices to cluster %s",
							memberClusters[w.ownerClusterIdx].Name())
					}
				}()
			}

			// Wait until all work is done.
			wg.Wait()
		})

		It("collect metrics", func() {
			fmt.Fprintf(GinkgoWriter, "wait for %d seconds to give Prometheus some time to scrape the metric data points\n", prometheusScrapeInterval*3/time.Second)
			time.Sleep(prometheusScrapeInterval * 3)

			svcExportDurationQuantileSums := make([]float64, len(quantilePhis))
			endpointSliceExportImportDurationQuantileSums := make([]float64, len(quantilePhis))

			for _, memberClient := range memberClusters {
				for phiIdx, phi := range quantilePhis {
					prometheusAPIClient, err := framework.NewPrometheusAPIClient(memberClient.PrometheusAPIServiceAddress())
					Expect(err).To(BeNil())

					svcExportDurationQuantile, err := framework.QueryHistogramQuantileAggregated(ctx,
						prometheusAPIClient, phi, svcExportDurationHistogramName)
					Expect(err).To(BeNil())
					svcExportDurationQuantileSums[phiIdx] += svcExportDurationQuantile

					endpointSliceExportImportQuantile, err := framework.QueryHistogramQuantileAggregated(ctx,
						prometheusAPIClient, phi, endpointSliceExportImportDurationHistogramName)
					Expect(err).To(BeNil())
					endpointSliceExportImportDurationQuantileSums[phiIdx] += endpointSliceExportImportQuantile
				}
			}

			for phiIdx, phi := range quantilePhis {
				fmt.Fprintf(GinkgoWriter, "service export duration: phi %f, time %f milliseconds\n",
					phi, svcExportDurationQuantileSums[phiIdx]/float64(len(memberClusters)))
				fmt.Fprintf(GinkgoWriter, "endpointslice export/import duration: phi %f, time %f milliseconds\n",
					phi, endpointSliceExportImportDurationQuantileSums[phiIdx]/float64(len(memberClusters)))
			}
		})
	})
})
