# Zero-Public-Exposure Ingress Implementation Plan

## Requirements

- Produce a tests-first implementation plan for the updated Zero-Public-Exposure Ingress product contract.
- Make AFD Premium and Private Link origins mandatory without customer edits to generated ServiceImports.
- Move PLS discovery and asynchronous approval into the first functional ingress milestone.
- Preserve Fleet ServiceImport and existing MCS/Traffic Manager behavior.
- Track approval automation as an external AFD/PLS action item, not an implemented dependency for the manual-approval milestone.

## Additional comments from user

- User requested an implementation plan after approving the guide-only product-decision update.
- This request authorizes planning, not implementation or Azure resource provisioning.
- User approved the plan and explicitly requested that implementation not start yet.
- User subsequently requested "start implementation"; Phase 1 is now in progress.
- User requested committing all pending changes and a better description for PR #400 centered on Zero-Public-Exposure Ingress.
- User will update the PR description after full implementation and requested continuing implementation; leave remote PR text unchanged.
- User approved the proposed HTTP-preview probe/Host policy: HTTP GET `/` every 30 seconds on the backend port, preserving the incoming request Host.
- User requested committing current changes before proceeding; committed as `78e4667` (`feat: discover and resolve private member ingress origins`) with required trailers. Worktree was clean immediately after the commit; no push.
- User requested keeping implementation inline with existing repository choices.

## Plan

### Phase 1: Establish the implementation baseline

- [x] Task 1.1: Read the current plan, product-decision breadcrumb, relevant Go foundation, member export flow, internal transport, scale gates, and existing validation commands.
  - Success criteria: identify concrete reuse points, missing reconcilers, and coupling to Traffic Manager.
- [x] Task 1.2: Check domain knowledge and specifications.
  - Success criteria: incorporate applicable sources or explicitly record their absence.

### Phase 2: Write the replacement plan

- [x] Task 2.1: Replace the public-first plan with phased private-only implementation, tests, dependencies, and measurable gates.
  - Success criteria: customer hub-access constraints, asynchronous approval, packaging, ownership, failure handling, and release validation are covered.
- [x] Task 2.2: Distinguish decided product requirements from unresolved engineering decisions and future features.
  - Success criteria: ConfigMap and approval automation are not claimed as available; mapping/probe decisions block dependent implementation.

### Phase 3: Present the plan

- [x] Task 3.1: Review the plan and summarize the milestone sequence for approval.
  - Success criteria: no Go code changes, no tests or live operations run, and implementation explicitly awaits approval.

### Detailed checklist

- [x] Phase 1 / Task 1.1 complete.
- [x] Phase 1 / Task 1.2 complete.
- [x] Phase 2 / Task 2.1 complete.
- [x] Phase 2 / Task 2.2 complete.
- [x] Phase 3 / Task 3.1 complete.

**Overall success criteria:** A concrete private-only plan is recorded in the existing implementation-plan document and presented for approval before implementation.

## Decisions

