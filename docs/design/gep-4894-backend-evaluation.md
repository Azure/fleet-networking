# GEP-4894 Backend evaluation for Fleet global ingress

## Document status

| Field | Value |
|---|---|
| Status | Evaluation |
| Date | 2026-08-25 |
| GEP-4894 status | Experimental |
| AFD RFC revision | `bb6ed4e6ba859d4895c9647568e792905ff70038` |
| GEP-1748 branch revision | `7bf9918ce41b10ae268a9acfdd21193cd92411ad` |

## Executive recommendation

GEP-4894 can contribute to the Fleet global ingress API, but it cannot
currently replace either GEP-1748 `ServiceImport` integration or the
Fleet/Azure backend policy described in the AFD global ingress RFC.

The resources have different responsibilities:

- Gateway API `Backend` describes how one Gateway client connects to a
  destination.
- Fleet `ServiceImport` describes one logical Service whose endpoints span
  member clusters.
- Fleet/Azure configuration describes how those member endpoints become AFD
  origins, including public or Private Link connectivity, health probes,
  placement, priority, weight, and provider-specific lifecycle.

The recommended target is an additive model:

```text
HTTPRoute
  -> Backend                         consumer connection contract
     -> stable endpoint binding      future upstream integration point
        -> Fleet ServiceImport       logical multi-cluster Service
           -> member export state    public endpoint or PLS per cluster
              -> AFD origin group    provider reconciliation
```

Fleet should retain direct `HTTPRoute` references to `ServiceImport` until
Gateway API defines a stable endpoint binding that can represent a
multi-cluster endpoint producer. Both the current path and a future `Backend`
path should normalize into the existing provider-neutral internal model.

## Sources evaluated

This evaluation compares:

- GEP-4894, which introduces the Experimental `Backend` resource.
- GEP-1748, which defines Experimental and Extended support for using MCS
  `ServiceImport` as a Gateway API backend.
- `rchinchani/afd-global-ingress-rfc`, which defines the product requirements,
  origin models, security model, and candidate Fleet/Azure policy APIs.
- `rchinchani/gep-1748-gateway-api`, which defines and partially implements a
  Fleet AFD Gateway controller based on direct `ServiceImport` references.
- Gateway API PR 5158, which proposes removing
  `Backend.spec.endpointSelector.selectorRef` for now because its upstream
  `EndpointSelector` dependency does not exist.

GEP-4894 is not a stable API contract. In particular, its endpoint-selection
shape is changing while this evaluation is being written. Fleet must treat the
GEP as design input rather than a dependency baseline.

## What GEP-4894 provides

GEP-4894 proposes a namespace-scoped, consumer-owned `Backend` with two
destination types:

| Type | Purpose | Conformance |
|---|---|---|
| `EndpointSelector` | Select internal endpoints and attach connection configuration | Core |
| `ExternalHostname` | Represent one external FQDN without an `ExternalName` Service | Extended |

The resource also provides:

- an explicit backend port;
- protocol metadata;
- inline server TLS validation;
- an optional client certificate for mutual TLS;
- parent-scoped status; and
- a future attachment point for retries, session persistence, timeouts, load
  balancing, and health checks.

GEP-4894 requires the `Backend` and referring Route to be in the same
namespace. The current draft allows an `EndpointSelector` backend to refer to a
producer-side endpoint selector across namespaces, but that field is the
subject of active upstream revision.

## Existing Fleet global ingress model

The evaluated GEP-1748 design uses:

```text
Gateway
  -> HTTPRoute
     -> Fleet ServiceImport
        -> InternalServiceExport records
           -> one origin per eligible member cluster
```

The `ServiceImport` backend maps to an AFD origin group. Each contributing
member export maps to an AFD origin. The current normalized model preserves two
different traffic controls:

- `HTTPRoute.backendRefs[*].weight` distributes traffic between logical
  backends.
- Fleet `ServiceExport` weight distributes traffic between member-cluster
  origins inside one logical backend.

The AFD RFC additionally requires:

- `DirectService` and `ClusterGateway` origin providers;
- public and Private Link connectivity;
- active-active and active-passive placement;
- per-cluster priority and weight overrides;
- health probe configuration;
- AFD Standard and Premium selection;
- WAF and diagnostics;
- certificate and custom-domain lifecycle;
- Azure ownership, deletion, and drift rules;
- PLS discovery, region selection, and approval state; and
- detailed origin programming and health status.

