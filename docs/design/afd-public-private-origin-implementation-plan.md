# Azure Front Door Public and Private Origins Implementation Plan

## Purpose

This plan implements the architecture in
`docs/design/afd-public-private-origin-proposal.md` as a sequence of reviewable pull
requests. It deliberately delivers public origins before Private Link while keeping the
API and normalized model capable of both topologies from the start.

All new behavior is disabled by default until its rollout gate is met. Tests are written
before or in the same change as the implementation they constrain.

## Gateway API GEP Alignment

- GEP-1748 supplies the portable routing contract:
  `HTTPRoute.backendRefs -> ServiceImport`.
- The provider-specific `AzureFrontDoorBackendAttachment` augments that route with an
  explicit `(Gateway, ServiceImport, port)` AFD connectivity contract.
- GEP-4894 remains an evaluated future integration point. This plan does not bind Fleet
  to its evolving namespace-local Pod `selectorRef`, because a hub selector cannot
  represent member-cluster endpoints.
- A future GEP-4894 migration must preserve `ServiceImport` identity, multi-cluster
  aggregation, deterministic attachment ownership, and status compatibility before it
  can replace the explicit attachment.

## Guiding Rules

- Keep `ServiceImport` backward compatible and status-only.
- Put Azure transport observations only in `InternalServiceExport`.
- Keep Gateway API fields portable; use typed provider CRDs for Azure behavior.
- Do not use GEP-4894's Pod selector for Fleet multi-cluster backends.
- Do not dual-write legacy annotations and typed provider resources.
- Never mix public and private origins in one AFD origin group.
- Never fall back from Private Link to a public origin.
- Treat AFD, not Traffic Manager, as the SFI Application DDoS enforcement plane for
  internet-facing HTTP/S. Traffic Manager is DNS-only and cannot satisfy the required
  Layer-7 proxy, WAF, bot-management, rate-limit, or origin-bypass controls.
- Add a finalizer only immediately before the first owned Azure write.
- Preserve the last known good Azure configuration when new desired state is invalid or a
  dependency read fails transiently.
- Run `goimports`, `go vet`, package tests, and generated-manifest verification for every
  Go/API pull request. Run `go mod tidy` only when dependencies change.

## Delivery Overview

| PR | Capability | Default state |
| --- | --- | --- |
| 1 | API contracts, CRDs, validation, generated code | No controller |
| 2 | Member transport discovery for public and PLS metadata | Feature-gated |
| 3 | Normalized Gateway/Fleet model and read-only status | Feature-gated |
| 4 | Azure provider interfaces, fakes, ownership, retry model | No production writes |
| 5 | Managed AFD profile, endpoint, public origin group, public origins | Feature-gated |
| 6 | Routes, custom domains/certificates, public draining and lifecycle | Feature-gated |
| 7 | Required WAF association, diagnostics, public bypass checks | Feature-gated |
| 8 | Private Link origin creation and manual approval workflow | Feature-gated |
| 9 | Production hardening, charts, scale, upgrade, and e2e | Candidate for opt-in |

## Phase 0: Resolve API Review Questions

### Task 0.1: Record the approved backend attachment scope

**Decision**

- Use `AzureFrontDoorBackendAttachment` with explicit `gatewayRef` and `backendRef`.
- Identify one attachment by Gateway UID, `ServiceImport` UID, and service port.
- Keep all three resources in one namespace initially. Require `ReferenceGrant` before
  later enabling cross-namespace `backendRef`.

**Work**

- Reject direct `AzureFrontDoorBackendPolicy.targetRef -> ServiceImport` because AFD
  connectivity, probes, traffic settings, readiness, and Azure ownership are scoped to a
  particular Gateway/profile consumption.
- Admission rejects observable duplicates. Under concurrent creation, the oldest
  attachment by creation timestamp, with UID as tie-breaker, remains accepted; later
  duplicates report `Accepted=False`, reason `Conflicted`, without disrupting traffic.

**Tests**

- API examples for one ServiceImport reused by two Gateways.
- Conflict tests for duplicate Gateway/ServiceImport/port tuples.
- Same ServiceImport with distinct Gateways or ports.
- Same-namespace enforcement and future `ReferenceGrant` behavior.

**Exit criteria**

