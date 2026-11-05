# HTTP live validation after review fixes

## Run 25 preparation: installer build fix and authorized amend

User: "fix and commit (but amend) and commit any other outstanding changes and
then start a fresh live run".

The run-24 installer failure is deterministic: Fleet v0.18.16 now imports the
networking API, but the installer Docker build stage copied only cmd sources.
Add `COPY api/ api/`, matching the other manager build contexts. Validate with
an actual installer image build and isolated `--help` smoke test before
amending HEAD. Include this outstanding breadcrumb in the requested amend.
No push or HTTPS changes are authorized.

- [x] Build and smoke-test the corrected installer image.
- [ ] Run script/tool checks, inspect the complete staged changes and amend.
- [ ] Prepare a fresh isolated exact-commit run; repeat source/access/quota,
  DNS/build health and old/fresh fixture absence checks before provisioning.
- [ ] Run standard setup/full HTTP E2E and independently verify teardown.

Validation before amend:

- `docker buildx build --file docker/net-crd-installer.Dockerfile --platform linux/amd64 --load --tag fleet-networking/net-crd-installer:live25-preflight --progress plain --build-arg GOARCH=amd64 --build-arg GOOS=linux .`
  passed. The first wrapper built the image successfully but failed afterward
  because PowerShell expanded a Bash status variable; a clean repeat returned
  exit 0. No failed wrapper is counted as a passing command.
- `docker run --rm --network=none fleet-networking/net-crd-installer:live25-preflight --help`
  passed, exit 0. Image ID:
  `sha256:13a5320c117439d34208fe940890976732723afffe595cf565a8c3ff2c2f9c33`.
- `go test -count=1 ./test/scripts` passed (6.789s) on the prior isolated
  source, whose scripts are unchanged by this Dockerfile-only fix.
- `git diff --check` passed. No Go production/test source or dependencies
  changed, so the previously passing full local suites are not repeated.
- Docker daemon and registry/control-plane DNS responded during preflight;
  that does not prove that the earlier intermittent DNS issue is fixed.

## Run 24 setup failure and completed cleanup

User requested cleanup and said testing will restart later. Run 24 has
run.exit=2 during setup and no successful E2E marker. Logs show ACR DNS
timeouts and a later net-crd-installer image-build failure; no live suite pass
is claimed. The returned WSL process snapshot showed no owned work/launcher.
After the cleanup request, independent Windows Azure CLI reads successfully
verified all four exact groups absent: fleet-afd-e2e-20261110-1745 and its hub,
member-1 and member-2 managed groups. Session receipt:
afd-standard-live-24/independent-cleanup-verification.txt.
Schedule 3 is stopped; do not start a replacement until explicitly requested.

## Fresh live run authorized after complete teardown

- User: "start the live testing now".
- Prior fixture teardown is verified complete; fresh fixture remains
  `fleet-afd-e2e-20261110-1745`, candidate 9931c1d.
- New admission at 2026-11-10T21:39:56Z passed original caller, effective
  Owner, old/fresh group absence, pinned source and quota checks.
- Renewed Owner expires 2026-11-11T05:36:24Z; execution cutoff is 04:21:24Z,
  retaining the full 75-minute cleanup reserve. This supersedes earlier
  read-only access and inadequate-budget observations.
- Proceed through the explicit approval gate and tested deadline/sleep-guarded
  launcher, standard full baseline plus AFD HTTP E2E, and guarded teardown.
  Do not equate launcher or setup success with test success. No push.

## Final old-fixture teardown and requested pause

Retry 4 passed with exit 0. At 2026-11-10T21:28:21Z, the parent
`fleet-afd-e2e-20261110-0457` and all three exact AKS-managed resource groups
were independently verified absent. No foreign AFD private endpoint was
targeted. No replacement fixture was provisioned.

Read-only readiness subsequently rechecked old-fixture absence, pinned clean
candidate 9931c1d, fresh fixture absence and quota: 100 DSv5 cores and 1110
regional cores available, versus 12 required. Source/scope/quota verification
completed at 21:30:59Z; independent old-fixture recheck at 21:31:12Z.

Admission failed closed on current access, not source or quota. Effective
permissions at 21:31Z were only `*/read`; separate role-schedule inspection
at 21:35:12Z found zero active Owner activations for the current caller at
the authorized subscription scope. This supersedes the earlier 21:24Z Owner
verification. Do not rely on its old expiry timestamp.

The requested cleanup/read-only preparation is finished and paused. A future
live launch requires explicit user go-ahead, newly verified Owner activation
with five execution hours plus 75 minutes cleanup reserve, and fresh
source/scope/quota admission. Local checks remain passed at 9931c1d; no push.

## Latest cleanup correction and authorized recovery

Recovery completed at 2026-11-10T21:22:07Z with exit 0 and actual evidence:
member-1 connection absent 21:20:23Z, PLS absent 21:21:05Z; member-2 connection
absent 21:21:25Z, PLS absent 21:22:07Z. Full ownership snapshots and native
provider-side removal/absence receipts are in the session's
`afd-standard-live-23-pls-dependency-recovery`. Retry 4 will now perform ordinary
parent teardown with unchanged ownership/access/deadline guards. New
provisioning remains paused.

