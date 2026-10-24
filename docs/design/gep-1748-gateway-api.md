# GEP-1748 Gateway API for Fleet Global Ingress

## Document status

- **Status:** Proposed
- **Date:** 2026-08-17
- **Product contract updated:** 2026-10-20; Zero-Public-Exposure Ingress
- **Target repository:** `Azure/fleet-networking`
- **Primary API:** Kubernetes Gateway API
- **Gateway API dependency:** `sigs.k8s.io/gateway-api v1.2.1`
- **Multi-cluster backend API:** `networking.fleet.azure.com/v1alpha1`, kind `ServiceImport`
- **Initial Azure provider:** Azure Front Door Premium with mandatory Private Link origins

## Summary

This design adds HTTP(S) global ingress to Fleet by combining Kubernetes Gateway API routing with
Fleet's existing multi-cluster `ServiceImport` API. It follows the interaction model described by
[GEP-1748](https://gateway-api.sigs.k8s.io/geps/gep-1748/): an `HTTPRoute` can reference a
`ServiceImport` as a backend, and the Gateway implementation resolves that logical service into
endpoints across the Fleet.

The design intentionally keeps the existing Fleet `ServiceImport` group, version, kind, schema, and
controller behavior unchanged:

```text
networking.fleet.azure.com/v1alpha1, kind ServiceImport
```

The product solution is **Zero-Public-Exposure Ingress**: the public Azure Front Door Premium edge
reaches member workloads only through Private Link, PLS, and internal load balancers. Public
member origins and AFD Standard are not supported. Zero public exposure refers to member origins,
not the client-facing edge.

Optional Gateway annotations declare Premium or attach WAF instead of introducing user-facing
Front Door CRDs. Backend settings are platform-owned, not ServiceImport annotations. The portable
routing contract remains `GatewayClass`, `Gateway`, `HTTPRoute`, and
`ServiceImport`. When another Gateway implementation ignores the Azure annotations, the portable
listener and routing intent remains understandable, although the Fleet-specific `ServiceImport`
group requires explicit support from that implementation.

Azure Front Door (AFD) Premium is the first provider implementation. Private Link Service (PLS)
connectivity is mandatory; Web Application Firewall (WAF) remains optional.

### Current implementation boundary

The opt-in hub Gateway manager now registers GatewayClass, Gateway and HTTPRoute
control-plane reconcilers. They validate the private HTTP-preview subset,
authorize listener namespaces and cross-namespace ServiceImport references, and
watch indexed Kubernetes dependencies. Status updates preserve other controllers'
route parents and retry conflicts against current intent.

This is not a functioning Azure ingress deployment. Gateway/listener `Programmed`
remain false, no AFD address or supported-feature claims are published, and no
Private Link connection has been requested. AFD lowering/full capacity preflight,
Azure creation/update, full-graph readiness, packaging and live traffic
evidence remain implementation gates. Enabled startup now initializes the owned
profile cleanup client using the existing hub Azure auth/options/rate-limit policy
and injects the uncached API reader for trusted hub identity. A deleting Gateway
already bearing this controller's cleanup finalizer can trigger profile GET/DELETE
after full ARM-scope and ownership-tag verification; the finalizer remains until
the profile is observed absent. This cleanup runs before class/backend validation
and outside status conflict retries. The standard GatewayClass in-use finalizer
protects classes with referencing Gateways; normal Gateways still acquire no
Azure finalizer until the create/update lifecycle is integrated.

A metadata-only preparation helper now persists and reobserves the Gateway
cleanup finalizer against matching uncached intent. It requires an available
cleanup provider and trusted hub identity, and checks Gateway UID/generation,
class acceptance, current routes, namespace policy, ReferenceGrants and private
backends before patching and again before returning ownership. Conflicts retry
only Kubernetes work and preserve other metadata. Uncached validation explicitly
lists/filters routes and exports without cache-only field selectors; the normal
indexed reconciliation path is unchanged. The helper itself makes no Azure calls
and is not platform/concurrent-writer authorization. Sequential rechecks cannot
make multi-object intent transactional.

An optional typed disabled-graph stager now invokes that guard before any staging
call, outside status conflict retries. Its SDK implementation composes root,
backend and route staging under one 30-second deadline and at most one submitted
write, stopping until root observations converge before proceeding. Status uses
only matching Gateway UID/generation and re-resolved intent, remains
`Programmed=False` without addresses, and polls staging progress with the existing
30-second plus jitter cadence. Approval diagnostics retain the asynchronous,
authorized PLS-owner action item. Errors are returned without dropping concurrent
observation failures. Deletion still takes precedence over staging.

Once the expected disabled route is observed, composed staging rechecks owned
disabled roots and paginates all endpoints, routes, origin groups and origins in
the supported graph. Every expected full ARM identity must appear exactly once;
missing, duplicate, nil or contradictory records fail explicitly. Unexpected
resources are refused except for the narrowly eligible stale origins below.
Every supported collection page must contain an explicit resource array.
Missing/null arrays invalidate completeness even when another page contains all
expected resources; explicitly empty pages with or without continuation are valid.
Listed records must also still prove disabled endpoint/route convergence, the
managed HTTP route and group probe/session policy, and matching approved/deployed
private origins. A later configuration, deployment or approval change invalidates
the earlier GET result and returns no partial staging success. Approval regressing
to pending retains the asynchronous, authorized PLS-owner action item. Unmanaged
AFD load-balancing defaults and equivalent ARM identity casing remain accepted.
It also paginates profile custom domains, rule sets, security policies and secrets,
requiring explicitly empty resource arrays on every page. Missing/null arrays,
any returned item (including nil records), and SDK errors block verification.
Only an exact final inventory reports `inventoryVerified=true`. Pending desired
approvals and provisioning submissions do not trigger these collection reads.
List failures (including 404) are not interpreted as empty success.
This verifies supported membership/managed convergence and observed absence of
unsupported assets, not an atomic configuration snapshot or traffic readiness.
The inventory clients
must be initialized before any staging writes; future enablement requires
profile-scoped read/list access to these collections, not Key Vault secret access
or external WAF/certificate mutation privileges. These resource features,
automatic migration and broader stale-resource cleanup remain separate work.

After complete desired convergence and all inventory checks, the injected path
can prune extra private Fleet-shaped origins only within the single expected
origin group. Verified profile ARM identity/hub/Gateway tags and full child
parent identity authorize cleanup; generated name shape is only an additional
eligibility filter. Public, malformed, unknown or foreign records and any other
unexpected/unsupported resources still block the operation. Desired origins are
never candidates. The provider reobserves the selected origin, refuses changed
configuration/identity, then rechecks owned Premium/disabled roots before one
DELETE under the same deadline/write bound. In-flight operations are observed,
not resubmitted. Known failed/rejected stale connectivity does not block removal.
`staleOriginsPending` remains nonzero until a later complete inventory proves
absence; a synchronous DELETE, GET 404 or DELETE 404 cannot establish success.
Only then can `inventoryVerified=true` return. Future integration additionally
needs profile-scoped origin DELETE permission, not PLS/private-endpoint mutation.
No groups/routes/profiles or external assets are pruned by this bounded step.
Replacements that cannot first stage alongside existing origins remain blocked;
cleanup does not speculate by deleting old bindings to unblock staging.
The SDK DELETE has no If-Match option: fresh reads are not external-writer
exclusion, and no live enforcement proof is claimed.

**Manager startup does not construct or inject this stager.** Existing deployments
therefore still create no AFD resources or new Gateway cleanup finalizers.
Fake-client/SDK-transport tests exercise the injectable path, not production
enablement. Platform proofs, disabled-resource deployment semantics and external
concurrent-writer policy remain mandatory before startup injection. Endpoint/
route enablement and cleanup of other stale resource types remain unimplemented.
These are outstanding proof/implementation requirements, not automatic runtime
gate checks.

Separately, enabled manager `/readyz` requires initial synchronization of every
watched Gateway/Fleet/Namespace informer and fails for stopped informers or
manager shutdown. Required informers are initialized before startup without
waiting for synchronization; probe requests only inspect local state. Standby
replicas can become ready without leadership. `/healthz` remains independent,
and disabled mode does not initialize these informers or require Azure config.
This is process readiness, not an SDK enablement gate, ongoing API-freshness
check, Private Link approval requirement or dataplane readiness claim.

The manager has a separate Dockerfile and explicit local
`make image-hub-gateway-controller-manager` target. Native build/run targets
compile the complete command package, including Azure/readiness helpers.
The existing image/push bundles now include Gateway; GHCR uses the same
`make push` path and MCR extends its existing architecture-tag/manifest steps, skipping
Gateway for historical releases that did not publish it. AFD stays disabled by
default. Linux CI verifies GNU make dry-run delivery contracts and defines a real
image build, nonroot-identity check and network-isolated entrypoint `--help`
smoke. Container/registry verification is pending those executions, not
established by local Go compilation or dry runs. The independent
[Gateway chart](../../charts/hub-gateway-controller-manager/README.md) requires an
explicit image repository/tag and defaults to disabled mode without Azure mounts,
GatewayClass or cluster-wide permissions. Enabled mode uses an existing platform
Secret/account, scoped Gateway/Fleet RBAC and an optional `azure-fleet-afd` class.
Native Helm render/lint tests and a read-only chart CI job verify these contracts,
not live authorization or informer behavior. Image publishing, live installation
and controller/datapath proof remain separate.

For a Gateway already bearing the cleanup finalizer, the injected path can now
suspend the owned endpoint when uncached validation confirms a complete model
with no effective routes. It does not acquire new ownership for empty intent.
An independent authorization-only path also suspends the whole already-owned
endpoint on a freshly confirmed backend `RefNotPermitted` for a currently
attached, eligible HTTPRoute. This intentionally interrupts permitted routes
too, even when other routes/backends remain unresolved or unreadable. Mixed
graphs still produce no complete candidate and never stage a replacement subset.
The uncached scan reads attachment policy and grants, not ServiceImports/exports.
A grant-read error alone is not denial evidence; a separate positive denial can
still authorize withdrawal while observed read failures are returned for retry.
Hub/Gateway identity, current class acceptance, existing finalizer, route
identity/specification, attachment and denial are rechecked before suspension.
Fresh permission blocks stale cached denial. Changed routes/restored permission
or failed proof reads prevent stale denial diagnostics after the operation.
If full backend resolution fails, the controller still clears addresses and
marks Gateway/existing listeners unprogrammed, preserves other previously
observed status instead of partial counts, and returns the resolution error.
The SDK operation verifies profile ownership and endpoint identity, GETs only
those resources, and submits at most one disable-only PATCH under a 30-second
budget. Missing resources are never created. Submitted disable operations remain
pending until later provisioning/deployment observations; no hostname or backend
readiness is required to withdraw. Cleanup metadata is retained. Stale cached
emptiness, lost identity/ownership and lack of positive evidence prevent suspension, and
status retries do not replay it. This is not selective stale-resource cleanup or
complete grant/membership/approval revocation: selective withdrawal, class
handoff and unresolved intent without a confirmed denial remain open. An observed
disabled endpoint is not live datapath-enforcement evidence.

The approved initial Azure execution subset is one unrestricted HTTP/80
listener, only root PathPrefix `/` matches and one logical ServiceImport port
across all rules, with no filters or WAF. Pure construction now produces one
Premium profile, one disabled endpoint, one private origin group with the
selected members and one disabled `/*` HTTP route linked only to the default
AFD hostname. Caching, HTTPS redirects, rewrites, rule sets and custom domains
are omitted/disabled; generated resource counts are explicit. Equivalent root
bindings to the same backend deduplicate without changing Azure identity.
Unsupported execution intent stays `Programmed=False` with an explicit
diagnostic rather than changing Kubernetes acceptance or widening traffic.
This does not complete the broader Exact/PathPrefix/hostname contract or
authorize Azure writes; supported region, probe Host, member-weight eligibility,
persisted ownership and operational apply gates remain outstanding.

A callable root provisioner now submits actual SDK profile and disabled-endpoint
operations, but is not constructed or invoked by the manager. It validates the
complete supported graph before ARM access, checks profile ownership and waits
for observed Premium/global Active/Succeeded state before child access. Each
invocation shares a 30-second budget, submits at most one write and discards LRO
pollers; even synchronous submissions require a subsequent GET. Owned enabled
endpoint drift is explicitly disabled, and an Azure hostname is returned only
after observed disabled-endpoint provisioning/deployment convergence. These
results are staging progress, not ingress readiness. A prior GET does not
atomically exclude concurrent external writers; that policy, persisted
pre-create finalization, platform proofs and remaining graph operations still
gate production creation.

A separate callable backend provisioner now observes those roots before staging
the expected origin group and private origins. It never creates/enables roots
and requires a converged disabled endpoint. Group provisioning precedes origin
writes; group deployment is tracked separately so an undeployed empty group
does not block its own origin creation. Each invocation submits at most one
write under one shared 30-second budget, with fresh GETs required after writes.
Only controlled probe/affinity and private-origin configuration are updated;
public or different-PLS origins are refused rather than adopted. Pending
approval does not block creating later expected origins. Existing correlated
approval diagnostics are reused, and convergence describes only expected
backends, not routes, origin health or removal of stale resources. This
provisioner is also not constructed or invoked by the manager.

A callable route provisioner now composes backend staging and the initially
disabled HTTP `/*` route. A submitted backend write ends the invocation; route
GET/create/disable operations require observed expected-backend convergence,
including approval. The entire call shares one 30-second deadline and submits
at most one backend or route write. It verifies the full route/endpoint ARM
identity and the exact approved routing configuration, creates the route
disabled and disables an otherwise matching enabled route. Other routing drift
is explicitly refused: omitted PATCH fields are not assumed to clear caching,
rewrites, rule sets, custom domains or protection. Even synchronous writes need
fresh GET provisioning/deployment observations. This is callable staging only:
neither the route nor endpoint is enabled, and production wiring, routing
migration, complete lifecycle proofs and whole-graph readiness remain open.

Pure private-origin observation now checks an owned profile and each expected
origin's full ARM identity, desired configuration and Private Link target.
Approval, provisioning and deployment are distinct: Approved alone is not
convergence, and missing/unknown states remain unready. Manual-approval
diagnostics are deduplicated for shared PLS/region targets without inferring one
origin's status from another's. A callable read-only SDK observer now obtains
these snapshots with one bounded operation budget: GET the profile, verify
ownership, then paginate only expected origin groups. Missing profiles are
explicit, missing groups discard partial pages, and API/identity errors return
no partial observation. Each invocation starts fresh; sequential ARM reads are
not transactional. Enabled startup now injects this observer alongside cleanup
using the same configured credential/options. Gateways within the execution
subset with accepted, fully resolved nonempty private backend intent recheck
every 30 seconds plus up to 20%
jitter, without needing Kubernetes edits for asynchronous approval changes.
Azure reads run once outside status-conflict retries; diagnostics are applied
only after re-resolving and matching Gateway UID/generation and normalized
dependency intent. Observation failures are explicit pending diagnostics and
retryable errors. Invalid, unresolved, empty and foreign intent is not observed.
Origin convergence alone does not prove complete AFD graph readiness, origin
health or traffic, and does not set `Programmed=True` or publish addresses.

The current observation path now uses the manager's standard EventRecorder for
asynchronous approval instructions, observation failures, problematic origin
states and origin convergence. Events follow successfully persisted current
observation-status transitions, outside conflict retries; unchanged polls,
discarded observations, failed patches and staging-overridden status emit none.
Fixed messages omit raw SDK payloads/resource IDs, and native client-go event
aggregation/rate limiting remains in effect. Enabled chart RBAC permits Event
create/patch in Gateway namespaces; disabled permissions remain unchanged.

Logical origin-observer count/duration metrics use the existing Prometheus
registry with only fixed `success`/`error`/`throttled` outcome labels. They count
actual completed observer calls, not each ARM request, approval waits, current
ready origins or traffic. A completed read can subsequently be discarded for
changed intent. See the [chart diagnostics reference](../../charts/hub-gateway-controller-manager/README.md#origin-observation-diagnostics).
Owned-profile finalization also has fixed-outcome logical call count/duration
metrics and sanitized failure/ownership-conflict events through the same
registry/recorder. Completion events require observed profile absence and a
successful cleanup-finalizer removal patch for the matching deleting Gateway.
Metadata retries do not replay provider calls or count extra samples. An absent
profile is not proof this controller deleted it, and provider-call success is
not proof of finalizer persistence. See the
[cleanup diagnostics reference](../../charts/hub-gateway-controller-manager/README.md#owned-profile-cleanup-diagnostics).
Existing injected graph staging and both empty-intent/ReferenceGrant-denial
suspension call sites also expose fixed-operation/outcome call counts and
durations, plus sanitized failed-call warnings. Calls count once outside status
retries even if their result becomes stale or later status persistence fails.
Nil-error calls include pending/absent resources and do not establish readiness
or successful withdrawal enforcement. The SDK stager remains uninjected by the
manager, so this telemetry does not enable production writes. See the
[graph-operation diagnostics reference](../../charts/hub-gateway-controller-manager/README.md#graph-operation-diagnostics).
Approval-wait telemetry and live event-delivery/enforcement proof remain
outstanding.

A bounded Ginkgo/envtest lifecycle suite now runs the actual GatewayClass,
Gateway and HTTPRoute controllers with real API validation, caches/indexes,
status patches and persisted Events. It covers fake-provider approval progression
without Kubernetes edits, export-watch discovery invalidation/recovery and
pending-to-absent cleanup with actual finalizer retention/removal. Focused and
full Gateway/manager runs passed under WSL with normal teardown; the production
polling cadence and uninjected SDK stager are unchanged. This is local
API/controller evidence, not deployed RBAC/identity, real AFD approval, recorder
rate limits or dataplane enforcement. Native Windows teardown is unsupported by
this controller-runtime version; use Linux/WSL. Broader lifecycle/provider
integration remains open; see the
[integration guide](../../test/README.md#gateway-api-server-integration).
The same lifecycle now covers a cross-namespace route: ReferenceGrant deletion
preserves attachment acceptance but sets current `ResolvedRefs=False` with
`RefNotPermitted` and stops origin observation after denied status settles.
Denial remains ahead of missing-target lookup; a recreated grant exposes
`BackendNotFound` until a recreated ServiceImport restores resolution through
dependency watches. Gateway/HTTPRoute specs and generations stay unchanged.
This verifies local authorization/status recovery, not transactional cache
authorization or actual Azure withdrawal/enforcement.
Export-watch integration also covers a recreated source Service's transported
UID and a replacement PLS/frontend identity. Each changed export is rejected
while its unconflicted status belongs to the prior generation; current status
restores observation of the exact new private origin without Gateway/HTTPRoute
spec or generation changes. Route reference validity remains independent of
origin readiness. This is imported-model evidence, not member discovery/transport,
independent live Service/PLS validation or actual Azure replacement/withdrawal.
Provider-only `Disconnected` and `Drifted` transitions after convergence also
exercise production polling: stale convergence diagnostics are replaced,
matching warning Events are persisted, and restored `Ready` state yields a new
persisted convergence Event occurrence. Gateway/HTTPRoute intent and backend
objects remain unchanged; route references remain valid and observed Gateway
status remains unprogrammed/address-free. This fake-provider evidence does not
prove SDK classification, real approval withdrawal, configuration repair or
dataplane enforcement.
An additional in-flight read case blocks a captured rejection, changes a valid
private origin and waits for the actual controller cache to observe its current
export spec/status before release. The old result cannot publish a rejection
warning; queued observations recover the exact replacement origin and a fresh
convergence Event occurrence, without Gateway/HTTPRoute intent changes or
programmed/address-bearing status. Cancellation-safe one-shot test coordination
does not establish transactional freshness, competing-writer exclusion, actual
ARM cancellation or Azure mutation safety.

## Motivation

Fleet already provides:

- Member-cluster `ServiceExport` resources.
- A hub-cluster `ServiceImport` representing one logical service exported by multiple clusters.
- Internal transport resources that identify the contributing member clusters.
- Per-cluster traffic weights through the `networking.fleet.azure.com/weight` annotation.
- Azure global routing integration through Traffic Manager for DNS-based scenarios.

Fleet does not currently provide a Gateway API implementation for HTTP(S) global ingress. Users
need a model that supports:

- Host, path, header, and method routing.
- Multiple multi-cluster backends.
- Weighted backend references.
- Edge TLS termination.
- Optional WAF attachment.
- Mandatory private connectivity from AFD Premium to AKS through PLS.
- Standard Gateway API status and ownership behavior.

Creating a second set of Front Door-specific routing CRDs would duplicate concepts already defined
by Gateway API. This design instead makes Gateway API the public ingress API and limits Azure
extensions to annotations.

## Goals

1. Use `GatewayClass`, `Gateway`, and `HTTPRoute` as the only user-facing AFD routing resources.
2. Allow `HTTPRoute.backendRefs` to target the existing Fleet `ServiceImport`.
3. Preserve the existing Fleet `ServiceImport` API without schema, group, version, or semantic
   changes.
4. Implement AFD Premium as a Gateway controller for Zero-Public-Exposure Ingress.
5. Support optional existing WAF policy attachment.
6. Require AFD-to-PLS private origin connectivity without public fallback.
7. Preserve portable Gateway API listener and route configuration.
8. Report standard Gateway API conditions and actionable validation failures.
9. Keep Traffic Manager behavior and APIs independent.
10. Isolate AFD reconciliation from the existing hub networking controller manager.

## Non-goals

1. Claim upstream GEP-1748 Extended conformance for the Fleet `ServiceImport` group.
2. Replace or migrate Fleet's `ServiceExport` or `ServiceImport` APIs.
3. Introduce `FrontDoorProfile`, `FrontDoorBackend`, or `FrontDoorCustomDomain` APIs.
4. Model every Azure Front Door property in Kubernetes.
5. Provision WAF policy definitions or managed rule sets.
6. Provision the member-cluster internal load balancer or PLS directly from the hub controller.
7. Implement Gateway API mesh/GAMMA behavior.
8. Make Azure Traffic Manager a GatewayClass.
9. Support arbitrary Azure resource adoption in the initial release.
10. Support shared per-cluster ingress gateways in the initial release.
11. Support public member origins, AFD Standard, or selectable origin connectivity.
12. Require customers to configure generated hub ServiceImports.

## Compatibility with GEP-1748

### Dependency baseline

| Component | Version | Rationale |
|---|---|---|
| Go | Repository baseline | No toolchain upgrade is introduced by Gateway API. |
| Kubernetes libraries | `v0.31.1` | Existing fleet-networking dependency baseline. |
| controller-runtime | `v0.19.0` | Existing fleet-networking dependency baseline. |
| Gateway API | `v1.2.1` | Uses Kubernetes `v0.31.1` and is compatible with the repository baseline. |

Gateway API `v1.3.0` and later require newer Kubernetes libraries and controller-runtime versions.
They are deferred to avoid coupling this feature to a repository-wide dependency upgrade.

GEP-1748 defines an `HTTPRoute` backend reference to:

```yaml
group: multicluster.x-k8s.io
kind: ServiceImport
```

Fleet currently owns and reconciles:

```yaml
group: networking.fleet.azure.com
kind: ServiceImport
```

This design adopts the GEP's behavior but not its upstream API group. The Fleet Gateway controller
recognizes the following backend reference as an implementation-specific Extended capability:

```yaml
backendRefs:
- group: networking.fleet.azure.com
  kind: ServiceImport
  name: store
  port: 8080
```

Consequences:

- Fleet `ServiceImport` support must be documented as an implementation-specific Extended
  capability.
- The implementation must not advertise upstream GEP-1748 Extended conformance unless it also
  supports `multicluster.x-k8s.io/ServiceImport`.
- The backend behavior should otherwise follow GEP-1748: routes, filters, weights, namespace
  authorization, and status apply consistently to `Service` and `ServiceImport` backends.
- The initial multi-cluster GatewayClass accepts only Fleet `ServiceImport` backend references,
  matching the separation used by GKE multi-cluster GatewayClasses. A `Service` backend is rejected
  for this class.
- Gateway API conformance claims must describe this class-specific backend restriction. A future
  single-cluster GatewayClass may support `Service`, but it is outside this design.

## Alignment with the GKE multi-cluster Gateway model

The GKE multi-cluster Gateway setup is a useful operational reference even though the Azure
resource model and network topology differ. This design intentionally follows its major control
plane patterns:

| GKE multi-cluster Gateway requirement or behavior | Fleet/AFD equivalent | Alignment |
|---|---|---|
| All workload clusters are registered to one fleet | All target clusters are joined to one Fleet hub and have healthy `MemberCluster` state | Aligned |
| Multi-cluster Services is enabled | Fleet member and hub networking controllers reconcile `ServiceExport` and `ServiceImport` | Aligned |
| A selected configuration cluster hosts Gateway resources | The Fleet hub is the configuration cluster for `Gateway`, `HTTPRoute`, and `ServiceImport` | Aligned |
| The platform installs multi-cluster GatewayClasses | The Fleet Gateway chart installs `azure-fleet-afd` | Aligned |
| A hosted controller programs global infrastructure | A dedicated hub Gateway controller manager programs AFD | Same responsibility; different hosting model |
| Workload identity is required | The controller uses Azure workload identity or another approved managed identity mechanism | Aligned |
| Multi-cluster Gateway supports only `ServiceImport` backends | `azure-fleet-afd` accepts only Fleet `ServiceImport` backends | Aligned |
| MCS requirements also apply to Gateway backends | A ServiceImport must be healthy and have resolvable member exports before a route is programmed | Aligned |
| Controller/API enablement is explicit and observable | The feature is enabled explicitly and GatewayClass acceptance confirms readiness | Aligned |
| Configuration-cluster changes can orphan resources | Hub/configuration-plane migration uses an explicit handoff procedure and fails static without ownership proof | Aligned risk treatment |
| Regional control-plane failure causes fail-static behavior | Existing AFD data-plane state remains unchanged while the hub controller is unavailable | Aligned |
| Load balancer quotas apply | AFD profile, endpoint, route, origin, WAF, and Private Link quotas are preflighted and monitored | Aligned |
| Clusters must share supported project/VPC topology | Azure topology is defined by AFD origin reachability and identity authorization, not a same-VNet rule | Intentionally different |

The Azure implementation does not copy GKE-specific requirements such as VPC-native clusters,
proxy-only subnets, Google APIs, Shared VPC roles, or the `HttpLoadBalancing` add-on. Their Azure
equivalents are AKS/Fleet registration, Azure resource-provider registration, managed identity,
AFD reachability, and PLS readiness.

## Portability model

Annotations are used to keep Azure configuration outside the portable Gateway API routing model.
They do not make Azure-specific behavior portable by themselves. Portability is achieved through
graceful degradation:

1. Listener and routing intent stays in Gateway API fields.
2. Azure annotations are optional and use documented defaults.
3. A non-Azure controller can ignore the annotations.
4. Removing the Azure annotations does not alter host, path, header, method, or backend routing
   intent.
5. No Azure resource identifier appears in `HTTPRoute.spec`.

There is one explicit limitation: Fleet's custom `ServiceImport` group is not directly portable to
controllers that only support the upstream MCS API. Such controllers must add support for Fleet's
group or translate the reference outside this design.

## User-facing resource model

### Hub cluster

This architecture requires an Azure Kubernetes Fleet Manager resource with a managed hub. A
hubless Fleet Manager provides an ARM management boundary but does not provide the Kubernetes
configuration cluster, hub-side `ServiceImport` aggregation, or controller placement required by
this design. Hubless fleets must be upgraded to a managed hub before enabling Gateway API
integration; managed-hub to hubless downgrade is not supported.

The managed Fleet hub acts as the Gateway API configuration cluster. It contains:

- One platform-managed `GatewayClass` for the AFD implementation.
- One or more platform-operator or authorized-ingress-owner-created `Gateway` resources.
- `HTTPRoute` resources created by the platform operator or authorized ingress owner.
- Existing Fleet `ServiceImport` resources.
- `ReferenceGrant` resources when a route references a backend in another namespace.

The initial implementation supports exactly one active configuration plane: the Fleet hub where
the Gateway controller is installed. Gateway resources are not copied to member clusters.
Application teams configure member Services and ServiceExports; they need not have hub access
or edit the generated ServiceImport.

### Member clusters

Each member cluster contains:

- The application workload.
- A Kubernetes `Service`.
- A Fleet `ServiceExport`.
- An internal `LoadBalancer` Service configured to request PLS creation.

### Azure

The Gateway controller owns:

- AFD profile and endpoint.
- Origin groups and origins.
- Routes and custom domains.
- Security-policy attachment to an existing WAF policy.
- Private Link origin configuration and approval status observation.

The controller does not own:

- The AKS-managed internal load balancer.
- The PLS created for the member Service.
- An externally managed WAF policy.
- Public DNS records unless a later design explicitly adds DNS integration.

## Environment requirements

These requirements are the Fleet/AFD counterpart of the GKE multi-cluster Gateway preparation
requirements.

### Fleet and hub requirements

- The Fleet Manager has a managed hub. Hubless Fleet Manager resources are unsupported by this
  architecture.
- All target AKS clusters are registered as healthy members of the same Fleet.
- Fleet member and hub networking controllers are installed and healthy.
- Gateway API CRDs for the selected supported version are installed on the hub.
- The dedicated hub Gateway controller manager and its `azure-fleet-afd` GatewayClass are installed.
- The GatewayClass reports `Accepted=True` before users create Gateways.
- The hub has a durable lifecycle and backup/recovery process because it is the configuration
  cluster.

To upgrade an existing hubless Fleet Manager, enable its managed hub and then reconcile existing
members as described in the
[Fleet hub upgrade guidance](https://learn.microsoft.com/azure/kubernetes-fleet/upgrade-hub-cluster-type).
The upgrade is one-way: a Fleet Manager with a managed hub cannot be converted back to hubless.

### Member-cluster requirements

- The Fleet member networking agent is installed and can publish internal export state to the hub.
- Each multi-cluster backend has a valid `Service` and matching `ServiceExport`.
- Service names, namespaces, and ports satisfy current Fleet ServiceImport conflict rules.
- All ingress origins use a supported internal load balancer and PLS configuration with no
  public frontend.

Gateway API CRDs are not required on member clusters for the initial architecture because Gateway
and HTTPRoute resources live only on the hub.

### Azure requirements

- Required Azure resource providers, including `Microsoft.Cdn` and `Microsoft.Network`, are
  registered in every subscription used by controller-owned resources or origins.
- The controller has an approved managed identity and scoped Azure RBAC.
- The identity can manage AFD resources in the configured resource group.
- The identity can read externally managed WAF policies that users attach.
- The member identity used for private-origin discovery can read required load balancer and PLS
  state independently of Traffic Manager enablement.
- Subscription and AFD quotas are sufficient for the requested Gateways, routes, origins, custom
  domains, WAF associations, and private endpoints.
- Private Link origins use AFD Premium and a supported origin type, Azure region, and approval
  topology.

### Enablement and readiness

Feature enablement is explicit:

1. Install Gateway API CRDs on the hub.
2. Install the hub Gateway controller with AFD disabled.
3. Configure identity, subscription, resource group, and Azure cloud.
4. Enable the AFD feature.
5. Verify controller health and readiness.
6. Verify `GatewayClass/azure-fleet-afd` reports `Accepted=True`.
7. Verify Fleet ServiceExport/ServiceImport reconciliation is healthy.
8. Create Gateway and HTTPRoute resources.

## Example configuration

### GatewayClass

The platform installs the class. Users reference it but do not modify it.

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: GatewayClass
metadata:
  name: azure-fleet-afd
spec:
  controllerName: networking.fleet.azure.com/afd
```

Provider-wide configuration such as subscription, resource group, identity, tags, and Azure
cloud environment are controller deployment configuration. They are not repeated on every
Gateway. Premium is mandatory and is used when the SKU annotation is omitted.

### Gateway without WAF

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: global-ingress
  namespace: store
spec:
  gatewayClassName: azure-fleet-afd
  listeners:
  - name: http
    protocol: HTTP
    port: 80
    hostname: store.example.com
    allowedRoutes:
      namespaces:
        from: Same
```

### HTTPRoute to Fleet ServiceImport

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: store
  namespace: store
spec:
  parentRefs:
  - name: global-ingress
  hostnames:
  - store.example.com
  rules:
  - matches:
    - path:
        type: PathPrefix
        value: /api
    backendRefs:
    - group: networking.fleet.azure.com
      kind: ServiceImport
      name: store-api
      port: 8080
      weight: 100
```

The referenced `ServiceImport` remains unchanged:

```yaml
apiVersion: networking.fleet.azure.com/v1alpha1
kind: ServiceImport
metadata:
  name: store-api
  namespace: store
```

### Cross-namespace backend

Gateway API `ReferenceGrant` controls cross-namespace access:

```yaml
apiVersion: gateway.networking.k8s.io/v1beta1
kind: ReferenceGrant
metadata:
  name: allow-store-route
  namespace: store-backends
spec:
  from:
  - group: gateway.networking.k8s.io
    kind: HTTPRoute
    namespace: store
  to:
  - group: networking.fleet.azure.com
    kind: ServiceImport
```

## Annotation contract

All annotations are optional unless stated otherwise. Unknown annotations under the
`networking.fleet.azure.com` prefix are rejected only when they use the reserved AFD annotation
family. Other Fleet annotations remain unaffected.

### Gateway annotations

| Annotation | Values | Default | Purpose |
|---|---|---|---|
| `networking.fleet.azure.com/afd-sku` | `Premium_AzureFrontDoor` only | `Premium_AzureFrontDoor` | Optionally declares the mandatory Premium SKU; Standard is rejected. |
| `networking.fleet.azure.com/afd-resource-group` | Reserved | None | Rejected until a separate per-Gateway ownership design is approved. |
| `networking.fleet.azure.com/afd-waf-policy-id` | Full Azure resource ID | None | Attaches an existing AFD WAF policy. |

The controller generates stable Azure resource names from the Gateway UID. User-selected Azure
profile names are not part of the initial annotation contract because they complicate ownership,
adoption, collision handling, and deletion.

The initial implementation does not allow a per-Gateway resource-group override. The resource
group is controller-wide configuration so that Azure RBAC, ownership, quota accounting, and
deletion remain bounded. The `afd-resource-group` annotation is reserved but rejected until a
separate ownership design enables it.

Annotation keys and values are an implementation API. Existing meanings and defaults cannot change
in place. A replacement must use a new annotation key, support a documented overlap period, and
emit deprecation warnings before the old key is removed.
The earlier multi-SKU and backend annotation contract existed only in the unreleased foundation.
The private-only product contract replaces it before release; this is not a released-API migration.

### Platform-owned backend settings

No AFD annotations are accepted on generated ServiceImports. Private Link is mandatory without
configuration. The approved initial HTTP-preview policy is HTTP GET `/` every 30 seconds on the
selected backend port, preserving the incoming request Host. The pure backend resolver includes
this controller-owned policy in desired state. Applications must return HTTP 200 to probes;
actual probe Host behavior and application responses must be verified before provider enablement.
This is not an implemented Azure programming or health/readiness claim.

The unreleased `afd-origin-connectivity`, `afd-health-probe-path` and `afd-origin-host-header`
annotations are retired. Remove them from foundation examples or patches. Validation in the AFD
path rejects them with an actionable error rather than silently consuming or ignoring them.
Other Fleet annotations remain unaffected, and validation does not mutate the object.

<!-- Decision: Customers may not have hub access and must not configure generated
ServiceImports. Private Link is invariant. If backend overrides are required later,
consider a platform-owned ConfigMap; scope, defaults and reconciliation require a
separate approved design. No ConfigMap API or customer probe path is introduced here. -->

### Existing ServiceExport weight annotation

The existing annotation remains the source of per-cluster origin weight:

```text
networking.fleet.azure.com/weight
```

`ServiceExport` weight controls traffic between member-cluster origins behind one logical
`ServiceImport`. Multiple weighted logical backendRefs require a proven AFD mapping before they
are advertised; the first preview targets one logical backend per rule.

## Annotation validation

Annotations are strings and do not receive CRD OpenAPI validation. Misspelled keys and invalid
values may be accepted by the Kubernetes API server. The implementation compensates with:

1. A single typed annotation parsing package.
2. Table-driven unit tests for all defaults, accepted values, malformed values, and conflicts.
3. A validating admission webhook for deterministic failures where practical.
4. Reconciliation-time validation as the authoritative fallback.
5. Gateway API conditions and Kubernetes warning events with actionable messages.
6. No silent fallback from an explicitly requested capability.

Examples:

- An unknown SKU sets the Gateway `Accepted` condition to `False`.
- A malformed WAF policy ID sets `Accepted=False`.
- Standard or another non-Premium SKU sets `Accepted=False`.
- A retired AFD annotation on ServiceImport produces an explicit backend validation error.
- A private backend missing PLS information sets the route `ResolvedRefs=False`.
- A backend containing a public member origin sets `ResolvedRefs=False`.

The controller must never interpret a malformed value as the default. Defaults apply only when an
annotation is absent.

## Controller architecture

### Process boundary

AFD support is implemented in a separate hub-side controller manager:

```text
cmd/hub-gateway-controller-manager
```

This binary runs in the hub cluster and contains the Gateway API controllers and Azure Front Door
clients. It remains separate from `cmd/hub-net-controller-manager` to provide:

- Independent feature enablement and rollout.
- Separate Azure permissions.
- Independent leader election and scaling.
- Failure isolation from Service export/import and Traffic Manager.
- Clear ownership of ARM rate limits and metrics.

### Proposed packages

```text
cmd/hub-gateway-controller-manager/
pkg/annotations/
pkg/controllers/hub/gatewayclass/
pkg/controllers/hub/gateway/
pkg/controllers/hub/httproute/
pkg/controllers/hub/gatewaymodel/
pkg/providers/azure/frontdoor/
pkg/providers/azure/origin/
```

### Reconciliation flow

1. The existing controllers aggregate member `ServiceExport` resources into a hub
   `ServiceImport`.
2. The Gateway controller watches `GatewayClass`, `Gateway`, `HTTPRoute`, `ReferenceGrant`,
   `ServiceImport`, and relevant internal export resources.
3. It validates the GatewayClass controller name and listener support.
4. It resolves each accepted `HTTPRoute` backend reference.
5. A Fleet `ServiceImport` resolves to its contributing member clusters.
6. Internal export state resolves each member cluster to a verified internal LB frontend and PLS;
   missing discovery never falls back to public-IP metadata.
7. The controller builds a provider-neutral normalized model.
8. The AFD provider reconciles the normalized model into Azure resources.
9. The controller updates Gateway API status only after observing the desired Azure state.

### Normalized model

Gateway API objects should not be translated directly into imperative ARM calls. A normalized
model separates Kubernetes validation from provider operations:

```text
GlobalGateway
  listeners[]
  routes[]
    listenerAttachments[]
    rules[]
      matches[]
      filters[]
      backends[]
        serviceImportPort
        routeWeight
        origins[]
          cluster
          endpoint
          clusterWeight
          privateLinkResourceID
          privateLinkLocation
        platformProbeAndHostPolicy
  wafPolicyID
```

The model is internal Go code, not a CRD. It allows deterministic comparison, unit testing, and
future provider implementations without changing the public API.

The implemented preview model preserves rule boundaries/order, listener-specific hostname
intersections, route creation time and UID, service-port identity and platform probe/Host settings.
It rejects HTTPS/non-80 edge listeners, method matching, filters and multiple logical backends per
rule, and zero logical-backend weights until their provider mappings are implemented. HTTP Exact/PathPrefix data remains
case-preserving; representing it does not establish that AFD route/rule lowering is correct.
Reference authorization and parent selection remain controller responsibilities.

Backend preflight counts one origin group per ServiceImport namespace/name/port and one origin
per active member in each group. Repeated references share these resources; different ports do
not. The HTTP port is an [AFD origin property](https://learn.microsoft.com/en-us/azure/templates/microsoft.cdn/profiles/origingroups/origins).
The runtime Gateway reconciler calls this helper only after resolving all attached
private backends. It checks only backend capacity, not generated AFD route/rule/domain
counts. Rule and listener attachment fidelity and private-origin validation are
implemented; provider lowering and complete generated-resource preflight remain pending.

## ServiceImport resolution

The public `ServiceImport` schema remains unchanged. The Gateway controller uses:

- `ServiceImport.status.ports` to validate the backend port.
- `ServiceImport.status.clusters` to identify contributing member clusters.
- `InternalServiceExport.spec.serviceReference` to map an export to its source cluster.
- Existing per-cluster weights, published independently of Traffic Manager enablement.
- New internal-only ILB/PLS discovery fields when private discovery is implemented.

Adding PLS information to `InternalServiceExport` or another internal transport resource is allowed
because this design's compatibility promise applies to the public `ServiceImport` API. Internal
schema changes must remain backward-compatible during mixed-version upgrades.

## HTTPRoute behavior

### First preview target features

- Fleet `ServiceImport` is the only supported `backendRef` kind for the multi-cluster GatewayClass.
- Hostname matching.
- Exact and path-prefix matching.
- One logical backend per rule, with existing per-member ServiceExport weights.
- Same-namespace and `ReferenceGrant`-authorized cross-namespace ServiceImport references.

These are implementation targets, not current foundation capabilities. Header/method matches,
multiple weighted backendRefs, redirects, rewrites and header modification require separately
proven AFD mappings and tests before being advertised. Rule precedence and service-port mapping
must be resolved before implementing even the initial subset.

### Unsupported features

An unsupported listener, match, or filter must produce the applicable Gateway API condition and
must not be silently ignored. The initial implementation publishes its supported Gateway API
features through `GatewayClass.status.supportedFeatures` when supported by the selected Gateway API
version.

A core Kubernetes `Service` backend is unsupported by `azure-fleet-afd`. This is a deliberate
multi-cluster class restriction, consistent with the GKE multi-cluster Gateway model, rather than
an attempt to reinterpret `Service` as a multi-cluster backend.

### Backend validation

A Fleet ServiceImport backend is resolved only when:

- Group is exactly `networking.fleet.azure.com`.
- Kind is exactly `ServiceImport`.
- The object exists and is authorized.
- The requested port exists in `ServiceImport.status.ports`.
- At least one valid member origin can be resolved.
- All contributing origins have valid internal load balancer and PLS discovery state.

Private endpoint approval is observed separately from reference validity. Pending approval keeps
programming incomplete; a valid reference need not become unresolved solely because approval is
pending.

## AFD resource model

The initial mapping is:

| Gateway API concept | AFD resource |
|---|---|
| `Gateway` | Premium profile and endpoint |
| Gateway listener hostname | Custom domain and route domain association |
| `HTTPRoute` | One or more AFD routes/rule sets |
| `ServiceImport` backend | Origin group; service-port identity requires a proven mapping |
| Member-cluster export | Private Link origin |
| `backendRefs.weight` | Logical backend distribution only after semantic mapping is proven |
| `ServiceExport` weight | Per-cluster origin weight |
| WAF annotation | Security policy association |

One Gateway owns one AFD profile in the initial implementation. This provides a simple ownership
and deletion boundary. Profile sharing can be considered separately after the controller's
isolation and quota behavior are understood.

Owned Azure resources that support tags receive:

- Fleet hub identity.
- Kubernetes namespace and name.
- Kubernetes UID.
- Controller identifier.

The Kubernetes UID, not only the object name, is used for ownership checks. Azure child resources
without tags use verified parent ownership and stable child identities.

The root-request/ownership helpers now require the hub `kube-system` Namespace UID
as the stable hub identity. Runtime integration must read this from the trusted hub
API; it is not a customer-supplied annotation. Profile and endpoint tags use the
Azure-safe `networking.fleet.azure.com.` prefix with `controller`, `hub-uid`,
`gateway-namespace`, `gateway-name` and `gateway-uid` suffixes.
An observed profile must match both its expected subscription/resource-group/root
ARM ID and every owner tag. Tag keys and ARM IDs are compared case-insensitively;
owner values are exact, and missing or ambiguous tags fail closed. Configuration
drift does not relinquish ownership or prevent eventual owned-resource deletion.

Pure root requests select Premium in `global` and start the endpoint Disabled
without inventing a hostname. These helpers do not establish persisted ownership,
register finalizers, create Azure resources or authorize child mutations by
themselves; complete graph validation and runtime lifecycle integration remain open.

## Optional WAF

WAF is enabled by adding a full Azure resource ID to the Gateway:

```yaml
metadata:
  annotations:
    networking.fleet.azure.com/afd-waf-policy-id: >-
      /subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/security-rg/providers/Microsoft.Network/frontdoorWebApplicationFirewallPolicies/store-waf
```

Behavior:

- Absence means no WAF security-policy association.
- The controller attaches the existing policy; it does not create or modify policy rules.
- The controller validates the resource ID shape, policy accessibility, and SKU compatibility.
- Removing the annotation removes only the association, not the WAF policy.
- Deleting the Gateway never deletes an externally managed WAF policy.
- Policy detection/prevention mode remains owned by the WAF policy resource.

This keeps security policy lifecycle separate from application routing lifecycle.

## SFI-NS253 alignment for AFD, WAF, and PLS

The private-backend topology is designed to support SFI-NS253:

```text
Internet
  → Azure Front Door Premium
  → optional Azure Front Door WAF policy
  → Azure Private Link
  → member-cluster Private Link Service
  → internal Azure Load Balancer
  → application
```

In this topology:

- Member workloads do not require public IP addresses or public load balancer frontends.
- AFD is the public edge and reaches every member origin through Private Link.
- WAF is optional in the API contract; environments that require WAF for SFI or another security
  baseline must enforce an approved policy through platform admission or deployment policy.
- Private Link connectivity cannot downgrade to a public origin, even without annotations.
- Missing required PLS discovery keeps the backend unresolved; pending connection approval keeps
  the Gateway not programmed without conflating approval with reference authorization.

Public-backend AFD topology is not supported by this product. Existing Traffic Manager public
endpoint behavior remains independent and unchanged.

This PR establishes the design, configuration contract, validation primitives, and controller
foundation. It does not by itself demonstrate SFI-NS253 compliance: PLS discovery, AFD Private Link
origin reconciliation, policy enforcement, deployment evidence, and end-to-end validation remain
implementation-plan work.

## Mandatory Private Link Service connectivity

### Member Service

The member-cluster Service requests an internal load balancer and PLS through supported AKS cloud
provider annotations:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: store-api
  namespace: store
  annotations:
    service.beta.kubernetes.io/azure-load-balancer-internal: "true"
    service.beta.kubernetes.io/azure-pls-create: "true"
spec:
  type: LoadBalancer
  ports:
  - name: http
    port: 8080
    targetPort: 8080
```

The matching `ServiceExport` remains unchanged.

### Connectivity policy

- AFD Premium and Private Link are mandatory; there is no selectable connectivity mode.
- Every contributing origin needs a valid internal endpoint, PLS resource ID and location.
- Missing, mixed or public topology is rejected; no public-IP fallback is allowed.
- No customer ServiceImport annotation enables or overrides this policy.

### Asynchronous approval

> [!IMPORTANT]
> AFD Private Link approval is asynchronous. A request may not yet exist or be visible,
> may remain pending owner approval, and still require provisioning after approval.
> Neither an empty pending-request list nor successful approval submission proves readiness.

The initial milestone requires authorized manual approval and bounded observation of Azure
state without requiring a Kubernetes edit to trigger progress. Correlate requests to the owned
AFD origin and distinguish Pending, Approved, Rejected, Disconnected and provisioning failures.
Do not report `Programmed=True` before the required state has been observed.

**Open action item:** Work with the AFD and PLS teams on approval automation, including request
correlation and PLS-owner authorization. This is not an available capability or permission to
enable broad auto-approval; the manual-approval milestone does not depend on it.

### Discovery

The member networking controller discovers:

- PLS resource ID.
- Azure location.
- Provisioning state.
- Any stable information required to correlate the PLS with the exported Service.

The information is transported to the hub through an internal Fleet networking resource. It is
not added to public `ServiceImport.status`.

### Constraints

- Only Private Link origins are accepted in every origin group.
- The controller never downgrades to public connectivity.
- Private endpoint approval is asynchronous; status remains not programmed until Azure reports
  a usable connection.
- PLS deletion or replacement triggers origin reconciliation without changing the ServiceImport.
- Direct per-Service origins are the initial model. Shared per-cluster Gateway origins are deferred.

## TLS and certificates

The initial milestone should support HTTP so that routing and origin reconciliation can be
validated independently. HTTPS requires a separate implementation decision because AFD certificate
sources do not map perfectly to Kubernetes TLS Secrets.

The preferred first HTTPS option is an AFD-managed certificate for a listener hostname. Bring-your-
own certificate support, including Key Vault/LUMA-managed certificates, requires a documented
reference mechanism and identity permissions. Certificate identifiers must not be stored as secret
values in annotations.

HTTPS design completion is required before declaring the feature production ready, but it does not
block the initial private-origin HTTP preview. That preview is not a production HTTPS or
compliance claim.

## Status and events

The implementation follows Gateway API status conventions.

### GatewayClass

- `Accepted=True` when `controllerName` is recognized and controller configuration is valid.
- `Accepted=False` for invalid platform configuration.

### Gateway

- `Accepted` reflects API, annotation, and listener validation.
- `Programmed` becomes true only after the AFD profile, endpoint, domains, and required security
  associations match the desired state and required private connections have converged.
- `addresses` reports the AFD endpoint hostname.
- Listener conditions report unsupported protocols, hostname conflicts, and route attachment state.

### HTTPRoute

For each parent:

- `Accepted` reports listener attachment.
- `ResolvedRefs` reports ServiceImport, port, namespace authorization, and origin resolution.
- Implementation-specific conditions may report Azure programming failures without replacing
  standard conditions.

### Events

Warning events are emitted for:

- Invalid annotations.
- Unsupported Gateway API features.
- Missing or incompatible ServiceImport ports.
- Missing internal endpoint or PLS information.
- Azure authorization or quota failures.
- Private endpoint approval requirements.

Repeated events must be rate limited.

## Ownership, deletion, and drift

- The Gateway receives a finalizer only before the controller creates an owned Azure resource.
- Deleting a route removes only route-owned configuration.
- Deleting a Gateway removes its owned AFD resources before removing the finalizer.
- Externally managed WAF policies and member-cluster PLS resources are never deleted.
- Azure resources not tagged with the expected Gateway UID are not adopted or deleted.
- Reconciliation is idempotent and compares normalized desired state with observed Azure state.
- Out-of-band edits to owned properties are corrected.
- Out-of-band edits to unowned external resources are observed but not overwritten.

### Configuration-plane migration

Changing or rebuilding the hub configuration plane is an explicit migration, not an ordinary
controller restart:

1. Preserve all Gateway, HTTPRoute, ReferenceGrant, ServiceImport, and ownership identity data.
2. Start the replacement controller in observation-only mode.
3. Verify every Azure resource is tagged with and attributable to the expected Gateway UID.
4. Transfer reconciliation ownership.
5. Disable the old controller only after the replacement reports complete observation.

If ownership cannot be proven, the replacement controller fails static: it reports status and does
not create, adopt, update, or delete Azure resources. Disabling the feature while Gateways still
exist is rejected or prominently warned because it can leave intentionally persistent Azure
resources without an active reconciler.

### Control-plane outage behavior

AFD continues serving the last successfully programmed configuration when the Fleet hub,
Kubernetes API, controller, or Azure management plane is unavailable. During the outage:

- No speculative changes are made.
- Kubernetes desired-state changes remain pending.
- Existing AFD data-plane traffic continues subject to Azure Front Door availability.
- Reconciliation resumes from observed state after recovery.

## Security and authorization

- The controller uses workload identity or another supported managed identity mechanism.
- Azure permissions are scoped to the configured AFD resource group where practical.
- WAF policy access is read plus association; WAF policy mutation is not required.
- Member networking requires read access to load balancer and PLS state needed for discovery.
- Full Azure resource IDs are identifiers, not credentials.
- Secrets, private keys, tokens, and connection strings are forbidden in annotations.
- `ReferenceGrant` is mandatory for cross-namespace ServiceImport references.
- Gateway and route status must not expose credentials or sensitive Azure responses.

## Coexistence with Traffic Manager

Traffic Manager remains the DNS-based global routing solution for scenarios such as:

- Non-HTTP protocols.
- DNS-level failover.
- Existing public endpoints.
- Workloads that do not need an edge proxy, WAF, or HTTP routing.

AFD handles HTTP(S), edge proxying, application routing, optional WAF, and mandatory Private Link.

The controllers are independent. A ServiceImport may be referenced by both products, but each
controller owns only its Azure resources. The implementation should emit a warning when
configuration creates an unsupported or ambiguous endpoint topology, but it must not modify
Traffic Manager resources.

## Repository ownership

### `fleet-networking`

All implementation changes belong in this repository:

- Gateway API dependencies and scheme registration.
- Gateway controller manager binary and chart.
- GatewayClass, Gateway, HTTPRoute, and ReferenceGrant reconcilers.
- Annotation parsing and validation.
- ServiceImport and internal export resolution.
- AFD, WAF association, and PLS origin clients.
- Status, events, metrics, tests, examples, and documentation.

### `fleet`

No controller or API changes are required. Optional follow-up changes may add:

- Placement examples that deploy Services and ServiceExports.
- Documentation that identifies the hub as the Gateway configuration cluster.
- Cluster capability labels if placement later depends on Private Link support.

### AKS cloud provider

The existing Service annotation contract remains responsible for internal load balancer and PLS
provisioning. Changes are required only if current status does not expose enough stable information
for Fleet member networking to discover the PLS.

## Feasibility

| Capability | Feasibility | Key consideration |
|---|---|---|
| GatewayClass/Gateway/HTTPRoute controller | High | Standard controller-runtime integration. |
| Existing Fleet ServiceImport backend | High | Group is implementation-specific and cannot claim upstream GEP Extended conformance. |
| GKE-style hub/config-cluster model | High | Fleet hub already provides a central configuration and membership plane. |
| Host/path routing | High | Directly maps to AFD routes and rule sets for a defined supported subset. |
| Existing WAF policy attachment | High | Requires resource ID validation, permissions, and SKU checks. |
| PLS discovery | Medium | Internal transport must carry PLS resource ID, location, and readiness. |
| AFD Private Link origins | Medium | Requires Premium capability, asynchronous approval handling, and strict topology validation. |
| Annotation validation | Medium-high | Webhook and reconciliation validation compensate for lack of CRD schema. |
| HTTPS with AFD-managed certificate | Medium-high | Requires domain ownership and provisioning lifecycle handling. |
| Key Vault/LUMA certificate integration | Medium | Requires a separate certificate reference and authorization design. |
| Formal GEP-1748 Extended conformance | Not targeted | Requires upstream `multicluster.x-k8s.io/ServiceImport`. |

The design is technically feasible. The largest delivery risks are Private Link lifecycle handling,
certificate integration, and maintaining clear status through asynchronous ARM operations. Keeping
the public ServiceImport unchanged is not a technical blocker, but it is a deliberate conformance
tradeoff.

## Rollout

1. Ship the controller and chart behind an explicit feature flag.
2. Require healthy Fleet membership, Service export/import, managed identity, and Azure provider
   registration before accepting the GatewayClass.
3. Align the Premium/private-only configuration contract and remove customer ServiceImport settings.
4. Implement member PLS discovery, private origin resolution and Gateway API reconciliation.
5. Prove the first HTTP/private-origin preview with observed manual approval, packaging and E2E.
6. Add HTTPS and certificate lifecycle.
7. Run Gateway API conformance tests for the features applicable to the multi-cluster class.
8. Publish Fleet ServiceImport behavior as implementation-specific Extended support.
9. Promote only after upgrade, configuration-plane migration, deletion, Azure throttling, quota,
   and control-plane failure testing.

Optional WAF and additional routing capabilities are independently gated on proven provider
mappings and tests. Follow the [implementation plan](./gep-1748-implementation-plan.md) for
dependencies, tests-first tasks and milestone criteria.

## Open questions

1. Which HTTPRoute filters map precisely enough to AFD to advertise as supported?
2. Should AFD-managed certificates be the only initial HTTPS mode?
3. How should Private Link approval ownership be represented when manual approval is required?
4. Should one Gateway always own one AFD profile, or should profile sharing be designed later?
5. What mixed-version compatibility window is required for internal PLS transport fields?
6. Does the approved fixed HTTP-preview probe policy work with actual AFD probe Host behavior and target applications?
7. What exact AFD mapping preserves service-port identity, rule precedence and weighted backends?

Detailed quotas, capacity formulas, the proposed initial support envelope, multitenancy guidance,
and release-validation scenarios are maintained in
[GEP-1748 Azure Front Door scale and capacity](./gep-1748-scale-and-capacity.md). Those proposed
values do not describe implemented behavior in the current controller-foundation release.

## References

- [Configure Gateway API with Azure Front Door](../howtos/gateway-api-afd-configuration.md)
- [GEP-1748 Azure Front Door scale and capacity](./gep-1748-scale-and-capacity.md)
- [GEP-1748: Gateway API Interaction with Multi-Cluster Services](https://gateway-api.sigs.k8s.io/geps/gep-1748/)
- [Gateway API v1.2.1 type definitions](https://raw.githubusercontent.com/kubernetes-sigs/gateway-api/v1.2.1/apis/v1/gateway_types.go)
- [Gateway API cross-namespace routing](https://gateway-api.sigs.k8s.io/guides/multiple-ns/)
- [GKE multi-cluster Gateway requirements](https://docs.cloud.google.com/kubernetes-engine/docs/how-to/prepare-environment-multi-cluster-gateways#requirements)
- [Azure Front Door Private Link](https://learn.microsoft.com/azure/frontdoor/private-link)
- [Azure Web Application Firewall on Azure Front Door](https://learn.microsoft.com/azure/web-application-firewall/afds/afds-overview)
- `api/v1alpha1/serviceimport_types.go`: Existing Fleet ServiceImport contract.
- `api/v1alpha1/serviceexport_types.go`: Existing Fleet ServiceExport and weight contract.
- `api/v1alpha1/internalserviceexport_types.go`: Current member-to-hub exported Service state.
- `pkg/controllers/hub/serviceimport/controller.go`: Existing ServiceImport aggregation.
- `pkg/controllers/hub/trafficmanagerbackend/controller.go`: Existing Azure global-routing reconciliation patterns.
- `cmd/hub-net-controller-manager/main.go`: Existing hub networking controller process.