- Attachment reuse, conflicts, authorization, status, and Azure ownership are
  deterministic.

**Status:** Resolved for the candidate `v1alpha1` API.

### Task 0.2: Approve profile and WAF ownership

**Work**

- Decide whether the first release supports both `Managed` and `Existing` profiles or
  only `Managed`.
- Confirm that WAF policies are referenced, not created.
- Define required Azure ownership tags and hub cluster identity source.

**Exit criteria**

- Creation, adoption, update, and deletion ownership are explicit for every Azure
  resource type.

**Decision**

- The initial API supports `Managed` profiles only.
- WAF policies are existing, security-owned resources referenced by resource ID.
- Existing-profile adoption is deferred until ownership and deletion behavior can be
  proven independently.

**Status:** Resolved for the candidate `v1alpha1` API.

### Task 0.3: Approve public bypass conformance contract

**Work**

- Decide how the controller proves exact `X-Azure-FDID` enforcement when the application
  owns that rule.
- Select one initial mechanism:
  - a typed acknowledgement on the backend attachment;
  - a separate conformance condition published by a member agent;
  - a probe that expects rejection for a request with an invalid ID.
- Keep `AzureFrontDoor.Backend` Service annotation validation mandatory.

**Exit criteria**

- A public origin cannot become `Programmed=True` merely because it has a public IP.

**Decision**

- A member-cluster condition reports exact `X-Azure-FDID` conformance.
- The hub controller must not infer conformance from a user acknowledgement.
- Negative hub-side probing may be added as defense in depth later, but is not the
  authoritative initial signal.

**Status:** Resolved for the candidate `v1alpha1` API.

## Phase 1: API Types, CRDs, and Validation

### Task 1.1: Add API unit tests first

**Candidate files**

- `api/v1alpha1/azurefrontdoorgatewaypolicy_types_test.go`
- `api/v1alpha1/azurefrontdoorbackendattachment_types_test.go`
- `api/v1alpha1/validation/azurefrontdoor_test.go`
- `test/apis/azurefrontdoor_integration_test.go`

**Test cases**

- defaults for probe method, interval, priority, weight, failure policy, and certificate
  validation;
- invalid profile SKU and Private Link combinations;
- invalid probe sample counts;
- invalid origin ports/protocols;
- missing WAF policy when required;
- duplicate Gateway/ServiceImport/port attachment tuples;
- one ServiceImport reused by distinct Gateways and service ports;
- multiple routes sharing one attachment and one origin group;
- route backend without a matching attachment;
- accepted but unused attachment causing no Azure writes;
- same-namespace attachment enforcement;
- immutable placement fields after Azure resources exist;
- status list bounds and condition generation.

**Exit criteria**

- Tests fail because types and validation do not yet exist.

### Task 1.2: Implement provider APIs

**Candidate files**

- `api/v1alpha1/azurefrontdoorgatewaypolicy_types.go`
- `api/v1alpha1/azurefrontdoorbackendattachment_types.go`
- `api/v1alpha1/azurefrontdoor_status_types.go`
- `api/v1alpha1/groupversion_info.go`

**Work**

- Add kubebuilder validation markers and defaults.
- Use Gateway API `LocalPolicyTargetReference` conventions for the Gateway policy where
  compatible; define explicit Gateway and backend references for the attachment.
- Make attachment conflict precedence, status ownership, and immutable identity fields
  explicit.
- Define conditions and reasons as constants.
- Avoid storing secrets or raw Azure error bodies.
- Keep per-member status bounded by the selected member count.

**Validation**

- Run unit and API integration tests.
- Regenerate deep copies and CRDs.
- Verify generated files have no manual drift.

**Exit criteria**

- CRDs reject all invalid combinations identified in Task 1.1.

### Task 1.3: Add feature gates and RBAC

**Candidate files**

- controller option/feature-gate packages used by existing managers;
- `charts/hub-net-controller-manager/templates/`;
- generated role manifests under `config/`.

**Work**

- Add `AzureFrontDoorGatewayAPI` feature gate, default `false`.
- Add gateway-policy and backend-attachment read/write/status RBAC.
- Do not add Azure credentials to chart values.

**Tests**

- Helm rendering with feature gate off and on.
- RBAC test proving the controller cannot modify unrelated Gateway objects.

