# Existing-code private HTTP live E2E

## Requirements

- Run existing committed candidate `aaa023b0f5b3d1f63821d168ebc668bf05faea85`.
  Pending repository changes are planning documentation only; do not add HTTPS,
  native multi-backend routing, ATM enhancements or a proxy to this run.
- Use the standard Sweden Central shared-VNet fixture: one hub and two member
  AKS Standard clusters, two Standard_D2s_v5 nodes per cluster.
- Enable AFD and exact test-caller Private Link auto-approval; disable retention
  and Traffic Manager. Run the full existing E2E suite without label exclusions.
- Require installed automatic activation, exact private bindings/approvals,
  fresh both-member HTTP/Host responses, actual probes, withdrawal and cleanup.
- Verify current effective Owner, its active lifetime, quota and fresh fixture
  absence before writes. Keep 75 minutes reserved for owned environment cleanup.

## Additional comments from user

- "we will add this later after a live test with existing code"
- "owner authorization has been refreshed"
- "proceed with live test"
- "try again"
- "ok, amend the test to increase testing delay to at most 45 mins - 404s may resolve sooner, who knows!"
- "testing delay = after routes, etc enabled"
- "not the total test time"
- "add clarifying but brief comments wherever applicable and then commit/amend outstanding changes"

## Plan

### Phase 1: Admission and local preparation

- Task 1.1: Independently verify current subscription/Owner activation/effective
  permissions, active execution window, quota and resource-group absence.
  Success: fresh access and capacity support the bounded standard fixture.
- Task 1.2: Clone the exact committed candidate, verify existing local tools and
  run the repository's local script tests before any Azure setup.
  Success: clean committed source and passing local runner checks.

### Phase 2: Installed existing-code live proof

- Task 2.1: Run standard `make e2e-setup`, build/publish/install the candidate and
  record its image digest. Success: deployed image provenance is verified.
- Task 2.2: Run the full existing E2E suite with auto-approval enabled and retention
  false. Success: actual automatic activation, traffic, probes and withdrawal.

### Phase 3: Teardown and independent verification

- Task 3.1: Collect status/logs/results and invoke standard owned cleanup.
  Success: resource tags and AKS managedBy identities verified before deletion.
- Task 3.2: Independently observe the parent and all three AKS-managed groups
  absent. Success: explicit E2E and cleanup outcomes, not an assumed teardown.

### Checklist and success criteria

- [x] Task 1.1: Fresh access/quota/absence admission.
- [x] Task 1.2: Exact-source local preparation/tests.
- [ ] Task 2.1: Candidate deployed with verified provenance.
- [ ] Task 2.2: Full existing-code live E2E result.
- [x] Task 3.1: Collected evidence and ordinary owned cleanup.
- [x] Task 3.2: Independent owned resource absence.

The task is complete only after live proof and verified teardown. An enabled
configuration, manual activation or a prepared clone alone is not success.

### Phase 4: Approved post-activation traffic-window correction

- [x] Task 4.1: Give only the application-traffic assertion a dedicated maximum
  45-minute timeout after independent enabled-graph verification. Keep five-second
  polling and early success when both members satisfy the existing assertions.
  Keep discovery/probe timeouts, total spec/suite timeouts and cleanup reserves
  unchanged. Update the existing sequential-budget regression for this phase.
- [x] Task 4.2: Run offline tagged E2E helper tests, formatting, vet and lint.
  Success: the new phase budget fits the unchanged overall timeout, checks pass,
  and no new Azure run is started.
- [x] Task 4.3: Fold all outstanding changes into their appropriate signed
  documentation and E2E commits, preserving the four-commit layout and trailers.
  Success: clean worktree, valid signatures and no push.

The user explicitly approved this targeted test change. This is a polling
deadline, not a fixed sleep or an extension of the total test timeout.

Validation for the approved change:

| Command | Result |
| --- | --- |
| `hack/tools/bin/goimports -w test/e2e/gateway_private_http_test.go test/e2e/gateway_private_http_assertions_test.go` | Passed |
| `go test -count=1 -tags=e2e -run "^TestPrivateHTTP" ./test/e2e` | Passed offline helpers, including unchanged overall-timeout headroom |
| `go test -run "^$" ./test/e2e` | Passed untagged compilation; no live tests |
| `go vet -tags=e2e ./test/e2e` | Passed |
| `GOFLAGS=-tags=e2e hack/tools/bin/golint -set_exit_status ./test/e2e` | Passed |
| `hack/tools/bin/golangci-lint-v1.64.7 run --build-tags=e2e ./test/e2e` | Passed |
| `git diff --check` | Passed |

Changes are folded into the existing documentation and E2E commits with signing
and the required trailers; the four-commit layout is retained. No push or new
live run is part of this correction.

## Decisions

