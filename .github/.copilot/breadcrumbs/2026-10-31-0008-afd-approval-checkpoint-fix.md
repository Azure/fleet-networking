# AFD approval checkpoint and post-approval observation

## Requirements

- Fix the E2E approval checkpoint using actual, uniquely correlated PLS
  connection evidence when the AFD shared-link status is null.
- Give manual approval observation and subsequent AFD convergence independent,
  bounded time budgets. The full post-approval convergence budget starts only
  after every exact connection is independently observed Approved.
- Preserve test-caller-only, default-off automatic approval and its removal
  FIXME; never approve from a production controller.
- Preserve exact ownership/private-frontend checks, fail-closed final AFD
  readiness, traffic/probe/withdrawal assertions, diagnostics and owned cleanup.
- Do not change the running attempt, recreate resources or start another run.

## Additional comments from user

- "ok make the fixes first"
- "add clarifying but brief comments wherever applicable"
- "commit/amend changes to relevant commits"
- User manually approved both test connections during attempt 10. Both were
  independently verified Approved at 2026-10-30T23:35:18Z.
- The pending-origin assertion subsequently failed and Gateway cleanup began
  at 23:53:55Z, before a full 45-minute post-approval window could complete.

## Plan

### Phase 1: Tests first

- [x] Task 1.1: Add focused table-driven regressions for correlated Pending PLS
  requests with null/Unknown AFD approval status, mixed Pending/Approved
  connections, staggered members, missing/foreign/ambiguous/rejected requests
  and read failures. Success: safe exact requests can reach approval without
  manufacturing AFD readiness; unsafe or incomplete targets cannot.
- [x] Task 1.2: Extend sequential budget regressions and verify fresh phase
  deadlines so manual approval cannot consume the later budget. Success: distinct bounded
  request-discovery, approval-observation and post-approval phases, with
  sufficient enclosing spec/suite/cleanup headroom.

### Phase 2: E2E implementation

- [x] Task 2.1: Reuse the existing PLS request validator and expected backend
  plan to collect every target's exact connection state. Keep all-origin
  identity/configuration observation and change-only diagnostics; allow null
  AFD status only for request correlation, never as readiness.
  Success: all members are examined and results identify every blocker.
- [x] Task 2.2: Report the actual PLS checkpoint and its UTC time; automatically
  approve only under the existing explicit opt-in, otherwise wait for manual
  approval. Re-read all exact requests until Approved without rewriting
  already-approved connections. Success: early/partial manual approvals are
  handled safely and a write response alone cannot establish approval.
- [x] Task 2.3: Begin a fresh 45-minute full-graph convergence phase after the
  last verified approval; report UTC phase starts and elapsed times. Update
  enclosing deadlines only where required by the sequential phase budget.
  Success: approval waiting does not consume propagation time, while original
  cleanup/lifetime guards and all final graph assertions remain unchanged.

### Phase 3: Validate and document

- [x] Task 3.1: Run focused offline private HTTP tests, tagged compile-only,
  goimports, explicit-file golint, go vet, pinned lint and whitespace checks.
  Success: new regressions and existing proof helpers pass without live setup.
- [x] Task 3.2: Update the E2E runbook and breadcrumb with exact checkpoint,
  phase budgets, evidence and limitations. Success: manual/automatic approval
  instructions match the new code and four-commit publication remains intact.

Success criteria: the E2E can safely approve uniquely correlated PLS requests
despite null AFD status, explicitly observes all approvals, then allows a full
independent convergence window. It still fails if the real AFD graph never
becomes ready; no cleanup or safety assertions are bypassed.

## Decisions

- Limit this implementation to confirmed E2E defects. Production also gates
  route creation on AFD Approved, but the live experiment did not complete its
  post-approval observation and did not test route ordering. Do not equate null
  with Approved, add production approval writes or introduce a speculative
  production fallback. A production change needs a separately approved design
  or controlled bootstrap experiment.
- Keep PLS correlation separate from readiness: a pending/approved PLS connection
  permits only the E2E approval workflow. Final readiness remains actual AFD
  approval/deployment, enabled owned graph and successful private traffic.
- Preserve bounds: a separate approval wait is not unlimited resource retention.
  No new live authorization is implied by implementing these fixes.

## Implementation Details

- User approved: "Approve; implement the E2E fixes". The initial tests-first
  run failed on undefined new helpers; the completed implementation passes.
- Reuse `privateHTTPApprovalForPLS` in a shared read-only all-member collector.
  The collector replaces the old single-connection verification helper for both
  checkpoint discovery and independent post-write verification. Partial results
  retain diagnostic value but collection errors prevent approval or success.
- Pin each exact connection ID at discovery and reject replacement, missing or
  duplicate target identities before writing or accepting approval.
