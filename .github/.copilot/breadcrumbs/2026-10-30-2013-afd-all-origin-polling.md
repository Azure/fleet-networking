# AFD all-origin polling diagnostics

## Requirements

- Inspect every observed origin on each manual-approval convergence poll.
- Report all origin identities, PLS targets and states, including pending members.
- Preserve the requirement that every expected member has a correlated pending
  request before emitting the manual approval checkpoint.
- Allow members to converge independently across polls without retaining stale
  state or changing approval, readiness, authorization or timeout requirements.

## Additional comments from user

- "can we fix the loop to check status of everything since they all may converge at different times"
- Attempt 9 timed out reporting only member-1 Unknown. Both member PLS resources
  had pending connections, but this does not prove their AFD observer states.
- User requested comparison with `C:\repos\aks1p`, which also provisions
  AFD/WAF/PLS independently of Fleet.
- User approved an explicit opt-in temporary E2E approval helper with a FIXME.
  Approval remains manual by default; this does not authorize another live run.
- User also requested the `C:\repos\aks-rp` AFD and monitoring reference.
- User requested deletion of remaining attempt-9 resources. Independent checks
  subsequently returned false for all four groups; no additional delete needed.

## Plan

### Phase 1: Regression tests first

- [x] Task 1.1: Add table-driven offline tests in the existing private HTTP
  assertions test file for both origin orders, multiple blockers, count mismatch,
  empty observation, all-pending success and staggered convergence snapshots.
  Success: failing snapshots include every observed origin and state.

### Phase 2: Bounded implementation

- [x] Task 2.1: Extract an E2E-only approval-state assertion into the existing
  proof helpers. Inspect the complete snapshot before returning any state/count
  error; preserve all-pending and exact-member-count requirements.
- [x] Task 2.2: Use it in the existing convergence callback and emit changed
  aggregate diagnostics to GinkgoWriter without logging every unchanged poll.
  Success: no first-origin short circuit; no partial approval checkpoint.

### Phase 3: Validation and record

- [x] Task 3.1: Run the offline tagged private HTTP helper tests, tagged E2E
  compile-only check, goimports, golint, go vet and repository pinned lint for the
  changed E2E package. Do not run TestE2E or initiate any live retry.
- [x] Task 3.2: Record results and implementation details here.

### Phase 4: Temporary E2E approval tests

- [x] Task 4.1: Add offline regressions for default-off/invalid opt-in, full
  exact-request correlation, foreign/ambiguous/missing/rejected requests, SDK
  errors, all-target validation before writes and observed approval verification.

### Phase 5: Temporary E2E-only approval implementation

- [x] Task 5.1: Behind ENABLE_AFD_PRIVATE_LINK_AUTO_APPROVAL=true, use the test
  caller's existing credentials to approve only one uniquely correlated request
  on each exact verified test PLS. Match the owned profile's request description
  and exact connection child ARM ID; validate every target before any write.
- [x] Task 5.2: Re-read approval before normal graph/traffic convergence. Keep
  manual behavior as the default, fail closed on SDK/identity/ambiguity errors,
  add the removal FIXME and document the test caller's scoped approval access.

### Phase 6: Final validation

- [x] Task 6.1: Re-run focused offline tests, tagged compile-only, formatting,
  vet/golint/pinned lint. No live run or production/controller permission change.

Success criteria: all current origin states are visible on changed failing
polls and on timeout, every expected origin must be pending for the checkpoint,
and offline validation passes without altering the active live checkout.

## Decisions

- The provider observer already evaluates all expected origins in successful
  snapshots; the E2E assertion is the source of the first-state diagnostic loss.
- Do not change provider fail-closed identity checks or Azure SDK error handling.
- Do not broaden this change into the Unknown-state root cause, polling budgets,
  production approval, controller RBAC changes or another live attempt.
- Reference investigation: AKS1P reads the individual AFD origin with API
  2025-06-01 and validates approval on the PLS connection, rather than requiring
  the origin's sharedPrivateLinkResource.status. Fleet reads origin collection
  pages using its SDK and maps absent/unrecognized approval status to Unknown.
  A collection-versus-GET response-shape difference is a hypothesis, not a
  confirmed cause; attempt 9 did not preserve raw origin responses.
- AKS1P performs automated connection approval. Do not copy that behavior into
  Fleet production; the separately approved temporary E2E opt-in is isolated.
- AKS-RP's dashboard is the directly relevant PLS example: Premium, internal LB
  frontend IP as origin host, custom PLS with omitted group ID, HTTP /healthz
  probes and a separately approved PLS connection. Its origin template uses
  2024-02-01, so the reference does not justify a blind Fleet API-version upgrade.
- AKS-RP dashboard profile diagnostics route FrontDoorAccessLog,
  FrontDoorHealthProbeLog, FrontDoorWebApplicationFirewallLog and AllMetrics to
  Log Analytics. These are useful complementary dataplane evidence, not proof
  of linked authorization, approved connections or Gateway readiness. Adding
  diagnostic resources/costs is outside this approved E2E-only change.
- A future authorized proof should preserve raw individual-origin GET versus
  collection responses and exact PLS connection states before cleanup, then use
  edge/probe diagnostics to separate observation gaps from delivery failures.