**Exit criteria**

- Installing the CRDs does not start reconciliation or create Azure resources.

## Phase 2: Internal Member Transport

### Task 2.1: Write transport discovery tests

**Candidate files**

- `pkg/controllers/member/serviceexport/controller_test.go`
- Azure provider fake tests near the existing member ServiceExport provider.

**Test cases**

- public Service reports public IP resource ID, FQDN, location, and bypass annotation;
- public IP exists without DNS settings;
- internal Service reports load balancer address and PLS resource ID/alias/location;
- PLS provisioning, failed, and deleted states;
- multiple PLS resources do not produce nondeterministic selection;
- stale Azure resource ID is removed from internal transport;
- Service changes from public to private and private to public;
- visibility and auto-approval observations are sanitized;
- Azure authorization, not-found, conflict, and throttling errors.

**Exit criteria**

- Tests define exactly when public and private transport is ready.

### Task 2.2: Extend InternalServiceExport

**Candidate files**

- `api/v1alpha1/internalserviceexport_types.go`
- generated CRDs and deep-copy files.

**Work**

- Add an optional Azure transport structure.
- Preserve existing fields during the transition.
- Store Azure location, public endpoint metadata, load balancer classification, and PLS
  metadata.
- Do not copy PLS details into `ServiceImport.status`.

**Compatibility**

- Older member agents omit new fields and remain readable by the hub.
- New member agents continue writing fields required by existing Traffic Manager
  reconciliation.

**Exit criteria**

- Existing ServiceImport behavior is unchanged.

### Task 2.3: Implement member discovery

**Candidate files**

- `pkg/controllers/member/serviceexport/controller.go`
- existing Azure client interface and fake implementations.

**Work**

- Resolve the Service load balancer frontend deterministically.
- Resolve public IP DNS settings and location.
- Resolve PLS by the load balancer frontend configuration/resource relationship, not name
  guessing alone.
- Classify errors using repository-standard retry behavior.
- Watch/requeue on Service and ServiceExport changes; Azure state remains polled with
  bounded backoff.

**Validation**

- Run modified package tests and `go vet`.
- Add envtest for public/private transition and deletion.

**Exit criteria**

- The hub receives sufficient observed metadata without querying member clusters.

## Phase 3: Normalized Model and Read-only Reconciliation

### Task 3.1: Write model tests

**Candidate package**

- `pkg/controllers/hub/gatewaymodel/`

**Test cases**

- `HTTPRoute` resolves direct `ServiceImport` references;
- route backend weight remains separate from member origin weight;
- `ServiceImport` expands into deterministic sorted member origins;
- all-public and all-private classifications;
- mixed public/private rejection;
- missing attachment, duplicate attachment tuple, and invalid port;
- one ServiceImport attached to distinct Gateways with independent connectivity;
- multiple routes sharing one Gateway/ServiceImport/port attachment;
- route backend without an accepted attachment;
- unused attachment omitted from the desired Azure model;
- `All` versus `Partial` member failure policy;
- zero eligible origins;
- same PLS/resource/region with different port rejection;
- Private Link on Standard SKU rejection;
- namespace and `ReferenceGrant` handling for supported references.

**Exit criteria**

- Model behavior is fully testable without Azure clients.

### Task 3.2: Implement model builder and indexes

**Work**

- Add field indexes:
  - gateway policy by target UID/name;
  - backend attachment by Gateway and ServiceImport/port tuple;
  - route by parent Gateway;
  - route by ServiceImport backend;
  - InternalServiceExport by ServiceImport identity;
  - ServiceImport by selected cluster.
- Return immutable desired model objects with deterministic Azure names.
- Include a stable hash of desired Azure-owned fields for drift/status.
- Never materialize member Pods or EndpointSlices on the hub.

**Exit criteria**

- One event queues only the affected Gateway/profile reconciliations.

### Task 3.3: Add read-only status controller

**Work**

- Reconcile gateway policies, backend attachments, and Gateway API references without
  Azure writes.
- Set `Accepted`, `ResolvedRefs`, transport readiness, and topology validation conditions.
- Emit transition events for missing public FQDN, missing PLS, mixed topology, and
  attachment conflicts.