- Each discovery, approval-observation and post-approval convergence call creates
  its own bounded context. Final graph verification starts only after all exact
  PLS approvals are independently read; it still requires real AFD readiness.
- The spec now allows six hours; the documented focused command uses the
  standard suite's existing Ginkgo 9h30m / Go 10h limits. Cleanup and resource
  authorization guards are unchanged. The budget regression accounts for all
  five sequential 45-minute phases and deferred cleanup; no framework-only
  timer test was added.
- Brief inline comments explain null-status correlation versus readiness,
  all-target validation before writes, pinned request identity, early manual
  approval and fresh phase deadlines.
- Corrected a fixture preallocation warning reported by pinned lint.

Validation completed successfully:

| Command | Result |
| --- | --- |
| `goimports -w test/e2e/gateway_private_http_assertions_test.go test/e2e/gateway_private_http_network_test.go test/e2e/gateway_private_http_proof_test.go test/e2e/gateway_private_http_test.go` | Passed |
| `go test -tags=e2e -count=1 -run '^TestPrivateHTTP' ./test/e2e` | Passed |
| `go test -tags=e2e -run '^$' ./test/e2e` | Passed; compile-only |
| `go vet -tags=e2e ./test/e2e` | Passed |
| `golint -set_exit_status test/e2e/gateway_private_http_assertions_test.go test/e2e/gateway_private_http_network_test.go test/e2e/gateway_private_http_proof_test.go test/e2e/gateway_private_http_test.go` | Passed |
| `golangci-lint run --build-tags=e2e ./test/e2e` | Passed with existing pinned v1.64.7 |
| `git -c core.safecrlf=false --no-pager diff --check` | Passed |

No live setup, rerun or production change was performed. Attempt 10 remains
a failed diagnostic experiment, not a passing proof. Its final suite outcome
and complete resource cleanup remain unverified; the attempt's completion
markers were not present at this task's final check.

## Changes Made

- `test/e2e/gateway_private_http_proof_test.go`: permit nullable AFD status only
  for independent PLS correlation while retaining count and failure/drift checks.
- `test/e2e/gateway_private_http_network_test.go`: shared all-member collection,
  pinned request identities, safe opt-in writes and independently read approvals.
- `test/e2e/gateway_private_http_test.go`: actual PLS checkpoint, separate phase
  deadlines, UTC/elapsed reporting and six-hour enclosing spec budget.
- `test/e2e/gateway_private_http_assertions_test.go`: null-status, early/staggered
  approval, cancellation, replacement refusal, all-member failures and budget
  regressions.
- `test/README.md`: exact manual/opt-in checkpoint instructions, identity pinning
  and phase/spec/suite budgets.
- This breadcrumb records the approved scope, implementation and validation.
  At the user's request, fold the runbook and breadcrumb into the docs commit
  and the four Go files into the E2E commit. Re-sign the four-commit chain while
  preserving its base, original authors/messages and required trailers. Verify
  signatures and the final tree, retain a backup ref, and do not push.

## Before/After Comparison

- Before: AFD Pending is required before examining/approving PLS requests;
  null status blocks both approval and the later observation phase.
- After: exact PLS request correlation unlocks approval, every approval
  is independently verified, and propagation receives its own complete budget.
- Production's null-status dependency is documented, not silently weakened.

## References

- `.github/.copilot/breadcrumbs/2026-10-30-2045-afd-unknown-diagnostics.md`:
  approved diagnostic capture and raw/SDK evidence.
- `.github/.copilot/breadcrumbs/2026-10-30-2013-afd-all-origin-polling.md`:
  all-member polling and temporary test-caller approval.
- `test/e2e/gateway_private_http_test.go:138-217,328-351`: separate discovery,
  approval and full-graph convergence phases with timing reports.
- `test/e2e/gateway_private_http_network_test.go:43-164`: all-target collection,
  approval prevalidation, pinned identities and independent verification.
- `test/e2e/gateway_private_http_assertions_test.go:35-388`: polling,
  correlation, approval sequence and timing-budget regressions.
- `pkg/providers/azure/frontdoor/bootstrap.go:110-115` and
  `backend_provision.go:132-147`: production approval dependency.
- `pkg/providers/azure/frontdoor/inventory.go:279-300`: fail-closed final
  origin inventory checks.
- Attempt-10 session artifacts: timestamped manual-approval before/after
  snapshots and failure-before-cleanup report. Raw individual GETs with both
  API 2024-02-01 and 2025-06-01 returned null after PLS approval; no evidence
  supports a simple API upgrade or LIST-to-GET fix.
- `.github/.copilot/domain_knowledge` and `.github/.copilot/specifications`
  are absent; no applicable versioned specification is available.
