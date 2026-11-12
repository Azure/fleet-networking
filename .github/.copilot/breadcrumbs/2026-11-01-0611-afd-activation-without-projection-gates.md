# AFD activation without unreliable projection gates

## Requirements

- Do not require AFD origin `sharedPrivateLinkResource.status=Approved` or
  `deploymentStatus=Succeeded` to activate otherwise correctly configured private
  ingress. Our retained live fixture serves both members while these fields
  remain null and NotStarted.
- Automatically enable the endpoint, then the route, after complete private
  graph creation and verification. No portal enablement should be required.
- Preserve Premium-only, Private Link/internal-LB-only topology, full ownership
  and supported-inventory checks, finalizers, authorization, bounded operations,
  subsequent write observations and explicit failure reporting.
- Separate configured/enabled ARM graph programming from actual traffic/health.
  Keep asynchronous backend Private Link approval as an operator action; do not
  automate approval, grant broader permissions or fabricate approval metadata.
- Safely reconcile the exact owned manually enabled fixture without blindly
  adopting foreign/drifted resources or publishing an unverified hostname.
- Preserve uncommitted retention-harness work and ordinary default-off behavior.
  No updated-controller deployment or push is requested. The subsequent user
  instruction authorizes cleanup of the owned retained fixture and folding
  validated changes into the corresponding existing commits.

## Additional comments from user

- "So this cannot be relied upon in the controller. The logic has to change.
  Also both endpoint and route have to be enabled after creation as per our manual enablement."
- "one of the articles did warn this to be the case?"
- Plan approved: "Approve plan and implement".
- "also afd handle origin lifecycle based on if the member cluster is added or removed or if a serviceexport is added or removed."
- "also update docs with notes about this along with rationale"
- "once done, commit/amend to corresponding commits"
- "also, you can start cleanup process now that we have learnt how to bring up afd wrt fleet"
- "add clarifying but brief comments wherever applicable"
- Reconfirmed: "once done, commit/amend to corresponding commits".

## Plan

### Phase 1: Write focused regressions first

- Task 1.1: Extend existing provider table-driven tests with complete private
  graphs whose approval is null/absent or Pending and deployment is NotStarted/
  InProgress. Require automatic endpoint-first, route-second activation and
  subsequent configured/enabled verification without positive projection gates.
  Success: these cases fail against current code for the observed deadlock.
- Task 1.2: Add recovery coverage for an exactly owned, correctly configured
  manually enabled endpoint/route still tagged `activation=pending`, and for the
  intended intermediate Enabled-endpoint/Disabled-route state.
  Success: valid activation is recorded and completed, not rejected/suspended;
  foreign ownership, public/different PLS targets and incomplete inventory remain
  blocked before adoption, activation or hostname publication.
- Task 1.3: Update narrowly affected negative expectations while retaining real
  provisioning failures, explicit rejection/disconnection/timeout signals, drift,
  unsupported inventory, SDK errors, cancellation and one-write-budget tests.
  Success: removing false positive-readiness prerequisites does not turn actual
  errors or invalid private configuration into successful programming.

### Phase 2: Correct shared provider lifecycle and observations

- Task 2.1: Separate exact private configuration/provisioning acceptance from
  read-only AFD approval/deployment diagnostics. Reuse shared checks in staging,
  inventory and enabled-graph verification rather than adding exceptions only
  to the final activation call.
  Success: null/Pending approval and absent/NotStarted/InProgress deployment do
  not block matching provisioned resources, including after first activation;
  required private-target properties and accepted provisioning still gate writes.
- Task 2.2: Preserve disabled construction until the complete supported private
  graph is verified; enable the endpoint and record `activation=started`, observe
  that write, then enable and observe the route. Recognize this intermediate
  state as intended progress rather than a suspension trigger.
  Success: every path uses at most one write per reconcile and no positive AFD
  approval/deployment projection is a circular activation prerequisite.
- Task 2.3: Adopt an externally enabled pending-marker endpoint only after full
  owned/private graph verification, with a bounded metadata update recording the
  real activation and preserving other tags. Revalidate before publication.
  Success: the existing manual fixture can converge without falsely approving a
  link or accepting an unverified graph; invalid metadata/identity still errors.
