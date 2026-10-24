/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package gateway

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	fleetnetv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
	fleetnetv1beta1 "go.goms.io/fleet-networking/api/v1beta1"
	"go.goms.io/fleet-networking/pkg/common/hubconfig"
	"go.goms.io/fleet-networking/pkg/common/objectmeta"
	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
	"go.goms.io/fleet-networking/pkg/providers/azure/frontdoor"
)

const (
	integrationPLS       = "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg/providers/Microsoft.Network/privateLinkServices/envtest"
	integrationClusterID = "envtest"
)

type integrationAFDProvider struct {
	mutex            sync.Mutex
	originState      frontdoor.PrivateOriginState
	absent           bool
	observations     int
	cleanups         int
	observationOwner frontdoor.GatewayOwner
	cleanupOwner     frontdoor.GatewayOwner
	model            gatewaymodel.GlobalGateway
	hold             *integrationObservationHold
}

type integrationObservationHold struct {
	entered  chan frontdoor.PrivateOriginsObservation
	released chan struct{}
	returned chan struct{}
	once     sync.Once
}

func (h *integrationObservationHold) unblock() {
	h.once.Do(func() { close(h.released) })
}

type integrationGatewayFixture struct {
	gateway *gatewayv1.Gateway
	export  *fleetnetv1alpha1.InternalServiceExport
	source  *corev1.Service
	service *fleetnetv1alpha1.ServiceImport
	route   *gatewayv1.HTTPRoute
	grant   *gatewayv1beta1.ReferenceGrant
}

func (p *integrationAFDProvider) Observe(ctx context.Context, owner frontdoor.GatewayOwner, model gatewaymodel.GlobalGateway) (frontdoor.PrivateOriginsObservation, error) {
	p.mutex.Lock()
	p.observations++
	p.observationOwner, p.model = owner, model
	state, hold := p.originState, p.hold
	p.hold = nil
	p.mutex.Unlock()
	if hold != nil {
		defer close(hold.returned)
	}
	if state == "" {
		return frontdoor.PrivateOriginsObservation{}, fmt.Errorf("integration origin state must be configured")
	}
	plan, err := frontdoor.PlanBackends(model)
	if err != nil {
		return frontdoor.PrivateOriginsObservation{}, err
	}
	result := frontdoor.PrivateOriginsObservation{}
	for _, group := range plan.OriginGroups {
		for _, origin := range group.Origins {
			result.Origins = append(result.Origins, frontdoor.PrivateOriginObservation{
				Reference: frontdoor.OriginReference{OriginGroup: group.Name, Origin: origin.Name},
				Target:    origin.PrivateLink, State: state,
			})
		}
	}
	result.OriginsConverged = len(result.Origins) != 0 && state == frontdoor.PrivateOriginReady
	if state == frontdoor.PrivateOriginApprovalPending {
		result.PendingApprovals = plan.PrivateLinks
	}
	if hold != nil {
		hold.entered <- result
		select {
		case <-ctx.Done():
			return frontdoor.PrivateOriginsObservation{}, ctx.Err()
		case <-hold.released:
		}
	}
	return result, nil
}

func (p *integrationAFDProvider) holdNextObservation(state frontdoor.PrivateOriginState) (*integrationObservationHold, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if p.hold != nil {
		return nil, fmt.Errorf("integration observation hold is already armed")
	}
	hold := &integrationObservationHold{
		entered:  make(chan frontdoor.PrivateOriginsObservation, 1),
		released: make(chan struct{}), returned: make(chan struct{}),
	}
	p.originState, p.hold = state, hold
	return hold, nil
}

func TestIntegrationObservationHold(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(fmt.Sprintf("canceled=%t", canceled), func(t *testing.T) {
			c, gateway, _, _ := gatewayFixture(t, 1)
			model := ownershipModel(t, c, gateway)
			provider := &integrationAFDProvider{}
			hold, err := provider.holdNextObservation(frontdoor.PrivateOriginRejected)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := provider.holdNextObservation(frontdoor.PrivateOriginReady); err == nil {
				t.Fatal("a second armed hold replaced the first")
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() {
				result, err := provider.Observe(ctx, frontdoor.GatewayOwner{}, model)
				if err == nil && (len(result.Origins) != 1 || result.Origins[0].State != frontdoor.PrivateOriginRejected || result.OriginsConverged) {
					err = fmt.Errorf("blocked observation did not retain its captured rejection: %+v", result)
				}
				done <- err
			}()
			t.Cleanup(func() {
				cancel()
				hold.unblock()
				select {
				case <-hold.returned:
				case <-time.After(5 * time.Second):
					t.Error("observer did not return during cleanup")
				}
			})
			select {
			case <-hold.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("observer did not enter the hold")
			}
			provider.setOriginState(frontdoor.PrivateOriginReady)
			select {
			case err := <-done:
				t.Fatalf("observer returned before release/cancellation: %v", err)
			default:
			}
			if canceled {
				cancel()
			} else {
				hold.unblock()
				hold.unblock()
			}
			select {
			case err := <-done:
				if (canceled && !errors.Is(err, context.Canceled)) || (!canceled && err != nil) {
					t.Fatalf("observation error = %v, canceled = %t", err, canceled)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("observer did not return after release/cancellation")
			}
			result, err := provider.Observe(context.Background(), frontdoor.GatewayOwner{}, model)
			if err != nil || !result.OriginsConverged || len(result.Origins) != 1 || result.Origins[0].State != frontdoor.PrivateOriginReady {
				t.Fatalf("one-shot hold affected the next observation: %+v, %v", result, err)
			}
		})
	}
}

func (p *integrationAFDProvider) DeleteProfile(_ context.Context, owner frontdoor.GatewayOwner) (bool, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.cleanups++
	p.cleanupOwner = owner
	return p.absent, nil
}

func (p *integrationAFDProvider) setOriginState(state frontdoor.PrivateOriginState) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.originState = state
}