**Tests**

- Envtest with Gateway, HTTPRoute, ServiceImport, gateway policies, backend attachments,
  and InternalServiceExports.
- Confirm feature gate off produces no status mutation.

**Exit criteria**

- Operators can validate YAML and member readiness before granting Azure permissions.

## Phase 4: Azure Provider Layer

### Task 4.1: Define provider interfaces and fakes

**Candidate package**

- `pkg/azure/frontdoor/`

**Interfaces**

- profile;
- endpoint;
- custom domain and certificate;
- origin group;
- origin;
- route;
- security policy;
- diagnostics;
- PLS/private endpoint connection reader.

Each operation accepts context and returns typed result/error information. Interfaces are
resource-oriented and narrow enough to fake without reproducing the entire Azure SDK.

**Tests**

- compile-time interface conformance;
- fake operation recording;
- context cancellation;
- typed not-found, conflict, throttling, authorization, and asynchronous provisioning.

### Task 4.2: Implement error classification and retry

**Work**

- Reuse existing Traffic Manager Azure error classification where possible.
- Honor `Retry-After`.
- Distinguish:
  - retryable control-plane errors;
  - invalid desired state;
  - authorization errors requiring operator action;
  - ownership conflicts;
  - asynchronous provisioning.
- Add per-profile mutation serialization and bounded concurrency.

**Exit criteria**

- Unit tests prove no hot loop for pending Private Link approval or Azure throttling.

### Task 4.3: Implement ownership and cleanup primitives

**Work**

- Generate stable resource names from namespace/name/UID and member cluster identity.
- Apply ownership tags.
- Require exact tags before update/delete.
- List owned children for cleanup.
- Provide disable/drain/delete operations.

**Tests**

- Existing untagged resource collision;
- matching ownership update;
- mismatched UID after Kubernetes object recreation;
- partial deletion retry;
- referenced WAF/PLS/public IP never deleted.

**Exit criteria**

- Destructive tests prove the controller cannot delete unowned Azure resources.

## Phase 5: Public Origin Vertical Slice

### Task 5.1: Write public reconciler tests

**Candidate files**

- `pkg/controllers/hub/azurefrontdoorgateway/controller_test.go`
- `pkg/controllers/hub/azurefrontdoorbackend/controller_test.go`

**Test sequence**

1. no finalizer before valid model;
2. add finalizer immediately before profile creation;
3. create profile and endpoint;
4. create origin group with probe/load-balancing settings;
5. create one origin per eligible member;
6. wait for asynchronous Azure provisioning;
7. set provider status;
8. update only changed fields;
9. disable/drain/delete removed member;
10. clean up owned resources on attachment/Gateway deletion.

**Failure cases**

- no public FQDN;
- internal member in a public attachment;
- bypass annotation absent;
- origin-host/certificate mismatch observed through Azure;
- one member invalid under `Partial` and `All`;
- Azure 429/409/403;
- controller restart during deletion.

### Task 5.2: Implement profile and public origin reconciliation

**Work**

- Start with managed profiles unless Phase 0 approves existing profiles.
- Create one origin group per normalized logical backend/configuration.
- Use member FQDN as origin hostname.
- Keep certificate subject-name validation enabled for HTTPS.
- Create disabled origins first; enable only after required dependencies exist.
- Normalize ServiceExport weights to Azure origin weights.

**Exit criteria**

- A fake-Azure integration test programs two public member origins and survives one member
  removal without traffic configuration loss.

### Task 5.3: Implement Gateway API route reconciliation

**Work**

- Map listeners/domains/routes to the AFD endpoint.
- Support the agreed subset of `HTTPRoute` matches, filters, and backend references.
- Report unsupported Gateway API fields with `UnsupportedProtocol` or
  `UnsupportedValue`-style standard reasons.
- Enable the route only after origin group and frontend dependencies are ready.

**Tests**

- route attach/detach;
- hostname conflict;
- unsupported filters;
- two logical backend weights;
- zero ready origins;
- deterministic reconciliation after restart.

**Exit criteria**

- Public traffic can flow in a disposable test subscription with WAF still gated off.

## Phase 6: WAF, TLS, and Public Bypass Enforcement

### Task 6.1: Write WAF association tests

**Test cases**

