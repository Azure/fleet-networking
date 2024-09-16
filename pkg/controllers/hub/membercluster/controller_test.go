package membercluster

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

const (
	testMemberClusterName = "test-mc"
	forceDeleteWaitTime   = 15 * time.Minute
)

var deletionTimeStamp = time.Now()

func TestReconcile(t *testing.T) {
	testCases := []struct {
		name              string
		memberClusterName string
		memberCluster     clusterv1beta1.MemberCluster
		wantResult        ctrl.Result
		wantErr           error
	}{
		{
			name:              "memberCluster is not found",
			memberClusterName: testMemberClusterName,
			wantResult:        ctrl.Result{},
			wantErr:           nil,
		},
		{
			name:              "memberCluster deletionTimestamp is nil",
			memberClusterName: testMemberClusterName,
			wantResult:        ctrl.Result{},
			wantErr:           nil,
		},
		{
			name:              "time since memberCluster deletionTimestamp is less than force delete wait time",
			memberClusterName: testMemberClusterName,
			memberCluster: clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-mc",
					DeletionTimestamp: &metav1.Time{Time: deletionTimeStamp},
					Finalizers:        []string{"test-member-cluster-cleanup-finalizer"},
				},
			},
			wantResult: ctrl.Result{RequeueAfter: forceDeleteWaitTime - time.Since(deletionTimeStamp)},
			wantErr:    nil,
		},
		{
			name:              "time since memberCluster deletionTimestamp is greater than force delete wait time",
			memberClusterName: testMemberClusterName,
			memberCluster: clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-mc",
					// To set deletionTimeStamp to some time 20 minutes before.
					DeletionTimestamp: &metav1.Time{Time: deletionTimeStamp.Add(-20 * time.Minute)},
					Finalizers:        []string{"test-member-cluster-cleanup-finalizer"},
				},
			},
			wantResult: ctrl.Result{},
			wantErr:    nil,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(testScheme(t)).
				WithObjects(&tc.memberCluster).
				Build()
			r := Reconciler{
				Client:              fakeClient,
				ForceDeleteWaitTime: forceDeleteWaitTime,
			}

			gotResult, gotErr := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: tc.memberClusterName}})
			if !cmp.Equal(gotErr, tc.wantErr) {
				t.Errorf("Reconcile() error = %+v, want %+v", gotErr, tc.wantErr)
			}
			// Want RequeueAfter is calculated when we expect it to be not zero. Got RequeueAfter from reconcile
			// will always be different from Want RequeueAfter because it calculated when the testCase is built.
			if got, want := gotResult.RequeueAfter == 0, tc.wantResult.RequeueAfter == 0; got != want {
				t.Errorf("Reconcile() RequeueAfter is zero = %v, want %v", got, want)
			}
		})
	}
}

func testScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	if err := clusterv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}
	if err := fleetnetv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}
	return scheme
}
