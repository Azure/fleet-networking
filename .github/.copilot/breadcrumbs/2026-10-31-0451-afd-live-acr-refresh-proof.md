# AFD live retry with refreshed ACR authentication

## Requirements

- Run the standard setup/full suite/owned cleanup from committed candidate
  `8d4e77eac51dc1bd310a7806a73f9645d41c00f0`.
- Enable E2E-only caller auto-approval; exercise the late app-push login refresh.
- Respect the user's eight-hour resource window, six-hour minimum execution
  guard and 75-minute cleanup reserve before actual Owner expiry.

## Additional comments from user

- "start a new live test, you have 8 hours"

## Plan

### Phase 1: Preflight

- [x] Task 1.1: Verify renewed activated Owner, effective permissions, all four
  prior groups absent and regional CPU quota. Success: sufficient access,
  capacity and execution/cleanup headroom without resource provisioning.
- [x] Task 1.2: Prepare an independent exact-source checkout and validate scripts,
  checksums, installed tools and offline harness. Success: no Git alternates and
  no candidate drift; local checks pass.
- [x] Task 1.3: Obtain immediate scoped confirmation. Success: explicit resource,
  identity/RBAC, auto-approval and cleanup authorization before starting.

### Phase 2: Run and verify

- [x] Task 2.1: Start and verify actual source, ownership tag and first Azure
  milestone. Success: intended runner is active and owned provisioning observed.
- [ ] Task 2.2: Observe refreshed app push, controller installation, exact PLS
  approvals, independent convergence, traffic, probes and withdrawal. Success:
  actual proof passes rather than only provisioning or approval.
- [ ] Task 2.3: Preserve outcome and independently verify all four groups absent.
  Success: complete owned cleanup within the permitted lifetime.

Success criteria: a passing installed-controller private HTTP proof and verified
cleanup; otherwise report the exact failed phase without speculative readiness.

## Decisions

- Reuse the same subscription/Sweden Central, shared VNet, ACR, AFD Premium and
  three clusters with two Standard_D2s_v5 nodes each. ATM remains disabled.
- Use a fresh independent clone, not shared Git alternates, to avoid attempt-11
  Windows/WSL build-metadata warnings. Do not change runtime source for that.
- Authorized window: 2026-10-31T04:51:00Z to 12:51:00Z. Activated Owner was
  independently observed through 12:44:09Z; execution cutoff is 11:29:09Z,
  retaining 75 minutes of cleanup access.
- Auto-approval is test-caller-only; production approval/readiness is unchanged.

## Implementation Details

- Session runner: `files/afd-standard-live-12`.
- Image tag: `8d4e77e-standard-12`.
- Ownership tag: `afd-proof-run=20261031-045100-standard-12`.
- Parent: `fleet-afd-e2e-20261028-1951`; only its three managed AKS node groups
  for `hub`, `member-1`, `member-2` in `swedencentral` accompany this scope.
- Activated/Provisioned Owner starts at 04:44:12Z and ends at 12:44:09Z;
  effective subscription permissions are unrestricted. All four groups were
  independently absent and rechecked by the guarded preflight.
- Refreshed quota: DSv5 4/100 and total regional cores 150/1230, with 12 required.
- Independent Git clone is clean with exact candidate SHA and no alternate
  object paths. Both WSL Git and native Windows Git can resolve HEAD.
- Bash syntax, ShellCheck for launch helpers, pinned checksums, tool/Docker
  checks and `AFD_VALIDATE_LOCAL_TOOLS=true go test -count=1 ./test/scripts`
  passed; preflight completed before 04:55:34Z without resource provisioning.
- User immediately confirmed "Start now with temporary E2E-only PLS
  auto-approval"; exact scope/window is recorded in `final-approval.txt`.
- Runner `afd-standard-live-12` started at 2026-10-31T04:59:00Z with the
  expected candidate SHA. First network milestone was recorded at 04:59:42Z.
  Independent Azure reads confirmed parent/VNet Succeeded and the exact
  attempt-12 ownership tag. Standard setup is underway; proof is not yet complete.
