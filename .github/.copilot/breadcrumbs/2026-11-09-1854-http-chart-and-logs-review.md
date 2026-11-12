# HTTP chart conventions and reconciliation logs

## Requirements

- Validate and address fleetSystemNamespace, lifecycle description, and progress
  logging review comments without restoring HTTPS work.
- Make separate fix commits on the current HTTP branch, without amending/pushing.
- Provide suggested review responses for each comment.

## Additional comments from user

- "use fleetSystemNamespace like the existing charts?"
- "This controller owns the entire AFD resources lifecycle? clarify"
- "Can you add more progress logs throughout the reconciliation process?"
- "Can the event messages give a concise cause instead of directing users back to logs?"
- "Does this need to be HTTPS as well for production? Not necessarily for this PR though."
- "Can we distinguish transient non-convergence from actual provisioning failures?"
- The outdated DeploymentStatusSucceeded gate/limited condition-detail comment
  was also supplied for validation.
- Withdrawal review: require a sustained blocked interval despite mixed edge
  propagation instead of failing after the first blocked response.
- Setup review: prepare hub Reader on verified member node resource groups,
  remove afd-pls-reader.sh and use the existing post-setup propagation window.
- Standard entry-point review: five AFD environment inputs followed by
  make e2e-setup, make e2e-tests, make e2e-cleanup should cover baseline and AFD.

## Plan

Status: approved. User selected "Approve implementation and separate commits".

### Phase 1: Align chart namespace

- [x] Task 1.1: Extend existing Helm tests for a release namespace different
  from the controller namespace, default fleet-system, explicit namespace
  override, RBAC subjects, Secret locality and leader election.
  Success: tests expose inconsistent resource placement before implementation.
- [x] Task 1.2: Add fleetSystemNamespace default fleet-system and schema
  validation; apply it to all namespaced resources, binding subjects and
  leader-election arguments. Preserve release-scoped cluster RBAC naming.
  Update the E2E installer to pass its explicit controller namespace and update
  chart documentation. Success: an override cannot split pod/identity/lease RBAC.
- [x] Task 1.3: Run Helm/script tests and applicable formatting/lint/vet checks,
  record results and commit the namespace fix separately.
  Success: existing Helm strict lint and affected installer tests pass.

### Phase 2: Clarify lifecycle ownership

- [x] Task 2.1: Replace the stale observer/cleanup-only chart description with
  supported private HTTP AFD lifecycle ownership. Document the boundary:
  profile/endpoint/origin groups/origins/route lifecycle, not every AFD feature,
  member ILB/PLS provisioning, PLS approval, or certificate/custom-domain support.
  Success: chart metadata agrees with the current gated implementation.
- [x] Task 2.2: Validate chart rendering/lint and commit this clarification
  separately. Success: no controller behavior or production gate changes.

### Phase 3: Add searchable structured progress logs

- [x] Task 3.1: Add focused tests using existing fake reconciliation fixtures
  and captured structured logs for provisioning progress, persisted status,
  errors and cleanup. Success: stages are correlated to Gateway identity and
  failed writes do not log successful persisted status/finalization.
- [x] Task 3.2: Follow repository klog structured logging and verbosity patterns.
  Log reconciliation start/end/latency; origin observation, desired graph
  resolution, provisioning/withdrawal states and pending approval counts;
  persisted Programmed changes; cleanup pending/completion; and failure stages.
  Success: log fields provide Gateway namespace/name, UID/generation and
  operation/outcome as applicable, without dumping objects, Secrets, SDK response
  bodies or arbitrary error payloads.
- [x] Task 3.3: Keep normal progress at V(2), noisy per-read/retry details out of
  default logs, and successful status/finalization logs after persisted changes
  outside retries. Preserve returned errors, requeue timing, ownership guards
  and external-mutation counts. Run controller tests and required static checks;
  record results, commit separately and supply all review responses.
  Success: logs improve diagnostics without claiming traffic readiness or
  changing reconciliation behavior.

