# Proposal 001 — Azure Front Door + WAF + Private Link Global Load Balancing

| Field       | Value                                               |
|-------------|-----------------------------------------------------|
| Status      | Draft                                               |
| Author      | @rchinchani_microsoft                               |
| Created     | 2026-07-15                                          |
| Depends on  | SFI-NS253                                           |
| Supersedes  | —                                                   |

## 1. Summary

Add a new global load-balancing (GLB) data-plane option to
fleet-networking based on **Azure Front Door (AFD) Standard / Premium**
with an attached **WAF policy** and **Azure Private Link** to the
member-cluster origins.  This new option runs **in parallel** with the
existing Azure Traffic Manager (ATM) implementation; ATM remains
supported for third-party / non-SFI workloads and for L4 / non-HTTP
scenarios.

The proposal introduces two new CRDs (`FrontDoorProfile`,
`FrontDoorBackend`) that mirror the existing `TrafficManagerProfile` /
`TrafficManagerBackend` pair, an additive extension to
`ServiceExport` / `InternalServiceExport` for reporting the
Private-Link-Service (PLS) resource ID of each member endpoint, one new
member controller that provisions the per-cluster PLS, and two new hub
controllers that reconcile AFD profiles, endpoints, origin groups,
origins, routes, and security policies via the `armcdn` SDK.

## 2. Motivation

### 2.1 SFI-NS253 in one paragraph

Any first-party Microsoft service exposed to the internet must:

* terminate ingress on an Azure-managed edge (AFD Standard/Premium),
* have a WAF policy in **Prevention** mode attached to that edge, and
* reach origins **exclusively over Private Link** — the origin must not
  expose a public IP.

See <https://eng.ms/docs/initiatives/project-standard/standards-categories/sc-networking/ddos/sfi-ns/sfi-ns253-kpi>.

### 2.2 Current state of `Azure/fleet-networking`

Confirmed by inspection at branch `main`:

* CRDs
  * `api/v1alpha1/trafficmanagerprofile_types.go`
  * `api/v1alpha1/trafficmanagerbackend_types.go`
  * `api/v1beta1/trafficmanagerprofile_types.go`
  * `api/v1beta1/trafficmanagerbackend_types.go`
* Controllers
  * `pkg/controllers/hub/trafficmanagerprofile/`
  * `pkg/controllers/hub/trafficmanagerbackend/`
* SDK wiring
  * `cmd/hub-net-controller-manager/main.go` — only
    `armtrafficmanager.ProfilesClient` and
    `armtrafficmanager.EndpointsClient` are constructed (`initAzureTrafficManagerClients`).
* Feature flags
  * `--enable-traffic-manager-feature` on both hub and member managers
    (default `true`).
* Search for `FrontDoor`, `frontdoor`, `AFD`, `WAF`, `armcdn`, or
  `PrivateLinkService` inside the repository returns zero matches.

Therefore ATM is today the **only** GLB surface, and it is
**structurally incompatible with SFI-NS253**:

| SFI-NS253 requirement       | Traffic Manager | Front Door |
|-----------------------------|:---------------:|:----------:|
| Edge TLS termination        | ❌ (DNS only)   | ✅         |
| WAF attach                  | ❌              | ✅         |
| Private Link to origin      | ❌ (public IP)  | ✅         |
| L7 routing (paths/headers)  | ❌              | ✅         |

### 2.3 Non-goals

* **L4 workloads (non-HTTP/HTTPS).** AFD is an L7 reverse proxy —
  it only serves HTTP, HTTPS, and WebSockets-over-HTTPS. Arbitrary
  TCP / UDP first-party workloads (databases, gRPC-over-plain-TCP,
  SMTP, DNS, etc.) are **not covered** by this proposal and cannot
  satisfy SFI-NS253 via AFD. The likely future counterpart for L4
  is Azure Cross-region Load Balancer (anycast, Private Link
  backends), tracked as a follow-up and not proposed here.
* Removing or deprecating ATM — the two features coexist behind
  independent feature flags.
* Building a generic ingress controller inside a member cluster.  We
  reuse the Azure cloud-provider Service annotations for internal load
  balancer + PLS creation, and rely on the AKS-managed cloud provider
  to program them.
* Supporting AFD **classic**.  Only AFD Standard / Premium (`Microsoft.Cdn`
  resource provider, API surface `armcdn`) are in scope, because
  classic does not support Private Link origins.