## Implementation Details

- User approved the all-origin polling fix and offline validation. Starting with
  regression tests; the provider and isolated live candidate remain unchanged.
- Added ten table-driven cases, including consecutive staggered/reversed
  convergence snapshots. The tests-first run failed because the new assertion
  did not exist; final passing validation is still pending.
- `privateHTTPPendingOrigins` now builds the complete identity/target/state
  diagnostic before returning a state or count error.
- The poll emits changed failing summaries to GinkgoWriter and retains the
  complete successful origin snapshot in the Ginkgo report.
- Offline private HTTP tests, tagged compile-only, go vet and pinned
  golangci-lint passed for the polling change. The initial formatter command
  failed because goimports was not on PATH; the existing repository binary was
  used successfully. The missing standalone golint was installed as a development
  tool without changing dependency manifests.
- Added the explicit temporary approval opt-in, FIXME, full-target prevalidation,
  profile-request description and exact connection ARM identity checks, fail-fast
  SDK errors, idempotent handling of existing approvals and independent approval
  reads. The live proof uses a bounded three-minute approval retry, followed by
  the existing graph and traffic convergence requirements.
- Added 18 offline approval scenarios covering opt-in, correlation, ambiguity,
  foreign resources/requests, rejected state, API errors and partial success.
- Final validation passed: `go test -tags=e2e -count=1 -run '^TestPrivateHTTP'
  ./test/e2e`, `go test -tags=e2e -run '^$' ./test/e2e`, `go vet -tags=e2e
  ./test/e2e`, pinned `golangci-lint run --build-tags=e2e ./test/e2e` and native
  `git diff --check`. Formatting used the existing goimports binary.
- Standalone golint passed both the package command and explicit paths of all
  four changed tagged E2E files, ensuring build tags did not hide the changes.
- WSL Git's whole-worktree whitespace check reported unrelated CRLF files;
  native Windows Git confirmed only the intended files changed and passed the
  whitespace check. No unrelated line endings were rewritten.
- All four attempt-9 resource groups independently verified absent. Its
  cleanup-complete.exit=2 preserves the failed proof result, not cleanup failure.
- Unknown-state root cause is not fixed or proven; no new live run, connection
  approvals or production/controller RBAC changes were performed.
- User requested folding outstanding changes into the four appropriate signed
  commits. Documentation belongs in the existing docs commit and E2E code/tests
  in the existing live-proof commit; preserve the complete validated final tree,
  signatures, co-author and session trailers. No push was requested.

## Changes Made

- Added this approved planning/implementation breadcrumb.
- Updated the four existing private HTTP E2E files with aggregate assertions,
  temporary SDK approval helpers, explicit opt-in integration and offline tests.
- Updated `test/README.md` with the default-off option and test-caller permissions.

## Before/After Comparison

- Before: the first non-pending origin hides every subsequent state.
- After: each poll inspects the complete observed snapshot and reports all
  states while retaining the existing all-member manual-approval gate.
- Approval still defaults to manual. An explicitly authorized E2E opt-in can
  approve only verified owned-profile requests; production remains unchanged.

## References

- `.github/.copilot/breadcrumbs/2026-10-21-0113-afd-provider-implementation-plan.md`:
  related approved implementation and live-proof scope.
- `docs/howtos/gateway-api-afd-configuration.md:77-80,381-388`:
  asynchronous approval and subsequent convergence; no partial readiness.
- `pkg/providers/azure/frontdoor/observation.go:68-104`: all-origin observation.
- `test/e2e/gateway_private_http_test.go:115-137`: first-state return in polling.
- `test/e2e/gateway_private_http_proof_test.go`: existing E2E evidence helpers.
- `test/e2e/gateway_private_http_assertions_test.go`: existing offline assertions.
- `.github/.copilot/specifications` and `.github/.copilot/domain_knowledge` do
  not exist in this checkout; no versioned specification applies.
- `C:\repos\aks1p\test\internal\behaviors\afd_waf_pls.go:127-174`:
  individual-origin GET and separate PLS approval check.
- `C:\repos\aks1p\test\internal\provisioning\public_endpoint.go:519-657`:
  automated approval/retry and PLS-side status; reference only, not Fleet policy.
- `pkg/providers/azure/frontdoor/reader.go:106-140`: paginated collection reads.
- `pkg/providers/azure/frontdoor/observation.go:128-154`: Unknown classification.
- `C:\repos\aks-rp\devinfraservices\dashboard\resources\origin\Templates\origin.arm.template.json:56-73,156-183`:
  custom PLS private IP origin, 2024-02-01 and health probe settings.
- `C:\repos\aks-rp\devinfraservices\dashboard\resources\afd\Templates\afd.arm.template.json:57-102`:
  Premium profile and profile-scoped access/probe/WAF diagnostic logs and metrics.
- `C:\repos\aks-rp\devinfraservices\dashboard\README.md:1577-1603`:
  separate PLS connection approval and subsequent HTTP health validation.
- `C:\repos\aks-rp\devinfraservices\e2e\e2e-overlay-resources\pkg\provisioner\azure_front_door.go:205-248`:
  storage-origin OIDC example, not the dashboard's ILB/PLS topology.
