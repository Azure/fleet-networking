# HTTPS origins proposal

## Requirements and user input

- Baseline: merged GEP-1748/AFD commit
  `4f80e9e42de24d3a775d2f877fd27b51185fde7e`.
- "Let's now work on a HTTPs origins proposal aligning with the customer howto and ux"
- Approved scope: "Approve origin-focused proposal: AFD-to-member TLS, with
  frontend HTTPS documented as a separate boundary/dependency".
- "create a new PR branch (rchinchani/*) for ths"
- "use also this reference":
  <https://learn.microsoft.com/nb-no/AZURE/frontdoor/end-to-end-tls?pivots=front-door-standard-premium>.
- "capture all the requirements that a customer would need to get right etc":
  include an owner/evidence checklist spanning prerequisites, permissions,
  networking, certificates, app/probe behavior, rollout and ongoing operations.
- "custoemr facing documentation should be a in a howto (extend the existing AFD one if it makes sense)":
  put the complete checklist, customer examples and private preflight in the
  existing AFD how-to; keep the design focused on the technical contract.
- "the engineering,implementation documentation can be separate which should include e2e testing requirements, etc":
  include engineering work packages, dependencies, offline checks, live fixture
  requirements, scenario matrix, evidence and release gates in the design doc.
- "add clarifying but brief comments wherever applicable": annotate proposed
  manifests and diagnostic/test commands at the non-obvious TLS boundaries.
- "commit all changes and categorize commits similarly as PR #400 (docs, feat, build, test)":
  commit the proposal/how-to as `docs(https)` and the existing HTTP live-validation
  record separately as `test(http)`. No feature/build changes exist to categorize.
- "ensure that https://multicluster.sigs.k8s.io/concepts/multicluster-services-api/ API shape is not violated":
  explicitly preserve public ServiceExport/ServiceImport fields and baseline
  MCS behavior; distinguish existing Fleet schema differences from conformance.
- "backtlspolicy approach looks good": retain the separate policy, document
  customer control through delegated hub namespace RBAC, and include complete
  certificate/workload/routing examples without new TLS annotations.
- "remember this split but work only on docs(https) so reviewers can review this plan first.":
  document the eventual `docs(https)`, `feat(https)`, `build(https)` and
  `test(https)` split; only documentation is authorized now.
- "Also fold a79887e342ce341503dd763cd9631e609d6a6555 into the docs commit":
  squash the two existing branch commits and current documentation updates into
  one `docs(https)` review commit, preserving the historical validation record.
  This supersedes its earlier separate `test(http)` categorization.
- Scope remains documentation only; the later request authorizes commits, not
  controller implementation, Azure operations, push or PR creation.

### Mermaid and PR update follow-up

- "use mermaid diagrams wherever applicable instead of ascii art and update/amend PR":
  replace the three topology sketches in the design/how-to with Mermaid,
  retaining the separate client/origin TLS sessions and private member boundary.
  Leave command-output tables as text rather than treating them as diagrams.
- This explicitly authorizes amending the single docs commit and updating the
  existing PR #450 head on `rchincha/fleet-networking` branch
  `rchinchani/https-origins-proposal`; no new PR or runtime work is requested.
- Validation checklist:
  1. [x] Render all three diagrams using the available Mermaid CLI and verify
     that the two TLS diagrams remain consistent.
  2. [x] Check affected Markdown links, remaining text blocks and whitespace.
  3. [x] Amend the docs commit, push with an explicit expected-head lease and
     verify PR #450 points at the new commit. Verified at `9cb1a22` before the
     subsequent review follow-ups below.
- Validation results:
  - Mermaid CLI rendered all three diagrams successfully with local Edge to
    valid, nonempty SVGs outside the repository. The two TLS diagrams match.
  - The configured `markdown-link-check` passed all 49 links across the design,
    how-to and this breadcrumb; `git diff --check` passed.
  - Fences are balanced in all eight PR Markdown files. The only remaining
    `text` block is unchanged GatewayClass command output, not a topology.
  - No runtime changes or live Azure/Kubernetes tests were required or run.

### Client hostname continuity review follow-up

- Review concern: whether an endpoint-lifetime hostname is acceptable or a
  stable custom domain is still needed. User approval: "update the docs
  appropriately then".
