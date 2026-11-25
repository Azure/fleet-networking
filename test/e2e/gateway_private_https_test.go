//go:build e2e

/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v9"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayv1alpha3 "sigs.k8s.io/gateway-api/apis/v1alpha3"

	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/common/origintls"
	"go.goms.io/fleet-networking/pkg/common/uniquename"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewayresolver"
	"go.goms.io/fleet-networking/pkg/providers/azure/frontdoor"
	"go.goms.io/fleet-networking/test/e2e/framework"
	tlsfixture "go.goms.io/fleet-networking/test/fixtures/origintls"
)

//go:embed fixtures/https_server.py
var privateHTTPSServer string

var _ = Describe("Gateway private HTTPS proof", Serial, Label("afd-private-https-proof"), func() {
	It("proves both TLS hops, probes, member-local revocation, no HTTP fallback and cleanup using installed controllers", func(ctx SpecContext) {
		if os.Getenv("ENABLE_AFD_PRIVATE_HTTPS_PROOF") != "true" {
			Skip("Explicit live authorization and ENABLE_AFD_PRIVATE_HTTPS_PROOF=true are required")
		}
		Expect(os.Getenv("ENABLE_ORIGIN_TLS")).To(Equal("true"))
		Expect(os.Getenv("ENABLE_AZURE_FRONT_DOOR")).To(Equal("true"))
		Expect(retainedAFDSetup).To(BeFalse(), "TLS proof does not support retained resources or secret fixtures")
		fixture, err := tlsfixture.FromEnv(os.Getenv)
		Expect(err).NotTo(HaveOccurred(), "AKV CSI fixture inputs must be explicitly configured")
		hostname := fixture.Hostname
		preflightPrivateHTTPSCSI(ctx, fixture.Namespace)
		waitPrivateHTTPController(ctx)
		credential, err := azidentity.NewDefaultAzureCredential(nil)
		Expect(err).NotTo(HaveOccurred())
		scope := frontdoor.ResourceScope{SubscriptionID: subscriptionID, ResourceGroup: atmResourceGroup}
		verifier, err := frontdoor.NewRouteProvisioner(scope, credential, nil)
		Expect(err).NotTo(HaveOccurred())
		profiles, err := armcdn.NewProfilesClient(scope.SubscriptionID, credential, nil)
		Expect(err).NotTo(HaveOccurred())
		services, err := armnetwork.NewPrivateLinkServicesClient(scope.SubscriptionID, credential, nil)
		Expect(err).NotTo(HaveOccurred())
		service := createPrivateHTTPSWorkload(ctx, fixture)
		class := framework.GatewayClass("https-proof-"+service.Namespace, gatewayControllerName)
		Expect(hubCluster.Client().Create(ctx, class)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) {
			Expect(client.IgnoreNotFound(hubCluster.Client().Delete(ctx, class))).To(Succeed())
		}, NodeTimeout(time.Minute))
		gateway := framework.Gateway(service.Namespace, "private-https", class.Name, "", nil)
		gateway.Spec.Listeners = []gatewayv1.Listener{{Name: "https", Protocol: gatewayv1.HTTPSProtocolType, Port: 443,
			TLS: &gatewayv1.GatewayTLSConfig{Mode: ptr.To(gatewayv1.TLSModeTerminate), Options: map[gatewayv1.AnnotationKey]gatewayv1.AnnotationValue{
				gatewayv1.AnnotationKey(origintls.DefaultCertificateOption): "true",
			}}}}
		Expect(hubCluster.Client().Create(ctx, gateway)).To(Succeed())
		var identity corev1.Namespace
		Expect(hubCluster.Client().Get(ctx, client.ObjectKey{Name: metav1.NamespaceSystem}, &identity)).To(Succeed())
		owner := frontdoor.GatewayOwner{HubUID: string(identity.UID), Namespace: gateway.Namespace, Name: gateway.Name, UID: string(gateway.UID)}
		roots, err := frontdoor.BuildRootRequests(scope, owner)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func(ctx SpecContext) { cleanupPrivateHTTPGateway(ctx, gateway, scope, owner, roots, profiles) }, NodeTimeout(privateHTTPCleanupNodeTimeout))
		route := framework.HTTPRouteToFleetServiceImport(service.Namespace, "private-https", gateway.Name, "", service.Name, 443, 1)
		route.Spec.Rules[0].Matches = []gatewayv1.HTTPRouteMatch{{Path: &gatewayv1.HTTPPathMatch{Type: ptr.To(gatewayv1.PathMatchPathPrefix), Value: ptr.To("/")}}}
		Expect(hubCluster.Client().Create(ctx, route)).To(Succeed())
		model := waitPrivateHTTPModel(ctx, gateway, route, service.Name, credential, true)
		plan, err := frontdoor.PlanBackends(model)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func(ctx SpecContext) {
			if CurrentSpecReport().Failed() {
				reportPrivateHTTPDiagnostics(ctx, "HTTPS proof failure", scope, owner, plan, credential, gateway)
			}
		}, NodeTimeout(privateHTTPDiagnosticTimeout+time.Minute))
		waitPrivateHTTPPhase(ctx, "HTTPS exact private-link approvals", func(ctx context.Context) error {
			requests, err := collectPrivateHTTPApprovalRequests(ctx, services, scope, roots, service.Namespace, plan.PrivateLinks)
			if err != nil {
				return err
			}
			if err := privateHTTPApprovalsComplete(requests); err != nil {
				if os.Getenv("ENABLE_AFD_PRIVATE_LINK_AUTO_APPROVAL") == "true" {
					return approvePrivateHTTPRequests(ctx, services, scope, roots, service.Namespace, plan.PrivateLinks, requests)
				}
				return err
			}
			return nil
		})
		var published string
		waitPrivateHTTPPhase(ctx, "HTTPS enabled graph", func(ctx context.Context) error {
			var current gatewayv1.Gateway
			if err := hubCluster.Client().Get(ctx, client.ObjectKeyFromObject(gateway), &current); err != nil {
				return err
			}
			host, err := privateHTTPGatewayState(&current, true, 1)
			if err != nil {
				return err
			}
			observed, err := verifier.VerifyEnabledHTTPPreview(ctx, owner, model)
			if err != nil {
				return err
			}
			if observed != host {
				return fmt.Errorf("published hostname differs from the observed owned endpoint")
			}
			published = host
			return nil
		})
		members := make(map[string]bool)
		for _, member := range memberClusters {
			members[member.Name()] = true
		}
		httpClient := privateHTTPClient()
		checkTraffic := func(expected map[string]bool) error {
			nonce := uniquename.RandomLowerCaseAlphabeticString(24)
			reply, err := requestPrivateHTTP(ctx, httpClient, "https://"+published+"/", nonce)
			if err != nil {
				return err
			}
			if _, err := validatePrivateHTTPResponse(reply, service.Namespace, nonce, published, expected); err != nil {
				return err
			}
			var tls struct{ SNI, Protocol string }
			if err := json.Unmarshal(reply.Body, &tls); err != nil {
				return err
			}
			if tls.SNI != hostname || (tls.Protocol != "TLSv1.2" && tls.Protocol != "TLSv1.3") {
				return fmt.Errorf("application did not observe the selected SNI over TLS")
			}
			return nil
		}
		waitPrivateHTTPPhase(ctx, "verified end-to-end TLS traffic", func(context.Context) error { return checkTraffic(members) })
		for _, member := range memberClusters {
			waitPrivateHTTPSProbe(ctx, member, service, hostname)
		}
		Eventually(ctx, func() error {
			reply, err := requestPrivateHTTP(ctx, httpClient, "http://"+published+"/", uniquename.RandomLowerCaseAlphabeticString(24))
			if err != nil {
				return err
			}
			return validatePrivateHTTPBlocked(reply, service.Namespace)
		}, privateHTTPTrafficTimeout, privateHTTPPoll).Should(Succeed(), "HTTP must neither deliver the application nor redirect")

		By("revoking only one member's export marker without changing policy generation")
		excluded := memberClusters[0]
		var policy gatewayv1alpha3.BackendTLSPolicy
		policyKey := client.ObjectKey{Namespace: service.Namespace, Name: service.Name + "-tls"}
		Expect(excluded.Client().Get(ctx, policyKey, &policy)).To(Succeed())
		generation := policy.Generation
		delete(policy.Annotations, origintls.ExportAnnotation)
		Expect(excluded.Client().Update(ctx, &policy)).To(Succeed())
		Expect(policy.Generation).To(Equal(generation))
		delete(members, excluded.Name())
		waitPrivateHTTPPhase(ctx, "HTTPS isolated member absence", func(ctx context.Context) error {
			updated, err := resolvePrivateHTTPSModel(ctx, gateway, route)
			if err != nil {
				return err
			}
			backend := updated.Routes[0].Rules[0].Backends[0]
			if len(backend.Origins) != len(memberClusters)-1 || len(backend.ExcludedOrigins) != 1 {
				return fmt.Errorf("member-local TLS revocation is not observed")
			}
			if _, err := verifier.VerifyEnabledHTTPPreview(ctx, owner, updated); err != nil {
				return err
			}
			return checkTraffic(members)
		})
		Consistently(ctx, func() error { return checkTraffic(members) }, time.Minute, privateHTTPPoll).Should(Succeed())
		By("requiring global withdrawal once every member is ineligible")
		for _, member := range memberClusters[1:] {
			var policy gatewayv1alpha3.BackendTLSPolicy
			Expect(member.Client().Get(ctx, policyKey, &policy)).To(Succeed())
			delete(policy.Annotations, origintls.ExportAnnotation)
			Expect(member.Client().Update(ctx, &policy)).To(Succeed())
		}
		var blocked privateHTTPBlockWindow
		waitPrivateHTTPPhase(ctx, "HTTPS withdrawal", func(ctx context.Context) error {
			var current gatewayv1.Gateway
			if err := hubCluster.Client().Get(ctx, client.ObjectKeyFromObject(gateway), &current); err != nil {
				return err
			}
			if _, err := privateHTTPGatewayState(&current, false, 1); err != nil {
				return err
			}
			reply, err := requestPrivateHTTP(ctx, httpClient, "https://"+published+"/", uniquename.RandomLowerCaseAlphabeticString(24))
			if err == nil {
				err = validatePrivateHTTPBlocked(reply, service.Namespace)
			}
			return blocked.observe(time.Now(), err)
		})
		cleanupPrivateHTTPGateway(ctx, gateway, scope, owner, roots, profiles)
	}, SpecTimeout(privateHTTPSpecTimeout))
})

