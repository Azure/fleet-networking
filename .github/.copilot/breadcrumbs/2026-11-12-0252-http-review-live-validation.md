# HTTP reviewer-fix live validation

## Requirements and authorization

- User: "start live testing now using previous values", followed by "try again".
- Validate committed candidate `07db9bf6ad213bf23a6030a02e8bc07a09e8715e`.
- Reuse the previously authorized Sweden Central shared-VNet test topology:
  hub, member-1 and member-2, two Standard_D2s_v5 nodes each.
- AFD enabled, Traffic Manager disabled, standard test-only correlated Private
  Link approval enabled, retention disabled, automatic guarded cleanup.
- Fresh disposable parent: `fleet-afd-e2e-20261112-0252`; never reuse old
  resources or change unrelated clusters. No Git push.

## Plan and success criteria

1. [x] Verify current caller, effective Owner, five execution hours plus
   75-minute cleanup reserve, 12 available vCPUs, prior-group absence and
   fresh-group absence. Fail closed before writes.
2. [x] Prepare an isolated exact-commit checkout, kubeconfig, Docker
   configuration and verified existing tools. Pass offline harness checks.
3. [ ] Run standard setup, five-minute role propagation wait and full baseline
   plus AFD HTTP E2E. Record actual test exit and installed candidate digest.
4. [x] Capture logs and Gateway status before guarded cleanup on success,
   failure or deadline. Independently verify the exact parent and three
   AKS-managed resource groups absent.
5. [x] Report definitive test and teardown outcomes, including skipped
   scenarios and incomplete evidence.

## Decisions and implementation

- Reuse the previous run's ownership-checked Azure wrapper, absolute-deadline
  monitor and cleanup scripts in session-only run26 artifacts.
- New source is isolated from shared workspace and default kubeconfig.
- Use the existing default Docker builder, avoiding global builder/QEMU setup.
- Owner expiry observed at `2026-11-12T10:44:39.197Z`; execution cutoff
  `2026-11-12T09:29:39.197Z`. Launch repeats admission before writes.
- Early retries returned Unauthorized. Access is restored; complete admission
  and isolated preparation are in progress. No live pass is implied.

## Validation and references

- Full local tests, builds, lint, offline chart/script/E2E checks, Python tests,
  Markdown links and native amd64 image smoke/registry checks passed before
  live authorization; source remained unchanged.
- Session artifacts: `files/afd-standard-live-26` in this Copilot session.
- Previous runner and topology:
  [HTTP live review validation](2026-11-09-2128-http-live-review-validation.md).
- Standard lifecycle and safety: [E2E guide](../../../test/README.md).

## Results and unresolved risks

Admission is complete; setup, full suite and cleanup are pending. Azure platform
convergence and successful teardown must be observed; a launcher or setup
success alone is not test proof.

Read-only admission passed at `2026-11-12T02:53:45Z`; all 16 previous/fresh
group names were absent. DSv5 usage/limit was 0/100, regional cores 128/1230,
versus 12 required. Isolated harness/tool compatibility tests passed in
19.228s. Source and builder identity were verified without global mutations.
Launch will repeat admission. The run-scoped Windows sleep guard is released
when the launcher exits; the absolute UTC execution cutoff remains enforced.

Launch repeated admission and started at `2026-11-12T02:59:14Z`. The tagged
parent was independently read back as Succeeded in Sweden Central. The ACR
is created and candidate image publication is underway. The launcher,
absolute-deadline monitor and work process are active; no suite result yet.
Detached shell ID: `http-live26-authorized`.

## Setup failure and cleanup recovery

The MCS controller image built, but registry publication failed with
`unexpected EOF`. Standard setup exited 2 before creating AKS clusters or
starting E2E; `test.exit` is absent. This is not a live test pass.

The launcher exited 1 after recording `run.exit=2`, without cleanup/launch
completion markers. The reason for that premature launcher exit is unverified;
do not claim that automatic cleanup ran successfully. No competing owned
runner remains.

Independent reads found the tagged parent, VNet and ACR still present, with
all three managed groups absent. A cleanup-only recovery revalidates the
original caller, current effective Owner permissions and active lease before
using the unchanged ownership-checked cleanup under an absolute deadline.
No provisioning retry or source/test change is authorized by this recovery.
Recovery shell ID: `live26-cleanup-recovery`; final absence is pending.