- Keep the approved generated-domain scope. Clarify that normal reconciliation
  and member churn retain the endpoint/URL, while replacement may change it.
  Record custom domains as a production follow-up for workloads requiring
  replacement-independent identity, not as supported behavior.
- Preserve member-only customer Kubernetes access and unchanged public MCS
  schemas. A CNAME alone is insufficient; the follow-up includes DNS ownership,
  AFD validation/routing, certificates and replacement cutover/recovery.
- Checklist:
  1. [x] Align the design contract, customer how-to and readiness checklist.
  2. [x] Check affected Markdown links and whitespace.
- Validation: configured `markdown-link-check` passed all 49 links across the
  three changed documents; `git diff --check` passed. Documentation only;
  no runtime or live Azure/Kubernetes tests were required or run.

### Explicit member-policy export review follow-up

- Review concern: distinguish member BackendTLSPolicies intended for Fleet from
  policies used by other implementations. Clarified the two objects: customers
  author member Service-targeted intent; Fleet generates the hub
  ServiceImport-targeted projection automatically.
- User approval: "ok, update the PR and give me a response".
- Proposed contract: require
  `networking.fleet.azure.com/export-to-fleet: "true"` on the member policy.
  This is explicit export consent, not exclusive ownership or a standard
  controller selector. Preserve other-controller configuration/status and
  resolve shared-target precedence before requiring the winner's opt-in.
- Include metadata-only opt-out invalidation without a generation change,
  internal opt-in provenance, fail-closed withdrawal, customer examples,
  first-party fixture guidance and regression/E2E requirements. Preserve
  member-only access and unchanged public MCS schemas.
- Include the preceding hostname-continuity clarification in the same amended
  docs commit. No runtime implementation, new PR or live test is authorized.
- Checklist:
  1. [x] Align design, how-to and first-party fixture documentation.
  2. [x] Validate affected links, YAML/contract examples and whitespace.
  3. [x] Amend/push the single docs commit with an explicit expected-head lease;
     verify PR #450's head and give the user a review-thread reply. Verified at
     `03a34c2` before the single-policy follow-up below.
- Validation results:
  - Configured `markdown-link-check` passed all 58 links in the four changed
    documents; `git diff --check` passed.
  - Parsed seven proposed YAML resources: the member policy has the exact
    string opt-in and local Service target, the route references the matching
    ServiceImport/port, and the public MCS example shape is unchanged.
  - Balanced fences; all three Mermaid diagrams and 31 Bash blocks are
    unchanged. All fifteen TLS scenario IDs are retained.
  - These are documentation checks, not admission, controller or live Azure
    results. Runtime implementation remains deferred.

### Single-policy eligibility review follow-up

- Review concern: multiple policies are unnecessary when all but one are
  ineffective. User approval: "ok update the PR".
- Supersede the preceding member-policy precedence selection with exactly one
  applicable policy per Service for Fleet TLS eligibility. Count all policies
  in that namespace targeting the core Service, including unmarked and
  identical policies. The sole policy must explicitly opt into Fleet export.
- This is a stricter Fleet subset, not upstream oldest-policy conflict
  resolution or cluster-wide admission enforcement. Multiple policies block/
  withdraw the affected AFD route; Fleet neither chooses a winner nor deletes/
  edits other implementations' policies. A single shared policy may serve
  multiple implementations when their requirements agree.
- Update member watches/invalidation, diagnostics, truth table, implementation
  gates, customer readiness/troubleshooting and first-party regression/E2E
  requirements. Retain automatic hub projection and unchanged public MCS APIs.
- Checklist:
  1. [x] Align the design, how-to and first-party fixture contract.
  2. [x] Validate affected links, YAML/contract examples and whitespace.
  3. [x] Amend/push the single docs commit with an explicit expected-head lease;
     verify PR #450's head and clean worktree. Verified at `449b3b2` before the
     failure-scope follow-up below.
- Validation: configured `markdown-link-check` passed all 58 links across the
  four changed documents; `git diff --check` passed. Seven proposed YAML
  resources parsed, with one explicitly opted-in member policy and unchanged
  public MCS example shape. Fences are balanced; three Mermaid diagrams,
  31 Bash blocks and seven YAML blocks are unchanged; all fifteen TLS scenario
  IDs are retained. Superseded precedence/reselection instructions are absent
  from active contract docs. No runtime/admission or live tests were run.

