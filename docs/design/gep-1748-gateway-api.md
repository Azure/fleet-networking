# GEP-1748 Gateway API for Fleet Global Ingress

## Document status

- **Status:** Proposed
- **Date:** 2026-08-17
- **Target repository:** `Azure/fleet-networking`
- **Primary API:** Kubernetes Gateway API
- **Gateway API dependency:** `sigs.k8s.io/gateway-api v1.2.1`
- **Multi-cluster backend API:** `networking.fleet.azure.com/v1alpha1`, kind `ServiceImport`
- **Initial Azure provider:** Azure Front Door Standard/Premium

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

Azure-specific behavior is configured through optional annotations instead of new user-facing
Front Door CRDs. The portable routing contract remains `GatewayClass`, `Gateway`, `HTTPRoute`, and
`ServiceImport`. When another Gateway implementation ignores the Azure annotations, the portable
listener and routing intent remains understandable, although the Fleet-specific `ServiceImport`
group requires explicit support from that implementation.

Azure Front Door (AFD) is the first provider implementation. Web Application Firewall (WAF) and
Private Link Service (PLS) are optional capabilities layered onto the same Gateway API model.

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
- Optional private connectivity from AFD to AKS through PLS.
- Standard Gateway API status and ownership behavior.

Creating a second set of Front Door-specific routing CRDs would duplicate concepts already defined
by Gateway API. This design instead makes Gateway API the public ingress API and limits Azure
extensions to annotations.

## Goals

1. Use `GatewayClass`, `Gateway`, and `HTTPRoute` as the only user-facing AFD routing resources.
2. Allow `HTTPRoute.backendRefs` to target the existing Fleet `ServiceImport`.
3. Preserve the existing Fleet `ServiceImport` API without schema, group, version, or semantic
   changes.
4. Implement AFD Standard/Premium as a Gateway controller.
5. Support optional existing WAF policy attachment.
6. Support optional AFD-to-PLS private origin connectivity.
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
- One or more user-created `Gateway` resources.
- User-created `HTTPRoute` resources.
- Existing Fleet `ServiceImport` resources.
- `ReferenceGrant` resources when a route references a backend in another namespace.

The initial implementation supports exactly one active configuration plane: the Fleet hub where
the Gateway controller is installed. Gateway resources are not copied to member clusters.

### Member clusters

Each member cluster contains:

- The application workload.
- A Kubernetes `Service`.
- A Fleet `ServiceExport`.
- For private origins, an internal `LoadBalancer` Service configured to request PLS creation.

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
- Public-origin Services expose the public endpoint information required by the controller.
- Private-origin Services use a supported internal load balancer and PLS configuration.

Gateway API CRDs are not required on member clusters for the initial architecture because Gateway
and HTTPRoute resources live only on the hub.

### Azure requirements

- Required Azure resource providers, including `Microsoft.Cdn` and `Microsoft.Network`, are
  registered in every subscription used by controller-owned resources or origins.
- The controller has an approved managed identity and scoped Azure RBAC.
- The identity can manage AFD resources in the configured resource group.
- The identity can read externally managed WAF policies that users attach.
- The member or hub identity used for origin discovery can read required public IP, load balancer,
  and PLS state.
- Subscription and AFD quotas are sufficient for the requested Gateways, routes, origins, custom
  domains, WAF associations, and private endpoints.
- Private Link origins use supported AFD SKU, origin type, Azure region, and approval topology.

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

Provider-wide defaults such as subscription, default resource group, identity, tags, and Azure
cloud environment are controller deployment configuration. They are not repeated on every
Gateway.

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
| `networking.fleet.azure.com/afd-sku` | `Standard_AzureFrontDoor`, `Premium_AzureFrontDoor` | Controller default | Selects the AFD SKU. |
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

### ServiceImport annotations

| Annotation | Values | Default | Purpose |
|---|---|---|---|
| `networking.fleet.azure.com/afd-origin-connectivity` | `auto`, `public`, `private-link` | `auto` | Selects how AFD reaches all member origins for the logical service. |
| `networking.fleet.azure.com/afd-health-probe-path` | Absolute HTTP path | `/` | Selects the origin-group health probe path. |
| `networking.fleet.azure.com/afd-origin-host-header` | Valid DNS hostname | Derived endpoint hostname | Overrides the Host header sent to origins. |

The connectivity annotation belongs on `ServiceImport`, rather than `HTTPRoute`, because:

- Multiple routes can share one logical backend.
- Connectivity is an origin property, not an HTTP match property.
- The same backend cannot safely be public in one route and private in another while sharing an
  origin group.
- Backend-level configuration avoids repeating annotations across routes.

### Existing ServiceExport weight annotation

The existing annotation remains the source of per-cluster origin weight:

```text
networking.fleet.azure.com/weight
```

`HTTPRoute.backendRefs[*].weight` controls traffic between logical backends. `ServiceExport`
weight controls traffic between member-cluster origins behind one logical `ServiceImport`.

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
- `private-link` with a non-compatible SKU sets `Accepted=False`.
- A private backend missing PLS information sets the route `ResolvedRefs=False`.
- A backend containing a mixture of public and private member origins sets
  `ResolvedRefs=False`.

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
6. Internal export state resolves each member cluster to a public endpoint or PLS.
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
    matches[]
    filters[]
    backends[]
      serviceImport
      routeWeight
      origins[]
        cluster
        endpoint
        clusterWeight
        connectivity
        privateLinkResourceID
        privateLinkLocation
      healthProbe
  wafPolicyID
