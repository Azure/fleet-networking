# Retained AFD live diagnostic

> Historical retention requirement was superseded by the user's explicit cleanup
> authorization. The exact owned AFD profile and parent/three managed resource
> groups were independently verified absent at `2026-11-01T07:11:44Z`.
> Local evidence is retained. This does not turn the earlier manual-state traffic
> success into a passing automated activation/withdrawal E2E.

## Requirements

- Run the new automatic Disabled-route staging implementation against fresh
  authorized Azure resources after verifying refreshed Owner access.
- Preserve the AFD diagnostic setup on success, failure and interrupted execution:
  do not delete Gateway, HTTPRoute, namespaces/workloads, private backends,
  controller-owned profile, parent RG or managed node RGs.
- Retention is explicitly a diagnostic mode, not a passing full withdrawal/
  cleanup proof. Existing ordinary E2E behavior must stay unchanged by default.
- Preserve ownership/finalizers, private-only origins, actual approval/readiness
  checks, bounded operations and exact caller-only PLS approval correlation.
- Confirm retention lifetime and eventual manual cleanup responsibility before
  creating billable resources. No implicit permission to remove retained resources.

## Additional comments from user

- "owner permissions have been refreshed, so try a live test now and retain the test setup (meaning don't delete)"
- Provided four Microsoft Q&A discussions about stale or stuck AFD deployment
  metadata and the current Front Door FAQ.
- Plan/live run approved; retain until 2026-11-01T09:17:32Z with user-owned cleanup
  or renewed access/lifetime authorization. Exact E2E-caller approval authorized.
- "why not start the traffic probe? maybe it may just work?"
- Subsequently: "also, you can start cleanup process now that we have learnt how to bring up afd wrt fleet".
  Scope/ownership/access were verified before cleanup; the original retention
  guards were not bypassed or weakened.

## Plan

### Phase 1: Add and validate explicit retention support

- Task 1.1: Add focused offline tests for a default-off
  `RETAIN_AFD_E2E_SETUP=true` diagnostic mode. Cover configuration validation,
  fixture/environment cleanup suppression and non-destructive test selection.
  Success: invalid flags fail explicitly and ordinary cleanup remains unchanged.
- Task 1.2: Preserve AFD fixture cleanup ownership but suppress fixture deletion
  in retained mode. Capture/report retained identities and skip the destructive
  withdrawal/cleanup portion after successful graph/traffic/probe checks.
  Success: retained diagnostics cannot claim complete withdrawal/cleanup success;
  failures still surface normally and collect diagnostics.
- Task 1.3: Coordinate setup signal traps, cleanup script, focused retained
  runner and session outer finish. Use focused AFD diagnostics rather than
  unrelated full-suite destructive tests.
  Success: no automatic teardown of retained setup on exit/timeout; ordinary
  workflow remains unchanged and explicit later cleanup remains possible.
- Task 1.4: Run focused Go/shell tests, formatting/static checks and verify
  the exact controller source/image candidate separately from test-only changes.
  Success: validated retained harness and no controller readiness bypass.

### Phase 2: Prepare a fresh authorized run

- Task 2.1: Confirm scope, cost/lifetime, manual cleanup owner and exact approval
  automation with the user. Reverify current activated Owner and effective
  actions, sufficient execution window, quota, local tools, candidate and absence.
  Success: immediate prelaunch access/resource checks succeed.
- Task 2.2: Prepare isolated session artifacts/checkout and a fresh shared-VNet
  three-cluster environment in Sweden Central, using the standard setup and
  latest committed controller candidate. Preserve source/image provenance.
  Success: owned/tagged fresh RGs, running exact controller and private discovery.

### Phase 3: Execute and preserve evidence

- Task 3.1: Run the focused retained diagnostic with bounded absolute cutoff.
  Observe automatic Disabled association, exact PLS approvals, AFD projections,
  complete enabled graph if it converges, member traffic/Host and probe evidence.
  Success: record actual outcomes without fabricating readiness or enabling
  resources manually to override unknown approval.
