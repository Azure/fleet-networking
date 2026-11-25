/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gateway

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha3 "sigs.k8s.io/gateway-api/apis/v1alpha3"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	fleetnetv1beta1 "go.goms.io/fleet-networking/api/v1beta1"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/common/origintls"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewayresolver"
	"go.goms.io/fleet-networking/pkg/providers/azure/frontdoor"
)

var _ = Describe("HTTPS intent with real CRD admission and dependency watches", func() {
	It("projects current intent, observes metadata heartbeats and revokes member opt-in without an HTTP fallback", func() {
		ctx := integrationContext
		integrationProvider.setOriginState(frontdoor.PrivateOriginReady)
		namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "https-integration-"}}
		Expect(integrationClient.Create(ctx, namespace)).To(Succeed())
		member := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "fleet-member-https-envtest"}}
		Expect(integrationClient.Create(ctx, member)).To(Succeed())
		class := &gatewayv1.GatewayClass{ObjectMeta: metav1.ObjectMeta{GenerateName: "https-integration-"},
			Spec: gatewayv1.GatewayClassSpec{ControllerName: gatewayv1.GatewayController(objectmeta.GatewayAPIControllerName)}}
		Expect(integrationClient.Create(ctx, class)).To(Succeed())
		service := &fleetnetv1alpha1.ServiceImport{ObjectMeta: metav1.ObjectMeta{Namespace: namespace.Name, Name: "api"}}
		Expect(integrationClient.Create(ctx, service)).To(Succeed())
		service.Status.Ports = []fleetnetv1alpha1.ServicePort{{Name: "https", Protocol: corev1.ProtocolTCP, Port: 443, AppProtocol: ptr.To("https")}}
		service.Status.Clusters = []fleetnetv1alpha1.ClusterStatus{{Cluster: "https-envtest"}}
		Expect(integrationClient.Status().Update(ctx, service)).To(Succeed())
		export := &fleetnetv1alpha1.InternalServiceExport{
			ObjectMeta: metav1.ObjectMeta{Namespace: member.Name, Name: namespace.Name + "-api"},
			Spec: fleetnetv1alpha1.InternalServiceExportSpec{
				Ports: service.Status.Ports, Type: corev1.ServiceTypeLoadBalancer, IsInternalLoadBalancer: true,
				ServiceReference: fleetnetv1alpha1.ExportedObjectReference{Namespace: namespace.Name, Name: "api", NamespacedName: namespace.Name + "/api",
					ClusterID: "https-envtest", UID: "source-service", Kind: "Service", APIVersion: "v1"},
				PrivateLinkOrigin: &fleetnetv1alpha1.PrivateLinkOrigin{ResourceID: integrationPLS, Location: "eastus", InternalIP: "10.0.0.10", Ready: true},
			},
		}
		intent := &fleetnetv1alpha1.OriginTLS{Ready: true, Reason: "Ready", ObservedAt: metav1.Now(), ServiceUID: "source-service",
			Hostname: "origin.example.com", Trust: "System",
			Policy: &fleetnetv1alpha1.OriginTLSPolicyReference{Namespace: namespace.Name, Name: "api-tls", UID: "source-policy", Generation: 1, ResourceVersion: "opaque", ExportToFleet: "true"}}
		Expect(origintls.RefreshObservation(export, intent)).To(Succeed())
		Expect(integrationClient.Create(ctx, export)).To(Succeed())
		export.Status.Conditions = []metav1.Condition{{Type: string(fleetnetv1beta1.ServiceExportConflict), Status: metav1.ConditionFalse,
			Reason: "NoConflictFound", Message: "Ports agree", ObservedGeneration: export.Generation, LastTransitionTime: metav1.Now()}}
		Expect(integrationClient.Status().Update(ctx, export)).To(Succeed())
		gateway := &gatewayv1.Gateway{ObjectMeta: metav1.ObjectMeta{Namespace: namespace.Name, Name: "https"},
			Spec: gatewayv1.GatewaySpec{GatewayClassName: gatewayv1.ObjectName(class.Name), Listeners: []gatewayv1.Listener{{
				Name: "https", Protocol: gatewayv1.HTTPSProtocolType, Port: 443,
				TLS: &gatewayv1.GatewayTLSConfig{Mode: ptr.To(gatewayv1.TLSModeTerminate), Options: map[gatewayv1.AnnotationKey]gatewayv1.AnnotationValue{
					gatewayv1.AnnotationKey(origintls.DefaultCertificateOption): "true",
				}},
			}}}}
		Expect(integrationClient.Create(ctx, gateway)).To(Succeed(), "Pinned schema must admit implementation-managed default certificate")
		route := &gatewayv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Namespace: namespace.Name, Name: "api"},
			Spec: gatewayv1.HTTPRouteSpec{CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gatewayv1.ParentReference{{Name: "https"}}},
				Rules: []gatewayv1.HTTPRouteRule{{BackendRefs: []gatewayv1.HTTPBackendRef{{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{
					Group: ptr.To(gatewayv1.Group(fleetnetv1alpha1.GroupVersion.Group)), Kind: ptr.To(gatewayv1.Kind("ServiceImport")), Name: "api", Port: ptr.To(gatewayv1.PortNumber(443)),
				}}}}}},
			}}
		Expect(integrationClient.Create(ctx, route)).To(Succeed())
		programmed := func() error {
			var current gatewayv1.Gateway
			if err := integrationClient.Get(ctx, client.ObjectKeyFromObject(gateway), &current); err != nil {
				return err
			}
			condition := meta.FindStatusCondition(current.Status.Conditions, "Programmed")
			if condition == nil || condition.Status != metav1.ConditionTrue || len(current.Status.Addresses) != 1 {
				return fmt.Errorf("waiting for admitted HTTPS projection: %+v", current.Status)
			}
			return nil
		}
		Eventually(programmed, 20*time.Second, 100*time.Millisecond).Should(Succeed())
		var policy gatewayv1alpha3.BackendTLSPolicy
		key := client.ObjectKey{Namespace: namespace.Name, Name: gatewayresolver.TLSProjectionName(service.Name)}
		Eventually(func(g Gomega) {
			g.Expect(integrationClient.Get(ctx, key, &policy)).To(Succeed())
			g.Expect(policy.Status.Ancestors).To(HaveLen(1))
		}, 10*time.Second, 100*time.Millisecond).Should(Succeed())
		Expect(policy.OwnerReferences[0].UID).To(Equal(service.UID))
		generation := export.Generation
		renewed := intent.DeepCopy()
		renewed.ObservedAt = metav1.NewTime(time.Now().Add(time.Second))
		Expect(origintls.RefreshObservation(export, renewed)).To(Succeed())
		Expect(integrationClient.Update(ctx, export)).To(Succeed())
		Expect(export.Generation).To(Equal(generation), "Heartbeat must preserve MCS conflict generation")
		Eventually(programmed, 10*time.Second, 100*time.Millisecond).Should(Succeed())
		renewed.Ready, renewed.Reason, renewed.Policy.ExportToFleet = false, "PolicyNotExported", "false"
		Expect(origintls.RefreshObservation(export, renewed)).To(Succeed())
		Expect(integrationClient.Update(ctx, export)).To(Succeed())
		Eventually(func() error {
			var current gatewayv1.Gateway
			if err := integrationClient.Get(ctx, client.ObjectKeyFromObject(gateway), &current); err != nil {
				return err
			}
			condition := meta.FindStatusCondition(current.Status.Conditions, "Programmed")
			if condition == nil || condition.Status != metav1.ConditionFalse || len(current.Status.Addresses) != 0 {
				return fmt.Errorf("stale TLS authority survived revocation")
			}
			err := integrationClient.Get(ctx, key, &policy)
			if err == nil {
				return fmt.Errorf("revoked projection retained")
			}
			if !apierrors.IsNotFound(err) {
				return fmt.Errorf("read revoked projection: %w", err)
			}
			return nil
		}, 10*time.Second, 100*time.Millisecond).Should(Succeed())
	})
})