- Task 2.4: Keep suspension, stale-origin cleanup and absence verification tied
  to real ownership/configuration failures and confirmed observations, not stale
  positive-readiness projections.
  Success: no automatic deletion of foreign resources, cleanup success-shaped
  fallback, silent SDK failures or unverified unsupported-resource inventory.

### Phase 3: Keep controller, E2E and documentation consistent

- Task 3.1: Define Programmed as the observed, correctly configured, owned,
  enabled ARM graph with accepted provisioning, not measured health or a promise
  that Private Link approval/edge propagation/application health has completed.
  Preserve consistent Gateway/listener messages and informative approval events;
  do not label a programmed graph unprogrammed solely because projection is null.
  Success: hostname publication and current-generation conditions follow the
  verified configuration contract; traffic claims remain independent.
- Task 3.2: Update existing E2E proof stages/helpers to expect automatic activation
  rather than a persistently Disabled preapproval graph. Keep exact caller-only
  backend connection approvals, fresh both-member/Host assertions, actual health
  probe observations and withdrawal/cleanup requirements for full proof.
  Success: full E2E cannot pass on accepted configuration alone; retained mode
  still omits destructive phases and never claims full proof.
- Task 3.3: Correct how-to/controller inline descriptions of activation ordering,
  stale metadata and asynchronous backend approval. Reference the reproduced
  public report and our retained evidence without presenting customer testimony
  as a universal Azure endpoint-first guarantee.
  Success: documentation distinguishes configured programming, propagation,
  private connectivity and actual delivery; SDK upgrades are not offered as an
  unsupported remedy.

### Phase 4: Validate and record outcome

- Task 4.1: Run the complete modified provider and Gateway packages, focused
  E2E/shell helper tests and build-tag compatibility checks. Run goimports,
  go vet, golint, configured golangci-lint and relevant shell checks. Build/type
  check the dependent manager. Do not change dependencies unless required.
  Success: affected existing checks pass and retained work is preserved.
- Task 4.2: Record exact commands/results, course corrections and remaining
  live-proof limitations. Any updated-controller deployment or fresh billable
  fixture requires separate authorization and renewed immediate access checks.
  Success: no claim of controller-owned live activation from the earlier manual
  traffic proof, and no unrequested resource change or commit.

### Checklist

- [x] Task 1.1: Null/Pending and stale-deployment activation regressions.
- [x] Task 1.2: Manual-state adoption and endpoint-first intermediate state.
- [x] Task 1.3: Preserve failure/ownership/budget coverage.
- [x] Task 2.1: Shared configured-state and diagnostic separation.
- [x] Task 2.2: Verified endpoint-first automatic activation.
- [x] Task 2.3: Verified owned pending-marker adoption.
- [x] Task 2.4: Safe suspension/cleanup/absence behavior.
- [x] Task 3.1: Consistent configuration-based programming conditions/events.
- [x] Task 3.2: Independent E2E activation, traffic and probe proof.
- [x] Task 3.3: Correct documentation and inline decisions.
- [x] Task 4.1: Affected tests, formatting, static checks and manager build.
- [x] Task 4.2: Final evidence and explicit live-proof limitations.

Implementation is complete when the regression cases and affected checks pass,
automatic private graph activation no longer depends on stale positive AFD
metadata, and controller/E2E/documentation share the same truthful contract.
The user explicitly approved this plan; implementation may proceed.

## Decisions

- Keep exact ownership/private configuration and ARM provisioning acceptance as
  authoritative control-plane checks; do not substitute "any Approved connection"
  on a PLS or broaden controller approval permissions.
- Do not install a generic HTTP health probe in the controller: customer routes
  can have arbitrary behavior, and Programmed is not an application health check.
  Real delivery remains separately validated.
- Preserve explicit adverse signals and real provisioning/SDK failures rather
  than treating every read or failure as success.
- Enable endpoint before route after verified staging to match the successful
  manual sequence; retain the empty/Disabled route as the traffic barrier until
  its exact private graph is verified. Public origins remain unsupported.