- Task 3.2: Capture final Gateway/HTTPRoute/controller/AFD/PLS evidence, final NAT
  subnet policies and LB SKU/backend type where accessible; verify retained
  resource existence and provide identities and actual test outcome.
  Success: setup remains available, incomplete checks are explicit, and no
  automatic deletion or unsupported full-proof claim occurs.

### Checklist

- [x] Task 1.1: Retention regression tests.
- [x] Task 1.2: Fixture retention and diagnostic classification.
- [x] Task 1.3: Coordinated non-destructive runner and cleanup behavior.
- [x] Task 1.4: Offline validation/provenance.
- [x] Task 2.1: Scope/lifetime/access/quota approval.
- [x] Task 2.2: Fresh owned environment and exact candidate.
- [x] Task 3.1: Bounded focused retained live diagnostic.
- [x] Task 3.2: Final evidence and retained-resource verification.

Complete when the authorized bounded diagnostic has an honest verified outcome,
the retained setup is independently verified, and its identities/cleanup
responsibility are recorded. Await explicit plan/lifetime approval before source
changes or Azure writes.

## Decisions

- Verified current caller subscription Owner is Activated/Provisioned from
  2026-11-01T01:17:34.910Z until 2026-11-01T09:17:32.457Z. The schedule response had
  no continuation, and effective subscription permissions include unrestricted
  actions. Recheck immediately before launching; reported refresh alone is not
  authorization evidence.
- Use the same previously authorized subscription and topology only after
  confirming this run's exact fresh scope. No resources from attempt 12 remain.
- `RETAIN_AFD_E2E_SETUP` is separate from temporary caller-only
  `ENABLE_AFD_PRIVATE_LINK_AUTO_APPROVAL`, and preserves setup on both failure
  and success. It must not remove finalizers or broaden controller RBAC.
- The existing spec explicitly deletes HTTPRoute for withdrawal and Gateway for
  cleanup. Merely skipping RG teardown cannot satisfy "don't delete." In retained
  mode those assertions must be deliberately not run and reported as such.
- Keep existing AFD approval/activation/readiness guards. Public Q&A reports of
  working traffic despite Pending metadata are useful escalation evidence, not
  proof the current ILB path works or permission to treat null as Approved.
- Current FAQ states up to 15 minutes per configuration operation, approximately
  30 minutes for back-to-back queued changes, and up to an hour for custom TLS
  updates. Keep the existing conservative 45-minute convergence window.
- No automatic cleanup at a retention deadline: the user must explicitly own
  eventual cleanup or later authorize it. Stop automated Azure work before access
  expiry, and make ongoing resource costs clear.

## Implementation Details

Retention regressions first failed for ignored cleanup/runner flags, as expected.
The directly modified cleanup script also had CRLF runtime option errors; normalize
that script to LF as part of making the validated retention guard reliable.
Retention implementation and offline validation are complete; the isolated
candidate has passed preflight, but no new live run has started yet.
The initial shell and offline AFD assertion tests pass after retention handling.
Suite AfterSuite namespace deletion was an additional teardown path; retained
mode now preserves that namespace too. Successful retained diagnostics are marked
Skipped with explicit verified/unproven report entries, not full-proof success.
The fresh run will use committed controller source plus an independently hashed
test/harness-only patch; no production source or dependency change is made.
Candidate controller HEAD: `4b52ff688e9dc19d00be702b3d6ddd60845e0876`.
The retention changes should be test/harness-only; no SDK or controller source
change is required.

Preparation initially stopped because WSL Git treated two unrelated CRLF YAML
files under `cmd/net-crd-installer` as modified. Native Windows Git verified the
production paths unchanged. Preparation now uses native Git for the shared
Windows worktree provenance check and test-only patch generation, with LF patch
transport into the independent Linux checkout. No unrelated files were changed
or reverted. The isolated checkout independently verifies exact candidate HEAD,
patch hash/diff equality and unchanged `cmd`, `pkg`, `go.mod` and `go.sum`.
ShellCheck now resolves the real checkout source path rather than failing to find
`test/scripts/e2e-config.sh`; source checking remains enabled.

Validation passed:

- `go test -count=1 ./test/scripts` (6.086s in the shared checkout and 26.460s in
  the independently patched checkout with `AFD_VALIDATE_LOCAL_TOOLS=true`).
- `go test -count=1 -tags=e2e -run "^Test(AFDSetupRetention|PrivateHTTP|GatewayPrivateHTTP)" ./test/e2e`.
- `go test -count=1 -run "^$" ./test/e2e` (ordinary build-tag compatibility).
- `go vet -tags=e2e ./test/scripts ./test/e2e`.
- `hack/tools/bin/golangci-lint-v1.64.7 run --build-tags=e2e ./test/scripts/... ./test/e2e/...`.
- `golint` for the modified test packages; goimports and Git whitespace checks.
- Bash syntax and source-aware ShellCheck for all session runner scripts;
  modified repository shell checks passed, with existing bootstrap informational
  warnings outside the change left unchanged.
- Fresh preflight verified tool hashes, Docker readiness, patch provenance,
  activated Owner/effective permissions, all four exact RGs absent and at least
  five hours before the execution cutoff. DSv5 quota usage/limit: 4/100; regional
  cores usage/limit: 150/1230; this run requires 12.

Launch will repeat the complete read-only preflight immediately before creation.

Live progress at 2026-11-01T03:40Z: standard setup completed and the focused
diagnostic reached the Private Link checkpoint. The controller automatically
observed its Disabled route with `PrivateBackendsStaged`, without manual route
association. Both exact member PLS requests were approved by the authorized test
caller and independently verified at 03:40:43Z. Fresh ARM origin reads still show
both approvals null, provisioning Succeeded and deployment NotStarted.
Gateway Programmed remains False and the endpoint remains Disabled while the
post-approval convergence phase runs, with a reported deadline of 04:25:43Z.
Traffic/probe proof has not completed; no exit result or cleanup was recorded.
The profile is `fleet-afd-46d205cb5504b6b4770af33f7f607e03` in
`fleet-afd-e2e-20261101-0124`.

At the user's request, a separate non-mutating edge probe ran at 03:46:56Z
against the actual owned AFD hostname using the same `/` path, fresh `proof`
query and no-cache header as the application proof. Both HTTP and HTTPS returned
HTTP 404 with `X-Azure-Ref` headers; neither delivered a successful application
response. Curl exited 63 because the error response exceeded the 64 KiB body
capture limit; status and headers were captured, but the bodies were not fully
downloaded. This does not establish backend health or private traffic delivery.
The normal proof and controller still wait for approval/activation; no endpoint
or route enablement, readiness bypass or cleanup was performed.
The session-only `edge-probe.sh` passed Bash syntax and source-aware ShellCheck.

A subsequent live member-1 PLS/NAT-subnet read verified provisioning Succeeded,
proxy protocol false, and NAT subnet `fleet/member-1` (`10.1.0.0/16`) with both
`privateLinkServiceNetworkPolicies` and `privateEndpointNetworkPolicies` Disabled.
That subnet has no directly associated NSG or route table; the user-supplied NSG
is attached to node NICs and still applies there. Thus the earlier suspected PLS
subnet-policy prerequisite is not missing on member-1 in this retained run.
The selected NAT address field was null, so the allocated NAT IP itself remains
unverified; no absence/failure inference is made from that projection. Member-2,
effective NIC rules and LB backend-pool type still require separate verification.

The supplied custom-domain/DNS interpretation does not apply to this fixture:
the resource is an AFD endpoint and uses its generated `azurefd.net` hostname.
`networking.fleet.azure.com.activation=pending` is Fleet-owned bootstrap metadata
defined in `bootstrap.go:18-20` and set by `ownership.go:147-150`, not an Azure
domain-validation state. `bootstrap.go:85-101` changes it to `started` when Fleet
enables the endpoint. No `_dnsauth` record or DNS-token regeneration is indicated
by this tag; the observed approval/activation gate remains the relevant blocker.

## Final retained diagnostic outcome