- Read-only checkpoint monitor: `afd-live-12-approval-checkpoint`.
- Progress check at 2026-10-31T05:17:53Z independently confirmed all three AKS
  clusters Succeeded/Running with two nodes each; parent group remains Succeeded.
  All five controller/CRD-installer image manifests were pushed.
- The new late-stage ACR login executed and reported Login Succeeded before
  the sample-app push. Layers continued completing through 05:20:56Z, with no
  authorization failure observed. At 05:21:51Z the final `app:latest` tag was
  not yet present; publication is not claimed complete.
- No proof/runner exit markers exist as of 05:22:15Z. AFD profile list was empty
  at the earlier state check; the full suite/PLS approval/convergence proof has
  not started yet. No live settings were changed by these progress checks.
- The corrected checkpoint succeeded at 05:45:36Z for profile
  `fleet-afd-80056390467f1335c008d0bb5bb197bd`, proof namespace `my-ns-mckfa`.
  Both exact requests were Pending. Automatic test-caller approval completed
  and independently verified all approvals at 05:45:38.753Z (about two seconds).
- Fresh post-approval AFD convergence started at 05:45:38.753Z, with logged
  deadline 06:30:38.753Z. Independent current PLS reads confirm both exact
  connections remain Approved and both PLS resources Succeeded.
- At 07:34Z, both AFD origins still report approval null, provisioning Succeeded
  and deployment NotStarted. The endpoint remains Disabled/NotStarted. Current
  Gateway read is Accepted=True, Programmed=False/Pending, with no address.
  Auto-approval worked, but the enabled graph/traffic proof has not succeeded.
- A separate harness/timing concern is now observed: the runner is still active,
  neither phase-finish nor exit markers exist, and logs have not advanced beyond
  05:45:38Z despite the logged 45-minute deadline being past. Windows and WSL
  wall clocks agree at 07:34Z; a bounded Kubernetes read succeeds. Do not infer
  normal propagation or a passing proof from this overdue phase. No timeout,
  readiness, endpoint or approval configuration was changed by the checks.
- Controller-log inspection confirms the exact candidate Gateway manager pod is
  Running/Ready, has zero restarts and acquired its leader lease. It recorded
  `AFDHTTPProvisioningFailed` and reconciliation errors
  `context deadline exceeded\ncontext deadline exceeded` at 05:53:46Z and
  07:35:50Z. The captured startup-to-current logs do not identify the specific
  timed-out ARM operation or supply an HTTP response code, so request timeouts
  are verified but Azure-side approval propagation is not an established root
  cause. The provisioning and observation code each create 30-second contexts;
  these logs alone do not explain the long wall-clock gaps or the stalled proof.
  Evidence is retained in the session artifact
  `afd-standard-live-12/logs/gateway-controller-progress.log`. No restart,
  runtime configuration change or Azure write was performed.
- Inspected the user-requested local SDK checkout `C:\repos\azure-sdk-for-go`
  at `e948fd22d7`, including the exact `armcdn/v2.2.0` and `azcore/v1.18.0`
  release tags. Fleet uses armcdn v2.2.0/API 2024-02-01; the checkout currently
  contains armcdn v4/API 2026-10-01. Both versions explicitly decode
  `sharedPrivateLinkResource.status` into the same nullable status field.
  The CDN changelog contains no identified null-approval fix. The pinned retry
  policy preserves parent contexts and aborts retries on parent cancellation;
  that does not identify which live transport/auth/ARM operation timed out.
- Read-only live LIST comparisons against APIs 2024-02-01 and 2026-10-01 return
  the same complete two-origin inventory: approval null, provisioning Succeeded,
  deployment NotStarted. A latest-API read therefore does not fix the observed
  projection, and a major SDK upgrade is not established as a remedy. No
  dependency manifests or SDK checkout files were modified.