- Revise the existing `docs/design/gep-1748-implementation-plan.md` rather than create a competing implementation plan.
- Keep the previous guide edits and breadcrumb intact.
- Record the existing design document's broader topology as requiring alignment in the first implementation slice.
- Treat Azure-only approval/provisioning transitions as bounded polling; Kubernetes changes should use watches and indexes.
- Propose a minimal HTTP/private-origin preview first; logical multi-backend weighting and richer routing remain gated on demonstrated AFD mappings rather than silently assumed support.
- Do not preselect probe defaults or introduce a ConfigMap implementation during planning.
- Plan approved on 2026-10-20; await a separate implementation go-ahead.
- Implementation go-ahead received. Execute Phase 1 first; later phases retain their dependency gates.
- Phase 1 removes selectable SKU defaults and ServiceImport parsing inputs rather than adding a platform configuration API before probe policy is decided.
- Retire the origin Host annotation alongside connectivity/probe annotations because no customer ServiceImport AFD configuration is supported. Platform probe/Host behavior is still a later design gate.
- Phase 2 transport: an optional InternalServiceExport private-origin structure carries PLS ID/location, internal IP, readiness and a diagnostic reason. Absence means unknown; readiness describes discovery, never AFD connection approval.
- Resolve the Service's observed internal LB IP to one Azure LB frontend, then require the PLS to reference that frontend. Support separate annotated LB/PLS resource groups and reject ambiguous discovery.
- Gate discovery independently of Traffic Manager. Publish unavailable/error discovery state without invalidating baseline MCS export, retry Azure failures explicitly, and periodically rediscover Azure-only transitions with bounded calls and jitter.
- Keep AKS-owned ILB/PLS read-only; no automatic connection approval.
- Approved platform HTTP-preview policy is fixed in the normalized backend contract, not parsed from ServiceImport. Probe Host behavior remains a provider verification gate.
- Phase 3 starts with a pure resolver over an already fetched ServiceImport and candidate internal exports. Future controllers must authorize cross-namespace references before invoking it. Validate member namespace, source identity, fresh conflict status, full port compatibility and private readiness; do not silently deploy a partially resolved backend.
- Return typed failures distinguishing invalid references from unavailable origins so later controllers do not conflate pending private connectivity with `ResolvedRefs=False`.
- Continue in the existing gatewaymodel/gatewayresolver packages, using manual deep copies, standard-library sorting, table-driven tests and controller-runtime field indexes as existing hub controllers do. No new configuration API, framework or dependencies.
- Preserve HTTPRoute rule boundaries, listener-specific hostname intersections, creation-time precedence metadata and rule order. Reject unimplemented matches/filters rather than flattening them.
- Count backend capacity by distinct ServiceImport namespace/name/port: origins have a fixed HTTP port, so different ports require distinct origin groups. Enforce only the backend dimensions proven by this inventory; exact AFD route/rule/domain accounting still requires the provider lowering and must not be approximated from HTTPRoute counts.

## Implementation Details

- The initial manager validated configuration without reconcilers; the runtime slice now registers the three control-plane reconcilers only when enabled.
- Member export Azure metadata and weight publication currently sit behind the Traffic Manager feature flag. New private-origin discovery must work independently without changing baseline export or Traffic Manager behavior.
- InternalServiceExport already transports source identity, service ports, internal-LB classification, public-IP metadata, and weights; PLS identity/readiness are missing.
- The normalized model needs rule and listener attachment fidelity before it can preserve HTTPRoute semantics.

## Changes Made

- Recorded planning scope and inspected implementation evidence.
- Replaced the existing public-first implementation plan with nine private-only phases, explicit prerequisites, tests-first tasks and success criteria.
- Defined Phases 1-6 as the first usable private HTTP/manual-approval preview, followed by optional WAF/routing expansion, HTTPS and release gates.
- Added unresolved-decision gates, bounded approval-state observation, exact capacity accounting, regression coverage, packaging, PR slices and validation commands.
- All implementation checkboxes are pending. No Go code, tests, cloud resources or existing guide edits were changed by this planning task.
- Reviewed phase dependencies, decision gates, unchecked implementation tasks, and private-only milestone boundaries. `git diff --check` passed.
- Presented the milestone sequence and received plan approval; recorded the explicit hold on starting implementation.
- Phase 1 started: write replacement parser contract tests, then simplify parsing, update E2E fixtures and align the architecture/capacity documents. Test completion remains pending.

### Phase 1 implementation completed

