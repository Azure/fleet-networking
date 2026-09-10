# AFD Public and Private Origin Proposal

## Requirements

- Write a detailed proposal for Azure Front Door plus WAF using public
  LoadBalancer Services in Fleet member clusters.
- Write a detailed proposal for Azure Front Door plus WAF plus Azure Private
  Link Service using private LoadBalancer Services in Fleet member clusters.
- Assume Gateway API PR 5158 merges and the initial GEP-4894
  `EndpointSelector` supports namespace-local pod selection without
  `selectorRef`.
- Preserve Fleet `ServiceImport` as the hub-side multi-cluster endpoint
  aggregation contract.
- Define Kubernetes YAML, Azure resource mapping, controller interactions,
  security, status, failure handling, lifecycle, migration, testing, and
  rollout.
- Produce a phased implementation plan with test-first tasks and measurable
  success criteria.
- Explain why AFD, rather than Traffic Manager, is required for the SFI
  Application DDoS control and carry that constraint into the implementation
  plan.

## Additional comments from user

- Create changes on a new PR branch under `rchinchani/*`.
- Current branch: `rchinchani/afd-public-private-origin-proposal`.
- User approved proceeding from the GEP-4894 evaluation to a detailed public
  and private AFD origin proposal.
- User approved this implementation plan before documentation changes.
- User requested that the feature-branch documentation and commit message explicitly
  identify its GEP alignment: preserve GEP-1748 `HTTPRoute -> ServiceImport` routing and
  avoid a production dependency on GEP-4894's evolving `selectorRef`.
- User requested a small AFD-versus-Traffic Manager SFI DDoS rationale in the
  relevant design documents.
- User chose to continue with the recommended attachment-scoped backend model
  after reviewing direct `ServiceImport` policy attachment versus an explicit
  Gateway-to-`ServiceImport` attachment.
- User approved implementing the API and controller foundation on this branch.
- The approved branch scope is API contracts plus a feature-gated, read-only
  controller foundation; Azure resource writes remain in later pull requests.
- The initial API supports controller-managed AFD profiles only.
- Public origin bypass conformance will be reported by a member-cluster
  condition rather than asserted by a hub-side acknowledgement or probe.

## Plan

### Phase 1: Confirm repository and API baselines

- [x] **Task 1.1: Inspect Fleet service aggregation APIs.** Trace
  `ServiceExport`, `InternalServiceExport`, and `ServiceImport` fields and
  controller ownership.
  - Success criteria: The proposal uses existing public fields correctly and
    identifies any required internal-only transport extensions.
- [x] **Task 1.2: Inspect existing Azure global-routing patterns.** Review
  Traffic Manager reconciliation, ownership, status, and Azure identity
  conventions that can be reused.
  - Success criteria: The design reuses repository patterns where applicable
    and explicitly explains deviations for AFD.
- [x] **Task 1.3: Reconcile the two source RFC branches.** Use the evaluated AFD
  RFC and GEP-1748 controller design as inputs while accounting for PR 5158.
  - Success criteria: The proposal has one coherent target API and does not
    depend on the removed `selectorRef`.

### Phase 2: Design the public-origin topology

- [x] **Task 2.1: Define public member resources and readiness.** Specify
  LoadBalancer Service, ServiceExport, endpoint eligibility, bypass
  protection, and health.
  - Success criteria: A member becomes an AFD origin only when every required
    public readiness condition is satisfied.
- [x] **Task 2.2: Define hub and Azure resources.** Specify Gateway,
  HTTPRoute, ServiceImport policy, AFD profile/endpoint, custom domain, WAF
  security policy, origin group, and origins.
  - Success criteria: Every Kubernetes field has a deterministic Azure mapping
    and ownership rule.
- [x] **Task 2.3: Define public reconciliation and failure behavior.**
  - Success criteria: Partial member failures, all-or-nothing validation,
    draining, health failures, and Azure errors have explicit behavior.

### Phase 3: Design the private-origin topology

- [x] **Task 3.1: Define private member resources and PLS discovery.** Specify
  internal LoadBalancer Service, PLS creation, approval, regional metadata,
  and internal transport.
  - Success criteria: Public fallback is impossible when Private Link is
    explicitly required.
