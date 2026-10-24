//go:build e2e

/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"go.goms.io/fleet-networking/pkg/common/azureerrors"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/common/uniquename"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewayresolver"
	"go.goms.io/fleet-networking/pkg/providers/azure/frontdoor"
	"go.goms.io/fleet-networking/test/e2e/framework"
)

const (
	privateHTTPTimeout            = 10 * time.Minute
	privateHTTPPoll               = 5 * time.Second
	privateHTTPConvergenceTimeout = 45 * time.Minute
	privateHTTPConvergencePoll    = 15 * time.Second
	privateHTTPCleanupTimeout     = 45 * time.Minute
	privateHTTPCleanupNodeTimeout = privateHTTPCleanupTimeout + time.Minute
	privateHTTPSpecTimeout        = 6 * time.Hour
)

var _ = Describe("Gateway private HTTP proof", Serial, Label("afd-private-http-proof"), func() {
	It("proves controller-owned private delivery, observed probes, withdrawal and cleanup", func(ctx SpecContext) {
		if os.Getenv("ENABLE_AFD_PRIVATE_HTTP_PROOF") != privateHTTPEnabled {
			Skip("Explicit live authorization and ENABLE_AFD_PRIVATE_HTTP_PROOF=true are required")
		}
		Expect(os.Getenv("ENABLE_GATEWAY_API")).To(Equal(privateHTTPEnabled), "Install and enable the Gateway controller first")
		Expect(enabledNetworkingFeatures).To(BeTrue(), "Private member discovery must be enabled")
		autoApproval := os.Getenv("ENABLE_AFD_PRIVATE_LINK_AUTO_APPROVAL")
		Expect(autoApproval == "" || autoApproval == privateHTTPDisabled || autoApproval == privateHTTPEnabled).To(BeTrue(),
			"ENABLE_AFD_PRIVATE_LINK_AUTO_APPROVAL must be true or false")
		waitPrivateHTTPController(ctx)
		credential, err := azidentity.NewDefaultAzureCredential(nil)
		Expect(err).NotTo(HaveOccurred())
		scope := frontdoor.ResourceScope{SubscriptionID: subscriptionID, ResourceGroup: atmResourceGroup}
		profiles, err := armcdn.NewProfilesClient(scope.SubscriptionID, credential, nil)
		Expect(err).NotTo(HaveOccurred())
		verifier, err := frontdoor.NewRouteProvisioner(scope, credential, nil)
		Expect(err).NotTo(HaveOccurred())

		By("creating real member workloads with only internal LB/PLS Services")
		service := createPrivateHTTPWorkload(ctx)
		class := framework.GatewayClass("afd-proof-"+service.Namespace, gatewayControllerName)
		Expect(hubCluster.Client().Create(ctx, class)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) {
			Expect(client.IgnoreNotFound(hubCluster.Client().Delete(ctx, class))).To(Succeed())
		}, NodeTimeout(time.Minute))
		gateway := framework.Gateway(service.Namespace, "private-http", class.Name, "", nil)
		gateway.Spec.Listeners[0].Hostname = nil
		Expect(hubCluster.Client().Create(ctx, gateway)).To(Succeed())
		var hubIdentity corev1.Namespace
		Expect(hubCluster.Client().Get(ctx, client.ObjectKey{Name: metav1.NamespaceSystem}, &hubIdentity)).To(Succeed())
		owner := frontdoor.GatewayOwner{HubUID: string(hubIdentity.UID), Namespace: gateway.Namespace,
			Name: gateway.Name, UID: string(gateway.UID)}
		roots, err := frontdoor.BuildRootRequests(scope, owner)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func(ctx SpecContext) {
			cleanupPrivateHTTPGateway(ctx, gateway, scope, owner, roots, profiles)
		}, NodeTimeout(privateHTTPCleanupNodeTimeout))

		By("proving the unique owned profile was absent before the route existed")
		readCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		_, err = profiles.Get(readCtx, scope.ResourceGroup, roots.ProfileName, nil)
		cancel()
		Expect(azureerrors.IsNotFound(err)).To(BeTrue(), "An existing profile is not controller-creation evidence: %v", err)
		route := framework.HTTPRouteToFleetServiceImport(service.Namespace, "private-http", gateway.Name, "", service.Name, 80, 1)
		route.Spec.Rules[0].Matches = []gatewayv1.HTTPRouteMatch{{Path: &gatewayv1.HTTPPathMatch{
			Type: ptr.To(gatewayv1.PathMatchPathPrefix), Value: ptr.To("/"),
		}}}
		Expect(hubCluster.Client().Create(ctx, route)).To(Succeed())
		model := waitPrivateHTTPModel(ctx, gateway, route, service.Name, credential)
		diagnosticPlan, err := frontdoor.PlanBackends(model)
		Expect(err).NotTo(HaveOccurred())
		recordDiagnostics := func(ctx context.Context, reason string) {
			reportPrivateHTTPDiagnostics(ctx, reason, scope, owner, diagnosticPlan, credential, gateway)
		}
		DeferCleanup(func(ctx SpecContext) {
			if CurrentSpecReport().Failed() {
				recordDiagnostics(ctx, "proof failure before fixture cleanup")
			}
		}, NodeTimeout(privateHTTPDiagnosticTimeout+time.Minute))
		if os.Getenv("ENABLE_AZURE_FRONT_DOOR") == privateHTTPEnabled {
			By("preparing only exact verified PLS Reader bindings for the installed hub identity")
			args := []string{"../scripts/afd-pls-reader.sh", service.Namespace}
			for _, member := range memberClusters {
				for _, origin := range model.Routes[0].Rules[0].Backends[0].Origins {
					if origin.Cluster == member.Name() {
						args = append(args, origin.PrivateLinkResourceID)
					}
				}
			}
			grantCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
			output, err := exec.CommandContext(grantCtx, "bash", args...).CombinedOutput()
			cancel()
			Expect(err).NotTo(HaveOccurred(), "Exact test PLS Reader preparation failed: %s", output)
		}
		observer, err := frontdoor.NewOriginObserver(scope, credential, nil)
		Expect(err).NotTo(HaveOccurred())
		services, err := armnetwork.NewPrivateLinkServicesClient(scope.SubscriptionID, credential, nil)
		Expect(err).NotTo(HaveOccurred())

		By("correlating every exact PLS request independently of nullable AFD approval status")
		var observed frontdoor.PrivateOriginsObservation
		var checkpoint []privateHTTPApprovalRequest
		var lastOriginProgress string
		var lastDiagnosticKey string
		waitPrivateHTTPPhase(ctx, "PLS request discovery", func(ctx context.Context) error {
			var err error
			observed, err = observer.Observe(ctx, owner, model)
			if err != nil {
				key := fmt.Sprintf("observer error %T", err)
				var responseError *azcore.ResponseError
				if errors.As(err, &responseError) {
					key = fmt.Sprintf("observer HTTP %d code=%s", responseError.StatusCode, responseError.ErrorCode)
				}
				if key != lastDiagnosticKey {
					recordDiagnostics(ctx, key)
					lastDiagnosticKey = key
				}
				return err
			}
			originProgress := fmt.Sprintf("%+v", observed.Origins)
			for _, origin := range observed.Origins {
				if origin.State == frontdoor.PrivateOriginUnknown && originProgress != lastDiagnosticKey {
					recordDiagnostics(ctx, originProgress)
					lastDiagnosticKey = originProgress
					break
				}
			}
			checkpoint, err = collectPrivateHTTPApprovalRequests(ctx, services, scope, roots, service.Namespace, diagnosticPlan.PrivateLinks)
			originErr := privateHTTPApprovalOrigins(observed, len(memberClusters))
			if err = errors.Join(originErr, err); err != nil {
				progress := fmt.Sprintf("%v; origins=%s; PLS=[%s]", err, originProgress, privateHTTPApprovalProgress(checkpoint))
				if progress != lastOriginProgress {
					fmt.Fprintf(GinkgoWriter, "AFD origin approval progress: %s\n", progress)
					lastOriginProgress = progress
				}
				return err
			}
			// Early manual approval may already have programmed the Gateway.
			if privateHTTPApprovalsComplete(checkpoint) != nil {
				var current gatewayv1.Gateway
				if err := hubCluster.Client().Get(ctx, client.ObjectKeyFromObject(gateway), &current); err != nil {
					return err
				}
				_, err = privateHTTPGatewayState(&current, false, 1)
			}
			return err
		})
		AddReportEntry("AFD origin approval observations", observed.Origins)
		AddReportEntry("AFD PLS approval checkpoint", privateHTTPApprovalProgress(checkpoint))
		fmt.Fprintf(GinkgoWriter, "PLS approval checkpoint captured for %s at %s.\nCorrelated requests: %s\n",
			roots.ProfileID, time.Now().UTC().Format(time.RFC3339Nano), privateHTTPApprovalProgress(checkpoint))

		if autoApproval == privateHTTPEnabled {
			By("temporarily approving only exact correlated E2E requests using the test caller")
		} else {
			fmt.Fprintln(GinkgoWriter, "Authorized PLS owners: approve only these requests now; automatic E2E approval is disabled.")
		}
		var lastApprovalProgress string
		waitPrivateHTTPPhase(ctx, "PLS approval observation", func(ctx context.Context) error {
			requests, err := collectPrivateHTTPApprovalRequests(ctx, services, scope, roots, service.Namespace, diagnosticPlan.PrivateLinks)
			if err != nil {
				return err
			}
			if err := privateHTTPApprovalIdentities(checkpoint, requests); err != nil {
				return err
			}
			progress := privateHTTPApprovalProgress(requests)
			if progress != lastApprovalProgress {
				fmt.Fprintf(GinkgoWriter, "PLS approval progress at %s: %s\n", time.Now().UTC().Format(time.RFC3339Nano), progress)
				lastApprovalProgress = progress
			}
			if err := privateHTTPApprovalsComplete(requests); err != nil {
				if autoApproval == privateHTTPEnabled {
					return approvePrivateHTTPRequests(ctx, services, scope, roots, service.Namespace, diagnosticPlan.PrivateLinks, checkpoint)
				}
				return err
			}
			return nil
		})
		AddReportEntry("All exact PLS approvals independently verified", time.Now().UTC().Format(time.RFC3339Nano))

		By("requiring current controller status and the complete owned enabled private graph")
		var hostname string
		waitPrivateHTTPPhase(ctx, "post-approval AFD convergence", func(ctx context.Context) error {
			var current gatewayv1.Gateway
			if err := hubCluster.Client().Get(ctx, client.ObjectKeyFromObject(gateway), &current); err != nil {
				return err
			}
			published, err := privateHTTPGatewayState(&current, true, 1)
			if err != nil {
				return err
			}
			observed, err := verifier.VerifyEnabledHTTPPreview(ctx, owner, model)
			if err != nil {
				return err
			}
			if published != observed {
				return fmt.Errorf("Gateway address %q differs from observed Azure hostname %q", published, observed)
			}
			var currentRoute gatewayv1.HTTPRoute
			if err := hubCluster.Client().Get(ctx, client.ObjectKeyFromObject(route), &currentRoute); err != nil {
				return err
			}
			attached := false
			for _, parent := range currentRoute.Status.Parents {
				key, recognized := gatewayresolver.GatewayParentKey(currentRoute.Namespace, parent.ParentRef)
				if parent.ControllerName != gatewayControllerName || !recognized || key != client.ObjectKeyFromObject(gateway) {
					continue
				}
				for _, condition := range []string{privateHTTPAccepted, string(gatewayv1.RouteConditionResolvedRefs)} {
					if err := currentGatewayCondition(parent.Conditions, condition, metav1.ConditionTrue, condition, currentRoute.Generation); err != nil {
						return err
					}
				}
				attached = true
			}
			if !attached {
				return fmt.Errorf("route has no current Fleet parent status for this Gateway")
			}
			hostname = observed
			return nil
		})
		AddReportEntry("AFD owned enabled graph", model)

		By("requiring fresh application responses from both members with preserved Host")
		httpClient := privateHTTPClient()
		DeferCleanup(httpClient.CloseIdleConnections)
		members, seen := make(map[string]bool), make(map[string]bool)
		for _, member := range memberClusters {
			members[member.Name()] = true
		}
		Eventually(ctx, func() error {
			nonce := uniquename.RandomLowerCaseAlphabeticString(24)
			reply, err := requestPrivateHTTP(ctx, httpClient, "http://"+hostname+"/", nonce)
			if err != nil {
				return err
			}
			member, err := validatePrivateHTTPResponse(reply, service.Namespace, nonce, hostname, members)
			if err != nil {
				return err
			}
			if !seen[member] {
				AddReportEntry("private HTTP response "+member, string(reply.Body))
				seen[member] = true
			}
			if len(seen) != len(members) {
				return fmt.Errorf("only %d of %d eligible members answered", len(seen), len(members))
			}
			return nil
		}, privateHTTPTimeout, privateHTTPPoll).Should(Succeed())
		waitPrivateHTTPProbes(ctx, service)

		By("withdrawing the route and observing endpoint disablement, not just status")
		Expect(hubCluster.Client().Delete(ctx, route)).To(Succeed())
		endpoints, err := armcdn.NewAFDEndpointsClient(scope.SubscriptionID, credential, nil)
		Expect(err).NotTo(HaveOccurred())
		Eventually(ctx, func() error {
			var current gatewayv1.Gateway
			if err := hubCluster.Client().Get(ctx, client.ObjectKeyFromObject(gateway), &current); err != nil {
				return err
			}
			if _, err := privateHTTPGatewayState(&current, false, 0); err != nil {
				return err
			}
			readCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			actual, err := endpoints.Get(readCtx, scope.ResourceGroup, roots.ProfileName, roots.EndpointName, nil)
			if err != nil {
				return err
			}
			if actual.ID == nil || !strings.EqualFold(*actual.ID, roots.ProfileID+"/afdEndpoints/"+roots.EndpointName) ||
				actual.Properties == nil || ptr.Deref(actual.Properties.EnabledState, "") != armcdn.EnabledStateDisabled ||
				ptr.Deref(actual.Properties.ProvisioningState, "") != armcdn.AfdProvisioningStateSucceeded ||
				ptr.Deref(actual.Properties.DeploymentStatus, "") != armcdn.DeploymentStatusSucceeded {
				return fmt.Errorf("owned endpoint withdrawal is not observed deployed")
			}
			return nil
		}, privateHTTPConvergenceTimeout, privateHTTPConvergencePoll).Should(Succeed())
		blocked := func() error {
			reply, err := requestPrivateHTTP(ctx, httpClient, "http://"+hostname+"/", uniquename.RandomLowerCaseAlphabeticString(24))
			if err != nil {
				return err
			}
			return validatePrivateHTTPBlocked(reply, service.Namespace)
		}
		Eventually(ctx, blocked, privateHTTPConvergenceTimeout, privateHTTPPoll).Should(Succeed())
		Consistently(ctx, blocked, time.Minute, privateHTTPPoll).Should(Succeed(), "Network errors are not affirmative withdrawal evidence")
		AddReportEntry("AFD withdrawal", "Observed deployed disabled endpoint and sustained negative edge responses")

		By("requiring controller-owned profile cleanup and persisted Gateway removal")
		cleanupPrivateHTTPGateway(ctx, gateway, scope, owner, roots, profiles)
	}, SpecTimeout(privateHTTPSpecTimeout))
})