- The user's empty route-association view matches the current controller gate:
  `EnsureDisabledRoute` calls `EnsurePrivateBackends` before route access, and
  bootstrap backends require `originApprovedAndProvisioned`. Null approval
  prevents even disabled-route creation. This confirms why the route is absent,
  not that Azure requires a route before projecting approval; a circular
  bootstrap dependency remains an unverified hypothesis. No route was added.
- Checked Microsoft's internal-load-balancer Private Link guide against the
  user's troubleshooting text. It describes origin creation followed by PLS
  approval, does not document route association as an approval prerequisite,
  and explicitly requires certificate subject name validation with Private Link.
  Both exact requests here are already Approved; null on the AFD side does not
  prove that they are unapproved. A missing ARM route is distinct from data-plane
  health-probe failures. Public-origin service-tag advice and origin TLS
  workarounds do not resolve this HTTP/private-origin proof's verified state.
  Reference:
  https://learn.microsoft.com/azure/frontdoor/standard-premium/how-to-enable-private-link-internal-load-balancer
- A separately approved manually assisted diagnostic created the exact Disabled
  route; see `2026-10-31-0755-afd-manual-route-association-experiment.md`.
  Its association provisioned successfully, but 12 independent snapshots through
  08:09:38Z retained null approval on both origins, Disabled/NotStarted endpoint,
  and Programmed=False/Pending Gateway.
- Post-approval convergence ultimately failed at 08:09:31.102Z. Its report says
  elapsed=45m0.030s despite UTC start 05:45:38.753Z and finish 08:09:31.102Z.
  This is evidence of a wall-clock/elapsed-clock discrepancy; clock adjustment
  or suspension is a hypothesis, not a verified cause. It does not establish that
  Ginkgo's duration-based timeout failed to fire.
- Fixture cleanup began with Gateway deletion timestamp 08:09:45Z and the AFD
  cleanup finalizer retained. Origin LIST/GET and both direct CLI origin reads
  subsequently returned NotFound. The manual observer exited 1 before its full
  window, so the experiment is interrupted rather than passed. Parent group and
  profile remained visible at the independent check; full suite/runner exit
  markers and complete resource-group cleanup are still pending.
- Final runner result is failed: `proof.exit`, `run.exit`, `launch.exit` and
  `cleanup-complete.exit` all preserve exit code 2. The Go E2E target reported
  FAIL; successful live private delivery remains unproven.
- The runner reported the owned parent and all managed node groups absent.
  A separate verification started at 16:22:54Z and confirmed `exists=false` for
  the parent and all three exact managed node groups. No resources were relaunched
  or recreated. Cleanup is now independently confirmed complete; exit code 2 on
  the cleanup marker preserves the failed test result, not a cleanup failure.
- Final Ginkgo summary: 24 Passed, 1 Failed, 19 Skipped (25 of 44 specs ran).
  `proof.exit`/`run.exit` were written at 08:29:10Z; `launch.exit` and
  `cleanup-complete.exit` at 08:35:49Z, before the 11:29:09Z execution cutoff
  and 12:44:09Z Owner expiry. The later independent absence check does not
  imply resources remained until its verification time.

## Changes Made

- Prepared local attempt-12 environment/checkout scripts and this breadcrumb.
- No controller source, RBAC policy or running-resource changes.

## Before/After Comparison

- Attempt 11 failed the late sample-app push roughly three hours after login.
- This candidate refreshes ACR login immediately before that push, fails
  explicitly if login fails, and preserves the corrected PLS/convergence phases.

## References

- `.github/.copilot/breadcrumbs/2026-10-31-0438-afd-acr-authentication-refresh.md`:
  tested/committed refresh and baseline ShellCheck comparison.
- `.github/.copilot/breadcrumbs/2026-10-31-0120-afd-live-auto-approval-proof.md`:
  failed attempt 11 and independently verified cleanup.
- `test/README.md:440-620`: standard setup/approval/validation/cleanup workflow.
- No domain knowledge/specification folders or applicable versioned specs exist.
