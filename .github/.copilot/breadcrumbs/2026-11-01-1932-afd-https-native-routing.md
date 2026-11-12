# HTTPS native Gateway API routing and customer demonstration

**Status: deferred until the existing private HTTP implementation completes a
live test.** This document is a future plan, not authorization to implement
HTTPS/native routing before that proof.

## Requirements

- Plan production HTTPS support across documentation, implementation, tests and
  live verification. No production code changes precede explicit plan approval.
- Choose native Gateway API routing in the Fleet AFD provider. Add no Go proxy,
  member ingress controller or other new routing component.
- Reuse the existing hub GatewayClass/Gateway/HTTPRoute reconcilers and AFD
  provider in `hub-gateway-controller-manager`; add no new controller/reconciler
  or controller Deployment. Extend existing member export discovery as needed.
- Keep portable routing in Gateway/HTTPRoute and ServiceImport references under
  GEP-1748. Identify Azure-specific certificate configuration explicitly rather
  than claiming it is a portable Gateway API certificate contract.
- Demonstrate one HTTPS Gateway with two ServiceImports: HTTPRoute path rules
  route `/catalog` to catalog and `/checkout` to checkout in both members.
- Each application's exported Service uses its own internal LoadBalancer/PLS.
  No public member ingress or implicit HTTP fallback is permitted.
- Preserve AFD Premium/Private Link, exact ownership and authorization, bounded
  operations, independent traffic proof, member/export lifecycle and cleanup.
- Preserve explicitly configured existing HTTP preview behavior; new HTTPS
  intent must not add an HTTP listener or silently downgrade origin transport.
- Require trusted certificate chains and hostname/SNI validation for HTTPS
  origins. Do not disable certificate checks or use insecure live probes.
- Remove the one-backend-per-Gateway restriction through a real multi-backend
  compiler/lifecycle extension, not merely deleting a preflight check.
- Preserve forty origins per backend and the existing aggregate capacity guards;
  scale expansion is outside this scope. Keep one backend per HTTPRoute rule
  initially; weighted multi-service backend splits are also outside this scope.
- Include runnable customer-facing examples and explicit operational expectations
  in the documentation commit, aligned with implemented and verified behavior.
- Make Traffic Manager resilience optional and off by default for customers.
  AFD-only deployments must not require ATM resources, a standby ingress or
  ATM-specific permissions/certificate changes. Preserve the selected managed
  frontend certificate strategy for the planned AFD-only HTTPS mode.
- Document ATM as a separate enhancement proposal only. Do not implement it or
  make its open design decisions prerequisites for native HTTPS/path routing.
- First run and verify the existing HTTP/private-link code, including installed
  automatic activation, traffic/probes, withdrawal and cleanup. Add HTTPS/native
  routing only later, after that live proof and explicit implementation approval.

## Additional comments from user

- "in production i need https, so plan accordingly including docs, implementation, tests and live testing"
- "also add a live test to exercise this path so it can be demonstrated to customers"
- The earlier proxy proposal was a workaround for the current execution subset,
  not a Gateway API requirement. The customer demonstration now uses AFD's native
  HTTPRoute-to-backend translation instead.
- Frontend certificate strategy selected: Azure-managed customer domains,
  with generated AFD hostname coverage as well.
- User asked "what is private proxy?" before selecting TLS coverage; coverage
  remains undecided.
- User asked "is the internal proxy as per Gateway api?" At that point routing
  ownership was unresolved. A standalone demo proxy is application routing,
  not Fleet's implementation of Gateway API path routing.
- "why not pick this?" chooses native Fleet/AFD Gateway API routing over an
  additional proxy. The revised demonstration necessarily has two ServiceImports,
  not one ServiceImport containing two distinct application backends.
- "in the doc commit, also add customer facing examples and expectations"
- "one additional concern is that AFD has outages sometimes and ATM is added
  in front to mitigate it"
- "is that possible to do with annotations, etc to make a complete solution"
  asks about optional controller-managed ingress resilience. This is a design
  question, not approval of another ingress provider or a certificate change.
- "make the ATM requirement optional for customers"
- "add ATM enhancement that as a separate doc" and "do not implement that"
- "HTTPS/native-routing implementation scope" and "confirm that this aligns
  with Gateway API and does not add any additional controller than the afd controller"
- "we will add this later after a live test with existing code"

## Plan

