# Front Door Phase 2 — Profile + Custom Domains

## Requirements

- Reconcile Azure Front Door (AFD) profiles from a `FrontDoorProfile` CRD.
- Reconcile AFD custom domains from a **separate** `FrontDoorCustomDomain` CRD.
- Support DNS-based domain validation and surface the DNS TXT token to users.
- Support both **Managed TLS** and **BYOC (Key Vault reference)** TLS modes.
- Auth to Azure via **Workload Identity** (federated token).
- Namespaced CRDs; custom domain must reference a profile in the **same namespace**.
- Emit both `.status` and Kubernetes `Event`s for the one-time DNS validation token.

## Additional comments from user

- User input: "is custom domain mandatory in production?" → concluded not mandatory technically, usually mandatory commercially.
- User input: "ok, then just add support for it immediately" → custom domains move into Phase 2.
- User input: "do custom domains change often?" → agreed lifecycle/ownership (not frequency) drives the API split.
- User input: "ok" → confirmed separate CRD approach.

## Plan

> **Scope note (POC):** This work ships as a proof-of-concept. The full Phase 2 plan below stays as the north star, but the POC deliberately trims scope to prove the reconciliation model, API shape (D2), and Workload Identity auth path (D6) end-to-end. Deferred items are additive and do not invalidate any recorded decision.
>
> **POC includes:**
> - 2.1 API types (both CRDs, minimal validation)
> - 2.2 Azure client factory with Workload Identity
> - 2.3 `FrontDoorProfile` controller — happy path only
> - 2.4 `FrontDoorCustomDomain` controller — **Managed TLS only**, DNS token surfaced in `.status` only
>
> **Deferred past POC (revisit before GA):**
> - BYOC / Key Vault integration (D3 — spec field reserved but branch not implemented)
> - Kubernetes Event emission for DNS validation (D5)
> - Full finalizer edge-case handling
> - envtest integration tests + E2E
> - New `charts/hub-afd-controller-manager/` sibling chart (D9)
> - CEL / webhook validation beyond basics

### Phase 2.1 — API types (`api/v1alpha1`)
- [ ] `FrontDoorProfile` type.
  - Spec: `ResourceGroup`, `SkuName` (Standard_AzureFrontDoor | Premium_AzureFrontDoor), `Tags`.
  - Status: `Conditions`, `ProfileID`, `EndpointHostname` (default `*.azurefd.net`), `ProvisioningState`, `ObservedGeneration`.
- [ ] `FrontDoorCustomDomain` type (namespaced).
  - Spec: `Hostname`, `ProfileRef{Name}` (same-namespace only), `TLS{Mode: Managed|BYOC, KeyVaultCertificate{VaultURI, CertificateName, Version?}}`.
  - Status: `Conditions`, `DomainID`, `ValidationState` (Pending|Approved|Rejected|TimedOut|InternalError), `DNSValidationToken`, `DNSValidationExpiry`, `TLSState`, `DeploymentStatus`, `ObservedGeneration`.
- [ ] Generate CRD manifests into `config/crd/bases`.
- [ ] Unit tests for validation (webhook or CEL): TLS mode ↔ KV ref consistency, hostname format, immutability of `Hostname` / `ProfileRef`.

### Phase 2.2 — Common Azure client plumbing (`pkg/common/azureclient`)
- [ ] AFD SDK client factory using `azidentity.NewWorkloadIdentityCredential`.
- [ ] Key Vault certificate client factory (same credential).
- [ ] Shared retry/backoff + long-poll requeue helper (validation may take minutes).

### Phase 2.3 — FrontDoorProfile controller (`pkg/controllers/hub/frontdoorprofile`)
- [ ] Reconciler: ensure AFD profile + default endpoint exist; write `ProfileID` + `EndpointHostname` to status.
- [ ] Finalizer: block deletion until profile is removed from ARM (or force-orphan annotation).
- [ ] Unit tests (table-driven) + envtest integration.