- Wrote replacement table-driven tests first. The initial `go test -count=1 .\pkg\annotations` failed as expected because the old parser required a configurable default SKU and the replacement validation entry point did not exist.
- Simplified `pkg/annotations/afd.go`: Premium-only Gateway parsing, no selectable Standard/auto/public settings, no ServiceImport configuration type, and actionable rejection of ServiceImport AFD annotations without mutation.
- Removed E2E ServiceImport annotation writes and asserted that generated objects need no AFD annotations; retained the API-round-trip-only scope.
- Aligned the architecture and scale documents with private-only ingress, platform-owned settings, first-preview routing targets and asynchronous manual approval. Updated the E2E guide accordingly.
- Updated the implementation plan's Phase 1 tasks and completion checklist; later phases remain pending.
- `goimports -local go.goms.io/fleet-networking -w` on the three modified Go files completed.
- `go test -count=1 .\pkg\annotations .\pkg\controllers\hub\gatewaymodel .\cmd\hub-gateway-controller-manager` passed.
- `go vet .\pkg\annotations .\pkg\controllers\hub\gatewaymodel .\cmd\hub-gateway-controller-manager` passed.
- `go test -count=1 -cover .\pkg\annotations` passed with 97.1% statement coverage.
- `go test -tags=e2e -run '^$' .\test\e2e .\test\e2e\framework` passed (compile only; no live tests).
- `go vet -tags=e2e .\test\e2e .\test\e2e\framework` passed.
- `golint` was not installed. After the missing-tool failure, `go run golang.org/x/lint/golint@latest -set_exit_status .\pkg\annotations` passed without modifying repository dependencies.
- `git diff --check` passed. No changes to `go.mod`, `go.sum`, API types or generated CRDs were introduced.
- No live Azure operations were run. No runtime Gateway reconciliation or Private Link programming is implemented in this slice.
- Re-ran focused unit tests, foundation vet, E2E compile-only checks, E2E vet and diff checks before the requested commit; all passed.
- Prepared a replacement PR description from `.github/PULL_REQUEST_TEMPLATE.md`, preserving its structure and accurately distinguishing the foundation from the planned private data path. `make reviewable`, full regression/race suites and live Azure E2E were not run.
- Commit scope includes all pending guide, design, implementation-plan, parser, test and breadcrumb changes. No push or remote PR edit is part of this request.

### Phase 2 implementation completed