### Completion checklist and success criteria

- [x] Phase 1 / Tasks 1.1-1.3: namespace consistency tested and committed.
- [x] Phase 2 / Tasks 2.1-2.2: accurate lifecycle description committed.
- [x] Phase 3 / Tasks 3.1-3.3: structured progress logs tested and committed.

### Phase 4: Add concise failure-event causes

Status: approved. User selected "Approve implementation and separate commit".

- [x] Task 4.1: Add focused table-driven event tests for wrapped authorization,
  throttling, timeout/cancellation, not-found, Azure service and ownership errors,
  plus unknown errors. Success: safe cause text is present without arbitrary
  payloads; existing event reasons and failed-operation semantics remain intact.
- [x] Task 4.2: Reuse a small safe cause classifier for graph, origin-observation
  and profile-cleanup failure events and structured failure logs.
  Success: event text explains operation and known cause, preserves readiness/
  finalizer boundaries, and honestly labels unclassified causes.
- [x] Task 4.3: Run targeted controller tests and static checks; commit separately
  and provide the suggested review response.
  Success: no raw SDK bodies, ownership identifiers or Secret material enter events.

- [x] Phase 4 / Tasks 4.1-4.3: concise sanitized failure causes committed.

### Phase 5: Clarify HTTP listener scope

The user explicitly requested a brief clarifying comment at the HTTP/80
listener gate. This is a comment-only clarification, not HTTPS implementation.

- [x] Task 5.1: State that HTTPS endpoint listeners/certificate lifecycle are
  deferred to a separate change. Add a brief comment at the listener gate,
  inspect the comment-only diff and commit separately.
  Success: no listener validation behavior changes.
- [x] Task 5.2: Add a brief comment at the origin forwarding protocol explaining
  that HTTPS origin forwarding and certificate validation are follow-up work.
  Success: no forwarding behavior changes.

### Phase 6: Treat known inventory provisioning as progress

Status: approved with the runtime/E2E plan.

- [x] Task 6.1: Add focused provider regressions distinguishing a later listed
  resource in Creating/Updating/Deleting from failed, unknown or drifted state.
  Success: known pending states never authorize activation, cleanup or hostname
  publication, while real failures remain errors.
- [x] Task 6.2: Represent known inventory provisioning with an internal typed/
  sentinel signal. Consume it only at provider orchestration boundaries to return
  existing staging/HTTP/withdrawal Pending results, not at arbitrary controller
  error boundaries. Validate explicit deployment failures before pending results.
  Success: expected cloud progress requeues normally with INFO diagnostics,
  preserving ownership, complete-inventory and unsupported-resource checks
  before any mutations.
- [x] Task 6.3: Run provider/controller tests and required Go checks; commit the
  fix separately with a suggested review response.
  Success: no blanket error suppression, string matching or weakened readiness.

- [x] Phase 6 / Tasks 6.1-6.3: transient inventory progress fixed and committed.

### Phase 7: Prepare E2E discovery access during setup

Status: approved with the runtime/E2E plan.

- [x] Task 7.1: Replace per-PLS script tests with setup tests proving exact
  subscription/cluster IDs and node RG managedBy ownership for both members
  before hub Reader grants, rejection of foreign/missing identities, and
  independent role-assignment observation.
- [x] Task 7.2: Move the hub Reader grants to verified member node RG scopes
  in install-afd.sh; retain member Reader discovery grants and hub CDN role.
  Remove afd-pls-reader.sh and its E2E invocation, obsolete tests and documentation.
  Success: no dynamic IAM grants inside the test and no production IAM changes.
- [x] Task 7.3: Validate standard AFD opt-in through existing setup/test/cleanup,
  keeping baseline tests and explicit manual approval opt-out. Default exact
  test-only PLS request approval on for standard ENABLE_AZURE_FRONT_DOOR=true,
  while retaining explicit false and rejecting invalid flags before work.
  Update tests/docs and commit setup access and standard-entry changes separately.
  Success: the five-input normal test path needs no additional manual PLS/IAM
  actions, assuming the runner already has the documented fixture permissions.