At 2026-11-10T21:13-21:14Z, independent Azure reads after retry 3 (deadline
exit 124) still found the parent and both member managed groups present, hub
absent. Remaining resources are the two failed AKS resources, parent VNet,
and each member's internal LB, PLS and PLS NIC. Activity errors still identify
PLS connections as the root deletion dependency.

The earlier claim that two connection records were removed is not supported:
the interrupted operation has an exit-zero marker but no deletion receipts,
no completed PLS snapshots, and no absence verification. Both connections are
currently Approved. Do not treat that marker as successful removal or infer
that records were recreated.

Continue the user's already authorized old-fixture cleanup only: freshly
verify original caller/Owner, ownership tags, managedBy, profile absence and
exact connection IDs/descriptions; remove provider-side connections with
the native PLS command, verify zero connections, delete only the owned PLS
resources and verify absence before ordinary guarded parent cleanup.
Foreign Microsoft-owned private endpoints are never deletion targets.
No new Azure provisioning is authorized. The session-only launch pause gate
tests and read-only preflight syntax checks pass; preflight remains gated
on complete old-fixture absence and fresh execution-budget admission.

## Requirements

- Live-test the reviewed HTTP-only implementation from committed HEAD
  `1c8fac8f6c312dce4904d5cd57cb86bb870f8ac9`.
- Exercise standard setup, the baseline-inclusive AFD E2E suite, and cleanup.
- Preserve paused HTTPS changes, source provenance and isolated credentials.
- Verify refreshed Owner access and quota before creating resources.

## Additional comments from user

- "start the live test, i refreshed owner creds"
- Earlier request: commit all outstanding changes before the live test.

## Plan

Execution authorized by the user's explicit start request. Reuse the previously
authorized subscription, Sweden Central topology and guarded live runner.

### Phase 1: Preflight

- [x] Task 1.1: Verify active Owner/effective permissions, five execution hours
  plus 75 minutes of cleanup reserve, 12 available regional/DSv5 vCPUs and absent
  fresh fixture groups. Success: fail closed before writes if any check fails.
- [x] Task 1.2: Prepare an isolated exact-commit checkout, kubeconfig, Docker
  configuration and verified existing tools. Run local script/tool regressions.
  Success: no source modification or contamination of default kubeconfig.

### Phase 2: Execute and collect

- [ ] Task 2.1: Run make e2e-setup, the CI-equivalent five-minute role propagation
  window, then make e2e-tests with AFD enabled, ATM disabled and retention false.
  Exercise the standard default for exact test-only Private Link approvals.
  Success: verify image provenance and actual test results, not just launch.
- [ ] Task 2.2: Capture setup/test output, Gateway status and controller logs.
  Success: distinguish configuration, approval, traffic, probes and withdrawal.

### Phase 3: Cleanup and report

- [ ] Task 3.1: Run standard cleanup on completion, failure or access cutoff,
  constrained by exact run tags and cluster-managed group ownership.
  Success: independently observe the parent and three managed groups absent.
- [ ] Task 3.2: Record definitive results and any failure stage without claiming
  partial evidence as full proof. Success: tests and cleanup outcomes explicit.

### Completion checklist and success criteria

- [x] Phase 1 / Tasks 1.1-1.2: preflight and isolated preparation verified.
- [ ] Phase 2 / Tasks 2.1-2.2: actual E2E results and evidence captured.
- [ ] Phase 3 / Tasks 3.1-3.2: cleanup absence and final outcome recorded.

## Decisions

- Fresh disposable parent: `fleet-afd-e2e-20261109-2128`; never reuse unrelated
  resource groups. Use the existing two-node Standard_D2s_v5 override for each
  of hub/member-1/member-2, matching the verified 12-vCPU quota admission.
- Standard shared-VNet topology and HTTP-only candidate; no HTTPS changes.
- Retain the existing access cutoff and automatic ownership-checked cleanup.
- Reuse the tested Windows Azure CLI wrapper with explicit run-owned kubeconfig
  destination, scoped subscription and exact tagged group-deletion guards.
- Use standard make entry points rather than a focused-only test label filter.
- Do not manually enable AFD resources or bypass lifecycle/readiness assertions.

## Implementation Details

Session runner: `files/afd-standard-live-18` in this Copilot session.
The isolated environment pins the candidate SHA and records its tree and image
digest. Existing access/launch/cleanup guards are reused; work.sh invokes the
standard make setup/tests flow. No product implementation changes are planned.

## Changes Made

Created the run plan and session-only environment/work scripts. Read-only
discovery confirmed the prior authorized subscription and working build tools.
Owner/effective wildcard permissions were verified at 2026-11-09T21:30:51Z.
Owner expires 2026-11-10T04:47:14Z; execution cutoff is 03:32:14Z, retaining
75 minutes for cleanup. DSv5 usage/limit was 0/100; regional cores 120/1230;
12 new vCPUs admitted. All four fresh fixture groups were absent.