- Wrote transport/discovery tests before production changes. Initial tests failed against the missing optional field and discovery entry point, as expected.
- Added optional `InternalServiceExport.spec.privateLinkOrigin` with PLS resource ID, region, verified internal frontend address, readiness and diagnostic reason. Absence remains unknown for older members; member readiness never reports AFD approval.
- Regenerated the internal CRD and deepcopy implementation with `controller-gen`; restored unrelated generator import-alias changes. Public ServiceImport, v1beta1 API types and dependency manifests are unchanged.
- Added read-only Azure discovery matching the observed Service IP to one provisioned internal LB frontend and the PLS referencing that exact frontend. Reject public, unsupported, ambiguous and unprovisioned origins. Support separate annotated resource groups and optional PLS name selection.
- Added default-off `--enable-private-link-origin-discovery` and Helm `enablePrivateLinkOriginDiscovery`. Construct only clients needed by the independently enabled features; load and mount cloud configuration when either discovery or Traffic Manager is enabled.
- Publish private metadata and existing weights with Traffic Manager disabled. Publish fresh not-ready state and explicitly return/log Azure errors while preserving baseline MCS export. Clear stale private identity on deletion, replacement, public conversion or feature disable; retain existing source-UID cleanup.
- Rediscover every two minutes plus up to 20% jitter for Azure-only changes. Tests assert the actual delay range and shared 30-second deadline, not just a positive requeue.
- Documented hub-CRD-first rollout, old-member compatibility, minimum Azure read operations, troubleshooting and separation from asynchronous AFD approval in the member chart README and configuration guide.
- Fixed a lifecycle-test fixture after discovering that the fake Kubernetes client does not assign creation timestamps; this ensures the existing production UID guard is exercised instead of changing unrelated production logic.
- `go test -count=1 -skip '^TestAPIs$' .\api\v1alpha1 .\pkg\controllers\member\serviceexport .\pkg\controllers\member\internalserviceexport .\pkg\controllers\hub\internalserviceexport .\pkg\controllers\hub\serviceimport .\pkg\controllers\hub\trafficmanagerbackend .\cmd\member-net-controller-manager` passed. The hub serviceimport package had no ordinary unit tests to execute; all `TestAPIs` envtest suites were explicitly excluded.
- `go vet .\api\v1alpha1 .\pkg\common\objectmeta .\pkg\controllers\member\serviceexport .\cmd\member-net-controller-manager` passed.
- `go run golang.org/x/lint/golint@latest -set_exit_status .\pkg\common\objectmeta .\pkg\controllers\member\serviceexport .\cmd\member-net-controller-manager` passed.
- `go build .\cmd\member-net-controller-manager .\cmd\hub-net-controller-manager .\cmd\hub-gateway-controller-manager` passed.
- `go test -tags=e2e -run '^$' .\test\e2e .\test\e2e\framework` passed, compile-only.
- `helm lint .\charts\member-net-controller-manager --set enablePrivateLinkOriginDiscovery=true --set enableTrafficManagerFeature=false` passed. Render checks for all four feature combinations confirmed cloud configuration is mounted iff either Azure feature is enabled and discovery flag values match.
- Envtest was not run: local API-server/etcd binaries and configured `KUBEBUILDER_ASSETS` were unavailable. Live Azure discovery, connection approval and data-plane E2E remain unverified; no Azure writes were performed.
- Phase 2 changes remain uncommitted. No push or remote PR description update was performed.
- Next dependency gate: Phase 3 private resolution/model work requires closing platform probe/Host policy and proving AFD lowering; no `/healthz` default, public fallback, ConfigMap API or automatic approval was introduced.
- Reviewed the AFD origin and health-probe documentation before proposing the next policy decision. AFD supports GET/HEAD probes on the configured origin protocol/port and treats only 200 as healthy. Leaving the origin Host override unset preserves the incoming request hostname; probe Host behavior still needs explicit provider verification. Proposed for review, not implemented: fixed platform HTTP GET `/` every 30 seconds on the selected backend port, preserving the incoming request Host without ServiceImport configuration.

### Phase 3 implementation in progress

- Probe/Host policy approval received. Start with resolver/model tests covering exact reference kinds/ports, deterministic private resolution, stale or conflicting exports, missing discovery, zero weights and member removal.
- Implement only private resolution, fixed preview policy data and origin validation in this slice. HTTPRoute rule lowering, listener attachment, authorization controllers, indexes and capacity preflight remain separate pending work.

### Phase 3 private resolver/model slice completed

