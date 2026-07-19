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
| A | `api/v1alpha1/frontdoorprofile_types.go` | ~250 lines | See §3.1 |
| A | `api/v1alpha1/frontdoorbackend_types.go` | ~200 lines | See §3.2 |
| A | `api/v1alpha1/common_types.go` (edit) | ~20 lines | add `PrivateLinkService` struct shared by FD types |
| M | `api/v1alpha1/serviceexport_types.go` | ~15 lines | add optional `Spec.ExportMode` |
| M | `api/v1alpha1/internalserviceexport_types.go` | ~30 lines | add `Status.PrivateLinkService` |
| G | `api/v1alpha1/zz_generated.deepcopy.go` | auto | `make generate` |
| A | `api/v1beta1/frontdoorprofile_types.go` | ~250 lines | Phase-5 copy of v1alpha1 |
| A | `api/v1beta1/frontdoorbackend_types.go` | ~200 lines | Phase-5 copy of v1alpha1 |
| M | `api/v1beta1/serviceexport_types.go` | ~15 lines | Phase-5 mirror |
| M | `api/v1beta1/internalserviceexport_types.go` (if exists; else v1alpha1 only) | ~30 lines | see §3.3 |
| G | `api/v1beta1/zz_generated.deepcopy.go` | auto | `make generate` |

### 2.2 CRD manifests and RBAC

| Op | Path | Notes |
|----|------|-------|
| G | `config/crd/bases/networking.fleet.azure.com_frontdoorprofiles.yaml` | `make manifests` |
| G | `config/crd/bases/networking.fleet.azure.com_frontdoorbackends.yaml` | `make manifests` |
| G | `config/rbac/role.yaml` | `make manifests` — adds verbs on new resources |
| M | `charts/hub-net-controller-manager/templates/rbac.yaml` | copy generated rules |
| M | `charts/hub-net-controller-manager/values.yaml` | `frontDoor.enabled: false` + AFD identity block |
| M | `charts/hub-net-controller-manager/templates/deployment.yaml` | pass `--enable-frontdoor-feature` |
| M | `charts/hub-net-controller-manager/templates/azurecloudconfig.yaml` | optional AFD-specific overrides |
| M | `charts/member-net-controller-manager/values.yaml` | member PLS provisioner toggle |
| M | `charts/member-net-controller-manager/templates/deployment.yaml` | pass `--enable-frontdoor-feature` |
| M | `charts/hub-net-controller-manager/README.md`, `charts/member-net-controller-manager/README.md` | document new values |

### 2.3 Hub controllers

| Op | Path | Notes |
|----|------|-------|
| A | `pkg/controllers/hub/frontdoorprofile/controller.go` | see §4.1 |
| A | `pkg/controllers/hub/frontdoorprofile/controller_test.go` | table-driven unit tests |
| A | `pkg/controllers/hub/frontdoorprofile/controller_integration_test.go` | Ginkgo, envtest |
| A | `pkg/controllers/hub/frontdoorprofile/suite_test.go` | envtest scaffolding |
| A | `pkg/controllers/hub/frontdoorbackend/controller.go` | see §4.2 |
| A | `pkg/controllers/hub/frontdoorbackend/controller_test.go` | |
| A | `pkg/controllers/hub/frontdoorbackend/controller_integration_test.go` | |
| A | `pkg/controllers/hub/frontdoorbackend/suite_test.go` | |

### 2.4 Member controller changes

| Op | Path | Notes |
|----|------|-------|
| M | `pkg/controllers/member/serviceexport/controller.go` | honour `ExportMode`, project PLS annotations, populate `InternalServiceExport.status.privateLinkService` |
| M | `pkg/controllers/member/serviceexport/controller_test.go` | new test cases |
| M | `pkg/controllers/member/serviceexport/controller_integration_test.go` | new Ginkgo `Context` |
| A | `pkg/controllers/member/serviceexport/frontdoor.go` | helper file for the PLS annotation logic (keeps `controller.go` small) |
| A | `pkg/controllers/member/serviceexport/frontdoor_test.go` | |

### 2.5 Common libraries

| Op | Path | Notes |
|----|------|-------|
| A | `pkg/common/azurefrontdoor/interface.go` | client interface for testability |
| A | `pkg/common/azurefrontdoor/clientfactory.go` | wraps `armcdn.ClientFactory` |
| A | `pkg/common/azurefrontdoor/naming.go` | resource naming helpers |
| A | `pkg/common/azurefrontdoor/naming_test.go` | |
| M | `pkg/common/azureerrors/errors.go` | classify AFD-specific errors (WAF-not-found, PL-approval-pending, etc.) |
| M | `pkg/common/objectmeta/objectmeta.go` | add FD-specific labels/annotations if needed; add helpers analogous to ATM |
| M | `pkg/common/objectmeta/objectmeta_test.go` | |
| A | `pkg/common/defaulter/frontdoorprofile.go` | |
| A | `pkg/common/defaulter/frontdoorprofile_test.go` | |
| A | `pkg/common/defaulter/frontdoorbackend.go` | |
| A | `pkg/common/defaulter/frontdoorbackend_test.go` | |