These requirements are destination topology and provider lifecycle concerns.
They are not equivalent to client protocol or TLS configuration.

## Compatibility assessment

| Requirement | GEP-4894 fit | Result |
|---|---|---|
| Route references an explicit backend object | Direct | Stronger discoverability than policy applied to `ServiceImport` |
| Consumer-specific backend TLS | Direct | Inline `Backend.spec.tls` is a good fit |
| Backend protocol metadata | Direct | `Backend.spec.protocol` is a good fit |
| One external FQDN | Direct | `ExternalHostname` is a good fit |
| Existing cluster-local Service decoration | Intended, but endpoint binding is unstable | Conditional |
| Fleet `ServiceImport` endpoint aggregation | Not defined | Gap |
| Dynamic member-cluster origins | Not defined | Gap |
| Public versus Private Link origin connectivity | Not defined | Fleet/Azure extension remains required |
| Direct Service versus shared cluster Gateway origin | Not defined | Fleet/Azure extension remains required |
| Per-cluster priority and weight | Not defined | Fleet placement policy remains required |
| AFD health probe contract | Only identified as a future Backend field | Fleet/Azure extension remains required |
| AFD SKU, WAF, diagnostics, resource ownership | Out of scope | Gateway-level Azure policy remains required |
| Frontend listener certificate | Different TLS direction | Gateway listener and AFD certificate design remain required |
| Parent-scoped backend status | Partial | Useful for connection readiness, insufficient for member-origin detail |
| GEP-1748 Extended conformance | Independent | Supporting `Backend` does not provide multi-cluster conformance |

## Important semantic distinctions

### Backend is not ServiceImport

`Backend` is consumer-side connection intent. `ServiceImport` is
multi-cluster service discovery and endpoint aggregation. Replacing
`ServiceImport` with `Backend` would remove the source of:

- contributing member-cluster identity;
- member eligibility;
- exported ports;
- per-cluster endpoint readiness;
- per-cluster weight;
- public load balancer state; and
- PLS resource ID, location, and readiness.

Fleet still needs `ServiceImport` and internal export state even if users
eventually reference a `Backend` from `HTTPRoute`.

### Inline backend TLS is not listener TLS

GEP-4894 TLS controls the connection from AFD to an origin. Gateway listener
TLS controls the connection from the client to AFD. The latter still requires
AFD custom-domain validation and an AFD-managed or Key Vault certificate
lifecycle.

The two directions must remain independently configurable:

```text
client -- listener TLS --> AFD -- Backend TLS --> member origin
```

### ExternalHostname is not a multi-cluster origin set

`ExternalHostname` represents one destination hostname. A Fleet backend is a
logical destination containing a dynamic set of member-cluster origins, each
with independent health, priority, weight, connectivity, and lifecycle.

Creating one `ExternalHostname` Backend per cluster would expose cluster
membership in application Routes, force Route churn when membership changes,
and conflate route-level weights with per-cluster placement. It would also not
represent Private Link resource identity. This is not a recommended mapping.

## Kubernetes YAML shape

### Current GEP-1748 Fleet shape

The current design is implementable with the existing Fleet APIs:

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: api
  namespace: contoso
spec:
  parentRefs:
  - name: contoso-global
  rules:
  - backendRefs:
    - group: networking.fleet.azure.com
      kind: ServiceImport
      name: api
      port: 443
```

Fleet resolves `ServiceImport/contoso/api` into member origins and the AFD
provider receives one normalized logical backend.

### GEP-4894 shape for a single external destination

The `ExternalHostname` case is independently useful and does not require
multi-cluster discovery:

```yaml
apiVersion: gateway.networking.k8s.io/v1alpha1
kind: Backend
metadata:
  name: partner-api
  namespace: contoso
spec:
  type: ExternalHostname
  externalHostname:
    hostname: api.partner.example
  port: 443
  protocol: HTTP2
  tls:
    mode: ServerOnly
    validation:
      wellKnownCACertificates: System
---
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: partner-api
  namespace: contoso
spec:
  parentRefs:
  - name: contoso-global
  rules:
  - backendRefs:
    - group: gateway.networking.k8s.io
      kind: Backend
      name: partner-api
      port: 443