func preflightPrivateHTTPSCSI(ctx context.Context, namespace string) {
	GinkgoHelper()
	for _, cluster := range fleet.Clusters() {
		err := cluster.Client().Get(ctx, client.ObjectKey{Name: namespace}, &corev1.Namespace{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "HTTPS proof refuses an existing namespace or an unverifiable namespace inventory")
	}
	for _, member := range memberClusters {
		var driver storagev1.CSIDriver
		Expect(member.Client().Get(ctx, client.ObjectKey{Name: tlsfixture.CSIDriver}, &driver)).To(Succeed(), "Install Secrets Store CSI and the Azure provider before running the proof")
		tokenRequest := false
		for _, request := range driver.Spec.TokenRequests {
			tokenRequest = tokenRequest || request.Audience == tlsfixture.WorkloadIdentityAudience
		}
		Expect(tokenRequest).To(BeTrue(), "CSI must request tokens for the preauthorized member ServiceAccount")
		providers := &unstructured.UnstructuredList{}
		providers.SetAPIVersion(tlsfixture.CSIGroupVersion)
		providers.SetKind("SecretProviderClassList")
		Expect(member.Client().List(ctx, providers, client.InNamespace(namespace), client.Limit(1))).To(Succeed(), "Install the SecretProviderClass CRD before running the proof")
	}
}

func createPrivateHTTPSWorkload(ctx context.Context, fixture tlsfixture.Config) corev1.Service {
	GinkgoHelper()
	workload := framework.NewWorkloadManager(fleet)
	service := workload.Service()
	service.Namespace = fixture.Namespace
	service.Annotations = map[string]string{objectmeta.ServiceAnnotationAzureLoadBalancerInternal: "true",
		objectmeta.ServiceAnnotationPLSName: service.Namespace, "service.beta.kubernetes.io/azure-pls-create": "true"}
	service.Spec.Ports = []corev1.ServicePort{{Name: "https", Protocol: corev1.ProtocolTCP, Port: 443, TargetPort: intstr.FromInt32(8443), AppProtocol: ptr.To("https")}}
	for _, cluster := range fleet.Clusters() {
		cluster := cluster
		namespace := framework.Namespace(service.Namespace)
		Expect(cluster.Client().Create(ctx, namespace)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) {
			Expect(client.IgnoreNotFound(cluster.Client().Delete(ctx, namespace, client.Preconditions{UID: &namespace.UID}))).To(Succeed())
			Eventually(ctx, func() bool {
				return apierrors.IsNotFound(cluster.Client().Get(ctx, client.ObjectKeyFromObject(namespace), &corev1.Namespace{}))
			},
				privateHTTPCleanupTimeout, privateHTTPConvergencePoll).Should(BeTrue())
		}, NodeTimeout(privateHTTPCleanupNodeTimeout))
	}
	for _, member := range memberClusters {
		account, provider, err := fixture.Resources()
		Expect(err).NotTo(HaveOccurred())
		Expect(member.Client().Create(ctx, account)).To(Succeed())
		Expect(member.Client().Create(ctx, provider)).To(Succeed())
		config := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: service.Namespace, Name: "https-server"}, Data: map[string]string{"server.py": privateHTTPSServer}}
		Expect(member.Client().Create(ctx, config)).To(Succeed())
		deployment := workload.Deployment(member.Name()).DeepCopy()
		container := &deployment.Spec.Template.Spec.Containers[0]
		container.Command, container.Args = []string{"python", "-u", "/fixture/server.py"}, nil
		container.Env = append(container.Env, corev1.EnvVar{Name: "FLEET_PROOF_ID", Value: service.Namespace})
		container.VolumeMounts = []corev1.VolumeMount{{Name: "fixture", MountPath: "/fixture", ReadOnly: true}}
		deployment.Spec.Template.Spec.Volumes = []corev1.Volume{
			{Name: "fixture", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: config.Name}}}},
		}
		Expect(fixture.Configure(deployment)).To(Succeed())
		Expect(member.Client().Create(ctx, deployment)).To(Succeed())
		Expect(member.Client().Create(ctx, service.DeepCopy())).To(Succeed())
		policy := &gatewayv1alpha3.BackendTLSPolicy{ObjectMeta: metav1.ObjectMeta{Namespace: service.Namespace, Name: service.Name + "-tls",
			Annotations: map[string]string{origintls.ExportAnnotation: "true"}},
			Spec: gatewayv1alpha3.BackendTLSPolicySpec{
				TargetRefs: []gatewayv1alpha2.LocalPolicyTargetReferenceWithSectionName{{LocalPolicyTargetReference: gatewayv1alpha2.LocalPolicyTargetReference{Group: "", Kind: "Service", Name: gatewayv1.ObjectName(service.Name)}}},
				Validation: gatewayv1alpha3.BackendTLSPolicyValidation{Hostname: gatewayv1.PreciseHostname(fixture.Hostname), WellKnownCACertificates: ptr.To(gatewayv1alpha3.WellKnownCACertificatesSystem)},
			}}
		Expect(member.Client().Create(ctx, policy)).To(Succeed())
		Expect(member.Client().Create(ctx, framework.ServiceExport(service.Namespace, service.Name))).To(Succeed())
	}
	return service
}

