// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package globalserviceexport

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	azfake "github.com/Azure/azure-sdk-for-go/sdk/azcore/fake"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armdeploymentstacks"
	armdsfake "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armdeploymentstacks/fake"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	armresfake "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources/fake"
	"istio.io/istio/pkg/kube"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/record"

	"go.goms.io/fleet-networking/api/v1alpha1"
	"go.goms.io/fleet-networking/pkg/apiclient"
	"go.goms.io/fleet-networking/pkg/generated/clientset/internalclientset"
	fakenetworkingclientset "go.goms.io/fleet-networking/pkg/generated/clientset/internalclientset/fake"
)

const (
	testNamespace       = "mclb-demo"
	testServiceName     = "helloworld"
	testResourceGroup   = "default-rg"
	testFrontendCfgID   = "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Network/loadBalancers/lb/frontendIPConfigurations/fe"
	testPublicIPResID   = "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Network/publicIPAddresses/pip"
	testGlobalIPAddress = "20.1.2.3"
	testPollTimeout     = 5 * time.Second
	testPollInterval    = 20 * time.Millisecond
)

var registerTypesOnce sync.Once

// testAPIClient implements apiclient.Client, backed by istio's fake kube.Client
// (for core/Gateway API resources) and the generated fake networking clientset
// (for fleet-networking CRDs).
type testAPIClient struct {
	kube.Client
	networking internalclientset.Interface
}

func (c *testAPIClient) Core() kube.Client                       { return c.Client }
func (c *testAPIClient) Networking() internalclientset.Interface { return c.networking }

var _ apiclient.Client = (*testAPIClient)(nil)

// newTestAPIClient builds a fake apiclient.Client seeded with the given core
// Kubernetes objects and fleet-networking CRD objects.
func newTestAPIClient(t *testing.T, kubeObjs []runtime.Object, crdObjs ...runtime.Object) *testAPIClient {
	t.Helper()
	// RegisterTypes wires fleet-networking CRDs into istio's generic client
	// registry; it is safe, but unnecessary, to call more than once.
	registerTypesOnce.Do(apiclient.RegisterTypes)
	return &testAPIClient{
		Client:     kube.NewFakeClient(kubeObjs...),
		networking: fakenetworkingclientset.NewClientset(crdObjs...),
	}
}

// fakeResourcesClient builds an *armresources.Client whose GetByID handler is
// controlled by getByID.
func fakeResourcesClient(t *testing.T, getByID func(resourceID string) (map[string]interface{}, error)) *armresources.Client {
	t.Helper()
	srv := armresfake.Server{
		GetByID: func(_ context.Context, resourceID string, _ string, _ *armresources.ClientGetByIDOptions) (resp azfake.Responder[armresources.ClientGetByIDResponse], errResp azfake.ErrorResponder) {
			// The fake server's router strips the leading "/" from the
			// resource ID path segment; restore it so callers can compare
			// against the same Azure resource ID strings (e.g.
			// testPublicIPResID) used elsewhere, which always start with "/".
			props, err := getByID("/" + strings.TrimPrefix(resourceID, "/"))
			if err != nil {
				errResp.SetResponseError(http.StatusNotFound, "NotFound")
				return
			}
			resp.SetResponse(http.StatusOK, armresources.ClientGetByIDResponse{
				GenericResource: armresources.GenericResource{Properties: props},
			}, nil)
			return
		},
	}
	transport := armresfake.NewServerTransport(&srv)
	client, err := armresources.NewClient("00000000-0000-0000-0000-000000000000", &azfake.TokenCredential{}, &arm.ClientOptions{
		ClientOptions: policy.ClientOptions{Transport: transport},
	})
	if err != nil {
		t.Fatalf("failed to create fake armresources client: %v", err)
	}
	return client
}

