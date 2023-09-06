/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1beta1

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

const (
	memberClusterNamespace = "fleet-system-member-cluster-a"
	memberClusterName      = "member-cluster-a"
)

var (
	ignoreConditionLTTAndMessageFields          = cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "Message")
	ignoreAgentStatusLastReceivedHeartbeatField = cmpopts.IgnoreFields(clusterv1beta1.AgentStatus{}, "LastReceivedHeartbeat")
)

// TestUpdateAgentStatus tests the updateAgentStatus method.
func TestUpdateAgentStatus(t *testing.T) {
	agentType := clusterv1beta1.AgentType("DummyAgent")

	testCases := []struct {
		name                  string
		internalMemberCluster *clusterv1beta1.InternalMemberCluster
		wantAgentStatus       []clusterv1beta1.AgentStatus
	}{
		{
			name: "member cluster is active, no status of the agent type reported before",
			internalMemberCluster: &clusterv1beta1.InternalMemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:       memberClusterName,
					Namespace:  memberClusterNamespace,
					Generation: 1,
				},
				Spec: clusterv1beta1.InternalMemberClusterSpec{
					State: clusterv1beta1.ClusterStateJoin,
				},
			},
			wantAgentStatus: []clusterv1beta1.AgentStatus{
				{
					Type: agentType,
					Conditions: []metav1.Condition{
						{
							Type:               string(clusterv1beta1.AgentJoined),
							Status:             metav1.ConditionTrue,
							Reason:             conditionReasonJoined,
							ObservedGeneration: 1,
						},
					},
				},
			},
		},
		{
			name: "member cluster is active, status of the agent type reported before",
			internalMemberCluster: &clusterv1beta1.InternalMemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:       memberClusterName,
					Namespace:  memberClusterNamespace,
					Generation: 1,
				},
				Spec: clusterv1beta1.InternalMemberClusterSpec{
					State: clusterv1beta1.ClusterStateJoin,
				},
				Status: clusterv1beta1.InternalMemberClusterStatus{
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type: agentType,
							Conditions: []metav1.Condition{
								{
									Type:               string(clusterv1beta1.AgentJoined),
									Status:             metav1.ConditionUnknown,
									ObservedGeneration: 1,
								},
							},
						},
					},
				},
			},
			wantAgentStatus: []clusterv1beta1.AgentStatus{
				{
					Type: agentType,
					Conditions: []metav1.Condition{
						{
							Type:               string(clusterv1beta1.AgentJoined),
							Status:             metav1.ConditionTrue,
							Reason:             conditionReasonJoined,
							ObservedGeneration: 1,
						},
					},
				},
			},
		},
		{
			name: "member cluster has left the fleet, status of the agent type reported before",
			internalMemberCluster: &clusterv1beta1.InternalMemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:       memberClusterName,
					Namespace:  memberClusterNamespace,
					Generation: 2,
				},
				Spec: clusterv1beta1.InternalMemberClusterSpec{
					State: clusterv1beta1.ClusterStateLeave,
				},
				Status: clusterv1beta1.InternalMemberClusterStatus{
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type: agentType,
							Conditions: []metav1.Condition{
								{
									Type:               string(clusterv1beta1.AgentJoined),
									Status:             metav1.ConditionTrue,
									Reason:             conditionReasonJoined,
									ObservedGeneration: 1,
								},
							},
							LastReceivedHeartbeat: metav1.NewTime(time.Now()),
						},
					},
				},
			},
			wantAgentStatus: []clusterv1beta1.AgentStatus{
				{
					Type: agentType,
					Conditions: []metav1.Condition{
						{
							Type:               string(clusterv1beta1.AgentJoined),
							Status:             metav1.ConditionFalse,
							Reason:             conditionReasonLeft,
							ObservedGeneration: 2,
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeHubClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.internalMemberCluster).
				Build()
			fakeMemberClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
			reconciler := &Reconciler{
				MemberClient: fakeMemberClient,
				HubClient:    fakeHubClient,
				AgentType:    agentType,
			}

			ctx := context.Background()
			beforeTimestamp := metav1.NewTime(time.Now().Add(-time.Minute))
			if err := reconciler.updateAgentStatus(ctx, tc.internalMemberCluster); err != nil {
				t.Fatalf("updateAgentStatus() = %v, want no err", err)
			}

			internalMemberCluster := &clusterv1beta1.InternalMemberCluster{}
			if err := fakeHubClient.Get(ctx, types.NamespacedName{Namespace: memberClusterNamespace, Name: memberClusterName}, internalMemberCluster); err != nil {
				t.Fatalf("Get() internalMemberCluster = %v, want no error", err)
			}

			// Check if the agent status has been updated as expected.
			if diff := cmp.Diff(
				internalMemberCluster.Status.AgentStatus, tc.wantAgentStatus,
				ignoreConditionLTTAndMessageFields,
				ignoreAgentStatusLastReceivedHeartbeatField,
			); diff != "" {
				t.Errorf("agentStatus diff (-got, +want): %s", diff)
			}

			if tc.internalMemberCluster.Spec.State != clusterv1beta1.ClusterStateLeave {
				// Check if a new heartbeat has been sent.
				heartbeatTimestamp := internalMemberCluster.Status.AgentStatus[0].LastReceivedHeartbeat
				if !heartbeatTimestamp.After(beforeTimestamp.Time) {
					t.Errorf("heartbeatTimestamp = %v, want after %v", heartbeatTimestamp, beforeTimestamp)
				}
			}
		})
	}
}