func (p *integrationAFDProvider) observeAbsence() {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.absent = true
}

func (p *integrationAFDProvider) cleanupCount() int {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	return p.cleanups
}

func (p *integrationAFDProvider) observationCount() int {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	return p.observations
}

func (p *integrationAFDProvider) observedOrigin() (gatewaymodel.Origin, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if len(p.model.Routes) != 1 || len(p.model.Routes[0].Rules) != 1 ||
		len(p.model.Routes[0].Rules[0].Backends) != 1 || len(p.model.Routes[0].Rules[0].Backends[0].Origins) != 1 {
		return gatewaymodel.Origin{}, fmt.Errorf("expected one observed route/rule/backend/origin, got %+v", p.model)
	}
	return p.model.Routes[0].Rules[0].Backends[0].Origins[0], nil
}

var _ = Describe("Current Gateway runtime with a real API server", func() {
	It("polls approval, reacts to backend watches and finalizes only after observed absence", func() {
		integrationProvider.setOriginState(frontdoor.PrivateOriginApprovalPending)
		fixture := createIntegrationGateway()
		gateway, export, source := fixture.gateway, fixture.export, fixture.source
		key := client.ObjectKeyFromObject(gateway)
		message := func() (string, error) { return integrationGatewayMessage(key) }
		Eventually(func() error {
			return integrationRouteReferences(fixture.route, string(gatewayv1.RouteReasonResolvedRefs))
		}, 20*time.Second, 100*time.Millisecond).Should(Succeed())
		By("observing pending approval and persisting its native Event")
		Eventually(message, 20*time.Second, 100*time.Millisecond).Should(ContainSubstring("approval pending"))
		Eventually(func() (bool, error) {
			return integrationHasEvent(gateway, "AFDPrivateLinkApprovalRequired")
		}, 10*time.Second, 100*time.Millisecond).Should(BeTrue())
		var before gatewayv1.Gateway
		Expect(integrationClient.Get(integrationContext, key, &before)).To(Succeed())
		Expect(controllerutil.ContainsFinalizer(&before, objectmeta.GatewayAFDCleanupFinalizer)).To(BeFalse())

		By("changing only provider state, without a Kubernetes edit or direct Reconcile call")
		integrationProvider.setOriginState(frontdoor.PrivateOriginReady)
		Eventually(message, 50*time.Second, 100*time.Millisecond).Should(ContainSubstring("approved and deployed"))
		Eventually(func() (bool, error) {
			return integrationHasEvent(gateway, "AFDPrivateOriginsConverged")
		}, 10*time.Second, 100*time.Millisecond).Should(BeTrue())
		var current gatewayv1.Gateway
		Expect(integrationClient.Get(integrationContext, key, &current)).To(Succeed())
		Expect(current.Generation).To(Equal(before.Generation))
		Expect(current.Spec).To(Equal(before.Spec))
		integrationProvider.mutex.Lock()
		owner, model, observations := integrationProvider.observationOwner, integrationProvider.model, integrationProvider.observations
		integrationProvider.mutex.Unlock()
		Expect(observations).To(BeNumerically(">=", 2))
		Expect(owner).To(Equal(frontdoor.GatewayOwner{
			HubUID: string(integrationHub.UID), Namespace: gateway.Namespace, Name: gateway.Name, UID: string(gateway.UID),
		}))
		Expect(model.UID).To(Equal(string(gateway.UID)))
		Expect(model.Routes).To(HaveLen(1))
		Expect(model.Routes[0].Namespace).To(Equal(fixture.route.Namespace))
		Expect(model.Routes[0].Rules).To(HaveLen(1))
		Expect(model.Routes[0].Rules[0].Backends).To(HaveLen(1))
		Expect(model.Routes[0].Rules[0].Backends[0].Namespace).To(Equal(fixture.service.Namespace))
		Expect(model.Routes[0].Rules[0].Backends[0].Origins).To(HaveLen(1))
		Expect(model.Routes[0].Rules[0].Backends[0].Origins[0].ServiceUID).To(Equal(string(source.UID)))

		By("invalidating and restoring discovery through the actual export dependency watch")
		updateIntegrationDiscovery(export, false)
		Eventually(message, 10*time.Second, 100*time.Millisecond).Should(ContainSubstring("no ready private origin"))
		updateIntegrationDiscovery(export, true)
		Eventually(message, 10*time.Second, 100*time.Millisecond).Should(ContainSubstring("approved and deployed"))

		verifyIntegrationIdentityLifecycle(fixture)
		verifyIntegrationConvergenceLifecycle(fixture)
		verifyIntegrationStaleObservation(fixture)
		verifyIntegrationGrantLifecycle(fixture)

		By("modeling persisted cleanup ownership without injecting a graph writer")
		Expect(integrationClient.Get(integrationContext, key, &current)).To(Succeed())
		controllerutil.AddFinalizer(&current, objectmeta.GatewayAFDCleanupFinalizer)
		Expect(integrationClient.Update(integrationContext, &current)).To(Succeed())
		Expect(integrationClient.Delete(integrationContext, &current)).To(Succeed())
		Eventually(integrationProvider.cleanupCount, 10*time.Second, 100*time.Millisecond).Should(BeNumerically(">", 0))
		Consistently(func(g Gomega) {
			var deleting gatewayv1.Gateway
			g.Expect(integrationClient.Get(integrationContext, key, &deleting)).To(Succeed())
			g.Expect(deleting.DeletionTimestamp).NotTo(BeNil())
			g.Expect(controllerutil.ContainsFinalizer(&deleting, objectmeta.GatewayAFDCleanupFinalizer)).To(BeTrue())
		}, time.Second, 100*time.Millisecond).Should(Succeed())

		By("releasing the finalizer after a later provider absence observation")
		integrationProvider.observeAbsence()
		Eventually(func() error {
			var deleted gatewayv1.Gateway
			err := integrationClient.Get(integrationContext, key, &deleted)
			if apierrors.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return err
			}
			return fmt.Errorf("Gateway still exists with finalizers %v", deleted.Finalizers)
		}, 45*time.Second, 100*time.Millisecond).Should(Succeed())
		Eventually(func() (bool, error) {
			return integrationHasEvent(gateway, "AFDProfileCleanupComplete")
		}, 10*time.Second, 100*time.Millisecond).Should(BeTrue())
		integrationProvider.mutex.Lock()
		cleanupOwner := integrationProvider.cleanupOwner
		integrationProvider.mutex.Unlock()
		Expect(cleanupOwner).To(Equal(owner))
	})
})