- The activation tag is Fleet-owned history, not Azure validation. Correctly
  observed external activation can be recorded only after strict adoption checks.
- Do not reset unrelated files or overwrite uncommitted retention modifications.

## Implementation Details

### Historical baseline and initial red/green evidence

Implementation is starting. Current gates are spread across `bootstrap.go`,
`backend_provision.go`, `observation.go`, `inventory.go`, `provision.go` and
`http_provision.go`, not just a single approval check.
Current activation enables the route first, then endpoint; an externally enabled
pending-marker endpoint errors before graph verification. Current inventory
requires approved/deployed origins and deployed endpoint/group/route resources.

Seven focused activation regressions have been written in `bootstrap_test.go`.
`go test -count=1 -run "^TestReconcileHTTPPreviewLaggingProjection$" ./pkg/providers/azure/frontdoor`
fails as expected against the original provider: null/Pending prevents inventory
verification, NotStarted blocks the enabled graph, and manual activation is
rejected before verification. The first shared configuration-readiness and
endpoint-first activation changes now pass all seven cases with the same command
(2026-11-01 06:37 UTC). Complete provider and lifecycle validation is still pending.

The user subsequently authorized cleanup of the exact retained parent group
`fleet-afd-e2e-20261101-0124` and its three owned AKS managed groups. A separate
bounded cleanup operation verifies the run tag, AKS-to-managed-group identities,
AFD ownership tags and active Owner access before writes. It stops only this
fixture's Gateway controller, deletes its owned AFD profile, then submits parent
group deletion and verifies all four groups absent. The original retention
harness and its guards remain unchanged; no finalizer or Azure permission bypass
is part of cleanup. Validated source changes will be folded into existing commits,
without pushing or deploying the updated controller.

## Proposed lifecycle extension

The additional user requirement extends the current approved activation work.
Existing member ServiceExport reconciliation publishes/removes hub
InternalServiceExports; ServiceImport aggregation and Gateway dependency watches
already provide the normal export lifecycle. Current source selection and stale
origin cleanup need explicit transition tests, and stale null approval metadata
currently prevents removing otherwise verified owned origins.

Additional tasks approved by the user:

- [x] Task 1.4: Add regressions for ready member/export arrival, member/export
  removal, temporary discovery unavailability, re-export with a new Service UID,
  and removal of the final origin. Success: origin membership tracks eligible
  sources; healthy remaining members are retained; stale null metadata cannot
  prevent safe removal.
- [x] Task 2.5: Reconcile new and retired origins using the existing member
  namespace/InternalServiceExport/ServiceImport pipeline. Extend source
  dependency handling where needed, without duplicating ServiceExport bridging
  or broadening Azure approval permissions. Success: bounded creation/pruning
  followed by automatic activation of the current verified private graph.
- [x] Task 2.6: When no eligible origins remain, disable the endpoint and remove
  the owned route and retired origins, while retaining the Gateway, AFD profile
  and endpoint for later re-export. Success: no old member remains routable, no
  orphaned origin is treated as current, and recreation is possible without
  deleting foreign/shared resources.
- [x] Task 3.4: Update controller/source/provider tests and lifecycle docs for
  these transitions; distinguish temporary missing discovery from malformed or
  unauthorized configuration. Success: existing safety/error behavior remains
  explicit and the agreed origin-membership policy is consistently tested.

The member/export addition should create an origin only when its export is
eligible and has ready private discovery, not merely because a member exists.
The user confirmed continued serving from remaining ready private members, and
removing owned route/origins while retaining the Disabled endpoint/profile after
the last eligible origin disappears. That initial lifecycle approval did not
authorize live member/export deletion. Subsequent explicit authorization covered
the exact retained Azure fixture cleanup, separately recorded above.

## Changes Made

- Provider configuration observations now require exact desired private bindings
  and accepted provisioning while treating positive approval/deployment as advisory.
  Endpoint-first activation records ownership history before route activation.
- Full read-only inventory verification remains strict. The separate topology
  reconciliation validates supported inventory, creates missing origins first and
  prunes one verified retired origin without disabling remaining ready members.