The focused live spec failed after its measured 2700.006-second post-approval
convergence wait (`gateway_private_http_test.go:231`). Gateway Programmed
remained False/Pending with `PrivateBackendsStaged`; it did not reach the enabled
graph or successful traffic/probe phases. `diagnostic.exit`, `run.exit`,
`retained-finish.exit` and `launch.exit` all contain 1. No cleanup was submitted.

Independent final reads verified all four RGs still exist, the Gateway and
HTTPRoute `my-ns-pmsrf/private-http` remain, and both member workloads have two
Running pods and internal-only LB addresses (`10.1.0.62`, `10.2.0.62`).
The Azure route is `fleet-route-46d205cb5504b6b4770af33f7f607e03`: Disabled,
provisioning Succeeded, deployment NotStarted, default-domain routing Enabled
and no custom domains. Both origins themselves are Enabled and provisioned,
but their shared Private Link approval remains null and deployment NotStarted.
Both exact PLS connections remain Approved.

Both final NAT subnets have PLS and PE policies Disabled, with no subnet NSG or
route table; both PLS resources are provisioned with proxy protocol false.
Both PLS-bound internal LBs have Standard SKU, two NIC references and AKS
`backendPoolType=nodeIPConfiguration`. Their API also lists backend addresses,
so address-entry presence alone must not be misclassified as an IP-based pool.
Allocated PLS NAT addresses, effective node NIC rules, actual LB health and
Microsoft-managed private endpoint NIC health remain unverified.

Evidence is retained under session `afd-retained-live-13/logs`, including the
Ginkgo JSON report, controller/Gateway diagnostics, edge probe headers/results
and independently collected `retained-evidence`. The final evidence script
passed Bash syntax and source-aware ShellCheck. This is a failed diagnostic,
not full private-delivery/withdrawal/cleanup proof. Controllers remain deployed,
and retained resources continue incurring costs; cleanup remains user-owned.

## Proposed endpoint-first experiment (not yet approved)

The user's proposed endpoint-first enablement is a hypothesis to test, not a
proven Azure sequencing requirement or null-approval remedy. Do not perform it
under the original automated-proof authorization, which excluded manual activation.

### Phase 4: Controlled manual activation, subject to separate approval

- [ ] Task 4.1: Reverify exact resource ownership, access and enough time for
  observation plus restoration before 08:02:32Z. Capture current controller
  replicas, endpoint/route states and tags. Pause only the hub Gateway controller
  deployment to avoid competing reconciliation. Success: exact scoped snapshots
  and no change to other controllers, origins, approval metadata or finalizers.
- [ ] Task 4.2: Enable the existing owned endpoint first, recording its real
  activation as `started` while preserving other tags; then enable the exact
  existing owned route. Success: explicit ARM observations, no DNS/public-origin
  changes and no Gateway readiness fabrication.
- [ ] Task 4.3: Observe raw AFD/PLS status and fresh default-hostname application
  requests for at most 45 minutes. Success: record either actual private member
  responses or the precise continued failure, without claiming controller proof.
- [ ] Task 4.4: Return route and endpoint to Disabled and resume the original
  controller replica count before the cutoff; do not delete retained resources.
  Preserve the truthful `started` activation history because manual activation
  means this fixture is no longer a never-activated bootstrap proof.
  Success: independently verified disabled states/controller restoration, or an
  explicit restoration failure; all diagnostic changes documented.

Are you happy with this implementation plan? No manual activation or controller
pause has been performed. If approved, the experiment will remain separate from
the failed automated diagnostic and cannot retroactively make that proof pass.

### User-performed portal activation

The user subsequently reported: "i manually enabled endpoint from afd manager
in portal". This is not approval of the full proposed experiment. Read-only
verification confirms endpoint Enabled/provisioning Succeeded/deployment
NotStarted, with Fleet's activation tag still `pending`. The Azure route remains
Disabled; both AFD origin approval projections remain null/NotStarted.
Gateway Programmed is False/Pending and now reports:
`AFD private HTTP provisioning failed: owned AFD endpoint was enabled without recording activation`.
No agent endpoint/route/tag updates, controller pause or reversal of the user's
portal change have been performed. Any further activation, tag reconciliation
or restoration requires an explicitly agreed scope.