func waitPrivateHTTPPhase(ctx context.Context, name string, check func(context.Context) error) {
	GinkgoHelper()
	// Each sequential phase gets a fresh deadline; manual approval time must
	// not consume the propagation window after all approvals are observed.
	ctx, cancel := context.WithTimeout(ctx, privateHTTPConvergenceTimeout)
	defer cancel()
	start := time.Now()
	succeeded := false
	deadline, _ := ctx.Deadline()
	fmt.Fprintf(GinkgoWriter, "AFD phase %s started at %s; deadline=%s\n",
		name, start.UTC().Format(time.RFC3339Nano), deadline.UTC().Format(time.RFC3339Nano))
	defer func() {
		finished := time.Now()
		timing := map[string]interface{}{
			"phase": name, "startedUTC": start.UTC().Format(time.RFC3339Nano),
			"finishedUTC": finished.UTC().Format(time.RFC3339Nano), "elapsed": finished.Sub(start).String(),
			"deadlineUTC": deadline.UTC().Format(time.RFC3339Nano), "succeeded": succeeded,
		}
		AddReportEntry("AFD phase timing: "+name, timing)
		fmt.Fprintf(GinkgoWriter, "AFD phase %s finished at %s; elapsed=%s; succeeded=%t\n",
			name, finished.UTC().Format(time.RFC3339Nano), finished.Sub(start), succeeded)
	}()
	Eventually(ctx, func() error { return check(ctx) }, privateHTTPConvergenceTimeout, privateHTTPConvergencePoll).Should(Succeed())
	succeeded = true
}

