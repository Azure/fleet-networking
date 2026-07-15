# Pre-Implementation Checklist for the AFD Proposal

| Field       | Value                                                    |
|-------------|----------------------------------------------------------|
| Status      | Open                                                     |
| Author      | @rchinchani_microsoft                                    |
| Created     | 2026-07-15                                               |
| Depends on  | [Proposal 001](./001-afd-global-load-balancing.md), [Proposal 002](./002-afd-implementation-plan.md) |

This document tracks the concrete gates that must clear **before**
Phase 1 code lands (or, where noted, before Phase 2+ lands). Every
item is either an **open design decision**, an **external review /
sign-off**, or a **spike** whose outcome can change the shape of the
code.

Nothing in Proposals 001 / 002 changes based on this document — it
converts their open questions and unstated assumptions into a
trackable, checkable list.

---

## 1. Open design decisions (from Proposal 001 §11)

Each of these can change the API surface. Resolve before typing
`api/v1alpha1/frontdoorprofile_types.go`.

- [ ] **1.1 `FrontDoorProfile` scope** — namespaced (current proposal)
      vs cluster-scoped. Namespaced matches ATM parity and RBAC
      isolation; cluster-scoped matches the "shared ingress" mental
      model.
      - Impact: `+kubebuilder:resource:scope=` marker, CRD manifest,
        RBAC rules in `charts/hub-net-controller-manager/templates/rbac.yaml`.
      - Owner: —
      - Decision: —

- [ ] **1.2 WAF policy required at SKU level?** — CEL rule in
      Proposal 002 §3.1 currently makes `wafPolicy` required only for
      `Premium_AzureFrontDoor`. Options:
      - (a) required for Premium only (current proposal)
      - (b) required for **any** SKU (stricter; matches SFI intent
        for internet-facing surfaces)
      - (c) required only when annotated as first-party
      - Impact: `+kubebuilder:validation:XValidation` rule, validation
        tests under `test/apis/v1alpha1/`.
      - Owner: —
      - Decision: —

- [ ] **1.3 Custom domains in phase 2 or phase 5?**
      - Impact: shape of `FrontDoorProfileStatus` (does it grow a
        `CustomDomains []` list now?), extra controller code in
        phase 2 vs. clean deferral.
      - Owner: —
      - Decision: —

- [ ] **1.4 Umbrella `GlobalLoadBalancer` CRD later?** — if yes,
      invest in shared abstractions in `pkg/common/globalload/` from
      day one. If no, keep AFD and ATM helpers strictly separate.
      - Impact: package layout of `pkg/common/azurefrontdoor/`;
        naming of shared types.
      - Owner: —
      - Decision: —

- [ ] **1.5 Adding `Spec` to `ServiceExport`** — today
      `api/v1alpha1/serviceexport_types.go:51-56` has no `Spec` at
      all. Proposal 002 §3.3 introduces one with a single defaulted
      field. Confirm with maintainers that this is acceptable
      (vs. e.g. an annotation-based opt-in that avoids the CRD
      schema change).
      - Owner: —
      - Decision: —

## 2. External review / sign-off gates

- [ ] **2.1 Fleet-networking maintainer review of PR #373** —
      https://github.com/Azure/fleet-networking/pull/373
      - Expect feedback on §1.1, §1.5, and on the feature-flag
        default.
- [ ] **2.2 SFI reviewer sign-off on Proposal 001 §7** — this is
      the Phase 0 exit criterion in Proposal 002 §6.
- [ ] **2.3 Product / PM sign-off on L4 scoping** — Proposal 001
      §2.3 and Proposal 002 §10 state L4 (raw TCP/UDP) is out of
      scope. Confirm no first-party adopter blocks on L4 before
      we commit to the phased plan.
- [ ] **2.4 Security review of identity split** — Proposal 001 §7
      requires a separate managed identity for AFD vs. ATM.
      Confirm charts / Helm can express this without breaking
      existing ATM installations.

## 3. Spikes (each ≤ 1 engineer-day)

Small implementation questions whose answers can invalidate parts of
Proposal 002. Do these **before** committing to Phase 2, but they can
run in parallel with Phase 1 API work.

- [ ] **3.1 AKS cloud-provider PLS status annotation** — verify that
      `service.beta.kubernetes.io/azure-pls-resource-id` (or an
      equivalent) is set by the in-tree Azure cloud provider once
      the PLS is created. Read the source in
      `kubernetes-sigs/cloud-provider-azure`.
      - Impact: member `serviceexport` controller reads this to
        populate `InternalServiceExport.status.privateLinkService.resourceID`.
        If missing, we need a separate ARM call (extra RBAC on the
        member).
      - Owner: —
      - Result: —