The user then reported enabling the route in AFD Manager. The requested exact
`az afd route show` read confirms `fleet-route-46d205cb5504b6b4770af33f7f607e03`
is Enabled/provisioning Succeeded/deployment NotStarted, linked to the default
domain and the expected origin group. The endpoint is also Enabled/Succeeded/
NotStarted; both origin approval projections are still null. No agent activation
or tag changes were made. Gateway Programmed remains False with the same
unrecorded-activation error.

Fresh HTTP and HTTPS requests at 2026-11-01T05:20:39Z both returned AFD HTTP 404,
with edge reference headers; no successful application traffic was demonstrated.
As before, the error-body capture exceeded the 64 KiB limit (curl exit 63).
Probe artifacts now use unique nonce-based filenames to preserve the earlier
pre-activation observations. The revised session probe passed Bash syntax and
source-aware ShellCheck. This is an immediate observation after manual changes,
not proof that propagation can never complete.

The user's security-profile question refers to an AFD security policy/WAF
association. It is not a prerequisite for basic endpoint/route or Private Link
activation, though WAF is recommended for production and separate organizational
policy may require it. It does not replace Private Link approval or fix Fleet's
activation marker. Microsoft Front Door Manager documentation defines this
policy as the domain-to-WAF association and requires endpoint and route enabled
for traffic; the CLI quickstart configures WAF as its web-application protection
layer. No security policy was added to this diagnostic.

References for this clarification:
https://learn.microsoft.com/azure/frontdoor/manager and
https://learn.microsoft.com/azure/frontdoor/create-front-door-cli.

### First verified manual-state HTTP delivery

At 2026-11-01T06:00:10Z, a fresh HTTP request returned 200 with `X-Azure-Ref`.
The captured JSON body identifies `member-2`, has `proof_id=my-ns-pmsrf`, echoes
the exact fresh `request_id=read-only-20261101060010` and preserves the actual AFD
hostname in `host`. This verifies one genuine application response through the
manually activated AFD path despite both origins' `sharedPrivateLinkResource.status`
still being null. Endpoint/route/origin deployment metadata remains NotStarted;
it is not equivalent to a null value or proof of nonfunctional HTTP delivery.

The simultaneous HTTPS request returned 404. Protocol configuration verification
and a fresh Gateway status read were started separately; their outputs are not
yet available. Both-member delivery, observed AFD health probes and automated
controller activation/readiness are not proven by this single successful
response. The original automated diagnostic remains failed, and no resources
or controller metadata have been changed by the agent.

### Azure-first, then Fleet synchronization check

Fresh Azure reads confirm profile Premium/provisioning Succeeded, endpoint and
route Enabled/provisioning Succeeded/deployment NotStarted, and both origins
Enabled/provisioning Succeeded/deployment NotStarted with only their
`sharedPrivateLinkResource.status` approval fields still null. The endpoint
ownership UID matches the live Gateway UID. The route supports only `Http`,
forwards `HttpOnly`, links the default domain and has HTTPS redirect Disabled.
Thus the HTTPS 404 is consistent with this HTTP-only fixture, not a failed
configured HTTPS listener.

After the Azure reads, Fleet checks verified the Gateway controller is healthy:
one desired/ready/available replica, Running container, zero restarts and the
exact candidate image digest matching the recorded ACR digest:
`sha256:231f8d2bca99309893ceebb077e7aed76b3914135c5a2f84d6f27fe727e2e56d`.
Gateway generation and observedGeneration are 1; Accepted is True, Programmed
is False/Pending and addresses are unpublished. HTTPRoute Accepted and
ResolvedRefs are True at generation 1 but do not establish Azure readiness.
Current controller logs confirm recurring reconciliation failure:
`owned AFD endpoint was enabled without recording activation`.

The controller is running the expected code, but its lifecycle has not converged
with the user's manually enabled endpoint whose activation tag is still pending.
HTTP delivery is now demonstrated independently of that Fleet readiness gate;
no tag adoption, approval spoofing or status repair was performed. Null AFD
approval/deployment metadata is a separate unresolved controller readiness
constraint, so changing only the activation tag is not a proven complete fix.

