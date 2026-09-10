# AFD GEP-1748 Scale and Performance Analysis

## Requirements

- Analyze the current PR branch implementing the GEP-1748 Gateway API foundation.
- Trace the intended end-to-end path across Gateway API, Fleet MCS resources, Azure Front Door,
  WAF, Private Link Service, internal Load Balancer, Service, ServiceExport, and workloads.
- Identify control-plane and data-plane scale limits, performance bottlenecks, failure storms,
  observability gaps, and mitigations.
- Distinguish findings in the current implementation from risks in planned but unimplemented
  reconciliation.
- Ground Azure limits and behaviors in current authoritative references.
- Create a separate scale and capacity planning document rather than expanding the architecture
  RFC with detailed operational limits.
- Start the document with a comprehensive table covering each independent platform and
  Kubernetes limit, its scope, the PR consumption model, failure behavior, headroom, and
  mitigation.
- Define the support envelope controlled by this PR in terms of Service, ServiceExport,
  ServiceImport, member-cluster, port, route, and resolved-origin cardinalities.
- Define when users should create another Gateway/AFD profile, another ingress shard, another
  cluster, another region, or another subscription.
- Analyze multitenant member clusters where several teams, applications, or customer groups share
  one AKS cluster but require independent global ingress, policy, quota, ownership, and failure
  boundaries.

## Additional comments from user

- The analysis must cover the current PR branch rather than the earlier Fleet-specific AFD CRD
  proposal.
- The requested topology is AFD + WAF + PLS + internal Load Balancer + Service/ServiceExport,
  integrated through the GEP-1748 model.
- The solution consists of independently limited components; the analysis must not collapse them
  into one generic service-count limit.
- Multiple AFD profiles increase aggregate capacity, but each profile retains its own origin,
  route, domain, policy, throughput, and Private Link limits.
- The document must explain what scale this PR should support for Service and ServiceExport
  resources, why that envelope is appropriate, and the best practices needed to remain inside it.
- A sensible split must distinguish tenant/environment/application isolation from capacity
  sharding and distinguish both from a topology change needed when one ServiceImport spans more
  member clusters than one AFD origin group supports.
- The scale document should include commentary on whether and how multiple groups can safely share
  the same member cluster.

## Plan

### Phase 1: Establish the implemented baseline

- [x] **Task 1.1: Inventory the PR diff and production wiring.**
  - Success criteria: identify every implemented manager, controller, watch, model, annotation,
    test, and deployment surface, and explicitly identify planned surfaces that do not yet exist.
- [x] **Task 1.2: Verify API and ownership boundaries.**
  - Success criteria: map GatewayClass, Gateway, HTTPRoute, ReferenceGrant, Fleet ServiceImport,
    ServiceExport, InternalServiceExport, Service, and Azure resource ownership.

### Phase 2: Model scale before implementation

- [x] **Task 2.1: Define the capacity dimensions and formulas.**
  - Success criteria: define at least `G` Gateways/profiles, `S_g` ServiceImports per Gateway,
    `C_s` contributing clusters per ServiceImport, `O_g = sum(C_s)` resolved origins per profile,
    `R_g` generated routes, and `P_s` exposed Service ports without double-counting shared Azure
    resources.
- [x] **Task 2.2: Define representative deployment tiers and worked examples.**
  - Success criteria: quantify small, medium, large, and extreme fleets and include valid
    compositions such as many single-cluster services, a few broadly exported services, and
    mixed-cardinality profiles.
- [x] **Task 2.3: Define measurable performance tests first.**
  - Success criteria: specify benchmarks and load scenarios for model construction, watch fan-out,
    status writes, ARM operations, propagation latency, and outage recovery before recommending
    implementation changes.

### Phase 3: Analyze control-plane bottlenecks

- [x] **Task 3.1: Analyze Kubernetes API and controller-runtime scaling.**
  - Success criteria: cover cache memory, watches, indexes, reconciliation fan-out, queue
    concurrency, retries, resync, status amplification, leader election, and object-size growth.
- [x] **Task 3.2: Analyze Fleet MCS propagation.**
  - Success criteria: trace member Service and ServiceExport state through internal exports and
    hub ServiceImport aggregation, including churn and failure behavior.
- [x] **Task 3.3: Analyze Azure resource reconciliation.**
  - Success criteria: cover AFD profile, endpoint, route, origin group, origins, domain,
    certificate, WAF association, Private Link approval, ARM throttling, eventual consistency,
    polling, and configuration propagation.