func createIntegrationGateway() integrationGatewayFixture {
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "gateway-integration-"}}
	Expect(integrationClient.Create(integrationContext, namespace)).To(Succeed())
	routeNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "gateway-route-"}}
	Expect(integrationClient.Create(integrationContext, routeNamespace)).To(Succeed())
	member := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf(hubconfig.HubNamespaceNameFormat, integrationClusterID)}}
	Expect(integrationClient.Create(integrationContext, member)).To(Succeed())
	class := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "gateway-integration-"},
		Spec:       gatewayv1.GatewayClassSpec{ControllerName: gatewayv1.GatewayController(objectmeta.GatewayAPIControllerName)},
	}
	Expect(integrationClient.Create(integrationContext, class)).To(Succeed())
	source := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: namespace.Name, Name: "private-api"},
		Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}}, Type: corev1.ServiceTypeLoadBalancer}}
	Expect(integrationClient.Create(integrationContext, source)).To(Succeed())
	service := &fleetnetv1alpha1.ServiceImport{ObjectMeta: metav1.ObjectMeta{Namespace: namespace.Name, Name: source.Name}}
	Expect(integrationClient.Create(integrationContext, service)).To(Succeed())
	service.Status = fleetnetv1alpha1.ServiceImportStatus{
		Ports:    []fleetnetv1alpha1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}},
		Clusters: []fleetnetv1alpha1.ClusterStatus{{Cluster: integrationClusterID}},
	}
	Expect(integrationClient.Status().Update(integrationContext, service)).To(Succeed())
	export := &fleetnetv1alpha1.InternalServiceExport{
		ObjectMeta: metav1.ObjectMeta{Namespace: member.Name, Name: namespace.Name + "-" + source.Name},
		Spec: fleetnetv1alpha1.InternalServiceExportSpec{
			Ports: service.Status.Ports, Type: corev1.ServiceTypeLoadBalancer, IsInternalLoadBalancer: true,
			ServiceReference: fleetnetv1alpha1.FromMetaObjects(integrationClusterID, metav1.TypeMeta{APIVersion: "v1", Kind: "Service"}, source.ObjectMeta, metav1.Now()),
			PrivateLinkOrigin: &fleetnetv1alpha1.PrivateLinkOrigin{
				ResourceID: integrationPLS, Location: observationTestLocation, InternalIP: "10.0.0.10", Ready: true, Reason: "Ready",
			},
		},
	}
	Expect(integrationClient.Create(integrationContext, export)).To(Succeed())
	updateIntegrationExportStatus(export)
	gateway := &gatewayv1.Gateway{ObjectMeta: metav1.ObjectMeta{Namespace: namespace.Name, Name: "private-gateway"},
		Spec: gatewayv1.GatewaySpec{GatewayClassName: gatewayv1.ObjectName(class.Name),
			Listeners: []gatewayv1.Listener{{Name: "ingress-http", Protocol: gatewayv1.HTTPProtocolType, Port: 80,
				AllowedRoutes: &gatewayv1.AllowedRoutes{Namespaces: &gatewayv1.RouteNamespaces{From: ptr.To(gatewayv1.NamespacesFromAll)}}}}}}
	Expect(integrationClient.Create(integrationContext, gateway)).To(Succeed())
	grant := &gatewayv1beta1.ReferenceGrant{ObjectMeta: metav1.ObjectMeta{Namespace: namespace.Name, Name: "allow-private-api"},
		Spec: gatewayv1beta1.ReferenceGrantSpec{
			From: []gatewayv1beta1.ReferenceGrantFrom{{
				Group: gatewayv1.GroupName, Kind: "HTTPRoute", Namespace: gatewayv1.Namespace(routeNamespace.Name),
			}},
			To: []gatewayv1beta1.ReferenceGrantTo{{
				Group: gatewayv1.Group(fleetnetv1alpha1.GroupVersion.Group), Kind: "ServiceImport", Name: ptr.To(gatewayv1.ObjectName(service.Name)),
			}},
		}}
	Expect(integrationClient.Create(integrationContext, grant)).To(Succeed())
	route := &gatewayv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Namespace: routeNamespace.Name, Name: source.Name},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gatewayv1.ParentReference{{
				Name: gatewayv1.ObjectName(gateway.Name), Namespace: ptr.To(gatewayv1.Namespace(gateway.Namespace)),
			}}},
			Rules: []gatewayv1.HTTPRouteRule{{BackendRefs: []gatewayv1.HTTPBackendRef{{BackendRef: gatewayv1.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{
					Group: ptr.To(gatewayv1.Group(fleetnetv1alpha1.GroupVersion.Group)), Kind: ptr.To(gatewayv1.Kind("ServiceImport")),
					Name: gatewayv1.ObjectName(service.Name), Namespace: ptr.To(gatewayv1.Namespace(service.Namespace)), Port: ptr.To(gatewayv1.PortNumber(80)),
				},
			}}}}},
		}}
	Expect(integrationClient.Create(integrationContext, route)).To(Succeed())
	return integrationGatewayFixture{gateway: gateway, export: export, source: source, service: service, route: route, grant: grant}
}

