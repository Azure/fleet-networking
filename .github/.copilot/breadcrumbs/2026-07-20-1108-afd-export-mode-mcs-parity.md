# AFD Export-Mode — MCS-API Parity

## Requirements

Update the AFD proposal (`docs/first-party/001..003`) so it no longer
adds a Fleet-specific `Spec.ExportMode` field to `ServiceExport`.
That divergence conflicts with the repository preference to keep
`ServiceExport` / `MultiClusterService` structurally aligned with
upstream mcs-api (KEP-1645) and to carry Fleet-specific configuration
via annotations.

Adopt a two-signal model instead:

1. **Primary — inference from the `Service`.** The member controller
   determines that an exported Service is destined for the AFD
   (private-origin) path by observing the Azure cloud-provider
   annotations already required to create an internal LB and a PLS
   (`service.beta.kubernetes.io/azure-load-balancer-internal: "true"`
   and `service.beta.kubernetes.io/azure-pls-create: "true"`).
   No CR schema change; the mode is emergent from the Service's
   actual shape.
2. **Fallback / opt-in intent flag — annotation on `ServiceExport`.**
   `networking.fleet.azure.com/export-mode: L7-FrontDoor | L4-TrafficManager`
   matches the existing `networking.fleet.azure.com/weight`
   precedent and lets tenants declare intent independently of the
   Service's current annotation state.

Precedence when both are present: **annotation wins** if set;
otherwise infer from the Service. Conflict (annotation says
`L7-FrontDoor` but Service lacks internal+PLS annotations)
surfaces `ServiceExportValid=False` with a new
`Reason=ExportModeAnnotationServiceMismatch`.

## Additional comments from user

- Stored preference (user memory): *"In fleet-networking, prefer no
  divergence from upstream mcs-api (KEP-1645) shapes for
  ServiceExport/MultiClusterService; carry Fleet-specific config via
  annotations, not new Spec fields."*
- Precedence rule chosen by user in this session:
  *"Annotation wins if set; otherwise infer from Service"* — see
  `ask_user` turn on 2026-07-20.

## Plan

Docs-only change; no code yet (proposal not implemented).

1. `docs/first-party/001-afd-global-load-balancing.md`
   - §3.3 (member-controller behaviour): reword to describe
     inference-based detection, and add optional annotation as the
     explicit-intent surface. Remove the language that says the
     controller "projects" annotations onto the Service — the tenant
     (or platform admission policy) owns the Service annotations;
     the controller only observes them.
   - §4.2 (additive CRD changes): drop the `Spec.ExportMode`
     bullet on `ServiceExport`; keep only the
     `InternalServiceExport.Status.PrivateLinkService` addition.
     Add a sentence about the annotation-based opt-in and the
     precedence rule.
2. `docs/first-party/002-afd-implementation-plan.md`
   - §2.1 (file table): remove the two `serviceexport_types.go`
     modification rows (v1alpha1 and v1beta1). Keep the
     `internalserviceexport_types.go` rows.
   - §3.3: replace the `ExportMode` Go sketch with an "Annotation
     constants" sketch under `pkg/common/objectmeta/` (or wherever
     the `weight` annotation lives) and a short note that no
     `ServiceExport` schema change is required.
   - §4.3 (member `serviceexport` extension): rewrite the
     additions to:
     - Read the annotation first; if unset, infer from the
       Service's internal-LB + PLS annotations.
     - Validate presence/absence of the required Service
       annotations; if the annotation demands AFD but the Service
       is not internal+PLS, surface the mismatch and do not mutate
       the Service.
     - Copy the AKS-programmed PLS resource ID into
       `InternalServiceExport.status.privateLinkService`.
     - Explicitly state that the member controller does **not**
       write to the Service (annotations are tenant-owned).
   - §8 (east-west compatibility): update the tables/prose that
     currently mention `Spec.ExportMode` default behaviour to
     describe the annotation/inference model instead.
   - §9 (risks): add a row about
     "annotation says AFD, Service lacks PLS annotations"
     precedence and validation.
3. `docs/first-party/003-pre-implementation-checklist.md`
   - Close open item **1.5** with the decision documented above.
   - No new open items — the precedence and mismatch handling
     are settled by this update.

## Decisions

