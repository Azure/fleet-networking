//go:build e2e

/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v4"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
)

const privateHTTPTestEdgeReference = "edge-reference"

func privateHTTPGatewayFixture() gatewayv1.Gateway {
	conditions := []metav1.Condition{
		{Type: privateHTTPAccepted, Status: metav1.ConditionTrue, Reason: privateHTTPAccepted, ObservedGeneration: 3},
		{Type: privateHTTPProgrammed, Status: metav1.ConditionTrue, Reason: privateHTTPProgrammed, ObservedGeneration: 3},
	}
	return gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{UID: types.UID("owned-gateway"), Generation: 3,
			Finalizers: []string{objectmeta.GatewayAFDCleanupFinalizer}},
		Spec: gatewayv1.GatewaySpec{Listeners: []gatewayv1.Listener{{Name: privateHTTPListener, Protocol: gatewayv1.HTTPProtocolType, Port: 80}}},
		Status: gatewayv1.GatewayStatus{
			Conditions: conditions,
			Addresses:  []gatewayv1.GatewayStatusAddress{{Type: ptr.To(gatewayv1.HostnameAddressType), Value: "proof.azurefd.net"}},
			Listeners: []gatewayv1.ListenerStatus{{
				Name: privateHTTPListener, AttachedRoutes: 1, Conditions: append([]metav1.Condition(nil), conditions...),
			}},
		},
	}
}

func TestPrivateHTTPGatewayEvidence(t *testing.T) {
	for _, scenario := range []string{"current Gateway", "stale gateway", "stale listener", "missing owner", "wrong listener", "no routes", "missing address", "IP address"} {
		t.Run(scenario, func(t *testing.T) {
			gateway := privateHTTPGatewayFixture()
			switch scenario {
			case "stale gateway":
				gateway.Status.Conditions[1].ObservedGeneration--
			case "stale listener":
				gateway.Status.Listeners[0].Conditions[1].ObservedGeneration--
			case "missing owner":
				gateway.Finalizers = nil
			case "wrong listener":
				gateway.Status.Listeners[0].Name = "other"
			case "no routes":
				gateway.Status.Listeners[0].AttachedRoutes = 0
			case "missing address":
				gateway.Status.Addresses = nil
			case "IP address":
				gateway.Status.Addresses[0].Type = ptr.To(gatewayv1.IPAddressType)
			}
			hostname, err := privateHTTPGatewayState(&gateway, true, 1)
			if (err == nil) != (scenario == "current Gateway") || (err != nil && hostname != "") {
				t.Fatalf("invalid programmed evidence: %q, %v", hostname, err)
			}
		})
	}
}

func TestPrivateHTTPResponseEvidence(t *testing.T) {
	const body = `{"proof_id":"run","cluster_id":"member-1","host":"proof.azurefd.net","request_id":"fresh"}`
	const memberName = "member-1"
	for _, scenario := range []string{"current response", "stale request", "wrong host", "wrong HTTP workload", "unknown member", "redirect", "no edge evidence", "invalid JSON"} {
		t.Run(scenario, func(t *testing.T) {
			reply := privateHTTPReply{Status: http.StatusOK, Body: []byte(body), AzureRef: privateHTTPTestEdgeReference}
			switch scenario {
			case "stale request":
				reply.Body = []byte(strings.ReplaceAll(body, "fresh", "old"))
			case "wrong host":
				reply.Body = []byte(strings.ReplaceAll(body, "proof.azurefd.net", "origin.internal"))
			case "wrong HTTP workload":
				reply.Body = []byte(strings.ReplaceAll(body, `"run"`, `"different"`))
			case "unknown member":
				reply.Body = []byte(strings.ReplaceAll(body, memberName, "unexpected"))
			case "redirect":
				reply.Status = http.StatusMovedPermanently
			case "no edge evidence":
				reply.AzureRef = ""
			case "invalid JSON":
				reply.Body = []byte("not-json")
			}
			member, err := validatePrivateHTTPResponse(reply, "run", "fresh", "proof.azurefd.net", map[string]bool{memberName: true})
			if (err == nil) != (scenario == "current response") || (err != nil && member != "") {
				t.Fatalf("invalid application evidence: %q, %v", member, err)
			}
		})
	}
}