Cleanup recovery exited 0. Independent Windows Azure CLI reads at
`2026-11-12T03:12:41Z` through `03:12:52Z` verified the exact parent and all
three managed groups absent. Receipt:
`logs/independent-cleanup-verification.txt` in the run26 artifacts.
No cluster existed, so Gateway status and controller logs were unavailable.
The live attempt is concluded as setup failure, not test failure or success;
the complete E2E suite remains unverified for this commit. No automatic retry.
Scheduled progress updates stop after this final verified outcome.

## Authorized run27 retry

User: "do another live test?" after run26 failed during registry publication.
Retry the unchanged committed source in a fresh disposable parent
`fleet-afd-e2e-20261112-0315`, with the same topology, approval behavior and
cleanup scope. No hidden provisioning retry or product-source change.

The old Bash launcher's premature exit remains unexplained. Replace its
session-only lifecycle orchestration with an explicit Python `finally` block
that always invokes the existing ownership-checked cleanup, including after
setup failure or evidence-collection failure. Reuse the absolute UTC
deadline monitor for both work and cleanup and the scoped Windows sleep guard.
Six offline regression cases passed in 11.086s: setup failure, expired work
deadline, log collection failure, proven success, unproven success rejection
and cleanup failure propagation.

- [x] Reverify original caller/Owner, prior/fresh-group absence and quota.
- [x] Prepare exact-commit isolated checkout and pass harness/tool checks.
- [ ] Launch standard setup/full suite and capture actual results.
- [ ] Verify parent and all managed groups absent, then report.

Admission passed at `2026-11-12T03:18:08Z`. All 20 old/fresh group names
were absent; quota remains DSv5 0/100 and regional cores 128/1230, versus
12 required. Owner expiry and execution cutoff are unchanged. Artifacts:
`files/afd-standard-live-27`; preparation is still underway.

Isolated harness/tool compatibility passed (17.702s). Launch repeated
admission and started `2026-11-12T03:23:14Z`; setup's own offline harness
passed (18.132s). The parent was independently read back as Succeeded,
ACR is created and candidate images are publishing. The finally-based driver
and owned work process are active. Detached shell:
`http-live27-authorized`. Progress schedule #2 runs every 15 minutes.
No actual E2E result yet.

### Run27 final outcome

Image publication succeeded, but hub AKS creation was denied by subscription
policy `aks1p-nodepools-azurelinux-assignment`: node pools must use AzureLinux.
The existing setup defaults did not specify that OS SKU. No policy exemption,
policy change or additional provisioning retry was attempted.

The hardened runner performed automatic guarded cleanup successfully:
`run.exit=2`, `cleanup.exit=0`, `launch.exit=2`, `powershell.exit=2`.
`test.exit` is absent: E2E never started. Cleanup completed at
`2026-11-12T03:33:03Z`. Independent Windows Azure CLI reads from
`03:39:51Z` through `03:40:02Z` verified the exact parent and all three
managed groups absent. Receipt: run27
`logs/independent-cleanup-verification.txt`.

The attempt is concluded as setup failure, not a test result. Progress
schedule #2 stops. A subsequent authorized attempt must use the required
AzureLinux OS SKU without weakening subscription policy and must recheck
source, access, quota, lifetime and prior/fresh-group absence.

## Authorized run28 retry with AzureLinux

User: "retry" after run27's policy denial and independently verified teardown.
Fresh disposable parent: `fleet-afd-e2e-20261112-0340`. Candidate remains
`07db9bf`; topology, resource bounds, approvals and cleanup scope are unchanged.

The session-only Azure wrapper now supplies `--os-sku AzureLinux` for every
AKS create, deduplicates matching inputs and rejects conflicting OS values
before any cloud call. It still enforces NodeImage and Standard_D2s_v5.
No subscription policy modification or exemption is requested or performed.
Installed Azure CLI help confirms AzureLinux is supported.

- [x] Run policy-wrapper and cleanup lifecycle regressions: 10 tests passed
  in 11.360s, including explicit conflicting-option rejection.
- [x] Reverify current Owner/lifetime, quota and all prior/fresh groups absent.
- [x] Prepare exact-commit isolated checkout and pass offline harness checks.
- [x] Run standard setup/full E2E and capture actual results.
- [x] Verify parent and all managed groups absent, then report.

Artifacts: `files/afd-standard-live-28`. Initial Owner verification at
`2026-11-12T03:42:35Z` observed the same expiry/cutoff; remaining admission
and preparation are in progress. No new Azure writes yet.

Run28 admission and isolated preparation passed; all 24 old/fresh groups
were absent, DSv5 usage/limit 0/100 and regional cores 128/1230. Offline
harness/tool compatibility passed (16.360s). Launch repeated admission and
started at `2026-11-12T03:48:00Z`; setup's harness passed (17.165s).
The tagged parent was independently read back as Succeeded, ACR is created,
and registry authentication is underway. Finally-based driver and owned
work process are active. Detached shell `http-live28-authorized`, progress
schedule #3 every 15 minutes. Actual E2E and cleanup remain pending.