Prepared the exact committed candidate in the isolated checkout. Existing yq
and buildx hashes were verified. Local script/tool tests passed (30.196s).
The reused Azure wrapper's explicit kubeconfig destination and rejection of
conflicting destinations were regression-tested without real Azure requests.
Launch will recheck access/quota/absence before any deployment writes.

The attached runner `afd-http-live-18` launched successfully. It repeated Owner,
quota and fixture-absence admission at 2026-11-09T21:35:50Z, then entered
`make e2e-setup`. Bootstrap script/tool tests passed (26.043s). Standard config
derived exact test-only approval=true without an extra approval environment
input. The fresh tagged parent group was created and independently read back
as Succeeded. Setup remains in progress; E2E results and cleanup are pending.

Run 18 subsequently failed before any AKS cluster creation: the subscription
now denies clusters unless nodeOSUpgradeChannel is NodeImage. All five candidate
images built/pushed, but E2E did not start. Run exit=2, cleanup exit=0; the
parent and three managed group names were independently observed absent at
2026-11-09T21:52:36Z by the cleanup guard.

Retry preparation uses a separate `afd-standard-live-19` session directory and
fresh parent `fleet-afd-e2e-20261109-2202`, preserving all run-18 evidence.
Candidate source remains exactly `1c8fac8`. The existing isolated Azure CLI
wrapper now supplies --node-os-upgrade-channel NodeImage for AKS creation and
rejects incompatible overrides. This complies with policy rather than changing,
exempting or bypassing it. Azure CLI help confirms the supported option.
Local fake-CLI regressions cover the policy argument without duplicate flags,
quota-admitted VM size and rejection of an incompatible channel.
No product or repository setup-script changes are required for this
subscription-specific runner adaptation.

Run-19 admission passed at 2026-11-09T22:04:43Z with the same Owner expiry and
cleanup reserve, unchanged available quotas and all fresh retry groups absent.
The exact-commit checkout and local script/tool tests passed (24.008s).
Fake-CLI regressions passed for run-owned kubeconfig isolation, NodeImage policy
compliance, admitted VM size and rejection of incompatible overrides.
The retry is ready to launch; no Azure writes occurred during preparation.

Run 19 launched as attached shell `afd-http-live-19`. Fresh launch admission
passed at 2026-11-09T22:10:40Z, followed by bootstrap local tests (25.954s).
The fresh tagged parent was created and independently read back as Succeeded.
Standard configuration again derived test-only auto-approval=true. Setup is
running; no AKS completion, traffic, E2E pass or cleanup outcome is claimed yet.

Run 19 completed unsuccessfully (run/launch exit=2). All three AKS clusters
provisioned successfully, and baseline hub/member Helm fixtures installed.
Setup failed applying the HTTPRoute CRD with
`Unable to connect to the server: http2: client connection lost`.
E2E never started: no test.exit or traffic/approval/probe result exists.

The original automatic cleanup failed (exit=1) because Windows Azure CLI could
not resolve login.microsoftonline.com during token acquisition. The runner
finished at 2026-11-10T00:38:49Z without confirmed resource absence.
At the subsequent progress check, DNS/Azure connectivity recovered and a fresh
ARM read confirmed the parent still exists. Recovery prioritizes rerunning the
same ownership-checked cleanup; original failure logs remain intact, with new
output in logs/cleanup-recovery.log and cleanup-recovery.exit.
No new full live run is launched: the remaining admitted execution window is
now shorter than the runner's required five hours.

Cleanup recovery completed successfully (exit=0). The parent and all three
managed group names were independently verified absent at
2026-11-10T00:57:50Z. The original failed cleanup log is preserved.

The user subsequently authorized another HTTP live test from the beginning.
Fresh run 20 will use parent `fleet-afd-e2e-20261110-0102` and the same exact
HTTP candidate, NodeImage-compliant runner, default test-only PLS approval and
standard baseline-inclusive setup/tests/cleanup. It must pass a new Owner
lifetime/quota/fixture-absence admission before any writes; prior access
verification is not reused as authorization for a full new run.

Run-20 preflight refused before Azure writes: the current Owner activation lacks
the required five execution hours plus 75-minute cleanup reserve. The previous
lease expires at 2026-11-10T04:47:14Z. The user must refresh Owner before fresh
admission; no authorization/time guard was reduced.

The user confirmed Owner refreshed and authorized starting the fresh HTTP run.
New admission passed at 2026-11-10T01:14:36Z: access expires at 09:12:18Z,
execution cutoff is 07:57:18Z, DSv5 usage/limit is 0/100, regional cores are
120/1230, and all four fresh run-20 groups are absent. Exact-source/tool
preparation is underway before launch.

Run-20 preparation passed: exact committed checkout, verified local tools,
script tests (16.354s), kubeconfig isolation and NodeImage guard regressions.
Attached runner `afd-http-live-20` repeated admission at
2026-11-10T01:17:06Z and started standard setup from the beginning. Bootstrap
script/tool tests passed (15.863s); the fresh tagged parent and ACR were created
with observed Succeeded provisioning. Standard AFD opt-in derived scoped
test-only auto-approval=true. Image publication/setup is in progress; E2E
and cleanup results remain pending.

