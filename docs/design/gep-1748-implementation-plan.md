# Zero-Public-Exposure Ingress Implementation Plan (GEP-1748)

## Plan status

- **Status:** Original plan approved on 2026-10-20. Phases 1-2 and Tasks 3.1-3.3 / 4.1-4.3 implemented. Image/chart/publication definitions and bounded current-runtime envtest coverage are also implemented. The root-HTTP provider staging/activation candidate is implemented and SDK-tested offline; production injection/status projection, broader feature lowering and live Azure/container validation remain outstanding. The detailed provider sequence was approved on 2026-10-21; a subsequent go-ahead started P1 implementation.
- **Updated:** 2026-10-21.
- **Product contract:** [Zero-Public-Exposure Ingress configuration guide](../howtos/gateway-api-afd-configuration.md).
- **Architecture:** [GEP-1748 Gateway API design](gep-1748-gateway-api.md); the
  implemented subset remains narrower than the full proposed routing/TLS design.
- **Capacity:** [Scale and capacity analysis](gep-1748-scale-and-capacity.md);
  its support envelope remains provisional, not a measured guarantee.
- **Target:** `Azure/fleet-networking`, branch `rchinchani/gep-1748-gateway-api`.

This plan replaces the 2026-08-17 public-origin-first delivery sequence. Existing
foundation code is an input, not evidence that the revised product works.
Tasks are checked only after their implementation and applicable validation complete.

**Current priority: one private HTTP vertical slice.** The candidate
`RouteProvisioner.ReconcileHTTPPreview` reuses disabled staging, waits for manual
Private Link approval, enables the route before the endpoint, and verifies the
complete enabled private graph before returning the Azure hostname. Enabled
convergence is a read-only no-op; confirmed drift requests whole-endpoint
suspension, whereas SDK/cancellation/ambiguous-read errors authorize no mutation.
Each invocation retains one write and one bounded deadline. The user explicitly
approved completing controller wiring offline while deferring live proof. Enabled
startup now constructs/injects the complete lifecycle; ownership and uncached
intent precede calls, positive readiness rechecks current uncached identity/
authorization after calls, and only complete enabled-graph convergence publishes
the observed hostname and Gateway/listener Programmed status. Empty, denied or
affirmatively unprogrammable owned intent suspends the endpoint. Default-disabled
behavior is preserved. This is offline control-plane evidence only: live platform,
installed identity and HTTP/Host/probe/withdrawal/cleanup evidence are still
required before release/readiness claims. Broader routing and standalone observer
matrix expansion are not the next milestone.

## Product requirements and boundaries

```text
Clients -> public AFD Premium edge -> Private Link -> member PLS
        -> internal Azure Load Balancer -> application
```

1. AFD Premium and Private Link origins are mandatory. No Standard SKU, public
   origin, `auto` connectivity, or public fallback is supported by this solution.
2. Zero public exposure describes the member application origins, not the
   client-facing AFD edge. Do not claim complete compliance from topology alone.
3. Preserve `networking.fleet.azure.com/v1alpha1 ServiceImport` without public
   schema changes. Support Fleet ServiceImport backend references without claiming
   upstream MCS ServiceImport Extended conformance.
4. Customers configure member Services and ServiceExports, not generated hub
   ServiceImports. Platform operators or authorized ingress owners configure hub
   Gateway API resources. Customer hub access is not a prerequisite.
5. Connectivity and probe-path annotations on ServiceImport are not configuration
   inputs. Probe settings are controller/platform-owned. A future ConfigMap is
   a design option, not part of this implementation unless separately approved.
6. PLS approval is asynchronous. The first usable milestone supports authorized
   manual approval and automatic observation of convergence, not automatic approval.
7. Retain a separate, opt-in hub Gateway manager. Preserve baseline MCS and
   Traffic Manager behavior, including their existing public-origin capabilities.
8. Optional WAF attaches an existing policy; the controller does not own policy
   definitions. HTTPS/certificates and approval automation are later milestones.
9. The first functional milestone is explicitly an HTTP/private-origin preview,
   not an HTTPS production-readiness or SFI compliance claim.

## Verified starting point

| Area | Existing foundation | Work still needed |
|---|---|---|
| Manager | Enabled-only GatewayClass/Gateway/HTTPRoute reconcilers, dependency indexes, schemes, health/readiness endpoints and Azure configuration checks | Provider clients, deployment packaging and meaningful provider readiness |
| Annotations | Premium-only Gateway validation, ServiceImport AFD annotation rejection, fixed approved HTTP-preview probe/Host policy | Provider enforcement and live probe/Host evidence |
| Model | Rule-scoped matches/backends, listener hostname intersections, precedence metadata, deep copies, private-origin validation and approved HTTP-preview probe data | Proven AFD route/rule lowering for every advertised feature |
| Backend resolution | Private resolver, parent/namespace authorization, ReferenceGrants, indexed lookups and dependency watches | Envtest, provider integration and live evidence |
| Member transport | Optional private-origin transport, opt-in read-only ILB/PLS discovery, hub resolution and weight publication independent of Traffic Manager | Envtest and live Azure verification |
| E2E | Gateway CRDs, builders, live API round-trip scenario | Running Gateway controllers, Azure provisioning, approval transitions and traffic assertions |
| Packaging | Package-based build/run, existing image/push and GHCR/MCR delivery integration, separate local image target, disabled-default Gateway chart with scoped RBAC/existing identity, image/chart CI definitions | Actual container/registry/multiarchitecture verification, live RBAC/identity verification and opt-in E2E deployment |

## Decisions to close before dependent implementation

| Decision | Proposed direction / required evidence | Blocks |
|---|---|---|
| Premium configuration | Default to Premium; allow the existing explicit Premium Gateway annotation as a redundant declaration; reject Standard | Phase 1 |
| Retired backend annotations | Reject retired connectivity/probe settings explicitly in the AFD path, without mutating ServiceImport or affecting MCS; document migration from unreleased foundation examples | Phase 1 |
| Probe policy and origin Host header | Approved HTTP-preview policy: HTTP GET `/` every 30 seconds on the selected backend port; preserve the incoming request Host. No ServiceImport configuration or ConfigMap. Controller wiring is implemented offline; actual probe Host behavior and application responses remain required live evidence | Live validation and release readiness |
| Private-origin transport | Prefer optional PLS identity/location, internal endpoint and discovery state on InternalServiceExport; confirm producer ownership and source Service UID validation | Phases 2-3 |
| AFD lowering | Prove mappings for rule boundaries, route precedence, service-port identity, and logical-backend versus cluster weights with SDK fixtures and focused Azure experiments | Phases 3-5 |
| Initial feature subset | Propose HTTP, hostname, Exact/PathPrefix, one logical backend per rule, and member weights for the first preview. Gate multiple weighted backendRefs, headers/methods and filters on proven semantic mappings; report unsupported intent, never approximate it silently | Phases 3-5 |
| Approval ownership | Keep explicit PLS-owner approval; agree correlation, observable states and automation ownership with AFD/PLS teams | Phase 5; future automation |
| Shared private endpoints and backend ports | AFD can share one private endpoint per profile/resource ID/group ID/region. Microsoft warns against different origin ports on that shared endpoint. Proposed preview policy: reject this combination before Azure mutation; separate origin groups do not remove the limitation | Provider lowering/preflight; approval correlation |