func verifyIntegrationIdentityLifecycle(fixture integrationGatewayFixture) {
	var beforeGateway gatewayv1.Gateway
	var beforeRoute gatewayv1.HTTPRoute
	gatewayKey := client.ObjectKeyFromObject(fixture.gateway)
	routeKey := client.ObjectKeyFromObject(fixture.route)
	Expect(integrationClient.Get(integrationContext, gatewayKey, &beforeGateway)).To(Succeed())
	Expect(integrationClient.Get(integrationContext, routeKey, &beforeRoute)).To(Succeed())
	want := gatewaymodel.Origin{
		Cluster: integrationClusterID, ServiceUID: string(fixture.source.UID), Endpoint: "10.0.0.10", Weight: 1,
		PrivateLinkResourceID: integrationPLS, PrivateLinkLocation: observationTestLocation,
	}
	Expect(integrationProvider.observedOrigin()).To(Equal(want))

	By("transporting a recreated source Service identity without changing Gateway or HTTPRoute intent")
	Expect(integrationClient.Delete(integrationContext, fixture.source)).To(Succeed())
	replacement := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: fixture.source.Namespace, Name: fixture.source.Name},
		Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}}, Type: corev1.ServiceTypeLoadBalancer},
	}
	Expect(integrationClient.Create(integrationContext, replacement)).To(Succeed())
	Expect(replacement.UID).NotTo(Equal(fixture.source.UID))
	want.ServiceUID = string(replacement.UID)
	verifyIntegrationExportReplacement(fixture, want, func(export *fleetnetv1alpha1.InternalServiceExport) {
		export.Spec.ServiceReference = fleetnetv1alpha1.FromMetaObjects(integrationClusterID,
			metav1.TypeMeta{APIVersion: "v1", Kind: "Service"}, replacement.ObjectMeta, metav1.Now())
	})

	By("replacing transported PLS/frontend identity while retaining the new source identity")
	want.PrivateLinkResourceID = integrationPLS + "-replacement"
	want.Endpoint = "10.0.0.20"
	verifyIntegrationExportReplacement(fixture, want, func(export *fleetnetv1alpha1.InternalServiceExport) {
		export.Spec.PrivateLinkOrigin.ResourceID = want.PrivateLinkResourceID
		export.Spec.PrivateLinkOrigin.InternalIP = want.Endpoint
	})

	var afterGateway gatewayv1.Gateway
	var afterRoute gatewayv1.HTTPRoute
	Expect(integrationClient.Get(integrationContext, gatewayKey, &afterGateway)).To(Succeed())
	Expect(integrationClient.Get(integrationContext, routeKey, &afterRoute)).To(Succeed())
	Expect(afterGateway.Generation).To(Equal(beforeGateway.Generation))
	Expect(afterGateway.Spec).To(Equal(beforeGateway.Spec))
	Expect(afterRoute.Generation).To(Equal(beforeRoute.Generation))
	Expect(afterRoute.Spec).To(Equal(beforeRoute.Spec))
}

