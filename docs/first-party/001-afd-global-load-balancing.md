# Proposal 001 — Azure Front Door + WAF + Private Link Global Load Balancing

| Field       | Value                                               |
|-------------|-----------------------------------------------------|
| Status      | Draft                                               |
| Author      | @rchinchani_microsoft                               |
| Created     | 2026-07-15                                          |
| Depends on  | SFI-NS253                                           |
| Supersedes  | —                                                   |

## 1. Summary

> **Reader's note.** This proposal reflects the state of the branch as
> of commit `cb02d14` (2026-07-19). Passages tagged `POC:` describe
> what is already in `main`; passages tagged **POC deviation** or
> **Impossibility flag** describe gaps between the shipped code and
> the target design. The reconciliation pass is recorded in
> `.github/.copilot/breadcrumbs/2026-07-20-1108-afd-export-mode-mcs-parity.md`
> (Addendum 2). Proposal 003 §6 tracks the outstanding blockers.

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

### Competitive context (industry parity)

Peer clouds already ship a first-party controller that programs a
global L7 edge from a Kubernetes-native surface, with WAF/bot rules
that travel with the workload manifest instead of a side-car IaC
pipeline. Today AKS/Fleet does not:

| Cloud Provider    | Controller Engine                 | Edge Routing Layer                                 | Native Bot/WAF Security Product                              | Native Bot/WAF Hook?                       | Layer-7 Controller Maturity                    |
| :---------------- | :-------------------------------- | :------------------------------------------------- | :----------------------------------------------------------- | :----------------------------------------- | :--------------------------------------------- |
| **GCP (GKE)**     | Multi-Cluster Gateway Controller  | Global External Application Load Balancer          | **Google Cloud Armor** (with reCAPTCHA Enterprise)           | **Yes** (via `GCPBackendPolicy`)           | Highly mature, production-standard             |
| **AWS (EKS)**     | AWS Load Balancer Controller      | Application Load Balancer (ALB) & VPC Lattice      | **AWS WAF** (with AWS Managed Rules Bot Control)             | **Yes** (via resource annotations)         | Gateway API implementation reached GA in early 2026 |
| **Azure (Fleet)** | Azure Kubernetes Fleet Manager    | **Azure Front Door (Premium)** — this proposal     | **Azure Web Application Firewall** (with Bot Manager Rule Set) | **No today**; must manage AFD outside K8s | **ATM only (L4 DNS)**; L7 requires Terraform/ASO |

The AKS/Fleet row is what this proposal changes. `FrontDoorProfile`
carries the WAF hook via `.spec.wafPolicy` (§4.1.1) and
`FrontDoorBackend` (§4.1.2) makes per-workload origin programming a
CR mutation instead of a Terraform plan — bringing AKS/Fleet to
functional parity with `GCPBackendPolicy` on GKE and the WAF
annotations on the AWS Load Balancer Controller, with the added
SFI-NS253 guarantee that origins are reached exclusively over
Private Link (§2.1). It also unblocks the migration path away from
the L4-only Traffic Manager surface documented in §2.2 without
forcing the workload owner to leave Kubernetes YAML.

### 2.1 SFI-NS253 in one paragraph

Any first-party Microsoft service exposed to the internet must:

* terminate ingress on an Azure-managed edge (**AFD Premium** — see
  §2.3, Standard does not support Private Link origins),
* have a WAF policy in **Prevention** mode attached to that edge, and
* reach origins **exclusively over Private Link** — the origin must not
  expose a public IP.

**Traffic-isolation property.** With AFD Premium + Private Link
Service to the AKS internal load balancer, the request path is:

```
client ──▶ AFD PoP (TLS termination) ──▶ Microsoft backbone
       ──▶ Private Endpoint in member VNet ──▶ AKS ILB ──▶ pod
```