If the AFD mapping cannot preserve a promised Gateway API behavior, stop that
feature's implementation and review the supported-feature contract. No design
table is sufficient evidence of provider capability.

## Phase 1: Align and enforce the private-only contract

**Dependencies:** Plan approval and Phase 1 decisions above.

- [x] **Task 1.1: Write contract tests first.** Cover omitted SKU selecting Premium,
  explicit Premium, rejected Standard, unknown/reserved annotations, and retired
  ServiceImport connectivity/probe settings. Preserve unrelated Fleet annotations.
- [x] **Task 1.2: Refactor typed configuration.** Remove selectable public/auto
  paths from AFD configuration. Separate platform-owned backend settings from
  customer annotation parsing; no ServiceImport writes or ConfigMap API.
- [x] **Task 1.3: Align documents and fixtures.** Update the architecture, remaining
  scale terminology, parser/model tests and E2E fixtures to the revised product.
  Preserve historical breadcrumbs as history, not active requirements.

**Success criteria:** No supported AFD configuration selects Standard or public
origins. No annotation is needed on ServiceImport. Focused tests pass and no
public ServiceImport CRD changes appear.

**Completed slice:** Removed caller-selectable SKU defaults and the ServiceImport
configuration/parser types. `ParseGatewayConfig` now accepts only the annotation
map and defaults to Premium; `ValidateServiceImportAnnotations` rejects AFD settings
without modifying the generated object. Probe/Host defaults remain undecided.
Focused unit tests, vet, golint and E2E compile-only checks passed. No reconciler,
admission enforcement or Azure provisioning is claimed by this slice.

## Phase 2: Discover and transport private member origins

**Dependencies:** Phase 1; reviewed internal transport and discovery identity.

- [x] **Task 2.1: Write transport/discovery tests first.** Cover internal LB plus
  PLS, provisioning/missing/deleted/replaced PLS, public Services, source Service
  recreation, custom resource groups, authorization failures and transient errors.
  Cover absent fields from old members and enabled/disabled feature combinations.
- [x] **Task 2.2: Extend internal transport only.** Add optional PLS resource ID,
  location, internal endpoint and discovery/readiness information. Regenerate
  deepcopy/CRDs and test compatibility. Missing fields mean unknown/unresolved,
  never public or approved; do not put per-AFD connection approval in member
  service readiness.
- [x] **Task 2.3: Add opt-in member discovery.** Reuse the member ServiceExport
  pipeline and Azure client initialization patterns. Discover the PLS associated
  with the actual Service/LB frontend, not merely a matching name. Read only;
  AKS remains responsible for creating the ILB and PLS.
- [x] **Task 2.4: Decouple from Traffic Manager.** Publish needed metadata and
  existing ServiceExport weights with private-origin discovery enabled even when
  Traffic Manager is disabled. Preserve existing MCS export validity and Traffic
  Manager fields/behavior. Clear stale private metadata on replacement or removal.
- [x] **Task 2.5: Observe Azure-only changes.** Use Service watches for Kubernetes
  changes plus bounded, rate-limited Azure rediscovery for transitions that do not
  update the Service. Document minimum member Azure read permissions.

**Success criteria:** Private discovery works with Traffic Manager off. Old
members remain compatible but unresolved for ingress. PLS changes converge without
customer hub access, stale identity reuse, or interference with baseline exports.

**Implemented slice:** `spec.privateLinkOrigin` carries optional member discovery
metadata without changing ServiceImport. The default-off member flag
`--enable-private-link-origin-discovery` and Helm value
`enablePrivateLinkOriginDiscovery` work independently of Traffic Manager. Discovery
matches the observed Service IP to a unique, provisioned internal LB frontend and
its PLS, publishes unavailable/error state without invalidating baseline MCS, and
rediscoveries run every two minutes plus up to 20% jitter with a shared 30-second
Azure-call deadline. The chart README documents read permissions and hub-CRD-first
rollout. Unit/fake-client regressions, vet, golint, manager builds, Helm feature
combinations and E2E compile-only checks passed. Member discovery envtest/live
Azure validation remains outstanding. Later current-runtime Gateway envtest
coverage is recorded under P6.1; it does not prove member Azure discovery or
traffic. Member discovery readiness does not imply AFD connection approval.

## Phase 3: Resolve backends and complete the normalized model

**Dependencies:** Phase 2; probe/Host policy and AFD lowering decisions.

**Current slice:** Pure private resolution and the rule-scoped HTTP-preview model
are implemented, together with reference indexes and backend capacity preflight.
Reference errors remain distinct from unavailable member origins; unresolved active
members cannot produce a partial backend. Parent/ReferenceGrant authorization and
indexed runtime watches are implemented in Phase 4. Full AFD route/rule/domain lowering and
capacity accounting remain pending; this slice cannot program Azure resources.

- [x] **Task 3.1: Write resolver/model tests first.** Cover exact Fleet group/kind,
  ServiceImport ports/clusters, valid internal exports, source identity, duplicate
  and conflicting exports, zero weights, member removal, unavailable discovery,
  deterministic ordering, deep-copy isolation and equal-intent equality.
- [x] **Task 3.2: Implement private-only resolution.** Resolve each ServiceImport
  to valid member ILB/PLS origins; reject unsupported topology or incomplete state
  explicitly. Do not read public-IP metadata as an ingress fallback.
- [x] **Task 3.3: Preserve HTTPRoute semantics in the model.** Represent per-rule
  matches, filters and backendRefs, listener/parent attachment, hostname intersection,
  route precedence, service-port identity and platform probe/Host settings. Preserve
  semantically meaningful ordering; validate private endpoint and PLS identity.
- [ ] **Task 3.4: Add indexes and capacity preflight.** Map export/import changes
  to affected routes and Gateways. Count exact generated origin groups, origins,
  routes and rules before Azure mutations; update service-port accounting after
  the lowering decision. Test proposed capacity thresholds and over-limit cases.

**Success criteria:** Equivalent intent yields stable desired state; providers do
not read Kubernetes objects. Invalid or over-limit desired state cannot generate
a partial or public-origin deployment. No broad list/poll loop for Kubernetes changes.

**Implemented resolver slice:** `gatewayresolver.Resolve` accepts already fetched
Kubernetes snapshots, validates the exact Fleet reference and selected TCP/HTTP
port, and resolves only active, current, unconflicted member exports with verified
ILB/PLS metadata. Source namespace, Service UID and full port compatibility are
checked. Removed/deleting exports are not reused, duplicate exports fail closed,
zero member weights are excluded, and any unresolved active member returns a typed
failure with no partial backend. Reference validity is distinct from member-origin
availability and neither implies AFD approval. The normalized origin contract now
requires source UID, an IPv4 frontend, PLS resource ID and region; selectable
connectivity is removed. The approved fixed HTTP-preview probe policy is carried
by the backend. Unit regressions, vet, golint, all manager builds and E2E
compile-only checks passed. The Phase 4 controllers now consume this resolver;
the complete provider is now wired for the narrower supported execution subset.