- **No new `Spec` field on `ServiceExport`.** Preserves upstream
  mcs-api parity per the repo preference.
- **Primary signal: Service shape.** The Service's internal-LB and
  PLS annotations are the physical prerequisite for AFD anyway; using
  them as the signal collapses two sources of truth into one.
- **Opt-in annotation on `ServiceExport`.** Kept as a tenant-visible
  intent flag for platforms that want to declare mode before the
  Service is fully provisioned (e.g., GitOps pipelines that apply
  the `ServiceExport` in one wave and the `Service` in the next).
- **Annotation wins when both are set.** Explicit intent overrides
  observed shape; mismatches surface a validation condition.
- **Controller does not mutate tenant-owned Services.** The proposal
  originally described "projecting" annotations onto the Service; that
  crosses ownership lines and complicates ATM ↔ AFD transitions.
  Ownership stays with the tenant / GitOps / admission policy.

## Implementation Details

See per-file edits applied to `001`, `002`, `003` under
`docs/first-party/`. This breadcrumb accompanies those edits — no
code changes in this pass.

## Changes Made

- `docs/first-party/001-afd-global-load-balancing.md` — §3.3 and
  §4.2 reworded.
- `docs/first-party/002-afd-implementation-plan.md` — §2.1, §3.3,
  §4.3, §8, §9 updated.
- `docs/first-party/003-pre-implementation-checklist.md` — item
  1.5 marked resolved.

## Before/After Comparison

| Aspect | Before | After |
|---|---|---|
| ServiceExport schema | Grew a new `Spec.ExportMode` enum | Unchanged — matches upstream mcs-api |
| Mode signal (primary) | Explicit `Spec.ExportMode` field | Inferred from Service's `azure-load-balancer-internal` + `azure-pls-*` annotations |
| Mode signal (opt-in) | n/a | `networking.fleet.azure.com/export-mode` annotation on `ServiceExport` |
| Precedence | n/a | Annotation wins if set; else inference; mismatch surfaces validation condition |
| Member controller writes to Service? | Yes (annotation projection) | No — tenants/GitOps own Service annotations |

## References

- KEP-1645 (Multi-Cluster Services API): <https://github.com/kubernetes/enhancements/tree/master/keps/sig-multicluster/1645-multi-cluster-services-api>
- Upstream mcs-api reference implementation: <https://github.com/kubernetes-sigs/mcs-api>
- Proposal 001: `docs/first-party/001-afd-global-load-balancing.md` (Draft)
- Proposal 002: `docs/first-party/002-afd-implementation-plan.md` (Draft)
- Checklist 003: `docs/first-party/003-pre-implementation-checklist.md` (Open)
- Existing Fleet-specific annotation precedent: `networking.fleet.azure.com/weight` on `ServiceExport` (see `pkg/common/objectmeta`).
- AKS internal LB annotations: <https://learn.microsoft.com/azure/aks/internal-lb>
- AKS PLS annotations: <https://learn.microsoft.com/azure/aks/internal-lb#create-a-private-link-service>

---

## Addendum — AKS Automatic support + AFD/ATM coexistence (2026-07-20)

Follow-up in the same session. Two additions on top of the
export-mode change above.

### Requirements (addendum)

1. Declare **AKS Automatic** a first-class supported member cluster
   SKU alongside AKS Standard. AFD + PLS data-plane primitives are
   identical on both SKUs; the only differences are operational
   (BYO VNet planning, Deployment Safeguards).
2. Explicitly document **coexistence with the existing Traffic
   Manager (ATM) path**: fleet-wide, same-namespace-different-Service
   coexistence is supported; same-`ServiceImport`-on-both-surfaces is
   forbidden by the AFD backend reconciler.
3. Capture the security implications (bypass surface, split
   identities, WAF-per-surface, audit split) and the tenancy
   implications (weight-annotation divergence, per-surface quotas,
   cost attribution, migration cadence) in the proposal itself
   rather than only in chat.

### Plan (addendum)