func createPrivateHTTPWorkload(ctx context.Context) corev1.Service {
	GinkgoHelper()
	workload := framework.NewWorkloadManager(fleet)
	service := workload.Service()
	service.Annotations = map[string]string{
		objectmeta.ServiceAnnotationAzureLoadBalancerInternal: privateHTTPEnabled,
		objectmeta.ServiceAnnotationPLSName:                   service.Namespace,
		"service.beta.kubernetes.io/azure-pls-create":         privateHTTPEnabled,
	}
	for _, cluster := range fleet.Clusters() {
		cluster := cluster
		namespace := framework.Namespace(service.Namespace)
		Expect(cluster.Client().Create(ctx, namespace)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) {
			Expect(client.IgnoreNotFound(cluster.Client().Delete(ctx, namespace))).To(Succeed())
			Eventually(ctx, func() bool {
				return apierrors.IsNotFound(cluster.Client().Get(ctx, client.ObjectKeyFromObject(namespace), &corev1.Namespace{}))
			}, privateHTTPCleanupTimeout, privateHTTPConvergencePoll).Should(BeTrue(), "Namespace cleanup must retain Service/PLS finalizers")
		}, NodeTimeout(privateHTTPCleanupNodeTimeout))
	}
	for _, member := range memberClusters {
		deployment := workload.Deployment(member.Name()).DeepCopy()
		deployment.Spec.Template.Spec.Containers[0].Env = append(deployment.Spec.Template.Spec.Containers[0].Env,
			corev1.EnvVar{Name: "FLEET_PROOF_ID", Value: service.Namespace})
		Expect(member.Client().Create(ctx, deployment)).To(Succeed())
		memberService := service.DeepCopy()
		Expect(member.Client().Create(ctx, memberService)).To(Succeed())
		Expect(member.Client().Create(ctx, framework.ServiceExport(service.Namespace, service.Name))).To(Succeed())
	}
	return service
}