```

The exact Experimental fields may change. This example must not be treated as
a stable Fleet API commitment.

### Future multi-cluster shape

The desired user experience is:

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: api
  namespace: contoso
spec:
  parentRefs:
  - name: contoso-global
  rules:
  - backendRefs:
    - group: gateway.networking.k8s.io
      kind: Backend
      name: api
      port: 443
---
apiVersion: gateway.networking.k8s.io/v1alpha1
kind: Backend
metadata:
  name: api
  namespace: contoso
spec:
  type: EndpointSelector
  port: 443
  protocol: HTTP
  tls:
    mode: ServerOnly
  endpointSelector:
    selectorRef:
      name: api
      namespace: contoso-backends
```

This shape is only viable if a stable upstream endpoint-selection resource can
represent endpoints derived from Fleet `ServiceImport/contoso-backends/api`.
The Fleet controller would own the translation from `ServiceImport` and
internal export state to that endpoint resource.

It is not viable today because:

1. the referenced upstream `EndpointSelector` API does not exist;
2. Gateway API PR 5158 proposes deferring `selectorRef`;
3. the proposed embedded pod selector cannot select member workloads from the
   Fleet hub; and
4. a direct `selectorRef` to Fleet `ServiceImport` would violate the current
   GEP semantics.

Fleet must not reinterpret the flexible object-reference syntax as permission
to put `kind: ServiceImport` in `selectorRef`.

## Scenario evaluation

| Scenario | Feasibility with GEP-4894 | Required design |
|---|---|---|
| Direct public Service in one cluster | Conditional | Stable Service or endpoint binding plus AFD provider mapping |
| Direct public Services across Fleet members | Conditional | Retain `ServiceImport` aggregation behind a stable endpoint binding |
| Direct private Services with PLS | Conditional | Retain Fleet PLS discovery and Azure connectivity policy |
| Shared public cluster Gateway | Conditional | Retain `ClusterGateway` provider selection and local Gateway orchestration |
| Shared private cluster Gateway with PLS | Conditional | Same as shared public plus PLS lifecycle |
| One external Internet hostname | Feasible | Implement `ExternalHostname` Extended support |
| Active-active multi-region | Conditional | Retain Fleet placement, priority, weight, and health semantics |
| Active-passive multi-region | Conditional | Retain explicit origin priority and failover semantics |
| Cross-namespace producer backend | Blocked on stable endpoint binding | Route-local `Backend`; producer-side endpoint object with authorization |
| Existing direct ServiceImport Route | Feasible now | Keep GEP-1748 path during migration |

No required global ingress scenario becomes impossible because of GEP-4894.
However, only the single external-hostname and consumer TLS/protocol scenarios
are directly solved by it. The core Fleet multi-cluster scenarios remain
conditional on a separate endpoint aggregation and provider policy layer.

## Recommended target architecture

### Public resources

Use portable Gateway API resources for portable intent:

- `GatewayClass` selects the Fleet AFD implementation.
- `Gateway` defines listeners, hostnames, and route delegation.
- `HTTPRoute` defines HTTP matching, filters, and logical backend weights.
- `Backend`, after its API is stable, defines consumer protocol and origin TLS.
- `ServiceExport` and `ServiceImport` define multi-cluster service discovery.

Use Fleet/Azure APIs only for non-portable behavior:

- AFD SKU, WAF, diagnostics, Azure placement, and ownership;
- origin provider and public or Private Link connectivity;
- PLS region and approval behavior;
- member placement, priority, and weight overrides; and
- detailed member-origin programming and health status.

### Controller layers

```text
Gateway API reconcilers
  - validate Gateway, HTTPRoute, Backend, and references
  - produce portable connection intent

Fleet backend resolver
  - resolve ServiceImport to eligible member exports
  - resolve public endpoint or PLS metadata
  - apply placement, priority, and weight

Normalized gateway model
  - preserve route weight separately from member-origin weight
  - carry protocol and backend TLS
  - carry provider connectivity and health settings

AFD provider
  - reconcile profile, endpoint, route, origin group, origins, Private Link,
    WAF, domains, certificates, and diagnostics
```

The normalized model on `rchinchani/gep-1748-gateway-api` is the correct
convergence boundary, but it must eventually add explicit fields for backend
protocol, backend TLS, host header, health probe details, priority, and
provider-specific origin metadata.