### Phase 2.4 — FrontDoorCustomDomain controller (`pkg/controllers/hub/frontdoorcustomdomain`)
- [ ] Resolve `ProfileRef` (same-namespace); requeue if profile not Ready.
- [ ] Ensure `AFDCustomDomain` ARM resource under referenced profile.
- [ ] Poll validation state; write `DNSValidationToken` to status **and** emit an Event (`DNSValidationRequired` with the TXT record and expected value).
- [ ] Managed TLS: request managed cert, bind on approval.
- [ ] BYOC: resolve `KeyVaultCertificate`, verify MI can read it, create AFD secret + bind.
- [ ] Bind approved domains to the endpoint/default route.
- [ ] Finalizer: unbind + delete ARM resource before removing finalizer.
- [ ] Unit + envtest integration tests.

### Phase 2.5 — E2E (`test/e2e`)
- [ ] Profile lifecycle test (create/delete round-trip).
- [ ] Custom domain lifecycle test — gated behind env vars for a real DNS zone + Key Vault; skip when unset.

### Phase 2.6 — Helm + RBAC (**new** chart `charts/hub-afd-controller-manager/`)
- [ ] New sibling chart — do **not** modify `charts/hub-net-controller-manager/`. Compatibility contract in D9 must hold.
- [ ] Ship new CRDs from within the AFD chart (no coupling to the shared `net-crd-installer`).
- [ ] RBAC for the AFD controller ServiceAccount.
- [ ] ServiceAccount annotations for Workload Identity (`azure.workload.identity/client-id`).
- [ ] Values for AFD subscription / resource group / tenant / client-id defaults; no shared `azureCloudConfig` block with the ATM chart.
- [ ] `helm template` diff on the existing ATM chart is empty after this change (regression check).

## Decisions

Each decision below records the **choice**, the **options considered**, the **justification**, the **trade-offs accepted**, and any **reversibility notes**. This is the authoritative record of intent for Phase 2.

### D1. Phase custom domains into Phase 2 (do not defer to Phase 5)

- **Choice:** Ship custom domain support as part of Phase 2 alongside profile/endpoint reconciliation.
- **Options considered:**
  - (A) Phase 2 = profile+endpoint only; custom domains in Phase 5.
  - (B) Phase 2 includes custom domains end-to-end.
- **Justification:**
  - Azure AFD is used almost exclusively for public/enterprise-facing traffic; the default `*.azurefd.net` hostname is technically functional but commercially unusable for branded, SEO-sensitive, or compliance-bound workloads.
  - Early adopters of this controller are expected to be application teams onboarding real production workloads, not internal-only services; a Phase 2 without custom domains would ship a controller they cannot actually use in production.
  - Deferral would force a status-schema expansion later (adding `CustomDomains` after v1 GA), which — while additive and backward-compatible — creates doc churn, release-note noise, and a period where the controller looks "half-done" to consumers.
- **Trade-offs accepted:**
  - Larger Phase 2 scope, longer time-to-first-release.
  - Higher risk of API churn on `FrontDoorCustomDomain` status fields (validation state, TLS state) since we're committing them earlier.
  - E2E environment must include a real DNS zone + Key Vault, raising CI complexity.
- **Reversibility:** Low — once the CRD ships in a tagged release, its shape becomes a public contract. Justifies extra care on API design in D2/D3.

### D2. Model custom domains as a separate `FrontDoorCustomDomain` CRD (not an inline field on `FrontDoorProfile`)

- **Choice:** Introduce a distinct namespaced CRD `FrontDoorCustomDomain` with a `ProfileRef` to the owning profile.
- **Options considered:**
  - (A) Inline slice: `FrontDoorProfileSpec.CustomDomains []CustomDomainSpec`.
  - (B) Separate CRD referencing the profile.
- **Justification:**
  - **Ownership boundary:** In real deployments the platform team owns the AFD profile (infra) while application teams own domains (app). Inline fields force both teams to edit the same object, breaking RBAC and GitOps ownership.
  - **Lifecycle independence:** Domains can be added, validated, rotated, and removed on a completely different cadence than the profile. Multi-tenant SaaS scenarios can add/remove domains daily; the profile is essentially immutable.
  - **Per-resource status:** Each domain has its own async validation + TLS state machine. Modeling N domains as inline entries forces a status slice with per-index conditions, which is awkward to consume and hard to alert on.
  - **Precedent:** Kubernetes Gateway API split `Gateway` (infra) from `HTTPRoute` (app) for the same reason. Azure ARM itself models `AFDCustomDomain` as a distinct child resource — the API mirrors reality.
  - **Frequency-of-change is a red herring:** The user asked whether domains change often; the answer is "rarely, except in multi-tenant SaaS," but frequency was not the deciding factor. Ownership, lifecycle, and status modeling were.