Run 20 failed at hub AKS creation with AuthorizationFailed for
Microsoft.ContainerService/managedClusters/write, despite successful refreshed
Owner/effective-permission admission. Candidate images built/pushed, but no
AKS cluster or E2E execution is claimed. Run/launch exit=2; cleanup exit=0;
all four group names were independently observed absent at
2026-11-10T01:23:00Z. This is a new authorization failure, not a PLS approval or
controller reconciliation failure.
Further deployment is paused while read-only caller/PIM/effective-permission
and failed-operation evidence is collected, without overwriting admission logs
or changing role assignments, policies, credentials or application code.

Read-only diagnostics at 2026-11-10T01:26:55Z confirmed the caller is unchanged,
the Owner activation remains current until 09:12:18Z, and subscription permission
enumeration still returns unrestricted wildcard actions. The bounded activity
log query returned no matching failed operations; that absence does not refute
the recorded CLI failure. The reason for the AKS write denial is unresolved;
do not label it a proven controller defect, expired Owner lease or fixed issue.
No further cloud writes or automatic full-run retries were performed.

The user explicitly authorized a further HTTP live test from the beginning
after the run-20 authorization result. Fresh run 21 uses
`fleet-afd-e2e-20261110-0128`, isolated credentials/configuration and unchanged
candidate `1c8fac8`. Previous groups remain recorded absent. The NodeImage
subscription adaptation, exact test-only PLS approval default, complete standard
suite and ownership-checked cleanup remain unchanged. Admission is repeated;
Owner visibility alone is not represented as proof that AKS writes will succeed.
No role/policy changes or authorization bypass are part of this retry.

Run-21 preflight passed at 2026-11-10T01:29:28Z with Owner expiry 09:12:18Z,
cutoff 07:57:18Z, sufficient unchanged quota and absent fresh groups.
Exact-source/tool preparation and local script tests passed (14.230s),
alongside the kubeconfig-isolation and NodeImage guard regressions.
Attached runner `afd-http-live-21` rechecked admission at 01:31:29Z and entered
standard setup. Bootstrap tests passed (15.377s); the fresh tagged parent and
ACR are observed Succeeded. Image publication/setup is running with standard
default scoped PLS auto-approval=true. AKS write success, E2E and cleanup
outcomes remain pending.

Run-21 progress check at approximately 2026-11-10T02:04Z: standard setup has
completed and the full E2E suite is running (44 of 44 specs selected).
Baseline service export/import scenarios have completed successfully; no
explicit Ginkgo failure markers were found in the current E2E log.
The AFD scenario has not appeared in the inspected progress yet. Final suite,
AFD traffic/approval, and cleanup outcomes remain pending. Successful setup
on this attempt does not establish the root cause of run-20 authorization denial.

Run-21 progress check at approximately 2026-11-10T02:15Z: the AFD scenario is
running. Both exact member PLS requests were discovered at 02:13:35Z and
automatically approved by the test caller. Independent approval verification
passed at 02:13:38Z (approval phase 2.927s). The suite is now waiting for
post-approval AFD graph convergence, with a 02:58:38Z phase deadline.
No final test or cleanup exit markers exist yet; successful HTTP traffic and
withdrawal have not yet been established.

Run-21 progress check at approximately 2026-11-10T02:23Z: post-approval AFD
convergence passed at 02:17:47Z and the complete owned enabled private graph
was verified. The active step now requires fresh application responses from
both member clusters with the Host header preserved. This traffic step has
not yet reported success or failure; withdrawal and cleanup remain pending.

Run-21 progress check at approximately 2026-11-10T02:29Z: the traffic step is
still waiting, with no newer E2E log entry since 02:17:47Z and no exit markers.
A read-only query of the isolated hub confirms current-generation
Accepted=True and Programmed=True, with the expected AFD hostname published.
A separate direct HTTP probe returned 404, not successful application delivery.
The existing traffic timeout is 45 minutes (approximately 03:02:47Z deadline);
the cause of the 404 is not established by this probe. Withdrawal and cleanup
remain pending.

Run-21 progress at 2026-11-10T02:35Z: fresh application responses passed for
member-1 at 02:34:37Z and member-2 at 02:35:17Z, including per-request proof
and preserved AFD Host. Probe Host observations passed for both members.
A separate direct HTTP probe returned 200. At 02:35:21Z the test advanced
to route withdrawal and actual endpoint-disablement validation. Final
withdrawal, suite and cleanup results remain pending.

Run-21 progress check at approximately 2026-11-10T02:41Z: withdrawal remains
active. A read-only ARM endpoint query reports enabledState=Disabled and
provisioningState=Succeeded (deploymentStatus=NotStarted), but a separate
direct edge probe still returns HTTP 200. Control-plane disablement alone
therefore does not establish traffic withdrawal; the sustained blocked-response
check has not passed. No final test or cleanup markers exist yet.