No leg of that path traverses the public internet between AFD and the
origin. The AKS ingress therefore has **no public IP**, which is
what SFI-NS253 fundamentally requires. This isolation is per-flow:
each `FrontDoorBackend` binds a specific AFD origin to a specific
PLS to a specific ILB to a specific `Service`, and there is no
shared route table between different profiles.

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
* Managing member-cluster provisioning (VNet layout, subnet
  `privateLinkServiceNetworkPolicies`, cluster SKU choice between
  AKS Standard and AKS Automatic). These are platform-team concerns;
  see §3.4 for the invariants a member cluster MUST satisfy.
* Supporting AFD **classic** or AFD **Standard**. Only AFD
  **Premium** (`Microsoft.Cdn` resource provider, API surface
  `armcdn`, SKU `Premium_AzureFrontDoor`) is in scope. **Private
  Link origins are a Premium-only feature** — Standard cannot
  satisfy SFI-NS253's private-origin requirement, and classic does
  not support Private Link at all. The CEL rule on
  `FrontDoorProfile.spec.sku` enforces Premium for any profile whose
  backends require Private Link.

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

1. An **AFD profile** named `fleet-<CR-UID>` (SKU
   `Premium_AzureFrontDoor`) in `fleet-frontdoor-rg`. Azure resource
   names are derived from the `FrontDoorProfile` CR's Kubernetes UID,
   not from `metadata.name`, so a CR rename does not orphan the
   underlying Azure resource. The `fleet-` prefix keeps the composed
   endpoint hostname (`fleet-<uid>-<hash>.z01.azurefd.net`) safely
   under AFD's 46-character endpoint-name cap.
2. An **AFD default endpoint** (also named `fleet-<CR-UID>`) whose
   hostname is surfaced back in `status.endpointHostname`.
3. One **origin group** per `FrontDoorBackend`, with the health probe
   copied from the profile.
4. One **AFD origin** per (member cluster × exported service) tuple.
   Each origin references the **PLS resource ID** reported by the
   member cluster in `InternalServiceExport.status.privateLinkService.resourceID`.
5. One **route** binding endpoint → origin group with the requested
   path patterns / protocol.
6. One **security policy** binding the endpoint domain(s) to the
   referenced WAF policy.

All traffic from step 2 onward stays on the Microsoft backbone:
AFD terminates TLS at its edge PoP, then reaches the origin over
the AFD → PLS → ILB private path. The public IP on the member-cluster
`Service` is never provisioned, satisfying SFI-NS253's private-origin
requirement.

### 3.3 What the member controller observes in each cluster

The proposal **does not add a new `Spec` field to `ServiceExport`**.
Upstream mcs-api (KEP-1645) parity is a repository preference, and
Fleet-specific configuration is already carried via annotations
(e.g. `networking.fleet.azure.com/weight`). This proposal follows
the same pattern.

The member controller determines that an exported `Service` is
destined for the AFD path using two signals, evaluated in this
order:

1. **Opt-in annotation on `ServiceExport`** (primary when set):
   ```
   networking.fleet.azure.com/export-mode: L7-FrontDoor   # or L4-TrafficManager (default)
   ```
   Suitable for GitOps pipelines that want the `ServiceExport`
   to declare intent before the `Service` is fully provisioned.
2. **Inference from the `Service` itself** (fallback): if the
   annotation is unset, the controller inspects the exported
   `Service` and infers `L7-FrontDoor` when **all** of the
   following AKS cloud-provider annotations are present
   (documented at <https://learn.microsoft.com/azure/aks/internal-lb>
   and <https://learn.microsoft.com/azure/aks/private-link-service>):

   ```
   service.beta.kubernetes.io/azure-load-balancer-internal: "true"
   service.beta.kubernetes.io/azure-pls-create: "true"
   service.beta.kubernetes.io/azure-pls-name: <name>
   service.beta.kubernetes.io/azure-pls-ip-configuration-subnet: <subnet>
   service.beta.kubernetes.io/azure-pls-visibility: "*"                              # or a comma-separated allow-list
   service.beta.kubernetes.io/azure-pls-auto-approval: "<AFD-subscription-id>"
   ```

   Otherwise the export is treated as `L4-TrafficManager` (today's
   default behaviour).

**Ownership boundary — the member controller does not mutate the
`Service`.** The Service's annotations are tenant-owned (typically
authored by the app team via GitOps, or enforced centrally by a
platform admission policy such as Kyverno / Gatekeeper). The
controller only *reads* them.

