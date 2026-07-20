# Proposal 002 — Implementation Plan and Scope of Changes for AFD-based GLB

| Field       | Value                                                    |
|-------------|----------------------------------------------------------|
| Status      | Draft                                                    |
| Author      | @rchinchani_microsoft                                    |
| Created     | 2026-07-15                                               |
| Depends on  | [Proposal 001](./001-afd-global-load-balancing.md)       |
| Supersedes  | —                                                        |

This document is the executable companion to proposal 001. Where 001
answers **what** and **why**, this document answers **where** and
**how** — file-by-file, phase-by-phase, with concrete code sketches
that follow existing repo conventions.

Nothing here is meant to introduce new architectural choices; if this
document and 001 disagree, 001 wins and this one should be updated to
match.

---

## 1. Guiding conventions (extracted from the existing codebase)

The AFD implementation MUST mirror the ATM implementation's structural
choices so that reviewers, on-callers, and future contributors have
one mental model, not two.

The relevant conventions, all verified in `main`:

* **Two-CRD split** — a `*Profile` (Azure control-plane object) and
  a `*Backend` (per-`ServiceImport` binding).  See
  `api/v1beta1/trafficmanagerprofile_types.go` and
  `trafficmanagerbackend_types.go`.
* **v1alpha1 → v1beta1 graduation.**  New types land first under
  `api/v1alpha1/`, then are copied and stabilized in `api/v1beta1/`
  once they have integration coverage.  The v1beta1 copy carries
  `// +kubebuilder:storageversion`.
* **Immutability via CEL** — cross-field/immutability constraints
  are expressed as `+kubebuilder:validation:XValidation` rules on
  the type or field (e.g. `resourceGroup is immutable`,
  `spec.profile is immutable`).
* **Resource naming** — controller-managed Azure resources are
  named `fleet-<UUID>` for profiles and
  `fleet-<UUID>#<ServiceImportName>#<ClusterName>` for endpoints /
  origins.  Helpers live in `pkg/common/objectmeta`.
* **Metrics** — one `prometheus.GaugeVec` per CR type,
  `Namespace: fleet_networking`, tagged with
  `namespace, name, generation, condition, status, reason`.  See
  `pkg/controllers/hub/trafficmanagerprofile/controller.go:70-84`.
* **Azure SDK wiring** — a single `initAzure*Clients(cloudConfig)`
  helper in `cmd/hub-net-controller-manager/main.go` builds all
  clients for a feature.  See `initAzureTrafficManagerClients` at
  `main.go:248`.
* **Feature flags** — one boolean `--enable-<feature>-feature` on
  the hub and (if applicable) member manager, gating both CRD
  presence-checks and controller registration.  See
  `main.go:69` and `main.go:193-238`.
* **Fake providers** — every Azure SDK call is behind an interface
  and a fake implementation lives under
  `test/common/<feature>/fakeprovider/`, mirroring
  `test/common/trafficmanager/fakeprovider/`.

## 2. Complete file-by-file scope

The table below is the authoritative checklist of every file that
will be added (`A`), modified (`M`), or generated (`G`).  “LoE” is a
rough size estimate for planning only.

### 2.1 API types

| Op | Path | LoE | Notes |
|----|------|-----|-------|
| **Shipped (cb02d14)** | `api/v1alpha1/frontdoorprofile_types.go` | ~130 lines | POC subset only: `Spec = {ResourceGroup, Sku}`. WAF/complianceMode/health-probe/response-timeout land in Phase 4. |
| **Shipped (cb02d14)** | `api/v1alpha1/frontdoorcustomdomain_types.go` | ~215 lines | Full shape shipped; Managed TLS reconciled, BYOC field reserved but reconciliation deferred (see §3.3 below). |
| **Shipped (cb02d14)** | `pkg/common/objectmeta/objectmeta.go` (edit) | ~10 lines | Finalizer constants `FrontDoorProfileFinalizer`, `FrontDoorCustomDomainFinalizer`. |
| A | `api/v1alpha1/frontdoorbackend_types.go` | ~200 lines | Phase 4. See §3.2. |
| A | `api/v1alpha1/common_types.go` (edit) | ~20 lines | Phase 4: add `PrivateLinkService` struct shared by FD types |
| M | `api/v1alpha1/internalserviceexport_types.go` | ~30 lines | Phase 3: add `Status.PrivateLinkService` |
| M | `pkg/common/objectmeta/annotations.go` (or equivalent) | ~10 lines | Phase 3: add `ExportModeAnnotation` constant next to the existing `weight` annotation |
| M | `api/v1alpha1/frontdoorprofile_types.go` (extend) | ~120 lines | Phase 4: add `WAFPolicy`, `HealthProbe`, `OriginResponseTimeoutSeconds`, `ComplianceMode`; also tighten `Sku` enum to Premium-only (see §9 risk row). |
| G | `api/v1alpha1/zz_generated.deepcopy.go` | auto | `make generate` |
| A | `api/v1beta1/frontdoor{profile,customdomain,backend}_types.go` | ~600 lines | Phase-5 copy of v1alpha1 |
| M | `api/v1beta1/internalserviceexport_types.go` (if exists; else v1alpha1 only) | ~30 lines | see §3.4 |
| G | `api/v1beta1/zz_generated.deepcopy.go` | auto | `make generate` |

`ServiceExport` itself is **unchanged** — no new `Spec` field, no
new `Status` field. Fleet-specific intent is carried via the
`networking.fleet.azure.com/export-mode` annotation (§3.4). This
preserves upstream mcs-api (KEP-1645) parity, which is a repository
preference for `ServiceExport` / `MultiClusterService`.

**No `Location` field on `FrontDoorProfile.Spec`.** AFD is a global
service (all profiles must be created at `Location: "Global"`); the
controller sets this internally rather than exposing a
single-valued CR field.

### 2.2 CRD manifests and RBAC

| Op | Path | Notes |
|----|------|-------|
| G | `config/crd/bases/networking.fleet.azure.com_frontdoorprofiles.yaml` | **Shipped (cb02d14)**; regenerated by `make manifests` |
| G | `config/crd/bases/networking.fleet.azure.com_frontdoorcustomdomains.yaml` | **Shipped (cb02d14)** |
| G | `config/crd/bases/networking.fleet.azure.com_frontdoorbackends.yaml` | Phase 4; `make manifests` |
| G | `config/rbac/role.yaml` | `make manifests` — adds verbs on new resources |
| A | `charts/hub-afd-controller-manager/Chart.yaml` | **New sibling chart** — isolates AFD RBAC, ServiceAccount, and Workload-Identity federated subject from the ATM chart (see Proposal 001 §6, §7). |
| A | `charts/hub-afd-controller-manager/values.yaml` | AFD identity block (`serviceAccount.azure.workloadIdentity.clientID`, `tenantID`), image, resources, `frontDoor.enabled: true` (single-purpose chart). |
| A | `charts/hub-afd-controller-manager/templates/deployment.yaml` | Runs `cmd/hub-afd-controller-manager` with `--enable-frontdoor-feature`. |
| A | `charts/hub-afd-controller-manager/templates/rbac.yaml` | AFD-only cluster role: verbs on `frontdoorprofiles`, `frontdoorcustomdomains`, `frontdoorbackends`. |
| A | `charts/hub-afd-controller-manager/templates/serviceaccount.yaml` | SA carrying the WI annotation with the AFD-scoped MI. |
| A | `charts/hub-afd-controller-manager/templates/pdb.yaml` | `PodDisruptionBudget` for Safeguards / Automatic (§6.5). |
| A | `charts/hub-afd-controller-manager/README.md` | Document AFD RBAC, WI setup, `frontDoor.enabled` (always true when this chart is installed at all). |
| — | `charts/hub-net-controller-manager/**` | **Unchanged for AFD.** ATM chart keeps its own SA/MI. Zero-diff `helm template` output preserved via baselines under `.github/.copilot/breadcrumbs/baselines/`. |
| M | `charts/member-net-controller-manager/values.yaml` | Member PLS provisioner toggle (Phase 3). |
| M | `charts/member-net-controller-manager/templates/deployment.yaml` | Phase 3: pass `--enable-frontdoor-feature` (member controller is a Service-annotation reader; no Azure SDK, no separate identity needed). |
| M | `charts/hub-net-controller-manager/README.md`, `charts/member-net-controller-manager/README.md` | Cross-reference the new sibling chart. |