func verifyIntegrationExportReplacement(fixture integrationGatewayFixture, want gatewaymodel.Origin,
	change func(*fleetnetv1alpha1.InternalServiceExport)) {
	export := fixture.export
	Expect(integrationClient.Get(integrationContext, client.ObjectKeyFromObject(export), export)).To(Succeed())
	beforeGeneration := export.Generation
	previous, err := integrationProvider.observedOrigin()
	Expect(err).NotTo(HaveOccurred())
	change(export)
	Expect(integrationClient.Update(integrationContext, export)).To(Succeed())
	Expect(export.Generation).To(BeNumerically(">", beforeGeneration))
	conflict := meta.FindStatusCondition(export.Status.Conditions, string(fleetnetv1beta1.ServiceExportConflict))
	Expect(conflict).NotTo(BeNil())
	Expect(conflict.Status).To(Equal(metav1.ConditionFalse))
	Expect(conflict.ObservedGeneration).To(Equal(beforeGeneration))

	By("rejecting the changed export while its unconflicted status is from the old generation")
	message := func() (string, error) {
		return integrationGatewayMessage(client.ObjectKeyFromObject(fixture.gateway))
	}
	Eventually(message, 10*time.Second, 100*time.Millisecond).Should(Equal(
		fmt.Sprintf("cluster %q requires matching source identity, compatible ports and current unconflicted status", integrationClusterID)))
	Expect(integrationRouteReferences(fixture.route, string(gatewayv1.RouteReasonResolvedRefs))).To(Succeed())
	calls := integrationProvider.observationCount()
	Consistently(integrationProvider.observationCount, time.Second, 100*time.Millisecond).Should(Equal(calls))
	Expect(integrationProvider.observedOrigin()).To(Equal(previous))

	By("recovering the exact new origin through the export status watch")
	updateIntegrationExportStatus(export)
	Eventually(integrationProvider.observedOrigin, 10*time.Second, 100*time.Millisecond).Should(Equal(want))
	Eventually(message, 10*time.Second, 100*time.Millisecond).Should(ContainSubstring("approved and deployed"))
	Expect(integrationProvider.observationCount()).To(BeNumerically(">", calls))
	Expect(integrationRouteReferences(fixture.route, string(gatewayv1.RouteReasonResolvedRefs))).To(Succeed())
	Consistently(integrationProvider.observedOrigin, time.Second, 100*time.Millisecond).Should(Equal(want))
}

func verifyIntegrationConvergenceLifecycle(fixture integrationGatewayFixture) {
	beforeGateway := fixture.gateway.DeepCopy()
	beforeRoute := fixture.route.DeepCopy()
	beforeService := fixture.service.DeepCopy()
	beforeExport := fixture.export.DeepCopy()
	for _, object := range []client.Object{beforeGateway, beforeRoute, beforeService, beforeExport} {
		Expect(integrationClient.Get(integrationContext, client.ObjectKeyFromObject(object), object)).To(Succeed())
	}
	origin, err := integrationProvider.observedOrigin()
	Expect(err).NotTo(HaveOccurred())
	integrationProvider.mutex.Lock()
	plan, err := frontdoor.PlanBackends(integrationProvider.model)
	integrationProvider.mutex.Unlock()
	Expect(err).NotTo(HaveOccurred())
	Expect(plan.OriginGroups).To(HaveLen(1))
	Expect(plan.OriginGroups[0].Origins).To(HaveLen(1))
	reference := frontdoor.OriginReference{OriginGroup: plan.OriginGroups[0].Name, Origin: plan.OriginGroups[0].Origins[0].Name}
	message := func() (string, error) {
		return integrationGatewayMessage(client.ObjectKeyFromObject(fixture.gateway))
	}
	convergenceEvents := func() (int64, error) {
		return integrationEventOccurrences(fixture.gateway, corev1.EventTypeNormal, "AFDPrivateOriginsConverged",
			"Observed private origins are approved and deployed; full Azure graph programming and ingress readiness are not established.")
	}
	for _, state := range []frontdoor.PrivateOriginState{frontdoor.PrivateOriginDisconnected, frontdoor.PrivateOriginDrifted} {
		By(fmt.Sprintf("detecting provider-only %s through ordinary polling", state))
		warningEvents := func() (int64, error) {
			return integrationEventOccurrences(fixture.gateway, corev1.EventTypeWarning, "AFDPrivateOriginsNotReady",
				fmt.Sprintf("Observed AFD private origin state %s; inspect Gateway status. Ingress readiness is not established.", state))
		}
		warningsBefore, err := warningEvents()
		Expect(err).NotTo(HaveOccurred())
		calls := integrationProvider.observationCount()
		integrationProvider.setOriginState(state)
		Eventually(message, 50*time.Second, 100*time.Millisecond).Should(Equal(
			fmt.Sprintf("AFD private origin %s/%s is %s; full enabled graph readiness is not established by origin observation", reference.OriginGroup, reference.Origin, state)))
		Expect(integrationProvider.observationCount()).To(BeNumerically(">", calls))
		Eventually(warningEvents, 10*time.Second, 100*time.Millisecond).Should(BeNumerically(">", warningsBefore))
		Expect(integrationRouteReferences(fixture.route, string(gatewayv1.RouteReasonResolvedRefs))).To(Succeed())
		Expect(integrationProvider.observedOrigin()).To(Equal(origin))

		By(fmt.Sprintf("recovering from %s without any Kubernetes edit", state))
		convergenceBefore, err := convergenceEvents()
		Expect(err).NotTo(HaveOccurred())
		Expect(convergenceBefore).To(BeNumerically(">", 0))
		calls = integrationProvider.observationCount()
		integrationProvider.setOriginState(frontdoor.PrivateOriginReady)
		Eventually(message, 50*time.Second, 100*time.Millisecond).Should(Equal(
			"Private origins are approved and deployed; full enabled graph readiness is not established by origin observation"))
		Expect(integrationProvider.observationCount()).To(BeNumerically(">", calls))
		Eventually(convergenceEvents, 10*time.Second, 100*time.Millisecond).Should(BeNumerically(">", convergenceBefore))
		Expect(integrationRouteReferences(fixture.route, string(gatewayv1.RouteReasonResolvedRefs))).To(Succeed())
		Expect(integrationProvider.observedOrigin()).To(Equal(origin))
	}
	afterGateway := beforeGateway.DeepCopy()
	afterRoute := beforeRoute.DeepCopy()
	afterService := beforeService.DeepCopy()
	afterExport := beforeExport.DeepCopy()
	for _, object := range []client.Object{afterGateway, afterRoute, afterService, afterExport} {
		Expect(integrationClient.Get(integrationContext, client.ObjectKeyFromObject(object), object)).To(Succeed())
	}
	Expect(afterGateway.Generation).To(Equal(beforeGateway.Generation))
	Expect(afterGateway.Spec).To(Equal(beforeGateway.Spec))
	Expect(afterRoute.Generation).To(Equal(beforeRoute.Generation))
	Expect(afterRoute.Spec).To(Equal(beforeRoute.Spec))
	Expect(afterService).To(Equal(beforeService))
	Expect(afterExport).To(Equal(beforeExport))
}