## Migration strategy

### Phase 1: Preserve the implementable path

- Continue supporting direct Fleet `ServiceImport` backend references.
- Complete public and Private Link origin resolution.
- Keep provider configuration on the current typed annotation or candidate
  Fleet policy surface.
- Do not claim GEP-4894 conformance.

### Phase 2: Add Backend where it is independent

- Add Experimental `ExternalHostname` support behind a feature gate.
- Add protocol and backend TLS fields to the normalized model.
- Publish the exact supported GEP-4894 revision and conformance tier.
- Reject unsupported fields rather than silently ignoring them.

### Phase 3: Add multi-cluster Backend binding

Proceed only after an upstream endpoint-selection contract can represent
controller-produced multi-cluster endpoints.

- Materialize or manage the stable upstream endpoint resource from
  `ServiceImport`.
- Require the Route and `Backend` to share a namespace.
- Authorize producer-side cross-namespace endpoint references with the
  upstream mechanism.
- Resolve both direct `ServiceImport` and `Backend` paths into the same
  normalized identity.

### Phase 4: Migrate with dual-read, single-write

- Accept both direct `ServiceImport` and new `Backend` references.
- Detect and reject configurations where both API paths claim the same AFD
  route/backend attachment.
- Preserve Azure resource identity and ownership tags.
- Program Azure from only one selected source.
- Report migration status before removing the old reference.
- Retain rollback until status, traffic behavior, and Azure ownership are
  verified.

## Decision analysis: bind to the current `selectorRef` or defer

This decision is not simply "upstream is Experimental, therefore wait."
Binding now has meaningful product and upstream advantages. The choice depends
on whether Fleet is willing to treat the YAML as a disposable preview contract.

### Option A: bind Fleet to the current `selectorRef`

Under this option, Fleet adopts the current GEP-4894 shape in a preview:

```yaml
spec:
  type: EndpointSelector
  endpointSelector:
    selectorRef:
      group: networking.fleet.azure.com
      kind: ServiceImport
      name: api
  port: 443
```

Because the current GEP text defines `selectorRef` as a reference to an
`EndpointSelector`, using `ServiceImport` would be an explicit Fleet extension.
Fleet could alternatively introduce an adapter resource that implements the
expected endpoint-selection role and is populated from `ServiceImport`.

#### Case for binding now

1. **The conceptual boundary is right for Fleet**

   A Route-local `Backend` describes the consumer's connection contract while
   a producer-side reference supplies endpoints. That separation matches
   Fleet's need to keep protocol and TLS distinct from multi-cluster endpoint
   aggregation.

2. **Fleet supplies a real implementation test that upstream lacks**

   Fleet has dynamic endpoints, cross-namespace consumers, multiple regions,
   and provider-specific origin metadata. Implementing the proposal would
   expose whether `selectorRef`, status ownership, and authorization work for
   more than a cluster-local Service.

3. **Early adoption can influence the standard**

   A working implementation and conformance proposal carry more weight than a
   design-only request. Fleet could use concrete findings to influence the
   future EndpointSelector KEP and GEP-4894 rather than adapting after those
   decisions are closed.

4. **Users get the intended Backend-first experience sooner**

   Protocol and TLS live next to the destination, and Routes consistently
   reference `Backend` instead of mixing `ServiceImport` and `Backend` kinds.
   If the shape survives, Fleet avoids a later user migration from direct
   `ServiceImport` references.

5. **The controller architecture already has the right seam**

   The GEP-1748 prototype normalizes Kubernetes resources before programming
   AFD. A `Backend` resolver can be added as another input without rewriting
   the AFD provider.

6. **An alpha API is allowed to learn**

   If the feature is explicitly experimental, feature-gated, disabled by
   default, and excluded from compatibility guarantees, changing or removing
   `selectorRef` is an acceptable preview cost.

#### Requirements for responsibly binding now

Binding now is defensible only with all of these constraints:

- use the upstream Experimental API version or a clearly named Fleet
  experimental API, never a stable Fleet API version;
- place the feature behind a disabled-by-default gate;
- publish the exact Gateway API commit implemented;
- state that `selectorRef` to `ServiceImport` is a Fleet extension and does not
  provide GEP-4894 conformance;
