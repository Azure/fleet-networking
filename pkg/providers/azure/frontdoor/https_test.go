/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package frontdoor

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	"k8s.io/utils/ptr"

	"go.goms.io/fleet-networking/pkg/controllers/hub/gatewaymodel"
)

func httpsModel() gatewaymodel.GlobalGateway {
	model := backendModel()
	model.Listeners[0].Protocol, model.Listeners[0].Port, model.Listeners[0].DefaultCertificate = "HTTPS", 443, true
	backend := &model.Routes[0].Rules[0].Backends[0]
	backend.TLSHostname, backend.TargetUID, backend.TLSProvenance = "origin.example.com", "service-import-uid", "source-digest"
	backend.HealthProbe = gatewaymodel.HTTPSHealthProbe()
	return model
}

func httpsFixture(t *testing.T) (backendProvisioningFixture, armcdn.Route) {
	t.Helper()
	f, _ := routeFixture(t)
	snapshot := originSnapshot(t, httpsModel())
	f.snapshot.model, f.snapshot.origins, f.snapshot.first = snapshot.model, snapshot.origins, snapshot.first
	var err error
	f.graph, err = BuildHTTPPreviewRequests(f.snapshot.scope, f.snapshot.owner, f.snapshot.model)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := BuildHTTPPreviewRequests(f.snapshot.scope, f.snapshot.owner, f.snapshot.model)
	if err != nil {
		t.Fatal(err)
	}
	f.group.Properties.HealthProbeSettings = observed.Backends.OriginGroups[0].Parameters.Properties.HealthProbeSettings
	route := observed.Route
	route.ID, route.Name = ptr.To(f.graph.Roots.ProfileID+routeSuffix(f)), ptr.To(f.graph.RouteName)
	route.Properties.EndpointName = ptr.To(f.graph.Roots.EndpointName)
	route.Properties.ProvisioningState, route.Properties.DeploymentStatus = ptr.To(armcdn.AfdProvisioningStateSucceeded), ptr.To(armcdn.DeploymentStatusSucceeded)
	return f, route
}

func TestHTTPSRequestsEnforceIndependentTLSHops(t *testing.T) {
	model := httpsModel()
	scope, owner := ownershipFixture()
	graph, err := BuildHTTPPreviewRequests(scope, owner, model)
	if err != nil {
		t.Fatal(err)
	}
	props := requestProperties(t, graph.Route)
	requireJSON(t, props, "supportedProtocols", []string{"Https"})
	requireJSON(t, props, "forwardingProtocol", "HttpsOnly")
	requireJSON(t, props, "httpsRedirect", "Disabled")
	requireJSON(t, props, "linkToDefaultDomain", "Enabled")
	group := graph.Backends.OriginGroups[0]
	requireJSON(t, requestProperties(t, group.Parameters), "healthProbeSettings", map[string]any{
		"probeIntervalInSeconds": 30, "probePath": "/", "probeProtocol": "Https", "probeRequestType": "GET",
	})
	for _, origin := range group.Origins {
		props := requestProperties(t, origin.Parameters)
		requireJSON(t, props, "hostName", "origin.example.com")
		requireJSON(t, props, "httpsPort", 8080)
		requireJSON(t, props, "enforceCertificateNameCheck", true)
		if _, exists := props["httpPort"]; exists {
			t.Fatal("TLS request includes an HTTP origin port")
		}
		if _, exists := props["originHostHeader"]; exists {
			t.Fatal("Host override must be omitted, not empty")
		}
		if origin.Parameters.Properties.SharedPrivateLinkResource == nil {
			t.Fatal("TLS origin lost private target")
		}
	}
	for _, change := range []func(*gatewaymodel.GlobalGateway){
		func(m *gatewaymodel.GlobalGateway) {
			m.Listeners[0].Protocol = "HTTP"
			m.Listeners[0].Port = 80
			m.Listeners[0].DefaultCertificate = false
		},
		func(m *gatewaymodel.GlobalGateway) {
			b := &m.Routes[0].Rules[0].Backends[0]
			b.TLSHostname = ""
			b.TargetUID = ""
			b.TLSProvenance = ""
			b.HealthProbe = gatewaymodel.HTTPPreviewHealthProbe()
		},
		func(m *gatewaymodel.GlobalGateway) { m.Listeners[0].DefaultCertificate = false },
		func(m *gatewaymodel.GlobalGateway) { m.Routes[0].Rules[0].Backends[0].HealthProbe.Protocol = "HTTP" },
		func(m *gatewaymodel.GlobalGateway) {
			m.Routes[0].Rules[0].Backends[0].OriginHostHeader = "override.example.com"
		},
	} {
		model := httpsModel()
		change(&model)
		if got, err := BuildHTTPPreviewRequests(scope, owner, model); err == nil || !reflect.DeepEqual(got, HTTPPreviewRequests{}) {
			t.Fatalf("mixed/unsupported TLS graph accepted: %+v %v", got, err)
		}
	}
}