### Phase 4: Analyze data-plane bottlenecks

- [x] **Task 4.1: Analyze request and health-probe paths.**
  - Success criteria: trace client-to-AFD-to-WAF-to-Private-Link-to-PLS-to-ILB-to-Service-to-pod,
    quantify probe amplification, and identify latency, throughput, SNAT, and readiness risks.
- [x] **Task 4.2: Analyze platform quotas and address capacity.**
  - Success criteria: inventory relevant AFD, WAF, Private Link, PLS, Load Balancer, subnet, AKS,
    Gateway API, and Kubernetes object limits from authoritative sources.
- [x] **Task 4.3: Analyze throughput and latency boundaries.**
  - Success criteria: cover AFD profile and PoP throughput, AFD Private Link protection limits,
    WAF processing, cross-region Private Link selection, PLS NAT capacity, Load Balancer rule and
    backend-IP growth, Service port multiplication, pod readiness, and application bottlenecks.
- [x] **Task 4.4: Analyze shared-cluster resource contention.**
  - Success criteria: quantify how tenant Services and ports share cluster-level Load Balancer
    rules, frontend IPs, backend IP configurations, PLS resources, subnets, nodes, Kubernetes API
    capacity, controller queues, and health probes.

### Phase 5: Analyze stress and failure scenarios

- [x] **Task 5.1: Model steady-state and burst scenarios.**
  - Success criteria: assess cluster join/leave, regional outage, export churn, blue-green fleet
    migration, WAF update, certificate lifecycle, and mass reconciliation.
- [x] **Task 5.2: Rank bottlenecks and mitigations.**
  - Success criteria: each finding includes trigger, symptom, blast radius, detection,
    instrumentation, mitigation, and validation test.

### Phase 6: Define the PR support envelope and split strategy

- [x] **Task 6.1: Separate hard, tested, and recommended limits.**
  - Success criteria: every published number is labeled as an Azure/Kubernetes hard limit, a
    repository-tested limit, a proposed supported limit, or an operational recommendation.
- [x] **Task 6.2: Propose an initial Service/ServiceExport support envelope.**
  - Success criteria: specify admission ceilings and lower release targets for clusters per
    ServiceImport, resolved origins per Gateway, ServiceImports per Gateway, generated routes,
    Service ports, and member-cluster LoadBalancer Services; include required headroom.
- [x] **Task 6.3: Define a sensible split taxonomy.**
  - Success criteria: provide deterministic guidance for splitting by tenant, environment,
    application portfolio, compliance boundary, blast radius, quota pressure, region, and
    subscription, and state which split creates another AFD profile.
- [x] **Task 6.4: Define supported shared-member-cluster tenancy patterns.**
  - Success criteria: compare dedicated clusters, shared clusters with dedicated namespaces and
    Gateways/profiles, shared profiles with dedicated listeners/domains/routes, and any shared
    ILB/PLS or ingress-shard model; document isolation, cost, quota, and operational tradeoffs.
- [x] **Task 6.5: Define multitenant ownership and policy boundaries.**
  - Success criteria: cover namespace and ReferenceGrant boundaries, Gateway/HTTPRoute attachment,
    ServiceExport authorization, tenant identity and RBAC, WAF/security policy ownership,
    certificates and domains, Azure resource ownership, quota attribution, observability,
    chargeback, noisy-neighbor controls, and tenant offboarding.
- [x] **Task 6.6: Recommend multitenant defaults and guardrails.**
  - Success criteria: state when sharing a member cluster is reasonable, when a dedicated
    Gateway/profile is required, when a dedicated ILB/PLS or ingress shard is required, and when
    security, compliance, scale, or blast-radius requirements justify a dedicated cluster.
- [x] **Task 6.7: Define topology-change triggers.**
  - Success criteria: explain why another profile does not let one origin group exceed 50 origins
    and identify when shared regional ingress, hierarchical routing, explicit service partitioning,
    or another approved topology is required.
- [x] **Task 6.8: Define enforcement and status behavior.**
  - Success criteria: specify preflight validation before partial ARM programming, Gateway API
    conditions, Kubernetes warning events, actionable messages, retry behavior, and recovery after
    capacity becomes available.

### Phase 7: Deliver and validate the separate document

- [x] **Task 7.1: Create `docs/design/gep-1748-scale-and-capacity.md`.**
  - Success criteria: the document begins with a comprehensive table whose columns include
    component/resource, quota scope, hard limit, PR consumption formula, proposed operating
    target, exhaustion symptom, split/mitigation action, and authoritative source.
