//go:build conformance

/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package conformance_test

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v9"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/wait"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/cloud-provider-azure/pkg/azclient/loadbalancerclient"
	"sigs.k8s.io/cloud-provider-azure/pkg/azclient/privatelinkserviceclient"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
	upstream "sigs.k8s.io/gateway-api/conformance"
	"sigs.k8s.io/gateway-api/conformance/tests"
	"sigs.k8s.io/gateway-api/conformance/utils/config"
	"sigs.k8s.io/gateway-api/conformance/utils/kubernetes"
	"sigs.k8s.io/gateway-api/conformance/utils/suite"

	"go.goms.io/fleet-networking/pkg/common/azureerrors"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/common/privateorigin"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewayresolver"
	"go.goms.io/fleet-networking/pkg/controllers/hub/localservice"
	"go.goms.io/fleet-networking/pkg/providers/azure/frontdoor"
	harness "go.goms.io/fleet-networking/test/conformance"
)

var kubeContext = flag.String("conformance-context", "hub-admin", "Approved workload-capable cluster context")

var requiredTests = []suite.ConformanceTest{
	tests.HTTPRouteSimpleSameNamespace,
}

var baseFixtureKeys = []string{
	"Namespace/gateway-conformance-infra",
	"Gateway/same-namespace",
	"Service/infra-backend-v1",
	"Deployment/infra-backend-v1",
}

