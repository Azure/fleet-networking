# First-Party AKS Support in Fleet Networking

This folder tracks design proposals and rollout plans for enabling
`fleet-networking` to be used by **first-party AKS clusters** (services
owned by Microsoft that are themselves offered as an Azure service).

First-party workloads are subject to additional security and networking
requirements enforced by
[SFI (Secure Future Initiative)](https://eng.ms/docs/initiatives/project-standard/standards-categories/sc-networking/ddos/sfi-ns/sfi-ns253-kpi),
notably **SFI-NS253**, which mandates that any internet-facing entry
point for a first-party workload must sit behind:

1. **Azure Front Door (AFD) Standard or Premium**, terminating TLS at
   the edge with an attached
2. **Web Application Firewall (WAF)** policy, and reaching origins over
3. **Azure Private Link** — the public IP on the origin (AKS ingress /
   Service) must be removed.

The current fleet-networking data plane satisfies neither (1) nor (3):
the only supported global load-balancing surface today is **Azure
Traffic Manager (ATM)**, which is DNS-based and always returns the
public IP of a Service on each member cluster.

The proposals in this folder describe how to close that gap.

## Proposals

| # | Title | Status |
|---|-------|--------|
| [001](./001-afd-global-load-balancing.md) | Azure Front Door + WAF + Private Link based Global Load Balancing | Draft |
| [002](./002-afd-implementation-plan.md) | Implementation plan and file-by-file scope of changes for Proposal 001 | Draft |
| [003](./003-pre-implementation-checklist.md) | Pre-implementation checklist — open design decisions, sign-offs, and spikes gating code | Open |

## Non-goals of this folder

* This folder is not a substitute for the SFI onboarding checklist —
  each first-party service that adopts fleet-networking must still
  complete the SFI review with its own service tree entry.
* This folder does not document customer-facing (third-party) usage of
  fleet-networking. Existing docs under `docs/concepts`,
  `docs/howtos`, and `docs/demos` remain the source of truth for that
  audience.
