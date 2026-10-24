//go:build e2e

/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"go.goms.io/fleet-networking/pkg/common/objectmeta"
)

const (
	privateHTTPMaxBody    = 1 << 20
	privateHTTPAccepted   = string(gatewayv1.GatewayConditionAccepted)
	privateHTTPProgrammed = string(gatewayv1.GatewayConditionProgrammed)
	privateHTTPPending    = string(gatewayv1.GatewayReasonPending)
	privateHTTPListener   = "http"
	privateHTTPManager    = "manager"
	privateHTTPEnabled    = "true"
)

type privateHTTPReply struct {
	Status   int
	Body     []byte
	AzureRef string
}

func privateHTTPGatewayState(gateway *gatewayv1.Gateway, programmed bool, attached int32) (string, error) {
	if gateway.UID == "" || gateway.DeletionTimestamp != nil {
		return "", fmt.Errorf("proof requires the live server-assigned Gateway identity")
	}
	owned := false
	for _, finalizer := range gateway.Finalizers {
		owned = owned || finalizer == objectmeta.GatewayAFDCleanupFinalizer
	}
	if !owned {
		return "", fmt.Errorf("Gateway lacks persisted AFD cleanup ownership")
	}
	status, reason := metav1.ConditionFalse, privateHTTPPending
	if programmed {
		status, reason = metav1.ConditionTrue, privateHTTPProgrammed
	}
	if err := currentGatewayCondition(gateway.Status.Conditions, privateHTTPAccepted, metav1.ConditionTrue, privateHTTPAccepted, gateway.Generation); err != nil {
		return "", err
	}
	if err := currentGatewayCondition(gateway.Status.Conditions, privateHTTPProgrammed, status, reason, gateway.Generation); err != nil {
		return "", err
	}
	if len(gateway.Spec.Listeners) != 1 || len(gateway.Status.Listeners) != 1 ||
		gateway.Status.Listeners[0].Name != gateway.Spec.Listeners[0].Name ||
		gateway.Status.Listeners[0].AttachedRoutes != attached {
		return "", fmt.Errorf("Gateway listener does not establish the expected attachment")
	}
	if err := currentGatewayCondition(gateway.Status.Listeners[0].Conditions, privateHTTPProgrammed, status, reason, gateway.Generation); err != nil {
		return "", err
	}
	if !programmed {
		if len(gateway.Status.Addresses) != 0 {
			return "", fmt.Errorf("unprogrammed Gateway still publishes an address")
		}
		return "", nil
	}
	if len(gateway.Status.Addresses) != 1 || gateway.Status.Addresses[0].Type == nil ||
		*gateway.Status.Addresses[0].Type != gatewayv1.HostnameAddressType {
		return "", fmt.Errorf("programmed Gateway requires one observed hostname")
	}
	host := gateway.Status.Addresses[0].Value
	if host == "" || strings.TrimSpace(host) != host || strings.ContainsAny(host, "/\\:?#") ||
		!strings.HasSuffix(strings.ToLower(host), ".azurefd.net") {
		return "", fmt.Errorf("Gateway does not publish a public Azure Front Door hostname")
	}
	return host, nil
}