### Phase 0: Prove the existing implementation first

- Task 0.1: Obtain current authorized live-run prerequisites and build/install
  the existing committed private HTTP candidate without HTTPS/native additions.
  Success: the deployed image matches the tested existing implementation and
  the standard live fixture starts under valid access and cleanup safeguards.
- Task 0.2: Require automatic endpoint/route activation, exact caller-approved
  PLS bindings, both-member HTTP/Host/probe evidence, withdrawal and verified
  owned cleanup. Success: a complete live result, not configuration-only or
  manually activated traffic evidence.

### Phase 1: Finalize native routing/TLS contracts and write regressions first

- Task 1.0: Record native Fleet/AFD routing as the selected architecture.
  Success: one Gateway and two ServiceImports, without an additional proxy or
  controller; existing hub reconcilers/provider and member discovery are reused.
- Task 1.1: Confirm TLS boundaries: HTTPS client-to-AFD and AFD-to-each-private
  application is the recommended production baseline. There is no proxy hop.
  Success: the docs, security contract and live fixtures agree on every hop.
- Task 1.2: Use the selected Azure-managed customer-domain frontend certificates,
  with generated AFD hostname coverage as well. Frontend Key Vault BYOC is deferred.
  Inspect the selected Gateway API schema and Azure SDK before specifying the
  certificate/ref/options contract; do not invent unsupported policy translation.
  Success: certificate ownership, DNS validation and required identities are explicit.
- Task 1.3: Add focused table-driven resolver/model/provider regressions before
  changing implementation: two logical backends in separate rules, deterministic
  route lowering, exact group references, HTTPS listeners/forwarding/probes,
  certificate checks, no downgrade, reference failures and configuration drift.
  Success: intended cases fail against the current HTTP-only implementation.
- Task 1.4: Establish a feasible native AFD translation for Gateway API path
  semantics before enabling it. Test case-sensitive Exact/segment-based
  PathPrefix behavior, precedence/ties, unmatched paths and Host/path/query
  preservation. Do not assume AFD's ordinary path patterns are equivalent.
  Choose supported routes/rule sets with real SDK evidence; return unsupported
  conditions for behavior that cannot be honored. If the requested demonstration
  cannot be represented faithfully, report that constraint before implementation
  approval is expanded; do not substitute a hidden proxy or approximate routing.
  Success: positive and negative routing tests prove the advertised subset.

### Phase 2: Carry explicit TLS intent and trusted origin identity

- Task 2.1: Extend normalized intent and source validation for HTTPS listener,
  frontend certificate configuration and private-origin TLS identity.
  Current origin discovery only carries an internal IP; HTTPS needs a valid DNS
  origin hostname matching a trusted backend certificate. Keep the verified PLS
  resource/location binding authoritative for private network connectivity.
  Success: no certificate-check-disabled or private-IP-only TLS shortcut.
- Task 2.2: Resolve supported certificate/policy references with scoped access,
  current identity and explicit failures for missing, conflicting or unsupported
  settings. Reject custom CA policies that AFD cannot honor. Add dependency
  watches to existing reconcilers, not new policy/certificate controllers.
  Origin certificate provisioning remains an application/platform responsibility.
  Success: malformed TLS input authorizes no new Azure write or hostname publication.
- Task 2.3: Compile effective HTTPRoute rules into deterministic owned AFD route
  and, where needed, rule-set resources, with an origin group per distinct
  ServiceImport/port. Reuse the existing multi-backend planner and preserve
  supported match precedence rather than flattening different Services together.
  Count actual generated routes/rules/domains as well as backend resources.
  Success: each request selects its intended service/member group and capacity
  rejection returns no usable write plan.

### Phase 3: Extend the owned AFD lifecycle without weakening guards

- Task 3.1: Build HTTPS-only multi-backend routes, origin forwarding and probes,
  enforcing origin certificate-name checks. Reject accidental HTTP downgrade.
  Decide HTTP rejection versus an explicitly requested redirect; do not introduce
  an implicit HTTP listener.
  Success: exact desired and observed TLS properties are checked before activation.
- Task 3.2: Add the selected owned frontend domain/certificate lifecycle,
  including asynchronous DNS/certificate issuance, renewal and cleanup. Extend
  inventory only for supported verified resources; keep unrelated resources blocked.
  Preserve endpoint-first automatic activation and configuration-based Programmed.
  Success: no false TLS-health claims, fabricated approval or abandoned owned resources.