**POC deviation (cb02d14):** the sibling chart does **not** exist
yet. The POC wires AFD registration into
`cmd/hub-net-controller-manager/main.go` behind
`--enable-frontdoor-feature` (default `false`). This preserves the
existing ATM chart's `helm template` output but violates the
identity-split invariant from Proposal 001 §7. The sibling chart
+ sibling `cmd/` binary is a hard GA prerequisite (Proposal 003
§2.4).

### 2.3 Hub controllers

| Op | Path | Notes |
|----|------|-------|
| **Shipped (cb02d14)** | `pkg/controllers/hub/frontdoorprofile/controller.go` | Happy-path reconciler with finalizer; see §4.1. |
| **Shipped (cb02d14)** | `pkg/controllers/hub/frontdoorcustomdomain/controller.go` | Managed TLS path only; BYOC deferred. |
| A | `pkg/controllers/hub/frontdoorprofile/controller_test.go` | table-driven unit tests — not yet in cb02d14 |
| A | `pkg/controllers/hub/frontdoorprofile/controller_integration_test.go` | Ginkgo, envtest — not yet in cb02d14 |
| A | `pkg/controllers/hub/frontdoorprofile/suite_test.go` | envtest scaffolding — not yet in cb02d14 |
| A | `pkg/controllers/hub/frontdoorcustomdomain/{controller,suite}_test.go` | as above |
| A | `pkg/controllers/hub/frontdoorbackend/controller.go` | Phase 4; see §4.2 |
| A | `pkg/controllers/hub/frontdoorbackend/controller_test.go` | |
| A | `pkg/controllers/hub/frontdoorbackend/controller_integration_test.go` | |
| A | `pkg/controllers/hub/frontdoorbackend/suite_test.go` | |

### 2.4 Member controller changes

| Op | Path | Notes |
|----|------|-------|
| M | `pkg/controllers/member/serviceexport/controller.go` | resolve export mode (annotation, else infer from Service), populate `InternalServiceExport.status.privateLinkService`; never mutate the Service |
| M | `pkg/controllers/member/serviceexport/controller_test.go` | new test cases |
| M | `pkg/controllers/member/serviceexport/controller_integration_test.go` | new Ginkgo `Context` |
| A | `pkg/controllers/member/serviceexport/frontdoor.go` | helper file for the PLS annotation logic (keeps `controller.go` small) |
| A | `pkg/controllers/member/serviceexport/frontdoor_test.go` | |

### 2.5 Common libraries

| Op | Path | Notes |
|----|------|-------|
| **Shipped (cb02d14)** | `pkg/common/azurefrontdoor/client.go` | Single-file: `Config`+`LoadConfigFromEnv`, `NewCredential` (Workload Identity), `NewClients` bundling `Profiles`, `AFDEndpoints`, `AFDCustomDomains`. |
| A | `pkg/common/azurefrontdoor/interface.go` | Phase 4: extract client interface for testability once fake provider is added. |
| A | `pkg/common/azurefrontdoor/naming.go` | Phase 4: promote the `fleet-<UID>` helpers out of the profile controller as more resource kinds are added. |
| A | `pkg/common/azurefrontdoor/naming_test.go` | |
| M | `pkg/common/azureerrors/errors.go` | classify AFD-specific errors (WAF-not-found, PL-approval-pending, etc.) |
| **Shipped (cb02d14)** | `pkg/common/objectmeta/objectmeta.go` | Finalizer constants: `FrontDoorProfileFinalizer = "networking.fleet.azure.com/frontdoor-profile-cleanup"`, `FrontDoorCustomDomainFinalizer = ".../frontdoor-custom-domain-cleanup"`. |
| M | `pkg/common/objectmeta/objectmeta_test.go` | |
| A | `pkg/common/defaulter/frontdoorprofile.go` | Phase 4 (arrives with WAF/complianceMode fields). |
| A | `pkg/common/defaulter/frontdoorprofile_test.go` | |
| A | `pkg/common/defaulter/frontdoorbackend.go` | Phase 4 |
| A | `pkg/common/defaulter/frontdoorbackend_test.go` | |

### 2.6 Entry points

| Op | Path | Notes |
|----|------|-------|
| A | `cmd/hub-afd-controller-manager/main.go` | **New sibling binary** — registers only the AFD controllers, mounts only the AFD WI subject. Enables the identity split from Proposal 001 §7. |
| M | `cmd/member-net-controller-manager/main.go` | Phase 3: add `--enable-frontdoor-feature` flag and thread through to serviceexport reconciler. |
| M | `cmd/net-crd-installer/utils/util.go` (and `_test.go`) | Include the three new CRDs in the installer inventory. |
| — | `cmd/hub-net-controller-manager/main.go` | **Reverted to ATM-only for GA.** The POC (cb02d14) currently registers AFD here behind `--enable-frontdoor-feature` as a bridge; the sibling-binary migration removes those wires before Phase 5. |

**POC deviation (cb02d14):** `cmd/hub-afd-controller-manager/main.go`
does **not** exist yet. Instead, `cmd/hub-net-controller-manager/main.go`
gained ~65 lines gated by `--enable-frontdoor-feature`
(`enableFrontDoorFeature` var, `initAzureFrontDoorClients` call site,
registration of `frontdoorprofile.Reconciler` + `frontdoorcustomdomain.Reconciler`).
The migration is: extract those lines into the new binary,
delete them from `hub-net-controller-manager`, add the new chart
(§2.2). Nothing about the reconciler packages themselves has to
change.

### 2.7 Tests

| Op | Path | Notes |
|----|------|-------|
| A | `test/common/frontdoor/fakeprovider/profile.go` | analog of `test/common/trafficmanager/fakeprovider/profile.go` |
| A | `test/common/frontdoor/fakeprovider/origin.go` | origin groups + origins |
| A | `test/common/frontdoor/fakeprovider/route.go` | routes + security policies |
| A | `test/common/frontdoor/validator/profile.go` | test assertion helpers |
| A | `test/common/frontdoor/validator/backend.go` | |
| A | `test/common/frontdoor/azureprovider/profile.go` | real Azure client for e2e |
| M | `test/apis/v1alpha1/api_validation_integration_test.go` | cover new CRDs and CEL rules |
| M | `test/apis/v1beta1/api_validation_integration_test.go` | Phase-5 |
| A | `test/e2e/frontdoor_test.go` | Ginkgo e2e suite |
| M | `test/e2e/e2e_test.go` | wire the new suite behind an env-gate |
| M | `test/scripts/bootstrap.sh` | provision AFD Premium profile RG + WAF policy |
| A | `hack/cl2/manifests/test-fdp.yaml`, `test-fdb.yaml` | scale-test manifests |
| A | `hack/cl2/afd_scale_test_config.yaml` | scale-test driver |