- keep direct `ServiceImport` references supported as the stable path;
- normalize both paths into the same internal backend identity;
- reject simultaneous claims from both paths instead of programming twice;
- add conversion or migration tooling before changing the preview schema;
- prohibit automatic field pruning during a Gateway API CRD upgrade;
- define authorization and status ownership locally rather than leaving them
  implicit; and
- accept that preview objects may require user action to migrate.

With these controls, early binding is a calculated upstream incubation
investment, not a production API commitment.

### Option B: do not bind Fleet to `selectorRef` now

Under this option, Fleet continues to expose direct `ServiceImport`
`backendRef`s and prepares the internal model for a future `Backend` adapter.
Fleet may implement the independent `ExternalHostname`, protocol, or TLS
features only after choosing a pinned Experimental revision.

#### Case for deferring

1. **Upstream is actively removing the field**

   Gateway API PR 5158 proposes deferring `selectorRef`, retaining only an
   embedded namespace-local pod selector, and adding `selectorRef` later when
   the upstream EndpointSelector resource exists. This is a direct signal that
   the current field has not reached design consensus.

2. **The referenced resource does not exist**

   The current GEP describes `selectorRef` as pointing to an upstream
   `EndpointSelector`, but that API is still being pursued through KEP-6116.
   The pending GEP update states Kubernetes 1.38 is the earliest target and
   notes Gateway API's GA+5 dependency policy. Fleet would be binding to a
   relationship whose target contract is unknown.

3. **`ServiceImport` is not an EndpointSelector**

   A `ServiceImport` carries logical multi-cluster Service semantics, ports,
   cluster membership, and aggregation status. Treating it as an
   EndpointSelector because `selectorRef` uses a generic object reference
   would be syntax-compatible but semantically incompatible with the current
   GEP.

4. **The pending replacement cannot serve Fleet**

   PR 5158's embedded label selector selects pods in the Backend namespace.
   Fleet's hub does not contain the member-cluster pods, so the proposed
   replacement cannot express Fleet's destination. Adopting it would create an
   API dead end rather than an incremental path.

5. **Authorization is unresolved**

   GEP-4894 currently requires the Route and Backend to share a namespace, but
   cross-namespace `selectorRef` authorization remains an open question. Fleet
   cannot safely infer that existing `ReferenceGrant` behavior applies.

6. **The abstraction does not reduce the hard implementation work**

   AFD still needs Fleet to resolve member exports, public endpoints, PLS
   metadata, eligibility, priority, weight, and health. Adding an unstable
   Backend-to-selector layer does not remove any of those control loops.

7. **CRD churn has operational cost**

   If upstream removes or changes `selectorRef`, upgrading the Experimental CRD
   can reject or prune stored fields. Fleet would need conversion, status
   migration, rollback, and user communication before it has delivered any
   additional AFD capability.

8. **Conformance cannot justify the cost**

   GEP-4894 and GEP-1748 are both Experimental, Fleet uses an
   implementation-specific `ServiceImport` API group, and there are no
   conformance tests that make a Fleet `selectorRef` extension portable.

Deferral protects the durable public API while leaving the controller design
ready to adopt the eventual standard.

### Comparative scorecard

| Criterion | Bind current `selectorRef` | Do not bind now |
|---|---|---|
| Backend-first user experience | Strong immediately | Delayed |
| Upstream implementation feedback | Strong | Limited to design feedback |
| Chance to influence the standard | Higher | Lower |
| Current semantic correctness | Low for direct `ServiceImport`; medium with an adapter | High |
| API stability | Low | High |
| Cross-namespace authorization clarity | Low | Existing GEP-1748 path is clear |
| Conformance value | Low | Neutral |
| New AFD capability unlocked | Little by itself | No loss |
| Migration burden | High if upstream changes | Lower |
| Time to production-ready public/private origins | Slower if coupled to adoption | Faster |
| Reversibility | Acceptable only as gated preview | High |

### Recommendation

Do not bind the production Fleet API to the current `selectorRef`.

The decisive point is not merely field instability: the current reference is
defined for an endpoint resource that does not exist, while a direct
`ServiceImport` target would be a Fleet-specific semantic fork. It does not
unlock public origins, Private Link, placement, or AFD programming, so the
migration cost is not justified on the critical delivery path.

