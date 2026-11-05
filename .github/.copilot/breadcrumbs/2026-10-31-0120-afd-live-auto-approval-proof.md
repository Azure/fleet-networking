# AFD live proof with corrected approval checkpoints

## Requirements

- Run the standard AFD-enabled setup/full suite/owned cleanup using committed
  candidate `860497d4d75e9cc2c9ab156212d31fb38c880a9e`.
- Enable temporary E2E-only PLS auto-approval under the test caller's identity.
  Do not enable production controller approval or weaken readiness.
- Preserve the six-hour minimum execution guard and 75-minute cleanup reserve.

## Additional comments from user

- "start a new live test run"
- The user previously requested hands-off E2E approval for the next run.

## Plan

### Phase 1: Preflight and authorization

- [x] Task 1.1: Verify fresh activated subscription Owner, effective permissions,
  all four previous groups absent, regional quotas and exact isolated candidate.
  Success: sufficient access/time and capacity without Azure resource changes.
- [x] Task 1.2: Validate launch scripts and installed-tool compatibility; update
  the external monitor for the PLS-based checkpoint. Success: local checks pass.
- [x] Task 1.3: Obtain immediate scoped confirmation. Success: authorized resource
  scope/window and explicit test-caller-only approval before provisioning.

### Phase 2: Live execution

- [x] Task 2.1: Start the guarded runner and independently verify startup/first
  owned provisioning milestone. Success: correct source and run tag are observed.
- [ ] Task 2.2: Observe exact all-member approval, fresh post-approval convergence,
  enabled graph, traffic, probes and withdrawal. Success: actual proof passes,
  not merely successful resource creation or PLS approval.
- [x] Task 2.3: Preserve the outcome and independently verify all four groups
  absent after cleanup. Success: no owned resources remain.

Success criteria: a passing installed-controller private HTTP proof and verified
owned cleanup within the authorized window; otherwise report the failed phase.

## Decisions

- Use the existing subscription, Sweden Central, shared VNet, three clusters and
  two Standard_D2s_v5 nodes per cluster. ATM remains disabled; ordinary suite
  scenarios remain included.
- Reuse only validated local tooling/runner behavior from attempt 10. Use a fresh
  isolated checkout, home/Kubernetes config, image tag and ownership tag.
- Authorized resource window: 2026-10-31T01:20:00Z to 09:20:00Z. Owner activation
  was independently verified through 09:11:39Z; execution stops by 07:56:39Z,
  preserving at least 75 minutes before access expiry for cleanup.
- Do not infer success from null AFD approval status, repeat manual approvals,
  change production route ordering or extend authorization silently.

## Implementation Details

- Session runner: `files/afd-standard-live-11`; source checkout remains clean.
- Image tag: `860497d-standard-11`.
- Ownership tag: `afd-proof-run=20261031-012000-standard-11`.
- Auto-approval: `ENABLE_AFD_PRIVATE_LINK_AUTO_APPROVAL=true`.
- Parent group: `fleet-afd-e2e-20261028-1951`; exact managed node groups for
  `hub`, `member-1` and `member-2` in `swedencentral` are the only cleanup scope.
- New monitor matches `PLS approval checkpoint captured for`.
- Refreshed preflight passed at 2026-10-31T01:21:04Z. Owner is Activated and
  Provisioned; effective subscription permissions are unrestricted. All four
  resource groups are independently absent. Quota usage/limits: DSv5 4/100,
  total regional cores 150/1230, with 12 additional cores required.
- `bash -n`, ShellCheck, pinned helper checksums, Docker/tool checks and
  `AFD_VALIDATE_LOCAL_TOOLS=true go test -count=1 ./test/scripts` passed.
- User explicitly confirmed: "Start now with temporary E2E-only PLS auto-approval".
  Exact scope and limits are recorded in the session's `final-approval.txt`.
- Runner `afd-standard-live-11` started at 2026-10-31T01:24:28Z with the exact
  candidate SHA. First owned VNet milestone was recorded at 01:25:11Z.
  Independent Azure reads confirmed the parent group and VNet Succeeded and the
  exact attempt-11 ownership tag. Standard setup is underway; no passing proof
  or completed approval/convergence is claimed yet.
- Read-only checkpoint monitor: `afd-live-11-approval-checkpoint`.
- Progress check at 2026-10-31T04:29Z found setup had failed at 04:27:20Z.
  The sample application's `docker push` returned ACR `UNAUTHORIZED:
  authentication required`; `make e2e-setup` exited unsuccessfully. Controller
  image manifests had been published earlier, but the Gateway controller was
  not installed and no Gateways existed in the captured status.
- `run.exit=2`; `proof.exit` is absent. The full suite and corrected approval
  workflow were never reached, so this attempt provides no new AFD convergence
  evidence. The checkpoint monitor exited because the run failed before it.
- Automatic owned cleanup was submitted. At 04:30Z all four groups still
  existed, and the parent was independently verified Deleting. Cleanup
  completion was unverified at that check; later confirmation is recorded below.
- Build output also contained Git alternate-object-path metadata warnings from
  the shared WSL checkout. Those warnings did not prevent the earlier image
  pushes and are not established as the cause of the ACR authorization failure.
- Authentication follow-up: Docker's credential file was last written at
  01:25:54Z, with one successful ACR login in the run log. The app push failed at
  04:27:20Z, roughly 3 hours 1 minute later. Bootstrap logs in before controller
  image pushes and long AKS preparation, but does not refresh before the late
  app push (`test/scripts/bootstrap.sh:61,422-423`).
- Microsoft documents a three-hour lifetime for the registry token returned by
  `az acr login`, including `--expose-token`:
  https://learn.microsoft.com/azure/container-registry/container-registry-authentication.
  Token expiry is therefore the strongest explanation, rather than new caller
  RBAC propagation. The token's own expiry claim was not inspected; this remains
  a high-confidence diagnosis, not direct token-expiration proof.
- A follow-up fix should refresh registry authentication immediately before the
  late app push and surface any authentication failure, without broader RBAC or
  blind sleeps/retries. No such source change has been made in this check.
- Attempt-11 cleanup is now complete: all four authorized resource groups were
  independently absent at 2026-10-31T04:42:56Z, matching the runner's cleanup
  confirmation. Exit markers remain 2, preserving the failed setup outcome.
- The user authorized the subsequent offline ACR-refresh fix and amendment;
  see `2026-10-31-0438-afd-acr-authentication-refresh.md`. No live rerun occurred.

## Changes Made

- Prepared local attempt-11 scripts/isolated checkout; no runtime source changes.
- Added this breadcrumb outside the isolated candidate.

## Before/After Comparison

- Attempt 10 could not reach approval while AFD status was null and began cleanup
  before a full post-approval window.
- Attempt 11 uses exact PLS request correlation and independent approval reads,
  then a fresh 45-minute AFD convergence window.

## References

- `.github/.copilot/breadcrumbs/2026-10-31-0008-afd-approval-checkpoint-fix.md`:
  implemented and tested checkpoint/deadline fixes.
- `test/README.md:440-620`: standard setup, approval, validation and cleanup.
- Session attempt-10 completion: failed proof; all four groups independently
  confirmed absent at 2026-10-31T00:38:31Z and rechecked during this preflight.
- Domain knowledge/specification folders are absent; no applicable versioned
  specification is available.