**Precedence and mismatch handling.** When the annotation says
`L7-FrontDoor` but the underlying `Service` is not internal +
PLS-enabled, the controller does not fall back to L4 — that would
silently downgrade an SFI intent. Instead it surfaces
`ServiceExportValid=False` with
`Reason=ExportModeAnnotationServiceMismatch` and waits.

Once AKS programs the PLS, the controller copies the resulting PLS
resource ID (surfaced by the cloud provider as
`service.beta.kubernetes.io/azure-pls-resource-id`) into
`InternalServiceExport.status.privateLinkService`. The hub
AFD-backend controller watches that field and creates / updates the
corresponding AFD origin.

### 3.4 Member cluster requirements (AKS Standard and AKS Automatic)

Both **AKS Standard** and **AKS Automatic** are supported as member
cluster SKUs. The controller code paths are identical because the
data-plane primitives this proposal relies on — Standard SKU internal
Load Balancer + Private Link Service, driven by
`service.beta.kubernetes.io/azure-*` annotations — are provided by
the AKS-managed cloud provider and are available on both SKUs.

The following cluster-side prerequisites apply regardless of SKU and
are the responsibility of the platform team, not the fleet-networking
controllers:

1. **Standard SKU Load Balancer.** Required by PLS. This is the
   default on both AKS Standard and AKS Automatic; Basic LB clusters
   are unsupported.
2. **BYO VNet with a dedicated PLS NAT subnet.** The subnet
   referenced by
   `service.beta.kubernetes.io/azure-pls-ip-configuration-subnet`
   MUST have `privateLinkServiceNetworkPolicies: Disabled`. This is
   a subnet-level property that must be set at (or before) cluster
   provisioning — AKS does not toggle it on the tenant's behalf.
   AKS Automatic supports BYO VNet at cluster creation but restricts
   post-hoc network reshaping; plan the ILB and PLS subnets up front.
3. **PLS auto-approval configured for the AFD subscription.** The
   `service.beta.kubernetes.io/azure-pls-auto-approval` annotation
   MUST include the AFD control-plane subscription ID so that AFD's
   private-endpoint connection requests are approved without human
   intervention.
4. **Egress path.** Not affected by this proposal — AKS Automatic's
   NAT-Gateway egress is orthogonal to ingress via PLS.

**AKS Automatic — additional considerations:**

* **Deployment Safeguards (Enforcement mode).** AKS Automatic ships
  Azure Policy safeguards in enforcement mode by default. The
  fleet-networking hub and member Helm charts (see Proposal 002
  §6.1) MUST satisfy those safeguards — resource requests/limits,
  `runAsNonRoot`, `readOnlyRootFilesystem` where feasible, no
  `hostPath`, images from allow-listed registries, `seccomp:
  RuntimeDefault`, no privileged containers. Any drift here blocks
  install on Automatic even though it succeeds on Standard.
* **Node auto-provisioning (NAP).** Controller Deployments should
  set pod anti-affinity / PDBs so that NAP-driven scale events do
  not simultaneously restart the active reconciler replicas.
* **Locked-down cluster configuration.** Some `az aks update` knobs
  are not permitted on Automatic. All state this proposal touches
  is user-surface (`Service`, `ServiceExport`, `FrontDoor*`), not
  cluster-config surface, so this is a non-issue for the data path
  — but bear it in mind when writing runbooks that assume a Standard
  cluster's mutability.
* **AGC (Application Gateway for Containers) coexistence.** AKS
  Automatic promotes AGC as the default HTTP entry point. AGC and
  the AFD + PLS path proposed here are orthogonal — AGC is an
  in-cluster L7, AFD is an external edge — and can coexist. This
  proposal does not require or interact with AGC.

