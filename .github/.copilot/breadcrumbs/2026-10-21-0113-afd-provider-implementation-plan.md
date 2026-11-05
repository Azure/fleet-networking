# AFD Control-Plane Automation Implementation Plan

## Requirements

- Plan the controller changes that configure and maintain Azure's existing
  AFD -> Private Link -> PLS -> internal LB -> application datapath.
- Build on the committed private-origin and Gateway control-plane implementation.
- Cover AFD resource mapping, ownership, asynchronous approval observation,
  safe changes/revocation and cleanup, followed by integration/install evidence.
- Keep Premium/private-only ingress, the approved probe/Host policy and existing
  repository patterns. No customer ServiceImport settings or new forwarding agent.
- Code implementation is authorized by the subsequent go-ahead; live Azure
  provisioning still requires separate authorization.

## Additional comments from user

- User clarified that AFD plus Private Link/PLS and ILB provide the datapath.
- Confirmed that the missing implementation is Azure configuration/observation,
  not another Fleet traffic-forwarding component.
- User requested: "ok, plan those changes".
- User reviewed the proposal and selected "Approve plan; wait to implement".
- User subsequently requested "go ahead"; implementation started at P1.
- Earlier direction remains: "keep the implementation inline with existing repo choices".

## Plan

The canonical detailed plan is
`docs/design/gep-1748-implementation-plan.md`, section
**Next implementation: AFD control-plane automation**. All implementation tasks
start unchecked; this breadcrumb records the proposal and approval boundary.

### Phase 1: Prove mapping and preflight

- [ ] Task 1.1: Execute canonical P1.1-P1.3, tests first.
  - Success criteria: deterministic, semantically faithful HTTP resource mapping;
    exact generated-resource counts; unsafe shared-endpoint multi-port intent
    rejected before Azure writes. Unproven capabilities remain gated for review.

### Phase 2: Establish provider and ownership

- [ ] Task 2.1: Execute canonical P2.1-P2.3, ownership tests/fakes first.
  - Success criteria: minimal typed provider boundary, UID-derived ownership,
    finalizer persisted before mutation and no foreign adoption/deletion.

### Phase 3: Program private AFD resources

- [ ] Task 3.1: Execute canonical P3.1-P3.3, client/operation tests first.
  - Success criteria: existing Azure identity/options/rate limits reused;
    dependency-ordered, bounded, restart-safe private-only reconciliation.

### Phase 4: Observe approval and publish truthful status

- [ ] Task 4.1: Execute canonical P4.1-P4.3, state-transition tests first.
  - Success criteria: correlate and deduplicate actual AFD connection requests;
    manual approval plus observed convergence advances status without Kubernetes
    edits. Status conflict retries never repeat external mutations.

### Phase 5: Maintain, withdraw and clean up

- [ ] Task 5.1: Execute canonical P5.1-P5.3, lifecycle/revocation tests first.
  - Success criteria: revoke confirmed invalid bindings even if another backend
    is unresolved; no unsafe routing fallback; shared resources remain for
    authorized users and finalizers clear only after observed owned deletion.

### Phase 6: Integrate and prove the existing Azure datapath

- [ ] Task 6.1: Execute canonical P6.1-P6.3.
  - Success criteria: deterministic controller integration and minimal install
    wiring, then separately authorized manual-approval/private HTTP evidence.

### Detailed checklist

- [ ] Phase 1 / Task 1.1 complete.
- [ ] Phase 2 / Task 2.1 complete.
- [ ] Phase 3 / Task 3.1 complete.
- [ ] Phase 4 / Task 4.1 complete.
- [ ] Phase 5 / Task 5.1 complete.
- [ ] Phase 6 / Task 6.1 complete.

**Overall success criteria:** Kubernetes intent automatically configures and
maintains the Azure-provided private-origin HTTP path after authorized manual
approval. No Fleet proxy, public origin fallback or ServiceImport annotation is
introduced. Approval of the plan authorizes code work only; live Azure operations
need separate authorization.

## Decisions

- Extend the existing approved implementation plan rather than create a competing
  design or redefine the datapath.
- Keep WAF association, HTTPS/certificates, approval automation and production/scale
  certification outside this next HTTP-preview automation milestone.
- Explicitly gate unsupported declared security/routing intent; never silently
  program an unprotected or semantically different alternative.
- Use existing controller-runtime, Go structs, Azure auth/options, retry/rate-limit,
  UID-based naming and test conventions; no generic orchestration framework.
- Approved shared-endpoint policy: reject different origin ports for the same
  profile/PLS/group/Private Link region tuple. Separate origin groups do not
  bypass the documented AFD limitation. The subsequent go-ahead lifts the
  implementation hold, not the live Azure authorization boundary.
- Approval observation is per correlated AFD/shared-endpoint identity, not one
  arbitrary PLS connection or one independent approval per origin.
- Separate provider operations from optimistic-lock status retry callbacks.
  Recheck dependency-derived intent, not only Gateway generation.
- Confirmed authorization/source withdrawal has priority over normal apply
  readiness; failures to enforce are explicit, not claimed as completed revocation.

## Implementation Details

- The initial planning task changed no Go code or dependencies.
- At initial planning, runtime discarded loaded Azure config and had no provider
  injection. Enabled startup now retains that configuration and injects the owned
  cleanup client and uncached API reader. Gateway model evaluation still runs
  within status conflict retries; ARM cleanup stays outside those callbacks.
- The existing backend capacity helper is necessary but not sufficient for
  generated AFD route/rule/domain or shared-endpoint safety preflight.
- The detailed plan identifies the proposed provider package, lifecycle/state
  boundaries, negative-case tests and authorization gates before implementation.

### P1 implementation slice: private backend planning

- Start with failing tests for a pure backend resource inventory under the planned
  frontdoor package: UID-derived names, deterministic/deduplicated origin groups
  and origins, fixed HTTP/probe/Host settings, deep-copy isolation and exact
  backend capacity boundaries.
- Canonicalize ARM resource IDs/locations on copied intent, deduplicate shared
  PLS targets within one profile, retain each referring origin, and reject
  different HTTP ports for one shared target across services/groups.
- Wire this safety preflight into the existing Gateway evaluation without adding
  an Azure client or changing `Programmed=False`.
- This is a backend inventory, not a complete or apply-ready AFD graph. Keep
  route/rule/domain lowering and related capacity tasks open. Documentation proves
  direct path patterns and rule-set overrides exist, but does not establish the
  full promised case/hostname/listener-isolation/no-fallback behavior.
- Keep PLS target group-ID/region and SDK serialization verification as explicit
  gates; do not invent settings from App Service or Application Gateway examples.
- Success criteria: tests cover actual inventory shape and shared-target rejection,
  runtime diagnostics expose unsafe intent, existing behavior is preserved, and
  no source/configuration mutation or Azure operation occurs.

### Completed backend-only slice

- Added `pkg/providers/azure/frontdoor/backend.go` with copied/canonicalized
  private backend intent, deterministic UID-derived resource names, deduplicated
  origin groups, member weights and all shared-target origin references.
- Reused the existing backend capacity helper. Shared PLS/region targets with
  different HTTP ports return a typed conflict and a zero plan.
- Gateway evaluation now runs this pure safety check. Acceptance/reference policy
  is unchanged, no Azure operation occurs, and `Programmed` remains false.
- Tests first failed for the missing planner API and then for the missing Gateway
  conflict diagnostic. After implementation, focused tests passed, including
  case normalization, source/result isolation, naming/replacement, capacity
  boundaries, status idempotence and clearing withdrawn conflict diagnostics.
- Validation passed:
  - `go test -count=1 .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  - `go vet .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  - `go run golang.org/x/lint/golint@latest -set_exit_status .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway`
  - `go build .\cmd\...`
- Full P1 remains unchecked: route/domain/rule lowering, complete resource
  accounting and the API/platform proof gates are not implemented. No live Azure
  or envtest validation was performed. User subsequently redirected work to
  reviewing all branch changes and adding concise clarifying comments.

### Next slice: stable SDK requests and controller E2E coverage

- User requested continued implementation and updated E2E coverage, then
  reaffirmed implementation after clarifying that Azure resource reconciliation
  follows the Traffic Manager patterns already present on `main`.
- Continue P1 SDK request/capability fixtures using `armcdn/v2 v2.2.0`, whose AFD
  clients target stable API `2024-02-01`. Add only this dependency, run tidy and
  keep the existing Azure auth/options/client setup choices for the later
  operational provider.
- Tests first: validate actual SDK JSON for origin groups and origins, the fixed
  HTTP probe, selected port, member weights, incoming-Host policy, certificate
  checks and mandatory private target. Reject invalid/conflicting intent before
  returning any requests; do not invent a public-origin fallback.
- The official VM/PLS quickstart explicitly passes an empty private resource type,
  which its module maps to null `groupId`. Use an omitted SDK group ID for custom
  PLS, not App Service's `sites` or Application Gateway's frontend name.
- Keep request construction backend-only and pure. Ownership/finalizers, complete
  routing/domain plans, supported-region/platform verification and Azure writes
  remain open; serialized SDK fixtures are not dataplane proof.
- Update the existing E2E scenario with opt-in checks against an externally
  running Gateway manager: accepted class/Gateway, pending programming without
  Azure addresses, current-generation route reference status, missing-backend
  recovery and grant creation/revocation without editing the route.
- Preserve baseline API-contract E2E when the opt-in is absent. Document how to
  start the current control-plane manager and enable the assertions. No live
  cluster/Azure operation is authorized or executed as part of local validation.
- Success criteria: scoped tests, vet/lint and E2E compilation pass; SDK JSON
  matches the private HTTP contract; controller E2E explicitly distinguishes
  configuration/status evidence from real private traffic.

### Completed SDK request and controller E2E slice

- [x] Task S1: Add failing SDK request fixtures, then implement pure private HTTP
  origin-group/origin request construction. Serialized bodies preserve the
  selected port, member weights, incoming Host and fixed probe, omit custom PLS
  group ID/observed status and never introduce a public target.
- [x] Task S2: Run request preflight during Gateway pending-status evaluation.
  Unsupported declared WAF protection produces an explicit diagnostic rather
  than being silently dropped. No Azure client or mutation was introduced.
- [x] Task S3: Extend the existing E2E scenario with an opt-in controller entry.
  It checks current-generation status, missing-backend recovery and grant
  deletion/recreation without route edits. A table-driven local test verifies
  rejection of missing, stale, wrong-status and wrong-reason conditions.
- [x] Task S4: Document external manager setup, format the changes and complete
  scoped validation. The baseline API entry remains available without the opt-in.
- A misplaced WAF regression-test declaration initially caused formatting to
  fail. Moved it to file scope before rerunning all affected checks successfully.
- Added only `armcdn/v2 v2.2.0`; `go mod tidy` also correctly marks the existing
  `sigs.k8s.io/yaml v1.4.0` dependency direct because API tests already import it.
  A subsequent `go mod tidy -diff` passed with no further changes.
- Validation passed:
  - `go test -count=1 .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  - `go test -tags=e2e -count=1 -run '^TestCurrentGatewayCondition$' .\test\e2e`
  - `go test -tags=e2e -run '^$' .\test\e2e\...` (compile only).
  - `go vet .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  - `go vet -tags=e2e .\test\e2e\...`
  - `go run golang.org/x/lint/golint@latest -set_exit_status .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway`
  - `go run golang.org/x/lint/golint@latest -set_exit_status .\test\e2e\gateway_controlplane_test.go .\test\e2e\gateway_conditions_test.go`
  - `go build .\cmd\...`
- Live E2E/ARM operations were not run. Full P1 remains open: faithful routing
  and domain lowering, exact whole-graph capacity and platform gates for region,
  matching, probe Host and weight eligibility still need implementation/evidence.
  Ownership, actual Azure reconciliation and approval-aware readiness follow
  under the already approved later phases.

### Next slice: root requests and ownership guards

- Continue the approved P2 ownership work independently of unresolved P1 routing
  proof. This slice remains pure: no provider clients, finalizer registration,
  Azure mutation or live validation.
- [x] Task O1: Write failing table-driven tests for required scope/owner identity,
  Premium/global profile request serialization, an initially disabled endpoint,
  UID replacement, SDK request isolation and foreign/colliding root refusal.
- [x] Task O2: Implement minimal ordinary structs/functions for root requests and
  observed profile ownership. Use existing UID-derived names and the repository's
  Azure-safe dotted tag prefix. Match expected subscription/resource group/root
  ID and all controller/hub/Gateway identity tags before permitting later use.
- [x] Task O3: Verify focused tests, vet/lint, manager builds and E2E compilation;
  update design progress without claiming all P2 or lifecycle integration.
- Hub identity will be the trusted hub `kube-system` Namespace UID, stable across
  controller restarts and distinct after hub replacement. This pure boundary
  requires that UID explicitly; reading it from Kubernetes is deferred to runtime
  integration, not replaced by an optional customer annotation/configuration.
- Ownership checks must accept owned configuration drift (including SKU) so
  cleanup is still possible. Missing/ambiguous identity fails closed; a name,
  matching tags in a different ARM scope or request parameters alone are not
  ownership proof. Children remain under the verified owned profile boundary.
- The endpoint request starts Disabled with no requested hostname. Later lifecycle
  code must persist the finalizer and prove the complete graph/approval before
  publishing traffic. These helpers are not write authorization or readiness.

### Completed root-request/ownership slice

- Added `ownership.go` and `ownership_test.go`. Tests first failed for the missing
  ownership API, then passed with the implementation.
- Request fixtures verify actual stable SDK JSON: Premium/global root, independent
  owner tags and a disabled endpoint without requested/read-only hostname fields.
  UID-derived root names agree with the existing backend plan and change for a
  same-name Gateway replacement.
- Ownership checks require the exact expected ARM scope/root and all five owner
  tags. Tests cover foreign hubs/controllers/Gateways, missing/nil/ambiguous tags,
  casing, child/wrong-scope IDs and preservation of observed response objects.
  Owned SKU drift does not prevent ownership verification for later repair/cleanup.
- Missing/invalid expected identity returns zero root requests. Endpoint/profile
  tags and separate builder invocations do not alias each other.
- Validation passed:
  - `go test -count=1 .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  - `go vet .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  - `go run golang.org/x/lint/golint@latest -set_exit_status .\pkg\providers\azure\frontdoor`
  - `go build .\cmd\...`
  - `go test -tags=e2e -run '^$' .\test\e2e\...` (compile only).
- No manager behavior, Kubernetes finalizer or Azure write path changed. Runtime
  hub-identity reads and persisted pre-mutation ownership remain future P2 work;
  full P1/P2 checkboxes intentionally remain open. No live operations were run.

### Next slice: operational owned-profile cleanup

- [x] Task C1: Add failing SDK-transport and Gateway fake-client tests for verified
  root deletion, absent/deleting profiles, rejected operations, class-independent
  cleanup, finalizer preservation and metadata conflicts.
- [x] Task C2: Implement bounded profile GET/BeginDelete using the stable SDK.
  Verify root ownership before every delete submission; discard in-memory pollers
  and resume by observing Azure. An accepted/completed submission alone never
  clears the Gateway finalizer.
- [x] Task C3: Wire enabled-only client initialization using the existing hub
  Azure auth/options/rate-limit pattern. Retain loaded configuration, use the
  uncached hub API reader for kube-system identity, and finalize before class/
  backend validation, outside status conflict retries.
- [x] Task C4: Update manager/cleanup regressions, E2E runbook and design progress;
  run focused tests, vet/lint, builds and compile-only E2E.
- This slice does not create profiles or attach new Gateway finalizers. Normal
  Gateways remain control-plane-only; a Gateway already bearing this controller's
  cleanup finalizer exercises the real, ownership-checked cleanup path. Azure
  create/update and traffic readiness still require complete routing validation.
- SDK operation tests use an in-process fake HTTPS transport only. No Azure or
  live cluster operations are authorized. Cleanup may delete only the verified
  owned profile and its AFD children, never member PLS/LB or external assets.

### CI lint correction before continuing cleanup integration

- User supplied CI failures for goconst, gocyclo, gosec G115, errorlint and
  prealloc. Pause remaining cleanup manager wiring and correct the feature branch
  without disabling linters, adding suppressions or weakening safety checks.
- [x] Task L1: Extract coherent validation/discovery/listener helpers to lower
  function complexity while preserving error priority, origin provenance,
  conflict handling and zero-partial-result behavior.
- [x] Task L2: Reuse semantic constants, bound the SDK weight conversion locally,
  use errors.As for wrapped resolution failures and preallocate indexed keys.
  Split the backend shape test into focused assertions without dropping checks.
- [ ] Task L3: Run the configured golangci-lint version and focused regressions;
  record actual results and any toolchain limitation. Existing cleanup additions
  remain uncommitted and must not be lost or bypass validation.
- User also reported HTTP 429 for the Gateway API source reference. Verified the
  identical version-pinned raw GitHub source successfully and updated both
  references, following the repository's existing raw-link remedy. Link-check
  status-code rules and exclusions are unchanged.
- Completed the reported source corrections. Added regression cases for wrapped
  backend-resolution errors and SDK weight boundaries, including an overflowing
  input. Preserved nil empty index results and sorted per-member error priority.
- The installed pinned golangci-lint v1.64.7 is built with Go 1.24 and refuses
  this module's Go 1.26.9 directive. Rebuilding that same version with local Go
  1.27 still fails because its analyzers cannot decode Go 1.27 export data.
  This is a local verification limitation, not a reason to lower the module's
  Go version or disable repository lint checks.
- A session-only migrated configuration and golangci-lint v2.15.0 provided a
  compatible alternative. Focused errorlint/gosec/gocyclo checks passed with zero
  issues. The exact CI gocyclo v0.6.0 also passed `-over 30` across all affected
  packages. An unrestricted newer-runner check reports additional literals,
  style and allocation findings from changed analyzer behavior; it is not an
  exact substitute for the pinned full CI run. L3 remains open for that CI run.
- Focused unit tests passed for frontdoor, gateway/gatewayclass/gatewaymodel/
  gatewayresolver, httproute, member serviceexport, objectmeta and the Gateway
  manager. The command used `-skip '^TestAPIs$'` because envtest binaries are not
  installed locally. Scoped vet, all manager builds and compile-only E2E passed.
- The raw Gateway API source replacement returned HTTP 200. No workflow, lint
  configuration, exclusions or dependency versions changed.
- At the CI-correction checkpoint, cleanup provider/Gateway finalization tests
  passed but manager injection and enabled-only client initialization were paused
  at C3. The completed integration below supersedes that boundary.

### Probe-path default clarification

- User requested making `/` a constant assigned to a variable for possible future
  platform ConfigMap configuration.
- Add a named default and a local path variable in `HTTPPreviewHealthProbe`.
  Keep the existing GET/HTTP/30-second behavior and no customer ServiceImport
  settings. A local variable avoids introducing shared mutable policy state.
- Existing serialized SDK request fixtures must continue to assert the actual
  default `/`; this change does not implement ConfigMap reads or alter routing.
- Implemented `defaultHTTPPreviewHealthProbePath` and the local `probePath`.
  Model/provider tests and vet passed after the change; SDK fixtures still
  require the serialized path to be `/`.

### Resuming cleanup manager integration

- User committed/pushed the cleanup and CI-correction slice, then requested
  continued implementation. Confirmed remote/local heads match.
- Resume C3 with manager regressions first: enabled startup must retain and pass
  the loaded cloud configuration, initialize the provider exactly once, inject
  the same provider into controller registration and fail explicitly on
  initialization failure. Disabled mode must do none of these.
- Initialize only the required Profiles SDK client using the existing
  `azclient.NewAuthProvider`, cloud options, user agent, backoff and rate-limit
  policy. Inject `mgr.GetAPIReader()` for trusted hub identity/finalizer reads.
- Update the runbook/current-boundary documentation: valid Azure auth settings
  are now needed for enabled startup. The runtime can clean up a verified owned
  finalized profile, but still creates no AFD resources or new finalizers.
- Validate scoped tests/vet/lint, manager builds and E2E compilation. Keep the
  pinned full-lint/toolchain limitation and unresolved P1 gates explicit.

### Completed cleanup manager integration

- Manager regression tests first failed for the missing provider factory and
  registration dependency. Enabled startup now retains the loaded configuration,
  initializes one cleanup provider and passes it to Gateway registration together
  with `mgr.GetAPIReader()`. Disabled startup initializes neither provider nor
  Gateway controllers. Initialization errors and missing providers stop startup.
- Added `cmd/hub-gateway-controller-manager/azure.go`, reusing the existing hub
  auth provider, configured cloud/user agent, backoff and rate-limit policy.
  Managed-identity/SDK construction performs no ARM request; successful
  construction is not evidence of token acquisition or resource permissions.
- Updated flag help, the E2E runbook and design progress to distinguish active
  owned-profile cleanup from still-unimplemented Azure creation/update. No normal
  Gateway gains an Azure finalizer, address or `Programmed=True`.
- Validation passed:
  - `go test -count=1 -skip '^TestAPIs$' .\cmd\hub-gateway-controller-manager .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute`
  - `go vet .\cmd\hub-gateway-controller-manager .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute`
  - `go build .\cmd\...`
  - `go test -tags=e2e -run '^$' .\test\e2e\...`
  - `go run golang.org/x/lint/golint@latest -set_exit_status .\cmd\hub-gateway-controller-manager`
  - `go run github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0 -over 30 .\cmd\hub-gateway-controller-manager`
  - `git diff --check`
- Exact full pinned CI lint remains blocked by the previously documented local
  toolchain incompatibility. No live Azure, cluster, informer or traffic
  validation was performed. Full P1-P6 phases remain open.

### Next slice: correlated private-origin approval observation

- User pushed the cleanup integration and requested continued implementation;
  remote/local heads match. Continue the approved P4 tests-first work without
  bypassing the incomplete P1 routing/platform gates.
- [x] Task O1: Add pure observed-SDK fixtures for missing origins, Pending,
  Approved while provisioning/deploying, Ready, Rejected, Disconnected, Timeout,
  failed and unknown states. Verify foreign profile/origin identities and target
  drift never count as ready; unrelated approved origins cannot satisfy intent.
- [x] Task O2: Implement a deterministic observation function using the existing
  backend planner and root ownership proof. Compare each expected origin's full
  ARM ID, private target/region/group and desired configuration before treating
  approval and provisioning/deployment as converged. Deduplicate pending manual
  approval targets across origin groups; never auto-approve or infer approval
  from member discovery. Return zero observation on invalid intent/identity.
- [x] Task O3: Document the evidence boundary and validate focused tests, vet,
  lint, manager builds and E2E compilation. These pure snapshots perform no ARM
  operations and do not yet wire Azure polling or change Gateway status.
- Success criteria: every expected origin must independently match and converge;
  empty/partial/unknown snapshots cannot imply readiness. Results must not alias
  input, and shared-target diagnostics must be deterministic. Full profile/group/
  route/domain readiness, origin health and traffic proof remain separate gates.
- Implemented `ObservePrivateOrigins` with ownership proof, expected child ARM
  identity checks and desired-origin drift detection. The stable SDK's actual
  approval, provisioning and deployment enums drive explicit origin states.
  An origin needs Approved plus Succeeded provisioning/deployment and matching
  configuration; missing/unknown values never count as ready.
- Pending manual-approval instructions are deduplicated by the existing
  profile-scoped PLS/region plans. Every referring origin still converges
  independently, including when shared-target snapshots disagree temporarily.
- Initial tests failed for the missing observation API; provider tests, vet,
  golint and the exact complexity analyzer passed after implementation. Added
  shared-target mixed-state and approval-without-intent-edit regressions before
  final integration validation.
- Final validation passed:
  - `go test -count=1 -skip '^TestAPIs$' .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  - `go vet .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  - `go run golang.org/x/lint/golint@latest -set_exit_status .\pkg\providers\azure\frontdoor`
  - `go run github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0 -over 30 .\pkg\providers\azure\frontdoor`
  - `go build .\cmd\...`
  - `go test -tags=e2e -run '^$' .\test\e2e\...`
  - `git diff --check`
- SDK evidence: local `go doc` for stable armcdn/v2 v2.2.0
  `AFDOriginProperties`, `SharedPrivateLinkResourceStatus`,
  `AfdProvisioningState` and `DeploymentStatus`; tests also round-trip an
  observed origin through the SDK's JSON deserializer. This is not live Azure
  evidence. No Azure operations, runtime polling/status changes, dependency
  changes or live informer/traffic validation were introduced. The previous
  exact full-CI-lint toolchain limitation remains open.

### Next slice: bounded read-only SDK origin observation

- User requested continued implementation after committing pure observation.
  Continue approved P3/P4 client work; do not bypass P1 before resource creation.
- [x] Task R1: Add fake HTTPS SDK-transport regressions first for profile
  ownership before child reads, paginated origins, missing roots/groups,
  duplicate/malformed responses, 403/409/429, cancellation and bounded deadlines.
- [x] Task R2: Add a narrow read-only observer with the existing stable Profiles
  and AFDOrigins clients. Validate owner/intent before ARM, verify the observed
  profile before listing expected groups, then feed correlated results to the
  pure evaluator. Use one 30-second operation budget and retain no pager state.
  A missing root is explicit; a group disappearing mid-pagination discards its
  partial snapshot. All other failures return errors and zero observation.
- [x] Task R3: Update design progress and validate scoped tests, vet/lint,
  manager builds and compile-only E2E.
- Success criteria: only GETs, no foreign-parent child reads, all pages required
  for convergence, no partial success after an API failure, and restart-safe
  repeated observations. This adds a callable SDK operation, not a scheduled
  controller polling loop, resource creation or Gateway readiness integration.
- Added `reader.go` with scope-bound stable SDK clients and explicit
  `ProfileMissing` observation. Expected child ARM IDs are verified before
  evaluation, duplicate identities fail explicitly, and unrelated named origins
  cannot satisfy intent. Each call starts from a fresh root read and pager.
- Tests initially failed for the missing observer API. On resumption after an
  interrupted command, cancellation regression exposed an SDK transport call
  despite an already-canceled context. Added explicit checks before observation,
  between pages and before returning evaluated state; cancellation retains zero
  observation and its typed cause.
- Added regressions for caller deadline expiration, cancellation after root and
  between pages, one shared deadline across requests, multiple expected groups,
  and approval changes across repeated reads without desired-intent edits.
  Provider tests, golint and the exact complexity analyzer pass.
- Updated both canonical design/progress documents to distinguish the callable
  read-only SDK operation from still-missing manager injection, scheduled
  controller polling and readiness integration.
- Final validation passed:
  - `go test -count=1 -skip '^TestAPIs$' .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  - `go vet .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  - `go run golang.org/x/lint/golint@latest -set_exit_status .\pkg\providers\azure\frontdoor`
  - `go run github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0 -over 30 .\pkg\providers\azure\frontdoor`
  - `go build .\cmd\...`
  - `go test -tags=e2e -run '^$' .\test\e2e\...`
  - `git diff --check`
- Stable SDK pagination API verified via local `go doc` for
  `AFDOriginsClient.NewListByOriginGroupPager` and `AFDOriginListResult`;
  operation fixtures exercise the actual SDK with fake HTTPS transport.
  No live Azure/cluster operations or dependency changes occurred. Full pinned
  CI lint remains blocked by the previously recorded toolchain incompatibility.

### Next slice: runtime private-origin observation and rechecks

- Continue approved P4 runtime integration after the bounded SDK observer.
- [x] Task T1: Add controller tests first for asynchronous approval changes,
  missing/failed observation, current-intent guards, status conflicts and
  invalid/foreign/deleting/empty Gateway skips.
- [x] Task T2: Resolve a complete private backend model before a read-only
  observation outside status retry callbacks. Read trusted hub identity through
  the uncached reader. Re-resolve dependencies before applying diagnostics;
  discard stale observations when UID/generation or normalized intent changes.
  Recheck on a bounded jittered interval without requiring Kubernetes edits.
- [x] Task T3: Initialize/inject the observer only in enabled startup using the
  same Azure credential/options as cleanup; preserve explicit init failures.
  Update manager regressions and current-boundary documentation.
- [x] Task T4: Run focused tests, vet/lint, manager builds and E2E compilation.
- Success criteria: one observation per reconcile despite status conflicts;
  no observation of unresolved/unauthorized subsets; approved/converged origins
  never imply a complete programmed graph or publish addresses. Missing roots
  remain explicit, failures return retryable errors, and cleanup still precedes
  class/backend validation. No creation or new finalizer is introduced.
- Added narrow controller observation interface and separate read/status phases.
  `resolve` now also returns the complete normalized backend intent when
  preflight succeeds. Enabled reconciliation reads trusted hub identity
  uncached, calls the observer once and then re-resolves before status patches.
  UID/generation/intent mismatch discards old diagnostics. Polling uses the
  existing Kubernetes jitter helper with a 30-second interval and 20% jitter.
- Pending diagnostics distinguish missing roots, asynchronous manual approval,
  failed/disconnected/unknown origins and approved/deployed origins without
  claiming complete programming. API failures patch current pending diagnostics
  when applicable and return errors for normal controller backoff.
- Enabled manager initialization now constructs cleanup and observation using
  one auth provider/options/rate-limit configuration; missing either dependency
  fails explicitly. Disabled startup makes no provider/controller initialization
  or ARM call. SDK construction itself is still lazy, not authentication proof.
- Regression-driven course corrections: Kubernetes semantic equality panics on
  the normalized model's plain `time.Time` fields, so current normalized intent
  uses standard-library strict `reflect.DeepEqual`. A misplaced appended test
  block was moved back to package scope before completing validation.
- Controller/provider/manager tests, golint and exact complexity checks pass,
  including approval changes without intent edits, one call across status
  conflict retries, route withdrawal, member changes without Gateway generation
  changes, uncached hub identity and Gateway UID/generation guards.
- Updated design/progress and E2E runbook for enabled profile/origin reads and
  required permissions. Synthetic E2E exports still do not supply ready private
  intent, so that scenario does not trigger ARM observations or prove approval.
- Validation passed:
  - `go test -count=1 -skip '^TestAPIs$' .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  - `go vet .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  - `go run golang.org/x/lint/golint@latest -set_exit_status .\pkg\controllers\hub\gateway .\cmd\hub-gateway-controller-manager`
  - `go run github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0 -over 30 .\pkg\controllers\hub\gateway .\cmd\hub-gateway-controller-manager`
  - `go build .\cmd\...`
  - `go test -tags=e2e -run '^$' .\test\e2e\...`
  - `git diff --check`
- No live Azure/cluster/informer/traffic validation or dependency changes.
  Exact full pinned lint remains subject to the recorded toolchain blocker.
  Full P1-P6 milestones remain open: observation integration does not implement
  the full routing/domain graph, resource creation, approval automation,
  full-graph readiness, deployment packaging or dataplane proof.

### Routing preflight review before the next implementation slice

- User requested continued implementation after the runtime-observation commit.
  Returned to the approved P1 routing gate before adding Azure creates.
- Verified the stable SDK route/override/path-condition fields and current
  Microsoft Learn route-matching and rule-action documentation. AFD native path
  matching does not establish Gateway API case-sensitive path semantics; the
  endpoint creation documentation explicitly describes patterns as case
  insensitive. Rule overrides can select an origin group, but documentation
  does not establish a fail-closed unmatched branch for the proposed full
  catch-all/rule mapping.
- A catch-all route with a real default origin could forward requests that fail
  the case-sensitive rule conditions. Do not introduce that approximation or
  claim that omitting a route origin group is a proven supported workaround.
- A narrower first lowering milestone could use one unconstrained HTTP/80
  listener, root PathPrefix `/`, one logical ServiceImport port and no custom
  domains, WAF or filters. This is only a proposed execution subset, not an
  approved change to the existing Exact/PathPrefix/hostname contract, and not
  proof that region, probe Host or member-weight gates are resolved.
- Ask for scope direction before implementing a narrowed execution subset.
  No Go code or behavior changed in this review.
- References:
  - https://learn.microsoft.com/azure/frontdoor/front-door-route-matching
    (native route precedence and unmatched handling).
  - https://learn.microsoft.com/azure/frontdoor/how-to-configure-endpoints
    (case-insensitive patterns and route origin-group configuration).
  - https://learn.microsoft.com/azure/frontdoor/front-door-rules-engine-actions
    (origin-group overrides versus the route's default).

### Approved gated root-path lowering slice

- User selected "Implement the gated root-path subset" after reviewing the
  routing limitations. This authorizes the narrower execution subset, not
  approximation of case-sensitive paths or live Azure operations.
- [x] Task G1: Add tests first for a complete pure SDK graph: one Premium
  profile, disabled endpoint, one private origin group, all selected member
  origins and a disabled HTTP route using `/*` on the default AFD hostname.
  Assert actual serialized route fields and exact generated-resource counts.
- [x] Task G2: Add root-path execution preflight and graph construction using
  existing root/backend request builders. Require one unrestricted HTTP/80
  listener, only unconstrained root PathPrefix matches, one logical backend
  across all rules and no filters/WAF. Equivalent duplicate bindings may
  deduplicate; empty intent must not create a graph.
- [x] Task G3: Surface execution-subset refusal as a pending Gateway diagnostic
  without changing reference validity/acceptance, adding finalizers, publishing
  addresses or setting Programmed=True. Preserve empty-Gateway handling.
- [x] Task G4: Update approved scope in canonical docs/runbook and validate
  focused tests, vet/lint, builds and compile-only E2E.
- Success criteria: deterministic fresh requests, full ARM references and
  route/backend UID identities; route/endpoint stay disabled; unsupported intent
  returns zero graph, never a partial or wider fallback. The graph is still not
  write authorization: region, probe Host/member-weight platform gates,
  persisted finalizer and dependency-ordered operations remain outstanding.
- Graph fixtures initially failed for missing construction/preflight APIs.
  Added `graph.go` using existing owner/root/backend validation and fresh SDK
  requests. Serialized route fixtures assert HTTP-only forwarding, explicit
  disabled endpoint/route, default-domain association, `/*`, full origin-group
  ARM ID and no caching/redirect/rewrite/rules/custom-domain settings.
- Tests cover one/40/41 member origins, equivalent binding deduplication,
  deterministic identity on withdrawal, Gateway recreation, zero graphs on
  invalid owner/empty/unsupported intent and isolation between invocations.
- Gateway diagnostic tests first failed by reaching observation for unsupported
  paths/hosts. Combined backend and routing preflight now rejects those models
  before observation, preserves accepted/reference semantics and makes no Azure
  call. Existing backend-capacity/shared-target safety tests still pass.
- Updated design, detailed implementation progress, E2E runbook and how-to
  warning: hostname-based examples remain future contract, not an executable
  feature claim. No broader routing milestone is marked complete.
- First-party grounding: current Learn route matching, endpoint configuration
  and rule-action documentation plus the ARM route example at
  https://learn.microsoft.com/azure/frontdoor/create-front-door-template
  and local stable SDK route/protocol/redirect/default-domain types. HTTP-only
  and disabled settings follow the approved preview policy, not the sample's
  HTTPS defaults. No App Service public-origin configuration was copied.
- Validation passed:
  - `go test -count=1 -skip '^TestAPIs$' .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  - `go vet .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  - `go run golang.org/x/lint/golint@latest -set_exit_status .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway`
  - `go run github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0 -over 30 .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway`
  - `go build .\cmd\...`
  - `go test -tags=e2e -run '^$' .\test\e2e\...`
  - `git diff --check`
- No live Azure/cluster/informer/traffic validation, resource creation,
  new finalizers, dependency changes or `Programmed=True`. Exact full pinned
  lint retains the documented toolchain limitation. Operational graph apply
  and the remaining platform proof gates are still open.

### Next slice: bounded disabled-root provisioning operations

- User requested continuation after committing gated root-path lowering.
- [x] Task A1: Add actual SDK fake-transport tests first for profile-before-
  endpoint ordering, initial disabled bodies, ownership refusal, restart/no-op,
  absent/creating/updating/deleting/failed/unknown resources, SDK errors and
  cancellation under one operation budget.
- [x] Task A2: Add a narrow scope-bound root provisioner. Require a complete
  supported graph and matching owner before requests. GET the profile before
  create/child access; never adopt an observed foreign root. Wait for observed
  Premium/global Active/Succeeded root before endpoint operations. Create the
  endpoint disabled, preserve a returned Azure hostname only after observed
  disabled endpoint convergence, and explicitly disable owned enabled drift.
  Discard LRO pollers and require later GET evidence, not accepted submissions.
- [x] Task A3: Document the precise operational boundary and validate focused
  tests, vet/lint, builds and compile-only E2E.
- Success criteria: actual stable SDK GET/PUT/PATCH requests tested without live
  Azure calls, bounded/restart-safe operations and no child mutation before
  observed root ownership/readiness. This provider is not injected or invoked by
  the manager yet. Persisted pre-create finalizer, remaining platform gates,
  concurrent external-writer protection and full graph apply remain prerequisites
  before production creation is enabled; no boolean authorization bypass is added.

#### Disabled-root implementation evidence

- Added `provision_test.go` before `provision.go`; initial tests failed for the
  missing APIs. After implementation, exact-body assertions exposed a test
  fixture alias between desired endpoint properties and observed read-only
  fields. Isolated those properties; no production behavior was relaxed.
- Implemented scope-bound stable SDK profile GET/BeginCreate and endpoint
  GET/BeginCreate/BeginUpdate. Profile ownership and Premium/global
  Active/Succeeded convergence precede all child access. Unknown/failed states
  and SKU/location drift return explicit errors; accepted writes remain pending.
- Tests cover 201/202 submissions, full lifecycle re-observation/no-op, malformed
  children, 403/409/429 typed errors, unsupported/empty/mismatched preflight,
  absent clients and cancellation before profile creation, child reads,
  endpoint creation and disabling. Already-expired deadlines make no request.
  One shared 30-second budget spans profile and child SDK operations.
- Focused provider tests and exact `gocyclo v0.6.0 -over 30` pass. Updated design,
  delivery progress and E2E runbook without changing production manager wiring,
  status, addresses or finalizer behavior.
- A3 validation passed:
  - `go test -count=1 -skip '^TestAPIs$' .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  - `go vet .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  - `go run golang.org/x/lint/golint@latest -set_exit_status .\pkg\providers\azure\frontdoor`
  - `go run github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0 -over 30 .\pkg\providers\azure\frontdoor`
  - `go build .\cmd\...`
  - `go test -tags=e2e -run '^$' .\test\e2e\...`
  - `git diff --check`
- No live Azure, cluster, envtest/informer or traffic validation was performed.
  The previously documented full pinned golangci-lint toolchain limitation
  remains separate from the passing focused checks. No dependencies changed.
  Remaining graph apply, finalizer/platform integration and concurrent-writer
  policy remain open; these staging operations do not complete P2/P3.

### Next slice: stage desired private backends under disabled roots

- Continuation authorized after the disabled-root provisioning commit.
- [x] Task B1: Write stable SDK fake-transport tests first for root ownership and
  observed-disabled endpoint prerequisites, group-before-origin ordering,
  missing/drifted/in-flight resources, exact private request bodies, one write
  per invocation, approval-pending origins not starving later missing origins,
  restart/no-op, SDK failures and shared-budget cancellation.
- [x] Task B2: Add a callable backend provisioner without manager integration.
  Validate the complete approved graph before ARM access. Observe (never create)
  roots; require owned Premium/global Active/Succeeded profile and converged
  disabled endpoint. Stage the expected group and private origins through
  bounded SDK operations, discard pollers and require later observations.
  Group provisioning must succeed before origin writes; group deployment is a
  separate convergence condition, not an invented child-creation dependency.
  Refuse foreign identities and public/different-PLS origin adoption. Reuse
  correlated approval observation; never approve connections or enable traffic.
- [x] Task B3: Update boundary documentation and run focused tests, vet/lint,
  manager builds and compile-only E2E.
- Success criteria: exact private GET/PUT/PATCH requests and dependency ordering
  verified without live Azure; at most one submitted write under one shared
  deadline, no writes until disabled-root prerequisites are observed, and no
  successful whole-graph readiness claim. Existing external-writer, finalizer,
  platform, route apply, revocation and stale-resource cleanup gates remain.

#### Private-backend staging implementation evidence

- Tests initially failed for missing backend provisioning types/APIs. Added
  `backend_provision.go` and `backend_provision_test.go`, reusing root ownership,
  disabled-endpoint verification and pure correlated origin observation.
- The backend provisioner validates the complete gated graph before ARM requests,
  reads roots without mutating them and stages at most one group/origin write.
  Group provisioning gates child writes; group deployment NotStarted/InProgress
  permits origin creation but never expected-backend convergence.
- Added a shared private-target comparison used by observation and staging.
  Full child ARM identity is required before drift updates. Public/Azure-origin
  adoption, a changed PLS/region/group ID and failed/unknown states fail explicitly.
  Group updates include only controlled probe/affinity fields, not unconfigured
  service-returned load-balancing defaults. Origin updates carry private intent,
  never approval status or a public fallback.
- Tests cover 201/202 create and 200/202 update submissions, exact bodies,
  root/group/origin ordering, pending/in-flight origins not starving later
  missing origins, rejected/disconnected/unknown approval without writes,
  restart/re-observation/no-op, typed 403/409/429 read/write errors, invalid
  preflight, expired deadlines and cancellation before/after writes.
- Corrected a nested test declaration introduced while extending the test file.
  Focused provider tests, golint and exact gocyclo checks pass. Design, delivery
  progress and runbook describe the callable-only boundary; production manager
  behavior is unchanged.
- B3 validation passed:
  - `go test -count=1 -skip '^TestAPIs$' .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  - `go vet .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  - `go run golang.org/x/lint/golint@latest -set_exit_status .\pkg\providers\azure\frontdoor`
  - `go run github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0 -over 30 .\pkg\providers\azure\frontdoor`
  - `go build .\cmd\...`
  - `go test -tags=e2e -run '^$' .\test\e2e\...`
  - `git diff --check`
- No live Azure, cluster, envtest/informer or traffic validation. No dependency
  changes or new finalizers. Full pinned golangci-lint retains the documented
  toolchain blocker. Routing operations/enablement, persisted pre-create ownership,
  concurrent-writer policy, platform proofs, stale-resource cleanup/revocation
  and whole-graph status remain open; P2/P3/P4 are not marked complete.

### Next slice: stage the disabled root-path HTTP route

- Continuation authorized after committing private-backend staging.
- [x] Task C1: Write SDK transport tests first for backend/approval prerequisites,
  exact disabled route creation, full endpoint/route identity, matching semantics,
  disable-only drift correction, asynchronous submission versus fresh observation,
  one shared budget/one write across dependencies, errors, cancellation and restart.
- [x] Task C2: Add a callable route provisioner composing backend staging. A backend
  write ends the invocation; route access requires observed desired backend
  convergence under owned disabled roots. Create the exact disabled HTTP `/*`
  route, disable an otherwise matching enabled route and refuse other routing
  configuration drift explicitly rather than invent unsafe PATCH clearing
  semantics. Require later GET provisioning/deployment convergence.
- [x] Task C3: Document the staging-only boundary and validate provider/controller
  tests, vet/lint, manager builds and compile-only E2E.
- Success criteria: at most one write across backend/route stages, zero route
  access before prerequisite convergence, exact private-only/default-domain
  HTTP configuration and no traffic enablement or whole-graph readiness claim.
  Production wiring, persisted finalizer, concurrent-writer/platform proofs,
  route migration, revocation and stale-resource cleanup remain open.

#### Disabled-route staging implementation evidence

- Added transport tests before the missing route provisioner APIs; the initial
  provider test command failed for undefined route types/functions. Reused the
  backend fake SDK transport through a shared options helper instead of
  duplicating deadline/request/body/error assertions.
- Added `route_provision.go` and `route_provision_test.go`, using the pinned
  stable SDK's `RoutesClient` (not a nonexistent `AFDRoutesClient`). Route
  GET/BeginCreate/BeginUpdate use the full profile/endpoint/route scope.
- Renamed existing internal SDK provisioning/deployment checks to resource-wide
  names for reuse without changing their state handling. Backend staging runs
  first; pending dependencies or any backend write prevent route access.
  Observed expected-backend convergence is a conservative route-stage gate,
  not a claim that disabled-resource deployment semantics are live-proven.
- Exact initially disabled HTTP/default-domain bodies and disable-only PATCHes
  are asserted. Full ARM identity, matching route configuration, 201/202 create,
  200/202 update, no-op/restart, in-flight deployment, manual approval diagnostics,
  typed 403/409/429 failures and cancellation before/after writes are covered.
  Unsupported cached/rewritten/domain/rule/protocol/backend routing drift fails
  explicitly rather than assuming nil PATCH properties remove existing settings.
- Focused provider tests, golint and exact gocyclo checks pass. Design, delivery
  progress and E2E runbook now describe this callable-only stage.
- First-party grounding: stable `armcdn/v2 v2.2.0` route/create/update types and
  current Learn Private Link/ILB documentation and route examples. The ILB guide
  confirms separate manual approval and propagation; neither examples nor
  generic/Q&A deployment-status guidance establish this disabled graph's live
  convergence. No public origin or HTTPS/default sample configuration was copied.
  The existing separately authorized Azure proof gate remains mandatory before
  production integration.
- C3 validation passed:
  - `go test -count=1 -skip '^TestAPIs$' .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  - `go vet .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  - `go run golang.org/x/lint/golint@latest -set_exit_status .\pkg\providers\azure\frontdoor`
  - `go run github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0 -over 30 .\pkg\providers\azure\frontdoor`
  - `go build .\cmd\...`
  - `go test -tags=e2e -run '^$' .\test\e2e\...`
  - `git diff --check`
- No live Azure, cluster, envtest/informer or traffic validation. No dependency
  changes, manager integration, new finalizers, addresses or `Programmed=True`.
  The existing full pinned golangci-lint toolchain blocker is unchanged.
  This completes only callable disabled-route staging, not production P2/P3/P4,
  routing migration/enablement, revocation, cleanup or the Azure datapath proof.

### Next slice: prepare persisted cleanup ownership against uncached intent

- Continuation authorized after committing disabled-route staging.
- [x] Task D1: Write tests first for persisted/observed cleanup finalizers,
  preservation of concurrent metadata, conflicts/failures and zero returned
  ownership on Gateway UID/generation, class, route/grant/backend or trusted hub
  changes. Verify uncached reads do not send cache-only field selectors.
- [x] Task D2: Add a metadata-only preparation helper requiring an available
  cleanup provider, trusted uncached hub identity and matching complete nonempty
  supported intent. Persist the finalizer outside Azure/status retries, re-read
  it and re-resolve current dependencies before returning ownership.
  Preserve the normal indexed resolver; add an explicit unindexed path because
  custom HTTPRoute/export cache indexes are not API-server field selectors.
- [x] Task D3: Document the exact boundary and validate focused resolver/controller/
  provider tests, vet/lint, manager builds and compile-only E2E.
- Success criteria: conflicts retry only Kubernetes work, errors/changed intent
  return no prepared ownership, and foreign metadata survives optimistic patches.
  The helper performs no Azure operations and is not called by normal reconciliation
  yet. Prepared metadata is not platform/concurrent-writer authorization; those
  gates and the eventual mutating controller integration remain mandatory.

#### Ownership preparation implementation evidence

- Added ownership tests before implementation; the initial targeted test command
  failed for the missing helper. Implemented `ownership.go` with optimistic
  finalizer patches and uncached, complete-intent re-observation. No Azure work
  occurs in the helper or metadata conflict callbacks.
- Preserved indexed resolution for normal reconciliation. Added the explicit
  unindexed HTTPRoute resolver and shared route-resolution path so API readers
  do not receive custom HTTPRoute/export cache selectors. Raw lists still apply
  the same source identity, attachment and ReferenceGrant checks; unrelated
  route namespaces are skipped before namespace/backend reads.
- Tests verify idempotent finalizer persistence, other metadata preservation,
  cache-reader avoidance, no Azure cleanup invocation, metadata conflicts,
  changed dependencies during retry, readback failure, cancellation, replacement
  Gateway/hub identities, route additions/withdrawals and backend changes.
  Cross-namespace tests verify current ReferenceGrant and namespace-label policy
  revocation without a Gateway-generation change.
- Focused provider/Gateway/resolver tests passed. Exact gocyclo found complexity
  33 in the shared resolver after adding query selection; extracted the existing
  route-attachment/deduplication logic and the complexity check now passes.
  Production provisioning/finalizer attachment remains disconnected.

### CI lint correction during ownership preparation

- User supplied CI findings: `goconst` for repeated replacement Gateway UID and
  stable SDK API-version literals, plus `whitespace` for a blank line at the end
  of the observation-test conditional.
- Plan: introduce shared test constants, reuse them in the affected identity and
  SDK transport fixtures, remove the trailing blank line and rerun targeted
  tests/source checks. Keep all lint rules and the pinned runner unchanged.
- Preserve the in-progress ownership preparation work; its validation continues
  independently. Full pinned lint still requires a compatible runner/CI result.
- Added shared replacement-UID/API-version test constants and reused the API
  version in transport assertions and async/pagination fixture URLs. Removed the
  reported trailing conditional blank line. No lint configuration was changed.
- The installed pinned v1.64.7 runner is built with Go 1.24.1 and remains
  incompatible with this module/toolchain. The exact standalone whitespace
  v0.2.0 CLI also fails to build because its old x/tools token layout rejects
  Go 1.27. Used the already-established compatible v2.15.0 runner for changed-code
  goconst/whitespace checks, not as a replacement claim for full pinned CI.
- The first compatible full-package check also reports pre-existing repeated
  literals. Those unrelated findings are not changed. Changed-code checks
  exposed repeated case labels in the new ownership test; converted those labels
  to local constants as well. Final focused verification passed below.

#### Final focused ownership/lint verification

- `goimports -local go.goms.io/fleet-networking -w .\pkg\controllers\hub\gateway\ownership_test.go`
  completed successfully.
- `go test -count=1 -skip '^TestAPIs$' .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  passed all seven packages. Envtest suites remain excluded because server assets
  are unavailable; fake-client checks do not prove informer/API-server lifecycle.
- `go vet .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  passed.
- `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run --no-config --enable-only=goconst,whitespace --new-from-rev=HEAD .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway .\pkg\controllers\hub\gatewayresolver`
  passed with zero changed-code issues. This compatible focused check does not
  establish full pinned CI lint success; the CI rerun remains required.
- `go run golang.org/x/lint/golint@latest -set_exit_status .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway .\pkg\controllers\hub\gatewayresolver`
  passed.
- `go run github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0 -over 30 .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway .\pkg\controllers\hub\gatewayresolver`
  passed.
- `go build .\cmd\...` passed.
- `go test -tags=e2e -run '^$' .\test\e2e\...` passed compilation only;
  no E2E scenarios were executed.
- `git diff --check` passed. No dependency or lint configuration changes, live
  Azure/cluster operations, production staging/enablement, commit or push occurred.
  The preparation helper remains disconnected from normal reconciliation;
  platform proofs, concurrent-writer policy and runtime write integration remain
  separate gates.

### Next slice: guarded injectable disabled-graph staging

- User requested continued implementation after commit `da13284`.
- [x] Task E1: Add SDK transport tests first and compose root/backend/route
  staging under one shared 30-second budget. Stop after any root write or
  nonconverged root observation; existing route staging already stops after a
  backend/route write. Require fresh GET evidence after every submission.
- [x] Task E2: Add controller tests first for persisted ownership before the
  injected stage call, metadata/status conflicts, provider failures, changed
  intent, deletion and nil injection. Invoke staging outside status retries
  only after the metadata preparation guard succeeds. Reobserve status intent,
  keep addresses absent and Programmed false, and schedule bounded rechecks.
- [x] Task E3: Update design/runbook and run focused tests, vet, source lint,
  manager builds and compile-only E2E. Record commands and remaining gates.
- Success criteria: at most one submitted SDK write per staging call; no stage
  call without observed finalizer/current intent; status retries cannot replay
  writes; normal manager startup still injects no staging provider. Tests use
  fakes/in-process SDK transport only. Do not enable traffic, publish hostname,
  claim whole-graph/stale-resource convergence or perform live Azure operations.
- Decision: extend the existing route provisioner rather than add another
  provider/client hierarchy. The injectable controller path is not production
  write authorization. Supported regions, probe Host/member-weight eligibility,
  disabled-graph deployment proof and concurrent external-writer policy remain
  prerequisites before SDK staging is constructed/injected by the manager.
- Existing design and canonical provider P2-P4 plan are the references; no
  domain-knowledge or specification files were found in their designated folders.
- E1 evidence: new tests initially failed for the missing staging API.
  `StageDisabledGraph` now reuses the route provisioner's clients and returns
  explicit root/route progress without partial success on errors. Transport tests
  enforce a single deadline across stage boundaries, one write at most, fresh
  ownership/root reads, manual-approval waiting, restart/no-op, cancellation and
  403/409/429 preservation. Missing later-stage clients fail before root writes.
  `go test -count=1 -run '^TestStageDisabledGraph' .\pkg\providers\azure\frontdoor`
  passed.
- E2 evidence: controller tests first failed for the missing injected-provider
  field. Reconciliation now prepares ownership and invokes the optional stage
  exactly once outside status retries. Metadata conflicts retry only the guard;
  status conflicts do not repeat staging. Nil injection leaves existing startup
  behavior unchanged. Pending status is revalidated against current intent,
  retains asynchronous/manual approval guidance and never publishes the SDK
  hostname or claims traffic readiness. Simultaneous observer/stager failures
  remain inspectable through joined errors; deleting owned Gateways still use
  cleanup without observing/staging.
- Expanded tests cover generation/backend/class changes during staging,
  withdrawal before/after preparation, missing prerequisites, patch failures,
  cleanup priority, repeated polling and explicit provider failure. Focused
  provider/Gateway/resolver tests and exact complexity checks pass. An intermediate
  test-edit placement error was corrected before those successful runs.
- Focused lint identified duplicated test case labels across the existing package;
  used local constants rather than changing rules and derived the export key from
  the fixture's route instead of repeating its literal name. Status text describes
  unimplemented enablement rather than claiming traffic is already disabled when
  root observation is still pending.

#### Final guarded staging verification

- `go test -count=1 -skip '^TestAPIs$' .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  passed all seven packages after the final source/test changes. Envtest suites
  remain excluded because server assets are unavailable.
- `go vet .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  passed.
- `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run --no-config --enable-only=goconst,whitespace,gocyclo,gosec,errorlint,prealloc,unused --new-from-rev=HEAD .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway .\pkg\controllers\hub\gatewayresolver`
  passed with zero changed-code findings. Full pinned v1 CI lint remains blocked
  locally by the previously documented toolchain incompatibility; this is not a
  claim of full pinned lint success.
- `go run golang.org/x/lint/golint@latest -set_exit_status .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway .\pkg\controllers\hub\gatewayresolver`
  passed.
- `go run github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0 -over 30 .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway .\pkg\controllers\hub\gatewayresolver`
  passed.
- `go build .\cmd\...` passed.
- `go test -tags=e2e -run '^$' .\test\e2e\...` passed compilation only;
  no E2E scenarios or live cluster/Azure operations were executed.
- `git diff --check` passed. Manager construction remains unchanged and omits
  `GraphStager`. No dependencies, configuration, lint rules, production write
  gates or advertised routing capabilities changed. No commit/push was requested
  for this slice.

### Next slice: suspend owned endpoints for confirmed empty intent

- User requested continued implementation after commit `815f19e`.
- [x] Task F1: Add SDK tests first for bounded, ownership-checked endpoint
  suspension. Read profile/endpoint only; never create missing resources. Submit
  at most one disable-only PATCH and require later provisioning/deployment
  observations before reporting suspension. Preserve API errors and cancellation.
- [x] Task F2: Add controller tests first for an already-owned Gateway whose
  complete model has no effective routes. Revalidate that empty intent, trusted
  hub/Gateway identity and existing finalizer uncached before calling suspension.
  Never acquire ownership for an empty Gateway. Keep suspension outside status
  retries and retain the finalizer for deletion/future authorized intent.
- [x] Task F3: Document the exact boundary and run focused tests, vet/lint,
  manager builds and compile-only E2E.
- Success criteria: removing the last effective route can suspend the previously
  owned endpoint through the injectable path; stale cached emptiness cannot
  suspend a currently routed Gateway. Missing/foreign resources are not created
  or adopted, and absent/disabled observations are distinct from submitted writes.
  Gateway/listener programming stays false and no address is published.
- Decisions: reuse the existing injected provider and root ownership/identity/state
  validators. Manager startup still injects no mutating provider. This addresses
  complete empty intent, not selective stale-resource cleanup or revocation while
  another backend is unresolved. Invalid/unresolved nonempty models, class
  handoff and full grant/membership/approval revocation remain separate work.
- References: canonical provider P5 plan, existing ownership preparation and
  root SDK disable operations. Live Azure operations remain unauthorized;
  observations do not prove datapath enforcement or exclude concurrent writers.
- F1 evidence: SDK tests first failed for the missing suspension API. Implemented
  profile ownership proof, expected endpoint identity/state checks and disable-only
  PATCH under one deadline. Missing profile/endpoint observations never create;
  a PATCH 404 is still an error, not observed absence. Synchronous/async submissions
  remain pending until a later GET. Backend/route clients and a usable hostname
  are unnecessary, and profile configuration readiness does not block withdrawal.
  Focused SDK tests and exact complexity checks passed.
- User asked what manager integration means. Clarified that it is still-missing
  startup construction/injection, not an automatic runtime gate; tests/callable
  helpers do not make the deployed manager create AFD resources. Continuing this
  withdrawal slice without changing that production boundary.
- F2 evidence: tests first failed because empty intent returned without scheduling
  or performing suspension. The existing injected provider now supports endpoint
  suspension, and complete empty models take a separate existing-ownership guard:
  uncached hub identity, current model and finalizer proof, with no metadata
  patch or new ownership acquisition. Status uses matching UID/generation/model,
  remains unprogrammed/unpublished and preserves cleanup ownership. New routes
  appearing during the operation prevent stale suspension diagnostics.
- Tests cover absent/disabled/pending observations, stale cached emptiness, removed
  finalizer, hub replacement, Gateway replacement/generation/deletion changes,
  missing reader/cleanup provider, failed API reads, nil injection, transient
  resolution errors, provider failure and status conflicts without replay.
  Focused provider/Gateway/resolver tests and exact complexity checks passed.
- Documentation now explicitly distinguishes missing startup construction/injection
  from implemented runtime gate checks. No automatic platform gate checker exists.
  The manager remains unchanged, and full revocation/live proof is not claimed.

#### Final empty-intent suspension verification

- `go test -count=1 -skip '^TestAPIs$' .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  passed all seven packages. Envtest remains excluded for missing server assets.
- `go vet .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  passed.
- `go build .\cmd\...` passed.
- `go test -tags=e2e -run '^$' .\test\e2e\...` passed compilation only.
- Focused lint found repeated test labels across existing packages. Converted them
  to local constants without changing lint rules. After the last test-only label
  correction, `go test -count=1 -run '^TestGatewaySuspensionRejectsChangedGateway$' .\pkg\controllers\hub\gateway`
  passed.
- `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run --no-config --enable-only=goconst,whitespace,gocyclo,gosec,errorlint,prealloc,unused --new-from-rev=HEAD .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway .\pkg\controllers\hub\gatewayresolver`
  passed with zero changed-code findings. Full pinned v1 CI lint still needs a
  compatible environment/CI rerun; the local toolchain incompatibility remains.
- `go run golang.org/x/lint/golint@latest -set_exit_status .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway .\pkg\controllers\hub\gatewayresolver`
  passed.
- `go run github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0 -over 30 .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway .\pkg\controllers\hub\gatewayresolver`
  passed.
- `git diff --check` passed. No dependency/configuration/manager injection changes,
  live Azure/cluster operations, traffic proof, commit or push occurred.

### Next slice: all-denied ReferenceGrant withdrawal

- User requested continued implementation after commit `a288d78`.
- [x] Task G1: Add controller tests first for explicit ReferenceGrant denial of
  all otherwise eligible routes, including missing target resources, no target
  reads before authorization, cached denial versus fresh permission, restored
  permission during suspension, grant-read errors and mixed graph refusal.
- [x] Task G2: Distinguish explicit backend RefNotPermitted denial from unknown
  backend resolution. Only all-denied routes may prove an empty authorized model
  and reuse the existing uncached existing-ownership suspension guard. A mixture
  with permitted or otherwise unresolved routes must still return no complete
  candidate; never stage its resolvable subset or mask an API failure.
- [x] Task G3: Update precise progress/gap documentation and run focused tests,
  vet/lint, manager builds and compile-only E2E.
- Success criteria: confirmed all-denied backend references can suspend an
  already-owned endpoint without requiring target ServiceImport/export reads,
  acquiring metadata ownership or replaying suspension in status retries.
  Permission restored in the uncached view prevents stale suspension; current
  status must not reuse a result after permission is restored.
- Scope decision: this closes the all-denied ReferenceGrant case, not mixed
  withdrawal alongside unresolved/still-permitted backends, selective stale-origin
  cleanup, class handoff or approval/member revocation. Manager injection and live
  authorization boundaries remain unchanged. ReferenceGrant read failures remain
  errors, not denial evidence.
- References: canonical provider P5 and the existing resolver's authorization-
  before-target-read behavior, plus the committed empty-intent suspension path.
- G1/G2 evidence: tests initially failed because denied references always made
  the candidate unresolved, so the existing empty-intent guard never ran.
  Resolution now recognizes explicit RefNotPermitted rejection as empty intent
  only when no eligible route resolves and no other resolution failure occurs.
  Mixed graphs still return nil. Both cached and uncached resolution share this
  rule, preserving the existing ownership/intent guard and status retry boundary.
- Expanded cases verify deleting grants, missing denied targets without backend
  reads, no new ownership on never-owned Gateways and cached/uncached grant-read
  failures. Fresh permission blocks stale cached denial; permission restored
  during suspension prevents stale result publication. Focused Gateway/resolver/
  HTTPRoute tests passed.
- An initial complexity check reported 31. Classifying the resolver's canonical
  RefNotPermitted reason directly removes the redundant ReferenceValid qualifier:
  that reason is emitted specifically for policy denial. Other typed resolution
  failures and API errors still block completeness. Exact complexity now passes
  without changing lint rules or adding a trivial predicate abstraction.

#### Final all-denied withdrawal verification

- `go test -count=1 -skip '^TestAPIs$' .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  passed all seven packages. Envtest remains excluded because server assets
  are unavailable; fake-client checks are not informer lifecycle evidence.
- `go vet .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  passed.
- `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run --no-config --enable-only=goconst,whitespace,gocyclo,gosec,errorlint,prealloc,unused --new-from-rev=HEAD .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway .\pkg\controllers\hub\gatewayresolver`
  passed with zero changed-code findings. Full pinned v1 CI lint still requires
  a compatible environment/CI rerun.
- `go run golang.org/x/lint/golint@latest -set_exit_status .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway .\pkg\controllers\hub\gatewayresolver`
  passed.
- `go run github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0 -over 30 .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway .\pkg\controllers\hub\gatewayresolver`
  passed.
- `go build .\cmd\...` passed.
- `go test -tags=e2e -run '^$' .\test\e2e\...` passed compilation only.
- `git diff --check` passed. No dependencies, manager injection, configuration,
  lint rules, live resources, advertised capabilities, commit or push changed.
  Mixed-graph revocation and real startup/enablement/datapath proofs remain open.

### H. Mixed-denial whole-endpoint withdrawal

#### Requirements and additional user comments

- User selected `suspend-endpoint`: a confirmed denied ReferenceGrant suspends
  the entire already-owned endpoint, intentionally interrupting permitted routes
  too. Unresolved peers must not block withdrawal; never stage a partial graph.
- User requested brief clarifying comments at critical design decisions.
- Existing private-only, ownership, manual approval and startup gates remain.

#### Plan and success criteria

- Phase H1 / Task H1.1: [x] Add regression tests first for mixed references,
  backend read errors, uncached evidence, ownership/attachment drift and conflicts.
  Success: cases distinguish a positive denial from unavailable evidence.
- Phase H2 / Task H2.1: [x] Extract the shared authorization-only check and
  current identity guard; scan attached routes without target backend reads.
  Success: only current, otherwise eligible denied references authorize withdrawal.
- Phase H2 / Task H2.2: [x] Suspend outside status retries after revalidation.
  Preserve read/provider/status errors and recheck denial before reporting it.
  Success: one suspension per reconcile, no partial staging/new ownership, no
  stale readiness or denial claim when unrelated resolution fails or policy changes.
- Phase H3 / Task H3.1: [x] Update directly related docs and run focused checks.
  Success: targeted tests/vet/lint/build pass; limitations remain explicit.

#### Decisions and implementation details

- Use a positive authorization-only proof, not a fabricated empty model. Reuse
  the existing grant semantics and disable-only SDK operation.
- Snapshot route identity/specification and pin hub/Gateway identity; re-read
  attachment policy, finalizer, class acceptance and grants before the operation
  and before reporting current denial. Sequential reads are not transactions.
- A full backend resolution failure must still permit a conservative unprogrammed
  status update. Preserve unrelated previously observed status rather than publish
  partially calculated attachment counts; return the failure for retry.
- Manager injection, traffic enablement and selective resource cleanup remain out
  of scope. These tests do not prove live traffic enforcement.

#### Progress and evidence

- New mixed-graph/backend-failure/conflict regressions first failed because the
  existing nil-model path never suspended. All seven focused packages now pass.
- Added `reference_withdrawal.go` for authorization-only scanning and fresh
  pre-operation/status proof. Shared `AuthorizeBackendReference` preserves the
  existing resolver semantics without reading target state; shared
  `currentGatewayIdentity` preserves trusted metadata checks without a model.
- Reconcile retains resolution/provider/observation/status errors together and
  patches conservative withdrawal status even when full resolution fails.
  A deep copy preserves the optimistic patch base during status restoration.
- Brief comments explain whole-endpoint impact, target-independent authorization,
  uncached indexing, nontransactional reads, retry safety and partial-status refusal.
- Regression cases cover stale permission, restored permission before/during
  mutation, changed route identity/specification, lost finalizer/current class,
  ineligible routes, unknown proof and unrelated grant failures.
- Updated design, canonical implementation progress and test runbook. Manager
  injection and enablement remain unchanged.
- Initial compatible lint found three newly introduced repeated test literals.
  Used the canonical Gateway condition enum and distinct descriptive case labels;
  no lint settings or unrelated baseline findings were changed.

#### Final mixed-denial withdrawal verification

- `go test -count=1 -skip '^TestAPIs$' .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  passed all seven focused packages, including the new policy-race cases.
- `go vet .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  passed.
- `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run --no-config --enable-only=goconst,whitespace,gocyclo,gosec,errorlint,prealloc,unused --new-from-rev=HEAD .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway .\pkg\controllers\hub\gatewayresolver`
  passed with zero changed-code issues.
- `go run golang.org/x/lint/golint@latest -set_exit_status .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway .\pkg\controllers\hub\gatewayresolver`
  passed.
- `go run github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0 -over 30 .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway .\pkg\controllers\hub\gatewayresolver`
  passed.
- `go build .\cmd\...` passed.
- `go test -tags=e2e -run '^$' .\test\e2e\...` passed compilation only.
- `git diff --check` passed. Envtest server assets remain unavailable; full
  pinned v1 CI lint remains locally incompatible and requires CI verification.
  No live Azure/cluster operations, dependencies, configuration, manager wiring,
  traffic enablement, commits or pushes were performed.

### I. Exact supported-graph inventory before reporting staged convergence

#### Requirements and additional user comments

- Continue the approved provider plan after mixed-reference withdrawal.
- Detect stale/unexpected endpoints, routes, origin groups and origins under the
  owned profile instead of equating expected-resource GETs with exact membership.
- Keep brief comments at critical decision boundaries. No deletes, enablement,
  new configuration/dependencies or manager injection are included.

#### Plan and success criteria

- Phase I1 / Task I1.1: [x] Write transport tests first for exact supported
  inventory, pagination, missing/extra/duplicate/malformed identities and errors.
  Success: the previous expected-only implementation fails the new regressions.
- Phase I2 / Task I2.1: [x] Reobserve owned disabled roots, then enumerate the
  supported resource collections after expected disabled-route convergence.
  Success: all pages exactly match expected ARM identities before reporting
  inventory verification; no collection reads after pending/submitted work.
- Phase I2 / Task I2.2: [x] Expose inventory verification in pending diagnostics.
  Success: no address/Programmed=True or stale-resource deletion; errors and
  cancellation yield no partial result and preserve the original shared budget.
- Phase I3 / Task I3.1: [x] Update docs and run focused regression/lint/build checks.
  Success: existing behavior remains except explicit refusal of nonexact inventory.

#### Decisions and implementation details

- This checks the resource types generated by the supported HTTP graph:
  endpoints, routes, groups and origins. Unsupported domain/rule-set/security
  policy/secret inventory and their lifecycles remain separate proof gates.
- Verify ownership and observed disabled roots again before collection access.
  Read all collection pages, reject duplicates/unknown IDs and prove expected
  membership rather than treating a partial/empty response as completeness.
- Collection membership is distinct from earlier configuration/approval GETs,
  atomic snapshots, safe external-writer exclusion, readiness and traffic proof.
- Refuse extras explicitly. Removing stale bindings/resources requires the
  separate lifecycle implementation, not blind deletion during observation.

#### Progress and evidence

- Recorded the tests-first slice before code changes. Design and canonical
  provider phases P3/P4/P5 remain the applicable approved references; no domain
  knowledge/specification files are present in their designated directories.
- New SDK transport cases first failed because expected-only staging accepted
  nonexact inventory and never exhausted collection pages.
- Added `inventory.go`: fresh owned/disabled-root checks followed by all supported
  collection pages, full ARM identity membership, duplicate/missing refusal and
  existing child identity validation. No extra writes/deletes are introduced.
- `StageDisabledGraph` reports `InventoryVerified` only after expected graph
  convergence and exact collection membership; controller diagnostics expose it
  without claiming programming or advertising an address.
- Extended dependency-order, shared-budget, restart, approval, SDK-failure and
  cancellation tests to include inventory. Added casing/foreign-parent,
  fresh ownership/disable-state and 404-not-empty-success cases.
- A test expansion initially inserted declarations inside another test; moved
  them to file scope. Focused tests then passed all seven packages.
- Compatible lint initially flagged repeated test JSON keys and shared case
  labels. Added semantic test-key constants and distinct case labels; no lint
  settings were changed. Final rerun passed.
- Updated design, canonical implementation progress and the test runbook, with
  unsupported inventory types and live/atomic/writer-policy limits explicit.

#### Final supported-inventory verification

- `go test -count=1 -skip '^TestAPIs$' .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  passed all seven focused packages.
- `go test -count=1 .\pkg\providers\azure\frontdoor` passed again after the final
  test-label correction.
- `go vet .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  passed.
- `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run --no-config --enable-only=goconst,whitespace,gocyclo,gosec,errorlint,prealloc,unused --new-from-rev=HEAD .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway .\pkg\controllers\hub\gatewayresolver`
  passed with zero changed-code issues.
- `go run golang.org/x/lint/golint@latest -set_exit_status .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway .\pkg\controllers\hub\gatewayresolver`
  passed.
- `go run github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0 -over 30 .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway .\pkg\controllers\hub\gatewayresolver`
  passed.
- `go build .\cmd\...` passed.
- `go test -tags=e2e -run '^$' .\test\e2e\...` passed compilation only.
- `git diff --check` passed. Envtest remains excluded because server assets are
  unavailable; the pinned v1 lint runner still requires compatible CI verification.
  No live Azure/cluster operations, manager injection, traffic enablement,
  dependency/configuration changes, commits or pushes were performed.

### J. Reject unsupported profile inventory without removing protection

#### Requirements and additional user comments

- Continue the approved provider work after supported-graph inventory.
- Do not silently ignore profile custom domains, rule sets, security policies or
  secrets while reporting inventory verification for the HTTP-only subset.
- Observation only: no deletes, migration, enablement, external asset changes,
  customer configuration/dependencies or manager injection.

#### Plan and success criteria

- Phase J1 / Task J1.1: [x] Add tests first for each unsupported collection:
  nonempty/nil records, missing/null resource arrays, pagination and errors.
  Success: expected-only inventory fails the new refusal regressions.
- Phase J2 / Task J2.1: [x] Construct scope-bound SDK clients without requests and
  require complete, explicit empty arrays across all pages after owned disabled
  supported-graph verification. Success: any unsupported/ambiguous inventory
  returns an error and zero staging result, never deletion or silent migration.
- Phase J2 / Task J2.2: [x] Extend client preflight and composed-budget tests.
  Success: missing clients cause zero Azure calls; pending/write stages skip
  inventory; all successful observations share the original bounded deadline.
- Phase J3 / Task J3.1: [x] Update current docs and run focused checks.
  Success: inventory diagnostics remain distinct from readiness; startup and
  live/platform/external-writer proof gates remain unchanged.

#### Decisions and implementation details

- An explicitly empty collection is different from an omitted/null `value`
  property. Both unsupported contents and ambiguous absence fail closed.
- Reject any returned item, including nil records. Do not inspect secret
  contents, approval state or referenced external WAF/certificate assets.
- Read every page, including empty pages with continuation. Shared SDK pager
  logic may be reused for the four zero-resource collections.
- Extending `InventoryVerified` strengthens its contract without adding a new
  configuration mode or implying those features are supported.

#### Progress and evidence

- Recorded this tests-first continuation before code changes. SDK documentation
  confirms the four profile-scoped pagers use stable API `2024-02-01`.
- Canonical provider phases P1/P3/P4 remain applicable. No new specification or
  domain-knowledge source supersedes the already recorded references.
- The first unsupported-domain regression failed because expected-only inventory
  returned success after 14 calls without checking the unsupported collection.
- Added four scope-bound SDK read clients and required initialization before
  composed staging accesses ARM. Constructor behavior remains request-free.
- Added `unsupported_inventory.go`, reusing one typed SDK pager operation to
  require explicit empty arrays across all pages. It rejects unknown/nonempty
  inventory without logging secret parameters, dereferencing external assets,
  migration, mutation or deletion.
- Strengthened `InventoryVerified`; existing diagnostics remain unprogrammed.
  Supported-membership tests retain their own prefix fixture, while successful
  full-stage fixtures include the four additional collections.
- Expanded tests cover all four collections, ambiguous first/later pages,
  unsupported later resources, later 403/404 errors, missing client preflight and
  cancellation/error handling throughout the original shared budget.
- Updated design/progress/runbook to distinguish absence verification from
  feature support and document future profile-scoped read/list requirements.

#### Final unsupported-inventory verification

- `go test -count=1 .\pkg\providers\azure\frontdoor` passed.
- `go test -count=1 -skip '^TestAPIs$' .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  passed all seven focused packages, including ambiguous later-page cases.
- `go vet .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  passed.
- `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run --no-config --enable-only=goconst,whitespace,gocyclo,gosec,errorlint,prealloc,unused --new-from-rev=HEAD .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway .\pkg\controllers\hub\gatewayresolver`
  passed with zero changed-code issues.
- `go run golang.org/x/lint/golint@latest -set_exit_status .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway .\pkg\controllers\hub\gatewayresolver`
  passed.
- `go run github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0 -over 30 .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway .\pkg\controllers\hub\gatewayresolver`
  passed.
- `go build .\cmd\...` passed.
- `go test -tags=e2e -run '^$' .\test\e2e\...` passed compilation only.
- `git diff --check` passed. Envtest server assets remain unavailable; full
  pinned v1 lint still requires compatible CI verification. No live Azure or
  cluster operations, dependency/configuration changes, startup injection,
  traffic enablement, commits or pushes were performed.

### K. Invalidate convergence on conflicting later collection observations

#### Requirements and additional user comments

- Continue approved observation work after unsupported-resource refusal.
- Do not accept later inventory properties that contradict earlier disabled
  endpoint/route, managed group policy or approved private-origin convergence.
- Keep verification read-only, with the existing deadline/one-write bound,
  explicit failures, no partial result and no startup/enablement changes.

#### Plan and success criteria

- Phase K1 / Task K1.1: [x] Add transport tests first for changes seen only in
  collection records, not the preceding expected-resource GETs.
  Success: unchanged identities cannot mask changed configuration/approval.
- Phase K2 / Task K2.1: [x] Reuse disabled-endpoint and managed-group validation;
  validate current disabled-route policy and canonical private-origin observations.
  Success: changed, pending, failed or unknown collection observations invalidate
  the previous convergence result without a write or partial-success fallback.
- Phase K2 / Task K2.2: [x] Preserve AFD-returned unmanaged defaults and identity
  casing; keep asynchronous approval guidance actionable.
  Success: equivalent observations pass; pending approval surfaced mid-read
  cannot appear converged and includes the authorized PLS-owner action item.
- Phase K3 / Task K3.1: [x] Update docs and complete focused checks.
  Success: all scoped checks pass, with sequential/nontransactional and live
  validation limits still explicit.

#### Decisions and implementation details

- A conflicting later read invalidates the earlier result and requests a fresh
  reconcile, rather than mutating during inventory or treating old GETs as current.
  Ordinary pending dependencies observed before inventory still use existing
  progress states; this slice handles changes during the observation sequence.
- Reuse canonical private-origin approval/configuration classification instead
  of duplicating Private Link identity, Host, port, weight and state semantics.
- Group checks cover only controller-managed probe/session policy. Preserve the
  existing acceptance of service-returned load-balancing defaults.
- No finite read sequence excludes later changes or external writers. Platform,
  runtime injection, traffic enablement and live enforcement proofs remain open.

#### Progress and evidence

- Recorded tests-first work before code changes. The approved provider P3/P4/P5
  references remain applicable; no new configuration/specification is introduced.
- Red transport regressions reproduced false convergence for later collection
  policy/state changes while the unchanged-equivalence control passed.
- Extracted the existing disabled-endpoint predicate and probe/session matcher;
  collection verification reuses these plus the HTTP route matcher and canonical
  private-origin observer. Full ARM IDs recover canonical origin map keys before
  state classification, including case-insensitive SDK identities.
- Pending approval observed during inventory explicitly requests asynchronous
  authorized PLS-owner approval. Any conflicting observation returns zero staging
  result, without extra writes, requests or startup integration.
- Focused provider/controller/manager unit tests and vet passed, including the
  later-page approval regression and unchanged-equivalence positive control.
- Updated both design documents and the test runbook. Compatible changed-code
  lint, golint, complexity, manager builds and E2E compile all passed.
- The extra pagination regression exposed a third `nextLink` literal via
  goconst. Extracted the shared inventory pagination field constant and reused it
  in supported/unsupported collection tests; provider tests and all lint reran
  successfully after this tightly coupled correction.
- Validation evidence (all passed):
  - `go test -count=1 -skip '^TestAPIs$' .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  - `go vet .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  - `go test -count=1 .\pkg\providers\azure\frontdoor` (rerun after pagination-key correction).
  - `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run --no-config --enable-only=goconst,whitespace,gocyclo,gosec,errorlint,prealloc,unused --new-from-rev=HEAD .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway .\pkg\controllers\hub\gatewayresolver`
  - `go run golang.org/x/lint/golint@latest -set_exit_status .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway .\pkg\controllers\hub\gatewayresolver`
  - `go run github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0 -over 30 .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway .\pkg\controllers\hub\gatewayresolver`
  - `go build .\cmd\...`
  - `go test -tags=e2e -run '^$' .\test\e2e\...` (compilation only).
  - `git diff --check`
- Existing limitations remain: pinned v1 lint is locally incompatible with the
  installed Go/module versions; compatible changed-code lint is not full pinned
  CI. Envtest assets and authorized live resources are unavailable; no live
  approval, disabled-deployment, private HTTP or concurrent-writer proof is claimed.
- K is complete; the user explicitly requested committing this slice.
  No dependencies, manager startup, enablement or Azure resources changed.

### L. Require explicit supported-resource arrays on every inventory page

#### Requirements and additional user comments

- Continue the approved provider plan after committed slice K (`81c1ff0`).
- Supported endpoint/route/group/origin collection pages must not claim complete
  inventory when any page omits its resource array or returns null, even when
  other pages contain all expected identities.
- Preserve legitimate explicitly empty pages, shared operation budgets,
  read-only inventory and zero partial result on failure.

#### Plan and success criteria

- Phase L1 / Task L1.1: [x] Write transport regressions first for missing/null
  arrays before and after pages containing the expected supported resources.
  Success: reproduce current false completeness for all four collections.
- Phase L2 / Task L2.1: [x] Require explicit arrays on every supported page,
  reusing the unsupported-collection completeness guard.
  Success: stop on the ambiguous page, return an explicit error and zero result,
  and perform no writes or later collection requests.
- Phase L2 / Task L2.2: [x] Preserve explicit empty-page pagination controls.
  Success: empty pages before/after valid records still verify the complete
  expected graph, with unchanged deadlines and request ordering.
- Phase L3 / Task L3.1: [x] Update design/runbook and run focused checks.
  Success: scoped tests/vet/lint/build/compilation checks pass; no automatic
  startup gate checks, traffic enablement or live proof is claimed.

#### Decisions and implementation details

- This applies the explicit-page requirement already used for unsupported
  collections to supported membership proof; it introduces no new feature API.
- A continuation link cannot prove an omitted/null page contained no additional
  records. Expected IDs found elsewhere cannot repair that missing evidence.
- Keep exact identity and fresh configuration/approval checks unchanged.
  Sequential observations remain nontransactional and do not authorize writes.
- The approved provider P3/P4 references apply. Domain knowledge/specification
  directories remain absent; no product/domain contract changes are proposed.

#### Progress and evidence

- Recorded the tests-first slice before code changes.
- The focused red run reproduced all 16 ambiguous cases across four collections
  and both page positions. All eight explicit empty-page controls already pass.
- Extracting the existing unsupported-page presence check preserves its error
  text and applies the same evidence requirement before supported record handling.
- Implemented one shared presence guard for all supported/unsupported inventory
  pages. Identity/configuration checks, unsupported-asset refusal and context
  checks retain their existing behavior.
- All scoped provider/controller/manager tests and vet passed. The 24-case
  transport matrix proves ambiguous-page refusal and explicit-empty equivalence
  before and after expected resources, including exact early-stop request counts.
- Design and runbook descriptions now specify page evidence consistently.
  Compatible lint, manager builds, E2E compilation and final diff review passed.
- Validation evidence (all passed):
  - `go test -count=1 -skip '^TestAPIs$' .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  - `go vet .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  - `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run --no-config --enable-only=goconst,whitespace,gocyclo,gosec,errorlint,prealloc,unused --new-from-rev=HEAD .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway .\pkg\controllers\hub\gatewayresolver`
  - `go run golang.org/x/lint/golint@latest -set_exit_status .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway .\pkg\controllers\hub\gatewayresolver`
  - `go run github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0 -over 30 .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway .\pkg\controllers\hub\gatewayresolver`
  - `go build .\cmd\...`
  - `go test -tags=e2e -run '^$' .\test\e2e\...` (compilation only).
  - `git diff --check`
- Existing pinned v1 lint compatibility, envtest asset and live Azure proof
  limitations from K remain unchanged. No dependencies/configuration, manager
  startup or Azure resources changed; no traffic enablement was introduced.
- L is complete; the user explicitly requested committing this slice.

### M. Bounded stale private-origin cleanup

#### Requirements and additional user comments

- User requested continuing after committed slice L (`7215706`).
- The next P5 lifecycle slice would remove private origins left behind after a
  member removal or source Service UID/PLS identity replacement, but only when a
  complete supported desired graph has converged under disabled owned roots.
- This changes existing extra-origin refusal into a narrowly authorized child
  deletion. The user explicitly approved this bounded plan before implementation.
- Additional user instruction: add clarifying but brief comments about critical
  design decisions; keep comments focused on safety/ownership/readiness boundaries.

#### Plan and success criteria

- Phase M1 / Task M1.1: [x] Write SDK transport tests first for removed members,
  Service UID replacement, multiple stale origins, restart and deletion-in-progress.
  Success: tests distinguish a deletion submission from freshly observed absence.
- Phase M1 / Task M1.2: [x] Add refusal tests for missing expected resources,
  pending desired approval, unknown/malformed/foreign/public-origin records,
  unexpected groups/routes/endpoints, unsupported profile assets, ambiguous pages,
  ownership/disabled-state drift, cancellation and SDK errors.
  Success: none of these conditions authorizes a stale-origin DELETE.
- Phase M2 / Task M2.1: [x] Separate complete supported inventory observation
  from the strict final no-extras convergence decision. Classify only stale
  Fleet-shaped private origins under the single expected group as candidates.
  Success: all desired origins and routing/group policy converge, every page is
  explicit, and all other supported/unsupported collections pass before deletion.
- Phase M2 / Task M2.2: [x] Reobserve trusted owned Premium/disabled roots just
  before deleting one deterministically selected eligible stale origin.
  Success: at most one write under the existing shared 30-second deadline;
  no deletes of desired origins, groups, routes, profiles or external resources.
- Phase M2 / Task M2.3: [x] Report cleanup pending without inventory success
  until a later complete collection read proves absence.
  Success: a completed DELETE or DELETE 404 cannot immediately claim
  `InventoryVerified=true`; restart and asynchronous deletion remain idempotent.
- Phase M3 / Task M3.1: [x] Update pending diagnostics, unit coverage,
  design/runbook and this breadcrumb; run focused validation.
  Success: explicit errors and zero partial results are preserved; no SDK work
  enters Kubernetes status retry callbacks.

#### Decisions and safety boundaries

- Authority remains the full owned profile ARM identity and exact hub/Gateway
  owner tags, plus the child's verified full ARM parent identity. Generated name
  shape only narrows cleanup eligibility; it is not ownership evidence by itself.
- Require the existing persisted-finalizer/current-intent preparation path.
  Only the already gated injectable stager gains this behavior; manager startup
  remains unchanged and performs no new resource provisioning or pruning.
- Only extra private Fleet-shaped origins in the current expected origin group
  are eligible. Unknown/nonprivate/malformed records remain explicit refusals.
  Removing obsolete groups or routes and migrating logical backends stay out of
  scope. Fully unresolved intent still does not authorize speculative cleanup.
- Verify complete desired private-origin convergence and all other inventory
  before a DELETE. Reobserve root ownership/disabled convergence afterward in the
  read sequence immediately before mutation; do not repair roots in this step.
- Deletion is bounded to one stale origin per invocation. Observe ongoing
  deletion rather than submitting repeatedly; require later absence even if an
  initial submission completes synchronously or returns 404.
- Never manage PLS/LB, AFD-owned private endpoints, DNS, WAF or certificate
  resources; AFD retains its shared private-endpoint lifecycle responsibility.
- Sequential checks do not exclude external writers. Existing region, Host/
  probe, weights, disabled-deployment and concurrent-writer proof gates remain
  prerequisites for manager integration and traffic enablement.
- No live Azure operations, new configuration/dependencies, approval automation,
  address publication or `Programmed=True` are part of this slice.

#### References and implementation grounding

- Provider phases P2/P3/P5 in `docs/design/gep-1748-implementation-plan.md`:
  child ownership, dependency ordering and stale binding lifecycle.
- `ownership.go`: verified root ARM scope, exact identity tags and inherited
  child authority; `backend.go`: UID/PLS/endpoint-derived origin names.
- `inventory.go` / `unsupported_inventory.go`: strict paginated identity,
  configuration/approval and explicit resource-array verification.
- `cleanup.go` / `suspension.go`: bounded SDK operation and later-observation
  patterns; `gateway/ownership.go`: persisted/current cleanup ownership guard.
- No applicable domain-knowledge/specification files were found in the prior
  approved plan; this proposal introduces no new product API.

#### Progress and evidence

- User approved M through the approval form. Tests-first implementation may now
  proceed; live Azure operations and manager/traffic enablement remain excluded.
- Added transport lifecycle/refusal tests first. The red run confirms that the
  existing implementation stops at extra-origin refusal instead of performing
  the newly approved bounded cleanup.
- SDK `AFDOriginsClientBeginDeleteOptions` has no If-Match precondition. The
  implementation will reobserve both the selected candidate and disabled owned
  parent before DELETE, while keeping external-writer exclusion an open gate.
- Implemented typed origin inventory tracking for all expected IDs and validated
  stale candidates. Missing expected origins, conflicting desired configuration/
  approval and any unsupported/other unexpected resource prevent deletion.
- Added `origin_cleanup.go`: private IPv4/PLS/group/port and fixed policy
  validation, known-state classification, deterministic selection, fresh bound
  candidate GET, refusal on changed identity/configuration, and fresh owned
  Premium/disabled parent observations before one DELETE.
- In-flight ARM work is observed without resubmission. Known failed/rejected
  stale private connectivity remains removable; unknown state remains an error.
  GET/DELETE 404 and synchronous completion remain pending until later full-list
  absence. `StaleOriginsPending` is exposed only as unprogrammed diagnostics.
- Added brief comments at parent-authority, all-inventory-before-delete,
  unhealthy-stale removal, in-flight-operation, observed-absence and no-If-Match
  boundaries, as requested. No automatic runtime gate checker was introduced.
- Focused provider/controller/manager tests, vet, compatible changed-code lint,
  golint and complexity checks passed. Transport coverage includes case aliases,
  Service UID/PLS replacement, removed members, multiple candidates, fresh
  identity swaps/configuration changes, root drift, unsupported assets, missing/
  revoked desired approval, errors/cancellation at every extra request and restart.
- Updated design and runbook descriptions, including future scoped origin DELETE
  permission and unchanged live/external-writer gates.
- Final validation (all passed):
  - `go test -count=1 -skip '^TestAPIs$' .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  - `go vet .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway... .\pkg\controllers\hub\httproute .\cmd\hub-gateway-controller-manager`
  - `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run --no-config --enable-only=goconst,whitespace,gocyclo,gosec,errorlint,prealloc,unused --new-from-rev=HEAD .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway .\pkg\controllers\hub\gatewayresolver`
  - `go run golang.org/x/lint/golint@latest -set_exit_status .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway .\pkg\controllers\hub\gatewayresolver`
  - `go run github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0 -over 30 .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway .\pkg\controllers\hub\gatewayresolver`
  - `go build .\cmd\...`
  - `go test -tags=e2e -run '^$' .\test\e2e\...` (compilation only).
  - `git diff --check`
- Existing pinned v1 lint compatibility and envtest asset limitations remain.
  Fake transports do not prove Azure replacement coexistence, disabled-resource
  behavior, deletion/PLS lifecycle, external-writer exclusion or traffic enforcement.
  In particular, a replacement that cannot first stage concurrently remains
  blocked; no speculative prune-before-convergence fallback is introduced.
- M is complete; the user explicitly requested committing this slice. No live
  Azure/cluster operations, dependencies/configuration changes, manager injection,
  traffic enablement or pushes were performed.

### N. Gateway manager cache readiness

#### Requirements and additional user comments

- User requested continuing after committed slice M (`a8325ff`).
- Address the initialization/cache readiness requirement in provider Task P6.2.
  The Gateway manager currently registers only `healthz.Ping` for readiness.
  Controller-runtime v0.19.0 starts probe serving before its caches; this can
  report readiness while the required Kubernetes informers are still unsynced.
- Keep liveness independent of cache synchronization. Readiness must not depend
  on Azure availability, Private Link approval, traffic readiness or leadership.
- Keep AFD disabled by default and preserve disabled-mode startup without Azure
  configuration or Gateway API/Fleet informer initialization.

#### Plan and success criteria

- Phase N1 / Task N1.1: [x] Add startup/readiness regressions first using the
  existing manager harness and fake informer synchronization state.
  Success: demonstrate the missing cache-readiness check before implementation;
  cover each required kind, all-synced readiness, stopped informers, shutdown,
  initialization/registration failures and disabled-mode isolation.
- Phase N2 / Task N2.1: [x] Initialize the seven watched informer kinds without
  blocking before manager start, then register a cache-readiness checker in
  enabled controller setup. Required kinds are GatewayClass, Gateway, HTTPRoute,
  ReferenceGrant, ServiceImport, InternalServiceExport and Namespace.
  Success: the checker fails until every required informer has synced, fails
  for stopped informers or cancelled manager context, and performs no SDK work.
- Phase N2 / Task N2.2: [x] Preserve liveness and the existing feature boundary;
  add brief comments at the early-informer and Azure/leadership boundaries.
  Success: disabled mode creates no additional informers/checks; readiness
  evaluation is nonblocking and does not require becoming the elected leader.
- Phase N3 / Task N3.1: [x] Update directly related design/runbook progress,
  run focused manager tests, vet, formatting/lint and build, and record results.
  Success: existing startup/provider behavior is preserved; P6.2 as a whole
  remains incomplete and no live informer/datapath evidence is claimed.

#### Decisions and implementation boundaries

- Bootstrap all required informers before manager start with
  `cache.BlockUntilSynced(false)`. A generic wait on an initially empty cache
  can succeed before controller sources request their informers.
- Retain the required informer handles and check `HasSynced` and `IsStopped`
  directly. Probe requests must not block on cache synchronization, create
  informers, start goroutines or call Kubernetes/Azure services.
- Register the additional check only through enabled controller setup, after
  controller registration succeeds. Surface initialization/registration errors
  through the existing startup error path.
- Readiness proves initial local informer synchronization, not ongoing API
  availability, authorization freshness, event-handler drain, Azure convergence,
  dataplane enforcement or exclusion of external writers.
- Standby replicas must be allowed to become ready after their caches sync;
  leadership and individual Gateway `Programmed` conditions are separate.
- No image/chart/CI/RBAC configuration, manager SDK stager injection, traffic
  enablement, new dependencies, live operations or automatic approval is included.

#### References and implementation grounding

- `docs/design/gep-1748-implementation-plan.md`, Task P6.2: readiness depends on
  initialization/caches rather than Azure uptime.
- `cmd/hub-gateway-controller-manager/main.go` and `main_test.go`: feature-gated
  setup, independent health/readiness registration and existing startup harness.
- Gateway, GatewayClass and HTTPRoute `SetupWithManager` methods: the seven
  required watched resource kinds; resolver indexes already initialize two kinds.
- Controller-runtime v0.19.0 `pkg/manager/internal.go`: probe HTTP servers start
  before caches. Its `cache.Informer` API exposes nonblocking synchronization
  and stopped-state checks; `BlockUntilSynced(false)` permits early construction.
- Domain-knowledge/specification searches still found no applicable files.
  No product/domain API change or new versioned specification is proposed.

#### Progress and evidence

- Recorded this tests-first plan before code changes. User explicitly approved
  slice N through the approval form; implementation may proceed.
- Worktree was clean at `a8325ff` before recording this section.
- User reported a CI `nilerr` finding in `reference_withdrawal.go` while the
  readiness red run was executing. Pause readiness implementation to correct
  this prior-slice finding without changing withdrawal semantics: express route
  eligibility as a positive boolean rather than treating unsupported-route
  validation as an authorization read error. Existing unsupported-route and
  read-error tests must remain green; explicitly include `nilerr` in validation.
- The readiness red run reproduced the missing check, but repeated full setup
  also hit controller-runtime's process-global duplicate-controller-name guard.
  Keep that guard intact: use one full setup integration case and exercise the
  readiness registration helper directly for the remaining unit matrix.
- The CI correction now expresses eligibility positively and preserves both
  ineligible-route handling and actual namespace/grant error propagation.
  Gateway/controller tests and the compatible explicit `nilerr` check passed.
- Implemented `readiness.go`: nonblocking early informer construction,
  synchronization/stopped/context checks and explicit startup error propagation.
  Enabled setup registers the additional check after controller registration;
  no SDK stager or Azure readiness dependency was added.
- The complete manager test suite passes, including the seven-kind state/error
  matrix, initial synchronization transitions, shutdown and disabled-mode isolation.
  HTTP tests initially returned 404 because the standalone handler was not given
  the manager's prefix stripping; they now exercise that same serving behavior.
  No production name validation or other guard was weakened for test isolation.
- Vet, controller-manager builds and golint passed. Compatible changed-code lint
  found a test-matrix preallocation issue; preallocate the known matrix size and
  rerun validation before completing N3.
- Preallocated the test matrix, reran manager tests and compatible changed-code
  lint including `nilerr`; both passed. Updated design/runbook progress and
  reviewed the final diff, including the new readiness implementation/tests.
- Final validation (all passed):
  - `go test -count=1 -skip '^TestAPIs$' .\pkg\controllers\hub\gateway...`
  - `go test -count=1 .\cmd\hub-gateway-controller-manager`
  - `go vet .\pkg\controllers\hub\gateway .\cmd\hub-gateway-controller-manager`
  - `go build .\cmd\...`
  - `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run --no-config --enable-only=nilerr,goconst,whitespace,gocyclo,gosec,errorlint,prealloc,unused --new-from-rev=HEAD .\pkg\controllers\hub\gateway .\cmd\hub-gateway-controller-manager`
  - `go run golang.org/x/lint/golint@latest -set_exit_status .\pkg\controllers\hub\gateway .\cmd\hub-gateway-controller-manager`
  - `git diff --check`
- N is complete, with the user-reported withdrawal lint correction included.
  Pinned v1 CI compatibility and absent envtest assets remain limitations;
  compatible lint is not full pinned CI evidence. No live cluster/Azure,
  informer freshness, provider enablement or dataplane proof is claimed.
  No dependencies/configuration, SDK stager injection or traffic enablement
  changed. The user explicitly requested committing this slice.

### O. Separate Gateway image build and CI smoke coverage

#### Requirements and additional user comments

- User requested continuing after committed slice N (`c5cb4a8`).
- Advance the separate-image/build/CI portion of Tasks 6.1/6.2 and P6.2 without
  enabling SDK staging, traffic or automatic publishing.
- The current Gateway Makefile build/run targets compile only `main.go`, omitting
  `azure.go` and `readiness.go`. Reproduced this failure with
  `go run .\cmd\hub-gateway-controller-manager\main.go --help`: undefined
  `afdProviders`, `initAzureProviders` and `registerGatewayCacheReadiness`.
- No local Docker or make executable is available. Actual container build/run
  verification must be reported as pending the new Linux CI job, not inferred
  from successful Go compilation or static configuration checks.

#### Plan and success criteria

- Phase O1 / Task O1.1: [x] Add a regression for the AFD flag's disabled default
  and define the real-image CI smoke contract before packaging changes.
  Success: preserve disabled startup and require actual image build/entrypoint
  execution, rather than a string-only Dockerfile assertion.
- Phase O2 / Task O2.1: [x] Fix Gateway Makefile build/run commands to use the
  full Go package. Add a separate Dockerfile using the existing pinned Go and
  Azure Linux distroless bases, CGO policy and nonroot UID/GID.
  Success: all package helpers are compiled; native package build/help succeeds.
- Phase O2 / Task O2.2: [x] Add dedicated image name/version variables and an
  opt-in local Gateway image target using the existing buildx conventions.
  Success: no changes to existing image bundles, registry publishing, MCR
  manifests, deployment or default feature flags. Unlike legacy image targets,
  this target must not run `go mod tidy` or rewrite dependency manifests.
- Phase O2 / Task O2.3: [x] Add a Linux image build/smoke job to the existing
  unit/integration workflow with read-only repository permissions.
  Success: CI invokes the real dedicated Makefile image target, verifies the
  configured nonroot identity and runs the image's actual entrypoint with
  `--help`, without Kubernetes/Azure access or registry publishing.
- Phase O3 / Task O3.1: [x] Update related design/runbook/breadcrumb progress
  and run local package tests, vet/lint, package build/help and available
  workflow validation.
  Success: report local evidence separately from pending container CI; do not
  claim an image was built/run locally, published, installed or traffic-ready.
- Phase O3 / Task O3.2: [ ] Execute the Linux image build/smoke job.
  Blocked locally: Docker/make are unavailable. Success requires a real image
  build, configured nonroot identity and entrypoint/AFD CLI checks in CI.

#### Decisions and implementation boundaries

- Build the complete command package in both native and Docker paths. Adding
  more individual source filenames would recreate the helper-omission bug.
- Follow existing image base versions, CGO policy and UID/GID `65532:65532`;
  keep this an independent image, not another process in the ATM manager image.
- Provide an explicit local image target rather than adding Gateway to the
  existing `image`/`push` bundles. Registry onboarding, release tags and
  multiarchitecture publication require a later explicit slice.
- The native-amd64 CI job can use Docker's existing default builder, avoiding
  unnecessary emulation/builder initialization. No Azure credentials, OIDC,
  package-write permissions, cluster configuration or live operations are needed.
- An image `--help` smoke proves packaging/linkage/entrypoint execution, not
  manager readiness, live informer behavior or an Azure datapath. Keep existing
  manager tests for initialization/readiness/disabled-mode behavior.
- Helm/RBAC/identity/discovery installation, events/metrics, ARM stager injection,
  approval automation and live/private HTTP proof remain outside this slice.

#### References and implementation grounding

- `docs/design/gep-1748-implementation-plan.md`, original Phase 6 and Task P6.2.
- `Makefile`: Gateway build/run targets and existing image/buildx variables.
- `docker/hub-net-controller-manager.Dockerfile` and
  `docker/member-net-controller-manager.Dockerfile`: image base/CGO/nonroot policy.
- `.github/workflows/unit-integration-tests.yml`: existing PR/main CI entry point.
- `.github/workflows/publish-image.yml` and `build-publish-mcr.yml`: publication
  intentionally left unchanged, including architecture-specific tags/manifests.
- No applicable domain-knowledge/specification files were available in the
  preceding approved slice; no product/domain API changes are proposed.

#### Progress and evidence

- Recorded the tests-first packaging plan before code changes. User explicitly
  approved slice O through the approval form.
- Worktree was clean at `c5cb4a8`. Confirmed the single-file compilation failure
  and the absence of local Docker/make before choosing verification boundaries.
- Added the flag-default regression and real-image CI build/smoke contract first.
  The focused Go default-flag regression passes. The standalone `actionlint`
  executable is missing; use its Go CLI distribution for workflow validation.
- Implemented full-package native build/run commands, separate Dockerfile and
  opt-in image target. The new low-level target also defaults to local Docker
  output rather than inheriting the legacy publication default. It explicitly
  selects its builder and does not run module tidy.
- The new Linux job calls that image target with the native default builder,
  checks configured UID/GID and runs the actual entrypoint with no network,
  read-only root filesystem, dropped capabilities and no new privileges.
  Captured help must expose Gateway-specific AFD flags, not merely exit zero.
- Native manager tests, vet, all-command builds and complete-package `--help`
  pass. The default flag remains false; no Kubernetes/Azure operation is needed
  for the help smoke.
- Workflow validation via Go-distributed actionlint v1.7.12 and compatible
  changed-code Go lint/golint pass. The existing workflow was stored as CRLF
  without text attributes, causing every added line to fail `git diff --check`;
  normalized this touched file to LF and preserved existing job behavior.
  No repository-wide line-ending or Git configuration was changed.
- Updated design/plan/test runbook progress. Local validation is complete;
  actual container verification remains explicitly blocked pending Linux CI.
  No dependency manifests, existing image/publication bundles, runtime SDK
  injection, feature defaults, deployment or live Azure resources changed.
- Validation evidence (passed):
  - `go test -count=1 .\cmd\hub-gateway-controller-manager`
  - `go vet .\cmd\hub-gateway-controller-manager`
  - `go build .\cmd\...`
  - `go run .\cmd\hub-gateway-controller-manager --help`
  - `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 .\.github\workflows\unit-integration-tests.yml`
  - `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run --no-config --enable-only=nilerr,goconst,whitespace,gocyclo,gosec,errorlint,prealloc,unused --new-from-rev=HEAD .\cmd\hub-gateway-controller-manager`
  - `go run golang.org/x/lint/golint@latest -set_exit_status .\cmd\hub-gateway-controller-manager`
  - `git diff --check`
- Packaging implementation is ready for CI, not yet container-verified. O3.2
  remains pending; the user explicitly requested committing this slice.
  Pinned v1 lint, envtest and live datapath proof limitations from earlier
  slices remain unchanged.

### P. Disabled-by-default Gateway Helm installation

#### Requirements and additional user comments

- User requested continuing after committed image packaging slice O (`83196fb`).
- User clarified: "for the delivery model stay close to the current repo and do
  not invent if it can be avoided (NIH)". Reuse the existing Makefile/buildx,
  per-component Dockerfile/chart, image.repository/image.tag and GHCR/MCR
  publication conventions; do not introduce a parallel delivery framework.
- Add an independent hub Gateway manager chart for the current observer/owned
  cleanup runtime, not an automatically provisioning ingress product.
- AFD remains disabled by default. Disabled installation must not require Azure
  configuration or Gateway API/Fleet CRDs, create a GatewayClass, initialize
  Gateway informers or grant cluster-wide Gateway/Fleet permissions.
- Require an explicitly selected image repository/tag: this branch's Gateway
  image is not yet automatically published. Do not invent a published default.
- Enabled installation uses a platform-owned existing cloud-config Secret,
  configured resource group and optional GatewayClass. Never render credentials
  into Helm values/Secrets or require customer ServiceImport writes.

#### Plan and success criteria

- Phase HC1 / Task HC1.1: [x] Add opt-in Go table-driven tests invoking real Helm
  rendering/lint first, without Kubernetes or Azure access.
  Success: cover disabled isolation, enabled required configuration, exact flags,
  probes, image identity, Secret references, conditional GatewayClass, scoped RBAC,
  existing ServiceAccount reuse and identity annotations/pod labels.
- Phase HC2 / Task HC2.1: [x] Add the separate chart with existing naming/label
  patterns, explicit image selection, replicas/resources and scheduling options.
  Success: Deployment flags/probes agree with the manager, and disabled mode has
  only the Deployment, ServiceAccount and namespaced leader-election permissions.
- Phase HC2 / Task HC2.2: [x] Add enabled-only read/watch, status/finalizer and
  metadata permissions for the current controllers; support a platform Secret
  and optional class using `networking.fleet.azure.com/afd`.
  Success: ServiceImport/InternalServiceExport/ReferenceGrant/Namespace access is
  read-only; no copied ATM, CRD-management, backend-write or secret-read RBAC.
- Phase HC2 / Task HC2.3: [x] Document operator-managed Azure identity, image,
  CRD and member-discovery prerequisites, upgrade/uninstall safety and the current
  non-provisioning limitation.
  Success: no image publication, Azure role assignment, member-chart behavior
  change or automatic PLS approval is implied by chart installation.
- Phase HC3 / Task HC3.1: [x] Add a read-only chart CI job and run native Helm
  lint/render tests, scoped Go vet/lint and workflow validation.
  Success: chart behavior is verified with real Helm locally; image execution,
  live Kubernetes RBAC/informers and private HTTP proof remain separate gates.

#### Decisions and implementation boundaries

- Use the release namespace consistently for Deployment, ServiceAccount,
  leader-election namespace and namespaced role/binding. Documentation uses the
  reserved `fleet-system` namespace; no Namespace resource is created by the chart.
- Keep Azure cloud config in an existing operator-managed Secret mounted only
  when AFD is enabled. The controller needs no Kubernetes Secret API permission.
- Expose ordinary ServiceAccount annotations and pod labels for existing identity
  projection mechanisms, not a second Azure credential/configuration API.
  Operators remain responsible for Azure auth configuration/federation/roles.
- Create the optional GatewayClass only when AFD is enabled; require the Gateway
  API v1.2.1 and current Fleet CRDs before enabled installation. Never install CRDs,
  Gateways, HTTPRoutes, Services/Ingresses or public cloud resources implicitly.
- Validate boolean configuration types so a quoted `"false"` cannot accidentally
  enable controllers/RBAC. Keep the existing manager's feature default unchanged.
- Do not modify legacy hub/member charts. Explicit member
  `--enable-private-link-origin-discovery` configuration remains an operator
  prerequisite. The existing member chart already exposes
  `enablePrivateLinkOriginDiscovery`; this hub chart does not enable it.
- Use `azure-fleet-afd` as the default GatewayClass name, matching the existing
  design and configuration guide rather than introducing another default.
- This chart does not inject the SDK stager, create AFD resources or publish a
  programmed address. Platform/manual-approval/lifecycle/live-proof gates remain.
- Delivery grounding: existing `make image`/`make push` drive per-component
  buildx targets; `publish-image.yml` and `build-publish-mcr.yml` already own
  GHCR and MCR delivery. Future Gateway publication should extend those existing
  paths, not create a new registry/release workflow. Keep those publication
  changes outside this approved chart slice.
- The chart follows existing image.repository/image.tag, fullname/selector labels,
  resources/scheduling and cloud-config mount conventions. Explicit image
  selection and an existing Secret are deliberate current-slice safety limits:
  publication is not yet wired, and credentials must not enter new Helm values.
  Do not present these limits as a new permanent image/credential delivery model.

#### References and implementation grounding

- `docs/design/gep-1748-implementation-plan.md`, Tasks 6.1/6.2 and P6.2.
- Existing `charts/hub-net-controller-manager` values, helpers, Deployment,
  ServiceAccount and RBAC: reuse structure, not its broad networking permissions.
- Gateway/GatewayClass/HTTPRoute annotations and status/finalizer implementations:
  actual current API requirements; Namespace and Fleet dependencies are read-only.
- `cmd/hub-gateway-controller-manager/main.go` / `azure.go` / `readiness.go`:
  flags, existing config-file auth, disabled-mode isolation and probe ports.
- `pkg/common/objectmeta/objectmeta.go`: exact controller identity.
- Native Helm v3.17.3 is available; no Helm tool installation is needed locally.
- No applicable domain-knowledge/specification files were available in earlier
  approved slices; no product/domain API change is proposed.

#### Progress and evidence

- Recorded this tests-first plan before code changes. User explicitly approved
  slice P through the approval form.
- Worktree was clean at `83196fb`. Previous container-image and pinned-CI-lint
  blockers remain unrelated to native chart rendering verification.
- Tests first: the focused disabled test compiled and failed because the chart
  directory did not exist. Added the independent chart templates/schema; the
  initial full `go test -count=1 -tags=helm .\test\helm` run passed, including strict
  Helm lint. Extending coverage for disabled-but-configured Azure values, disabled
  leader election, alternate namespaces, selected Secret keys and immutable
  selector labels before finalizing the chart.
- Verified `Azure/setup-helm@v5.0.1` exposes the `version` input and uses Node 24.
  Chart CI will pin Helm v3.17.3, matching the native renderer used locally.
- Course correction from source inspection: the existing member chart already
  passes `enablePrivateLinkOriginDiscovery` to the binary and conditionally mounts
  Azure configuration. Installation documentation will link that existing guide,
  not describe member-chart wiring as missing.
- Extended tests passed after correcting the default class name to the existing
  `azure-fleet-afd` contract. The intermediate red run failed only that class-name
  assertion. `goimports -local go.goms.io/fleet-networking -w
  .\test\helm\gateway_chart_test.go` and the full tagged Helm suite passed.
  HC1.1/HC2.1/HC2.2 are complete; proceeding to installation documentation and CI.
- Added the chart README with explicit image selection, disabled/enabled examples,
  existing Secret and identity projection, current provider read/delete behavior,
  member-discovery prerequisites, asynchronous manual approval and safe removal.
  Updated the configuration guide, design/implementation plan and test guide.
  HC2.3 is complete; no runtime, publication or legacy-chart behavior changed.
- Added a `gateway-chart` job using Helm v3.17.3 and the real render/lint suite.
  It overrides inherited OIDC permissions with `contents: read`, needs no cloud
  credentials/cluster, and preserves existing image/test jobs.
- Scoped compatible lint initially reported repeated constants, excessive
  complexity in the combined disabled/pod-policy test and the variable-argument
  subprocess check. Factored shared literals, split independent probe/security
  assertions into normal tests, and documented the subprocess false positive:
  fixed `helm` executable, test-owned arguments, no shell. No lint rules were
  disabled. Added explicit seccomp/capability assertions while splitting tests.

#### Verification and before/after

- Before: no independent Gateway installation chart or native chart contract
  suite; packaging provided a local image target and pending image CI proof.
- After: a separate chart follows existing naming/image/resources/scheduling and
  cloud-config mount conventions. Disabled mode has no Azure mounts, class or
  Gateway/Fleet cluster permissions. Enabled rendering requires a platform Secret
  and group, with scoped permissions and the existing `azure-fleet-afd` identity.
- Passed `go test -count=1 -tags=helm .\test\helm`, including real Helm rendering
  and `helm lint --strict` through the suite.
- Passed `go vet -tags=helm .\test\helm`.
- Passed `go run golang.org/x/lint/golint@latest -set_exit_status .\test\helm`.
- Passed `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
  run --no-config --build-tags=helm
  --enable-only=nilerr,goconst,whitespace,gocyclo,gosec,errorlint,prealloc,unused
  --new-from-rev=HEAD .\test\helm` with zero issues.
- Passed `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
  .\.github\workflows\unit-integration-tests.yml` and `git diff --check`.
- Existing publishing workflows, legacy charts and runtime provider injection
  are unchanged. No dependency manifests changed, so no dependency tidy/install
  was needed. No cluster/Azure resources were created or modified.
- All HC tasks are complete locally. Actual Linux image execution, pinned-v1
  CI lint, live installation/authorization/informers, Azure identity and private
  HTTP proof remain unverified external gates. Image publication and full P6
  remain open; rendering is not deployment or ingress proof.

### Q. Gateway delivery through existing image publication paths

#### Requirements and additional user comments

- User requested completing/committing the outstanding slice before continuing.
  Slice P is committed as `1c89d33` (`feat: add disabled-by-default Gateway Helm
  chart`); the worktree was clean after that commit.
- Preserve the user's no-NIH delivery constraint: extend the existing Makefile,
  GHCR workflow and MCR architecture/manifest workflow, not a new release system.
- Keep AFD disabled, runtime staging uninjected and chart installation opt-in.
  Publishing an image does not enable an Azure provisioning path.
- No registry login, push, workflow dispatch or live infrastructure operation is
  authorized or needed for this source-code slice.

#### Plan and success criteria

- Phase IP1 / Task IP1.1: [x] Add dry-run assertions to the existing Linux Gateway
  image CI job before changing publication recipes.
  Success: verify Gateway participation in both ordinary image/push bundles,
  local versus registry output, explicit tag/architecture overrides, and retained
  independent local-image behavior without executing a push.
- Phase IP2 / Task IP2.1: [x] Extend the existing `image`/`push` Makefile bundles
  with the existing Gateway target and make its output obey the caller.
  Success: existing components keep their recipes; `make image` stays local,
  `make push` publishes Gateway alongside the other components, and the dedicated
  image wrapper remains local. No duplicate build framework or dependency changes.
- Phase IP2 / Task IP2.2: [x] Extend the existing MCR amd64/arm64 version overrides
  and manifest-bundle steps for the Gateway component.
  Success: architecture images have distinct existing-style suffixes and the
  final tag combines both while retaining attestations through the existing
  buildx imagetools pattern. Older release tags lacking Gateway publication must
  not acquire failing pulls of nonexistent Gateway images.
- Phase IP2 / Task IP2.3: [x] Update image/chart/test/design documentation to
  describe existing publication-path integration, not verified published assets.
  Success: explicit operator image selection remains truthful; no new registry,
  release workflow, automatic install or Azure enablement is implied.
- Phase IP3 / Task IP3.1: [x] Validate touched workflows, dry-run contracts where
  GNU make is available, chart regressions and whitespace; record external limits.
  Success: source contracts are verified independently from actual container
  builds, registry publication and multiarchitecture execution. Do not claim the
  existing container/pinned-CI-lint blockers or full P6 are complete.

#### Decisions and implementation grounding

- Existing `publish-image.yml` already invokes `make push`; prefer extending its
  existing Makefile membership rather than adding another GHCR job/workflow.
- Existing `build-publish-mcr.yml` supplies per-component version overrides for
  both architectures and explicitly combines their manifests. Extend that same
  pattern, including safe handling of a selected historical release's contents.
- Reuse the existing Linux `gateway-image` job for GNU make dry-run regressions,
  followed by its actual container build/smoke. Do not add a custom delivery
  framework, new test-runner dependency or registry credentials to PR validation.
- Keep unrelated publishing-workflow issues and legacy chart behavior out of
  scope. Do not alter the manager's feature default or inject the SDK stager.

#### References

- Related slices O/P in this breadcrumb and their commits `83196fb`/`1c89d33`.
- `Makefile`: existing image/push/buildx targets and component name/version inputs.
- `.github/workflows/publish-image.yml`: existing GHCR `make push` integration.
- `.github/workflows/build-publish-mcr.yml`: release checkout, per-architecture
  builds and explicit attestation-preserving manifest bundles.
- `.github/workflows/unit-integration-tests.yml`: existing Gateway Linux image CI.
- `docs/design/gep-1748-implementation-plan.md`, Tasks 6.1/6.2 and P6.2.
- Rechecked `.github/.copilot/domain_knowledge` and specifications: no files
  present. No domain API change is proposed.

#### Progress

- Plan recorded before implementation. User explicitly approved slice Q.
- Native `make --version` failed because GNU make is absent. Git Bash is
  available, but neither it nor an existing compiler provides make. Seeking an
  isolated portable GNU make rather than changing system/global tooling; Docker
  and actual registry/container execution remain external proof requirements.
- Restored GNU Make 4.4.1 from the Git for Windows SDK into the session tools
  directory, verified against Git blob
  `1f1965659e7cd8164ad54646cfbb0242f59f74a5`, and ran it using existing Git Bash.
  No global installation/PATH change or repository dependency was added. An
  earlier GnuWin32 download returned non-archive content and was not executed.
- Added the delivery-contract step first. Executing its actual shell body with
  GNU make failed against the pre-integration bundles as expected; Gateway is not
  yet a member of either bundle. Test output now identifies the missing component
  explicitly and enforces output-mode failures independently of shell negation.
- Extended existing Makefile bundles and removed the Gateway low-level target's
  forced local output so `make push` can select registry output. The dedicated
  local wrapper remains unchanged in behavior. Other component recipes remain
  intact; no module tidy prerequisite or new publishing workflow was added.
- Extended MCR architecture tags and imagetools steps. Selected-release discovery
  inspects that release's actual push recipe, not just Dockerfile existence, so
  older tags remain usable. Invalid releases without a push target fail clearly.
- Executed the exact CI delivery-step body through GNU Make 4.4.1: passed all
  component memberships, local/registry output, amd64/arm64 tag overrides and
  independent local image assertions. Executed the actual MCR prepare script
  against isolated Git tree fixtures: current release=true; old release with
  standalone Gateway target=false; old release without Gateway=false; missing
  push target=explicit error. Temporary plans were removed after each run.
- Actionlint initially reported only the existing private 1ES runner labels,
  which its default public-runner catalog does not know. Validate with a
  session-local config declaring those exact existing labels, not by changing
  runners or ignoring label validation.
- Actionlint passed for both touched workflows with the exact existing runner
  labels declared in that temporary config. No validation rule or repository
  runner setting was disabled/changed.
- Updated chart values comments, chart README, test runbook and design/plan to
  describe existing-path image publication wiring while retaining explicit image
  selection and separating verified dry runs from unverified builds/publication.
  IP2 is implemented; final regression/diff review remains.

#### Verification, changes and before/after

- Before: only the dedicated local Gateway image wrapper was wired; ordinary
  image/push bundles and architecture publication omitted this component.
- After: `Makefile` includes the existing Gateway target in both bundles.
  `publish-image.yml` needs no changes because it already runs `make push`.
  `build-publish-mcr.yml` uses the existing architecture-tag/imagetools pattern
  with selected-release membership checks. The current feature flag, existing
  component recipes, registry identities and release triggers are unchanged.
- `unit-integration-tests.yml` reuses the existing Gateway image job for delivery
  dry runs, then its actual image smoke. Generated plans are removed on exit.
  No new workflow/test framework or package dependency was introduced.
- Passed exact CI-body execution with GNU Make 4.4.1 using the existing Git Bash.
  Both ordinary bundles retained all five components, used the required output
  modes, respected amd64/arm64 version overrides and preserved the dedicated
  wrapper's local behavior. Separate mutated Makefile fixtures confirmed failures
  for missing Gateway membership, local-output push and registry-output local
  wrapper; this verifies the assertions reject broken behavior, not just pass it.
- Passed the actual MCR prepare-step script against current, old-with-standalone
  Gateway-target, old-without-Gateway and invalid-no-push Git-tree fixtures.
  No fixture commit, release tag, registry call or repository ref was created.
- Passed `go test -count=1 -tags=helm .\test\helm`, including strict Helm lint.
- Passed `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
  -config-file <session-local-runner-config>
  .\.github\workflows\unit-integration-tests.yml
  .\.github\workflows\build-publish-mcr.yml`. The temporary config declares only
  the existing `1ES.Pool=1es-aks-fleet-networking-pool-ubuntu` and
  `1ES.Pool=1es-aks-fleet-networking-pool-ubuntu-arm64` self-hosted labels.
- Passed `git diff --check` and scoped source review. No Go source or dependency
  manifests changed in slice Q; prior chart vet/lint results remain applicable.
- Updated chart README/values comments, design/implementation plan and test guide
  to distinguish publication wiring from actual publication. Explicit chart image
  selection remains required until the chosen image/tag is independently verified.
- All IP tasks are source-complete and locally verified within this boundary.
  Actual Docker build/smoke, registry publication, multiarchitecture execution,
  pinned-v1 CI lint and live installation/private HTTP remain external gates.
  No AFD provisioning is enabled and full P6 remains incomplete.

### R. Current origin-observation events and metrics

#### Requirements and additional user comments

- User requested continuing after slice Q was committed as `d10607c`.
  Confirmed a clean worktree before planning this step.
- Advance the operational-visibility portion of P6.2 without enabling SDK staging,
  traffic, address publication or `Programmed=True`.
- Follow existing repository mechanisms: client-go EventRecorder supplied by the
  manager, Prometheus collectors registered with controller-runtime, and shared
  `fleet`/`networking` metric namespace/subsystem constants.
- Bound this slice to the currently injected origin observer. Cleanup/staging
  operation instrumentation, approval-wait duration and full P6.2 remain later
  work; do not expose simulated provisioning metrics for an uninjected provider.

#### Plan and success criteria

- Phase OV1 / Task OV1.1: [x] Add table-driven event/metric regressions first,
  reusing existing Gateway fixtures, fake observer/recorder and conflict interceptors.
  Success: cover pending asynchronous approval, converged origins without ingress
  readiness, rejected/disconnected/failed origins and observation errors; unchanged
  polls, stale intent and unsuccessful status patches emit no misleading events.
  One status retry must not multiply provider calls, samples or events.
- Phase OV2 / Task OV2.1: [x] Supply the standard EventRecorder from enabled manager
  setup and emit fixed, actionable Gateway events only after a current observation
  is reflected in a successful status patch.
  Success: no events inside conflict-retry callbacks, no event for discarded or
  overridden observations, and no event named/worded as successful ingress
  programming. Identical polls do not repeat transition events; the standard
  client-go correlator supplies additional aggregation/rate limiting.
- Phase OV2 / Task OV2.2: [x] Record logical origin-observer call count and duration,
  once per actual provider invocation, outside status retries.
  Success: metrics distinguish success/error/throttled with a fixed outcome-label
  set, preserve wrapped SDK throttling classification and skip uninvoked providers.
  No names, namespaces, UIDs, ARM IDs, exception text or request IDs become labels.
  These are logical provider operations, not individual ARM HTTP request counts,
  current inventory, approval-wait time or dataplane health.
- Phase OV2 / Task OV2.3: [x] Update enabled-only chart Event permissions and exact
  render regressions; document the event/metric meanings and current limitations.
  Success: namespaced Gateway events work outside fleet-system, while disabled
  mode gains no cluster permission and backend/Secret permissions remain unchanged.
  New event text contains no raw SDK errors, credentials or resource identifiers.
- Phase OV3 / Task OV3.1: [x] Run affected Gateway/manager tests, chart contracts,
  scoped formatting/vet/lint, build and final diff review.
  Success: existing approval polling, status conflict/stale-intent handling,
  ownership/readiness and disabled-mode contracts are preserved. Record envtest/
  live event delivery and Azure proof separately rather than claiming them.

#### Decisions and implementation boundaries

- Reuse `mgr.GetEventRecorderFor` and existing Prometheus dependencies; do not
  introduce a telemetry service/interface, custom broadcaster, per-Gateway memory
  cache, annotation, CRD or configuration surface.
- Events describe origin observation only: asynchronous PLS-owner action, read
  failure, observed problematic origin state or observed origin convergence.
  Successful origin convergence explicitly does not imply programmed ingress.
- Use the existing persisted status transition and current UID/generation/model
  checks to suppress unchanged/discarded observations. Never emit a candidate
  event from a failed retry, and preserve existing returned errors.
- Count/time only the actual observer call after trusted hub-identity checks.
  Metrics may count a real read subsequently discarded for changed intent; they
  must not present that read as the current Gateway's ready state.
- Add only enabled cluster Event create/patch permission, because Gateways can
  live outside the leader-election namespace. Existing namespaced election Event
  permissions, feature gating and Fleet-input read-only boundaries remain intact.
- Fixed event messages point operators to Gateway status and the manual approval
  responsibility rather than copying raw SDK payloads into another surface.
- Do not change existing status-message content, retry intervals, Azure operation
  budgets, provider injection or legacy controller telemetry in this slice.

#### References

- This breadcrumb slices N/P/Q: current readiness, scoped chart and delivery.
- `docs/design/gep-1748-implementation-plan.md`, Task 6.3 and P6.2.
- `pkg/controllers/hub/trafficmanagerprofile/controller.go` and hub manager setup:
  EventRecorder, controller-runtime registry and common metric naming patterns.
- `pkg/common/metrics/metrics.go`: shared namespace/subsystem constants.
- `pkg/controllers/hub/gateway/{controller,observation}.go`: current-intent checks,
  external reads outside conflict retries and observation/status boundaries.
- Existing `observation_test.go` fixtures/interceptors, manager startup tests and
  exact Helm RBAC contracts: extend rather than adding another test framework.
- Rechecked domain-knowledge/specification paths: no files present. No product
  configuration or domain contract changes are proposed.

#### Progress

- Plan recorded before implementation. User explicitly approved slice R.
- Existing `azureerrors.IsThrottled` already preserves wrapped SDK 429
  classification; reuse it rather than adding another Azure-error classifier.
- Tests first: new Gateway tests failed to compile for the absent Recorder field;
  the manager test failed for missing recorder setup; the exact enabled chart
  matrix failed for missing cluster Event create/patch permission.
- Added standard recorder injection, transition-scoped post-patch events and
  logical observer call/duration collectors. Retained existing status contents,
  Azure errors, approval polling and provider wiring. Staging/withdrawal status
  that overrides observation cannot produce an observation event.
- Gateway and manager package tests passed. The full Helm suite passed with
  enabled cluster Event permissions and unchanged disabled resource/RBAC shape.
  Additional tests verify overwritten staging status suppresses events and that
  skipped observer calls change no metric outcome series.
- Compatible scoped lint identified repeated test literals and table capacity.
  Reused the existing Gateway resource constant, named the fixed outcome-label
  set and expressed all event-state cases as a fixed table. No lint rule changed.
- Added the chart event/metric reference and corresponding design/plan/test-guide
  updates. Documented logical-call versus HTTP retry counts, final 429 semantics,
  no resource/error labels, native best-effort event delivery and remaining broader
  operation/live-proof work. OV2.3 is complete.
- Final review added a deterministic 250ms-to-0.25s histogram assertion and found
  a contradictory provider result could emit convergence while status said the
  profile was missing. The regression failed as expected; event classification
  now gives profile absence precedence, matching the existing status contract.

#### Verification, changes and before/after

- Before: observer diagnostics were available in pending status but there were
  no Gateway observation events or logical observer call/duration metrics.
- After: `observability.go` registers existing-pattern fixed-outcome collectors
  and supplies sanitized observation events. `observation.go` instruments only
  actual completed provider calls and reports whether its status projection was
  current/applied. `controller.go` captures successful, changed observation status
  and emits only after the retry operation finishes successfully.
- Manager setup supplies the standard recorder. Existing startup test doubles
  assert its selection without controller-name guard bypasses or remote API calls.
  Enabled chart RBAC adds only core Event create/patch permission, with exact
  enabled/disabled render assertions. No new dependency, flag or Azure permission
  was introduced; production SDK staging remains uninjected.
- Passed `go test -count=1 -skip '^TestAPIs$'
  .\pkg\controllers\hub\gateway... .\cmd\hub-gateway-controller-manager`.
  Gateway, GatewayClass, model, resolver and manager regressions passed.
- Passed `go test -count=3 -run '^TestOriginObservation'
  .\pkg\controllers\hub\gateway`: repeated telemetry tests preserve per-call
  deltas without resetting shared collectors or repeating external reads in
  status retries.
- Passed `go vet .\pkg\controllers\hub\gateway
  .\cmd\hub-gateway-controller-manager` and `go build .\cmd\...`.
- Passed `go test -count=1 -tags=helm .\test\helm` (including strict Helm lint)
  and `go vet -tags=helm .\test\helm`.
- Passed `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
  run --no-config
  --enable-only=nilerr,goconst,whitespace,gocyclo,gosec,errorlint,prealloc,unused
  --new-from-rev=HEAD .\pkg\controllers\hub\gateway
  .\cmd\hub-gateway-controller-manager` with zero issues.
- Passed `go run golang.org/x/lint/golint@latest -set_exit_status
  .\pkg\controllers\hub\gateway .\cmd\hub-gateway-controller-manager`.
- Applied repository-local `goimports` grouping to all affected Go files; scoped
  diff/whitespace review passed. New tests cover observed convergence and manual
  approval transitions, absence precedence, problematic states, sanitized event
  payloads, unchanged polls, stale intent, status failure/conflict, overridden
  staging status, fixed labels, wrapped throttling, skipped calls and exact
  duration units.
- Updated the chart diagnostics reference, design/plan and test guide. Telemetry
  does not alter status messages, retry timing, ownership, readiness, addresses,
  provider injection or the disabled default.
- OV tasks are complete within this source/unit/render boundary. Envtest/live
  API-server Event delivery and native rate-limit behavior, Docker/registry
  execution, pinned-v1 CI lint and private HTTP remain unverified external gates.
  Broader staging/cleanup/approval-wait telemetry and full P6.2 remain open.

### S. Owned-profile finalization diagnostics

#### Requirements

- Continue P6.2 with diagnostics for the already-injected owned-profile cleanup
  path; preserve ownership checks, finalizer retention, retries and disabled
  provisioning. Slice R was committed as `5ee0951`.
- Reuse the existing EventRecorder, Prometheus registry and enabled chart Event
  permissions. Do not introduce another delivery or telemetry framework.
- Count completed logical DeleteProfile calls, not ARM requests, deleted
  resources or Kubernetes finalization success. Distinguish pending deletion,
  observed absence, ownership conflict, throttling and other errors with fixed
  labels only.

#### Additional comments from user

- User requested "continue implementation" after committing slice R.
- Existing instruction to stay close to repository mechanisms remains in force.

#### Plan

**Phase CD1: Tests first**

- [x] Task CD1.1: Extend existing cleanup table tests and metric/event helpers for
  pending, absent, failed, wrapped-throttled and ownership-conflict results.
  Success: tests distinguish call outcomes and sanitized event messages without
  changing cleanup return values or finalizer expectations.
- [x] Task CD1.2: Cover skipped calls, metadata patch failures/conflicts, repeated
  polls and stale/replaced Gateway identity during finalizer removal.
  Success: prerequisites produce no provider-call samples; metadata retries do
  not replay or recount Azure calls; failed/skipped removal produces no
  completion event.

**Phase CD2: Instrument existing finalization**

- [x] Task CD2.1: Add logical cleanup call count/duration collectors alongside
  the observation collectors. Record only around the actual DeleteProfile call,
  including its SDK retry time, with fixed outcomes `pending`, `absent`,
  `ownership_conflict`, `throttled` and `error`.
  Success: one counter/histogram sample per completed call, seconds units and no
  Gateway/resource IDs, names or raw errors in labels.
- [x] Task CD2.2: Emit a sanitized warning for provider cleanup failure, with
  actionable ownership-conflict wording, using native recorder aggregation.
  Emit normal completion only after this reconcile successfully removes the
  matching deleting Gateway's cleanup finalizer following observed absence.
  Reset the completion candidate on each metadata retry.
  Success: no premature completion on pending Azure deletion, failed patch,
  not-found/replaced Gateway or another writer's removal; no new custom cache,
  status condition or pending-event flood.

**Phase CD3: Document and verify**

- [x] Task CD3.1: Update chart diagnostics, design progress and focused test
  guidance; run affected Gateway tests, goimports, vet, golint and compatible
  changed-code lint. Review whitespace and finalizer/error behavior.
  Success: targeted checks pass and proof limits are explicit; no claim of live
  Event delivery, durable audit, ARM-request metrics or full P6.2 completion.

**Completion checklist:** CD1.1, CD1.2, CD2.1, CD2.2 and CD3.1 must all be checked
with recorded evidence. No implementation starts before explicit plan approval.

#### Decisions

- Instrument the logical controller/provider seam, outside metadata conflict
  retries, rather than duplicating SDK-level request instrumentation.
- Absence includes an already-missing profile; it does not prove this controller
  submitted a delete. A successful provider call does not prove finalizer
  persistence. Keep these boundaries explicit in metric help and event wording.
- Failure events remain best effort and use fixed messages, not SDK payloads.
  Repeated failures rely on existing recorder aggregation/rate limiting.
- No events for missing cleanup prerequisites in this slice; existing explicit
  reconcile errors remain intact. No new Azure writes, stager injection, RBAC,
  dependencies, or changes to the 30-second pending retry.

#### Implementation Details

- User explicitly approved slice S through the plan form. Changes are confined to Gateway cleanup/
  observability code and tests, directly related diagnostics documentation and
  this breadcrumb.
- Added tests first for bounded outcomes, sanitized events, exact duration
  units, skipped prerequisites, pending polls and finalizer-persistence guards.
  Corrected test syntax/interceptor signatures before confirming the expected
  red result: focused tests fail because `recordProfileCleanup` does not exist.
  Focused cleanup tests then passed after implementation; the cleanup and origin
  diagnostics regressions also passed together with `-count=3`.
- Added the collectors/classification in `observability.go` and instrumented
  only the actual provider call in `cleanup.go`. Completion candidates reset
  inside metadata retries; successful patches alone authorize completion events.
  Wrapped ownership conflicts/throttling and inconsistent absence-plus-error
  results retain error precedence and existing finalizer semantics.
- Gateway subtree/manager tests and vet passed. Initial compatible changed-code
  lint reported zero issues, and golint passed. No provider writes, injection,
  ownership checks, finalizer retry timings or RBAC changed.

#### Changes Made

- Implemented bounded cleanup telemetry and native-recorder diagnostics with
  regressions. Updated the chart diagnostics reference, design/implementation
  progress and focused test guide. No new chart permissions or dependencies.

#### Final verification and proof limits

- Passed `go test -count=1 -skip '^TestAPIs$'
  .\pkg\controllers\hub\gateway... .\cmd\hub-gateway-controller-manager`.
- Passed `go test -count=3 -run
  'TestGatewayFinalizesOwnedProfileWithoutClass|TestProfileCleanup|TestOriginObservation'
  .\pkg\controllers\hub\gateway`, including pending polls, absence/error
  precedence, completion suppression, successful metadata conflict retry,
  exhausted conflicts and no extra call after finalizer removal.
- Passed `go vet .\pkg\controllers\hub\gateway
  .\cmd\hub-gateway-controller-manager`.
- Passed `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
  run --no-config
  --enable-only=nilerr,goconst,whitespace,gocyclo,gosec,errorlint,prealloc,unused
  --new-from-rev=HEAD .\pkg\controllers\hub\gateway` with zero issues.
- Passed `go run golang.org/x/lint/golint@latest -set_exit_status
  .\pkg\controllers\hub\gateway`.
- Applied repository-local goimports grouping; final formatting listing was
  empty, production diff review and `git diff --check` passed.
- CD1-CD3 are complete within the source/unit boundary. No envtest assets,
  live API-server Event delivery/rate limiting, Azure cleanup, container/registry,
  pinned-v1 CI lint or private HTTP proof was performed in this slice. Existing
  external gates remain; staging/approval-wait telemetry and full P6.2 stay open.

#### Before/After Comparison

- Before: origin observation has diagnostics; owned-profile finalization has
  explicit errors and retries but no dedicated logical-call telemetry.
- After: bounded cleanup outcomes and post-persistence completion
  diagnostics, preserving existing cleanup semantics.

#### References

- `pkg/controllers/hub/gateway/cleanup.go`: existing uncached hub identity,
  logical provider call and metadata-only conflict retries.
- `pkg/controllers/hub/gateway/cleanup_test.go`: existing finalizer, missing
  prerequisite, ownership-conflict and concurrent metadata regression table.
- `pkg/controllers/hub/gateway/observability.go`: existing native recorder,
  registry, bounded outcomes and duration-seconds conventions.
- `pkg/providers/azure/frontdoor/cleanup.go`: observed-absence contract,
  ownership verification and asynchronous deletion.
- `docs/design/gep-1748-implementation-plan.md`, P6.2: remaining operations
  diagnostics and explicit live-proof boundaries.
- No domain-knowledge or specification files were found in their designated
  directories for this slice; no specification selection is required.

### T. Disabled-graph and endpoint-suspension call diagnostics

#### Requirements

- Continue P6.2 after the slice S cleanup diagnostics commit `289dfb4`.
- Cover the existing injected StageDisabledGraph and both SuspendEndpoint call
  sites: confirmed empty intent and positively revalidated ReferenceGrant denial.
- Preserve the production gate: the manager still must not inject the SDK graph
  stager. Diagnostics are not permission to provision or enable traffic.
- Reuse the existing Prometheus registry and EventRecorder, not another delivery,
  monitoring, state-cache or configuration mechanism.

#### Additional comments from user

- User requested "continue implementation" after committing slice S.
- Retain the existing repository-first delivery/implementation constraint.

#### Plan

**Phase GD1: Tests first**

- [x] Task GD1.1: Add table-driven logical-call telemetry tests for graph staging,
  empty-intent suspension and denied-reference suspension, covering success,
  ordinary failure, wrapped throttling and wrapped ownership conflict.
  Success: exactly one counter/histogram sample per completed actual call;
  fixed labels and exact duration-seconds units are verified.
- [x] Task GD1.2: Extend existing guards/conflict/stale-intent regressions to
  prove skipped calls produce no samples/events and status retries never replay
  calls or duplicate samples. Verify failed-call messages omit raw SDK/resource
  payloads, and provider errors/results remain unchanged.
  Success: metrics describe attempted logical operations even if later status
  publication fails or becomes stale; no readiness or enforcement claims.

**Phase GD2: Instrument existing call sites**

- [x] Task GD2.1: Add count/duration collectors with only fixed `operation` and
  `outcome` labels. Operation values are `stage_disabled_graph`, `suspend_empty`
  and `suspend_denied`; outcome values are `success`, `error`, `throttled` and
  `ownership_conflict`.
  Success: time only the actual provider invocation, outside metadata/status
  retries and after existing authorization/ownership prerequisites.
- [x] Task GD2.2: Add sanitized native-recorder warnings for failed staging or
  suspension calls, with actionable ownership-conflict wording. Share error
  classification with existing cleanup only where it preserves its outcomes.
  Success: warnings describe failed attempts, not current persisted status,
  successful enforcement or readiness; native aggregation/rate limiting remains
  responsible for repeated failures. No success/pending events or new state.

**Phase GD3: Document and verify**

- [x] Task GD3.1: Update the chart diagnostics reference, design progress and
  focused test guidance. Run affected Gateway/manager tests, goimports, vet,
  golint and compatible changed-code lint; review the final diff.
  Success: no return/error, status, retry, ownership, injection, RBAC or dependency
  behavior changes, and proof limits are documented honestly.

**Completion checklist:** GD1.1, GD1.2, GD2.1, GD2.2 and GD3.1 must all pass with
recorded evidence. Implementation starts only after explicit plan approval.

#### Decisions

- Success is a nil-error logical provider return, including pending or absent
  resources; it is not ARM request success, inventory convergence, traffic
  readiness or proof that revocation has taken effect.
- Count failed calls even if later Kubernetes status cannot be persisted. Warning
  events describe that attempt and do not depend on status retries. Keep the
  existing origin transition-event and cleanup completion-event semantics intact.
- Use fixed operation labels to distinguish empty withdrawal from authorization
  denial without including any Gateway, route, Azure resource or error identity.
- Approval-wait durations and live traffic/enforcement proof remain separate:
  this slice introduces no durable timestamps, gauges or write-gate bypass.

#### Implementation Details

- User explicitly approved slice T through the plan form. Scope: existing Gateway staging/withdrawal/
  observability code and tests, directly related documentation and this
  breadcrumb.

#### Changes Made

- Added tests first for all three call paths, four outcome classes, native
  failure events, repeated polls, status conflicts/failures, stale results,
  missing prerequisites and exact duration units. Existing staging/empty/
  denial ownership-guard matrices also assert no operation samples.
- Confirmed the expected red result with `go test -count=1 -run
  '^TestGraphOperation' .\pkg\controllers\hub\gateway`: missing
  `recordGraphOperation`.
- Instrumented exactly the existing three external call sites after their
  prerequisites, outside status retries. Added the two collectors and sanitized
  attempt warnings to `observability.go`; cleanup now reuses the same error
  classifier while retaining its pending/absent outcomes and error precedence.
- Focused graph/guard/cleanup regressions passed, followed by the full Gateway
  subtree and manager tests plus vet. Repeated all telemetry tests with
  `-count=3`; they passed without resetting shared collectors.
- Initial compatible lint caught repeated outcome/test-label strings. Reused
  existing operation outcome constants and clarified the Azure 429 test name;
  no lint rules were disabled.
- Updated chart diagnostics, design/implementation progress and the focused test
  guide with fixed labels, attempt-event semantics and the uninjected SDK gate.

#### Final verification and proof limits

- Passed `go test -count=1 -skip '^TestAPIs$'
  .\pkg\controllers\hub\gateway... .\cmd\hub-gateway-controller-manager`.
- Passed `go test -count=3 -run
  'TestGraphOperation|TestOriginObservation|TestProfileCleanup|TestGatewayFinalizesOwnedProfileWithoutClass'
  .\pkg\controllers\hub\gateway`, without resetting shared collectors.
- Passed `go vet .\pkg\controllers\hub\gateway
  .\cmd\hub-gateway-controller-manager`.
- Passed `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
  run --no-config
  --enable-only=nilerr,goconst,whitespace,gocyclo,gosec,errorlint,prealloc,unused
  --new-from-rev=HEAD .\pkg\controllers\hub\gateway` with zero issues.
- Passed `go run golang.org/x/lint/golint@latest -set_exit_status
  .\pkg\controllers\hub\gateway`.
- Applied repository-local goimports grouping; final formatting listing was
  empty. Reviewed the production diff and checked whitespace. No return values,
  status messages, authorization/ownership guards, retry timings, finalizers,
  manager injection, RBAC or dependencies changed.
- GD1-GD3 are complete within the source/unit boundary. No envtest/live
  API-server event delivery/rate-limit, Azure call/enforcement, container/registry,
  pinned-v1 CI lint or private HTTP proof was performed. SDK stager production
  injection and its write/platform gates remain open, as do approval-wait
  telemetry and full P6.2.

#### Before/After Comparison

- Before: observation and profile finalization have diagnostics; injected graph
  staging and endpoint suspension have explicit status/errors but no dedicated
  logical-call telemetry.
- Planned after: consistent bounded call diagnostics for these existing seams,
  without turning on production staging or claiming successful enforcement.

#### References

- `pkg/controllers/hub/gateway/staging.go` and `staging_test.go`: persisted
  ownership prerequisites, single external call and status-conflict separation.
- `pkg/controllers/hub/gateway/withdrawal.go` and `withdrawal_test.go`: confirmed
  complete-empty intent and existing-finalizer suspension guards.
- `pkg/controllers/hub/gateway/reference_withdrawal.go` and its tests: positive
  uncached denial proof, repeated authorization validation and whole-endpoint
  fail-closed suspension; sequential reads are not transactional enforcement.
- `pkg/controllers/hub/gateway/observability.go`: existing native recorder,
  registry and fixed-label cleanup/origin metrics.
- `docs/design/gep-1748-implementation-plan.md`, P6.2: operation diagnostics
  remain distinct from platform/write and live-installation proof gates.
- Rechecked designated domain-knowledge and specification directories; no files
  were found, so no specification selection is needed.

### U. Current-runtime API-server integration coverage

#### Requirements

- Advance P6.1 after slice T commit `bac7977`, moving beyond fake Kubernetes
  clients to real envtest API-server, informer/cache, status and Event behavior.
- Exercise only the current supported runtime seams: origin observation and
  owned-profile cleanup. Keep the SDK graph stager uninjected and never contact
  Azure, a live cluster or a registry.
- Reuse repository Ginkgo/envtest and existing `make test` CI conventions rather
  than adding a parallel harness, workflow, dependency or delivery system.

#### Additional comments from user

- User requested "continue implementation" after committing slice T.
- Existing repository-first/non-NIH implementation constraint remains in force.

#### Plan

**Phase EI1: Integration fixtures and tests first**

- [x] Task EI1.1: Add a Ginkgo-style Gateway integration suite using the existing
  `TestAPIs` naming convention, real envtest clients and the actual GatewayClass,
  Gateway and HTTPRoute controllers/dependency indexes. Load repository Fleet
  CRDs and the selected Gateway API module's own standard CRDs using a fixed,
  read-only Go module lookup; do not copy or invent CRD schemas.
  Success: the suite compiles and fails explicitly when required assets or API
  setup are missing; no silent skip/fallback to fake Kubernetes clients.
- [x] Task EI1.2: Add thread-safe provider fakes and an API-valid private-backend
  fixture with server-assigned UIDs/generations and separately persisted status.
  Test pending approval -> observed convergence by changing only fake provider
  state, using the unchanged production requeue cadence. Test actual
  InternalServiceExport dependency-watch invalidation and recovery.
  Success: real controllers publish current truthful status with no address or
  `Programmed=True`; informer changes, not direct Reconcile calls, drive updates.
- [x] Task EI1.3: Assert approval/convergence Events are persisted through the
  real recorder/API server. Add a deletion case with test-seeded persisted
  cleanup ownership, provider pending/absence observations and actual finalizer
  retention/removal.
  Success: pending cleanup retains the finalizer and observed absence releases
  it via the controller; server/hub/Gateway UIDs reach the fake provider correctly.

**Phase EI2: Run with supported assets and resolve integration failures**

- [x] Task EI2.1: Run the focused suite. If assets are missing, restore the
  repository-pinned setup-envtest release/asset version into session-local
  storage and use process-local `KUBEBUILDER_ASSETS`; do not install system-wide
  tools or change dependency manifests.
  Success: actual local API-server/etcd execution, not merely compilation or
  mocked readiness. Stop all test-owned processes on success/failure.
- [x] Task EI2.2: Fix issues exposed by these integration scenarios with minimal
  directly related changes only. Preserve controller-name validation, production
  polling, ownership checks and disabled provisioning; do not relax schemas or
  bypass guards to satisfy the suite.
  Success: focused suite passes reliably together with existing Gateway/manager
  regressions.

**Phase EI3: Document and verify**

- [x] Task EI3.1: Update design progress and test instructions with exact commands
  and evidence; run goimports, vet, golint and compatible changed-code lint.
  Success: clearly separate real local API/cache/Event/finalizer evidence from
  unverified deployed RBAC/identity, Azure/private HTTP and recorder rate limits.
  P6.1 as a whole remains open for later lifecycle/production-provider scenarios.

**Completion checklist:** EI1.1, EI1.2, EI1.3, EI2.1, EI2.2 and EI3.1 require
recorded evidence. Implementation starts only after explicit plan approval.

#### Decisions

- Add the suite alongside Gateway tests, matching existing hub controller
  suites. Existing `make test` runs it; unit-only checks can continue using the
  established `-skip '^TestAPIs$'` selector.
- Use real API validation/defaulting and persisted status rather than reusing
  fake-client fixtures with synthetic UIDs/resource versions.
- Use thread-safe in-process provider fakes: this proves Kubernetes integration,
  not Azure SDK requests or dataplane behavior. No injection of the production
  write provider is authorized by this work.
- Do not shorten/override production polling just for tests. Allow bounded test
  deadlines covering the current 30-second-plus-jitter requeue behavior.
- Seed the cleanup finalizer explicitly in the deletion scenario to model a
  previously owned Gateway; do not enable staging merely to create it.

#### Implementation Details

- User explicitly approved slice U through the plan form. Changes are a Gateway integration suite/fixtures,
  directly related documentation and any tightly coupled fixes proven by it.

#### Changes Made

- Recorded the tests-first plan only. Read the existing hub Ginkgo/envtest suite,
  Gateway setup/indexes, production manager wiring and CI `make test` path.
- Confirmed the public `envtest-v1.33.0` release includes Windows amd64 assets,
  matching the current Makefile asset version. No assets downloaded or tools
  installed yet.
- Added the real API/controller suite and lifecycle scenario first. The initial
  focused run compiled and failed explicitly because etcd assets were missing.
  Its cleanup also exposed envtest v0.19's unsafe Stop before process
  initialization; guarded cleanup on initialized environment configuration while
  preserving the explicit startup failure. Restore session-local assets next.
- Restored setup-envtest `release-0.22` / `1.33.0` into session-local storage.
  The native Windows lifecycle spec passed all assertions, but suite teardown
  failed because controller-runtime v0.19 sends a stop signal unsupported by
  Windows. Did not skip teardown, alter dependency source or accept this as a
  passing run. Stopped the two exact test-owned process IDs and removed their
  known remaining temporary data directory. The setup tool's locked temporary
  archive was also explicitly removed after its process exited.
- Course correction: used the existing Ubuntu WSL runtime and matching Linux
  assets from the same setup tool/version. Focused `TestAPIs` passed, including
  normal manager/control-plane teardown. A fresh full Gateway-subtree/manager
  invocation then passed with Go 1.26.9 and no `TestAPIs` skip.
- An explicit post-test check found no processes whose command line referenced
  this session's Linux test-asset directory. Native Windows process IDs were
  also confirmed stopped. No test server is left running.
- Existing unit-only Gateway/manager regressions, vet, golint and compatible
  changed-code lint passed. Fixed repeated fixture strings without disabling
  lint rules. No production implementation bug was exposed by this bounded
  scenario; no production changes were needed.
- Updated test commands to distinguish unit-only checks from asset-backed
  integration. Documented Linux/WSL teardown requirements, the unchanged
  production polling cadence and existing CI inclusion. Design/chart references
  now distinguish verified local Event persistence from deployed RBAC/identity.
- Final source review strengthened expected model-shape assertions and preserved
  non-NotFound API errors during deletion polling.

#### Final verification and proof limits

- Initial native `go test -count=1 -run '^TestAPIs$'
  .\pkg\controllers\hub\gateway` failed explicitly for missing assets.
  After asset restore the lifecycle assertions passed, but native teardown
  failed for unsupported stop signals; this run is not counted as a pass.
- Under Ubuntu WSL with process-local `KUBEBUILDER_ASSETS` pointing to the
  session-local Linux `1.33.0` assets, passed:
  `go test -count=1 -run '^TestAPIs$'
  go.goms.io/fleet-networking/pkg/controllers/hub/gateway`.
- In a fresh WSL Go process, passed:
  `go test -count=1 go.goms.io/fleet-networking/pkg/controllers/hub/gateway...
  go.goms.io/fleet-networking/cmd/hub-gateway-controller-manager`.
  This included the new API-server suite without a skip and existing regressions.
- Passed the final race-enabled WSL run:
  `go test -race -count=1 -run '^TestAPIs$'
  go.goms.io/fleet-networking/pkg/controllers/hub/gateway`.
  Production polling and controller-name validation stayed intact.
- Full and race-enabled runs both included a successful post-test process check:
  no command line referenced the session's Linux envtest asset directory.
- Passed native unit-only `go test -count=1 -skip '^TestAPIs$'
  .\pkg\controllers\hub\gateway... .\cmd\hub-gateway-controller-manager`,
  `go vet .\pkg\controllers\hub\gateway .\cmd\hub-gateway-controller-manager`,
  and `go run golang.org/x/lint/golint@latest -set_exit_status
  .\pkg\controllers\hub\gateway`.
- Passed final compatible changed-code lint with zero issues:
  `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
  run --no-config
  --enable-only=nilerr,goconst,whitespace,gocyclo,gosec,errorlint,prealloc,unused
  --new-from-rev=HEAD .\pkg\controllers\hub\gateway`.
- Applied repository-local goimports grouping. EI1-EI3 are complete within this
  bounded API/controller scope. No new production code, dependencies, workflow
  or chart permissions were needed. SDK stager injection, actual Azure/private
  HTTP, deployed identity/RBAC, recorder rate limits and `/readyz` HTTP integration
  remain unverified. Broader restart/drift/revocation/provider scenarios keep
  P6.1 and Task 6.4 open; this is not full feature completion.

#### Before/After Comparison

- Before: Gateway observation/finalization is covered with fake clients and
  manager setup checks, not a running API server/cache/Event writer.
- Planned after: bounded real API/informer/polling/Event/finalizer coverage for
  the current observation/cleanup runtime, with Azure/write gates unchanged.

#### References

- `pkg/controllers/hub/trafficmanagerprofile/suite_test.go`: repository
  Ginkgo/envtest manager and lifecycle convention.
- `pkg/controllers/hub/gateway/controller.go`, `observation.go`, `cleanup.go`:
  actual controller watches, polling and finalization boundaries.
- `pkg/controllers/hub/gatewayresolver/indexes.go`: real dependency indexes.
- `cmd/hub-gateway-controller-manager/main.go`: production registration of the
  three controllers and intentionally uninjected stager.
- `Makefile`: setup-envtest `release-0.22`, asset version `1.33.0`.
- `.github/workflows/unit-integration-tests.yml`: existing `make test` job.
- `docs/design/gep-1748-implementation-plan.md`, P6.1/P6.2: integration and live
  proof requirements.
- `https://github.com/kubernetes-sigs/controller-tools/releases/tag/envtest-v1.33.0`:
  published native test-asset platforms; asset presence is not execution proof.
- Designated domain-knowledge/specification directories have no files; no
  applicable versioned specification was found.

### V. ReferenceGrant revocation/restoration API integration

#### Requirements

- Continue P6.1 after slice U commit `6feed5c`, reusing its real API/controller
  suite rather than introducing a new manager/harness or delivery mechanism.
- Exercise authorized cross-namespace HTTPRoute attachment and ServiceImport
  references, grant deletion, absent/recreated backend and grant recreation.
- Preserve reference-validity semantics, controller-driven updates and truthful
  unprogrammed/address-free status. Do not claim dataplane revocation: the SDK
  stager remains uninjected and fake providers do not enforce Azure policy.

#### Additional comments from user

- User requested "continue implementation" after committing slice U.
- Retain the repository-first implementation/delivery constraint.

#### Plan

**Phase RG1: Extend the existing real-API lifecycle**

- [x] Task RG1.1: Adapt the API-valid fixture to a separate HTTPRoute namespace,
  explicit Gateway AllowedRoutes policy and a correctly scoped ReferenceGrant.
  Carry related objects in a small fixture value instead of growing positional
  return tuples. Preserve the existing approval, discovery-watch, Event and
  cleanup scenarios.
  Success: initial route-parent Accepted/ResolvedRefs conditions are current and
  true; fake observer receives the authorized private backend.
- [x] Task RG1.2: Add tests for grant deletion while Gateway/HTTPRoute specs are
  unchanged. Assert Accepted stays true, ResolvedRefs becomes false with
  RefNotPermitted, Gateway diagnostics show denied intent, and denied intent
  does not keep invoking the observer after controller/cache status settles.
  Success: actual grant watches/indexes, not direct Reconcile calls or Gateway
  edits, drive current status within a bounded test deadline.
- [x] Task RG1.3: Remove the ServiceImport while authorization remains denied,
  then recreate the grant with a new UID before restoring the backend as a
  negative control. Assert denial wins over missing backend lookup until
  permission returns, when BackendNotFound becomes visible; target restoration
  recovers reference resolution and origin observation.
  Success: errors are explicit, Gateway/HTTPRoute generations remain unchanged
  across grant/backend changes, and no address or Programmed=True is published.

**Phase RG2: Run, resolve and document**

- [x] Task RG2.1: Run the focused Linux/WSL envtest suite with existing session
  assets and race detection. Fix only directly exposed integration defects.
  Success: unchanged polling, ownership, controller-name validation and uninjected
  production stager; normal manager/control-plane teardown with no orphaned
  test-owned processes.
- [x] Task RG2.2: Run affected regressions, goimports, vet, golint and compatible
  changed-code lint. Update the test guide, design progress and breadcrumb.
  Success: exact reference-status and recovery evidence is documented separately
  from unverified production enforcement/identity and broader P6.1 lifecycle work.

**Completion checklist:** RG1.1, RG1.2, RG1.3, RG2.1 and RG2.2 require passing
evidence. Implementation starts only after explicit plan approval.

#### Decisions

- Extend the existing lifecycle rather than restart a second manager in the same
  process or relax controller-runtime's name-validation guard.
- ReferenceGrant denial affects ResolvedRefs, not the independent route
  attachment acceptance. Missing targets cannot override a known authorization
  denial; no fallback backend is introduced.
- Backend status setup/recreation is performed by the test's platform/admin
  fixture only, not a new customer ServiceImport configuration requirement.
- Assert stable observer calls only after real denied route/Gateway status is
  observed; sequential caches are not a transactional authorization boundary.
- Keep P6.1 and actual fail-closed Azure withdrawal/enforcement open.

#### Implementation Details

- User explicitly approved slice V through the plan form. Changes are confined to the existing Gateway
  integration fixture/spec and directly related documentation, plus only defects
  demonstrably exposed by the scenario.

#### Changes Made

- Recorded the tests-first plan; no slice V implementation yet.
- Refined the negative control before editing tests: restore the grant while the
  backend is still absent, assert BackendNotFound instead of RefNotPermitted,
  then recreate the backend and observe recovery. This distinguishes actual
  target absence from authorization denial without adding actors, writes or
  changing the approved test scope.
- Extended the existing lifecycle fixture to a distinct route namespace with
  explicit AllowedRoutes and a named-backend ReferenceGrant. A small fixture
  value carries the related API objects; the single manager/suite is unchanged.
- Added exact current parent-controller/ParentRef/Accepted/ResolvedRefs checks,
  denied/missing-target/recovered Gateway diagnostics and settled observer-call
  assertions. Grant and ServiceImport recreation require different server UIDs;
  Gateway and HTTPRoute specs/generations stay unchanged through the sequence.
- The race-enabled focused real-API suite passed with the original approval,
  discovery-watch, Event and finalizer assertions retained. Normal teardown
  passed and the post-run check found no session-owned Linux envtest processes.
- Initial compatible changed-code lint reported zero issues. No production code
  fix, dependency, RBAC, polling or injection change was required.

#### Final verification and proof limits

- Passed the focused real API/controller lifecycle under Ubuntu WSL with existing
  session-local Linux `1.33.0` assets and process-local `KUBEBUILDER_ASSETS`:
  `go test -race -count=1 -run '^TestAPIs$'
  go.goms.io/fleet-networking/pkg/controllers/hub/gateway`.
  Existing approval polling, Event persistence, discovery watches and cleanup
  assertions remained enabled. The post-run process check found no command line
  referencing this session's Linux envtest asset directory.
- Passed native `go test -count=1 -skip '^TestAPIs$'
  .\pkg\controllers\hub\gateway... .\cmd\hub-gateway-controller-manager`.
- Passed `go vet .\pkg\controllers\hub\gateway
  .\cmd\hub-gateway-controller-manager` and
  `go run golang.org/x/lint/golint@latest -set_exit_status
  .\pkg\controllers\hub\gateway`.
- Passed compatible changed-code lint with zero issues:
  `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
  run --no-config
  --enable-only=nilerr,goconst,whitespace,gocyclo,gosec,errorlint,prealloc,unused
  --new-from-rev=HEAD .\pkg\controllers\hub\gateway`.
- Repository-local goimports listing was empty and the scoped production/test
  diff and whitespace check passed. Updated design progress and test guide.
- RG1-RG2 are complete within this local authorization/status-watch scope.
  RefNotPermitted/BackendNotFound precedence, current controlled-parent status,
  recreated identities and generation-stable recovery are now asserted against
  a real API server. No actual AFD mutation/withdrawal or live traffic, deployed
  identity/RBAC, restart, drift, weights or member/approval-revocation proof is
  claimed. P6.1 remains incomplete and the SDK stager stays uninjected.

#### Before/After Comparison

- Before: real API integration covers same-namespace resolution, provider-only
  approval changes, export watches, Events and cleanup.
- Planned after: the same lifecycle also proves cross-namespace authorization
  watches, denial precedence and restored permission, without enabling writes.

#### References

- `pkg/controllers/hub/gateway/observation_integration_test.go` and `suite_test.go`:
  existing real API/controller lifecycle and thread-safe provider fixture.
- `pkg/controllers/hub/gatewayresolver/route.go`, AuthorizeBackendReference:
  grant authorization precedes target resolution.
- `pkg/controllers/hub/httproute/controller.go`: independent Accepted and
  ResolvedRefs projection and dependency watches.
- `pkg/controllers/hub/gatewayresolver/indexes.go`: target-namespace grant index.
- `docs/design/gep-1748-implementation-plan.md`, P6.1/P5: revocation/recovery
  integration remains distinct from actual Azure enforcement.
- Rechecked designated domain-knowledge/specification directories; no files
  were found, so no versioned specification selection is required.

### W. Exported source/PLS identity replacement API integration

#### Requirements

- Extend bounded P6.1 coverage to source Service UID and private-origin identity
  replacement through the existing InternalServiceExport dependency watch.
- Prove that a spec change cannot reuse an older generation's unconflicted
  status, and that current status restores observation of the exact new model.
- Preserve private-only configuration, production polling and disabled writes.

#### Additional comments from user

- User requested continued implementation after committing slice V.
- Retain existing repository delivery and test mechanisms rather than inventing
  another harness. User explicitly approved slice W through the plan form.

#### Plan

**Phase IR1: Extend the existing tests first**

- [x] Task IR1.1: Add race-safe provider model assertions and a source-identity
  replacement scenario to the existing single-manager lifecycle. Recreate the
  fixture Service with the same namespace/name and a new server UID, then update
  the transported InternalServiceExport reference. Before refreshing conflict
  status, require stale-generation rejection and no additional observations
  after rejection settles. Refresh current unconflicted status and require
  observation of exactly the replacement source UID.
  Success: old status cannot authorize changed source metadata; recovery uses
  the actual dependency watch without direct Reconcile or Gateway/route edits.
- [x] Task IR1.2: Replace the export's synthetic private-link resource ID and
  internal frontend address while preserving source identity. Repeat the stale
  status negative control, then require the exact new private-origin fields
  after status recovery. Check unchanged Gateway/HTTPRoute specs/generations
  and independent current route-reference status.
  Success: the observer receives the replacement private intent, never a model
  retaining the old origin after the new model is observed.

**Phase IR2: Execute, correct and document**

- [x] Task IR2.1: Run the focused race-enabled suite under Ubuntu WSL with the
  existing Linux envtest assets and normal manager/control-plane teardown.
  Fix only defects directly exposed by these scenarios; retain all previous
  approval, grant, Event and finalizer assertions.
  Success: the full lifecycle passes with no test-owned processes left behind.
- [x] Task IR2.2: Run affected unit regressions, goimports, vet, golint and
  compatible changed-code lint; update the design progress, test guide and this
  breadcrumb with exact outcomes and boundaries.
  Success: checks pass and documentation distinguishes imported-model recovery
  from member discovery and actual Azure mutation/withdrawal.

**Completion checklist:** IR1.1, IR1.2, IR2.1 and IR2.2 are complete with passing
evidence below. P6.1 stays open for its other scenarios.

#### Decisions

- Reuse the committed fixture, provider mutex and single manager. Do not add a
  dependency, suite, workflow, polling override or controller-name bypass.
- A fixture Service supplies a genuine new UID, but the hub consumes transported
  export metadata, not a direct member Service watch. This does not prove the
  member discovery/transport pipeline or independent live Service verification.
- Update spec and status separately to expose the existing observed-generation
  authorization gate. Wait for rejection before asserting stable observer calls;
  cached sequential reads are not transactional writer exclusion.
- Assert normalized provider input, not just convergence-message reuse or call
  count. No SDK stager injection or real PLS lookup is required.

#### Implementation Details

- Planned changes are confined to the existing integration fixture/spec and
  directly related documentation unless a scenario exposes a coupled defect.
- Implement the approved test scenarios before running the real-API suite;
  production changes require a defect demonstrated by those scenarios.

#### Changes Made

- Recorded the bounded tests-first plan for approval.
- Added a mutex-protected scalar origin snapshot that explicitly rejects
  unexpected observed route/rule/backend/origin cardinality.
- Extended the existing single lifecycle before grant revocation: recreated
  source Service, transported its fresh UID, then changed the PLS resource ID
  and frontend address while retaining the replacement source identity.
- Both changes separately retain prior-generation conflict status and require
  the exact stale-export Gateway diagnostic, current valid route references,
  stable observer calls and the unchanged last observed origin. Current status
  must then deliver the exact replacement origin and convergence diagnostic.
- Checked unchanged Gateway/HTTPRoute specs and generations, retained the
  original approval, grant, Event and finalizer scenarios, and updated design
  progress and the test guide. No production, dependency, RBAC, polling or
  provider-injection changes were needed.

#### Verification and proof limits

- Passed compilation:
  `go test -run '^$' .\pkg\controllers\hub\gateway`.
- Passed real-API lifecycle under Ubuntu WSL with existing session-local Linux
  `1.33.0` assets and process-local `KUBEBUILDER_ASSETS`:
  `go test -race -count=1 -run '^TestAPIs$'
  go.goms.io/fleet-networking/pkg/controllers/hub/gateway` (77.836 seconds).
  Normal manager/control-plane teardown succeeded; the post-run process check
  found no command line referencing the session's Linux asset directory.
- Passed native regressions:
  `go test -count=1 -skip '^TestAPIs$'
  .\pkg\controllers\hub\gateway... .\cmd\hub-gateway-controller-manager`.
- Passed `go vet .\pkg\controllers\hub\gateway
  .\cmd\hub-gateway-controller-manager` and
  `go run golang.org/x/lint/golint@latest -set_exit_status
  .\pkg\controllers\hub\gateway`.
- Passed compatible changed-code lint with zero issues:
  `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
  run --no-config
  --enable-only=nilerr,goconst,whitespace,gocyclo,gosec,errorlint,prealloc,unused
  --new-from-rev=HEAD .\pkg\controllers\hub\gateway`.
- No actual Azure mutation/traffic, member discovery/transport, independently
  verified live source Service/PLS, restart or transactional cache guarantee is
  claimed. P6.1 remains incomplete and production SDK staging stays disabled.
- The repository-local goimports listing was empty; scoped code/documentation
  diff review and whitespace checks passed. Slice W is complete and uncommitted.

#### Before/After Comparison

- Before: real integration proves discovery readiness and grant recovery, but
  not changed exported source/PLS identities or stale conflict-status rejection.
- After: real dependency watches reject stale exported generations and
  deliver exact replacement identities after current status, without changing
  Gateway/HTTPRoute intent or enabling production Azure writes.

#### References

- `pkg/controllers/hub/gateway/observation_integration_test.go` and `suite_test.go`:
  existing real API lifecycle and thread-safe provider.
- `pkg/controllers/hub/gatewayresolver/backend.go`, resolveExportOrigin:
  source identity, private-origin validation and current conflict-status gate.
- `pkg/controllers/hub/gatewayresolver/backend_test.go`: prior stale-status,
  changed-source and private-origin unit coverage.
- `pkg/controllers/hub/gatewayresolver/indexes.go`: export dependency mapping.
- `docs/design/gep-1748-implementation-plan.md`, P5.1/P6.1: identity replacement
  and deterministic integration gaps; actual Azure withdrawal remains separate.
- Slice V above: committed authorization-recovery baseline.
- Designated domain-knowledge/specification inventory returned no files; no
  versioned specification applies to this bounded test extension.

### X. Provider-only loss of convergence and recovery API integration

#### Requirements

- Extend P6.1 to loss of previously observed private-origin convergence without
  Kubernetes spec edits: disconnected approval and drifted origin configuration.
- Prove that ordinary production polling replaces stale convergence diagnostics
  and persists the appropriate warning, then observes restored readiness.
- Retain truthful Programmed=False/address-free status, valid route references
  and the existing private-only, disabled-write runtime boundary.

#### Additional comments from user

- User requested continued implementation after committing slice W.
- Reuse repository mechanisms. User explicitly approved slice X through the
  plan form; no Azure scope or traffic experiment has been authorized.

#### Plan

**Phase OC1: Extend the existing tests first**

- [x] Task OC1.1: Extend the mutex-protected fake observer with explicit origin
  states, constructing correlated origin references/targets from the existing
  backend planner rather than hard-coded identities. Preserve initial pending
  approval and later ready behavior.
  Success: the fake reflects the currently observed model, including slice W
  replacements, without Azure access or a new harness.
- [x] Task OC1.2: Add table-driven disconnected and drifted transitions to the
  existing single-manager lifecycle. Change only fake provider state; require
  a later observer call, exact state-specific Gateway diagnostic, persisted
  Warning/AFDPrivateOriginsNotReady Event and valid current route references.
  Restore Ready after each case and require convergence plus a new persisted
  convergence Event occurrence, not just an earlier matching Event.
  Success: both regressions and recoveries are detected without direct Reconcile,
  shorter polling or Kubernetes dependency/spec edits.
- [x] Task OC1.3: Snapshot Gateway/HTTPRoute and backend intent across these
  transitions; assert unchanged specs, generations and backend source/private
  identities, with Programmed=False and no published addresses throughout the
  observed transitions.
  Success: origin convergence diagnostics cannot masquerade as ingress readiness
  or change authorized private intent.

**Phase OC2: Execute, correct and document**

- [x] Task OC2.1: Run the full focused race-enabled real-API lifecycle under WSL
  using existing Linux assets, retaining previous assertions and normal teardown.
  Fix only defects directly exposed by this scope.
  Success: all transitions pass with no test-owned control-plane process left.
- [x] Task OC2.2: Run affected unit regressions, goimports, vet, golint and
  compatible changed-code lint; update existing design progress, test guide and
  this breadcrumb with exact results and increased polling-driven runtime.
  Success: checks pass and proof limits remain explicit.

**Completion checklist:** OC1.1, OC1.2, OC1.3, OC2.1 and OC2.2 are complete with
passing evidence below. P6.1 remains open for broader lifecycle and
production-provider scenarios.

#### Decisions

- Reuse the actual manager, its production polling intervals and standard Event
  recorder. Do not introduce a polling override, fake clock, controller-name
  bypass, additional manager or workflow.
- Verify Events by involved Gateway UID, type, reason and message. For recovery,
  compare persisted occurrence counts so a historical convergence Event cannot
  satisfy the new transition. Do not assert exact Event totals or claim recorder
  rate-limit behavior.
- Fake disconnected/drifted states exercise controller observation/status/Event
  behavior, not actual SDK state classification, Azure approval revocation,
  configuration repair or dataplane enforcement.
- SDK graph staging remains uninjected. Manual/asynchronous approval and its
  external automation action item remain unchanged.

#### Implementation Details

- Planned files: existing Gateway integration spec and, if required, its explicit
  fake initialization in the existing suite; directly related documentation.
- Implement the approved fake-state and lifecycle assertions before running the
  real API suite; production fixes require a directly demonstrated defect.

#### Changes Made

- Recorded the bounded tests-first plan for approval.
- Replaced the boolean approved fake state with explicit typed origin state.
  The lifecycle configures initial ApprovalPending before creating any Gateway;
  an unconfigured fake fails explicitly. Observe uses the existing PlanBackends
  identities and targets, including replacement models from slice W.
- Added a table-driven provider-only Disconnected/Drifted sequence after identity
  replacement, with Ready recovery after each case. Assertions check later
  observer calls, exact current origin-identity/state diagnostic and matching
  persisted Gateway UID/type/reason/message warning Events.
- Recovery requires a persisted convergence occurrence count greater than the
  baseline captured after the warning. This cannot pass by finding the old
  convergence Event. No exact Event totals or rate-limit behavior are assumed.
- Kept current route references valid, exact observed private model unchanged,
  and Gateway status unprogrammed/address-free at observed transitions. Snapshot
  assertions preserve Gateway/HTTPRoute specs/generations and entire backend
  ServiceImport/InternalServiceExport objects across the provider-only changes.
- Preserved the prior approval, export readiness, identity/grant recovery and
  pending-finalizer/absence assertions. No production, suite startup, dependency,
  RBAC, polling override or provider-injection changes were needed.
- Updated design progress and the test guide, including the now roughly
  three-to-four-minute production-polling-driven lifecycle duration.

#### Verification and proof limits

- Passed compilation:
  `go test -run '^$' .\pkg\controllers\hub\gateway`.
- Passed the real-API lifecycle under Ubuntu WSL with existing session-local
  Linux `1.33.0` assets and process-local `KUBEBUILDER_ASSETS`:
  `go test -race -count=1 -run '^TestAPIs$'
  go.goms.io/fleet-networking/pkg/controllers/hub/gateway` (208.969 seconds).
  Manager/control-plane teardown succeeded normally; the post-run process check
  found no command line referencing the session's Linux asset directory.
- Passed native regressions:
  `go test -count=1 -skip '^TestAPIs$'
  .\pkg\controllers\hub\gateway... .\cmd\hub-gateway-controller-manager`.
- Passed `go vet .\pkg\controllers\hub\gateway
  .\cmd\hub-gateway-controller-manager` and
  `go run golang.org/x/lint/golint@latest -set_exit_status
  .\pkg\controllers\hub\gateway`.
- Passed compatible changed-code lint with zero issues:
  `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
  run --no-config
  --enable-only=nilerr,goconst,whitespace,gocyclo,gosec,errorlint,prealloc,unused
  --new-from-rev=HEAD .\pkg\controllers\hub\gateway`.
- This is local fake-provider observation/status/native Event evidence, not
  actual SDK classification, real Azure approval withdrawal/configuration
  repair, deployed identity/RBAC, recorder rate-limit or dataplane enforcement
  proof. P6.1 remains incomplete and production SDK staging stays disabled.
- Repository-local goimports listing was empty; scoped code/documentation diff
  review and whitespace checks passed. Slice X is complete and uncommitted.

#### Before/After Comparison

- Before: initial pending-to-converged polling is covered, but later provider-only
  loss of convergence and restoration are not real-API scenarios.
- After: disconnected/drifted observations replace stale convergence and
  recover through ordinary polling, with current status and fresh Event evidence.

#### References

- `pkg/controllers/hub/gateway/observation.go`: observation correlation,
  truthful unprogrammed status and production progress requeue.
- `pkg/controllers/hub/gateway/observability.go` and `observability_test.go`:
  state-specific warning/convergence Events and existing unit evidence.
- `pkg/providers/azure/frontdoor/observation.go`: typed observed origin states;
  `backend.go`: existing backend plan and origin-reference identities.
- `pkg/controllers/hub/gateway/observation_integration_test.go` and `suite_test.go`:
  committed single-manager API/watch/Event lifecycle and mutex-protected fake.
- `docs/design/gep-1748-implementation-plan.md`, P5.1/P6.1/P6.2:
  approval withdrawal, drift and operational proof boundaries.
- Slices U-W above are the related committed integration baseline.
- Rechecked designated domain-knowledge/specification inventory: no files found;
  no versioned specification applies to this bounded test extension.

### Y. In-flight stale observation API integration

#### Requirements

- Extend P6.1 beyond sequential edits: change valid backend intent while an
  already-started provider observation is blocked.
- Prove the old observation cannot publish a stale diagnostic/Event after the
  controller's own cache has observed the changed backend.
- Retain existing polling, authorization, private-only models and disabled
  production Azure writes.

#### Additional comments from user

- User requested continued implementation after committing slice X.
- Reuse the existing suite and runtime seams; user explicitly approved slice Y
  through the plan form.

#### Plan

**Phase SO1: Extend the existing tests first**

- [x] Task SO1.1: Add a one-shot, channel-controlled hold to the existing fake
  observer. Atomically arm a captured Rejected observation, signal when its old
  model/result has been captured, and release the provider mutex before waiting.
  Honor cancellation and register idempotent test cleanup to release a hold even
  when an assertion fails.
  Success: the test can edit Kubernetes state while the actual controller worker
  is waiting, without sleeps, goroutine leaks or production hooks.
- [x] Task SO1.2: Expose the manager's existing cached client as a test-only
  read-only Reader. While the old observation is blocked, change the export's
  private frontend address and refresh its current unconflicted status. Wait
  until that exact spec/current status is visible through the controller cache
  before releasing the old result.
  Success: cache propagation is an explicit barrier, not a scheduling assumption;
  both old and replacement models remain valid private intent.
- [x] Task SO1.3: Return the old captured Rejected result after setting subsequent
  observations to Ready. Require observation of the exact replacement origin,
  current convergence diagnostics and a fresh persisted convergence Event
  occurrence. Assert no stale Rejected warning Event, valid route references,
  unchanged Gateway/HTTPRoute specs/generations and unprogrammed/address-free
  status.
  Success: a rapidly corrected stale diagnostic cannot escape detection through
  its persisted Event; recovery uses the actual queued controller/watch path.

**Phase SO2: Execute, correct and document**

- [x] Task SO2.1: Run the existing full focused race-enabled lifecycle under WSL
  with session-local Linux assets and normal teardown. Correct only demonstrated
  defects directly coupled to stale-result rejection.
  Success: new synchronization and all prior scenarios pass without test-owned
  processes left behind.
- [x] Task SO2.2: Run affected regressions, goimports, vet, golint and compatible
  changed-code lint; update design progress, test guide and this breadcrumb.
  Success: passing evidence and precise proof limits are recorded.

**Completion checklist:** SO1.1, SO1.2, SO1.3, SO2.1 and SO2.2 are complete with
passing evidence below. P6.1 stays open for remaining lifecycle and actual
production-provider scenarios.

#### Decisions

- Reuse the one manager and fake provider; do not invoke Reconcile directly,
  accelerate polling, disable controller-name validation or add dependencies.
- Change a valid origin rather than merely making the model unavailable: this
  exercises full model equality as well as existing identity/generation guards.
- Rejected is not used by prior provider-state scenarios, so the exact warning
  message distinguishes this stale result from historical warnings. Check
  persisted Event identity/type/reason/message and fresh recovery occurrences.
- Exposing a read-only cache Reader is solely a synchronization seam in tests.
  It does not establish transactional cache freshness, exclusion of competing
  writers, cancellation of Azure work or real ARM mutation safety.
- The fake captures its result before blocking; later state changes must not
  rewrite that in-flight result. SDK graph staging remains uninjected.

#### Implementation Details

- Planned files: existing integration spec, its suite's read-only cached Reader
  assignment, and directly related design/test documentation.
- Implement the approved fake hold and cache-barrier assertions before running
  the real API suite; production fixes require a directly demonstrated defect.

#### Changes Made

- Recorded the bounded tests-first plan for approval.
- Added a one-shot provider hold that captures typed observation state and model
  before waiting outside its mutex. A buffered entry notification, context-aware
  release wait, returned notification and sync.Once cleanup support deterministic
  coordination without test-owned worker goroutines in the integration case.
- Added table-driven hold unit tests using existing fixtures: explicit release,
  context cancellation, idempotent unblock, duplicate-arm rejection, captured
  Rejected state after future state changes and normal Ready observations after
  the hold is consumed. Cleanup cancels/releases and bounds worker completion.
- Exposed the same manager cached client as a test-only client.Reader; the
  controller clients and production registrations are unchanged.
- Extended the existing lifecycle after provider-state recovery: arm/capture
  Rejected, set subsequent state Ready, replace the valid private frontend and
  refresh conflict status, then wait for the exact generation/spec/status through
  the controller cache before releasing the old result.
- Required the exact replacement origin, fresh current convergence and a new
  persisted convergence Event occurrence, with no exact stale Rejected warning.
  Current route refs and unchanged Gateway/HTTPRoute specs/generations remain
  asserted. All previous lifecycle scenarios remain enabled.
- No production, dependency, RBAC, polling or provider-injection changes were
  required. Updated existing design progress and test guide, including combined
  focused selection and now roughly four-to-five-minute lifecycle duration.

#### Verification and proof limits

- Passed initial compilation:
  `go test -run '^$' .\pkg\controllers\hub\gateway`.
- Passed targeted hold unit tests:
  `go test -count=1 -run '^TestIntegrationObservationHold$'
  .\pkg\controllers\hub\gateway`.
- Passed the complete real-API lifecycle and hold unit tests under Ubuntu WSL
  with existing session-local Linux `1.33.0` assets and process-local
  `KUBEBUILDER_ASSETS`:
  `go test -race -count=1 -run '^(TestAPIs|TestIntegrationObservationHold)$'
  go.goms.io/fleet-networking/pkg/controllers/hub/gateway` (243.989 seconds).
  Normal manager/control-plane teardown passed; the post-run process check found
  no command line referencing the session's Linux asset directory.
- Passed native regressions:
  `go test -count=1 -skip '^TestAPIs$'
  .\pkg\controllers\hub\gateway... .\cmd\hub-gateway-controller-manager`.
- Passed `go vet .\pkg\controllers\hub\gateway
  .\cmd\hub-gateway-controller-manager` and
  `go run golang.org/x/lint/golint@latest -set_exit_status
  .\pkg\controllers\hub\gateway`.
- Passed compatible changed-code lint with zero issues:
  `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
  run --no-config
  --enable-only=nilerr,goconst,whitespace,gocyclo,gosec,errorlint,prealloc,unused
  --new-from-rev=HEAD .\pkg\controllers\hub\gateway`.
- Cache advancement is intentionally synchronized before releasing the fake
  result. This does not establish transactional freshness, competing-writer
  exclusion, actual ARM cancellation/mutation safety or live Azure traffic
  enforcement. P6.1 remains incomplete and SDK staging stays uninjected.
- Repository-local goimports listings were empty; scoped code/documentation
  diff review and whitespace checks passed. Slice Y is complete and uncommitted.

#### Before/After Comparison

- Before: stale-result guards have fake-client unit coverage, but the real API
  lifecycle only changes dependencies between completed observations.
- After: a real worker receives an old observation after a cache-observed
  valid model change, discards its status/Event evidence and recovers on the
  replacement private model.

#### References

- `pkg/controllers/hub/gateway/observation.go`, applyOriginObservation:
  Gateway identity/generation and full-model equality guards.
- `pkg/controllers/hub/gateway/controller.go`: re-resolution after observation,
  status retry boundary and post-persistence Event recording.
- `pkg/controllers/hub/gateway/observation_test.go`: existing changed-backend and
  stale-result unit cases; `observability_test.go`: stale Event suppression.
- `pkg/controllers/hub/gateway/observation_integration_test.go` and `suite_test.go`:
  current real controllers, cache, fake state and persisted Event helpers.
- `docs/design/gep-1748-implementation-plan.md`, P6.1:
  changed intent during provider work and deterministic integration.
- Slices U-X above are the related committed integration baseline.
- Rechecked designated domain-knowledge/specification inventory: no files found;
  no versioned specification applies to this bounded test extension.

### Z. Priority: one operational private HTTP vertical slice

#### Requirements and additional comments from user

- User requested: "focus the remaining implementation on one proven end-to-end
  supported private HTTP path" and "do this first".
- This supersedes further standalone observation-matrix expansion, PR splitting
  and broader routing/features as the next implementation priority.
- Retain the existing supported subset: Premium, one unrestricted HTTP/80
  listener, root PathPrefix `/`, one logical ServiceImport port, ILB/PLS origins,
  preserved incoming Host and platform HTTP GET `/` probe policy. No customer
  ServiceImport configuration, public origins, custom domains, WAF or HTTPS.

#### Plan

**Phase VH1: Complete the missing provider path, tests first**

- [x] Task VH1.1: Add focused SDK-transport tests for the complete supported
  lifecycle: absent -> disabled staging -> pending manual approval -> observed
  approval/convergence -> safe activation -> stable enabled no-op -> owned
  cleanup. Cover failure/withdrawal only where needed to prevent unsafe traffic.
  Success: accepted writes never masquerade as observed readiness.
- [x] Task VH1.2: Implement the smallest complete HTTP reconcile/activation flow
  by extending the existing provider/builders. Current disabled staging actively
  disables enabled resources, so it cannot simply be injected repeatedly.
  Preserve ownership/finalizer guards, bounded operations, fresh observations
  and private-only full-inventory validation.
  Success: a completed graph stays enabled without disable/re-enable churn.

**Phase VH2: Prove platform assumptions, then connect the runtime**

- [ ] Task VH2.1: In an explicitly authorized Azure scope, exercise the same
  candidate provider against real private origins. Verify region support,
  actual disabled/enabled convergence order, manual asynchronous approval,
  Host/probe behavior and the supported member eligibility/weight policy.
  Use an exclusively controller-managed owned profile, not arbitrary external
  resources. Success: recorded API and private-HTTP evidence settles the
  platform gates; no fabricated capability flag substitutes for that evidence.
- [x] Task VH2.2: Wire the complete provider through the
  existing manager enablement and chart, and project observed full-graph
  convergence into Gateway/listener Programmed and Azure-returned hostname.
  Reuse existing suspension for confirmed withdrawal; suspend the whole endpoint
  rather than invent selective fallback. Keep default-disabled behavior.
  Success: enabled reconciliation provisions and maintains the supported path;
  origin approval alone never publishes readiness.
  Updated ordering: the user explicitly authorized offline controller completion
  before live proof in section ZB; implementation is complete under that approval,
  not because the VH2.1 platform evidence has been obtained.

**Phase VH3: Prove the installed controller path**

- [ ] Task VH3.1: Extend existing E2E/build/install mechanisms, not a new harness.
  Demonstrate controller-created Premium resources, PLS-owner approval, current
  programmed status/address, HTTP application responses over private origins,
  preserved Host/probes, fail-closed withdrawal and observed owned cleanup.
  Inspect Azure and member Service configuration for no public origins/frontends.
  Success: actual installed-controller traffic and cleanup evidence, not fake
  providers, request serialization or manual Azure configuration alone.

#### Decisions and implementation details

- No further standalone observer scenarios or speculative framework/features.
  Change only code/tests/documentation needed to close this vertical slice.
- Reuse current auth/options, SDK clients, ownership, polling, cleanup, image,
  Helm chart and existing test runner. Do not add an alternative delivery model.
- Implement candidate provider code offline, but do not lift production write
  gates or claim end-to-end proof without authorized live evidence.
- Live work requires approved subscription/resource group/region, hub/member
  access, private-origin resources, allowed operations, cost/lifetime limits and
  a PLS owner who can approve connections. No Azure mutation is authorized yet.

#### Changes made and before/after

- User approved the plan and explicitly selected offline-only work; live Azure
  operations are not authorized. Implement VH1 first; VH2.1 is blocked on scope
  and authorization, and production wiring remains dependent on that proof.
- VH1.1: New SDK lifecycle tests first failed because the top-level provider did
  not exist, then passed with the candidate. They cover disabled staging,
  asynchronous manual approval, separate route/endpoint activation, stable
  enabled observations across client reconstruction and owned cleanup.
- Candidate implementation shares existing root and inventory validation across
  disabled/enabled phases; it does not repeatedly disable a healthy enabled graph.
  Confirmed private-graph drift requests whole-endpoint suspension, while SDK,
  cancellation and ambiguous inventory failures remain explicit errors and never
  authorize enablement or suspension.
- VH1.2: Added `frontdoor/http_provision.go` on the existing `RouteProvisioner`.
  Route enablement follows verified disabled staging; endpoint enablement follows
  a subsequent complete private-graph/approval/deployment inventory. Only a fresh
  enabled-graph observation returns a top-level hostname; accepted writes do not.
  SDK clients can be reconstructed between invocations without traffic churn.
- Shared existing root/deployment/client/inventory checks instead of adding
  another provider or delivery mechanism. Active graphs reject all extra origins;
  eligible stale-origin cleanup remains restricted to disabled staging. Confirmed
  drift under a disabled endpoint restages with an explicit diagnostic.
- Focused safety checks cover approval loss before endpoint activation and recovery,
  public-origin drift/extras, missing/disabled/changed routes, unsupported domains,
  ambiguous arrays/resources, unknown approval, per-read SDK failures and
  cancellation, activation errors, invalid owners/clients and foreign profiles.
  Existing SDK transport assertions enforce one write and the same bounded
  deadline throughout each invocation. Existing owned-profile cleanup is reused.
- Updated the design plan and private-ingress how-to without removing their
  nonoperational-runtime warning. No manager/chart/runtime injection was changed.
- Validation passed:
  `go test -count=1 .\pkg\providers\azure\frontdoor`;
  `go test -count=1 -skip '^TestAPIs$' .\pkg\controllers\hub\gateway... .\cmd\hub-gateway-controller-manager`;
  `go vet .\pkg\providers\azure\frontdoor .\pkg\controllers\hub\gateway .\cmd\hub-gateway-controller-manager`;
  `go run golang.org/x/lint/golint@latest -set_exit_status .\pkg\providers\azure\frontdoor`;
  `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run --no-config --enable-only=nilerr,goconst,whitespace,gocyclo,gosec,errorlint,prealloc,unused --new-from-rev=HEAD .\pkg\providers\azure\frontdoor`.
  This compatible local lint does not resolve the pinned-CI-version blocker.
  No new dependencies or temporary repository artifacts were introduced.
- Before: extensive observation coverage, but runtime cannot provision traffic.
  After: the complete narrow provider path has offline SDK evidence, while actual
  platform behavior, production wiring and installed-controller private HTTP proof
  remain incomplete. VH2.1 is blocked by the explicit offline-only authorization;
  no Azure/cluster operations or container proof were performed.

#### References and completion checklist

- Existing P1/P3/P4/P5/P6 gates above; `frontdoor/graph.go`, `staging.go`,
  `route_provision.go`, `inventory.go`; Gateway `staging.go`; manager `azure.go`;
  existing private-ingress how-to, Helm chart and E2E framework.
- Designated domain-knowledge/specification inventory returned no files.
- [x] VH1.1; [x] VH1.2; [ ] VH2.1; [x] VH2.2; [ ] VH3.1.
  Completion requires the live proof, not merely finishing offline code.

### ZA. Prepare the installed-controller proof offline

#### Requirements and additional comments from user

- User requested end-to-end proof as the highest priority, then explicitly chose
  "Prepare the existing E2E proof scenario offline; no Azure or cluster operations".
- This prepares VH3.1 without claiming VH2.1/VH2.2 are complete or enabling the
  production provider. No live setup, cluster access, image push or approval is
  authorized by this work.

#### Plan

**Phase ZA1: Test the proof predicates first**

- [x] Task ZA1.1: Add offline unit tests for current-generation programmed/address
  checks, exact application identity/Host/request correlation, probe-log evidence
  and affirmative blocked-response checks. Success: stale status, cached/wrong
  application responses, transport failures and missing probe evidence cannot
  masquerade as successful proof.

**Phase ZA2: Add one opt-in scenario to the existing E2E suite**

- [x] Task ZA2.1: Reuse the existing test image, member deployment and object
  builders to create only internal-LB/PLS Services and real member ServiceExports,
  plus an unrestricted HTTP/80 Gateway and root-path Fleet ServiceImport route.
  Add minimal proof identity/Host/request logging to the existing sample app.
  Success: no synthetic hub exports, public-LB creation or customer ServiceImport
  edits; the test never provisions AFD directly or auto-approves connections.
- [x] Task ZA2.2: Require installed-controller ownership, current status and ARM
  observations of the owned Premium/private graph, manual approval and the
  approved probe/Host policy; require fresh application responses from both
  members and actual probe logs. Remove the route, require observed endpoint
  withdrawal plus sustained negative HTTP responses, then delete the Gateway
  and require profile and Gateway absence without finalizer bypass.
  Success: status alone, approval alone, failed network reads or a direct SDK
  provisioning call cannot pass the scenario. Explicit opt-in against the current
  unconnected manager must fail, not skip or claim operational readiness.

**Phase ZA3: Validate and document the prepared scenario**

- [x] Task ZA3.1: Run helper/app unit tests, E2E compile-only checks and relevant
  Go formatting/lint/vet offline; document prerequisites, opt-in invocation,
  manual approval, time bounds, cleanup and remaining gates in `test/README.md`.
  Success: repeatable offline checks pass without invoking `TestE2E`, authenticating
  to Azure or contacting Kubernetes. Actual platform/traffic proof remains open.

#### Decisions, references and completion checklist

- Reuse `test/e2e`, its existing runner and `examples/getting-started/app`; add no
  alternate harness, automatic deployment, provider injection or approval API.
- Related references: section Z, `test/README.md`, existing Gateway E2E/object/
  workload helpers, private-ingress how-to, ownership helpers and provider graph.
  Designated domain-knowledge/specification inventory returned no files.
- User explicitly approved ZA and its offline-only execution scope.
- ZA1.1 passed offline: SDK transport checks prove enabled-graph verification is
  GET-only; Go tests reject stale/missing status, wrong/cached application identity,
  malformed/absent probe evidence, unsafe frontend bindings and nonaffirmative
  withdrawal. Python sample-app tests pass using the existing declared Flask
  dependency in a session-local virtual environment after native Flask was missing.
- Reuse full private-graph verification through a narrowly read-only provider
  entry point, rather than duplicate the production ARM inventory validator in
  E2E or call a provisioning method as a readiness assertion. Independent app,
  probe and Azure frontend observations remain required.
- ZA2.1/ZA2.2 are prepared and compile with passing offline predicate tests, not
  live-executed. The serial opt-in spec reuses the existing workload templates but
  deliberately avoids `DeployWorkload`, which would create public LB Services.
  Member Services have internal/PLS annotations before creation. Only Kubernetes
  intent writes are allowed; ARM access in the spec is read-only.
- Installed identity evidence follows Deployment -> ReplicaSet -> running ready
  Pod UIDs, exact manager flags/account/image and nonempty runtime image IDs.
  Independent PLS/frontend GETs reject public IP bindings and stale source UIDs.
  The read-only provider verifies complete enabled graph inventory; fresh
  two-member HTTP identity/Host/nonce/edge responses and actual probe logs supply
  separate delivery evidence. Withdrawal requires deployed disablement plus
  sustained affirmative negative edge responses, not failed connections.
- Added `test/README.md` instructions for authorized prerequisites, manual approval,
  explicit installed-controller selection, focused label/report command, time
  bounds, finalizer-preserving cleanup and safe offline-only commands.
- [x] ZA1.1; [x] ZA2.1; [x] ZA2.2; [x] ZA3.1.

#### Implementation Details and Changes Made

- `pkg/providers/azure/frontdoor/http_provision.go` adds
  `VerifyEnabledHTTPPreview(ctx, owner, model) (string, error)`: validate intent
  and required clients, observe owned enabled roots, verify complete enabled
  inventory within one 30-second deadline, then return the observed hostname.
  SDK transport tests assert GET-only traces for success, disabled endpoint and
  pending approval. No production injection or reconciliation behavior changed.
- `test/e2e/gateway_private_http_test.go` adds the explicit opt-in serial lifecycle
  to the existing Ginkgo runner. The accompanying proof/network helper files
  implement current-generation status, installed-image evidence, read-only Azure
  frontend verification, bounded HTTP requests, probe-log witnesses and cleanup.
  Table-driven assertions exercise positive controls and evidence rejection.
- `test/e2e/framework/cluster.go` adds bounded-by-caller native pod-log access
  using its existing REST configuration. The proof supplies a deadline,
  container, line limit and byte limit.
- `examples/getting-started/app/main.py` preserves existing response fields and
  adds Host/request/run identity. Structured request/probe logging is enabled
  only by the proof's optional environment variable. Three Python unit tests
  cover normal behavior, exact response identity and actual probe log fields.
- `test/README.md` documents the future authorized command and current limitations.
  This breadcrumb records plan approval, implementation and offline evidence.

#### Offline Validation and Course Corrections

- `go test -count=1 .\pkg\providers\azure\frontdoor` passed (5.800s).
- `go test -count=1 -tags=e2e -run '^TestPrivateHTTP' .\test\e2e`
  passed (9.390s).
- `go test -tags=e2e -run '^$' .\test\e2e` passed, no tests run.
- `go vet -tags=e2e .\pkg\providers\azure\frontdoor .\test\e2e...` passed.
- `go test -count=1 -skip '^TestAPIs$' .\pkg\controllers\hub\gateway .\cmd\hub-gateway-controller-manager`
  passed both dependent packages. No envtest or real-cluster suite invoked.
- Goimports ran with `-local go.goms.io/fleet-networking` on all changed Go files.
  Compatible tagged changed-code lint reported zero issues:
  `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run --build-tags=e2e --no-config --enable-only=nilerr,goconst,whitespace,gocyclo,gosec,errorlint,prealloc,unused --new-from-rev=HEAD .\pkg\providers\azure\frontdoor .\test\e2e...`.
  Initial findings were fixed by reusing constants and extracting pod matching,
  without weakening checks. The repository's older pinned linter remains
  incompatible with the local Go version, as previously recorded.
- Native `golint` was absent from PATH; rerunning via
  `go run golang.org/x/lint/golint@latest -set_exit_status` passed for
  `.\pkg\providers\azure\frontdoor .\test\e2e\framework` and separately for all
  four explicit new tagged Go test files. No dependency manifests changed.
- All three sample-app tests passed using a session-local Python environment
  with the already-declared Flask dependency and `-B -m unittest discover -v -s
  .\examples\getting-started\app -p test_main.py`. No repository bytecode artifacts.
- `git diff --check` passed. No `TestE2E`, Azure authentication, cluster operations,
  image publication, controller installation or Private Link approval performed.

#### Before/After Comparison and Remaining Gates

- Before: fake-transport activation lifecycle existed, but no prepared installed
  scenario tied real private frontends, manual approval, current status, traffic,
  observed probes, withdrawal and controller-mediated cleanup together.
- After: one opt-in scenario and its evidence predicates are prepared and checked
  offline using the existing harness/image. Actual Azure resource behavior,
  member traffic distribution, probe Host, installed identity permissions,
  asynchronous approval and deployed withdrawal remain unproven until authorized
  live execution. Offline completion does not complete VH2.1/VH2.2/VH3.1.
- Production provider wiring remains pending platform proof; no default enablement,
  direct test provisioning, automatic approval or finalizer bypass was introduced.

### ZB. Complete controller execution offline; defer live proof

#### Requirements

- Finish the controller-to-provider private HTTP lifecycle, current Gateway and
  listener readiness/address projection, and fail-closed withdrawal.
- Preserve explicit AFD enablement, default-disabled manager/chart behavior,
  private-only Premium policy, persisted ownership and bounded provider calls.
- Defer all Azure/real-cluster operations and live testing. Do not describe
  offline controller completion as successful end-to-end traffic proof.

#### Additional comments from user

- "finish the remaining controller work. Defer the live testing for later and
  review existing codebase for what ATM does for live proof"
- This explicitly supersedes Z's ordering gate: controller implementation and
  wiring may now complete offline before VH2.1 live evidence. Live authorization,
  installation, identity/permissions, approval and traffic proof remain open.

#### Plan

**Phase ZB1: Controller and startup tests first**

- [x] Task ZB1.1: Add focused table-driven reconciliation tests for all candidate
  lifecycle states, observed ownership before provider calls, exact observed
  hostname/current Gateway and listener conditions, stable enabled maintenance,
  errors/invalid results, status-conflict no-replay and stale results after
  Gateway/backend/authorization changes. Success: only fresh full-graph
  convergence publishes readiness; neither origin approval nor accepted writes
  suffice.
- [x] Task ZB1.2: Test manager provider initialization/required injection and
  disabled startup. Test provisioner-driven empty/denied and affirmatively
  unsupported/unresolved intent withdrawal, preserved finalizers and deletion
  priority. Success: no competing disabled-staging replay, no Azure mutation
  authorized by ambiguous API/read errors, and no stale ready address on failure.

**Phase ZB2: Connect the complete narrow controller path**

- [x] Task ZB2.1: Add a typed full HTTP provisioner seam, reuse the existing
  ownership/uncached intent preparation, and invoke the complete candidate once
  outside status retries. Reuse suspension for empty/denied/confirmed
  unprogrammable intent; retain the existing disabled-stager seam for its
  focused regressions, but never inject both into production.
  Success: enabled reconciliation stages, waits for approval, activates and
  maintains the one supported graph without disabling a healthy enabled graph.
- [x] Task ZB2.2: Apply results only to matching current identity/model, recheck
  uncached ownership/intent before positive readiness, project Gateway and
  listener conditions/address together, and preserve explicit provider/status
  errors, progress polling and bounded diagnostics. Construct the SDK provisioner
  with existing auth/options/scope and inject it only when AFD is enabled.
  Success: current observed full convergence is the sole ready/address source;
  withdrawal and failures clear published readiness without bypassing cleanup.

**Phase ZB3: Offline validation, accurate documentation and ATM comparison**

- [x] Task ZB3.1: Run modified/dependent Go unit regressions, provider fake-SDK
  regressions, safe E2E helper/compile checks, goimports, golint, vet and compatible
  changed-code lint. Reuse existing local-only integration checks where needed;
  never invoke real E2E setup or `TestE2E`. Success: offline checks pass and
  disabled-default behavior is preserved.
- [x] Task ZB3.2: Update the how-to, chart guide, implementation plan and E2E
  runbook to distinguish completed controller code from deferred live proof.
  Record ATM's actual evidence and differences, without copying its public-LB
  workload setup or broadening the AFD feature subset. Success: no current claim
  that SDK provisioning is unwired, and no claim that live traffic is proven.

**Success criteria:** The enabled controller has a complete, offline-validated
private HTTP execution path and an existing opt-in live proof scenario ready for
later authorized use. Live platform, installed identity, container publication
and HTTP/Host/probe/withdrawal/cleanup proof remain explicitly unverified.

#### Decisions

- Keep the separate manager and existing `--enable-afd`/chart opt-in; no new
  default enablement, deployment harness, configuration API or public fallback.
- Preserve cleanup/deletion priority, finalizer reobservation, uncached
  authorization and source-identity checks, and external calls outside status
  retry callbacks. Sequential reads are not transactional authorization.
- Confirmed unsupported/unresolved desired intent on an already-owned Gateway
  must not leave a previously enabled endpoint serving indefinitely. Revalidate
  that evidence uncached before suspension; transient API errors are not proof
  authorizing mutation.
- ATM's live E2E is explicitly gated by `ENABLE_TRAFFIC_MANAGER`, creates real
  Kubernetes intent/workloads, waits for current-generation status and verifies
  actual Azure SDK profile/endpoint configuration. It tests Azure drift/deletion
  recovery, backend/member/weight changes, and Kubernetes plus Azure cleanup.
  Its Azure comparator intentionally ignores monitor/health fields. The reviewed
  ATM scenario has no application HTTP request, observed health-probe log or
  statistical traffic-distribution assertions; do not present its configured
  weights/status as live application delivery proof.
- Reuse that existing setup/status/ARM/cleanup pattern, but retain AFD-specific
  private frontend inspection, manual approval, fresh application Host/identity,
  probe logs and deployed withdrawal evidence in the prepared AFD scenario.

#### Implementation Details

- User explicitly approved ZB and offline implementation. New controller/startup
  tests first failed on the missing full-provisioner seams, then passed after
  wiring. Targeted Gateway/dependent, manager and provider unit regressions passed.
- Added a typed complete HTTP provider seam alongside the legacy disabled-stager
  test seam; configuring both is an explicit error. Production startup constructs
  the existing SDK RouteProvisioner with shared auth/options/scope and injects it
  only for enabled AFD.
- Reuse persisted/reobserved ownership and uncached complete intent before calls;
  calls remain outside status retries. Positive readiness also rechecks uncached
  source identity, hub, authorization and cleanup ownership after provider work
  and on every status retry. Only Programmed plus a usable observed Azure hostname
  publishes a Hostname address and current Gateway/listener readiness.
- Errors clear readiness/address without releasing cleanup. Already-owned empty,
  denied and affirmatively unprogrammable intent (including rejected controlled
  classes) uses whole-endpoint suspension.
  Uncached read errors or restored valid intent cannot authorize that suspension.
- Preserve actual condition transition times across temporary pending evaluation,
  avoiding stable enabled status churn. Approval/readiness Events follow persisted
  current status, not failed status writes. Added fixed graph-operation labels
  for HTTP reconciliation and invalid-intent suspension.

#### Changes Made

- Implemented the controller/provider/status/withdrawal/startup path and focused
  tests. Existing metrics allow-list regressions were updated for the two new
  fixed operation values; unsupported routing expectations remain strict.
- Updated the chart guide, private-ingress how-to, implementation plan and E2E
  runbook: enabled mode now performs AFD lifecycle writes, remains disabled by
  default and has no live-proof claim. Removed obsolete uninjected-runtime claims,
  documented scoped write/list permissions and added the source-verified ATM
  comparison. No real Azure/cluster operations performed.

#### Offline Validation and Course Corrections

- Passed
  `go test -count=1 -skip '^TestAPIs$' .\pkg\controllers\hub\gateway... .\cmd\hub-gateway-controller-manager .\pkg\providers\azure\frontdoor`.
  Gateway/dependent, manager and full provider fake-SDK regressions passed.
- Passed
  `go vet -tags=e2e .\pkg\controllers\hub\gateway .\cmd\hub-gateway-controller-manager .\pkg\providers\azure\frontdoor .\test\e2e...`.
- Passed local-only API-server/cache/Event/finalizer regressions under WSL with
  existing session-local Linux envtest assets:
  `go test -race -count=1 -timeout=10m -run '^TestAPIs$' go.goms.io/fleet-networking/pkg/controllers/hub/gateway`
  (236.957s). This existing suite retains fake observation/cleanup providers; it
  is not live ARM or installed-controller delivery proof. Normal teardown passed,
  and the targeted post-run process check found no test-owned envtest processes.
- Passed the final new controller lifecycle tests natively:
  `go test -count=1 -run '^TestHTTPController' .\pkg\controllers\hub\gateway`
  (8.299s), and under WSL with the race detector:
  `go test -race -count=1 -run '^TestHTTPController' go.goms.io/fleet-networking/pkg/controllers/hub/gateway`
  (2.508s). Stable-maintenance tests force old condition timestamps so same-second
  clock precision cannot hide churn. Status-conflict tests also change effective
  intent between retries and reject stale positive results without replaying calls.
- Passed safe E2E helper and compile-only commands:
  `go test -count=1 -tags=e2e -run '^TestPrivateHTTP' .\test\e2e`
  and `go test -tags=e2e -run '^$' .\test\e2e`.
  No `TestE2E` or real suite setup was invoked.
- Compatible changed-code lint reported zero issues:
  `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run --no-config --enable-only=nilerr,goconst,whitespace,gocyclo,gosec,errorlint,prealloc,unused --new-from-rev=HEAD .\pkg\controllers\hub\gateway .\cmd\hub-gateway-controller-manager .\pkg\providers\azure\frontdoor`.
- Passed
  `go run golang.org/x/lint/golint@latest -set_exit_status .\pkg\controllers\hub\gateway .\cmd\hub-gateway-controller-manager .\pkg\providers\azure\frontdoor`.
  Goimports listings for all changed Go files were empty; whitespace checks passed.
- Initial stale-source fixture correction: uncached export resolution uses List,
  so the test now changes the uncached List result rather than an unused Get.
  Metrics regressions retain an explicit bounded allow-list including the two new
  operations; unsupported multi-backend routing retains its specific rejection.
  No checks were weakened, dependencies changed or temporary repository artifacts
  introduced. The previously documented pinned-CI-linter incompatibility remains
  separate from this successful compatible local lint.

#### Before/After Comparison

- Before: full provider candidate and opt-in proof scenario exist, but production
  startup injects only observation/cleanup and cannot publish programmed readiness.
- After: explicit enablement connects the complete private HTTP path, including
  current programmed status/address, maintenance, fail-closed withdrawal and
  cleanup. Offline correctness and deferred live platform evidence remain
  distinct. This slice is complete and uncommitted; VH2.1/VH3.1 remain unproven.

#### References

- Sections Z and ZA: existing candidate lifecycle, offline proof and prior gates.
- `pkg/controllers/hub/gateway/{controller,staging,ownership,withdrawal,reference_withdrawal,observation}.go`:
  current execution, status, ownership and revocation boundaries.
- `cmd/hub-gateway-controller-manager/{main,azure}.go`: opt-in provider construction
  and current missing provisioner injection.
- `test/e2e/traffic_manager_test.go`: real intent/status/ARM, drift, weight/member
  lifecycle and cleanup assertions.
- `test/common/trafficmanager/{validator,azureprovider}/profile.go`: current
  status polling and actual Azure profile comparison/absence.
- Existing private-ingress how-to, chart guide, design implementation plan and
  `test/README.md`: directly dependent runtime/deployment/proof documentation.
- Designated domain-knowledge/specification inventory returned no files; no
  versioned specification was found for this bounded wiring change.

#### Completion checklist

- [x] ZB1.1; [x] ZB1.2; [x] ZB2.1; [x] ZB2.2; [x] ZB3.1; [x] ZB3.2.

### ZC. Complete category-3 build verification and preserve categorized history

#### Requirements and additional user comments

- User: "ok finish category 3 and squash/amend to the right commit category".
- Keep four category commits after the protected `cd2bf54` boundary. Do not
  change that commit or its ancestors, publish production images, deploy to
  Azure/Kubernetes or run the deferred private HTTP live scenario.
- Preserve existing controller behavior and linter rules.

#### Evidence and decisions

- WSL now provides a working Docker daemon and GNU Make. The Docker buildx
  plugin was missing; an official checksum-verified binary was installed only
  in isolated session tooling, without changing machine-wide Docker settings.
- Actual GitHub run `39895629832` at the historical `d10607c` revision passed
  Gateway image build/nonroot smoke, Helm contracts and unit/integration tests.
  Run `39895629848` passed the repository-pinned lint. These older results are
  not proof for the final current controller.
- Current `make lint` now executes under WSL with the pinned v1.64.7 tool and
  existing configuration. Its sole finding is complexity 37 (limit 30) in
  `TestGatewayFinalizesOwnedProfileWithoutClass`. Extract related assertions;
  do not disable, suppress or relax the check.
- The PR image smoke covers only amd64. Add native arm64 coverage rather than
  changing the existing CGO policy or installing privileged host emulation.
- Existing production publication already builds architecture-specific images
  and creates a combined Gateway manifest. Verify the resulting manifest
  explicitly contains both supported Linux architectures.
- Required domain-knowledge/specification directories remain absent. Existing
  Dockerfile, Makefile, publication workflows, chart tests and section Q are the
  applicable repository references.

#### Approved plan

User selected "Approve and implement offline" on 2026-10-24 UTC.

1. Phase 1: Preserve assertions and close the actual lint failure.
   - [x] Task ZC1.1: Run the existing cleanup test, extract cohesive assertions
     without removing cases, and rerun the test plus the unmodified full lint.
     Success: the same cleanup/error/ownership/event/metric coverage passes and
     the repository lint has zero findings.
2. Phase 2: Close build-delivery implementation gaps.
   - [x] Task ZC2.1: Configure native amd64/arm64 image smoke jobs in CI using the
     existing Makefile target and nonroot entrypoint checks.
     Success: validated workflow builds and executes each architecture on a
     matching runner; actual arm64 CI execution remains separately recorded.
   - [x] Task ZC2.2: Validate the published Gateway index after its existing
     manifest creation. Success: missing or duplicate required architectures
     fail rather than accepting an incomplete release.
3. Phase 3: Validate locally without production publication.
   - [x] Task ZC3.1: Build/run the actual current amd64 image with restricted
     container settings and verify default-disabled startup/probes; run chart,
     Makefile delivery and workflow checks. Exercise local-only registry or
     multiarchitecture verification where supported, reporting any environment
     limitation explicitly. Success: no Azure or real-cluster operations.
4. Phase 4: Document evidence and amend the matching categories.
   - [x] Task ZC4.1: Record exact outcomes and fold docs into category 1, the
     test-only lint refactor into category 2, and build changes into category 3.
     Replay category 4 without changing its contents. Success: exactly four
     category commits, protected ancestry unchanged, clean worktree.

#### Completion criteria

- All feasible current-revision build checks pass with no weakened safeguards
  or validation rules; actual production release execution and deferred live
  Azure/Kubernetes proof are distinguished from local/CI implementation.
- Checklist: [x] ZC1.1; [x] ZC2.1; [x] ZC2.2; [x] ZC3.1; [x] ZC4.1.

#### Implementation details and validation

- ZC1.1: The unchanged cleanup cases pass before and after extracting finalizer
  assertions. Full `make lint` passes under WSL using the repository-pinned
  v1.64.7/configuration, with zero remaining findings. No linter upgrade or rule
  weakening was needed; the old Windows executable was built with an older Go.
- Real current amd64 image build and restricted nonroot `--help` execution
  succeeded. Default-disabled startup with a synthetic loopback-only kubeconfig
  returned HTTP 200 from both `/healthz` and `/readyz`, without Azure or a cluster.
  The image, container and current workflows are used rather than fake binaries.
- This actual image check exposed stale CLI help claiming resource creation was
  unimplemented. Correct the enable flag's help to state that enabling performs
  Azure writes, and make image smoke assert the warning. This text-only
  controller correction belongs in category 2.
- The image workflow now uses native `ubuntu-latest`/amd64 and
  `ubuntu-24.04-arm`/arm64 jobs. Both retain the real Dockerfile, Makefile,
  configured nonroot user and restricted runtime policy. They verify metadata,
  help, the Azure-write warning, actual disabled-manager probes and real
  export/pull/execution using disposable loopback-only registries.
- The MCR workflow verifies both required Linux architectures after its existing
  manifest creation. Existing production registry credentials/TLS and the
  historical-release Gateway gate are unchanged. Quoting in the touched
  publication workflow was corrected so ShellCheck also passes without
  suppressing the existing release-tag/user-value findings.
- Successful current-revision commands and checks:
  - WSL `make image-hub-gateway-controller-manager
    REGISTRY=fleet-networking-category3 TAG=proof TARGET_OS=linux
    TARGET_ARCH=amd64 AUTO_DETECT_ARCH=FALSE BUILDX_BUILDER_NAME=default
    BUILDKIT_PROGRESS_TYPE=plain`: passed using the actual unchanged Dockerfile
    and existing CGO policy. Final image ID:
    `sha256:67119b87f837cb43532538b6c4a9d60a365fc0951c585a6540ca652dd4492133`.
  - Exact workflow Makefile delivery-contract script: passed for local/registry
    output and amd64/arm64 platform/tag overrides.
  - Exact workflow nonroot/help and disabled startup/probe scripts: passed
    against the built image, with both HTTP probes returning 200. The synthetic
    kubeconfig refers only to `https://127.0.0.1:1`; no cluster is contacted.
  - Exact workflow local-registry script: passed real Makefile registry export,
    manifest retrieval, pull, platform/user inspection and restricted nonroot
    execution. Local manifest digest:
    `sha256:51c336c31fea1cabb20875598b6cc9c0b1455ee0a73475fdea6dd8435fde9468`.
    Test registry HTTP is limited to an ephemeral host-loopback endpoint.
  - Exact release-manifest jq predicate: both architectures and attestations
    accepted; missing amd64, missing arm64, duplicate amd64, wrong OS and empty
    inventory rejected. No production manifest was read or published.
  - `go test -count=1 -skip '^TestAPIs$'
    ./cmd/hub-gateway-controller-manager ./pkg/controllers/hub/gateway`: passed.
  - WSL `go test -race -count=1
    -run '^TestGatewayFinalizesOwnedProfileWithoutClass$'
    go.goms.io/fleet-networking/pkg/controllers/hub/gateway`: passed.
  - `go vet ./cmd/hub-gateway-controller-manager
    ./pkg/controllers/hub/gateway`: passed.
  - `go run golang.org/x/lint/golint@latest -set_exit_status
    ./cmd/hub-gateway-controller-manager ./pkg/controllers/hub/gateway`: passed.
  - `go test -count=1 -tags=helm ./test/helm`: passed.
  - WSL `make lint`: passed repeatedly with the original v1.64.7 pin and
    `.golangci.yml`; zero findings. No dependency manifests changed.
  - WSL `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
    -config-file <session-actionlint-config>
    .github/workflows/unit-integration-tests.yml
    .github/workflows/build-publish-mcr.yml`: passed including ShellCheck.
- Temporary containers, loopback registry contents/tags, kubeconfigs, help files
  and delivery-plan files were removed by targeted cleanup. Named local image
  and downloaded tooling/logs remain local; nothing was pushed to production.

#### Before/after and remaining execution boundary

- Before: local container proof and full pinned lint were marked blocked; only
  amd64 CI image smoke was configured and combined manifests were not checked.
- After: current amd64 real image, restricted runtime, actual offline probes,
  local registry delivery, full lint and chart/workflow checks pass. Both native
  image jobs and production manifest validation are implemented.
- Actual native arm64 hosted-job execution and authenticated production registry
  publication remain unrun. The local x86_64 builder advertises no arm64 worker;
  no privileged host-emulation installation or different-CGO proxy build was
  used to manufacture arm64 proof. These execution results require later CI or
  release authorization, not additional private HTTP controller wiring.
- Live Azure/AKS and installed-controller HTTP proof remain deferred. No remote
  workflow dispatch, branch push, production publication or cluster operation
  was performed in this slice.
- History is maintained as four category commits: documentation evidence in
  docs, assertion/help corrections in controller, delivery workflow changes in
  build, and the unchanged live-test patch replayed last. The guarded rewrite
  verifies every category's file set, the unchanged protected boundary, exact
  final-tree equality with the staged changes and a clean worktree. Original
  history is retained in a local backup reference.

### ZD. Allow slow AFD convergence without weakening private HTTP proof

Plan approved: user selected "Approve and implement offline" on 2026-10-28 UTC.

#### Requirements and additional user comments

- User reports a live proof waiting for endpoint deployment `Succeeded`, notes
  that AFD may need 30 minutes, and requests: "ok, fix the timeouts".
- Increase only test timing and related guidance. Preserve all deployed-state,
  private-target, current-status, HTTP, probe, withdrawal and cleanup assertions.
- Do not run live Azure/Kubernetes tests, change controller request budgets or
  amend/commit history without a separate request.
- Subsequent user authorization: "update the test into the respective commit
  (commit and amend and rebase) - there should be 4". Fold documentation into
  category 1 and E2E timing/tests into category 4, preserving four signed commits.

#### Approved plan

1. Phase 1: Add a fast offline timing regression test.
   - [x] Task ZD1.1: Assert at least 45 minutes for AFD convergence/cloud cleanup,
     cleanup-node headroom, slower ARM polling, and an overall spec budget large
     enough for every sequential proof phase. Success: it fails against the old
     budgets and passes without sleeping or calling Azure/Kubernetes.
2. Phase 2: Update the focused scenario's timing.
   - [x] Task ZD2.1: Use 45-minute waits for initial AFD staging/approval-request
     observation, approved/enabled graph convergence, deployed withdrawal and
     edge withdrawal. Use 45 minutes for owned-profile and namespace cleanup,
     with 46-minute cleanup nodes. Retain ordinary 10-minute workload, traffic
     and per-member probe checks; retain the one-minute withdrawal stability
     assertion and all success predicates. Poll ARM convergence every 15 seconds
     while retaining five-second traffic checks.
   - [x] Task ZD2.2: Allow a five-hour spec upper bound and document a nine-hour
     outer Go timeout. Success: all sequential phase budgets fit the spec, and
     all four potentially slow deferred cleanup nodes fit outside it with room
     for suite startup/teardown. These are maximum budgets, not fixed sleeps.
3. Phase 3: Validate offline and document the execution boundary.
   - [x] Task ZD3.1: Run private-HTTP helper/timing tests, compile the tagged suite
     without invoking `TestE2E`, and run tagged vet/lint/format checks. Update the
     live command and timing guidance. Success: no assertion weakening, live
     resource operations, dependency changes or unrelated edits.

#### Decisions and references

- A 45-minute per-phase budget gives 15 minutes of headroom over the reported
  30-minute convergence. The spec accommodates four separate convergence phases,
  discovery, traffic, two member probe checks and normal owned-profile cleanup;
  the outer process also accommodates three namespace and one Gateway deferred
  cleanup node. Increasing only the outer command timeout is insufficient.
- Existing manual approval behavior remains unchanged. This timing fix does not
  establish that persistent `NotStarted`/deployment churn is healthy.
- Sources: `test/e2e/gateway_private_http_test.go`, private-HTTP evidence tests,
  `test/e2e/framework/{consts,fleet}.go`, and `test/README.md`.
  Designated domain-knowledge/specification directories remain absent.
- Checklist: [x] ZD1.1; [x] ZD2.1; [x] ZD2.2; [x] ZD3.1.

#### Implementation details and validation

- Added `TestPrivateHTTPTimeoutBudgets` first; the focused offline run failed
  because the new convergence, cleanup, enclosing-spec and polling constants did
  not exist. The table checks actual duration thresholds, sequential phase
  coverage and the documented outer budget without sleeps or remote clients.
- Added distinct 45-minute AFD convergence and cloud-cleanup budgets, 46-minute
  deferred cleanup nodes, a five-hour spec limit and 15-second ARM polling.
  Ordinary 10-minute/five-second checks and one-minute withdrawal stability are
  retained. The runbook's focused outer command now allows nine hours.
- Only private-HTTP E2E timing/evidence tests and directly related documentation
  are changed. Production/provider request budgets and success predicates are
  untouched; no live test has been executed by the assistant.
- `go test -count=1 -tags=e2e -run '^TestPrivateHTTP' ./test/e2e` passed,
  including the timing-budget regression and all existing evidence-rejection
  tests. `go test -tags=e2e -run '^$' ./test/e2e` also passed without invoking
  `TestE2E` or the live `BeforeSuite`.
- `go vet -tags=e2e ./test/e2e` and
  `go run golang.org/x/lint/golint@latest -set_exit_status ./test/e2e` passed.
- WSL `make lint` and the repository-pinned
  `./hack/tools/bin/golangci-lint-v1.64.7 run --build-tags=e2e ./test/e2e`
  both passed. `goimports` was applied and `git diff --check` passed.
- No dependencies or production code changed. All validation was offline.

#### Before/after comparison

- Before: normal 30-minute AFD propagation could exceed 10-/15-minute waits or
  the 45-minute overall spec even when resource deployment was progressing.
- After: dedicated 45-minute convergence/cleanup waits fit inside a five-hour
  spec; the documented nine-hour outer command also covers all deferred cleanup
  nodes. Success predicates, request deadlines and the one-minute affirmative
  withdrawal stability requirement are unchanged. Persistent deployment stalls
  remain failures requiring diagnosis rather than a relaxed readiness signal.

### ZE. Execute an authorized isolated private HTTP live proof

#### Requirements and additional user comments

- User requested execution against the supplied test subscription: "and then
  plug in and run the tests". Subscription access was verified read-only.
- Existing subscription clusters belong to other experiments; do not modify or
  reuse them without explicit selection. Use an isolated, newly created resource
  group unless the user selects an existing dedicated environment.
- Confirm resource scope, lifetime/cost authorization, registry policy and the
  Private Link approval operator before provisioning. No Azure resource or
  Kubernetes mutation has occurred in this preflight.

#### Approved scope and preflight findings

- User approved a new isolated environment, anonymous ACR pulls, scoped roles
  and cleanup in `fleet-afd-e2e-20261028-1951`, selecting `swedencentral`.
  The approved lifetime is at most 12 hours; the user will approve correlated
  Private Link requests after notification.
- Current operator has subscription Owner access; required Azure providers are
  registered. Microsoft Learn confirms AFD Private Link supports Sweden Central.
- The harness's hardcoded `Standard_A2_v2` is restricted as
  `NotAvailableForSubscription` in Sweden Central despite available family quota.
  Verify an equivalent two-vCPU alternative before provisioning; retain the
  default for other harness callers and document any run-specific override.
- Restore the missing yq in session-local tooling and reuse the verified local
  buildx plugin. Use a session-local kubeconfig/home and a narrow Azure CLI
  interoperability adapter: Windows Azure CLI supplies existing authentication,
  while Linux Docker needs an exposed ACR login token piped directly to stdin.
  Do not log, persist separately or commit token values, and do not change the
  shared default subscription or user's kubeconfig.
- `Standard_D2s_v5` has no subscription/location restrictions in Sweden Central;
  its family has 100 cores available and this run requires 12. Use it for all
  six nodes. A session-local detached clone of `7b70573` supplies LF shell files
  without rewriting the Windows worktree. Only that clone's six VM-size
  arguments gain a default-preserving `NODE_VM_SIZE` override; application and
  controller sources remain exactly the signed revision.
- Offline mocked bootstrap/shared-VNet checks passed for default and overridden
  sizes with Traffic Manager both disabled/enabled. Run-local shell syntax and
  ShellCheck passed. Missing yq was restored locally and existing buildx verified.
  Windows Azure authentication is bridged without changing shared account
  selection; only the run's ACR token is piped directly to Linux Docker login.
- The run is bounded to 11 hours before termination/owned cleanup, reserving
  time inside the approved 12-hour lifetime. Cleanup verifies the unique run
  ownership tag before deletion and waits for observed resource-group absence.
- Execution started on 2026-10-28 at approximately 20:09 UTC. The dedicated
  resource group and Standard ACR were created successfully in Sweden Central;
  the approved anonymous-pull configuration and authenticated Docker login
  succeeded. Candidate image builds/publication are in progress before cluster
  provisioning. Private HTTP assertions have not yet executed.
- The bounded orchestration is attached as `afd-live-run`; a separate bounded
  `afd-approval-checkpoint` monitor notifies when correlated pending requests
  require the user's approval, or when the run exits before reaching them.
  Reports, logs, the exact source revision and the run-only harness diff remain
  session-local. No feature commit was changed or pushed.
- Subsequent live observation: all three AKS clusters and candidate image
  publication completed; both member discovery releases and the enabled hub
  Gateway Deployment rolled out successfully. The actual E2E test binary began
  around 20:50 UTC. Gateway `my-ns-kmthr/private-http` is accepted but remains
  unprogrammed with `AFD private HTTP graph: Pending`.
- At the approval-stage check, the owned profile provisioning state was
  `Succeeded`; its endpoint was `Disabled`, provisioning `Succeeded`, deployment
  `NotStarted`, and the origin-group collection was empty. These observations
  do not establish an approvable Private Link request or successful traffic.
  Preserve the 45-minute convergence wait and diagnose persistent staging
  rather than relax readiness requirements.
- Discovered an observability defect in the documented command: Ginkgo `-ginkgo.v`
  does not replace Go's `-v`. Without Go verbosity, the parent runner buffers
  checkpoint output until completion. Corrected the documented invocation and
  called out the distinction. The already-running test retains its original
  arguments; its empty output log is not proof of any particular test phase.

#### Approved plan

1. **Phase ZE1: Bound the run and complete preflight**
   - [x] Task ZE1.1: Verify subscription access, inspect existing AKS environments
     read-only and locate the harness toolchain. Success: account is enabled,
     existing unrelated resources are excluded, WSL tool availability is known.
   - [x] Task ZE1.2: Obtain approval for a unique disposable resource group,
     region, three two-node AKS clusters, ACR/image publication, scoped controller
     identity assignments, AFD Premium, lifetime/cost and cleanup. Resolve the
     bootstrap's anonymous ACR-pull policy and manual approval responsibility.
     Success: an explicit approved scope exists before cloud changes.
   - [x] Task ZE1.3: Restore only missing tools, verify supported regional
     capacity/provider access and keep run configuration, kubeconfig and logs
     in session-local storage. Success: required tools and access are verified
     without overwriting shared user configuration.
2. **Phase ZE2: Prepare the installed-controller environment**
   - [x] Task ZE2.1: Build/publish this checkout and provision the shared-VNet
     baseline using the existing harness and isolated kubeconfig. Success:
     candidate hub/member images, sample app and all three clusters are ready.
   - [x] Task ZE2.2: Configure independent member discovery and the enabled hub
     Gateway controller with functioning identities and approved scoped Azure
     access. Success: actual Deployment rollout and discovery prerequisites
     are established; no competing writer or public-origin fallback is enabled.
3. **Phase ZE3: Exercise the real private HTTP proof**
   - [ ] Task ZE3.1: Run only the installed-controller private HTTP proof with the
     documented nine-hour outer timeout and retain its JSON report/logs.
     Success: actual pending checkpoint precedes authorized approval; all
     existing Azure deployment, HTTP, Host/probe, withdrawal and cleanup
     assertions pass without weakening any predicates.
4. **Phase ZE4: Preserve evidence and remove owned resources**
   - [ ] Task ZE4.1: Collect evidence and diagnose any failures within the
     approved scope; delete only this run's dedicated environment as authorized.
     Success: report the actual test outcome and observed cleanup state, not
     merely accepted asynchronous deletion.

#### Decisions, implementation details and references

- Retain the harness's AKS Standard/shared-VNet topology rather than replacing it
  with AKS Automatic: the proof depends on its existing cluster/network setup.
- WSL has Azure CLI, kubectl, Helm, Go, Docker, jq and make; Mike Farah yq is
  missing. Tool restoration is separate from changing repository dependencies.
- Reuse `test/README.md`, `test/scripts/bootstrap.sh`,
  `test/scripts/aks-shared-vnet.sh` and both controller chart guides. No
  specification/domain-knowledge files were found in the configured directories.
- Do not commit credentials, runtime artifacts or subscription inventory. Keep
  the four signed feature categories and production code unchanged.
- Completion checklist: [x] ZE1.1; [x] ZE1.2; [x] ZE1.3; [x] ZE2.1;
  [x] ZE2.2; [ ] ZE3.1; [ ] ZE4.1.
- Before/after: offline proof preparation becomes live evidence only after the
  real assertions pass. Until then, successful subscription access is preflight
  evidence only; no dataplane or cleanup success is claimed.

### ZF. Investigate initial-deployment sequencing exposed by the live run

#### Requirements and evidence

- The user requested the actual private HTTP proof, then asked whether approval
  was ready and where to approve. Do not approve on the user's behalf.
- Live Azure showed a provisioned disabled endpoint with deployment `NotStarted`
  and no origin group. The controller had accepted the Gateway but remained
  pending. The current attempt retains the full 45-minute initial wait.
- Canonical code gates `ReconcileHTTPPreview` on `endpointConverged` before
  checking/creating a route; `endpointConverged` requires deployment `Succeeded`.
  Initial root staging and backend staging also impose that deployment gate.
  Backend/route staging and activation have further convergence dependencies
  that must be reviewed together rather than applying a blanket state bypass.
- Microsoft Learn's AFD Endpoints Create reference (API 2025-04-15) explicitly
  shows successful 200/201 responses with provisioning `Succeeded` and deployment
  `NotStarted`. This supports a bootstrap-sequencing hypothesis; it does not
  establish final dataplane readiness or justify publishing a hostname.
  Reference: https://learn.microsoft.com/en-us/rest/api/frontdoor/azurefrontdoorstandardpremium/afd-endpoints/create

#### Proposed plan requiring approval before production changes

1. **Phase ZF1: Reproduce actual bootstrap states**
   - [x] Task ZF1.1: Add focused provider regressions using the observed fresh
     endpoint state and dependent group/origin/route states. Success: tests expose
     initial child-creation stalls without accepting unapproved/public origins.
2. **Phase ZF2: Correct lifecycle sequencing**
   - [x] Task ZF2.1: Separate initial ARM construction/configuration eligibility
     from deployed-edge readiness. Trace subsequent staging/activation gates and
     keep existing withdrawal/cleanup ownership and full inventory protections.
     Success: construction can reach actual approval requests while readiness
     remains false; activation still requires verified authorized private targets
     and approval; final readiness still requires full deployed convergence.
3. **Phase ZF3: Validate offline**
   - [x] Task ZF3.1: Run targeted provider/controller tests and applicable pinned
     lint/vet; retain negative regressions, one-write budgets and cancellation.
     Success: all relevant regressions pass without relaxing live success checks.
4. **Phase ZF4: Retry authorized live proof**
   - [ ] Task ZF4.1: After the current attempt's cleanup, publish/install the
     candidate and retry with Go `-v` so the pending checkpoint is visible.
     Keep the original approved resource scope and remaining 12-hour lifetime;
     do not silently reset the clock. Success: record actual approval, traffic,
     withdrawal and observed cleanup, or report the concrete remaining failure.

- Checklist: [x] ZF1.1; [x] ZF2.1; [x] ZF3.1; [ ] ZF4.1.
- User approved the sequencing fix and retry within the existing authorization.
  Five new provider regressions reproduce the bootstrap block (all stop after
  the two root GETs); non-bootstrap deployment-negative controls already pass.
- Implementation decision: a new endpoint carries a controller-internal
  `networking.fleet.azure.com.activation=pending` ARM tag. The first endpoint
  enable PATCH must atomically change it to `started`, preserving other tags.
  Only an owned, disabled endpoint still marked pending can stage provisioned
  `NotStarted` resources; legacy/previously activated endpoints retain strict
  deployed-disable gates. This prevents a bootstrap allowance from weakening
  withdrawal/restaging after an endpoint has actually been enabled.
- Construction/inventory must still prove provisioning, exact private targets,
  approval, managed policy, complete membership and absence of unsupported
  resources. Only the bootstrap deployment prerequisite changes; enabled
  readiness, origin observations, suspension and live traffic checks stay strict.
- Before/after goal: remove a construction dependency cycle, not weaken
  deployment `Succeeded`, real HTTP/probe evidence or withdrawal assertions.
- Implemented the bootstrap marker, configuration-only backend state and bounded
  root/backend/route/inventory eligibility. Strict active-graph, origin
  observation, suspension and live E2E success criteria are unchanged.
- Added passing regressions for initial group/route construction, pending-approval
  starvation, unapproved-origin refusal, route/endpoint activation, cancellation,
  inventory drift, malformed metadata, canonical tag updates and preservation
  of unmanaged tags. Explicit active route/group/origin `NotStarted` cases cannot
  publish a hostname through reconciliation or read-only verification.
- Offline validation:
  - `go test -count=1 ./pkg/providers/azure/frontdoor`: passed after final tests.
  - `CGO_ENABLED=1 KUBEBUILDER_ASSETS=/root/.local/share/kubebuilder-envtest/k8s/1.33.0-linux-amd64 go test -count=1 -race ./pkg/providers/azure/frontdoor ./pkg/controllers/hub/gateway ./cmd/hub-gateway-controller-manager`:
    passed, including the actual controller envtest suite (241.578 seconds).
  - `./hack/tools/bin/golangci-lint-v1.64.7 run`: full repository passed.
  - `go vet ./pkg/providers/azure/frontdoor ./pkg/controllers/hub/gateway ./cmd/hub-gateway-controller-manager`:
    passed.
  - `go run golang.org/x/lint/golint@latest -set_exit_status ./pkg/providers/azure/frontdoor ./pkg/controllers/hub/gateway ./cmd/hub-gateway-controller-manager`:
    passed. The separate golint binary was absent; module execution restored it.
  - Initial native race validation lacked cgo; WSL provided gcc. Initial native
    and WSL integration attempts lacked the envtest asset setting; reused the
    already installed Kubernetes 1.33.0 binaries rather than skipping tests.
- First live attempt: Gateway deletion began at `2026-10-28T21:39:10Z` after
  the initial pending-request wait failed. At approximately `22:08Z`, Azure
  reported the owned profile as `Deleting`; the Gateway retained its cleanup
  finalizer and namespace was terminating. No approval requests or HTTP proof
  were obtained. Original bounded orchestration still owns cleanup; do not
  launch conflicting resource-group provisioning before observed absence.
- Prepared an isolated retry clone and distinct image tag
  `7b70573-bootstrap-1`, with candidate diff/hash artifacts stored only in the
  session workspace. The actual Linux/amd64 candidate image built and its
  `--help` invocation succeeded. The isolated candidate's provider suite and
  final original-worktree provider race suite also passed.
- Retry orchestration waits for the first attempt's cleanup-complete marker and
  independently confirms resource-group absence before provisioning. It uses a
  distinct run-ownership tag, Go `-v` and a visible approval-checkpoint monitor.
  Authorization still expires at `2026-10-29T08:09:24Z`; execution stops by
  `06:54:24Z`, reserving 75 minutes for owned cleanup. If fewer than six execution
  hours remain after the first cleanup, it refuses to create more resources.
  No new approval, traffic or complete live-proof success is claimed.
- First report is now available: the pending-request assertion failed after its
  full 2,700-second budget because origins remained missing. Ginkgo then reported
  `Suite Timeout Elapsed` and cancelled Gateway cleanup after roughly 15 minutes,
  before its 46-minute node budget; namespace cleanup subsequently also failed.
  The external owned resource-group deletion has been submitted, with absence
  still unconfirmed. This is failure evidence, not successful feature cleanup.
- Course correction within the already approved timeout/retry scope: the Go
  nine-hour timeout does not override Ginkgo's independent default one-hour
  suite timeout. Before the queued retry starts its proof, add explicit
  `-ginkgo.timeout=8h30m` to the documented and actual retry invocations, and
  strengthen the offline budget regression to cover that suite deadline plus
  30 minutes of Go-runner headroom. The existing five-hour spec, 45-minute waits,
  46-minute cleanup nodes and all success assertions remain unchanged.
- Applied the explicit Ginkgo suite deadline to the queued retry and runbook;
  strengthened `TestPrivateHTTPTimeoutBudgets` to budget suite and outer Go
  deadlines separately. The isolated candidate's `TestPrivateHTTP*` tests
  passed, and a focused Ginkgo dry run accepted the actual `8h30m` flag without
  invoking live suite setup. Tagged E2E pinned lint and vet passed. These are
  test/runbook-only follow-up changes; the verified production image is unchanged.
- User approved committing the outstanding changes into the existing four
  categories. Fold documentation into category 1, provider code/regressions into
  category 2 and E2E timing/runbook changes into category 4; category 3 has no
  content changes. Preserve the protected parent, create recovery refs, compare
  the final tree with the complete pre-rewrite snapshot and SSH-sign all four
  resulting commits. No push is authorized. Live proof remains outstanding and
  the isolated retry continues independently of this history-only operation.

### ZG: Supply Azure's required origin-group load-balancing settings

- **Requirements / approval:** User requested the missing-field code fix and
  background deletion of the current failed live attempt. No additional live
  run or history rewrite is requested.
- **Evidence:** The current attempt passed the endpoint bootstrap gate but
  Azure rejected origin-group creation with HTTP 400:
  `Property 'AfdOriginGroup.LoadBalancingSettings' is required but it was not set`.
  The owned profile is `fleet-afd-a112b1835a4808275cd8af87a0412cb9`, and no
  approval checkpoint or HTTP proof was obtained.
- **Phase ZG1: Preserve evidence and delete the owned environment**
  - [x] Task ZG1.1: Capture the controller error, submit ownership-checked
    resource-group deletion and monitor it independently. Success: deletion is
    submitted promptly; completion is claimed only after observed parent/node
    resource-group absence. Keep the existing run's failure separate.
- **Phase ZG2: Add regression coverage and correct the create request**
  - [x] Task ZG2.1: Verify the documented API contract and add a failing request
    regression. Success: the real serialized SDK body must contain the required
    load-balancing block with valid service-default settings.
  - [x] Task ZG2.2: Add the smallest creation-only default configuration,
    preserving existing observed-setting tolerance and update behavior.
    Success: no public fallback, approval bypass or readiness relaxation.
- **Phase ZG3: Validate and document**
  - [x] Task ZG3.1: Run provider regressions, applicable race/lint/vet checks and
    update directly related documentation. Success: validation passes and live
    proof is still explicitly outstanding.
- **Checklist:** [x] ZG1.1; [x] ZG2.1; [x] ZG2.2; [x] ZG3.1.
- **References:** `requests.go`, `requests_test.go`, `backend_provision.go` and
  existing provider SDK-transport tests; official AFD origin-group API guidance
  to be verified before selecting creation defaults. Existing domain/specification
  folders were absent; the design/runbook and this breadcrumb remain applicable.
- **Decisions:** Supply explicit creation settings with sample size 4, successful
  samples 3 and additional latency 50 ms, as documented in Microsoft's AFD
  creation examples. Preserve the existing policy boundary: later group updates
  manage only probe/session settings, and inventory continues to tolerate
  observed load-balancing values. Do not copy examples' unrelated origin type,
  HTTPS, header, probe or approval settings.
- **References verified:** Microsoft's
  https://learn.microsoft.com/azure/frontdoor/create-front-door-cli
  and
  https://learn.microsoft.com/azure/frontdoor/standard-premium/how-to-enable-private-link-apim
  use the 4/3/50 creation settings. The Azure Verified Module
  `terraform-azurerm-avm-res-cdn-profile` also documents these defaults.
- **Before/after:** The serialized SDK request previously omitted
  `loadBalancingSettings`, which Azure rejected. The new regression first failed
  with `loadBalancingSettings = null`; the creation request now carries all
  three settings. Existing exact PATCH expectations are kept independent of
  create defaults so unwanted load-balancing resets remain detectable.
- **Deletion:** The independent worker submitted ownership-checked deletion at
  `2026-10-29T01:44:38Z` and subsequently verified absence of the parent RG and
  all three managed node RGs. The running proof then ended failed after deliberate
  infrastructure removal; its cleanup connection errors are expected teardown
  evidence, not a feature pass. No approval or HTTP delivery was obtained.
- **Implementation:** Added only the required creation block to `requests.go`.
  The existing request-JSON and alias-isolation tests cover the defaults.
  SDK transport regressions verify that creation includes the block, policy
  PATCH requests omit it, and different observed values do not trigger updates.
  `go test -count=1 ./pkg/providers/azure/frontdoor` now passes (5.350 seconds).
- **Validation:** `CGO_ENABLED=1 go test -count=1 -race ./pkg/providers/azure/frontdoor`
  passed (7.681 seconds); `go vet ./pkg/providers/azure/frontdoor` and
  `go run golang.org/x/lint/golint@latest -set_exit_status ./pkg/providers/azure/frontdoor`
  passed. The full pinned `./hack/tools/bin/golangci-lint-v1.64.7 run` passed.
  `goimports` was applied to the three changed Go files. These are local results,
  not proof that Azure accepts the corrected request or that traffic works.

### ZH: Cross-check AFD, WAF and PLS deployment references

- **Requirements / additional user input:** Inspect
  `C:\repos\aks1p-compliant-cluster` for settings that can expedite subsequent
  deployment and testing. This is read-only research, not authorization to
  deploy that repository, introduce WAF/HTTPS, approve connections or start
  another Fleet live run.
- **Phase ZH1: Inspect canonical templates and orchestration**
  - [x] Task ZH1.1: Compare the private HTTP/shared HTTPS AFD templates, PLS
    prerequisites and approval/deployment scripts. Success: distinguish
    reusable request requirements from incompatible application policies.
- **Phase ZH2: Record bounded next-run guidance**
  - [x] Task ZH2.1: Record the findings and preserve the existing proof boundary.
    Success: no changes in the reference repository and no relaxed approval,
    exposure, readiness or traffic assertions.
- **Decisions / implementation details:**
  - `src\bicep\cluster\publicendpoint\CreatePublicEndpointAFD.bicep:105-108`
    explicitly supplies 4/3/50, independently confirming ZG's creation fix.
    `src\bicep\cluster\afd\CreateAFD.bicep:53-56` agrees; the shared template
    uses 4/3 with a configurable additional latency defaulting to 50 ms.
  - The private HTTP template uses Premium/global, a disabled endpoint, an
    internal origin IP and a PLS resource ID/region without a custom-PLS
    `groupId`. Resolve the actual ILB frontend and PLS before constructing
    origins. Its complete resource graph and explicit route/origin dependency
    reinforce constructing children before demanding endpoint edge deployment;
    they do not justify weakening final Fleet deployment/readiness gates.
  - `src\scripts\Deploy-Public-Endpoint.ps1:978-1105` deploys the graph before
    polling for the AFD-managed Private Link request. Approval is asynchronous;
    customer-subscription PLS auto-approval does not approve AFD-managed private
    endpoints, which originate from a Microsoft-owned subscription.
  - `src\bicep\cluster\publicendpoint\ApprovePrivateLinkConnection.bicep`
    supports exact `expectedRequestMessage` correlation, but its empty default
    approves all pending requests. Do not copy that compatibility mode or its
    silent read-error fallback. Fleet retains the captured correlation checkpoint
    and explicit user approval; no approval script was executed.
  - WAF requires both a WAF policy and a separate
    `Microsoft.Cdn/profiles/securityPolicies` association with the endpoint and
    `/*` pattern. The private HTTP template's Prevention/body-inspection-disabled
    settings differ from the shared HTTPS template's Detection/body-inspection-
    enabled defaults. Neither is a Fleet default: current inventory deliberately
    rejects security policies and the approved proof excludes WAF.
  - Do not copy the donor's HTTP-to-HTTPS redirect, explicit origin Host override,
    `/health` probe or shared template's HEAD/HTTPS probes. Fleet's current
    contract remains HTTP-only, incoming Host preservation and GET/HTTP `/`
    probes every 30 seconds. The donor API version is 2025-06-01; Fleet's SDK
    requests use 2024-02-01. This comparison does not request an SDK/API upgrade.
  - `src\bicep\cluster\afd\CreateSharedAFD.bicep:266-274` provides optional
    access, health-probe and WAF logs through diagnostic settings. Access/probe
    logs are useful future troubleshooting aids, not enabled by this inspection;
    adding a workspace or changing deployed diagnostics needs appropriate scope.
  - `src\bicep\cluster\privatelink\DeployIstioGateway.bicep` keeps proxy
    protocol off unless the backend understands PROXY v2, and defaults visibility
    and auto-approval lists to empty. Its dual-stack standalone IPv4 LB workaround
    is not needed for the current IPv4 proof.
- **Changes made / before-after:** Recorded canonical request settings,
  asynchronous sequencing and policy incompatibilities in this breadcrumb.
  Only ZG changes production behavior. Reference source is not live validation;
  approval, HTTP delivery and withdrawal proof remain outstanding.
- **Checklist:** [x] ZH1.1; [x] ZH2.1. No new run, commit or history rewrite.

### ZI: Fold the required-setting fix into the four feature commits

- **Approval:** User explicitly requested committing and amending the outstanding
  work into the existing four commits. Preserve the protected parent, signing,
  attribution trailers, category boundaries and the complete reviewed source tree.
  Do not push.
- **Phase ZI1: Preserve and classify**
  - [x] Task ZI1.1: Confirm the five outstanding files and four-commit history.
    Documentation belongs in the documentation commit; the request builder and
    its regressions belong in the controllers/tests commit. No build or live-test
    patch changes are needed.
- **Phase ZI2: Fold and verify**
  - [x] Task ZI2.1: Create recovery refs, autosquash category-specific fixups and
    verify four signed commits, unchanged parent, exact source-tree preservation,
    unchanged build/live-test patches and a clean worktree.
- **Readiness:** Local correction and checks are complete, but live approval,
  delivery and withdrawal proof are not. At the user's readiness check on
  2026-10-28 at 20:35 PDT, the original authorization expires in less than five
  hours and the execution cutoff is earlier still. A fresh full retry cannot
  satisfy the existing six-hour minimum execution-budget guard. Renew the test
  window before another deployment; do not extend it silently or bypass the guard.
- **Results:** Folded the documentation and provider changes into their existing
  categories, preserving exactly four SSH-signed commits and protected parent
  `cd2bf54dc83dc20e295ac5432056c1e88fb54abc`. Verified all four signatures and
  required attribution/session trailers, exact source-tree equality with the
  pre-autosquash snapshot, unchanged build/live-test patches and a clean worktree.
  Recovery refs are
  `backup/afd-before-load-balancing-fold-20261029-033735` and
  `backup/afd-load-balancing-before-autosquash-20261029-033735`.
  This completion note is folded into the documentation category as well;
  no source change, push or live deployment is introduced by that final note.
- **Checklist:** [x] ZI1.1; [x] ZI2.1.

### ZJ: Retry the required-setting fix under renewed authorization

- **Approval / requirements:** After being told that a full retry requires
  renewing the test window, the user replied "ok go ahead" at
  `2026-10-29T03:39:49Z`. Proceed under the same disposable scope and a renewed
  12-hour window ending `2026-10-29T15:39:49Z` (08:39:49 PDT on October 29).
  Execution stops at `2026-10-29T14:24:49Z`, preserving 75 minutes for cleanup.
  Keep the six-hour minimum startup budget. This is an explicit new window,
  not a reset of the earlier attempt's recorded authorization.
- **Scope / decisions:** Same approved subscription, resource group and
  Sweden Central region; three two-node Standard AKS clusters with shared VNet,
  ACR/image publication, scoped roles and AFD Premium. Preserve anonymous ACR
  pull authorization and user-only approval at a correlated checkpoint. No WAF,
  HTTPS/custom domains, public backend, new diagnostics workspace or production
  publication. AKS Standard remains necessary to reproduce the approved harness.
- **Phase ZJ1: Isolate and preflight**
  - [x] Task ZJ1.1: Clone committed `f9819f9` into fresh session-local
    `afd-live-retry-load-balancing`, with fresh HOME/kubeconfig/logs and distinct
    ownership/image tags. Preserve the explicit TAG override, registry-digest
    gate before AKS creation and approved D2s_v5 harness overrides.
  - [x] Task ZJ1.2: Verify source, script syntax, tools, subscription access,
    preceding run termination and parent/node resource-group absence before
    creating resources. Success: no reuse of stale evidence or credentials.
- **Phase ZJ2: Run and observe**
  - [ ] Task ZJ2.1: Start the bounded runner with Go `-v`, nine-hour Go timeout
    and 8h30m Ginkgo timeout; arm a read-only approval-checkpoint monitor.
    Success: observe a running process and fresh phase/log progress.
  - [ ] Task ZJ2.2: Capture actual AFD origin-group creation, correlated pending
    approval and subsequent strict traffic/probe/withdrawal results. Success:
    do not report approval readiness or feature success before observing it.
- **Phase ZJ3: Verify cleanup**
  - [x] Task ZJ3.1: On completion, failure or cutoff, collect evidence, delete
    only the matching ownership-tagged environment, and verify absence of the
    parent and all three managed node resource groups.
- **Preflight evidence:** The exact committed source was cloned through WSL;
  only the three previously approved bootstrap/VM/tag/registry-gate harness
  overrides are retained. The VM overrides touch two scripts; no Go/controller
  code is changed in the clone. Fresh image tag is `f9819f9-load-balancing-1`;
  ownership tag is `afd-proof-run=20261029-033949-load-balancing`.
  Bash syntax and ShellCheck passed. With the existing isolated Docker config,
  buildx and Docker are available; the selected subscription is Enabled.
  Read-only Azure queries independently confirmed the parent and all three node
  groups absent. No resources were created by preflight.
- **Cleanup instrumentation:** The existing ownership-checked Azure wrapper
  prevents deletion unless the exact run tag matches. The new runner additionally
  checks all three node groups before writing its cleanup-complete marker.
  Early launcher failures are recorded so the approval monitor cannot wait
  indefinitely; duplicate launches cannot overwrite this attempt's evidence.
- **Observed attempt / blocker:** The bounded runner started at
  `2026-10-29T03:50:45Z` and immediately failed on the first resource-group create
  with `AuthorizationFailed` for
  `Microsoft.Resources/subscriptions/resourcegroups/write`. Authentication and
  subscription visibility work, but the subsequent ARM effective-permissions
  query returned only `actions: ["*/read"]` with no exclusions. This is a
  subscription authorization blocker, not evidence against the corrected AFD
  request. Restore the required resource-write and scoped role-assignment
  permissions through the user's normal access process; do not self-elevate.
- **Observed cleanup:** No resource group, ACR, cluster or AFD resource was
  created. The runner verified the parent and all three managed node groups
  absent. Run, launcher and cleanup-complete markers preserve exit code 2
  from the failed attempt; the nonzero cleanup marker is not a feature pass.
  The read-only approval monitor ended without a checkpoint. Both background
  shells have completed and their results were read.
- **Next attempt constraint:** Preserve this failed attempt's logs and use a
  fresh directory after access is restored. The renewed authorization deadline
  remains unchanged; do not reset it merely because startup failed.
- **Checklist:** [x] ZJ1.1; [x] ZJ1.2; [ ] ZJ2.1; [ ] ZJ2.2; [x] ZJ3.1.

### ZK: Retry after subscription access is restored

- **Approval:** User requested "try again now" at `2026-10-29T04:01:47Z`.
  The effective-permissions recheck now includes `actions: ["*"]` and empty
  `notActions`, in addition to read access. Independent reads again confirmed
  the parent and all three node groups absent. Source HEAD remains `f9819f9`;
  only this execution breadcrumb is uncommitted in the original repository.
- **Unchanged authorization:** Continue the existing renewed window ending
  `2026-10-29T15:39:49Z`, execution cutoff `2026-10-29T14:24:49Z`, 75-minute
  cleanup reserve and six-hour startup guard. Do not start a new 12-hour clock.
  Keep the same approved topology, resource scope and manual approval boundary.
- **Phase ZK1: Isolate and preflight**
  - [x] Task ZK1.1: Prepare fresh `afd-live-retry-load-balancing-2` from committed
    source, with fresh HOME/kubeconfig/logs and distinct
    `f9819f9-load-balancing-2` image and
    `afd-proof-run=20261029-040147-load-balancing-2` ownership tags.
  - [x] Task ZK1.2: Verify script syntax, fixed serialized SDK request, exact
    source and retained bootstrap/VM/tag/registry-gate harness overrides.
    Require previous termination and cleanup markers plus actual absence of all
    four groups. The preceding failure's cleanup marker preserves its exit code;
    observed Azure absence, not that code, establishes cleanup.
- **Phase ZK2: Run and observe**
  - [x] Task ZK2.1: Launch the bounded runner and read-only approval monitor,
    and verify fresh live process/log progress rather than assuming startup.
  - [ ] Task ZK2.2: Observe origin-group creation and correlated manual approval,
    then unchanged traffic/Host/probe/withdrawal assertions. Do not claim success
    merely because permissions or deployment are available.
- **Phase ZK3: Verify cleanup**
  - [x] Task ZK3.1: Collect evidence and verify absence of the ownership-checked
    parent and all three managed node groups after completion, failure or cutoff.
- **Preflight results:** Exact pinned source and the retained harness patch
  verified; serialized-request and alias-isolation regressions passed
  (0.024 seconds). Bash syntax, ShellCheck, Docker/buildx and unchanged
  authorization/cleanup-budget checks passed. No resources created by preflight.
- **Observed launch:** Actual attempt start is `2026-10-29T04:37:52Z`.
  Shell `afd-live-load-balancing-2` and read-only checkpoint watcher
  `afd-load-balancing-2-approval` are running; process and fresh log checks
  confirmed both. Azure resource-group creation now succeeded with the exact
  new ownership tag, independently verified through `az group show`.
  The runner has advanced to ACR creation. No approval checkpoint or HTTP proof
  is claimed at this stage; the authorization and execution deadlines above
  remain unchanged.
- **Provisioning progress at 2026-10-29T04:52:59Z:** Image publication completed
  and the pre-AKS registry gate recorded Gateway digest
  `sha256:0bf790a414a2c321a680898d0036f19f49bd48217af6acdff11097c6b25162b7`.
  Shared VNet and member subnets were created. Azure reports hub and member-1
  clusters Creating; no run/proof exit marker or approval checkpoint exists.
- **Provisioning progress at 2026-10-29T05:00:14Z:** All three AKS clusters
  report Succeeded; baseline Helm releases are installing/deployed.
- **Harness correction:** Inspection found `HUB_URL` empty in bootstrap output.
  The run-preparation line-ending normalization mistakenly included the compiled
  `bin/yq`, corrupting that binary; a direct version check segfaulted. Restored
  `yq` byte-for-byte from the previous unmodified bootstrap attempt and verified
  version v4.45.1. Do not normalize executable binaries; normalize only named
  text shell scripts. This was an agent-introduced execution-harness problem,
  not an AFD provider defect.
  - [x] Task ZK2.1a: After the main runner completes its Helm upgrades (Phase 3
    marker), repair the four member/MCS release `config.hubURL` values to the
    actual hub-admin context server. Wait for rollout and verify every deployed
    HUB_SERVER_URL. Waiting avoids concurrent Helm operations and later resets
    by the main runner. Preserve all test assertions and record any test outcome
    independently; no production source change or image rebuild is required.
- **Repair coordination:** The initial three-minute wait ended without changing
  any release because bootstrap was still pushing its workload image. This is
  not a main-run failure. A run-local repair observer now waits for the same
  safe Phase 3 boundary, bounded by the unchanged execution cutoff and stopped
  by main-run exit markers. It records checksum/version checks, Helm values,
  rollout completion and actual deployed URLs, and never repairs during cleanup.
- **Installed proof started:** The workload-image push completed, scoped roles
  and private-origin configuration were installed, and the Gateway deployment
  rolled out. The actual E2E spec started at approximately
  `2026-10-29T05:14:18Z`, passed BeforeSuite, and verified the running Gateway
  digest matches `0bf790a414a2c321a680898d0036f19f49bd48217af6acdff11097c6b25162b7`.
  It is creating internal-only member workloads and checking pre-route profile
  absence. The repair observer reached its safe boundary and started the four
  release upgrades; complete hub-URL verification and approval remain pending.
- **Repair verification correction:** The first observer upgraded and checked
  both member-1 Helm values successfully, but its final deployment query used
  Helm's `default` release-storage namespace. The actual deployments are in
  `fleet-system`, as both chart templates and live reads confirm. Correct the
  namespace, retain the exact two-controller URL assertion, explicitly check
  rollouts, skip already-correct Helm values, and append rather than overwrite
  the initial repair log. Member-1 has the correct live URLs; member-2 still
  needs its two upgrades. This observer failure does not alter main-test results.
- **Repair completed:** The corrected verifier succeeded: both MCS and both
  member-controller Helm values equal the actual hub-admin context URL, all four
  deployments rolled out, and exact live URL checks passed in `fleet-system`.
  The run-local `hub-url-repair-verified-utc.txt` marker and append-only repair
  log preserve the result. No Go source, image, assertion or readiness gate was
  changed to repair this harness input.
- **Live creation evidence at 2026-10-29T05:27:18Z:** Independent Azure reads
  show the owned profile and origin group provisioned Succeeded. The origin group
  contains sample size 4, successful samples 3 and additional latency 50 ms.
  This directly verifies Azure accepted ZG's corrected creation block, but does
  not establish deployed readiness or traffic. The endpoint remains Disabled,
  provisioning Succeeded and deployment NotStarted during graph construction.
  The spec reached pending-request observation at `2026-10-29T05:20:00Z`, but has
  not captured the all-origin manual-approval checkpoint.
- **Rate-limit observation:** Gateway logs report `rate limit reached`.
  The pinned cloud-provider rate-limit policy can return that exact error before
  sending an HTTP request; do not describe it as an Azure 429 without response
  evidence. The graph has nevertheless advanced to origin-group creation.
  No throttling configuration or assertion was changed as a workaround.
- **Observed failure:** The E2E pending-request assertion failed at
  `2026-10-29T06:06:56Z` (`gateway_private_http_test.go:121`) without ever
  capturing an approval checkpoint. No traffic/probe/withdrawal pass is claimed.
  The earlier 4/3/50 creation acceptance remains valid independent evidence,
  not completion of the full proof.
- **Cleanup progress at 2026-10-29T06:34Z:** Ginkgo deferred cleanup is still
  running, so final proof/run/launcher markers are not yet written. The hub and
  member-2 test namespaces are absent; member-1 is Terminating with content and
  content-preserving finalizers cleared. Earlier reads still showed the parent
  and all three managed node resource groups present. Do not claim cloud
  deletion until independently observed; the runner retains ownership-checked
  resource-group deletion after Ginkgo exits.
- **Additional teardown diagnostic:** Gateway-controller logs also report
  HTTPRoute status updates rejected because `status.parents` is required/null
  while the Gateway is being removed. Preserve this as a separate follow-up
  from the local rate-limit/provisioning stall; do not hide either with relaxed
  assertions or manual finalizer removal.
- **Final test result / cloud deletion:** Ginkgo completed with exactly one
  failed spec and exit code 1. The final assertion reported an origin Missing,
  not an observed pending manual approval request, after 2700 seconds.
  The approval monitor ended and its final output was read. The runner wrote
  `proof.exit=1` and `run.exit=1`, collected evidence and submitted ownership-
  checked resource-group deletion. At `2026-10-29T06:37:50Z`, the parent and
  all three managed node groups still existed; launcher and cleanup-complete
  markers remain pending. Deletion submission is not verified deletion.
- **Deletion diagnostics around 2026-10-29T07:02Z:** Member-2's node group is
  absent; the parent, hub node group and member-1 node group remain. Parent
  provisioning state is Deleting. The hub managed-cluster object is no longer
  listed, while member-1 reports Failed without exposed provisioning-error
  details. Its latest AKS operation subsequently reports InProgress, starting
  `2026-10-29T07:04:34Z`, with no end/error. No failed activity events were
  returned by the scoped recent-parent-RG query. Preserve the distinction
  between the cluster's Failed field and the active latest operation; no manual
  force deletion, retry, finalizer removal or scope expansion was performed.
- **Cleanup verified at 2026-10-29T07:13:02Z:** Independent Azure checks return
  false for the parent resource group and all three managed node groups. The
  runner separately logged parent/all-node absence, wrote its completion marker
  and exited. Its final shell result was read. `launch.exit=1` and
  `cleanup-complete.exit=1` preserve the failed test's result; they do not mean
  cloud deletion failed. The complete disposable environment is deleted; no new
  run started. Full private HTTP proof remains failed/unverified.
- **Checklist:** [x] ZK1.1; [x] ZK1.2; [x] ZK2.1; [x] ZK2.1a; [ ] ZK2.2; [x] ZK3.1.

### ZL: Publish the four-commit branch and refresh the draft PR

- **Approval:** User requested pushing to the `fork` remote and updating the PR.
  Publish only `rchincha/fleet-networking` branch
  `rchinchani/gep-1748-gateway-api`; do not push `origin` or launch Azure.
- **Phase ZL1: Verify destination and validation**
  - [x] Task ZL1.1: Confirm draft Azure/fleet-networking PR #400 targets main
    and uses the exact fork/branch. Preserve draft status and template structure.
    The fetched remote head is `05ea818688a20f6dac27e11b522adbee05ae614a`,
    matching the existing pre-timeout recovery ref, not new contributor work.
  - [x] Task ZL1.2: Refresh focused local checks and record exact commands/results
    alongside the failed live run and independently verified Azure deletion.
- **Phase ZL2: Fold and publish**
  - [x] Task ZL2.1: Fold outstanding execution notes into documentation, preserving
    four signed commits, required trailers, protected parent, all runtime source
    and build/live-test patches. Retain recovery refs.
  - [ ] Task ZL2.2: Push the verified branch to `fork` with an exact expected-head
    lease, refresh the PR body from `.github/PULL_REQUEST_TEMPLATE.md`, and verify
    remote/PR head equality and the clean worktree.
- **Reviewer scope:** Keep live delivery unproven. Report the missing-origin
  timeout and locally fixed, regression-verified limiter/HTTPRoute teardown
  issues separately from live creation/bootstrap evidence. Disclose run-local
  tooling/config repairs and do not claim clean turnkey E2E or hosted arm64 proof.
- **Destination/template verification:** Exact remote head equals the preserved
  `backup/afd-before-timeout-amend-20261028-194444` history. PR #400 is draft,
  targets main and has no closing issue reference. The staged PR-body artifact
  preserves every template heading, comment and checklist item in order;
  unsupported attestations remain unchecked. Publication validation was stopped
  when the user approved ZM; canceled checks are not passing evidence.
- **Checklist:** [x] ZL1.1; [x] ZL1.2; [x] ZL2.1; [ ] ZL2.2.

### ZM: Fix local admission starvation and HTTPRoute teardown status

- **Approval:** User said "ok proceed with the fixes." Finish these fixes before
  resuming ZL. This does not authorize another Azure deployment or approval.
- **Diagnosis:** The shared Azure policy rejects exhausted local token buckets
  immediately (`rate limit reached`); this is not evidence of an ARM HTTP 429.
  Rebuilding the last owned HTTPRoute parent into nil emits `parents: null`,
  which the installed Gateway API CRD rejects.
- **Phase ZM1: Regressions first**
  - [x] Task ZM1.1: Reproduce local admission failure with the actual SDK graph
    fixture beyond the default five-token burst. Verify two-origin activation
    retains all inventory checks and can fit the existing 30-second budget.
  - [x] Task ZM1.2: Reproduce last-parent withdrawal for missing Gateway/class,
    foreign class and removed references; inspect serialized patches, stable
    second reconciliation and real API-server acceptance using the existing suite.
- **Phase ZM2: Minimal fixes**
  - [x] Task ZM2.1: Use the existing token-bucket implementation's context-aware
    Wait for AFD requests. Preserve shared configured QPS/bursts, separate
    GET/HEAD and write pools, disabled/nil behavior, SDK retries and cancellation.
    Reject invalid enabled settings explicitly; do not disable limits, skip
    inventory, expand readiness or extend request deadlines.
  - [x] Task ZM2.2: Rebuild owned parent statuses into a non-nil empty array while
    preserving foreign status and optimistic-lock retries. Update related docs.
- **Phase ZM3: Validate and resume publication**
  - [x] Task ZM3.1: Run targeted regressions, affected race/envtest tests, offline
    E2E helpers, vet, golint and pinned lint; record exact outcomes.
  - [ ] Task ZM3.2: Fold code/tests and documentation into their existing signed
    categories, preserve exactly four commits, then finish the approved fork/PR
    publication with an exact lease.
- **Success criteria:** Regressions establish paced progress under default
  limits and schema-valid teardown without weakened safety checks. Publication
  distinguishes locally verified fixes from still-unproven live delivery.
- **References:** `cmd/hub-gateway-controller-manager/azure.go`;
  `pkg/providers/azure/frontdoor/{http_provision,staging,inventory}.go`;
  `pkg/controllers/hub/httproute/controller.go`; the existing Gateway envtest
  suite and pinned cloud-provider-azure token-bucket `Wait(context.Context)`.
  Domain-knowledge/specification directories remain absent as recorded above.
- **Checklist:** [x] ZM1.1; [x] ZM1.2; [x] ZM2.1; [x] ZM2.2;
  [x] ZM3.1; [ ] ZM3.2.
- **Red evidence:** The complete two-origin SDK activation fixture failed at
  request 6 of 22, after exactly five reads, with `rate limit reached`. All four
  last-parent unit cases rejected the null patch. The focused real API-server
  test independently reproduced `status.parents: Required value` and invalid
  null, not merely fake-client behavior. No production code had changed.
- **Initial green/correction:** Focused pacing/parent tests and the isolated real
  API-server regression passed. The first combined race run passed the provider,
  HTTPRoute and manager packages, but exposed a test-isolation error introduced
  by creating the existing fixed member namespace twice. The final regression
  now extends the existing Gateway teardown lifecycle instead of adding a second
  fixture or weakening namespace/API errors. The two-origin graph also passed
  with its shared read bucket already exhausted, retaining 22 requests, one
  write, original QPS/bursts and the same 30-second operation deadline.
- **Publication refresh:** The fetched fork and PR now both point to the known
  local pre-fix HEAD `f9819f9d93f6d14e8ce54ce83499a8dd5700aa92`.
  This is the already-known four-commit tree, not unknown contributor changes;
  use this refreshed exact lease for the later approved update.
- **Implementation:** `frontdoor.NewRateLimitPolicy` retains shared configured
  token buckets but uses cancellable admission, explicitly rejecting invalid
  enabled QPS/bursts. Manager construction remains lazy and preserves existing
  auth/retry/cloud settings. The complete two-origin SDK activation pass retains
  all 22 requests, including all inventory reads and its sole activation write.
  At 1 QPS/burst 5 the regression enforces at least 16 seconds with a fresh
  bucket, at least 21 seconds with an exhausted shared bucket, and less than the
  unchanged 30-second budget in both cases. No read/approval/ownership gate moved.
  HTTPRoute rebuilds into an explicit empty slice, retaining foreign statuses
  and optimistic-lock/no-op behavior.
- **Final local validation:** All commands below passed in WSL/Linux. The full
  race run includes the existing real API-server Gateway lifecycle, now checking
  the last-parent array after finalization. A test-only `goconst` finding was
  fixed with a shared foreign-controller constant; HTTPRoute race/vet and the
  full lint were repeated afterward.

  ```bash
  CGO_ENABLED=1 KUBEBUILDER_ASSETS=/root/.local/share/kubebuilder-envtest/k8s/1.33.0-linux-amd64 go test -count=1 -race ./pkg/providers/azure/frontdoor ./pkg/controllers/hub/gateway ./pkg/controllers/hub/httproute ./cmd/hub-gateway-controller-manager
  go test -count=1 -race ./pkg/controllers/hub/httproute
  go test -count=1 -tags=e2e -run '^TestPrivateHTTP' ./test/e2e
  go vet ./pkg/providers/azure/frontdoor ./pkg/controllers/hub/gateway ./pkg/controllers/hub/httproute ./cmd/hub-gateway-controller-manager
  go vet ./pkg/controllers/hub/httproute
  go vet -tags=e2e ./pkg/providers/azure/frontdoor ./test/e2e/...
  go run golang.org/x/lint/golint@latest -set_exit_status ./pkg/providers/azure/frontdoor ./pkg/controllers/hub/gateway ./pkg/controllers/hub/httproute ./cmd/hub-gateway-controller-manager
  ./hack/tools/bin/golangci-lint-v1.64.7 run
  ```

- **Boundaries:** Go files were formatted with isolated
  `go run golang.org/x/tools/cmd/goimports@v0.49.0`; no dependency manifests
  changed. `make reviewable` was not run as an aggregate. No new Azure
  deployment or approval occurred. Live delivery and clean turnkey-bootstrap
  proof remain blocked on a separately authorized successful run.
- **Publication preparation:** The refreshed PR-body artifact preserves every
  template heading, comment and checklist item/order, with exact passing local
  commands and explicit failed-live-run boundaries. Next fold only these seven
  Go files into controllers/tests and the two documentation files into docs.
  Preserve the build/live-test patches, signed four-commit structure and protected
  parent, then publish with the refreshed `f9819f9` exact lease.
- **Verified first publication and manual handoff:** The fixes and execution
  notes were folded into exactly four signed commits with required trailers,
  unchanged protected parent and identical build/live-test patches. The tested
  tree was preserved byte-for-byte. The explicit-lease push succeeded and the
  fork was independently verified at
  `f2e423f15f460c87fd1c869958574a9d4a3343d4`.
  Updating PR #400's body was rejected because the configured GitHub account
  cannot edit this public PR. A user-confirmed authentication retry returned the
  same denial; no permissions were bypassed. The user then said "i can push
  myself", so all further remote writes are handed off rather than retried.
  The prepared template-preserving PR body remains in session artifact
  `files/afd-pr-400-body.md`, with passing local checks and failed live proof
  reported separately. ZL2.2/ZM3.2 remain pending the manual PR update.
- **Final local audit note:** This handoff entry is documentation-only and is
  being folded into the documentation category, retaining exactly four signed
  commits and unchanged runtime/build/live-test trees. This rewrites the local
  commit IDs; the user's next push must use an exact lease against the verified
  published head above. No further push, PR edit, Azure deployment or approval
  is performed by the agent.

### ZN: Renew the same-scope live proof after local admission fixes

- **Approval:** User authorized the same-scope run for a fresh 12-hour window.
  The immutable start is `2026-10-29T15:59:50Z`, execution cutoff is
  `2026-10-30T02:44:50Z`, and expiry is `2026-10-30T03:59:50Z`. Preserve 75
  minutes for cleanup and the existing six-hour minimum startup budget. Failed
  preparation or launch attempts do not restart this clock.
- **Scope:** Same approved subscription, `swedencentral` and disposable resource
  group `fleet-afd-e2e-20261028-1951`: three two-node Standard_D2s_v5 AKS
  clusters, ACR/image publication with anonymous pulls, AFD Premium and scoped
  identities/roles. Existing Standard/shared-VNet dev/test topology is retained
  for this custom private-origin E2E, not replaced with AKS Automatic or new
  production add-ons. No GitHub writes. Approval remains manual after a captured,
  correlated Private Link checkpoint; the monitor never approves requests.
- **Phase ZN1: Read-only preflight**
  - [x] Task ZN1.1: Confirm effective Azure permissions include unrestricted `*`
    without exclusions, and parent/all three node resource groups are absent.
  - [x] Task ZN1.2: Create isolated checkout from local `962412c4878b94e8c4dfed521871e470ba82f25e`;
    copy pristine yq byte-for-byte, verify SHA256
    `99359cabfee9ade8d3217498d2e47eea63c1b7df72fbbcca3b6f30d97d6f1573`
    and execute its v4.45.1 version check before provisioning. Never normalize
    compiled binaries.
- **Phase ZN2: Guard the execution harness**
  - [x] Task ZN2.1: Reuse scoped owner-tagged Azure wrapper, bounded runner and
    dependency-order setup. Preserve candidate TAG and verify registry digest
    before creating clusters. Extract/validate the hub URL with kubectl and
    affirmatively verify all four member/MCS deployment values and rollouts
    before the proof. Fail explicitly rather than repairing live Helm state.
  - [x] Task ZN2.2: Syntax-check named shell scripts, verify immutable timer and
    exact candidate source/patch scope; keep Go code/test assertions unchanged.
- **Phase ZN3: Execute and preserve evidence**
  - [x] Task ZN3.1: Start the bounded verbose run and verify responsive progress
    and owned resource creation. Capture manual-approval checkpoint separately.
  - [x] Task ZN3.2: Record actual proof outcome and independently verify deletion
    of the parent and all three managed node resource groups.
- **Success criteria:** A new attempt starts with the tested admission/status
  fixes and guarded tooling within the renewed window. Only actual installed
  controller traffic/probe/withdrawal evidence can pass the live proof; cleanup
  must be independently observed. No prior failure is relabeled as success.
- **References:** ZK/ZM findings; existing e2e bootstrap/shared-VNet scripts and
  proof runner; the loaded Azure Kubernetes skill (no Azure AKS MCP is exposed,
  so the existing explicitly scoped Azure CLI harness is retained).
- **Checklist:** [x] ZN1.1; [x] ZN1.2; [x] ZN2.1; [x] ZN2.2;
  [x] ZN3.1; [x] ZN3.2.
- **Harness validation:** Named Bash scripts passed syntax checks and ShellCheck;
  immutable timer check and Docker server readiness passed. Windows-created
  script CRLF was corrected only in explicit text-script paths before launch;
  yq was independently rehashed and executed afterward, unchanged. Checkout
  differences are confined to bootstrap/shared-VNet execution overrides; all Go
  files and dependency manifests remain identical to candidate `962412c`.
  Registry digest must match the full SHA256 shape before any AKS creation.
  Run tag is `afd-proof-run=20261029-155950-paced-admission`; image tag is
  `962412c-paced-admission`. No resource was created by these validation steps.
- **Launch verified:** The bounded attempt is running in shell
  `afd-paced-live-run`; read-only checkpoint monitor is
  `afd-paced-approval-checkpoint`. Actual environment setup began
  `2026-10-29T16:20:12Z`. Independent startup verification confirmed Phase 1,
  exact source `962412c4878b94e8c4dfed521871e470ba82f25e`, responsive monitor
  and successful creation of the resource group with the matching run-owner tag.
  Current stage is baseline image publication/shared-VNet setup, not a passed
  Go E2E spec or an observed Private Link request. Authorization still ends at
  the original `2026-10-30T03:59:50Z`; launch does not reset its start.
- **Provisioning progress:** Baseline image publication finished and the Gateway
  registry digest was independently verified as
  `sha256:65e1b7b3ee79a935fd00337c0e2883afc70c3da0ef42477b1c922a4175096b09`.
  Azure reports hub/member-1 provisioning Succeeded and member-2 Creating.
  No terminal run/proof marker or manual-approval checkpoint has appeared;
  the installed-controller AFD proof has not started yet.
- **Installed proof progress/new blocker:** All three clusters and controllers
  deployed. All four member/MCS hub URLs and rollouts were verified at
  `2026-10-29T17:03:04Z`; no live Helm repair was required. BeforeSuite passed,
  and the installed Gateway image matches the registry digest above. The proof
  reached pending-request observation at `2026-10-29T17:05:06Z`.
  The controller is Running with zero restarts and the AFD profile provisioning
  is Succeeded, but actual origin PUT now fails with HTTP 403
  `LinkedAuthorizationFailed`: the hub identity has CDN origin-write permission
  but lacks `Microsoft.Network/privateLinkServices/read` on the linked member PLS.
  This is a new, explicit linked-resource permission blocker, not evidence of
  AFD HTTP 429 or the earlier local fail-fast admission error. No manual-approval
  checkpoint has been captured. A narrowly scoped Reader grant on each of the
  two run-owned PLS resources would supply the missing read action without
  granting connection approval or network writes; no such grant has been made.
- **Linked-read remediation approval/plan:** User explicitly approved "Grant
  Reader on the two run-owned PLS resources and continue." Before binding,
  verify the run-owner tag, hub kubelet principal/client against the actual 403,
  member node-group ownership and exact existing proof PLS identities. Grant
  Reader only at each of those two individual PLS resource IDs, verify persisted
  bindings and let the existing reconcile loop retry. Do not grant node-group/
  subscription Network Contributor, approve connections, restart the image or
  alter Go assertions/deadlines. Capture this as a live setup permission repair,
  not clean turnkey-bootstrap evidence; follow-up platform guidance/harness
  needs this linked-resource read prerequisite.
- **Reader repair persisted:** Verified parent run-owner tag, exact hub
  principal/client from the 403, both member node groups' managed-by cluster
  identities and both existing proof PLS IDs before modifying permissions.
  Built-in Reader bindings at only the two individual PLS scopes were created
  and independently listed/verified at `2026-10-29T17:29:37Z`; receipts are
  `member-{1,2}-hub-pls-reader.json` in the isolated run directory. The subsequent
  immediate observation still showed no origins and the previous linked-read
  403 status, so permission propagation/retry success is not claimed yet.
  The original proof remains running; no restart, deadline/image/assertion
  change, network-write grant or connection approval occurred.
- **Post-grant progress check:** Origins remain absent and no approval checkpoint
  or exit marker exists. The latest actual reconcile error is
  `2026-10-29T17:31:25Z`, still linked PLS read HTTP 403, shortly after the
  `17:29:37Z` grants. Error intervals grew from `17:14:43Z` to `17:20:18Z` to
  `17:31:25Z`, consistent with reconcile backoff; the unchanged status is not
  evidence of a fresh failure on every status read. Recent error records contain
  no old `rate limit reached` failure. The original 45-minute pending-request
  observation budget ends around `17:50Z`; it has not been extended.
- **Final proof outcome:** Failed at `2026-10-29T17:51:38Z` after the unchanged
  2700-second pending-request wait: the expected origin remained Missing.
  One spec failed, 43 skipped. No manual-approval checkpoint, traffic, probe or
  withdrawal pass was obtained; no connection was approved.
- **New final ARM blocker:** Preserved controller evidence shows the Reader
  repair cleared the linked-read 403 on a later retry. At `17:48:17Z` and
  `17:48:31Z`, origin creation instead returned HTTP 400 BadRequest:
  `OriginHostHeader "" is invalid. It must be a valid domain name, IP version 4,
  or IP version 6.` The final failure is therefore not the old local limiter or
  the initial missing Reader binding. The origin request's explicit-empty versus
  omitted host-header semantics must be investigated without silently replacing
  the incoming-Host preservation contract.
- **Cleanup verified:** Runner submitted owner-checked deletion and exited;
  approval monitor also ended without a checkpoint. Independent Azure queries
  at `2026-10-29T18:23:06Z` confirmed the parent and all three managed node
  resource groups absent. `proof.exit`, `run.exit`, `launch.exit` and
  `cleanup-complete.exit` are all 1 because the failed proof result is retained,
  not because cloud deletion failed. No live attempt remains active. Completing
  this execution/cleanup task does not complete the blocked live feature proof.

### ZO: Correct origin Host-header serialization after the live rejection

#### Requirements and additional comments from user

- User approved further code fixes: "ok if more code fixes are needed go ahead".
  This authorizes the following local tests-first correction, not another Azure
  run, broader IAM grants, GitHub writes or commit-history rewriting.
- Preserve incoming application Host, private-only Premium origins, strict
  ownership/readiness checks, manual approval and existing deadlines/rate limits.
- Correct the exact serialized request rejected in ZN. Document the separately
  established hub identity linked-PLS read prerequisite.

#### Plan

1. **Phase 1: Regression tests**
   - [x] **Task 1.1:** Require absent `originHostHeader` in serialized SDK create
     requests and actual SDK PUT/PATCH transport bodies. Reject nonempty desired
     overrides; require observed nil and empty representations to converge.
   - [x] **Task 1.2:** Require existing nonempty overrides, alone or with other
     mutable drift, to fail explicitly without writes or success-shaped results.
     Success criterion: demonstrate the original implementation fails the new
     wire-shape and unsafe-update regressions.
2. **Phase 2: Minimal fix and operational documentation**
   - [x] **Task 2.1:** Omit the optional create/update Host override. Refuse
     automatic migration of observed nonempty overrides because omission in a
     PATCH is not a demonstrated clearing operation.
   - [x] **Task 2.2:** Update operator/chart/live-test prerequisites with hub
     identity `Microsoft.Network/privateLinkServices/read` on each linked PLS,
     distinct from member discovery and connection approval. Document omission
     semantics, override drift and the outstanding live proof.
3. **Phase 3: Validation and handoff**
   - [x] **Task 3.1:** Run targeted red/green tests, affected provider race tests,
     offline E2E helpers, goimports, vet, golint and pinned lint.
   - [x] **Task 3.2:** Record exact results and before/after behavior; preserve
     four existing signed commits and leave publication to the user.

#### Decisions and references

- Microsoft Learn, https://learn.microsoft.com/azure/frontdoor/origin, fetched
  for this fix: when ARM or another method does not explicitly set this field,
  AFD forwards the incoming hostname. The portal's auto-populated backend
  hostname is not the desired contract.
- Pinned `armcdn/v2 v2.2.0`, `models_serde.go:717-732,819-832`: both create and
  update serializers pass the pointer through `populate`; tests will inspect
  resulting JSON rather than infer behavior from pointer values.
- No assertion that JSON null, an explicit empty string or omitted PATCH fields
  clear an existing override. Such drift remains unready and requires explicit
  platform intervention; unrelated updates must not silently retain it.
- Relevant local source: `requests.go`, `backend_provision.go`,
  `observation.go`, `origin_cleanup.go` and their existing fixtures. Existing
  observation and stale-origin cleanup rejection of nonempty overrides stays
  intact. No dependency changes are planned.
- `.github/.copilot/domain_knowledge` and `.github/.copilot/specifications`
  remain absent; the how-to and ZN live evidence define the applicable contract.

#### Implementation details, changes and before/after

The targeted pre-fix run failed as required: serialized create requests and
actual SDK PUT/PATCH bodies contained `originHostHeader: ""`; both override
cases attempted an unexpected PATCH rather than refusing the incompatible
snapshot. Nil/empty observation and desired nonempty override rejection already
passed. Command:

```bash
go test -count=1 -run "TestBuildBackendRequests|TestHTTPPreview|TestBackendProvisionerStagesPrivateResources|TestBackendProvisionerRejectsOriginHostOverride" ./pkg/providers/azure/frontdoor
```

The corrected builder leaves the optional Host override unset. Origin updates
omit it too, but only after refusing a nonempty observed override with an
explicit error. Existing observation/inventory/cleanup comparisons are unchanged.
The fixtures assert field absence in actual SDK PUT and PATCH bodies as well as
standalone serialization; desired nonempty overrides still fail preflight.
Both nil and explicit-empty Azure observations converge.

The expanded targeted command passed, including the complete preview request
graph and paced SDK activation regressions (37.363 seconds):

```bash
go test -count=1 -run "BuildBackendRequests|HTTPPreview|BackendProvisioner(StagesPrivateResources|RejectsOriginHostOverride)" ./pkg/providers/azure/frontdoor
```

Operator, chart and live-test prerequisites now distinguish hub linked-PLS
Reader access from AFD resource-group permissions, member discovery and manual
connection approval. No role assignments are performed by this fix.

#### Validation and completed handoff

After isolated `goimports@v0.49.0` formatting of the five changed Go files,
all of the following passed:

```bash
CGO_ENABLED=1 go test -count=1 -race ./pkg/providers/azure/frontdoor ./cmd/hub-gateway-controller-manager
go test -count=1 -tags=e2e -run "^TestPrivateHTTP" ./test/e2e
go vet -tags=e2e ./pkg/providers/azure/frontdoor ./cmd/hub-gateway-controller-manager ./test/e2e/...
go run golang.org/x/lint/golint@latest -set_exit_status ./pkg/providers/azure/frontdoor ./cmd/hub-gateway-controller-manager
./hack/tools/bin/golangci-lint-v1.64.7 run
git diff --check
git diff --exit-code -- go.mod go.sum
```

- Provider race tests: 47.397 seconds; manager: 1.881 seconds; offline E2E
  helpers: 0.097 seconds. Existing rate-limit, approval, observation/inventory
  and stale-origin cleanup coverage passed with the new serialization.
- No Gateway/HTTPRoute source changed in this correction; their prior full
  race/envtest evidence remains historical and was not rerun or represented as
  a new live pass. No dependencies, rate limits, operation/test deadlines,
  controller identity configuration or Azure resources were changed.
- Reviewed the full source/doc diff. The exact four existing signed commits
  remain unchanged (`git rev-list` count 4 above the protected parent). New
  source/tests/documentation and the pre-existing ZN execution notes remain
  uncommitted; no commit, rebase, push or GitHub authentication retry occurred.
- Updated the existing unposted session PR-body artifact after re-reading the
  repository template, preserving headings/comments/checklists. It now records
  ZN's linked-read repair, final Host-header 400, exact source/image, verified
  cleanup and current offline evidence. Publication remains the user's handoff.
- **Before:** create and drift-update bodies sent `originHostHeader: ""`, and
  nonempty actual overrides triggered invalid clearing attempts. **After:**
  create and compatible updates omit the property; incompatible overrides
  produce an explicit error and zero writes, never reported convergence.

The local correction is complete. Azure convergence, actual traffic/Host/probe
behavior, installed-controller withdrawal and a passing live end-to-end proof
remain blocked pending a separately requested authorized run with these fixes
and the linked-PLS read prerequisites in place. No run was started.

### ZP: Publish the Host fix before the next live attempt

- **Explicit approval:** "commit/amend outstanding changes and push first and
  then start live e2e test". This supersedes the earlier manual-publication
  handoff, but does not request another PR-body edit or broader Azure scope.
- **Phase ZP1: Publish**
  - [x] ZP1.1: Verify the fetched fork head, record a recovery ref and exact
    expected final tree, and fold changes into their four signed categories.
    Place the chart README in the build commit and test runbook in the live-test
    commit because those sections/files are introduced by those commits.
  - [x] ZP1.2: Verify four signed commits, unchanged protected parent, required
    trailers and exact final tree; push to `fork` with the fetched-head lease
    and independently confirm the remote hash before any Azure creation.
- **Phase ZP2: Start the same-scope proof**
  - [x] ZP2.1: Prepare a fresh isolated checkout/image and owner-tagged harness,
    retaining pristine yq, digest/hub-URL checks and original assertions.
    Include hub Reader on the two actual run-owned member PLS resources as
    soon as they exist; verify parent ownership, managed-node ownership,
    resource identities and exact per-PLS role bindings. Do not grant hub
    network-write access, node-group Reader or connection approval.
  - [ ] ZP2.2: Independently recheck permissions and absence of the parent and
    three managed node groups. Start only after publication and with at least
    six hours before `2026-10-30T02:44:50Z`; verify responsive owned provisioning
    and capture the later correlated manual-approval checkpoint.
- **Immutable authorization:** Retain ZN's original start
  `2026-10-29T15:59:50Z`, expiry `2026-10-30T03:59:50Z` and 75-minute cleanup
  reserve. The latest allowable startup is `2026-10-29T20:44:50Z`. This request
  does not reset that clock. If preparation overruns it, do not create resources;
  obtain renewed authorization.
- **Scope and decisions:** Same approved subscription, `swedencentral`,
  disposable resource group, three two-node Standard_D2s_v5 clusters, existing
  Standard/shared-VNet custom dev/test topology, ACR/image publication, AFD
  Premium and scoped identity roles. No Azure AKS MCP is exposed; reuse the
  scoped CLI harness. Manual asynchronous Private Link approval stays manual.
  README-only build/live category updates are intentional; their executable
  code is unchanged by the fold.
- **References:** ZN authorization/cleanup and least-privilege Reader approval;
  ZO validated Host fix; existing bootstrap/runner/proof assertions and loaded
  Azure Kubernetes skill.
- **Success criteria:** Published exact corrected candidate, then an observed
  running bounded attempt. Starting is not passing E2E; subsequent traffic,
  Host/probe, withdrawal and independent cleanup evidence remain required.
- **Publication completed:** Exact validated tree
  `ec1e546c954c7d291a19225182bb4966420b43eb` was preserved through signed
  autosquash and contiguous trailer normalization. Final four commits:
  documentation `ab0a8cc181c91fdc9569e496b65a8afce2c6f2d5`, controllers
  `89168c8867a834da4cef87735e0ce30510bc5de5`, build
  `0eb03ea20eaeabe4306815d9c5acb81933f584d2`, live tests
  `428de9e8c4629e50265101afcd59c47d86c00783`. All signatures verified and
  both required trailers are in each final trailer block. Source worktree was
  clean. Recovery refs preserve the original and the complete fixup tree.
- **Lease protection:** Initial exact lease rejected a concurrent publication
  of our known `7f0133d` fixup head. Fetched it, verified the tree was identical
  to the validated local tree, then replaced only that exact head with the
  four-commit form. Independent `ls-remote` verified `428de9e` at
  `2026-10-29T20:39:48Z`. No unexpected source tree was overwritten and no
  PR-body/authentication retry occurred.
- **Explicit renewed authorization:** With insufficient setup margin before
  the old latest-start time, asked for a fresh same-scope 12-hour window. User
  approved. New immutable start is `2026-10-29T20:41:45Z`, expiry
  `2026-10-30T08:41:45Z` and execution cutoff `2026-10-30T07:26:45Z`.
  Preserve 75 minutes for cleanup and six hours before execution cutoff at
  startup. Preparation failures do not reset this renewed clock.
- **Preparation:** Fresh attempt directory `afd-live-retry-host-header`, source
  `428de9e`, image tag `428de9e-host-header`, owner tag
  `afd-proof-run=20261029-204145-host-header`. Pristine yq SHA256 remains
  `99359cabfee9ade8d3217498d2e47eea63c1b7df72fbbcca3b6f30d97d6f1573`.
  A native-to-WSL inline quoting error initially placed the fresh verified clone
  at `/checkout`; verified its exact origin/commit and safely moved that clone
  into the isolated attempt, without deleting or touching unrelated content.
  The known execution-only patch applied and Docker reports server 29.1.3.
  Subsequent variable-containing Bash commands use explicit script files.
  No Azure resource has yet been created.
- **Ready-to-launch validation:** Bash syntax, ShellCheck, exact isolated source
  and two-file execution override scope, unchanged Go/manifests, yq hash/version,
  Docker responsiveness, immutable authorization arithmetic and six-hour
  startup budget passed. Fresh effective-permissions observations contain
  unrestricted `*` with no exclusions, the subscription is Enabled and all four
  resource groups are absent. One ShellCheck trap diagnostic was corrected
  with an ordinary exit-recording function, without suppressing the check.
  The new helper matches each PLS to its actual internal LoadBalancer Service,
  checks identical proof namespaces and independently observes the exact hub
  Reader bindings. Helper errors remain explicit and cannot produce run success;
  it is stopped/reaped before resource cleanup if the proof ends early.
  The original verbose Go proof, assertions, all 45-minute waits and the
  read-only manual-approval checkpoint monitor are unchanged. Only the two
  known bootstrap/shared-VNet execution overrides differ from the published
  checkout. Subsequent breadcrumb entries are post-publication run tracking,
  not uncommitted controller fixes.
- **Launch verified:** Bounded attempt started at `2026-10-29T21:09:04Z`,
  after verified fork publication, in shell `afd-host-header-live-run`.
  Independent startup verification confirmed the exact published source
  `428de9e8c4629e50265101afcd59c47d86c00783`, responsive Phase 1 progress and
  successful parent resource-group creation with the new matching ownership
  tag. The read-only monitor is `afd-host-header-approval-checkpoint`.
  Current phase is baseline image publication/shared-VNet setup, not an
  executed or passing private HTTP Go spec. No approval checkpoint has been
  reached and no PLS connection was approved. The renewed clock remains
  `20:41:45Z` through `08:41:45Z`; starting does not reset it. ZP2.2 remains
  open for the later manual-approval checkpoint and actual proof outcome.
- **Startup failure, not a feature result:** Image publication stopped before
  any AKS/AFD provisioning: the fresh isolated Docker configuration lacked the
  Buildx CLI plugin (`docker: unknown command: docker buildx`). Docker engine
  readiness alone did not verify this build prerequisite. The checkpoint
  monitor ended without a checkpoint; the runner deleted its owned parent/ACR
  and observed all four groups absent. Exit markers are 2, preserving the setup
  failure, not indicating failed deletion. No Go proof spec ran.
- **Bounded recovery plan:** Prepare a separate
  `afd-live-retry-host-header-2` attempt without overwriting failed evidence.
  Copy only the existing known working `docker-buildx` binary from the prior
  paced-admission attempt's isolated `docker-config/cli-plugins`, never Docker
  authentication/configuration. Verify byte identity and `docker buildx version`
  inside the new isolated environment before Azure creation; add that check to
  preflight. Reconfirm all four group absences independently. Source stays
  `428de9e` and the original renewed clock stays `20:41:45Z` to `08:41:45Z`;
  this failure does not restart authorization or broaden scope. Use a distinct
  image/owner tag and the same manual approval and exact-PLS Reader guards.
- **Recovery preflight passed:** Restored only the existing Buildx executable,
  SHA256 `e25d5cd675092bb5d1d03632fdc46cdf3fede7b514dc5f023a105e00d3ce5ad4`,
  and verified `docker buildx v0.38.1` in the fresh isolated Docker configuration.
  New preflight also independently confirmed the failed attempt's parent and
  all three managed node groups absent, effective permissions, source and
  unchanged Go/dependencies, yq identity, Docker/ShellCheck and startup budget.
  No new Azure resource was created by recovery preflight. The isolated
  bootstrap additionally checks Go/manifests remain unchanged after `make push`
  (which invokes tidy), before any cluster creation. The startup verifier now
  requires actual BuildKit build-definition progress, not engine readiness and
  resource-group creation alone. Source stays `428de9e`; new image tag is
  `428de9e-host-header-2`, owner tag `20261029-204145-host-header-2`; the
  authorization start/expiry and test assertions are unchanged.
- **Recovery launch verified:** Separate retry started at
  `2026-10-29T22:00:53Z` in shell `afd-host-header-live-run-2`; read-only monitor
  is `afd-host-header-approval-checkpoint-2`. Startup verification now observed
  actual BuildKit build-definition progress as well as exact published source
  and matching new owner-tagged parent creation. Image/environment setup is
  running; no Go proof spec, correlated approval checkpoint or ingress pass
  is claimed. The initial attempt remains preserved as a failed setup with
  verified cleanup. The same renewed authorization still expires
  `2026-10-30T08:41:45Z`, with execution stopping at `07:26:45Z`.
- **Progress/avoid duplicate run:** At `2026-10-29T22:30:12Z`, the existing
  retry has completed baseline image publication and passed the candidate
  digest/unchanged-source gates before cluster creation. Registry digest is
  `sha256:9da8216632acad8e6dcb0b5a757a2c73d0b217693983d958b7e3db23a87c5cfa`.
  The log has advanced into AKS provisioning: member-1 creation was submitted
  and member-2 is waiting for AAD role propagation. No terminal exit markers
  exist. This is the already-authorized attempt, not a reason to start a second
  simultaneous environment. Controller installation and the actual Go private
  HTTP spec/manual-approval checkpoint remain later phases.
- **Terminal authorization/cleanup blocker:** The retry exited before controller
  installation or the Go proof. Member-2 setup reported subnet role-assignment
  propagation failure; fetching hub admin credentials then returned HTTP 403
  for `managedClusters/listClusterAdminCredential/action`. The owner-checked
  cleanup also returned HTTP 403 for `resourcegroups/delete` and did not verify
  deletion. `run.exit=2`, `launch.exit=1`, and no `cleanup-complete.exit` exists.
  At `2026-10-29T23:08:04Z`, this is an Azure authorization failure, not a new
  AFD/Host-header error or passing test. Resources may remain billable; cleanup
  is the priority. Do not retry provisioning or silently escalate privileges.
- **Immediate plan:** Preserve failed evidence, read current Azure permissions,
  group ownership and cluster states using the same scoped identity, then ask
  the user to restore the previously approved access needed for owned cleanup
  (or perform that cleanup themselves). PIM expiry is a possibility, not an
  established root cause. No permission/security bypass, alternate identity,
  broader role grant, resource reuse or new run is authorized by this failure.
- **Independent inspection:** Current subscription permissions now have one
  permission set and no unrestricted `*` grant, unlike the passing preflight.
  The parent still exists with matching owner
  `20261029-204145-host-header-2`; all three managed node groups still exist
  with expected AKS `managedBy` identities. All three AKS resources report
  `Succeeded`. Thus cleanup is confirmed blocked with retained resources,
  not merely unverified. No AFD resources/controller proof were reached.
- **Cleanup recovery approval:** User selected "Access restored—retry
  owned-resource cleanup". Recheck the exact parent ownership tag and managed
  child identities, request deletion only through the existing guarded wrapper,
  await parent absence and independently query all three node groups. Preserve
  original failed setup exit markers and record cleanup retry results separately.
  Do not self-grant permissions, switch identities or restart provisioning as
  part of this cleanup action.
- **Recovery still blocked:** After the user's restoration confirmation, the
  validated owner-checked cleanup retried but Azure again denied
  `resourcegroups/delete`; no deletion was submitted. A fresh same-identity
  inspection at `2026-10-29T23:16:16Z` shows the only allowed action pattern is
  `*/read`, with no exclusions. The parent and all three managed node groups
  remain, and the AKS resources still report `Succeeded`. `cleanup-retry.exit=1`
  and no cleanup-complete marker exists. Restored write access is not yet
  effective for this Azure CLI identity. No credentials were cleared, account
  switched, privileges self-granted or new run started. User/authorized-owner
  action remains required; retained resources may continue incurring cost.
- **Additional requested cleanup retry:** User requested another attempt at
  `2026-10-29T23:20Z`. The existing guarded script again verified ownership but
  Azure denied `resourcegroups/delete` with `AuthorizationFailed`. No deletion
  was submitted and no cleanup-complete marker was created. No provisioning,
  identity switch or role grant was attempted.
- **Manual cleanup verified:** User deleted the parent resource group. Read-only
  verification at `2026-10-29T23:46:06Z` independently confirmed the parent and
  all three managed node groups absent. Manual verification exit is 0; the
  original failed setup exit remains 2 and is preserved in the cleanup-complete
  marker. No further deletion or provisioning was performed for verification.
- **Next attempt approved:** User reported authorization fixed and explicitly
  requested that a new test can be started. Prepare a separate
  `afd-live-retry-host-header-3` from the same published `428de9e` candidate.
  Recheck actual effective permissions and group absence before any creation,
  preserve the restored Buildx/yq identity and post-build source/digest gates,
  and retain exact-PLS Reader/manual approval behavior. Inspect any visible
  current-principal PIM activation lifetime to avoid repeating the temporal
  authorization failure. Do not silently switch identity or grant new roles.
  The existing authorization still expires `2026-10-30T08:41:45Z`; original
  execution cutoff is `07:26:45Z`, with at least six hours before cutoff at
  startup. No authorization clock is reset by this new attempt.
- **Observed IAM lifetime:** Effective unrestricted subscription access is
  restored, but the current subscription Owner activation ends
  `2026-10-30T07:17:55.477Z`, earlier than the authorized resource lifetime.
  Keep the authorization expiry unchanged and conservatively shorten this
  attempt's execution cutoff to `06:02:55Z`, retaining a full 75-minute cleanup
  reserve before actual Owner expiry. The launch guard now requires at least
  75 minutes before both authorization and IAM expiry; this tightens the budget
  rather than extending it. The six-hour startup guard remains unchanged.
  If preparation misses its resulting latest start `00:02:55Z`, do not create
  resources; request renewed/extended IAM activation instead of guessing that
  permissions will remain valid.
- **Attempt 3 startup result:** Preflight passed, including tooling, source,
  permissions and resource-group absence checks. The launcher independently
  rechecked all four group absences, then refused with `Insufficient remaining
  execution budget; no resources created.` Its six-hour startup deadline had
  passed; UTC was independently observed as `2026-10-30T00:03:38Z` afterward.
  Launch and read-only approval monitor both exited 1. No build, cluster
  creation, Go proof or approval checkpoint started. Preserve these exit
  markers rather than reusing the attempt directory. Further execution is
  blocked on renewed/extended actual Owner activation; the existing resource
  authorization still expires `08:41:45Z` and permits startup only through
  `01:26:45Z` even if IAM is extended. Do not weaken either budget guard.
- **Renewal verification:** After the user selected renewed activation and
  retry, a direct PowerShell Azure CLI current-principal schedule lookup at
  `2026-10-30T00:05:10Z` still showed the same provisioned subscription Owner
  activation ending `07:17:55.477Z`, with no pagination. Renewal is not yet
  observable, so no additional attempt or resources were started. A full
  extension through authorization expiry is sufficient but not necessary:
  the verified IAM lifetime must allow six hours of execution plus 75 minutes
  for cleanup at actual launch. A fresh eight-hour activation can satisfy that
  while the original resource authorization remains unchanged; compute the
  execution cutoff from the earlier authorization/IAM deadline.
  A second lookup after another user confirmation, at `2026-10-30T00:11:27Z`,
  reproduced the unchanged `07:17:55.477Z` Owner expiry. Execution remains
  blocked; do not repeatedly prompt or launch against unobserved renewal.
- **Owner reactivation verified / attempt 4 plan:** Following the user's
  reactivation and request to check, a direct PowerShell lookup at
  `2026-10-30T00:20:54Z` confirmed provisioned subscription Owner from
  `00:18:06.507Z` until `08:18:04.863Z`. Prepare a fresh
  `afd-live-retry-host-header-4` checkout at published `428de9e`, with image tag
  `428de9e-host-header-4` and ownership `20261029-204145-host-header-4`.
  Preserve the original authorization and all approved topology, permissions,
  manual-approval and proof gates. Conservatively floor IAM expiry to
  `08:18:04Z`, stop execution at `07:03:04Z`, and require launch by `01:03:04Z`.
  Copy only named harness scripts, public SSH key and verified tool binaries;
  never copy old credentials, Docker configuration or execution evidence.
  Run syntax, ShellCheck and complete preflight again before launch, then
  require actual BuildKit/source/owned-group startup verification.
- **Attempt 4 launched and startup verified:** Fresh preparation and complete
  preflight passed. Confirmed the isolated bootstrap preserves the candidate
  tag, refuses post-build Go/module drift, and validates the registry digest
  before AKS creation. The guarded launcher started at
  `2026-10-30T00:24:26Z`, leaving more than six hours before `07:03:04Z`.
  Independent startup verification confirmed actual BuildKit build-definition
  progress, exact published source `428de9e8c4629e50265101afcd59c47d86c00783`,
  and the newly owned parent resource group with attempt-4 tag.
  Active shells are `afd-host-header-live-run-4` and read-only
  `afd-host-header-approval-checkpoint-4`. Current phase is baseline image
  publication/shared-VNet setup, not a passed Go E2E proof. No Private Link
  connection was approved; the correlated checkpoint still requires explicit
  user approval. Execution remains bounded by the recorded IAM-aware cutoff,
  with guarded cleanup reserved before Owner expiry.
- **Attempt 4 failed setup; cleanup verified:** Image publication completed
  with Gateway digest
  `sha256:923f8e03fe0871e22972d64f5183f0c28f0acea60dd509c5f776b3757d6f8d0f`.
  The post-build Go/module drift gate passed. Hub AKS creation was submitted,
  but shared-VNet creation failed with `AuthorizationFailed` for
  `Microsoft.Network/virtualNetworks/write`. No member setup, Go private-HTTP
  proof or approval checkpoint was reached. Run and launch exited 2;
  `cleanup-complete.exit=2` preserves the failed setup result despite successful
  cleanup. The runner submitted owner-checked deletion and observed the parent
  plus all three managed node groups absent. Independent direct PowerShell
  queries confirmed all four absent at `2026-10-30T01:22:38Z`.
  At that same inspection, effective subscription permissions included both
  `*/read` and unrestricted `*` with no exclusions. This does not reproduce
  the earlier read-only state: the cause of the Network write denial remains
  unproven, and permission propagation is only a possibility. Both run and
  checkpoint-monitor processes have terminated. The attempt-4 latest startup
  `01:03:04Z` is now past; do not retry under its unchanged IAM budget or
  claim live Host/traffic proof from successful image publication.
- **Attempt 5 authorization and plan:** The user reported fixed roles and
  explicitly requested another live test. Direct PowerShell inspection at
  `2026-10-30T01:37:54Z` confirmed unrestricted effective permissions and a new
  provisioned Owner activation from `01:28:11.853Z` until `09:28:09.477Z`.
  The prior resource authorization no longer had six hours plus cleanup
  available. The user explicitly approved a fresh same-scope 12-hour window,
  fixed at `2026-10-30T01:39:00Z` through `13:39:00Z`; preparation and failures
  do not reset it. Actual execution is more tightly bounded by Owner expiry:
  stop at `08:13:09Z`, preserve 75 minutes before `09:28:09Z`, and require
  launch by `02:13:09Z`.
  Prepare isolated `afd-live-retry-host-header-5` at published `428de9e`,
  image `428de9e-host-header-5`, owner `20261030-013900-host-header-5`.
  Preserve topology, source, proof assertions, scoped roles and manual
  approval. Before publishing images or provisioning AKS, the isolated
  bootstrap will create the same approved `fleet` shared VNet in the newly
  owned group, with the existing `10.0.0.0/8` address range. This verifies the
  exact previously denied Network write early, rather than treating the
  effective-permission listing as proof of that operation. The ordinary later
  shared-VNet step remains unchanged. Record successful Network write before
  accepting startup verification. No extra resources or permissions, shared
  authentication mutation, Go changes or production source changes are planned.
  Repeat syntax, ShellCheck, source/tool identity, absence and time preflight;
  launch only if all guards pass.
- **Attempt 5 started; previous failing operation passed:** Complete preflight
  passed, including syntax and ShellCheck for the preparation, activation and
  startup helpers. The launcher started at `2026-10-30T01:42:38Z`.
  The intended shared-VNet creation succeeded at `01:43:06Z`, affirmatively
  passing the exact Network write previously denied. Startup verification at
  `01:44:10Z` confirmed that write, actual BuildKit execution, exact published
  source and the newly owned resource group. The latest progress observation
  shows baseline controller image build/publication in progress, with no run,
  launch or cleanup exit marker. AKS setup and the Go proof are not yet proven.
  Active processes are `afd-host-header-live-run-5` and read-only
  `afd-host-header-approval-checkpoint-5`; startup verifier completed 0.
  No connection approval was performed, no Go assertions were changed, and
  execution/cleanup remain bounded by the recorded authorization and IAM times.
- **Attempt 5 ended at a later AKS authorization denial:** Images published;
  Gateway digest is
  `sha256:5bf02793859c1a1c6114853af91e74eaa49486754d9d066c631499246c61a311`.
  The ordinary later VNet step and both member subnet creations also succeeded.
  Hub AKS submission was accepted, but creating `member-1` returned
  `AuthorizationFailed` for `Microsoft.ContainerService/managedClusters/write`.
  The Go E2E proof, controller installation and approval checkpoint were never
  reached. Run, launch and cleanup-complete markers are 2, preserving failed
  setup even though owner-checked cleanup succeeded. Both long-running
  processes terminated; no retry was launched.
  Independent direct PowerShell inspection confirmed all four resource groups
  absent at `2026-10-30T02:15:38Z`. The activity-log sample records hub creation
  accepted at `01:49:38Z` and canceled during cleanup at `01:51:09Z`, correlation
  `61a606a4-105a-4757-a854-0dd3e5a2601e`; the sampled events do not establish the
  member denial's root cause. Do not equate an accepted hub submission or
  successful Network writes with permission for every subsequent AKS operation.
  The actual failure remains an Azure authorization blocker, not evidence that
  the private-ingress feature passed or failed. The IAM-aware latest startup
  `02:13:09Z` has now passed. The newly approved resource window remains fixed
  through `13:39:00Z`, but does not extend actual Owner access or permit weakening
  the six-hour execution plus 75-minute cleanup startup requirement.
- **Quota diagnosis and requested retry blocked by current roles:** The user
  asked whether quota explained the failure, then requested another live test
  after fixing roles. CLI-first Compute quota discovery and usage checks
  succeeded. Sweden Central `standardDSv5Family` has limit 100, usage 0 and
  availability 100; regional `cores` has limit 1230, usage 138 and availability
  1092. Usage was observed at `2026-10-30T03:40:57Z`; the approved six
  `Standard_D2s_v5` nodes require 12 baseline vCPUs. These current snapshots do
  not establish historical usage or reserved capacity, but show no present
  compute-quota shortage. The actual recorded setup failure was authorization,
  not a quota error.
  At `03:40:07Z`, direct current-principal role and permission reads showed only
  provisioned Reader and effective `*/read`, with no subscription Owner
  activation. After requesting activation and receiving user confirmation,
  a repeat at `03:40:58Z` returned the same read-only result. No sixth attempt,
  resource creation or new authorization window was started. Deployment remains
  blocked until actual write authorization and sufficient verified IAM lifetime
  are observable. The existing resource authorization still ends `13:39:00Z`.
- **Attempt 6 plan after direct Owner restoration:** The user requested
  another retry after fixing roles. At `2026-10-30T03:45:39Z`, effective
  subscription permissions again included unrestricted `*`, but the PIM
  schedule still listed only Reader. Do not invent a PIM expiration from that
  discrepancy. A signed-in-user-filtered RBAC inspection at `03:46:19Z`
  verified an unconditional direct subscription Owner assignment, resource
  `roleAssignments/35446138-a9aa-41a3-aa01-a553c49dcbe3`.
  The user explicitly confirmed it is non-expiring and will remain active
  through cleanup. No role was granted or modified by the agent.
  Prepare fresh `afd-live-retry-host-header-6` at published `428de9e`, image
  `428de9e-host-header-6`, owner `20261030-013900-host-header-6`.
  Keep the existing immutable resource authorization
  `01:39:00Z` through `13:39:00Z`; no renewal/reset is needed.
  For this confirmed direct role, express an access cleanup deadline rather
  than claim an observed PIM expiry. Bind it to authorization expiry, stop
  execution at `12:24:00Z`, and preserve 75 minutes for cleanup. The six-hour
  minimum launch budget remains unchanged. Recheck the exact direct Owner
  assignment and unrestricted effective rights in preflight, together with
  all four group absences, source/tool integrity and script checks.
  Preserve the early real VNet write, original Go proof, scoped identities and
  separate manual Private Link approval. No production source changes or
  shared authentication changes are planned.
- **Attempt 6 preflight correction:** The first check refused before resource
  creation because the scoped wrapper appended `--subscription` to the
  subscription-independent `az ad signed-in-user show` operation. Use the
  real CLI only for that read-only identity lookup, normalize its CRLF, and
  continue to pin and validate the Owner assignment ARM scope through the
  wrapper. All Azure resource writes retain the existing guards; repeat the
  complete preflight without resetting authorization.
- **Attempt 6 failed startup / Owner lifetime corrected:** Full preflight
  subsequently passed, including the direct Owner binding and effective
  unrestricted rights. The attempt nevertheless failed at the first
  `Microsoft.Resources/subscriptions/resourcegroups/write` with
  `AuthorizationFailed`. No image build, AKS creation, Go proof or approval
  checkpoint started. Run, launch and cleanup-complete markers are 2; the
  runner observed the parent and all three managed groups absent. Both
  long-running processes terminated. At `2026-10-30T03:58:11Z`, direct Owner
  still existed (created `03:44:06.4176Z`) and effective rights still included
  unrestricted `*`; those observations do not explain or negate the actual
  write denial.
  The user then corrected the earlier lifetime confirmation: Owner is limited
  to eight hours. This supersedes the non-expiring assumption and invalidates
  using resource authorization expiry as the access deadline for any future
  attempt. Preserve the ended attempt's evidence, but do not reuse its
  `12:24:00Z` execution cutoff or direct-Owner lifetime assumption. Obtain
  the actual expiration timestamp; eight hours alone does not establish a
  start/end time, and RBAC `createdOn` is not proof of expiration.
  Any next cutoff must be the earlier of resource authorization expiry and
  actual access expiry, minus 75 minutes, with six hours available at launch.
  The existing resource authorization remains `01:39:00Z` to `13:39:00Z`;
  no new attempt or reset was authorized or started by this correction.
  The time limit does not by itself establish the cause of the earlier
  resource-group denial. Read-only deny-assignment and RBAC propagation
  diagnostics were started separately and remain evidence gathering only.
- **Actual expiration supplied and timezone confirmed:** The user supplied
  the portal's activated Owner row with expiry
  `10/30/2026, 4:44:05 AM` and explicitly confirmed **UTC**, not Pacific time.
  Treat the access expiration as `2026-10-30T04:44:05Z`; do not reinterpret it
  from the user's local timezone or from the newer direct RBAC record.
  Preserving 75 minutes of cleanup makes the latest execution cutoff
  `03:29:05Z`, already past the recorded `03:59:57Z` diagnostics.
  A new six-hour live run cannot start under this activation. A renewed
  activation with at least seven hours fifteen minutes remaining at actual
  launch is required, together with the unchanged resource authorization.
  Subscription-scope deny-assignment inspection returned zero entries at
  `03:59:57Z`; this is not proof of unrestricted authorization at every scope
  or a diagnosis of the actual write denial. Official RBAC troubleshooting
  documents propagation and refreshing the same account's token, but no
  shared login/cache was modified and the denial's root cause remains
  unproven. No additional attempt or resource creation was started.
- **Authoritative expiration correction:** After the user clarified that
  activation should last about eight hours from now, the broader subscription
  `roleAssignmentScheduleInstances` query using `atScope()` returned the
  exact current Owner assignment's provisioned activation at
  `2026-10-30T04:07:34Z`: start `03:34:58.59Z`, end `11:44:05.973Z`,
  origin role assignment `35446138-a9aa-41a3-aa01-a553c49dcbe3`.
  This authoritative result supersedes both the earlier non-expiring
  assumption and the supplied portal time's mistaken UTC interpretation.
  Conservative access expiry is `11:44:05Z`; execution must stop by
  `10:29:05Z`, requiring startup by `04:29:05Z`. The current-principal
  `asTarget()` lookup alone did not expose this activation; match the actual
  Owner assignment rather than treating that missing result as permanent
  access or proof that Owner is inactive. No new test started.

### Review feedback follow-up: standard E2E entry point and useful progress

The user supplied review requests for a single `ENABLE_AZURE_FRONT_DOOR=true`
opt-in with the standard three Make targets, the disabled-endpoint deployment
deadlock, and more specific Gateway progress messages.

Current assessment at published `428de9e`:

- The initial disabled-empty-endpoint `NotStarted` deadlock is already fixed
  by the bounded activation-marker bootstrap path and regression coverage in
  `bootstrap.go` / `bootstrap_test.go`. Enabled deployment/readiness and
  withdrawal verification remain strict. Complete live success is still
  unproven; recent retries failed Azure setup before the Go proof.
- The requested single public flag is absent. Bootstrap has no AFD controller
  installation path, and standard `e2e-tests` still has a 50-minute Go timeout.
  The existing proof requires separately configured controllers, identities
  and opt-ins. The requested three-command experience is not implemented.
- `applyHTTPPreview` can append an existing provider Diagnostic and observed
  pending-approval count, but ordinary provisioning frequently returns only
  `Pending` or `Staging`. Relevant Azure resource/deployment states are lost.

Approved plan: the user selected "Approve—implement the plan". Production
implementation has not started at this tests-first checkpoint.

1. **Phase 1: Regression tests first.**
   Task 1.1: Cover public flag default-off behavior, AFD enabled with ATM
   disabled, supported topology validation and separate setup/test processes.
   Task 1.2: Cover informative provisioning/deployment/approval messages while
   preserving bootstrap behavior, current-generation readiness and strict
   failure handling for unknown Azure states.
2. **Phase 2: Integrate the requested behavior.**
   Task 2.1: Wire the public AFD flag through setup and suite selection,
   candidate image/controller installation, managed-identity configuration
   and required bounded RBAC. Keep ATM independent and hub PLS access scoped
   to actual test PLS resources; never auto-approve Private Link requests.
   Task 2.2: Make the standard full-suite target select sufficient Go/Ginkgo
   deadlines only when AFD is opted in. Preserve ordinary default behavior
   and do not rely on child-shell exports surviving between Make invocations.
   Task 2.3: Carry observed provider progress through the existing diagnostic
   path into Gateway/listener conditions and events. Do not remove deployed
   readiness gates or change Azure write ordering merely to improve messages.
3. **Phase 3: Documentation and verification.**
   Task 3.1: Document the reviewer's exact flag/three-command flow, actual
   scoped prerequisites, asynchronous manual approval and diagnostic examples.
   Task 3.2: Run formatting, affected provider/controller and offline E2E
   regressions, script syntax/ShellCheck, vet and existing lint tooling.
   Task 3.3: Keep live success unchecked until an authorized candidate run
   actually passes, with verified access lifetime and observed cleanup.

Completion checklist and success criteria:

- [x] 1.1 Public flag regression coverage passes without contacting Azure.
- [x] 1.2 Status/bootstrap regression coverage passes with unchanged safety gates.
- [x] 2.1 One AFD opt-in configures setup and includes the proof, independently of ATM.
- [x] 2.2 Standard Make targets support AFD deadlines and retain default behavior.
- [x] 2.3 Waiting status names the observed resource/state or approval dependency.
- [x] 3.1 Runbook matches the actual standard entry point and manual action.
- [x] 3.2 Affected offline validation passes with no dependency/source drift.
- [ ] 3.3 Actual live proof and cleanup pass, or remain explicitly blocked/unverified.

References: `test/README.md`, `Makefile`, `test/scripts/bootstrap.sh`,
`test/e2e/gateway_private_http_test.go`, provider `bootstrap.go` and
`http_provision.go`, and Gateway `http_provisioning.go`, all at current
`428de9e`. The requested domain-knowledge/specification directories were not
present in this checkout; the existing how-to/runbook and this approved
implementation breadcrumb remain the applicable references.

Tests-first checkpoint before implementation:

- Added offline script regressions in `test/scripts/afd_test.go` for the
  public opt-in, ATM independence, invalid inputs, independently derived
  candidate image and conditional full-suite deadlines.
- Added provider lifecycle assertions requiring meaningful staging progress
  and observed endpoint provisioning/deployment/enabled-state diagnostics.
- Added a Gateway/listener diagnostic propagation regression.
- Offline `go test -count=1 -run TestAFD ./test/scripts` fails because the
  planned shared configuration and standard test wrapper do not exist yet.
- Offline `go test -count=1 -run TestReconcileHTTPPreviewLifecycle
  ./pkg/providers/azure/frontdoor` fails in three cases because staging and
  endpoint progress diagnostics are currently empty.
- Offline `go test -count=1 -run TestHTTPControllerProgressDiagnostic
  ./pkg/controllers/hub/gateway` passes, confirming the existing status path
  already propagates a supplied provider diagnostic.
- These are deliberately red regression tests, not a completed implementation.
  Checklist items remain unchecked until the corresponding behavior passes.
- The user requested committing all current changes before proceeding.
  Formatting was applied with goimports. No live setup or test was started.
  Fresh confirmation is required immediately before any live setup/E2E run.

Local repair implementation:

- The user requested fixing local E2E and unit tests before proceeding.
  Implement the shared configuration/runner and provider diagnostics against
  the committed red tests, then run only offline helpers and fake-client tests.
- Reuse the actual AKS kubelet managed identities for AFD setup, querying each
  cluster instead of requiring ATM's optional precreated identity path.
  This keeps ATM disabled and avoids changing ordinary AKS creation behavior.
- Dynamic PLS Reader preparation remains a one-shot, public-opt-in fixture
  operation after verified discovery. Validate the actual member node group
  and exact test PLS identity before granting Reader; never approve requests.
- No live setup/test or publication occurs as part of offline validation.
- The harness/provider regressions now pass. The complete affected Gateway
  package also passes using existing cached Kubernetes 1.33.0 envtest binaries.
  An earlier package run failed only because KUBEBUILDER_ASSETS was unset;
  no tests were skipped or weakened to address that missing-path failure.
- Offline tagged `^TestPrivateHTTP` helpers and tagged compile-only `^$` pass.
  Observed endpoint diagnostics now include NotStarted/InProgress and actual
  enabled/provisioning state without publishing an incomplete hostname.
- The user requires these changes to land in the appropriate original four
  signed commits. The temporary tests-first checkpoint must be absorbed,
  not published as a fifth commit.

Local repair completion and commit-fold plan:

- Shared configuration and the full-suite wrapper now support the single
  public opt-in. Bootstrap installs the candidate Gateway controller and
  read-only member discovery using actual AKS kubelet identities, without
  enabling ATM. The hub URL query now respects the selected kubeconfig.
- The fixture validates both actual AKS-managed node groups and exact proof
  PLS targets before any role write, then independently verifies each exact
  hub Reader binding. Negative tests reject either mismatched PLS, mismatched
  cluster ownership and missing binding evidence. No connection is approved.
- Standard AFD Go/Ginkgo deadlines are 10h/9h30m rather than the focused
  command's 9h/8h30m: the full suite must also budget for ordinary scenarios.
  The ordinary Go deadline remains 50m. No label filter excludes baseline tests.
- The runbook documents the actual standard flow, focused compatibility,
  bounded role prerequisites, asynchronous manual approval, image consistency
  and observed diagnostics. Shell scripts have an explicit LF checkout rule.
- Validation passed: `go test -count=1 ./test/scripts`;
  `go test -count=1 ./pkg/providers/azure/frontdoor`;
  full `go test -count=1 ./pkg/controllers/hub/gateway` using cached
  `KUBEBUILDER_ASSETS=/root/.local/share/kubebuilder-envtest/k8s/1.33.0-linux-amd64`;
  `go test -count=1 -tags=e2e -run '^TestPrivateHTTP' ./test/e2e`;
  `go test -tags=e2e -run '^$' ./test/e2e`; and E2E framework compilation.
- Validation passed: `go vet -tags=e2e ./test/scripts
  ./pkg/providers/azure/frontdoor ./pkg/controllers/hub/gateway ./test/e2e/...`;
  golint on those packages; pinned golangci-lint v1.64.7 with E2E build tags
  and `--new-from-rev=428de9e` on those packages; Bash bootstrap syntax;
  ShellCheck on all four new scripts; real Gateway chart rendering with the
  installer's values; and `make -n e2e-tests`. The dry-run did not execute E2E.
- Go test mock commands are owner-read/execute only. The narrowly documented
  G306 exception explains why executables cannot use data-file permissions;
  no linter or agent safety control was disabled. The initially missing
  golint was installed and removed in an isolated temporary directory.
- Fold the breadcrumb into the documentation commit, provider/controller
  implementation and regressions into the feature commit, standard harness
  integration and its mocked tests into the build commit, and proof invocation
  plus the test runbook into the live-preparation commit. Re-sign the original
  four-commit sequence and verify its final tree equals the validated tree.
- Live evidence remains unchecked: no live setup/test, resource creation,
  approval or push was performed. Fresh confirmation is still required
  immediately before any live setup/E2E execution; old time windows must not
  be assumed renewed.
- Commit folding completed: the temporary checkpoint was absorbed into exactly
  four commits above the unchanged protected parent. All four signatures
  verified good, and the folded tree exactly matched validated snapshot
  `24c4ae71b22da1b9a5d9433bc08a6f0b79c489f6`. Dependencies were unchanged.
  Post-fold harness tests, offline private HTTP helper tests and tagged E2E
  compilation passed again. This completion record is documentation-only
  and is folded into the documentation commit as well.

## Changes Made

### Follow-up: reject CLI syntax failures before live execution

- The user requested regression coverage after attempt 7 failed before the
  approval checkpoint. The helper combined `az role assignment list --scope`
  and `--all`; the installed CLI rejects these mutually exclusive options.
  A read-only replay confirmed the exact error. This was a fixture bug missed
  by permissive command stubs, not evidence of failed Owner access or quota.
- Plan: first make the Azure stub reject incompatible/unknown options and
  require each command's arguments, reproduce red, then remove `--all` while
  preserving exact-scope Reader verification. Validate installation command
  arguments and generated Azure config, automate real Helm rendering, and
  cover nonzero CLI failures without allowing downstream work to continue.
- Keep all default tests cloud-free. Add an explicit local-tool compatibility
  mode that checks emitted arguments against installed Azure CLI help without
  executing Azure operations. Test explicit semantic constraints such as
  `--all` versus `--scope` separately because help alone cannot prove them.
- Gate AFD-enabled bootstrap on these local tests and required CLI tools before
  the first Azure operation. Add a regression proving failed preflight prevents
  any Azure call. Ordinary non-AFD bootstrap remains unchanged.
- Follow the existing approved tests-first plan and four signed commit
  categories. Do not patch the running attempt or start another live test.
  Its deferred cleanup remains active and must be observed separately.
- [x] First reproduced the original error offline: the stricter Azure stub
  rejected the exact helper invocation with the same `--all`/`--scope` message.
  Removed only `--all`; exact scope/principal/Reader verification is retained.
- [x] Azure stubs now reject unknown/repeated options, missing values/required
  options, unexpected commands/subscription and the incompatible list flags.
  Helm/kubectl stubs validate actual arguments rather than returning success
  for arbitrary input. Structured command recording preserves argument boundaries.
- [x] Added eight command-failure cases across identity reads, role creation/
  observation, PLS reads, member/Gateway chart operations and manifest application.
  Tests require explicit failure evidence and no prohibited downstream calls.
- [x] Real offline Helm rendering now uses the installer's actual values and
  verifies both member discovery/ATM arguments and Azure config Secrets,
  Gateway image/identity/AFD arguments and the prepared Secret mount.
- [x] A bootstrap regression first demonstrated the missing local gate, then
  passed after AFD-only preflight was added before `az account set` and writes.
- [x] `AFD_VALIDATE_LOCAL_TOOLS=true go test -count=1 ./test/scripts` passes,
  including installed Azure CLI help/option compatibility and actual Helm
  renders (approximately 18 seconds locally, no Azure operations).
  `go vet ./test/scripts`, pinned golangci-lint v1.64.7
  `run --new-from-rev=bec2bec ./test/scripts`, golint on `./test/scripts`,
  ShellCheck on the four AFD helpers, bootstrap Bash syntax and diff checks pass.
- The original live proof eventually printed the captured helper stderr,
  confirming the exact diagnosed CLI rejection. Its failed AFD spec completed
  deferred cleanup and the already-running full suite continued baseline
  scenarios; whole-environment cleanup is not yet verified. No retry, new
  cloud mutation outside that existing runner, live source patch or push
  was performed for this repair.
- These checks cover CLI argument contracts and local chart/config rendering;
  they do not prove Azure permissions, propagation, quota or convergence.
  Fold the implementation/tests into the build commit, this record into
  documentation and the runbook into live preparation, retaining four signatures.

- ZB completes the opt-in controller-to-provider private HTTP execution path,
  observed readiness/address projection, maintenance and confirmed withdrawal.
  Live platform, installed identity and traffic/Host/probe/withdrawal/cleanup proof
  remain deferred; source wiring is not operational evidence.
- Corrected the canonical plan's stale status: runtime wiring is implemented;
  Azure ownership/provider and live proof remain outstanding.
- Added the six-phase detailed provider sequence with P1.1-P6.3 task identifiers,
  tests-first steps, measurable gates, completion checklist and official references.
- Recorded the new shared-endpoint port restriction and approval deduplication.
- Added this planning breadcrumb. Plan approved on 2026-10-21; the user explicitly
  requested waiting to implement. No implementation or live Azure work started.
- Subsequent go-ahead received; P1 backend planning/safety work starts with tests.
- Implemented backend SDK request construction, explicit WAF gating, opt-in
  controller E2E coverage and its local condition-predicate regression tests.
- Updated dependency manifests, the test runbook and canonical design progress;
  full provider phases remain unchecked rather than claiming dataplane completion.
- Added pure Premium root request builders and observed profile ownership guards,
  with tests and documentation of trusted hub identity and the remaining runtime gate.
- Latest slice K updates `backend_provision.go`, `inventory.go` and `staging.go`
  to revalidate collection policy/deployment/approval observations. Added
  `inventory_convergence_test.go`, shared pagination keys in existing inventory
  tests, and updated design/runbook descriptions of the stronger verification.
- Slice L extracts the shared explicit-array guard in `inventory.go`, reuses it
  in `unsupported_inventory.go`, adds the 24-case page-evidence matrix to
  `inventory_test.go`, and updates design/runbook completeness requirements.
- Slice M adds `origin_cleanup.go` / `_test.go`, typed expected/stale origin
  inventory observation, one-write cleanup integration and pending-count status
  coverage. Design/runbook now distinguish this bounded current-group pruning
  from broader still-unimplemented cleanup and production integration.
- Slice N adds `readiness.go` / `_test.go`, enabled setup registration, startup
  probe/error coverage and design/runbook descriptions of local cache readiness.
  Also expresses reference-withdrawal eligibility positively to correct the
  user-reported CI `nilerr` finding without changing withdrawal behavior.
- Slice O adds the separate Gateway Dockerfile, package-based native commands,
  opt-in local image target/variables, disabled-default flag regression and Linux
  image smoke job. Design/runbook distinguish these definitions/local Go checks
  from pending actual container CI, publication and installation.

## Before/After Comparison

- Before: broad provider tasks existed, but the distinction between Azure's datapath
  and Fleet automation, concrete runtime seams and shared-endpoint constraints
  were not explicit.
- After: a bounded, tests-first plan connects the existing controller/model to
  private-only Azure provisioning, asynchronous observation and safe lifecycle
  handling without inventing another datapath component.
- Current implementation now validates both backend inventory and actual SDK
  request bodies. E2E can assert real controller status/watches when opted in,
  while clearly separating that evidence from unimplemented Azure provisioning.
- Slice K before: valid inventory identities could hide configuration or approval
  changes after expected GET convergence. After: later contradictory collection
  observations invalidate staging with explicit errors and no partial result;
  sequential observations still do not prove atomic or live traffic readiness.
- Slice L before: expected records on other pages could hide an omitted/null
  supported-resource array. After: every page must provide explicit evidence,
  while explicitly empty pages remain valid and no partial result escapes refusal.
- Slice M before: every extra origin blocked otherwise converged disabled intent.
  After: strictly eligible private stale origins can be pruned one at a time
  after all other verification, with fresh candidate/parent checks and later
  absence proof. Other extras remain refusals; no production enablement is claimed.
- Slice N before: an unconditional readiness ping could succeed while required
  Gateway caches were unsynced. After: enabled readiness requires every watched
  informer initially synced and not stopped, with shutdown refusal. Liveness,
  disabled mode and standby readiness stay independent of Azure and leadership.
- Slice O before: file-only Makefile commands failed to compile package helpers
  and no separate image build/smoke path existed. After: native package build/help
  works and an opt-in image plus real-container CI contract exists. Container
  execution is still pending CI; no automatic publication/deployment was added.

## References

- `.github/.copilot/breadcrumbs/2026-10-20-2013-zero-public-exposure-implementation-plan.md`:
  prior approved product decisions and completed runtime slice.
- `docs/design/gep-1748-implementation-plan.md`: canonical delivery plan; original
  approval 2026-10-20, provider detail proposed 2026-10-21.
- `docs/design/gep-1748-gateway-api.md`: architecture and ownership contract;
  Gateway API v1.2.1.
- `docs/howtos/gateway-api-afd-configuration.md`: private-only topology,
  platform-owned settings and manual asynchronous approval.
- `cmd/hub-gateway-controller-manager/main.go`,
  `pkg/controllers/hub/gateway/controller.go`: verified provider integration seams.
- `cmd/hub-net-controller-manager/main.go:245-276`,
  `pkg/controllers/hub/trafficmanagerprofile/controller.go:77-104`: verified
  repository identity/client options and UID-derived resource conventions.
- https://learn.microsoft.com/en-us/azure/frontdoor/private-link:
  fetched 2026-10-21; AFD-managed endpoints, approval/convergence, sharing and
  different-port limitation.
- https://learn.microsoft.com/en-us/azure/frontdoor/standard-premium/how-to-enable-private-link-internal-load-balancer:
  fetched 2026-10-21; Custom origin properties, PLS and approval workflow.
- `.github/.copilot/domain_knowledge` and `.github/.copilot/specifications`:
  neither directory exists in this checkout; no versioned specification applied.
- https://pkg.go.dev/github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2:
  `v2.2.0`, stable AFD API `2024-02-01`.
- https://raw.githubusercontent.com/Azure/azure-quickstart-templates/master/quickstarts/microsoft.cdn/front-door-premium-vm-private-link/main.bicep
  and `modules/front-door.bicep`: verified custom PLS group-ID null behavior.
  Do not copy the sample's unrelated HTTPS/caching/probe defaults.
