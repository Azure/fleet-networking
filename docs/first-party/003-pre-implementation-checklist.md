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

> **Reader's note.** As of the reconciliation pass on 2026-07-20 (see
> `.github/.copilot/breadcrumbs/2026-07-20-1108-afd-export-mode-mcs-parity.md`,
> Addendum 2), several items are already `[x] Resolved` by decisions
> that shipped in commit `cb02d14`. The single current hard blocker
> for GA is §2.4 (SFI identity split) — see the readiness table in
> §6.
>
> **Status update (2026-07-20 session, head `629644b`).**
> Every open item below whose blocker was "reconciler / API not yet
> written" is now unblocked by shipped code (WAFPolicy +
> ComplianceMode, `FrontDoorBackend` CRD + reconciler + coexistence
> guard + envtests, member `serviceexport` PLS lookup). The
> **§2.4 identity split** is *structurally* resolved by the sibling
> binary + chart landing (`fcb37f2`, `5672313`, `c219ca2`), but the
> POC bridge in `cmd/hub-net-controller-manager` remains and must
> be removed before GA. Item checkboxes below have NOT been
> re-checked; trust this summary block for current status and see
> the breadcrumb Addendum 3 for the per-commit narrative.

---

## 1. Open design decisions (from Proposal 001 §11)

Each of these can change the API surface. Resolve before typing
`api/v1alpha1/frontdoorprofile_types.go`.

- [x] **1.1 `FrontDoorProfile` scope** — **Resolved: namespaced.**
      An AFD `FrontDoorBackend` binds an AFD origin to a specific PLS
      that fronts a specific `Service` in a specific namespace on a
      member cluster. Ownership follows the workload: the app team
      that owns the `Service` also owns the `ServiceExport`,
      `ServiceImport`, `FrontDoorBackend`, and — for RBAC parity — the
      `FrontDoorProfile` too. A cluster-scoped profile would split
      ownership between cluster admins (owning the public hostname +
      WAF attach) and app teams (owning the origins grafted onto it),
      which the controller cannot safely arbitrate across namespaces.
      Namespaced also matches the existing `TrafficManagerProfile`
      scoping (parity), and the traffic-isolation property is
      per-flow so nothing about the Private-Link backbone story
      requires a shared cluster-wide profile. Truly shared AFD
      profiles remain an operator/IaC concern outside fleet-networking.
      - Impact: `+kubebuilder:resource:scope=Namespaced` (already the
        working assumption in Proposal 002 §3.1), CRD manifest,
        namespaced RBAC in
        `charts/hub-net-controller-manager/templates/rbac.yaml`.
      - Owner: @rchinchani_microsoft
      - Decision: **Namespaced** (2026-07-17)

- [x] **1.2 WAF policy required at SKU level?** — **Resolved:
      required when `spec.complianceMode == SFI-NS253`, optional
      otherwise.** The SKU-based gate collapsed once §2.3 of Proposal
      001 scoped the feature to Premium only. Instead of a hidden
      annotation, `FrontDoorProfileSpec` gains an explicit
      `complianceMode: None | SFI-NS253` field (default `None`,
      immutable). When `SFI-NS253`, a CEL rule requires
      `spec.wafPolicy` and the `FrontDoorBackend` reconciler
      additionally requires `spec.privateLink.enabled = true` on every
      referencing backend (surfaced as
      `Accepted=False, Reason=SFIComplianceViolation`). This keeps
      third-party dev/test paths permissive while making SFI intent
      a first-class, kubectl-discoverable, mistype-safe field, and
      lets a single Spec field drive both profile-side and
      backend-side enforcement. Proposal 002 §3.1 and §4.2 updated
      accordingly.
      - Owner: @rchinchani_microsoft
      - Decision: **Explicit Spec field, default None, immutable**
        (2026-07-17)

- [x] **1.3 Custom domains in phase 2 or phase 5?** — **Resolved:
      Phase 2, as a separate CRD.** cb02d14 landed
      `FrontDoorCustomDomain` (`afdcd`) alongside `FrontDoorProfile`
      rather than growing `FrontDoorProfileStatus` with a
      `CustomDomains []` list. This keeps DNS-validation lifecycle
      (Pending → Approved) and TLS binding (Managed today; BYOC
      reserved) in a dedicated reconciler with its own finalizer,
      rather than complicating the profile controller. Route binding
      (attaching a validated custom domain to an AFD route) waits
      for `FrontDoorBackend` in Phase 4. Proposal 001 §4.1.3 and
      Proposal 002 §3.3 describe the shipped shape.
      - Owner: @rchinchani_microsoft
      - Decision: **Separate CRD in Phase 2** (2026-07-19,
        commit cb02d14)

- [ ] **1.4 Umbrella `GlobalLoadBalancer` CRD later?** — if yes,
      invest in shared abstractions in `pkg/common/globalload/` from
      day one. If no, keep AFD and ATM helpers strictly separate.
      - Impact: package layout of `pkg/common/azurefrontdoor/`;
        naming of shared types.
      - Owner: —
      - Decision: —

- [x] **1.5 Adding `Spec` to `ServiceExport`** — **Resolved: no
      schema change.** The AFD path uses an annotation
      (`networking.fleet.azure.com/export-mode: L7-FrontDoor |
      L4-TrafficManager`) following the existing
      `networking.fleet.azure.com/weight` precedent, and the member
      controller additionally infers `L7-FrontDoor` from the
      Service's internal-LB + PLS annotations when the annotation
      is unset. Precedence: annotation wins when set; a mismatch
      between annotation and Service surfaces
      `ServiceExportValid=False,
      Reason=ExportModeAnnotationServiceMismatch` (no silent
      fallback). This preserves upstream mcs-api (KEP-1645) parity
      for `ServiceExport` / `MultiClusterService`, which is a
      repository preference. Proposals 001 §3.3 / §4.2 and 002 §2.1
      / §3.4 / §4.3 / §8 updated accordingly.
      - Owner: @rchinchani_microsoft
      - Decision: **Annotation + inference; no `Spec` change**
        (2026-07-20)