**Implemented model/index/backend-capacity slice:** The existing model now keeps
rules in source order, listener-specific hostname intersections and route UID/time
metadata. Canonicalization preserves Exact paths, normalizes PathPrefix trailing
slashes/default matches, deduplicates OR matches and deeply copies nested slices.
The HTTP-preview validator rejects unsupported edge protocols/ports, methods,
filters, multiple logical backends, zero logical-backend weights, missing origins
and policy overrides explicitly. Standard controller-runtime indexes cover
GatewayClass, Gateway parents, Fleet ServiceImport references and source exports,
including removed references. Backend preflight counts distinct
namespace/name/port origin groups and deduplicates repeated references, refusing
inconsistent origins or counts above 100 groups / 160 total origins / 40 per group.
Tests cover the exact accepted/rejected boundaries. Task 3.4 remains open for
actual generated AFD routes/rules/domains; runtime watch wiring is implemented. The backend
helper is not a substitute for full provider preflight or live scale evidence.

## Phase 4: Implement Gateway API controllers and ownership

**Dependencies:** Phases 1 and 3; fake-provider contract can be developed alongside.

- [x] **Task 4.1: Write controller tests first.** Cover recognized/foreign classes,
  configuration prerequisites, listener support/conflicts, allowed route namespaces,
  parent section selection, hostname intersection, ReferenceGrant creation/removal,
  unsupported Service backendRefs and unsupported matches/filters.
- [x] **Task 4.2: Add GatewayClass, Gateway and HTTPRoute reconcilers.** Register
  only when enabled. Publish supported features accurately; enqueue dependents on
  class, Gateway, namespace policy, ReferenceGrant and backend changes.
- [x] **Task 4.3: Implement status ownership.** Use generation-aware conditions,
  conflict retries and idempotent patches. Keep reference validity distinct from
  Azure programming: a valid reference need not become unresolved merely because
  its private endpoint is awaiting approval. Finalize exact reasons against the
  selected Gateway API version.
- [ ] **Task 4.4: Establish resource ownership.** Derive stable names from Gateway
  UID and child identity; use tags where supported and parent-scoped ownership
  for Azure child resources. Persist the finalizer before the first owned Azure
  mutation; never adopt/delete foreign resources or block never-programmed deletion.

**Success criteria:** Valid objects can be accepted without claiming programmed
traffic. Unauthorized references lose their desired attachment; reconcile removals
without retaining revoked access as a generic fail-static fallback.

**Implemented control-plane slice:** The enabled manager registers the three
reconcilers after configuration validation and index setup; failures stop startup.
Foreign classes/Gateways and foreign HTTPRoute parent statuses are not claimed.
Optimistic-lock status patches retry conflicts by fetching and evaluating current
intent again. Conditions use the current resource generation, and unchanged
reconciliation does not write status. Unsupported routing affects `Accepted`
independently of backend reference validity. ReferenceGrant authorization precedes
cross-namespace target reads; removal revokes `ResolvedRefs`. Missing private
discovery does not invalidate an otherwise valid reference.

Indexed watches cover class/Gateway/route, ServiceImport, InternalServiceExport,
ReferenceGrant and route-namespace label changes. Gateways watch backend changes
directly, because private readiness can change without changing route conditions.
Listener attachment counts deduplicate parents and do not imply private readiness.
All listeners must be valid and nonconflicting in this preview; unsupported
Gateway address/infrastructure/backend-TLS intent is rejected rather than ignored.
The backend preflight is called on fully resolved desired models. Gateway and
listener `Programmed` remain false, no address or supported-feature claims are
published, and no AFD connection has been requested.

The standard GatewayClass in-use finalizer preserves class identity while Gateways
reference it. Never-programmed Gateways receive no finalizer. Task 4.4 remains
open for Azure resource naming, ownership and deletion; no Azure writes or
programmed-resource revocation evidence are provided by this slice.

## Phase 5: Program AFD Premium and observe asynchronous approval

**Dependencies:** Phases 2-4; provider mapping proven for each advertised feature.

- [ ] **Task 5.1: Write provider tests and fakes first.** Test create/update/no-op,
  partial success, restart, drift, UID collisions, deletion and async ARM operations.
  Classify 403/404/409/429, honor Retry-After, and bound retries and contexts.
- [ ] **Task 5.2: Add narrow Azure SDK clients.** Use repository identity,
  user-agent, retry and rate-limit patterns. Serialize conflicting parent writes.
  Add dependencies only as needed; run `go mod tidy` after dependency changes.
- [ ] **Task 5.3: Reconcile private-only resources.** Create one Premium profile
  and endpoint per Gateway, then origin groups/origins, supported routes/rules and
  required HTTP custom-domain associations. Every origin must include the required
  Private Link configuration before it can carry traffic. Publish the AFD hostname.
- [ ] **Task 5.4: Implement approval/provisioning observation.** Distinguish absent
  request, Pending, Approved, Rejected, Disconnected, provisioning failure and
  unknown state. Correlate the request with the controller-owned AFD origin.
  An empty connection list or successful approval submission is not readiness.
- [ ] **Task 5.5: Test delayed approval transitions.** Poll Azure-only states with
  bounded backoff and jitter; expose actionable conditions/events and keep
  `Programmed=False` while required provisioning is incomplete. Approval observation
  must proceed even when the desired Kubernetes model has not changed.
- [ ] **Task 5.6: Implement safe updates and cleanup.** Observe desired resources
  before reporting success. Handle PLS replacement, revoked approval and member
  removal without retaining an invalid origin. Retain valid last-known-good state
  during transient/capacity failures, not revoked authorization. Delete only owned
  AFD resources; never delete the member PLS/LB or external policy.

**Success criteria:** Manual PLS approval followed by observed Azure convergence
programs the Gateway without Kubernetes edits or restart. No public fallback,
premature success, hot loop, duplicate resources or foreign-resource deletion.

**External action item:** Work with AFD and PLS teams on approval automation,
including authorization, request correlation, API permissions and failure semantics.
Deliver it separately after agreement; do not grant broad auto-approval or make
the manual-approval preview depend on an uncommitted service-team capability.

## Phase 6: Package and prove the first private-origin vertical slice

**Dependencies:** Phases 1-5. Packaging can start after manager/client contracts stabilize.

- [ ] **Task 6.1: Add tests for installation and disabled mode.** Cover chart
  rendering, required flags/configuration, minimal RBAC, identity and no Azure
  activity when disabled.
- [ ] **Task 6.2: Add image/chart/CI wiring.** Build the separate manager image,
  install GatewayClass, enable member discovery explicitly, and supply platform
  configuration. Do not require customer ServiceImport write permissions.
  Admission webhooks are optional follow-up; reconciler validation is authoritative.
- [ ] **Task 6.3: Add operational visibility.** Gate readiness on required caches
  and controller initialization; expose reconciliation, ARM throttling, capacity,
  approval wait and provisioning metrics plus rate-limited events. Azure outages
  must not trigger liveness restart loops or leak configuration secrets.
- [ ] **Task 6.4: Add envtest integration.** Run controllers with deterministic
  fake Azure clients. Assert status and operations through creation, delayed
  approval, backend/weight changes, grant revocation, restart and deletion.