func verifyIntegrationStaleObservation(fixture integrationGatewayFixture) {
	By("holding an old-model rejection while allowing the real cache to advance")
	var beforeGateway gatewayv1.Gateway
	var beforeRoute gatewayv1.HTTPRoute
	gatewayKey := client.ObjectKeyFromObject(fixture.gateway)
	routeKey := client.ObjectKeyFromObject(fixture.route)
	Expect(integrationClient.Get(integrationContext, gatewayKey, &beforeGateway)).To(Succeed())
	Expect(integrationClient.Get(integrationContext, routeKey, &beforeRoute)).To(Succeed())
	previous, err := integrationProvider.observedOrigin()
	Expect(err).NotTo(HaveOccurred())
	convergenceEvents := func() (int64, error) {
		return integrationEventOccurrences(fixture.gateway, corev1.EventTypeNormal, "AFDPrivateOriginsConverged",
			"Observed private origins are approved and deployed; full Azure graph programming and ingress readiness are not established.")
	}
	convergenceBefore, err := convergenceEvents()
	Expect(err).NotTo(HaveOccurred())
	rejectedEvents := func() (int64, error) {
		return integrationEventOccurrences(fixture.gateway, corev1.EventTypeWarning, "AFDPrivateOriginsNotReady",
			"Observed AFD private origin state Rejected; inspect Gateway status. Ingress readiness is not established.")
	}
	Expect(rejectedEvents()).To(BeZero())
	hold, err := integrationProvider.holdNextObservation(frontdoor.PrivateOriginRejected)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(hold.unblock)
	var captured frontdoor.PrivateOriginsObservation
	Eventually(hold.entered, 50*time.Second).Should(Receive(&captured))
	Expect(captured.Origins).To(HaveLen(1))
	Expect(captured.Origins[0].State).To(Equal(frontdoor.PrivateOriginRejected))
	Expect(captured.OriginsConverged).To(BeFalse())
	Expect(integrationProvider.observedOrigin()).To(Equal(previous))
	integrationProvider.setOriginState(frontdoor.PrivateOriginReady)

	export := fixture.export
	Expect(integrationClient.Get(integrationContext, client.ObjectKeyFromObject(export), export)).To(Succeed())
	Expect(export.Spec.PrivateLinkOrigin.InternalIP).NotTo(Equal("10.0.0.30"))
	export.Spec.PrivateLinkOrigin.InternalIP = "10.0.0.30"
	Expect(integrationClient.Update(integrationContext, export)).To(Succeed())
	updateIntegrationExportStatus(export)
	Eventually(func() error {
		var cached fleetnetv1alpha1.InternalServiceExport
		if err := integrationCachedReader.Get(integrationContext, client.ObjectKeyFromObject(export), &cached); err != nil {
			return err
		}
		if cached.Generation != export.Generation || !equality.Semantic.DeepEqual(cached.Spec, export.Spec) ||
			!equality.Semantic.DeepEqual(cached.Status, export.Status) {
			return fmt.Errorf("controller cache has not observed replacement export generation %d and current status", export.Generation)
		}
		return nil
	}, 10*time.Second, 100*time.Millisecond).Should(Succeed())
	Expect(hold.returned).NotTo(BeClosed())

	By("discarding the captured rejection and observing the replacement private model")
	hold.unblock()
	Eventually(hold.returned, 10*time.Second).Should(BeClosed())
	want := previous
	want.Endpoint = export.Spec.PrivateLinkOrigin.InternalIP
	Eventually(integrationProvider.observedOrigin, 10*time.Second, 100*time.Millisecond).Should(Equal(want))
	Eventually(func() (string, error) {
		return integrationGatewayMessage(gatewayKey)
	}, 10*time.Second, 100*time.Millisecond).Should(Equal(
		"Private origins are approved and deployed; full enabled graph readiness is not established by origin observation"))
	Eventually(convergenceEvents, 10*time.Second, 100*time.Millisecond).Should(BeNumerically(">", convergenceBefore))
	Consistently(rejectedEvents, time.Second, 100*time.Millisecond).Should(BeZero())
	Expect(integrationRouteReferences(fixture.route, string(gatewayv1.RouteReasonResolvedRefs))).To(Succeed())
	var afterGateway gatewayv1.Gateway
	var afterRoute gatewayv1.HTTPRoute
	Expect(integrationClient.Get(integrationContext, gatewayKey, &afterGateway)).To(Succeed())
	Expect(integrationClient.Get(integrationContext, routeKey, &afterRoute)).To(Succeed())
	Expect(afterGateway.Generation).To(Equal(beforeGateway.Generation))
	Expect(afterGateway.Spec).To(Equal(beforeGateway.Spec))
	Expect(afterRoute.Generation).To(Equal(beforeRoute.Generation))
	Expect(afterRoute.Spec).To(Equal(beforeRoute.Spec))
}