```

The model is internal Go code, not a CRD. It allows deterministic comparison, unit testing, and
future provider implementations without changing the public API.

## ServiceImport resolution

The public `ServiceImport` schema remains unchanged. The Gateway controller uses:

- `ServiceImport.status.ports` to validate the backend port.
- `ServiceImport.status.clusters` to identify contributing member clusters.
- `InternalServiceExport.spec.serviceReference` to map an export to its source cluster.
- Existing member-networking state for public IP information and per-cluster weight.
- New internal-only PLS discovery fields when Private Link is implemented.

Adding PLS information to `InternalServiceExport` or another internal transport resource is allowed
because this design's compatibility promise applies to the public `ServiceImport` API. Internal
schema changes must remain backward-compatible during mixed-version upgrades.

## HTTPRoute behavior

### Initial supported features

- Fleet `ServiceImport` is the only supported `backendRef` kind for the multi-cluster GatewayClass.
- Hostname matching.
- Exact and path-prefix matching.
- Header matching supported by AFD.
- HTTP method matching supported by AFD.
- Weighted backend references.
- Request redirects supported by AFD.
- URL rewrite and header modification where AFD behavior matches Gateway API semantics.
- Same-namespace and `ReferenceGrant`-authorized cross-namespace ServiceImport references.

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
- All origins are compatible with the selected connectivity mode.

## AFD resource model

The initial mapping is:

| Gateway API concept | AFD resource |
|---|---|
| `Gateway` | Profile and endpoint |
| Gateway listener hostname | Custom domain and route domain association |
| `HTTPRoute` | One or more AFD routes/rule sets |
| `ServiceImport` backend | Origin group |
| Member-cluster export | Origin |
| `backendRefs.weight` | Logical backend distribution |
| `ServiceExport` weight | Per-cluster origin weight |
| WAF annotation | Security policy association |

One Gateway owns one AFD profile in the initial implementation. This provides a simple ownership
and deletion boundary. Profile sharing can be considered separately after the controller's
isolation and quota behavior are understood.

All owned Azure resources receive tags containing:

- Fleet hub identity.
- Kubernetes namespace and name.
- Kubernetes UID.
- Controller identifier.

The Kubernetes UID, not only the object name, is used for ownership checks.

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
- Explicit `private-link` connectivity cannot silently downgrade to a public origin.
- A backend remains unresolved until every required PLS and private endpoint connection is ready.

The public-backend topology described by this design is not the SFI-NS253 topology because member
Services have public load balancer origins. It remains available only for environments where that
exposure is permitted.

This PR establishes the design, configuration contract, validation primitives, and controller
foundation. It does not by itself demonstrate SFI-NS253 compliance: PLS discovery, AFD Private Link
origin reconciliation, policy enforcement, deployment evidence, and end-to-end validation remain
implementation-plan work.

## Optional Private Link Service connectivity

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

### Connectivity modes

`auto`:

- Use Private Link when every valid member origin has discoverable PLS state.
- Otherwise use public connectivity when every valid member origin has a valid public endpoint.
- Reject mixed public/private resolution instead of guessing.

`public`:

- Require all valid member origins to expose a usable public endpoint.
- Ignore PLS state for origin creation.

`private-link`:

- Require an AFD SKU that supports Private Link origins.
- Require all valid member origins to expose discoverable PLS resource IDs and locations.
- Reject the backend until required private endpoint approvals are complete.

### Discovery

The member networking controller discovers:

- PLS resource ID.
- Azure location.
- Provisioning state.
- Any stable information required to correlate the PLS with the exported Service.

The information is transported to the hub through an internal Fleet networking resource. It is
not added to public `ServiceImport.status`.

### Constraints

- Public and Private Link origins are not mixed within one origin group.
- The controller does not silently downgrade explicitly requested `private-link` connectivity.
- Private endpoint approval may be asynchronous; status remains not programmed until Azure reports
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
block the initial controller and public-origin implementation.

## Status and events

The implementation follows Gateway API status conventions.

### GatewayClass

- `Accepted=True` when `controllerName` is recognized and controller configuration is valid.
- `Accepted=False` for invalid platform configuration.

### Gateway

- `Accepted` reflects API, annotation, and listener validation.
- `Programmed` becomes true only after the AFD profile, endpoint, domains, and required security
  associations match the desired state.
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
- Missing public endpoint or PLS information.
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

AFD handles HTTP(S), edge proxying, application routing, WAF, and optional Private Link.

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
| Public AFD origins | High | Existing internal export state already includes public IP information. |
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
3. Start with HTTP and public AFD origins.
4. Add WAF attachment.
5. Add member PLS discovery and private origins.
6. Add HTTPS and certificate lifecycle.
7. Run Gateway API conformance tests for the features applicable to the multi-cluster class.
8. Publish Fleet ServiceImport behavior as implementation-specific Extended support.
9. Promote only after upgrade, configuration-plane migration, deletion, Azure throttling, quota,
   and control-plane failure testing.

## Open questions

1. Which HTTPRoute filters map precisely enough to AFD to advertise as supported?
2. Should AFD-managed certificates be the only initial HTTPS mode?
3. How should Private Link approval ownership be represented when manual approval is required?
4. Should one Gateway always own one AFD profile, or should profile sharing be designed later?
5. What mixed-version compatibility window is required for internal PLS transport fields?

## References

- [Configure Gateway API with Azure Front Door](../howtos/gateway-api-afd-configuration.md)
- [GEP-1748: Gateway API Interaction with Multi-Cluster Services](https://gateway-api.sigs.k8s.io/geps/gep-1748/)
- [Gateway API v1.2.1 type definitions](https://github.com/kubernetes-sigs/gateway-api/blob/v1.2.1/apis/v1/gateway_types.go)
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