- [ ] **Task 6.5: Add explicitly opt-in live Azure E2E.** Deploy two member ILBs/PLSs
  and the manager with Traffic Manager off. Exercise pending then authorized manual
  approval, HTTP traffic through AFD, both member responses, origin health changes,
  weights, PLS replacement and cleanup. Keep current API round-trip coverage labeled
  separately. Verify Azure origin configuration contains no public path and that
  member Services have no public frontend; do not infer this from successful traffic.

**Packaging progress:** The separate image/build target and disabled-default
`charts/hub-gateway-controller-manager` are implemented. Real Helm tests cover
conditional cluster RBAC/class creation, existing platform Secret/account wiring,
namespace/identity/probes and invalid configuration; read-only CI runs that suite.
Existing Makefile image/push bundles and GHCR/MCR workflows now include Gateway,
with dry-run bundle/output/tag contracts and MCR historical-release handling.
The member chart already exposes `enablePrivateLinkOriginDiscovery`, which must
be enabled independently by the operator. Actual image publication/container
execution, multiarchitecture proof, live installation/RBAC/identity and private
HTTP evidence remain open; Tasks 6.1/6.2 are not complete merely because rendering
succeeds.

**Success criteria:** A reproducible private-only HTTP preview works end to end
without ServiceImport annotations or customer hub access. Cleanup leaves no owned
AFD resources; tests distinguish planned behavior from observed evidence.

## Phase 7: Add optional WAF and expand proven routing features

**Dependencies:** Phase 6; reviewed provider mappings for additional route semantics.

- [ ] **Task 7.1: Write WAF tests first.** Cover policy accessibility/SKU, domain
  associations, absent/removed annotations and deletion. Explicit invalid WAF
  configuration must not silently become an unprotected programmed configuration.
- [ ] **Task 7.2: Implement existing-policy association.** Reconcile only the AFD
  security-policy association and reflect failures. Never modify external WAF rules
  or delete the policy.
- [ ] **Task 7.3: Add routing capabilities only with evidence.** Test logical-backend
  weights separately from member weights, rule precedence, headers/methods and
  supported redirects/rewrites/header filters. Reject unsupported features explicitly;
  update supportedFeatures and the capacity model with each expansion.
- [ ] **Task 7.4: Validate live WAF/routing behavior.** Verify policy enforcement,
  traffic distribution and cleanup at the public edge without exposing member origins.

**Success criteria:** Every advertised routing/WAF capability is tested; the
private-only boundary and externally owned policy lifecycle remain intact.

## Phase 8: Add HTTPS and certificate lifecycle

**Dependencies:** Phase 6; reviewed domain, DNS and certificate ownership contract.

- [ ] **Task 8.1: Specify and test certificate behavior first.** Define managed
  certificate mode, DNS validation, pending/failure/renewal states and rotation.
  Do not invent an unmanaged Secret-to-AFD bridge or promise Key Vault integration.
- [ ] **Task 8.2: Implement HTTPS and custom-domain lifecycle.** Observe domain
  validation, certificate deployment and route/domain association before success.
  Preserve private origin connectivity regardless of frontend protocol.
- [ ] **Task 8.3: Validate HTTPS E2E.** Test valid certificates/hostnames, DNS delay,
  validation failure, renewal/rotation and deletion without affecting external DNS
  or externally owned certificate assets.

**Success criteria:** HTTPS is reported programmed only after required Azure
state is observed. The HTTP-only preview is not represented as production HTTPS.

## Phase 9: Complete release and capacity gates

**Dependencies:** Phase 6 for preview review; Phases 7-8 for their release claims.

- [ ] **Task 9.1: Validate compatibility and conformance.** Run applicable Gateway
  API tests plus implementation-specific Fleet backend tests. Test old/new
  member/hub combinations, upgrade/rollback, feature disable/re-enable and unchanged
  MCS/Traffic Manager behavior. Document rollback order for internal CRD changes.
- [ ] **Task 9.2: Execute the scale/failure matrix.** Measure the provisional
  100-service/160-origin/160-route envelope and 40 origins/service only where the
  proven service-port mapping permits. Test exact thresholds and over-limit refusal,
  approval delay, ARM throttling, PLS/NAT capacity, noisy tenants and failover.
  Benchmark normalized-model p99 below one second as proposed; establish Azure
  convergence/failover SLOs from measurements and service-team review, not that
  local benchmark.
- [ ] **Task 9.3: Validate recovery and deletion.** Cover outage during finalization,
  configuration-plane handoff, ownership conflicts, denied approvals and PLS
  replacement. Document manual recovery without automatic foreign adoption.
- [ ] **Task 9.4: Publish release evidence.** Record supported features/limits,
  minimal roles, troubleshooting, pending-approval guidance, cleanup and rollback.
  Complete the required identity/network/compliance review before making production
  or SFI claims.

**Success criteria:** Published claims match passing tests and measured support
limits. External approval automation remains an explicit tracked item until delivered.

## Next implementation: AFD control-plane automation