Run-21 progress check at approximately 2026-11-10T02:49Z: an independent
edge probe now returns HTTP 404 rather than 200. This is a single observation,
not proof that the test's sustained blocked-response interval has passed.
The E2E log still ends at withdrawal start; final suite and cleanup markers
remain absent.

Run-21 progress at 2026-11-10T02:52Z: withdrawal passed at 02:49:26Z,
confirming a Disabled endpoint, absent owned route/origins, and sustained
negative edge responses. The active step now requires controller-owned
profile cleanup and persisted Gateway removal. Final suite outcome and
standard infrastructure cleanup remain pending.

Run-21 progress check at approximately 2026-11-10T02:58Z: ARM reports the
owned AFD profile resourceState=Deleting. The Gateway has deletionTimestamp
02:49:28Z and retains the expected afd-cleanup finalizer while Azure deletion
is pending. No final suite or infrastructure-cleanup exit markers exist yet.

## Approved history folding

The user explicitly requested folding all committed review fixes after
`a0c29a828c3699ae957c7c22b2243dc3a5e1f8e4` into the original four HTTP commits.
Plan: preserve the old head in a recovery branch, reconstruct the four commits
by their existing docs/runtime/packaging/test ownership, and require byte-exact
final Git tree equality before atomically updating the current branch.
The untracked live evidence stays untracked; the isolated run stays pinned to
its original commit. No source changes, HTTPS restoration or push is planned.

- [x] Preserve the old committed head and reconstruct the four commits.
- [x] Verify exact tree equality, four-commit ancestry and clean tracked state.
- [x] Record the replacement commit IDs and recovery reference.

Replacement commits: `86d201b` docs, `ca18671` runtime, `1da08ee` packaging,
and `21ea0a4` tests. Recovery branch:
`copilot/http-before-review-fold-20261110` at `1c8fac8`.
The final tree is identical (`5e99547f4d3e50d1a430ea12068657693a001b13`);
`git diff --exit-code copilot/http-before-review-fold-20261110 HEAD`
and `git diff --check 5945d92 HEAD` passed. Exactly four commits remain
above the unchanged base, all required trailers are present, and tracked
worktree/index state is clean. No runtime tests were repeated for this
history-only rewrite. The live breadcrumb remains the only untracked file.
Run 21 remains isolated on its original commit and is still in controller
cleanup; a fresh full run must wait for verified infrastructure cleanup and
fresh authorization/quota admission.

Run-21 progress check at approximately 2026-11-10T03:10Z: the owned Azure
profile still reports resourceState=Deleting; the Gateway cleanup finalizer
remains present. The suite has not advanced beyond controller-owned cleanup
and no test/run/cleanup exit markers exist. Earlier approval, delivery,
probe and sustained withdrawal checks remain successful.

Run-21 result at approximately 2026-11-10T03:23Z: the full E2E command
passed (4748.459s), with Ginkgo reporting 25 passed, zero failed, and
19 skipped under the selected feature flags. The AFD live scenario passed,
including controller-owned profile cleanup and persisted Gateway removal.
Independent ARM lookup reports ResourceNotFound for the exact profile;
the exact Gateway query returns no resource. Both test.exit and run.exit
are zero. Infrastructure cleanup has not yet produced a final exit/absence
marker, so complete environment teardown remains pending.

## Fresh local validation after folding

The user requested fresh local validation of the latest commits before any
further full live run. Validate exact head `21ea0a4` in a clean isolated
checkout, reusing installed tools and envtest assets. This avoids Windows/WSL
line-ending interpretation differences in the main worktree without changing
tracked files. Run Go race tests for API, commands, packages, API integration,
script/Helm contracts and E2E helpers (explicitly skip live TestE2E), then
go vet, configured golangci-lint, feature goimports and golint checks.
No new Azure deployment or dependency changes are part of this validation.

- [x] Fresh Go race/envtest and local contract/helper tests pass.
- [x] Configured static checks and feature formatting/lint pass; tracked state
  remains unchanged. Broader legacy golint warnings are documented below.

Fresh local validation on exact folded head `21ea0a4`:

- `go test -count=1 -race -p 2 -tags=helm -skip '^TestE2E$' ./api/... ./cmd/... ./pkg/... ./test/apis/... ./test/common/... ./test/scripts/... ./test/helm/... ./test/e2e/...`
  passed. Gateway envtest/race: 234.744s; provider: 46.684s; scripts: 10.060s;
  Helm: 22.894s. This invocation does not enable build-tagged E2E helpers.
- `go test -count=1 -race -tags=e2e -skip '^TestE2E$' ./test/e2e/...`
  passed in 1.668s without live TestE2E.
- `go vet -tags=helm` over the same package selectors passed;
  `go vet -tags=e2e ./test/e2e/...` also passed.
- `golangci-lint-v1.64.7 run --timeout=10m` passed with no diagnostics.
- Feature Go files passed `goimports -local go.goms.io/fleet-networking -l`
  and per-directory, feature-file `golint -set_exit_status` checks.