- Resolver selection follows active exports and current reserved member namespaces.
  Missing/deleting exports and discovery delay omit only that member; malformed
  active sources remain errors. Retired sources are excluded before indexed identity
  validation so stale malformed records cannot suspend healthy remaining members.
- Last-origin withdrawal disables the endpoint, removes the owned route, then
  retires origins with one write/30-second budget per reconcile. Roots are retained;
  Gateway finalizer deletion still deletes the verified owned profile.
- Added controller export-withdrawal, provider activation/membership/withdrawal,
  namespace-departure and resolver transition regressions. Updated the manager
  fake for the new withdrawal interface, keeping startup free of Azure writes.
- E2E permits configured programming before approval but independently requires
  exact backend-side approvals, actual traffic/Host/probe evidence and observed
  route/origin absence plus sustained negative edge responses for full proof.
- Updated how-to, chart/test guides and brief rationale comments. Preserved all
  prior default-off retention changes; no dependency or permission expansion.
- Authorized fixture cleanup finished at `2026-11-01T07:11:44Z`: verified owned AFD
  profile and parent plus all three managed groups absent. Local evidence remains.

### Validation and course corrections

- Complete Gateway/HTTPRoute/GatewayClass suites passed, including the real local
  API-server Gateway integration suite. Complete provider/resolver suites passed.
- Offline tagged AFD proof helpers, all shell helper tests and non-tagged E2E
  compilation passed; no Azure/full E2E was executed with the updated code.
- Manager tests and build passed. Vet/golint and configured lint passed after
  fixing the directly dependent manager fake, removing obsolete unused metadata
  helpers, using `errors.As`, and separating explicit profile-absence observation.
- Withdrawal additionally reobserves owned Disabled roots before route deletion;
  targeted rejection tests cover unsupported inventory, SDK read errors, changed
  root state and cancellation.
- A final test exposed contradictory identity validation running before deleting
  export exclusion. It failed before the ordering fix; complete resolver/Gateway
  suites then passed, including the API-server integration suite.
- Shell syntax passed. Source-aware ShellCheck has 194 existing findings versus
  196 in the committed baseline, with zero new source-line-identity findings.
  Existing unrelated diagnostics were not suppressed or changed.
- Signed corresponding-commit integration completed after all final checks.
  Autosquash preserved the validated tree exactly; all four commit signatures
  verified `G` and the worktree was clean. The Windows `cmd /c exit 0` editor
  failed before rebase started; retrying with Git's `true` editor succeeded.
  This final audit note is folded into the documentation commit as well.
  No push or updated-controller deployment occurred.

### Exact validation commands/results

All Go commands used `GOCACHE=/root/.cache/go-build GOPATH=/root/go`.
Gateway integration additionally used
`KUBEBUILDER_ASSETS=/root/.local/share/kubebuilder-envtest/k8s/1.33.0-linux-amd64`.

| Command | Result |
| --- | --- |
| `go test -count=1 ./pkg/providers/azure/frontdoor ./pkg/controllers/hub/gatewayresolver` | Passed complete suites; provider 38.681s, resolver 0.056s |
| `go test -count=1 ./pkg/controllers/hub/gateway ./pkg/controllers/hub/httproute ./pkg/controllers/hub/gatewayclass` | Passed, including Gateway API-server suite |
| `go test -count=1 ./pkg/controllers/hub/gateway ./pkg/controllers/hub/gatewayresolver` | Passed after final ordering fix; 229.678s and 0.045s |
| `go test -count=1 ./cmd/hub-gateway-controller-manager` | Passed |
| `go test -count=1 -tags=e2e -run "^Test(PrivateHTTP\|AFD)" ./test/e2e ./test/scripts` | Passed offline helpers; no live E2E |
| `go test -run "^$" ./test/e2e` | Passed non-tagged compilation |
| `go test -count=1 ./test/scripts` | Passed complete script suite |
| `go vet -tags=e2e ./test/e2e ./test/scripts` | Passed |
| `hack/tools/bin/golangci-lint-v1.64.7 run --build-tags=e2e ./test/e2e ./test/scripts` | Passed |
| `go build -o <session-files>/afd-validated-manager ./cmd/hub-gateway-controller-manager` | Passed; generated binary stays outside repository |
| `bash -n test/scripts/bootstrap.sh test/scripts/cleanup.sh test/scripts/e2e-config.sh test/scripts/run-e2e.sh` | Passed |
| `shellcheck -x -P test/scripts -f json test/scripts/bootstrap.sh test/scripts/cleanup.sh test/scripts/e2e-config.sh test/scripts/run-e2e.sh` | Existing diagnostics only: baseline 196, current 194, zero new |
| `git diff --check` | Passed |