func waitPrivateHTTPModel(ctx context.Context, gateway *gatewayv1.Gateway, route *gatewayv1.HTTPRoute,
	serviceName string, credential azcore.TokenCredential) gatewaymodel.GlobalGateway {
	GinkgoHelper()
	var model gatewaymodel.GlobalGateway
	Eventually(ctx, func() error {
		attachments, err := gatewayresolver.ResolveAttachments(gateway, route, route.Spec.ParentRefs[0], nil)
		if err != nil {
			return err
		}
		resolved, err := gatewayresolver.ResolveHTTPRouteUnindexed(ctx, hubCluster.Client(), route, attachments)
		if err != nil {
			return err
		}
		origins := resolved.Rules[0].Backends[0].Origins
		if len(origins) != len(memberClusters) {
			return fmt.Errorf("expected private discovery from both real members, observed %d origins", len(origins))
		}
		for _, member := range memberClusters {
			var service corev1.Service
			if err := member.Client().Get(ctx, client.ObjectKey{Namespace: gateway.Namespace, Name: serviceName}, &service); err != nil {
				return err
			}
			found := false
			for _, origin := range origins {
				if origin.Cluster == member.Name() {
					if err := verifyPrivateHTTPFrontend(ctx, credential, &service, origin); err != nil {
						return err
					}
					found = true
				}
			}
			if !found {
				return fmt.Errorf("real member %s is missing from the resolved private graph", member.Name())
			}
		}
		model = gatewaymodel.GlobalGateway{Namespace: gateway.Namespace, Name: gateway.Name, UID: string(gateway.UID),
			Listeners: []gatewaymodel.Listener{{Name: privateHTTPListener, Protocol: "HTTP", Port: 80}},
			Routes:    []gatewaymodel.Route{resolved}}
		return nil
	}, privateHTTPTimeout, privateHTTPPoll).Should(Succeed())
	return model
}