### Phase 8: Require a recoverable sustained withdrawal window

Status: approved with the runtime/E2E plan.

- [x] Task 8.1: Add focused deterministic tests for mixed blocked/200 responses,
  network errors, 408/429/non-AFD replies, continuous success and deadline expiry.
  Success: every unconfirmed observation resets the blocked interval.
- [x] Task 8.2: Replace first-success-plus-Consistently with a bounded observation
  requiring one uninterrupted minute of affirmative AFD blocked responses.
  Keep the existing 45-minute overall propagation deadline, nonce and response
  validation, and prior ARM/status withdrawal requirements.
  Success: temporary edge disagreement restarts the interval, never counts as
  blocking, and persistent success/network failure cannot pass.
- [x] Task 8.3: Run E2E unit tests without live TestE2E, script tests and applicable
  Go checks; commit separately and supply the review response.

- [x] Phase 7 / Tasks 7.1-7.3: setup IAM and standard E2E entry path committed.
- [x] Phase 8 / Tasks 8.1-8.3: sustained withdrawal proof committed.

## Decisions

- Existing charts use fleetSystemNamespace. Updating only Deployment would be
  incorrect: identity, lease Role/Binding and binding subjects must agree.
- This intentionally changes custom release-namespace placement: callers must
  explicitly set fleetSystemNamespace for a non-default controller namespace.
  Document that instead of silently falling back to the Helm release namespace.
- Cluster RBAC names remain Helm-release scoped to avoid cross-release naming
  changes unrelated to resource placement.
- Additional listener comment describes the current HTTP-only merge scope;
  paused HTTPS work remains untouched.
- The controller owns its supported HTTP graph lifecycle, not arbitrary AFD
  resources or platform/network prerequisites. AFD remains default-disabled.
- Logging must not imply PLS approval, propagation or live traffic from
  Programmed=True, and must not repeat external operations on status retries.
- Local tests only; no Azure writes, live CI, pushes or HTTPS changes.

## Implementation Details

Approved. Reuse chart tests and fake controller/provider fixtures.
Avoid new logging dependencies or a new observability subsystem.

Reconciliation now supplies a klog context with Gateway namespace/name, UID and
generation. The root logger follows the manager's existing --v flag (the chart
defaults to 2), rather than controller-runtime's separately configured zap
verbosity. Progress is V(2); failures include the existing bounded cause/outcome
classification instead of arbitrary error text.

The status retry records only a successful patch:

```go
persistedGateway = gateway.DeepCopy()
```

After the retry completes, `logPersistedGatewayStatus` emits the persisted
condition status/reason/generation and address count, not the condition's
potentially unbounded failure message. Cleanup similarly logs finalizer removal
only after its metadata patch succeeds, independently of event recording.

## Changes Made

Phase 6: added an internal inventory-progress sentinel for known
Creating/Updating/Deleting observations. Disabled staging retains incomplete
inventory with a diagnostic; HTTP reconciliation returns Pending without an
address; withdrawal returns Pending without deletion authority. Failed/unknown
deployment states remain errors even alongside Updating. Listed endpoint
metadata, route configuration and origin-group policy changes are checked before
being classified as pending. Complete supported/unsupported inventory remains
required before activation or cleanup writes.

Added provider regressions covering all four resource collections in staging and
enabled reconciliation, failed/unknown deployments while Updating, bootstrap
inventory regression and withdrawal without deletes. Updated only the old tests
that expected ordinary provisioning progress to fail.
`go test ./pkg/providers/azure/frontdoor -count=1` passed (38.464s);
`go test ./pkg/controllers/hub/gateway -skip '^TestAPIs$' -count=1`
passed (1.141s). Provider and controller go vet, golint and configured
golangci-lint passed; goimports and whitespace checks passed.
An intermediate shell wrapper misreported its own exit after successful commands
because status variables were lost across Windows/WSL quoting; static checks were
rerun directly and exited zero.