### 2.8 Examples and docs

| Op | Path | Notes |
|----|------|-------|
| A | `examples/getting-started/artifacts/afd.yaml` | analog of `atm.yaml` |
| A | `docs/concepts/HTTPBasedGlobalLoadBalancing/README.md` | user-facing concept doc |
| A | `docs/howtos/frontdoor-permissions-setup.md` | least-privilege setup guide |
| A | `docs/toubleshooting/HTTPBasedGlobalLoadBalancing.md` | note: keeps existing folder’s typo `toubleshooting` |
| A | `docs/demos/FrontDoorProfile/…` | (optional, phase 5) |

### 2.9 Module manifest

| Op | Path | Notes |
|----|------|-------|
| M | `go.mod`, `go.sum` | add `github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn` (Standard/Premium AFD lives under this SDK, not `armfrontdoor` which is classic) |

---

## 3. API sketches

The Go snippets below are indicative — not final.  They exist so that
reviewers can point at concrete fields and CEL rules rather than
prose.

### 3.1 `FrontDoorProfile`

> **POC status (cb02d14).** The reconciler and CRD have shipped, but
> the fields marked `POC:` are the only ones currently present.
> `ComplianceMode`, `WAFPolicy`, `HealthProbe`,
> `OriginResponseTimeoutSeconds` and their CEL rules land alongside
> the WAF work in Phase 4. Similarly, the `Sku` enum is currently
> permissive (accepts Standard too); tightening it to Premium-only
> is a Phase-4 change (see §9 risks).

```go
// api/v1alpha1/frontdoorprofile_types.go

const FrontDoorProfileKind = "FrontDoorProfile"

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,categories={fleet-networking},shortName=afdp
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=`.status.endpointHostname`,name="Endpoint",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=='Programmed')].status`,name="Is-Programmed",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date
// +kubebuilder:validation:XValidation:rule="size(self.metadata.name) < 64",message="metadata.name max length is 63"
type FrontDoorProfile struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`

    Spec   FrontDoorProfileSpec   `json:"spec"`
    Status FrontDoorProfileStatus `json:"status,omitempty"`
}

type FrontDoorSKU string

const (
    // Premium is the only supported SKU — Private Link origins are
    // Premium-only and required for SFI-NS253 (see Proposal 001 §2.3
    // and Proposal 003 checklist §1.2). Standard is intentionally
    // excluded at the CRD enum layer so misconfiguration is rejected
    // at admission time, not surfaced as a Programmed=False condition
    // hours later.
    //
    // POC status (cb02d14): the enum currently also lists
    // Standard_AzureFrontDoor. Tightening to Premium-only is tracked
    // as a Phase-4 change (see §9 risks) — a CRD-only edit, no
    // controller changes required.
    FrontDoorSKUPremium FrontDoorSKU = "Premium_AzureFrontDoor"
)

// ComplianceMode declares the security/compliance regime a profile
// (and its backends) must satisfy. See Proposal 001 §2.1.
type ComplianceMode string

const (
    // ComplianceModeNone imposes no additional constraints beyond the
    // structural ones. Suitable for dev/test or non-first-party use.
    ComplianceModeNone ComplianceMode = "None"

    // ComplianceModeSFINS253 enforces:
    //   * spec.wafPolicy is required          (CEL on the profile)
    //   * every referencing FrontDoorBackend must have
    //     spec.privateLink.enabled = true     (enforced in the backend reconciler,
    //                                          surfaced as Accepted=False,
    //                                          Reason=SFIComplianceViolation)
    //   * WAF policy MUST be in Prevention mode (surfaced as
    //                                          Programmed=False,
    //                                          Reason=WAFPolicyNotInPreventionMode)
    ComplianceModeSFINS253 ComplianceMode = "SFI-NS253"
)

type FrontDoorProfileSpec struct {
    // +kubebuilder:validation:MinLength=1
    // +kubebuilder:validation:MaxLength=90
    // +kubebuilder:validation:XValidation:rule="self == oldSelf",message="resourceGroup is immutable"
    ResourceGroup string `json:"resourceGroup"`

    // Only Premium_AzureFrontDoor is supported; the enum is
    // single-valued so the field is effectively fixed but stays
    // present for forward compatibility.
    // +kubebuilder:validation:Enum=Premium_AzureFrontDoor
    // +kubebuilder:default=Premium_AzureFrontDoor
    // +kubebuilder:validation:XValidation:rule="self == oldSelf",message="sku is immutable"
    SKU FrontDoorSKU `json:"sku,omitempty"`

    // ComplianceMode declares the security/compliance regime this
    // profile is subject to. When set to "SFI-NS253", the CEL rule
    // below requires spec.wafPolicy, and the FrontDoorBackend
    // reconciler additionally requires PrivateLink on every
    // backend that references this profile. Immutable: switching
    // out of SFI-NS253 mode would silently weaken guarantees the
    // operator relied on — recreate the profile instead.
    // +optional
    // +kubebuilder:validation:Enum=None;SFI-NS253
    // +kubebuilder:default=None
    // +kubebuilder:validation:XValidation:rule="self == oldSelf",message="complianceMode is immutable"
    ComplianceMode ComplianceMode `json:"complianceMode,omitempty"`

    // WAFPolicy is REQUIRED when complianceMode == SFI-NS253.
    // Optional otherwise, so third-party dev/test flows can create
    // an AFD profile without a WAF attach.
    // +optional
    // +kubebuilder:validation:XValidation:rule="self.complianceMode != 'SFI-NS253' || has(self.wafPolicy)",message="wafPolicy is required when complianceMode is SFI-NS253"
    WAFPolicy *FrontDoorWAFPolicyRef `json:"wafPolicy,omitempty"`

    // +optional
    HealthProbe *FrontDoorHealthProbe `json:"healthProbe,omitempty"`

    // +optional
    // +kubebuilder:validation:Minimum=16
    // +kubebuilder:validation:Maximum=240
    // +kubebuilder:default=60
    OriginResponseTimeoutSeconds *int32 `json:"originResponseTimeoutSeconds,omitempty"`
}

type FrontDoorWAFPolicyRef struct {
    // ResourceID of an existing
    // Microsoft.Network/frontdoorwebapplicationfirewallpolicies resource.
    // Mutually exclusive with Inline.
    // +optional
    ResourceID string `json:"resourceID,omitempty"`

    // Inline creation is opt-in — most first-party services will
    // reference a centrally managed WAF policy.
    // +optional
    Inline *InlineWAFPolicy `json:"inline,omitempty"`
}

type FrontDoorProfileStatus struct {
    // +optional
    HostName *string `json:"hostName,omitempty"`
    // +optional
    ResourceID string `json:"resourceID,omitempty"`
    // +optional
    EndpointResourceID string `json:"endpointResourceID,omitempty"`

    // +optional
    // +listType=map
    // +listMapKey=type
    Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

const (
    FrontDoorProfileConditionProgrammed = "Programmed"

    FrontDoorProfileReasonProgrammed              = "Programmed"
    FrontDoorProfileReasonInvalid                 = "Invalid"
    FrontDoorProfileReasonPending                 = "Pending"
    FrontDoorProfileReasonWAFPolicyNotFound       = "WAFPolicyNotFound"
    FrontDoorProfileReasonWAFPolicyNotInPrevention = "WAFPolicyNotInPreventionMode"
    FrontDoorProfileReasonHostNameNotAvailable    = "HostNameNotAvailable"
)
```