Testing note: Phase 4 e2e (Proposal 002 §11) MUST include at least
one AKS Automatic member alongside AKS Standard members, so the
Deployment Safeguards path is exercised in CI rather than discovered
at first-adopter onboarding.

### 3.5 Coexistence with the existing Traffic Manager path

ATM is not deprecated by this proposal (§2.3). Both surfaces are
first-class and can run side-by-side in the same fleet. Where they
interact:

**Coexistence granularity.**

| Scope | Coexistence outcome |
|---|---|
| Fleet-wide | Safe. Different CRDs (`TrafficManager*` vs `FrontDoor*`), different hub controllers, different Azure resource types (`Microsoft.Network/trafficManagerProfiles` vs `Microsoft.Cdn/profiles`), different identities (§7), different resource groups. No shared reconciler state. |
| Same tenant namespace, different Services | Safe. `Service A` fronted by ATM (public LB) and `Service B` fronted by AFD (internal LB + PLS) is a supported topology. |
| Same `ServiceImport` on both surfaces | **Forbidden by the AFD backend reconciler.** The two paths require mutually exclusive `Service` shapes (public LB for ATM, internal LB + PLS for AFD). See Proposal 002 §8.4 — the reconciler surfaces `Accepted=False, Reason=ConflictsWithTrafficManagerBackend` and refuses to program AFD origins for a `ServiceImport` already referenced by a `TrafficManagerBackend`. Invariant: at most one north-south surface per `Service`. |

East-west traffic (pod-to-pod via `EndpointSlice` imports) is
unaffected on either path.

**Security implications of coexistence.**

* **ATM public IPs remain a bypass surface.** ATM is DNS-only; its
  origins have public IPs, so clients that discover those IPs can
  connect directly, bypassing any WAF or rate-limiting. This is
  unchanged by AFD's arrival. Fleets that run both must not
  characterise the fleet as SFI-compliant simply because AFD is
  available — compliance is per-tenant, per-Service.
* **Split identities are mandatory.** The AFD managed identity
  (`CDN Profile Contributor` on the AFD resource group) MUST be
  distinct from the ATM identity (`Traffic Manager Contributor` on
  the TM resource group). A single identity for both would grant
  ATM-only tenants unnecessary AFD write permissions and vice-versa,
  silently expanding blast radius. Chart wiring is tracked in
  Proposal 003 §3.6.
* **WAF is per-surface.** ATM has no WAF. Enforcement of "must be
  behind WAF" is only achievable for `FrontDoor*`-fronted Services;
  cluster-level admission policy (Kyverno / OPA / Gatekeeper) should
  deny `TrafficManagerProfile` / `TrafficManagerBackend` creation in
  first-party namespaces if a tenant class is required to be
  AFD-only. Do not rely on tenants opting out voluntarily.
* **Audit / diagnostic split.** ATM and AFD emit to separate
  diagnostic streams. SFI KPI dashboards that count WAF-blocked
  requests must query AFD only; ATM has no such concept.

**Tenancy implications of coexistence.**

* **Namespace RBAC is unchanged.** Tenant-A cannot see or modify
  tenant-B's `TrafficManager*` or `FrontDoor*` CRs. Reserved
  `fleet-member-*` namespaces on the hub remain platform-only.
* **Per-Service surface choice.** With the annotation + inference
  model (§3.3, §4.2), a single tenant namespace may host a mix of
  Services on ATM and AFD without any `Spec`-level opt-in — the
  Service annotations themselves drive the routing choice.
* **Weight-annotation semantics differ.** The Fleet-specific
  `networking.fleet.azure.com/weight` annotation on `ServiceExport`
  is consumed by the ATM backend controller. The AFD backend
  controller ignores it and takes weights from
  `FrontDoorBackend.spec.weight` instead (see Proposal 002 §8.1).
  Migration howtos MUST call this out.