**Status:** Approved on 2026-10-21; subsequent implementation go-ahead received.
The backend-only planner and Gateway safety integration are implemented: legal
UID-derived names, canonical/deduplicated private targets, member weights,
backend capacity and shared-target HTTP-port conflict detection. Backend SDK
request construction now uses `armcdn/v2 v2.2.0` (stable API `2024-02-01`) with
custom PLS group ID omitted, explicit private target and fixed HTTP probe/Host
policy. Declared WAF protection is gated until association is implemented.
Opt-in live controller E2E assertions cover status and reference revocation;
they are not Azure traffic evidence. This is not a complete or apply-ready AFD
graph; full P1 tasks below remain open.
Pure P2 root-request/ownership helpers also exist: Premium/global profile,
initially disabled endpoint, required hub/Gateway identity tags and strict
observed-root ARM scope/tag checks. Bounded SDK profile GET/DELETE and Gateway
finalization are now wired into enabled startup using existing hub Azure auth,
client options and rate limits. Cleanup reads trusted hub identity uncached,
verifies full root ownership and removes only the owned finalizer after observed
absence, independently of class/backend validity and outside status retries.
A metadata-only preparation helper also persists/reobserves the cleanup finalizer,
requiring an available cleanup provider and matching uncached hub/Gateway/class/
route/namespace/grant/backend intent before and after persistence. Explicit
unindexed route/export reads avoid sending cache-only field selectors to the API
server; normal reconciliation keeps its indexed path. Conflict retries contain
only Kubernetes work. An optional typed disabled-graph stager now invokes this
guard before staging, outside status retries. Its SDK implementation composes
root/backend/route stages under one shared 30-second deadline and at most one
submitted write. Status requires matching UID/generation and re-resolved intent;
it remains pending without addresses and requeues with bounded jitter. Provider
failures preserve cleanup metadata and are returned alongside observation errors.
After expected disabled-route convergence, the composed SDK path reobserves owned
disabled roots and checks complete paginated endpoint/route/group/origin
membership. Full ARM identities must match exactly once, including every later
page. All supported pages require explicit resource arrays: missing/null arrays
cannot be repaired by expected identities found on another page. Explicit empty
pages remain valid before or after populated pages. Missing, duplicate,
contradictory or ineligible extra records and SDK failures return
no partial staging result. Listed properties revalidate disabled endpoint/route
convergence, managed routing/group policy and approved/deployed private-origin
configuration through the existing canonical predicates. Later drift or pending,
failed or unknown state invalidates earlier GET convergence without a write.
Approval becoming pending preserves the asynchronous authorized PLS-owner action;
unmanaged AFD load-balancing defaults and equivalent ARM ID casing remain valid.
These later reads still cannot exclude subsequent changes or external writers.
Pending status exposes `inventoryVerified` separately
from traffic readiness; neither provisioning submissions nor pending desired
approval trigger these collection reads. The shared deadline/one-write bound is
unchanged. Profile custom-domain/rule-set/security-policy/secret
collections must additionally be explicitly empty across every page. Missing/null
arrays, unsupported contents and SDK failures block verification without touching
those resources or external WAF/certificate assets. Their read-only SDK clients
are preflight requirements before any staging operation. Future injection needs
profile-scoped collection read/list access, not Key Vault or external asset writes.
The composed path can now remove at most one eligible stale private Fleet-shaped
origin in the current expected group, only after complete desired convergence
and all supported/unsupported inventory checks. Root identity/tags plus full
child parent identity authorize removal, never a generated name alone. A fresh
candidate GET must retain its identity/private configuration, followed by fresh
owned Premium/disabled root observations before DELETE. Busy ARM operations wait.
Known failed/rejected stale bindings remain removable; unknown/public/malformed/
foreign records refuse cleanup. `staleOriginsPending` reports pending confirmation,
and inventory remains unverified until a later complete list proves absence,
including after GET/DELETE 404 or a synchronous submission. Future injection
needs scoped origin DELETE permission; no PLS/private-endpoint lifecycle writes
are introduced. The SDK offers no DELETE If-Match precondition, so sequential
rechecks do not resolve the external-writer gate. Group/route migration, broader
cleanup, feature support and atomic/live proof remain open.
Deletion retains priority. Production constructs/injects the complete HTTP
lifecycle, not this disabled-only stager; explicitly enabled reconciliation can
create owned Azure resources and persist Gateway cleanup finalizers. The user
authorized completing this code offline and deferring live validation. Sequential
reads are not transactional write authorization; platform, disabled-deployment
and concurrent-writer assumptions remain unverified live/release requirements,
not automatic runtime capability checks. The path also suspends an owned endpoint after
uncached validation confirms complete empty effective intent and its existing
cleanup finalizer. It never acquires ownership or creates missing resources for
empty intent. A bounded disable-only PATCH remains pending until a later observed
disabled deployment; missing profile/endpoint observations are distinct from
submissions. Status retries do not replay suspension and cleanup metadata stays.
Transient resolution errors are not withdrawal proof. A separate uncached,
authorization-only path now confirms backend RefNotPermitted on an attached,
eligible HTTPRoute without reading ServiceImports/exports. It rechecks trusted
hub/Gateway identity, class acceptance, existing finalizer, route identity/spec,
attachment and current denial before suspending the whole owned endpoint.
This intentionally interrupts permitted peers; unresolved/unreadable backends
cannot block a separate positive denial. Mixed graphs still return no complete
candidate and never stage a replacement subset. Grant-read errors alone do not
authorize withdrawal, and observed unrelated errors remain explicit for retry.
Fresh/restored permission or changed route/policy prevents stale suspension
diagnostics. Status conflict retries recheck proof without replaying Azure.
Backend resolution errors still allow unprogrammed status/address clearing,
without publishing partial attachment counts; status/provider/read errors are
preserved together. This does not complete selective revocation, class handoff
or unresolved withdrawal without a confirmed denial; live enforcement and
concurrent-writer proofs remain outstanding.
An additional callable SDK root provisioner validates the complete gated graph,
GETs/verifies the owned Premium/global profile before child access and stages
profile/disabled-endpoint creation or disables owned enabled endpoint drift.
It shares one 30-second budget, submits at most one write and requires fresh GET
evidence rather than polling or treating accepted submissions as readiness.
Ownership/SDK failure, cancellation, restart/no-op and exact disabled mutation
bodies are covered by fake-transport tests. It is not constructed or called by
the manager. Guarded startup enablement, concurrent external-writer policy,
platform proof gates, full graph apply and complete P2 lifecycle tests remain
open; normal Gateways still create no Azure resources. A prior GET alone is
not atomic collision protection.
A callable backend provisioner now stages the expected group and private origins
only after fresh observations prove the owned Premium/global profile and
converged disabled endpoint. Group provisioning gates origin writes, while
group deployment is checked separately for backend convergence. Exact SDK
GET/PUT/PATCH tests cover controlled drift, private-target/identity refusal,
one-write operation ordering, in-flight/no-op/restart behavior, SDK errors and
cancellation under a shared 30-second deadline. Pending approval does not starve
later origin creation; connection approval remains manual. These operations
also remain disconnected from the manager. Expected-backend convergence is
not route/whole-graph readiness or stale-origin cleanup.
A callable route provisioner now composes backend staging with disabled HTTP
`/*` route GET/create/disable operations. Route access waits for observed desired
backend convergence, including approval. A backend write stops the invocation;
the composed call shares one 30-second budget and at most one write. Full route
ARM/endpoint identity and exact routing properties are verified, with non-enable
routing drift refused rather than guessed PATCH clearing/migration. Fresh
observations are required after submissions. This completes neither traffic
enablement nor production integration: the strict disabled-graph/deployment
convergence assumptions still require separately authorized live proof, and
route migrations/revocation/stale cleanup remain unimplemented.
P4 also has a pure, ownership-checked private-origin snapshot observer. It
requires each expected origin's full ARM identity, matching desired private
configuration and explicit approval/provisioning/deployment convergence, and
deduplicates pending manual-approval targets. A separate callable SDK observer
now GETs the root and paginates expected origin groups under a shared 30-second
budget, after validating intent and proving profile ownership. Missing resources
remain unready; pagination errors, duplicate identities and foreign children
return no partial observation. Enabled startup now injects the observer using
the same credential/options as cleanup. Accepted, fully resolved nonempty intent
is observed outside status retries and rechecked every 30 seconds plus up to 20%
jitter. Status diagnostics use only matching UID/generation and re-resolved
dependency intent; failures remain explicit and retryable. This reports origin
approval/deployment progress only, never full-graph readiness or addresses.
Full P4 tasks remain open pending complete resource programming/convergence.
After reviewing native AFD case-insensitive path patterns and the unproven
fail-closed unmatched branch of a catch-all/rule mapping, the user approved a
gated initial execution subset: one unrestricted HTTP/80 listener, only root
PathPrefix `/`, one logical ServiceImport port and no filters/WAF/custom domains.
Pure complete graph construction now covers that subset, with one disabled
endpoint/route and exact generated profile/endpoint/group/origin/route counts;
no rules or custom domains are generated. Unsupported execution intent gets
pending diagnostics, not a broader fallback. This milestone does not complete
the full P1 routing scope or lift platform proof and Azure-write gates.
This details the remaining Task 3.4, Task 4.4 and Phase 5 work, followed by the
integration/installation evidence needed from Phase 6. It does not replace the
larger release plan or authorize live Azure resource creation.

