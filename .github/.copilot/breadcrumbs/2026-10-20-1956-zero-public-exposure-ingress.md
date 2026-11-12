# Zero-Public-Exposure Ingress

## Requirements

- Position Zero-Public-Exposure Ingress as the product solution: AFD Private Link to member-cluster internal load balancers is the only supported origin topology.
- Support AFD Premium only; Standard does not support Private Link origins.
- Remove customer instructions to set `afd-origin-connectivity` and `afd-health-probe-path` on generated hub ServiceImports.
- Add inline decision comments explaining that customers may lack hub access and that a platform-owned ConfigMap is a possible future configuration source, not an implemented API.
- Emphasize asynchronous AFD Private Link approval and track approval automation with the AFD and PLS teams as an open action item.

## Additional comments from user

- Customers may not have access to the hub cluster.
- Work with AFD and PLS teams to automate approval is ongoing; automation must not be described as available.

## Plan

### Phase 1: Revise the product contract

- [x] Task 1.1: Replace the dual-topology introduction with the private-origin-only, Premium-only product positioning.
  - Success criteria: no public-backend setup path or Standard option remains in the guide; clarify that the public AFD edge remains intentional.
- [x] Task 1.2: Align prerequisites, responsibility assignments, and numbered configuration steps.
  - Success criteria: platform operators own hub-side ingress configuration; application teams need no ServiceImport edits.

### Phase 2: Document configuration and approval decisions

- [x] Task 2.1: Remove the ServiceImport annotation step and add inline Markdown decision comments.
  - Success criteria: Private Link is mandatory, not customer-selected; health-probe configuration is controller/platform-owned, with ConfigMap configuration explicitly deferred and no new default invented.
- [x] Task 2.2: Elevate asynchronous approval into a prominent warning and an explicit automation action item.
  - Success criteria: manual authorized approval remains required until automation exists, and provisioning/approval convergence is not presented as immediate.

### Phase 3: Review consistency

- [x] Task 3.1: Review the edited guide for obsolete topology references, numbering, links, and configuration commands.
  - Success criteria: the guide consistently presents the revised intended contract while retaining the unreleased-foundation caveat.
- [x] Task 3.2: Record implementation and design follow-up boundaries.
  - Success criteria: this pass changes documentation only and does not imply existing parsers or controllers enforce the new product decisions.

### Completion checklist

- [x] Phase 1 / Task 1.1 complete.
- [x] Phase 1 / Task 1.2 complete.
- [x] Phase 2 / Task 2.1 complete.
- [x] Phase 2 / Task 2.2 complete.
- [x] Phase 3 / Task 3.1 complete.
- [x] Phase 3 / Task 3.2 complete.

**Overall success criteria:** The guide documents one supported private-origin product solution, requires no customer ServiceImport configuration, and clearly identifies asynchronous approval and its automation dependency.

## Decisions

- User approved guide and breadcrumb updates, without broader design or implementation changes.
- Limit this pass to the named configuration guide and this breadcrumb; do not change Go behavior or silently rewrite the broader implementation plan.
- Zero public exposure describes member workload origins, not a private-only AFD client-facing edge.
- Do not replace the removed `/healthz` annotation with an undocumented hard-coded default.
- Preserve optional WAF and the existing limitation that HTTPS/certificate lifecycle is a later phase.

## Implementation Details

- Revised `docs/howtos/gateway-api-afd-configuration.md` to document Premium-only Private Link ingress and remove the public-backend walkthrough.
- Added inline Markdown comments for the product constraints, removed ServiceImport settings, possible future ConfigMap, and approval automation dependency.
- Kept an explicit Premium annotation in the Gateway example as documentation of the mandatory setting, not a choice of supported SKUs.
- Clarified that successful approval submission is not proof of a ready data path.

## Changes Made

- Recorded and obtained approval for the documentation plan.
- Removed the public-origin workflow and customer ServiceImport annotation step.
- Assigned hub-side operations to platform operators or authorized ingress owners.
- Added prominent asynchronous-approval warnings and an open AFD/PLS approval-automation action item.
- Renumbered the retained workflow and preserved optional WAF, traffic weights, DNS guidance, and cleanup.
- No Go behavior or broader design/implementation-plan changes made.
- Reviewed the full edited guide, sequential step headings, approval link target, and removal of public-backend commands and ServiceImport annotation commands. `git diff --check` passed.
- Follow-up: align the broader design, public-first implementation sequencing, existing multi-SKU/connectivity parser contracts, and their tests with the new product decisions before implementation. This documentation pass does not enforce those constraints at runtime.

## Before/After Comparison

- Before: public or private origins, Standard or Premium, and customer annotation of generated hub ServiceImports.
- After: Premium and Private Link origins only, platform-owned backend configuration, and explicit asynchronous approval/action items.

## References

- `docs/howtos/gateway-api-afd-configuration.md`: current operator workflow and target of this revision.
- `docs/design/gep-1748-gateway-api.md`: proposed 2026-08-17 design; currently describes broader topology and annotation choices requiring later alignment.
- `docs/design/gep-1748-implementation-plan.md`: proposed 2026-08-17 implementation plan; public-first sequencing requires later alignment with these decisions.
- `.github/.copilot/breadcrumbs/2026-08-17-2356-gep-1748-gateway-api.md`: prior design and foundation work.
- No domain-knowledge or specification files were found in the earlier branch inspection.
