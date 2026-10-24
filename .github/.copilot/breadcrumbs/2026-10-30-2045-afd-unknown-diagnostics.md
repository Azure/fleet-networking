# AFD Unknown-state diagnostic evidence

## Requirements

- Preserve enough read-only evidence to diagnose Unknown origin states on the
  next explicitly authorized proof, before fixture cleanup destroys resources.
- Inspect all members; distinguish raw collection responses, individual-origin
  GET responses, SDK-decoded fields and PLS-side connection states.
- Preserve original readiness/approval/ownership gates, resource authorization
  and test results. Do not treat diagnostic evidence as readiness.
- Never log credentials, authorization headers or unrelated resource payloads.

## Additional comments from user

- "can you also include more diagnostics around this to root cause the issue should it occur next time"
- AKS1P and AKS-RP were requested as references in the preceding task.
- "C:\repos\azure-sdk-for-net" / "check this sdk also for afd + waf +pls"
- The previous live failure reported member-1 Unknown even though both test PLS
  resources had pending connections. Raw origin responses were not preserved.

## Plan

### Phase 1: Tests first

- [x] Task 1.1: Add focused offline transport/fixture tests for missing versus
  null versus unrecognized Private Link status, LIST/GET disagreement, complete
  multi-member snapshots and bounded/paginated reads.
- [x] Task 1.2: Test failures, capture bounds and redaction: record diagnostic
  read failures without losing the original failure; do not emit credentials or
  whole unrelated resource bodies, and do not change SDK response consumption.

### Phase 2: Read-only E2E diagnostics

- [x] Task 2.1: Add an E2E-only collector for the exact owned expected origins
  and verified member PLS resources. Record UTC time, operation/resource identity,
  API version, HTTP status, Azure request/correlation IDs, and selected raw
  provisioning/deployment/Private Link fields alongside SDK-decoded fields.
- [x] Task 2.2: Collect both origin collection pages and individual-origin GETs,
  plus each PLS's provisioning and connection identity/status/description. Scope
  output to the test's expected resources; surface missing fields and read errors.
- [x] Task 2.3: Capture on the first/changed Unknown observation and once on proof
  failure before Gateway/member cleanup, with bounded calls and diagnostic report
  entries. Include current Gateway conditions/controller error evidence.
  Avoid adding Azure reads to every unchanged 15-second convergence poll.

### Phase 3: Validate and document

- [x] Task 3.1: Run focused offline private HTTP tests, tagged compile-only,
  goimports, explicit-file golint, go vet, pinned lint and native whitespace check.
- [x] Task 3.2: Document automatic capture, evidence interpretation and remaining
  limitations. Keep the four-commit convention when publication is requested.

Success criteria: a repeated failure retains all-member LIST/GET/raw-versus-SDK
and PLS-side evidence plus current controller diagnostics before cleanup, while
approval, readiness and credential protection remain unchanged.

## Decisions

- Limit this change to E2E diagnostics, not a speculative provider/API-version
  fix. AKS-RP's PLS template also uses 2024-02-01.
- Preserve raw selected JSON field shapes rather than only marshaling SDK models,
  which can lose unknown properties or omitted-versus-null distinctions.
- Diagnostic collection is read-only. Its errors must be explicit in the report
  without replacing the original convergence failure or claiming success.
- No Azure setup, resource recreation, approval or new live test is authorized
  by this code-change request.
- The .NET SDK uses the same nullable `sharedPrivateLinkResource.status` wire
  field, with documented Pending/Approved/Rejected/Disconnected/Timeout values.
  Omitted or null status stays null; it does not manufacture Pending. This is
  corroborating model evidence, not proof of a Go decoder bug or the service's
  LIST/GET projection behavior.
- Network PLS approval/description and CDN WAF security-policy
  provisioning/deployment are separate model states; neither substitutes for
  an observed AFD origin approval state. No speculative production change made.

## Implementation Details

- User approved the diagnostics implementation and offline validation. Starting
  with offline response-shape and all-member capture regressions.
- Added an exact-GET-path Azure pipeline policy that captures at most 1 MiB of
  selected JSON, then replays consumed bytes and original read errors to the SDK.
  Foreign pagination is rejected before transport; cancellation is checked
  explicitly, including with offline custom transports.
- Raw projection preserves omitted/null/new status shapes and selected field
  names. SDK projections use the same selected schema. Unrelated collection
  resources, authorization headers and arbitrary error/description text are
  excluded; owned-profile request descriptions can be emitted.
- The one-minute Azure collector verifies profile ownership, reads all origin
  LIST pages and every individual origin/PLS GET, and continues other members
  after one failed read. Reports retain HTTP status, API version, request IDs,
  explicit incomplete-capture issues and original wrapped SDK errors.
- Registered failure diagnostics after the Gateway/member cleanup callbacks,
  so Ginkgo LIFO cleanup captures evidence before those fixtures are deleted.
  Pending-origin polling captures changed Unknown/error evidence, not unchanged
  polls. Diagnostic failures do not replace the proof failure.
- Current Gateway status and structured controller error metadata are included.
  Controller discovery reuses Deployment -> ReplicaSet UID -> Pod ownership;
  typed Kubernetes PodLogs reads at most two owned manager pods, 100 lines /
  32 KiB each. Raw controller error bodies are never emitted.
