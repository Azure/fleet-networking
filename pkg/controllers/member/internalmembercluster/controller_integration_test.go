package internalmembercluster

import (
	"context"
	"fmt"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2/klogr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

var _ = Describe("Test InternalMemberCluster Controller", func() {
	var (
		imcKey  = types.NamespacedName{Namespace: memberClusterNamespace, Name: memberClusterName}
		options = []cmp.Option{
			cmpopts.IgnoreFields(fleetv1alpha1.AgentStatus{}, "LastReceivedHeartbeat"),
			cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "ObservedGeneration"),
		}
	)

	const (
		timeout         = time.Second * 10
		interval        = time.Millisecond * 250
		duration        = time.Second * 2
		heartbeatPeriod = 1
	)

	Context("Test mcs agent type", func() {
		var (
			ctx    context.Context
			cancel context.CancelFunc
		)
		BeforeEach(func() {
			By("Starting the controller manager for mcs agent type")
			mgr, err := ctrl.NewManager(hubCfg, ctrl.Options{
				Scheme:             scheme.Scheme,
				MetricsBindAddress: "0",
				Logger:             klogr.NewWithOptions(klogr.WithFormat(klogr.FormatKlog)),
				Port:               4848,
			})
			Expect(err).NotTo(HaveOccurred())

			err = (&Reconciler{
				HubClient:    mgr.GetClient(),
				MemberClient: memberClient,
				AgentType:    fleetv1alpha1.MultiClusterServiceAgent,
			}).SetupWithManager(mgr)
			Expect(err).ToNot(HaveOccurred())

			ctx, cancel = context.WithCancel(context.TODO())
			go func() {
				defer GinkgoRecover()
				err = mgr.Start(ctx)
				Expect(err).ToNot(HaveOccurred(), "failed to run manager")
			}()
		})
		AfterEach(func() {
			By("Stop the controller manager")
			cancel()
		})

		It("MCS agent will join and leave without MCSes", func() {
			By("Creating internalMemberCluster")
			imc := fleetv1alpha1.InternalMemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      memberClusterName,
					Namespace: memberClusterNamespace,
				},
				Spec: fleetv1alpha1.InternalMemberClusterSpec{
					State:                  fleetv1alpha1.ClusterStateJoin,
					HeartbeatPeriodSeconds: int32(heartbeatPeriod),
				},
			}
			Expect(hubClient.Create(ctx, &imc)).Should(Succeed())

			// TODO, need to removed once the fleet api is updated
			imc.Status = fleetv1alpha1.InternalMemberClusterStatus{
				Conditions:    []metav1.Condition{},
				Capacity:      corev1.ResourceList{},
				Allocatable:   corev1.ResourceList{},
				ResourceUsage: fleetv1alpha1.ResourceUsage{},
			}
			Expect(hubClient.Status().Update(ctx, &imc)).Should(Succeed())

			By("Checking internalMemberCluster status")
			var joinLastTransitionTime metav1.Time
			var heartbeatReceivedTime metav1.Time
			Eventually(func() string {
				if err := hubClient.Get(ctx, imcKey, &imc); err != nil {
					return err.Error()
				}
				want := []fleetv1alpha1.AgentStatus{
					{
						Type: fleetv1alpha1.MultiClusterServiceAgent,
						Conditions: []metav1.Condition{
							{
								Type:   string(fleetv1alpha1.AgentJoined),
								Status: metav1.ConditionTrue,
								Reason: conditionReasonJoined,
							},
						},
					},
				}
				if len(imc.Status.AgentStatus) != 1 || len(imc.Status.AgentStatus[0].Conditions) != 1 {
					return fmt.Sprintf("got empty agent status, want %+v", want)
				}
				joinLastTransitionTime = imc.Status.AgentStatus[0].Conditions[0].LastTransitionTime
				heartbeatReceivedTime = imc.Status.AgentStatus[0].LastReceivedHeartbeat
				return cmp.Diff(want, imc.Status.AgentStatus, options...)
			}, timeout, interval).Should(BeEmpty())

			// wait for some time to make sure we get the new heartbeat
			timer := time.NewTimer(2 * heartbeatPeriod * time.Second)
			<-timer.C
			Expect(hubClient.Get(ctx, imcKey, &imc)).Should(Succeed())
			want := []fleetv1alpha1.AgentStatus{
				{
					Type: fleetv1alpha1.MultiClusterServiceAgent,
					Conditions: []metav1.Condition{
						{
							Type:   string(fleetv1alpha1.AgentJoined),
							Status: metav1.ConditionTrue,
							Reason: conditionReasonJoined,
						},
					},
				},
			}
			Expect(cmp.Diff(want, imc.Status.AgentStatus, options...)).Should(BeEmpty())
			By("Checking LastTransitionTime of join condition, which should not change")
			Expect(imc.Status.AgentStatus[0].Conditions[0].LastTransitionTime.Equal(&joinLastTransitionTime)).Should(BeTrue())
			By("Checking heartbeat, which should be updated")
			Expect(imc.Status.AgentStatus[0].LastReceivedHeartbeat.Equal(&heartbeatReceivedTime)).Should(BeFalse())

			By("Creating serviceExports")
			svcExportsList := []fleetnetv1alpha1.ServiceExport{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-1",
						Namespace: namespaceList[0].Name,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-2",
						Namespace: namespaceList[1].Name,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-3",
						Namespace: namespaceList[0].Name,
					},
				},
			}
			for i := range svcExportsList {
				Expect(memberClient.Create(ctx, &svcExportsList[i])).Should(Succeed())
			}

			By("Updating internalMemberCluster spec as leave")
			Eventually(func() error {
				if err := hubClient.Get(ctx, imcKey, &imc); err != nil {
					return err
				}
				imc.Spec.State = fleetv1alpha1.ClusterStateLeave
				return hubClient.Update(ctx, &imc)
			}, timeout, interval).Should(Succeed())

			By("Checking internalMemberCluster status")
			Eventually(func() string {
				if err := hubClient.Get(ctx, imcKey, &imc); err != nil {
					return err.Error()
				}
				want := []fleetv1alpha1.AgentStatus{
					{
						Type: fleetv1alpha1.MultiClusterServiceAgent,
						Conditions: []metav1.Condition{
							{
								Type:   string(fleetv1alpha1.AgentJoined),
								Status: metav1.ConditionFalse,
								Reason: conditionReasonLeft,
							},
						},
					},
				}
				return cmp.Diff(want, imc.Status.AgentStatus, cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "ObservedGeneration"))
			}, timeout, interval).Should(BeEmpty())

			By("Checking MCSes and should have no change")
			list := &fleetnetv1alpha1.ServiceExportList{}
			Expect(memberClient.List(ctx, list)).Should(Succeed())
			Expect(len(list.Items) == len(svcExportsList)).Should(BeTrue())

			By("Deleting internalMemberCluster")
			Expect(hubClient.Delete(ctx, &imc)).Should(Succeed())

			By("Deleting serviceExports")
			for i := range svcExportsList {
				Expect(memberClient.Delete(ctx, &svcExportsList[i])).Should(Succeed())
			}
		})

		It("MCS agent will join and leave with MCSes", func() {
			By("Creating internalMemberCluster")
			imc := fleetv1alpha1.InternalMemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      memberClusterName,
					Namespace: memberClusterNamespace,
				},
				Spec: fleetv1alpha1.InternalMemberClusterSpec{
					State:                  fleetv1alpha1.ClusterStateJoin,
					HeartbeatPeriodSeconds: int32(heartbeatPeriod),
				},
			}
			Expect(hubClient.Create(ctx, &imc)).Should(Succeed())

			// TODO, need to removed once the fleet api is updated
			imc.Status = fleetv1alpha1.InternalMemberClusterStatus{
				Conditions:    []metav1.Condition{},
				Capacity:      corev1.ResourceList{},
				Allocatable:   corev1.ResourceList{},
				ResourceUsage: fleetv1alpha1.ResourceUsage{},
			}
			Expect(hubClient.Status().Update(ctx, &imc)).Should(Succeed())

			By("Checking internalMemberCluster status")
			var joinLastTransitionTime metav1.Time
			var heartbeatReceivedTime metav1.Time
			Eventually(func() string {
				if err := hubClient.Get(ctx, imcKey, &imc); err != nil {
					return err.Error()
				}
				want := []fleetv1alpha1.AgentStatus{
					{
						Type: fleetv1alpha1.MultiClusterServiceAgent,
						Conditions: []metav1.Condition{
							{
								Type:   string(fleetv1alpha1.AgentJoined),
								Status: metav1.ConditionTrue,
								Reason: conditionReasonJoined,
							},
						},
					},
				}
				if len(imc.Status.AgentStatus) != 1 || len(imc.Status.AgentStatus[0].Conditions) != 1 {
					return fmt.Sprintf("got empty agent status, want %+v", want)
				}
				joinLastTransitionTime = imc.Status.AgentStatus[0].Conditions[0].LastTransitionTime
				heartbeatReceivedTime = imc.Status.AgentStatus[0].LastReceivedHeartbeat
				return cmp.Diff(want, imc.Status.AgentStatus, options...)
			}, timeout, interval).Should(BeEmpty())

			// wait for some time to make sure we get the new heartbeat
			timer := time.NewTimer(2 * heartbeatPeriod * time.Second)
			<-timer.C
			Expect(hubClient.Get(ctx, imcKey, &imc)).Should(Succeed())
			want := []fleetv1alpha1.AgentStatus{
				{
					Type: fleetv1alpha1.MultiClusterServiceAgent,
					Conditions: []metav1.Condition{
						{
							Type:   string(fleetv1alpha1.AgentJoined),
							Status: metav1.ConditionTrue,
							Reason: conditionReasonJoined,
						},
					},
				},
			}
			Expect(cmp.Diff(want, imc.Status.AgentStatus, options...)).Should(BeEmpty())
			By("Checking LastTransitionTime of join condition, which should not change")
			Expect(imc.Status.AgentStatus[0].Conditions[0].LastTransitionTime.Equal(&joinLastTransitionTime)).Should(BeTrue())
			By("Checking heartbeat, which should be updated")
			Expect(imc.Status.AgentStatus[0].LastReceivedHeartbeat.Equal(&heartbeatReceivedTime)).Should(BeFalse())

			By("Creating MCSes")
			mcsList := []fleetnetv1alpha1.MultiClusterService{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "mcs-1",
						Namespace:  namespaceList[0].Name,
						Finalizers: []string{"test"},
					},
					Spec: fleetnetv1alpha1.MultiClusterServiceSpec{
						ServiceImport: fleetnetv1alpha1.ServiceImportRef{
							Name: "my-svc-1",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "mcs-2",
						Namespace: namespaceList[1].Name,
					},
					Spec: fleetnetv1alpha1.MultiClusterServiceSpec{
						ServiceImport: fleetnetv1alpha1.ServiceImportRef{
							Name: "my-svc-2",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "mcs-3",
						Namespace: namespaceList[0].Name,
					},
					Spec: fleetnetv1alpha1.MultiClusterServiceSpec{
						ServiceImport: fleetnetv1alpha1.ServiceImportRef{
							Name: "my-svc-3",
						},
					},
				},
			}
			for i := range mcsList {
				Expect(memberClient.Create(ctx, &mcsList[i])).Should(Succeed())
			}

			By("Updating internalMemberCluster spec as leave")
			Eventually(func() error {
				if err := hubClient.Get(ctx, imcKey, &imc); err != nil {
					return err
				}
				imc.Spec.State = fleetv1alpha1.ClusterStateLeave
				return hubClient.Update(ctx, &imc)
			}, timeout, interval).Should(Succeed())

			By("Checking internalMemberCluster status and should still mark as joined")
			Consistently(func() string {
				if err := hubClient.Get(ctx, imcKey, &imc); err != nil {
					return err.Error()
				}
				want := []fleetv1alpha1.AgentStatus{
					{
						Type: fleetv1alpha1.MultiClusterServiceAgent,
						Conditions: []metav1.Condition{
							{
								Type:   string(fleetv1alpha1.AgentJoined),
								Status: metav1.ConditionTrue,
								Reason: conditionReasonJoined,
							},
						},
					},
				}
				return cmp.Diff(want, imc.Status.AgentStatus, options...)
			}, duration, interval).Should(BeEmpty())

			By("Removing finalizer on MCS")
			Eventually(func() error {
				mcs := fleetnetv1alpha1.MultiClusterService{}
				key := types.NamespacedName{Namespace: mcsList[0].Namespace, Name: mcsList[0].Name}
				if err := memberClient.Get(ctx, key, &mcs); err != nil {
					return err
				}
				controllerutil.RemoveFinalizer(&mcs, "test")
				return memberClient.Update(ctx, &mcs)
			}, timeout, interval).Should(Succeed())

			By("Checking internalMemberCluster status")
			Eventually(func() string {
				if err := hubClient.Get(ctx, imcKey, &imc); err != nil {
					return err.Error()
				}
				want := []fleetv1alpha1.AgentStatus{
					{
						Type: fleetv1alpha1.MultiClusterServiceAgent,
						Conditions: []metav1.Condition{
							{
								Type:   string(fleetv1alpha1.AgentJoined),
								Status: metav1.ConditionFalse,
								Reason: conditionReasonLeft,
							},
						},
					},
				}
				return cmp.Diff(want, imc.Status.AgentStatus, cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "ObservedGeneration"))
			}, 15*time.Second, interval).Should(BeEmpty())

			By("Checking MCSes")
			list := &fleetnetv1alpha1.MultiClusterServiceList{}
			Expect(memberClient.List(ctx, list)).Should(Succeed())
			Expect(len(list.Items) == 0).Should(BeTrue())

			By("Deleting internalMemberCluster")
			Expect(hubClient.Delete(ctx, &imc)).Should(Succeed())
		})
	})

	Context("Test serviceExportImport agent type", func() {
		var (
			ctx    context.Context
			cancel context.CancelFunc
		)
		BeforeEach(func() {
			By("Starting the controller manager for serviceExportImport agent type")
			mgr, err := ctrl.NewManager(hubCfg, ctrl.Options{
				Scheme:             scheme.Scheme,
				MetricsBindAddress: "0",
				Logger:             klogr.NewWithOptions(klogr.WithFormat(klogr.FormatKlog)),
				Port:               4848,
			})
			Expect(err).NotTo(HaveOccurred())

			err = (&Reconciler{
				HubClient:    mgr.GetClient(),
				MemberClient: memberClient,
				AgentType:    fleetv1alpha1.ServiceExportImportAgent,
			}).SetupWithManager(mgr)
			Expect(err).ToNot(HaveOccurred())

			ctx, cancel = context.WithCancel(context.TODO())
			go func() {
				defer GinkgoRecover()
				err = mgr.Start(ctx)
				Expect(err).ToNot(HaveOccurred(), "failed to run manager")
			}()
		})
		AfterEach(func() {
			By("Stop the controller manager")
			cancel()
		})

		It("ServiceExportImport agent will join and leave without serviceExports", func() {
			By("Creating internalMemberCluster")
			imc := fleetv1alpha1.InternalMemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      memberClusterName,
					Namespace: memberClusterNamespace,
				},
				Spec: fleetv1alpha1.InternalMemberClusterSpec{
					State:                  fleetv1alpha1.ClusterStateJoin,
					HeartbeatPeriodSeconds: int32(heartbeatPeriod),
				},
			}
			Expect(hubClient.Create(ctx, &imc)).Should(Succeed())

			// TODO, need to removed once the fleet api is updated
			imc.Status = fleetv1alpha1.InternalMemberClusterStatus{
				Conditions:    []metav1.Condition{},
				Capacity:      corev1.ResourceList{},
				Allocatable:   corev1.ResourceList{},
				ResourceUsage: fleetv1alpha1.ResourceUsage{},
			}
			Expect(hubClient.Status().Update(ctx, &imc)).Should(Succeed())

			By("Checking internalMemberCluster status")
			var joinLastTransitionTime metav1.Time
			var heartbeatReceivedTime metav1.Time
			Eventually(func() string {
				if err := hubClient.Get(ctx, imcKey, &imc); err != nil {
					return err.Error()
				}
				want := []fleetv1alpha1.AgentStatus{
					{
						Type: fleetv1alpha1.ServiceExportImportAgent,
						Conditions: []metav1.Condition{
							{
								Type:   string(fleetv1alpha1.AgentJoined),
								Status: metav1.ConditionTrue,
								Reason: conditionReasonJoined,
							},
						},
					},
				}
				if len(imc.Status.AgentStatus) != 1 || len(imc.Status.AgentStatus[0].Conditions) != 1 {
					return fmt.Sprintf("got empty agent status, want %+v", want)
				}
				joinLastTransitionTime = imc.Status.AgentStatus[0].Conditions[0].LastTransitionTime
				heartbeatReceivedTime = imc.Status.AgentStatus[0].LastReceivedHeartbeat
				return cmp.Diff(want, imc.Status.AgentStatus, options...)
			}, timeout, interval).Should(BeEmpty())

			// wait for some time to make sure we get the new heartbeat
			timer := time.NewTimer(2 * heartbeatPeriod * time.Second)
			<-timer.C
			Expect(hubClient.Get(ctx, imcKey, &imc)).Should(Succeed())
			want := []fleetv1alpha1.AgentStatus{
				{
					Type: fleetv1alpha1.ServiceExportImportAgent,
					Conditions: []metav1.Condition{
						{
							Type:   string(fleetv1alpha1.AgentJoined),
							Status: metav1.ConditionTrue,
							Reason: conditionReasonJoined,
						},
					},
				},
			}
			Expect(cmp.Diff(want, imc.Status.AgentStatus, options...)).Should(BeEmpty())
			By("Checking LastTransitionTime of join condition, which should not change")
			Expect(imc.Status.AgentStatus[0].Conditions[0].LastTransitionTime.Equal(&joinLastTransitionTime)).Should(BeTrue())
			By("Checking heartbeat, which should be updated")
			Expect(imc.Status.AgentStatus[0].LastReceivedHeartbeat.Equal(&heartbeatReceivedTime)).Should(BeFalse())

			By("Creating MCSes")
			mcsList := []fleetnetv1alpha1.MultiClusterService{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "mcs-1",
						Namespace: namespaceList[0].Name,
					},
					Spec: fleetnetv1alpha1.MultiClusterServiceSpec{
						ServiceImport: fleetnetv1alpha1.ServiceImportRef{
							Name: "my-svc-1",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "mcs-2",
						Namespace: namespaceList[1].Name,
					},
					Spec: fleetnetv1alpha1.MultiClusterServiceSpec{
						ServiceImport: fleetnetv1alpha1.ServiceImportRef{
							Name: "my-svc-2",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "mcs-3",
						Namespace: namespaceList[0].Name,
					},
					Spec: fleetnetv1alpha1.MultiClusterServiceSpec{
						ServiceImport: fleetnetv1alpha1.ServiceImportRef{
							Name: "my-svc-3",
						},
					},
				},
			}
			for i := range mcsList {
				Expect(memberClient.Create(ctx, &mcsList[i])).Should(Succeed())
			}

			By("Updating internalMemberCluster spec as leave")
			Eventually(func() error {
				if err := hubClient.Get(ctx, imcKey, &imc); err != nil {
					return err
				}
				imc.Spec.State = fleetv1alpha1.ClusterStateLeave
				return hubClient.Update(ctx, &imc)
			}, timeout, interval).Should(Succeed())

			By("Checking internalMemberCluster status")
			Eventually(func() string {
				if err := hubClient.Get(ctx, imcKey, &imc); err != nil {
					return err.Error()
				}
				want := []fleetv1alpha1.AgentStatus{
					{
						Type: fleetv1alpha1.ServiceExportImportAgent,
						Conditions: []metav1.Condition{
							{
								Type:   string(fleetv1alpha1.AgentJoined),
								Status: metav1.ConditionFalse,
								Reason: conditionReasonLeft,
							},
						},
					},
				}
				return cmp.Diff(want, imc.Status.AgentStatus, cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "ObservedGeneration"))
			}, timeout, interval).Should(BeEmpty())

			By("Checking MCSes and should have no change")
			list := &fleetnetv1alpha1.MultiClusterServiceList{}
			Expect(memberClient.List(ctx, list)).Should(Succeed())
			Expect(len(list.Items) == len(mcsList)).Should(BeTrue())

			By("Deleting internalMemberCluster")
			Expect(hubClient.Delete(ctx, &imc)).Should(Succeed())

			By("Deleting mcs")
			for i := range mcsList {
				Expect(memberClient.Delete(ctx, &mcsList[i])).Should(Succeed())
			}
		})

		It("ServiceExportImport agent will join and leave with serviceExports", func() {
			By("Creating internalMemberCluster")
			imc := fleetv1alpha1.InternalMemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      memberClusterName,
					Namespace: memberClusterNamespace,
				},
				Spec: fleetv1alpha1.InternalMemberClusterSpec{
					State:                  fleetv1alpha1.ClusterStateJoin,
					HeartbeatPeriodSeconds: int32(heartbeatPeriod),
				},
			}
			Expect(hubClient.Create(ctx, &imc)).Should(Succeed())

			// TODO, need to removed once the fleet api is updated
			imc.Status = fleetv1alpha1.InternalMemberClusterStatus{
				Conditions:    []metav1.Condition{},
				Capacity:      corev1.ResourceList{},
				Allocatable:   corev1.ResourceList{},
				ResourceUsage: fleetv1alpha1.ResourceUsage{},
			}
			Expect(hubClient.Status().Update(ctx, &imc)).Should(Succeed())

			By("Checking internalMemberCluster status")
			var joinLastTransitionTime metav1.Time
			var heartbeatReceivedTime metav1.Time
			Eventually(func() string {
				if err := hubClient.Get(ctx, imcKey, &imc); err != nil {
					return err.Error()
				}
				want := []fleetv1alpha1.AgentStatus{
					{
						Type: fleetv1alpha1.ServiceExportImportAgent,
						Conditions: []metav1.Condition{
							{
								Type:   string(fleetv1alpha1.AgentJoined),
								Status: metav1.ConditionTrue,
								Reason: conditionReasonJoined,
							},
						},
					},
				}
				if len(imc.Status.AgentStatus) != 1 || len(imc.Status.AgentStatus[0].Conditions) != 1 {
					return fmt.Sprintf("got empty agent status, want %+v", want)
				}
				joinLastTransitionTime = imc.Status.AgentStatus[0].Conditions[0].LastTransitionTime
				heartbeatReceivedTime = imc.Status.AgentStatus[0].LastReceivedHeartbeat
				return cmp.Diff(want, imc.Status.AgentStatus, options...)
			}, timeout, interval).Should(BeEmpty())

			// wait for some time to make sure we get the new heartbeat
			timer := time.NewTimer(2 * heartbeatPeriod * time.Second)
			<-timer.C
			Expect(hubClient.Get(ctx, imcKey, &imc)).Should(Succeed())
			want := []fleetv1alpha1.AgentStatus{
				{
					Type: fleetv1alpha1.ServiceExportImportAgent,
					Conditions: []metav1.Condition{
						{
							Type:   string(fleetv1alpha1.AgentJoined),
							Status: metav1.ConditionTrue,
							Reason: conditionReasonJoined,
						},
					},
				},
			}
			Expect(cmp.Diff(want, imc.Status.AgentStatus, options...)).Should(BeEmpty())
			By("Checking LastTransitionTime of join condition, which should not change")
			Expect(imc.Status.AgentStatus[0].Conditions[0].LastTransitionTime.Equal(&joinLastTransitionTime)).Should(BeTrue())
			By("Checking heartbeat, which should be updated")
			Expect(imc.Status.AgentStatus[0].LastReceivedHeartbeat.Equal(&heartbeatReceivedTime)).Should(BeFalse())

			By("Creating MCSes")
			mcsList := []fleetnetv1alpha1.MultiClusterService{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "mcs-1",
						Namespace: namespaceList[0].Name,
					},
					Spec: fleetnetv1alpha1.MultiClusterServiceSpec{
						ServiceImport: fleetnetv1alpha1.ServiceImportRef{
							Name: "my-svc-1",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "mcs-2",
						Namespace: namespaceList[1].Name,
					},
					Spec: fleetnetv1alpha1.MultiClusterServiceSpec{
						ServiceImport: fleetnetv1alpha1.ServiceImportRef{
							Name: "my-svc-2", // import does not exist
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "mcs-3",
						Namespace: namespaceList[0].Name,
						// mcs does not have serviceImport label
					},
					Spec: fleetnetv1alpha1.MultiClusterServiceSpec{
						ServiceImport: fleetnetv1alpha1.ServiceImportRef{
							Name: "my-svc-3",
						},
					},
				},
			}
			for i := range mcsList {
				Expect(memberClient.Create(ctx, &mcsList[i])).Should(Succeed())
			}

			By("Creating serviceExports")
			svcExportsList := []fleetnetv1alpha1.ServiceExport{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "svc-1",
						Namespace:  namespaceList[0].Name,
						Finalizers: []string{"test"},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-2",
						Namespace: namespaceList[1].Name,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-3",
						Namespace: namespaceList[0].Name,
					},
				},
			}
			for i := range svcExportsList {
				Expect(memberClient.Create(ctx, &svcExportsList[i])).Should(Succeed())
			}

			By("Updating internalMemberCluster spec as leave")
			Eventually(func() error {
				if err := hubClient.Get(ctx, imcKey, &imc); err != nil {
					return err
				}
				imc.Spec.State = fleetv1alpha1.ClusterStateLeave
				return hubClient.Update(ctx, &imc)
			}, timeout, interval).Should(Succeed())

			By("Checking internalMemberCluster status and should still mark as joined")
			Consistently(func() string {
				if err := hubClient.Get(ctx, imcKey, &imc); err != nil {
					return err.Error()
				}
				want := []fleetv1alpha1.AgentStatus{
					{
						Type: fleetv1alpha1.ServiceExportImportAgent,
						Conditions: []metav1.Condition{
							{
								Type:   string(fleetv1alpha1.AgentJoined),
								Status: metav1.ConditionTrue,
								Reason: conditionReasonJoined,
							},
						},
					},
				}
				return cmp.Diff(want, imc.Status.AgentStatus, options...)
			}, duration, interval).Should(BeEmpty())

			By("Removing finalizer on serviceExport")
			Eventually(func() error {
				svcExport := fleetnetv1alpha1.ServiceExport{}
				key := types.NamespacedName{Namespace: svcExportsList[0].Namespace, Name: svcExportsList[0].Name}
				if err := memberClient.Get(ctx, key, &svcExport); err != nil {
					return err
				}
				controllerutil.RemoveFinalizer(&svcExport, "test")
				return memberClient.Update(ctx, &svcExport)
			}, timeout, interval).Should(Succeed())

			By("Checking internalMemberCluster status")
			Eventually(func() string {
				if err := hubClient.Get(ctx, imcKey, &imc); err != nil {
					return err.Error()
				}
				want := []fleetv1alpha1.AgentStatus{
					{
						Type: fleetv1alpha1.ServiceExportImportAgent,
						Conditions: []metav1.Condition{
							{
								Type:   string(fleetv1alpha1.AgentJoined),
								Status: metav1.ConditionFalse,
								Reason: conditionReasonLeft,
							},
						},
					},
				}
				return cmp.Diff(want, imc.Status.AgentStatus, cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "ObservedGeneration"))
			}, timeout, interval).Should(BeEmpty())

			By("Checking ServiceExports")
			list := &fleetnetv1alpha1.ServiceExportList{}
			Expect(memberClient.List(ctx, list)).Should(Succeed())
			Expect(len(list.Items) == 0).Should(BeTrue())

			By("Deleting internalMemberCluster")
			Expect(hubClient.Delete(ctx, &imc)).Should(Succeed())
		})
	})
})