## 3. User-facing shape

### 3.1 Author’s mental model

For a customer service `contoso` that wants to be reachable at
`contoso.first-party.example.com`:

```yaml
# In the hub cluster, in namespace "contoso".
apiVersion: networking.fleet.azure.com/v1alpha1
kind: FrontDoorProfile
metadata:
  name: contoso
spec:
  resourceGroup: fleet-frontdoor-rg
  sku: Premium_AzureFrontDoor          # required for Private Link
  wafPolicy:
    resourceID: /subscriptions/…/providers/Microsoft.Network/frontdoorWebApplicationFirewallPolicies/contoso-waf
  healthProbe:
    path: /healthz
    protocol: Https
    intervalInSeconds: 30
  originResponseTimeoutSeconds: 60
---
apiVersion: networking.fleet.azure.com/v1alpha1
kind: FrontDoorBackend
metadata:
  name: contoso
  namespace: contoso
spec:
  profile:
    name: contoso
  backend:
    name: contoso                       # a ServiceImport in the same namespace
  weight: 100
  routing:
    patternsToMatch: ["/*"]
    forwardingProtocol: HttpsOnly
    supportedProtocols: [Https]
    linkToDefaultDomain: Enabled
  privateLink:
    enabled: true                       # required for SFI-NS253
    requestMessage: "fleet-networking auto-approve"
```

### 3.2 What the controllers create in Azure

For the example above, the reconciler ensures:

1. An **AFD profile** `contoso` (SKU `Premium_AzureFrontDoor`) in
   `fleet-frontdoor-rg`.
2. An **AFD endpoint** with hostname
   `contoso-<hash>.z01.azurefd.net`, surfaced back in
   `status.hostName`.
3. One **origin group** per `FrontDoorBackend`, with the health probe
   copied from the profile.
4. One **AFD origin** per (member cluster × exported service) tuple.
   Each origin references the **PLS resource ID** reported by the
   member cluster in `InternalServiceExport.status.privateLinkService.resourceID`.
5. One **route** binding endpoint → origin group with the requested
   path patterns / protocol.
6. One **security policy** binding the endpoint domain(s) to the
   referenced WAF policy.

### 3.3 What the member controller creates in each cluster