func resolvePrivateHTTPSModel(ctx context.Context, gateway *gatewayv1.Gateway, route *gatewayv1.HTTPRoute) (gatewaymodel.GlobalGateway, error) {
	attachments, err := gatewayresolver.ResolveAttachments(gateway, route, route.Spec.ParentRefs[0], nil, true)
	if err != nil {
		return gatewaymodel.GlobalGateway{}, err
	}
	resolved, err := gatewayresolver.ResolveHTTPRouteUnindexed(ctx, hubCluster.Client(), route, attachments, true)
	if err != nil {
		return gatewaymodel.GlobalGateway{}, err
	}
	return gatewaymodel.Normalize(gatewaymodel.GlobalGateway{Namespace: gateway.Namespace, Name: gateway.Name, UID: string(gateway.UID),
		Listeners: []gatewaymodel.Listener{{Name: "https", Protocol: "HTTPS", Port: 443, DefaultCertificate: true}}, Routes: []gatewaymodel.Route{resolved}})
}

func waitPrivateHTTPSProbe(ctx context.Context, member *framework.Cluster, service corev1.Service, hostname string) {
	GinkgoHelper()
	Eventually(ctx, func() error {
		var pods corev1.PodList
		if err := member.Client().List(ctx, &pods, client.InNamespace(service.Namespace), client.MatchingLabels(service.Spec.Selector)); err != nil {
			return err
		}
		for _, pod := range pods.Items {
			text, err := member.PodLogs(ctx, service.Namespace, pod.Name, &corev1.PodLogOptions{Container: "python", TailLines: ptr.To[int64](1000), LimitBytes: ptr.To[int64](privateHTTPMaxBody)})
			if err != nil {
				return err
			}
			for _, line := range strings.Split(text, "\n") {
				if !strings.HasPrefix(line, "{") {
					continue
				}
				var record struct {
					Event, SNI, Host, Protocol, Path string
					Probe                            bool
				}
				if err := json.Unmarshal([]byte(line), &record); err != nil {
					return err
				}
				if record.Event == "tls_request" && record.Probe && record.Path == "/" && record.SNI == hostname && record.Host != "" &&
					(record.Protocol == "TLSv1.2" || record.Protocol == "TLSv1.3") {
					AddReportEntry("HTTPS probe Host "+member.Name(), record.Host)
					return nil
				}
			}
		}
		return fmt.Errorf("waiting for real HTTPS probe with expected SNI")
	}, privateHTTPTrafficTimeout, privateHTTPPoll).Should(Succeed())
}