Phase 3: added captured-log regressions first. All nine scenarios failed before
implementation because progress logs were absent. They cover pending/programmed
graphs, sanitized provider and status failures, conflict retry without replaying
provisioning or persistence logs, origin observation, withdrawal, and cleanup
pending/completion/failed finalizer writes.
Implemented structured start/completion/duration, model-resolution, origin
observation, HTTP/disabled staging and withdrawal progress, persisted status,
profile-cleanup observation and finalizer-removal logs. Staging inventory and
approval fields are explicitly staging-scoped so an empty intermediate result
does not contradict complete-graph Programmed status. Logs do not equate ARM
configuration with Private Link approval, edge propagation or live health.

The first log-capture run revealed that klog checks its own verbosity before
forwarding to the injected logger; the tests now set the real klog --v flag
through a local FlagSet and restore the captured global state afterward.
No production verbosity workaround or new dependency was added.
`go test ./pkg/controllers/hub/gateway -count=1` passed with existing envtest
assets (232.459s). After clarifying staging field names, the nine focused
log scenarios passed again (0.111s). Package go vet, golint and configured
golangci-lint passed; goimports and whitespace checks passed.

All supplied review comments are now addressed or verified already fixed.
The user asked about an HTTP live run after reviews: it is the next validation
stage, not an outcome claimed by these local tests. No Azure operations,
pushes, amendments, or HTTPS restoration occurred during these fixes.

Inspected the Gateway chart/schema/templates/README, existing hub chart
conventions, installer, Helm tests, reconciliation/staging/observation/cleanup,
and neighboring controllers' structured logging patterns.

Phase 1: added default fleetSystemNamespace plus DNS-label schema validation;
updated all namespaced resources, binding subjects, leader election, installer,
chart documentation and tests. Release-scoped cluster RBAC naming is unchanged.
The new namespace test failed before implementation for both default and explicit
Fleet namespace cases with a different Helm release namespace.

Validation (WSL, no Azure/Docker writes):
- `hack/tools/bin/goimports -w test/helm/gateway_chart_test.go test/scripts/afd_test.go`: passed.
- `go test -tags=helm ./test/helm ./test/scripts -count=1`: passed (25.613s/5.115s).
- `go vet -tags=helm ./test/helm ./test/scripts`: passed.
- `hack/tools/bin/golint -set_exit_status ./test/helm ./test/scripts`: passed.
- `hack/tools/bin/golangci-lint-v1.64.7 run --build-tags=helm ./test/helm ./test/scripts`: passed.
- `bash -n test/scripts/install-afd.sh`: passed.
- `shellcheck -x test/scripts/install-afd.sh`: passed.
- `git diff --check`: passed.

Phase 2: clarified Chart.yaml lifecycle ownership and README boundaries.
`go test -tags=helm ./test/helm -run '^TestGatewayChartLint$' -count=1`
passed (0.738s), including Helm strict lint; `git diff --check` passed.

Phase 5.1: added the explicitly requested brief listener-scope comment; inspected
the comment-only source change. No runtime tests needed for unchanged behavior.
Phase 5.2: added the corresponding origin-forwarding comment. Go formatting and
whitespace checks passed; no forwarding behavior or HTTPS implementation changed.

Phase 8: replaced first-blocked-plus-Consistently with a resettable observation
window inside the existing 45-minute Eventually deadline. It requires a full
minute of valid negative AFD samples, resetting on any application success,
network error, timeout/throttling or unconfirmed edge response. Existing ARM
absence/disabled checks, nonces and affirmative-response validation are unchanged.
New tests were added first (initially missing the new window helper); all
non-live E2E unit tests passed (0.139s), including deterministic mixed-response,
recovery and persistent-failure cases. Goimports, e2e-tagged go vet, golint and
configured golangci-lint passed. Script tests passed in the preceding setup fix;
this change touches no scripts. No live edge-propagation outcome is claimed.