func TestAFDConformance(t *testing.T) {
	if len(requiredTests) == 0 {
		t.Fatal("Select at least one required upstream test")
	}
	if os.Getenv("ENABLE_GATEWAY_CONFORMANCE") != "true" || os.Getenv("ENABLE_AFD_PRIVATE_LINK_AUTO_APPROVAL") != "true" ||
		os.Getenv("KUBECONFIG") == "" {
		t.Fatal("Require explicit ENABLE_GATEWAY_CONFORMANCE=true, ENABLE_AFD_PRIVATE_LINK_AUTO_APPROVAL=true and KUBECONFIG")
	}
	subscription, group, nodeGroup := os.Getenv("AZURE_SUBSCRIPTION_ID"), os.Getenv("AZURE_RESOURCE_GROUP"), os.Getenv("LOCAL_SERVICE_RESOURCE_GROUP")
	if subscription == "" || group == "" || nodeGroup == "" {
		t.Fatal("Require approved AZURE_SUBSCRIPTION_ID, AZURE_RESOURCE_GROUP and LOCAL_SERVICE_RESOURCE_GROUP")
	}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{CurrentContext: *kubeContext}).ClientConfig()
	if err != nil {
		t.Fatal(err)
	}
	scheme := runtime.NewScheme()
	for _, install := range []func(*runtime.Scheme) error{
		clientgoscheme.AddToScheme, gatewayv1.Install, gatewayv1beta1.Install, apiextensionsv1.AddToScheme,
	} {
		if err := install(scheme); err != nil {
			t.Fatal(err)
		}
	}
	options := client.Options{Scheme: scheme}
	c, err := client.New(cfg, options)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	var class gatewayv1.GatewayClass
	if err := c.Get(ctx, client.ObjectKey{Name: "azure-fleet-afd"}, &class); err != nil {
		t.Fatal(err)
	}
	if class.Spec.ControllerName != gatewayv1.GatewayController(objectmeta.GatewayAPIControllerName) {
		t.Fatal("GatewayClass is not owned by Fleet AFD")
	}
	fixtures, err := harness.HTTPFixtures(&upstream.Manifests, class.Name, baseFixtureKeys)
	if err != nil {
		t.Fatal(err)
	}
	credential, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		t.Fatal(err)
	}
	lb, err := loadbalancerclient.New(subscription, credential, nil)
	if err != nil {
		t.Fatal(err)
	}
	pls, err := privatelinkserviceclient.New(subscription, credential, nil)
	if err != nil {
		t.Fatal(err)
	}
	discovery := &privateorigin.Discovery{LoadBalancerClient: lb, PrivateLinkServiceClient: pls, ResourceGroupName: nodeGroup}
	resolver := &localservice.Reconciler{Client: c, APIReader: c, Discovery: discovery}
	for _, test := range requiredTests {
		t.Run(test.ShortName, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 75*time.Minute)
			defer cancel()
			timeouts := config.DefaultTimeoutConfig()
			timeouts.GatewayMustHaveAddress, timeouts.GatewayMustHaveCondition = 45*time.Minute, 45*time.Minute
			timeouts.DeleteTimeout = 45 * time.Minute
			cs, err := suite.NewConformanceTestSuite(suite.ConformanceOptions{
				Client: c, ClientOptions: options, RestConfig: cfg, GatewayClassName: class.Name,
				SupportedFeatures: sets.New(test.Features...),
				ManifestFS:        []fs.FS{&upstream.Manifests}, TimeoutConfig: timeouts,
			})
			if err != nil {
				t.Fatal(err)
			}
			cs.ControllerName = kubernetes.GWCMustHaveAcceptedConditionTrue(t, c, cs.TimeoutConfig, class.Name)
			cs.Applier.ManifestFS, cs.Applier.ControllerName, cs.Applier.GatewayClass = cs.ManifestFS, cs.ControllerName, class.Name
			var created []unstructured.Unstructured
			var namespaces []string
			// Defers run before upstream t.Cleanup handlers delete the route and fixture resources.
			defer captureDiagnostics(t, c, test.ShortName, namespacesForFixtures(fixtures))
			t.Cleanup(func() {
				cleanupCtx, stop := context.WithTimeout(context.Background(), 45*time.Minute)
				defer stop()
				if err := harness.CleanupFixtures(cleanupCtx, c, created); err != nil {
					t.Errorf("test fixture cleanup: %v", err)
				}
			})
			for _, fixture := range fixtures {
				resource := fixture.DeepCopy()
				if resource.GetKind() == "Namespace" {
					if err := c.Get(ctx, client.ObjectKey{Name: resource.GetName()}, &corev1.Namespace{}); !apierrors.IsNotFound(err) {
						t.Fatalf("Require unused test namespace %s; GET returned %v", resource.GetName(), err)
					}
					namespaces = append(namespaces, resource.GetName())
				}
				if err := c.Create(ctx, resource); err != nil {
					t.Fatal(err)
				}
				created = append(created, *resource)
			}
			// Stage each test's own manifests, not a hard-coded route, before approval.
			for _, manifest := range test.Manifests {
				cs.Applier.MustApplyWithCleanup(t, c, cs.TimeoutConfig, manifest, true)
			}
			var routes []gatewayv1.HTTPRoute
			for _, namespace := range namespaces {
				var list gatewayv1.HTTPRouteList
				if err := c.List(ctx, &list, client.InNamespace(namespace)); err != nil {
					t.Fatal(err)
				}
				routes = append(routes, list.Items...)
			}
			targets, err := harness.OriginTargets(routes)
			if err != nil {
				t.Fatal(err)
			}
			if err := approveAndWarm(ctx, c, resolver, credential,
				frontdoor.ResourceScope{SubscriptionID: subscription, ResourceGroup: group}, nodeGroup, targets); err != nil {
				t.Fatal(err)
			}
			t.Log("Running selected upstream test with shared private-origin setup; not full profile conformance")
			if err := cs.Run(t, []suite.ConformanceTest{test}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type originCheckpoint struct {
	connectionID string
	targetID     string
	gatewayUID   string
	skipWarm     bool
	lastProgress string
}

func (checkpoint *originCheckpoint) progress(target harness.OriginTarget, message string) {
	if checkpoint.lastProgress == message {
		return
	}
	checkpoint.lastProgress = message
	fmt.Fprintf(os.Stderr, "Preparation Gateway=%s route=%s backend=%s: %s\n",
		target.Gateway, target.Route, target.Reference.Name, message)
}

func approveAndWarm(ctx context.Context, c client.Client, resolver gatewayresolver.LocalServiceResolver,
	credential *azidentity.DefaultAzureCredential, scope frontdoor.ResourceScope, nodeGroup string, targets []harness.OriginTarget) error {
	services, err := armnetwork.NewPrivateLinkServicesClient(scope.SubscriptionID, credential, nil)
	if err != nil {
		return err
	}
	profiles, err := armcdn.NewProfilesClient(scope.SubscriptionID, credential, nil)
	if err != nil {
		return err
	}
	checkpoints := make([]originCheckpoint, len(targets))
	httpClient := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	return wait.PollUntilContextCancel(ctx, 5*time.Second, true, func(ctx context.Context) (bool, error) {
		allApproved := true
		for i, target := range targets {
			approved, err := approveTarget(ctx, c, resolver, profiles, services, scope, nodeGroup, target, &checkpoints[i])
			if err != nil {
				return false, err
			}
			allApproved = allApproved && approved
		}
		// Approve every target first; one unavailable backend must not starve other approvals.
		if !allApproved {
			return false, nil
		}
		for i, target := range targets {
			if checkpoints[i].skipWarm {
				continue
			}
			ready, err := warmTarget(ctx, c, httpClient, target)
			if err != nil || !ready {
				return false, err
			}
		}
		return true, nil
	})
}

func approveTarget(ctx context.Context, c client.Client, resolver gatewayresolver.LocalServiceResolver,
	profiles *armcdn.ProfilesClient, services *armnetwork.PrivateLinkServicesClient,
	scope frontdoor.ResourceScope, nodeGroup string, target harness.OriginTarget, checkpoint *originCheckpoint) (bool, error) {
	key := client.ObjectKey(target.Gateway)
	var gateway gatewayv1.Gateway
	if err := c.Get(ctx, key, &gateway); err != nil {
		if apierrors.IsNotFound(err) {
			checkpoint.skipWarm = true
			return true, nil
		}
		return false, err
	}
	if checkpoint.gatewayUID == "" {
		checkpoint.gatewayUID = string(gateway.UID)
	}
	if checkpoint.gatewayUID == "" || string(gateway.UID) != checkpoint.gatewayUID || gateway.DeletionTimestamp != nil {
		return false, fmt.Errorf("test Gateway identity changed")
	}
	var class gatewayv1.GatewayClass
	if err := c.Get(ctx, client.ObjectKey{Name: string(gateway.Spec.GatewayClassName)}, &class); err != nil {
		return false, err
	}
	if class.Spec.ControllerName != gatewayv1.GatewayController(objectmeta.GatewayAPIControllerName) {
		return false, fmt.Errorf("fixture Gateway is not owned by Fleet AFD")
	}
	var route gatewayv1.HTTPRoute
	if err := c.Get(ctx, client.ObjectKey(target.Route), &route); err != nil {
		return false, err
	}
	currentReference := false
	if target.RuleIndex >= 0 && target.RuleIndex < len(route.Spec.Rules) {
		for _, ref := range route.Spec.Rules[target.RuleIndex].BackendRefs {
			currentReference = currentReference || reflect.DeepEqual(ref, target.Reference)
		}
	}
	currentParent := false
	for _, parent := range route.Spec.ParentRefs {
		currentParent = currentParent || reflect.DeepEqual(parent, target.Parent)
	}
	if !currentReference || !currentParent {
		return false, fmt.Errorf("fixture references changed during approval")
	}
	var namespace corev1.Namespace
	if err := c.Get(ctx, client.ObjectKey{Name: route.Namespace}, &namespace); err != nil {
		return false, err
	}
	if _, err := gatewayresolver.ResolveAttachments(&gateway, &route, target.Parent, namespace.Labels); err != nil {
		var denied *gatewayresolver.AttachmentError
		if errors.As(err, &denied) {
			// Negative attachment tests run their assertions without approving unused origins.
			checkpoint.skipWarm = true
			return true, nil
		}
		return false, err
	}
	backend, err := resolver.ResolveLocalService(ctx, target.Route.Namespace, target.Reference)
	if err != nil {
		var pending *gatewayresolver.ResolutionError
		if errors.As(err, &pending) && pending.ReferenceValid && pending.Reason == "BackendUnavailable" {
			checkpoint.progress(target, err.Error())
			return false, nil
		}
		if errors.As(err, &pending) && !pending.ReferenceValid {
			fmt.Fprintf(os.Stderr, "No approval for invalid fixture reference %s/%s: %v\n", target.Route.Namespace, target.Reference.Name, err)
			checkpoint.skipWarm = true
			return true, nil
		}
		return false, err
	}
	checkpoint.skipWarm = false
	if len(backend.Origins) != 1 {
		return false, fmt.Errorf("local test backend must have one verified companion origin")
	}
	origin := backend.Origins[0]
	plsID, err := arm.ParseResourceID(origin.PrivateLinkResourceID)
	if err != nil || !strings.EqualFold(plsID.SubscriptionID, scope.SubscriptionID) ||
		!strings.EqualFold(plsID.ResourceGroupName, nodeGroup) {
		return false, fmt.Errorf("companion PLS is outside the approved subscription/node group")
	}
	if checkpoint.targetID != "" && !strings.EqualFold(checkpoint.targetID, origin.PrivateLinkResourceID) {
		return false, fmt.Errorf("approval target changed")
	}
	checkpoint.targetID = origin.PrivateLinkResourceID
	var hub corev1.Namespace
	if err := c.Get(ctx, client.ObjectKey{Name: metav1.NamespaceSystem}, &hub); err != nil {
		return false, err
	}
	owner := frontdoor.GatewayOwner{HubUID: string(hub.UID), Namespace: key.Namespace, Name: key.Name, UID: checkpoint.gatewayUID}
	roots, err := frontdoor.BuildRootRequests(scope, owner)
	if err != nil {
		return false, err
	}
	profile, err := profiles.Get(ctx, scope.ResourceGroup, roots.ProfileName, nil)
	if azureerrors.IsNotFound(err) {
		checkpoint.progress(target, "waiting for owned AFD profile "+roots.ProfileName)
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := frontdoor.VerifyProfileOwnership(scope, owner, profile.Profile); err != nil {
		return false, err
	}
	pls, err := services.Get(ctx, plsID.ResourceGroupName, plsID.Name, nil)
	if err != nil {
		return false, err
	}
	connection, err := harness.ApprovalConnection(pls.PrivateLinkService, checkpoint.targetID, "Fleet AFD private origin for "+roots.ProfileName, checkpoint.connectionID)
	if err != nil || connection == nil {
		if err == nil {
			checkpoint.progress(target, "waiting for correlated private endpoint request on "+checkpoint.targetID)
		}
		return false, err
	}
	checkpoint.connectionID = *connection.ID
	if *connection.Properties.PrivateLinkServiceConnectionState.Status == "Pending" {
		fmt.Fprintf(os.Stderr, "Approving exact test connection %s for verified profile %s\n", checkpoint.connectionID, roots.ProfileID)
		_, err := services.UpdatePrivateEndpointConnection(ctx, plsID.ResourceGroupName, plsID.Name, *connection.Name,
			armnetwork.PrivateEndpointConnection{Properties: &armnetwork.PrivateEndpointConnectionProperties{
				PrivateLinkServiceConnectionState: &armnetwork.PrivateLinkServiceConnectionState{
					Status: ptr.To("Approved"), Description: connection.Properties.PrivateLinkServiceConnectionState.Description,
				},
			}}, nil)
		return false, err
	}
	checkpoint.progress(target, "exact private endpoint connection approved")
	return true, nil
}

func namespacesForFixtures(fixtures []unstructured.Unstructured) []string {
	var namespaces []string
	for _, fixture := range fixtures {
		if fixture.GetKind() == "Namespace" {
			namespaces = append(namespaces, fixture.GetName())
		}
	}
	return namespaces
}

func captureDiagnostics(t *testing.T, c client.Client, testName string, namespaces []string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	data, err := harness.FixtureDiagnostics(ctx, c, namespaces)
	t.Logf("Pre-teardown fixture snapshot:\n%s", data)
	if err != nil {
		t.Errorf("fixture diagnostics failed: %v", err)
	}
	dir := os.Getenv("LOG_DIR")
	if dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Errorf("create diagnostic directory: %v", err)
		} else if err := os.WriteFile(filepath.Join(dir, testName+"-fixtures.json"), data, 0600); err != nil {
			t.Errorf("write fixture snapshot: %v", err)
		}
	}
	namespace := os.Getenv("AFD_PROOF_CONTROLLER_NAMESPACE")
	if namespace == "" {
		namespace = "fleet-system"
	}
	logs, err := exec.CommandContext(ctx, "kubectl", "--context", *kubeContext, "-n", namespace, "logs",
		"-l", "app.kubernetes.io/name=hub-gateway-controller-manager", "--all-containers=true",
		"--prefix=true", "--tail=2000", "--max-log-requests=5", "--request-timeout=30s").CombinedOutput()
	if err != nil {
		t.Errorf("capture Gateway-manager logs: %v: %s", err, logs)
	}
	if dir == "" {
		t.Logf("Gateway-manager logs:\n%s", logs)
	} else if err := os.WriteFile(filepath.Join(dir, testName+"-gateway-manager.log"), logs, 0600); err != nil {
		t.Errorf("write Gateway-manager logs: %v", err)
	}
}

func warmTarget(ctx context.Context, c client.Client, httpClient *http.Client, target harness.OriginTarget) (bool, error) {
	var gateway gatewayv1.Gateway
	if err := c.Get(ctx, client.ObjectKey(target.Gateway), &gateway); err != nil {
		return false, err
	}
	if len(gateway.Status.Addresses) != 1 || gateway.Status.Addresses[0].Type == nil ||
		*gateway.Status.Addresses[0].Type != gatewayv1.HostnameAddressType {
		return false, nil
	}
	host := gateway.Status.Addresses[0].Value
	if len(validation.IsDNS1123Subdomain(host)) != 0 || !strings.HasSuffix(host, ".azurefd.net") {
		return false, fmt.Errorf("unexpected test Gateway hostname")
	}
	var route gatewayv1.HTTPRoute
	if err := c.Get(ctx, client.ObjectKey(target.Route), &route); err != nil {
		return false, err
	}
	var namespace corev1.Namespace
	if err := c.Get(ctx, client.ObjectKey{Name: route.Namespace}, &namespace); err != nil {
		return false, err
	}
	request, err := warmRequest(ctx, host, &gateway, &route, target, namespace.Labels)
	if err != nil {
		return false, err
	}
	response, err := httpClient.Do(request)
	if err != nil {
		fmt.Fprintf(os.Stderr, "waiting for AFD propagation: %v\n", err)
		return false, nil
	}
	if err := response.Body.Close(); err != nil {
		return false, err
	}
	if response.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "Waiting for private HTTP delivery: status=%d\n", response.StatusCode)
	}
	return response.StatusCode == http.StatusOK, nil
}