- Initial `go test -count=1 .\pkg\controllers\hub\gatewayresolver` failed as expected because the new resolver, typed failures and platform probe data did not exist.
- Added `pkg/controllers/hub/gatewayresolver/backend.go` and table-driven tests. Resolution requires the exact Fleet ServiceImport group/kind and selected TCP/HTTP port. It validates the source Service identity, reserved member namespace, full port compatibility and generation-current unconflicted status before accepting a private origin.
- Source TypeMeta may be empty on cached Services, matching the existing member producer; if populated it must identify a v1 Service. UID and namespaced source identity are always required. Hub-side code cannot independently observe a member Service recreation before updated member transport arrives; no stronger freshness guarantee is claimed.
- Select only ServiceImport member clusters; ignore removed/deleting exports and zero-weight members. Duplicate active exports, stale/conflicting status, invalid topology and incomplete discovery fail with no partial backend. Neither public-IP metadata nor ServiceImport IPs are fallback inputs.
- `ResolutionError.ReferenceValid` distinguishes invalid references from unavailable private origins; this is intended for future status reconciliation, not a claim that reference authorization or connection approval has occurred. Cross-namespace authorization remains the caller's obligation and is not implemented by the pure resolver.
- Replaced free-form origin connectivity with mandatory private identity validation in the normalized model. Added source Service UID, platform health-probe data and the empty Host override preserving incoming request Host. The fixed preview policy is HTTP GET `/`, 30 seconds, selected backend port.
- Added resolver equality/order/nonmutation tests, boundary weights, missing/stale/conflicting/recreated sources, old member discovery, public fallback refusal, namespace references, and private model validation. Fixed a misplaced test-function insertion before rerunning the tests successfully.
- `go test -count=1 .\pkg\controllers\hub\gatewayresolver .\pkg\controllers\hub\gatewaymodel` passed.
- `go vet .\pkg\controllers\hub\gatewayresolver .\pkg\controllers\hub\gatewaymodel` and `go run golang.org/x/lint/golint@latest -set_exit_status .\pkg\controllers\hub\gatewayresolver .\pkg\controllers\hub\gatewaymodel` passed.
- Combined unit regression run passed across v1alpha1 API, annotations, gatewayresolver, gatewaymodel, member serviceexport/internalserviceexport, hub internalserviceexport/serviceimport/trafficmanagerbackend and both affected managers, with `-count=1 -skip '^TestAPIs$'`. Hub serviceimport had no ordinary unit tests; envtest remains excluded and unverified.
- `go build .\cmd\...` and `go test -tags=e2e -run '^$' .\test\e2e .\test\e2e\framework` passed.
- `go test -count=1 -cover .\pkg\controllers\hub\gatewayresolver .\pkg\controllers\hub\gatewaymodel` passed with 96.3% and 91.4% statement coverage respectively. Final `git diff --check` passed, and public ServiceImport/v1beta1/dependency diffs remained empty.
- Aligned the architecture and how-to with the approved preview policy and its provider-verification caveat. Marked implementation-plan Tasks 3.1-3.2 complete; Tasks 3.3-3.4 remain pending.
- No Gateway reconcilers, route lowering, Azure provisioning, connection approval or live traffic evidence is claimed. New changes remain uncommitted and the remote PR is unchanged.

### Next Phase 3 slice plan

- [x] Task A: Write model tests first, then preserve rules, listener attachments, hostname intersections and precedence metadata with complete deep-copy isolation. Validate the fixed private HTTP-preview subset without changing MCS or Traffic Manager behavior.
- [x] Task B: Write exact-threshold backend capacity tests first, then deduplicate service-port groups and origins and enforce the provisional 100 groups / 160 origins / 40 origins per group envelope. Fail explicitly on inconsistent copies of a backend. Do not claim full AFD route/rule preflight.
- [x] Task C: Add reference index tests and standard controller-runtime field indexes for GatewayClass, Gateway parent references, Fleet ServiceImport references and source exports. No reconciler registration yet.
- [x] Task D: Run affected tests, vet, golint, manager builds and compile-only E2E checks, then update plan/progress and record remaining provider/integration gates.

**Success criteria:** Existing repository patterns are retained, equivalent intent normalizes stably without losing rule order, private backends are counted by port with exact boundary checks, and indexed lookups cover dependency changes. Full traffic behavior remains unclaimed until provider and controller slices land.

- Tasks A-B validation: tests initially failed because the new rule/attachment and capacity types/functions did not exist. `go test -count=1 .\pkg\controllers\hub\gatewaymodel .\pkg\controllers\hub\gatewayresolver` then passed for the model/resolver; capacity tests subsequently passed at exact 100/101 groups, 40/41 origins per group and 160/161 total-origin boundaries. Index tests were added afterward and failed as expected before their implementation.

### Rule model, backend capacity and dependency indexes completed

