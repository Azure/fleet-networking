# Stage disabled AFD routes before approval

## Requirements

- Separate disabled route construction/association from Private Link approval.
- For an owned, never-activated Disabled bootstrap endpoint, create the desired
  Disabled route once all expected private backends are correctly configured and
  provisioned, even when approval is Pending or absent.
- Continue waiting for every AFD origin to report Approved before inventory
  success or route/endpoint activation. Publish no address or Programmed=True
  until the complete enabled graph satisfies existing readiness requirements.
- Preserve authorization, ownership/finalizers, private-only validation,
  post-activation deployment checks, cancellation and one-write-per-call limits.
- No Azure mutation, new live run, SDK upgrade, retention flag or commit/push.

## Additional comments from user

- "is this a dependency? if not maybe also add the routes and then wait for approval?"
- Plan explicitly approved: "Approve plan and implement".
- "Make sure the message format in the events and conditions and description aligns with the rest of the repo"
- "commit/amend to appropriate commits"

## Plan

### Phase 1: Write focused regression tests first

- Task 1.1: Extend existing backend/route/bootstrap fixtures for Pending and null
  approval. Verify both expected origins are present before a Disabled route is
  created and that incomplete/drifted backends do not unlock route construction.
  Success: focused new tests fail with the current approval-before-route gate.
- Task 1.2: Cover a fully associated Disabled graph still awaiting approval.
  Verify no activation, inventory-success or hostname/readiness publication;
  verify approval later permits the existing activation sequence.
  Success: focused tests distinguish construction from activation and preserve
  already-activated endpoint restrictions.

### Phase 2: Separate construction and approval gates

- Task 2.1: Represent fully configured/provisioned bootstrap private backends
  awaiting approval separately from approved Configured/Converged backends.
  Allow only this narrow additional state to reach Disabled route staging.
  Success: Pending/null does not prevent association, but missing backends,
  provisioning submissions, contradictory identities and unsafe configuration do.
- Task 2.2: Keep inventory completion and activation gated on the existing
  approved states. Return ordinary staging progress while approval is pending
  rather than issuing inventory reads that would turn the wait into an error.
  Success: route association is observed while route and endpoint stay Disabled;
  existing approval diagnostics and all activation checks remain effective.

### Phase 3: Validate and document

- Task 3.1: Run affected provider tests and directly dependent tests; format Go
  changes with goimports and run go vet/golint as requested using available
  repository tooling. Investigate actual missing-tool failures before restoring
  tools; do not change dependencies unnecessarily.
  Success: relevant tests and available required static checks pass; any platform
  or missing-tool limitation is explicit.
- Task 3.2: Update the how-to/current construction comments and this breadcrumb
  to distinguish early Disabled association from approval-gated activation.
  Success: documentation matches tested behavior and does not claim Azure's null
  projection is repaired or a live traffic proof exists.

### Checklist

- [x] Task 1.1: Construction regression tests.
- [x] Task 1.2: Approval/activation separation tests.
- [x] Task 2.1: Bootstrap backend construction state and route gate.
- [x] Task 2.2: Approval-gated inventory and activation.
- [x] Task 3.1: Formatting, tests and static checks.
- [x] Task 3.2: Documentation and completed evidence.

Complete when a provisioned Pending/null private bootstrap graph automatically
acquires its Disabled route, all existing activation/readiness/ownership guards
are preserved, focused checks pass, and limitations are documented.
Plan explicitly approved and implementation completed. No live operation authorized.

## Decisions

- Azure accepted the exact Disabled route in the earlier authorized manual
  experiment while origin approval was null. Thus Approved is not a prerequisite
  for that configuration write. It remains an activation requirement here.
- Microsoft ILB guidance does not require AFD-projected Approved before Disabled
  route creation. APIM/Application Gateway examples approve backend connections
  before routes, but do not establish a mandatory AFD-field gate on construction.
- Do not reuse the broad OriginsPending state for route eligibility: it also
  describes incomplete/provisioning backends. Use an explicit narrow bootstrap
  state so callers cannot confuse construction eligibility with approval.
- StageDisabledGraph must not report InventoryVerified or start approval-dependent
  inventory/stale cleanup when the new state merely allows Disabled association.
- Preserve rejection/failure handling and do not translate null into Approved.
- This change addresses the missing automatic association, not the unproven
  Azure cause of persistent null approval. The manual experiment did not clear it.

## Implementation Details

Expected changes are localized to backend/bootstrap eligibility, route staging
and composed staging in `pkg/providers/azure/frontdoor`, existing tests there,
and the current how-to. Reuse existing lifecycle, identity and configuration
checks; retain the approved states used by activation.
Plan approved; regression tests were written before implementation.
Rejected/disconnected/timed-out/unrecognized approval states remain outside the
new route-construction eligibility state.

The new Pending/null route-creation and route-observation cases failed against
the original implementation as expected: route access stopped at BackendsPending
(10/12 or 10/11 planned calls). Ineligible backend guard cases passed. Tasks 1.1
and 1.2 were checked only after the focused regression/bootstrap tests passed.
Existing Gateway messages use `AFD private HTTP graph: <State>` and semicolon
diagnostics; approval events use Normal/AFDPrivateLinkApprovalRequired only for
observed Pending targets. Preserve these formats and do not infer pending PLS
requests from null AFD approval.