- [ ] **3.2 `armcdn` SDK compatibility with pinned `azure-sdk-for-go`** —
      `sigs.k8s.io/cloud-provider-azure` transitively pins
      `azure-sdk-for-go/sdk/azcore`. Run:
      ```
      go get github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn@latest
      go mod tidy && go build ./...
      ```
      on a throwaway branch and confirm no conflict.
      - Impact: `go.mod` version pin decisions.
      - Owner: —
      - Result: —

- [ ] **3.3 Cross-subscription PLS auto-approval** — confirm that
      listing the AFD subscription in
      `service.beta.kubernetes.io/azure-pls-auto-approval` on a
      Service in a member VNet in a **different subscription/tenant**
      actually results in the private endpoint connection being
      auto-approved without a separate ARM call.
      - Impact: whether Phase 4 needs to implement an ARM-based
        approval fallback (`Microsoft.Network/privateLinkServices/
        privateEndpointConnections` PUT). If yes, member controller
        gets additional RBAC.
      - Owner: —
      - Result: —

- [ ] **3.4 RBAC minimums for the AFD MI** — verify against
      Microsoft.Cdn role definitions:
      - Does `CDN Profile Contributor` cover creation + deletion of
        `profiles`, `afdEndpoints`, `originGroups`, `origins`,
        `routes`, `securityPolicies`?
      - Does the AFD MI need any role on the member-cluster PLS
        resource group (for the AFD → PLS private endpoint connection
        approval call, if 3.3 is a "no")?
      - Impact: `docs/howtos/frontdoor-permissions-setup.md`; the
        Bicep/az CLI snippets included in that doc.
      - Owner: —
      - Result: —

- [ ] **3.5 Dev-sub provisioning** — stand up the test rig:
      - One AFD Premium profile RG.
      - One WAF policy in Prevention mode.
      - Two member AKS clusters with PLS-capable subnets and the
        managed identity trust to create PLS resources.
      - Impact: needed for Phase 2 integration test in a live sub,
        and Phase 4 e2e.
      - Owner: —
      - Result: —

- [ ] **3.6 Interaction with existing `azcloudconfig` in charts** —
      the ATM path today uses a single `azurecloudconfig.yaml` per
      chart (see `charts/hub-net-controller-manager/templates/azurecloudconfig.yaml`).
      Decide whether AFD reuses the same file (single identity for
      all Azure work — simpler but violates SFI least-privilege) or
      gets its own (two configs, two mounts). Proposal 001 §7
      assumes the latter — confirm this is chart-expressible without
      breaking existing users.
      - Owner: —
      - Result: —

## 4. Nice-to-have before Phase 1

- [ ] **4.1 Draft `docs/concepts/HTTPBasedGlobalLoadBalancing/README.md`**
      user-facing concept skeleton. Even a stub makes the API
      choices in Phase 1 easier to defend during review.
- [ ] **4.2 Set up a per-CRD Prometheus dashboard mock** — the
      metric names in Proposal 001 §5.4 should match an actual
      Grafana panel we're willing to ship.

## 5. What is **not** blocking

Explicitly *not* required before starting Phase 1:

- Custom domain implementation.
- Full WAF-inline mode implementation (reference-only is fine for the
  first PR).
- End-to-end infrastructure automation for the dev sub (Bicep of the
  RG can come with the howto doc).
- Umbrella-CRD refactor (only relevant if §1.4 resolves to "yes").

## 6. Overall readiness verdict

| Category                   | Status |
|----------------------------|--------|
| Design coherent            | ✅     |
| Scope and phases documented | ✅     |
| Open design decisions closed | ⏳ (§1) |
| Maintainer review          | ⏳ (§2.1) |
| SFI sign-off               | ⏳ (§2.2) |
| SDK / cloud-provider spikes | ⏳ (§3.1–3.4) |
| Dev-sub ready              | ⏳ (§3.5) |

**Recommendation:** start Phase 1 (API types + defaulters + CEL, no
controllers) *only after* §1.1, §1.2, §1.5, §2.1 are closed. Phases
2–4 additionally require §3.1–3.4 and §3.5.

Update the checkboxes above as items close; when every box in §1–§3
is checked, Phase 2 can begin.
