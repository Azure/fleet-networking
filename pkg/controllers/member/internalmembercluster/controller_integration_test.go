package internalmembercluster

import (
	"context"
	"fmt"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2/klogr"
	ctrl "sigs.k8s.io/controller-runtime"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
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

		It("MCS agent will join and leave without serviceImports", func() {
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

			By("Deleting internalMemberCluster")
			Expect(hubClient.Delete(ctx, &imc)).Should(Succeed())
		})

		It("MCS agent will join and leave with serviceImports", func() {
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
						Name:      "mcs-1",
						Namespace: namespaceList[0].Name,
						Labels: map[string]string{
							objectmeta.MultiClusterServiceLabelServiceImport: "my-svc-1",
						},
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
						Labels: map[string]string{
							objectmeta.MultiClusterServiceLabelServiceImport: "my-svc-2",
						},
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
			By("Creating serviceImports")
			serviceImportList := []fleetnetv1alpha1.ServiceImport{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-svc-1",
						Namespace: namespaceList[0].Name,
					},
				},
			}
			for i := range serviceImportList {
				Expect(memberClient.Create(ctx, &serviceImportList[i])).Should(Succeed())
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

			By("Checking serviceImports")
			list := &fleetnetv1alpha1.ServiceImportList{}
			Expect(memberClient.List(ctx, list)).Should(Succeed())
			Expect(len(list.Items) == 0).Should(BeTrue())

			By("Deleting internalMemberCluster")
			Expect(hubClient.Delete(ctx, &imc)).Should(Succeed())

			By("Deleting mcs")
			for i := range mcsList {
				Expect(memberClient.Delete(ctx, &mcsList[i])).Should(Succeed())
			}
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

		It("ServiceExportImport agent will join and leave", func() {
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
						Labels: map[string]string{
							objectmeta.MultiClusterServiceLabelServiceImport: "my-svc-1",
						},
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
						Labels: map[string]string{
							objectmeta.MultiClusterServiceLabelServiceImport: "my-svc-2",
						},
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
			By("Creating serviceImports")
			serviceImportList := []fleetnetv1alpha1.ServiceImport{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-svc-1",
						Namespace: namespaceList[0].Name,
					},
				},
			}
			for i := range serviceImportList {
				Expect(memberClient.Create(ctx, &serviceImportList[i])).Should(Succeed())
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

			By("Checking serviceImports and should have no change")
			list := &fleetnetv1alpha1.ServiceImportList{}
			Expect(memberClient.List(ctx, list)).Should(Succeed())
			Expect(len(list.Items) == len(serviceImportList)).Should(BeTrue())

			By("Deleting internalMemberCluster")
			Expect(hubClient.Delete(ctx, &imc)).Should(Succeed())

			By("Deleting mcs")
			for i := range mcsList {
				Expect(memberClient.Delete(ctx, &mcsList[i])).Should(Succeed())
			}

			By("Deleting serviceImports")
			for i := range serviceImportList {
				Expect(memberClient.Delete(ctx, &serviceImportList[i])).Should(Succeed())
			}
		})
	})
})
