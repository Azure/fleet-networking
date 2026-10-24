# AFD manual route-association experiment

## Requirements

- Test whether manually associating the existing private origin group with the
  controller's exact desired route changes the AFD approval/deployment projection.
- Keep the experiment inside attempt 12's existing owned resource scope and
  access window. Do not introduce public origins, broaden RBAC, alter probes or
  bypass readiness checks.
- Record this as a manually assisted diagnostic, not an unmodified-controller
  live proof.

## Additional comments from user

- "can you manually add the routes now and check what happens?"
- The user observed that Front Door Manager has no associated routes.

## Plan

### Phase 1: Verify and authorize

- Task 1.1: Recheck activated Owner access, current Gateway/HTTPRoute identities,
  profile ownership, origin privacy and absence of the exact route.
  Success: all checks refer to the existing attempt-12 graph and no cleanup is
  underway. Owner access was independently reverified through 12:44:09.767Z.
- Task 1.2: Get explicit approval for the disabled-route experiment below.
  Success: the user accepts the specific state, scope and proof limitation.

### Phase 2: Associate the exact route

- Task 2.1: Capture before-state and validate the request against
  `BuildHTTPPreviewRequests`.
  Success: exactly one route, HTTP-only forwarding, `/*`, default-domain linkage,
  no redirect/cache/custom domains/rule sets, and EnabledState=Disabled.
- Task 2.2: Create only
  `fleet-route-80056390467f1335c008d0bb5bb197bd` under endpoint
  `fleet-ep-80056390467f1335c008d0bb5bb197bd`, associated with origin group
  `fleet-og-2bf09043b3789ed4d41411c7060fee12`.
  Success: an independent read confirms the request was persisted unchanged.
  Do not directly enable the route or endpoint. The existing controller may
  subsequently activate its graph only if its own approval/inventory gates pass.

### Phase 3: Observe and preserve evidence

- Task 3.1: Observe both origin approval/deployment states, route/endpoint state,
  Gateway conditions and controller logs for at most 15 minutes after the write.
  Success: captured observations distinguish association from approval, full
  activation and traffic readiness. A timeout is inconclusive, not a pass.
- Task 3.2: Record results and retain the original owned cleanup/cutoff.
  Success: no extra mutation is attempted if the result stays null. No new run
  is started; execution cutoff stays 11:29:09Z with the existing cleanup reserve.

### Checklist

- [x] Task 1.1: Verify all live prerequisites.
- [x] Task 1.2: Obtain scoped plan approval.
- [x] Task 2.1: Capture baseline and validate the exact disabled request.
- [x] Task 2.2: Submit and independently verify the single route.
- [ ] Task 3.1: Complete bounded observation.
- [x] Task 3.2: Document results and preserve cleanup.

Task 3.1 is interrupted/blocked: the original proof failed and deleted its
Gateway fixture before the 15-minute observation window could finish.

Success means the authorized route write and bounded observations are independently
verified and recorded. It does not mean the controller feature passed its original
unmodified live proof.

## Decisions

- One Kubernetes HTTPRoute targets one logical ServiceImport backend. One AFD
  route references the origin group containing both member origins; separate
  routes per member are neither needed nor controller-compatible.
- Start with the exact desired Disabled route. Directly enabling resources before
  private-origin/inventory readiness would confound the experiment and bypass the
  current activation ordering.
- The currently null approval blocks disabled-route creation in the controller.
  Whether association changes the Azure projection is a hypothesis, not a fact.
- No runtime source, SDK, image or controller configuration change is planned.

## Implementation Details

Approved: "Approve disabled-route experiment." Route properties come directly from
`pkg/providers/azure/frontdoor/graph.go:107-121`; identity derivation uses the
length-prefixed SHA-256 helper at `backend.go:206-213`.

The live Gateway and HTTPRoute are unchanged, the exact route is absent, the
profile/parent group retain expected ownership, the endpoint has never-activated
Disabled state, both origins target the expected member PLS resources, and both
pinned connections remain Approved. Baseline JSON is retained under session
`afd-standard-live-12/logs/manual-route`.

Validation passed:

- `go test -count=1 -run '^TestBuildHTTPPreviewRequests$' ./pkg/providers/azure/frontdoor`
- `bash -n manual-route-prepare.sh`
- `shellcheck -x -P "$RUN_ROOT" manual-route-prepare.sh`
- The read-only preparation's ownership, privacy, absence and access checks.
- `bash -n manual-route-write.sh` and its scoped ShellCheck passed.
- The conditional REST PUT succeeded. Independent GET verified the exact route,
  Disabled state, intended origin-group association and HTTP-only properties.
  Initial provisioning is Creating and deployment NotStarted; neither is a
  successful convergence result.

The initial Bash artifact had Windows CRLF endings and failed syntax validation
before any Azure call. Normalized that specific new script to LF and reran all
checks successfully.

## Changes Made

- Created this approved experiment plan and session-only request/preparation
  artifacts.
- Created the one authorized Disabled AFD route. No other resource was directly
  changed. PUT response and independent GET are retained with baseline evidence.
- Independent observations from 08:02:08Z through 08:09:38Z (12 snapshots)
  consistently show the route Disabled/Succeeded/NotStarted, both origin approvals
  null, endpoint Disabled/Succeeded/NotStarted, and Gateway Programmed=False/Pending
  without an address. No controller activation or traffic readiness was observed.
- The original post-approval convergence phase failed at 08:09:31.102Z. Failure
  diagnostics ran before fixture cleanup. The Gateway received deletion timestamp
  08:09:45Z with its AFD cleanup finalizer retained.
- The observer then received NotFound and exited 1, before its intended full
  window. Direct `az afd origin show` calls for both origins also returned
  NotFound. The parent group and profile still appeared in independent reads,
  so this is partial fixture cleanup, not confirmed full resource-group cleanup.
  No origin recreation, second approval, route enablement or cleanup bypass
  was attempted.
- Attempt 12 subsequently finished with failed test exit code 2. Its automatic
  owned cleanup completed, and a separate check confirmed the parent plus all
  three managed node groups absent. The manually created route was inside that
  cleaned scope; no manual teardown or finalizer bypass was required.

## Before/After Comparison

- Before: both PLS requests Approved; both AFD origin approvals null; endpoint
  Disabled/NotStarted; no AFD route; Gateway Programmed=False/Pending.
- After initial write: the exact route exists, Disabled, Creating/NotStarted.
- After provisioning: association succeeds, but approval stays null and the
  graph does not activate in the observed interval.
- Final diagnostic result: inconclusive beyond the observed interval because
  fixture cleanup interrupted the full 15-minute experiment. It is not a passing
  controller proof or proof that every possible route-first bootstrap is ineffective.

## References

- Related breadcrumb:
  `2026-10-31-0451-afd-live-acr-refresh-proof.md` (attempt-12 evidence and scope).
- `pkg/providers/azure/frontdoor/graph.go:85-129`: exact disabled route request.
- `pkg/providers/azure/frontdoor/route_provision.go:80-125`: current
  approval-before-route creation ordering.
- `pkg/providers/azure/frontdoor/bootstrap.go:110-115`: approval gate.
- Local SDK comparison: pinned armcdn v2.2.0/API 2024-02-01 and current
  armcdn v4/API 2026-10-01 both return null approval in read-only live checks.