func verifyIntegrationGrantLifecycle(fixture integrationGatewayFixture) {
	By("revoking the cross-namespace grant without changing Gateway or HTTPRoute specs")
	var beforeGateway gatewayv1.Gateway
	var beforeRoute gatewayv1.HTTPRoute
	gatewayKey := client.ObjectKeyFromObject(fixture.gateway)
	routeKey := client.ObjectKeyFromObject(fixture.route)
	Expect(integrationClient.Get(integrationContext, gatewayKey, &beforeGateway)).To(Succeed())
	Expect(integrationClient.Get(integrationContext, routeKey, &beforeRoute)).To(Succeed())
	Expect(integrationClient.Delete(integrationContext, fixture.grant)).To(Succeed())
	denied := func() error {
		return integrationRouteReferences(fixture.route, string(gatewayv1.RouteReasonRefNotPermitted))
	}
	message := func() (string, error) { return integrationGatewayMessage(gatewayKey) }
	Eventually(denied, 10*time.Second, 100*time.Millisecond).Should(Succeed())
	Eventually(message, 10*time.Second, 100*time.Millisecond).Should(ContainSubstring("no ReferenceGrant permits"))
	calls := integrationProvider.observationCount()
	Consistently(integrationProvider.observationCount, time.Second, 100*time.Millisecond).Should(Equal(calls))

	By("keeping authorization denial ahead of a missing backend")
	Expect(integrationClient.Delete(integrationContext, fixture.service)).To(Succeed())
	var missing fleetnetv1alpha1.ServiceImport
	err := integrationClient.Get(integrationContext, client.ObjectKeyFromObject(fixture.service), &missing)
	Expect(apierrors.IsNotFound(err)).To(BeTrue(), "expected deleted backend, got %v", err)
	Consistently(denied, time.Second, 100*time.Millisecond).Should(Succeed())
	Eventually(message, 10*time.Second, 100*time.Millisecond).Should(ContainSubstring("no ReferenceGrant permits"))
	Consistently(integrationProvider.observationCount, time.Second, 100*time.Millisecond).Should(Equal(calls))

	By("restoring permission with a newly identified grant while the backend remains absent")
	restoredGrant := &gatewayv1beta1.ReferenceGrant{
		ObjectMeta: metav1.ObjectMeta{Name: fixture.grant.Name, Namespace: fixture.grant.Namespace},
		Spec:       *fixture.grant.Spec.DeepCopy(),
	}
	Expect(integrationClient.Create(integrationContext, restoredGrant)).To(Succeed())
	Expect(restoredGrant.UID).NotTo(Equal(fixture.grant.UID))
	Eventually(func() error {
		return integrationRouteReferences(fixture.route, string(gatewayv1.RouteReasonBackendNotFound))
	}, 10*time.Second, 100*time.Millisecond).Should(Succeed())
	Eventually(message, 10*time.Second, 100*time.Millisecond).Should(Equal(
		fmt.Sprintf("ServiceImport %s/%s is unavailable", fixture.service.Namespace, fixture.service.Name)))
	Consistently(integrationProvider.observationCount, time.Second, 100*time.Millisecond).Should(Equal(calls))

	By("recovering through the ServiceImport watch after the target is recreated")
	restoredService := &fleetnetv1alpha1.ServiceImport{ObjectMeta: metav1.ObjectMeta{
		Name: fixture.service.Name, Namespace: fixture.service.Namespace,
	}}
	Expect(integrationClient.Create(integrationContext, restoredService)).To(Succeed())
	Expect(restoredService.UID).NotTo(Equal(fixture.service.UID))
	restoredService.Status = *fixture.service.Status.DeepCopy()
	Expect(integrationClient.Status().Update(integrationContext, restoredService)).To(Succeed())
	Eventually(func() error {
		return integrationRouteReferences(fixture.route, string(gatewayv1.RouteReasonResolvedRefs))
	}, 10*time.Second, 100*time.Millisecond).Should(Succeed())
	Eventually(message, 10*time.Second, 100*time.Millisecond).Should(ContainSubstring("approved and deployed"))
	Expect(integrationProvider.observationCount()).To(BeNumerically(">", calls))
	var afterGateway gatewayv1.Gateway
	var afterRoute gatewayv1.HTTPRoute
	Expect(integrationClient.Get(integrationContext, gatewayKey, &afterGateway)).To(Succeed())
	Expect(integrationClient.Get(integrationContext, routeKey, &afterRoute)).To(Succeed())
	Expect(afterGateway.Generation).To(Equal(beforeGateway.Generation))
	Expect(afterGateway.Spec).To(Equal(beforeGateway.Spec))
	Expect(afterRoute.Generation).To(Equal(beforeRoute.Generation))
	Expect(afterRoute.Spec).To(Equal(beforeRoute.Spec))
}

