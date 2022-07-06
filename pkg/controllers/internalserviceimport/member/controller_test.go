/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package member

import (
	"context"
	"errors"
	"log"
	"os"
	"reflect"
	"testing"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/controllers/internalserviceimport/consts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// TestMain bootstraps the test environment.
func TestMain(m *testing.M) {
	// Add custom APIs to the runtime scheme
	err := fleetnetv1alpha1.AddToScheme(scheme.Scheme)
	if err != nil {
		log.Fatalf("failed to add custom APIs to the runtime scheme: %v", err)
	}

	os.Exit(m.Run())
}

func TestReconcile(t *testing.T) {
	cases := []struct {
		desc                      string
		serviceImport             *fleetnetv1alpha1.ServiceImport
		expectedResult            reconcile.Result
		expectedErr               error
		expectedInternalSvcImport *fleetnetv1alpha1.InternalServiceImport
	}{
		{
			desc:                      "service import is not found",
			serviceImport:             &fleetnetv1alpha1.ServiceImport{},
			expectedResult:            reconcile.Result{},
			expectedErr:               nil,
			expectedInternalSvcImport: nil,
		},
		{
			desc: "create or update internalservice import successfully",
			serviceImport: &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "serviceimport",
					Namespace: "serviceimportnamespace",
				},
			},
			expectedResult: reconcile.Result{},
			expectedErr:    nil,
			expectedInternalSvcImport: &fleetnetv1alpha1.InternalServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "serviceimport",
					Namespace: "member-x-in-hub-to-be-changed",
					Labels: map[string]string{
						consts.LabelTargetNamespace:    "serviceimportnamespace",
						consts.LabelExposedClusterName: "clustername-to-be-changed",
					},
				},
			},
		},
	}
	for _, tc := range cases {
		fakeMemberClient := fakeclient.NewClientBuilder().
			WithScheme(scheme.Scheme).
			WithObjects(client.Object(tc.serviceImport)).
			Build()
		if len(tc.serviceImport.Name) == 0 {
			fakeMemberClient = fakeclient.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects().
				Build()
		}

		fakeHubClient := fakeclient.NewClientBuilder().Build()
		reconciler := Reconciler{
			memberClient: fakeMemberClient,
			hubClient:    fakeHubClient,
			Scheme:       scheme.Scheme,
		}

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      tc.serviceImport.Name,
				Namespace: tc.serviceImport.Namespace,
			},
		}
		actualResult, actualErr := reconciler.Reconcile(context.TODO(), req)
		if !reflect.DeepEqual(actualResult, tc.expectedResult) {
			t.Errorf("Expected result %v, got %v", tc.expectedResult, actualResult)
		}
		if !errors.Is(actualErr, tc.expectedErr) {
			t.Errorf("Expected result error %v, got %v", tc.expectedErr, actualErr)
		}

		if len(tc.serviceImport.Name) == 0 {
			continue
		}

		// check labels are correctly set
		obtainedInternalSvcImport := &fleetnetv1alpha1.InternalServiceImport{}
		namespacedName := types.NamespacedName{Namespace: tc.expectedInternalSvcImport.Namespace, Name: tc.expectedInternalSvcImport.Name}
		if err := reconciler.hubClient.Get(context.TODO(), namespacedName, obtainedInternalSvcImport); err != nil {
			t.Errorf("Expected no error when getting internal service import, got %v", err)
		}

		for _, labelKey := range []string{consts.LabelExposedClusterName, consts.LabelTargetNamespace} {
			expectedLabelValue := tc.expectedInternalSvcImport.Labels[labelKey]
			actualLabelValue := obtainedInternalSvcImport.Labels[labelKey]
			if expectedLabelValue != actualLabelValue {
				t.Errorf("Expected label value of key %s is %s, got %s", labelKey, expectedLabelValue, actualLabelValue)
			}
		}
	}
}