- Task 3.3: Generalize activation, full inventory and stale-resource reconciliation
  beyond single route/group assumptions. Route/backend/member/export removal
  retires only the verified affected resources; healthy other application routes
  remain active. The final effective route/backend still disables the endpoint
  and withdraws owned graph resources while retaining roots for re-export.
  Preserve one-write/30-second operations and fresh ownership/private verification.
  Verify default request pacing makes progress for the two-service graph; avoid
  introducing starvation through additional sequential inventory reads.
  Success: catalog removal does not withdraw checkout, and final withdrawal
  removes all supported owned route/group-origin/rule associations safely.

### Phase 4: Build the native two-application demonstration and offline tests

- Task 4.1: Reuse the existing sample-app/fixture pattern with catalog and checkout
  TLS-serving applications in each of two members. Export both private application
  Services and attach path rules to one HTTPS Gateway; deploy no router/proxy.
  Success: two ServiceImports, two backend groups and four private member origins
  behind one owned AFD profile/endpoint.
- Task 4.2: Add focused routing/TLS and proof-helper tests for service selection,
  fresh nonces, member identity, Host/path/query preservation, TLS verification,
  unexpected-path isolation and negative certificate cases.
  Local TLS tests can trust their explicit test CA; live AFD-origin certificates
  must meet Azure's public trusted-CA requirements.
  Success: wrong-service responses and insecure TLS cannot satisfy proof.
- Task 4.3: Add multi-backend controller/provider lifecycle tests, including
  member arrival/removal for each service, one backend disappearing while the
  other continues, route/filter/certificate drift, unsupported inventory,
  source read ambiguity, shared operation budgets and final graph withdrawal.
  Success: tests cover the new graph shape rather than only the positive demo.

### Phase 5: Update docs and validate implementation

- Task 5.1: Update main how-to, chart/test guides and a runnable customer demo.
  Explain TLS termination boundaries, certificate/DNS ownership, native HTTPRoute
  paths selecting separate ServiceImports, private-only topology, current limits and
  the distinction between Programmed and real TLS delivery.
  In the documentation commit, include:
  - Copyable Gateway/HTTPRoute and private Service/ServiceExport examples for
    catalog and checkout, with explicit placeholders, prerequisites and which
    member/hub operator applies each resource. Do not require application
    customers to configure ServiceImport annotations or directly access the hub.
  - Trusted HTTPS request examples for both paths, expected service/member
    responses, path/query forwarding without implicit prefix stripping, and
    demonstrated unmatched/case/segment behavior. Explain certificate SNI
    separately from the HTTP Host header; no curl -k examples.
  - A simple topology and expectations: the AFD edge is public HTTPS, while
    application LoadBalancers/origins remain private through Private Link.
    Azure-managed edge certificates do not provision origin certificates.
  - Link the separate documentation-only optional ATM proposal. Do not require
    ATM or standby ingress, invent a supported annotation, or claim independent
    AFD-outage protection for the standard AFD-only mode.
  - DNS/domain-certificate validation, asynchronous PLS approval and actor
    responsibilities, readiness versus traffic proof, observable failure states,
    troubleshooting, propagation expectations without a guaranteed duration,
    and what happens when a member or one complete application disappears.
  - Honest customer limits: forty eligible member origins per ServiceImport/port,
    aggregate origin/group and generated-resource limits, one backend per rule
    initially, no advertised Fleet Gateway-count guarantee, and no claim of
    forty-cluster load validation from the two-member demonstration.
  - A reproducible live-demo procedure, actual validation evidence and owned
    cleanup steps, separating implemented support from deferred features.
  Success: docs match supported behavior and clearly separate future capabilities.
- Task 5.2: Run complete affected packages, provider/TLS/native-routing regressions,
  tagged/non-tagged E2E compatibility, existing goimports/golint/vet/lint,
  manager/sample-app builds and relevant chart/script tests.
  Success: checks pass without weakening security controls or unrelated fixes.
- Task 5.3: Write the separate optional ATM enhancement proposal and link it from
  customer guidance. Success: default-off, limitations and unresolved design are
  explicit, with no implementation or native-HTTPS dependency.

### Phase 6: Obtain fresh live prerequisites and prove the customer scenario