// TestCleanupMCSRelatedResources tests the cleanupMCSRelatedResources method.
func TestCleanupMCSRelatedResources(t *testing.T) {
	multiClusterSvcs := []fleetnetv1alpha1.MultiClusterService{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      mcsName1,
				Namespace: workNamespaceName,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      mcsName2,
				Namespace: workNamespaceName,
			},
		},
	}

	fakeHubClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
	fakeMemberClientBuilder := fake.NewClientBuilder().WithScheme(scheme.Scheme)
	for idx := range multiClusterSvcs {
		multiClusterSvc := multiClusterSvcs[idx]
		fakeMemberClientBuilder.WithObjects(&multiClusterSvc)
	}
	fakeMemberClient := fakeMemberClientBuilder.Build()
	reconciler := &Reconciler{
		MemberClient: fakeMemberClient,
		HubClient:    fakeHubClient,
		AgentType:    mcsAgentType,
	}

	ctx := context.Background()
	if err := reconciler.cleanupMCSRelatedResources(ctx); err != nil {
		t.Fatalf("cleanupMCSRelatedResources() = %v, want no error", err)
	}

	for idx := range multiClusterSvcs {
		multiClusterSvc := multiClusterSvcs[idx]
		key := types.NamespacedName{Namespace: multiClusterSvc.GetNamespace(), Name: multiClusterSvc.GetName()}
		if err := fakeMemberClient.Get(ctx, key, &fleetnetv1alpha1.MultiClusterService{}); !errors.IsNotFound(err) {
			t.Errorf("multiClusterService %s still exists", key)
		}
	}
}

// TestCleanupServiceExportRelatedResources tests the cleanupServiceExportRelatedResources method.
func TestCleanupServiceExportRelatedResources(t *testing.T) {
	svcExports := []fleetnetv1alpha1.ServiceExport{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svcExportName1,
				Namespace: workNamespaceName,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svcExportName2,
				Namespace: workNamespaceName,
			},
		},
	}

	fakeHubClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
	fakeMemberClientBuilder := fake.NewClientBuilder().WithScheme(scheme.Scheme)
	for idx := range svcExports {
		svcExport := svcExports[idx]
		fakeMemberClientBuilder.WithObjects(&svcExport)
	}
	fakeMemberClient := fakeMemberClientBuilder.Build()
	reconciler := &Reconciler{
		MemberClient: fakeMemberClient,
		HubClient:    fakeHubClient,
		AgentType:    mcsAgentType,
	}

	ctx := context.Background()
	if err := reconciler.cleanupServiceExportRelatedResources(ctx); err != nil {
		t.Fatalf("cleanupServiceExportRelatedResources() = %v, want no error", err)
	}

	for idx := range svcExports {
		svcExport := svcExports[idx]
		key := types.NamespacedName{Namespace: svcExport.GetNamespace(), Name: svcExport.GetName()}
		if err := fakeMemberClient.Get(ctx, key, &fleetnetv1alpha1.ServiceExport{}); !errors.IsNotFound(err) {
			t.Errorf("serviceExport %s still exists", key)
		}
	}
}