The following exact package set passed `go vet`, `hack/tools/bin/golint`
and `hack/tools/bin/golangci-lint-v1.64.7 run`:
`./pkg/providers/azure/frontdoor ./pkg/controllers/hub/gateway ./pkg/controllers/hub/gatewayresolver ./pkg/controllers/hub/gatewayclass ./pkg/controllers/hub/httproute ./cmd/hub-gateway-controller-manager`.
Modified Go files were formatted with existing `hack/tools/bin/goimports -w`.
No lint/security settings were relaxed and no dependencies changed.

### Commit integration

Changes were folded into the existing signed commits by original/latest file scope:
how-to/test guide/breadcrumbs into docs; provider/resolver/controller/manager
interface tests into feature; chart guide and build-owned script retention into
build; E2E proof/fixture/cleanup retention into E2E. Signed fixups/autosquash
preserved both required trailers and the identical final tree. Signature and
clean-worktree verification passed. No push or updated-controller deployment
was performed; updated automatic activation/lifecycle still lacks live proof.

## Before/After Comparison

- Before: a working private HTTP graph can remain unprogrammed forever because
  AFD approval is null/Pending or deployment is NotStarted; portal activation
  also triggers Fleet's pending-marker error.
- After: a strictly verified private graph is enabled automatically and
  reports configured ARM programming, with stale projections advisory, actual
  failure signals explicit and real delivery/probes requiring independent proof.
  Member/export transitions preserve ready remaining origins and withdraw the
  last effective backend without deleting retained roots.

## References

- `2026-11-01-0124-afd-retained-live-diagnostic.md`: failed original automated
  attempt, user-performed activation and successful two-member traffic probe.
- `2026-11-01-0052-afd-route-staging-before-approval.md`: prior staging change.
- `2026-10-31-2026-afd-setup-sequence-and-restrictions-audit.md`: topology checks.
- Domain-knowledge/specification directories are absent; `.github/.copilot`
  currently contains only `breadcrumbs`. No applicable specification version.
- `docs/howtos/gateway-api-afd-configuration.md`: current product contract.
- `pkg/providers/azure/frontdoor/http_provision.go:24-25,113-149,168-174,207-227`:
  current route-first activation and deployed readiness.
- `pkg/providers/azure/frontdoor/bootstrap.go:50-81,111-136`: bootstrap/projection gates.
- `pkg/providers/azure/frontdoor/backend_provision.go:125-151,324-325`:
  configured/approved/deployed backend gating.
- `pkg/providers/azure/frontdoor/observation.go:128-155,190-204`: origin metadata gates.
- `pkg/providers/azure/frontdoor/inventory.go:69-107,164-168,211-215`: inventory gates.
- https://learn.microsoft.com/en-us/answers/questions/5988188/azure-front-door-premium-deployment-stuck-at-notst:
  customer reports HTTP 200 while deployment NotStarted and approval Pending.
  Our ILB/PLS fixture independently reproduces working traffic with approval null.
- https://learn.microsoft.com/azure/frontdoor/manager:
  endpoint and route must both be Enabled for traffic.
- https://learn.microsoft.com/azure/frontdoor/create-front-door-cli:
  reference Enabled endpoint and route setup, not a proof of mandatory update ordering.
- https://learn.microsoft.com/azure/frontdoor/front-door-faq:
  propagation timing guidance; not a reliable private-approval readiness contract.