Fleet should nevertheless pursue a **bounded incubation implementation** if
upstream influence is a priority:

1. keep the stable user path as direct `HTTPRoute` to `ServiceImport`;
2. add a disabled-by-default experimental resolver for the current
   `selectorRef`;
3. pin it to a Gateway API commit and make no compatibility promise;
4. use it to test status, authorization, and multi-cluster endpoint semantics;
5. upstream the findings and conformance cases; and
6. delete or migrate the experiment when GEP-4894 and EndpointSelector settle.

This separates two decisions that should not be conflated:

- **Should Fleet help validate `selectorRef` now?** Yes, potentially.
- **Should Fleet make the current shape its customer API now?** No.

## Status model

Standard Gateway API conditions remain authoritative where applicable:

- `GatewayClass Accepted`;
- `Gateway Accepted` and `Programmed`;
- listener conditions;
- `HTTPRoute Accepted` and `ResolvedRefs`; and
- GEP-4894 Backend parent conditions for connection configuration.

Fleet-specific status remains necessary for:

- each member cluster and region;
- origin eligibility and drain state;
- public or Private Link connectivity;
- PLS approval;
- Azure origin programming;
- origin health;
- active-active or active-passive readiness; and
- minimum healthy-origin and regional availability policy.

`Backend Available=True` or `Programmed=True` must not imply that every member
origin is healthy. The condition reason and Fleet origin summary must preserve
that distinction.

## Security and namespace implications

- A Route-local `Backend` improves consumer ownership and avoids ambiguous
  producer policy.
- Inline client certificate references must remain namespace-bound and must
  not expose secret material in status.
- `ExternalHostname` requires DNS trust, admission guardrails, egress network
  controls, and protection from confused-deputy targets.
- Fleet Private Link remains the preferred private-origin topology for
  SFI-NS253 requirements.
- A Backend must not let a namespace bypass `ServiceImport` authorization,
  Fleet membership, Azure policy, WAF requirements, or allowed origin
  connectivity.
- Cross-namespace direct `ServiceImport` references remain governed by
  `ReferenceGrant` while that API path exists.
- A future Route-local Backend to producer endpoint binding must use the
  standard upstream authorization model; Fleet must not invent implicit
  cross-namespace access.

## Adoption gates

Fleet should not make GEP-4894 part of its durable public contract until:

1. the `EndpointSelector` shape and Service binding are merged and versioned;
2. the binding can represent controller-produced endpoints from
   `ServiceImport`;
3. namespace authorization semantics are testable;
4. Gateway API publishes usable CRDs and conformance tests;
5. backend TLS and protocol semantics are sufficiently stable for AFD;
6. status ownership between the Backend controller and AFD Gateway controller
   is defined;
7. direct `ServiceImport` migration and rollback are tested; and
8. Fleet can publish which Core and Extended features it supports without
   implying upstream MCS conformance for its implementation-specific API
   group.

## Final decision

Adopt the GEP-4894 concepts, but do not replace the current GEP-1748
`ServiceImport` path now.

The long-term API should use `Backend` as the explicit consumer connection
object only after its endpoint binding is stable. `ServiceImport` remains the
multi-cluster destination source, and Fleet/Azure policy remains the source of
global origin topology and provider behavior. This layered design can satisfy
the same product goals while improving backend discoverability and per-consumer
TLS, without forcing unstable upstream YAML into the first implementation.

## References

- [GEP-4894: Backend Resource](https://gateway-api.sigs.k8s.io/geps/gep-4894/)
- [GEP-1748: Gateway API Interaction with Multi-Cluster Services](https://gateway-api.sigs.k8s.io/geps/gep-1748/)
- [Gateway API PR 4488: Experimental Backend Resource](https://github.com/kubernetes-sigs/gateway-api/pull/4488)
- [Gateway API PR 5158: Defer selectorRef](https://github.com/kubernetes-sigs/gateway-api/pull/5158)
- `rchinchani/afd-global-ingress-rfc:docs/design/afd-global-ingress-rfc.md`
- `rchinchani/gep-1748-gateway-api:docs/design/gep-1748-gateway-api.md`
- `rchinchani/gep-1748-gateway-api:docs/design/gep-1748-implementation-plan.md`
- `rchinchani/gep-1748-gateway-api:pkg/controllers/hub/gatewaymodel/model.go`