- Kept changes in existing gatewaymodel/gatewayresolver packages, retaining explicit typed structs, manual nested copies, standard-library sorting, table-driven unit tests and controller-runtime indexing. No new dependencies, flags, ConfigMap or abstractions around controller-runtime were introduced.
- Replaced flattened route-level matches/backends with ordered rules and listener-specific attachments. Preserved UID/creation time for future ownership and precedence lowering. Canonicalization normalizes UTC timestamps, default matches, PathPrefix trailing slashes and OR match order/duplicates without changing Exact trailing slashes or rule order. Invalid paths are rejected rather than repaired.
- Added hostname intersections following Gateway API wildcard semantics, including multi-label matches, exclusion of the apex, narrow wildcard intersections and explicit distinction between unconstrained and unmatched hostnames. Reused Kubernetes DNS validation.
- Validated only the supported private HTTP-preview representation. Unsupported listener protocols/non-80 edge ports, methods, filters, multiple/zero logical backends, missing/zero-weight origins and platform-policy overrides fail explicitly. Canonical container order is not represented as a provider routing algorithm; hostname/path specificity lowering still needs proof.
- Added `PreflightBackendCapacity`: exact deduplication by ServiceImport namespace/name/port with fixed proposed 100-group / 160-origin / 40-origins-per-group limits. Conflicting copies of an origin set fail rather than choosing one. Different ports consume separate groups/origins. Full AFD route/rule/domain limits remain a provider prerequisite.
- Added standard field indexes for GatewayClass, Gateway parent refs, exact Fleet ServiceImport backend refs and source NamespacedName, matching existing hub indexing conventions. Tests cover cross-namespace/default references, foreign kinds/groups, sorted deduplication, fake-client lookup/removal and index-registration failures. These indexes are not registered in a runtime manager until its reconciler slice lands.
- Updated architecture, implementation plan and scale accounting from service-only groups to service-port groups (`B_g`). Kept one-port worked examples explicit and aligned probe guidance with the approved GET `/` policy. Marked Task 3.3 complete; Task 3.4 remains open for full provider preflight and runtime event mapping.
- `go test -count=1 -skip '^TestAPIs$' .\api\v1alpha1 .\pkg\annotations .\pkg\controllers\hub\gatewaymodel .\pkg\controllers\hub\gatewayresolver .\pkg\controllers\hub\internalserviceexport .\pkg\controllers\hub\serviceimport .\pkg\controllers\hub\trafficmanagerbackend .\pkg\controllers\member\serviceexport .\pkg\controllers\member\internalserviceexport .\cmd\hub-gateway-controller-manager .\cmd\member-net-controller-manager` passed; hub serviceimport had no ordinary unit tests and envtest was excluded.
- `go vet .\pkg\controllers\hub\gatewaymodel .\pkg\controllers\hub\gatewayresolver .\cmd\hub-gateway-controller-manager` passed.
- `go run golang.org/x/lint/golint@latest -set_exit_status .\pkg\controllers\hub\gatewaymodel .\pkg\controllers\hub\gatewayresolver` passed.
- `go build .\cmd\...` and `go test -tags=e2e -run '^$' .\test\e2e .\test\e2e\framework` passed.
- `go test -count=1 -cover .\pkg\controllers\hub\gatewaymodel .\pkg\controllers\hub\gatewayresolver` passed with 96.3% and 97.3% coverage respectively.
- Envtest/live Azure/data-plane validation remains unavailable or unauthorized as previously recorded. No Azure operations, push or PR-body edits were performed. This new slice remains uncommitted after the requested baseline commit.

### Runtime control-plane slice plan

- User committed the previous slice as `e15679c`, then requested continuing implementation.
- [x] Task E: Test and implement listener/parent/namespace hostname attachment, explicit unsupported-route validation, ReferenceGrant authorization and indexed private backend lookup in gatewayresolver.
- [x] Task F: Test and add GatewayClass, Gateway and HTTPRoute reconcilers using existing controller-runtime client/builder/status patterns. Preserve foreign controller status; make updates idempotent and generation-aware.
- [x] Task G: Register indexes and reconcilers only with `--enable-afd`, after configuration validation. Retain disabled startup behavior and fail startup on registration errors.
- [x] Task H: Validate focused regressions, vet, golint, manager builds and E2E compilation; update design/plan with actual scope and remaining provider gates.

