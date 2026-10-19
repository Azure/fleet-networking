# GEP-4894 Backend Evaluation

## Requirements

- Evaluate GEP-4894's proposed Gateway API `Backend` resource and Kubernetes YAML shape.
- Compare it with `rchinchani/afd-global-ingress-rfc` at `bb6ed4e6ba859d4895c9647568e792905ff70038`.
- Compare it with `rchinchani/gep-1748-gateway-api` at `7bf9918ce41b10ae268a9acfdd21193cd92411ad`.
- Determine whether GEP-4894 can accomplish the same global ingress, multi-cluster backend, Azure Front Door, public origin, and Private Link requirements.
- Identify required API, controller, status, migration, conformance, and security changes.

## Additional comments from user

- Create any changes on a new PR branch under `rchinchani/*`.
- Working branch: `rchinchani/gep-4894-backend-evaluation`.
- Make the case for binding Fleet to the current GEP-4894 `selectorRef`
  proposal versus not binding to it now.

## Plan

### Phase 1: Establish the API baseline

- [x] **Task 1.1: Analyze GEP-4894.** Record the `Backend` types, namespace rules, inline TLS model, status contract, conformance level, and active upstream schema changes.
  - Success criteria: The evaluation distinguishes merged Experimental behavior from pending proposals.
- [x] **Task 1.2: Analyze the existing RFC.** Extract the global ingress goals, origin providers, connectivity modes, traffic controls, and provider-specific policy requirements.
  - Success criteria: Every material RFC requirement has a comparison target.
- [x] **Task 1.3: Analyze the GEP-1748 prototype.** Trace the `HTTPRoute` to Fleet `ServiceImport` to member-origin model and current controller foundation.
  - Success criteria: Existing public API and normalized model responsibilities are documented.

### Phase 2: Build the compatibility assessment

- [x] **Task 2.1: Map portable concepts.** Compare `Backend` with `ServiceImport`, `FleetBackendPolicy`, and the internal normalized backend model.
  - Success criteria: The assessment identifies direct fits, adapters, and semantic mismatches.
- [x] **Task 2.2: Evaluate end-to-end scenarios.** Test the design conceptually against public direct Service, private direct Service with PLS, shared cluster Gateway, active-active, active-passive, and cross-namespace scenarios.
  - Success criteria: Each scenario has a clear feasible, conditional, or unsupported result.
- [x] **Task 2.3: Evaluate operational contracts.** Compare status, ownership, migration, security, conformance, and failure behavior.
  - Success criteria: No product requirement is treated as solved solely by a compatible YAML shape.

### Phase 3: Document the recommendation

- [x] **Task 3.1: Update the applicable design document.** Add a concise GEP-4894 evaluation and recommended target architecture without prematurely committing to an unstable upstream field.
  - Success criteria: The RFC clearly states what GEP-4894 replaces, what remains Fleet-specific, and the adoption gates.
- [x] **Task 3.2: Update related implementation guidance if needed.** Align the GEP-1748 implementation direction with an additive `Backend` adapter path.
  - Success criteria: Existing `ServiceImport` behavior remains compatible and migration avoids dual programming.
- [x] **Task 3.3: Complete the breadcrumb.** Record decisions, implementation details, changed files, before/after comparison, references, and any course corrections.
  - Success criteria: The breadcrumb is an accurate review trail for the resulting PR.

### Detailed checklist

- [x] Phase 1 / Task 1.1 complete.
- [x] Phase 1 / Task 1.2 complete.
- [x] Phase 1 / Task 1.3 complete.
- [x] Phase 2 / Task 2.1 complete.
- [x] Phase 2 / Task 2.2 complete.
- [x] Phase 2 / Task 2.3 complete.
- [x] Phase 3 / Task 3.1 complete.
- [x] Phase 3 / Task 3.2 complete; the standalone evaluation records the
  required additive adapter direction without modifying either unmerged source
  branch.
- [x] Phase 3 / Task 3.3 complete.

### Overall success criteria

- The recommendation answers whether GEP-4894 can meet the same goals and under which conditions.
- The design preserves Fleet multi-cluster endpoint aggregation and Azure Front Door-specific behavior.
- The proposal avoids depending on an unmerged `selectorRef` contract.
- Existing GEP-1748 `ServiceImport` routes have a safe, explicit compatibility and migration path.