Phase 7.1-7.2: moved both hub Reader assignments to setup, after both AKS node
RG IDs/managedBy owners are validated and before member chart installation.
Each assignment is independently observed. Removed afd-pls-reader.sh and
the test-time invocation, replaced obsolete tests and updated test documentation.
The foreign-second-owner regression failed before implementation.
Validation: `go test ./test/scripts -count=1` passed (7.157s);
`go test -tags=e2e ./test/e2e -run '^Test' -skip '^TestE2E$' -count=1`
passed (0.118s). Goimports, package go vet, golint, configured golangci-lint
with e2e tags, installer Bash syntax/ShellCheck and diff checks all passed.
Standard-entry approval defaults remain a separate pending fix (Task 7.3).
Task 7.3 subsequently completed: the standard AFD opt-in now defaults exact
test-only approvals on, preserving explicit false for manual approvals and
rejecting invalid values before work. Non-AFD standard runs stay off and the
focused preinstalled proof retains its existing default-off behavior.
The five-input setup/test/cleanup path and baseline-inclusive runner already
existed; only the outstanding approval admission/default was changed.
The new default regression failed before implementation. Script tests passed
(8.031s); goimports, go vet, golint, configured golangci-lint, config Bash
syntax/ShellCheck and whitespace checks passed. No live run is claimed.

Phase 4 was implemented before Phase 3 so logs can reuse the sanitized cause
classifier. Failure events now name known authorization/authentication, throttling,
timeout/cancellation, missing-resource, update-conflict, Azure service and
ownership causes, with an explicit unclassified fallback. Event reasons and
readiness/finalizer boundaries are unchanged; arbitrary wrapped payloads are
never copied into event text. All 30 new source/cause combinations failed before
implementation and pass afterward.

`go test ./pkg/controllers/hub/gateway -count=1` passed with existing envtest
assets (228.511s). The initial configured lint found repeated timeout text;
extracted one constant and reran the relevant event/telemetry/finalization tests
(0.481s), go vet, golint and configured golangci-lint; all passed. Goimports and
whitespace checks passed. The returned controller errors remain unchanged.

Outdated DeploymentStatusSucceeded review: current `validateAFDDeploymentState`
already accepts absent/NotStarted/InProgress projections; endpointProgress and
staging diagnostics propagate observed provisioning/deployment/enabled states
into conditions. Existing bootstrap/lifecycle tests cover the behavior.
No claim of a fully automatic successful live lifecycle is made: the retained
diagnostic traffic required manual activation. The supplied Microsoft Q&A
https://learn.microsoft.com/en-us/answers/questions/5642906/azure-front-door-endpoint-deployment-stuck-at-nots
was read and supports treating deployment projections as advisory rather than
a positive deployment-Succeeded gate; explicit failures are still rejected.

## Before/After Comparison

Before: release-namespace-only placement, stale chart description, and events/
metrics/conditions but little searchable reconciliation progress.
After: explicit Fleet namespace setting, scoped lifecycle description, and
structured progress/status/cleanup diagnostics.

## References

- `charts/hub-net-controller-manager/{values.yaml,templates/deployment.yaml}`:
  existing fleetSystemNamespace convention.
- `charts/hub-gateway-controller-manager`: current chart and resource identity.
- `test/helm/gateway_chart_test.go`: existing rendering and strict lint tests.
- `test/scripts/install-afd.sh`: explicit E2E controller namespace call site.
- `pkg/controllers/hub/gateway`: runtime lifecycle and existing observability.
- `pkg/controllers/hub/trafficmanagerprofile/controller.go`: klog start/end and
  operation logging precedent.
- `docs/howtos/gateway-api-afd-configuration.md`: HTTP scope and asynchronous
  approval boundary; no specification version declared.
- Earlier `2026-11-09-1848-http-afd-ci-review.md` and
  `2026-11-09-1853-http-image-ci-review.md`: preceding independent HTTP fixes.