- Task 6.1: Independently verify current Owner/effective access, lease, quotas,
  billable fixture scope, certificate availability and authorized DNS operations.
  Existing live-run access has expired; no live execution is authorized to
  bypass that guard. Arrange certificates securely, never paste or commit keys.
  Success: trusted per-origin names/certificates and time-bounded ownership are ready.
- Task 6.2: Build/install the exact validated candidate on a fresh two-member
  fixture, with auto-approval explicitly enabled and retention false.
  From both the Azure-managed customer domain and generated AFD HTTPS endpoint,
  repeatedly request `/catalog` and `/checkout` with
  fresh nonces/query values; require both services on both members, preserved Host,
  valid client/origin TLS, actual HTTPS-origin probe logs and exact PLS approval
  for all four verified private bindings. Also verify unmatched/case/prefix
  negative cases and continued checkout delivery after catalog withdrawal.
  Success: no portal activation, local-controller substitute or curl -k.
- Task 6.3: Require withdrawal and cleanup, collect a redacted report showing
  Gateway/ServiceImport/AFD identities and service/member response evidence,
  then independently verify owned cloud resource absence.
  Success: a reproducible customer demonstration and a complete live-proof result.

### Checklist

- [ ] Task 0.1: Existing-code live fixture admitted and deployed.
- [ ] Task 0.2: Existing-code automatic traffic/lifecycle/cleanup proof.
- [x] Task 1.0: Native Fleet/AFD Gateway API architecture selected.
- [ ] Task 1.1: TLS boundaries confirmed.
- [ ] Task 1.2: Frontend certificate/API contract confirmed.
- [ ] Task 1.3: HTTPS regressions written and expected-red verified.
- [ ] Task 1.4: Feasible faithful native path translation demonstrated.
- [ ] Task 2.1: TLS intent/private origin identity carried.
- [ ] Task 2.2: Supported TLS references and failure behavior.
- [ ] Task 2.3: Multi-backend native route compiler/generated capacity.
- [ ] Task 3.1: Exact HTTPS route/origin/probe requests and observations.
- [ ] Task 3.2: Selected owned certificate/domain lifecycle.
- [ ] Task 3.3: Multi-backend inventory/activation/scoped withdrawal lifecycle.
- [ ] Task 4.1: Native two-service/two-member TLS fixture.
- [ ] Task 4.2: Routing/TLS/proof-helper tests passing.
- [ ] Task 4.3: Multi-backend safety/lifecycle/default pacing tests passing.
- [ ] Task 5.1: Customer examples/expectations, accurate guidance and runnable demo.
- [ ] Task 5.2: Affected validation/build checks passing.
- [x] Task 5.3: Separate documentation-only optional ATM proposal and guide link.
- [ ] Task 6.1: Current live access/DNS/certificate/quota approval.
- [ ] Task 6.2: Actual automatic HTTPS delivery/probe proof.
- [ ] Task 6.3: Withdrawal, teardown and verified cloud absence.

Success means the approved TLS contract is implemented and locally validated,
the customer native HTTPRoute paths work through real private HTTPS ingress,
and the owned fixture is independently verified deleted. Offline success alone
is not live proof.

## Decisions

- Native Fleet/AFD HTTPRoute routing is selected. No extra proxy component is
  added; the two separate applications retain two separate ServiceImports.
- The AFD provider runs within the existing hub Gateway controller manager, not
  a separate AFD Deployment. HTTPS/native routing adds no controller/reconciler;
  existing Gateway and member export reconciliation paths are extended.
- Gateway API alignment is a design requirement, not a completed conformance
  result. Path semantics and supported TLS/provider extensions must pass the
  stated feasibility/validation gates before being advertised.
- HTTPS/native implementation is deferred until existing-code live proof.
  A live failure is not permission to introduce these new feature paths.
- Keep one logical backend per HTTPRoute rule while allowing distinct rules
  and routes to select different groups within one Gateway/profile.
- Match semantics are an explicit feasibility gate. Native path support is not
  achieved merely by replacing the single-backend preflight with permissive code.
- HTTPS-only intent does not permit insecure certificate bypass or fallback.
- Frontend and origin certificates are different responsibilities. Azure-managed
  edge certificates do not automatically supply private-origin certificates.
- AFD requires trusted backend chains matching the configured origin hostname;
  internal CA/self-signed origins are not a valid live substitute.