- [x] **Task 3.2: Define AFD Premium private origins.** Specify managed private
  endpoints, origin grouping, region constraints, approval state, and WAF.
  - Success criteria: The design covers asynchronous provisioning and approval
    without reporting false readiness.
- [x] **Task 3.3: Define private reconciliation and failure behavior.**
  - Success criteria: Missing PLS, stale resource IDs, rejected connections,
    mixed connectivity, and regional failures have deterministic outcomes.

### Phase 4: Define shared contracts

- [x] **Task 4.1: Define API and validation.** Separate portable Gateway API
  fields from Fleet/Azure-specific policy and define defaults and immutability.
  - Success criteria: The YAML shape is schema-validatable and has no secret
    values in annotations or status.
- [x] **Task 4.2: Define traffic, health, and status semantics.** Preserve
  logical backend weights separately from member-origin priority and weight.
  - Success criteria: `Programmed`, `Ready`, origin health, and regional
    availability cannot be confused.
- [x] **Task 4.3: Define security and ownership.** Cover WAF, origin bypass,
  identity, RBAC, ReferenceGrant, Azure RBAC, finalizers, drift, and deletion.
  - Success criteria: The proposal has explicit tenant and Azure resource
    boundaries for both topologies.

### Phase 5: Write the implementation plan

- [x] **Task 5.1: Plan test infrastructure and API work first.**
  - Success criteria: Unit, integration, envtest, fake Azure, and live Azure
    tests precede or accompany their implementation tasks.
- [x] **Task 5.2: Plan controllers and Azure provider delivery.**
  - Success criteria: Tasks identify packages, dependencies, watches, indexes,
    reconciliation order, retry behavior, and status writers.
- [x] **Task 5.3: Plan staged rollout and production readiness.**
  - Success criteria: Public and private capabilities can ship independently
    behind feature gates with upgrade and rollback tests.

### Phase 6: Complete documentation

- [x] **Task 6.1: Write the detailed design proposal.**
  - Success criteria: A reviewer can evaluate both complete data paths,
    resource mappings, API contracts, security, operations, and alternatives.
- [x] **Task 6.2: Write the detailed implementation plan.**
  - Success criteria: Each task has prerequisites, deliverables, validation,
    and completion criteria.
- [x] **Task 6.3: Complete this breadcrumb.**
  - Success criteria: Decisions, changed files, comparisons, and references
    accurately reflect the final documents.
- [x] **Task 6.4: Document the SFI Application DDoS product boundary.**
  - Success criteria: The proposal explains why DNS-based Traffic Manager is
    not the required Layer-7 enforcement plane, and the implementation plan
    makes the AFD Premium, WAF, Bot Manager, rate-limit, and origin-bypass
    controls explicit.
- [x] **Task 6.5: Adopt the Gateway-to-ServiceImport attachment API.**
  - Success criteria: The proposal and implementation plan replace the direct
    backend policy with an attachment identified by Gateway UID,
    ServiceImport UID, and port; define conflict, namespace, status, and
    ownership behavior; and close implementation-plan Task 0.1.

### Phase 7: Implement the API and controller foundation

- [x] **Task 7.1: Add API and validation tests first.** Cover defaults,
  immutable references, enum/range constraints, managed-profile placement,
  WAF requirements, status bounds, and scheme registration.
  - Success criteria: tests constrain every new API default and validation
    rule before generated CRDs are accepted.
- [x] **Task 7.2: Implement and generate the provider APIs.** Add
  `AzureFrontDoorGatewayPolicy`, `AzureFrontDoorBackendAttachment`, shared
  status types, deep copies, CRDs, and RBAC.
  - Success criteria: generated artifacts contain no manual drift and reject
    invalid combinations at the API server boundary.
- [x] **Task 7.3: Add read-only attachment controller tests first.** Cover
  missing references, port validation, deterministic duplicate precedence,
  generation-aware conditions, dependency-triggered reconciliation, and no
  finalizer or Azure writes.
  - Success criteria: tests define deterministic status behavior for every
    dependency and conflict state.
- [x] **Task 7.4: Implement the feature-gated controller foundation.** Add
  repository-consistent structured logging, events, field indexes, watches,
  status patching, and manager wiring behind a default-off flag.
  - Success criteria: disabled installations start no AFD controller; enabled
    installations reconcile attachment status without changing Azure or
    Gateway resources.