func integrationRouteReferences(route *gatewayv1.HTTPRoute, reason string) error {
	var current gatewayv1.HTTPRoute
	if err := integrationClient.Get(integrationContext, client.ObjectKeyFromObject(route), &current); err != nil {
		return err
	}
	if len(current.Status.Parents) != 1 || len(current.Spec.ParentRefs) != 1 {
		return fmt.Errorf("expected one controlled parent status: %+v", current.Status)
	}
	parent := current.Status.Parents[0]
	if parent.ControllerName != gatewayv1.GatewayController(objectmeta.GatewayAPIControllerName) ||
		!equality.Semantic.DeepEqual(parent.ParentRef, current.Spec.ParentRefs[0]) {
		return fmt.Errorf("unexpected route parent identity: %+v", parent)
	}
	accepted := meta.FindStatusCondition(parent.Conditions, string(gatewayv1.RouteConditionAccepted))
	resolved := meta.FindStatusCondition(parent.Conditions, string(gatewayv1.RouteConditionResolvedRefs))
	wantResolved := metav1.ConditionFalse
	if reason == string(gatewayv1.RouteReasonResolvedRefs) {
		wantResolved = metav1.ConditionTrue
	}
	if accepted == nil || accepted.Status != metav1.ConditionTrue || accepted.ObservedGeneration != current.Generation ||
		resolved == nil || resolved.Status != wantResolved || resolved.Reason != reason || resolved.ObservedGeneration != current.Generation {
		return fmt.Errorf("expected current Accepted=True, ResolvedRefs=%s/%s: %+v", wantResolved, reason, parent.Conditions)
	}
	return nil
}

func updateIntegrationDiscovery(export *fleetnetv1alpha1.InternalServiceExport, ready bool) {
	Expect(integrationClient.Get(integrationContext, client.ObjectKeyFromObject(export), export)).To(Succeed())
	export.Spec.PrivateLinkOrigin.Ready = ready
	Expect(integrationClient.Update(integrationContext, export)).To(Succeed())
	updateIntegrationExportStatus(export)
}

func updateIntegrationExportStatus(export *fleetnetv1alpha1.InternalServiceExport) {
	meta.SetStatusCondition(&export.Status.Conditions, metav1.Condition{
		Type: string(fleetnetv1beta1.ServiceExportConflict), Status: metav1.ConditionFalse, ObservedGeneration: export.Generation,
		Reason: "NoConflict", Message: "Integration fixture has no conflicting export",
	})
	Expect(integrationClient.Status().Update(integrationContext, export)).To(Succeed())
}

func integrationGatewayMessage(key client.ObjectKey) (string, error) {
	var gateway gatewayv1.Gateway
	if err := integrationClient.Get(integrationContext, key, &gateway); err != nil {
		return "", err
	}
	accepted := meta.FindStatusCondition(gateway.Status.Conditions, "Accepted")
	programmed := meta.FindStatusCondition(gateway.Status.Conditions, "Programmed")
	if accepted == nil || accepted.Status != metav1.ConditionTrue || accepted.ObservedGeneration != gateway.Generation ||
		programmed == nil || programmed.Status != metav1.ConditionFalse || programmed.ObservedGeneration != gateway.Generation ||
		len(gateway.Status.Addresses) != 0 {
		return "", fmt.Errorf("current Gateway status is not accepted and unprogrammed: %+v", gateway.Status)
	}
	return programmed.Message, nil
}

func integrationHasEvent(gateway *gatewayv1.Gateway, reason string) (bool, error) {
	var events corev1.EventList
	if err := integrationClient.List(integrationContext, &events, client.InNamespace(gateway.Namespace)); err != nil {
		return false, err
	}
	for _, event := range events.Items {
		if event.InvolvedObject.UID == gateway.UID && event.Reason == reason {
			return true, nil
		}
	}
	return false, nil
}

func integrationEventOccurrences(gateway *gatewayv1.Gateway, eventType, reason, message string) (int64, error) {
	var events corev1.EventList
	if err := integrationClient.List(integrationContext, &events, client.InNamespace(gateway.Namespace)); err != nil {
		return 0, err
	}
	var count int64
	for _, event := range events.Items {
		if event.InvolvedObject.UID == gateway.UID && event.Type == eventType && event.Reason == reason && event.Message == message {
			count += int64(event.Count)
		}
	}
	return count, nil
}