- The initial directory-wide legacy golint check failed with six missing-doc
  suggestions in `api/v1alpha1/trafficmanagerbackend_types.go` and
  `api/v1alpha1/trafficmanagerprofile_types.go`. Both files are unchanged from
  base `5945d92` (verified by exact Git diff). No unrelated fixes were made;
  the initial validation wrapper's exit=1 is retained as evidence, not hidden.
- `git diff --check` and isolated tracked-state checks passed. The main
  worktree remains unchanged except for this untracked breadcrumb.

Operational course corrections: normalize newly written Bash artifacts to LF;
use file-based scripts to avoid PowerShell stripping inline Bash variables.
These changes affect only session artifacts, not the tested product tree.

## Final run-21 teardown

The runner completed successfully at 2026-11-10T03:53:44Z.
`test.exit`, `run.exit`, `cleanup.exit`, and `launch.exit` are all zero.
Its absence marker confirms the parent and three AKS-managed resource groups
are gone. A separate post-completion query independently returned false for
all four exact group names. Thus standard setup, baseline plus AFD proof,
controller-owned cleanup, and infrastructure teardown all succeeded.
The live candidate `1c8fac8` and folded local head `21ea0a4` have identical
committed trees; fresh local validation above directly exercised the latter.

## Run 22: amended-head confirmation

The user refreshed Owner and explicitly requested another full live run at
2026-11-10T04:15:48Z. Candidate: folded HTTP head `21ea0a4`; fresh parent:
`fleet-afd-e2e-20261110-0416`. Run 21 is complete and its four groups are absent.
Reuse its validated guarded setup/tests/cleanup runner, including isolated
kubeconfig, NodeImage policy compliance, scoped default test-only PLS approval,
five execution hours plus 75-minute cleanup reserve, and exact ownership checks.
Do not reuse resource groups, restore HTTPS, change product code or push.

- [x] Recheck refreshed Owner lifetime, permissions, quota and fresh absence.
- [x] Prepare exact amended source and rerun local tool/wrapper checks.
- [ ] Run standard setup and baseline plus AFD E2E from scratch.
- [ ] Verify controller cleanup and independent infrastructure absence.

Preflight passed at 2026-11-10T04:17:08Z. The refreshed Owner lease expires
12:14:14Z and execution cutoff is 10:59:14Z, preserving the cleanup reserve.
DSv5 usage/limit is 0/100; regional cores 120/1230, sufficient for 12 vCPUs.
All four fresh resource-group names are absent. Exact-source preparation,
tool tests (16.377s), isolated kubeconfig and NodeImage guard tests passed.
No Azure provisioning has occurred during these admission/preparation steps.

Runner `afd-http-live-22` rechecked admission at 04:19:42Z and entered
standard setup. Bootstrap script/tool tests passed (15.120s). The fresh tagged
parent and ACR are observed Succeeded; setup is active. The standard config
derived PLS auto-approval=true without an extra approval-variable export.
E2E, traffic, controller cleanup and infrastructure cleanup results are pending.

Run 22 failed during member-1 AKS creation with
`management.azure.com` DNS resolution failure. E2E never started. Original
run/launch exits are 2 and cleanup exit is 1; retain these markers and logs.
Read-only recovery checks confirm DNS/API access works again; the parent,
hub-managed and member-1-managed groups remain, while member-2 is absent.

The user explicitly requested cleanup and restart without another Owner
refresh and approved the retry plan in the session `plan.md`. Recover using
the unchanged ownership-checked cleanup script under its existing bound,
with separate recovery logs/markers. New run 23 must wait for verified old
fixture absence, then repeat current-lease/quota/source admission. No code
fix, DNS workaround, role/policy change or weakened guard is authorized.

Run-22 recovery completed successfully: separate recovery exit zero and
independent Azure queries confirmed parent and all three managed groups absent
at approximately 2026-11-10T04:58Z. Original failure evidence is unchanged.
Preparing run 23 at exact `21ea0a4` with fresh parent
`fleet-afd-e2e-20261110-0457`; repeat current-lease and quota admission before
provisioning. No new Owner refresh has been requested.

Run-23 admission passed at 04:59:18Z on the existing Owner lease ending
12:14:14Z; execution cutoff remains 10:59:14Z. Regional quota and all four
fresh names passed. Exact-source/tool preparation and script tests passed
(19.210s), as did explicit Windows kubeconfig isolation and NodeImage policy
regressions. Starting the approved standard setup/tests/guarded-cleanup path.

Run 23 launched at 05:02:48Z; exact-source execution began at 05:02:54Z.
Bootstrap tool tests passed (19.549s), and the fresh tagged parent group
is observed Succeeded. Registry creation is in progress. E2E and all final
cleanup outcomes remain pending; no fresh Owner activation was required.

Run-23 progress at approximately 05:18Z: hub, member-1 and member-2 all
report Succeeded. Setup remains active and is publishing the application
fixture image. E2E has not started; no run/test failure markers are present.

Run-23 setup completed at approximately 05:24Z: application image publication,
both member controller upgrades and hub Gateway chart deployment succeeded.
The runner is in its existing five-minute role-propagation wait before the
standard full E2E command. No failure/exit markers are present.