- [x] **Task 7.5: Validate the foundation.** Run generation, formatting,
  focused tests, `go vet`, linting, and manifest drift checks.
  - Success criteria: all checks pass and the breadcrumb records any
    repository baseline failures separately from change-induced failures.

### Phase 8: Final API hardening

- [ ] **Task 8.1: Preserve nested defaults for omitted YAML objects.** Ensure
  omitting `healthProbe` and `traffic` still materializes their documented
  child defaults, and cover the manifest-shaped admission path.
  - Success criteria: an API-server round trip of an object without either
    parent returns all documented defaults.
- [ ] **Task 8.2: Reject non-TCP ServiceImport origins.** Resolve the selected
  service port's protocol and reject UDP or SCTP because AFD HTTP/S origins
  require TCP.
  - Success criteria: controller tests accept TCP and reject UDP/SCTP ports.
- [ ] **Task 8.3: Regenerate and revalidate.** Regenerate deep copies and CRDs,
  then rerun focused tests, compile checks, vet, lint, and manifest rendering.
  - Success criteria: generated artifacts are stable and all applicable
  checks pass.

### Detailed checklist

- [x] Phase 1 / Task 1.1 complete.
- [x] Phase 1 / Task 1.2 complete.
- [x] Phase 1 / Task 1.3 complete.
- [x] Phase 2 / Task 2.1 complete.
- [x] Phase 2 / Task 2.2 complete.
- [x] Phase 2 / Task 2.3 complete.
- [x] Phase 3 / Task 3.1 complete.
- [x] Phase 3 / Task 3.2 complete.
- [x] Phase 3 / Task 3.3 complete.
- [x] Phase 4 / Task 4.1 complete.
- [x] Phase 4 / Task 4.2 complete.
- [x] Phase 4 / Task 4.3 complete.
- [x] Phase 5 / Task 5.1 complete.
- [x] Phase 5 / Task 5.2 complete.
- [x] Phase 5 / Task 5.3 complete.
- [x] Phase 6 / Task 6.1 complete.
- [x] Phase 6 / Task 6.2 complete.
- [x] Phase 6 / Task 6.3 complete.
- [x] Phase 6 / Task 6.4 complete.
- [x] Phase 6 / Task 6.5 complete.
- [x] Phase 7 / Task 7.1 complete.
- [x] Phase 7 / Task 7.2 complete.
- [x] Phase 7 / Task 7.3 complete.
- [x] Phase 7 / Task 7.4 complete.
- [x] Phase 7 / Task 7.5 complete.
- [ ] Phase 8 / Task 8.1 complete.
- [ ] Phase 8 / Task 8.2 complete.
- [ ] Phase 8 / Task 8.3 complete.

### Overall success criteria

- Public and private origin topologies are independently implementable.
- WAF is consistently enforced at the AFD edge in both topologies.
- Private mode never silently downgrades to public connectivity.
- Fleet membership changes produce safe origin add, drain, and removal.
- Existing `ServiceImport` consumers remain compatible.
- One `ServiceImport` can be consumed by multiple Gateways with independent,
  deterministic AFD connectivity contracts.
- The design does not bind Fleet to GEP-4894 `selectorRef`.
- The implementation plan is test-first, phased, and suitable for a sequence
  of reviewable pull requests.

## Decisions

- Keep direct `HTTPRoute` to Fleet `ServiceImport` references for the initial
  global ingress backend path. Do not bind production behavior to GEP-4894's
  namespace-local Pod selector.
- Use typed `AzureFrontDoorGatewayPolicy` and
  `AzureFrontDoorBackendAttachment` candidate APIs instead of annotations as
  the durable provider contract.
- Scope each backend attachment to a Gateway UID, `ServiceImport` UID, and
  service port. Keep references same-namespace initially and require
  `ReferenceGrant` before adding cross-namespace backend references.
- Resolve duplicate attachment tuples without disrupting traffic: the oldest
  attachment by creation timestamp, with UID as tie-breaker, remains accepted
  and later duplicates report `Accepted=False`, reason `Conflicted`.
- Program a route backend only when its parent Gateway,
  `ServiceImport`, and port match an accepted attachment. Multiple routes
  share that attachment and origin group; an unused attachment performs no
  Azure writes.