- [x] **Task 7.2: Add capacity formulas, worked examples, and best practices.**
  - Success criteria: readers can calculate whether a proposed Fleet topology fits without relying
    on a single ambiguous "number of services" value.
- [x] **Task 7.3: Add the performance and release-validation matrix.**
  - Success criteria: include steady-state, burst, cluster join/leave, regional failure,
    ServiceExport churn, controller restart, throttling, Private Link approval, and recovery tests
    with measurable gates.
- [x] **Task 7.4: Cross-reference the analysis from the architecture RFC.**
  - Success criteria: `docs/design/gep-1748-gateway-api.md` links to the separate analysis without
    duplicating its detailed limits or presenting proposed support values as implemented behavior.
- [x] **Task 7.5: Validate references and internal consistency.**
  - Success criteria: repository references, formulas, links, and recommendations are checked;
    no unimplemented behavior is presented as working.

### Detailed checklist

- [x] Phase 1 / Task 1.1 completed.
- [x] Phase 1 / Task 1.2 completed.
- [x] Phase 2 / Task 2.1 completed.
- [x] Phase 2 / Task 2.2 completed.
- [x] Phase 2 / Task 2.3 completed.
- [x] Phase 3 / Task 3.1 completed.
- [x] Phase 3 / Task 3.2 completed.
- [x] Phase 3 / Task 3.3 completed.
- [x] Phase 4 / Task 4.1 completed.
- [x] Phase 4 / Task 4.2 completed.
- [x] Phase 4 / Task 4.3 completed.
- [x] Phase 4 / Task 4.4 completed.
- [x] Phase 5 / Task 5.1 completed.
- [x] Phase 5 / Task 5.2 completed.
- [x] Phase 6 / Task 6.1 completed.
- [x] Phase 6 / Task 6.2 completed.
- [x] Phase 6 / Task 6.3 completed.
- [x] Phase 6 / Task 6.4 completed.
- [x] Phase 6 / Task 6.5 completed.
- [x] Phase 6 / Task 6.6 completed.
- [x] Phase 6 / Task 6.7 completed.
- [x] Phase 6 / Task 6.8 completed.
- [x] Phase 7 / Task 7.1 completed.
- [x] Phase 7 / Task 7.2 completed.
- [x] Phase 7 / Task 7.3 completed.
- [x] Phase 7 / Task 7.4 completed.
- [x] Phase 7 / Task 7.5 completed.

### Overall success criteria

- The analysis reflects PR #400's actual implementation state.
- The complete intended control-plane and data-plane path is modeled.
- Scale is quantified with formulas and representative fleet tiers.
- Bottlenecks are prioritized by likely production impact and supported by mitigations and tests.
- Azure and Kubernetes limits are cited from current authoritative sources.
- The report does not claim that PR #400 currently provisions AFD, WAF, PLS, or Load Balancers.
- The report provides an explicit, reviewable initial support envelope for the objects controlled by
  the PR and does not imply that all 1,000 Fleet members can back one ServiceImport.
- The report explains which constraints can be addressed by another Gateway/profile and which
  require a different origin topology.
- The report provides actionable tenancy guidance for multiple groups sharing one member cluster
  without treating namespace isolation as equivalent to quota, network, security, or blast-radius
  isolation.

## Decisions

- Treat PR #400 as a Gateway API foundation and API-contract implementation, because its manager
  explicitly registers no reconcilers.
- Separate current-code bottlenecks from projected bottlenecks in future controller and Azure
  provider work.
- Use direct per-Service, per-member-cluster origins as the baseline because shared per-cluster
  ingress gateways are explicitly deferred by the design.
- Analyze Private Link as the target production topology and public origins only as a comparison.
- Treat one Gateway/AFD profile as both an isolation boundary and an independent quota boundary.
- Express support as a multidimensional envelope rather than a single maximum ServiceExport count.
- Reserve operational headroom below provider hard limits; exact supported values remain decisions
  to be justified by performance tests and service-team review.
- Use additional Gateways/profiles for tenant, environment, application, compliance, blast-radius,
  or per-profile quota isolation. Do not describe profile sharding as a solution for a single
  ServiceImport that exceeds the per-origin-group member-cluster limit.
- Do not assume one Fleet member cluster belongs to one tenant. Evaluate cluster-level shared
  infrastructure separately from namespace-scoped Kubernetes ownership and profile-scoped AFD
  ownership.