## Run 23 overnight interruption and cleanup blocker

Morning inspection at approximately 2026-11-10T14:38Z found the runner still
active beyond its recorded 10:59:14Z execution cutoff and 12:14:14Z Owner
expiry. The E2E log jumps from member-1 delivery at 06:07:56Z to member-2
delivery at 14:37:21Z. Linux process elapsed time was much shorter than the
wall-clock interval; host suspension/time discontinuity is suspected but not
proven. The relative timeout did not enforce the recorded wall-clock cutoff.

PLS auto-approval and graph convergence passed; both members' fresh delivery
and probe checks were observed. The active step was withdrawal. Neither
sustained withdrawal nor profile/Gateway cleanup nor a final suite pass was
established. Do not treat delivery after the gap as a full uninterrupted run.

A fresh read at 14:43:53Z verified zero active Owner activations for the admitted
caller and scope. The run was stopped; a subsequent process check showed no
run-23/test processes remaining. Its cleanup attempt failed with
`Authorized Azure access window expired`. The recorded run.exit=0 is not a
test pass: test.exit and final suite result are absent, and the launcher was
interrupted. Preserve all original markers, including cleanup.exit=1.

A read-only Azure query confirms the exact parent group still exists in
Succeeded state. Cleanup is outstanding and requires freshly verified Owner
access. Do not extend the access-window file without validating a real lease,
start another test, bypass expiry checks or remove Gateway finalizers.

The user refreshed Owner and authorized the previously requested cleanup only.
At 14:54:55Z the original caller, target subscription, effective wildcard
permission and exactly one current activated Owner lease were verified.
The new lease expires 22:47:34Z, satisfying the existing cleanup reserve.
Recovery uses separate `afd-standard-live-23-cleanup-recovery` session artifacts,
the unchanged ownership-checked cleanup and Azure CLI wrapper, and a fresh
cleanup-only access window. The original run's expiry/markers/logs are untouched.
No new test or product-code changes are being made.

## Rebase onto current main

The user requested fetching origin/main and rebasing to resolve conflicts.
Fetched main is `22d0969`, incorporating the Fleet v0.18.16 / Kubernetes
v0.35.8 / controller-runtime v0.23.3 / Azure network SDK v9 upgrade, x/net
v0.62.0, and updated repository instructions. Preserve old HTTP head
`21ea0a4` at `copilot/http-before-main-rebase-20261110-1623`.

The rebase stopped in the runtime commit with four conflicts: member manager,
ServiceExport controller, go.mod and go.sum. Resolve by retaining upstream
versions and event-recorder changes while preserving all private-origin
discovery wiring and AFD dependencies. Adapt directly affected feature code
and tests to the newer SDK/runtime APIs, regenerate dependency sums via
go mod tidy, and keep the four-commit organization. Validate affected Go,
chart/script and non-live helper contracts; no push or live provisioning.

- [x] Resolve runtime/dependency conflicts without downgrading main.
- [x] Finish the four-commit rebase and retain HTTP source/review fixes.
- [x] Pass affected tests, formatting and configured static checks.

Separately, run-23 cleanup recovery exited 1 because absence was not confirmed
before its bound. The parent still exists; teardown remains outstanding.

The four commits replayed successfully. Initial affected-package validation
passed both managers, member ServiceExport, GatewayClass/model/resolver,
HTTPRoute and the AFD provider, but failed the Gateway integration suite's old
Event.Count assertion. The upgraded Kubernetes recorder emits events.k8s.io
Event series: legacy Count is not its occurrence counter, and repeated
emissions are coalesced with delayed persistence. Its aggregation key also
excludes the note. Do not weaken the lifecycle/emission assertions or force
the production broadcaster to flush solely for tests. The integration recorder
now delegates to the real manager recorder and separately counts exact
UID/type/reason/note emissions under a mutex; existing native Event persistence
assertions remain in place.

The Gateway chart now permits events.k8s.io event writes, with exact Helm RBAC
expectations updated. Four newly replayed E2E files now import Azure network
SDK v9, matching main and member discovery without reintroducing v4.
Fleet bootstrap retained main's v0.18.16 CRD path. These compatibility changes
will be folded into their runtime, packaging and test commits before full
local validation on the exact candidate.

The user requests all local tests first and then another fresh live E2E after
the rebase. No push is requested. Fresh read-only checks confirm run 23 still
has its parent and both member-managed groups; the hub-managed group is absent.
The next live run must wait for verified teardown, passing local checks and
fresh access/budget admission. Host-suspension cutoff enforcement remains an
unresolved runner-safety concern.

The rebased candidate is now `eb1844343d784d76662885cad6fd2ee8555e9a9d`,
tree `e24f697a97f4c38ae37fb32537b33e6de9d5a107`. Exactly four HTTP commits
remain above main: docs `9b9bb24`, runtime `5fb809c`, packaging `837e176`,
tests `eb18443`. Main ancestry, trailers and unchanged HTTPS recovery refs
were checked. Full local race/envtest, tagged Helm/E2E helpers and static
validation are running in a clean isolated checkout; these results are pending.