- Course corrections: formatter caught misplaced test function declarations;
  fixed their top-level placement. Lint rejected subprocess log capture, so
  reused the existing typed PodLogs helper instead. Fixed preallocation and
  extracted focused evidence assertions to meet the pinned complexity rule.

### Validation

All final checks passed in WSL using existing repository tools:

- `goimports -w test/e2e/gateway_private_http_test.go test/e2e/gateway_private_http_diagnostics_test.go test/e2e/gateway_private_http_diagnostics_assertions_test.go`
- `go test -tags=e2e -count=1 -run '^TestPrivateHTTP' ./test/e2e`
- `go test -tags=e2e -run '^$' ./test/e2e` (compile-only; no live BeforeSuite)
- `go vet -tags=e2e ./test/e2e`
- `golint -set_exit_status test/e2e/gateway_private_http_test.go test/e2e/gateway_private_http_diagnostics_test.go test/e2e/gateway_private_http_diagnostics_assertions_test.go`
- `golangci-lint run --build-tags=e2e ./test/e2e`
- Native Git `diff --check` on changed tracked files; new diagnostic Go files
  were formatted and included in compiler/linter checks.

Offline regressions cover wire shapes, all members after a first-origin 403,
paginated LIST/GET disagreement, foreign pagination/profile refusal,
cancellation, oversized and failing response-body replay, output redaction and
controller-pod ownership. No dependencies or production provider code changed.
No live proof run, approval, commit rewrite or push performed for this task.
Actual service behavior and pre-cleanup live report capture remain unverified
until a separately authorized run.

### Publication

- User requested amending these changes into their appropriate commits.
- Fold this breadcrumb and `test/README.md` into the documentation commit;
  fold the three diagnostic Go files into the E2E/live-proof commit.
- Preserve the original parent, messages, authors and required trailers, exactly
  four signed commits, and the validated final working-tree contents.
- Retain a backup ref and verify signatures/tree equality before moving the
  branch. No push or live run is requested.
- Signed folding completed and verified: exactly four valid signatures, original
  messages/authors/trailers and base parent preserved, only the five intended
  diagnostic/runbook/breadcrumb files differ, and the worktree is clean.
  The original published head remains at
  `refs/copilot/backups/19936d92-before-unknown-diagnostics-amend`.
- Windows native stdin initially changed message line endings; the message
  equality guard stopped before moving the branch. Switched to explicit UTF-8/LF
  stdin. A later temporary-index environment restore failed; synchronized the
  real index in a fresh shell without changing working files, then verified
  clean staged/unstaged state. Folding this completion record into docs as well.

## Changes Made

- `test/e2e/gateway_private_http_diagnostics_test.go`: E2E-only bounded
  raw/SDK/PLS collector and controller-log projection.
- `test/e2e/gateway_private_http_diagnostics_assertions_test.go`: offline
  diagnostic transport, replay, redaction and ownership regressions.
- `test/e2e/gateway_private_http_test.go`: change-only/failure-before-cleanup
  reports and shared owned-controller pod discovery.
- `test/README.md`: automatic capture, interpretation and limits.
- This breadcrumb: approved plan, SDK evidence, course corrections and results.

## Before/After Comparison

- Before: aggregate Unknown states and an earlier controller 403 remain, but the
  wire response fields that produced Unknown are lost after cleanup.
- After: bounded diagnostic snapshots retain raw versus decoded status and
  independent PLS connection evidence for every member before destruction.
- The previous Unknown root cause remains unresolved: no raw response survived
  that deleted run. These changes collect evidence; they do not claim a fix.

## References

- `.github/.copilot/breadcrumbs/2026-10-30-2013-afd-all-origin-polling.md`:
  approved aggregation and temporary E2E-only approval changes.
- `pkg/providers/azure/frontdoor/reader.go:47-140`: owned collection observation.
- `pkg/providers/azure/frontdoor/reader_test.go:25-175`: bounded/paginated SDK
  transport regression patterns.
- `pkg/providers/azure/frontdoor/observation.go:128-154`: Unknown classification.
- `test/e2e/gateway_private_http_test.go:83-150`: cleanup ordering and polling.
- `docs/howtos/gateway-api-afd-configuration.md:77-80,381-388`: asynchronous
  approval does not establish full-graph readiness.
- AKS1P and AKS-RP reference paths and conclusions are recorded in the preceding
  breadcrumb; no source copied from those repositories.
- `.github/.copilot/domain_knowledge` and `.github/.copilot/specifications` are
  absent in this checkout; no versioned specification applies.
- `C:\repos\azure-sdk-for-net\sdk\cdn\Azure.ResourceManager.Cdn\src\Generated\Models\SharedPrivateLinkResourceProperties.Serialization.cs:141-199`
  and `SharedPrivateLinkResourceProperties.cs:53-75`: nullable wire status and
  documented values.
- Same CDN `RestOperations\FrontDoorOriginsRestOperations.cs:48-73,159-183`
  and `CollectionResults\FrontDoorOriginDataCollectionResultOfT.cs:53-89`:
  separate individual GET/collection requests and origin data models.
- `C:\repos\azure-sdk-for-net\sdk\network\Azure.ResourceManager.Network\src\Generated\Models\NetworkPrivateLinkServiceConnectionState.cs:27-57`:
  independent PLS status, description and actions required.
- Same CDN `FrontDoorSecurityPolicyData.cs:47-99`: separate WAF policy
  provisioning/deployment/association properties.