## Decisions

- Treat GEP-4894 as an additive consumer-side connection resource, not as a replacement for MCS `ServiceImport`.
- Preserve `ServiceImport` as the logical multi-cluster endpoint aggregation contract.
- Preserve Fleet/Azure configuration for origin provider, public versus Private Link connectivity, placement, per-cluster priority and weight, AFD SKU, WAF, diagnostics, and Azure ownership.
- Do not ship a user-facing dependency on `Backend.spec.endpointSelector.selectorRef` while Gateway API PR 5158 proposes removing it and the referenced upstream `EndpointSelector` API does not exist.
- Present early binding as a viable option rather than dismissing it: it can
  accelerate a coherent Backend-first API, generate upstream implementation
  feedback, and reduce later user migration if the shape survives.
- Evaluate the decision by API compatibility, implementation cost, upstream
  influence, conformance truthfulness, migration risk, and delivery schedule.
- Keep the existing provider-neutral normalized model as the convergence point for direct `ServiceImport` references and any future `Backend` references.
- Document the evaluation in a standalone design note because both source design documents live on separate unmerged branches.

## Implementation Details

- GEP-4894 `Backend` directly covers consumer-side protocol and backend TLS configuration.
- `ExternalHostname` covers one external FQDN but not a dynamic, weighted set of Fleet member origins.
- `EndpointSelector` currently lacks a stable binding that can represent a Fleet `ServiceImport` on the hub.
- GEP-4894 requires a `Backend` to share a namespace with its Route, while GEP-1748 permits direct cross-namespace `ServiceImport` references through `ReferenceGrant`.
- A future integration requires either a stable upstream endpoint-selection resource derived from `ServiceImport`, or a separately standardized extension point. Fleet must not reinterpret `selectorRef` as a direct `ServiceImport` reference.
- The decision record distinguishes an experimental incubation implementation
  from a customer-facing API commitment. Fleet may implement the current field
  behind a disabled-by-default gate to generate upstream evidence while
  retaining direct `ServiceImport` references as the supported path.

## Changes Made

- Created this breadcrumb on `rchinchani/gep-4894-backend-evaluation`.
- Added `docs/design/gep-4894-backend-evaluation.md`.
- Documented requirement mapping, scenario feasibility, target architecture,
  migration, status, security, and adoption gates.
- Added a balanced decision analysis for binding to the current `selectorRef`
  versus deferring, with a comparative scorecard and bounded-incubation option.

## Before/After Comparison

Before this evaluation, the AFD RFC treated Gateway API and GEP-1748 as a
future option, while the GEP-1748 branch directly referenced Fleet
`ServiceImport`. Neither branch evaluated GEP-4894.

After this evaluation, the proposed architecture has explicit responsibility
boundaries:

- `Backend` owns consumer protocol and origin TLS.
- `ServiceImport` owns multi-cluster endpoint aggregation.
- Fleet/Azure policy owns AFD origin topology and provider lifecycle.
- Both current and future API paths converge on one normalized model.
- The current `selectorRef` can be incubated experimentally but is not
  recommended as a production Fleet API commitment.

## References

- GEP-4894: <https://gateway-api.sigs.k8s.io/geps/gep-4894/> (Experimental)
- GEP-1748: <https://gateway-api.sigs.k8s.io/geps/gep-1748/> (Experimental)
- Gateway API PR 5158: <https://github.com/kubernetes-sigs/gateway-api/pull/5158> (open; proposes deferring `selectorRef`)
- `rchinchani/afd-global-ingress-rfc:docs/design/afd-global-ingress-rfc.md`
- `rchinchani/gep-1748-gateway-api:docs/design/gep-1748-gateway-api.md`
- `rchinchani/gep-1748-gateway-api:docs/design/gep-1748-implementation-plan.md`
- `rchinchani/gep-1748-gateway-api:docs/howtos/gateway-api-afd-configuration.md`
- `rchinchani/gep-1748-gateway-api:pkg/controllers/hub/gatewaymodel/model.go`