- **Trade-offs accepted:**
  - Two CRDs to install, document, and RBAC.
  - Cross-resource reconciliation (custom-domain controller must watch profiles and requeue).
  - Slightly more boilerplate than inline.
- **Reversibility:** Low — same GA-contract argument as D1.

### D3. Support both Managed TLS and BYOC (Key Vault) from day one

- **Choice:** `FrontDoorCustomDomain.Spec.TLS.Mode` supports `Managed` and `BYOC`; BYOC references a Key Vault certificate via `KeyVaultCertificate{VaultURI, CertificateName, Version?}`.
- **Options considered:**
  - (A) Managed only in Phase 2, BYOC later.
  - (B) Managed + BYOC in Phase 2.
- **Justification:**
  - Enterprise adopters routinely require BYOC for compliance (HSM-backed certs, corporate PKI, cert pinning, HSTS preload lists tied to specific keys).
  - Adding BYOC later would either require a spec migration or a parallel field, both of which break users mid-flight.
  - Managed TLS alone excludes the exact customer segment (regulated / large enterprise) most likely to demand custom domains.
- **Trade-offs accepted:**
  - Controller must resolve Key Vault references, verify MI permissions, and manage AFD `secrets` resources — non-trivial extra code and RBAC surface (Key Vault `get`/`list` on secrets/certificates for the workload identity).
  - Cert rotation semantics must be defined now (watch KV cert version, re-bind on change) rather than deferred.
- **Reversibility:** Medium — spec is additive; the BYOC branch could in principle be marked deprecated later, but that would strand existing users.

### D4. CRD scope: namespaced, `ProfileRef` restricted to same namespace

- **Choice:** Both `FrontDoorProfile` and `FrontDoorCustomDomain` are namespaced. A `FrontDoorCustomDomain` may only reference a `FrontDoorProfile` in its own namespace.
- **Options considered:**
  - (A) Cluster-scoped CRDs, any-to-any references.
  - (B) Namespaced, same-namespace-only references.
  - (C) Namespaced with cross-namespace refs gated by a `ReferenceGrant`-style opt-in.
- **Justification:**
  - Namespaced resources inherit standard Kubernetes RBAC and multi-tenancy patterns; cluster-scoped CRDs would concentrate authority and complicate delegated administration.
  - Same-namespace-only reference is the **least-privilege default** and prevents a tenant in namespace `foo` from hijacking a profile in namespace `bar`.
  - Cross-namespace referencing is a common future ask, but Gateway API has shown that adding a `ReferenceGrant`-style opt-in later is fully backward-compatible.
- **Trade-offs accepted:**
  - Users who legitimately want one central-infra profile serving domains in many namespaces must wait for the opt-in mechanism.
- **Reversibility:** High — adding cross-namespace support later is additive; today's users are unaffected.

### D5. Surface DNS validation token via both `.status` and Kubernetes Events

- **Choice:** Write the DNS TXT challenge (`DNSValidationToken` + expected record name) into `FrontDoorCustomDomain.status` **and** emit a `DNSValidationRequired` Event.
- **Options considered:**
  - (A) `.status` only.
  - (B) `.status` + Event.
- **Justification:**
  - Status is the correct machine-readable surface (GitOps, operators, automation).
  - DNS validation is a one-time human step; humans debug with `kubectl describe` and `kubectl get events`, where fields buried in status are easy to miss.
  - Event carries the exact TXT record and value inline, cutting the "how do I complete validation?" support loop.
- **Trade-offs accepted:**
  - Extra event volume (bounded — emitted only on state transitions, not every reconcile).
  - Slightly more test surface (must assert event emission).
- **Reversibility:** High — event emission can be tuned/disabled behind a flag if noisy.

### D6. Azure authentication: Workload Identity (federated token)

