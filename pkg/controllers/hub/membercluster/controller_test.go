package membercluster

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

const (
	testMemberClusterName   = "test-mc"
	testEndpointSliceImport = "test-esi"
	forceDeleteWaitTime     = 15 * time.Minute

	errorMsg = "fake error for testing"
)

var deletionTimeStamp = time.Now()

func TestReconcile(t *testing.T) {
	testCases := []struct {
		name              string
		memberClusterName string
		memberCluster     clusterv1beta1.MemberCluster
		shouldGetErr      bool
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
			name:              "failed to get memberCluster",
			memberClusterName: testMemberClusterName,
			shouldGetErr:      true,
			wantResult:        ctrl.Result{},
			wantErr:           errors.New(errorMsg),
		},
		{
			name:              "memberCluster deletionTimestamp is nil",
			memberClusterName: testMemberClusterName,
			memberCluster: clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-mc",
				},
			},
			wantResult: ctrl.Result{},
			wantErr:    nil,
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
			errorFakeClient := errorReturningFakeClient{
				Client: fake.NewClientBuilder().
					WithScheme(testScheme(t)).
					WithObjects(&tc.memberCluster).
					Build(),
				shouldGetError: tc.shouldGetErr,
			}

			r := Reconciler{
				Client:              errorFakeClient,
				ForceDeleteWaitTime: forceDeleteWaitTime,
			}

			gotResult, gotErr := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: tc.memberClusterName}})
			if gotErr.Error() != tc.wantErr.Error() {
				t.Errorf("Reconcile() error = %+v, want = %+v", gotErr, tc.wantErr)
			}
			// Want RequeueAfter is calculated when we expect it to be not zero. Got RequeueAfter from reconcile
			// will always be different from Want RequeueAfter because it calculated when the testCase is built.
			if got, want := gotResult.RequeueAfter == 0, tc.wantResult.RequeueAfter == 0; got != want {
				t.Errorf("Reconcile() RequeueAfter is zero = %v, want %v", got, want)
			}
		})
	}
}

func TestRemoveFinalizer(t *testing.T) {
	testCases := []struct {
		name                string
		memberCluster       clusterv1beta1.MemberCluster
		endPointSliceImport fleetnetv1alpha1.EndpointSliceImport
		shouldGetErr        bool
		shouldUpdateErr     bool
		wantResult          ctrl.Result
		wantErr             error
	}{
		// the happy path is handled as part of IT.
		{
			name: "failed to list endpointSliceImports",
			memberCluster: clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: memberClusterName,
				},
			},
			shouldGetErr: true,
			wantResult:   ctrl.Result{},
			wantErr:      errors.New(errorMsg),
		},
		{
			name: "failed to update endpointSliceImport",
			memberCluster: clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: memberClusterName,
				},
			},
			endPointSliceImport: *buildEndpointSliceImport(testEndpointSliceImport),
			shouldGetErr:        false,
			shouldUpdateErr:     true,
			wantResult:          ctrl.Result{},
			wantErr:             errors.New(errorMsg),
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			errorFakeClient := errorReturningFakeClient{
				Client: fake.NewClientBuilder().
					WithScheme(testScheme(t)).
					WithObjects(&tc.endPointSliceImport).
					Build(),
				shouldGetError:    tc.shouldGetErr,
				shouldUpdateError: tc.shouldUpdateErr,
			}
			r := Reconciler{
				Client: errorFakeClient,
			}
			gotResult, gotErr := r.removeFinalizer(context.Background(), tc.memberCluster)
			if gotErr.Error() != tc.wantErr.Error() {
				t.Errorf("removeFinalizer() error = %+v, want = %+v", gotErr, tc.wantErr)
			}
			if !cmp.Equal(gotResult, tc.wantResult) {
				t.Errorf("removeFinalizer() result = %v, want %v", gotResult, tc.wantResult)
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

type errorReturningFakeClient struct {
	client.Client
	shouldGetError    bool
	shouldUpdateError bool
}

func (fc errorReturningFakeClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if fc.shouldGetError {
		return errors.New(errorMsg)
	}
	return fc.Client.Get(ctx, key, obj, opts...)
}

func (fc errorReturningFakeClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if fc.shouldGetError {
		return errors.New(errorMsg)
	}
	return fc.Client.List(ctx, list, opts...)
}

func (fc errorReturningFakeClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	if fc.shouldUpdateError {
		return errors.New(errorMsg)
	}
	return fc.Client.Update(ctx, obj, opts...)
}