func privateHTTPClient() *http.Client {
	return &http.Client{
		Timeout:       10 * time.Second,
		Transport:     &http.Transport{Proxy: nil, DisableKeepAlives: true, ResponseHeaderTimeout: 10 * time.Second},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func requestPrivateHTTP(ctx context.Context, c *http.Client, endpoint, nonce string) (privateHTTPReply, error) {
	target, err := url.Parse(endpoint)
	if err != nil {
		return privateHTTPReply{}, err
	}
	query := target.Query()
	query.Set("proof", nonce)
	target.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return privateHTTPReply{}, err
	}
	request.Header.Set("Cache-Control", "no-cache")
	response, err := c.Do(request)
	if err != nil {
		return privateHTTPReply{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, privateHTTPMaxBody+1))
	if err != nil {
		return privateHTTPReply{}, err
	}
	if len(body) > privateHTTPMaxBody {
		return privateHTTPReply{}, fmt.Errorf("HTTP proof response exceeded %d bytes", privateHTTPMaxBody)
	}
	return privateHTTPReply{Status: response.StatusCode, Body: body, AzureRef: response.Header.Get("X-Azure-Ref")}, nil
}

func validatePrivateHTTPResponse(reply privateHTTPReply, proofID, nonce, host string, members map[string]bool) (string, error) {
	var body struct {
		ProofID   string `json:"proof_id"`
		ClusterID string `json:"cluster_id"`
		Host      string `json:"host"`
		RequestID string `json:"request_id"`
	}
	if reply.Status != http.StatusOK || reply.AzureRef == "" {
		return "", fmt.Errorf("AFD did not return HTTP 200 with edge evidence (HTTP %d)", reply.Status)
	}
	if err := json.Unmarshal(reply.Body, &body); err != nil {
		return "", fmt.Errorf("decode private application response: %w", err)
	}
	if proofID == "" || nonce == "" || body.ProofID != proofID || body.RequestID != nonce ||
		body.Host != host || !members[body.ClusterID] {
		return "", fmt.Errorf("application response lacks exact workload/member/Host/fresh-request identity: %+v", body)
	}
	return body.ClusterID, nil
}

func validatePrivateHTTPBlocked(reply privateHTTPReply, proofID string) error {
	if reply.Status < 400 || reply.Status > 599 || reply.Status == http.StatusRequestTimeout ||
		reply.Status == http.StatusTooManyRequests || reply.AzureRef == "" {
		return fmt.Errorf("no affirmative AFD withdrawal response (HTTP %d)", reply.Status)
	}
	if proofID == "" || strings.Contains(string(reply.Body), proofID) {
		return fmt.Errorf("withdrawn endpoint returned the proof application")
	}
	return nil
}

func privateHTTPProbeEvidence(logs, proofID string) (string, error) {
	for _, line := range strings.Split(logs, "\n") {
		start := strings.IndexByte(line, '{')
		if start < 0 {
			continue
		}
		var record struct {
			ProofID   string `json:"fleet_private_http_proof"`
			Method    string `json:"method"`
			Path      string `json:"path"`
			Host      string `json:"host"`
			UserAgent string `json:"user_agent"`
		}
		if err := json.Unmarshal([]byte(line[start:]), &record); err != nil {
			continue
		}
		if proofID != "" && record.ProofID == proofID && record.Method == http.MethodGet &&
			record.Path == "/" && record.Host != "" && record.UserAgent == "Edge Health Probe" {
			return record.Host, nil
		}
	}
	return "", fmt.Errorf("no HTTP GET / AFD probe with an observed Host for this proof workload")
}

func privateHTTPControllerEvidence(deployment *appsv1.Deployment, pods []corev1.Pod, image, resourceGroup string) ([]string, error) {
	replicas := ptr.Deref(deployment.Spec.Replicas, 1)
	if image == "" || resourceGroup == "" || deployment.UID == "" || replicas < 1 ||
		deployment.Status.ObservedGeneration != deployment.Generation || deployment.Status.UpdatedReplicas != replicas ||
		deployment.Status.ReadyReplicas != replicas || deployment.Status.AvailableReplicas != replicas ||
		deployment.Spec.Template.Spec.ServiceAccountName == "" {
		return nil, fmt.Errorf("installed Gateway controller rollout/account is not current and available")
	}
	configured := false
	for _, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name != privateHTTPManager || container.Image != image {
			continue
		}
		enabled, scoped := false, false
		for _, arg := range container.Args {
			enabled = enabled || arg == "--enable-afd=true"
			scoped = scoped || arg == "--afd-resource-group="+resourceGroup
		}
		configured = enabled && scoped
	}
	if !configured {
		return nil, fmt.Errorf("installed controller image/enablement/resource group differs from the proof scope")
	}
	var images []string
	for _, pod := range pods {
		if !privateHTTPPodMatches(pod, image, deployment.Spec.Template.Spec.ServiceAccountName) {
			continue
		}
		for _, container := range pod.Status.ContainerStatuses {
			if container.Name == privateHTTPManager && container.Ready && container.State.Running != nil &&
				container.ImageID != "" && container.ContainerID != "" {
				images = append(images, container.ImageID)
			}
		}
	}
	if len(images) != int(replicas) {
		return nil, fmt.Errorf("expected %d ready owned controller runtime images, observed %d", replicas, len(images))
	}
	return images, nil
}

func privateHTTPPodMatches(pod corev1.Pod, image, account string) bool {
	if pod.DeletionTimestamp != nil || pod.Spec.ServiceAccountName != account {
		return false
	}
	for _, container := range pod.Spec.Containers {
		if container.Name == privateHTTPManager && container.Image == image {
			return true
		}
	}
	return false
}
