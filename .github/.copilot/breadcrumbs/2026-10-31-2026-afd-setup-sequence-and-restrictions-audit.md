# AFD setup-sequence and restrictions audit

## Requirements

- Compare available official AFD Premium/Private Link setup instructions with
  controller construction, approval, association, activation and E2E setup.
- Check documented restrictions that could explain approval null or deployment
  NotStarted, including VNet, ILB, PLS and managed private endpoint requirements.
- Distinguish verified implementation behavior and restrictions from hypotheses
  or missing evidence. Do not infer data-plane failure from an approval field.
- This is a read-only code/resource-evidence audit. No source implementation,
  new live run, Azure mutation or SDK upgrade is authorized by this audit.

## Additional comments from user

- "also check if there are restrictions which is blocking AFD deployment (null)"
- "anything on the Vnet + pls endpoint also?"
- "does it make sense to expose e2e env var to also control deletion - maybe i want to keep the setup for further debugging"

## Plan

### Phase 1: Gather authoritative instructions

- Task 1.1: Read official AFD/PLS setup and restrictions documentation.
  Success: record supported topology/regions, sequence, network and approval rules.
- Task 1.2: Read repository guides, design and applicable specifications.
  Success: identify intended/current subset and inconsistencies without treating
  future-contract examples as implemented features.

### Phase 2: Trace actual sequencing

- Task 2.1: Trace roots, private backends, route association, activation and
  readiness in the injected controller provider.
  Success: record exact gates and write ordering with source line references.
- Task 2.2: Trace bootstrap scripts, E2E approvals, verification and cleanup.
  Success: compare their ordering with the controller and identify diagnostic
  retention implications.

### Phase 3: Check restrictions against captured evidence

- Task 3.1: Compare SKU/region/origin/RBAC/Private Link restrictions with the
  saved attempt-12 resource and controller snapshots.
  Success: separate verified compliant fields from unresolved checks.
- Task 3.2: Inspect available VNet/subnet/ILB/PLS endpoint evidence.
  Success: explicitly state missing NSG/UDR/policy or managed-subscription access
  rather than claiming the data plane is healthy.

### Phase 4: Validate and report

- Task 4.1: Run the smallest relevant existing sequencing tests.
  Success: report exact commands/results and explain limitations of fixture tests.
- Task 4.2: Record findings and concrete next steps, including retention design.
  Success: no claimed root cause or proposed runtime bypass without evidence.

### Checklist

- [x] Task 1.1: Official guidance.
- [x] Task 1.2: Repository/domain/specification guidance.
- [x] Task 2.1: Controller sequencing.
- [x] Task 2.2: Scripts/E2E sequencing and cleanup.
- [x] Task 3.1: AFD/Private Link/RBAC restrictions.
- [x] Task 3.2: VNet/ILB/PLS endpoint evidence and gaps.
- [x] Task 4.1: Focused existing tests.
- [x] Task 4.2: Findings and recommendations.

Complete when the compared sequence, proven constraints, missing evidence and
validation results are documented. Any implementation needs a separate approved plan.

## Decisions

- Live resources were deleted after attempt 12. Use saved snapshots/logs; do not
  recreate resources or imply current network access has been checked.
- `sharedPrivateLinkResource.status=null` and `deploymentStatus=NotStarted` are
  different fields. Approved PLS connections alone prove neither AFD projection
  nor private HTTP delivery.
- The optional debugging-retention idea is separate from auto-approval. It must
  cover both fixture and environment cleanup and remain default-off, bounded by
  authorized resource lifetime and an explicit cleanup responsibility.
- Official APIM and Application Gateway CLI procedures explicitly approve the
  backend private endpoint before creating the route. Their target-specific
  settings are not applicable to this ILB topology, but their shared AFD sequence
  contradicts a universal claim that route creation must precede approval.
- Do not replace unknown AFD approval with Approved, enable resources manually,
  or claim an SDK upgrade is a demonstrated fix.

## Implementation Details

### Controller and documentation alignment

Automatic route creation and association are controller responsibilities.
`route_provision.go:99-125` first calls `EnsurePrivateBackends`, then accesses or
creates the exact Disabled route only when backends are Converged or Configured.
`backend_provision.go:133-147` defines bootstrap Configured using
`bootstrap.go:110-115`: every origin must have AFD-projected Approved status,
Succeeded provisioning, and bootstrap-compatible deployment. Backend-side PLS
approval is not a substitute for this AFD field.