- **Choice:** Controller authenticates to Azure Resource Manager and Key Vault using Azure AD Workload Identity via `azidentity.NewWorkloadIdentityCredential` (reads `AZURE_CLIENT_ID`, `AZURE_TENANT_ID`, `AZURE_FEDERATED_TOKEN_FILE`).
- **Options considered:**
  - (A) Workload Identity (federated token).
  - (B) Managed Identity via IMDS.
  - (C) Service principal with client secret stored in a Kubernetes `Secret`.
- **Justification:**
  - Workload Identity is the current Microsoft-recommended default on AKS; IMDS-based MI is being phased out in favor of federated tokens.
  - Removes any long-lived secret from the cluster (rules out C on principle).
  - Cleanly scoped per-ServiceAccount via `azure.workload.identity/client-id` annotation, matching per-controller least-privilege.
- **Trade-offs accepted:**
  - Requires OIDC issuer + federated credentials to be configured on the AKS cluster and Azure AD app; operators without WI must configure it before installing the chart.
  - Slightly more Helm surface (SA annotations + values for client-id/tenant-id).
- **Reversibility:** High — the credential is constructed in one place (`pkg/common/azureclient`); switching to a `DefaultAzureCredential` chain to also support MI would be a small localized change.

### D7. No umbrella `GlobalLoadBalancer` CRD; keep AFD and ATM strictly separate

- **Choice:** Do not introduce a `GlobalLoadBalancer` umbrella CRD or a `pkg/common/globalload/` shared-abstraction package. AFD and (future) ATM are modeled as independent, product-named CRDs with product-scoped helper packages (`pkg/common/azurefrontdoor/`, later `pkg/common/azuretrafficmanager/`). Genuinely cross-cutting utilities (Azure auth, ARM polling, conditions, finalizers) live in existing common packages (`pkg/common/azureclient/`, `pkg/common/conditions/`), not in a speculative umbrella package.
- **Options considered:**
  - (A) Independent controllers, no umbrella — product-named CRDs and helpers.
  - (B) Umbrella `GlobalLoadBalancer` CRD with a provider interface and shared `pkg/common/globalload/` abstractions from day one.
- **Justification:**
  - **Overlap is thin.** AFD is L7 (HTTP reverse proxy, WAF, cache, TLS termination, custom domains). ATM is L4/DNS (returns IPs by routing method). Custom domains, TLS, WAF, caching, and rules are AFD-only. Endpoint model, health-probe semantics, and async ARM-op tuning differ. The genuinely reusable pieces (auth, polling, conditions, finalizers) already have natural homes in existing common packages.
  - **Umbrella specs leak.** A shared spec inevitably degenerates to a discriminated union (`Spec.AzureFrontDoor{...} | Spec.AzureTrafficManager{...}`), which relocates two shapes into one CRD without simplifying anything and complicates validation and defaulting.
  - **Different consumers.** Users pick AFD for L7/CDN/WAF and ATM for DNS-based failover. The choice is driven by requirements, not preference — a "pick-my-provider" UX solves a problem few users actually have.
  - **Provider interfaces designed before their first implementation are wrong.** Committing to an abstraction now, with only AFD in hand, would bake in AFD-shaped assumptions that ATM would then have to work around.
  - **Precedent.** Gateway API works as an umbrella because L7 gateways genuinely share a spec. Cross-layer unification attempts (Service type=LoadBalancer + ExternalDNS + Ingress) have consistently stayed as separate resources composed together.
- **Trade-offs accepted:**
  - Users who want to switch between AFD and ATM must migrate between two CRDs (acceptable — such migrations are rare and require operational planning anyway).
  - Some duplication in controller scaffolding across product-scoped packages (mitigated by shared helpers in `pkg/common/azureclient/`).
  - No single "global LB" entry point in the API surface today.
- **Reversibility:** **High.** If demand emerges, a `GlobalLoadBalancer` CRD can be added later as a thin composition layer over the existing product CRDs (Crossplane `Composition`-style) — additive and non-breaking. The reverse (retracting a shipped umbrella CRD) would be far more disruptive, which is another reason to defer.
- **Impact on Phase 2:** None. Proceed with `FrontDoorProfile` + `FrontDoorCustomDomain` as planned; AFD-specific helpers live under `pkg/common/azurefrontdoor/`.

### D8. Do not deviate from upstream `mcs-api` (KEP-1645) unless strictly necessary