func waitPrivateHTTPProbes(ctx context.Context, service corev1.Service) {
	GinkgoHelper()
	for _, member := range memberClusters {
		member := member
		Eventually(ctx, func() error {
			var pods corev1.PodList
			if err := member.Client().List(ctx, &pods, client.InNamespace(service.Namespace),
				client.MatchingLabels(service.Spec.Selector)); err != nil {
				return err
			}
			var logs strings.Builder
			for _, pod := range pods.Items {
				readCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
				text, err := member.PodLogs(readCtx, service.Namespace, pod.Name,
					&corev1.PodLogOptions{Container: "python", TailLines: ptr.To[int64](1000), LimitBytes: ptr.To[int64](privateHTTPMaxBody)})
				cancel()
				if err != nil {
					return err
				}
				logs.WriteString(text)
				logs.WriteByte('\n')
			}
			host, err := privateHTTPProbeEvidence(logs.String(), service.Namespace)
			if err != nil {
				return err
			}
			AddReportEntry("observed probe Host "+member.Name(), host)
			return nil
		}, privateHTTPTimeout, privateHTTPPoll).Should(Succeed())
	}
}

func cleanupPrivateHTTPGateway(ctx context.Context, gateway *gatewayv1.Gateway, scope frontdoor.ResourceScope,
	owner frontdoor.GatewayOwner, roots frontdoor.RootRequests, profiles *armcdn.ProfilesClient) {
	GinkgoHelper()
	Expect(client.IgnoreNotFound(hubCluster.Client().Delete(ctx, gateway, client.Preconditions{UID: &gateway.UID}))).To(Succeed())
	Eventually(ctx, func() error {
		readCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		profile, err := profiles.Get(readCtx, scope.ResourceGroup, roots.ProfileName, nil)
		if err == nil {
			if err := frontdoor.VerifyProfileOwnership(scope, owner, profile.Profile); err != nil {
				return err
			}
			return fmt.Errorf("owned AFD profile has not been observed absent")
		}
		if !azureerrors.IsNotFound(err) {
			return err
		}
		var current gatewayv1.Gateway
		err = hubCluster.Client().Get(ctx, client.ObjectKeyFromObject(gateway), &current)
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		return fmt.Errorf("Gateway cleanup finalizer has not completed persisted deletion")
	}, privateHTTPCleanupTimeout, privateHTTPConvergencePoll).Should(Succeed())
}