### Successful bounded traffic-only probe

At the user's request, a separate traffic-only probe ran without requiring Fleet
Programmed or AFD deployment/approval metadata to converge. It is read-only,
uses the existing manually enabled HTTP route, and is bounded to 30 attempts/
120 seconds (with a 130-second outer timeout). The session script passed Bash
syntax and source-aware ShellCheck before execution.

It passed in two attempts at 2026-11-01T06:07:05Z:

- Attempt 1: HTTP 200 from `member-1`.
- Attempt 2: HTTP 200 from `member-2`.
- Each response had an AFD edge reference, the expected `proof_id=my-ns-pmsrf`,
  an exact fresh nonce echo and the preserved actual AFD hostname.
- A post-probe ARM read still returned null `sharedPrivateLinkResource.status`
  and deployment NotStarted for both origins.

This establishes actual fresh HTTP application delivery from both private
members after the user's manual endpoint/route activation, despite the reported
AFD metadata. It does not establish automated controller activation, Gateway
Programmed convergence, observed AFD health probes, withdrawal or cleanup, and
does not retroactively pass the original failed automated diagnostic.
Evidence is under `afd-retained-live-13/logs/traffic-only-*`; no configuration,
approval, controller metadata, replica count or retained resource was changed.

## Changes Made

- Created the retained-run plan and recorded refreshed access and public sources.
- Added default-off retention configuration, focused diagnostic selection,
  setup signal handling and explicit cleanup refusal.
- Preserved retained fixture/suite resources and omitted destructive withdrawal/
  cleanup phases with explicit Ginkgo Skipped/report-entry classification.
- Added regression tests and ordinary-build compatibility checks.
- Prepared an independently hashed test-only patch over the exact committed
  controller, with scoped no-deletion runner and absolute authorization cutoff.

## Before/After Comparison

- Before: fixture deferred deletion, explicit withdrawal/cleanup and outer RG
  teardown destroy setup after failure or success.
- Planned after: an explicitly enabled retained diagnostic leaves its setup,
  reports omitted destructive proof phases and preserves actual failure status.

## References

- `2026-11-01-0052-afd-route-staging-before-approval.md`: tested route staging change.
- `2026-10-31-2026-afd-setup-sequence-and-restrictions-audit.md`: constraints/evidence gaps.
- `test/e2e/gateway_private_http_test.go:76-110,296-331,367`:
  fixture cleanup and explicit destructive proof phases.
- `test/scripts/bootstrap.sh:46-49`, `test/scripts/cleanup.sh:9`:
  setup interruption and parent RG teardown.
- `test/scripts/run-e2e.sh`, `test/scripts/e2e-config.sh`: public AFD setup/test path.
- Session `afd-standard-live-12/{environment,preflight,run,launch}.sh`:
  prior bounded isolated runner; old dates/cleanup behavior must not be reused.
- Domain-knowledge/specification directories are absent.
- https://learn.microsoft.com/en-us/answers/questions/5642906/azure-front-door-endpoint-deployment-stuck-at-nots
  Standard/custom-domain report with a cosmetic-status reply; not ILB evidence.
- https://learn.microsoft.com/en-us/answers/questions/5988188/azure-front-door-premium-deployment-stuck-at-notst
  Premium/ACA user reports HTTP 200 while deployment NotStarted and approval Pending;
  no corresponding ILB/null-state proof or generally validated remedy.
- https://learn.microsoft.com/en-us/answers/questions/5629708/azure-frontdoor-probe-update-stuck-in-notstarted-d
  Historical probe-change incident; original report observed old probes, despite
  the accepted reply's broad claim that deployment status can be ignored.
- https://learn.microsoft.com/en-us/answers/questions/6019564/azure-front-door-standard-stuck-at-deploymentstatu
  Standard/public-origin 404 report with no posted resolution.
- https://learn.microsoft.com/en-us/azure/frontdoor/front-door-faq
  Current canonical FAQ; document date 2026-08-21, update 2026-09-14.