- Use a fresh isolated runner rather than the previous checkout with an expired
  fixed lease. Fresh authorization produces the execution deadline used by the
  wrapper and cleanup; launch repeats authorization/absence checks.
- Keep the approved existing AKS Standard topology. This is test infrastructure,
  not a redesign of production clusters. No configured Azure AKS MCP surface is
  available; the existing standard workflow uses Azure CLI.
- Preserve the existing caller-only approval, scope/tag/managedBy cleanup,
  rate-limit and lifetime guards. Do not manually activate AFD or grant the
  controller permission to approve its own connections.
- Pending native HTTPS and optional ATM documents do not change tested code.
- AFD configuration propagation can take up to 15 minutes per operation and
  approximately 30 minutes for back-to-back changes. Ten minutes of traffic
  polling cannot rule out propagation delay after automatic activation; use a
  bounded 45-minute traffic window without weakening response assertions.
  Propagation remains a hypothesis, not a confirmed cause of run 16's 404.

## Implementation Details

- Session runner: `files/afd-standard-live-15/`.
- Intended owned parent: `fleet-afd-e2e-20261101-2230`.
- Read-only admission verified at `2026-11-01T22:58:27.671525Z`.
- Current Owner expires `2026-11-02T06:26:02.223Z`; execution cutoff is
  `2026-11-02T05:11:02.223Z`, preserving 75 minutes for cleanup.
- Sweden Central DSv5 quota: usage 4, limit 100. Total vCPU quota: usage 150,
  limit 1230. The fixture requires twelve additional vCPUs.
- The fresh parent and its three exact AKS-managed group names were absent.
- Newly created Windows session scripts initially had CRLF line endings and
  failed before any Azure request; shell formatting normalized them to LF.
  Subsequent complete admission passed. No guard was weakened.
- Existing operational prepare/work/cleanup scripts and the scoped Azure wrapper
  are reused. Candidate source is not patched.
- Exact candidate clone and tool checks passed. Docker/buildx/yq were verified;
  `AFD_VALIDATE_LOCAL_TOOLS=true go test -count=1 ./test/scripts` passed in 25.594s.
  No Azure resources were created during preparation.
- Launch repeated admission at `2026-11-01T23:03:06.325167Z`, preserving the same
  expiry/cutoff. The bounded attached runner is PowerShell session `afd-live-15`.
- Launch and worker start markers are present. Standard setup passed its own
  local script tests (25.087s), invoked ordinary group creation, and an independent
  Azure read confirmed the fresh parent exists. This is an actual live setup,
  not merely a prepared clone. Deployment/traffic/cleanup results remain pending.
- Run 15 failed in `make e2e-setup` with exit 2 after successful AKS provisioning.
  Windows Azure CLI wrote `az aks get-credentials` output to its Windows default
  kubeconfig; Linux kubectl was using the intended isolated run kubeconfig and
  could not find `hub-admin`. The E2E/AFD traffic suite never started.
- Automatic standard cleanup returned 0 and recorded all four groups absent at
  `2026-11-01T23:45:26Z`. Independent Azure reads at
  `2026-11-02T00:08:32Z` again confirmed the parent and all managed groups absent.
- Prepared a separate runner-only retry under `files/afd-standard-live-16/`,
  with intended fresh parent `fleet-afd-e2e-20261102-0009`. Its Azure wrapper
  supplies an explicit run-owned Windows `--file` destination for credentials,
  rather than depending on Linux KUBECONFIG propagation into Windows Azure CLI.
  The failed run's original wrapper/evidence remains unchanged.
- A local fake-CLI regression verified the exact destination and rejection of
  conflicting destinations. Retry exact-source/tool preparation and local
  script tests passed (20.744s). Candidate controller code remains unchanged.
- Fresh retry admission then refused the insufficient remaining window at
  `2026-11-02T00:17:26Z`: Owner still expires at 06:26 UTC, but five execution
  hours plus 75 minutes cleanup reserve are no longer available. No retry Azure
  resources were created; no lifetime/cleanup guard was weakened.
- The unintended Windows default kubeconfig merge is not blindly reverted:
  pre-existing entries were not backed up, so deleting/replacing them could
  destroy unrelated configuration. Future credentials are explicitly isolated.
- Retry admission passed at `2026-11-02T00:34:49.614639Z` after refreshed Owner:
  expiry `2026-11-02T08:32:45.997Z`, execution cutoff
  `2026-11-02T07:17:45.997Z`. Quotas remain sufficient and the fresh retry
  parent/managed names are absent. Corrected run 16 is authorized to launch.
- Run 16 setup completed and all three AKS clusters are Succeeded/Running.
  The full E2E suite reached the AFD private HTTP spec, which verified installed
  controller digest `sha256:4e339a531b71034e4e915187b222189fb94f9cea216c30d84fd63ee1e453ea95`.