Run28 progress at `2026-11-12T04:05Z`: all three AKS clusters independently
report Succeeded, each with two Standard_D2s_v5 AzureLinux nodes. Image
publication completed and setup is installing hub/member Helm resources.
Owned driver/work processes remain active; all outcome markers are pending.
E2E has not started, so no test pass is claimed.

Run28 progress at `2026-11-12T04:22Z`: standard setup reached its five-minute
CI-matching role propagation wait at `04:19Z`, after deploying the hub Gateway
controller Helm release and upgrading both member controllers. `e2e.log` is
not present and all five exit markers remain pending. Full E2E and teardown
are still unverified.

Run28 progress at `2026-11-12T04:38Z`: the full 44-spec E2E suite started
around `04:24Z`. The private HTTP proof discovered both exact correlated PLS
requests, independently verified their approvals at `04:29:48Z`, and passed
post-approval owned/enabled AFD graph convergence at `04:33:30Z`. It is now
requiring fresh responses from both members with Host preservation. This is
an intermediate milestone, not a traffic or suite pass. All five exit markers
remain pending; teardown has not started.

Run28 progress at `2026-11-12T04:56Z`: fresh private HTTP responses were
observed from member-1 (`04:49:01Z`) and member-2 (`04:50:01Z`), both
preserving the AFD request Host and carrying this fixture's proof ID.
Probe Host observations were also recorded for both members. The test
advanced at `04:50:05Z` to route withdrawal and actual endpoint disablement
verification. All exit markers remain pending; no full-suite pass or
infrastructure teardown is claimed.

Run28 progress at `2026-11-12T05:11Z`: route withdrawal passed at
`05:02:28Z`, observing an actually Disabled endpoint, absent owned
route/origins, and sustained negative edge responses. The private HTTP spec
is now requiring controller-owned profile cleanup and persisted Gateway
removal. All five exit markers remain pending, and fixture teardown has
not started. No final spec or suite pass is claimed.

Run28 progress at `2026-11-12T05:26Z`: the private HTTP spec passed
(`3378.156 seconds`), including controller-owned profile cleanup and
persisted Gateway removal. The suite advanced to Gateway API contract and
baseline service export/import tests; recent baseline cases are passing.
No failure markers were found in the inspected E2E log. All five exit
markers remain pending, so full-suite completion and fixture teardown
remain unverified.

Run28 progress at `2026-11-12T05:34Z`: the standard full-suite invocation
finished successfully: `25 Passed | 0 Failed | 0 Pending | 19 Skipped`,
with Go package PASS in `3999.100s`. Both `test.exit` and `run.exit` are
0. Ownership-checked fixture cleanup has started and issued asynchronous
deletion of the exact tagged parent resource group. Cleanup, launcher and
PowerShell exit markers are pending. Independent absence verification of
the parent and all three managed groups is still required; this is not
yet a completed live-validation outcome.

## Run28 final outcome

The standard `make e2e-tests` invocation passed: 25 specs passed, zero failed,
19 skipped; Go package PASS in 3999.100s. Skips include Traffic Manager
(disabled for this approved fixture) and networking-disabled cases (networking
is enabled). The private HTTP proof passed both-member delivery with preserved
Host, exact-correlated Private Link approval, graph convergence, withdrawal,
profile cleanup and persisted Gateway removal. `test.exit=0`, `run.exit=0`.

The cleanup monitor did not complete successfully: `cleanup.exit=124`,
`launch.exit=124`, `powershell.exit=124`. Its log reports stopping the owned
cleanup process group at the deadline; the cleanup log ends at asynchronous
parent deletion. Launcher output was last written at `12:38:26Z`. The reason
for this delayed monitor completion is not established; no clean harness
success or exact infrastructure deletion time is claimed.

Independent read-only Windows Azure CLI checks at `2026-11-12T14:56:17Z`
through `14:56:32Z` returned exit 0 and `false` from `az group exists` for
the exact parent and all three AKS managed groups. The fixture, including
hub and both member clusters, is gone. Receipt:
`files/afd-standard-live-28/logs/independent-cleanup-verification.txt`.
No additional Azure writes or provisioning retries were performed.

Live validation is concluded with passing tested behavior and independently
verified teardown, but a recorded cleanup-harness timeout. Candidate remains
`07db9bf`; no product-source changes or push. Progress schedule #3 stops.