**Decisions before implementation:** Accept only the recognized AFD controller and supported HTTP-preview configuration. Keep `Programmed=False` with an explicit provider-not-implemented message; no Azure writes, finalizers for nonexistent Azure resources or advertised conformance/features. Reject unsupported intent and unauthorized references explicitly. Separate resolved references from unavailable private-origin discovery. Use indexed watches, including grant and namespace policy changes. The standard client-go conflict retry re-reads/re-evaluates objects; the repository's existing apiretry helper intentionally does not retry conflicts and is not modified.

**Success criteria:** Enabled manager registers real control-plane reconcilers, foreign resources/status remain untouched, authorization removals invalidate references without retaining stale accepted status, and no status implies a functioning AFD data path.

**Runtime progress:** Resolver authorization and class/Gateway/HTTPRoute status tests pass. The reconcilers use optimistic-lock status patches inside conflict retries and re-evaluate intent on every retry. A fifth route index targets backend namespaces so grant deletion and wildcard changes invalidate references. Gateway watches backend/grant/namespace dependencies directly rather than relying on an HTTPRoute status change when private discovery changes. The standard GatewayClass in-use finalizer protects class identity while Gateways reference it; no Gateway/Azure finalizer or Azure mutation is introduced. Tests prove concurrent foreign status survives a real fake-client optimistic-lock conflict and that the retry evaluates the updated route generation/backend. Additional tests cover target-read authorization, grant removal, parent removal, private readiness updates and runtime backend preflight at 40/41 origins.

**Course corrections:** Kept `ResolvedRefs` independent of unsupported route matching/filter intent; unsupported intent changes `Accepted`, while backend references are evaluated separately. Added direct Gateway dependency watches to avoid missing readiness changes when HTTPRoute status is unchanged. Local compilation caught a misplaced startup test declaration; it was moved to file scope, then focused checks passed.

### Runtime control-plane validation and remaining gates

- `go test -count=1 -skip '^TestAPIs$' .\api\v1alpha1 .\pkg\annotations .\pkg\common\objectmeta .\pkg\controllers\hub\gatewaymodel .\pkg\controllers\hub\gatewayresolver .\pkg\controllers\hub\gatewayclass .\pkg\controllers\hub\gateway .\pkg\controllers\hub\httproute .\pkg\controllers\hub\serviceimport .\pkg\controllers\hub\internalserviceexport .\pkg\controllers\hub\trafficmanagerbackend .\pkg\controllers\member\serviceexport .\cmd\hub-gateway-controller-manager .\cmd\hub-net-controller-manager .\cmd\member-net-controller-manager` passed. ServiceImport has only the excluded envtest suite; the hub manager has no test files.
- Final focused rerun: `go test -count=1 .\pkg\controllers\hub\gatewayclass .\pkg\controllers\hub\gateway .\pkg\controllers\hub\httproute .\pkg\controllers\hub\gatewayresolver .\pkg\controllers\hub\gatewaymodel .\cmd\hub-gateway-controller-manager` passed, including conservative status for unread TLS references and unsupported filter references.
- `go vet .\pkg\common\objectmeta .\pkg\controllers\hub\gatewayclass .\pkg\controllers\hub\gateway .\pkg\controllers\hub\httproute .\pkg\controllers\hub\gatewayresolver .\pkg\controllers\hub\gatewaymodel .\cmd\hub-gateway-controller-manager` passed.
- `go run golang.org/x/lint/golint@latest -set_exit_status .\pkg\common\objectmeta .\pkg\controllers\hub\gatewayclass .\pkg\controllers\hub\gateway .\pkg\controllers\hub\httproute .\pkg\controllers\hub\gatewayresolver .\pkg\controllers\hub\gatewaymodel .\cmd\hub-gateway-controller-manager` passed.
- `go build .\cmd\...`, `go test -tags=e2e -run '^$' .\test\e2e .\test\e2e\framework`, and `git diff --check` passed. E2E compilation is not a traffic test.
- Envtest/informer event delivery and live Azure/data-plane validation remain unverified: the existing environment lacks API-server/etcd assets and no Azure provisioning was authorized or performed. Runtime setup was exercised with a real manager and a static REST mapper without starting it or contacting an API server.
- Updated the approved plan: Tasks 4.1-4.3 are implemented; Task 3.4 remains open for full provider lowering/preflight, and Task 4.4 remains open for Azure ownership. Gateway-manager deployment RBAC/chart packaging is still a Phase 6 gate; this slice adds source RBAC markers only.
- No new dependencies, configuration APIs or frameworks. User subsequently requested committing all changes in this slice; no push or PR-body update was requested.