* `docs/first-party/001-afd-global-load-balancing.md`
  * New **§3.4** — Member cluster requirements (AKS Standard +
    Automatic): Standard LB, BYO VNet, PLS NAT subnet with
    `privateLinkServiceNetworkPolicies: Disabled`, PLS auto-approval
    for the AFD subscription. Automatic-specific notes for
    Deployment Safeguards, NAP, cluster-config lockdown, AGC
    coexistence.
  * New **§3.5** — Coexistence with ATM: coexistence-granularity
    table, security implications, tenancy implications, migration
    playbook.
  * §2.3 non-goals: add "managing member-cluster provisioning" as
    an explicit non-goal, pointing at §3.4.
* `docs/first-party/002-afd-implementation-plan.md`
  * New **§6.5** — AKS Automatic compatibility: Safeguards
    requirements, file table (Deployment `securityContext` /
    `resources`, PDBs, `values.yaml` surface, `hack/verify-safeguards.sh`),
    NAP handling, e2e coverage note, doc pointers.
  * §9 risks: two new rows — Safeguards drift, and NAP-driven
    controller restarts.
  * §11 success criteria: add criterion 7 — clean install on an AKS
    Automatic member with Safeguards in Enforcement mode.
* `docs/first-party/003-pre-implementation-checklist.md`
  * New **§1.6** — closed: "AKS Automatic as supported member SKU"
    — Yes, first-class alongside Standard.
  * New spike **§3.7** — AKS Automatic Deployment Safeguards install
    validation: `helm template` + policy check offline, fold gaps
    into §6.5.2 chart hygiene work before Phase 4.
  * §6 readiness table gains an "AKS Automatic install validated"
    row.

### Decisions (addendum)

- **AKS Automatic is supported.** No code branch; the safeguards-clean
  chart is the correct chart for AKS Standard too. Data-plane code
  paths are identical.
- **BYO VNet is a platform responsibility, not a Fleet
  responsibility.** Fleet controllers do not provision or reshape
  subnets; the PLS NAT subnet must exist with the correct
  network-policy at cluster create time.
- **AFD and ATM coexist behind independent feature flags.** No plan
  to deprecate ATM. Same-`ServiceImport`-on-both-surfaces is
  forbidden at reconcile time (Proposal 002 §8.4).
- **Compliance is per-tenant / per-Service, not per-fleet.** A fleet
  running both surfaces is not "SFI compliant" as a whole; only
  AFD-fronted Services are. First-party tenancy classes that must be
  AFD-only should be enforced via cluster admission policy that
  denies ATM CRs in those namespaces.
- **ATM → AFD migration is staged, not hot-swapped.** Add a second
  Service with internal LB + PLS, verify AFD, cut DNS, delete the
  ATM Service — no controller-driven cutover.

### Changes Made (addendum)

- `docs/first-party/001-afd-global-load-balancing.md` — new §3.4
  and §3.5 added; §2.3 non-goals extended.
- `docs/first-party/002-afd-implementation-plan.md` — new §6.5
  added; §9 risks and §11 success criteria extended.
- `docs/first-party/003-pre-implementation-checklist.md` — item
  1.6 added (resolved), spike 3.7 added, §6 readiness table
  extended.

### References (addendum)

- AKS Automatic overview: <https://learn.microsoft.com/azure/aks/intro-aks-automatic>
- AKS Deployment Safeguards: <https://learn.microsoft.com/azure/aks/deployment-safeguards>
- AKS Node auto-provisioning: <https://learn.microsoft.com/azure/aks/node-autoprovision>
- AKS Application Gateway for Containers: <https://learn.microsoft.com/azure/application-gateway/for-containers/overview>
- Azure Private Link Service network-policy prerequisite: <https://learn.microsoft.com/azure/private-link/disable-private-link-service-network-policy>


---

## Addendum 2 (2026-07-20 T19:00Z): reconcile design docs with the cb02d14 POC