The 63-char `metadata.name` cap is Kubernetes' standard label-value
limit; Azure resource names are derived from the CR's UID
(`fleet-<UID>` — 42 chars) rather than `metadata.name`, so a CR
rename does not orphan the Azure profile and the AFD 46-char
endpoint-name cap is always respected regardless of what the user
chooses for `metadata.name`.

### 3.2 `FrontDoorBackend`

```go
// api/v1alpha1/frontdoorbackend_types.go

const FrontDoorBackendKind = "FrontDoorBackend"

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,categories={fleet-networking},shortName=fdb
// +kubebuilder:subresource:status
type FrontDoorBackend struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`

    Spec   FrontDoorBackendSpec   `json:"spec"`
    Status FrontDoorBackendStatus `json:"status,omitempty"`
}

type FrontDoorBackendSpec struct {
    // +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.profile is immutable"
    Profile FrontDoorProfileRef `json:"profile"`

    // +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.backend is immutable"
    Backend FrontDoorBackendRef `json:"backend"`

    // +optional
    // +kubebuilder:validation:Minimum=1
    // +kubebuilder:validation:Maximum=1000
    // +kubebuilder:default=100
    Weight *int32 `json:"weight,omitempty"`

    // +optional
    Routing *FrontDoorRoutingConfig `json:"routing,omitempty"`

    // PrivateLink.enabled MUST be true for SFI-NS253 workloads.
    // +optional
    PrivateLink *FrontDoorPrivateLinkConfig `json:"privateLink,omitempty"`
}

type FrontDoorRoutingConfig struct {
    // +optional
    // +kubebuilder:default={"/*"}
    PatternsToMatch []string `json:"patternsToMatch,omitempty"`

    // +optional
    // +kubebuilder:validation:Enum=HttpOnly;HttpsOnly;MatchRequest
    // +kubebuilder:default=HttpsOnly
    ForwardingProtocol string `json:"forwardingProtocol,omitempty"`

    // +optional
    // +kubebuilder:default={"Https"}
    SupportedProtocols []string `json:"supportedProtocols,omitempty"`

    // +optional
    // +kubebuilder:validation:Enum=Enabled;Disabled
    // +kubebuilder:default=Enabled
    LinkToDefaultDomain string `json:"linkToDefaultDomain,omitempty"`
}

type FrontDoorPrivateLinkConfig struct {
    // +kubebuilder:default=true
    Enabled bool `json:"enabled"`

    // +optional
    // +kubebuilder:validation:MaxLength=140
    RequestMessage string `json:"requestMessage,omitempty"`
}

type FrontDoorBackendStatus struct {
    // Origins is the list of AFD origins created under the profile
    // for this backend.  One entry per member-cluster serviceExport.
    // +optional
    Origins []FrontDoorOriginStatus `json:"origins,omitempty"`

    // +optional
    // +listType=map
    // +listMapKey=type
    Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

type FrontDoorOriginStatus struct {
    Name       string  `json:"name"`
    ResourceID string  `json:"resourceID,omitempty"`
    Weight     *int32  `json:"weight,omitempty"`
    HostName   *string `json:"hostName,omitempty"` // PLS alias FQDN
    PrivateLinkResourceID string `json:"privateLinkResourceID,omitempty"`
    PrivateEndpointStatus string `json:"privateEndpointStatus,omitempty"` // Pending|Approved|Rejected|Disconnected
    // +optional
    From *FromCluster `json:"from,omitempty"`
}
```

### 3.3 `FrontDoorCustomDomain`

Shipped in `cb02d14`. See the source for the complete Go shape
(`api/v1alpha1/frontdoorcustomdomain_types.go`). Key design points
worth calling out for future reviewers:

* **Namespaced, `shortName: afdcd`.**
* **Immutable `ProfileRef` and `Hostname`.** Retargeting a custom
  domain to a different profile is not a supported mutation — delete
  and recreate.
* **Same-namespace-only `ProfileRef`.** Cross-namespace references
  are rejected by design (breadcrumb D4): the AFD tenancy story
  requires that a custom domain and its owning profile share an RBAC
  boundary.
* **TLS.Mode enum: `Managed` or `BYOC`.** `Managed` is fully
  reconciled today; `BYOC` field shape is present (Key Vault URI +
  certificate name + optional version) but the reconciler does not
  yet call the Key Vault binding APIs. CEL cross-field validation
  already enforces that `keyVaultCertificate` is present iff
  `Mode == BYOC`.
* **DNS validation surface.** `Status` exposes `ValidationState`
  (Pending | Approved | Rejected | TimedOut | InternalError |
  Submitting | RefreshingValidationToken | Unknown),
  `DNSValidationToken`, and `DNSValidationExpiry`. The tenant
  publishes a TXT record at `_dnsauth.<hostname>` with the token
  value; AFD polls DNS, then the controller reflects `Approved` and
  transitions `Programmed=True`.
* **Condition reasons on `Programmed`:** `Programmed`, `Invalid`,
  `ProfileNotReady`, `AwaitingDNSValidation`, `ValidationFailed`,
  `TLSFailed`, `AzureError`, `Pending`.
* **Finalizer:** `networking.fleet.azure.com/frontdoor-custom-domain-cleanup`.

Route binding (attaching a validated custom domain to an AFD route)
lives with `FrontDoorBackend` in Phase 4 — a `FrontDoorCustomDomain`
by itself only validates ownership and provisions the AFD-side
resource; it does not front any traffic.

### 3.4 Additive changes to existing types

**`ServiceExport` gains no schema change.** Per Proposal 001 §3.3 /
§4.2, mode selection is carried by an annotation, matching the
existing `networking.fleet.azure.com/weight` precedent and preserving
upstream mcs-api (KEP-1645) parity.

```go
// pkg/common/objectmeta/annotations.go (partial)

const (
    // ExportModeAnnotation, when set on a ServiceExport, selects the
    // north-south surface the exported Service should be attached to.
    // Absence of the annotation is equivalent to L4-TrafficManager
    // (today's implicit default). See Proposal 001 §3.3 for the
    // annotation-vs-inference precedence rule.
    ExportModeAnnotation = "networking.fleet.azure.com/export-mode"

    ExportModeValueTrafficManager = "L4-TrafficManager"
    ExportModeValueFrontDoor      = "L7-FrontDoor"
)
```

No CRD manifest regeneration is required for `ServiceExport`; only
`InternalServiceExport` gains the `Status.PrivateLinkService` block
below.

```go
// api/v1alpha1/internalserviceexport_types.go (partial)

type ServiceExportPrivateLinkStatus struct {
    ResourceID string `json:"resourceID"`
    Alias      string `json:"alias,omitempty"`
    InternalLoadBalancerFrontendIP string `json:"internalLoadBalancerFrontendIP,omitempty"`
    LastProbedTime metav1.Time `json:"lastProbedTime,omitempty"`
}

type InternalServiceExportStatus struct {
    // ... existing fields ...

    // +optional
    PrivateLinkService *ServiceExportPrivateLinkStatus `json:"privateLinkService,omitempty"`
}
```

---

## 4. Controller sketches

### 4.1 Hub `frontdoorprofile` reconciler

```go
// pkg/controllers/hub/frontdoorprofile/controller.go (skeleton)

package frontdoorprofile

const (
    ControllerName = "frontdoorprofile-controller"

    AzureResourceProfileNameFormat  = "fleet-%s"
    AzureResourceEndpointNameFormat = "fleet-%s-endpoint"

    profileEventReasonAzureAPIError = "AzureAPIError"
    profileEventReasonProgrammed    = "Programmed"
    profileEventReasonDeleted       = "Deleted"
)

type Reconciler struct {
    client.Client

    ProfilesClient        *armcdn.ProfilesClient
    AFDEndpointsClient    *armcdn.AFDEndpointsClient
    SecurityPoliciesClient *armcdn.SecurityPoliciesClient
    Recorder              record.EventRecorder
}

//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=frontdoorprofiles,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=frontdoorprofiles/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.fleet.azure.com,resources=frontdoorprofiles/finalizers,verbs=update

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // 1. Fetch FrontDoorProfile
    // 2. Add finalizer / handle deletion (mirror TM profile)
    // 3. Apply defaults via defaulter.SetDefaultsFrontDoorProfile
    // 4. Ensure Microsoft.Cdn/profiles
    // 5. Ensure Microsoft.Cdn/profiles/afdEndpoints
    // 6. If spec.wafPolicy: ensure Microsoft.Cdn/profiles/securityPolicies binding to endpoint
    // 7. Update status.hostName, status.resourceID, status.endpointResourceID
    // 8. Set condition Programmed
    // 9. Emit event + metric
}
```

Reconciler ordering, event names, finalizer semantics, and status
patching all copy the shape of
`pkg/controllers/hub/trafficmanagerprofile/controller.go` verbatim
where possible.

### 4.2 Hub `frontdoorbackend` reconciler

Additional inputs beyond the profile reconciler:

* `AFDOriginGroupsClient`, `AFDOriginsClient`, `RoutesClient`
* A field indexer on `InternalServiceExport.status.privateLinkService.resourceID`
  so that PLS status changes on the member side trigger a hub-side
  requeue.

Watches:

* Owned: `FrontDoorBackend` (primary).
* Enqueue-on-change:
  * `FrontDoorProfile` (name/namespace match) — recompute if profile
    is (re)programmed.
  * `ServiceImport` (spec.backend.name).
  * `InternalServiceExport` (indexed by exported service reference).

Origin naming: `fleet-<FrontDoorBackendUID>#<ServiceImportName>#<ClusterName>`.