The observed null field therefore explains why automatic association never
occurred. This is a confirmed controller gate, not a confirmed cause of Azure's
null projection. Microsoft documents backend approval followed by route creation,
without an explicit prerequisite to observe AFD's projected Approved field before
submitting the route. Fleet's stricter projection gate and Disabled bootstrap
strategy deserve review; neither establishes an Azure service deadlock.

The manual Disabled route was accepted and provisioned, but both origins remained
null and the endpoint remained Disabled during the observed interval. Fixture
cleanup interrupted the experiment. Association success is configuration evidence,
not approval, edge activation, health or traffic evidence.

`requests.go:45-89` builds a mandatory private origin group, explicit required
load-balancing defaults, HTTP probes, enabled private origins, and a custom PLS
target without a group ID. Host override is omitted intentionally; matching
observations accept the returned null override. Approval is observed, not written.
`observation.go:147-179` distinguishes unknown approval from Pending/Approved and
checks target, region, host, port, enabled state and certificate validation.
Activation enables the route before the endpoint; publishing readiness still
requires the fully deployed enabled graph.

The how-to's opening implementation boundary matches this narrow, not-live-proven
HTTP subset. The proposed design includes older incremental implementation
descriptions; those are not evidence of current manager wiring. Domain-knowledge
and specification directories are absent. No documentation or implementation
changes beyond this breadcrumb were made.

### Restrictions and saved network evidence

Verified against the captured attempt-12 origins/PLS and official guidance:

- Premium, private ILB origins, IPv4 HTTP/80, supported Sweden Central location,
  matching member PLS IDs, and no public/private mixing.
- Custom PLS has no target group ID; certificate-name validation is true.
  The configured HTTPS-port field does not make HTTP-only forwarding use TLS.
- Both PLS resources and their pinned connections were Succeeded/Approved.
  Visibility or linked request authorization did not prevent these requests.
  No matching LinkedAuthorizationFailed response establishes the null-field cause.
- PLS proxy protocol is false and each PLS has one dynamic IPv4 NAT configuration
  in its respective member subnet. The verified frontend is private, subnet-bound,
  and uniquely referenced by the PLS.
- AFD's managed private endpoint subscription/VNet differs from the provider's by
  design. Direct access to that Microsoft-managed subscription is not established.

Important unresolved infrastructure checks:

- Microsoft requires `privateLinkServiceNetworkPolicies=Disabled` on the PLS NAT
  subnet. Saved `logs/run.log:864-878` shows both subnets initially created with
  `privateEndpointNetworkPolicies=Disabled` but
  `privateLinkServiceNetworkPolicies=Enabled`. These are different properties.
  `test/scripts/aks-shared-vnet.sh` does not explicitly change the PLS policy.
  Final PLS snapshots reference those same subnets, but contain no subsequent
  subnet GET. AKS/cloud-provider changes during PLS creation are not ruled out.
  This is a verification gap, not proof the final policy remained Enabled or
  caused null approval; PLS creation and connection approval succeeded.
- PLS requires Standard LB and NIC-based `nodeIPConfiguration` backend pools,
  not `nodeIP`. Member setup does not explicitly pin this choice. Saved selected
  evidence does not establish the final backend pool type.
- `validatePrivateHTTPFrontend` in
  `test/e2e/gateway_private_http_network_test.go:223-260` checks Service/source,
  PLS provisioning/location, private frontend configuration and unique binding.
  It does not verify LB SKU/backend membership, NAT subnet policies, effective
  NSGs/UDRs, available NAT capacity, backend probes, or managed PE NIC health.
- AKS additionally restricts same-subnet PLS with `externalTrafficPolicy=Local`
  and requires compatible probes if PROXY protocol is used with Local. No proxy
  protocol is enabled in the captured PLS; do not infer all other dataplane
  prerequisites from frontend verification alone.
- Two origins/two nodes per member provide no demonstrated quota or connection
  exhaustion diagnosis. NAT capacity and effective backend reachability remain
  unmeasured. Public-origin service-tag/firewall advice does not repair a missing
  private route or establish the cause of a null approval field.

### Validation and remaining actions

Passed in WSL from the repository:

```text
go test -count=1 -run 'Test(ReconcileHTTPPreview|HTTPPreview|Bootstrap|ActivatedHTTPPreview|EndpointActivation|VerifyEnabledHTTPPreview|RouteProvisioner|StageDisabledGraphDependencyOrder|StageDisabledGraphApprovalAndRestart|BuildHTTPPreviewRequests|BuildBackendRequestsSerializesPrivateHTTP)' ./pkg/providers/azure/frontdoor
```

Result: passed, package test duration 37.453 seconds. An earlier narrower selector
also passed but omitted the `TestReconcileHTTPPreview...` tests; the exact-name
run above corrects that coverage gap. Fixtures supply Approved origins for
progression and validate refusal/ordering, not Azure's asynchronous projection.
No live E2E, SDK upgrade, dependency change, Azure mutation or new environment
was performed.

Before a separately approved live retry, capture final NAT-subnet policies,
Standard/NIC backend-pool configuration, effective network/probe evidence and
bounded operation/request correlation diagnostics. Review disabled-route staging
separately from activation/readiness without treating null as approval. Neither
the old/new API comparison nor the manual route experiment proved a remedy.
The controller's logged context deadlines lack an identified failing ARM operation;
wall-clock/elapsed-time disagreement also remains unproven as a timer defect.

Debug retention is sensible as a separate default-off
`RETAIN_AFD_E2E_ON_FAILURE` proposal, not an implemented option. It must coordinate
Gateway/namespace/workload/reader-binding fixture cleanup
(`gateway_private_http_test.go:76-110,367`) and outer RG cleanup
(`test/scripts/cleanup.sh:9`, session runner finish). Keeping only the RG does not
preserve fixtures already deleted. Preserve finalizers/ownership and require an
authorized expiry, explicit cleanup owner and cleanup access reserve. Resources
already deleted cannot be retained retroactively.

## Changes Made

- Created and completed this audit breadcrumb with verified code/documentation
  gates, captured restrictions/evidence gaps, exact passing tests and bounded
  follow-up recommendations. Runtime source and cleanup behavior are unchanged.

## Before/After Comparison

- Before: successful setup/auto-approval, null AFD projection, blocked automatic
  route creation, manually associated Disabled route without observed activation.
- After: automatic association is confirmed, its AFD-projected approval gate is
  identified, generic approval-before-route guidance is verified, and missing
  final subnet/backend-pool/network evidence is explicit. Root cause and successful
  private data-plane proof remain unresolved; audit completion is not live success.

## References

- Related breadcrumbs:
  `2026-10-31-0451-afd-live-acr-refresh-proof.md`;
  `2026-10-31-0755-afd-manual-route-association-experiment.md`.
- `docs/howtos/gateway-api-afd-configuration.md`: current subset and user contract.
- `pkg/providers/azure/frontdoor`: controller graph lifecycle.
- `test/e2e/gateway_private_http_test.go`: approval, convergence and fixture cleanup.
- `test/scripts/cleanup.sh`: unconditional parent-group teardown.
- Session `afd-standard-live-12/run.sh`: bounded owned-environment cleanup.
- https://learn.microsoft.com/azure/frontdoor/private-link
- https://learn.microsoft.com/azure/frontdoor/standard-premium/how-to-enable-private-link-internal-load-balancer
- https://learn.microsoft.com/azure/frontdoor/standard-premium/how-to-enable-private-link-apim
  and https://learn.microsoft.com/azure/frontdoor/how-to-enable-private-link-application-gateway:
  full CLI procedures confirm backend approval followed by route creation;
  target-specific subresources are not applied to custom ILB/PLS.
- https://learn.microsoft.com/azure/private-link/private-link-service-overview:
  Standard/NIC backend pools, IPv4, NAT, visibility and provider/consumer topology.
- https://learn.microsoft.com/azure/private-link/disable-private-link-service-network-policy:
  explicit PLS subnet policy requirement, distinct from PE policy.
- https://learn.microsoft.com/azure/aks/internal-lb:
  AKS PLS setup, backend-pool requirement, Local/proxy restrictions and VNet access.
- `test/README.md:462-503,519-543`: temporary caller-only approval, exact pinned
  correlation, unknown diagnostics, separately authorized runs and linked Reader.
- Session `afd-standard-live-12/logs/run.log:825-905` and
  `logs/manual-route/{origins,pls-1,pls-2}-before.json`: saved creation policy,
  private target, NAT/frontend and pinned connection evidence, not current state.
- Design status: Proposed, dated 2026-08-17, product contract updated 2026-10-20.
  Microsoft Learn pages were read as current guidance; no document version claimed.