### 2.6 Entry points

| Op | Path | Notes |
|----|------|-------|
| M | `cmd/hub-net-controller-manager/main.go` | add `--enable-frontdoor-feature` flag, register two controllers, add `initAzureFrontDoorClients` |
| M | `cmd/member-net-controller-manager/main.go` | add `--enable-frontdoor-feature` flag and thread through to serviceexport reconciler |
| M | `cmd/net-crd-installer/utils/util.go` (and `_test.go`) | include the two new CRDs in the installer inventory |

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

```go
// api/v1alpha1/frontdoorprofile_types.go

const FrontDoorProfileKind = "FrontDoorProfile"

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,categories={fleet-networking},shortName=fdp
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=`.status.hostName`,name="Host-Name",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.conditions[?(@.type=='Programmed')].status`,name="Is-Programmed",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.creationTimestamp`,name="Age",type=date
// +kubebuilder:validation:XValidation:rule="size(self.metadata.name) < 46",message="metadata.name max length is 45"
type FrontDoorProfile struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`

    Spec   FrontDoorProfileSpec   `json:"spec"`
    Status FrontDoorProfileStatus `json:"status,omitempty"`
}

type FrontDoorSKU string

const (
    // Premium is the only supported SKU — Private Link origins are
    // Premium-only and required for SFI-NS253. Standard and classic
    // AFD are out of scope (see Proposal 001 §2.3).
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

The 46-char metadata.name budget accounts for AFD endpoint hostname
composition (`<name>-<hash>.z01.azurefd.net`, with hash occupying up
to 16 chars) — tighter than ATM’s 63.

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

### 3.3 Additive changes to existing types

```go
// api/v1alpha1/serviceexport_types.go (partial)

type ExportMode string

const (
    ExportModeTrafficManager ExportMode = "L4-TrafficManager"
    ExportModeFrontDoor      ExportMode = "L7-FrontDoor"
)

type ServiceExportSpec struct {
    // +optional
    // +kubebuilder:validation:Enum=L4-TrafficManager;L7-FrontDoor
    // +kubebuilder:default=L4-TrafficManager
    ExportMode ExportMode `json:"exportMode,omitempty"`
}
```

Note: today `ServiceExport` has no `Spec` (verified at
`api/v1alpha1/serviceexport_types.go:51-56`).  We add one — this
requires a coordinated CRD manifest regeneration and a Helm upgrade
gate but is backward compatible because the field is optional with a
default equal to today’s implicit behaviour.

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

1. If `ServiceExport.Spec.ExportMode == L7-FrontDoor`:
   * Ensure the underlying `Service` is `type: LoadBalancer` (reject
     otherwise with a new `ServiceExportInvalid` reason
     `UnsupportedServiceTypeForFrontDoor`).
   * Ensure it carries the internal-LB + PLS annotations
     enumerated in §3.3 of proposal 001.
   * Watch the resulting `Service.Status.LoadBalancer` + the
     AKS-cloud-provider-set annotation
     `service.beta.kubernetes.io/azure-pls-resource-id`.
   * Copy the PLS resource ID into
     `InternalServiceExport.status.privateLinkService`.
2. Emit a Kubernetes `Event` on the source `ServiceExport` when the
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
* Integration test proves that a `ServiceExport` with
  `spec.exportMode: L7-FrontDoor` produces a PLS in the member
  cluster and the resource ID is reflected in
  `InternalServiceExport.status.privateLinkService.resourceID`.

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

## 7. Backward compatibility, versioning, and downgrade

* Every new field is optional with a defaulted enum value.  Existing
  `ServiceExport` YAMLs continue to apply and behave identically
  (default `ExportMode = L4-TrafficManager`).
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
| Existing consumers of `ServiceExport` | `Spec.ExportMode` is optional with a default of `L4-TrafficManager`. Existing manifests apply and behave identically. |
| MCS `weight` annotation | `networking.fleet.azure.com/weight` (used by MCS aggregation and by the ATM backend) is **not** consumed by the AFD backend controller. AFD weights are declared explicitly on `FrontDoorBackend.spec.weight` and `FrontDoorBackend.status.origins[].weight`. |
| Existing `Service` types | Only `Services` whose owning `ServiceExport` opts in with `ExportMode: L7-FrontDoor` are subject to the PLS-annotation projection described in §4.3. |

### 8.2 What the AFD path adds on top

Setting `ServiceExport.Spec.ExportMode = L7-FrontDoor` causes the
member `serviceexport` controller to layer three things onto the
underlying `Service`, alongside the east-west export that would have
happened anyway:

1. Ensure the `Service` is `type: LoadBalancer` with the
   `azure-load-balancer-internal: "true"` annotation.
2. Ensure the PLS-creation annotations (`azure-pls-*`) documented in
   §3.3 of proposal 001.
3. Copy the AKS-programmed PLS resource ID back into
   `InternalServiceExport.status.privateLinkService.resourceID`.

None of the above touches `EndpointSliceExport`, `EndpointSliceImport`,
or `ServiceImport`. The `Service.ClusterIP` is preserved on a
`type: LoadBalancer` `Service`, so an east-west consumer that
resolves via `ServiceImport` → imported `EndpointSlice` → pod IP is
byte-for-byte unchanged.

### 8.3 Guardrails to enforce

To make “east-west stays working” a checked invariant rather than an
assumption, the member `serviceexport` controller MUST reject the
opt-in when the underlying `Service` cannot be safely mutated to
`type: LoadBalancer`:

* Reject if `Service.Spec.Type == ExternalName` — surface
  `ServiceExportValid=False, Reason=UnsupportedServiceTypeForFrontDoor`.
* Reject if `Service.Spec.ClusterIP == "None"` (headless service) —
  PLS requires an ILB frontend IP; headless services can't provide one.
* Refuse to overwrite user-provided values on the annotations it
  manages; if a conflicting value is present, surface
  `Reason=ConflictingServiceAnnotations` and requeue without mutation.
* On `ExportMode` transitions **away from** `L7-FrontDoor`, strip only
  the annotations the controller itself added (tracked via a
  `fleet.networking.fleet.azure.com/afd-managed-annotations` sentinel
  annotation) — never remove user annotations.

### 8.4 Interaction with the ATM backend controller

`TrafficManagerBackend` reads endpoint IPs from the `Service`'s
external LoadBalancer IP. An operator who flips a `ServiceExport`
from `L4-TrafficManager` to `L7-FrontDoor` on a `Service` that already
has a **public** LB IP referenced by an existing `TrafficManagerBackend`
would inadvertently drop that public IP (internal LB has no public
frontend). The controller MUST detect this and refuse the transition:

* If any `TrafficManagerBackend` in the namespace references the same
  `ServiceImport`, refuse `ExportMode: L7-FrontDoor` with reason
  `ConflictsWithTrafficManagerBackend`, requiring the user to delete
  the ATM backend first (or use a distinct `Service` for AFD).

This keeps the invariant that at any point in time, a `Service` is
attached to **at most one** north-south surface, while east-west
continues to operate untouched.

### 8.5 Test coverage for east-west non-regression

Phase 3 must add integration tests that assert, for a `ServiceExport`
with `ExportMode: L7-FrontDoor`:

* `InternalServiceExport.Spec` is produced identically to the
  `L4-TrafficManager` case (byte diff on spec fields).
* `EndpointSliceExport` objects are produced identically.
* East-west round trip (pod-A in cluster-A → `ServiceImport` VIP in
  cluster-B → pod-B) still succeeds when the same `Service` is also
  fronted by AFD — covered end-to-end in phase 4 e2e as an added
  assertion, not a separate test.

## 9. Risks and mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| AFD private-endpoint approval races on cluster autoscale | New origins stuck in `Pending` | Reconciler backoff + status surface + doc using `azure-pls-auto-approval` |
| `armcdn` SDK API drift vs. `azure-sdk-for-go` version pinned by `sigs.k8s.io/cloud-provider-azure` | Build breakage | Vendor pin, `go.sum` review; wrap SDK in `pkg/common/azurefrontdoor` interface so an SDK swap is one file |
| WAF policy reference lives in a different subscription than AFD | Cross-sub RBAC errors | Support fully qualified resource ID; controller surfaces `WAFPolicyNotFound` with the exact ID |
| Two hub controllers competing for the same AFD profile | Split-brain writes | Owner-references from backends → profile; single reconciler per resource; `client.OwnerReference` gating |
| SFI review demands additional controls (e.g. mandatory managed identity, mandatory diagnostic settings) | Slippage | Track in open questions §11 of Proposal 001; add controls in phase 5 without blocking phases 1–4 |

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
1. `--enable-frontdoor-feature=true` on both hub and member managers
   yields a Programmed `FrontDoorProfile` with a reachable AFD
   endpoint, in <5 minutes.
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