- Keep Azure endpoint and PLS metadata internal to
  `InternalServiceExport`; do not add Azure-specific fields to public
  `ServiceImport.status`.
- Use one homogeneous AFD origin group per logical backend. Public and Private
  Link origins cannot be mixed.
- Require AFD Premium for Private Link and for managed WAF rule sets.
- Reference an existing security-owned WAF policy initially; Fleet owns the
  AFD security-policy association.
- Require both `AzureFrontDoor.Backend` network filtering and exact
  `X-Azure-FDID` validation for public origins.
- Use restrictive, RBAC-only PLS visibility by default and manual private
  endpoint approval initially. Automatic approval is deferred until exact
  request correlation is proven safe.
- Distinguish desired configuration programming from AFD probe health and
  Private Link connection readiness.
- Deliver public origins first, then WAF enforcement, then Private Link, each
  behind feature gates.
- Use AFD, not Traffic Manager, as the SFI Application DDoS enforcement plane
  for internet-facing HTTP/S. Traffic Manager remains suitable only where a
  DNS steering product without Layer-7 enforcement is intentionally required.
- Limit this branch to the API and read-only controller foundation. Do not add
  an Azure SDK client, finalizer, or Azure write until a later implementation
  pull request.
- Support managed AFD profiles only in the initial API.
- Require member-cluster status to attest exact `X-Azure-FDID` conformance
  before a later public-origin controller may report the origin programmed.

## Implementation Details

- The proposal includes complete public and private request paths, example
  member/hub YAML, candidate CRD fields, validation, origin eligibility,
  traffic/drain behavior, WAF/TLS semantics, status, reconciliation,
  ownership, observability, scale, failures, migration, and alternatives.
- The implementation plan defines nine reviewable capability PRs and a
  test-first task sequence covering API, member transport, normalized model,
  Azure provider abstractions, public origins, WAF, Private Link, operations,
  and e2e rollout.
- Existing Traffic Manager patterns are reused for ServiceImport expansion,
  indexes, finalizer timing, ownership-aware deletion, status/events, and
  Azure error classification.
- The proposal now records the SFI product boundary, and Phase 6 includes
  tests and exit criteria for AFD Premium, WAF association, Bot Manager, rate
  limiting, diagnostics, and public/private origin-bypass controls.
- The direct `ServiceImport` backend policy was replaced with an explicit
  Gateway-to-`ServiceImport` attachment. Attachment status and finalizers now
  map to one AFD origin group consumption context.
- Phase 1 and normalized-model tests cover attachment reuse, duplicate tuples,
  missing matches, multiple routes sharing one origin group, and unused
  attachments producing no desired Azure resources.
- The initial provider API is implemented in `api/v1alpha1` with managed
  profile placement, existing WAF references, public and Private Link
  attachment contracts, health/traffic defaults, immutable references,
  bounded status conditions, and generated CRDs/deep copies.
- `pkg/controllers/hub/azurefrontdoorbackendattachment` resolves same-namespace
  Gateway, Gateway policy, ServiceImport, and service-port dependencies. It
  selects duplicate attachments by creation timestamp and UID, publishes
  generation-aware conditions and resolved UIDs, and deliberately adds no
  finalizer or Azure client.
- The controller uses the repository's klog `InfoS`/`ErrorS`, Kubernetes
  events, API-server error wrappers, status patching, field indexes, and
  dependency watches.
- The hub manager and Helm chart expose
  `--enable-azure-front-door-gateway-api`, defaulting to false. Conditional
  RBAC grants Gateway and provider-resource reads plus attachment status
  updates only.
- Gateway API `v1.2.1` is reused from the earlier GEP-1748 branch and is
  compatible with the repository's Kubernetes `v0.31.1` dependencies.

## Changes Made

- Created this breadcrumb and completed its approved plan.
- Added `docs/design/afd-public-private-origin-proposal.md`.
- Added `docs/design/afd-public-private-origin-implementation-plan.md`.
- Added the AFD-versus-Traffic Manager SFI Application DDoS rationale and its
  implementation gates to both design documents.
- Replaced the direct backend-policy candidate API with
  `AzureFrontDoorBackendAttachment` and closed implementation-plan Task 0.1.
- Added the two AFD `v1alpha1` API types, shared status types, unit and API
  integration tests, generated deep copies, and generated CRDs.