- **Choice:** Fleet networking CRDs that mirror upstream `mcs-api` types — currently `ServiceExport` and `MultiClusterService` — **must not diverge** from the upstream shape. Configuration extensions carry over annotations (existing pattern) rather than by adding local `Spec` fields.
- **Applies to open question 1.5 (`ServiceExport` `Spec`):** Reject Option A (add `Spec`). Adopt **Option B** — the new Proposal 002 §3.3 field ships as an annotation under the `networking.fleet.azure.com/` prefix, with defaulting and validation implemented in the controller.
- **Scope of the rule:** Any type whose name and semantics match an upstream `mcs-api` type. Fleet-only types (e.g., `FrontDoorProfile`, `FrontDoorCustomDomain`, `InternalServiceExport`) are unaffected and continue to use idiomatic spec/status.
- **Options considered:**
  - (A) Case-by-case divergence when it's convenient.
  - (B) Hard rule: no divergence from `mcs-api` types.
- **Justification:**
  - **Documented public contract.** The AKS Fleet L4 load balancing docs (learn.microsoft.com/azure/kubernetes-fleet/l4-load-balancing) describe `ServiceExport` and `MultiClusterService` with the upstream shape. Divergence would silently break customers following the official docs, break tooling that assumes mcs-api compliance, and complicate future upstream conformance testing.
  - **Portability across MCS implementations.** Users and third-party tools (Submariner, Cilium ClusterMesh, other mcs-api impls) expect a stable, portable `ServiceExport`. A Fleet-only `Spec` is a lock-in signal.
  - **Precedent in this repo.** The existing `weight` knob is already carried as an annotation (`networking.fleet.azure.com/weight`, documented at `api/v1alpha1/serviceexport_types.go:43-49`) exactly because of this constraint. Proposal 002's new field is analogous and should follow the same pattern.
  - **Future upstream alignment.** If KEP-1645 (or a successor) eventually adds an equivalent field to `ServiceExport.Spec`, we can migrate the annotation to that upstream field with a deprecation window — a strictly better outcome than shipping a Fleet-specific `Spec` we then have to reconcile with upstream.
- **Trade-offs accepted:**
  - Annotation-carried config is untyped: no OpenAPI validation, no `kubectl explain`, no CEL rules. Validation and defaulting must live in the controller and be covered by unit tests.
  - Annotation keys grow linearly with tunables; if the count becomes unwieldy, revisit — but only after upstream direction is clearer.
  - Slightly worse UX than a typed `Spec`.
- **Reversibility:** **High.** The rule is a policy, not a schema. If upstream evolves or maintainers change position, we can add spec fields later without a breaking migration (annotation-first users just gain a typed path).
- **Impact on other decisions:** Supersedes the earlier informal recommendation on 1.5. Open question **OQ (1.5)** is now closed as "annotation-based; no `ServiceExport.Spec`."

### D9. Package AFD as a separate sibling chart, do not extend the existing `hub-net-controller-manager` chart

**Baseline captured:** `.github/.copilot/breadcrumbs/baselines/` contains `hub-net-atm-default.yaml` and `hub-net-atm-enabled.yaml` — rendered outputs of the current ATM chart in both flag states (Helm v4.2.3). SHA-256 hashes and the exact `helm template` commands to reproduce are in `baselines/README.md`. Any AFD PR must produce empty `diff` output against these files before merge, mechanically proving the D9 compatibility contract.

- **Choice:** Ship a new top-level chart `charts/hub-afd-controller-manager/` with its own Deployment, ServiceAccount, RBAC, values, and images. The existing `charts/hub-net-controller-manager/` chart is **not modified** as part of Phase 2. ATM continues to be delivered by that chart unchanged; AFD is a second `helm install`.
- **Options considered:**
  - (A) Extend existing chart with a second Deployment + SA behind an `enableFrontDoorFeature` flag.
  - (B) New sibling chart `charts/hub-afd-controller-manager/`.
  - (C) Single Deployment with dual identities (rejected outright — violates the identity-split security requirement by construction).