// fakeDeploymentStacksClient builds an *armdeploymentstacks.Client whose
// create/delete handlers are controlled by createErr/deleteErr; on success,
// the create handler returns globalIP as the deployment's output.
func fakeDeploymentStacksClient(t *testing.T, globalIP string, createErr, deleteErr error) *armdeploymentstacks.Client {
	t.Helper()
	srv := armdsfake.Server{
		BeginCreateOrUpdateAtResourceGroup: func(_ context.Context, _ string, _ string, _ armdeploymentstacks.DeploymentStack, _ *armdeploymentstacks.ClientBeginCreateOrUpdateAtResourceGroupOptions) (resp azfake.PollerResponder[armdeploymentstacks.ClientCreateOrUpdateAtResourceGroupResponse], errResp azfake.ErrorResponder) {
			if createErr != nil {
				errResp.SetResponseError(http.StatusInternalServerError, "DeploymentFailed")
				return
			}
			resp.SetTerminalResponse(http.StatusOK, armdeploymentstacks.ClientCreateOrUpdateAtResourceGroupResponse{
				DeploymentStack: armdeploymentstacks.DeploymentStack{
					Properties: &armdeploymentstacks.DeploymentStackProperties{
						Outputs: map[string]interface{}{
							"publicGlobalIPAddress": map[string]interface{}{
								"value": globalIP,
							},
						},
					},
				},
			}, nil)
			return
		},
		BeginDeleteAtResourceGroup: func(_ context.Context, _ string, _ string, _ *armdeploymentstacks.ClientBeginDeleteAtResourceGroupOptions) (resp azfake.PollerResponder[armdeploymentstacks.ClientDeleteAtResourceGroupResponse], errResp azfake.ErrorResponder) {
			if deleteErr != nil {
				errResp.SetResponseError(http.StatusInternalServerError, "DeleteFailed")
				return
			}
			resp.SetTerminalResponse(http.StatusOK, armdeploymentstacks.ClientDeleteAtResourceGroupResponse{}, nil)
			return
		},
	}
	transport := armdsfake.NewServerTransport(&srv)
	client, err := armdeploymentstacks.NewClient("00000000-0000-0000-0000-000000000000", &azfake.TokenCredential{}, &arm.ClientOptions{
		ClientOptions: policy.ClientOptions{Transport: transport},
	})
	if err != nil {
		t.Fatalf("failed to create fake armdeploymentstacks client: %v", err)
	}
	return client
}

func newMCLB(name, namespace string, deleting bool, finalizers ...string) *v1alpha1.MultiClusterLoadBalancer {
	mclb := &v1alpha1.MultiClusterLoadBalancer{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Finalizers: finalizers,
		},
	}
	if deleting {
		now := metav1.Now()
		mclb.DeletionTimestamp = &now
	}
	return mclb
}

func newISE(name, namespace, publicIPResourceID string) *v1alpha1.InternalServiceExport {
	return &v1alpha1.InternalServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-member", name),
			Namespace: namespace,
		},
		Spec: v1alpha1.InternalServiceExportSpec{
			ServiceReference: v1alpha1.ExportedObjectReference{
				NamespacedName: fmt.Sprintf("%s/%s", namespace, name),
				Name:           name,
				Namespace:      namespace,
			},
			PublicIPResourceID: ptrTo(publicIPResourceID),
		},
	}
}

func ptrTo[T any](v T) *T { return &v }

// runReconciler starts the reconciler's krt-backed informers in the
// background and returns a cancel func to stop them at the end of the test.
func runReconciler(t *testing.T, r *Reconciler) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		if err := r.Start(ctx); err != nil {
			t.Logf("reconciler exited: %v", err)
		}
	}()
	return cancel
}