Private-endpoint approval:

* When `privateLink.enabled = true`, the AFD API creates a
  private endpoint connection on the PLS.
* If the PLS was created with `azure-pls-auto-approval` including
  the AFD subscription, the connection reaches `Approved` without
  further action.
* Otherwise, the reconciler surfaces status
  `PrivateEndpointStatus = Pending` and sets the backend condition
  `Accepted=False, Reason=PrivateLinkPending`, waiting for
  approval.

Cross-check with profile compliance mode:

* When the referenced `FrontDoorProfile.spec.complianceMode == SFI-NS253`
  and this backend has `privateLink == nil` or
  `privateLink.enabled == false`, the reconciler refuses to create
  any origin and surfaces
  `Accepted=False, Reason=SFIComplianceViolation` with a message
  explaining that the parent profile is in SFI mode. This closes the
  loophole where an operator could create an SFI-compliant profile
  but then attach a lax backend to it.

### 4.3 Member `serviceexport` extension

Today `serviceexport.controller.go` translates a
`ServiceExport` + `Service` into an `InternalServiceExport` in the
member-cluster’s reserved hub namespace.

Additions:

1. **Mode resolution** (per Proposal 001 §3.3):
   * Read the `networking.fleet.azure.com/export-mode` annotation
     on the `ServiceExport`. If set to `L7-FrontDoor`, treat as AFD
     mode.
   * If the annotation is unset, infer AFD mode when the exported
     `Service` carries **all** of the following annotations:
     `azure-load-balancer-internal: "true"`,
     `azure-pls-create: "true"`,
     `azure-pls-name`,
     `azure-pls-ip-configuration-subnet`,
     `azure-pls-visibility`,
     `azure-pls-auto-approval`.
     Otherwise treat as `L4-TrafficManager` (today's behaviour).
   * If the annotation demands `L7-FrontDoor` but the Service is
     not internal + PLS-enabled, surface
     `ServiceExportValid=False,
     Reason=ExportModeAnnotationServiceMismatch` and do not
     mutate anything. **The controller never writes annotations
     to the Service** — the internal-LB + PLS annotations are
     tenant-owned (via GitOps and/or platform admission policy).
2. When AFD mode is resolved, additionally require:
   * `Service.Spec.Type == LoadBalancer` (reject otherwise with
     `ServiceExportInvalid` reason
     `UnsupportedServiceTypeForFrontDoor`).
   * `Service.Spec.ClusterIP != "None"` (headless services cannot
     back a PLS; reject with the same reason).
3. Watch the `Service.Status.LoadBalancer` and the
   AKS-cloud-provider-set annotation
   `service.beta.kubernetes.io/azure-pls-resource-id`, and copy
   the PLS resource ID into
   `InternalServiceExport.status.privateLinkService`.
4. Emit a Kubernetes `Event` on the source `ServiceExport` when the
   PLS transitions to `Approved`.

Nothing in this controller talks to Azure directly — the AKS cloud
provider does the PLS provisioning. That keeps member-side RBAC
identical to today.

---

## 5. `cmd/hub-net-controller-manager/main.go` diff sketch

```go
// Additions (illustrative — actual line numbers will differ):

import (
    "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn"
    // ...
    "go.goms.io/fleet-networking/pkg/controllers/hub/frontdoorbackend"
    "go.goms.io/fleet-networking/pkg/controllers/hub/frontdoorprofile"
)

var (
    enableFrontDoorFeature = flag.Bool("enable-frontdoor-feature", false,
        "If set, the Azure Front Door feature will be enabled.")

    frontDoorFeatureRequiredGVKs = []schema.GroupVersionKind{
        fleetnetv1alpha1.GroupVersion.WithKind(fleetnetv1alpha1.FrontDoorProfileKind),
        fleetnetv1alpha1.GroupVersion.WithKind(fleetnetv1alpha1.FrontDoorBackendKind),
    }
)

// ... inside main(), after the existing enableTrafficManagerFeature block:

if *enableFrontDoorFeature {
    for _, gvk := range frontDoorFeatureRequiredGVKs {
        if err = utils.CheckCRDInstalled(discoverClient, gvk); err != nil {
            klog.ErrorS(err, "Unable to find required Front Door CRD", "GVK", gvk)
            exitWithErrorFunc()
        }
    }
    cloudConfig, err := azure.NewCloudConfigFromFile(*cloudConfigFile)
    if err != nil { /* ... */ }
    cloudConfig.SetUserAgent("fleet-hub-net-controller-manager")

    fdClients, err := initAzureFrontDoorClients(cloudConfig)
    if err != nil { /* ... */ }

    if err := (&frontdoorprofile.Reconciler{
        Client:                 mgr.GetClient(),
        ProfilesClient:         fdClients.Profiles,
        AFDEndpointsClient:     fdClients.Endpoints,
        SecurityPoliciesClient: fdClients.SecurityPolicies,
        Recorder:               mgr.GetEventRecorderFor(frontdoorprofile.ControllerName),
    }).SetupWithManager(mgr); err != nil { /* ... */ }

    if err := (&frontdoorbackend.Reconciler{
        Client:                mgr.GetClient(),
        ProfilesClient:        fdClients.Profiles,
        AFDOriginGroupsClient: fdClients.OriginGroups,
        AFDOriginsClient:      fdClients.Origins,
        RoutesClient:          fdClients.Routes,
        Recorder:              mgr.GetEventRecorderFor(frontdoorbackend.ControllerName),
    }).SetupWithManager(ctx, mgr, true); err != nil { /* ... */ }
}

// initAzureFrontDoorClients mirrors initAzureTrafficManagerClients at cmd/hub-net-controller-manager/main.go:248.
type frontDoorClientBundle struct {
    Profiles         *armcdn.ProfilesClient
    Endpoints        *armcdn.AFDEndpointsClient
    OriginGroups     *armcdn.AFDOriginGroupsClient
    Origins          *armcdn.AFDOriginsClient
    Routes           *armcdn.RoutesClient
    SecurityPolicies *armcdn.SecurityPoliciesClient
}

func initAzureFrontDoorClients(cloudConfig *azure.CloudConfig) (*frontDoorClientBundle, error) {
    // exact same authProvider + options + rate-limit-policy pattern as ATM
    // then construct all six clients via armcdn.NewClientFactory
}
```

---

## 6. Phased delivery — one PR per phase

Each phase is designed to land as a **reviewable, mergeable** PR that
does not regress ATM behavior.  The AFD feature flag stays `false`
until Phase 4.

> **Current phase status (as of cb02d14).**
> - **Phase 1:** *Partially delivered.* Two of three CRDs shipped
>   (`FrontDoorProfile`, `FrontDoorCustomDomain`) with immutability
>   CEL. Not yet in `main`: `FrontDoorBackend` type, `WAFPolicy` /
>   `ComplianceMode` / `HealthProbe` / `OriginResponseTimeoutSeconds`
>   fields, Sku enum tightened to Premium-only, defaulters, envtest
>   scaffolding, `test/apis` coverage.
> - **Phase 2:** *Partially delivered.* Happy-path `frontdoorprofile`
>   and `frontdoorcustomdomain` reconcilers shipped, with WI-based
>   Azure client factory (`pkg/common/azurefrontdoor`) and finalizers.
>   Not yet: unit tests, integration tests, fake provider, sibling
>   binary+chart (currently hosted inside `cmd/hub-net-controller-manager`
>   under `--enable-frontdoor-feature`).
> - **Phases 3–5:** not started.
>
> The "Deliverables" and "Exit criteria" bullets below describe the
> *complete* phase; treat items already in `main` as done and the
> rest as remaining work.

### Phase 1 — API types + generated artifacts + defaulters (~1 week)

Deliverables:
* All files under §2.1 (v1alpha1 only), §2.2 (CRD manifests + RBAC),
  and the four `defaulter` files from §2.5.
* Unit tests for defaulters and CEL rules.
* No controller registered anywhere — controllers do not compile
  yet, only the types.

Exit criteria:
* `make generate manifests` produces stable output.
* `go test ./api/... ./pkg/common/defaulter/...` green.
* `test/apis/v1alpha1/api_validation_integration_test.go` covers
  every CEL rule on the new types.

### Phase 2 — Hub `frontdoorprofile` controller (~1.5 weeks)

Deliverables:
* §2.3 profile files, §2.5 azurefrontdoor client interface + naming,
  `initAzureFrontDoorClients` in `cmd/hub-net-controller-manager/main.go`,
  and the flag registration.
* Fake provider `test/common/frontdoor/fakeprovider/profile.go`.
* Integration test uses the fake provider under envtest.

Exit criteria:
* Feature flag `--enable-frontdoor-feature=true` on a dev cluster
  produces an AFD profile + endpoint + (optional) WAF security policy
  in Azure, observable via `az afd profile show`.
* No AFD backend controller registered yet.

### Phase 3 — Member PLS provisioner + status plumbing (~1 week)

Deliverables:
* §2.4 changes to member `serviceexport`.
* The additive field on `InternalServiceExport.Status`.
* Integration test proves that a `ServiceExport` resolved to
  `L7-FrontDoor` (annotation or inference) with a `Service` that
  has the internal-LB + PLS annotations produces a PLS in the member
  cluster and the resource ID is reflected in
  `InternalServiceExport.status.privateLinkService.resourceID`.
  A companion test proves that an annotation-driven mismatch
  (Service missing the annotations) surfaces
  `ExportModeAnnotationServiceMismatch` and does not mutate anything.

Exit criteria:
* Member controller does not require any Azure SDK — the AKS cloud
  provider does the PLS creation via Service annotations.
* Passing test in `pkg/controllers/member/serviceexport/`.

### Phase 4 — Hub `frontdoorbackend` controller + e2e (~2 weeks)

Deliverables:
* §2.3 backend files, updated fakeprovider (origin, route,
  private-endpoint approval).
* Origin/route/security-policy reconciliation.
* Wire the backend controller into `main.go`.
* `test/e2e/frontdoor_test.go` — full end-to-end path: create
  `FrontDoorProfile` + two member `ServiceExport` (L7) + a
  `FrontDoorBackend` → curl the AFD hostname → traffic reaches at
  least one origin.

Exit criteria:
* Full e2e passes in the ci-e2e pipeline gated by an env var.
* SFI reviewer signs off on the produced Azure topology.

### Phase 5 — v1beta1 promotion, docs, GA (~1 week)

Deliverables:
* Everything under `api/v1beta1/` from §2.1.
* `test/apis/v1beta1/api_validation_integration_test.go`.
* Concept, howto, and troubleshooting docs from §2.8.
* Flip default of `--enable-frontdoor-feature` to `true`.

Exit criteria:
* v1beta1 marked `storageversion`.
* Feature announced in root `README.md` alongside ATM.

Total nominal effort: **~6.5 weeks** for one engineer, or **~4 weeks**
with a second engineer taking phases 2/3 in parallel with phases 4
API + fake provider work.

---

## 6.5 AKS Automatic compatibility

Proposal 001 §3.4 declares AKS Automatic a supported member cluster
SKU. This section enumerates the concrete chart / manifest changes
that keep the fleet-networking components installable on Automatic
alongside the existing AKS Standard install path.

### 6.5.1 Deployment Safeguards requirements

AKS Automatic runs Azure Policy safeguards in Enforcement mode by
default. The hub and member Deployments MUST satisfy at least the
following (all standard restricted-workload hygiene):

* Every container declares `resources.requests` and `resources.limits`
  for `cpu` and `memory`.
* `securityContext.runAsNonRoot: true` and `runAsUser` >= 1000 on
  every container.
* `securityContext.allowPrivilegeEscalation: false`.
* `securityContext.capabilities.drop: ["ALL"]`; no `add:` unless
  strictly required.
* `securityContext.readOnlyRootFilesystem: true` where compatible
  (may require an `emptyDir` for `/tmp` or logs).
* `securityContext.seccompProfile.type: RuntimeDefault` at pod scope.
* No `hostPath`, `hostNetwork`, `hostPID`, or `hostIPC`.
* Container images pulled from an allow-listed registry
  (`mcr.microsoft.com` or the tenant's ACR — never Docker Hub for
  first-party installs).
* Every Deployment ships a matching `PodDisruptionBudget` with
  `minAvailable: 1` (or `maxUnavailable: 0` if a single replica).

### 6.5.2 File-by-file additions

| Op | Path | Notes |
|----|------|-------|
| M | `charts/hub-net-controller-manager/templates/deployment.yaml` | Add `resources`, `securityContext`, and `readOnlyRootFilesystem` for the hub manager container; add `emptyDir` for `/tmp` if `readOnlyRootFilesystem: true`. |
| M | `charts/member-net-controller-manager/templates/deployment.yaml` | Same, for the member manager container. |
| M | `charts/hub-net-controller-manager/templates/pdb.yaml` (new) | `PodDisruptionBudget` for the hub manager. |
| M | `charts/member-net-controller-manager/templates/pdb.yaml` (new) | `PodDisruptionBudget` for the member manager. |
| M | `charts/hub-net-controller-manager/values.yaml` | Surface `resources`, `securityContext`, and `image.registry` as configurable values (default to safeguards-compliant values). |
| M | `charts/member-net-controller-manager/values.yaml` | Same. |
| A | `hack/verify-safeguards.sh` | Optional helper: `helm template` each chart and run `kubectl-safeguards` (or equivalent) offline; wired into `Makefile` under a new `verify-safeguards` target. |

None of the above changes are Automatic-specific — they are strict
generalisations that also apply cleanly on AKS Standard. There is
no chart branch, no conditional templating.

### 6.5.3 Node auto-provisioning (NAP)

AKS Automatic uses NAP: nodes come and go as workloads scale. To
avoid controller flap when NAP evicts the leader replica:

* Deployments carry `spec.replicas: 2` (already the case for the
  hub manager; align the member manager if it currently ships as a
  single replica).
* Pod anti-affinity (`preferredDuringSchedulingIgnoredDuringExecution`,
  `topologyKey: kubernetes.io/hostname`) spreads replicas across
  nodes.
* Leader-election lease durations (already tuned in
  `cmd/*-net-controller-manager/main.go`) tolerate a ~30s replica
  restart window.

### 6.5.4 e2e coverage

Phase 4 e2e (§11 below) MUST include at least one AKS Automatic
member alongside AKS Standard members. The test framework in
`test/e2e/framework/cluster.go` needs an `AksSKU` field on the
per-cluster config with `Standard | Automatic` values; the ci-e2e
pipeline stands up one of each and asserts that a `FrontDoorBackend`
programs origins successfully for both.

### 6.5.5 Documentation

* `docs/first-party/README.md` — add a short "Member cluster SKUs"
  paragraph pointing to Proposal 001 §3.4 and this §6.5.
* `docs/howtos/frontdoor-permissions-setup.md` (added in Phase 5) —
  include an "AKS Automatic checklist" appendix mirroring §3.4 of
  Proposal 001.

---

## 7. Backward compatibility, versioning, and downgrade

* Every new field on `FrontDoor*` types is optional with a defaulted
  value. `ServiceExport` itself gains no schema change; absence of
  the `networking.fleet.azure.com/export-mode` annotation is
  equivalent to today's `L4-TrafficManager` behaviour. Existing
  `ServiceExport` YAMLs continue to apply and behave identically.
* Downgrade: since both new CRDs are gated by
  `--enable-frontdoor-feature`, an operator can downgrade by
  disabling the flag and deleting all `FrontDoor*` CRs.  The CRD
  manifests themselves stay installed (removing them mid-flight
  would strand finalizers).
* Storage version: v1alpha1 is `storageversion` in phase 1–4.  The
  phase-5 promotion adds a conversion webhook only if we introduce
  breaking field changes; the plan is to keep v1beta1 field-identical
  to v1alpha1 for the first cut so no webhook is needed.

## 8. East-west (MCS) compatibility

This proposal is a **north-south** feature (internet → member cluster
via AFD). The pre-existing **east-west** (in-fleet, cluster-to-cluster)
data plane is:

```
ServiceExport (member)
  → InternalServiceExport  (hub, per-cluster shard)
  → ServiceImport          (hub, aggregated)
  → InternalServiceImport  (member)
  → local ClusterSet IP + imported EndpointSlices
     from EndpointSliceExport / EndpointSliceImport
```

Traffic between clusters resolves to **pod IPs** via imported
`EndpointSlice`s. It does not traverse any external load balancer,
Traffic Manager, or Front Door. The two directions of traffic share
`ServiceExport` as the entry-point CR but otherwise use disjoint
control paths.

### 8.1 What is guaranteed to keep working

| Concern | Guarantee |
|---------|-----------|
| `ServiceImport` aggregation | Untouched. `ServiceImport`, `InternalServiceImport`, and `EndpointSlice{Export,Import}` types are not modified. |
| Pod-to-pod fleet traffic | Uses `EndpointSlice` imports, which carry pod IPs. Neither the addition of an internal LB nor a PLS on the origin `Service` changes pod IPs. |
| Existing consumers of `ServiceExport` | `ServiceExport` schema is **unchanged**. Mode selection is an opt-in annotation (`networking.fleet.azure.com/export-mode`); absence keeps today's `L4-TrafficManager` behaviour. Existing manifests apply and behave identically. |
| MCS `weight` annotation | `networking.fleet.azure.com/weight` (used by MCS aggregation and by the ATM backend) is **not** consumed by the AFD backend controller. AFD weights are declared explicitly on `FrontDoorBackend.spec.weight` and `FrontDoorBackend.status.origins[].weight`. |
| Existing `Service` types | Only `Services` that carry the internal-LB + PLS annotations (either authored by the tenant/GitOps or required by a platform admission policy) trigger the AFD path described in §4.3. A `ServiceExport` opted in via annotation but pointing at a Service without those annotations surfaces `ExportModeAnnotationServiceMismatch` and does not mutate anything. |

### 8.2 What the AFD path adds on top

When the member `serviceexport` controller resolves an export to
`L7-FrontDoor` (either by annotation on the `ServiceExport` or by
inference from the Service's own annotations — see §4.3), the
controller does exactly one thing beyond the east-west export that
would have happened anyway:

* Copy the AKS-programmed PLS resource ID from
  `service.beta.kubernetes.io/azure-pls-resource-id` on the
  `Service` into `InternalServiceExport.status.privateLinkService`.

The internal-LB + PLS annotations on the `Service` itself are
authored by the tenant / GitOps or enforced by a platform admission
policy — **not** written by this controller. That preserves the
existing ownership boundary: the tenant owns the `Service`, Fleet
owns the `ServiceExport` → `InternalServiceExport` mirror.

None of the above touches `EndpointSliceExport`, `EndpointSliceImport`,
or `ServiceImport`. The `Service.ClusterIP` is preserved on a
`type: LoadBalancer` `Service`, so an east-west consumer that
resolves via `ServiceImport` → imported `EndpointSlice` → pod IP is
byte-for-byte unchanged.

### 8.3 Guardrails to enforce

Because the member `serviceexport` controller does not mutate the
tenant-owned `Service`, the guardrails become validation-only —
surfaced as conditions on the `ServiceExport` — rather than
mutation refusals:

* If the `networking.fleet.azure.com/export-mode` annotation is
  `L7-FrontDoor` but the `Service` is `type: ExternalName`, headless
  (`ClusterIP: None`), or missing the required internal-LB + PLS
  annotations, surface
  `ServiceExportValid=False, Reason=ExportModeAnnotationServiceMismatch`
  and requeue without mutation.
* If inference selects `L7-FrontDoor` (annotation unset, Service has
  full internal-LB + PLS annotations) but the `Service` is `type:
  ExternalName` or headless, surface
  `ServiceExportValid=False, Reason=UnsupportedServiceTypeForFrontDoor`.
* The controller never adds or removes annotations on the `Service`.
  All ATM ↔ AFD transitions are driven by the tenant / GitOps
  editing the `Service` and/or `ServiceExport`. This avoids the
  "who wins" fight between the controller and admission policy.

### 8.4 Interaction with the ATM backend controller

`TrafficManagerBackend` reads endpoint IPs from the `Service`'s
external LoadBalancer IP. If a tenant flips the underlying `Service`
to internal + PLS (or opts the export in to AFD via annotation while
the Service is already internal + PLS), the existing
`TrafficManagerBackend` would immediately lose its public IP and
start failing probes. The AFD backend controller MUST refuse to
program AFD origins in this case:

* If any `TrafficManagerBackend` in the namespace already references
  the same `ServiceImport`, refuse to accept a `FrontDoorBackend`
  for that `ServiceImport` with reason
  `ConflictsWithTrafficManagerBackend`, requiring the user to delete
  the ATM backend first (or use a distinct `Service` for AFD).

This keeps the invariant that at any point in time, a `Service` is
attached to **at most one** north-south surface, while east-west
continues to operate untouched.

### 8.5 Test coverage for east-west non-regression

Phase 3 must add integration tests that assert, for a `ServiceExport`
resolved to `L7-FrontDoor` (via annotation or inference):

* `InternalServiceExport.Spec` is produced identically to the
  `L4-TrafficManager` case (byte diff on spec fields).
* `EndpointSliceExport` objects are produced identically.
* East-west round trip (pod-A in cluster-A → `ServiceImport` VIP in
  cluster-B → pod-B) still succeeds when the same `Service` is also
  fronted by AFD — covered end-to-end in phase 4 e2e as an added
  assertion, not a separate test.
* Annotation set to `L7-FrontDoor` against a Service missing
  internal-LB + PLS annotations surfaces
  `ExportModeAnnotationServiceMismatch` and produces no
  `PrivateLinkService` status.

## 9. Risks and mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| AFD private-endpoint approval races on cluster autoscale | New origins stuck in `Pending` | Reconciler backoff + status surface + doc using `azure-pls-auto-approval` |
| `armcdn` SDK API drift vs. `azure-sdk-for-go` version pinned by `sigs.k8s.io/cloud-provider-azure` | Build breakage | Vendor pin, `go.sum` review; wrap SDK in `pkg/common/azurefrontdoor` interface so an SDK swap is one file |
| WAF policy reference lives in a different subscription than AFD | Cross-sub RBAC errors | Support fully qualified resource ID; controller surfaces `WAFPolicyNotFound` with the exact ID |
| Two hub controllers competing for the same AFD profile | Split-brain writes | Owner-references from backends → profile; single reconciler per resource; `client.OwnerReference` gating |
| SFI review demands additional controls (e.g. mandatory managed identity, mandatory diagnostic settings) | Slippage | Track in open questions §11 of Proposal 001; add controls in phase 5 without blocking phases 1–4 |
| `networking.fleet.azure.com/export-mode` annotation set to `L7-FrontDoor` on a `Service` that lacks the internal-LB + PLS annotations | Silent AFD misconfiguration if the controller falls back to L4 | Controller surfaces `ServiceExportValid=False, Reason=ExportModeAnnotationServiceMismatch` and does not fall back; a platform admission policy (Kyverno / Gatekeeper) can additionally reject the mismatch at write-time to give tenants an immediate error |
| Charts drift from AKS Automatic Deployment Safeguards (missing resource limits, root user, `hostPath`, non-allow-listed image) | Install blocked on Automatic member clusters even when Standard works | Chart hygiene enumerated in §6.5.1; pre-merge `hack/verify-safeguards.sh` runs `helm template` + a policy check offline; Phase 4 e2e installs on at least one Automatic cluster |
| Node auto-provisioning (NAP) on AKS Automatic restarts the leader controller replica during scale events | Reconciliation stalls for the leader-election lease duration on every NAP scale | Two replicas per manager, pod anti-affinity across nodes, and leader-election lease tuned to tolerate a ~30s restart window (§6.5.3) |
| POC `FrontDoorProfile.Spec.Sku` enum permissively accepts `Standard_AzureFrontDoor` | An SFI-intended tenant creates a Standard profile, gets no admission-time rejection, then discovers at backend-creation time that Private Link origins are unavailable | Phase-4 CRD tightening removes `Standard_AzureFrontDoor` from the enum. Interim: document Premium-only requirement in the POC README; add an early-reconcile check that surfaces `Programmed=False, Reason=Invalid, Message="Standard SKU is not supported for SFI-NS253"` immediately. |
| POC hosts AFD + ATM controllers in the same pod (shared Workload-Identity subject) | Violates Proposal 001 §7 identity-split invariant; ATM-only tenants inherit AFD write permissions if the shared subject is used in production | Sibling binary+chart split (`cmd/hub-afd-controller-manager`, `charts/hub-afd-controller-manager`) is a hard GA prerequisite (Proposal 003 §2.4). POC installs must be gated by an explicit "non-production" acknowledgement in the chart values and are excluded from SFI-NS253 conformance. |
| `FrontDoorBackend` controller (which enforces the AFD/ATM coexistence invariant — a `ServiceImport` cannot be a backend on both surfaces) does not exist in the POC | Coexistence guard from Proposal 001 §3.5 is unenforced until Phase 4 | Document as an intentional gap in the POC README; e2e in Phase 4 asserts the guard. Nothing in the POC produces AFD origins yet, so the exposure is limited to future manual `az cli` misconfiguration. |
| Custom domain BYOC (Key Vault) reconciliation is not yet implemented | Tenants who set `TLS.Mode: BYOC` see the CR admission-time validation pass but reconciliation surfaces `Programmed=False, Reason=TLSFailed` | Managed mode is documented as the only supported path in the POC. Phase 4 adds Key Vault binding (`AzureKeyVault` secret) reconciliation. |

## 10. Out-of-scope for this proposal

* **L4 (non-HTTP/HTTPS) workloads.** AFD is L7-only (HTTP, HTTPS,
  WebSockets-over-HTTPS). This proposal therefore does not extend
  SFI-NS253 compliance to raw TCP, UDP, gRPC-over-plain-TCP,
  databases, SMTP, DNS, etc. Those workloads either stay on ATM
  (non-compliant with SFI-NS253) or wait for a separate L4 proposal
  (likely Azure Cross-region Load Balancer with Private Link
  backends).
* Rules engine / URL rewriting inside AFD.
* Multi-region AFD failover policies (uses AFD-native latency /
  weighted / priority load balancing implicitly).
* Custom domains + managed TLS certificates — **deferred to Phase 5
  of this proposal**, not to a separate proposal. See §12 for the
  forward-compatible field shape that Phase 2 must reserve.
* IPv6 origins (AFD limitation, not ours).
* An umbrella `GlobalLoadBalancer` CRD unifying ATM and AFD (open
  question §11.4 of Proposal 001).

## 11. Success criteria

Feature is considered done when **all** of the following hold on
`main`:
1. Installing the sibling AFD chart (`charts/hub-afd-controller-manager`)
   yields a Programmed `FrontDoorProfile` with a reachable AFD
   endpoint, in <5 minutes. (POC bridge: `--enable-frontdoor-feature=true`
   on `hub-net-controller-manager` produces the same effect but does
   not satisfy the §7 identity split.)
2. A `FrontDoorBackend` referencing an L7-mode `ServiceImport` reaches
   `Accepted=True` and produces one AFD origin per member cluster,
   each with `PrivateEndpointStatus=Approved`.
3. `curl https://<afd-endpoint-hostname>/` returns 200 from a workload
   running on the member cluster, via Private Link.
4. Existing ATM e2e tests continue to pass unchanged.
5. Documentation under `docs/first-party/`, `docs/concepts/`, and
   `docs/howtos/` is merged and reviewed by an SFI reviewer.
6. Fleet-networking maintainers approve the design and the SFI-NS253
   KPI dashboard reflects compliance for at least one first-party
   adopter.
7. The hub and member charts install cleanly on an **AKS Automatic**
   member cluster (Deployment Safeguards in Enforcement mode) and
   pass the same e2e as an AKS Standard member. Covered by the
   Phase 4 e2e matrix (§6.5.4).