func TestHTTPSObservationChecksNamePortAndValidation(t *testing.T) {
	for _, change := range []func(*armcdn.AFDOrigin){
		func(o *armcdn.AFDOrigin) { o.Properties.HostName = ptr.To("different.example.com") },
		func(o *armcdn.AFDOrigin) { o.Properties.HTTPSPort = ptr.To[int32](8443) },
		func(o *armcdn.AFDOrigin) { o.Properties.EnforceCertificateNameCheck = ptr.To(false) },
		func(o *armcdn.AFDOrigin) { o.Properties.OriginHostHeader = ptr.To("stale.example.com") },
	} {
		snapshot := originSnapshot(t, httpsModel())
		origin := snapshot.origins[snapshot.first]
		change(&origin)
		snapshot.origins[snapshot.first] = origin
		observed, err := snapshot.observe()
		if err != nil || observed.OriginsConverged {
			t.Fatalf("TLS drift accepted: %+v %v", observed, err)
		}
	}
}

func TestHTTPSProviderReadbackAndWithdrawal(t *testing.T) {
	f, route := httpsFixture(t)
	f.endpoint.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
	route.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled)
	steps := append(httpPreviewReads(f, route), httpPreviewInventory(f, route)...)
	provider, calls := disabledGraphClient(t, f, steps, nil)
	got, err := provider.ReconcileHTTPPreview(context.Background(), f.snapshot.owner, f.snapshot.model)
	if err != nil || got.State != HTTPPreviewProgrammed || got.EndpointHostName == "" || *calls != len(steps) {
		t.Fatalf("HTTPS readback not programmed: %+v %v (%d/%d)", got, err, *calls, len(steps))
	}
	if err := verifyWithdrawalRoute(f.graph, route); err != nil {
		t.Fatalf("TLS route cannot be withdrawn: %v", err)
	}
}

func TestHTTPSIsolationPrunesBeforeAddingAndRequiresAbsence(t *testing.T) {
	f, route := httpsFixture(t)
	f.endpoint.Properties.EnabledState, route.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled), ptr.To(armcdn.EnabledStateEnabled)
	inventory := httpPreviewInventory(f, route)
	stale := f.originRead(0).body.(armcdn.AFDOrigin)
	stale.Name = ptr.To(resourceName("fleet-origin", "excluded-source"))
	stale.ID = ptr.To(f.graph.Roots.ProfileID + "/originGroups/" + f.graph.Backends.OriginGroups[0].Name + "/origins/" + *stale.Name)
	at := len(f.roots()) + 3
	inventory[at].body.(map[string]interface{})[inventoryValueField] = []interface{}{f.originRead(1).body, stale}
	steps := append(httpPreviewReads(f, route), inventory...)
	suffix := "/originGroups/" + f.graph.Backends.OriginGroups[0].Name + "/origins/" + *stale.Name
	steps = append(steps, backendStep{method: http.MethodGet, suffix: suffix, status: http.StatusOK, body: stale})
	steps = append(steps, f.roots()...)
	steps = append(steps, backendStep{method: http.MethodDelete, suffix: suffix, status: http.StatusNoContent})
	provider, calls := disabledGraphClient(t, f, steps, nil)
	got, err := provider.ReconcileHTTPPreview(context.Background(), f.snapshot.owner, f.snapshot.model)
	if err != nil || got.State != HTTPPreviewPending || got.EndpointHostName != "" || *calls != len(steps) {
		t.Fatalf("TLS isolation reported prematurely: %+v %v (%d/%d)", got, err, *calls, len(steps))
	}
}

func TestHTTPSInventoryFailureAttemptsSuspensionAndSurfacesDeniedEnforcement(t *testing.T) {
	for _, denied := range []bool{false, true} {
		t.Run(map[bool]string{false: "suspension submitted", true: "suspension denied"}[denied], func(t *testing.T) {
			f, route := httpsFixture(t)
			f.endpoint.Properties.EnabledState, route.Properties.EnabledState = ptr.To(armcdn.EnabledStateEnabled), ptr.To(armcdn.EnabledStateEnabled)
			steps := append(httpPreviewReads(f, route), httpPreviewInventory(f, route)...)
			steps[len(steps)-1].status = http.StatusForbidden
			suspension := suspendEndpointSteps(f)
			if denied {
				suspension[len(suspension)-1].status = http.StatusForbidden
			}
			steps = append(steps, suspension...)
			provider, calls := disabledGraphClient(t, f, steps, nil)
			got, err := provider.ReconcileHTTPPreview(context.Background(), f.snapshot.owner, f.snapshot.model)
			if err == nil || !strings.Contains(err.Error(), "list unsupported AFD secrets inventory") || got.State != HTTPPreviewSuspending ||
				got.EndpointHostName != "" || *calls != len(steps) {
				t.Fatalf("inventory uncertainty was hidden: %+v %v (%d/%d)", got, err, *calls, len(steps))
			}
			if denied {
				if got.Suspension.State != "" || !strings.Contains(err.Error(), "suspend owned AFD endpoint") {
					t.Fatalf("denied enforcement reported as successful: %+v %v", got, err)
				}
			} else if got.Suspension.State != EndpointSuspensionPending {
				t.Fatalf("submission reported as observed suspension: %+v", got)
			}
		})
	}
}