- At the `2026-11-02T01:35Z` progress check, AFD provisioning remains blocked by
  repeated 403 `LinkedAuthorizationFailed`: the hub runtime identity lacks
  effective `Microsoft.Network/privateLinkServices/read` on a member PLS linked
  scope. This is authorization failure, not a nullable-approval readiness gate.
  Both origin names are visible but unconfigured; exact PLS requests are absent
  and Gateway Programmed is false. The live discovery window is 01:30-02:15 UTC.
  Traffic/probe/lifecycle success is not claimed; the bounded runner remains active.
- Independent role reads confirmed Reader assignments exist on both exact PLS
  scopes for the same hub kubelet/runtime principal identified by the 403.
  Therefore the failure is effective linked authorization, not a proven absent
  assignment. Propagation may be involved, but its cause/timing is not established.
  E2E auto-approval remains enabled; it approves exact caller-correlated requests
  only after Azure actually creates them. Approval cannot repair the earlier 403.
- Final run 16 evidence supersedes the earlier progress blocker: exact request
  discovery succeeded at `2026-11-02T01:54:10Z`, and caller-side auto-approval
  independently completed for both PLS connections at `01:54:12Z`.
- Post-approval convergence succeeded at `2026-11-02T01:58:47Z`: current Gateway/
  HTTPRoute status and the independent verifier agreed on the complete owned
  enabled private graph and published hostname. No manual endpoint/route
  activation was performed. The controller's enable operations were authorized.
- The actual failed assertion was application delivery: after 600.004 seconds,
  AFD still returned HTTP 404 instead of HTTP 200 with edge evidence. This is not
  a final linked-authorization or activation-permission failure. The cause of
  the edge response remains unconfirmed; traffic/probe/withdrawal proof did not pass.
- Ginkgo result: 24 passed, 1 failed, 19 skipped; 25 of 44 specs executed.
  Test/run/launch exits are 1; standard cleanup exit is 0. Owned parent and all
  three managed groups were recorded absent at `2026-11-02T03:05:55Z` and
  independently reconfirmed absent at `2026-11-02T03:48:31Z`.

## Changes Made

- Created isolated session-only runner/environment/preflight/launch artifacts.
- Created this live-run breadcrumb. No production code or new feature changes.
- Fresh read-only authorization/quota/absence admission passed.
- Exact-source preparation and existing local script tests passed; the bounded
  launch is ready to repeat admission before standard setup.
- Started standard existing-code live setup and independently verified fresh
  Azure resource-group creation.
- Recorded the infrastructure failure, successful cleanup and tested runner-only
  kubeconfig fix. Retry is prepared but blocked by the remaining access budget.
- Reverified a fresh sufficiently long Owner window and authorized corrected
  existing-code retry 16; no controller code changes.
- Recorded final successful caller approval and automatic enabled-graph proof,
  failed HTTP 404 delivery assertion, full-suite result and verified teardown.
- Added a dedicated 45-minute post-enabled-graph application-traffic timeout and
  a brief edge-propagation comment. Existing setup/probe timeouts, five-second
  HTTP polling, strict assertions, overall deadlines and cleanup are unchanged.
  Updated the existing sequential-budget regression to account for this window.

## Before/After Comparison

- Before: the previous live attempt was blocked by expired Owner access and
  never created Azure resources.
- Current: both runs are cleaned up. Run 16 proved caller auto-approval and
  automatic configured activation, but failed actual HTTP delivery with 404.
  Full private traffic/probe/withdrawal success remains unproven.

### Retry 16 checklist

- [x] Current Owner/quota/fresh-absence admission.
- [x] Exact-source preparation and kubeconfig isolation regression.
- [x] Corrected standard setup and deployed image provenance.
- [ ] Full existing-code E2E/AFD traffic/probe/withdrawal result.
- [x] Retry-owned cleanup and independent resource absence.

## References

- `2026-11-01-0611-afd-activation-without-projection-gates.md`: implemented and
  locally validated activation/lifecycle behavior to prove.
- `2026-11-01-1822-afd-standard-live-e2e.md`: previous expired-access blocker.
- `2026-11-01-1932-afd-https-native-routing.md`: deferred future implementation.
- `test/README.md`, `test/scripts/e2e-config.sh`, `bootstrap.sh`, `run-e2e.sh`,
  `cleanup.sh`: standard workflow, default-off retention and exact caller approval.
- https://learn.microsoft.com/troubleshoot/azure/azure-kubernetes/connectivity/config-file-is-not-available-when-connecting:
  explicit `az aks get-credentials --file` for a non-default kubeconfig.
- Domain-knowledge/specification directories are absent; no applicable version.
- https://learn.microsoft.com/azure/frontdoor/front-door-faq:
  configuration propagation timing supporting the approved traffic-window change.