Scoped Azure activity logs explain the parent rollback: failed AKS deletions,
an in-use VNet subnet and an AFD profile deletion timeout. The profile is now
absent, but the two remaining AKS clusters report Failed. Retry 2 reuses
unchanged ownership-checked cleanup scripts and separately preserved artifacts,
after fresh caller/permission/Owner verification at 2026-11-10T17:06:00Z.
Current Owner expiry remains 22:47:34Z: sufficient for cleanup, but insufficient
for fresh-run admission's unchanged five execution hours plus 75-minute
reserve. A new live run requires refreshed access as well as local-test and
cleanup success.

At 2026-11-10T17:24:44Z, a new read-only check independently verified the same
caller, effective wildcard permission and exactly one current activated Owner
lease. Renewed expiry is 2026-11-11T01:18:36Z, with execution cutoff
2026-11-11T00:03:36Z. The five-hour execution plus 75-minute cleanup admission
budget is now sufficient. Verification artifacts are separate under
`files/http-access-check-20261110-*` (directory name is generated at launch).

All local race/envtest suites, Helm/script tests, tagged non-live E2E helpers
and vet passed on `eb18443`. Configured lint found one deprecated Requeue field
selector in the feature test. Its assertion now compares the complete expected
result, retaining the upstream controller's unchanged immediate-requeue
behavior and strengthening the expected result check. Revalidate that test and
finish static/format checks before reporting a clean candidate.

All local checks are complete. Final head is `9931c1d`, tree
`174c51619534ac0c27a692563a134f3142b37936`, with four commits on main.
The full race/envtest/tagged Helm/E2E-helper suites passed on `eb18443`; the
final tree changes only the single expected-result test assertion. Its focused
race regression and all final static/format checks passed. The first combined
golint invocation returned zero but reported mixed packages and was not valid
evidence; corrected individual-file validation passed for all 111 feature Go
files and is recorded separately. No push occurred.

Run-24 session-only preparation targets fresh fixture
`fleet-afd-e2e-20261110-1745` on `9931c1d`. Its launcher now cancels owned
process groups on absolute UTC deadlines, refuses zero-exit work without a
successful test marker, and is paired with scoped Windows automatic-sleep
inhibition. Five deadline/signal/error tests, four actual-launcher phase
scenarios and a scoped Windows power-request test passed. These guards do not
guarantee teardown across manual suspension or power loss; no global power
settings or repository scripts were modified.

The managed-group activity log exposed the old cleanup blocker:
`PrivateLinkServiceWithPrivateEndpointConnectionsCannotBeDeleted`. Each
member's `my-ns-dzkwp` PLS retains one Approved connection explicitly described
as belonging to deleted profile `fleet-afd-ac9142ccf210a5a77b44a4ac97137765`.
Within the already authorized retired-fixture cleanup, verify fresh Owner and
exact parent tags / managedBy / profile absence / PLS-child-ID and request
description fences, then remove only those two owned connection records.
Do not delete or mutate the actual AFD-managed private endpoints in the
Microsoft-owned subscription. Keep the original failed/retry evidence intact.

The early owned-connection attempt recorded exit 0 at
2026-11-10T17:58:34Z, but later inspection found no deletion receipts or
absence proof. It did not establish removal. The verified replacement
deletions are recorded above at 21:20-21:22Z. No external private endpoint
was targeted.
Retry-2 logs then jumped from 17:59Z to 19:26Z; its relative timeout again did
not bound wall time across that gap. It finished without verified absence.
Fresh reads confirmed parent Succeeded, both member groups present, hub absent.
Retry 3 will revalidate access and ownership before repeating ordinary parent
cleanup, now with an absolute clock guard and scoped Windows sleep inhibition.

The user clarified: "Finish cleanup and preflight, then pause before any new
Azure provisioning". No run-24 launch is authorized until explicit go-ahead.
Offline run-24 preparation passed (script/tool contracts 26.775s); no fresh
Azure fixture was created. Progress schedule #2 updates every 15 minutes
through this preparation/pause point. The current Owner lease may require
refresh before a future launch because the earlier wall-time gap consumed the
admission budget; never lower that guard to finish preflight.

## Before/After Comparison

Before: local review fixes and automated tests passed; no new live proof.
After: preflight/preparation passed and standard setup is running on Azure.
A successful launch alone is not live E2E success.

## References

- `2026-11-09-1854-http-chart-and-logs-review.md`: reviewed HTTP changes and tests.
- `2026-11-09-1839-http-only-hardening.md`: HTTPS preservation and HTTP baseline.
- `test/README.md`, `test/scripts/{bootstrap,run-e2e,cleanup,e2e-config}.sh`:
  standard live workflow.
- `.github/workflows/e2e-tests.yml`: shared-VNet AFD role propagation window.
- Prior `afd-standard-live-17` session runner: existing scoped guards, tested
  kubeconfig isolation and candidate-provenance checks; no credentials copied.