// waitForCondition polls the named MultiClusterLoadBalancer until it has a
// condition of condType with status wantStatus, or fails the test after
// testPollTimeout.
func waitForCondition(t *testing.T, name, namespace string, networking internalclientset.Interface, condType string, wantStatus metav1.ConditionStatus) {
	t.Helper()
	err := wait.PollUntilContextTimeout(context.Background(), testPollInterval, testPollTimeout, true, func(ctx context.Context) (bool, error) {
		mclb, err := networking.NetworkingV1alpha1().MultiClusterLoadBalancers(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, nil //nolint:nilerr // object may not exist yet
		}
		for _, c := range mclb.Status.Conditions {
			if c.Type == condType && c.Status == wantStatus {
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		mclb, getErr := networking.NetworkingV1alpha1().MultiClusterLoadBalancers(namespace).Get(context.Background(), name, metav1.GetOptions{})
		t.Fatalf("timed out waiting for condition %s=%s on %s/%s: %v (latest object: %+v, getErr: %v)", condType, wantStatus, namespace, name, err, mclb, getErr)
	}
}

func TestReconciler_ValidBackendDeploysGlobalLoadBalancer(t *testing.T) {
	mclb := newMCLB(testServiceName, testNamespace, false)
	ise := newISE(testServiceName, testNamespace, testPublicIPResID)
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: testServiceName, Namespace: testNamespace},
	}

	client := newTestAPIClient(t, []runtime.Object{svc}, mclb, ise)
	rc := fakeResourcesClient(t, func(resourceID string) (map[string]interface{}, error) {
		if resourceID != testPublicIPResID {
			return nil, fmt.Errorf("unexpected resource id %q", resourceID)
		}
		return map[string]interface{}{
			"ipConfiguration": map[string]interface{}{
				"id": testFrontendCfgID,
			},
		}, nil
	})
	dc := fakeDeploymentStacksClient(t, testGlobalIPAddress, nil, nil)

	r := NewReconciler(client, dc, rc, testResourceGroup, record.NewFakeRecorder(100))
	defer runReconciler(t, r)()

	waitForCondition(t, testServiceName, testNamespace, client.networking, "Valid", metav1.ConditionTrue)
	waitForCondition(t, testServiceName, testNamespace, client.networking, "Deployed", metav1.ConditionTrue)

	got, err := client.networking.NetworkingV1alpha1().MultiClusterLoadBalancers(testNamespace).Get(context.Background(), testServiceName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get mclb: %v", err)
	}
	if len(got.Status.LoadBalancer.Ingress) != 1 || got.Status.LoadBalancer.Ingress[0].IP != testGlobalIPAddress {
		t.Fatalf("unexpected LoadBalancer status: %+v", got.Status.LoadBalancer)
	}

	// The finalizer should have been applied so the controller can clean up
	// the Azure deployment on deletion.
	err = wait.PollUntilContextTimeout(context.Background(), testPollInterval, testPollTimeout, true, func(ctx context.Context) (bool, error) {
		obj, err := client.networking.NetworkingV1alpha1().MultiClusterLoadBalancers(testNamespace).Get(ctx, testServiceName, metav1.GetOptions{})
		if err != nil {
			return false, nil //nolint:nilerr
		}
		for _, f := range obj.Finalizers {
			if f == "mclb" {
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		t.Fatalf("timed out waiting for mclb finalizer: %v", err)
	}

	// The backing Service should be annotated with the global anycast IP,
	// since no targetGateway annotation was set.
	err = wait.PollUntilContextTimeout(context.Background(), testPollInterval, testPollTimeout, true, func(ctx context.Context) (bool, error) {
		s, err := client.Kube().CoreV1().Services(testNamespace).Get(ctx, testServiceName, metav1.GetOptions{})
		if err != nil {
			return false, nil //nolint:nilerr
		}
		return s.Annotations["service.beta.kubernetes.io/azure-additional-public-ips"] == testGlobalIPAddress, nil
	})
	if err != nil {
		t.Fatalf("timed out waiting for service annotation: %v", err)
	}
}

func TestReconciler_MissingPublicIPMarksInvalid(t *testing.T) {
	mclb := newMCLB(testServiceName, testNamespace, false)
	ise := newISE(testServiceName, testNamespace, testPublicIPResID)
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: testServiceName, Namespace: testNamespace},
	}

	client := newTestAPIClient(t, []runtime.Object{svc}, mclb, ise)
	rc := fakeResourcesClient(t, func(string) (map[string]interface{}, error) {
		return nil, errors.New("public IP not found")
	})
	dc := fakeDeploymentStacksClient(t, testGlobalIPAddress, nil, nil)

	r := NewReconciler(client, dc, rc, testResourceGroup, record.NewFakeRecorder(100))
	defer runReconciler(t, r)()

	waitForCondition(t, testServiceName, testNamespace, client.networking, "Valid", metav1.ConditionFalse)

	got, err := client.networking.NetworkingV1alpha1().MultiClusterLoadBalancers(testNamespace).Get(context.Background(), testServiceName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get mclb: %v", err)
	}
	for _, c := range got.Status.Conditions {
		if c.Type == "Deployed" {
			t.Fatalf("expected no Deployed condition when the backend is invalid, got: %+v", c)
		}
	}
}

// TestReconciler_DeploymentFailureReportsFailedStatus verifies that when
// writeDeployment fails, the controller reports Deployed=False and does not
// subsequently report Deployed=True. Previously, ApplyStatusDeployed was
// invoked unconditionally after writeDeployment regardless of whether it
// succeeded, racily overwriting the failure status with Deployed=True and an
// empty LoadBalancer IP. Moving the deployment call into the async worker
// (see deployForKey) fixed this by making ApplyStatusDeployed conditional on
// writeDeployment's success.
func TestReconciler_DeploymentFailureReportsFailedStatus(t *testing.T) {
	mclb := newMCLB(testServiceName, testNamespace, false)
	ise := newISE(testServiceName, testNamespace, testPublicIPResID)
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: testServiceName, Namespace: testNamespace},
	}

	client := newTestAPIClient(t, []runtime.Object{svc}, mclb, ise)
	rc := fakeResourcesClient(t, func(string) (map[string]interface{}, error) {
		return map[string]interface{}{
			"ipConfiguration": map[string]interface{}{"id": testFrontendCfgID},
		}, nil
	})
	dc := fakeDeploymentStacksClient(t, testGlobalIPAddress, errors.New("deployment failed"), nil)

	recorder := record.NewFakeRecorder(10)
	r := NewReconciler(client, dc, rc, testResourceGroup, recorder)
	defer runReconciler(t, r)()

	waitForCondition(t, testServiceName, testNamespace, client.networking, "Deployed", metav1.ConditionFalse)

	select {
	case event := <-recorder.Events:
		if !strings.Contains(event, deploymentFailedEventReason) {
			t.Fatalf("unexpected event: %s", event)
		}
	case <-time.After(testPollTimeout):
		t.Fatalf("timed out waiting for a %s event", deploymentFailedEventReason)
	}

	// Give any (incorrect) eventual Deployed=True write a chance to land, to
	// confirm the failed status sticks rather than being overwritten.
	time.Sleep(testPollInterval * 5)

	got, err := client.networking.NetworkingV1alpha1().MultiClusterLoadBalancers(testNamespace).Get(context.Background(), testServiceName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get mclb: %v", err)
	}
	for _, c := range got.Status.Conditions {
		if c.Type == "Deployed" && c.Status == metav1.ConditionTrue {
			t.Fatalf("expected Deployed to remain False after a failed deployment, got: %+v", c)
		}
	}
}