- **Justification:**
  - **Identity split enforceable at the packaging boundary.** A Pod binds to exactly one ServiceAccount, and Workload Identity is per-SA. Two identities require two Pods and two SAs. Putting them in separate charts guarantees the AFD SA cannot be accidentally reused for ATM operations.
  - **Zero-risk upgrade for existing ATM installs.** With the ATM chart untouched, `helm upgrade` on existing releases produces an empty diff (verifiable via `helm template`). No values renames, no default flips, no new required keys.
  - **Consistent with D7.** AFD and ATM are independent products with independent lifecycles; the chart layer should reflect that. Cramming both into one chart is the packaging analogue of the umbrella CRD we rejected.
  - **Independent release cadence.** AFD image versions, CRDs, RBAC, and values evolve on their own timeline without forcing ATM users to re-review each release.
  - **Ecosystem norm.** Kubernetes controllers of this shape (Azure Service Operator, ExternalDNS providers, cert-manager sub-components) ship one chart per controller.
- **Trade-offs accepted:**
  - Operators wanting both must run two `helm install`s.
  - A new chart to publish, document, and version.
  - Some templating duplication (leader-election flags, image blocks, resource blocks) — acceptable and can be factored into a shared library chart later if it grows.
- **Compatibility contract for existing ATM installations** (must hold for the entirety of Phase 2):
  1. `helm upgrade` of an existing `hub-net-controller-manager` release with unchanged values produces a byte-identical rendered manifest.
  2. No keys under `azureCloudConfig` are renamed, removed, or repurposed.
  3. `enableTrafficManagerFeature` default is not flipped.
  4. The `hub-net-controller-manager-sa` ServiceAccount is not renamed and gains no new bindings.
  5. AFD CRDs (`FrontDoorProfile`, `FrontDoorCustomDomain`) are installed by the AFD chart only — never as a side effect of installing or upgrading the ATM chart.
- **Reversibility:** **Medium.** Merging the two charts later would be a breaking Helm change; splitting them further is easy. Consciously erring on the side of separation now because the reverse migration is much more disruptive.
- **Impact on other decisions:**
  - Updates Phase 2.6 in the plan: swap "update `charts/hub-net-controller-manager`" for "create `charts/hub-afd-controller-manager`".
  - No change to D1–D8.

### Summary table

| ID | Decision | Choice | Reversibility |
|---|---|---|---|
| D1 | Phase custom domains | Phase 2, not deferred | Low |
| D2 | API shape | Separate `FrontDoorCustomDomain` CRD | Low |
| D3 | TLS support | Managed + BYOC (Key Vault) | Medium |
| D4 | CRD scope | Namespaced, same-namespace `ProfileRef` | High (cross-ns can be added) |
| D5 | DNS token surfacing | `.status` + Kubernetes Event | High |
| D6 | Azure auth | Workload Identity (federated token) | High |
| D7 | Umbrella `GlobalLoadBalancer` CRD | No — keep AFD and ATM separate; product-scoped helpers | High (can be added additively later) |
| D8 | mcs-api divergence | Prohibited for `ServiceExport`/`MultiClusterService`; use annotations under `networking.fleet.azure.com/` | High |
| D9 | AFD chart packaging | New sibling chart `charts/hub-afd-controller-manager/`; do not modify existing ATM chart | Medium |

## Implementation Details

_To be filled as implementation proceeds._

## Changes Made

### Phase 2.1 — API types (completed 2026-07-18)
- **New:** `api/v1alpha1/frontdoorprofile_types.go` — `FrontDoorProfile` + `FrontDoorProfileList`, immutable `ResourceGroup`/`Sku`, `Programmed` condition type/reasons.
- **New:** `api/v1alpha1/frontdoorcustomdomain_types.go` — `FrontDoorCustomDomain` + `FrontDoorCustomDomainList`, same-namespace `ProfileRef` (D4), immutable `Hostname`, `TLS{Mode, KeyVaultCertificate}` with CEL cross-field validation, validation-state enum, `Programmed` condition type/reasons.
- **Regenerated:** `api/v1alpha1/zz_generated.deepcopy.go` via `controller-gen v0.20.0`.
- **Regenerated:** `config/crd/bases/networking.fleet.azure.com_frontdoorprofiles.yaml`, `config/crd/bases/networking.fleet.azure.com_frontdoorcustomdomains.yaml`.