* **Quota accounting is per-surface.** ATM profile limits (200/sub
  default) and AFD Premium profile/endpoint/origin quotas are
  independent. Fleets running both surfaces at scale need
  per-tenant subscription / RG sharding informed by both quotas.
* **Cost attribution.** Different cost models (ATM: per million DNS
  queries; AFD Premium: base fee + per-request + WAF).  Use
  per-tenant `resourceGroup` on both `TrafficManagerProfile` and
  `FrontDoorProfile` so Azure Cost Management can split the bill
  cleanly.

**Migration (ATM → AFD) is a staged tenant operation, not a hot swap.**
Because the same `ServiceImport` cannot attach to both surfaces:

1. Stand up a second `Service` (e.g. `api-v2`) in each member cluster
   with internal LB + PLS annotations; create the matching
   `ServiceExport api-v2`.
2. Hub aggregates a new `ServiceImport api-v2`; create
   `FrontDoorProfile` + `FrontDoorBackend` pointing at it and verify
   `Accepted=True` on every origin.
3. Cut the public DNS record from `<atm-name>.trafficmanager.net` to
   the AFD endpoint hostname; drain traffic per your TTL.
4. Delete `TrafficManagerBackend api` and the original public-LB
   `Service api` in each member cluster.

This intentionally slow, reviewable sequence matches first-party
migration cadence; a controller-driven hot swap is not planned.

## 4. API changes

### 4.1 New CRDs

Both new types land first in `api/v1alpha1` (matching how
`TrafficManager*` graduated) and are promoted to `v1beta1` after
integration coverage is in place.

#### 4.1.1 `FrontDoorProfile` (shortName `afdp`)

Package: `api/v1alpha1/frontdoorprofile_types.go`.

> **POC status (commit `cb02d14`).** The Phase-2 POC currently ships a
> deliberately minimal `Spec` (`ResourceGroup` + `Sku` only) and no
> WAF / compliance / health-probe fields yet. The illustrative Go
> below is the *target* shape; fields marked `POC:` are the ones
> already in `main`, everything else is Phase-4/5 work. See §6 of
> Proposal 002 for the phase table.

Key fields (illustrative Go, not final):

```go
type FrontDoorProfileSpec struct {
    // POC: present in cb02d14.
    // +kubebuilder:validation:MinLength=1
    // +kubebuilder:validation:MaxLength=90
    // +kubebuilder:validation:XValidation:rule="self == oldSelf",message="resourceGroup is immutable"
    ResourceGroup string `json:"resourceGroup"`

    // Sku selects the AFD SKU. The CRD enum accepts only Premium
    // (`Premium_AzureFrontDoor`); Standard is intentionally excluded
    // because Private Link origins — the SFI-NS253 cornerstone — are
    // Premium-only, so a Standard profile could never satisfy the
    // first-party compliance envelope. Failing at admission is
    // preferable to surfacing `Programmed=False` hours later.
    // The field is retained (rather than removed as redundant) so
    // future SKUs can be added additively without a schema break.
    // Immutable after creation: AFD does not support in-place SKU
    // upgrades on an existing profile.
    // +kubebuilder:validation:Enum=Premium_AzureFrontDoor
    // +kubebuilder:default=Premium_AzureFrontDoor
    // +kubebuilder:validation:XValidation:rule="self == oldSelf",message="sku is immutable"
    SKU FrontDoorSKU `json:"sku,omitempty"`

    // Post-POC: optional attach of a WAF policy. Required when
    // complianceMode == SFI-NS253 (see Proposal 003 §1.2).
    // +optional
    WAFPolicy *FrontDoorWAFPolicyRef `json:"wafPolicy,omitempty"`

    // Post-POC.
    // +optional
    HealthProbe *FrontDoorHealthProbe `json:"healthProbe,omitempty"`

    // Post-POC.
    // +optional
    // +kubebuilder:validation:Minimum=16
    // +kubebuilder:validation:Maximum=240
    OriginResponseTimeoutSeconds *int32 `json:"originResponseTimeoutSeconds,omitempty"`
}

type FrontDoorProfileStatus struct {
    // POC: present in cb02d14. Full ARM resource ID of the AFD
    // profile.
    // +optional
    ResourceID string `json:"resourceID,omitempty"`

    // POC: present in cb02d14. The default endpoint's *.azurefd.net
    // hostname (string, not the full endpoint resource ID).
    // +optional
    EndpointHostname *string `json:"endpointHostname,omitempty"`

    // +optional
    // +listType=map
    // +listMapKey=type
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}
```