### Member-local failure isolation review follow-up

- Review concern: one member's configuration failure must not unnecessarily
  cause global failure. User approval: "update the PR with all the decisions and
  text".
- Supersede unconditional all-member policy agreement/route withdrawal with
  complete source inventory, independent member eligibility and explicit
  exclusions. Local missing/invalid/duplicate/opted-out/stale or unreadable
  intent excludes only the attributable member; never silently omit it.
- Continue eligible members only after verifying excluded origins absent or
  administratively disabled/removed and agreement among all otherwise eligible
  intents. Conflicting valid TLS identities remain backend-wide ambiguity,
  never majority selection or an exclusion chosen to manufacture agreement.
- Withdraw/suspend the affected route for no eligible origins, shared dependency/
  protocol/ownership failures, incomplete authoritative inventory or inability
  to verify isolation. Health-probe failure alone is not isolation. Preserve
  bounded ownership-safe operations; propagation/in-flight requests preclude
  instantaneous isolation or a zero-error guarantee.
- If provider errors also prevent route suspension, expose enforcement failure
  and retry/escalate; requested withdrawal is not evidence that traffic stopped.
- Report excluded identity/reason, isolation state and degraded capacity through
  existing diagnostics/platform handoff, not new public MCS fields or a portable
  Degraded condition. Keep runtime certificate failures distinct from observed
  policy errors. Repair/re-admission needs fresh validation and agreement.
- Align aggregation/projection provenance, watches, truth table, lifecycle,
  customer onboarding/readiness/troubleshooting, first-party fixture guidance
  and offline/live scenario requirements. Runtime implementation remains deferred.
- Checklist:
  1. [x] Align the design, how-to and first-party fixture contract.
  2. [x] Validate affected links, examples, failure-scope consistency and whitespace.
  3. [ ] Amend/push the single docs commit with an explicit expected-head lease;
     verify PR #450's head and clean worktree. Publication verification follows
     commit creation and is recorded in the PR handoff.
- Validation: configured `markdown-link-check` passed all 58 links in the four
  changed documents; `git diff --check` passed. Seven proposed YAML resources
  parsed with unchanged opt-in, Service target, route/import/port wiring and
  public MCS example shape. Fences are balanced; three Mermaid diagrams,
  31 Bash blocks and seven YAML blocks are unchanged; all fifteen TLS scenario
  IDs are retained. Superseded unconditional all-member failure instructions
  are absent from active contracts. These are documentation checks, not
  admission/controller or live Azure validation.

### Approved end-to-end TLS follow-up

- "ok close that gap also since we want tls end-to-end": supersedes the
  original origin-only boundary. Include client-to-AFD HTTPS in the proposal,
  not merely a different client URL.
- "and make sure customer only touches member clusters (as always) and no changes to MCS api shape":
  remove the optional customer hub-access workflow. Platform operators alone
  manage hub Gateway/HTTPRoute/BackendTLSPolicy; application customers retain
  member-only access and receive the endpoint through the platform handoff.
- "and in the user guide also add the curl example (use curl instead of curl.exe)".
- Approved hostname scope: "Generated AFD hostname only; custom domains deferred".
  Azure supplies/renews the generated hostname's edge certificate; member
  application certificates remain separately customer-owned.
- "also include separate test documentation (makes sense in the \"docs\" commit or the \"test\" commit) for first-party on how to get a SNI hostname and certificate/private key (OneCert?)":
  add `test/https-origins-certificates.md` to the docs review and link it from
  the E2E guide/engineering proposal. Cover controlled DNS identity,
  conditional approved public-CA enrollment, key custody/export constraints,
  certificate/key/chain checks, member deployment, rotation and negative
  fixtures. Keep internal enrollment/access procedures and real identities
  out of this public repository.