For each `ServiceExport` whose exported `Service` opts into AFD (see
§4.2), the member controller ensures the underlying `Service` is
`type: LoadBalancer` and carries the following AKS cloud-provider
annotations (documented at
<https://learn.microsoft.com/azure/aks/internal-lb> and
<https://learn.microsoft.com/azure/aks/private-link-service>):

```
service.beta.kubernetes.io/azure-load-balancer-internal: "true"
service.beta.kubernetes.io/azure-pls-create: "true"
service.beta.kubernetes.io/azure-pls-name: fleet-<uuid>
service.beta.kubernetes.io/azure-pls-ip-configuration-subnet: <subnet>
service.beta.kubernetes.io/azure-pls-visibility: "*"
service.beta.kubernetes.io/azure-pls-auto-approval: "<AFD-subscription-id>"
```

Once AKS programs the PLS, the controller writes the resulting PLS
resource ID into `InternalServiceExport.status.privateLinkService`.
The hub AFD-backend controller watches that field and creates / updates
the corresponding AFD origin.

## 4. API changes

### 4.1 New CRDs

Both new types land first in `api/v1alpha1` (matching how
`TrafficManager*` graduated) and are promoted to `v1beta1` after
integration coverage is in place.

#### 4.1.1 `FrontDoorProfile` (shortName `fdp`)

Package: `api/v1alpha1/frontdoorprofile_types.go`.

Key fields (illustrative Go, not final):

```go
type FrontDoorProfileSpec struct {
    // +kubebuilder:validation:MinLength=1
    // +kubebuilder:validation:MaxLength=90
    // +kubebuilder:validation:XValidation:rule="self == oldSelf",message="resourceGroup is immutable"
    ResourceGroup string `json:"resourceGroup"`

    // +kubebuilder:validation:Enum=Standard_AzureFrontDoor;Premium_AzureFrontDoor
    // +kubebuilder:default=Premium_AzureFrontDoor
    SKU FrontDoorSKU `json:"sku,omitempty"`

    // Optional attach of a WAF policy.  Recommended: reference an
    // externally-managed policy; inline creation is opt-in.
    // +optional
    WAFPolicy *FrontDoorWAFPolicyRef `json:"wafPolicy,omitempty"`

    // +optional
    HealthProbe *FrontDoorHealthProbe `json:"healthProbe,omitempty"`

    // +optional
    // +kubebuilder:validation:Minimum=16
    // +kubebuilder:validation:Maximum=240
    OriginResponseTimeoutSeconds *int32 `json:"originResponseTimeoutSeconds,omitempty"`
}

type FrontDoorProfileStatus struct {
    // HostName is the AFD endpoint FQDN, e.g. contoso-<hash>.z01.azurefd.net.
    // +optional
    HostName *string `json:"hostName,omitempty"`

    // ResourceID of the underlying Microsoft.Cdn/profiles resource.
    // +optional
    ResourceID string `json:"resourceID,omitempty"`

    // EndpointResourceID of the Microsoft.Cdn/profiles/afdEndpoints resource.
    // +optional
    EndpointResourceID string `json:"endpointResourceID,omitempty"`

    // +optional
    // +listType=map
    // +listMapKey=type
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}
```

Condition types mirror `TrafficManagerProfile`: `Programmed` with
reasons `Programmed`, `Invalid`, `Pending`, plus a new
`WAFPolicyNotFound` reason.

#### 4.1.2 `FrontDoorBackend` (shortName `fdb`)

Package: `api/v1alpha1/frontdoorbackend_types.go`.

```go
type FrontDoorBackendSpec struct {
    // +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.profile is immutable"
    Profile FrontDoorProfileRef `json:"profile"`

    // +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.backend is immutable"
    Backend FrontDoorBackendRef `json:"backend"` // references a ServiceImport

    // +kubebuilder:validation:Minimum=1
    // +kubebuilder:validation:Maximum=1000
    // +kubebuilder:default=100
    Weight *int32 `json:"weight,omitempty"`

    // Optional per-backend HTTP routing configuration.
    // +optional
    Routing *FrontDoorRoutingConfig `json:"routing,omitempty"`

    // PrivateLink controls whether AFD origins are wired via the PLS
    // reported by the member cluster.  MUST be `enabled: true` for
    // SFI-NS253 workloads.
    // +optional
    PrivateLink *FrontDoorPrivateLinkConfig `json:"privateLink,omitempty"`
}
```

Status mirrors `TrafficManagerBackend.Status.Endpoints` but each entry
represents an AFD **origin** rather than an ATM endpoint, and includes
the PLS resource ID and the PLS connection approval state
(`Pending` / `Approved` / `Rejected` / `Disconnected`).

Condition types: `Accepted` (reasons: `Accepted`, `Invalid`, `Pending`,
`PrivateLinkPending`, `PrivateLinkRejected`).

### 4.2 Additive changes to existing CRDs

Only additive fields — no breaking changes.

* `api/v1alpha1/serviceexport_types.go`
  * New optional `Spec.ExportMode` (enum `L4-TrafficManager` |
    `L7-FrontDoor`, default `L4-TrafficManager`).  Governs whether the
    member controller should provision an internal LB + PLS for this
    Service.
* `api/v1alpha1/internalserviceexport_types.go`
  * New optional `Status.PrivateLinkService` block:

    ```go
    type ServiceExportPrivateLinkStatus struct {
        // ResourceID of the Microsoft.Network/privateLinkServices resource.
        ResourceID string `json:"resourceID"`
        // Alias of the PLS (used when auto-approval is not in effect).
        // +optional
        Alias string `json:"alias,omitempty"`
        // InternalLoadBalancerFrontendIP is the ILB frontend IP that
        // fronts the PLS.
        // +optional
        InternalLoadBalancerFrontendIP string `json:"internalLoadBalancerFrontendIP,omitempty"`
    }
    ```

The mirror change lands in `api/v1beta1` after the v1alpha1 shape is
proven.

### 4.3 CRD manifests

`config/crd/bases/` will grow two new files
(`networking.fleet.azure.com_frontdoorprofiles.yaml` and
`networking.fleet.azure.com_frontdoorbackends.yaml`) generated by
`make manifests`.

## 5. Controller changes

### 5.1 New hub packages

* `pkg/controllers/hub/frontdoorprofile/`
  * Reconciles `Microsoft.Cdn/profiles` + `afdEndpoints` and, if
    requested, the `securityPolicies` binding to the WAF policy.
* `pkg/controllers/hub/frontdoorbackend/`
  * Reconciles `originGroups`, `origins`, and `routes` under the
    referenced profile.
  * Watches `InternalServiceExport` for changes to
    `status.privateLinkService.resourceID`.
  * Handles the AFD private-endpoint approval workflow when
    auto-approval is not in effect.

Both packages follow the structural conventions of the existing
`trafficmanager*` packages: `controller.go`, `controller_test.go`,
`controller_integration_test.go`, `suite_test.go`, plus a shared fake
provider under `test/common/frontdoor/`.

### 5.2 New / extended member packages

* `pkg/controllers/member/serviceexport/` — extended to honour
  `spec.exportMode: L7-FrontDoor` and to project the AKS-cloud-provider
  PLS annotations onto the exported Service.
* PLS status is copied into `InternalServiceExport.status.privateLinkService`
  by the same controller once the AKS cloud provider surfaces the PLS
  resource ID as a Service annotation (`service.beta.kubernetes.io/azure-pls-resource-id`).

### 5.3 SDK wiring

`cmd/hub-net-controller-manager/main.go`:

* Add `initAzureFrontDoorClients(cloudConfig)` returning a small
  bundle of `armcdn` clients:
  * `armcdn.ProfilesClient`
  * `armcdn.AFDEndpointsClient`
  * `armcdn.AFDOriginGroupsClient`
  * `armcdn.AFDOriginsClient`
  * `armcdn.RoutesClient`
  * `armcdn.SecurityPoliciesClient`
* Add a new flag `--enable-frontdoor-feature` (default `false` until
  the feature is GA) gating the registration of the two new
  controllers.

`go.mod` gains a dependency on
`github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn`.

### 5.4 Common libraries

* `pkg/common/azurefrontdoor/` — SDK helpers analogous to what
  `pkg/controllers/hub/trafficmanager*` already inlines, plus
  origin/route/security-policy naming (reusing the `fleet-<UUID>#…`
  scheme from `pkg/common/objectmeta`).
* `pkg/common/azureerrors/` — extended to classify AFD-specific error
  codes (private-link approval races, WAF policy not found, etc.).
* `pkg/common/defaulter/` — new defaulters for `FrontDoorProfile` and
  `FrontDoorBackend`.
* Prometheus metrics analogous to the existing ATM ones, e.g.
  `fleet_networking_frontdoor_profile_status_last_timestamp_seconds`.

## 6. Deployment / charts

* `charts/hub-net-controller-manager/`
  * New RBAC rules for `frontdoorprofiles` and `frontdoorbackends`
    (both `*` verbs on the resources and `get`, `update` on
    `/status`).
  * New value block `frontDoor.enabled` (default `false`) wired to
    `--enable-frontdoor-feature`.
  * Documentation of the AFD-authorized identity requirement (see
    §7).
* `charts/member-net-controller-manager/`
  * Update `--enable-traffic-manager-feature` documentation to note it
    now toggles ATM only; add `--enable-frontdoor-feature` for the
    member half of the PLS provisioner.

## 7. Security / SFI considerations

* **Least privilege.**  AFD reconciliation requires the `CDN Profile
  Contributor` role (or a custom role that grants `Microsoft.Cdn/*`
  under the AFD resource group).  This is a **strict superset** of
  what ATM needs and MUST be granted to a **separate managed identity**
  from the ATM identity — otherwise ATM-only tenants inherit AFD
  write permissions they do not need.
* **WAF mode.**  For SFI-NS253 compliance the referenced WAF policy
  MUST be in `Prevention` mode.  The controller validates this at
  admission time (via CEL on `FrontDoorProfile.spec.wafPolicy` if the
  policy is inline; via a status condition
  `WAFPolicyNotInPreventionMode` if referenced).
* **Public origin guard.**  The `FrontDoorBackend` controller refuses
  to create an AFD origin against an `InternalServiceExport` that has
  no `status.privateLinkService.resourceID` when
  `spec.privateLink.enabled = true`, and instead reports
  `Accepted=False` with reason `PrivateLinkPending`.
* **Auto-approval subscription.**  The PLS `auto-approval` annotation
  on the member Service is populated from the AFD profile’s home
  subscription so that AFD origin creation does not block on manual
  approval.  Cross-tenant deployments require manual approval; the
  status surface makes that explicit.

## 8. Testing strategy

* **Unit tests.**  New `*_test.go` next to each new source file.
  Table-driven, in the style of the existing
  `pkg/common/defaulter/trafficmanagerprofile_test.go`.
* **Integration tests.**  New
  `pkg/controllers/hub/frontdoorprofile/controller_integration_test.go`
  and `.../frontdoorbackend/controller_integration_test.go`, using a
  fake `armcdn` implementation under
  `test/common/frontdoor/fakeprovider/`.
* **E2E tests.**  New `test/e2e/frontdoor_test.go`, mirroring the
  structure of `test/e2e/traffic_manager_test.go`.  Requires an AFD
  Premium profile per test suite plus one PLS-capable AKS pool per
  member cluster.  Gated behind an env var so the existing e2e
  matrix does not become AFD-mandatory.
* **API validation tests.**  Extend
  `test/apis/v1alpha1/api_validation_integration_test.go` and the
  v1beta1 counterpart.

## 9. Migration and coexistence

* ATM users are unaffected.  All new CRDs and flags default to
  disabled.
* A single `ServiceExport` cannot participate in both ATM and AFD
  simultaneously; the `exportMode` field disambiguates.  A user who
  wants both must create two `ServiceExport` objects (matching two
  Services), each with a different mode.
* Fleet-wide migration path: start with `exportMode: L4-TrafficManager`
  (implicit), add `L7-FrontDoor` alongside once AFD is validated,
  cut DNS over, then delete the ATM object.

## 10. Rollout plan

| Phase | Scope | Exit criteria |
|-------|-------|---------------|
| 0 | This proposal accepted, breadcrumb approved | Sign-off from fleet-networking maintainers + SFI reviewer |
| 1 | `api/v1alpha1` types + CRD manifests + defaulter/validation, no controller | `make manifests`, `go test ./api/... ./pkg/common/defaulter/...` green |
| 2 | Hub `frontdoorprofile` controller (no backend, no PLS) | Integration tests green; profile + endpoint + WAF attach observable in a dev sub |
| 3 | Member PLS provisioner + `InternalServiceExport` status field | Integration test proves PLS created and status reported |
| 4 | Hub `frontdoorbackend` controller (origins + routes + PL approval) | E2E test in `test/e2e/frontdoor_test.go` green in ci-e2e pipeline |
| 5 | `api/v1beta1` promotion + docs under `docs/concepts/HTTPBasedGlobalLoadBalancing/` and `docs/howtos/frontdoor-permissions-setup.md` | Feature marked GA in `README`; SFI-NS253 checklist attached |

## 11. Open questions

1. Should `FrontDoorProfile` be **cluster-scoped** (one profile shared
   across namespaces, akin to a shared ingress) or **namespace-scoped**
   (matching `TrafficManagerProfile`)?  Current proposal:
   namespace-scoped for isolation parity with ATM.
2. Do we support **multiple custom domains** per profile in the first
   cut, or only the auto-generated `*.azurefd.net` hostname?  Current
   proposal: auto-generated only in phase 2; custom domains + managed
   certificates in phase 5.
3. Should the WAF policy be **required** (validation error if absent)
   for `Premium_AzureFrontDoor` SKU, given the SFI intent?  Current
   proposal: required at the SKU level.
4. Is there appetite to fold this into a common `GlobalLoadBalancer`
   umbrella CRD later (with `type: TrafficManager | FrontDoor`) rather
   than shipping parallel CRDs?  Not proposed here — the two Azure
   surfaces are too different structurally — but flagged for review.

## 12. References

* SFI-NS253 KPI —
  <https://eng.ms/docs/initiatives/project-standard/standards-categories/sc-networking/ddos/sfi-ns/sfi-ns253-kpi>
* Azure Front Door Standard/Premium overview —
  <https://learn.microsoft.com/azure/frontdoor/front-door-overview>
* AFD Private Link origins —
  <https://learn.microsoft.com/azure/frontdoor/private-link>
* AKS internal load balancer —
  <https://learn.microsoft.com/azure/aks/internal-lb>
* AKS Private Link Service integration —
  <https://learn.microsoft.com/azure/aks/private-link-service>
* Existing ATM design in this repo —
  [`docs/concepts/DNSBasedGlobalLoadBalancing/README.md`](../concepts/DNSBasedGlobalLoadBalancing/README.md)