func reportPrivateHTTPDiagnostics(ctx context.Context, reason string, scope frontdoor.ResourceScope,
	owner frontdoor.GatewayOwner, plan frontdoor.BackendPlan, credential azcore.TokenCredential, gateway *gatewayv1.Gateway) {
	ctx, cancel := context.WithTimeout(ctx, privateHTTPDiagnosticTimeout+30*time.Second)
	defer cancel()
	snapshot, diagnosticErr := collectPrivateHTTPDiagnostics(ctx, scope, owner, plan, credential, nil)
	if diagnosticErr != nil && len(snapshot.Issues) == 0 {
		snapshot.Issues = append(snapshot.Issues, fmt.Sprintf("Azure diagnostics failed: %T", diagnosticErr))
	}
	// Collection issues are part of the report; diagnostics must not replace
	// the original convergence error or claim successful capture.
	var current gatewayv1.Gateway
	if err := hubCluster.Client().Get(ctx, client.ObjectKeyFromObject(gateway), &current); err != nil {
		snapshot.Issues = append(snapshot.Issues, fmt.Sprintf("Gateway status read failed: %T", err))
	} else if current.UID != gateway.UID {
		snapshot.Issues = append(snapshot.Issues, "Gateway status read returned a different owner UID")
	} else {
		snapshot.GatewayStatus = &current.Status
	}
	collectPrivateHTTPControllerDiagnostics(ctx, gateway, &snapshot)
	snapshot.Complete = len(snapshot.Issues) == 0
	AddReportEntry("AFD Unknown diagnostics: "+reason, snapshot)
	payload, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		fmt.Fprintf(GinkgoWriter, "AFD diagnostics encoding failed: %T\n", err)
		return
	}
	fmt.Fprintf(GinkgoWriter, "AFD Unknown diagnostics (%s):\n%s\n", reason, payload)
}