- Follow-up plan and success criteria:
  1. [x] Extend the contract with an explicit HTTPS/443 listener and
     implementation-specific default-certificate option, HTTPS-only accepted
     protocols, and unchanged MCS schemas.
  2. [x] Separate member-only customer tasks from operator-only hub tasks;
     add endpoint handoff, plain HTTPS curl, certificate/lifecycle guidance
     and required independent proof of both TLS hops.
  3. [x] Update offline/live validation gates, run link/YAML/command/anchor
     checks, and inspect the docs-only diff. No live operations are authorized.

The follow-up supersedes the original frontend deferral and optional customer
hub-policy access below. It does not change the documentation-only runtime
boundary or authorize Azure/certificate provisioning.

### Follow-up implementation notes and validation

- Extended the design and customer how-to with HTTPS/443, explicit proposed
  `networking.fleet.azure.com/afd-default-certificate: "true"` under Gateway
  `tls.options`, default-domain association, HTTPS-only accepted edge protocols,
  no redirect, and independent HTTPS origin forwarding/probes/name checks.
  The pinned v1.2.1 TLS schema permits the nonempty-options alternative to
  certificate references; actual admission/runtime tests remain future work.
- Removed the customer hub-access alternative. The platform operator applies/
  reads hub resources, coordinates identity changes/cleanup and supplies the
  observed HTTPS URL. Customers configure/inspect members and use plain `curl`.
  No MCS API, generated manifest or controller changes were made.
- Added `test/https-origins-certificates.md` and an E2E README link. The runbook
  covers first-party name reservation, conditional OneCert/public-CA enrollment,
  approved key custody/exportability, certificate/key checks and negative/
  rotation fixtures. Internal enrollment/access details and real identities
  are intentionally excluded from the checked-in guide.
- Configured link check passed: 73 checked links across all five changed files:

  ```powershell
  npx --no-install markdown-link-check --config .github\workflows\markdown.links.config.json docs\design\gep-1748-https-origins-proposal.md docs\howtos\gateway-api-afd-configuration.md .github\.copilot\breadcrumbs\2026-11-16-2239-https-origins-proposal.md test\https-origins-certificates.md test\README.md
  ```

- Offline Python/PyYAML validation passed: seven resource manifests; matching
  HTTPS listener/options/route and unchanged example MCS shapes; certificate
  mount and TLS/SNI/Host logging; 19 Bash blocks passed `bash -n`; 25 local
  anchors; whitespace and balanced fences across five documents.
- These are documentation/structural checks only. No issuance, secret retrieval,
  OpenSSL certificate verification, Kubernetes admission, Go tests or live
  client/AFD/origin TLS execution was performed. Live proof and the exact
  deployment flag/install contract remain required before release claims.

## Plan and success criteria

### Approved member-driven policy follow-up

- "can the controller create this automatically, so everything is just member cluster driven?"
  followed by "ok, make the doc changes then": replace manual hub policy
  application with customer BackendTLSPolicy targeting the local member
  Service and automatic controller-owned hub policy projection.
- Keep both TLS hops, member-only customer credentials and unchanged public
  MCS shapes. Platform Gateway/HTTPRoute remains the explicit authorization
  to expose a backend; policy/export creation alone must not create ingress.
- This supersedes the earlier platform-applied policy/identity handoff, not
  the platform's install, ingress authorization, PLS approval or URL handoff.
- Plan and success criteria:
  1. [x] Define member selection, non-secret internal transport, cross-member
     agreement, generated hub ownership and fail-closed invalidation/status.
  2. [x] Update customer manifests/setup/rotation/cleanup and first-party
     fixture guidance; remove obsolete manual hub-policy steps.
  3. [x] Validate links, member/route/policy YAML references, Bash syntax,
     anchors and documentation-only scope. No runtime/live operations.

Validation: the configured five-file Markdown link-check command recorded above
passed again (73 checked links). Python/PyYAML checks passed for seven manifests:
member core-Service target/apply/rotation, unchanged MCS shapes and ServiceImport
route target, HTTPS listener/Secret/log wiring, and absence of manual hub-policy
apply/delete steps. All 15 scenario IDs, 19 Bash blocks (`bash -n` only), 25
local anchors, whitespace/fences and `git diff --check` passed.