- Treat dedicated clusters as the strongest isolation option, not the automatic default. The
  analysis must justify when namespace, Gateway/profile, ingress-shard, ILB/PLS, or cluster
  isolation is sufficient.
- Propose an initial direct-origin envelope of 40 member origins per attached ServiceImport,
  160 resolved origins, 100 distinct ServiceImports, 160 generated routes, and a 4,000 composite
  routing metric per Gateway/profile.
- Propose six PLS-backed exported Services per shared member Load Balancer because Azure permits
  eight PLS resources per Standard Load Balancer and two slots are reserved for migration and
  repair.

## Implementation Details

- Planned output: `docs/design/gep-1748-scale-and-capacity.md`.
- Planned cross-reference: `docs/design/gep-1748-gateway-api.md`.
- The analysis document will use a table-first structure followed by formulas, worked examples,
  proposed support policy, multitenancy and split guidance, bottleneck analysis, validation gates,
  and open decisions.
- The user approved the implementation plan.
- Implementation started after confirming that PR #400 is open on
  `rchinchani/gep-1748-gateway-api` and that the feature flag currently validates configuration
  without registering AFD reconcilers.
- Created `docs/design/gep-1748-scale-and-capacity.md` with the complete table, formulas,
  provisional support envelope, examples, controller and data-plane analysis, multitenancy and
  split guidance, admission behavior, and measurable release-validation matrix.
- Cross-referenced the scale document from `docs/design/gep-1748-gateway-api.md`.
- Validated relative links, reference-style links, table-of-contents anchors, trailing whitespace,
  and the tracked diff.

## Changes Made

- Added this breadcrumb and scale-analysis plan.
- Expanded the plan to capture the comprehensive limit table, PR-specific Service/ServiceExport
  support envelope, profile/cluster/region/subscription split guidance, admission behavior, and
  performance release gates requested by the user.
- Completed the implemented-baseline and API ownership inventory before writing the scale
  document.
- Added the standalone scale and capacity document and linked it from the architecture RFC.
- Completed the approved analysis plan and validation checklist.

## Before/After Comparison

- **Before:** PR #400 had architecture and implementation plans but no quantified end-to-end scale
  or performance analysis.
- **After:** The standalone scale and capacity document quantifies every major scope, proposes a
  reviewable initial support envelope, explains multitenant and sharding boundaries, and defines
  admission and release-validation requirements without claiming that PR #400 provisions Azure
  resources.

## References

- `cmd/hub-gateway-controller-manager/main.go`: Current manager wiring and explicit no-reconciler
  foundation boundary.
- `pkg/controllers/hub/gatewaymodel/model.go`: Current normalized desired-state model.
- `docs/design/gep-1748-gateway-api.md`: Intended architecture and ownership model.
- `docs/design/gep-1748-implementation-plan.md`: Planned reconciliation phases.
- `docs/howtos/gateway-api-afd-configuration.md`: Intended public and Private Link workflows.
- `api/v1alpha1/serviceimport_types.go`: Fleet ServiceImport contract.
- `api/v1alpha1/internalserviceexport_types.go`: Member-to-hub origin transport.
- `pkg/controllers/hub/serviceimport/controller.go`: Existing ServiceImport aggregation.
- `pkg/controllers/member/serviceexport/controller.go`: Existing member export reconciliation.
- `test/perftest/latency/peak/latency_test.go`: Existing peak ServiceExport/ServiceImport workload.
- `test/perftest/latency/sustained/latency_test.go`: Existing sustained ServiceExport and
  EndpointSlice churn workload.
- GEP-1748: <https://gateway-api.sigs.k8s.io/geps/gep-1748/>
- Azure subscription and service limits:
  <https://learn.microsoft.com/azure/azure-resource-manager/management/azure-subscription-service-limits>
- Azure Front Door Private Link:
  <https://learn.microsoft.com/azure/frontdoor/private-link>
- Azure Front Door health probes:
  <https://learn.microsoft.com/azure/frontdoor/health-probes>
- AKS quotas and limits:
  <https://learn.microsoft.com/azure/aks/quotas-skus-regions>
- AKS large-scale performance guidance:
  <https://learn.microsoft.com/azure/aks/best-practices-performance-scale-large>
- AKS internal Load Balancer:
  <https://learn.microsoft.com/azure/aks/internal-lb>
- Fleet Manager FAQ:
  <https://learn.microsoft.com/azure/kubernetes-fleet/faq>
- Azure Resource Manager throttling:
  <https://learn.microsoft.com/azure/azure-resource-manager/management/request-limits-and-throttling>