### Requirements
Reconcile Proposals 001/002/003 with the POC that landed as
`cb02d14ba31b1a42f5ec51821c7bf3b856836581`
("feat(hub): POC for Azure Front Door (AFD) controllers behind
`--enable-frontdoor-feature`"). Flag anything that is impossible or
incompatible under the current wiring rather than silently
resolving it.

### User inputs during the conversation
- **Choice A (SKU):** Premium-only. Drop `Standard_AzureFrontDoor`
  from the CRD enum (Phase-4 change; document the current POC
  permissiveness as a risk).
- **Choice B (identity split):** B-ii — sibling binary
  (`cmd/hub-afd-controller-manager`) + sibling chart
  (`charts/hub-afd-controller-manager`) is the target model, so §7
  of Proposal 001 is satisfiable. Docs-only this session
  (**B-ii/a**); actual code split lands in a follow-up commit.

### Facts extracted from cb02d14 by direct code reads
- **API types shipped:**
  - `api/v1alpha1/frontdoorprofile_types.go` — Spec = {ResourceGroup,
    Sku}; Status = {ResourceID, EndpointHostname, Conditions}; Sku
    enum permissively includes both `Standard_AzureFrontDoor` and
    `Premium_AzureFrontDoor`; immutability CEL on ResourceGroup and
    Sku; metadata.name < 64 CEL.
  - `api/v1alpha1/frontdoorcustomdomain_types.go` — Spec =
    {ProfileRef (same-namespace, immutable), Hostname (immutable),
    TLS = {Mode (Managed|BYOC), KeyVaultCertificate}}; Status =
    {ResourceID, ValidationState, DNSValidationToken,
    DNSValidationExpiry, Conditions}. BYOC reconciliation deferred.
  - Neither `FrontDoorBackend` nor member-side changes are in cb02d14.
- **Controllers shipped:**
  - `pkg/controllers/hub/frontdoorprofile/controller.go` — happy-path
    reconcile; finalizer `networking.fleet.azure.com/frontdoor-profile-cleanup`;
    Azure name `fleet-<UID>`; Azure Location hardcoded to `Global`.
  - `pkg/controllers/hub/frontdoorcustomdomain/controller.go`
    (Managed TLS only).
  - No unit tests, no envtest scaffolding, no fake provider yet.
- **Client library shipped:** `pkg/common/azurefrontdoor/client.go`
  (single file). Auth = **Workload Identity** via
  `azidentity.NewWorkloadIdentityCredential`; env vars
  `AZURE_TENANT_ID`, `AZURE_CLIENT_ID`,
  `AZURE_FEDERATED_TOKEN_FILE`, `AZURE_SUBSCRIPTION_ID`. Bundles
  `Profiles`, `AFDEndpoints`, `AFDCustomDomains` sub-clients.
  `armcdn v1.1.1` pinned in `go.mod`.
- **Wiring in `cmd/hub-net-controller-manager/main.go`** — flag
  `--enable-frontdoor-feature` (default `false`); when true, calls
  `azurefrontdoor.LoadConfigFromEnv() → NewCredential → NewClients`
  and registers both AFD reconcilers on the shared manager. **No
  sibling binary or sibling chart.**

### Incompatibilities / impossibilities flagged in docs
1. **Identity split (SFI blocker).** POC hosts AFD + ATM in one pod
   → one WI federated subject → §7 not satisfied. Sibling
   binary+chart is a hard GA prerequisite. Flagged in 001 §7
   "Impossibility flag on the current POC", 002 §2.6 and §9 risks,
   003 §2.4 and readiness table.
2. **SKU enum permissiveness.** POC allows Standard; docs require
   Premium-only. Flagged as a Phase-4 CRD tightening in 002 §3.1
   Sku comment and §9 risks.
3. **`FrontDoorBackend` unimplemented.** Docs described it as a peer
   to `FrontDoorProfile`; marked "not yet in main; Phase 4" in 001
   §4.1.2 / §5.1 and 002 §2.3.
4. **AFD/ATM coexistence guard unenforced.** Depends on
   `FrontDoorBackend`; flagged in 002 §9.
5. **BYOC (Key Vault) TLS unimplemented.** Field shape present but
   reconciler no-ops; flagged in 002 §9.
6. **Custom domain route binding deferred.** A `FrontDoorCustomDomain`
   by itself does not front traffic; needs `FrontDoorBackend`.
   Noted in 002 §3.3.

### Files touched this session (Addendum 2)
- `docs/first-party/001-afd-global-load-balancing.md`
  - §3.2 rewritten to describe the `fleet-<UID>` naming pattern and
    the AFD-endpoint 46-char cap that motivates it.
  - §4.1.1 `shortName: afdp` (was `fdp`); illustrative Go now marks
    `POC:` fields vs. post-POC fields; Status shape corrected
    (`EndpointHostname` string, `ResourceID` string, no separate
    `EndpointResourceID`).
  - §4.1.2 `shortName: afdb`; added "not yet implemented" callout.
  - **New §4.1.3** `FrontDoorCustomDomain` (`afdcd`) — full
    description of Spec/Status/Conditions/finalizer.
  - §4.3 lists three CRD YAMLs, notes two are shipped.
  - §5.1 marks profile+customdomain controllers "shipped in cb02d14",
    backend controller "not yet in main; Phase 4".
  - §5.2 clarifies member changes are Phase 3.
  - §5.3 describes target sibling-binary wiring; documents POC
    deviation.
  - §5.4 aligns to actual `azurefrontdoor/client.go` and finalizer
    constants.
  - **§6 rewritten** to describe the sibling-chart target model with
    an explicit "POC deviation" paragraph.
  - **§7 rewritten** — Workload Identity, identity-split impossibility
    flag on cb02d14.
- `docs/first-party/002-afd-implementation-plan.md`
  - §2.1 file table: marks shipped rows, adds custom-domain type,
    drops Location from Spec, notes Sku tightening as future work.
  - §2.2 chart section: new sibling chart `charts/hub-afd-controller-manager`;
    ATM chart unchanged; POC deviation call-out.
  - §2.3 controllers table: shipped/pending markers.
  - §2.5 common libs: aligned to shipped `client.go`.
  - §2.6 entry points: new `cmd/hub-afd-controller-manager/main.go`;
    documents the POC bridge.
  - §3.1 `shortName: afdp`; metadata.name < 64 CEL; Sku comment
    describes POC vs. GA; note about UID-based naming.
  - **New §3.3** describes shipped `FrontDoorCustomDomain`.
  - Renumbered old §3.3 → §3.4 (additive changes to existing types);
    updated cross-references.
  - §6 phase table: prepended "Current phase status" block.
  - §9 risks: two new rows (Sku permissiveness, identity split) plus
    two more (backend coexistence guard unenforced, BYOC deferred).
  - §11 success criterion 1: references sibling chart install as the
    canonical form; POC bridge as bridge.
- `docs/first-party/003-pre-implementation-checklist.md`
  - §1.3 (custom domains): resolved — separate CRD in Phase 2.
  - §2.4 (security review): resolved and expanded — sibling
    binary+chart is a hard GA prerequisite.
  - §3.2 (armcdn compat): resolved — pinned at v1.1.1.
  - §3.6 (azcloudconfig): resolved — WI supersedes it.
  - §6 readiness table: added SFI identity-split row and "POC
    reconciled with docs" row.
  - Fixed §3.3 → §3.4 citation.

### Not touched
- The ~130 unrelated unstaged files in `api/`, `pkg/`, `cmd/`,
  `test/`, `hack/`.
- Any code files (per B-ii/a: docs only this session; code split
  is a follow-up commit).

---

## Addendum 3 (2026-07-20 T22:00Z-07:00): landing the reconciled design

This addendum records the 11-commit series that closes the delta the
reconciliation pass (Addendum 2) identified between the `cb02d14`
POC and the target design in Proposals 001/002/003. All commits land
on branch `rchinchani/afd-first-party-proposal`; envtests green in
WSL and go vet / go build clean on Windows for every commit.

### Commit series (in landing order)

| # | SHA | Scope |
|---|-----|-------|
| 0a | `2fecf8d` | AFD SKU Premium-only enum tightening |
| 0b | `8367527` | CRD regen for §0a |
| 0c | `fcb37f2` | Split `hub-afd-controller-manager` binary |
| 0d | `5672313` | Dockerfile + Makefile for §0c |
| 0e | `c219ca2` | `charts/hub-afd-controller-manager` sibling chart |
| 0f | `668b56f` | `armcdn` v2 bump to unlock OriginGroup+Origins fakes |
| 0g | `9b6a337` | net-crd-installer test fixture repair |
| 0h | `73e4150` | frontdoorprofile envtest scaffolding |
| 1  | `aeb116c` | `FrontDoorProfileSpec.wafPolicy` + `complianceMode` |
| 2  | `5b52f84` | deepcopy + CRD regen for §1 |
| 3  | `95a0096` | Client bundle: WAFPolicies + SecurityPolicies |
| 4  | `b2cf58f` | Reconciler: attach WAF policy via SecurityPolicy |
| 5  | `4b85044` | envtest specs for WAF (happy / NotFound / SFI Detection) |
| 6a | `b85a115` | `objectmeta`: export-mode annotation + extractor |
| 6b | `c6d0d8e` | `InternalServiceExportSpec.ExportMode` + `PrivateLinkServiceResourceID` |
| 6c | `cb6f23a` | Member `serviceexport` reconciler: ExportMode + PLS ARM Get |
| 6d | `cf5324b` | Unit + integration tests for §6c |
| 7  | `c61dc43` | `FrontDoorBackend` v1alpha1 CRD |
| 8  | `afe12d7` | Client bundle: OriginGroups + Origins |
| 9  | `be0ccb2` | `frontdoorbackend` reconciler (happy / Invalid / Pending) |
| 10 | `ceac0ab` | AFD/ATM coexistence guard (`Conflict` reason) |
| 11 | `629644b` | Fake OriginGroup/Origin providers + envtest specs |
| doc | `d2ff7df` | Docs: competitive context (GKE/EKS/AKS parity) in 001 §2 |

Plus this commit: docs cleanup (status-update callouts at top of
001/002/003 + this Addendum 3).

### Design decisions locked during landing

Anything not in Addendum 2 but that we hardened during the landing
pass:

- **Wire contract for `ExportMode`.** Absent (`""`) on the wire is
  treated semantically as `L4-TrafficManager`. The member reconciler
  explicitly writes `""` for the default case so preexisting
  `InternalServiceExport` shapes round-trip unchanged (kept 12
  integration specs green without churning their expectations).
  `L7-FrontDoor` is written verbatim.
- **PLS resolution.** Requires an explicit
  `networking.fleet.azure.com/azure-pls-name` annotation on the
  source `Service`; no default derivation from
  `cloud-provider-azure` internals. Rejected the "guess from
  ILB name" path because it silently races service reconciliation.
- **`FrontDoorBackend` UID naming.** OriginGroup is named
  `fleet-<backendUID>`; Origins are `fleet-<backendUID>-<clusterID>`.
  Deterministic per Kubernetes object, stable across renames,
  matches the pattern `frontdoorprofile` already established with
  `AzureProfileName`.
- **Weight math.** `ceil(backend.Weight * export.Weight /
  sum(export.Weight))`, byte-for-byte identical to the TMB formula
  (see `trafficmanagerbackend/controller.go` line 580) so operators
  moving between ATM and AFD get identical traffic splits.
- **Coexistence guard scope.** Only rejects when a live
  `TrafficManagerBackend` (DeletionTimestamp zero) in the same
  namespace references the same `ServiceImport.Name`. TMB tearing
  itself down is not a conflict, so the migration path
  (delete TMB -> reconcile AFDB) works without any orchestration.
- **Origin `HostName` placeholder.** AFD requires a non-nil
  HostName even when SharedPrivateLinkResource is present; we
  pass the PLS ID string. Prevents leaking a public hostname while
  keeping the API payload valid.
- **SetupWithManager watches.** FrontDoorProfile, TrafficManagerBackend
  and InternalServiceExport all enqueue same-namespace
  FrontDoorBackends (list-based). Chose list-based enqueue over a
  field indexer because namespaces are expected to have single-digit
  backend counts.

### What is *not* in this series (intentional)

- FrontDoorRoute + FrontDoorCustomDomain BYOC reconciler (Phase 4
  tail; requires Key Vault plumbing).
- Removing the POC bridge in `cmd/hub-net-controller-manager`
  (GA prerequisite; sibling binary is running side-by-side today).
- e2e coverage on a live sub (Phase 4 e2e in 002 §11).
- OriginGroup health-probe / session-affinity knobs (§4.1.2
  Phase 4 tail — CRD does not expose them yet).
- Metrics + prometheus wiring on the FrontDoorBackend reconciler.

### Verification

- `make build` in WSL: green (all commits).
- `make local-unit-test` in WSL: green.
- `pkg/controllers/hub/frontdoorbackend` envtest suite: 5/5 passed.
- `pkg/controllers/hub/frontdoorprofile` envtest suite: unaffected,
  still passes.
- `pkg/controllers/member/serviceexport` integration suite: green
  after the wire-contract fix (`""` on default ExportMode).