- AFD warns against different ports sharing one Private Link target within one
  profile. Separate application PLS targets using the same HTTPS port are valid;
  different-port reuse of one target remains explicitly rejected.
- Frontend uses Azure-managed custom-domain certificates; frontend Key Vault
  BYOC is outside this scope. Live proof must exercise the customer domain as
  well as the generated hostname, not infer domain/certificate success from it.
- TLS coverage, the exact API contract and implementation approval remain pending.
- Customer ATM opt-in is confirmed and defaults off; the annotation/policy API
  and fallback implementation are not yet approved or supported. AFD-only
  customers retain the base requirements and selected future managed-certificate
  strategy. Optional resilience is not a prerequisite for ordinary AFD readiness;
  do not advertise failover readiness for invalid or incomplete opt-in settings.
- The ATM enhancement is a separate documentation-only proposal in
  `docs/design/gep-1748-optional-atm-resilience.md`. Its failure-domain, private
  network, certificate, API and operational questions do not block this plan.
  No ATM controller/API/cloud changes or failover tests are authorized.

## Implementation Details

- Existing listener validation rejects HTTPS and TLS configuration.
- Current normalized Listener has no TLS representation. Backend discovery
  exposes internal IP/PLS identity, not a certificate-matching origin hostname.
- Current graph creates one catch-all HTTP route and HTTP-only forwarding/probes.
- The current backend planner already identifies groups by ServiceImport/port.
  Execution inventory and withdrawal still assume one group/route and must be
  updated alongside the new route compiler.
- Proposed live flow: client HTTPS -> AFD native route selection -> Private Link
  -> catalog or checkout TLS application in the selected member.
- Only planning documentation has changed; no implementation or live resources.

## Changes Made

- Created this combined HTTPS and private path-routing demonstration plan.
- Referred to the current implementation and Microsoft's TLS/Private Link rules.
- Replaced the proxy workaround with the chosen native routing architecture and
  renamed this breadcrumb. No proxy, production code or live resource was added.
- Expanded the documentation-commit requirements with customer manifests,
  request/response examples, ownership boundaries, limits and lifecycle expectations.
- Recorded the optional ATM composition question, existing API limitations and
  private-network/certificate/failure-domain decisions without advertising support.
- Confirmed ATM as customer opt-in/default-off, with no additional requirements
  for AFD-only users; added the customer-facing distinction to the main how-to.
- Separated the ATM enhancement into its own design document and removed its
  design decisions from native HTTPS implementation prerequisites.
- Deferred HTTPS/native routing behind the existing HTTP live test, retaining
  this plan for later without implementing any of its feature changes.

## Before/After Comparison

- Before: HTTP-only private ingress, one catch-all backend, no supported
  certificate/domain lifecycle or native multi-backend HTTPS path routing.
- Planned after: explicitly validated HTTPS ingress and a repeatable one-Gateway,
  two-ServiceImport demonstration serving two applications through native
  HTTPRoute rules, with truthful status and verified cleanup.

## References

- `2026-11-01-0611-afd-activation-without-projection-gates.md`: existing approved,
  implemented ownership/configuration/lifecycle behavior to preserve.
- `2026-11-01-1822-afd-standard-live-e2e.md`: expired-access live-run blocker.
- `docs/howtos/gateway-api-afd-configuration.md`: current HTTP-only product scope.
- `pkg/controllers/hub/gatewayresolver/route.go:55-59`: listener/TLS rejection.
- `pkg/controllers/hub/gatewaymodel/model.go:38-43`: current listener model.
- `pkg/providers/azure/frontdoor/graph.go:45-123`: current catch-all execution graph.
- `pkg/providers/azure/frontdoor/backend.go:20-95`: backend grouping and Private Link port conflict.
- https://learn.microsoft.com/azure/frontdoor/end-to-end-tls#supported-certificates:
  trusted certificate chains, origin hostname verification, no internal/self-signed CA.
- https://learn.microsoft.com/azure/frontdoor/domain#https-for-custom-domains:
  Azure-managed/custom certificate lifecycle and DNS requirements.
- https://learn.microsoft.com/azure/frontdoor/private-link#association-of-a-private-endpoint-with-an-azure-front-door-profile:
  shared private endpoints and different-port limitation.
- `docs/design/gep-1748-optional-atm-resilience.md`: separate optional enhancement,
  documentation only; no native HTTPS implementation dependency.
- Domain-knowledge/specification directories are absent; no applicable version.
