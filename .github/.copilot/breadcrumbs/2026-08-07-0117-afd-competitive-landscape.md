# AFD Competitive Landscape

## Requirements

- Update PR #373 so its first-party AFD proposal includes accurate
  competitive context for GKE and Amazon EKS.
- Avoid claiming exact parity between provider architectures.
- Keep PR #373 focused on implementation evidence and point broader product
  decisions to the cross-team AFD global ingress RFC.

## Additional comments from user

- "also add a early competitive section about GKE (gcp) and EKE (aws)"
- "PR #373 should also have information about the comptetive landscape"

## Plan

### Phase 1: Correct the competitive comparison

- [x] **Task 1.1: Update the GKE comparison.**
  - Describe GKE fleets, hosted Multi Cluster Ingress and multi-cluster
    Gateway, Gateway API, MCS `ServiceImport`, and external/internal
    GatewayClasses.
  - Success criteria: the comparison uses current Google Cloud product
    boundaries and constraints.
- [x] **Task 1.2: Update the Amazon EKS comparison.**
  - Separate the per-cluster AWS Load Balancer Controller from Route 53,
    Global Accelerator, AWS WAF, and VPC Lattice.
  - Success criteria: the comparison does not present these components as one
    integrated EKS fleet ingress controller.
- [x] **Task 1.3: Clarify the proposal's role.**
  - Describe PR #373 as a private-origin implementation foundation and refer
    broader architecture questions to the cross-team RFC.
  - Success criteria: reviewers do not interpret the implementation proposal
    as the final external-customer product design.

### Success criteria

- The competitive section is accurate, concise, and backed by official vendor
  documentation.
- GKE is presented as an integrated fleet-aware multi-cluster ingress model.
- EKS is presented as a composable collection of cluster and global traffic
  services.
- The proposal retains its first-party AFD Premium + WAF + PLS focus.

## Decisions

- Update the existing competitive section rather than adding a duplicate.
- Compare operating models and product integration, not feature checkboxes.
- Keep the cross-team RFC and PR #373 as complementary artifacts.

## Implementation Details

- Replaced the existing parity-oriented table with a comparison of
  multi-cluster control, Kubernetes APIs, global traffic, private
  connectivity, WAF, and operating models.
- Documented GKE's fleet-aware hosted controller, Gateway API, and MCS model.
- Documented the Amazon EKS pattern of per-cluster load balancers combined
  with Route 53, Global Accelerator, AWS WAF, or VPC Lattice.
- Linked the proposal to the cross-team RFC for broader product decisions.

## Changes Made

- Added this breadcrumb before modifying the proposal.
- Updated `docs/first-party/001-afd-global-load-balancing.md`.
- Added official GCP and AWS documentation references.

## Before/After Comparison

| Aspect | Before | Target |
|---|---|---|
| GKE | High-level parity claim | Hosted fleet-aware ingress/Gateway model with constraints |
| Amazon EKS | Presented as a unified L7 controller | Per-cluster controller plus separately assembled global services |
| PR #373 framing | Implied final product parity | Private-origin implementation foundation |

## References

- GKE Multi Cluster Ingress and multi-cluster Gateway documentation.
- GKE Gateway API and Cloud Armor documentation.
- Amazon EKS Load Balancer Controller documentation.
- AWS Global Accelerator, Route 53, VPC Lattice, and AWS WAF documentation.