Condition types (POC, per `cb02d14`): `Programmed` with reasons
`Programmed`, `Invalid`, `AzureError`, `Pending`. `WAFPolicyNotFound`
and `WAFPolicyNotInPreventionMode` land alongside the WAF fields in
a later phase. AFD is a global service, so **no `Location` field is
exposed on the CR**; the controller sets `Location: "Global"`
internally.

There is no separate `EndpointResourceID` status field; the ARM ID
of the endpoint is deterministically composable from `ResourceID` +
the fixed endpoint name (`fleet-<UID>`).

#### 4.1.2 `FrontDoorBackend` (shortName `afdb`)

Package: `api/v1alpha1/frontdoorbackend_types.go`.

> **POC status.** `FrontDoorBackend` is **not yet implemented** in
> `main` (cb02d14 only landed `FrontDoorProfile` + `FrontDoorCustomDomain`).
> This section describes the target shape; the type + controller
> land together in Phase 4 (see Proposal 002 §6).

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

#### 4.1.3 `FrontDoorCustomDomain` (shortName `afdcd`)

Package: `api/v1alpha1/frontdoorcustomdomain_types.go`. **Present in
`main` as of cb02d14** (Managed TLS reconciled; BYOC deferred).

`FrontDoorCustomDomain` represents a custom domain attached to a
`FrontDoorProfile`, including DNS-based ownership validation and the
TLS binding. Same-namespace-only reference to its parent profile
(immutable). Immutable `hostname`.

Key fields (as shipped):

```go
type FrontDoorCustomDomainSpec struct {
    // Same-namespace ref to the owning FrontDoorProfile. Immutable.
    ProfileRef FrontDoorProfileReference `json:"profileRef"`

    // Fully qualified custom domain, e.g. www.contoso.com. Immutable.
    // +kubebuilder:validation:Pattern=`^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$`
    Hostname string `json:"hostname"`

    // TLS.Mode = Managed (AFD-issued cert, auto-renewed) or BYOC
    // (Key Vault cert). BYOC field shape is present but the
    // reconciler currently only implements Managed.
    TLS FrontDoorTLSConfig `json:"tls"`
}

type FrontDoorCustomDomainStatus struct {
    ResourceID          string                          `json:"resourceID,omitempty"`
    ValidationState     FrontDoorDomainValidationState  `json:"validationState,omitempty"`
    // The value to publish as TXT record at `_dnsauth.<hostname>`.
    DNSValidationToken  *string                         `json:"dnsValidationToken,omitempty"`
    DNSValidationExpiry *metav1.Time                    `json:"dnsValidationExpiry,omitempty"`
    Conditions          []metav1.Condition              `json:"conditions,omitempty"`
}
```

Condition: `Programmed` with reasons `Programmed`, `Invalid`,
`ProfileNotReady`, `AwaitingDNSValidation`, `ValidationFailed`,
`TLSFailed`, `AzureError`, `Pending`.

The tenant workflow is: apply the CR → controller creates the AFD
custom domain resource → status surfaces `DNSValidationToken` → the
tenant publishes a TXT record → AFD validates and `Programmed=True`.

### 4.2 Additive changes to existing CRDs

Only additive fields — no breaking changes. **`ServiceExport` itself
gains no new `Spec` field.** Per the mcs-api parity preference (see
§3.3), Fleet-specific intent is expressed via an annotation:

```
networking.fleet.azure.com/export-mode: L7-FrontDoor   # optional; default is L4-TrafficManager (i.e., today's behaviour)
```