- referenced policy exists and matches profile tier;
- policy missing, unauthorized, wrong tier, or disabled;
- every route domain is associated;
- domain moves between policies without an unprotected enabled interval;
- WAF required versus explicitly optional development mode;
- security policy drift.

### Task 6.2: Implement security-policy association

**Work**

- Read the existing WAF policy.
- Create/update `Microsoft.Cdn/profiles/securityPolicies`.
- Associate only domains owned by the target Gateway.
- Apply WAF policy changes without recreating origins.
- Fail closed when `waf.required` is true.

**Exit criteria**

- No AFD route is enabled before its domain is WAF-associated.

### Task 6.3: Implement certificate and bypass gates

**Work**

- Integrate the reviewed `AzureFrontDoorCertificate` contract.
- Report frontend certificate provisioning separately from origin TLS.
- Expose AFD profile ID in policy status.
- Validate `AzureFrontDoor.Backend` observation and the approved exact-FDID conformance
  mechanism.
- Add documentation and example regional ingress rules.

**Live validation**

- Direct request to public origin without valid FDID is denied.
- Request through AFD succeeds.
- WAF detection and prevention logs reach diagnostics destination.

**Exit criteria**

- Public vertical slice is production-security complete, not merely routable.

### Task 6.4: Enforce the SFI Application DDoS baseline

**Tests**

- reject production configuration that is not AFD Premium;
- reject a missing WAF association or disabled Bot Manager managed rule set;
- reject configuration with no enabled rate-limit custom rule;
- verify public origin bypass denial and private origin public unreachability;
- verify diagnostics expose AFD access, health-probe, and WAF events.

**Exit criteria**

- Every production HTTP/S route is demonstrably behind the AFD enforcement plane before
  it is enabled; a Traffic Manager endpoint is never accepted as an equivalent control.

## Phase 7: Private Link Vertical Slice

### Task 7.1: Write Private Link state-machine tests

**Test cases**

- PLS absent, provisioning, ready, failed, and replaced;
- origin creation yields pending AFD-managed private endpoint request;
- manual approval pending does not hot loop or report an error;
- approved but not established;
- established and origin enabled;
- rejected/disconnected connection;
- no public fallback;
- wrong SKU;
- unsupported/member region mapping;
- same PLS tuple and port conflict;
- private endpoint reuse across origins;
- public member in a private attachment;
- deletion while approval is pending.

### Task 7.2: Implement PLS origin reconciliation

**Work**

- Require Premium profile.
- Use observed PLS resource ID.
- Select same or nearest supported Private Link region using a versioned mapping.
- Set request message with stable Kubernetes/profile identity.
- Create origin disabled until connection is established.
- Read and publish connection state.
- Leave approval manual.
- Ensure AFD probes use the private path.

**Exit criteria**

- A two-member private topology reaches `OriginsProgrammed=True` only after both PLS
  connections satisfy the configured member failure policy.

### Task 7.3: Implement private lifecycle and resilience

**Work**

- Handle PLS resource replacement as a new connection requiring approval.
- Drain old origin before removing the old private endpoint relationship.
- Preserve other origins when one regional connection fails.
- Serialize topology transitions so Azure never sees a mixed origin group.
- Add alerts for prolonged pending/rejected/disconnected states.

**Live validation**

- Member internal load balancer has no public route.
- AFD request succeeds after approval.
- Direct internet request cannot reach the origin.
- One private member/region outage shifts traffic to another healthy origin.

**Exit criteria**

- Private topology passes security and failover tests with no public endpoint.

## Phase 8: Operations, Scale, and Productization

### Task 8.1: Add metrics, events, and diagnostics

**Work**

- Add reconciler, origin state, Private Link state-age, WAF, Azure retry, and programming
  latency metrics.
- Emit bounded transition events.
- Include object UID, Azure resource name, member cluster, and correlation ID in logs.
- Add sample alerts and dashboards.

**Tests**

- Metric label cardinality test.
- Event deduplication test.
- Sanitization test for Azure errors.

### Task 8.2: Add quota and scale protection

**Work**

- Preflight known AFD resource counts.
- Limit concurrent Azure writes per profile/subscription.
- Cache/deduplicate Azure reads during one reconciliation.
- Add load tests for large numbers of routes, ServiceImports, and member origins.
- Verify current Azure service limits before publishing supported scale.