- Added the read-only backend attachment controller and table-driven
  reconciliation tests.
- Added hub-manager scheme registration, CRD discovery checks, feature-gate
  wiring, generated RBAC, conditional Helm RBAC, values, and chart
  documentation.
- Updated the CRD installer test so hub installations include the new AFD
  CRDs while member installations remain unchanged.
- Added and tidied the Gateway API `v1.2.1` dependency.
- Validation completed:
  - focused API, controller, and hub-manager tests passed;
  - all 60 `v1alpha1` API integration specs passed;
  - the integration command reported only the existing Windows envtest
    teardown limitation (`not supported by windows`) after all specs passed;
  - repository-wide compile-only tests and `go vet ./...` passed;
  - repository-pinned golangci-lint `v1.64.7`, rebuilt with Go `1.25.12`,
    passed for branch changes;
  - CRD-installer tests and disabled/enabled Helm rendering checks passed;
  - generated CRDs have no unrelated version drift and `git diff --check`
    passed.

## Before/After Comparison

Before this work, the repository had an AFD global ingress direction and a
GEP-4894 compatibility analysis, but no end-to-end contract for public and
private member-cluster origins. After this work, both topologies have explicit
Kubernetes and Azure resource models, security/readiness state machines, and a
test-first delivery sequence that does not depend on unstable `selectorRef`
behavior. A `ServiceImport` can also be reused by multiple Gateways without
forcing all AFD profiles to share one origin contract or one ambiguous status.
The branch now also contains an installable, default-off API and read-only
controller foundation that validates those attachment identities without
claiming Azure resources are programmed.

## References

- `docs/design/gep-4894-backend-evaluation.md`
- `rchinchani/afd-global-ingress-rfc:docs/design/afd-global-ingress-rfc.md`
- `rchinchani/gep-1748-gateway-api:docs/design/gep-1748-gateway-api.md`
- `rchinchani/gep-1748-gateway-api:docs/design/gep-1748-implementation-plan.md`
- GEP-1748: <https://gateway-api.sigs.k8s.io/geps/gep-1748/>
- GEP-4894: <https://gateway-api.sigs.k8s.io/geps/gep-4894/>
- `api/v1alpha1/azurefrontdoorgatewaypolicy_types.go`
- `api/v1alpha1/azurefrontdoorbackendattachment_types.go`
- `pkg/controllers/hub/azurefrontdoorbackendattachment/controller.go`
- Gateway API `v1.2.1`: <https://github.com/kubernetes-sigs/gateway-api/releases/tag/v1.2.1>
- Gateway API PR 5158:
  <https://github.com/kubernetes-sigs/gateway-api/pull/5158>
- Azure Front Door Private Link:
  <https://learn.microsoft.com/azure/frontdoor/private-link>
- Azure WAF on Front Door:
  <https://learn.microsoft.com/azure/web-application-firewall/afds/afds-overview>
- Azure Front Door origin security:
  <https://learn.microsoft.com/azure/frontdoor/origin-security>
- Azure Front Door origins and origin groups:
  <https://learn.microsoft.com/azure/frontdoor/origin>
- SFI NS 2.5.3 KPI:
  <https://eng.ms/docs/initiatives/project-standard/standards-categories/sc-networking/ddos/sfi-ns/sfi-ns253-kpi>
- Application DDoS Standard:
  <https://eng.ms/docs/initiatives/project-standard/standards-categories/sc-networking/ddos/ads/index>
- AFD to internal load balancer with Private Link:
  <https://learn.microsoft.com/azure/frontdoor/standard-premium/how-to-enable-private-link-internal-load-balancer>
- AKS internal load balancer and PLS:
  <https://learn.microsoft.com/azure/aks/internal-lb>
- AKS public Standard Load Balancer:
  <https://learn.microsoft.com/azure/aks/configure-load-balancer-standard>
- `api/v1alpha1/serviceimport_types.go`
- `api/v1alpha1/internalserviceexport_types.go`
- `pkg/controllers/member/serviceexport/controller.go`
- `pkg/controllers/hub/serviceimport/controller.go`
- `pkg/controllers/hub/trafficmanagerbackend/controller.go`
- `pkg/controllers/hub/trafficmanagerprofile/controller.go`
- `api/v1beta1/trafficmanagerbackend_types.go`
