# AFD Export-Mode — MCS-API Parity

## Requirements

Update the AFD proposal (`docs/first-party/001..003`) so it no longer
adds a Fleet-specific `Spec.ExportMode` field to `ServiceExport`.
That divergence conflicts with the repository preference to keep
`ServiceExport` / `MultiClusterService` structurally aligned with
upstream mcs-api (KEP-1645) and to carry Fleet-specific configuration
via annotations.

Adopt a two-signal model instead:

1. **Primary — inference from the `Service`.** The member controller
   determines that an exported Service is destined for the AFD
   (private-origin) path by observing the Azure cloud-provider
   annotations already required to create an internal LB and a PLS
   (`service.beta.kubernetes.io/azure-load-balancer-internal: "true"`
   and `service.beta.kubernetes.io/azure-pls-create: "true"`).
   No CR schema change; the mode is emergent from the Service's
   actual shape.
2. **Fallback / opt-in intent flag — annotation on `ServiceExport`.**
   `networking.fleet.azure.com/export-mode: L7-FrontDoor | L4-TrafficManager`
   matches the existing `networking.fleet.azure.com/weight`
   precedent and lets tenants declare intent independently of the
   Service's current annotation state.

Precedence when both are present: **annotation wins** if set;
otherwise infer from the Service. Conflict (annotation says
`L7-FrontDoor` but Service lacks internal+PLS annotations)
surfaces `ServiceExportValid=False` with a new
`Reason=ExportModeAnnotationServiceMismatch`.

## Additional comments from user

- Stored preference (user memory): *"In fleet-networking, prefer no
  divergence from upstream mcs-api (KEP-1645) shapes for
  ServiceExport/MultiClusterService; carry Fleet-specific config via
  annotations, not new Spec fields."*
- Precedence rule chosen by user in this session:
  *"Annotation wins if set; otherwise infer from Service"* — see
  `ask_user` turn on 2026-07-20.

## Plan

Docs-only change; no code yet (proposal not implemented).

1. `docs/first-party/001-afd-global-load-balancing.md`
   - §3.3 (member-controller behaviour): reword to describe
     inference-based detection, and add optional annotation as the
     explicit-intent surface. Remove the language that says the
     controller "projects" annotations onto the Service — the tenant
     (or platform admission policy) owns the Service annotations;
     the controller only observes them.
   - §4.2 (additive CRD changes): drop the `Spec.ExportMode`
     bullet on `ServiceExport`; keep only the
     `InternalServiceExport.Status.PrivateLinkService` addition.
     Add a sentence about the annotation-based opt-in and the
     precedence rule.
2. `docs/first-party/002-afd-implementation-plan.md`
   - §2.1 (file table): remove the two `serviceexport_types.go`
     modification rows (v1alpha1 and v1beta1). Keep the
     `internalserviceexport_types.go` rows.
   - §3.3: replace the `ExportMode` Go sketch with an "Annotation
     constants" sketch under `pkg/common/objectmeta/` (or wherever
     the `weight` annotation lives) and a short note that no
     `ServiceExport` schema change is required.
   - §4.3 (member `serviceexport` extension): rewrite the
     additions to:
     - Read the annotation first; if unset, infer from the
       Service's internal-LB + PLS annotations.
     - Validate presence/absence of the required Service
       annotations; if the annotation demands AFD but the Service
       is not internal+PLS, surface the mismatch and do not mutate
       the Service.
     - Copy the AKS-programmed PLS resource ID into
       `InternalServiceExport.status.privateLinkService`.
     - Explicitly state that the member controller does **not**
       write to the Service (annotations are tenant-owned).
   - §8 (east-west compatibility): update the tables/prose that
     currently mention `Spec.ExportMode` default behaviour to
     describe the annotation/inference model instead.
   - §9 (risks): add a row about
     "annotation says AFD, Service lacks PLS annotations"
     precedence and validation.
3. `docs/first-party/003-pre-implementation-checklist.md`
   - Close open item **1.5** with the decision documented above.
   - No new open items — the precedence and mismatch handling
     are settled by this update.

## Decisions

- **No new `Spec` field on `ServiceExport`.** Preserves upstream
  mcs-api parity per the repo preference.
- **Primary signal: Service shape.** The Service's internal-LB and
  PLS annotations are the physical prerequisite for AFD anyway; using
  them as the signal collapses two sources of truth into one.
- **Opt-in annotation on `ServiceExport`.** Kept as a tenant-visible
  intent flag for platforms that want to declare mode before the
  Service is fully provisioned (e.g., GitOps pipelines that apply
  the `ServiceExport` in one wave and the `Service` in the next).
- **Annotation wins when both are set.** Explicit intent overrides
  observed shape; mismatches surface a validation condition.
- **Controller does not mutate tenant-owned Services.** The proposal
  originally described "projecting" annotations onto the Service; that
  crosses ownership lines and complicates ATM ↔ AFD transitions.
  Ownership stays with the tenant / GitOps / admission policy.

## Implementation Details

See per-file edits applied to `001`, `002`, `003` under
`docs/first-party/`. This breadcrumb accompanies those edits — no
code changes in this pass.

## Changes Made

- `docs/first-party/001-afd-global-load-balancing.md` — §3.3 and
  §4.2 reworded.
- `docs/first-party/002-afd-implementation-plan.md` — §2.1, §3.3,
  §4.3, §8, §9 updated.
- `docs/first-party/003-pre-implementation-checklist.md` — item
  1.5 marked resolved.

## Before/After Comparison

| Aspect | Before | After |
|---|---|---|
| ServiceExport schema | Grew a new `Spec.ExportMode` enum | Unchanged — matches upstream mcs-api |
| Mode signal (primary) | Explicit `Spec.ExportMode` field | Inferred from Service's `azure-load-balancer-internal` + `azure-pls-*` annotations |
| Mode signal (opt-in) | n/a | `networking.fleet.azure.com/export-mode` annotation on `ServiceExport` |
| Precedence | n/a | Annotation wins if set; else inference; mismatch surfaces validation condition |
| Member controller writes to Service? | Yes (annotation projection) | No — tenants/GitOps own Service annotations |

## References

- KEP-1645 (Multi-Cluster Services API): <https://github.com/kubernetes/enhancements/tree/master/keps/sig-multicluster/1645-multi-cluster-services-api>
- Upstream mcs-api reference implementation: <https://github.com/kubernetes-sigs/mcs-api>
- Proposal 001: `docs/first-party/001-afd-global-load-balancing.md` (Draft)
- Proposal 002: `docs/first-party/002-afd-implementation-plan.md` (Draft)
- Checklist 003: `docs/first-party/003-pre-implementation-checklist.md` (Open)
- Existing Fleet-specific annotation precedent: `networking.fleet.azure.com/weight` on `ServiceExport` (see `pkg/common/objectmeta`).
- AKS internal LB annotations: <https://learn.microsoft.com/azure/aks/internal-lb>
- AKS PLS annotations: <https://learn.microsoft.com/azure/aks/private-link-service>