**Success criteria**

- At target scale, reconciliation meets the agreed latency SLO without Azure throttling
  storms or unbounded hub memory.

### Task 8.3: Complete charts, docs, and supportability

**Work**

- Chart values for feature gates, Azure identity/client configuration, concurrency, and
  diagnostics.
- Example YAML for public and private topologies.
- Troubleshooting guide for WAF, certificates, public bypass, PLS approval, and probes.
- Upgrade/downgrade guide and CRD compatibility policy.
- Document Azure permissions separately for profile management and optional future PLS
  approval.

**Exit criteria**

- A new operator can deploy both topologies using only published examples and diagnose
  every failure-matrix state from status/events.

## Phase 9: End-to-End Validation and Rollout

### Task 9.1: Automated test matrix

| Layer | Required coverage |
| --- | --- |
| Unit | defaults, validation, model, naming, weight normalization, state machines, error classification |
| Envtest | watches/indexes, policy/attachment conflicts, conditions, finalizers, deletion, feature gates |
| Fake Azure integration | complete create/update/drain/delete flows and injected Azure errors |
| Live Azure integration | profile/origin/route/WAF/PLS API shapes and asynchronous states |
| E2E public | two clusters, WAF, bypass denial, health failover, member removal |
| E2E private | two clusters/regions, manual approvals, no public reachability, failover |
| Upgrade | gate off/on, controller restart, CRD upgrade, rollback with existing Azure resources |

Tests must use unique resource names, explicit cleanup, and ownership assertions. Live tests
must fail rather than silently skip cleanup errors.

### Task 9.2: Rollout gates

1. **Developer preview:** APIs and read-only status only.
2. **Private preview - public:** managed profile, public origins, WAF required.
3. **Private preview - PLS:** Premium only, manual approval.
4. **Public preview:** scale and multi-region failure tests complete.
5. **General availability:** upgrade compatibility, SLOs, support documentation, and
   security review complete.

Each gate defines:

- supported Gateway API fields;
- supported Azure clouds/regions;
- maximum tested scale;
- upgrade paths;
- known limitations;
- rollback procedure.

### Task 9.3: Rollback

Feature-gate rollback stops new reconciliation but must not orphan finalizers. Provide a
safe suspend mode:

- no create/update writes;
- status reports `ReconciliationSuspended`;
- deletion cleanup remains available through an explicit controller mode or documented
  operator procedure.

For production traffic rollback:

1. disable affected AFD route;
2. restore the last known good route/origin-group configuration;
3. verify WAF association;
4. drain newly introduced origins;
5. remove only owned resources;
6. retain referenced WAF, PLS, load balancer, and public IP resources.

## Pull Request Validation Checklist

Every implementation PR must complete the applicable items:

- [ ] Tests were added before or with behavior.
- [ ] Modified package tests pass.
- [ ] `goimports` made no further changes.
- [ ] `go vet` passes for modified packages.
- [ ] Generated deep copies and CRDs are current.
- [ ] Helm templates render with feature gates on and off.
- [ ] No dependency changed without `go mod tidy`.
- [ ] No secret, token, certificate content, or unbounded Azure error is stored in API
  spec/status.
- [ ] Azure writes are ownership checked and idempotent.
- [ ] Deletion and controller restart are tested.
- [ ] Conditions include `observedGeneration` and stable reasons.
- [ ] Public/private topology cannot silently change.
- [ ] Documentation and examples match the implemented API.

## Completion Criteria

The implementation is complete when:

- public and private topologies pass their two-member, multi-region e2e suites;
- public origin bypass attempts are denied while AFD traffic succeeds;
- private origins have no public path and never receive public fallback;
- WAF is associated before routes are enabled;
- member add, failure, drain, removal, and Fleet leave are safe and observable;
- PLS pending, approved, established, rejected, and disconnected states are distinguishable;
- origin programming and probe health are separate status signals;
- unowned Azure resources survive all update and deletion tests;
- tested scale and Azure quotas are documented;
- feature-gate rollback and controller restart do not strand finalizers or traffic;
- `ServiceImport` API compatibility and existing Traffic Manager tests remain intact.