The design now requires member/hub CRD and RBAC changes, non-secret internal
transport with source provenance, complete current agreement, controller-owned
projection repair/cleanup and fail-closed invalidation. It leaves the concrete
freshness/liveness bound, safe disable/upgrade mechanics and any richer durable
member readiness API as explicit implementation-review decisions. Member
diagnostics use events, never invented cross-cluster ancestors or MCS fields.
No controller, API/CRD, chart, dependency or live resource was changed in this
documentation update.
The user explicitly approved "Amend the existing docs commit" to retain one
docs review commit. Preserve the unchanged base, historical HTTP record and
required trailers; no push is authorized.

The checklist below records the earlier review stages.

1. [x] Inspect the merged resolver, model, provider, API and customer how-to.
   Success: distinguish current HTTP behavior from future TLS support.
2. [x] Obtain approval of origin-focused documentation scope and create
   `rchinchani/https-origins-proposal`.
3. [x] Draft the proposal with explicit customer/platform responsibilities,
   proposed API, trust/identity, lifecycle, compatibility and validation gates.
   Success: no implicit HTTP fallback or claim of frontend HTTPS support.
4. [x] Link a clearly proposed customer workflow from the current how-to.
   Success: examples do not require editing generated ServiceImports.
5. [x] Check Markdown links and whitespace and inspect the complete diff.
   Success: record exact results; preserve unrelated local work.
6. [x] Finish task-oriented customer setup, verification, renewal, lifecycle,
   troubleshooting and cleanup examples and document the future commit split.
7. [x] Validate the final docs/examples. Success: links/anchors, YAML references,
   command syntax, whitespace and code-fence checks pass without live operations.

The requested final history rewrite folds the historical test record into the
docs review commit above unchanged base
`b48652c04f54904808da10ef10d99fcbd06c4399`. Verify one docs-only commit, preserved
historical record, required trailers and clean worktree after committing;
feature/build/test implementation and push remain outside this review.

## Evidence and decisions

- Current backend resolution accepts TCP with absent/http appProtocol only.
  `Origin.Validate` requires a discovered internal IPv4 address. Provider
  requests use HTTP forwarding and probes; setting certificate-name checks in
  these HTTP requests does not implement TLS.
- Gateway API is pinned to v1.2.1. Its experimental v1alpha3 BackendTLSPolicy
  permits implementation-specific non-Service targets. The user selected a
  separate hub policy targeting Fleet ServiceImport, not a new annotation
  on generated objects or a new member policy controller.
- The proposed first subset uses one HTTPS port and a shared certificate DNS
  identity across members. Member certificates may have different keys.
  Per-member identities and multi-port policy semantics are deferred.
- Existing HTTP discovery already transports appProtocol in exported ports.
  Keep the private IP/PLS binding separate from TLS identity; do not replace
  discovery provenance with an application-supplied hostname.
- The user-provided AFD TLS reference and the AFD ILB/Private Link guide establish
  separate TLS sessions, trusted origin chains, hostname/SNI and name checks.
  No custom CA upload, origin mTLS or certificate-check bypass is proposed.
- BackendTLSPolicy targeting ServiceImport is the selected design direction, not
  an implemented or portable Gateway API capability.
- Upstream MCS and Fleet already differ in group and ServiceImport field
  placement. Preserve Fleet's baseline without claiming upstream wire
  compatibility. Existing Fleet aggregation compares complete exported port
  structs including targetPort; do not promise differing member targetPorts.

## Changes and behavior

- Added technical design:
  `docs/design/gep-1748-https-origins-proposal.md`.
  Includes seven implementation work packages and fourteen E2E scenarios,
  separate origin/frontend proof and explicit future test-command requirements.
- Added clearly proposed customer workflow, owner/evidence checklist and
  private TLS preflight:
  `docs/howtos/gateway-api-afd-configuration.md`.
- Added concise example comments distinguishing TLS intent from certificate
  provisioning, Service port from workload port, SNI from Host, AFD from hub
  trust, and private/offline checks from live AFD proof.
- Follow-up: selected BackendTLSPolicy, documented public MCS schema/behavior
  invariants and isolation tests, and added member certificate Secret creation,
  TLS workload wiring and a complete origin-TLS Gateway/HTTPRoute example.