func TestPrivateHTTPWithdrawalEvidence(t *testing.T) {
	for _, status := range []int{200, 301, 404, 408, 429, 503} {
		reply := privateHTTPReply{Status: status, Body: []byte("disabled"), AzureRef: privateHTTPTestEdgeReference}
		err := validatePrivateHTTPBlocked(reply, "unique-run")
		if (err == nil) != (status == 404 || status == 503) {
			t.Fatalf("HTTP %d: invalid withdrawal proof: %v", status, err)
		}
	}
	for _, reply := range []privateHTTPReply{
		{Status: 503, Body: []byte("application unique-run"), AzureRef: privateHTTPTestEdgeReference},
		{Status: 404, Body: []byte("disabled")},
		{},
	} {
		if err := validatePrivateHTTPBlocked(reply, "unique-run"); err == nil {
			t.Fatalf("application/no-edge/no-response evidence passed withdrawal: %+v", reply)
		}
	}
}

func TestPrivateHTTPProbeEvidence(t *testing.T) {
	const entry = `WARNING: {"fleet_private_http_proof":"run","method":"GET","path":"/","host":"observed-probe-host","user_agent":"Edge Health Probe"}`
	for _, scenario := range []string{"probe", "wrong probe workload", "application request", "wrong path", "wrong method", "no host", "absent probe record"} {
		t.Run(scenario, func(t *testing.T) {
			logs := entry
			switch scenario {
			case "wrong probe workload":
				logs = strings.ReplaceAll(logs, `"run"`, `"old-run"`)
			case "application request":
				logs = strings.ReplaceAll(logs, "Edge Health Probe", "Go-http-client/1.1")
			case "wrong path":
				logs = strings.ReplaceAll(logs, `"path":"/"`, `"path":"/healthz"`)
			case "wrong method":
				logs = strings.ReplaceAll(logs, "GET", "HEAD")
			case "no host":
				logs = strings.ReplaceAll(logs, "observed-probe-host", "")
			case "absent probe record":
				logs = ""
			}
			host, err := privateHTTPProbeEvidence(logs, "run")
			if (err == nil) != (scenario == "probe") || (err != nil && host != "") {
				t.Fatalf("invalid probe evidence: %q, %v", host, err)
			}
		})
	}
}

func TestPrivateHTTPRequestBoundaries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte(strings.Repeat("x", privateHTTPMaxBody+1)))
	}))
	defer server.Close()
	c := privateHTTPClient()
	defer c.CloseIdleConnections()
	reply, err := requestPrivateHTTP(context.Background(), c, server.URL+"/redirect", "nonce")
	if err != nil || reply.Status != http.StatusFound {
		t.Fatalf("redirect followed instead of observed: %+v, %v", reply, err)
	}
	if _, err := requestPrivateHTTP(context.Background(), c, server.URL, "nonce"); err == nil {
		t.Fatal("oversized application response accepted")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := requestPrivateHTTP(canceled, c, server.URL, "nonce"); err == nil {
		t.Fatal("canceled request accepted as traffic evidence")
	}
}

func TestPrivateHTTPFrontendEvidence(t *testing.T) {
	const plsID = "/subscriptions/sub/resourceGroups/member/providers/Microsoft.Network/privateLinkServices/pls"
	const frontendID = "/subscriptions/sub/resourceGroups/member/providers/Microsoft.Network/loadBalancers/ilb/frontendIPConfigurations/private"
	for _, scenario := range []string{"private", "public address", "public prefix", "missing subnet", "source replaced", "wrong IP", "unbound frontend", "wrong PLS"} {
		t.Run(scenario, func(t *testing.T) {
			service := corev1.Service{
				ObjectMeta: metav1.ObjectMeta{UID: "service-uid", Annotations: map[string]string{
					objectmeta.ServiceAnnotationAzureLoadBalancerInternal: privateHTTPEnabled,
				}},
				Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
				Status: corev1.ServiceStatus{LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: []corev1.LoadBalancerIngress{{IP: "10.0.0.5"}},
				}},
			}
			origin := gatewaymodel.Origin{Cluster: "member-1", ServiceUID: "service-uid", Endpoint: "10.0.0.5",
				Weight: 1, PrivateLinkResourceID: plsID, PrivateLinkLocation: "eastus2"}
			pls := armnetwork.PrivateLinkService{ID: ptr.To(plsID), Location: ptr.To("eastus2"),
				Properties: &armnetwork.PrivateLinkServiceProperties{
					ProvisioningState:                    ptr.To(armnetwork.ProvisioningStateSucceeded),
					LoadBalancerFrontendIPConfigurations: []*armnetwork.FrontendIPConfiguration{{ID: ptr.To(frontendID)}},
				}}
			frontend := armnetwork.FrontendIPConfiguration{ID: ptr.To(frontendID),
				Properties: &armnetwork.FrontendIPConfigurationPropertiesFormat{
					PrivateIPAddress: ptr.To("10.0.0.5"), Subnet: &armnetwork.Subnet{ID: ptr.To("subnet-id")},
					ProvisioningState: ptr.To(armnetwork.ProvisioningStateSucceeded),
				}}
			switch scenario {
			case "public address":
				frontend.Properties.PublicIPAddress = &armnetwork.PublicIPAddress{ID: ptr.To("public-id")}
			case "public prefix":
				frontend.Properties.PublicIPPrefix = &armnetwork.SubResource{ID: ptr.To("public-prefix")}
			case "missing subnet":
				frontend.Properties.Subnet = nil
			case "source replaced":
				service.UID = "new-service-uid"
			case "wrong IP":
				frontend.Properties.PrivateIPAddress = ptr.To("10.0.0.6")
			case "unbound frontend":
				pls.Properties.LoadBalancerFrontendIPConfigurations = nil
			case "wrong PLS":
				pls.ID = ptr.To(plsID + "-different")
			}
			err := validatePrivateHTTPFrontend(&service, origin, pls, frontend)
			if (err == nil) != (scenario == "private") {
				t.Fatalf("invalid private frontend evidence: %v", err)
			}
		})
	}
}