### Phase 2.2 — Azure client factory (completed 2026-07-18)
- **New:** `pkg/common/azurefrontdoor/client.go` — `Config` + `LoadConfigFromEnv()`, `NewCredential()` (WorkloadIdentityCredential per D6), `NewClients()` bundling `Profiles`/`AFDEndpoints`/`CustomDomains` sub-clients, `DefaultARMClientOptions()`.
- **New:** `pkg/common/azurefrontdoor/client_test.go` — table-driven validation tests + nil-guard tests.
- **Dependency:** added `github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn v1.1.1`.

### Phase 2.3 — FrontDoorProfile controller, happy path (completed 2026-07-19)
- **New:** `pkg/controllers/hub/frontdoorprofile/controller.go` — reconciler + finalizer + AFD profile & default endpoint CRUD + status.
- **Modified:** `pkg/common/objectmeta/objectmeta.go` — added `FrontDoorProfileFinalizer` and `FrontDoorCustomDomainFinalizer` constants under existing `networking.fleet.azure.com/` prefix.

### Phase 2.4 — FrontDoorCustomDomain controller, Managed TLS only (completed 2026-07-19)
- **New:** `pkg/controllers/hub/frontdoorcustomdomain/controller.go` — reconciler + finalizer + AFD custom domain CRUD + validation-state polling + Programmed condition; BYOC path rejected with permanent Invalid condition per POC scope (D3).

### Wiring — hub-net-controller-manager entrypoint (completed 2026-07-19)
- **Modified:** `cmd/hub-net-controller-manager/main.go` —
  - Added imports for `pkg/common/azurefrontdoor`, `pkg/controllers/hub/frontdoorprofile`, `pkg/controllers/hub/frontdoorcustomdomain`.
  - New `--enable-frontdoor-feature` flag (default `false`) with justifying comment referencing D9 (WI + AFD subscription config not present on existing installs; ATM-only installs unaffected).
  - New `frontDoorFeatureRequiredGVKs` slice for CRD presence check, mirroring `trafficManagerFeatureRequiredGVKs`.
  - New `if *enableFrontDoorFeature` block in `main()`: CRD check → `LoadConfigFromEnv` → `NewCredential` → `NewClients` → `SetupWithManager` for both AFD reconcilers.
  - Zero changes to the ATM branch, `cloudConfigFile`, or `initAzureTrafficManagerClients` — D9 compatibility contract preserved.

### POC validation
- `go build ./...` — exit 0 (verified after every phase).
- `go vet ./...` — exit 0 (verified after Phase 2.3 and Phase 2.4).
- `go test ./pkg/common/azurefrontdoor/...` — ok.
- ATM chart baseline captured at `.github/.copilot/breadcrumbs/baselines/` (D9 zero-diff guard).

## Before/After Comparison

_To be filled as implementation proceeds._

## References

- Azure Front Door ARM model: `Microsoft.Cdn/profiles`, `.../afdEndpoints`, `.../customDomains`, `.../secrets`.
- Kubernetes Gateway API split precedent (`Gateway` vs `HTTPRoute`) for lifecycle-based API separation.
- Repo custom instructions: Breadcrumb Protocol, Testing Rules.

## Checklist

- [ ] 2.1 API types + CRD manifests + validation tests
- [ ] 2.2 Azure client + Key Vault client + Workload Identity wiring
- [ ] 2.3 FrontDoorProfile controller (reconcile, finalizer, tests)
- [ ] 2.4 FrontDoorCustomDomain controller (reconcile, validation polling, TLS both modes, finalizer, tests)
- [ ] 2.5 E2E tests (profile always; custom domain gated)
- [ ] 2.6 Helm chart + RBAC + WI ServiceAccount annotations

## Success Criteria

- `kubectl apply` of a `FrontDoorProfile` creates the ARM profile + endpoint and reports `Ready=True` with a populated `EndpointHostname`.
- `kubectl apply` of a `FrontDoorCustomDomain` (Managed TLS) produces a `DNSValidationRequired` Event with the TXT token; once the TXT record exists, status transitions `Pending → Approved`, TLS provisions, and domain binds to the endpoint.
- BYOC path validated against a real Key Vault cert in E2E.
- Deleting either CR cleans up the corresponding ARM resource; finalizers block premature deletion.
- All unit and integration tests pass under `go test ./...`.