- Final docs-review scope: task navigation, ordered file/apply workflow,
  configuration versus TLS/probe verification and expected responses,
  certificate renewal/reload and identity rotation, member addition/removal,
  symptom/action troubleshooting and scoped cleanup. Examples remain proposed.
- The engineering plan records four future categories but feature/build/test
  implementation remains deferred until a separate explicit approval.
- Before/after runtime: unchanged HTTP-only implementation. New text describes
  proposed origin TLS, with frontend HTTPS separately deferred.
- Preserve the contents of the pre-existing
  `.github/.copilot/breadcrumbs/2026-11-12-0252-http-review-live-validation.md`;
  it was initially committed as `test(http)` and is now being folded into
  `docs(https)` at the user's request. Reviewed the full record without
  modifying it. Its historical live
  results are not a new TLS test or a fresh validation run.

## Validation and unresolved risks

- Final task-oriented documentation checks passed:

  ```powershell
  npx --no-install markdown-link-check --config .github\workflows\markdown.links.config.json docs\design\gep-1748-https-origins-proposal.md docs\howtos\gateway-api-afd-configuration.md .github\.copilot\breadcrumbs\2026-11-16-2239-https-origins-proposal.md .github\.copilot\breadcrumbs\2026-11-12-0252-http-review-live-validation.md
  ```

- Link check: 47 checked links across all four documents. Offline Python checks:
  18 local anchor references; seven parsed YAML resources with cross-resource
  and TLS Secret/log wiring checks; 13 proposed-guide Bash blocks passed
  `bash -n`; Markdown whitespace/code fences and `git diff --check` passed.
  No documented command was executed against a cluster/Azure, no NGINX workload
  ran and no Kubernetes admission/runtime TLS behavior was verified.
- Follow-up configured Markdown link check passed for all three changed
  documents (39 checked links across files):

  ```powershell
  npx --no-install markdown-link-check --config .github\workflows\markdown.links.config.json docs\design\gep-1748-https-origins-proposal.md docs\howtos\gateway-api-afd-configuration.md .github\.copilot\breadcrumbs\2026-11-16-2239-https-origins-proposal.md
  ```

- Follow-up Python/PyYAML checks parsed all seven proposed YAML resources and
  verified export identity, generated-import boundary, policy/backend references
  and certificate mount/configuration consistency. Whitespace/code-fence checks
  and `git diff --check` passed. This is structural example validation, not
  Kubernetes admission, a live NGINX run or AFD TLS proof.
- No runtime implementation is included; the final documentation commit will
  fold the earlier documentation/history commits as explicitly requested.
- Pre-commit baseline Markdown link check passed for all four documents then
  committed (36 checked links across files):

  ```powershell
  npx --no-install markdown-link-check --config .github\workflows\markdown.links.config.json docs\design\gep-1748-https-origins-proposal.md docs\howtos\gateway-api-afd-configuration.md .github\.copilot\breadcrumbs\2026-11-16-2239-https-origins-proposal.md .github\.copilot\breadcrumbs\2026-11-12-0252-http-review-live-validation.md
  ```

- `git diff --check`: passed. Explicit PowerShell trailing-whitespace and
  balanced-fenced-code checks passed for all three documents, including the
  two new untracked files.
- Reviewed the documentation changes and verified the requested branch.
  No production code, generated artifacts, dependency manifests,
  remote branches or Azure resources were changed.
- No Go or Azure tests needed for text-only work; none performed.
- Implementation still requires API/UX review, exact policy attachment/status
  tests, AFD SNI/probe verification and separately approved live evidence.
- Public-trust certificates for private applications are a real customer
  prerequisite, not supplied by an AFD-managed frontend certificate.

## References

- [Proposal](../../../docs/design/gep-1748-https-origins-proposal.md)
- [Customer how-to](../../../docs/howtos/gateway-api-afd-configuration.md)
- [Earlier broader HTTPS/native-routing plan](2026-11-01-1932-afd-https-native-routing.md)
- [AFD Private Link ILB guidance](https://learn.microsoft.com/azure/frontdoor/standard-premium/how-to-enable-private-link-internal-load-balancer)
- [Pinned BackendTLSPolicy API](https://github.com/kubernetes-sigs/gateway-api/blob/v1.2.1/apis/v1alpha3/backendtlspolicy_types.go)