The annotation constant lives alongside the existing
`networking.fleet.azure.com/weight` constant under
`pkg/common/objectmeta/`. Absence of the annotation is equivalent to
`L4-TrafficManager`; the member controller may still infer
`L7-FrontDoor` from the Service's internal-LB + PLS annotations, per
the precedence rule in §3.3.

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

`config/crd/bases/` grows three new files
(`networking.fleet.azure.com_frontdoorprofiles.yaml`,
`networking.fleet.azure.com_frontdoorcustomdomains.yaml`, and
`networking.fleet.azure.com_frontdoorbackends.yaml`) generated by
`make manifests`. The first two land in `main` as of cb02d14; the
third arrives with Phase 4.

## 5. Controller changes

### 5.1 New hub packages

* `pkg/controllers/hub/frontdoorprofile/` — **shipped in cb02d14.**
  Reconciles `Microsoft.Cdn/profiles` + the default `afdEndpoint`.
  WAF `securityPolicies` binding lands with the WAF fields on the
  CR (post-POC).
* `pkg/controllers/hub/frontdoorcustomdomain/` — **shipped in cb02d14.**
  Reconciles `Microsoft.Cdn/profiles/customDomains`, surfaces the
  DNS validation token, and (Managed TLS only for now) binds the
  cert. BYOC (Key Vault) reconciliation is deferred.
* `pkg/controllers/hub/frontdoorbackend/` — **not yet in main;
  Phase 4.** Reconciles `originGroups`, `origins`, and `routes`
  under the referenced profile; watches `InternalServiceExport` for
  changes to `status.privateLinkService.resourceID`; handles the AFD
  private-endpoint approval workflow when auto-approval is not in
  effect.

All three packages follow the structural conventions of the existing
`trafficmanager*` packages: `controller.go`, `controller_test.go`,
`controller_integration_test.go`, `suite_test.go`, plus a shared fake
provider under `test/common/frontdoor/`. cb02d14 ships the
happy-path reconciler + finalizer for Profile and CustomDomain, but
**not** the unit/integration test scaffolding — that is tracked in
Proposal 002 §6 as remaining Phase-2 work.

### 5.2 New / extended member packages

* `pkg/controllers/member/serviceexport/` — **not yet extended in
  main; Phase 3.** Will be extended to
  (a) detect the AFD path via the annotation-then-inference rule
  described in §3.3, and
  (b) copy the AKS-cloud-provider-set annotation
  `service.beta.kubernetes.io/azure-pls-resource-id` into
  `InternalServiceExport.status.privateLinkService` once the PLS is
  ready. The controller does **not** mutate the exported `Service`;
  the internal-LB + PLS annotations are tenant-owned.

### 5.3 SDK wiring

The **target** wiring lives in a new binary
`cmd/hub-afd-controller-manager/main.go` (see §6), separate from
`cmd/hub-net-controller-manager/main.go`. The wiring adds:

* `initAzureFrontDoorClients(cfg)` returning the AFD sub-clients
  (`ProfilesClient`, `AFDEndpointsClient`, `AFDCustomDomainsClient`,
  and — Phase 4 — `AFDOriginGroupsClient`, `AFDOriginsClient`,
  `RoutesClient`, `SecurityPoliciesClient`). See
  `pkg/common/azurefrontdoor/client.go` (shipped in cb02d14).
* A flag `--enable-frontdoor-feature` gating controller registration.

**POC deviation (cb02d14):** the AFD controllers currently live
inside `cmd/hub-net-controller-manager/main.go` behind the same
`--enable-frontdoor-feature` flag (default `false`) as a temporary
bridge — the sibling binary+chart split is a hard prerequisite for
GA because of §7 (see the Impossibility flag there).

`go.mod` gains a dependency on
`github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn`
(pinned at `v1.1.1` in cb02d14).

### 5.4 Common libraries