**Architecture clarification:** Azure provides the complete request path:

```text
Client -> AFD endpoint -> matching route/rules -> origin group
       -> Private Link-enabled origin -> approved AFD-managed private endpoint
       -> member PLS -> internal LB -> application
```

Fleet implements the configuration and observation loop, not another proxy,
tunnel, packet-forwarding agent or custom datapath. Manually configured Azure
resources can serve traffic independently of this controller.

### Provider phase P1: Prove the resource mapping and reject unsafe intent

- [ ] **Task P1.1: Write lowering and capability tests first.** Cover HTTP/80,
  unconstrained/exact/wildcard hostnames, Exact versus segment-aware PathPrefix,
  case and trailing-slash behavior, overlapping matches, route-age/rule-order
  tie-breaks, unmatched requests and removal of more-specific routes. Include
  invalid/unresolved backend cases so removing a binding cannot unexpectedly
  fall through to a broader route. SDK request fixtures are not dataplane proof.
- [ ] **Task P1.2: Implement a pure, deterministic AFD plan.** Extend the existing
  gatewaymodel flow and place provider-specific lowering under
  `pkg/providers/azure/frontdoor`. Preserve one profile/endpoint per Gateway,
  origin groups by ServiceImport namespace/name/port, and origins by verified
  member/source-Service identity. Define legal UID-derived names and stable rule
  identities. Require every origin's PLS configuration; disable caching and
  automatic HTTPS redirects for this HTTP preview, preserve path/query and the
  incoming request Host, and carry GET `/` / 30-second probe policy. Only link
  the default AFD hostname where the effective Gateway/route hostname policy
  permits it. Concrete/wildcard hosts require supported domain associations and
  explicit operator DNS/validation prerequisites, not automatic DNS ownership.
- [ ] **Task P1.3: Finish capability and generated-resource preflight.** Count
  actual profiles/endpoints, origin groups, origins, routes, rule sets/rules and
  domains after lowering and deduplication; cover exact limits and over-limit
  refusal. Track shared private endpoints by profile plus PLS resource ID,
  target group ID and Private Link region. Reject different origin ports on
  the same shared-endpoint tuple, including across distinct ServiceImports or
  origin groups. Verify the PLS-specific group ID, required origin properties
  and supported Private Link regions against the selected SDK/API. Do not
  invent region fallback or treat a PLS region as automatically supported.

**P1 gate:** Demonstrate the promised routing semantics, HTTP custom-domain
lifecycle, probe Host behavior and member-weight eligibility policy with documented
APIs and separately authorized focused Azure experiments where required. If a
mapping cannot preserve the contract, present the limitation for review rather
than silently approximate it or add a software proxy. Unsupported explicit
WAF/TLS/routing protection cannot produce a newly programmed unprotected or
cleartext alternative. WAF association and HTTPS remain separate later phases.

### Provider phase P2: Establish the provider contract and Azure ownership

- [ ] **Task P2.1: Write provider/ownership tests and fakes first.** Cover no-op,
  ownership collisions, same-name Gateway recreation, missing objects, partial
  creation, generation/intent changes and deletion after restart. Add fixtures
  for shared resources, empty Gateways and provider responses with unknown state.
- [ ] **Task P2.2: Add the smallest typed provider boundary.** Use ordinary Go
  structs/functions and narrow interfaces for the SDK operations actually used.
  The provider consumes normalized/lowered intent and ownership identity, not
  Kubernetes clients or objects. Return observed endpoint hostname, provisioning,
  approval diagnostics and retry requirements. Keep Kubernetes policy/status in
  the current reconcilers; no new CRD, configuration API or controller framework.
- [ ] **Task P2.3: Persist and enforce ownership before mutation.** Use Gateway
  UID-derived identities, expected controller/owner tags on taggable roots, and
  verified parent ownership for children. Settle stable hub identity using
  existing platform/Kubernetes identity sources. Define the profile as the
  controller-owned resource boundary and document child ownership. Persist the
  Gateway finalizer successfully before the first owned Azure write. A resource
  found by name alone is never proof of ownership; collisions fail explicitly.
  Keep finalization independent of current class acceptance/backend resolution.

**P2 gate:** Fakes prove zero writes without persisted ownership/finalizer, no
foreign adoption/deletion, and immediate cleanup completion when no owned Azure
resource exists. Preserve the existing GatewayClass in-use guard.

### Provider phase P3: Create and observe the private HTTP configuration

- [ ] **Task P3.1: Write Azure-client and operation-order tests first.** Cover ARM
  long-running operations, pagination, 403/404/409/429, Retry-After, timeouts,
  cancellation, ambiguous responses, drift and restart during creation.
- [ ] **Task P3.2: Initialize narrow AFD SDK clients using repository patterns.**
  Reuse `azclient.NewAuthProvider`, existing cloud options, user-agent, backoff and
  rate-limit policy from `cmd/hub-net-controller-manager`. Select the stable
  armcdn module/version after verifying AFD, not classic-CDN, APIs. Add only the
  required dependency and run `go mod tidy`. Retain validated cloud configuration
  in the Gateway manager instead of discarding it; inject clients/provider only
  when enabled. Record minimal AFD write and any required PLS connection-read
  permissions; do not request PLS approval/write or member LB mutation privileges.
- [ ] **Task P3.3: Reconcile the dependency-ordered Azure graph.** Ensure the
  Premium profile, endpoint, origin groups and Private Link-configured origins,
  then proven rules/routes and required domain associations. Never create a
  temporary public origin to unblock provisioning. Set selected backend HTTP
  port, approved probe/Host policy, member weights and explicit HTTP forwarding
  settings. Keep new traffic bindings disabled/unpublished until required
  dependencies converge. Serialize conflicting writes under the same parent.
  Bound operations and resume long-running work after restart by observing Azure,
  not by depending on an in-memory poller. Publish the hostname returned by Azure,
  not a hostname fabricated from the requested endpoint name.

**P3 gate:** Repeated identical intent is a no-op, interrupted creation resumes
without duplicate profiles/origins, and every generated origin is private-only.
An accepted ARM request or completed write call is not observed readiness.

### Provider phase P4: Observe manual approval and connect runtime status

- [ ] **Task P4.1: Write approval/convergence tests first.** Cover absent request,
  Pending, Approved-but-still-provisioning, Ready, Rejected, disconnected/removed,
  failure and unknown responses. Test two origins sharing one private endpoint,
  unrelated PLS connections and approval without any Kubernetes update.
- [ ] **Task P4.2: Observe the controller's actual AFD connections.** Use the
  selected AFD APIs and, only where necessary, read-only PLS connection observation
  to correlate profile/origin and shared-endpoint identity. Do not treat a random
  Approved PLS connection, an empty list, member discovery readiness or an approval
  submission as this Gateway's readiness. Deduplicate approval instructions for
  shared endpoints. Poll Azure-only changes with bounded backoff/jitter even when
  desired intent is unchanged. Never auto-approve or directly manage AFD-owned
  private endpoints; authorized PLS-owner approval remains manual.