func TestPrivateHTTPUnprogrammedEvidence(t *testing.T) {
	for _, retainedAddress := range []bool{false, true} {
		gateway := privateHTTPGatewayFixture()
		gateway.Status.Conditions[1].Status = metav1.ConditionFalse
		gateway.Status.Conditions[1].Reason = privateHTTPPending
		gateway.Status.Listeners[0].Conditions[1] = gateway.Status.Conditions[1]
		gateway.Status.Listeners[0].AttachedRoutes = 0
		if !retainedAddress {
			gateway.Status.Addresses = nil
		}
		_, err := privateHTTPGatewayState(&gateway, false, 0)
		if (err == nil) != !retainedAddress {
			t.Fatalf("withdrawn Gateway retained an address: %v", err)
		}
	}
}

func TestPrivateHTTPInstalledControllerEvidence(t *testing.T) {
	for _, scenario := range []string{"installed", "stale rollout", "disabled", "wrong image", "old pod image", "unready pod", "no image identity"} {
		t.Run(scenario, func(t *testing.T) {
			deployment := appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{UID: "controller-uid", Generation: 2},
				Spec: appsv1.DeploymentSpec{Replicas: ptr.To[int32](1), Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{ServiceAccountName: "gateway", Containers: []corev1.Container{{
						Name: privateHTTPManager, Image: "registry/controller:proof",
						Args: []string{"--enable-afd=true", "--afd-resource-group=test-rg"},
					}}},
				}},
				Status: appsv1.DeploymentStatus{ObservedGeneration: 2, UpdatedReplicas: 1, ReadyReplicas: 1, AvailableReplicas: 1},
			}
			pod := corev1.Pod{Spec: corev1.PodSpec{ServiceAccountName: "gateway", Containers: []corev1.Container{{
				Name: privateHTTPManager, Image: "registry/controller:proof",
			}}}, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
				Name: privateHTTPManager, Ready: true, ImageID: "registry/controller@sha256:actual-image", ContainerID: "containerd://running",
				State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
			}}}}
			switch scenario {
			case "stale rollout":
				deployment.Status.ObservedGeneration--
			case "disabled":
				deployment.Spec.Template.Spec.Containers[0].Args = nil
			case "wrong image":
				deployment.Spec.Template.Spec.Containers[0].Image = "different:tag"
			case "old pod image":
				pod.Spec.Containers[0].Image = "registry/controller:old"
			case "unready pod":
				pod.Status.ContainerStatuses[0].Ready = false
			case "no image identity":
				pod.Status.ContainerStatuses[0].ImageID = ""
			}
			images, err := privateHTTPControllerEvidence(&deployment, []corev1.Pod{pod}, "registry/controller:proof", "test-rg")
			if (err == nil) != (scenario == "installed") || (err == nil && len(images) != 1) {
				t.Fatalf("invalid installed-controller evidence: %v, %v", images, err)
			}
		})
	}
}