## Before/After Comparison

- Before: public-origin-first delivery with optional Private Link and customer ServiceImport annotation.
- After: private-only contract, member ILB/PLS discovery/transport, private resolution, rule-scoped model, hostname intersections, service-port backend capacity, dependency indexes and enabled-only control-plane reconcilers implemented. Provider lowering/full resource preflight, Azure ownership/provisioning and approval observation remain pending; envtest/live Azure evidence is still outstanding.

## References

- `.github/.copilot/breadcrumbs/2026-10-20-1956-zero-public-exposure-ingress.md`: approved product decisions and guide update.
- `.github/.copilot/breadcrumbs/2026-08-17-2356-gep-1748-gateway-api.md`: original foundation work.
- `docs/howtos/gateway-api-afd-configuration.md`: current product contract.
- `docs/design/gep-1748-gateway-api.md`: proposed 2026-08-17 architecture; broader topology requires alignment.
- `docs/design/gep-1748-implementation-plan.md`: existing delivery plan being revised.
- `docs/design/gep-1748-scale-and-capacity.md`: provisional support envelope and outstanding mapping/approval decisions.
- `pkg/annotations/afd.go`, `pkg/controllers/hub/gatewaymodel/model.go`, `cmd/hub-gateway-controller-manager/main.go`: foundation reuse and gaps.
- `api/v1alpha1/internalserviceexport_types.go`, `pkg/controllers/member/serviceexport/controller.go`, `cmd/member-net-controller-manager/main.go`: internal transport and Traffic Manager coupling.
- `api/v1alpha1/common_types.go`, `pkg/controllers/hub/internalserviceexport/controller.go`, `pkg/common/hubconfig/hubconfig.go`: source-reference identity, port/conflict rules and reserved member namespace convention.
- `pkg/controllers/hub/gatewayresolver/backend.go`, `pkg/controllers/hub/gatewaymodel/model.go`: pure private backend resolution, typed failures and normalized origin/probe contract.
- `pkg/controllers/hub/serviceimport/controller.go:208-221`, `pkg/controllers/hub/trafficmanagerbackend/controller.go:734-773`: existing controller-runtime field-index setup, source-reference lookup and error logging patterns.
- https://learn.microsoft.com/en-us/azure/templates/microsoft.cdn/profiles/origingroups/origins (ARM reference, updated 2026-10-20): origin-scoped HTTP port and Private Link configuration; service-port identity cannot be collapsed into one origin group across ports.
- https://learn.microsoft.com/en-us/azure/frontdoor/front-door-route-matching (updated 2026-05-01): route path-pattern restrictions, origin-group rule overrides and specificity behavior. Documentation alone is not provider/data-plane evidence of Gateway API semantic equivalence.
- `Makefile`: repository formatting, vet, lint, unit/integration, and E2E commands.
- No files found under `.github/.copilot/domain_knowledge` or `.github/.copilot/specifications`; no applicable versioned specification exists there.
- https://learn.microsoft.com/en-us/azure/frontdoor/origin (updated 2025-09-25): origin Host forwarding and origin-group probe settings.
- https://learn.microsoft.com/en-us/azure/frontdoor/health-probes (updated 2026-05-01): supported protocols/methods, port behavior, 200-only success and distributed probe volume.
