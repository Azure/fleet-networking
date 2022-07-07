/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package hub

import (
	"context"
	"errors"
	"log"
	"os"
	"reflect"
	"testing"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/controllers/internalserviceimport/consts"
	"go.goms.io/fleet-networking/pkg/controllers/internalserviceimport/utils"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
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
	if err := fleetnetv1alpha1.AddToScheme(scheme.Scheme); err != nil {
		log.Fatalf("failed to add custom APIs to the runtime scheme: %v", err)
	}

	os.Exit(m.Run())
}

func TestReconcile(t *testing.T) {
	cases := []struct {
		desc                  string
		internalSvcImport     *fleetnetv1alpha1.InternalServiceImport
		expectedResult        reconcile.Result
		expectedErr           error
		expectedServiceImport *fleetnetv1alpha1.ServiceImport
	}{
		{
			desc:                  "internal service import is not found",
			internalSvcImport:     &fleetnetv1alpha1.InternalServiceImport{},
			expectedResult:        reconcile.Result{},
			expectedErr:           nil,
			expectedServiceImport: nil,
		},
		{
			desc: "update service import from internal service import successfully",
			internalSvcImport: &fleetnetv1alpha1.InternalServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "serviceimport",
					Namespace: "serviceimportnamespace",
				},
				Spec: fleetnetv1alpha1.InternalServiceImportSpec{
					TargetNamespace: "serviceimportnamespace",
					ExposedCluster:  "clustername-to-be-changed",
				},
			},
			expectedResult: reconcile.Result{},
			expectedErr:    nil,
			expectedServiceImport: &fleetnetv1alpha1.ServiceImport{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "serviceimport",
					Namespace: "member-x-in-hub-to-be-changed",
					Labels:    map[string]string{consts.LabelExposedClusterName: "clustername-to-be-changed"},
				},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			fakeHubClient := fakeclient.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(client.Object(tc.internalSvcImport)).
				Build()
			if len(tc.internalSvcImport.Name) == 0 {
				fakeHubClient = fakeclient.NewClientBuilder().
					WithScheme(scheme.Scheme).
					WithObjects().
					Build()
			}

			reconciler := Reconciler{
				hubClient: fakeHubClient,
				Scheme:    scheme.Scheme,
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      tc.internalSvcImport.Name,
					Namespace: tc.internalSvcImport.Namespace,
				},
			}
			actualResult, actualErr := reconciler.Reconcile(context.TODO(), req)
			if !reflect.DeepEqual(actualResult, tc.expectedResult) {
				t.Errorf("Expected result %v , got %v", tc.expectedResult, actualResult)
			}
			if !errors.Is(actualErr, tc.expectedErr) {
				t.Errorf("Expected result %s, got %v", tc.expectedErr, actualErr)
			}

			if len(tc.internalSvcImport.Name) == 0 {
				return
			}

			obtainedServiceImport := &fleetnetv1alpha1.ServiceImport{}
			targetNamespace := utils.GetTargetNamespace(tc.internalSvcImport)
			namespacedName := types.NamespacedName{Namespace: targetNamespace, Name: tc.expectedServiceImport.Name}
			err := reconciler.hubClient.Get(context.TODO(), namespacedName, obtainedServiceImport)
			if apiErrors.IsNotFound(err) {
				t.Errorf("internal service import is not found by namespaced name %s ", namespacedName.String())
			}

			actualExposedCluster := obtainedServiceImport.Labels[consts.LabelExposedClusterName]
			expectedExposedCluster := tc.expectedServiceImport.Labels[consts.LabelExposedClusterName]
			if actualExposedCluster != expectedExposedCluster {
				t.Errorf("Expected exposed cluster %s, got %s", expectedExposedCluster, actualExposedCluster)
			}
		})
	}
}