func warmRequest(ctx context.Context, address string, gateway *gatewayv1.Gateway, route *gatewayv1.HTTPRoute, target harness.OriginTarget, namespaceLabels map[string]string) (*http.Request, error) {
	if target.RuleIndex < 0 || target.RuleIndex >= len(route.Spec.Rules) {
		return nil, fmt.Errorf("fixture route rules changed during warmup")
	}
	attachments, err := gatewayresolver.ResolveAttachments(gateway, route, target.Parent, namespaceLabels)
	if err != nil {
		return nil, err
	}
	var listener gatewayv1.Listener
	for _, candidate := range gateway.Spec.Listeners {
		if candidate.Name == gatewayv1.SectionName(attachments[0].Listener) {
			listener = candidate
			break
		}
	}
	if listener.Protocol != gatewayv1.HTTPProtocolType {
		return nil, fmt.Errorf("HTTPS fixture warmup requires explicit certificate/domain setup")
	}
	match := gatewayv1.HTTPRouteMatch{}
	if matches := route.Spec.Rules[target.RuleIndex].Matches; len(matches) != 0 {
		match = matches[0]
	}
	path := "/"
	if match.Path != nil {
		if ptr.Deref(match.Path.Type, gatewayv1.PathMatchPathPrefix) == gatewayv1.PathMatchRegularExpression {
			return nil, fmt.Errorf("regex fixtures require an explicit warmup request")
		}
		path = ptr.Deref(match.Path.Value, "/")
	}
	request, err := http.NewRequestWithContext(ctx, string(ptr.Deref(match.Method, gatewayv1.HTTPMethod(http.MethodGet))),
		"http://"+net.JoinHostPort(address, fmt.Sprint(listener.Port))+path, nil)
	if err != nil {
		return nil, err
	}
	for _, header := range match.Headers {
		if ptr.Deref(header.Type, gatewayv1.HeaderMatchExact) != gatewayv1.HeaderMatchExact {
			return nil, fmt.Errorf("regex headers require an explicit warmup request")
		}
		request.Header.Set(string(header.Name), header.Value)
	}
	query := request.URL.Query()
	for _, parameter := range match.QueryParams {
		if ptr.Deref(parameter.Type, gatewayv1.QueryParamMatchExact) != gatewayv1.QueryParamMatchExact {
			return nil, fmt.Errorf("regex query parameters require an explicit warmup request")
		}
		query.Set(string(parameter.Name), parameter.Value)
	}
	request.URL.RawQuery = query.Encode()
	if hosts := attachments[0].Hostnames; len(hosts) != 0 {
		request.Host = hosts[0]
	}
	if strings.HasPrefix(request.Host, "*.") {
		request.Host = "probe." + strings.TrimPrefix(request.Host, "*.")
	}
	return request, nil
}