- [ ] **Task P4.3: Wire the provider into Gateway reconciliation safely.** Separate
  reading/resolution, persisted ownership, external operations and status patching.
  Do not execute Azure mutations inside status conflict-retry callbacks. Recheck
  UID/generation and relevant desired-intent/authorization changes before reporting
  current-generation success; Gateway generation alone does not capture route,
  grant or backend changes. Set Gateway/listener `Programmed=True` only for their
  observed, converged required configuration and approved private connectivity.
  Keep origin health and actual traffic evidence distinct from ARM programming.
  Preserve reference-validity semantics, foreign route-parent status and truthful
  diagnostics/events for pending or failed work.

**P4 gate:** Manual approval alone, followed by observed Azure convergence, advances
status without Kubernetes edits or restart. Unchanged desired intent still triggers
necessary observation, but not redundant writes. Approval automation remains an
external AFD/PLS-team action item, not a preview dependency.

### Provider phase P5: Handle change, revocation and deletion without unsafe fallback

- [ ] **Task P5.1: Write update/withdrawal/deletion tests first.** Cover member
  addition/removal, zero/changed weights, Service UID/PLS replacement, public
  conversion, route/parent deletion, grant and namespace-policy revocation,
  approval withdrawal and lost permissions during cleanup. Include revocation
  while another backend is unresolved or the new model exceeds capacity.
- [ ] **Task P5.2: Diff and apply only authorized private intent.** Share backend
  resources only where identity and port rules permit; do not delete origins still
  used by another authorized route. Process confirmed authorization/membership/
  identity withdrawal separately from the normal fully-resolved-model apply gate.
  Do not retain revoked access as last-known-good state. If surgical withdrawal
  would allow unintended routing fallthrough, disable the affected endpoint until
  safe configuration converges rather than select a fallback backend. Distinguish
  confirmed withdrawal from transient observation failure; retain only still-valid
  authorized configuration during ordinary transient/capacity errors. Report
  enforcement failures explicitly: an Azure outage cannot guarantee immediate
  dataplane revocation.
- [ ] **Task P5.3: Finalize and recover idempotently.** Delete only the verified
  owned profile/resources; retain the Gateway finalizer until Azure deletion is
  observed, with 404 treated as already deleted. Recover after process outages,
  rejected operations and partial cleanup. Let AFD manage its private endpoints
  and their sharing lifecycle. Never delete member PLS/LB, external DNS, external
  WAF policy or certificate assets. Never-programmed deletion must not be blocked.

**P5 gate:** No stale unauthorized/orphaned private binding survives a successful
reconcile; no public or less-specific-route fallback is introduced to mask failure.
Cleanup completes after observed deletion, not merely after a delete submission.

### Provider phase P6: Verify controller integration and the Azure datapath

- [ ] **Task P6.1: Add deterministic controller/provider integration tests.** Use
  existing fake-client/envtest conventions for create, delayed approval, drift,
  changed intent during ARM work, concurrent status writes, authorization
  revocation, restart, deletion and disabled mode. Provision API-server/etcd test
  assets in a supported environment; ordinary unit tests are not informer evidence.
- [ ] **Task P6.2: Complete the minimal install/operations slice.** Add the separate
  image/chart/CI wiring, deployment RBAC/identity and explicit discovery enablement
  from Phase 6. Gate readiness on required initialization/caches, not Azure uptime;
  add actionable, rate-limited events/metrics for provisioning, approval wait,
  ownership errors and throttling. Do not require customer ServiceImport writes.
- [ ] **Task P6.3: Run separately authorized private HTTP E2E.** With an explicitly
  approved Azure scope and cleanup budget, install two member ILBs/PLSs and the
  controller with Traffic Manager off. Verify requested connection identity,
  Pending -> authorized manual approval -> convergence, HTTP matching/Host/probes,
  member responses and weight behavior, replacement/revocation and owned-resource
  cleanup. Inspect Azure/Service configuration to prove no public origin/frontend;
  successful requests alone do not prove that boundary. Record propagation delays
  and keep API-round-trip, control-plane and actual traffic evidence separate.

**Detailed completion checklist**

- P6.1 has bounded current-runtime integration coverage: a Ginkgo `TestAPIs`
  suite runs actual GatewayClass/Gateway/HTTPRoute controllers, real API
  validation, caches/indexes and native Event persistence with thread-safe fake
  providers. It verifies approval progression without Kubernetes edits, export
  dependency-watch invalidation/recovery, truthful unprogrammed/address-free
  status and pending-to-absent deletion with actual finalizer retention/removal
  and server-assigned ownership identities. The lifecycle also covers explicit
  cross-namespace grants: deletion retains attachment acceptance but produces
  current RefNotPermitted reference status and stops observation after denied
  state settles; denial precedes missing-target lookup. Recreated permission
  exposes BackendNotFound until target recreation restores resolution, without
  Gateway/HTTPRoute spec or generation changes. This grant-status evidence is
  not actual Azure revocation/enforcement. Export-watch coverage also replaces
  a source Service's transported UID and the PLS/frontend identity: stale
  unconflicted status cannot authorize the changed export generation, and
  current status restores observation of the exact replacement model while
  route references remain valid. Gateway/HTTPRoute intent remains unchanged.
  This does not prove member discovery/transport, independent live Service/PLS
  validation or actual Azure origin replacement/withdrawal. Provider-only
  Disconnected/Drifted transitions after convergence also replace stale
  diagnostics through normal polling and persist matching warning Events;
  restored Ready state requires a new persisted convergence Event occurrence.
  Gateway/route intent and backend objects stay unchanged, references remain
  valid and observed status remains unprogrammed/address-free. These fake states
  do not prove SDK classification, actual approval withdrawal, drift repair or
  traffic enforcement. In-flight fake-provider coverage now holds a captured
  rejection across a valid private-origin edit, waits for exact current export
  spec/status in the real controller cache, then releases the old result. It
  requires exact replacement-origin recovery, a fresh convergence Event
  occurrence and no stale rejection warning, with unchanged Gateway/route
  intent and unprogrammed/address-free status. Hold release/cancellation,
  captured-state stability and one-shot behavior have table-driven coverage.
  This is not transactional freshness, competing-writer exclusion, actual ARM
  cancellation or Azure mutation safety. Focused and full Gateway/manager runs
  passed under WSL with normal teardown and production polling intervals.
  Native Windows starts the assets but this controller-runtime version cannot
  complete signal-based teardown; Linux/WSL is required for passing full runs.
  Existing CI `make test` includes the suite. Restart, weight/member changes,
  actual ARM-work integration and production-provider drift/revocation remain,
  so Task 6.4/P6.1 are not complete. This is not deployed RBAC/identity, `/readyz`
  HTTP integration, actual Azure approval/provisioning, rate-limit or private-HTTP proof.