* `pkg/common/azurefrontdoor/` — **shipped in cb02d14** as a single
  `client.go`: Workload-Identity `Config` + `NewCredential` +
  `NewClients` factory bundling `ProfilesClient`, `AFDEndpointsClient`,
  `AFDCustomDomainsClient`. Additional sub-clients (origin, route,
  security-policy) land alongside `FrontDoorBackend` in Phase 4.
  Naming helpers (`fleet-<UID>`) live in each controller for now.
* `pkg/common/objectmeta/` — **shipped in cb02d14**: finalizer
  constants `networking.fleet.azure.com/frontdoor-profile-cleanup`
  and `.../frontdoor-custom-domain-cleanup`.
* `pkg/common/azureerrors/` — extended to classify AFD-specific
  error codes (private-link approval races, WAF policy not found,
  etc.).
* `pkg/common/defaulter/` — new defaulters for `FrontDoorProfile`
  and `FrontDoorBackend` (Phase 4).
* Prometheus metrics analogous to the existing ATM ones, e.g.
  `fleet_networking_frontdoor_profile_status_last_timestamp_seconds`.

## 6. Deployment / charts

The **target** deployment model is a **sibling chart + sibling
binary**, isolated from the existing hub-net-controller-manager
chart:

* `charts/hub-afd-controller-manager/` (new) — RBAC on
  `frontdoorprofiles`, `frontdoorcustomdomains`, `frontdoorbackends`;
  its own ServiceAccount with its own Workload-Identity federated
  subject; its own Deployment, values, and PDB. This isolation is
  what makes the §7 identity-split requirement satisfiable end to
  end (see §7).
* `charts/hub-net-controller-manager/` — **unchanged** for AFD. The
  ATM controller retains its own SA and MI. The `helm template`
  fixtures under `.github/.copilot/breadcrumbs/baselines/` protect
  this chart from silent drift.
* `charts/member-net-controller-manager/` — update `--enable-traffic-manager-feature`
  documentation to note it toggles ATM only. The member-side AFD
  work (Phase 3) is a *reader* of Service annotations and does not
  need Azure SDK access, so a member-side sibling chart is not
  required; the flag `--enable-frontdoor-feature` is added to the
  existing member chart.

**POC deviation (cb02d14):** the sibling chart+binary do **not
exist yet**. The AFD controllers are hosted inside
`cmd/hub-net-controller-manager` under `--enable-frontdoor-feature`
(default `false`, preserving zero-diff `helm template` output for
ATM-only installs). The chart+binary split is a Phase-4/5
prerequisite and is tracked in Proposal 003 §2.4.

## 7. Security / SFI considerations

* **Least privilege (identity split).** AFD reconciliation requires
  the `CDN Profile Contributor` role (or a custom role that grants
  `Microsoft.Cdn/*` under the AFD resource group). It MUST run under
  a **different Azure identity** from the ATM controller — otherwise
  ATM-only tenants inherit AFD write permissions they do not need.
  Authentication uses **Azure AD Workload Identity** (a projected
  federated token backed by the pod's Kubernetes ServiceAccount);
  managed-identity-with-mounted-azure.json is not used. Because a
  federated token is a **pod-level attribute** (the projected token
  path is set on the pod, not the ServiceAccount alone), the only
  way to have two Azure identities is to run two pods with two
  distinct ServiceAccounts. This is why §6 mandates a sibling
  binary+chart (`cmd/hub-afd-controller-manager` +
  `charts/hub-afd-controller-manager`) for AFD.

  > **Impossibility flag on the current POC (cb02d14).** The POC
  > hosts AFD and ATM controllers **in the same pod**
  > (`hub-net-controller-manager` under `--enable-frontdoor-feature`),
  > so today they necessarily share one Workload-Identity federated
  > subject. This means the "separate identity" requirement above is
  > **not** satisfied by cb02d14. The sibling binary+chart split
  > (§6) is a hard GA prerequisite, tracked in Proposal 003 §2.4.
  > Interim POC installs must use a WI subject bound to the *union*
  > of the AFD and ATM roles, and MUST NOT be used for production
  > SFI-NS253 workloads.
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