func collectPrivateHTTPControllerDiagnostics(ctx context.Context, gateway *gatewayv1.Gateway, snapshot *privateHTTPDiagnostics) {
	var deployment appsv1.Deployment
	key := client.ObjectKey{Namespace: os.Getenv("AFD_PROOF_CONTROLLER_NAMESPACE"), Name: os.Getenv("AFD_PROOF_CONTROLLER_DEPLOYMENT")}
	if err := hubCluster.Client().Get(ctx, key, &deployment); err != nil {
		snapshot.Issues = append(snapshot.Issues, fmt.Sprintf("Controller deployment read failed: %T", err))
		return
	}
	pods, err := privateHTTPControllerPods(ctx, hubCluster.Client(), &deployment)
	if err != nil {
		snapshot.Issues = append(snapshot.Issues, fmt.Sprintf("Controller owned-pod evidence unavailable: %T", err))
		return
	}
	if len(pods) == 0 {
		snapshot.Issues = append(snapshot.Issues, "Controller diagnostic capture found no owned pods")
		return
	}
	if len(pods) > 2 {
		snapshot.Issues = append(snapshot.Issues, "Controller diagnostic capture limited to two owned pods")
		pods = pods[:2]
	}
	for _, pod := range pods {
		logs, err := hubCluster.PodLogs(ctx, pod.Namespace, pod.Name,
			&corev1.PodLogOptions{Container: privateHTTPManager, TailLines: ptr.To[int64](100), LimitBytes: ptr.To[int64](32768)})
		if err != nil {
			snapshot.Issues = append(snapshot.Issues, fmt.Sprintf("Controller pod %s log read failed: %T", pod.Name, err))
			continue
		}
		if len(logs) >= 32768 {
			snapshot.Issues = append(snapshot.Issues, "Controller pod log capture may be byte-truncated: "+pod.Name)
		}
		records := projectPrivateHTTPControllerDiagnostics(logs, gateway.Namespace, gateway.Name, snapshot.Expected)
		for _, record := range records {
			record["pod"] = pod.Name
			record["podUID"] = pod.UID
		}
		snapshot.Controller = append(snapshot.Controller, records...)
	}
}

func privateHTTPControllerPods(ctx context.Context, c client.Client, deployment *appsv1.Deployment) ([]corev1.Pod, error) {
	if deployment.Spec.Selector == nil || len(deployment.Spec.Selector.MatchLabels) == 0 ||
		len(deployment.Spec.Selector.MatchExpressions) != 0 {
		return nil, fmt.Errorf("proof requires the chart's explicit controller label selector")
	}
	var pods corev1.PodList
	if err := c.List(ctx, &pods, client.InNamespace(deployment.Namespace),
		client.MatchingLabels(deployment.Spec.Selector.MatchLabels)); err != nil {
		return nil, err
	}
	owned := make([]corev1.Pod, 0, len(pods.Items))
	for _, pod := range pods.Items {
		for _, parent := range pod.OwnerReferences {
			if parent.Kind != "ReplicaSet" || !ptr.Deref(parent.Controller, false) {
				continue
			}
			var replicaSet appsv1.ReplicaSet
			if err := c.Get(ctx, client.ObjectKey{Namespace: deployment.Namespace, Name: parent.Name}, &replicaSet); err != nil {
				return nil, err
			}
			if replicaSet.UID != parent.UID {
				return nil, fmt.Errorf("controller pod ReplicaSet identity changed")
			}
			for _, owner := range replicaSet.OwnerReferences {
				if owner.Kind == "Deployment" && owner.UID == deployment.UID && ptr.Deref(owner.Controller, false) {
					owned = append(owned, pod)
				}
			}
		}
	}
	return owned, nil
}

func waitPrivateHTTPController(ctx context.Context) {
	GinkgoHelper()
	namespace, name, image := os.Getenv("AFD_PROOF_CONTROLLER_NAMESPACE"),
		os.Getenv("AFD_PROOF_CONTROLLER_DEPLOYMENT"), os.Getenv("AFD_PROOF_CONTROLLER_IMAGE")
	Expect(namespace).NotTo(BeEmpty(), "Select the installed chart Deployment namespace")
	Expect(name).NotTo(BeEmpty(), "Select the installed chart Deployment name")
	Expect(image).NotTo(BeEmpty(), "Select the exact controller image expected in that Deployment")
	Eventually(ctx, func() error {
		var deployment appsv1.Deployment
		if err := hubCluster.Client().Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &deployment); err != nil {
			return err
		}
		owned, err := privateHTTPControllerPods(ctx, hubCluster.Client(), &deployment)
		if err != nil {
			return err
		}
		images, err := privateHTTPControllerEvidence(&deployment, owned, image, atmResourceGroup)
		if err != nil {
			return err
		}
		AddReportEntry("installed controller runtime images", images)
		return nil
	}, framework.PollTimeout, privateHTTPPoll).Should(Succeed())
}