func TestReconciler_DeletionRemovesFinalizerAndDeletesStack(t *testing.T) {
	mclb := newMCLB(testServiceName, testNamespace, true, "mclb")

	client := newTestAPIClient(t, nil, mclb)
	rc := fakeResourcesClient(t, func(string) (map[string]interface{}, error) {
		t.Fatal("GetByID should not be called while deleting")
		return nil, nil
	})

	var deleteCalled bool
	srv := armdsfake.Server{
		BeginDeleteAtResourceGroup: func(_ context.Context, resourceGroupName string, deploymentStackName string, _ *armdeploymentstacks.ClientBeginDeleteAtResourceGroupOptions) (resp azfake.PollerResponder[armdeploymentstacks.ClientDeleteAtResourceGroupResponse], errResp azfake.ErrorResponder) {
			deleteCalled = true
			if resourceGroupName != testResourceGroup {
				t.Errorf("unexpected resource group %q, want %q", resourceGroupName, testResourceGroup)
			}
			// Note: deleteDeployment is called with the bare mclb.Name, not the
			// "namespace-name" stack name used by writeDeployment on create;
			// this asymmetry is an existing quirk of the controller, not a
			// test bug.
			if deploymentStackName != testServiceName {
				t.Errorf("unexpected deployment stack name %q, want %q", deploymentStackName, testServiceName)
			}
			resp.SetTerminalResponse(http.StatusOK, armdeploymentstacks.ClientDeleteAtResourceGroupResponse{}, nil)
			return
		},
	}
	transport := armdsfake.NewServerTransport(&srv)
	dc, err := armdeploymentstacks.NewClient("00000000-0000-0000-0000-000000000000", &azfake.TokenCredential{}, &arm.ClientOptions{
		ClientOptions: policy.ClientOptions{Transport: transport},
	})
	if err != nil {
		t.Fatalf("failed to create fake armdeploymentstacks client: %v", err)
	}

	r := NewReconciler(client, dc, rc, testResourceGroup, record.NewFakeRecorder(100))
	defer runReconciler(t, r)()

	err = wait.PollUntilContextTimeout(context.Background(), testPollInterval, testPollTimeout, true, func(ctx context.Context) (bool, error) {
		obj, getErr := client.networking.NetworkingV1alpha1().MultiClusterLoadBalancers(testNamespace).Get(ctx, testServiceName, metav1.GetOptions{})
		if getErr != nil {
			return false, nil //nolint:nilerr
		}
		for _, f := range obj.Finalizers {
			if f == "mclb" {
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil {
		t.Fatalf("timed out waiting for finalizer removal: %v", err)
	}
	if !deleteCalled {
		t.Fatal("expected the deployment stack to be deleted")
	}
}