- P6.2 local process readiness is implemented: enabled startup initializes all
  seven required informers without waiting, and `/readyz` checks synchronization,
  stopped state and shutdown locally. Standby readiness does not require leader
  election or Azure/approval availability; liveness and disabled mode retain their
  independent behavior. This does not verify ongoing cache freshness or satisfy
  provider enablement gates. Image/build/smoke-CI wiring is also defined: the
  dedicated local target builds the complete package, and the ordinary image/push
  bundles now include it through the existing GHCR/MCR paths. MCR retains its
  architecture tag/manifest convention and skips absent historical components.
  GNU make dry-run CI checks delivery contracts before the real nonroot image
  build and network-isolated entrypoint smoke. The independent disabled-default
  chart now supplies enabled-only Gateway/Fleet cluster RBAC, namespace-scoped leader
  election, optional GatewayClass and existing platform Secret/identity wiring,
  verified by native Helm render/lint tests. Its read-only CI job does not contact
  Azure or a cluster. Current origin observation also has standard-recorder
  transition events and fixed-outcome logical call count/duration metrics:
  samples occur outside status retries, and events follow successful current
  status persistence. Enabled chart Event create/patch permissions cover Gateway
  namespaces without changing disabled isolation. Owned-profile finalization
  also records fixed-outcome logical call counts/durations and sanitized cleanup
  failure/ownership-conflict events. Completion events follow observed absence
  and successfully persisted finalizer removal, not merely Azure-call success;
  metadata retries do not replay/count provider calls. Injected graph staging
  and both empty-intent/denied-reference suspension paths now also expose
  fixed-operation/outcome call counts/durations and sanitized failed-call
  warnings outside status retries. A nil-error return is not proof of readiness
  or enforced withdrawal. Production now injects complete HTTP reconciliation,
  with separate fixed labels for it and invalid-intent suspension; disabled-only
  staging is not injected alongside it.
  Actual container/registry
  verification, deployed RBAC/identity/informer/event delivery, broader
  approval-wait telemetry and operational proof remain, so P6.2
  and P6 as a whole are not complete.

- [ ] P1.1-P1.3: Proven supported lowering and complete, safe resource preflight.
- [ ] P2.1-P2.3: Typed provider contract and persisted UID-based ownership.
- [ ] P3.1-P3.3: Idempotent private-only Azure configuration and observation.
- [ ] P4.1-P4.3: Correlated manual-approval convergence and truthful runtime status.
- [ ] P5.1-P5.3: Safe updates, revocation, shared-resource lifecycle and deletion.
- [ ] P6.1-P6.3: Integration, install/operations and authorized private HTTP evidence.

**Overall success criteria:** Gateway/HTTPRoute intent automatically configures and
maintains the Azure-provided datapath. After explicit PLS-owner approval and Azure
convergence, declared HTTP traffic reaches private member applications without
ServiceImport annotations or a Fleet forwarding component. Unsupported semantics,
foreign ownership, unapproved connections and failed enforcement are reported
honestly. WAF, HTTPS, approval automation and production/scale certification remain
separate gates; approval of this implementation plan is not approval for live
Azure operations.

### Provider planning references

- [AFD Private Link](https://learn.microsoft.com/en-us/azure/frontdoor/private-link):
  AFD owns endpoint creation, approval and post-approval convergence are required,
  endpoint sharing is profile/resource/group/region-scoped, and different ports
  on a shared endpoint have a documented platform limitation.
- [AFD to internal load balancer](https://learn.microsoft.com/en-us/azure/frontdoor/standard-premium/how-to-enable-private-link-internal-load-balancer):
  Custom-origin settings, PLS selection, internal-IP hostnames and owner approval;
  verify mandatory origin properties for the HTTP preview.
- `cmd/hub-net-controller-manager/main.go`: existing Azure identity/client options,
  SDK initialization and rate-limit policy.
- `pkg/controllers/hub/trafficmanagerprofile/controller.go`: existing UID-derived
  resource naming, recorder/status, Azure error and lifecycle patterns.
- `pkg/controllers/hub/gateway/controller.go` and
  `cmd/hub-gateway-controller-manager/main.go`: current model/preflight and runtime
  injection points; external writes must be separated from status retries.
- `.github/.copilot/domain_knowledge` and `.github/.copilot/specifications` do not
  exist in this checkout; no applicable versioned specification was available.

## Proposed pull request sequence

| PR slice | Scope | Gate |
|---|---|---|
| 1 | Private-only parser/configuration tests and document alignment | Premium only; no ServiceImport configuration dependency |
| 2 | Internal PLS transport, member discovery and independent feature wiring | Works with Traffic Manager off; mixed-version and regression tests pass |
| 3 | Private resolver, complete model and exact capacity accounting | Deterministic, semantically faithful desired state |
| 4 | Gateway API controllers, statuses, watches and ownership | Fake-provider reconciliation and authorization tests pass |
| 5 | AFD Premium provider and asynchronous approval observation | Idempotent private programming with no premature success |
| 6 | Image/chart/metrics, integration and private HTTP E2E | First usable manual-approval preview |
| 7 | Optional WAF and separately proven routing expansions | Each claimed feature has semantic/data-plane evidence |
| 8 | HTTPS, domains and certificate lifecycle | TLS lifecycle and private-origin E2E pass |
| 9 | Scale, upgrade, recovery and release evidence | Approved measured support and operational contract |

Provider fakes should land with the first consumer; each slice must remain
buildable, feature-gated and independently testable. Do not expose incomplete
public-origin behavior to make an intermediate slice appear functional.

## Validation approach

For each implementation slice, write table-driven unit tests first and reuse
existing envtest/Ginkgo suites for integration. Run tests for modified packages
and directly affected callers before broader validation.

- Initial contract slice: `go test -count=1 ./pkg/annotations ./pkg/controllers/hub/gatewaymodel ./cmd/hub-gateway-controller-manager`.
- Run `go vet` and `golint` on modified packages; use repository `goimports`
  formatting with the `go.goms.io/fleet-networking` local import grouping.
- Run `make reviewable` for repository formatting, vet, lint/static checks and
  dependency consistency before delivery; review its changes rather than accepting
  unrelated generated edits.
- For internal transport changes, run `make manifests generate` and inspect
  generated diffs; the public ServiceImport CRD must remain unchanged.
- Run applicable envtest-backed packages with configured `KUBEBUILDER_ASSETS`.
  Use `make local-unit-test` and `make integration-test` for broader regression
  gates, including race coverage in a supported build environment.
- Compile E2E-tagged packages before provisioning. Run `make e2e-tests` only in
  an explicitly authorized test environment and clean that environment with
  `make e2e-cleanup`; live Azure operations require separate authorization.
- No tests or live provisioning are part of this planning-only change.

## Detailed checklist and overall success criteria

- [x] Phase 1: Private-only contract, parser tests and document alignment complete.
- [x] Phase 2: Member PLS discovery and transport implemented; envtest/live Azure validation outstanding as noted above.
- [ ] Phase 3: Private resolver, complete normalized model and capacity preflight complete.
- [ ] Phase 4: Gateway API controllers, authorization, status and ownership complete.
- [ ] Phase 5: AFD Premium programming and asynchronous approval observation complete.
- [ ] Phase 6: Packaged, observable private HTTP preview proven end to end.
- [ ] Phase 7: Optional WAF and each claimed routing expansion proven.
- [ ] Phase 8: HTTPS/certificate lifecycle proven.
- [ ] Phase 9: Compatibility, scale, recovery and release review complete.

The first usable milestone completes Phases 1-6 with Premium Private Link origins,
authorized manual approval, truthful status and no customer ServiceImport edits.
Production claims require the applicable later gates. At every stage, invalid
configuration or unavailable private connectivity must never create a public
member-origin fallback.