Added `PrivateBackendsStaged` only for fully provisioned bootstrap origins with
Approved/Pending/absent approval. The existing Configured state still requires
Approved. Route staging accepts the new state; composed inventory explicitly
requires Configured or Converged. Rejected/disconnected/timed-out/unrecognized
approval cannot unlock the new state. The existing never-activated endpoint,
private configuration, write observations and deployment checks remain in use.

The focused new regression, bootstrap activation and staging/restart tests now
pass. Added dependent controller message tests for staged Pending/Unknown origins:
Gateway/listener Pending messages preserve the standard prefix and diagnostics;
only actual Pending targets produce the existing approval-required event, and
unchanged polls emit no duplicate event. The full controller package validated
these cases and its existing integration tests.

### Validation results

All commands below passed in WSL from the repository; existing tools/assets were
used without dependency changes or installation:

```text
hack/tools/bin/goimports-latest -local go.goms.io/fleet-networking -w pkg/providers/azure/frontdoor/bootstrap.go pkg/providers/azure/frontdoor/backend_provision.go pkg/providers/azure/frontdoor/route_provision.go pkg/providers/azure/frontdoor/staging.go pkg/providers/azure/frontdoor/http_provision.go pkg/providers/azure/frontdoor/bootstrap_test.go pkg/controllers/hub/gateway/http_provisioning_test.go

go test -count=1 -run 'Test(ReconcileHTTPPreviewBootstrap|RouteProvisionerBootstrap|StageDisabledGraphApprovalAndRestart)' ./pkg/providers/azure/frontdoor

assets=$(hack/tools/bin/setup-envtest-release-0.22 use 1.33.0 -p path --installed-only)
KUBEBUILDER_ASSETS="$assets" go test -count=1 ./pkg/providers/azure/frontdoor ./pkg/controllers/hub/gateway

go vet ./pkg/providers/azure/frontdoor ./pkg/controllers/hub/gateway ./cmd/hub-gateway-controller-manager
hack/tools/bin/golint -set_exit_status ./pkg/providers/azure/frontdoor ./pkg/controllers/hub/gateway
hack/tools/bin/golangci-lint-v1.64.7 run ./pkg/providers/azure/frontdoor/... ./pkg/controllers/hub/gateway/...
```

Full package durations: provider 38.404 seconds; Gateway 234.894 seconds.
The initial preimplementation regression selector intentionally failed for all
four Pending/null route cases, then the focused implementation selector passed.
An initial composed PowerShell command produced no test output and was not used
as validation evidence; the literal Bash command above produced both package
results. Git diff whitespace checks also passed.

The new tests cover absent/existing Disabled routes with Pending/null approval,
ordinary approval waits without inventory reads/activation, all-member Pending
diagnostics, provisioning/deployment and rejection-state construction guards,
and existing controller condition/event formatting and event deduplication.
Existing activation, ownership, post-bootstrap deployment, drift and failure
tests remain passing. Mocked ARM tests and local envtest are not live Azure proof.
No SDK/dependency changes, Azure mutations, environment recreation, commit or
push were performed. Null approval projection's Azure-side cause remains unknown.

### Authorized commit consolidation

Fold the how-to and four pending investigation/implementation breadcrumbs into
the existing docs commit; fold provider/controller implementation and regression
tests into the existing feature commit. Preserve the four-commit docs/feature/
build/E2E structure and existing commit trailers; re-sign rewritten commits.
Record the complete validated tree before history rewriting and require the
same tree afterward, a clean worktree, four commits over the unchanged base,
and valid signatures. Keep a temporary recovery ref during the rewrite. No push
is authorized by this request.

## Changes Made

- Updated backend/bootstrap eligibility, route staging and inventory gate.
- Added existing-file bootstrap regressions and controller message tests.
- Updated the how-to to separate Disabled association from approval/activation.
- Completed formatting, full affected-package tests, dependent manager vet,
  requested golint, repository-configured lint and whitespace validation.

## Before/After Comparison

- Before: AFD-projected Approved is required even to create the Disabled route.
- After: provisioned private bootstrap backends permit Disabled route
  creation/association; Approved still gates inventory success and activation.

## References

- `2026-10-31-2026-afd-setup-sequence-and-restrictions-audit.md`: completed audit.
- `2026-10-31-0755-afd-manual-route-association-experiment.md`: conditional
  Disabled route creation succeeded; observed null did not change.
- `pkg/providers/azure/frontdoor/backend_provision.go:133-147`: approval-gated
  bootstrap Configured state.
- `pkg/providers/azure/frontdoor/bootstrap.go:110-115`: Approved/provisioned guard.
- `pkg/providers/azure/frontdoor/route_provision.go:99-108`: current route gate.
- `pkg/providers/azure/frontdoor/staging.go:55-72`: route-observed inventory gate.
- `pkg/providers/azure/frontdoor/http_provision.go:212-229`: inventory-gated activation.
- `pkg/providers/azure/frontdoor/bootstrap_test.go:39-114`: existing table-driven
  bootstrap/activation fixtures.
- `docs/howtos/gateway-api-afd-configuration.md:1-36`: current implementation boundary.
- `docs/design/gep-1748-gateway-api.md`: Proposed, dated 2026-08-17,
  product contract updated 2026-10-20. Preserve private-only product contract.
- Domain-knowledge and specification directories are absent; no applicable
  specification contents are inferred.
- https://learn.microsoft.com/azure/frontdoor/standard-premium/how-to-enable-private-link-internal-load-balancer
- https://learn.microsoft.com/azure/frontdoor/standard-premium/how-to-enable-private-link-apim
- https://learn.microsoft.com/azure/frontdoor/how-to-enable-private-link-application-gateway
