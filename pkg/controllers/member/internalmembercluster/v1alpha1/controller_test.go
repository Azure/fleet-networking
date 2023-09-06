/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package internalmembercluster

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
)

func joinedCondition() metav1.Condition {
	return metav1.Condition{
		Type:               string(fleetv1alpha1.AgentJoined),
		Status:             metav1.ConditionTrue,
		Reason:             conditionReasonJoined,
		ObservedGeneration: 1,
	}
}
func leftCondition() metav1.Condition {
	return metav1.Condition{
		Type:               string(fleetv1alpha1.AgentJoined),
		Status:             metav1.ConditionFalse,
		Reason:             conditionReasonLeft,
		ObservedGeneration: 1,
	}
}

func TestSetAgentStatus(t *testing.T) {
	// tests expect there will be only one agent type in the agent status.
	now := metav1.NewTime(time.Now())
	testCases := []struct {
		name                          string
		status                        []fleetv1alpha1.AgentStatus
		newStatus                     fleetv1alpha1.AgentStatus
		want                          []fleetv1alpha1.AgentStatus
		wantLastTransitionTimeChanged bool
	}{
		{
			name:   "agent status is empty",
			status: []fleetv1alpha1.AgentStatus{},
			newStatus: fleetv1alpha1.AgentStatus{
				Type: fleetv1alpha1.MultiClusterServiceAgent,
				Conditions: []metav1.Condition{
					joinedCondition(),
				},
				LastReceivedHeartbeat: now,
			},
			want: []fleetv1alpha1.AgentStatus{
				{
					Type: fleetv1alpha1.MultiClusterServiceAgent,
					Conditions: []metav1.Condition{
						joinedCondition(),
					},
					LastReceivedHeartbeat: now,
				},
			},
			wantLastTransitionTimeChanged: true,
		},
		{
			name: "existing agent type and condition status is not changed",
			status: []fleetv1alpha1.AgentStatus{
				{
					Type: fleetv1alpha1.ServiceExportImportAgent,
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1alpha1.AgentJoined),
							Status:             metav1.ConditionTrue,
							Reason:             conditionReasonJoined,
							ObservedGeneration: 1,
							LastTransitionTime: now,
						},
					},
					LastReceivedHeartbeat: now,
				},
			},
			newStatus: fleetv1alpha1.AgentStatus{
				Type: fleetv1alpha1.ServiceExportImportAgent,
				Conditions: []metav1.Condition{
					joinedCondition(),
				},
				LastReceivedHeartbeat: now,
			},
			want: []fleetv1alpha1.AgentStatus{
				{
					Type: fleetv1alpha1.ServiceExportImportAgent,
					Conditions: []metav1.Condition{
						joinedCondition(),
					},
					LastReceivedHeartbeat: now,
				},
			},
			wantLastTransitionTimeChanged: false,
		},
		{
			name: "existing agent type and condition status is changed",
			status: []fleetv1alpha1.AgentStatus{
				{
					Type: fleetv1alpha1.ServiceExportImportAgent,
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1alpha1.AgentJoined),
							Status:             metav1.ConditionTrue,
							Reason:             conditionReasonJoined,
							ObservedGeneration: 1,
						},
					},
					LastReceivedHeartbeat: now,
				},
			},
			newStatus: fleetv1alpha1.AgentStatus{
				Type: fleetv1alpha1.ServiceExportImportAgent,
				Conditions: []metav1.Condition{
					leftCondition(),
				},
				LastReceivedHeartbeat: now,
			},
			want: []fleetv1alpha1.AgentStatus{
				{
					Type: fleetv1alpha1.ServiceExportImportAgent,
					Conditions: []metav1.Condition{
						leftCondition(),
					},
					LastReceivedHeartbeat: now,
				},
			},
			wantLastTransitionTimeChanged: true,
		},
	}
	options := []cmp.Option{
		cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime"),
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			setAgentStatus(&tc.status, tc.newStatus)
			if diff := cmp.Diff(tc.want, tc.status, options...); diff != "" {
				t.Errorf("setAgentStatus Get() mismatch (-want, +got):\n%s", diff)
			}
			for _, a := range tc.status {
				for _, c := range a.Conditions {
					if c.LastTransitionTime.IsZero() {
						t.Errorf("%v LastTransitionTime got zero, want not zero", c)
					}
					if tc.wantLastTransitionTimeChanged && c.LastTransitionTime.Equal(&now) {
						t.Errorf("LastTransitionTime got %v, want not equal %v", c.LastTransitionTime, now)
					}
					if !tc.wantLastTransitionTimeChanged && !c.LastTransitionTime.Equal(&now) {
						t.Errorf("LastTransitionTime got %v, want equal %v", c.LastTransitionTime, now)
					}
				}
			}
		})
	}
}