- [x] **1.6 AKS Automatic as a supported member cluster SKU** —
      **Resolved: yes, first-class alongside AKS Standard.** The AFD
      + PLS data plane depends only on cloud-provider-managed
      annotations (Standard SKU LB, PLS, `azure-pls-*`), which are
      available identically on both SKUs. Two operational caveats
      apply: (a) member cluster VNet/subnet layout must be planned
      up-front on Automatic (BYO VNet at create-time; PLS subnet
      MUST have `privateLinkServiceNetworkPolicies: Disabled`), and
      (b) the hub and member Helm charts MUST satisfy AKS Automatic
      Deployment Safeguards. Neither is Automatic-specific in the
      sense of requiring a code branch — the safeguards-clean chart
      is also the correct chart for AKS Standard. Recorded in
      Proposal 001 §3.4 and Proposal 002 §6.5.
      - Owner: @rchinchani_microsoft
      - Decision: **Supported; see 3.7 spike for validation**
        (2026-07-20)

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
- [x] **2.4 Security review of identity split** — Proposal 001 §7
      requires a separate managed identity for AFD vs. ATM.
      **Structural blocker uncovered by cb02d14:** the POC hosts
      both AFD and ATM controllers in the same pod, so today they
      necessarily share one Workload-Identity federated subject —
      Kubernetes does not allow two federated identities per pod
      (the projected-token path is a pod-level attribute).
      Satisfying §7 requires a sibling binary
      (`cmd/hub-afd-controller-manager`) + sibling chart
      (`charts/hub-afd-controller-manager`). Docs updated to
      describe this end state (Proposal 001 §6, §7; Proposal 002
      §2.2, §2.6). Actual code split is a follow-up commit, gated on
      security-reviewer sign-off before Phase 5 (GA).
      - Impact: adds one new binary, one new chart, one new Docker
        image, corresponding CI wiring. No changes required to the
        controller packages themselves.
      - Owner: @rchinchani_microsoft (docs); TBD (code split)
      - Decision: **Sibling binary+chart is a hard GA prerequisite;
        POC bridge uses a shared WI subject and is explicitly
        non-production** (2026-07-20)

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

- [x] **3.2 `armcdn` SDK compatibility with pinned `azure-sdk-for-go`** —
      **Resolved by cb02d14.**
      `github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn v1.1.1`
      was pinned and `go mod tidy` / `go build ./...` succeed
      alongside the existing `sigs.k8s.io/cloud-provider-azure`
      pin. No dependency conflict.
      - Owner: @rchinchani_microsoft
      - Result: **compatible** (2026-07-19, commit cb02d14)

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

- [x] **3.6 Interaction with existing `azcloudconfig` in charts** —
      **Resolved: obsoleted by the Workload-Identity + sibling-chart
      decision.** cb02d14 picked Azure AD Workload Identity for AFD
      auth (env vars `AZURE_TENANT_ID`, `AZURE_CLIENT_ID`,
      `AZURE_FEDERATED_TOKEN_FILE`, `AZURE_SUBSCRIPTION_ID`), not
      `azurecloudconfig.yaml`. Combined with the sibling-chart
      decision (§2.4 above), AFD gets its own ServiceAccount, its
      own WI federation, and its own values file — the ATM chart's
      `azurecloudconfig.yaml` remains untouched.
      - Owner: @rchinchani_microsoft
      - Result: **WI supersedes azcloudconfig for AFD; ATM chart
        unmodified** (2026-07-20)

- [ ] **3.7 AKS Automatic Deployment Safeguards install validation** —
      run `helm template charts/hub-net-controller-manager | kubectl
      apply --dry-run=server -f -` (or a `kubectl-safeguards` /
      equivalent policy-check tool offline) against the current chart
      output. Enumerate every Safeguards violation and confirm each
      one can be closed with a values-only or template-only change
      (no code changes). Fold the fixes into the §6.5.2 file list of
      Proposal 002 before Phase 4 starts.
      - Impact: sizing the chart hygiene work in Phase 4; may reveal
        that some hub/member containers need an `emptyDir` for `/tmp`
        or writable log paths, or that init containers pulling from
        Docker Hub have to be re-hosted in `mcr.microsoft.com` /
        the tenant ACR.
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
| Open design decisions closed | ⏳ (§1.4 only) |
| Maintainer review          | ⏳ (§2.1) |
| SFI sign-off               | ⏳ (§2.2) |
| **SFI identity split (sibling binary+chart)** | ⏳ **hard GA blocker (§2.4)** |
| SDK / cloud-provider spikes | ⏳ (§3.1, §3.3, §3.4) |
| Dev-sub ready              | ⏳ (§3.5) |
| AKS Automatic install validated | ⏳ (§3.7) |
| POC (cb02d14) reconciled with docs | ✅ (2026-07-20) |

**Recommendation:** start Phase 1 (API types + defaulters + CEL, no
controllers) *only after* §1.1, §1.2, §1.5, §2.1 are closed. Phases
2–4 additionally require §3.1–3.4 and §3.5. Phase 4 additionally
requires §3.7 (AKS Automatic Deployment Safeguards) to be run and
its findings folded into the chart hygiene work. **Phase 5 (GA)
additionally requires §2.4** — no SFI-NS253 install can be declared
until the sibling AFD binary+chart replace the current in-binary
bridge.

Update the checkboxes above as items close; when every box in §1–§3
is checked, Phase 2 can begin.
